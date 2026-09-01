package audit

import (
	"context"
	"database/sql"
	"errors"

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
	err := r.db.GetContext(ctx, &value, r.db.Rebind(`SELECT `+recordColumns+` FROM audit_records WHERE id = ? AND tenant_id = ?`), id, tenantID)
	if errors.Is(err, sql.ErrNoRows) {
		return Record{}, ErrNotFound
	}
	return value, err
}

func (r *SQLRepository) Query(ctx context.Context, filter Filter) ([]Record, int64, error) {
	where := ` WHERE tenant_id = ?` +
		` AND (? = '' OR application_id = ?)` +
		` AND (? = '' OR actor_id = ?)` +
		` AND (? = '' OR actor_type = ?)` +
		` AND (? = '' OR action = ?)` +
		` AND (? = '' OR resource_type = ?)` +
		` AND (? = '' OR resource_id = ?)` +
		` AND (? = '' OR request_id = ?)` +
		` AND (? = '' OR trace_id = ?)` +
		` AND (? = '' OR source_service = ?)` +
		` AND (? = FALSE OR occurred_at >= ?)` +
		` AND (? = FALSE OR occurred_at <= ?)`
	args := []any{
		filter.TenantID,
		filter.ApplicationID, filter.ApplicationID,
		filter.ActorID, filter.ActorID,
		filter.ActorType, filter.ActorType,
		filter.Action, filter.Action,
		filter.ResourceType, filter.ResourceType,
		filter.ResourceID, filter.ResourceID,
		filter.RequestID, filter.RequestID,
		filter.TraceID, filter.TraceID,
		filter.SourceService, filter.SourceService,
		!filter.OccurredFrom.IsZero(), filter.OccurredFrom,
		!filter.OccurredTo.IsZero(), filter.OccurredTo,
	}
	var total int64
	if err := r.db.GetContext(ctx, &total, r.db.Rebind(`SELECT count(*) FROM audit_records`+where), args...); err != nil {
		return nil, 0, err
	}
	args = append(args, filter.PageSize, (filter.Page-1)*filter.PageSize)
	var values []Record
	err := r.db.SelectContext(ctx, &values, r.db.Rebind(`SELECT `+recordColumns+` FROM audit_records`+where+` ORDER BY occurred_at DESC, id DESC LIMIT ? OFFSET ?`), args...)
	return values, total, err
}
