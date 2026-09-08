package database

import (
	"path/filepath"
	"testing"
)

func TestOpenAppliesSQLiteSettingsAndSchema(t *testing.T) {
	db, err := Open(filepath.Join(t.TempDir(), "app.sqlite"))
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
}
