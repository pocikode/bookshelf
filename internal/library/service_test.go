package library

import (
	"bytes"
	"context"
	"database/sql"
	"errors"
	"os"
	"path/filepath"
	"testing"

	"pocikode/bookshelf/internal/database"
)

type libRepo struct{}

func (libRepo) InsertBook(context.Context, *database.Book) error { return nil }
func (libRepo) FindBook(context.Context, int64) (database.Book, error) {
	return database.Book{}, sql.ErrNoRows
}
func (libRepo) FindBookByHash(context.Context, string) (database.Book, error) {
	return database.Book{}, sql.ErrNoRows
}
func (libRepo) UpdateBook(context.Context, int64, string, string, string) error { return nil }
func (libRepo) DeleteBookTx(context.Context, int64) error                       { return nil }
func TestStageStreamsLimitAndHash(t *testing.T) {
	dir := t.TempDir()
	for _, name := range []string{"uploads", "books", "covers", "trash"} {
		if err := os.Mkdir(filepath.Join(dir, name), 0o750); err != nil {
			t.Fatal(err)
		}
	}
	svc := New(libRepo{}, dir, 8)
	if _, err := svc.Stage(0, "big.pdf", bytes.NewReader([]byte("%PDF-123456"))); !errors.Is(err, ErrTooLarge) {
		t.Fatalf("large upload error=%v", err)
	}
	svc.maxBytes = 64
	stage, err := svc.Stage(0, "book.pdf", bytes.NewReader([]byte("%PDF-test")))
	if err != nil {
		t.Fatal(err)
	}
	defer stage.Cleanup()
	if stage.Format != "pdf" || stage.Size != 9 || len(stage.Hash) != 64 {
		t.Fatalf("unexpected stage: %+v", stage)
	}
}
func TestStoragePathContainment(t *testing.T) {
	root := t.TempDir()
	svc := New(libRepo{}, root, 1)
	if _, err := svc.Path("../../etc/passwd"); err == nil {
		t.Fatal("traversal accepted")
	}
	if _, err := svc.Path("/etc/passwd"); err == nil {
		t.Fatal("absolute accepted")
	}
	if err := os.Symlink(t.TempDir(), filepath.Join(root, "books")); err != nil {
		t.Fatal(err)
	}
	if _, err := svc.Path("books/escaped.pdf"); err == nil {
		t.Fatal("symbolic-link escape accepted")
	}
}
