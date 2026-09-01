//go:build integration

package integration

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"path/filepath"
	"testing"
	"time"

	"github.com/lihongjie0209/audit-service/internal/app"
	"github.com/lihongjie0209/audit-service/internal/auth"
	"github.com/lihongjie0209/audit-service/internal/config"
	auditv1 "github.com/lihongjie0209/platform-protos/gen/go/platform/audit/v1"
	goredis "github.com/redis/go-redis/v9"
	"github.com/testcontainers/testcontainers-go"
	"github.com/testcontainers/testcontainers-go/modules/postgres"
	rediscontainer "github.com/testcontainers/testcontainers-go/modules/redis"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/credentials/insecure"
	grpc_health_v1 "google.golang.org/grpc/health/grpc_health_v1"
	"google.golang.org/grpc/metadata"
	"google.golang.org/grpc/status"
)

func TestHTTPAndGRPCEndToEnd(t *testing.T) {
	ctx, cancel := context.WithTimeout(t.Context(), 3*time.Minute)
	defer cancel()
	postgresContainer, err := postgres.Run(ctx, "postgres:17-alpine", postgres.WithDatabase("app"), postgres.WithUsername("app"), postgres.WithPassword("app"), postgres.BasicWaitStrategies(), postgres.WithSQLDriver("pgx"))
	if err != nil {
		t.Fatal(err)
	}
	testcontainers.CleanupContainer(t, postgresContainer)
	dsn, err := postgresContainer.ConnectionString(ctx, "sslmode=disable")
	if err != nil {
		t.Fatal(err)
	}
	migrationPath, _ := filepath.Abs(filepath.Join("..", "migrations", "postgres"))

	redisContainer, err := rediscontainer.Run(ctx, "redis:7.4-alpine")
	if err != nil {
		t.Fatal(err)
	}
	testcontainers.CleanupContainer(t, redisContainer)
	redisURL, err := redisContainer.ConnectionString(ctx)
	if err != nil {
		t.Fatal(err)
	}
	redisOptions, err := goredis.ParseURL(redisURL)
	if err != nil {
		t.Fatal(err)
	}

	httpAddress := freeAddress(t)
	grpcAddress := freeAddress(t)
	const secret = "01234567890123456789012345678901"
	cfg := config.Config{
		Runtime:       config.Runtime{ActiveProfile: "integration"},
		App:           config.App{Name: "integration", Env: "integration", ShutdownTimeout: 10 * time.Second},
		HTTP:          config.HTTP{Address: httpAddress, ReadTimeout: 5 * time.Second, WriteTimeout: 5 * time.Second, IdleTimeout: 30 * time.Second, RequestTimeout: 5 * time.Second, MaxBodyBytes: 1 << 20},
		GRPC:          config.GRPC{Enabled: true, Address: grpcAddress, MaxReceiveBytes: 4 << 20},
		Log:           config.Log{Level: "error", Format: "json", File: filepath.Join(t.TempDir(), "app.log"), MaxSizeMB: 1, MaxBackups: 1, MaxAgeDays: 1},
		Database:      config.Database{Enabled: true, Type: "postgres", DSN: dsn, MaxOpenConns: 5, MaxIdleConns: 2, ConnMaxLifetime: time.Minute, ConnMaxIdleTime: time.Minute, PingTimeout: 10 * time.Second},
		Migration:     config.Migration{AutoUp: true, Path: migrationPath, DatabaseURL: dsn, Table: "integration_e2e_schema_migrations"},
		Redis:         config.Redis{Enabled: true, Address: redisOptions.Addr, DB: redisOptions.DB, DialTimeout: 5 * time.Second, ReadTimeout: 3 * time.Second, WriteTimeout: 3 * time.Second},
		Health:        config.Health{DatabaseTimeout: 2 * time.Second, RedisTimeout: 2 * time.Second},
		Observability: config.Observability{MetricsEnabled: true},
		JWT:           config.JWT{Issuer: "integration", Secret: secret, TTL: time.Hour},
		Auth:          config.Auth{ClientID: "client", ClientSecret: "secret", SkipHTTPPaths: []string{"/api/v1/version"}, SkipGRPCMethods: []string{"/grpc.health.v1.Health/*"}, PSK: config.PSK{Enabled: true, Key: secret, HTTPPaths: []string{"/api/v1/audit/records/*"}, GRPCMethods: []string{"/platform.audit.v1.AuditService/*"}}},
		Cron:          config.Cron{Enabled: false, Timezone: "UTC"},
		User:          config.User{CacheTTL: time.Minute, LockTTL: 10 * time.Second, LockRetryDelay: 20 * time.Millisecond},
		Idempotency:   config.Idempotency{Enabled: true, ProcessingTTL: 30 * time.Second, ResultTTL: time.Hour, FailureTTL: time.Minute},
	}
	application := app.New(cfg)
	if err := application.Start(ctx); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		stopCtx, stop := context.WithTimeout(context.Background(), 10*time.Second)
		defer stop()
		_ = application.Stop(stopCtx)
	})
	token, err := auth.New(cfg).Issue("client")
	if err != nil {
		t.Fatal(err)
	}

	baseURL := "http://" + httpAddress
	if status := postJSON(t, baseURL+"/api/v1/version", "", "", `{}`); status != http.StatusOK {
		t.Fatalf("public version status = %d", status)
	}
	if status := postJSON(t, baseURL+"/api/v1/me", "Bearer "+token, "", `{}`); status != http.StatusOK {
		t.Fatalf("JWT status = %d", status)
	}
	createdBody, statusCode := postJSONBody(
		t,
		baseURL+"/api/v1/audit/records/create",
		"PSK "+secret,
		"audit-create-1",
		`{"id":"73b76e80-31f8-4b85-938d-57760ba54c91","tenant_id":"tenant-1","actor_id":"user-1","actor_type":"user","action":"invoice.updated","resource_type":"invoice","resource_id":"invoice-1","request_id":"request-1","trace_id":"trace-1","source_service":"billing-service","before_summary":{"status":"draft"},"after_summary":{"status":"published"}}`,
	)
	if statusCode != http.StatusOK || !bytes.Contains(createdBody, []byte(`"before_summary":{"status":"draft"}`)) {
		t.Fatalf("create audit status=%d body=%s", statusCode, createdBody)
	}
	queryBody, statusCode := postJSONBody(
		t,
		baseURL+"/api/v1/audit/records/query",
		"PSK "+secret,
		"",
		`{"tenant_id":"tenant-1","actor_type":"user","trace_id":"trace-1","source_service":"billing-service","page":1,"page_size":20}`,
	)
	if statusCode != http.StatusOK || !bytes.Contains(queryBody, []byte(`"id":"73b76e80-31f8-4b85-938d-57760ba54c91"`)) {
		t.Fatalf("query audit status=%d body=%s", statusCode, queryBody)
	}
	exportBody, statusCode := postJSONBody(
		t,
		baseURL+"/api/v1/audit/records/export",
		"PSK "+secret,
		"",
		`{"tenant_id":"tenant-1","trace_id":"trace-1","source_service":"billing-service","max_records":100}`,
	)
	if statusCode != http.StatusOK || auditExportContent(t, exportBody) == "" {
		t.Fatalf("export audit status=%d body=%s", statusCode, exportBody)
	}

	connection, err := grpc.NewClient(grpcAddress, grpc.WithTransportCredentials(insecure.NewCredentials()))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = connection.Close() })
	healthResponse, err := grpc_health_v1.NewHealthClient(connection).Check(ctx, &grpc_health_v1.HealthCheckRequest{})
	if err != nil || healthResponse.GetStatus() != grpc_health_v1.HealthCheckResponse_SERVING {
		t.Fatalf("health = %v, %v", healthResponse, err)
	}
	pskCtx := metadata.AppendToOutgoingContext(ctx, "authorization", "PSK "+secret)
	if _, err := auditv1.NewAuditServiceClient(connection).Query(pskCtx, &auditv1.QueryRequest{}); status.Code(err) == codes.Unauthenticated {
		t.Fatalf("PSK Query was not authenticated: %v", err)
	}
}

func auditExportContent(t *testing.T, data []byte) string {
	t.Helper()
	var response struct {
		Body struct {
			Content string `json:"content"`
		} `json:"body"`
	}
	if err := json.Unmarshal(data, &response); err != nil {
		t.Fatal(err)
	}
	if !bytes.Contains([]byte(response.Body.Content), []byte("id,tenant_id,application_id,actor_id")) {
		t.Fatalf("invalid audit CSV: %q", response.Body.Content)
	}
	return response.Body.Content
}

func freeAddress(t *testing.T) string {
	t.Helper()
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	address := listener.Addr().String()
	if err := listener.Close(); err != nil {
		t.Fatal(err)
	}
	return address
}

func postJSON(t *testing.T, target, authorization, key, body string) int {
	t.Helper()
	_, status := postJSONBody(t, target, authorization, key, body)
	return status
}
func postJSONBody(t *testing.T, target, authorization, key, body string) ([]byte, int) {
	t.Helper()
	request, err := http.NewRequestWithContext(t.Context(), http.MethodPost, target, bytes.NewBufferString(body))
	if err != nil {
		t.Fatal(err)
	}
	request.Header.Set("Content-Type", "application/json")
	if authorization != "" {
		request.Header.Set("Authorization", authorization)
	}
	if key != "" {
		request.Header.Set("Idempotency-Key", key)
	}
	response, err := http.DefaultClient.Do(request)
	if err != nil {
		t.Fatal(err)
	}
	defer response.Body.Close()
	data, err := io.ReadAll(response.Body)
	if err != nil {
		t.Fatal(err)
	}
	var validJSON any
	if err := json.Unmarshal(data, &validJSON); err != nil {
		t.Fatalf("invalid JSON response: %v (%s)", err, data)
	}
	return data, response.StatusCode
}

var _ = fmt.Sprintf
