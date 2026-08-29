package eventbus

import (
	"context"
	"testing"
)

func TestDisabledBus(t *testing.T) {
	t.Parallel()
	if err := (*Bus)(nil).Publish(context.Background(), "service.user.created.v1", Envelope{}); err == nil {
		t.Fatal("Publish() error = nil")
	}
	if err := (*Bus)(nil).Close(); err != nil {
		t.Fatalf("Close() error = %v", err)
	}
}
