package progress

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
	progress  database.Progress
	has       bool
	preserve  bool
	findErr   error
	upsertErr error
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
func (r *repoStub) GetProgress(context.Context, int64) (database.Progress, error) {
	if !r.has {
		return database.Progress{}, sql.ErrNoRows
	}
	return r.progress, nil
}
func (r *repoStub) UpsertProgress(_ context.Context, p *database.Progress, preserve bool) error {
	if r.upsertErr != nil {
		return r.upsertErr
	}
	r.preserve = preserve
	if preserve && r.has {
		p.Percent = r.progress.Percent
	}
	r.progress = *p
	r.has = true
	return nil
}
func TestEPUBValidationAndPercentPreservation(t *testing.T) {
	repo := &repoStub{book: database.Book{ID: 1, Format: "epub"}, progress: database.Progress{BookID: 1, Percent: .4}, has: true}
	svc := New(repo)
	p, err := svc.Save(context.Background(), 1, SaveRequest{Position: "epubcfi(/6/2[chap]!/4/1:0)"})
	if err != nil {
		t.Fatal(err)
	}
	if !repo.preserve || p.Percent != .4 {
		t.Fatalf("preserve=%v percent=%v", repo.preserve, p.Percent)
	}
	if _, err = svc.Save(context.Background(), 1, SaveRequest{Position: "page 4"}); err == nil {
		t.Fatal("invalid CFI accepted")
	}
}
func TestPDFDerivesPercent(t *testing.T) {
	repo := &repoStub{book: database.Book{ID: 2, Format: "pdf", PageCount: 20}}
	p, err := New(repo).Save(context.Background(), 2, SaveRequest{Page: 5, DeviceLabel: "Chrome on Android"})
	if err != nil {
		t.Fatal(err)
	}
	if p.Position != "5" || p.Percent != .25 {
		t.Fatalf("unexpected progress: %+v", p)
	}
	if _, err = New(repo).Save(context.Background(), 2, SaveRequest{Page: 21}); err == nil {
		t.Fatal("out-of-range page accepted")
	}
}

func TestProgressGetFallbackAndErrors(t *testing.T) {
	ctx := context.Background()
	missing := &repoStub{book: database.Book{ID: 4, Format: "epub"}}
	p, err := New(missing).Get(ctx, 4)
	if err != nil || p.BookID != 4 {
		t.Fatalf("missing progress: %+v, %v", p, err)
	}
	if _, err = New(&repoStub{}).Get(ctx, 4); err == nil {
		t.Fatal("missing book accepted")
	}
	wantErr := errors.New("find failed")
	if _, err = New(&repoStub{findErr: wantErr}).Get(ctx, 4); !errors.Is(err, wantErr) {
		t.Fatalf("find error: %v", err)
	}
}

func TestProgressValidationAndUpsertErrors(t *testing.T) {
	tests := []SaveRequest{
		{Position: ""},
		{Position: "epubcfi(x)", Percent: float64Ptr(-.1)},
		{Position: "epubcfi(x)", Percent: float64Ptr(1.1)},
		{Position: "epubcfi(x)", DeviceLabel: strings.Repeat("x", 101)},
	}
	for i, in := range tests {
		repo := &repoStub{book: database.Book{ID: 1, Format: "epub"}}
		if _, err := New(repo).Save(context.Background(), 1, in); !errors.Is(err, ErrInvalid) {
			t.Errorf("case %d error=%v", i, err)
		}
	}
	if _, err := New(&repoStub{book: database.Book{ID: 2, Format: "mobi"}}).Save(context.Background(), 2, SaveRequest{}); !errors.Is(err, ErrInvalid) {
		t.Fatalf("unsupported format: %v", err)
	}
	if _, err := New(&repoStub{book: database.Book{ID: 2, Format: "pdf", PageCount: 0}}).Save(context.Background(), 2, SaveRequest{Page: 1}); !errors.Is(err, ErrInvalid) {
		t.Fatalf("invalid PDF: %v", err)
	}
	wantErr := errors.New("upsert failed")
	if _, err := New(&repoStub{book: database.Book{ID: 1, Format: "epub"}, upsertErr: wantErr}).Save(context.Background(), 1, SaveRequest{Position: "epubcfi(x)", Percent: float64Ptr(.2)}); !errors.Is(err, wantErr) {
		t.Fatalf("upsert error: %v", err)
	}
}

func float64Ptr(v float64) *float64 { return &v }
