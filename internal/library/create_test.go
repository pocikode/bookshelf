package library

import (
	"bytes"
	"context"
	"database/sql"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"pocikode/bookshelf/internal/database"
)

type createRepo struct {
	libRepo
	duplicate database.Book
	findErr   error
	insertErr error
	inserted  database.Book
}

func (r *createRepo) FindBookByHash(context.Context, string) (database.Book, error) {
	if r.findErr != nil {
		return database.Book{}, r.findErr
	}
	if r.duplicate.ID != 0 {
		return r.duplicate, nil
	}
	return database.Book{}, sql.ErrNoRows
}

func (r *createRepo) InsertBook(_ context.Context, book *database.Book) error {
	r.inserted = *book
	return r.insertErr
}

func TestCreateForUserFromEPUB(t *testing.T) {
	root := t.TempDir()
	if err := os.Mkdir(filepath.Join(root, "uploads"), 0o750); err != nil {
		t.Fatal(err)
	}
	source := writeEPUB(t, validEPUBFiles(pngFixture(t)))
	r := &createRepo{}
	svc := New(r, root, 1)
	stage := StagedBook{Filename: "uploaded.epub", Path: source, Hash: strings.Repeat("a", 64), Format: "epub", Size: 123}
	svc.now = func() time.Time { return time.Date(2026, 2, 3, 4, 5, 6, 0, time.FixedZone("test", 3600)) }
	book, err := svc.CreateForUser(context.Background(), 7, false, stage, "", " Fiction ")
	if err != nil {
		t.Fatal(err)
	}
	if book.OwnerID != 7 || book.Public || book.Title != "EPUB Title" || book.Category != "Fiction" || book.CoverPath == "" || book.FilePath == "" || !book.CreatedAt.Equal(svc.now().UTC()) {
		t.Fatalf("book = %+v", book)
	}
	if r.inserted.FilePath != book.FilePath {
		t.Fatalf("inserted book = %+v", r.inserted)
	}
	if _, err := os.Stat(filepath.Join(root, filepath.FromSlash(book.FilePath))); err != nil {
		t.Fatalf("installed book missing: %v", err)
	}
	if _, err := os.Stat(filepath.Join(root, filepath.FromSlash(book.CoverPath))); err != nil {
		t.Fatalf("installed cover missing: %v", err)
	}
	if _, err := os.Stat(source); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("staged source remains: %v", err)
	}
}

func TestCreateForUserDuplicateAndLookupErrors(t *testing.T) {
	root := t.TempDir()
	r := &createRepo{duplicate: database.Book{ID: 4, Title: "Existing"}}
	stage := StagedBook{Filename: "book.epub", Path: filepath.Join(root, "stage"), Hash: strings.Repeat("b", 64), Format: "epub"}
	if err := os.WriteFile(stage.Path, []byte("stage"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := New(r, root, 1).CreateForUser(context.Background(), 1, true, stage, "", ""); err == nil {
		t.Fatal("duplicate was accepted")
	} else {
		var duplicate *DuplicateError
		if !errors.As(err, &duplicate) || duplicate.Book.ID != 4 {
			t.Fatalf("duplicate error = %v", err)
		}
	}
	if _, err := os.Stat(stage.Path); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("duplicate stage remains: %v", err)
	}
	r = &createRepo{findErr: errors.New("repository down")}
	stage.Path = filepath.Join(root, "stage2")
	if err := os.WriteFile(stage.Path, []byte("stage"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := New(r, root, 1).CreateForUser(context.Background(), 1, true, stage, "", ""); !errors.Is(err, r.findErr) {
		t.Fatalf("lookup error = %v", err)
	}
}

func TestCreateForUserInvalidPDFAndExistingBook(t *testing.T) {
	root := t.TempDir()
	if err := os.Mkdir(filepath.Join(root, "uploads"), 0o750); err != nil {
		t.Fatal(err)
	}
	r := &createRepo{}
	bad := filepath.Join(root, "bad.pdf")
	if err := os.WriteFile(bad, []byte("not pdf"), 0o600); err != nil {
		t.Fatal(err)
	}
	stage := StagedBook{Filename: "bad.pdf", Path: bad, Hash: strings.Repeat("c", 64), Format: "pdf"}
	if _, err := New(r, root, 1).CreateForUser(context.Background(), 1, true, stage, "", ""); !errors.Is(err, ErrInvalidFormat) {
		t.Fatalf("invalid PDF error = %v", err)
	}

	source := filepath.Join(root, "source.epub")
	if err := os.WriteFile(source, []byte("source"), 0o600); err != nil {
		t.Fatal(err)
	}
	hash := strings.Repeat("d", 64)
	dst := filepath.Join(root, "books", hash[:2], hash+".epub")
	if err := os.MkdirAll(filepath.Dir(dst), 0o750); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(dst, []byte("existing"), 0o600); err != nil {
		t.Fatal(err)
	}
	// A malformed EPUB is tolerated during metadata extraction, then rejected by validation.
	stage = StagedBook{Filename: "fallback.epub", Path: source, Hash: hash, Format: "epub", Size: 6}
	if _, err := New(r, root, 1).CreateForUser(context.Background(), 1, true, stage, "", ""); err != nil {
		t.Fatalf("existing book install should not fail: %v", err)
	}
	if got, err := os.ReadFile(dst); err != nil || !bytes.Equal(got, []byte("existing")) {
		t.Fatalf("existing destination changed: %q, %v", got, err)
	}
}
