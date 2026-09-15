package database

import (
	"errors"
	"regexp"
	"testing"

	"github.com/DATA-DOG/go-sqlmock"
	"github.com/jmoiron/sqlx"
	"github.com/lihongjie0209/microservice-platform-go/principal"
)

func TestTransactorInjectsAuditActorOnTransactionConnection(t *testing.T) {
	t.Parallel()
	database, mock, err := sqlmock.New()
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = database.Close() })
	db := sqlx.NewDb(database, "postgres")
	mock.ExpectBegin()
	mock.ExpectExec(regexp.QuoteMeta("SELECT set_config('app.actor_id', $1, true)")).WithArgs("actor-1").WillReturnResult(sqlmock.NewResult(0, 1))
	mock.ExpectCommit()
	ctx := principal.WithContext(t.Context(), principal.Principal{ID: "actor-1", Type: principal.TypeUser})
	if err := NewTransactor(db).Within(ctx, nil, func(*sqlx.Tx) error { return nil }); err != nil {
		t.Fatal(err)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatal(err)
	}
}

func TestTransactorRejectsMissingAuditActor(t *testing.T) {
	t.Parallel()
	database, mock, err := sqlmock.New()
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = database.Close() })
	db := sqlx.NewDb(database, "postgres")
	mock.ExpectBegin()
	mock.ExpectRollback()
	err = NewTransactor(db).Within(t.Context(), nil, func(*sqlx.Tx) error { return nil })
	if !errors.Is(err, ErrMissingAuditActor) {
		t.Fatalf("error = %v", err)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatal(err)
	}
}
