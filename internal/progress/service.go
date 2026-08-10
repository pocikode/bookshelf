package progress

import (
	"context"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"unicode/utf8"

	"pocikode/bookshelf/internal/database"
)

var ErrInvalid = errors.New("invalid progress")

type repository interface {
	FindBook(context.Context, int64) (database.Book, error)
	GetProgress(context.Context, int64) (database.Progress, error)
	UpsertProgress(context.Context, *database.Progress, bool) error
}
type userRepository interface {
	GetProgressForUser(context.Context, int64, int64) (database.Progress, error)
	UpsertProgressForUser(context.Context, *database.Progress, bool) error
}
type Service struct{ repo repository }

func New(repo repository) *Service { return &Service{repo: repo} }

type SaveRequest struct {
	Position    string   `json:"position"`
	Percent     *float64 `json:"percent,omitempty"`
	Page        int      `json:"page,omitempty"`
	DeviceLabel string   `json:"device_label,omitempty"`
	CSRFToken   string   `json:"csrf_token,omitempty"`
}

func (s *Service) Get(ctx context.Context, id int64) (database.Progress, error) {
	return s.GetForUser(ctx, 0, id)
}
func (s *Service) GetForUser(ctx context.Context, userID, id int64) (database.Progress, error) {
	r, _ := s.repo.(userRepository)
	get := s.repo.GetProgress
	if r != nil {
		get = func(ctx context.Context, bookID int64) (database.Progress, error) {
			return r.GetProgressForUser(ctx, userID, bookID)
		}
	}
	p, err := get(ctx, id)
	if database.IsNotFound(err) {
		if _, bookErr := s.repo.FindBook(ctx, id); bookErr != nil {
			return p, bookErr
		}
		return database.Progress{BookID: id}, nil
	}
	return p, err
}
func (s *Service) Save(ctx context.Context, id int64, in SaveRequest) (database.Progress, error) {
	return s.SaveForUser(ctx, 0, id, in)
}
func (s *Service) SaveForUser(ctx context.Context, userID, id int64, in SaveRequest) (database.Progress, error) {
	book, err := s.repo.FindBook(ctx, id)
	if err != nil {
		return database.Progress{}, err
	}
	label := strings.TrimSpace(in.DeviceLabel)
	if utf8.RuneCountInString(label) > 100 {
		return database.Progress{}, fmt.Errorf("%w: device_label is too long", ErrInvalid)
	}
	p := database.Progress{UserID: userID, BookID: id, DeviceLabel: label}
	preserve := false
	switch book.Format {
	case "epub":
		p.Position = strings.TrimSpace(in.Position)
		if p.Position == "" || len(p.Position) > 16*1024 || !strings.HasPrefix(p.Position, "epubcfi(") {
			return p, fmt.Errorf("%w: position must be a valid EPUB CFI", ErrInvalid)
		}
		if in.Percent == nil {
			preserve = true
		} else if *in.Percent < 0 || *in.Percent > 1 {
			return p, fmt.Errorf("%w: percent must be between 0 and 1", ErrInvalid)
		} else {
			p.Percent = *in.Percent
		}
	case "pdf":
		if in.Page < 1 || book.PageCount < 1 || in.Page > book.PageCount {
			return p, fmt.Errorf("%w: page is outside the PDF", ErrInvalid)
		}
		p.Page = in.Page
		p.Position = strconv.Itoa(in.Page)
		p.Percent = float64(in.Page) / float64(book.PageCount)
	default:
		return p, fmt.Errorf("%w: unsupported book format", ErrInvalid)
	}
	if r, ok := s.repo.(userRepository); ok {
		err = r.UpsertProgressForUser(ctx, &p, preserve)
	} else {
		err = s.repo.UpsertProgress(ctx, &p, preserve)
	}
	if err != nil {
		return p, err
	}
	if preserve {
		stored, e := s.GetForUser(ctx, userID, id)
		if e == nil {
			p.Percent = stored.Percent
		}
	}
	return p, nil
}
