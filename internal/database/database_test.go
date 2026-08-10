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

func TestCheckpointAndRepositoryConvenienceMethods(t *testing.T) {
	ctx := context.Background()
	db, err := Open(ctx, t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()

	repo := NewRepository(db)
	book := Book{
		OwnerID: 1, Public: true, Title: "Checkpoint", Category: "Reference",
		Format: "epub", FileHash: "checkpoint", FilePath: "books/checkpoint.epub", FileSize: 1,
		CreatedAt: time.Now().UTC(),
	}
	if err := repo.InsertBook(ctx, &book); err != nil {
		t.Fatal(err)
	}
	if err := repo.UpsertProgress(ctx, &Progress{BookID: book.ID, Percent: 0.4}, false); err != nil {
		t.Fatal(err)
	}

	continued, err := repo.ContinueReading(ctx)
	if err != nil || len(continued) != 0 {
		t.Fatalf("continued: %+v, %v", continued, err)
	}
	continued, err = repo.ContinueReadingForUser(ctx, 1, true)
	if err != nil || len(continued) != 1 || continued[0].ID != book.ID {
		t.Fatalf("admin continued: %+v, %v", continued, err)
	}
	categories, err := repo.Categories(ctx)
	if err != nil || len(categories) != 1 || categories[0] != book.Category {
		t.Fatalf("categories: %v, %v", categories, err)
	}
	categories, err = repo.CategoriesForUser(ctx, 1, false)
	if err != nil || len(categories) != 1 || categories[0] != book.Category {
		t.Fatalf("user categories: %v, %v", categories, err)
	}
	if err := Checkpoint(ctx, db); err != nil {
		t.Fatal(err)
	}
}

func TestRepositoryMissingRecords(t *testing.T) {
	ctx := context.Background()
	repo := testRepository(t)
	if _, err := repo.FindUserByUsername(ctx, "missing"); !IsNotFound(err) {
		t.Fatalf("missing username: %v", err)
	}
	if _, err := repo.FindUser(ctx, 999); !IsNotFound(err) {
		t.Fatalf("missing user: %v", err)
	}
	if _, err := repo.GetUserPasswordDigest(ctx, 999); !IsNotFound(err) {
		t.Fatalf("missing password digest: %v", err)
	}
	if _, err := repo.GetPasswordCredential(ctx); !IsNotFound(err) {
		t.Fatalf("missing credential: %v", err)
	}
	if _, err := repo.FindSession(ctx, "missing"); !IsNotFound(err) {
		t.Fatalf("missing session: %v", err)
	}
	if _, err := repo.CreateUser(ctx, "admin", "ffffffffffffffffffffffffffffffffffffffffffffffffffffffffffffffff", "user", time.Now().UTC()); err == nil {
		t.Fatal("duplicate user was accepted")
	}
	if _, err := repo.DB.ExecContext(ctx, `UPDATE users SET created_at='bad' WHERE id=1`); err != nil {
		t.Fatal(err)
	}
	if _, err := repo.FindUser(ctx, 1); err == nil {
		t.Fatal("invalid user timestamp was accepted")
	}
	if _, err := repo.FindUserByUsername(ctx, "admin"); err == nil {
		t.Fatal("invalid username timestamp was accepted")
	}
	if _, err := repo.ListUsers(ctx); err == nil {
		t.Fatal("invalid list timestamp was accepted")
	}
	book := Book{Title: "bad timestamp", Format: "epub", FileHash: "bad-timestamp", FilePath: "books/bad.epub", FileSize: 1, CreatedAt: time.Now().UTC()}
	if err := repo.InsertBook(ctx, &book); err != nil {
		t.Fatal(err)
	}
	if _, err := repo.DB.ExecContext(ctx, `UPDATE books SET created_at='bad' WHERE id=?`, book.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := repo.FindBook(ctx, book.ID); err == nil {
		t.Fatal("invalid book timestamp was accepted")
	}
	if err := repo.UpsertProgress(ctx, &Progress{BookID: book.ID, Percent: 0.2}, false); err != nil {
		t.Fatal(err)
	}
	if _, err := repo.DB.ExecContext(ctx, `UPDATE books SET created_at=? WHERE id=?`, stamp(time.Now().UTC()), book.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := repo.DB.ExecContext(ctx, `UPDATE reading_progress SET updated_at='bad' WHERE book_id=?`, book.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := repo.FindBook(ctx, book.ID); err == nil {
		t.Fatal("invalid progress timestamp was accepted")
	}
	session := Session{TokenHash: "bad-session", PasswordBinding: "binding", CreatedAt: time.Now().UTC(), LastSeenAt: time.Now().UTC(), ExpiresAt: time.Now().Add(time.Hour)}
	if err := repo.CreateSession(ctx, session); err != nil {
		t.Fatal(err)
	}
	if _, err := repo.DB.ExecContext(ctx, `UPDATE sessions SET created_at='bad' WHERE token_hash=?`, session.TokenHash); err != nil {
		t.Fatal(err)
	}
	if _, err := repo.FindSession(ctx, session.TokenHash); err == nil {
		t.Fatal("invalid session timestamp was accepted")
	}
	if _, err := repo.DB.ExecContext(ctx, `UPDATE sessions SET created_at=?,last_seen_at='bad' WHERE token_hash=?`, stamp(session.CreatedAt), session.TokenHash); err != nil {
		t.Fatal(err)
	}
	if _, err := repo.FindSession(ctx, session.TokenHash); err == nil {
		t.Fatal("invalid session last-seen timestamp was accepted")
	}
	if _, err := repo.DB.ExecContext(ctx, `UPDATE sessions SET last_seen_at=?,expires_at='bad' WHERE token_hash=?`, stamp(session.LastSeenAt), session.TokenHash); err != nil {
		t.Fatal(err)
	}
	if _, err := repo.FindSession(ctx, session.TokenHash); err == nil {
		t.Fatal("invalid session expiry timestamp was accepted")
	}
}
