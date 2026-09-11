package database

import (
	"database/sql"
	"path/filepath"
	"testing"
)

func TestOpenAppliesSQLiteSettingsAndSchema(t *testing.T) {
	db, err := Open(filepath.Join(t.TempDir(), "bookshelf.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()

	var journal string
	var foreignKeys, busyTimeout, migration int
	if err := db.QueryRow(`PRAGMA journal_mode`).Scan(&journal); err != nil {
		t.Fatal(err)
	}
	if journal != "wal" && journal != "WAL" {
		t.Fatal("journal mode was not configured")
	}
	if err := db.QueryRow(`PRAGMA foreign_keys`).Scan(&foreignKeys); err != nil {
		t.Fatal(err)
	}
	if foreignKeys != 1 {
		t.Fatalf("foreign keys = %d", foreignKeys)
	}
	if err := db.QueryRow(`PRAGMA busy_timeout`).Scan(&busyTimeout); err != nil {
		t.Fatal(err)
	}
	if busyTimeout != 5000 {
		t.Fatalf("busy timeout = %d", busyTimeout)
	}
	if err := db.QueryRow(`SELECT version FROM schema_migrations`).Scan(&migration); err != nil {
		t.Fatal(err)
	}
	if migration != 1 {
		t.Fatalf("migration version = %d", migration)
	}
	var visibility string
	if err := db.QueryRow(`SELECT visibility FROM books LIMIT 1`).Scan(&visibility); err != nil && err != sql.ErrNoRows {
		t.Fatal(err)
	}
	if _, err := db.Exec(`INSERT INTO users (id, username, password_hash, created_at, updated_at) VALUES ('owner', 'owner', 'hash', 1, 1)`); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`INSERT INTO books (id, user_id, title, original_name, file_path, file_hash, mime_type, file_size, created_at, updated_at) VALUES ('book', 'owner', 'Book', 'book.epub', '/book', 'hash', 'application/epub+zip', 1, 1, 1)`); err != nil {
		t.Fatal(err)
	}
	if err := db.QueryRow(`SELECT visibility FROM books WHERE id = 'book'`).Scan(&visibility); err != nil {
		t.Fatal(err)
	}
	if visibility != "public" {
		t.Fatalf("visibility default = %q", visibility)
	}
}
