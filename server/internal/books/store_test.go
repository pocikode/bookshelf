package books

import (
	"database/sql"
	"encoding/json"
	"errors"
	"path/filepath"
	"testing"

	"pocikode/bookshelf/server/internal/database"
)

func testBookDB(t *testing.T) *sql.DB {
	t.Helper()
	db, err := database.Open(filepath.Join(t.TempDir(), "books.sqlite"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Close() })
	for _, id := range []string{"owner", "other", "admin"} {
		if _, err := db.Exec(`INSERT INTO users (id, username, password_hash, created_at, updated_at) VALUES (?, ?, 'hash', 1, 1)`, id, id); err != nil {
			t.Fatal(err)
		}
	}
	return db.DB
}

func createTestBook(t *testing.T, store Store, owner, id string) Book {
	t.Helper()
	book, err := store.Create(owner, Uploaded{
		OriginalName: id + ".epub",
		MimeType:     "application/epub+zip",
		Size:         10,
		Hash:         id + "-hash",
		Path:         "/" + id,
	}, id, "Author", json.RawMessage(`{"title":"`+id+`"}`))
	if err != nil {
		t.Fatal(err)
	}
	return book
}

func TestPublicBooksAreReadableByOtherUsersAndPrivateBooksAreNot(t *testing.T) {
	store := Store{DB: testBookDB(t)}
	publicBook := createTestBook(t, store, "owner", "public")
	privateBook := createTestBook(t, store, "owner", "private")
	if _, err := store.Update("owner", privateBook.ID, privateBook.Title, privateBook.Author, "private", privateBook.Metadata); err != nil {
		t.Fatal(err)
	}

	books, err := store.List("other", "")
	if err != nil {
		t.Fatal(err)
	}
	if len(books) != 1 || books[0].ID != publicBook.ID {
		t.Fatalf("other user list = %#v", books)
	}
	if _, err := store.Get("other", publicBook.ID); err != nil {
		t.Fatalf("public book read: %v", err)
	}
	if _, err := store.Get("other", privateBook.ID); !errors.Is(err, sql.ErrNoRows) {
		t.Fatalf("private book read error = %v", err)
	}
}

func TestDeleteIsOwnerScopedButAdminCanDeleteAnyBook(t *testing.T) {
	store := Store{DB: testBookDB(t)}
	book := createTestBook(t, store, "owner", "owned")
	if _, err := store.Delete("other", book.ID, false); !errors.Is(err, sql.ErrNoRows) {
		t.Fatalf("other user delete error = %v", err)
	}
	if _, err := store.Get("owner", book.ID); err != nil {
		t.Fatalf("book deleted by other user: %v", err)
	}
	if _, err := store.Delete("admin", book.ID, true); err != nil {
		t.Fatalf("admin delete: %v", err)
	}
	if _, err := store.Get("owner", book.ID); !errors.Is(err, sql.ErrNoRows) {
		t.Fatalf("book after admin delete error = %v", err)
	}
}
