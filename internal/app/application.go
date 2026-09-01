package app

import (
	"context"
	"errors"
	"time"

	"github.com/lihongjie0209/audit-service/internal/config"
	"github.com/lihongjie0209/audit-service/internal/outbound"
	"github.com/lihongjie0209/microservice-platform-go/appaccess"
	applicationv1 "github.com/lihongjie0209/platform-protos/gen/go/platform/application/v1"
)

type disabledApplicationVerifier struct{}

func (disabledApplicationVerifier) Verify(context.Context, string, string) error { return nil }

func newApplicationVerifier(cfg config.Config, registry *outbound.Registry) (appaccess.Verifier, error) {
	if !cfg.Database.Enabled {
		return disabledApplicationVerifier{}, nil
	}
	if registry == nil {
		return nil, errors.New("audit service requires outbound registry")
	}
	connection, ok := registry.GRPC("application")
	if !ok {
		return nil, errors.New("audit service requires outbound.grpc.application")
	}
	return appaccess.NewGRPCVerifier(applicationv1.NewApplicationServiceClient(connection), 2*time.Second), nil
}
