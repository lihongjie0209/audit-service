//go:build integration

package integration

import (
	"context"
	"fmt"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jmoiron/sqlx"
	auditdomain "github.com/lihongjie0209/audit-service/internal/audit"
	"github.com/lihongjie0209/audit-service/internal/config"
	appdb "github.com/lihongjie0209/audit-service/internal/database"
	"github.com/lihongjie0209/audit-service/internal/migration"
	"github.com/lihongjie0209/microservice-platform-go/principal"
	"github.com/testcontainers/testcontainers-go"
	"github.com/testcontainers/testcontainers-go/modules/mysql"
	"github.com/testcontainers/testcontainers-go/modules/postgres"
)

func TestRepositoryAndMigrations(t *testing.T) {
	for _, databaseType := range []string{"postgres", "mysql"} {
		t.Run(databaseType, func(t *testing.T) {
			ctx, cancel := context.WithTimeout(t.Context(), 2*time.Minute)
			defer cancel()
			dsn, migrationURL := startDatabase(t, ctx, databaseType)
			migrationPath, err := filepath.Abs(filepath.Join("..", "migrations", databaseType))
			if err != nil {
				t.Fatal(err)
			}
			schema := ""
			if databaseType == "postgres" {
				schema = "integration_postgres"
			}
			migrationCfg := config.Migration{Path: migrationPath, DatabaseURL: migrationURL, Table: "integration_" + databaseType + "_schema_migrations", Schema: schema, CreateSchema: schema != ""}
			migrationErrors := make(chan error, 3)
			var migrations sync.WaitGroup
			for range 3 {
				migrations.Add(1)
				go func() {
					defer migrations.Done()
					migrationErrors <- migration.Run(migrationCfg, "up", 0)
				}()
			}
			migrations.Wait()
			close(migrationErrors)
			for err := range migrationErrors {
				if err != nil {
					t.Fatalf("concurrent migration up: %v", err)
				}
			}

			db, err := appdb.Open(ctx, config.Database{Type: databaseType, DSN: dsn, Schema: schema, MaxOpenConns: 5, MaxIdleConns: 2, ConnMaxLifetime: time.Minute, ConnMaxIdleTime: time.Minute, PingTimeout: 10 * time.Second})
			if err != nil {
				t.Fatal(err)
			}
			t.Cleanup(func() { _ = db.Close() })
			repository := auditdomain.NewRepository(db)
			ctx = principal.WithContext(ctx, principal.Principal{ID: "integration-auditor", Type: principal.TypeSystem})
			now := time.Now().Truncate(time.Microsecond)
			record := auditdomain.Record{ID: uuid.NewString(), TenantID: "tenant-1", ApplicationID: "application-1", ActorID: "user-1", ActorType: "user", Action: "created", ResourceType: "invoice", ResourceID: "invoice-1", RequestID: "request-1", TraceID: "trace-1", SourceService: "billing-service", BeforeSummary: []byte(`{}`), AfterSummary: []byte(`{"status":"draft"}`), OccurredAt: now, Version: 1, CreatedAt: now, UpdatedAt: now, CreatedBy: "user-1", UpdatedBy: "user-1"}
			if err := appdb.NewTransactor(db).Within(ctx, nil, func(tx *sqlx.Tx) error { return repository.Create(ctx, tx, record) }); err != nil {
				t.Fatalf("create audit record: %v", err)
			}
			found, err := repository.Get(ctx, record.ID, record.TenantID)
			if err != nil || found.ResourceID != record.ResourceID {
				t.Fatalf("get audit record=%+v err=%v", found, err)
			}
			if found.CreatedBy != "integration-auditor" || found.UpdatedBy != "integration-auditor" || found.Version != 1 {
				t.Fatalf("database-owned audit fields = %+v", found)
			}
			if err := appdb.NewTransactor(db).Within(ctx, nil, func(tx *sqlx.Tx) error {
				_, err := tx.ExecContext(ctx, db.Rebind(`UPDATE audit_records SET action=? WHERE id=? AND occurred_at=?`), "tampered", record.ID, record.OccurredAt)
				return err
			}); err == nil {
				t.Fatal("immutable audit record update unexpectedly succeeded")
			}
			if err := appdb.NewTransactor(db).Within(ctx, nil, func(tx *sqlx.Tx) error {
				_, err := tx.ExecContext(ctx, db.Rebind(`DELETE FROM audit_records WHERE id=? AND occurred_at=?`), record.ID, record.OccurredAt)
				return err
			}); err == nil {
				t.Fatal("physical audit record delete unexpectedly succeeded")
			}
			items, total, err := repository.Query(ctx, auditdomain.Filter{TenantID: record.TenantID, ApplicationID: record.ApplicationID, ActorType: "user", ResourceType: "invoice", TraceID: "trace-1", SourceService: "billing-service", Page: 1, PageSize: 20})
			if err != nil || total != 1 || len(items) != 1 {
				t.Fatalf("query total=%d len=%d err=%v", total, len(items), err)
			}
			items, total, err = repository.Query(ctx, auditdomain.Filter{TenantID: record.TenantID, ApplicationID: record.ApplicationID, Keyword: "BILLING", IDs: []string{record.ID, uuid.NewString()}, OccurredFrom: now.Add(-time.Hour), OccurredTo: now.Add(time.Hour), Page: 1, PageSize: 20})
			if err != nil || total != 1 || len(items) != 1 || items[0].ID != record.ID {
				t.Fatalf("standard filter query total=%d items=%+v err=%v", total, items, err)
			}
			var userTables int
			if databaseType == "postgres" {
				if err := db.GetContext(ctx, &userTables, `SELECT count(*) FROM pg_tables WHERE schemaname = current_schema() AND tablename = 'users'`); err != nil {
					t.Fatal(err)
				}
				var timezone string
				if err := db.GetContext(ctx, &timezone, `SHOW TIMEZONE`); err != nil || timezone != "Asia/Shanghai" {
					t.Fatalf("timezone=%q err=%v", timezone, err)
				}
			} else if err := db.GetContext(ctx, &userTables, `SELECT count(*) FROM information_schema.tables WHERE table_schema = DATABASE() AND table_name = 'users'`); err != nil {
				t.Fatal(err)
			} else {
				var timezone string
				if err := db.GetContext(ctx, &timezone, `SELECT @@session.time_zone`); err != nil || timezone != "+08:00" {
					t.Fatalf("timezone=%q err=%v", timezone, err)
				}
			}
			if userTables != 0 {
				t.Fatal("generic template migration must not create a users table")
			}
			if err := db.Close(); err != nil {
				t.Fatal(err)
			}
			if err := migration.Run(migrationCfg, "down", 0); err != nil {
				t.Fatalf("migration down: %v", err)
			}
		})
	}
}

func startDatabase(t *testing.T, ctx context.Context, databaseType string) (string, string) {
	t.Helper()
	switch databaseType {
	case "postgres":
		container, err := postgres.Run(ctx, "postgres:17-alpine", postgres.WithDatabase("app"), postgres.WithUsername("app"), postgres.WithPassword("app"), postgres.BasicWaitStrategies(), postgres.WithSQLDriver("pgx"))
		if err != nil {
			t.Fatal(err)
		}
		testcontainers.CleanupContainer(t, container)
		dsn, err := container.ConnectionString(ctx, "sslmode=disable")
		if err != nil {
			t.Fatal(err)
		}
		return dsn, dsn
	case "mysql":
		configPath, err := filepath.Abs(filepath.Join("testdata", "mysql.cnf"))
		if err != nil {
			t.Fatal(err)
		}
		container, err := mysql.Run(ctx, "mysql:8.4", mysql.WithDatabase("app"), mysql.WithUsername("app"), mysql.WithPassword("app"), mysql.WithConfigFile(configPath))
		if err != nil {
			t.Fatal(err)
		}
		testcontainers.CleanupContainer(t, container)
		dsn, err := container.ConnectionString(ctx, "parseTime=true")
		if err != nil {
			t.Fatal(err)
		}
		migrationDSN, err := container.ConnectionString(ctx)
		if err != nil {
			t.Fatal(err)
		}
		return dsn, "mysql://" + migrationDSN
	default:
		t.Fatal(fmt.Errorf("unsupported database %q", databaseType))
		return "", ""
	}
}
