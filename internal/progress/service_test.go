package progress

import (
	"context"
	"database/sql"
	"testing"

	"pocikode/bookshelf/internal/database"
)

type repoStub struct {
	book     database.Book
	progress database.Progress
	has      bool
	preserve bool
}

func (r *repoStub) FindBook(context.Context, int64) (database.Book, error) {
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
