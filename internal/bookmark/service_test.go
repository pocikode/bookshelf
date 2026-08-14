package bookmark

import (
	"context"
	"database/sql"
	"errors"
	"strings"
	"testing"

	"pocikode/bookshelf/internal/database"
)

type repoStub struct {
	book      database.Book
	bookmarks []database.Bookmark
	nextID    int64
	count     int
	countSet  bool
	findErr   error
	listErr   error
	insertErr error
	deleteErr error
}

func (r *repoStub) FindBook(context.Context, int64) (database.Book, error) {
	if r.findErr != nil {
		return database.Book{}, r.findErr
	}
	if r.book.ID == 0 {
		return database.Book{}, sql.ErrNoRows
	}
	return r.book, nil
}
func (r *repoStub) ListBookmarks(context.Context, int64, int64) ([]database.Bookmark, error) {
	if r.listErr != nil {
		return nil, r.listErr
	}
	return r.bookmarks, nil
}
func (r *repoStub) InsertBookmark(_ context.Context, b *database.Bookmark) error {
	if r.insertErr != nil {
		return r.insertErr
	}
	for i, existing := range r.bookmarks {
		if existing.Position == b.Position {
			b.ID = existing.ID
			r.bookmarks[i] = *b
			return nil
		}
	}
	r.nextID++
	b.ID = r.nextID
	r.bookmarks = append(r.bookmarks, *b)
	return nil
}
func (r *repoStub) DeleteBookmark(context.Context, int64, int64) error { return r.deleteErr }
func (r *repoStub) CountBookmarks(context.Context, int64, int64) (int, error) {
	if r.countSet {
		return r.count, nil
	}
	return len(r.bookmarks), nil
}

func TestEPUBBookmarkValidation(t *testing.T) {
	repo := &repoStub{book: database.Book{ID: 1, Format: "epub"}}
	service := New(repo)
	ctx := context.Background()

	for _, position := range []string{"", "   ", "/6/2", "epubcfi(" + strings.Repeat("x", 16*1024)} {
		if _, err := service.Create(ctx, 7, 1, CreateRequest{Position: position}); !errors.Is(err, ErrInvalid) {
			t.Fatalf("position %q: %v", position, err)
		}
	}
	if _, err := service.Create(ctx, 7, 1, CreateRequest{Position: "epubcfi(/6/2)", Percent: 1.5}); !errors.Is(err, ErrInvalid) {
		t.Fatalf("percent out of range: %v", err)
	}
	saved, err := service.Create(ctx, 7, 1, CreateRequest{Position: " epubcfi(/6/2) ", Percent: .42})
	if err != nil || saved.Position != "epubcfi(/6/2)" || saved.Percent != .42 || saved.UserID != 7 || saved.BookID != 1 {
		t.Fatalf("saved=%+v err=%v", saved, err)
	}
	if saved.Label != "42%" {
		t.Fatalf("label fallback=%q", saved.Label)
	}
}

func TestPDFBookmarkDerivesPercentAndLabel(t *testing.T) {
	repo := &repoStub{book: database.Book{ID: 1, Format: "pdf", PageCount: 20}}
	service := New(repo)
	ctx := context.Background()

	for _, page := range []int{0, -1, 21} {
		if _, err := service.Create(ctx, 7, 1, CreateRequest{Page: page}); !errors.Is(err, ErrInvalid) {
			t.Fatalf("page %d: %v", page, err)
		}
	}
	/* Percent is derived from the page, so a client claim is ignored. */
	saved, err := service.Create(ctx, 7, 1, CreateRequest{Page: 5, Percent: .99})
	if err != nil || saved.Page != 5 || saved.Position != "5" || saved.Percent != .25 || saved.Label != "Page 5" {
		t.Fatalf("saved=%+v err=%v", saved, err)
	}
	labelled, err := service.Create(ctx, 7, 1, CreateRequest{Page: 6, Label: "  Chapter Three  "})
	if err != nil || labelled.Label != "Chapter Three" {
		t.Fatalf("labelled=%+v err=%v", labelled, err)
	}
	long, err := service.Create(ctx, 7, 1, CreateRequest{Page: 7, Label: strings.Repeat("é", 260)})
	if err != nil || len([]rune(long.Label)) != maxLabelRunes {
		t.Fatalf("long label runes=%d err=%v", len([]rune(long.Label)), err)
	}
}

func TestBookmarkLimitAndErrorPropagation(t *testing.T) {
	ctx := context.Background()
	missing := New(&repoStub{})
	if _, err := missing.Create(ctx, 7, 1, CreateRequest{Page: 1}); !database.IsNotFound(err) {
		t.Fatalf("missing book: %v", err)
	}
	unsupported := New(&repoStub{book: database.Book{ID: 1, Format: "djvu"}})
	if _, err := unsupported.Create(ctx, 7, 1, CreateRequest{Page: 1}); !errors.Is(err, ErrInvalid) {
		t.Fatalf("unsupported format: %v", err)
	}

	full := &repoStub{book: database.Book{ID: 1, Format: "pdf", PageCount: 500}, count: MaxPerBook, countSet: true}
	service := New(full)
	if _, err := service.Create(ctx, 7, 1, CreateRequest{Page: 3}); !errors.Is(err, ErrLimit) {
		t.Fatalf("limit: %v", err)
	}
	/* At the cap, a position already bookmarked can still be relabelled. */
	full.bookmarks = []database.Bookmark{{ID: 9, Position: "3", Label: "Old"}}
	saved, err := service.Create(ctx, 7, 1, CreateRequest{Page: 3, Label: "New"})
	if err != nil || saved.ID != 9 || saved.Label != "New" {
		t.Fatalf("relabel at cap: %+v, %v", saved, err)
	}

	boom := errors.New("boom")
	if _, err := New(&repoStub{book: database.Book{ID: 1, Format: "pdf", PageCount: 5}, insertErr: boom}).Create(ctx, 7, 1, CreateRequest{Page: 1}); !errors.Is(err, boom) {
		t.Fatalf("insert error: %v", err)
	}
	listing := &repoStub{bookmarks: []database.Bookmark{{ID: 1}}}
	got, err := New(listing).List(ctx, 7, 1)
	if err != nil || len(got) != 1 {
		t.Fatalf("list: %+v, %v", got, err)
	}
	if err := New(&repoStub{deleteErr: sql.ErrNoRows}).Delete(ctx, 7, 1); !database.IsNotFound(err) {
		t.Fatalf("delete: %v", err)
	}
	/* A list failure must not be read as "already bookmarked" and slip past the cap. */
	if _, err := New(&repoStub{book: database.Book{ID: 1, Format: "pdf", PageCount: 5}, count: MaxPerBook, countSet: true, listErr: boom}).Create(ctx, 7, 1, CreateRequest{Page: 1}); !errors.Is(err, ErrLimit) {
		t.Fatalf("list failure at cap: %v", err)
	}
}
