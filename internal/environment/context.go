package environment

import "context"

type contextKey struct{}

func WithContext(ctx context.Context, profile string) context.Context {
	return context.WithValue(ctx, contextKey{}, profile)
}

func FromContext(ctx context.Context) (string, bool) {
	profile, ok := ctx.Value(contextKey{}).(string)
	return profile, ok
}
