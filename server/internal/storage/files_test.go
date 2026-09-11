package storage

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestSaveValidatesAndControlsPaths(t *testing.T) {
	store := FileStore{Root: t.TempDir(), MaxUpload: 1024}
	book, err := store.Save(bytes.NewReader([]byte("%PDF-1.7\nbook")), "../private.pdf", "application/pdf")
	if err != nil {
		t.Fatal(err)
	}
	if book.OriginalName != "private.pdf" {
		t.Fatalf("original name = %q", book.OriginalName)
	}
	if filepath.Dir(book.Path) != filepath.Join("books", book.Hash[:2]) {
		t.Fatalf("unexpected sharded path: %q", book.Path)
	}
	if filepath.Base(book.Path) != book.Hash+".pdf" {
		t.Fatalf("unexpected content-addressed path: %q", book.Path)
	}
	if !book.Created {
		t.Fatal("expected first upload to create the stored file")
	}
	if err := store.Remove("/tmp/not-the-book.pdf"); err == nil {
		t.Fatal("expected path safety error")
	}
}

func TestSaveRejectsInvalidBook(t *testing.T) {
	store := FileStore{Root: t.TempDir(), MaxUpload: 1024}
	if _, err := store.Save(bytes.NewReader([]byte("not a pdf")), "book.pdf", "application/pdf"); err == nil {
		t.Fatal("expected invalid PDF error")
	}
	if _, err := store.Save(bytes.NewReader([]byte("book")), "book.txt", "text/plain"); err == nil {
		t.Fatal("expected unsupported format error")
	}
}

func TestSaveCoverValidatesAndStoresImageBesideBook(t *testing.T) {
	root := t.TempDir()
	store := FileStore{Root: root, MaxUpload: 1024}
	bookPath := filepath.Join(root, "books", "ab", strings.Repeat("a", 64)+".epub")
	if err := os.MkdirAll(filepath.Dir(bookPath), 0o750); err != nil {
		t.Fatal(err)
	}
	png := append([]byte("\x89PNG\r\n\x1a\n"), make([]byte, 504)...)

	coverPath, err := store.SaveCover(bytes.NewReader(png), bookPath)
	if err != nil {
		t.Fatal(err)
	}
	if coverPath != filepath.Join("covers", "aa", strings.Repeat("a", 64)+".png") {
		t.Fatalf("cover path = %q", coverPath)
	}
	stored, err := os.ReadFile(filepath.Join(root, filepath.FromSlash(coverPath)))
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(stored, png) {
		t.Fatal("stored cover differs from upload")
	}
}

func TestSaveCoverRejectsNonImage(t *testing.T) {
	root := t.TempDir()
	store := FileStore{Root: root, MaxUpload: 1024}
	if _, err := store.SaveCover(bytes.NewReader([]byte("not an image")), "books/ab/"+strings.Repeat("a", 64)+".epub"); err == nil {
		t.Fatal("expected invalid cover error")
	}
}
