package outbound

import (
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"

	"github.com/lihongjie0209/audit-service/internal/config"
	"github.com/lihongjie0209/audit-service/internal/idempotency"
)

func TestHTTPClient_RetryPolicy(t *testing.T) {
	t.Parallel()
	for _, test := range []struct {
		name         string
		withKey      bool
		wantAttempts int32
	}{
		{name: "POST is not retried by default", wantAttempts: 1},
		{name: "idempotent POST is retried", withKey: true, wantAttempts: 3},
	} {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			var attempts atomic.Int32
			server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, _ *http.Request) {
				attempts.Add(1)
				writer.WriteHeader(http.StatusServiceUnavailable)
			}))
			defer server.Close()
			client, err := NewHTTPClient("test", config.HTTPUpstream{BaseURL: server.URL, Timeout: time.Second, Retry: config.Retry{MaxAttempts: 3, InitialBackoff: time.Millisecond, MaxBackoff: 2 * time.Millisecond}}, nil)
			if err != nil {
				t.Fatal(err)
			}
			ctx := t.Context()
			if test.withKey {
				ctx = idempotency.WithContext(ctx, "request-0001")
			}
			response, err := client.Do(ctx, http.MethodPost, "/test", []byte(`{}`), nil)
			if err != nil {
				t.Fatal(err)
			}
			if response != nil {
				_ = response.Body.Close()
			}
			if got := attempts.Load(); got != test.wantAttempts {
				t.Fatalf("attempts = %d, want %d", got, test.wantAttempts)
			}
		})
	}
}

func TestHTTPClient_RejectsPlaintextCredentials(t *testing.T) {
	t.Parallel()
	_, err := NewHTTPClient("test", config.HTTPUpstream{BaseURL: "http://example.com", Timeout: time.Second, Auth: config.ClientAuth{Type: "psk", Token: "secret"}}, nil)
	if err == nil {
		t.Fatal("NewHTTPClient() error = nil")
	}
}
