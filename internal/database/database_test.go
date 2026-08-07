package database

import (
	"context"
	"path/filepath"
	"testing"
	"time"
)

func TestOpenMigratePragmasAndRepositories(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	db, err := Open(context.Background(), dir)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	if err := Migrate(context.Background(), db); err != nil {
		t.Fatalf("second migration: %v", err)
	}
	var foreign, busy int
	var journal string
	if err := db.QueryRow(`PRAGMA foreign_keys`).Scan(&foreign); err != nil {
		t.Fatal(err)
	}
	if err := db.QueryRow(`PRAGMA busy_timeout`).Scan(&busy); err != nil {
		t.Fatal(err)
	}
	if err := db.QueryRow(`PRAGMA journal_mode`).Scan(&journal); err != nil {
		t.Fatal(err)
	}
	if foreign != 1 || busy < 5000 || journal != "wal" {
		t.Fatalf("pragmas: %d %d %q", foreign, busy, journal)
	}

	repo := NewRepository(db)
	now := time.Now().UTC()
	books := []Book{
		{Title: "100% Real", Category: "Essays", Format: "epub", FileHash: "a", FilePath: filepath.ToSlash("books/a.epub"), FileSize: 1, CreatedAt: now},
		{Title: "1000 Real", Category: "Essays", Format: "epub", FileHash: "b", FilePath: filepath.ToSlash("books/b.epub"), FileSize: 1, CreatedAt: now},
		{Title: "Under_score", Category: "Fiction", Format: "pdf", FileHash: "c", FilePath: filepath.ToSlash("books/c.pdf"), FileSize: 1, PageCount: 10, CreatedAt: now},
	}
	for i := range books {
		if err := repo.InsertBook(context.Background(), &books[i]); err != nil {
			t.Fatal(err)
		}
	}
	found, total, err := repo.ListBooks(context.Background(), BookListOptions{Query: "%", Page: 1, Limit: 60, Sort: "title", Direction: "asc"})
	if err != nil {
		t.Fatal(err)
	}
	if total != 1 || found[0].Title != "100% Real" {
		t.Fatalf("literal %% search: total=%d books=%v", total, found)
	}
	found, total, err = repo.ListBooks(context.Background(), BookListOptions{Query: "_", Page: 1, Limit: 60})
	if err != nil {
		t.Fatal(err)
	}
	if total != 1 || found[0].Title != "Under_score" {
		t.Fatalf("literal _ search: total=%d books=%v", total, found)
	}
	p := Progress{BookID: books[2].ID, Position: "5", Page: 5, Percent: .5, DeviceLabel: "Safari on iOS"}
	if err = repo.UpsertProgress(context.Background(), &p, false); err != nil {
		t.Fatal(err)
	}
	if err = repo.DeleteBookTx(context.Background(), books[2].ID); err != nil {
		t.Fatal(err)
	}
	if _, err = repo.GetProgress(context.Background(), books[2].ID); !IsNotFound(err) {
		t.Fatalf("progress did not cascade: %v", err)
	}
}
