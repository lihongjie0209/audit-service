package routepolicy

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jmoiron/sqlx"
	appdb "github.com/lihongjie0209/audit-service/internal/database"
	"github.com/lihongjie0209/microservice-platform-go/authz"
	"github.com/lihongjie0209/microservice-platform-go/principal"
	platformpolicy "github.com/lihongjie0209/microservice-platform-go/routepolicy"
)

type Repository struct {
	db         *sqlx.DB
	transactor *appdb.Transactor
}

func NewRepository(db *sqlx.DB, transactor *appdb.Transactor) *Repository {
	return &Repository{db: db, transactor: transactor}
}

type policyRow struct {
	ID         string `db:"id"`
	RouteID    string `db:"route_id"`
	Expression string `db:"expression"`
	Version    int64  `db:"version"`
}
type permissionRow struct {
	PolicyID string `db:"policy_id"`
	ID       string `db:"id"`
	Key      string `db:"permission_key"`
	Resource string `db:"resource"`
	Action   string `db:"action"`
	Scope    string `db:"scope"`
}

var ErrNotFound = errors.New("route policy resource not found")
var ErrStaleVersion = errors.New("stale route policy version")

type Filter struct {
	Keyword, Protocol, RouteStatus, PolicyStatus string
}

type Detail struct {
	RouteID      string            `json:"route_id" db:"route_id"`
	Protocol     string            `json:"protocol" db:"protocol"`
	Method       string            `json:"method" db:"method"`
	Path         string            `json:"path" db:"path"`
	Operation    string            `json:"operation" db:"operation"`
	RouteStatus  string            `json:"route_status" db:"route_status"`
	PolicyID     string            `json:"policy_id" db:"policy_id"`
	Expression   string            `json:"expression" db:"expression"`
	Description  string            `json:"description" db:"description"`
	PolicyStatus string            `json:"policy_status" db:"policy_status"`
	Version      int64             `json:"version" db:"version"`
	Permissions  []PermissionInput `json:"permissions"`
}

func (r *Repository) Load(ctx context.Context) ([]platformpolicy.Definition, error) {
	if r.db == nil {
		return nil, errors.New("route policy database is disabled")
	}
	policies := []policyRow{}
	if err := r.db.SelectContext(ctx, &policies, `SELECT id,route_id,expression,version FROM route_policy_definitions WHERE status='active' AND deleted_at IS NULL ORDER BY route_id`); err != nil {
		return nil, fmt.Errorf("select route policies: %w", err)
	}
	refs := []permissionRow{}
	query := `SELECT r.policy_id,r.id,r.permission_key,r.resource,r.action,r.scope FROM route_policy_permission_refs r JOIN route_policy_definitions d ON d.id=r.policy_id AND d.deleted_at IS NULL AND d.status='active' WHERE r.deleted_at IS NULL ORDER BY r.policy_id,r.permission_key`
	if err := r.db.SelectContext(ctx, &refs, query); err != nil {
		return nil, fmt.Errorf("select route policy references: %w", err)
	}
	definitions := make([]platformpolicy.Definition, 0, len(policies))
	byID := make(map[string]*platformpolicy.Definition, len(policies))
	for _, row := range policies {
		definitions = append(definitions, platformpolicy.Definition{ID: row.ID, RouteID: row.RouteID, Expression: row.Expression, Permissions: map[string]platformpolicy.Permission{}, Version: row.Version})
		byID[row.ID] = &definitions[len(definitions)-1]
	}
	for _, row := range refs {
		definition := byID[row.PolicyID]
		if definition == nil {
			continue
		}
		scope, err := parseScope(row.Scope)
		if err != nil {
			return nil, err
		}
		definition.Permissions[row.Key] = platformpolicy.Permission{ID: row.ID, Key: row.Key, Resource: row.Resource, Action: row.Action, Scope: scope}
	}
	return definitions, nil
}
func parseScope(value string) (authz.Scope, error) {
	switch value {
	case "tenant":
		return authz.ScopeTenant, nil
	case "platform":
		return authz.ScopePlatform, nil
	case "principal":
		return authz.ScopePrincipal, nil
	default:
		return 0, fmt.Errorf("%w: invalid scope %q", platformpolicy.ErrInvalid, value)
	}
}

func (r *Repository) Revision(ctx context.Context) (time.Time, error) {
	var revision sql.NullTime
	if err := r.db.GetContext(ctx, &revision, `SELECT max(updated_at) FROM (SELECT updated_at FROM route_policy_definitions UNION ALL SELECT updated_at FROM route_policy_permission_refs) route_policy_revisions`); err != nil {
		return time.Time{}, err
	}
	return revision.Time, nil
}
func (r *Repository) ActiveRouteIDs(ctx context.Context, service string) ([]string, error) {
	ids := []string{}
	err := r.db.SelectContext(ctx, &ids, r.db.Rebind(`SELECT id FROM route_definitions WHERE service_name=? AND status='active' AND deleted_at IS NULL ORDER BY id`), service)
	return ids, err
}
func (r *Repository) SyncRoutes(ctx context.Context, routes []platformpolicy.Route, actor string) error {
	if len(routes) == 0 {
		return errors.New("route sync requires routes")
	}
	actorCtx := principal.SystemContext(ctx, actor)
	return r.transactor.Within(actorCtx, nil, func(tx *sqlx.Tx) error {
		now := time.Now()
		if _, err := tx.ExecContext(actorCtx, tx.Rebind(`UPDATE route_definitions SET status='inactive',updated_at=?,updated_by=? WHERE service_name=? AND protocol=? AND status='active' AND deleted_at IS NULL`), now, actor, routes[0].ServiceName, routes[0].Protocol); err != nil {
			return err
		}
		for _, route := range routes {
			var exists int
			err := tx.GetContext(actorCtx, &exists, tx.Rebind(`SELECT 1 FROM route_definitions WHERE id=? AND deleted_at IS NULL`), route.ID)
			switch {
			case errors.Is(err, sql.ErrNoRows):
				_, err = tx.ExecContext(actorCtx, tx.Rebind(`INSERT INTO route_definitions(id,protocol,method,path,operation,description,service_name,source_version,status,last_discovered_at,created_at,created_by,updated_at,updated_by,version) VALUES(?,?,?,?,?,?,?,?,?,?,?,?,?,?,1)`), route.ID, route.Protocol, route.Method, route.Path, route.Operation, route.Description, route.ServiceName, route.SourceVersion, "active", now, now, actor, now, actor)
			case err == nil:
				_, err = tx.ExecContext(actorCtx, tx.Rebind(`UPDATE route_definitions SET protocol=?,method=?,path=?,operation=?,description=?,service_name=?,source_version=?,status='active',last_discovered_at=?,updated_at=?,updated_by=? WHERE id=? AND deleted_at IS NULL`), route.Protocol, route.Method, route.Path, route.Operation, route.Description, route.ServiceName, route.SourceVersion, now, now, actor, route.ID)
			}
			if err != nil {
				return fmt.Errorf("sync route %s: %w", route.Path, err)
			}
		}
		return nil
	})
}

func (r *Repository) Page(ctx context.Context, filter Filter, limit, offset int) ([]Detail, int64, error) {
	where, args := routePolicyWhere(filter)
	var total int64
	if err := r.db.GetContext(ctx, &total, r.db.Rebind(`SELECT COUNT(*) FROM route_definitions r LEFT JOIN route_policy_definitions p ON p.route_id=r.id AND p.deleted_at IS NULL WHERE `+where), args...); err != nil {
		return nil, 0, err
	}
	args = append(args, limit, offset)
	items := []Detail{}
	query := `SELECT r.id route_id,r.protocol,r.method,r.path,r.operation,r.status route_status,COALESCE(p.id,'') policy_id,COALESCE(p.expression,'') expression,COALESCE(p.description,'') description,COALESCE(p.status,'') policy_status,COALESCE(p.version,0) version FROM route_definitions r LEFT JOIN route_policy_definitions p ON p.route_id=r.id AND p.deleted_at IS NULL WHERE ` + where + ` ORDER BY r.protocol,r.path LIMIT ? OFFSET ?`
	if err := r.db.SelectContext(ctx, &items, r.db.Rebind(query), args...); err != nil {
		return nil, 0, err
	}
	return items, total, nil
}

func routePolicyWhere(filter Filter) (string, []any) {
	parts := []string{"r.deleted_at IS NULL"}
	args := []any{}
	if value := strings.TrimSpace(filter.Keyword); value != "" {
		parts = append(parts, `(LOWER(r.path) LIKE ? OR LOWER(r.operation) LIKE ?)`)
		like := "%" + strings.ToLower(value) + "%"
		args = append(args, like, like)
	}
	for _, item := range []struct{ column, value string }{{"r.protocol", filter.Protocol}, {"r.status", filter.RouteStatus}, {"p.status", filter.PolicyStatus}} {
		column, value := item.column, item.value
		if value = strings.TrimSpace(value); value != "" {
			parts = append(parts, column+"=?")
			args = append(args, value)
		}
	}
	return strings.Join(parts, " AND "), args
}

func (r *Repository) Get(ctx context.Context, routeID string) (Detail, error) {
	var detail Detail
	query := `SELECT r.id route_id,r.protocol,r.method,r.path,r.operation,r.status route_status,COALESCE(p.id,'') policy_id,COALESCE(p.expression,'') expression,COALESCE(p.description,'') description,COALESCE(p.status,'') policy_status,COALESCE(p.version,0) version FROM route_definitions r LEFT JOIN route_policy_definitions p ON p.route_id=r.id AND p.deleted_at IS NULL WHERE r.id=? AND r.deleted_at IS NULL`
	if err := r.db.GetContext(ctx, &detail, r.db.Rebind(query), routeID); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return Detail{}, ErrNotFound
		}
		return Detail{}, err
	}
	if detail.PolicyID != "" {
		if err := r.db.SelectContext(ctx, &detail.Permissions, r.db.Rebind(`SELECT permission_key "key",resource,action,scope FROM route_policy_permission_refs WHERE policy_id=? AND deleted_at IS NULL ORDER BY permission_key`), detail.PolicyID); err != nil {
			return Detail{}, err
		}
	}
	return detail, nil
}

func (r *Repository) Set(ctx context.Context, input SetInput, actor string) error {
	actorCtx := principal.SystemContext(ctx, actor)
	return r.transactor.Within(actorCtx, nil, func(tx *sqlx.Tx) error {
		var routeExists int
		if err := tx.GetContext(actorCtx, &routeExists, tx.Rebind(`SELECT 1 FROM route_definitions WHERE id=? AND deleted_at IS NULL`), input.RouteID); err != nil {
			if errors.Is(err, sql.ErrNoRows) {
				return ErrNotFound
			}
			return err
		}
		var row policyRow
		err := tx.GetContext(actorCtx, &row, tx.Rebind(`SELECT id,route_id,expression,version FROM route_policy_definitions WHERE route_id=? AND deleted_at IS NULL FOR UPDATE`), input.RouteID)
		now := time.Now()
		switch {
		case errors.Is(err, sql.ErrNoRows):
			if input.ExpectedVersion != 0 {
				return ErrStaleVersion
			}
			row.ID = uuid.NewString()
			if _, err = tx.ExecContext(actorCtx, tx.Rebind(`INSERT INTO route_policy_definitions(id,route_id,expression,description,status,created_at,created_by,updated_at,updated_by,version) VALUES(?,?,?,?,?,?,?,?,?,1)`), row.ID, input.RouteID, input.Expression, input.Description, input.Status, now, actor, now, actor); err != nil {
				return err
			}
		case err != nil:
			return err
		default:
			if row.Version != input.ExpectedVersion {
				return ErrStaleVersion
			}
			result, updateErr := tx.ExecContext(actorCtx, tx.Rebind(`UPDATE route_policy_definitions SET expression=?,description=?,status=?,updated_at=?,updated_by=? WHERE id=? AND version=? AND deleted_at IS NULL`), input.Expression, input.Description, input.Status, now, actor, row.ID, input.ExpectedVersion)
			if updateErr != nil {
				return updateErr
			}
			changed, _ := result.RowsAffected()
			if changed == 0 {
				return ErrStaleVersion
			}
		}
		existing := []permissionRow{}
		if err := tx.SelectContext(actorCtx, &existing, tx.Rebind(`SELECT policy_id,id,permission_key,resource,action,scope FROM route_policy_permission_refs WHERE policy_id=?`), row.ID); err != nil {
			return err
		}
		byKey := make(map[string]permissionRow, len(existing))
		for _, ref := range existing {
			byKey[ref.Key] = ref
		}
		desired := make(map[string]struct{}, len(input.Permissions))
		for _, ref := range input.Permissions {
			desired[ref.Key] = struct{}{}
			if old, ok := byKey[ref.Key]; ok {
				if _, err := tx.ExecContext(actorCtx, tx.Rebind(`UPDATE route_policy_permission_refs SET resource=?,action=?,scope=?,deleted_at=NULL,deleted_by=NULL,updated_at=?,updated_by=? WHERE id=?`), ref.Resource, ref.Action, ref.Scope, now, actor, old.ID); err != nil {
					return err
				}
			} else if _, err := tx.ExecContext(actorCtx, tx.Rebind(`INSERT INTO route_policy_permission_refs(id,policy_id,permission_key,resource,action,scope,created_at,created_by,updated_at,updated_by,version) VALUES(?,?,?,?,?,?,?,?,?,?,1)`), uuid.NewString(), row.ID, ref.Key, ref.Resource, ref.Action, ref.Scope, now, actor, now, actor); err != nil {
				return err
			}
		}
		for _, old := range existing {
			if _, keep := desired[old.Key]; !keep {
				if _, err := tx.ExecContext(actorCtx, tx.Rebind(`UPDATE route_policy_permission_refs SET deleted_at=?,deleted_by=?,updated_at=?,updated_by=? WHERE id=? AND deleted_at IS NULL`), now, actor, now, actor, old.ID); err != nil {
					return err
				}
			}
		}
		return nil
	})
}
