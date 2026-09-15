package database

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestAuditTablesHaveImmutableDatabaseTriggersForEveryDialect(t *testing.T) {
	t.Parallel()
	for _, dialect := range []string{"postgres", "kingbase", "mysql"} {
		dialect := dialect
		t.Run(dialect, func(t *testing.T) {
			t.Parallel()
			content, err := os.ReadFile(filepath.Join("..", "..", "migrations", dialect, "000005_database_audit.up.sql"))
			if err != nil {
				t.Fatal(err)
			}
			text := strings.ToLower(string(content))
			for _, table := range []string{"audit_records", "audit_record_keys"} {
				if !strings.Contains(text, table) || !strings.Contains(text, "deleted_at") || !strings.Contains(text, "deleted_by") {
					t.Fatalf("%s migration does not add complete audit shape for %s", dialect, table)
				}
			}
			if dialect == "mysql" {
				for _, suffix := range []string{"_bi", "_bu", "_bd"} {
					if strings.Count(text, suffix) < 2 {
						t.Fatalf("%s migration lacks %s trigger coverage", dialect, suffix)
					}
				}
			} else if strings.Count(text, "execute function audit_immutable_row()") != 2 {
				t.Fatalf("%s migration lacks immutable trigger coverage", dialect)
			}
		})
	}
}

func TestRoutePolicyTablesHaveMutableDatabaseAuditTriggersForEveryDialect(t *testing.T) {
	t.Parallel()
	for _, dialect := range []string{"postgres", "kingbase", "mysql"} {
		dialect := dialect
		t.Run(dialect, func(t *testing.T) {
			t.Parallel()
			content, err := os.ReadFile(filepath.Join("..", "..", "migrations", dialect, "000007_route_policies.up.sql"))
			if err != nil {
				t.Fatal(err)
			}
			text := strings.ToLower(string(content))
			if strings.Contains(text, "application-service") || !strings.Contains(text, "audit-service:migration") {
				t.Fatal("route policy bootstrap ownership is not audit-service")
			}
			for _, table := range []string{"route_definitions", "route_policy_definitions", "route_policy_permission_refs"} {
				for _, field := range []string{"created_at", "created_by", "updated_at", "updated_by", "version", "deleted_at", "deleted_by"} {
					if !strings.Contains(text, table) || !strings.Contains(text, field) {
						t.Fatalf("%s migration lacks %s on %s", dialect, field, table)
					}
				}
			}
			if dialect == "mysql" {
				for _, suffix := range []string{"_bi", "_bu", "_bd"} {
					if strings.Count(text, suffix) < 3 {
						t.Fatalf("%s migration lacks %s trigger coverage", dialect, suffix)
					}
				}
			} else if strings.Count(text, "execute function audit_mutable_row()") != 3 {
				t.Fatalf("%s migration lacks mutable trigger coverage", dialect)
			}
		})
	}
}
