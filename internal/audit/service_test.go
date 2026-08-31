package audit

import (
	"bytes"
	"context"
	"errors"
	"testing"
	"time"

	"github.com/DATA-DOG/go-sqlmock"
	"github.com/jmoiron/sqlx"
	"github.com/lihongjie0209/audit-service/internal/apperror"
	"github.com/lihongjie0209/audit-service/internal/database"
	platformprincipal "github.com/lihongjie0209/microservice-platform-go/principal"
)

type fakeRepository struct {
	record    Record
	createErr error
	getErrs   []error
}

func (f *fakeRepository) Create(_ context.Context, _ sqlx.ExtContext, value Record) error {
	if f.createErr != nil {
		return f.createErr
	}
	f.record = value
	return nil
}
func (f *fakeRepository) Get(_ context.Context, _, _ string) (Record, error) {
	if len(f.getErrs) > 0 {
		err := f.getErrs[0]
		f.getErrs = f.getErrs[1:]
		return f.record, err
	}
	return f.record, nil
}
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
	ctx := platformprincipal.WithContext(t.Context(), platformprincipal.Principal{ID: "admin-1", Type: platformprincipal.TypeUser, TenantID: "tenant-1"})
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
	ctx := platformprincipal.WithContext(t.Context(), platformprincipal.Principal{ID: "admin-1", Type: platformprincipal.TypeUser, TenantID: "tenant-1"})
	if _, err := service.Record(ctx, Record{TenantID: "tenant-1", Action: "updated", ResourceType: "user", ResourceID: "user-1", SourceService: "identity-service", BeforeSummary: []byte(`{"password":`)}); err == nil {
		t.Fatal("Record() accepted invalid JSON")
	}
}

func TestServiceRecordRejectsInvalidExplicitUUIDBeforeRepository(t *testing.T) {
	repository := &fakeRepository{}
	service := NewService(repository, &database.Transactor{})
	ctx := platformprincipal.SystemContext(t.Context(), "audit-event-consumer")
	_, err := service.Record(ctx, Record{ID: "audit-1", TenantID: "tenant-1", Action: "updated", ResourceType: "user", ResourceID: "user-1", SourceService: "identity-service"})
	var appErr *apperror.Error
	if !errors.As(err, &appErr) || appErr.Code != apperror.CodeInvalidArgument {
		t.Fatalf("Record() error = %v, want invalid argument", err)
	}
}

func TestServiceRecordTreatsConcurrentDuplicateEventAsSuccess(t *testing.T) {
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Close() })
	mock.ExpectBegin()
	mock.ExpectRollback()
	existing := Record{ID: "73b76e80-31f8-4b85-938d-57760ba54c91", TenantID: "tenant-1", Action: "user.updated", ResourceType: "user", ResourceID: "user-1", SourceService: "identity-service", Version: 1}
	repository := &fakeRepository{record: existing, createErr: ErrDuplicate, getErrs: []error{ErrNotFound, nil}}
	service := NewService(repository, database.NewTransactor(sqlx.NewDb(db, "sqlmock")))
	ctx := platformprincipal.SystemContext(t.Context(), "audit-event-consumer")
	replayed, err := service.Record(ctx, existing)
	if err != nil || replayed.ID != existing.ID {
		t.Fatalf("replayed=%+v err=%v", replayed, err)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatal(err)
	}
}

func TestExportRequiresActorAndNeutralizesSpreadsheetFormula(t *testing.T) {
	repository := &fakeRepository{record: Record{ID: "audit-1", TenantID: "tenant-1", ActorID: "user-1", Action: "=HYPERLINK(\"bad\")", ResourceType: "user", ResourceID: "user-1", SourceService: "identity-service", OccurredAt: time.Now(), CreatedAt: time.Now(), Version: 1}}
	service := NewService(repository, &database.Transactor{})
	if _, _, err := service.Export(t.Context(), Filter{TenantID: "tenant-1"}, 100); err == nil {
		t.Fatal("Export accepted missing principal")
	}
	ctx := platformprincipal.WithContext(t.Context(), platformprincipal.Principal{ID: "auditor-1", Type: platformprincipal.TypeUser, TenantID: "tenant-1"})
	content, count, err := service.Export(ctx, Filter{TenantID: "tenant-1"}, 100)
	if err != nil || count != 1 || !bytes.Contains(content, []byte(`'=HYPERLINK`)) {
		t.Fatalf("content=%s count=%d err=%v", content, count, err)
	}
}

func TestQueryRejectsTenantOutsideJWTContext(t *testing.T) {
	service := NewService(&fakeRepository{}, &database.Transactor{})
	ctx := platformprincipal.WithContext(t.Context(), platformprincipal.Principal{ID: "auditor-1", Type: platformprincipal.TypeUser, TenantID: "tenant-1"})
	_, err := service.Query(ctx, Filter{TenantID: "tenant-2"})
	var appErr *apperror.Error
	if !errors.As(err, &appErr) || appErr.Code != apperror.CodeForbidden {
		t.Fatalf("Query() error = %v, want forbidden", err)
	}
}
