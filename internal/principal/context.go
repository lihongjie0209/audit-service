package principal

import "context"

type AuthenticationMethod string

const (
	AuthenticationJWT AuthenticationMethod = "jwt"
	AuthenticationPSK AuthenticationMethod = "psk"
)

type Principal struct {
	Subject string
	Method  AuthenticationMethod
}

type contextKey struct{}

func WithContext(ctx context.Context, value Principal) context.Context {
	return context.WithValue(ctx, contextKey{}, value)
}

func FromContext(ctx context.Context) (Principal, bool) {
	value, ok := ctx.Value(contextKey{}).(Principal)
	return value, ok && value.Subject != ""
}
