package database

import (
	"context"
	"database/sql"
	"embed"
	"fmt"
	"time"

	_ "modernc.org/sqlite"
)

// The SQL file is mirrored at server/migrations/001_init.sql for operators.
// Keeping the embedded copy here makes the binary self-contained.
//
//go:embed schema.sql
var migrationFS embed.FS

type DB struct{ *sql.DB }

func Open(path string) (*DB, error) {
	db, err := sql.Open("sqlite", path)
	if err != nil {
		return nil, err
	}
	db.SetMaxOpenConns(1)
	db.SetMaxIdleConns(1)
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	for _, pragma := range []string{"PRAGMA journal_mode = WAL", "PRAGMA foreign_keys = ON", "PRAGMA busy_timeout = 5000"} {
		if _, err := db.ExecContext(ctx, pragma); err != nil {
			db.Close()
			return nil, fmt.Errorf("sqlite setup: %w", err)
		}
	}
	schema, err := migrationFS.ReadFile("schema.sql")
	if err != nil {
		db.Close()
		return nil, err
	}
	if _, err := db.ExecContext(ctx, string(schema)); err != nil {
		db.Close()
		return nil, fmt.Errorf("migration: %w", err)
	}
	return &DB{DB: db}, nil
}
