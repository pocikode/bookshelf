package library

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/pdfcpu/pdfcpu/pkg/api"
	"pocikode/bookshelf/internal/database"
)

var (
	ErrTooLarge      = errors.New("book is too large")
	ErrInvalidFormat = errors.New("only valid EPUB and PDF files are accepted")
)

type DuplicateError struct{ Book database.Book }

func (e *DuplicateError) Error() string { return "Already in library as " + e.Book.Title }

type repository interface {
	InsertBook(context.Context, *database.Book) error
	FindBook(context.Context, int64) (database.Book, error)
	FindBookByHash(context.Context, string) (database.Book, error)
	UpdateBook(context.Context, int64, string, string, string) error
	DeleteBookTx(context.Context, int64) error
}
type scopedHashRepository interface {
	FindBookByHashForUser(context.Context, string, int64, bool) (database.Book, error)
}

type Service struct {
	repo     repository
	dataDir  string
	maxBytes int64
	now      func() time.Time
}

func New(repo repository, dataDir string, maxBytes int64) *Service {
	return &Service{repo: repo, dataDir: dataDir, maxBytes: maxBytes, now: time.Now}
}

type StagedBook struct {
	Index                                   int
	Filename, Path, Hash, Format, Extension string
	Size                                    int64
}

func (s StagedBook) Cleanup() {
	if s.Path != "" {
		_ = os.Remove(s.Path)
	}
}

func (s *Service) Stage(index int, filename string, src io.Reader) (StagedBook, error) {
	f, err := os.CreateTemp(filepath.Join(s.dataDir, "uploads"), "book-*")
	if err != nil {
		return StagedBook{}, fmt.Errorf("create temporary upload: %w", err)
	}
	stage := StagedBook{Index: index, Filename: displayFilename(filename), Path: f.Name()}
	ok := false
	defer func() {
		_ = f.Close()
		if !ok {
			_ = os.Remove(stage.Path)
		}
	}()
	h := sha256.New()
	n, copyErr := io.Copy(io.MultiWriter(f, h), io.LimitReader(src, s.maxBytes+1))
	if copyErr != nil {
		return stage, fmt.Errorf("stream upload: %w", copyErr)
	}
	if n > s.maxBytes {
		return stage, ErrTooLarge
	}
	if err := f.Sync(); err != nil {
		return stage, fmt.Errorf("sync upload: %w", err)
	}
	if err := f.Close(); err != nil {
		return stage, fmt.Errorf("close upload: %w", err)
	}
	stage.Size = n
	stage.Hash = hex.EncodeToString(h.Sum(nil))
	stage.Extension = strings.ToLower(filepath.Ext(filename))
	format, err := identify(stage.Path, stage.Extension)
	if err != nil {
		return stage, err
	}
	stage.Format = format
	ok = true
	return stage, nil
}

func (s *Service) StageCover(src io.Reader) (string, error) {
	f, err := os.CreateTemp(filepath.Join(s.dataDir, "uploads"), "submitted-cover-*")
	if err != nil {
		return "", err
	}
	name := f.Name()
	ok := false
	defer func() {
		_ = f.Close()
		if !ok {
			_ = os.Remove(name)
		}
	}()
	n, err := io.Copy(f, io.LimitReader(src, (5<<20)+1))
	if err != nil {
		return "", err
	}
	if n == 0 {
		return "", nil
	}
	if n > 5<<20 {
		return "", ErrTooLarge
	}
	if err = f.Sync(); err != nil {
		return "", err
	}
	if err = f.Close(); err != nil {
		return "", err
	}
	ok = true
	return name, nil
}

func (s *Service) Path(rel string) (string, error) { return s.absolute(rel) }

func (s *Service) Create(ctx context.Context, stage StagedBook, coverTemp, category string) (database.Book, error) {
	return s.CreateForUser(ctx, 1, true, stage, coverTemp, category)
}

func (s *Service) CreateForUser(ctx context.Context, ownerID int64, public bool, stage StagedBook, coverTemp, category string) (database.Book, error) {
	defer stage.Cleanup()
	findDuplicate := s.repo.FindBookByHash
	if scoped, ok := s.repo.(scopedHashRepository); ok {
		findDuplicate = func(ctx context.Context, hash string) (database.Book, error) {
			return scoped.FindBookByHashForUser(ctx, hash, ownerID, false)
		}
	}
	if existing, err := findDuplicate(ctx, stage.Hash); err == nil {
		return database.Book{}, &DuplicateError{existing}
	} else if !database.IsNotFound(err) {
		return database.Book{}, err
	}
	meta := Metadata{Title: fallbackTitle(stage.Filename)}
	pageCount := 0
	if stage.Format == "epub" {
		if extracted, err := extractEPUB(stage.Path); err == nil {
			meta = mergeMetadata(meta, extracted)
		}
	} else {
		count, err := api.PageCountFile(stage.Path)
		if err != nil || count < 1 {
			return database.Book{}, ErrInvalidFormat
		}
		pageCount = count
	}
	category = NormalizeCategory(category)
	if err := validateMetadata(meta, category); err != nil {
		return database.Book{}, err
	}
	relBook := filepath.Join("books", stage.Hash[:2], stage.Hash+stage.Extension)
	absBook, err := s.absolute(relBook)
	if err != nil {
		return database.Book{}, err
	}
	if err := os.MkdirAll(filepath.Dir(absBook), 0o750); err != nil {
		return database.Book{}, err
	}
	bookCreated, err := installNoReplace(stage.Path, absBook)
	if err != nil {
		return database.Book{}, fmt.Errorf("install book: %w", err)
	}
	if bookCreated {
		stage.Path = ""
	}
	relCover := ""
	coverCreated := false
	coverSource := coverTemp
	removeCoverSource := false
	if stage.Format == "epub" && len(meta.Cover) > 0 {
		coverSource, err = writeValidatedCoverTemp(filepath.Join(s.dataDir, "uploads"), bytes.NewReader(meta.Cover), 20<<20)
		if err == nil {
			removeCoverSource = true
		} else {
			coverSource = ""
		}
	}
	if coverSource != "" {
		if removeCoverSource {
			defer os.Remove(coverSource)
		}
		relCover = filepath.Join("covers", stage.Hash[:2], stage.Hash+".png")
		absCover, _ := s.absolute(relCover)
		if err = os.MkdirAll(filepath.Dir(absCover), 0o750); err == nil {
			if removeCoverSource {
				coverCreated, err = installNoReplace(coverSource, absCover)
			} else {
				coverCreated, err = installValidatedCover(coverSource, absCover)
			}
		}
		if err != nil {
			relCover = ""
			coverCreated = false
		}
	}
	b := database.Book{OwnerID: ownerID, Public: public, Title: meta.Title, Author: meta.Author, Category: category, Format: stage.Format, FileHash: stage.Hash, FilePath: filepath.ToSlash(relBook), FileSize: stage.Size, CoverPath: filepath.ToSlash(relCover), PageCount: pageCount, Language: meta.Language, Publisher: meta.Publisher, CreatedAt: s.now().UTC()}
	if err := s.repo.InsertBook(ctx, &b); err != nil {
		findDuplicate := s.repo.FindBookByHash
		if scoped, ok := s.repo.(scopedHashRepository); ok {
			findDuplicate = func(ctx context.Context, hash string) (database.Book, error) {
				return scoped.FindBookByHashForUser(ctx, hash, ownerID, false)
			}
		}
		if existing, findErr := findDuplicate(ctx, stage.Hash); findErr == nil {
			return database.Book{}, &DuplicateError{existing}
		}
		if bookCreated {
			_ = os.Remove(absBook)
		}
		if coverCreated && relCover != "" {
			abs, _ := s.absolute(relCover)
			_ = os.Remove(abs)
		}
		return database.Book{}, err
	}
	return b, nil
}

func (s *Service) Update(ctx context.Context, id int64, title, author, category string) error {
	title = strings.TrimSpace(title)
	author = strings.TrimSpace(author)
	category = NormalizeCategory(category)
	if title == "" {
		return errors.New("title is required")
	}
	if runeLen(title) > 512 || runeLen(author) > 512 || runeLen(category) > 100 {
		return errors.New("metadata is too long")
	}
	return s.repo.UpdateBook(ctx, id, title, author, category)
}

func (s *Service) UpdateVisibility(ctx context.Context, id int64, public bool) error {
	repo, ok := s.repo.(interface {
		UpdateBookVisibility(context.Context, int64, bool) error
	})
	if !ok {
		return errors.New("book visibility update is unavailable")
	}
	return repo.UpdateBookVisibility(ctx, id, public)
}

func identify(path, ext string) (string, error) {
	f, err := os.Open(path)
	if err != nil {
		return "", err
	}
	defer f.Close()
	header := make([]byte, 5)
	n, _ := io.ReadFull(f, header)
	header = header[:n]
	switch ext {
	case ".pdf":
		if len(header) >= 5 && string(header) == "%PDF-" {
			return "pdf", nil
		}
	case ".epub":
		if len(header) >= 4 && header[0] == 'P' && header[1] == 'K' {
			if _, err := openEPUB(path); err == nil {
				return "epub", nil
			}
		}
	}
	return "", ErrInvalidFormat
}

func installNoReplace(src, dst string) (bool, error) {
	if err := os.Link(src, dst); err == nil {
		if err := os.Remove(src); err != nil {
			return true, err
		}
		return true, nil
	} else if errors.Is(err, os.ErrExist) {
		return false, nil
	} else {
		return false, err
	}
}
func displayFilename(name string) string {
	name = filepath.Base(strings.ReplaceAll(name, "\\", "/"))
	r := []rune(name)
	if len(r) > 255 {
		name = string(r[:255])
	}
	return name
}
func fallbackTitle(name string) string {
	base := strings.TrimSuffix(filepath.Base(name), filepath.Ext(name))
	base = strings.TrimSpace(strings.ReplaceAll(strings.ReplaceAll(base, "_", " "), "-", " "))
	if base == "" {
		return "Untitled"
	}
	return base
}
func NormalizeCategory(s string) string {
	s = strings.TrimSpace(s)
	if s == "" {
		return "Uncategorized"
	}
	return s
}
func runeLen(s string) int { return utf8.RuneCountInString(s) }
func validateMetadata(m Metadata, category string) error {
	if strings.TrimSpace(m.Title) == "" {
		return errors.New("title is required")
	}
	if runeLen(m.Title) > 512 || runeLen(m.Author) > 512 || runeLen(m.Publisher) > 512 || runeLen(category) > 100 || runeLen(m.Language) > 35 {
		return errors.New("metadata is too long")
	}
	return nil
}
func mergeMetadata(base, got Metadata) Metadata {
	if strings.TrimSpace(got.Title) != "" {
		base.Title = strings.TrimSpace(got.Title)
	}
	base.Author = strings.TrimSpace(got.Author)
	base.Language = strings.TrimSpace(got.Language)
	base.Publisher = strings.TrimSpace(got.Publisher)
	base.Cover = got.Cover
	return base
}

func (s *Service) CleanupStaleUploads(olderThan time.Duration) error {
	entries, err := os.ReadDir(filepath.Join(s.dataDir, "uploads"))
	if err != nil {
		return err
	}
	cut := s.now().Add(-olderThan)
	for _, e := range entries {
		if e.IsDir() {
			continue
		}
		info, err := e.Info()
		if err == nil && info.ModTime().Before(cut) {
			_ = os.Remove(filepath.Join(s.dataDir, "uploads", e.Name()))
		}
	}
	return nil
}
