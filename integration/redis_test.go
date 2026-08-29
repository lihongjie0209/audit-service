//go:build integration

package integration

import (
	"context"
	"testing"
	"time"

	"github.com/lihongjie0209/audit-service/internal/cache"
	"github.com/lihongjie0209/audit-service/internal/config"
	"github.com/lihongjie0209/audit-service/internal/idempotency"
	goredis "github.com/redis/go-redis/v9"
	"github.com/testcontainers/testcontainers-go"
	rediscontainer "github.com/testcontainers/testcontainers-go/modules/redis"
)

func TestRedisLockAndIdempotency(t *testing.T) {
	ctx, cancel := context.WithTimeout(t.Context(), time.Minute)
	defer cancel()
	container, err := rediscontainer.Run(ctx, "redis:7.4-alpine")
	if err != nil {
		t.Fatal(err)
	}
	testcontainers.CleanupContainer(t, container)
	connectionString, err := container.ConnectionString(ctx)
	if err != nil {
		t.Fatal(err)
	}
	options, err := goredis.ParseURL(connectionString)
	if err != nil {
		t.Fatal(err)
	}
	client := goredis.NewClient(options)
	t.Cleanup(func() { _ = client.Close() })

	locker := cache.NewLocker(client)
	lock, acquired, err := locker.TryLock(ctx, "integration", 10*time.Second)
	if err != nil || !acquired {
		t.Fatalf("first lock acquired=%v err=%v", acquired, err)
	}
	_, secondAcquired, err := locker.TryLock(ctx, "integration", 10*time.Second)
	if err != nil || secondAcquired {
		t.Fatalf("competing lock acquired=%v err=%v", secondAcquired, err)
	}
	if err := lock.Unlock(ctx); err != nil {
		t.Fatal(err)
	}

	manager := idempotency.New(client, config.Config{Idempotency: config.Idempotency{Enabled: true, ProcessingTTL: time.Minute, ResultTTL: time.Hour, FailureTTL: time.Minute}})
	first, err := manager.Begin(ctx, "request-0001", "fingerprint-a")
	if err != nil || first.State != idempotency.StateAcquired {
		t.Fatalf("begin = %+v, %v", first, err)
	}
	processing, err := manager.Begin(ctx, "request-0001", "fingerprint-a")
	if err != nil || processing.State != idempotency.StateProcessing {
		t.Fatalf("processing = %+v, %v", processing, err)
	}
	conflict, err := manager.Begin(ctx, "request-0001", "fingerprint-b")
	if err != nil || conflict.State != idempotency.StateConflict {
		t.Fatalf("conflict = %+v, %v", conflict, err)
	}
	response := map[string]string{"id": "user-1"}
	if err := manager.Complete(ctx, "request-0001", first.Owner, response); err != nil {
		t.Fatal(err)
	}
	replay, err := manager.Begin(ctx, "request-0001", "fingerprint-a")
	if err != nil || replay.State != idempotency.StateCompleted {
		t.Fatalf("replay = %+v, %v", replay, err)
	}
}
