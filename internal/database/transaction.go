package database

import (
	"context"
	"database/sql"
	"fmt"

	"github.com/jmoiron/sqlx"
)

type Transactor struct{ db *sqlx.DB }

func NewTransactor(db *sqlx.DB) *Transactor { return &Transactor{db: db} }

func (t *Transactor) Available() bool { return t.db != nil }

func (t *Transactor) Within(ctx context.Context, opts *sql.TxOptions, fn func(*sqlx.Tx) error) error {
	if t.db == nil {
		return fmt.Errorf("begin transaction: database is disabled")
	}
	tx, err := t.db.BeginTxx(ctx, opts)
	if err != nil {
		return fmt.Errorf("begin transaction: %w", err)
	}
	if err := fn(tx); err != nil {
		if rollbackErr := tx.Rollback(); rollbackErr != nil {
			return fmt.Errorf("rollback transaction after %v: %w", err, rollbackErr)
		}
		return err
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("commit transaction: %w", err)
	}
	return nil
}
