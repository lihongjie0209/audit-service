package audit

import (
	"bytes"
	"context"
	"encoding/csv"
	"errors"
	"strconv"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jmoiron/sqlx"
	"github.com/lihongjie0209/audit-service/internal/apperror"
	"github.com/lihongjie0209/audit-service/internal/database"
	platformprincipal "github.com/lihongjie0209/microservice-platform-go/principal"
	"github.com/lihongjie0209/microservice-platform-go/redact"
	"go.uber.org/fx"
)

type Service struct {
	repository Repository
	transactor *database.Transactor
	now        func() time.Time
}

func NewService(repository Repository, transactor *database.Transactor) *Service {
	return &Service{repository: repository, transactor: transactor, now: time.Now}
}

func (s *Service) Record(ctx context.Context, value Record) (Record, error) {
	value.TenantID, value.Action, value.ResourceType, value.ResourceID = strings.TrimSpace(value.TenantID), strings.TrimSpace(value.Action), strings.TrimSpace(value.ResourceType), strings.TrimSpace(value.ResourceID)
	value.SourceService = strings.TrimSpace(value.SourceService)
	if value.Action == "" || value.ResourceType == "" || value.ResourceID == "" || value.SourceService == "" {
		return Record{}, apperror.Invalid("action, resource_type, resource_id and source_service are required", nil)
	}
	caller, ok := platformprincipal.FromContext(ctx)
	if !ok {
		return Record{}, apperror.Unauthorized("authenticated actor is required")
	}
	if err := enforceTenant(caller, value.TenantID); err != nil {
		return Record{}, err
	}
	before, err := redact.JSON(value.BeforeSummary)
	if err != nil {
		return Record{}, apperror.Invalid("before_summary must be valid JSON", err)
	}
	after, err := redact.JSON(value.AfterSummary)
	if err != nil {
		return Record{}, apperror.Invalid("after_summary must be valid JSON", err)
	}
	now := s.now()
	if value.ID == "" {
		value.ID = uuid.NewString()
	} else if existing, getErr := s.repository.Get(ctx, value.ID, value.TenantID); getErr == nil {
		return existing, nil
	} else if !errors.Is(getErr, ErrNotFound) {
		return Record{}, apperror.Internal(getErr)
	}
	if value.ActorID == "" {
		value.ActorID, value.ActorType = caller.ID, string(caller.Type)
	}
	if value.OccurredAt.IsZero() {
		value.OccurredAt = now
	}
	value.BeforeSummary, value.AfterSummary = before, after
	value.Version, value.CreatedAt, value.UpdatedAt, value.CreatedBy, value.UpdatedBy = 1, now, now, caller.ID, caller.ID
	err = s.transactor.Within(ctx, nil, func(tx *sqlx.Tx) error { return s.repository.Create(ctx, tx, value) })
	if errors.Is(err, ErrDuplicate) {
		existing, getErr := s.repository.Get(ctx, value.ID, value.TenantID)
		if getErr == nil {
			return existing, nil
		}
	}
	if err != nil {
		return Record{}, apperror.Internal(err)
	}
	return value, nil
}

func (s *Service) Get(ctx context.Context, id, tenantID string) (Record, error) {
	caller, ok := platformprincipal.FromContext(ctx)
	if !ok {
		return Record{}, apperror.Unauthorized("authenticated actor is required")
	}
	if err := enforceTenant(caller, tenantID); err != nil {
		return Record{}, err
	}
	value, err := s.repository.Get(ctx, strings.TrimSpace(id), strings.TrimSpace(tenantID))
	if errors.Is(err, ErrNotFound) {
		return Record{}, apperror.NotFound("audit record not found")
	}
	if err != nil {
		return Record{}, apperror.Internal(err)
	}
	return value, nil
}

func (s *Service) Query(ctx context.Context, filter Filter) (Page, error) {
	caller, ok := platformprincipal.FromContext(ctx)
	if !ok {
		return Page{}, apperror.Unauthorized("authenticated actor is required")
	}
	if strings.TrimSpace(filter.TenantID) == "" {
		return Page{}, apperror.Invalid("tenant_id is required", nil)
	}
	if err := enforceTenant(caller, filter.TenantID); err != nil {
		return Page{}, err
	}
	if filter.Page <= 0 {
		filter.Page = 1
	}
	if filter.PageSize <= 0 {
		filter.PageSize = 20
	}
	if filter.PageSize > 100 {
		return Page{}, apperror.Invalid("page_size must not exceed 100", nil)
	}
	values, total, err := s.repository.Query(ctx, filter)
	if err != nil {
		return Page{}, apperror.Internal(err)
	}
	return Page{Records: values, Total: total, Page: filter.Page, PageSize: filter.PageSize}, nil
}

func enforceTenant(caller platformprincipal.Principal, requestedTenantID string) error {
	if caller.Type == platformprincipal.TypeUser && (strings.TrimSpace(caller.TenantID) == "" || caller.TenantID != strings.TrimSpace(requestedTenantID)) {
		return apperror.Forbidden("tenant access denied")
	}
	return nil
}

func (s *Service) Export(ctx context.Context, filter Filter, maxRecords int) ([]byte, int64, error) {
	if maxRecords <= 0 {
		maxRecords = 10_000
	}
	if maxRecords > 10_000 {
		return nil, 0, apperror.Invalid("max_records must not exceed 10000", nil)
	}
	var output bytes.Buffer
	writer := csv.NewWriter(&output)
	if err := writer.Write([]string{"id", "tenant_id", "actor_id", "actor_type", "action", "resource_type", "resource_id", "request_id", "trace_id", "source_service", "before_summary", "after_summary", "occurred_at", "version", "created_at", "created_by"}); err != nil {
		return nil, 0, apperror.Internal(err)
	}
	var exported int64
	for page := 1; exported < int64(maxRecords); page++ {
		filter.Page, filter.PageSize = page, min(100, maxRecords-int(exported))
		result, err := s.Query(ctx, filter)
		if err != nil {
			return nil, 0, err
		}
		for _, value := range result.Records {
			record := []string{value.ID, value.TenantID, value.ActorID, value.ActorType, value.Action, value.ResourceType, value.ResourceID, value.RequestID, value.TraceID, value.SourceService, string(value.BeforeSummary), string(value.AfterSummary), value.OccurredAt.Format(time.RFC3339Nano), strconv.FormatInt(value.Version, 10), value.CreatedAt.Format(time.RFC3339Nano), value.CreatedBy}
			for index := range record {
				record[index] = csvSafe(record[index])
			}
			if err := writer.Write(record); err != nil {
				return nil, 0, apperror.Internal(err)
			}
			exported++
		}
		if len(result.Records) < filter.PageSize || exported >= result.Total {
			break
		}
	}
	writer.Flush()
	if err := writer.Error(); err != nil {
		return nil, 0, apperror.Internal(err)
	}
	return output.Bytes(), exported, nil
}

func csvSafe(value string) string {
	if value != "" && strings.ContainsRune("=+-@", rune(value[0])) {
		return "'" + value
	}
	return value
}

var Module = fx.Module("audit", fx.Provide(NewRepository, NewService))
