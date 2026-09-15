package httptransport

import (
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/lihongjie0209/audit-service/internal/auth"
	"github.com/lihongjie0209/audit-service/internal/config"
	"github.com/lihongjie0209/audit-service/internal/idempotency"
	platformauthz "github.com/lihongjie0209/microservice-platform-go/authz"
	"github.com/lihongjie0209/microservice-platform-go/operationlog"
	platformprincipal "github.com/lihongjie0209/microservice-platform-go/principal"
)

type operationRecorderStub struct {
	entry       operationlog.Entry
	contextErr  error
	recordCalls int
}

func (*operationRecorderStub) Enabled() bool { return true }
func (r *operationRecorderStub) Record(ctx context.Context, entry operationlog.Entry) error {
	r.entry = entry
	r.contextErr = ctx.Err()
	r.recordCalls++
	return nil
}

func TestAuditAccessLogRecordsReadAfterRequestCancellation(t *testing.T) {
	t.Parallel()
	gin.SetMode(gin.TestMode)
	recorder := &operationRecorderStub{}
	ctx, cancel := context.WithCancel(context.Background())
	router := gin.New()
	router.Use(RequestID(), func(c *gin.Context) {
		requestCtx := platformprincipal.WithContext(c.Request.Context(), platformprincipal.Principal{ID: "user-1", Type: platformprincipal.TypeUser, TenantID: "tenant-1"})
		c.Request = c.Request.WithContext(requestCtx)
		c.Next()
	}, AuditAccessLog(recorder, slog.New(slog.NewTextHandler(io.Discard, nil))))
	router.POST("/api/v1/audit/records/query", func(c *gin.Context) {
		cancel()
		OK(c, nil)
	})
	request := httptest.NewRequest(http.MethodPost, "/api/v1/audit/records/query", nil).WithContext(ctx)
	request.Header.Set("X-Request-ID", "request-1")
	request.Header.Set("User-Agent", "audit-console")
	response := httptest.NewRecorder()
	router.ServeHTTP(response, request)
	if recorder.recordCalls != 1 || recorder.contextErr != nil {
		t.Fatalf("record calls=%d context error=%v", recorder.recordCalls, recorder.contextErr)
	}
	if recorder.entry.Operation != "audit.record.query" || recorder.entry.Protocol != "http" || recorder.entry.RequestID != "request-1" || recorder.entry.UserAgent != "audit-console" || !recorder.entry.Succeeded {
		t.Fatalf("entry = %+v", recorder.entry)
	}
}

func TestAuditAccessLogDoesNotDuplicateRecordIngestion(t *testing.T) {
	t.Parallel()
	recorder := &operationRecorderStub{}
	router := gin.New()
	router.Use(AuditAccessLog(recorder, slog.New(slog.NewTextHandler(io.Discard, nil))))
	router.POST("/api/v1/audit/records/create", func(c *gin.Context) { OK(c, nil) })
	router.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest(http.MethodPost, "/api/v1/audit/records/create", nil))
	if recorder.recordCalls != 0 {
		t.Fatalf("record calls = %d, want 0", recorder.recordCalls)
	}
}

type fakeIdempotencyManager struct {
	decision  idempotency.Decision
	beginKey  string
	completed *Response
}

func (*fakeIdempotencyManager) Enabled() bool { return true }
func (m *fakeIdempotencyManager) Begin(_ context.Context, key, _ string) (idempotency.Decision, error) {
	m.beginKey = key
	return m.decision, nil
}
func (m *fakeIdempotencyManager) Complete(_ context.Context, _, _ string, value any) error {
	response, ok := value.(Response)
	if ok {
		m.completed = &response
	}
	return nil
}
func (*fakeIdempotencyManager) Fail(context.Context, string, string, idempotency.Failure) error {
	return nil
}
func TestIdempotencyExecutionCompletesAndReplaysAuditRecord(t *testing.T) {
	t.Parallel()
	manager := &fakeIdempotencyManager{decision: idempotency.Decision{State: idempotency.StateAcquired, Owner: "owner"}}
	calls := 0
	router := gin.New()
	router.Use(RequestID(), func(c *gin.Context) {
		c.Set("subject", "service-1")
		c.Request = c.Request.WithContext(idempotency.WithContext(c.Request.Context(), "operation-1"))
		c.Next()
	}, IdempotencyExecution(manager, []string{"/api/v1/audit/records/create"}, slog.New(slog.NewTextHandler(io.Discard, nil))))
	router.POST("/api/v1/audit/records/create", func(c *gin.Context) { calls++; OK(c, gin.H{"id": "audit-1"}) })
	out := httptest.NewRecorder()
	router.ServeHTTP(out, httptest.NewRequest(http.MethodPost, "/api/v1/audit/records/create", strings.NewReader(`{"id":"audit-1"}`)))
	if calls != 1 || manager.completed == nil || manager.completed.RequestID != "" {
		t.Fatalf("calls=%d completed=%+v", calls, manager.completed)
	}
	stored, _ := json.Marshal(*manager.completed)
	manager.decision = idempotency.Decision{State: idempotency.StateCompleted, Response: stored}
	request := httptest.NewRequest(http.MethodPost, "/api/v1/audit/records/create", strings.NewReader(`{"id":"audit-1"}`))
	request.Header.Set("X-Request-ID", "current-request")
	replay := httptest.NewRecorder()
	router.ServeHTTP(replay, request)
	var response Response
	if err := json.Unmarshal(replay.Body.Bytes(), &response); err != nil {
		t.Fatal(err)
	}
	if calls != 1 || response.RequestID != "current-request" {
		t.Fatalf("calls=%d response=%+v", calls, response)
	}
}
func TestIdempotencyExecutionBypassesAuditReadsAndExport(t *testing.T) {
	t.Parallel()
	for _, route := range []string{"/api/v1/audit/records/get", "/api/v1/audit/records/query", "/api/v1/audit/records/export"} {
		t.Run(route, func(t *testing.T) {
			manager := &fakeIdempotencyManager{decision: idempotency.Decision{State: idempotency.StateConflict}}
			calls := 0
			router := gin.New()
			router.Use(func(c *gin.Context) {
				c.Request = c.Request.WithContext(idempotency.WithContext(c.Request.Context(), "operation-1"))
				c.Next()
			}, IdempotencyExecution(manager, []string{"/api/v1/audit/records/create"}, slog.New(slog.NewTextHandler(io.Discard, nil))))
			router.POST(route, func(c *gin.Context) { calls++; OK(c, nil) })
			out := httptest.NewRecorder()
			router.ServeHTTP(out, httptest.NewRequest(http.MethodPost, route, nil))
			if calls != 1 || manager.beginKey != "" {
				t.Fatalf("calls=%d key=%q", calls, manager.beginKey)
			}
		})
	}
}

type authorizationStub struct{ err error }

func (a authorizationStub) Authorize(context.Context, platformprincipal.Principal, platformauthz.Requirement) error {
	return a.err
}

func TestAuditHTTPRequirementCoversEveryBusinessRoute(t *testing.T) {
	t.Parallel()
	for _, route := range []string{"/api/v1/audit/records/create", "/api/v1/audit/records/get", "/api/v1/audit/records/query", "/api/v1/audit/records/export"} {
		if requirement, ok := auditHTTPRequirement(route); !ok || requirement.Resource == "" || requirement.Action == "" || requirement.Scope != platformauthz.ScopePrincipal {
			t.Fatalf("route %q requirement = %+v, %v", route, requirement, ok)
		}
	}
	if _, ok := auditHTTPRequirement("/api/v1/version"); ok {
		t.Fatal("version must not require a domain permission")
	}
}

func TestAuthorizationFailsClosedAndClassifiesOutage(t *testing.T) {
	t.Parallel()
	gin.SetMode(gin.TestMode)
	for _, test := range []struct {
		name   string
		err    error
		status int
	}{{name: "denied", err: platformauthz.ErrDenied, status: http.StatusForbidden}, {name: "unavailable", err: platformauthz.ErrDecisionUnavailable, status: http.StatusServiceUnavailable}} {
		t.Run(test.name, func(t *testing.T) {
			router := gin.New()
			router.Use(RequestID(), func(c *gin.Context) {
				c.Request = c.Request.WithContext(platformprincipal.WithContext(c.Request.Context(), platformprincipal.Principal{ID: "user-1", Type: platformprincipal.TypeUser, TenantID: "tenant-1", MembershipID: "membership-1"}))
				c.Next()
			}, Authorization(true, authorizationStub{err: test.err}, slog.New(slog.NewTextHandler(io.Discard, nil))))
			router.POST("/api/v1/audit/records/query", func(c *gin.Context) { OK(c, nil) })
			recorder := httptest.NewRecorder()
			router.ServeHTTP(recorder, httptest.NewRequest(http.MethodPost, "/api/v1/audit/records/query", nil))
			if recorder.Code != test.status {
				t.Fatalf("status = %d, want %d", recorder.Code, test.status)
			}
		})
	}
}

func TestRequestID(t *testing.T) {
	t.Parallel()
	gin.SetMode(gin.TestMode)
	router := gin.New()
	router.Use(RequestID())
	router.POST("/test", func(c *gin.Context) { OK(c, nil) })
	request := httptest.NewRequest(http.MethodPost, "/test", nil)
	request.Header.Set("X-Request-ID", "client-request-1")
	recorder := httptest.NewRecorder()
	router.ServeHTTP(recorder, request)
	if got := recorder.Header().Get("X-Request-ID"); got != "client-request-1" {
		t.Fatalf("X-Request-ID = %q", got)
	}
	var response Response
	if err := json.Unmarshal(recorder.Body.Bytes(), &response); err != nil {
		t.Fatal(err)
	}
	if response.RequestID != "client-request-1" {
		t.Fatalf("request_id = %q", response.RequestID)
	}
}

func TestAuthentication_PSKPrecedesSkipAndJWT(t *testing.T) {
	t.Parallel()
	gin.SetMode(gin.TestMode)
	const key = "01234567890123456789012345678901"
	service := auth.New(config.Config{JWT: config.JWT{Issuer: "test", Secret: key, TTL: time.Hour}})
	for _, test := range []struct {
		name   string
		header string
		status int
	}{
		{name: "valid PSK", header: "PSK " + key, status: http.StatusOK},
		{name: "PSK route does not become public", status: http.StatusUnauthorized},
		{name: "bearer cannot access PSK route", header: "Bearer invalid", status: http.StatusUnauthorized},
	} {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			router := gin.New()
			router.Use(RequestID(), Authentication(service, slog.New(slog.NewTextHandler(io.Discard, nil)), config.Auth{
				SkipHTTPPaths: []string{"/api/v1/external/*"},
				PSK:           config.PSK{Enabled: true, Key: key, HTTPPaths: []string{"/api/v1/external/*"}},
			}))
			router.POST("/api/v1/external/callback", func(c *gin.Context) {
				value, ok := platformprincipal.FromContext(c.Request.Context())
				if test.status == http.StatusOK && (!ok || value.ID != "audit-service:psk" || value.Type != platformprincipal.TypeServiceAccount) {
					c.AbortWithStatus(http.StatusInternalServerError)
					return
				}
				OK(c, nil)
			})
			request := httptest.NewRequest(http.MethodPost, "/api/v1/external/callback", nil)
			request.Header.Set("Authorization", test.header)
			recorder := httptest.NewRecorder()
			router.ServeHTTP(recorder, request)
			if recorder.Code != test.status {
				t.Fatalf("status = %d, want %d", recorder.Code, test.status)
			}
		})
	}
}

func TestRequireJSON(t *testing.T) {
	t.Parallel()
	gin.SetMode(gin.TestMode)
	router := gin.New()
	router.Use(RequestID(), RequireJSON())
	router.POST("/test", func(c *gin.Context) { OK(c, nil) })
	request := httptest.NewRequest(http.MethodPost, "/test", io.NopCloser(&oneByteReader{}))
	request.ContentLength = 1
	request.Header.Set("Content-Type", "text/plain")
	recorder := httptest.NewRecorder()
	router.ServeHTTP(recorder, request)
	if recorder.Code != http.StatusUnsupportedMediaType {
		t.Fatalf("status = %d", recorder.Code)
	}
}

func TestTimeoutPropagatesCancellation(t *testing.T) {
	t.Parallel()
	gin.SetMode(gin.TestMode)
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	router := gin.New()
	router.Use(RequestID(), Timeout(time.Millisecond, logger))
	router.POST("/test", func(c *gin.Context) { <-c.Request.Context().Done() })
	recorder := httptest.NewRecorder()
	router.ServeHTTP(recorder, httptest.NewRequest(http.MethodPost, "/test", nil))
	if recorder.Code != http.StatusGatewayTimeout {
		t.Fatalf("status = %d, want %d", recorder.Code, http.StatusGatewayTimeout)
	}
}

type oneByteReader struct{}

func (*oneByteReader) Read(buffer []byte) (int, error) { buffer[0] = 'x'; return 1, io.EOF }
