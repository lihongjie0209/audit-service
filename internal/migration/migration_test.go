package migration

import (
	"database/sql"
	"net/url"
	"testing"

	"github.com/DATA-DOG/go-sqlmock"
)

func TestWithMigrationTable(t *testing.T) {
	t.Parallel()
	result, err := withMigrationOptions("postgres://user:pass@db/app?sslmode=disable", "orders_db", "orders_schema_migrations", "orders")
	if err != nil {
		t.Fatal(err)
	}
	parsed, err := url.Parse(result)
	if err != nil {
		t.Fatal(err)
	}
	if got := parsed.Query().Get("x-migrations-table"); got != "orders_schema_migrations" {
		t.Fatalf("x-migrations-table = %q", got)
	}
	if got := parsed.Query().Get("sslmode"); got != "disable" {
		t.Fatalf("sslmode = %q", got)
	}
	if got := parsed.Query().Get("search_path"); got != "orders" {
		t.Fatalf("search_path = %q", got)
	}
	if parsed.Path != "/orders_db" {
		t.Fatalf("database path = %q", parsed.Path)
	}
}

func TestCreateSchemaSerializesConcurrentBootstrap(t *testing.T) {
	t.Parallel()
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = db.Close() }()
	mock.ExpectBegin()
	mock.ExpectExec(`SELECT pg_advisory_xact_lock\(hashtext\(\$1\)\)`).WithArgs("audit-service:schema:orders").WillReturnResult(sqlmock.NewResult(0, 1))
	mock.ExpectExec(`CREATE SCHEMA IF NOT EXISTS "orders"`).WillReturnResult(sqlmock.NewResult(0, 1))
	mock.ExpectCommit()
	if err := createSchema((*sql.DB)(db), "orders"); err != nil {
		t.Fatal(err)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatal(err)
	}
}

func TestWithMigrationTableMySQLDSN(t *testing.T) {
	t.Parallel()
	result, err := withMigrationOptions("mysql://app:app@tcp(mysql:3306)/app", "orders_db", "orders_schema_migrations", "ignored")
	if err != nil {
		t.Fatal(err)
	}
	const expected = "mysql://app:app@tcp(mysql:3306)/orders_db?x-migrations-table=orders_schema_migrations"
	if result != expected {
		t.Fatalf("result = %q, want %q", result, expected)
	}
}
