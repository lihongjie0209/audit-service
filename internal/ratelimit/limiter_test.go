package ratelimit

import (
	"testing"
	"time"

	"github.com/alicebob/miniredis/v2"
	"github.com/lihongjie0209/audit-service/internal/config"
	"github.com/redis/go-redis/v9"
)

func TestLimiter_Allow(t *testing.T) {
	t.Parallel()
	server, err := miniredis.Run()
	if err != nil {
		t.Fatal(err)
	}
	defer server.Close()
	client := redis.NewClient(&redis.Options{Addr: server.Addr()})
	defer func() { _ = client.Close() }()
	limiter := New(client, config.Config{RateLimit: config.RateLimit{Enabled: true}})
	rule := config.RateLimitRule{Rate: 1, Burst: 1, Period: time.Minute}
	first, err := limiter.Allow(t.Context(), "test", rule)
	if err != nil {
		t.Fatalf("first Allow() error = %v", err)
	}
	if !first.Allowed {
		t.Fatal("first Allow() allowed = false")
	}
	second, err := limiter.Allow(t.Context(), "test", rule)
	if err != nil {
		t.Fatalf("second Allow() error = %v", err)
	}
	if second.Allowed {
		t.Fatal("second Allow() allowed = true")
	}
	if second.RetryAfter <= 0 {
		t.Fatalf("RetryAfter = %v", second.RetryAfter)
	}
}

func TestLimiter_Disabled(t *testing.T) {
	t.Parallel()
	limiter := New(nil, config.Config{})
	result, err := limiter.Allow(t.Context(), "test", config.RateLimitRule{})
	if err != nil {
		t.Fatal(err)
	}
	if !result.Allowed {
		t.Fatal("disabled limiter denied request")
	}
}
