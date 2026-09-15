package routepolicy

import (
	"context"
	"errors"
	"log/slog"
	"strings"
	"time"

	"github.com/lihongjie0209/audit-service/internal/apperror"
	"github.com/lihongjie0209/audit-service/internal/requestid"
	"github.com/lihongjie0209/microservice-platform-go/operationlog"
	"github.com/lihongjie0209/microservice-platform-go/principal"
	platformpolicy "github.com/lihongjie0209/microservice-platform-go/routepolicy"
)

type PermissionInput struct {
	Key      string `json:"key" db:"key"`
	Resource string `json:"resource" db:"resource"`
	Action   string `json:"action" db:"action"`
	Scope    string `json:"scope" db:"scope"`
}

type SetInput struct {
	RouteID, Expression, Description, Status string
	ExpectedVersion                          int64
	Permissions                              []PermissionInput
}

type Service struct {
	repository *Repository
	manager    *Manager
	compiler   *platformpolicy.Compiler
	operations operationlog.Recorder
	logger     *slog.Logger
}

func NewService(repository *Repository, manager *Manager, compiler *platformpolicy.Compiler, operations operationlog.Recorder, logger *slog.Logger) *Service {
	return &Service{repository: repository, manager: manager, compiler: compiler, operations: operations, logger: logger}
}

func (s *Service) Page(ctx context.Context, filter Filter, page, size int) ([]Detail, int64, error) {
	if page < 1 || size < 1 || size > 200 {
		return nil, 0, apperror.Invalid("page must be positive and page_size must be between 1 and 200", nil)
	}
	items, total, err := s.repository.Page(ctx, filter, size, (page-1)*size)
	if err != nil {
		return nil, 0, apperror.Internal(err)
	}
	return items, total, nil
}

func (s *Service) Get(ctx context.Context, routeID string) (Detail, error) {
	detail, err := s.repository.Get(ctx, strings.TrimSpace(routeID))
	if errors.Is(err, ErrNotFound) {
		return Detail{}, apperror.NotFound("route policy not found")
	}
	if err != nil {
		return Detail{}, apperror.Internal(err)
	}
	return detail, nil
}

func (s *Service) Set(ctx context.Context, input SetInput) (Detail, error) {
	started := time.Now()
	actor, ok := principal.FromContext(ctx)
	if !ok || strings.TrimSpace(actor.ID) == "" {
		return Detail{}, apperror.Unauthorized("authenticated principal is required")
	}
	input.RouteID, input.Expression, input.Description, input.Status = strings.TrimSpace(input.RouteID), strings.TrimSpace(input.Expression), strings.TrimSpace(input.Description), strings.TrimSpace(input.Status)
	if input.RouteID == "" || input.Expression == "" || (input.Status != "active" && input.Status != "disabled") || input.ExpectedVersion < 0 {
		return Detail{}, apperror.Invalid("route_id, expression, valid status and non-negative expected_version are required", nil)
	}
	definition := platformpolicy.Definition{ID: "validation", RouteID: input.RouteID, Expression: input.Expression, Permissions: map[string]platformpolicy.Permission{}}
	for _, ref := range input.Permissions {
		ref.Key, ref.Resource, ref.Action, ref.Scope = strings.TrimSpace(ref.Key), strings.TrimSpace(ref.Resource), strings.TrimSpace(ref.Action), strings.TrimSpace(ref.Scope)
		scope, err := parseScope(ref.Scope)
		if err != nil || ref.Key == "" || ref.Resource == "" || ref.Action == "" {
			return Detail{}, apperror.Invalid("invalid permission reference", err)
		}
		if _, duplicate := definition.Permissions[ref.Key]; duplicate {
			return Detail{}, apperror.Invalid("duplicate permission key", nil)
		}
		definition.Permissions[ref.Key] = platformpolicy.Permission{Key: ref.Key, Resource: ref.Resource, Action: ref.Action, Scope: scope}
	}
	if _, err := s.compiler.Compile(definition); err != nil {
		return Detail{}, apperror.Invalid("invalid route policy expression", err)
	}
	err := s.repository.Set(ctx, input, actor.ID)
	switch {
	case errors.Is(err, ErrNotFound):
		err = apperror.NotFound("route not found")
	case errors.Is(err, ErrStaleVersion):
		err = apperror.Conflict("route policy version is stale", err)
	case err != nil:
		err = apperror.Internal(err)
	}
	if err == nil {
		if refreshErr := s.manager.RefreshSource(ctx, "write"); refreshErr != nil {
			err = apperror.Unavailable("refresh route policy cache", refreshErr)
		} else if notifyErr := s.manager.Notify(ctx); notifyErr != nil {
			s.logger.WarnContext(ctx, "notify route policy peers", "error", notifyErr)
		}
	}
	requestID, _ := requestid.FromContext(ctx)
	entry := operationlog.Entry{Operation: "route_policy.set", ResourceType: "route_policy", ResourceID: input.RouteID, Source: "audit-service", Protocol: "internal", Request: map[string]any{"route_id": input.RouteID, "status": input.Status, "expected_version": input.ExpectedVersion}, RequestID: requestID, Duration: time.Since(started), Succeeded: err == nil}
	if err != nil {
		entry.ErrorMessage = err.Error()
	}
	if s.operations != nil && s.operations.Enabled() {
		persistCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 2*time.Second)
		defer cancel()
		if recordErr := s.operations.Record(persistCtx, entry); recordErr != nil && err == nil {
			return Detail{}, apperror.Unavailable("operation log unavailable", recordErr)
		}
	}
	if err != nil {
		return Detail{}, err
	}
	return s.Get(ctx, input.RouteID)
}
