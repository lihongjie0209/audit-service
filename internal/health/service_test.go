package health

import (
	"testing"
	"time"

	"github.com/alicebob/miniredis/v2"
	"github.com/lihongjie0209/audit-service/internal/config"
	"github.com/redis/go-redis/v9"
)

func TestService_Ready(t *testing.T) {
	t.Parallel()
	server, err := miniredis.Run()
	if err != nil {
		t.Fatal(err)
	}
	defer server.Close()
	client := redis.NewClient(&redis.Options{Addr: server.Addr()})
	defer func() { _ = client.Close() }()
	service := New(nil, client, config.Config{Health: config.Health{DatabaseTimeout: time.Second, RedisTimeout: time.Second}})
	status, ready := service.Ready(t.Context())
	if !ready {
		t.Fatalf("Ready() ready = false, status = %#v", status)
	}
	if status.Dependencies["database"].Status != "disabled" || status.Dependencies["redis"].Status != "up" {
		t.Fatalf("Ready() dependencies = %#v", status.Dependencies)
	}
}

func TestService_ReadyWhenRedisIsDown(t *testing.T) {
	t.Parallel()
	client := redis.NewClient(&redis.Options{Addr: "127.0.0.1:1", DialTimeout: time.Millisecond})
	defer func() { _ = client.Close() }()
	service := New(nil, client, config.Config{Health: config.Health{DatabaseTimeout: time.Second, RedisTimeout: 10 * time.Millisecond}})
	status, ready := service.Ready(t.Context())
	if ready {
		t.Fatal("Ready() ready = true, want false")
	}
	if status.Status != "not_ready" || status.Dependencies["redis"].Status != "down" {
		t.Fatalf("Ready() status = %#v", status)
	}
}
