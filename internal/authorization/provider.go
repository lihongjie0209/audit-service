package authorization

import (
	"time"

	"github.com/lihongjie0209/audit-service/internal/outbound"
	platformauthz "github.com/lihongjie0209/microservice-platform-go/authz"
	authorizationv1 "github.com/lihongjie0209/platform-protos/gen/go/platform/authorization/v1"
)

func New(registry *outbound.Registry) platformauthz.Authorizer {
	connection, ok := registry.GRPC("authorization")
	if !ok {
		return platformauthz.NewGRPCAuthorizer(nil, 2*time.Second)
	}
	return platformauthz.NewGRPCAuthorizer(authorizationv1.NewAuthorizationServiceClient(connection), 2*time.Second)
}
