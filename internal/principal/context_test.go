package principal

import (
	"context"
	"testing"
)

func TestContextRoundTrip(t *testing.T) {
	t.Parallel()
	want := Principal{Subject: "user-1", Method: AuthenticationJWT}
	got, ok := FromContext(WithContext(context.Background(), want))
	if !ok || got != want {
		t.Fatalf("FromContext() = %#v, %v, want %#v, true", got, ok, want)
	}
}
