package database

import (
	"context"
	"fmt"
	"time"

	mysqlDriver "github.com/go-sql-driver/mysql"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/stdlib"
	"github.com/jmoiron/sqlx"
	"github.com/lihongjie0209/audit-service/internal/config"
)

func Open(ctx context.Context, cfg config.Database) (*sqlx.DB, error) {
	driver, err := driverName(cfg.Type)
	if err != nil {
		return nil, err
	}
	var db *sqlx.DB
	switch driver {
	case "pgx":
		connectionConfig, parseErr := pgx.ParseConfig(cfg.DSN)
		if parseErr != nil {
			return nil, fmt.Errorf("parse database dsn: %w", parseErr)
		}
		if cfg.Name != "" {
			connectionConfig.Database = cfg.Name
		}
		if cfg.Schema != "" {
			connectionConfig.RuntimeParams["search_path"] = cfg.Schema
		}
		connectionConfig.RuntimeParams["timezone"] = "Asia/Shanghai"
		db = sqlx.NewDb(stdlib.OpenDB(*connectionConfig), driver)
	case "mysql":
		connectionConfig, parseErr := mysqlDriver.ParseDSN(cfg.DSN)
		if parseErr != nil {
			return nil, fmt.Errorf("parse database dsn: %w", parseErr)
		}
		if cfg.Name != "" {
			connectionConfig.DBName = cfg.Name
		}
		location, locationErr := time.LoadLocation("Asia/Shanghai")
		if locationErr != nil {
			return nil, fmt.Errorf("load database timezone: %w", locationErr)
		}
		connectionConfig.ParseTime = true
		connectionConfig.Loc = location
		db, err = sqlx.Open(driver, connectionConfig.FormatDSN())
		if err != nil {
			return nil, fmt.Errorf("open database: %w", err)
		}
	default:
		db, err = sqlx.Open(driver, cfg.DSN)
		if err != nil {
			return nil, fmt.Errorf("open database: %w", err)
		}
	}
	db.SetMaxOpenConns(cfg.MaxOpenConns)
	db.SetMaxIdleConns(cfg.MaxIdleConns)
	db.SetConnMaxLifetime(cfg.ConnMaxLifetime)
	db.SetConnMaxIdleTime(cfg.ConnMaxIdleTime)
	pingCtx, cancel := context.WithTimeout(ctx, cfg.PingTimeout)
	defer cancel()
	if err := db.PingContext(pingCtx); err != nil {
		_ = db.Close()
		return nil, fmt.Errorf("ping database: %w", err)
	}
	return db, nil
}

func driverName(dbType string) (string, error) {
	switch dbType {
	case "mysql":
		return "mysql", nil
	case "postgres", "kingbase":
		return "pgx", nil
	default:
		return "", fmt.Errorf("unsupported database type %q", dbType)
	}
}
