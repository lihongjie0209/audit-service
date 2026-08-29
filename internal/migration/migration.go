package migration

import (
	"database/sql"
	"errors"
	"fmt"
	"net/url"
	"path/filepath"
	"regexp"
	"strings"

	"github.com/golang-migrate/migrate/v4"
	_ "github.com/golang-migrate/migrate/v4/database/mysql"
	_ "github.com/golang-migrate/migrate/v4/database/postgres"
	_ "github.com/golang-migrate/migrate/v4/source/file"
	"github.com/lihongjie0209/audit-service/internal/config"
)

func Run(cfg config.Migration, direction string, steps int) (runErr error) {
	if cfg.DatabaseURL == "" {
		return errors.New("migration.database_url is required")
	}
	absPath, err := filepath.Abs(cfg.Path)
	if err != nil {
		return fmt.Errorf("resolve migration path: %w", err)
	}
	source := (&url.URL{Scheme: "file", Path: filepath.ToSlash(absPath)}).String()
	databaseURL, err := withMigrationOptions(cfg.DatabaseURL, cfg.DatabaseName, cfg.Table, cfg.Schema)
	if err != nil {
		return err
	}
	if err := ensureSchema(cfg, databaseURL); err != nil {
		return err
	}
	m, err := migrate.New(source, databaseURL)
	if err != nil {
		return fmt.Errorf("create migrator: %w", err)
	}
	defer func() {
		sourceErr, databaseErr := m.Close()
		if sourceErr != nil {
			runErr = errors.Join(runErr, fmt.Errorf("close migration source: %w", sourceErr))
		}
		if databaseErr != nil {
			runErr = errors.Join(runErr, fmt.Errorf("close migration database: %w", databaseErr))
		}
	}()
	if steps != 0 {
		err = m.Steps(steps)
	} else if direction == "down" {
		err = m.Down()
	} else {
		err = m.Up()
	}
	if err != nil && !errors.Is(err, migrate.ErrNoChange) {
		return fmt.Errorf("run migration: %w", err)
	}
	return runErr
}

func withMigrationOptions(databaseURL, databaseName, table, schema string) (string, error) {
	if databaseName == "" && table == "" && schema == "" {
		return databaseURL, nil
	}
	if strings.HasPrefix(databaseURL, "mysql://") {
		if databaseName != "" {
			queryIndex := strings.IndexByte(databaseURL, '?')
			pathEnd := len(databaseURL)
			if queryIndex >= 0 {
				pathEnd = queryIndex
			}
			slash := strings.LastIndex(databaseURL[:pathEnd], "/")
			if slash < 0 {
				return "", errors.New("mysql migration URL is missing database path")
			}
			databaseURL = databaseURL[:slash+1] + databaseName + databaseURL[pathEnd:]
		}
		if table == "" {
			return databaseURL, nil
		}
		separator := "?"
		if strings.Contains(databaseURL, "?") {
			separator = "&"
		}
		return databaseURL + separator + "x-migrations-table=" + url.QueryEscape(table), nil
	}
	parsed, err := url.Parse(databaseURL)
	if err != nil {
		return "", fmt.Errorf("parse migration database URL: %w", err)
	}
	query := parsed.Query()
	if databaseName != "" {
		parsed.Path = "/" + databaseName
	}
	if table != "" {
		query.Set("x-migrations-table", table)
	}
	if schema != "" {
		query.Set("search_path", schema)
	}
	parsed.RawQuery = query.Encode()
	return parsed.String(), nil
}

func ensureSchema(cfg config.Migration, databaseURL string) error {
	if !cfg.CreateSchema || cfg.Schema == "" || strings.HasPrefix(cfg.DatabaseURL, "mysql://") {
		return nil
	}
	if !regexp.MustCompile(`^[a-z][a-z0-9_]{0,62}$`).MatchString(cfg.Schema) {
		return errors.New("invalid migration schema")
	}
	parsed, err := url.Parse(databaseURL)
	if err != nil {
		return fmt.Errorf("parse database URL for schema creation: %w", err)
	}
	query := parsed.Query()
	query.Del("x-migrations-table")
	parsed.RawQuery = query.Encode()
	db, err := sql.Open("postgres", parsed.String())
	if err != nil {
		return fmt.Errorf("open database to create schema: %w", err)
	}
	defer func() { _ = db.Close() }()
	if err := createSchema(db, cfg.Schema); err != nil {
		return fmt.Errorf("create migration schema %q: %w", cfg.Schema, err)
	}
	return nil
}

func createSchema(db *sql.DB, schema string) (err error) {
	tx, err := db.Begin()
	if err != nil {
		return fmt.Errorf("begin schema creation: %w", err)
	}
	defer func() {
		if err != nil {
			_ = tx.Rollback()
		}
	}()
	if _, err = tx.Exec(`SELECT pg_advisory_xact_lock(hashtext($1))`, "audit-service:schema:"+schema); err != nil {
		return fmt.Errorf("lock schema creation: %w", err)
	}
	if _, err = tx.Exec(`CREATE SCHEMA IF NOT EXISTS "` + schema + `"`); err != nil {
		return fmt.Errorf("execute schema creation: %w", err)
	}
	if err = tx.Commit(); err != nil {
		return fmt.Errorf("commit schema creation: %w", err)
	}
	return nil
}
