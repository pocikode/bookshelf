package library

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"testing"

	"pocikode/bookshelf/internal/database"
)

type deleteRepo struct {
	libRepo
	book      database.Book
	findErr   error
	deleteErr error
	deleted   bool
}

func (r *deleteRepo) FindBook(context.Context, int64) (database.Book, error) {
	return r.book, r.findErr
}

func (r *deleteRepo) DeleteBookTx(context.Context, int64) error {
	r.deleted = true
	return r.deleteErr
}

func TestDeleteMovesFilesAndRemovesTrash(t *testing.T) {
	root := t.TempDir()
	if err := os.Mkdir(filepath.Join(root, "trash"), 0o750); err != nil {
		t.Fatal(err)
	}
	for _, rel := range []string{"books/book.epub", "covers/cover.png"} {
		if err := os.MkdirAll(filepath.Dir(filepath.Join(root, rel)), 0o750); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(root, rel), []byte(rel), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	r := &deleteRepo{book: database.Book{FileHash: "hash", FilePath: "books/book.epub", CoverPath: "covers/cover.png"}}
	if err := New(r, root, 1).Delete(context.Background(), 9); err != nil {
		t.Fatal(err)
	}
	if !r.deleted {
		t.Fatal("repository delete was not called")
	}
	for _, rel := range []string{"books/book.epub", "covers/cover.png"} {
		if _, err := os.Stat(filepath.Join(root, rel)); !errors.Is(err, os.ErrNotExist) {
			t.Fatalf("%s still exists: %v", rel, err)
		}
	}
	entries, err := os.ReadDir(filepath.Join(root, "trash"))
	if err != nil || len(entries) != 0 {
		t.Fatalf("trash entries = %v, %v", entries, err)
	}
}

func TestDeleteRollsBackWhenRepositoryDeleteFails(t *testing.T) {
	root := t.TempDir()
	if err := os.Mkdir(filepath.Join(root, "trash"), 0o750); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(root, "books"), 0o750); err != nil {
		t.Fatal(err)
	}
	original := filepath.Join(root, "books/book.epub")
	if err := os.WriteFile(original, []byte("book"), 0o600); err != nil {
		t.Fatal(err)
	}
	r := &deleteRepo{book: database.Book{FileHash: "hash", FilePath: "books/book.epub"}, deleteErr: errors.New("database unavailable")}
	if err := New(r, root, 1).Delete(context.Background(), 1); !errors.Is(err, r.deleteErr) {
		t.Fatalf("Delete() error = %v", err)
	}
	if got, err := os.ReadFile(original); err != nil || string(got) != "book" {
		t.Fatalf("restored file = %q, %v", got, err)
	}
}

func TestDeleteErrorsAndRollbackMissingFile(t *testing.T) {
	root := t.TempDir()
	r := &deleteRepo{findErr: sql.ErrNoRows}
	if err := New(r, root, 1).Delete(context.Background(), 1); !errors.Is(err, sql.ErrNoRows) {
		t.Fatalf("find error = %v", err)
	}
	r.findErr = nil
	r.book = database.Book{FilePath: "../outside"}
	if err := New(r, root, 1).Delete(context.Background(), 1); err == nil {
		t.Fatal("unsafe path accepted")
	}
	r.book = database.Book{FilePath: "books/missing.epub"}
	if err := New(r, root, 1).Delete(context.Background(), 1); err == nil {
		t.Fatal("missing file delete succeeded")
	}
}

func TestReconcileTrash(t *testing.T) {
	root := t.TempDir()
	trash := filepath.Join(root, "trash")
	if err := os.MkdirAll(trash, 0o750); err != nil {
		t.Fatal(err)
	}
	makeDir := func(name string, value any) string {
		dir := filepath.Join(trash, name)
		if err := os.Mkdir(dir, 0o750); err != nil {
			t.Fatal(err)
		}
		raw, _ := json.Marshal(value)
		if err := os.WriteFile(filepath.Join(dir, "manifest.json"), raw, 0o600); err != nil {
			t.Fatal(err)
		}
		return dir
	}
	makeDir("restore", trashManifest{BookID: 1, FileHash: "same", Files: []trashFile{{Original: "books/restored.epub", Trashed: "book.epub"}}})
	if err := os.WriteFile(filepath.Join(trash, "restore", "book.epub"), []byte("book"), 0o600); err != nil {
		t.Fatal(err)
	}
	makeDir("remove", trashManifest{BookID: 2, FileHash: "gone"})
	makeDir("keep", trashManifest{BookID: 3, FileHash: "different"})
	makeDir("bad", "not a manifest")
	if err := os.WriteFile(filepath.Join(trash, "plain.txt"), []byte("ignored"), 0o600); err != nil {
		t.Fatal(err)
	}
	// FindBook returns the same book for the restore case; use a repo that varies by ID.
	r2 := &reconcileRepo{}
	if err := New(r2, root, 1).ReconcileTrash(context.Background()); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(root, "books/restored.epub")); err != nil {
		t.Fatalf("restored file missing: %v", err)
	}
	if _, err := os.Stat(filepath.Join(trash, "remove")); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("orphan trash remains: %v", err)
	}
	if _, err := os.Stat(filepath.Join(trash, "keep")); err != nil {
		t.Fatalf("unmatched trash removed: %v", err)
	}
}

type reconcileRepo struct{ libRepo }

func (*reconcileRepo) FindBook(_ context.Context, id int64) (database.Book, error) {
	switch id {
	case 1:
		return database.Book{FileHash: "same"}, nil
	case 2:
		return database.Book{}, sql.ErrNoRows
	default:
		return database.Book{FileHash: "other"}, nil
	}
}

func TestReconcileTrashMissingRootAndHelpers(t *testing.T) {
	root := filepath.Join(t.TempDir(), "missing")
	if err := New(&deleteRepo{}, root, 1).ReconcileTrash(context.Background()); err == nil {
		t.Fatal("missing trash root succeeded")
	}
	if !databaseNotFound(sql.ErrNoRows) || databaseNotFound(errors.New("other")) {
		t.Fatal("databaseNotFound classification is incorrect")
	}
	if err := os.MkdirAll(filepath.Join(root, "trash"), 0o750); err != nil {
		t.Fatal(err)
	}
	svc := New(libRepo{}, root, 1)
	if err := svc.restoreTrash(filepath.Join(root, "trash", "none"), trashManifest{Files: []trashFile{{Original: "../bad", Trashed: "x"}}}); err == nil {
		t.Fatal("restore accepted unsafe path")
	}
}

func TestDeleteWithoutCover(t *testing.T) {
	root := t.TempDir()
	if err := os.Mkdir(filepath.Join(root, "trash"), 0o750); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(root, "books"), 0o750); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "books/book.epub"), []byte("x"), 0o600); err != nil {
		t.Fatal(err)
	}
	r := &deleteRepo{book: database.Book{FilePath: "books/book.epub"}}
	if err := New(r, root, 1).Delete(context.Background(), 1); err != nil {
		t.Fatal(err)
	}
}
