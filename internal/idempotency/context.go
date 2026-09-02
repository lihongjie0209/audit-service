package idempotency

import (
	"context"

	platformidempotency "github.com/lihongjie0209/microservice-platform-go/idempotency"
)

func Valid(key string) bool { return platformidempotency.Valid(key) }
func WithContext(ctx context.Context, key string) context.Context {
	return platformidempotency.WithContext(ctx, key)
}
func FromContext(ctx context.Context) (string, bool) { return platformidempotency.FromContext(ctx) }
