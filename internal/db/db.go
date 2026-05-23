package db

import (
	"database/sql"
	"fmt"
	"log/slog"

	"github.com/pressly/goose/v3"
	_ "modernc.org/sqlite"

	"github.com/TheOutdoorProgrammer/crate/internal/migrations"
)

func Open(path string) (*sql.DB, error) {
	db, err := sql.Open("sqlite", path+"?_pragma=journal_mode(wal)&_pragma=foreign_keys(on)")
	if err != nil {
		return nil, fmt.Errorf("open database: %w", err)
	}

	db.SetMaxOpenConns(1)

	if err := db.Ping(); err != nil {
		db.Close()
		return nil, fmt.Errorf("ping database: %w", err)
	}

	if err := runMigrations(db); err != nil {
		db.Close()
		return nil, fmt.Errorf("run migrations: %w", err)
	}

	slog.Info("database ready", "path", path)
	return db, nil
}

func runMigrations(db *sql.DB) error {
	goose.SetBaseFS(migrations.FS)
	goose.SetDialect("sqlite3")
	goose.SetLogger(goose.NopLogger())

	return goose.Up(db, ".")
}
