package routepolicy

import (
	"errors"
	"regexp"
	"testing"

	"github.com/DATA-DOG/go-sqlmock"
	"github.com/jmoiron/sqlx"
	appdb "github.com/lihongjie0209/audit-service/internal/database"
	"github.com/lihongjie0209/microservice-platform-go/authz"
	"github.com/lihongjie0209/microservice-platform-go/principal"
	platformpolicy "github.com/lihongjie0209/microservice-platform-go/routepolicy"
)

func TestRepositoryLoadsDenormalizedPermissionReferences(t *testing.T) {
	database, mock, err := sqlmock.New()
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = database.Close() })
	db := sqlx.NewDb(database, "sqlmock")
	repository := NewRepository(db, appdb.NewTransactor(db))
	mock.ExpectQuery(regexp.QuoteMeta(`SELECT id,route_id,expression,version FROM route_policy_definitions WHERE status='active' AND deleted_at IS NULL ORDER BY route_id`)).WillReturnRows(sqlmock.NewRows([]string{"id", "route_id", "expression", "version"}).AddRow("policy-1", "route-1", `permissions["application.read"]`, 2))
	mock.ExpectQuery(regexp.QuoteMeta(`SELECT r.policy_id,r.id,r.permission_key,r.resource,r.action,r.scope FROM route_policy_permission_refs r JOIN route_policy_definitions d ON d.id=r.policy_id AND d.deleted_at IS NULL AND d.status='active' WHERE r.deleted_at IS NULL ORDER BY r.policy_id,r.permission_key`)).WillReturnRows(sqlmock.NewRows([]string{"policy_id", "id", "permission_key", "resource", "action", "scope"}).AddRow("policy-1", "ref-1", "application.read", "application.catalog", "read", "platform"))
	definitions, err := repository.Load(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	permission := definitions[0].Permissions["application.read"]
	if len(definitions) != 1 || definitions[0].RouteID != "route-1" || permission.Scope != authz.ScopePlatform || permission.Resource != "application.catalog" {
		t.Fatalf("definitions = %+v", definitions)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatal(err)
	}
}

func TestServiceRejectsInvalidExpressionBeforePersistence(t *testing.T) {
	compiler, err := platformpolicy.NewCompiler()
	if err != nil {
		t.Fatal(err)
	}
	service := NewService(nil, nil, compiler, nil, nil)
	ctx := principal.WithContext(t.Context(), principal.Principal{ID: "admin", Type: principal.TypeUser})
	_, err = service.Set(ctx, SetInput{RouteID: "route-1", Expression: `permissions["missing"]`, Status: "active"})
	if err == nil {
		t.Fatal("invalid expression unexpectedly reached persistence")
	}
	if !errors.Is(err, platformpolicy.ErrInvalid) {
		t.Fatalf("error = %v", err)
	}
}

func TestRoutePolicyWhereKeepsCountAndItemsFiltersEquivalent(t *testing.T) {
	where, args := routePolicyWhere(Filter{Keyword: "Menu", Protocol: "http", RouteStatus: "active", PolicyStatus: "disabled"})
	want := `r.deleted_at IS NULL AND (LOWER(r.path) LIKE ? OR LOWER(r.operation) LIKE ?) AND r.protocol=? AND r.status=? AND p.status=?`
	if where != want {
		t.Fatalf("where = %q", where)
	}
	if len(args) != 5 || args[0] != "%menu%" || args[2] != "http" || args[4] != "disabled" {
		t.Fatalf("args = %#v", args)
	}
}

func TestBootstrapRouteIDsMatchSharedStableIdentity(t *testing.T) {
	t.Parallel()
	for path, want := range map[string]string{
		"/api/v1/version":              "7e138924-3ae3-5a84-9bfe-c640ed009e29",
		"/api/v1/me":                   "730a9a3a-0c83-5718-bf58-492d4e942cfd",
		"/api/v1/route-policies/page":  "aa24b363-285c-5d9b-a74b-aa7f336eb5a1",
		"/api/v1/route-policies/get":   "e8e0c9d7-1383-551e-ba67-29c7f190d1a5",
		"/api/v1/route-policies/set":   "f8006a81-2610-54d8-9155-528cd250a2eb",
		"/api/v1/audit/records/create": "0a9152f9-642e-5966-b809-43d86298e8db",
		"/api/v1/audit/records/export": "5f2b65f6-859a-5515-b529-041bc0f8d711",
	} {
		route, err := platformpolicy.NewRoute("http", "post", path, "audit-service", "")
		if err != nil || route.ID != want {
			t.Fatalf("route %s id = %q, %v; want %q", path, route.ID, err, want)
		}
	}
}
