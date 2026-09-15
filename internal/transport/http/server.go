package httptransport

import (
	"context"
	"crypto/subtle"
	"errors"
	"fmt"
	"log/slog"
	"net"
	"net/http"
	stdpprof "net/http/pprof"
	"strings"

	"github.com/gin-gonic/gin"
	docs "github.com/lihongjie0209/audit-service/docs"
	"github.com/lihongjie0209/audit-service/internal/auth"
	"github.com/lihongjie0209/audit-service/internal/buildinfo"
	"github.com/lihongjie0209/audit-service/internal/config"
	"github.com/lihongjie0209/audit-service/internal/health"
	"github.com/lihongjie0209/audit-service/internal/idempotency"
	"github.com/lihongjie0209/audit-service/internal/observability"
	"github.com/lihongjie0209/audit-service/internal/ratelimit"
	appPolicy "github.com/lihongjie0209/audit-service/internal/routepolicy"
	platformauthz "github.com/lihongjie0209/microservice-platform-go/authz"
	"github.com/lihongjie0209/microservice-platform-go/operationlog"
	platformpolicy "github.com/lihongjie0209/microservice-platform-go/routepolicy"
	swaggerFiles "github.com/swaggo/files"
	ginSwagger "github.com/swaggo/gin-swagger"
	"go.opentelemetry.io/contrib/instrumentation/github.com/gin-gonic/gin/otelgin"
	"go.uber.org/fx"
)

func NewServer(lc fx.Lifecycle, cfg config.Config, handler *Handler, authService *auth.Service, authorizer platformauthz.Authorizer, policies *appPolicy.Manager, policyRepository *appPolicy.Repository, operationRecorder operationlog.Recorder, limiter *ratelimit.Limiter, idempotencyManager *idempotency.Manager, metrics *observability.Metrics, tracing *observability.Tracing, logger *slog.Logger) (*http.Server, error) {
	if cfg.App.Env == "production" {
		gin.SetMode(gin.ReleaseMode)
	}
	router := gin.New()
	if err := router.SetTrustedProxies(cfg.HTTP.TrustedProxies); err != nil {
		return nil, fmt.Errorf("configure trusted proxies: %w", err)
	}
	_ = tracing
	router.Use(RequestID(), IdempotencyKey(logger), Environment(cfg.Runtime.ActiveProfile), otelgin.Middleware(cfg.App.Name), RequestLogger(logger), Recovery(logger), HTTPMetrics(metrics), SecurityHeaders(), CORS(cfg.HTTP.CORS), MaxBody(cfg.HTTP.MaxBodyBytes), Timeout(cfg.HTTP.RequestTimeout, logger), RequireJSON())
	for _, method := range []string{http.MethodGet, http.MethodPost} {
		router.Handle(method, "/live", handler.Live)
		router.Handle(method, "/ready", handler.Ready)
	}
	if metrics.Enabled() {
		router.GET("/metrics", gin.WrapH(metrics.Handler()))
	}
	if cfg.Observability.PprofEnabled {
		registerPprof(router.Group("/debug/pprof", pprofAuth(cfg.Observability.PprofToken)))
	}
	if cfg.Swagger.Enabled {
		docs.SwaggerInfo.Version = buildinfo.Version
		swagger := router.Group("/swagger")
		if cfg.Swagger.RequireAuth {
			swagger.Use(JWT(authService, logger))
		}
		swagger.GET("/*any", ginSwagger.WrapHandler(swaggerFiles.Handler))
	}
	api := router.Group("/api/v1", RateLimit(limiter, cfg.RateLimit.IP, "ip", func(c *gin.Context) string { return c.ClientIP() }, logger), RateLimit(limiter, cfg.RateLimit.API, "api", func(c *gin.Context) string { return c.FullPath() }, logger), DatabaseAuthentication(authService, logger, cfg), DatabaseAuthorization(cfg.Authorization.Enabled, cfg.App.Name, policies, authorizer, logger), AuditAccessLog(operationRecorder, logger), RateLimit(limiter, cfg.RateLimit.User, "user", func(c *gin.Context) string {
		value, _ := c.Get("subject")
		subject, _ := value.(string)
		return subject
	}, logger))
	api.Use(IdempotencyExecution(idempotencyManager, cfg.Idempotency.HTTPPaths, logger))
	api.POST("/version", handler.Version)
	api.POST("/me", handler.Me)
	api.POST("/audit/records/create", handler.RecordAudit)
	api.POST("/audit/records/get", handler.GetAudit)
	api.POST("/audit/records/query", handler.QueryAudits)
	api.POST("/audit/records/export", handler.ExportAudits)
	api.POST("/route-policies/page", handler.PageRoutePolicies)
	api.POST("/route-policies/get", handler.GetRoutePolicy)
	api.POST("/route-policies/set", handler.SetRoutePolicy)
	server := &http.Server{Addr: cfg.HTTP.Address, Handler: router, ReadTimeout: cfg.HTTP.ReadTimeout, WriteTimeout: cfg.HTTP.WriteTimeout, IdleTimeout: cfg.HTTP.IdleTimeout}
	var listener net.Listener
	policyContext, stopPolicies := context.WithCancel(context.Background())
	lc.Append(fx.Hook{OnStart: func(ctx context.Context) error {
		if cfg.Authorization.Enabled {
			routes, err := discoveredBusinessRoutes(router, cfg.App.Name)
			if err != nil {
				return err
			}
			if err := policyRepository.SyncRoutes(ctx, routes, cfg.App.Name+":route-discovery"); err != nil {
				return fmt.Errorf("sync HTTP routes: %w", err)
			}
			if err := policies.RefreshSource(ctx, "startup-http"); err != nil {
				return fmt.Errorf("load route policies: %w", err)
			}
			if err := policies.ValidateRoutes(ctx, cfg.App.Name); err != nil {
				logger.WarnContext(ctx, "route authorization policy coverage is incomplete; uncovered routes fail closed", "error", err)
			}
			go policies.Run(policyContext)
		}
		var err error
		listener, err = net.Listen("tcp", server.Addr)
		if err != nil {
			return fmt.Errorf("listen http: %w", err)
		}
		go func() {
			if serveErr := server.Serve(listener); serveErr != nil && !errors.Is(serveErr, http.ErrServerClosed) {
				logger.Error("http server stopped unexpectedly", "error", serveErr)
			}
		}()
		logger.Info("http server started", "address", server.Addr)
		return nil
	}, OnStop: func(ctx context.Context) error { stopPolicies(); return server.Shutdown(ctx) }})
	return server, nil
}

func discoveredBusinessRoutes(router *gin.Engine, serviceName string) ([]platformpolicy.Route, error) {
	routes := []platformpolicy.Route{}
	for _, info := range router.Routes() {
		if !strings.HasPrefix(info.Path, "/api/v1/") {
			continue
		}
		route, err := platformpolicy.NewRoute("http", info.Method, info.Path, serviceName, buildinfo.Version)
		if err != nil {
			return nil, fmt.Errorf("describe HTTP route %s: %w", info.Path, err)
		}
		route.Operation = info.Handler
		routes = append(routes, route)
	}
	return routes, nil
}

func pprofAuth(expected string) gin.HandlerFunc {
	return func(c *gin.Context) {
		scheme, token, ok := strings.Cut(c.GetHeader("Authorization"), " ")
		if !ok || !strings.EqualFold(scheme, "Bearer") || subtle.ConstantTimeCompare([]byte(token), []byte(expected)) != 1 {
			c.AbortWithStatus(http.StatusUnauthorized)
			return
		}
		c.Next()
	}
}

func registerPprof(group *gin.RouterGroup) {
	group.GET("/", gin.WrapF(stdpprof.Index))
	group.GET("/cmdline", gin.WrapF(stdpprof.Cmdline))
	group.GET("/profile", gin.WrapF(stdpprof.Profile))
	group.POST("/symbol", gin.WrapF(stdpprof.Symbol))
	group.GET("/symbol", gin.WrapF(stdpprof.Symbol))
	group.GET("/trace", gin.WrapF(stdpprof.Trace))
	for _, profile := range []string{"allocs", "block", "goroutine", "heap", "mutex", "threadcreate"} {
		group.GET("/"+profile, gin.WrapH(stdpprof.Handler(profile)))
	}
}

var Module = fx.Module("http", fx.Provide(auth.NewRuntime, health.New, ratelimit.New, NewHandler, NewServer), fx.Invoke(func(*http.Server) {}))
