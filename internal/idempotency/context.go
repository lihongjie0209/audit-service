package idempotency

import (
	"context"
	"regexp"
)

type contextKey struct{}

var validKey = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._:-]{7,127}$`)

func Valid(key string) bool { return validKey.MatchString(key) }

func WithContext(ctx context.Context, key string) context.Context {
	return context.WithValue(ctx, contextKey{}, key)
}

func FromContext(ctx context.Context) (string, bool) {
	key, ok := ctx.Value(contextKey{}).(string)
	return key, ok && key != ""
}
