package storage

import (
	"bytes"
	"os"
	"path/filepath"
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
	if book.Path == "" || book.Path == "../private.pdf" {
		t.Fatalf("unsafe path: %q", book.Path)
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
	bookPath := filepath.Join(root, "book.epub")
	png := append([]byte("\x89PNG\r\n\x1a\n"), make([]byte, 504)...)

	coverPath, err := store.SaveCover(bytes.NewReader(png), bookPath)
	if err != nil {
		t.Fatal(err)
	}
	if coverPath != filepath.Join(root, "book.cover.png") {
		t.Fatalf("cover path = %q", coverPath)
	}
	stored, err := os.ReadFile(coverPath)
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
	if _, err := store.SaveCover(bytes.NewReader([]byte("not an image")), filepath.Join(root, "book.epub")); err == nil {
		t.Fatal("expected invalid cover error")
	}
}
