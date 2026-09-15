package grpctransport

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"errors"
	"fmt"
	"log/slog"
	"net"
	"os"
	"strings"
	"time"

	"github.com/lihongjie0209/audit-service/internal/apperror"
	auditdomain "github.com/lihongjie0209/audit-service/internal/audit"
	"github.com/lihongjie0209/audit-service/internal/auth"
	"github.com/lihongjie0209/audit-service/internal/buildinfo"
	"github.com/lihongjie0209/audit-service/internal/config"
	"github.com/lihongjie0209/audit-service/internal/environment"
	apphealth "github.com/lihongjie0209/audit-service/internal/health"
	"github.com/lihongjie0209/audit-service/internal/idempotency"
	"github.com/lihongjie0209/audit-service/internal/observability"
	"github.com/lihongjie0209/audit-service/internal/requestid"
	appPolicy "github.com/lihongjie0209/audit-service/internal/routepolicy"
	platformauthz "github.com/lihongjie0209/microservice-platform-go/authz"
	platformidempotency "github.com/lihongjie0209/microservice-platform-go/idempotency"
	"github.com/lihongjie0209/microservice-platform-go/operationlog"
	platformprincipal "github.com/lihongjie0209/microservice-platform-go/principal"
	platformpolicy "github.com/lihongjie0209/microservice-platform-go/routepolicy"

	auditv1 "github.com/lihongjie0209/platform-protos/gen/go/platform/audit/v1"
	commonv1 "github.com/lihongjie0209/platform-protos/gen/go/platform/common/v1"
	"go.opentelemetry.io/contrib/instrumentation/google.golang.org/grpc/otelgrpc"
	"go.opentelemetry.io/otel/trace"
	"go.uber.org/fx"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/credentials"
	grpc_health_v1 "google.golang.org/grpc/health/grpc_health_v1"
	"google.golang.org/grpc/metadata"
	"google.golang.org/grpc/reflection"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/types/known/timestamppb"
)

type Server struct {
	server  *grpc.Server
	address string
	logger  *slog.Logger
}

func NewServer(lc fx.Lifecycle, cfg config.Config, authService *auth.Service, authorizer platformauthz.Authorizer, policies *appPolicy.Manager, policyRepository *appPolicy.Repository, operationRecorder operationlog.Recorder, healthService *apphealth.Service, auditService *auditdomain.Service, idempotencyManager *idempotency.Manager, metrics *observability.Metrics, logger *slog.Logger) (*Server, error) {
	options := []grpc.ServerOption{
		grpc.MaxRecvMsgSize(cfg.GRPC.MaxReceiveBytes),
		grpc.StatsHandler(otelgrpc.NewServerHandler()),
		grpc.ChainUnaryInterceptor(environmentInterceptor(cfg.Runtime.ActiveProfile), requestIDInterceptor, idempotencyInterceptor, recoveryInterceptor(logger), optionalAuthInterceptor(authService, cfg), databaseAuthorizationInterceptor(cfg.Authorization.Enabled, cfg.App.Name, policies, authorizer), auditAccessInterceptor(operationRecorder, logger), platformidempotency.UnaryServerInterceptor(idempotencyManager, cfg.Idempotency.GRPCMethods, logger), metricsInterceptor(metrics, logger)),
		grpc.ChainStreamInterceptor(environmentStreamInterceptor(cfg.Runtime.ActiveProfile), requestIDStreamInterceptor, idempotencyStreamInterceptor, recoveryStreamInterceptor(logger), optionalAuthStreamInterceptor(authService, cfg), databaseAuthorizationStreamInterceptor(cfg.Authorization.Enabled, cfg.App.Name, policies, authorizer), metricsStreamInterceptor(metrics, logger)),
	}
	if cfg.GRPC.TLS.Enabled {
		creds, err := serverCredentials(cfg.GRPC.TLS)
		if err != nil {
			return nil, err
		}
		options = append(options, grpc.Creds(creds))
	}
	grpcServer := grpc.NewServer(options...)
	auditv1.RegisterAuditServiceServer(grpcServer, &auditServer{service: auditService})
	grpc_health_v1.RegisterHealthServer(grpcServer, &healthServer{health: healthService})
	if cfg.GRPC.ReflectionEnabled {
		reflection.Register(grpcServer)
	}
	server := &Server{server: grpcServer, address: cfg.GRPC.Address, logger: logger}
	lc.Append(fx.Hook{OnStart: func(ctx context.Context) error {
		if cfg.GRPC.Enabled && cfg.Authorization.Enabled {
			routes, err := discoveredGRPCRoutes(grpcServer, cfg.App.Name)
			if err != nil {
				return err
			}
			if err := policyRepository.SyncRoutes(ctx, routes, cfg.App.Name+":route-discovery"); err != nil {
				return fmt.Errorf("sync gRPC routes: %w", err)
			}
			if err := policies.RefreshSource(ctx, "startup-grpc"); err != nil {
				return fmt.Errorf("load gRPC route policies: %w", err)
			}
		}
		return server.start(cfg.GRPC.Enabled)(ctx)
	}, OnStop: server.stop})
	return server, nil
}

func discoveredGRPCRoutes(server *grpc.Server, serviceName string) ([]platformpolicy.Route, error) {
	routes := []platformpolicy.Route{}
	for service, info := range server.GetServiceInfo() {
		if service == grpc_health_v1.Health_ServiceDesc.ServiceName {
			continue
		}
		for _, method := range info.Methods {
			path := "/" + service + "/" + method.Name
			route, err := platformpolicy.NewRoute("grpc", "call", path, serviceName, buildinfo.Version)
			if err != nil {
				return nil, err
			}
			route.Operation = path
			routes = append(routes, route)
		}
	}
	return routes, nil
}

type routePolicyEvaluator interface {
	EvaluateRoute(context.Context, string, string, string, string, platformauthz.Authorizer) error
}

func databaseAuthorizationInterceptor(enabled bool, serviceName string, policies routePolicyEvaluator, authorizer platformauthz.Authorizer) grpc.UnaryServerInterceptor {
	return func(ctx context.Context, request any, info *grpc.UnaryServerInfo, handler grpc.UnaryHandler) (any, error) {
		if enabled {
			if err := policies.EvaluateRoute(ctx, "grpc", "call", info.FullMethod, serviceName, authorizer); err != nil {
				if errors.Is(err, platformpolicy.ErrDenied) {
					return nil, status.Error(codes.PermissionDenied, "permission denied")
				}
				return nil, status.Error(codes.Unavailable, "authorization decision is unavailable")
			}
		}
		return handler(ctx, request)
	}
}

func databaseAuthorizationStreamInterceptor(enabled bool, serviceName string, policies routePolicyEvaluator, authorizer platformauthz.Authorizer) grpc.StreamServerInterceptor {
	return func(server any, stream grpc.ServerStream, info *grpc.StreamServerInfo, handler grpc.StreamHandler) error {
		if enabled {
			if err := policies.EvaluateRoute(stream.Context(), "grpc", "call", info.FullMethod, serviceName, authorizer); err != nil {
				if errors.Is(err, platformpolicy.ErrDenied) {
					return status.Error(codes.PermissionDenied, "permission denied")
				}
				return status.Error(codes.Unavailable, "authorization decision is unavailable")
			}
		}
		return handler(server, stream)
	}
}

func auditGRPCRequirement(enabled bool) platformauthz.GRPCResolver {
	return func(method string) (platformauthz.Requirement, bool) {
		if !enabled {
			return platformauthz.Requirement{}, false
		}
		requirements := map[string]platformauthz.Requirement{
			auditv1.AuditService_Record_FullMethodName: {Resource: "audit.record", Action: "create", Scope: platformauthz.ScopePrincipal},
			auditv1.AuditService_Get_FullMethodName:    {Resource: "audit.record", Action: "read", Scope: platformauthz.ScopePrincipal},
			auditv1.AuditService_Query_FullMethodName:  {Resource: "audit.record", Action: "query", Scope: platformauthz.ScopePrincipal},
			auditv1.AuditService_Export_FullMethodName: {Resource: "audit.record", Action: "export", Scope: platformauthz.ScopePrincipal},
		}
		requirement, ok := requirements[method]
		return requirement, ok
	}
}

func auditAccessInterceptor(recorder operationlog.Recorder, logger *slog.Logger) grpc.UnaryServerInterceptor {
	return func(ctx context.Context, request any, info *grpc.UnaryServerInfo, handler grpc.UnaryHandler) (any, error) {
		operation, tracked := auditGRPCAccessOperation(info.FullMethod)
		if !tracked || recorder == nil || !recorder.Enabled() {
			return handler(ctx, request)
		}
		started := time.Now()
		response, err := handler(ctx, request)
		requestID, _ := requestid.FromContext(ctx)
		entry := operationlog.Entry{Operation: operation, ResourceType: "audit_record", Source: "audit-service", Protocol: "grpc", Method: info.FullMethod, Route: info.FullMethod, RequestID: requestID, Duration: time.Since(started), Succeeded: err == nil}
		if err != nil {
			entry.ErrorCode = status.Code(err).String()
			entry.ErrorMessage = status.Code(err).String()
		}
		persistCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 2*time.Second)
		defer cancel()
		if recordErr := recorder.Record(persistCtx, entry); recordErr != nil {
			logger.ErrorContext(persistCtx, "record audit gRPC access operation", "operation", operation, "error", recordErr, "request_id", requestID)
		}
		return response, err
	}
}

func auditGRPCAccessOperation(method string) (string, bool) {
	operations := map[string]string{
		auditv1.AuditService_Get_FullMethodName:    "audit.record.get",
		auditv1.AuditService_Query_FullMethodName:  "audit.record.query",
		auditv1.AuditService_Export_FullMethodName: "audit.record.export",
	}
	operation, ok := operations[method]
	return operation, ok
}

type auditServer struct {
	auditv1.UnimplementedAuditServiceServer
	service *auditdomain.Service
}

func (s *auditServer) Record(ctx context.Context, request *auditv1.RecordRequest) (*auditv1.RecordResponse, error) {
	input := request.GetRecord()
	if input == nil {
		return nil, status.Error(codes.InvalidArgument, "record is required")
	}
	created, err := s.service.Record(ctx, fromProtoAudit(input))
	if err != nil {
		return nil, grpcError(err)
	}
	return &auditv1.RecordResponse{Record: toProtoAudit(created)}, nil
}
func (s *auditServer) Get(ctx context.Context, request *auditv1.GetRequest) (*auditv1.GetResponse, error) {
	value, err := s.service.Get(ctx, request.GetId(), request.GetTenantId())
	if err != nil {
		return nil, grpcError(err)
	}
	return &auditv1.GetResponse{Record: toProtoAudit(value)}, nil
}
func (s *auditServer) Query(ctx context.Context, request *auditv1.QueryRequest) (*auditv1.QueryResponse, error) {
	page, pageSize := 0, 0
	if request.GetPage() != nil {
		page, pageSize = int(request.GetPage().GetPage()), int(request.GetPage().GetPageSize())
	}
	filter := auditdomain.Filter{Keyword: request.GetKeyword(), IDs: request.GetIds(), TenantID: request.GetTenantId(), ApplicationID: request.GetApplicationId(), ActorID: request.GetActorId(), ActorType: request.GetActorType(), Action: request.GetAction(), ResourceType: request.GetResourceType(), ResourceID: request.GetResourceId(), RequestID: request.GetRequestId(), TraceID: request.GetTraceId(), SourceService: request.GetSourceService(), Page: page, PageSize: pageSize}
	if request.GetOccurredFrom() != nil {
		filter.OccurredFrom = request.GetOccurredFrom().AsTime()
	}
	if request.GetOccurredTo() != nil {
		filter.OccurredTo = request.GetOccurredTo().AsTime()
	}
	result, err := s.service.Query(ctx, filter)
	if err != nil {
		return nil, grpcError(err)
	}
	response := &auditv1.QueryResponse{Page: &commonv1.PageResult{Total: uint64(result.Total), Page: uint32(result.Page), PageSize: uint32(result.PageSize)}}
	for _, value := range result.Records {
		response.Records = append(response.Records, toProtoAudit(value))
	}
	return response, nil
}

func (s *auditServer) Export(ctx context.Context, request *auditv1.ExportRequest) (*auditv1.ExportResponse, error) {
	filter := filterFromProto(request.GetFilter())
	content, count, err := s.service.Export(ctx, filter, int(request.GetMaxRecords()))
	if err != nil {
		return nil, grpcError(err)
	}
	return &auditv1.ExportResponse{Content: content, ContentType: "text/csv; charset=utf-8", Filename: "audit-records.csv", RecordCount: count}, nil
}

func filterFromProto(request *auditv1.QueryRequest) auditdomain.Filter {
	if request == nil {
		return auditdomain.Filter{}
	}
	page, pageSize := 0, 0
	if request.GetPage() != nil {
		page, pageSize = int(request.GetPage().GetPage()), int(request.GetPage().GetPageSize())
	}
	filter := auditdomain.Filter{Keyword: request.GetKeyword(), IDs: request.GetIds(), TenantID: request.GetTenantId(), ApplicationID: request.GetApplicationId(), ActorID: request.GetActorId(), ActorType: request.GetActorType(), Action: request.GetAction(), ResourceType: request.GetResourceType(), ResourceID: request.GetResourceId(), RequestID: request.GetRequestId(), TraceID: request.GetTraceId(), SourceService: request.GetSourceService(), Page: page, PageSize: pageSize}
	if request.GetOccurredFrom() != nil {
		filter.OccurredFrom = request.GetOccurredFrom().AsTime()
	}
	if request.GetOccurredTo() != nil {
		filter.OccurredTo = request.GetOccurredTo().AsTime()
	}
	return filter
}

func fromProtoAudit(value *auditv1.AuditRecord) auditdomain.Record {
	result := auditdomain.Record{ID: value.GetId(), TenantID: value.GetTenantId(), ApplicationID: value.GetApplicationId(), ActorID: value.GetActorId(), ActorType: value.GetActorType(), Action: value.GetAction(), ResourceType: value.GetResourceType(), ResourceID: value.GetResourceId(), RequestID: value.GetRequestId(), TraceID: value.GetTraceId(), SourceService: value.GetSourceService(), BeforeSummary: value.GetBeforeSummary(), AfterSummary: value.GetAfterSummary()}
	if value.GetOccurredAt() != nil {
		result.OccurredAt = value.GetOccurredAt().AsTime()
	}
	return result
}
func toProtoAudit(value auditdomain.Record) *auditv1.AuditRecord {
	return &auditv1.AuditRecord{Id: value.ID, TenantId: value.TenantID, ApplicationId: value.ApplicationID, ActorId: value.ActorID, ActorType: value.ActorType, Action: value.Action, ResourceType: value.ResourceType, ResourceId: value.ResourceID, RequestId: value.RequestID, TraceId: value.TraceID, SourceService: value.SourceService, BeforeSummary: value.BeforeSummary, AfterSummary: value.AfterSummary, OccurredAt: timestamppb.New(value.OccurredAt), Version: value.Version, CreatedAt: timestamppb.New(value.CreatedAt), UpdatedAt: timestamppb.New(value.UpdatedAt), CreatedBy: value.CreatedBy, UpdatedBy: value.UpdatedBy}
}

func (s *Server) start(enabled bool) func(context.Context) error {
	return func(context.Context) error {
		if !enabled {
			s.logger.Warn("grpc server is disabled")
			return nil
		}
		listener, err := net.Listen("tcp", s.address)
		if err != nil {
			return fmt.Errorf("listen grpc: %w", err)
		}
		go func() {
			if err := s.server.Serve(listener); err != nil {
				s.logger.Error("grpc server stopped unexpectedly", "error", err)
			}
		}()
		s.logger.Info("grpc server started", "address", s.address)
		return nil
	}
}
func (s *Server) stop(ctx context.Context) error {
	stopped := make(chan struct{})
	go func() { s.server.GracefulStop(); close(stopped) }()
	select {
	case <-stopped:
		return nil
	case <-ctx.Done():
		s.server.Stop()
		return ctx.Err()
	}
}

type healthServer struct {
	grpc_health_v1.UnimplementedHealthServer
	health *apphealth.Service
}

func grpcError(err error) error {
	var appErr *apperror.Error
	if !errors.As(err, &appErr) {
		return status.Error(codes.Internal, "internal server error")
	}
	code := codes.Internal
	switch appErr.Code {
	case apperror.CodeInvalidArgument:
		code = codes.InvalidArgument
	case apperror.CodeUnauthorized:
		code = codes.Unauthenticated
	case apperror.CodeForbidden:
		code = codes.PermissionDenied
	case apperror.CodeNotFound:
		code = codes.NotFound
	case apperror.CodeConflict:
		code = codes.Aborted
	case apperror.CodeDependencyUnavailable:
		code = codes.Unavailable
	}
	return status.Error(code, appErr.Message)
}

func (s *healthServer) Check(ctx context.Context, _ *grpc_health_v1.HealthCheckRequest) (*grpc_health_v1.HealthCheckResponse, error) {
	_, ready := s.health.Ready(ctx)
	serving := grpc_health_v1.HealthCheckResponse_NOT_SERVING
	if ready {
		serving = grpc_health_v1.HealthCheckResponse_SERVING
	}
	return &grpc_health_v1.HealthCheckResponse{Status: serving}, nil
}
func (s *healthServer) List(context.Context, *grpc_health_v1.HealthListRequest) (*grpc_health_v1.HealthListResponse, error) {
	return &grpc_health_v1.HealthListResponse{Statuses: map[string]*grpc_health_v1.HealthCheckResponse{"": {Status: grpc_health_v1.HealthCheckResponse_SERVING}}}, nil
}

func requestIDInterceptor(ctx context.Context, req any, info *grpc.UnaryServerInfo, handler grpc.UnaryHandler) (any, error) {
	id := ""
	if values := metadata.ValueFromIncomingContext(ctx, "x-request-id"); len(values) > 0 && requestid.Valid(values[0]) {
		id = values[0]
	}
	if id == "" {
		id = requestid.Generate()
	}
	header := metadata.Pairs("x-request-id", id)
	_ = grpc.SetHeader(ctx, header)
	return handler(requestid.WithContext(ctx, id), req)
}
func idempotencyInterceptor(ctx context.Context, req any, info *grpc.UnaryServerInfo, handler grpc.UnaryHandler) (any, error) {
	values := metadata.ValueFromIncomingContext(ctx, "idempotency-key")
	if len(values) == 0 {
		return handler(ctx, req)
	}
	if !idempotency.Valid(values[0]) {
		return nil, status.Error(codes.InvalidArgument, "invalid idempotency-key")
	}
	return handler(idempotency.WithContext(ctx, values[0]), req)
}
func environmentInterceptor(profile string) grpc.UnaryServerInterceptor {
	return func(ctx context.Context, req any, info *grpc.UnaryServerInfo, handler grpc.UnaryHandler) (any, error) {
		return handler(environment.WithContext(ctx, profile), req)
	}
}
func optionalAuthInterceptor(service *auth.Service, cfg config.Config) grpc.UnaryServerInterceptor {
	return func(ctx context.Context, request any, _ *grpc.UnaryServerInfo, handler grpc.UnaryHandler) (any, error) {
		authenticated, err := authenticateGRPCOptional(ctx, service, cfg)
		if err != nil {
			return nil, err
		}
		return handler(authenticated, request)
	}
}

func authenticateGRPCOptional(ctx context.Context, service *auth.Service, cfg config.Config) (context.Context, error) {
	values := metadata.ValueFromIncomingContext(ctx, "authorization")
	if len(values) == 0 || strings.TrimSpace(values[0]) == "" {
		return ctx, nil
	}
	header := strings.TrimSpace(values[0])
	scheme, raw, ok := strings.Cut(header, " ")
	if !ok || raw == "" {
		return nil, status.Error(codes.Unauthenticated, "invalid authorization credential")
	}
	var identity platformprincipal.Principal
	switch {
	case strings.EqualFold(scheme, "Bearer"):
		verified, err := service.Verify(ctx, raw)
		if err != nil {
			return nil, status.Error(codes.Unauthenticated, "invalid or expired token")
		}
		identity = verified
	case strings.EqualFold(scheme, "PSK"):
		if !cfg.Auth.PSK.Enabled || !auth.VerifyPSK(header, cfg.Auth.PSK.Key) {
			return nil, status.Error(codes.Unauthenticated, "invalid PSK")
		}
		identity = platformprincipal.Principal{ID: cfg.App.Name + ":psk", Type: platformprincipal.TypeServiceAccount}
	default:
		return nil, status.Error(codes.Unauthenticated, "unsupported authorization scheme")
	}
	authenticated := platformprincipal.WithContext(ctx, identity)
	return platformauthz.WithCallerCredential(authenticated, header), nil
}

func authenticateGRPC(ctx context.Context, method string, service *auth.Service, cfg config.Auth) (context.Context, error) {
	values := metadata.ValueFromIncomingContext(ctx, "authorization")
	if cfg.PSK.Enabled && auth.MatchesAny(method, cfg.PSK.GRPCMethods) {
		if len(values) == 0 || !auth.VerifyPSK(values[0], cfg.PSK.Key) {
			return nil, status.Error(codes.Unauthenticated, "missing or invalid PSK")
		}
		return platformprincipal.WithContext(ctx, platformprincipal.Principal{ID: "audit-service:psk", Type: platformprincipal.TypeServiceAccount}), nil
	}
	if auth.MatchesAny(method, cfg.SkipGRPCMethods) {
		return ctx, nil
	}
	if len(values) == 0 {
		return nil, status.Error(codes.Unauthenticated, "missing bearer token")
	}
	scheme, raw, ok := strings.Cut(values[0], " ")
	if !ok || !strings.EqualFold(scheme, "Bearer") {
		return nil, status.Error(codes.Unauthenticated, "invalid bearer token")
	}
	caller, err := service.Verify(ctx, raw)
	if err != nil {
		return nil, status.Error(codes.Unauthenticated, "invalid or expired token")
	}
	return platformprincipal.WithContext(ctx, caller), nil
}

type contextServerStream struct {
	grpc.ServerStream
	ctx context.Context
}

func (s *contextServerStream) Context() context.Context { return s.ctx }

func environmentStreamInterceptor(profile string) grpc.StreamServerInterceptor {
	return func(srv any, stream grpc.ServerStream, info *grpc.StreamServerInfo, handler grpc.StreamHandler) error {
		return handler(srv, &contextServerStream{ServerStream: stream, ctx: environment.WithContext(stream.Context(), profile)})
	}
}

func requestIDStreamInterceptor(srv any, stream grpc.ServerStream, info *grpc.StreamServerInfo, handler grpc.StreamHandler) error {
	ctx := stream.Context()
	id := ""
	if values := metadata.ValueFromIncomingContext(ctx, "x-request-id"); len(values) > 0 && requestid.Valid(values[0]) {
		id = values[0]
	}
	if id == "" {
		id = requestid.Generate()
	}
	if err := stream.SetHeader(metadata.Pairs("x-request-id", id)); err != nil {
		return status.Error(codes.Internal, "set request metadata")
	}
	return handler(srv, &contextServerStream{ServerStream: stream, ctx: requestid.WithContext(ctx, id)})
}

func idempotencyStreamInterceptor(srv any, stream grpc.ServerStream, info *grpc.StreamServerInfo, handler grpc.StreamHandler) error {
	values := metadata.ValueFromIncomingContext(stream.Context(), "idempotency-key")
	if len(values) == 0 {
		return handler(srv, stream)
	}
	if !idempotency.Valid(values[0]) {
		return status.Error(codes.InvalidArgument, "invalid idempotency-key")
	}
	return handler(srv, &contextServerStream{ServerStream: stream, ctx: idempotency.WithContext(stream.Context(), values[0])})
}

func optionalAuthStreamInterceptor(service *auth.Service, cfg config.Config) grpc.StreamServerInterceptor {
	return func(server any, stream grpc.ServerStream, _ *grpc.StreamServerInfo, handler grpc.StreamHandler) error {
		ctx, err := authenticateGRPCOptional(stream.Context(), service, cfg)
		if err != nil {
			return err
		}
		return handler(server, &contextServerStream{ServerStream: stream, ctx: ctx})
	}
}

func recoveryStreamInterceptor(logger *slog.Logger) grpc.StreamServerInterceptor {
	return func(srv any, stream grpc.ServerStream, info *grpc.StreamServerInfo, handler grpc.StreamHandler) (err error) {
		defer func() {
			if recovered := recover(); recovered != nil {
				logger.ErrorContext(stream.Context(), "grpc stream panic recovered", "method", info.FullMethod, "panic", recovered)
				err = status.Error(codes.Internal, "internal server error")
			}
		}()
		return handler(srv, stream)
	}
}

func metricsStreamInterceptor(metrics *observability.Metrics, logger *slog.Logger) grpc.StreamServerInterceptor {
	return func(srv any, stream grpc.ServerStream, info *grpc.StreamServerInfo, handler grpc.StreamHandler) error {
		started := time.Now()
		err := handler(srv, stream)
		code := status.Code(err)
		if metrics.Enabled() {
			metrics.GRPCRequests.WithLabelValues(info.FullMethod, code.String()).Inc()
			metrics.GRPCDuration.WithLabelValues(info.FullMethod).Observe(time.Since(started).Seconds())
		}
		requestID, _ := requestid.FromContext(stream.Context())
		logger.InfoContext(stream.Context(), "grpc stream", "request_id", requestID, "method", info.FullMethod, "code", code.String(), "duration", time.Since(started))
		return err
	}
}

func recoveryInterceptor(logger *slog.Logger) grpc.UnaryServerInterceptor {
	return func(ctx context.Context, req any, info *grpc.UnaryServerInfo, handler grpc.UnaryHandler) (response any, err error) {
		defer func() {
			if recovered := recover(); recovered != nil {
				logger.ErrorContext(ctx, "grpc panic recovered", "method", info.FullMethod, "panic", recovered)
				err = status.Error(codes.Internal, "internal server error")
			}
		}()
		return handler(ctx, req)
	}
}
func metricsInterceptor(metrics *observability.Metrics, logger *slog.Logger) grpc.UnaryServerInterceptor {
	return func(ctx context.Context, req any, info *grpc.UnaryServerInfo, handler grpc.UnaryHandler) (any, error) {
		started := time.Now()
		response, err := handler(ctx, req)
		code := status.Code(err)
		if metrics.Enabled() {
			metrics.GRPCRequests.WithLabelValues(info.FullMethod, code.String()).Inc()
			metrics.GRPCDuration.WithLabelValues(info.FullMethod).Observe(time.Since(started).Seconds())
		}
		span := trace.SpanFromContext(ctx).SpanContext()
		requestID, _ := requestid.FromContext(ctx)
		logger.InfoContext(ctx, "grpc request", "request_id", requestID, "trace_id", span.TraceID().String(), "span_id", span.SpanID().String(), "method", info.FullMethod, "code", code.String(), "duration", time.Since(started))
		return response, err
	}
}

func serverCredentials(cfg config.GRPCTLS) (credentials.TransportCredentials, error) {
	certificate, err := tls.LoadX509KeyPair(cfg.CertFile, cfg.KeyFile)
	if err != nil {
		return nil, fmt.Errorf("load grpc certificate: %w", err)
	}
	tlsConfig := &tls.Config{MinVersion: tls.VersionTLS12, Certificates: []tls.Certificate{certificate}}
	if cfg.ClientCAFile != "" {
		pem, err := os.ReadFile(cfg.ClientCAFile)
		if err != nil {
			return nil, fmt.Errorf("read grpc client CA: %w", err)
		}
		pool := x509.NewCertPool()
		if !pool.AppendCertsFromPEM(pem) {
			return nil, fmt.Errorf("parse grpc client CA")
		}
		tlsConfig.ClientCAs = pool
		tlsConfig.ClientAuth = tls.RequireAndVerifyClientCert
	}
	return credentials.NewTLS(tlsConfig), nil
}

var Module = fx.Module("grpc", fx.Provide(NewServer), fx.Invoke(func(*Server) {}))
