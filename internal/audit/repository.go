package audit

import (
	"context"
	"database/sql"
	"errors"
	"strings"

	"github.com/go-sql-driver/mysql"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jmoiron/sqlx"
)

var ErrNotFound = errors.New("audit record not found")
var ErrDuplicate = errors.New("audit record already exists")

type Repository interface {
	Create(context.Context, sqlx.ExtContext, Record) error
	Get(context.Context, string, string) (Record, error)
	Query(context.Context, Filter) ([]Record, int64, error)
}

type SQLRepository struct{ db *sqlx.DB }

func NewRepository(db *sqlx.DB) Repository { return &SQLRepository{db: db} }

const recordColumns = `id, tenant_id, application_id, actor_id, actor_type, action, resource_type, resource_id, request_id, trace_id, source_service, before_summary, after_summary, occurred_at, version, created_at, updated_at, created_by, updated_by`

func (r *SQLRepository) Create(ctx context.Context, executor sqlx.ExtContext, value Record) error {
	if _, err := executor.ExecContext(ctx, r.db.Rebind(`INSERT INTO audit_record_keys (id,occurred_at,version,created_at,updated_at,created_by,updated_by) VALUES (?,?,?,?,?,?,?)`), value.ID, value.OccurredAt, 1, value.CreatedAt, value.UpdatedAt, value.CreatedBy, value.UpdatedBy); err != nil {
		if uniqueViolation(err) {
			return ErrDuplicate
		}
		return err
	}
	query := r.db.Rebind(`INSERT INTO audit_records (` + recordColumns + `) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`)
	_, err := executor.ExecContext(ctx, query, value.ID, value.TenantID, value.ApplicationID, value.ActorID, value.ActorType, value.Action, value.ResourceType, value.ResourceID, value.RequestID, value.TraceID, value.SourceService, value.BeforeSummary, value.AfterSummary, value.OccurredAt, value.Version, value.CreatedAt, value.UpdatedAt, value.CreatedBy, value.UpdatedBy)
	return err
}

func uniqueViolation(err error) bool {
	var mysqlError *mysql.MySQLError
	if errors.As(err, &mysqlError) {
		return mysqlError.Number == 1062
	}
	var postgresError *pgconn.PgError
	return errors.As(err, &postgresError) && postgresError.Code == "23505"
}

func (r *SQLRepository) Get(ctx context.Context, id, tenantID string) (Record, error) {
	var value Record
	err := r.db.GetContext(ctx, &value, r.db.Rebind(`SELECT `+recordColumns+` FROM audit_records WHERE id = ? AND tenant_id = ? AND deleted_at IS NULL`), id, tenantID)
	if errors.Is(err, sql.ErrNoRows) {
		return Record{}, ErrNotFound
	}
	return value, err
}

func (r *SQLRepository) Query(ctx context.Context, filter Filter) ([]Record, int64, error) {
	where, args, err := queryWhere(filter)
	if err != nil {
		return nil, 0, err
	}
	var total int64
	if err := r.db.GetContext(ctx, &total, r.db.Rebind(`SELECT count(*) FROM audit_records`+where), args...); err != nil {
		return nil, 0, err
	}
	args = append(args, filter.PageSize, (filter.Page-1)*filter.PageSize)
	var values []Record
	err = r.db.SelectContext(ctx, &values, r.db.Rebind(`SELECT `+recordColumns+` FROM audit_records`+where+` ORDER BY occurred_at DESC, id DESC LIMIT ? OFFSET ?`), args...)
	return values, total, err
}

func queryWhere(filter Filter) (string, []any, error) {
	parts := []string{"tenant_id = ?", "deleted_at IS NULL"}
	args := []any{filter.TenantID}
	for _, item := range []struct{ column, value string }{
		{"application_id", filter.ApplicationID}, {"actor_id", filter.ActorID}, {"actor_type", filter.ActorType},
		{"action", filter.Action}, {"resource_type", filter.ResourceType}, {"resource_id", filter.ResourceID},
		{"request_id", filter.RequestID}, {"trace_id", filter.TraceID}, {"source_service", filter.SourceService},
	} {
		if item.value != "" {
			parts = append(parts, item.column+" = ?")
			args = append(args, item.value)
		}
	}
	if len(filter.IDs) > 0 {
		query, inArgs, err := sqlx.In("id IN (?)", filter.IDs)
		if err != nil {
			return "", nil, err
		}
		parts = append(parts, query)
		args = append(args, inArgs...)
	}
	if filter.Keyword != "" {
		parts = append(parts, `(LOWER(actor_id) LIKE ? OR LOWER(action) LIKE ? OR LOWER(resource_type) LIKE ? OR LOWER(resource_id) LIKE ? OR LOWER(request_id) LIKE ? OR LOWER(trace_id) LIKE ? OR LOWER(source_service) LIKE ?)`)
		like := "%" + strings.ToLower(filter.Keyword) + "%"
		for range 7 {
			args = append(args, like)
		}
	}
	if !filter.OccurredFrom.IsZero() {
		parts = append(parts, "occurred_at >= ?")
		args = append(args, filter.OccurredFrom)
	}
	if !filter.OccurredTo.IsZero() {
		parts = append(parts, "occurred_at <= ?")
		args = append(args, filter.OccurredTo)
	}
	return " WHERE " + strings.Join(parts, " AND "), args, nil
}
