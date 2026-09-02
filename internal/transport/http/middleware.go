package httptransport

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"mime"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/lihongjie0209/audit-service/internal/apperror"
	"github.com/lihongjie0209/audit-service/internal/auth"
	"github.com/lihongjie0209/audit-service/internal/config"
	"github.com/lihongjie0209/audit-service/internal/environment"
	"github.com/lihongjie0209/audit-service/internal/idempotency"
	"github.com/lihongjie0209/audit-service/internal/observability"
	appLimit "github.com/lihongjie0209/audit-service/internal/ratelimit"
	"github.com/lihongjie0209/audit-service/internal/requestid"
	platformauthz "github.com/lihongjie0209/microservice-platform-go/authz"
	platformprincipal "github.com/lihongjie0209/microservice-platform-go/principal"
	"go.opentelemetry.io/otel/trace"
)

const requestIDKey = "request_id"

func RequestID() gin.HandlerFunc {
	return func(c *gin.Context) {
		id := c.GetHeader("X-Request-ID")
		if !requestid.Valid(id) {
			id = requestid.Generate()
		}
		c.Set(requestIDKey, id)
		c.Header("X-Request-ID", id)
		c.Request = c.Request.WithContext(requestid.WithContext(c.Request.Context(), id))
		c.Next()
	}
}

func Environment(profile string) gin.HandlerFunc {
	return func(c *gin.Context) {
		c.Set("environment", profile)
		c.Request = c.Request.WithContext(environment.WithContext(c.Request.Context(), profile))
		c.Next()
	}
}

func IdempotencyKey(logger *slog.Logger) gin.HandlerFunc {
	return func(c *gin.Context) {
		key := c.GetHeader("Idempotency-Key")
		if key == "" {
			c.Next()
			return
		}
		if !idempotency.Valid(key) {
			Fail(c, logger, apperror.Invalid("invalid Idempotency-Key", nil))
			return
		}
		c.Header("Idempotency-Key", key)
		c.Request = c.Request.WithContext(idempotency.WithContext(c.Request.Context(), key))
		c.Next()
	}
}

type idempotencyManager interface {
	Enabled() bool
	Begin(context.Context, string, string) (idempotency.Decision, error)
	Complete(context.Context, string, string, any) error
	Fail(context.Context, string, string, idempotency.Failure) error
}
type responseCapture struct {
	gin.ResponseWriter
	body bytes.Buffer
}

func (w *responseCapture) Write(v []byte) (int, error) {
	_, _ = w.body.Write(v)
	return w.ResponseWriter.Write(v)
}
func (w *responseCapture) WriteString(v string) (int, error) {
	_, _ = w.body.WriteString(v)
	return w.ResponseWriter.WriteString(v)
}
func IdempotencyExecution(manager idempotencyManager, paths []string, logger *slog.Logger) gin.HandlerFunc {
	return func(c *gin.Context) {
		key, ok := idempotency.FromContext(c.Request.Context())
		if !ok || manager == nil || !manager.Enabled() || c.Request.Method != http.MethodPost || !auth.MatchesAny(c.FullPath(), paths) {
			c.Next()
			return
		}
		fingerprint, err := idempotencyFingerprint(c)
		if err != nil {
			Fail(c, logger, apperror.Invalid("read idempotent request", err))
			return
		}
		decision, err := manager.Begin(c.Request.Context(), key, fingerprint)
		if err != nil {
			Fail(c, logger, apperror.Unavailable("idempotency is unavailable", err))
			return
		}
		switch decision.State {
		case idempotency.StateCompleted:
			var response Response
			if err := json.Unmarshal(decision.Response, &response); err != nil {
				Fail(c, logger, apperror.Unavailable("idempotency response is unavailable", err))
				return
			}
			response.RequestID = requestID(c)
			c.AbortWithStatusJSON(http.StatusOK, response)
			return
		case idempotency.StateFailed:
			c.AbortWithStatusJSON(decision.Failure.HTTPStatus, Response{Code: decision.Failure.Code, Message: decision.Failure.Message, Body: nil, RequestID: requestID(c)})
			return
		case idempotency.StateProcessing:
			Fail(c, logger, apperror.RequestInProgress())
			return
		case idempotency.StateConflict:
			Fail(c, logger, apperror.Conflict("idempotency key belongs to a different request", nil))
			return
		case idempotency.StateAcquired:
		default:
			Fail(c, logger, apperror.Unavailable("idempotency state is invalid", nil))
			return
		}
		capture := &responseCapture{ResponseWriter: c.Writer}
		c.Writer = capture
		c.Next()
		persistCtx, cancel := context.WithTimeout(context.WithoutCancel(c.Request.Context()), 2*time.Second)
		defer cancel()
		var response Response
		if err := json.Unmarshal(capture.body.Bytes(), &response); err != nil {
			logger.ErrorContext(c.Request.Context(), "idempotency response was not unified JSON", "error", err, "request_id", requestID(c))
			return
		}
		response.RequestID = ""
		if c.Writer.Status() >= http.StatusBadRequest {
			err = manager.Fail(persistCtx, key, decision.Owner, idempotency.Failure{Code: response.Code, Message: response.Message, HTTPStatus: c.Writer.Status()})
		} else {
			err = manager.Complete(persistCtx, key, decision.Owner, response)
		}
		if err != nil {
			logger.ErrorContext(c.Request.Context(), "persist idempotency result", "error", err, "request_id", requestID(c))
		}
	}
}
func idempotencyFingerprint(c *gin.Context) (string, error) {
	body, err := io.ReadAll(c.Request.Body)
	if err != nil {
		return "", err
	}
	c.Request.Body = io.NopCloser(bytes.NewReader(body))
	subject, _ := c.Get("subject")
	hash := sha256.New()
	_, _ = io.WriteString(hash, fmt.Sprint(subject))
	_, _ = io.WriteString(hash, "\x00"+c.Request.Method+"\x00"+c.FullPath()+"\x00")
	_, _ = hash.Write(body)
	return hex.EncodeToString(hash.Sum(nil)), nil
}

func RequestLogger(logger *slog.Logger) gin.HandlerFunc {
	return func(c *gin.Context) {
		started := time.Now()
		c.Next()
		span := trace.SpanFromContext(c.Request.Context()).SpanContext()
		logger.InfoContext(c.Request.Context(), "http request", "request_id", requestID(c), "trace_id", span.TraceID().String(), "span_id", span.SpanID().String(), "method", c.Request.Method, "path", c.FullPath(), "status", c.Writer.Status(), "duration", time.Since(started), "client_ip", c.ClientIP())
	}
}

func HTTPMetrics(metrics *observability.Metrics) gin.HandlerFunc {
	return func(c *gin.Context) {
		started := time.Now()
		c.Next()
		if !metrics.Enabled() {
			return
		}
		route := c.FullPath()
		if route == "" {
			route = "unmatched"
		}
		metrics.HTTPRequests.WithLabelValues(c.Request.Method, route, strconv.Itoa(c.Writer.Status())).Inc()
		metrics.HTTPDuration.WithLabelValues(c.Request.Method, route).Observe(time.Since(started).Seconds())
	}
}

func Recovery(logger *slog.Logger) gin.HandlerFunc {
	return gin.CustomRecovery(func(c *gin.Context, recovered any) {
		logger.Error("http panic recovered", "request_id", requestID(c), "panic", recovered)
		c.AbortWithStatusJSON(http.StatusInternalServerError, Response{Code: apperror.CodeInternal, Message: "internal server error", Body: nil, RequestID: requestID(c)})
	})
}

func Timeout(timeout time.Duration, logger *slog.Logger) gin.HandlerFunc {
	return func(c *gin.Context) {
		ctx, cancel := context.WithTimeout(c.Request.Context(), timeout)
		defer cancel()
		c.Request = c.Request.WithContext(ctx)
		c.Next()
		if ctx.Err() == context.DeadlineExceeded && !c.Writer.Written() {
			Fail(c, logger, apperror.RequestTimeout())
		}
	}
}

func MaxBody(limit int64) gin.HandlerFunc {
	return func(c *gin.Context) { c.Request.Body = http.MaxBytesReader(c.Writer, c.Request.Body, limit); c.Next() }
}

func RequireJSON() gin.HandlerFunc {
	return func(c *gin.Context) {
		if c.Request.Method == http.MethodPost && c.Request.ContentLength != 0 {
			mediaType, _, err := mime.ParseMediaType(c.GetHeader("Content-Type"))
			if err != nil || mediaType != "application/json" {
				c.AbortWithStatusJSON(http.StatusUnsupportedMediaType, Response{Code: apperror.CodeInvalidArgument, Message: "content type must be application/json", Body: nil, RequestID: requestID(c)})
				return
			}
		}
		c.Next()
	}
}

func SecurityHeaders() gin.HandlerFunc {
	return func(c *gin.Context) {
		c.Header("X-Content-Type-Options", "nosniff")
		c.Header("X-Frame-Options", "DENY")
		c.Header("Referrer-Policy", "no-referrer")
		c.Header("Content-Security-Policy", "default-src 'none'; frame-ancestors 'none'")
		c.Header("Cache-Control", "no-store")
		if c.Request.TLS != nil {
			c.Header("Strict-Transport-Security", "max-age=31536000; includeSubDomains")
		}
		c.Next()
	}
}

func CORS(cfg config.CORS) gin.HandlerFunc {
	allowed := make(map[string]struct{}, len(cfg.AllowedOrigins))
	for _, origin := range cfg.AllowedOrigins {
		allowed[origin] = struct{}{}
	}
	return func(c *gin.Context) {
		if !cfg.Enabled {
			c.Next()
			return
		}
		origin := c.GetHeader("Origin")
		if origin == "" {
			c.Next()
			return
		}
		if _, ok := allowed[origin]; !ok {
			c.AbortWithStatusJSON(http.StatusForbidden, Response{Code: apperror.CodeForbidden, Message: "origin is not allowed", Body: nil, RequestID: requestID(c)})
			return
		}
		c.Header("Access-Control-Allow-Origin", origin)
		c.Header("Vary", "Origin")
		c.Header("Access-Control-Allow-Methods", "POST, GET, OPTIONS")
		c.Header("Access-Control-Allow-Headers", strings.Join(cfg.AllowedHeaders, ", "))
		c.Header("Access-Control-Expose-Headers", strings.Join(cfg.ExposedHeaders, ", "))
		c.Header("Access-Control-Max-Age", strconv.Itoa(int(cfg.MaxAge.Seconds())))
		if c.Request.Method == http.MethodOptions {
			c.AbortWithStatus(http.StatusNoContent)
			return
		}
		c.Next()
	}
}

type RateKeyFunc func(*gin.Context) string

func RateLimit(limiter *appLimit.Limiter, rule config.RateLimitRule, dimension string, keyFunc RateKeyFunc, logger *slog.Logger) gin.HandlerFunc {
	return func(c *gin.Context) {
		if !limiter.Enabled() {
			c.Next()
			return
		}
		key := keyFunc(c)
		if key == "" {
			c.Next()
			return
		}
		result, err := limiter.Allow(c.Request.Context(), "rate:"+dimension+":"+key, rule)
		if err != nil {
			if limiter.FailOpen() {
				logger.Warn("rate limit check failed open", "request_id", requestID(c), "dimension", dimension, "error", err)
				c.Next()
				return
			}
			Fail(c, logger, apperror.Unavailable("rate limiter unavailable", err))
			return
		}
		c.Header("X-RateLimit-Limit", strconv.Itoa(result.Limit))
		c.Header("X-RateLimit-Remaining", strconv.Itoa(result.Remaining))
		if !result.Allowed {
			retry := max(1, int(result.RetryAfter.Round(time.Second)/time.Second))
			c.Header("Retry-After", strconv.Itoa(retry))
			Fail(c, logger, apperror.TooManyRequests())
			return
		}
		c.Next()
	}
}

func JWT(service *auth.Service, logger *slog.Logger) gin.HandlerFunc {
	return func(c *gin.Context) {
		header := c.GetHeader("Authorization")
		scheme, raw, ok := strings.Cut(header, " ")
		if !ok || !strings.EqualFold(scheme, "Bearer") || raw == "" {
			Fail(c, logger, apperror.Unauthorized("missing bearer token"))
			return
		}
		caller, err := service.Verify(c.Request.Context(), raw)
		if err != nil {
			Fail(c, logger, apperror.Unauthorized("invalid or expired token"))
			return
		}
		c.Set("subject", caller.ID)
		ctx := platformprincipal.WithContext(c.Request.Context(), caller)
		c.Request = c.Request.WithContext(platformauthz.WithCallerCredential(ctx, header))
		c.Next()
	}
}

func Authorization(enabled bool, authorizer platformauthz.Authorizer, logger *slog.Logger) gin.HandlerFunc {
	return func(c *gin.Context) {
		requirement, protected := auditHTTPRequirement(c.FullPath())
		if !enabled || !protected {
			c.Next()
			return
		}
		if err := platformauthz.Enforce(c.Request.Context(), authorizer, requirement); err != nil {
			if errors.Is(err, platformauthz.ErrDecisionUnavailable) {
				Fail(c, logger, apperror.Unavailable("authorization decision is unavailable", err))
				return
			}
			Fail(c, logger, apperror.Forbidden("permission denied"))
			return
		}
		c.Next()
	}
}

func auditHTTPRequirement(route string) (platformauthz.Requirement, bool) {
	requirements := map[string]platformauthz.Requirement{
		"/api/v1/audit/records/create": {Resource: "audit.record", Action: "create", Scope: platformauthz.ScopePrincipal},
		"/api/v1/audit/records/get":    {Resource: "audit.record", Action: "read", Scope: platformauthz.ScopePrincipal},
		"/api/v1/audit/records/query":  {Resource: "audit.record", Action: "query", Scope: platformauthz.ScopePrincipal},
		"/api/v1/audit/records/export": {Resource: "audit.record", Action: "export", Scope: platformauthz.ScopePrincipal},
	}
	requirement, ok := requirements[route]
	return requirement, ok
}

func Authentication(service *auth.Service, logger *slog.Logger, cfg config.Auth) gin.HandlerFunc {
	authenticate := JWT(service, logger)
	return func(c *gin.Context) {
		if cfg.PSK.Enabled && auth.MatchesAny(c.FullPath(), cfg.PSK.HTTPPaths) {
			if !auth.VerifyPSK(c.GetHeader("Authorization"), cfg.PSK.Key) {
				Fail(c, logger, apperror.Unauthorized("missing or invalid PSK"))
				return
			}
			c.Set("subject", "psk")
			ctx := platformprincipal.WithContext(c.Request.Context(), platformprincipal.Principal{ID: "audit-service:psk", Type: platformprincipal.TypeServiceAccount})
			c.Request = c.Request.WithContext(platformauthz.WithCallerCredential(ctx, c.GetHeader("Authorization")))
			c.Next()
			return
		}
		if auth.MatchesAny(c.FullPath(), cfg.SkipHTTPPaths) {
			c.Next()
			return
		}
		authenticate(c)
	}
}

func requestID(c *gin.Context) string {
	value, _ := c.Get(requestIDKey)
	id, _ := value.(string)
	return id
}
