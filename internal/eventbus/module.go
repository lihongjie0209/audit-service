package eventbus

import (
	"context"

	"github.com/lihongjie0209/audit-service/internal/config"
	"go.uber.org/fx"
)

func newBus(lifecycle fx.Lifecycle, cfg config.Config) (*Bus, error) {
	bus, err := New(context.Background(), cfg)
	if err != nil {
		return nil, err
	}
	lifecycle.Append(fx.StopHook(bus.Close))
	return bus, nil
}

var Module = fx.Module("event-bus", fx.Provide(newBus), fx.Invoke(func(*Bus) {}))
