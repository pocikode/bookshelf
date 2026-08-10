package library

import (
	"bytes"
	"context"
	"database/sql"
	"errors"
	"image"
	"image/color"
	"image/png"
	"os"
	"path/filepath"
	"testing"
	"time"

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

func TestDisplayFilename(t *testing.T) {
	if got := displayFilename(`C:\books\nested\title.epub`); got != "title.epub" {
		t.Fatalf("displayFilename() = %q", got)
	}
	long := string(bytes.Repeat([]byte("a"), 300))
	if got := displayFilename(long); len([]rune(got)) != 255 {
		t.Fatalf("displayFilename() rune length = %d", len([]rune(got)))
	}
}

func TestFallbackTitle(t *testing.T) {
	tests := map[string]string{
		"A_Well-Named Book.epub": "A Well Named Book",
		".pdf":                   "Untitled",
		"/tmp/untitled.pdf":      "untitled",
	}
	for input, want := range tests {
		if got := fallbackTitle(input); got != want {
			t.Errorf("fallbackTitle(%q) = %q, want %q", input, got, want)
		}
	}
}

func TestValidateAndMergeMetadata(t *testing.T) {
	if err := validateMetadata(Metadata{}, "Fiction"); err == nil {
		t.Fatal("empty title accepted")
	}
	if err := validateMetadata(Metadata{Title: string(bytes.Repeat([]byte("x"), 513))}, "Fiction"); err == nil {
		t.Fatal("oversized title accepted")
	}
	if err := validateMetadata(Metadata{Title: "Title", Language: string(bytes.Repeat([]byte("x"), 36))}, "Fiction"); err == nil {
		t.Fatal("oversized language accepted")
	}
	if err := validateMetadata(Metadata{Title: "  Title  ", Author: "Author"}, "Fiction"); err != nil {
		t.Fatal(err)
	}

	merged := mergeMetadata(Metadata{Title: "Fallback", Author: "old", Cover: []byte("old")}, Metadata{
		Title: "  Extracted  ", Author: "  New Author ", Language: " en ", Publisher: " Pub ", Cover: []byte("new"),
	})
	if merged.Title != "Extracted" || merged.Author != "New Author" || merged.Language != "en" || merged.Publisher != "Pub" || string(merged.Cover) != "new" {
		t.Fatalf("mergeMetadata() = %+v", merged)
	}
	if got := mergeMetadata(Metadata{Title: "Fallback"}, Metadata{Author: "Author"}).Title; got != "Fallback" {
		t.Fatalf("blank extracted title replaced fallback with %q", got)
	}
}

func TestIdentifyPDF(t *testing.T) {
	path := filepath.Join(t.TempDir(), "book.pdf")
	if err := os.WriteFile(path, []byte("%PDF-1.7\ncontent"), 0o600); err != nil {
		t.Fatal(err)
	}
	if got, err := identify(path, ".pdf"); err != nil || got != "pdf" {
		t.Fatalf("valid PDF: %q, %v", got, err)
	}
	if err := os.WriteFile(path, []byte("not a PDF"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := identify(path, ".pdf"); !errors.Is(err, ErrInvalidFormat) {
		t.Fatalf("invalid PDF error = %v", err)
	}
	if _, err := identify(path, ".epub"); !errors.Is(err, ErrInvalidFormat) {
		t.Fatalf("wrong extension error = %v", err)
	}
}

func TestStageCoverEmptyAndOversize(t *testing.T) {
	dir := t.TempDir()
	if err := os.Mkdir(filepath.Join(dir, "uploads"), 0o750); err != nil {
		t.Fatal(err)
	}
	svc := New(libRepo{}, dir, 1)
	name, err := svc.StageCover(bytes.NewReader(nil))
	if err != nil || name != "" {
		t.Fatalf("empty cover: %q, %v", name, err)
	}
	name, err = svc.StageCover(bytes.NewReader(bytes.Repeat([]byte("x"), 5<<20+1)))
	if !errors.Is(err, ErrTooLarge) || name != "" {
		t.Fatalf("oversize cover: %q, %v", name, err)
	}
}

type visibilityRepo struct {
	libRepo
	id     int64
	public bool
}

func (r *visibilityRepo) UpdateBookVisibility(_ context.Context, id int64, public bool) error {
	r.id, r.public = id, public
	return nil
}

func TestUpdateValidation(t *testing.T) {
	r := &updateRepo{}
	svc := New(r, t.TempDir(), 1)
	if err := svc.Update(context.Background(), 7, "  ", "author", "cat"); err == nil {
		t.Fatal("blank title accepted")
	}
	if err := svc.Update(context.Background(), 7, string(bytes.Repeat([]byte("x"), 513)), "", ""); err == nil {
		t.Fatal("long title accepted")
	}
	if err := svc.Update(context.Background(), 7, " Title ", " Author ", " "); err != nil {
		t.Fatal(err)
	}
	if r.id != 7 || r.title != "Title" || r.author != "Author" || r.category != "Uncategorized" {
		t.Fatalf("update arguments = %+v", r)
	}
}

type updateRepo struct {
	libRepo
	id                      int64
	title, author, category string
}

func (r *updateRepo) UpdateBook(_ context.Context, id int64, title, author, category string) error {
	r.id, r.title, r.author, r.category = id, title, author, category
	return nil
}

func TestUpdateVisibilityUnavailableAndAvailable(t *testing.T) {
	if err := New(libRepo{}, t.TempDir(), 1).UpdateVisibility(context.Background(), 3, true); err == nil {
		t.Fatal("visibility update reported available without repository support")
	}
	r := &visibilityRepo{}
	if err := New(r, t.TempDir(), 1).UpdateVisibility(context.Background(), 4, true); err != nil {
		t.Fatal(err)
	}
	if r.id != 4 || !r.public {
		t.Fatalf("visibility arguments = %d, %v", r.id, r.public)
	}
}

func TestCleanupStaleUploads(t *testing.T) {
	dir := t.TempDir()
	uploads := filepath.Join(dir, "uploads")
	if err := os.MkdirAll(filepath.Join(uploads, "nested"), 0o750); err != nil {
		t.Fatal(err)
	}
	old := filepath.Join(uploads, "old.tmp")
	fresh := filepath.Join(uploads, "fresh.tmp")
	if err := os.WriteFile(old, []byte("old"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(fresh, []byte("fresh"), 0o600); err != nil {
		t.Fatal(err)
	}
	now := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	if err := os.Chtimes(old, now.Add(-2*time.Hour), now.Add(-2*time.Hour)); err != nil {
		t.Fatal(err)
	}
	if err := os.Chtimes(fresh, now.Add(-10*time.Minute), now.Add(-10*time.Minute)); err != nil {
		t.Fatal(err)
	}
	svc := New(libRepo{}, dir, 1)
	svc.now = func() time.Time { return now }
	if err := svc.CleanupStaleUploads(time.Hour); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(old); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("old upload still exists: %v", err)
	}
	if _, err := os.Stat(fresh); err != nil {
		t.Fatalf("fresh upload missing: %v", err)
	}
	if _, err := os.Stat(filepath.Join(uploads, "nested")); err != nil {
		t.Fatalf("upload directory was removed: %v", err)
	}
}

func pngFixture(t *testing.T) []byte {
	t.Helper()
	var buf bytes.Buffer
	img := image.NewRGBA(image.Rect(0, 0, 1, 1))
	img.Set(0, 0, color.RGBA{R: 20, G: 40, B: 60, A: 255})
	if err := png.Encode(&buf, img); err != nil {
		t.Fatal(err)
	}
	return buf.Bytes()
}

func TestCoverValidationHelpers(t *testing.T) {
	dir := t.TempDir()
	data := pngFixture(t)
	name, err := writeValidatedCoverTemp(dir, bytes.NewReader(data), int64(len(data)))
	if err != nil {
		t.Fatal(err)
	}
	defer os.Remove(name)
	decoded, err := os.ReadFile(name)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := png.Decode(bytes.NewReader(decoded)); err != nil {
		t.Fatalf("validated cover is not PNG: %v", err)
	}
	if _, err := writeValidatedCoverTemp(dir, bytes.NewReader([]byte("not an image")), 1<<20); !errors.Is(err, ErrInvalidFormat) {
		t.Fatalf("invalid cover error = %v", err)
	}
	if _, err := writeValidatedCoverTemp(dir, bytes.NewReader(data), int64(len(data)-1)); !errors.Is(err, ErrTooLarge) {
		t.Fatalf("oversize cover error = %v", err)
	}

	src := filepath.Join(dir, "source.jpg")
	dst := filepath.Join(dir, "installed.png")
	if err := os.WriteFile(src, data, 0o600); err != nil {
		t.Fatal(err)
	}
	created, err := installValidatedCover(src, dst)
	if err != nil || !created {
		t.Fatalf("installValidatedCover() = %v, %v", created, err)
	}
	if _, err := os.Stat(dst); err != nil {
		t.Fatal(err)
	}
}
