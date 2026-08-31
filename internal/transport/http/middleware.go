package httptransport

import (
	"context"
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
		c.Request = c.Request.WithContext(platformprincipal.WithContext(c.Request.Context(), caller))
		c.Next()
	}
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
			c.Request = c.Request.WithContext(platformprincipal.WithContext(c.Request.Context(), platformprincipal.Principal{ID: "audit-service:psk", Type: platformprincipal.TypeServiceAccount}))
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
