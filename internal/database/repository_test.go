package database

import (
	"context"
	"database/sql"
	"path/filepath"
	"testing"
	"time"
)

func testRepository(t *testing.T) *Repository {
	t.Helper()
	db, err := Open(context.Background(), t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Close() })
	return NewRepository(db)
}

func TestRepositoryUsersAndCredentials(t *testing.T) {
	ctx := context.Background()
	repo := testRepository(t)
	now := time.Date(2026, 1, 2, 3, 4, 5, 0, time.UTC)

	admin, err := repo.FindUser(ctx, 1)
	if err != nil || !admin.IsAdmin() {
		t.Fatalf("admin: %+v, %v", admin, err)
	}
	user, err := repo.CreateUser(ctx, "Reader", "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa", "user", now)
	if err != nil {
		t.Fatal(err)
	}
	if user.ID == 0 || user.Username != "Reader" || !user.CreatedAt.Equal(now) {
		t.Fatalf("created user: %+v", user)
	}
	byName, err := repo.FindUserByUsername(ctx, "reader")
	if err != nil || byName.ID != user.ID {
		t.Fatalf("find by username: %+v, %v", byName, err)
	}
	if err := repo.SetUserPassword(ctx, user.ID, "bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb"); err != nil {
		t.Fatal(err)
	}
	digest, err := repo.GetUserPasswordDigest(ctx, user.ID)
	if err != nil || digest[0] != 'b' {
		t.Fatalf("password digest: %q, %v", digest, err)
	}
	if err := repo.SetUserDisabled(ctx, user.ID, true); err != nil {
		t.Fatal(err)
	}
	byID, err := repo.FindUser(ctx, user.ID)
	if err != nil || !byID.Disabled {
		t.Fatalf("disabled user: %+v, %v", byID, err)
	}
	users, err := repo.ListUsers(ctx)
	if err != nil || len(users) != 2 || users[0].Username != "admin" || users[1].Username != "Reader" {
		t.Fatalf("users: %+v, %v", users, err)
	}
	if err := repo.EnsureAdmin(ctx, "cccccccccccccccccccccccccccccccccccccccccccccccccccccccccccccccc", now); err != nil {
		t.Fatal(err)
	}
	adminDigest, err := repo.GetUserPasswordDigest(ctx, 1)
	if err != nil || adminDigest[0] != 'c' {
		t.Fatalf("admin digest: %q, %v", adminDigest, err)
	}
	credential := PasswordCredential{Digest: "dddddddddddddddddddddddddddddddddddddddddddddddddddddddddddddddd", UpdatedAt: now}
	if err := repo.SetPasswordCredential(ctx, credential); err != nil {
		t.Fatal(err)
	}
	gotCredential, err := repo.GetPasswordCredential(ctx)
	if err != nil || gotCredential != credential {
		t.Fatalf("credential: %+v, %v", gotCredential, err)
	}
}

func TestRepositorySessions(t *testing.T) {
	ctx := context.Background()
	repo := testRepository(t)
	now := time.Date(2026, 2, 3, 4, 5, 6, 0, time.UTC)
	sessions := []Session{
		{TokenHash: "anonymous", PasswordBinding: "a", CreatedAt: now, LastSeenAt: now, ExpiresAt: now.Add(time.Hour), UserAgent: ""},
		{TokenHash: "user", PasswordBinding: "b", CreatedAt: now, LastSeenAt: now, ExpiresAt: now.Add(-time.Hour), UserAgent: "reader", UserID: 1},
	}
	for _, session := range sessions {
		if err := repo.CreateSession(ctx, session); err != nil {
			t.Fatal(err)
		}
	}
	got, err := repo.FindSession(ctx, "anonymous")
	if err != nil || got.UserID != 1 || got.UserAgent != "" || !got.ExpiresAt.Equal(sessions[0].ExpiresAt) {
		t.Fatalf("anonymous session: %+v, %v", got, err)
	}
	touched := now.Add(2 * time.Hour)
	if err := repo.TouchSession(ctx, "anonymous", touched); err != nil {
		t.Fatal(err)
	}
	got, err = repo.FindSession(ctx, "anonymous")
	if err != nil || !got.LastSeenAt.Equal(touched) {
		t.Fatalf("touched session: %+v, %v", got, err)
	}
	removed, err := repo.SweepSessions(ctx, now)
	if err != nil || removed != 1 {
		t.Fatalf("swept=%d err=%v", removed, err)
	}
	if err := repo.DeleteSession(ctx, "anonymous"); err != nil {
		t.Fatal(err)
	}
	if err := repo.CreateSession(ctx, sessions[0]); err != nil {
		t.Fatal(err)
	}
	if err := repo.DeleteUserSessions(ctx, 1); err != nil {
		t.Fatal(err)
	}
	if err := repo.DeleteAllSessions(ctx); err != nil {
		t.Fatal(err)
	}
	if _, err := repo.FindSession(ctx, "anonymous"); !IsNotFound(err) {
		t.Fatalf("session still exists: %v", err)
	}
}

func TestRepositoryBookVisibilityListingAndProgress(t *testing.T) {
	ctx := context.Background()
	repo := testRepository(t)
	user, err := repo.CreateUser(ctx, "reader", "eeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeee", "user", time.Now().UTC())
	if err != nil {
		t.Fatal(err)
	}
	now := time.Now().UTC()
	books := []*Book{
		{OwnerID: 1, Public: true, Title: "Public", Author: "Author", Category: "Fiction", Format: "epub", FileHash: "public", FilePath: filepath.ToSlash("books/public.epub"), FileSize: 1, CreatedAt: now},
		{OwnerID: user.ID, Public: false, Title: "Private Reader", Category: "Private", Format: "pdf", FileHash: "reader", FilePath: filepath.ToSlash("books/reader.pdf"), FileSize: 1, PageCount: 10, CreatedAt: now.Add(time.Second)},
		{OwnerID: 1, Public: false, Title: "Private Admin", Category: "Private", Format: "epub", FileHash: "admin", FilePath: filepath.ToSlash("books/admin.epub"), FileSize: 1, CreatedAt: now.Add(2 * time.Second)},
	}
	for _, book := range books {
		if err := repo.InsertBook(ctx, book); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := repo.FindBookForUser(ctx, books[2].ID, user.ID, false); !IsNotFound(err) {
		t.Fatalf("private book leaked: %v", err)
	}
	if got, err := repo.FindBookForUser(ctx, books[2].ID, user.ID, true); err != nil || got.ID != books[2].ID {
		t.Fatalf("admin book: %+v, %v", got, err)
	}
	if got, err := repo.FindBookByHash(ctx, "public"); err != nil || got.ID != books[0].ID {
		t.Fatalf("book hash: %+v, %v", got, err)
	}
	if _, err := repo.FindBookByHashForUser(ctx, 0, user.ID, false); !IsNotFound(err) {
		t.Fatalf("unexpected numeric hash result: %v", err)
	}
	visible, count, err := repo.ListBooks(ctx, BookListOptions{UserID: user.ID, Page: 0, Limit: 0, Sort: "unknown", Direction: "asc"})
	if err != nil || count != 2 || len(visible) != 2 {
		t.Fatalf("visible books: count=%d books=%+v err=%v", count, visible, err)
	}
	all, count, err := repo.ListBooks(ctx, BookListOptions{Admin: true, Page: 1, Limit: 2, Sort: "title", Direction: "asc"})
	if err != nil || count != 3 || len(all) != 2 || all[0].Title != "Private Admin" {
		t.Fatalf("admin page: count=%d books=%+v err=%v", count, all, err)
	}
	if err := repo.UpdateBook(ctx, books[0].ID, "Renamed", "", "Updated"); err != nil {
		t.Fatal(err)
	}
	if err := repo.UpdateBookVisibility(ctx, books[0].ID, false); err != nil {
		t.Fatal(err)
	}
	updated, err := repo.FindBook(ctx, books[0].ID)
	if err != nil || updated.Title != "Renamed" || updated.Author != "" || updated.Public {
		t.Fatalf("updated book: %+v, %v", updated, err)
	}
	progress := Progress{UserID: user.ID, BookID: books[1].ID, Position: "epubcfi(/6/2)", Page: 3, Percent: .3, DeviceLabel: "phone"}
	if err := repo.UpsertProgressForUser(ctx, &progress, false); err != nil {
		t.Fatal(err)
	}
	gotProgress, err := repo.GetProgressForUser(ctx, user.ID, books[1].ID)
	if err != nil || gotProgress.Percent != .3 || gotProgress.DeviceLabel != "phone" {
		t.Fatalf("progress: %+v, %v", gotProgress, err)
	}
	progress.Position = "epubcfi(/6/4)"
	if err := repo.UpsertProgressForUser(ctx, &progress, true); err != nil {
		t.Fatal(err)
	}
	defaultProgress := Progress{BookID: books[2].ID, Position: "epubcfi(/6/2)", Percent: .2}
	if err := repo.UpsertProgress(ctx, &defaultProgress, false); err != nil {
		t.Fatal(err)
	}
	gotProgress, err = repo.GetProgress(ctx, books[2].ID)
	if err != nil || gotProgress.UserID != 1 {
		t.Fatalf("default progress user: %+v, %v", gotProgress, err)
	}
	continued, err := repo.ContinueReadingForUser(ctx, user.ID, false)
	if err != nil || len(continued) != 1 || continued[0].ID != books[1].ID {
		t.Fatalf("continued: %+v, %v", continued, err)
	}
	categories, err := repo.CategoriesForUser(ctx, user.ID, false)
	if err != nil || len(categories) != 1 || categories[0] != "Private" {
		t.Fatalf("categories: %v, %v", categories, err)
	}
	if err := repo.UpdateBook(ctx, 999, "missing", "", ""); !IsNotFound(err) {
		t.Fatalf("missing update: %v", err)
	}
	if err := repo.DeleteBookTx(ctx, books[0].ID); err != nil {
		t.Fatal(err)
	}
	if _, err := repo.FindBook(ctx, books[0].ID); !IsNotFound(err) {
		t.Fatalf("deleted book: %v", err)
	}
	if err := repo.DeleteBookTx(ctx, 999); !IsNotFound(err) {
		t.Fatalf("missing delete: %v", err)
	}
}

func TestRepositoryBookmarks(t *testing.T) {
	ctx := context.Background()
	repo := testRepository(t)
	now := time.Date(2026, 3, 4, 5, 6, 7, 0, time.UTC)
	reader, err := repo.CreateUser(ctx, "marker", "dddddddddddddddddddddddddddddddddddddddddddddddddddddddddddddddd", "user", now)
	if err != nil {
		t.Fatal(err)
	}
	book := Book{Title: "Marked", Category: "Tests", Format: "epub", FileHash: "hash-bookmarks", FilePath: "books/hash.epub", FileSize: 12, CreatedAt: now}
	if err := repo.InsertBook(ctx, &book); err != nil {
		t.Fatal(err)
	}

	first := Bookmark{UserID: reader.ID, BookID: book.ID, Position: "epubcfi(/6/2)", Label: "Opening", Percent: .1}
	if err := repo.InsertBookmark(ctx, &first); err != nil {
		t.Fatal(err)
	}
	if first.ID == 0 || first.CreatedAt.IsZero() {
		t.Fatalf("inserted bookmark: %+v", first)
	}
	second := Bookmark{UserID: reader.ID, BookID: book.ID, Position: "epubcfi(/6/4)", Label: "Later", Percent: .5}
	if err := repo.InsertBookmark(ctx, &second); err != nil {
		t.Fatal(err)
	}
	/* Same position again: the row is reused, so the label moves and the count
	   holds steady. */
	renamed := Bookmark{UserID: reader.ID, BookID: book.ID, Position: "epubcfi(/6/2)", Label: "Prologue", Percent: .12}
	if err := repo.InsertBookmark(ctx, &renamed); err != nil {
		t.Fatal(err)
	}
	if renamed.ID != first.ID {
		t.Fatalf("duplicate position made a new row: %d vs %d", renamed.ID, first.ID)
	}
	bookmarks, err := repo.ListBookmarks(ctx, reader.ID, book.ID)
	if err != nil || len(bookmarks) != 2 || bookmarks[0].Label != "Prologue" || bookmarks[0].Percent != .12 {
		t.Fatalf("bookmarks: %+v, %v", bookmarks, err)
	}
	count, err := repo.CountBookmarks(ctx, reader.ID, book.ID)
	if err != nil || count != 2 {
		t.Fatalf("count=%d err=%v", count, err)
	}
	if empty, err := repo.ListBookmarks(ctx, 1, book.ID); err != nil || len(empty) != 0 {
		t.Fatalf("other user bookmarks: %+v, %v", empty, err)
	}
	if err := repo.DeleteBookmark(ctx, 1, first.ID); !IsNotFound(err) {
		t.Fatalf("delete as another user: %v", err)
	}
	if err := repo.DeleteBookmark(ctx, reader.ID, first.ID); err != nil {
		t.Fatal(err)
	}
	if err := repo.DeleteBookmark(ctx, reader.ID, first.ID); !IsNotFound(err) {
		t.Fatalf("delete twice: %v", err)
	}
	if err := repo.DeleteBookTx(ctx, book.ID); err != nil {
		t.Fatal(err)
	}
	if remaining, err := repo.ListBookmarks(ctx, reader.ID, book.ID); err != nil || len(remaining) != 0 {
		t.Fatalf("bookmarks survived the book: %+v, %v", remaining, err)
	}
}

func TestRepositoryHelpers(t *testing.T) {
	if got := escapeLike(`a%_\\b`); got != `a\%\_\\\\b` {
		t.Fatalf("escapeLike: %q", got)
	}
	if nullString("") != nil || nullString("x") != "x" || nullInt(0) != nil || nullInt(2) != 2 || nullInt64(0) != nil || nullInt64(2) != int64(2) {
		t.Fatal("null helpers")
	}
	if _, err := parseStamp("bad"); err == nil || !IsNotFound(sql.ErrNoRows) || IsNotFound(nil) {
		t.Fatalf("helper errors: %v", err)
	}
}
