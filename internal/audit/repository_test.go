package audit

import (
	"strings"
	"testing"
	"time"
)

func TestQueryWhereBuildsBoundedStandardFilters(t *testing.T) {
	t.Parallel()
	from := time.Date(2026, 9, 1, 0, 0, 0, 0, time.UTC)
	to := from.Add(24 * time.Hour)
	where, args, err := queryWhere(Filter{TenantID: "tenant-1", ApplicationID: "app-1", ActorType: "user", TraceID: "trace-1", SourceService: "identity-service", Keyword: "Login", IDs: []string{"id-1", "id-2"}, OccurredFrom: from, OccurredTo: to})
	if err != nil {
		t.Fatal(err)
	}
	for _, fragment := range []string{"tenant_id = ?", "deleted_at IS NULL", "application_id = ?", "actor_type = ?", "trace_id = ?", "source_service = ?", "id IN (?, ?)", "LOWER(action) LIKE ?", "occurred_at >= ?", "occurred_at <= ?"} {
		if !strings.Contains(where, fragment) {
			t.Fatalf("where %q does not contain %q", where, fragment)
		}
	}
	if len(args) != 16 {
		t.Fatalf("args = %#v", args)
	}
}

func TestNormalizeFilterTrimsAndDeduplicatesIDs(t *testing.T) {
	t.Parallel()
	filter := normalizeFilter(Filter{Keyword: " login ", TenantID: " tenant ", SourceService: " identity-service ", IDs: []string{" id-1 ", "", "id-1", "id-2"}})
	if filter.Keyword != "login" || filter.TenantID != "tenant" || filter.SourceService != "identity-service" || len(filter.IDs) != 2 || filter.IDs[0] != "id-1" || filter.IDs[1] != "id-2" {
		t.Fatalf("filter = %+v", filter)
	}
}
