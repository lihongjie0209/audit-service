package audit

import (
	"bytes"
	"context"
	"testing"
	"time"

	"github.com/DATA-DOG/go-sqlmock"
	"github.com/jmoiron/sqlx"
	"github.com/lihongjie0209/audit-service/internal/database"
	"github.com/lihongjie0209/audit-service/internal/principal"
)

type fakeRepository struct{ record Record }

func (f *fakeRepository) Create(_ context.Context, _ sqlx.ExtContext, value Record) error {
	f.record = value
	return nil
}
func (f *fakeRepository) Get(_ context.Context, _, _ string) (Record, error) { return f.record, nil }
func (f *fakeRepository) Query(_ context.Context, _ Filter) ([]Record, int64, error) {
	return []Record{f.record}, 1, nil
}

func TestServiceRecordInjectsAuditAndRedactsSecrets(t *testing.T) {
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Close() })
	mock.ExpectBegin()
	mock.ExpectCommit()
	repository := &fakeRepository{}
	service := NewService(repository, database.NewTransactor(sqlx.NewDb(db, "sqlmock")))
	now := time.Date(2026, 8, 30, 8, 0, 0, 0, time.FixedZone("UTC+8", 8*60*60))
	service.now = func() time.Time { return now }
	ctx := principal.WithContext(t.Context(), principal.Principal{Subject: "admin-1", Method: principal.AuthenticationJWT})
	created, err := service.Record(ctx, Record{TenantID: "tenant-1", Action: "user.updated", ResourceType: "user", ResourceID: "user-1", SourceService: "identity-service", BeforeSummary: []byte(`{"password":"old"}`), AfterSummary: []byte(`{"password":"new","name":"Alice"}`)})
	if err != nil {
		t.Fatal(err)
	}
	if created.Version != 1 || created.CreatedBy != "admin-1" || created.ActorID != "admin-1" || created.OccurredAt != now {
		t.Fatalf("created=%+v", created)
	}
	if bytes.Contains(created.BeforeSummary, []byte("old")) || bytes.Contains(created.AfterSummary, []byte("new")) {
		t.Fatalf("secrets were persisted: before=%s after=%s", created.BeforeSummary, created.AfterSummary)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatal(err)
	}
}

func TestServiceRecordRejectsInvalidJSON(t *testing.T) {
	service := NewService(&fakeRepository{}, &database.Transactor{})
	ctx := principal.WithContext(t.Context(), principal.Principal{Subject: "admin-1", Method: principal.AuthenticationJWT})
	if _, err := service.Record(ctx, Record{TenantID: "tenant-1", Action: "updated", ResourceType: "user", ResourceID: "user-1", SourceService: "identity-service", BeforeSummary: []byte(`{"password":`)}); err == nil {
		t.Fatal("Record() accepted invalid JSON")
	}
}

func TestExportRequiresActorAndNeutralizesSpreadsheetFormula(t *testing.T) {
	repository := &fakeRepository{record: Record{ID: "audit-1", TenantID: "tenant-1", ActorID: "user-1", Action: "=HYPERLINK(\"bad\")", ResourceType: "user", ResourceID: "user-1", SourceService: "identity-service", OccurredAt: time.Now(), CreatedAt: time.Now(), Version: 1}}
	service := NewService(repository, &database.Transactor{})
	if _, _, err := service.Export(t.Context(), Filter{TenantID: "tenant-1"}, 100); err == nil {
		t.Fatal("Export accepted missing principal")
	}
	ctx := principal.WithContext(t.Context(), principal.Principal{Subject: "auditor-1"})
	content, count, err := service.Export(ctx, Filter{TenantID: "tenant-1"}, 100)
	if err != nil || count != 1 || !bytes.Contains(content, []byte(`'=HYPERLINK`)) {
		t.Fatalf("content=%s count=%d err=%v", content, count, err)
	}
}
