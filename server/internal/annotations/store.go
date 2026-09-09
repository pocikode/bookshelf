package annotations

import (
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"
)

type Bookmark struct {
	ID        string          `json:"id"`
	BookID    string          `json:"bookId"`
	Locator   json.RawMessage `json:"locator"`
	Title     string          `json:"title"`
	Version   int64           `json:"version"`
	CreatedAt int64           `json:"createdAt"`
	UpdatedAt int64           `json:"updatedAt"`
	DeletedAt *int64          `json:"deletedAt,omitempty"`
}

type Annotation struct {
	ID           string          `json:"id"`
	BookID       string          `json:"bookId"`
	Type         string          `json:"type"`
	Locator      json.RawMessage `json:"locator"`
	SelectedText string          `json:"selectedText"`
	Note         string          `json:"note"`
	Metadata     json.RawMessage `json:"metadata"`
	Version      int64           `json:"version"`
	CreatedAt    int64           `json:"createdAt"`
	UpdatedAt    int64           `json:"updatedAt"`
	DeletedAt    *int64          `json:"deletedAt,omitempty"`
}

// ErrInvalidInput marks a rejected client payload, keeping it distinguishable
// from a storage failure at the HTTP boundary.
var ErrInvalidInput = errors.New("invalid input")

type Store struct{ DB *sql.DB }

func (s Store) ListBookmarks(userID, bookID string) ([]Bookmark, error) {
	rows, err := s.DB.Query(`SELECT id, book_id, locator, title, version, created_at, updated_at, deleted_at FROM bookmarks WHERE user_id = ? AND book_id = ? ORDER BY created_at`, userID, bookID)
	if err != nil {
		return nil, fmt.Errorf("query bookmarks for book %s: %w", bookID, err)
	}
	defer rows.Close()
	var result []Bookmark
	for rows.Next() {
		var item Bookmark
		if err := rows.Scan(&item.ID, &item.BookID, &item.Locator, &item.Title, &item.Version, &item.CreatedAt, &item.UpdatedAt, &item.DeletedAt); err != nil {
			return nil, fmt.Errorf("scan bookmark: %w", err)
		}
		result = append(result, item)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate bookmarks: %w", err)
	}
	return result, nil
}

func (s Store) CreateBookmark(userID, bookID string, item Bookmark) (Bookmark, error) {
	return s.UpsertBookmark(userID, bookID, item)
}

func (s Store) UpsertBookmark(userID, bookID string, item Bookmark) (Bookmark, error) {
	if len(item.Locator) == 0 || !json.Valid(item.Locator) {
		return Bookmark{}, fmt.Errorf("%w: bookmark locator", ErrInvalidInput)
	}
	if item.ID == "" {
		item.ID = uuid.NewString()
	}
	now := time.Now().UnixMilli()
	var version int64
	var createdAt int64
	err := s.DB.QueryRow(`SELECT version, created_at FROM bookmarks WHERE id = ? AND user_id = ? AND book_id = ?`, item.ID, userID, bookID).Scan(&version, &createdAt)
	switch {
	case errors.Is(err, sql.ErrNoRows):
		version = 1
		createdAt = now
		_, err = s.DB.Exec(`INSERT INTO bookmarks (id, user_id, book_id, locator, title, version, created_at, updated_at) VALUES (?, ?, ?, ?, ?, ?, ?, ?)`, item.ID, userID, bookID, item.Locator, item.Title, version, createdAt, now)
	case err == nil:
		version++
		_, err = s.DB.Exec(`UPDATE bookmarks SET locator = ?, title = ?, version = ?, deleted_at = NULL, updated_at = ? WHERE id = ? AND user_id = ? AND book_id = ?`, item.Locator, item.Title, version, now, item.ID, userID, bookID)
	default:
		return Bookmark{}, fmt.Errorf("read bookmark %s: %w", item.ID, err)
	}
	if err != nil {
		return Bookmark{}, fmt.Errorf("upsert bookmark %s: %w", item.ID, err)
	}
	item.BookID, item.Version, item.CreatedAt, item.UpdatedAt = bookID, version, createdAt, now
	return item, nil
}

func (s Store) DeleteBookmark(userID, bookID, id string) error {
	now := time.Now().UnixMilli()
	result, err := s.DB.Exec(`UPDATE bookmarks SET deleted_at = ?, version = version + 1, updated_at = ? WHERE id = ? AND user_id = ? AND book_id = ?`, now, now, id, userID, bookID)
	if err != nil {
		return fmt.Errorf("delete bookmark %s: %w", id, err)
	}
	if count, err := result.RowsAffected(); err != nil {
		return fmt.Errorf("check deleted bookmark %s: %w", id, err)
	} else if count == 0 {
		return nil
	}
	return nil
}

func (s Store) ListAnnotations(userID, bookID string) ([]Annotation, error) {
	rows, err := s.DB.Query(`SELECT id, book_id, type, locator, selected_text, note, metadata, version, created_at, updated_at, deleted_at FROM annotations WHERE user_id = ? AND book_id = ? ORDER BY created_at`, userID, bookID)
	if err != nil {
		return nil, fmt.Errorf("query annotations for book %s: %w", bookID, err)
	}
	defer rows.Close()
	var result []Annotation
	for rows.Next() {
		var item Annotation
		if err := rows.Scan(&item.ID, &item.BookID, &item.Type, &item.Locator, &item.SelectedText, &item.Note, &item.Metadata, &item.Version, &item.CreatedAt, &item.UpdatedAt, &item.DeletedAt); err != nil {
			return nil, fmt.Errorf("scan annotation: %w", err)
		}
		result = append(result, item)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate annotations: %w", err)
	}
	return result, nil
}

func (s Store) CreateAnnotation(userID, bookID string, item Annotation) (Annotation, error) {
	return s.UpsertAnnotation(userID, bookID, item)
}

func (s Store) UpsertAnnotation(userID, bookID string, item Annotation) (Annotation, error) {
	if item.Type != "highlight" && item.Type != "note" {
		return Annotation{}, fmt.Errorf("%w: annotation type %q", ErrInvalidInput, item.Type)
	}
	if len(item.Locator) == 0 || !json.Valid(item.Locator) {
		return Annotation{}, fmt.Errorf("%w: annotation locator", ErrInvalidInput)
	}
	if item.ID == "" {
		item.ID = uuid.NewString()
	}
	if len(item.Metadata) == 0 {
		item.Metadata = json.RawMessage(`{}`)
	}
	now := time.Now().UnixMilli()
	var version int64
	var createdAt int64
	err := s.DB.QueryRow(`SELECT version, created_at FROM annotations WHERE id = ? AND user_id = ? AND book_id = ?`, item.ID, userID, bookID).Scan(&version, &createdAt)
	switch {
	case errors.Is(err, sql.ErrNoRows):
		version = 1
		createdAt = now
		_, err = s.DB.Exec(`INSERT INTO annotations (id, user_id, book_id, type, locator, selected_text, note, metadata, version, created_at, updated_at) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`, item.ID, userID, bookID, item.Type, item.Locator, item.SelectedText, item.Note, item.Metadata, version, createdAt, now)
	case err == nil:
		version++
		_, err = s.DB.Exec(`UPDATE annotations SET type = ?, locator = ?, selected_text = ?, note = ?, metadata = ?, version = ?, deleted_at = NULL, updated_at = ? WHERE id = ? AND user_id = ? AND book_id = ?`, item.Type, item.Locator, item.SelectedText, item.Note, item.Metadata, version, now, item.ID, userID, bookID)
	default:
		return Annotation{}, fmt.Errorf("read annotation %s: %w", item.ID, err)
	}
	if err != nil {
		return Annotation{}, fmt.Errorf("upsert annotation %s: %w", item.ID, err)
	}
	item.BookID, item.Version, item.CreatedAt, item.UpdatedAt = bookID, version, createdAt, now
	return item, nil
}

func (s Store) DeleteAnnotation(userID, bookID, id string) error {
	now := time.Now().UnixMilli()
	result, err := s.DB.Exec(`UPDATE annotations SET deleted_at = ?, version = version + 1, updated_at = ? WHERE id = ? AND user_id = ? AND book_id = ?`, now, now, id, userID, bookID)
	if err != nil {
		return fmt.Errorf("delete annotation %s: %w", id, err)
	}
	if count, err := result.RowsAffected(); err != nil {
		return fmt.Errorf("check deleted annotation %s: %w", id, err)
	} else if count == 0 {
		return nil
	}
	return nil
}
