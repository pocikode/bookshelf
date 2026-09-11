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
		return nil, fmt.Errorf("open sqlite at %s: %w", path, err)
	}
	db.SetMaxOpenConns(1)
	db.SetMaxIdleConns(1)
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	for _, pragma := range []string{"PRAGMA journal_mode = WAL", "PRAGMA foreign_keys = ON", "PRAGMA busy_timeout = 5000"} {
		if _, err := db.ExecContext(ctx, pragma); err != nil {
			db.Close()
			return nil, fmt.Errorf("sqlite setup (%s): %w", pragma, err)
		}
	}
	schema, err := migrationFS.ReadFile("schema.sql")
	if err != nil {
		db.Close()
		return nil, fmt.Errorf("read embedded schema: %w", err)
	}
	if _, err := db.ExecContext(ctx, string(schema)); err != nil {
		db.Close()
		return nil, fmt.Errorf("migration: %w", err)
	}
	if err := ensureUserRole(db); err != nil {
		db.Close()
		return nil, fmt.Errorf("user role migration: %w", err)
	}
	if err := ensureBookVisibility(db); err != nil {
		db.Close()
		return nil, fmt.Errorf("book visibility migration: %w", err)
	}
	return &DB{DB: db}, nil
}

func ensureUserRole(db *sql.DB) error {
	rows, err := db.Query(`PRAGMA table_info(users)`)
	if err != nil {
		return err
	}
	defer rows.Close()

	var hasRole bool
	for rows.Next() {
		var cid, notNull, primaryKey int
		var name, columnType string
		var defaultValue sql.NullString
		if err := rows.Scan(&cid, &name, &columnType, &notNull, &defaultValue, &primaryKey); err != nil {
			return err
		}
		if name == "role" {
			hasRole = true
		}
	}
	if err := rows.Err(); err != nil {
		return err
	}
	if !hasRole {
		if _, err := db.Exec(`ALTER TABLE users ADD COLUMN role TEXT NOT NULL DEFAULT 'user' CHECK(role IN ('admin', 'user'))`); err != nil {
			return err
		}
	}
	// Databases created before roles existed have one initial account. Preserve
	// its access by making that account the first administrator.
	if _, err := db.Exec(`UPDATE users SET role = 'admin' WHERE id = (SELECT id FROM users ORDER BY created_at, id LIMIT 1) AND NOT EXISTS (SELECT 1 FROM users WHERE role = 'admin')`); err != nil {
		return err
	}
	_, err = db.Exec(`INSERT OR IGNORE INTO schema_migrations (version, applied_at) VALUES (2, unixepoch() * 1000)`)
	return err
}

func ensureBookVisibility(db *sql.DB) error {
	rows, err := db.Query(`PRAGMA table_info(books)`)
	if err != nil {
		return err
	}
	defer rows.Close()

	var hasVisibility bool
	for rows.Next() {
		var cid, notNull, primaryKey int
		var name, columnType string
		var defaultValue sql.NullString
		if err := rows.Scan(&cid, &name, &columnType, &notNull, &defaultValue, &primaryKey); err != nil {
			return err
		}
		if name == "visibility" {
			hasVisibility = true
		}
	}
	if err := rows.Err(); err != nil {
		return err
	}
	if !hasVisibility {
		if _, err := db.Exec(`ALTER TABLE books ADD COLUMN visibility TEXT NOT NULL DEFAULT 'public' CHECK(visibility IN ('public', 'private'))`); err != nil {
			return err
		}
	}
	_, err = db.Exec(`INSERT OR IGNORE INTO schema_migrations (version, applied_at) VALUES (3, unixepoch() * 1000)`)
	return err
}
