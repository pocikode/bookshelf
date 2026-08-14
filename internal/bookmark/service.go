package bookmark

import (
	"context"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"unicode/utf8"

	"pocikode/bookshelf/internal/database"
)

var (
	ErrInvalid = errors.New("invalid bookmark")
	ErrLimit   = errors.New("bookmark limit reached")
)

/* A bookmark list is loaded whole by the reader, so the cap keeps that payload
   bounded rather than guarding storage. */
const MaxPerBook = 200

const maxLabelRunes = 200

type repository interface {
	FindBook(context.Context, int64) (database.Book, error)
	ListBookmarks(ctx context.Context, userID, bookID int64) ([]database.Bookmark, error)
	InsertBookmark(context.Context, *database.Bookmark) error
	DeleteBookmark(ctx context.Context, userID, id int64) error
	CountBookmarks(ctx context.Context, userID, bookID int64) (int, error)
}

type Service struct{ repo repository }

func New(repo repository) *Service { return &Service{repo: repo} }

type CreateRequest struct {
	Position  string  `json:"position"`
	Page      int     `json:"page,omitempty"`
	Label     string  `json:"label,omitempty"`
	Percent   float64 `json:"percent,omitempty"`
	CSRFToken string  `json:"csrf_token,omitempty"`
}

func (s *Service) List(ctx context.Context, userID, bookID int64) ([]database.Bookmark, error) {
	return s.repo.ListBookmarks(ctx, userID, bookID)
}

func (s *Service) Delete(ctx context.Context, userID, id int64) error {
	return s.repo.DeleteBookmark(ctx, userID, id)
}

func (s *Service) Create(ctx context.Context, userID, bookID int64, in CreateRequest) (database.Bookmark, error) {
	book, err := s.repo.FindBook(ctx, bookID)
	if err != nil {
		return database.Bookmark{}, err
	}
	b := database.Bookmark{UserID: userID, BookID: bookID}
	switch book.Format {
	case "epub":
		b.Position = strings.TrimSpace(in.Position)
		if b.Position == "" || len(b.Position) > 16*1024 || !strings.HasPrefix(b.Position, "epubcfi(") {
			return b, fmt.Errorf("%w: position must be a valid EPUB CFI", ErrInvalid)
		}
		if in.Percent < 0 || in.Percent > 1 {
			return b, fmt.Errorf("%w: percent must be between 0 and 1", ErrInvalid)
		}
		b.Percent = in.Percent
	case "pdf":
		if in.Page < 1 || book.PageCount < 1 || in.Page > book.PageCount {
			return b, fmt.Errorf("%w: page is outside the PDF", ErrInvalid)
		}
		b.Page = in.Page
		b.Position = strconv.Itoa(in.Page)
		b.Percent = float64(in.Page) / float64(book.PageCount)
	default:
		return b, fmt.Errorf("%w: unsupported book format", ErrInvalid)
	}
	b.Label = label(in.Label, book.Format, b.Page, b.Percent)
	count, err := s.repo.CountBookmarks(ctx, userID, bookID)
	if err != nil {
		return b, err
	}
	/* The count is only a ceiling for new positions: replacing the label on a
	   spot already bookmarked must keep working at the cap. */
	if count >= MaxPerBook && !s.marked(ctx, userID, bookID, b.Position) {
		return b, fmt.Errorf("%w: at most %d bookmarks per book", ErrLimit, MaxPerBook)
	}
	if err := s.repo.InsertBookmark(ctx, &b); err != nil {
		return b, err
	}
	return b, nil
}

func (s *Service) marked(ctx context.Context, userID, bookID int64, position string) bool {
	existing, err := s.repo.ListBookmarks(ctx, userID, bookID)
	if err != nil {
		return false
	}
	for _, b := range existing {
		if b.Position == position {
			return true
		}
	}
	return false
}

func label(raw, format string, page int, percent float64) string {
	value := strings.TrimSpace(raw)
	if value == "" {
		if format == "pdf" {
			return "Page " + strconv.Itoa(page)
		}
		return strconv.Itoa(int(percent*100+0.5)) + "%"
	}
	if utf8.RuneCountInString(value) > maxLabelRunes {
		return string([]rune(value)[:maxLabelRunes])
	}
	return value
}
