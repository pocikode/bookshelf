package annotations

import (
	"database/sql"
	"encoding/json"
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
}

type Store struct{ DB *sql.DB }

func (s Store) ListBookmarks(userID, bookID string) ([]Bookmark, error) {
	rows, err := s.DB.Query(`SELECT id, book_id, locator, title, version, created_at, updated_at FROM bookmarks WHERE user_id = ? AND book_id = ? AND deleted_at IS NULL ORDER BY created_at`, userID, bookID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var result []Bookmark
	for rows.Next() {
		var item Bookmark
		if err := rows.Scan(&item.ID, &item.BookID, &item.Locator, &item.Title, &item.Version, &item.CreatedAt, &item.UpdatedAt); err != nil {
			return nil, err
		}
		result = append(result, item)
	}
	return result, rows.Err()
}

func (s Store) CreateBookmark(userID, bookID string, item Bookmark) (Bookmark, error) {
	if len(item.Locator) == 0 || !json.Valid(item.Locator) {
		return Bookmark{}, fmt.Errorf("invalid locator")
	}
	if item.ID == "" {
		item.ID = uuid.NewString()
	}
	now := time.Now().UnixMilli()
	item.BookID, item.Version, item.CreatedAt, item.UpdatedAt = bookID, 1, now, now
	_, err := s.DB.Exec(`INSERT INTO bookmarks (id, user_id, book_id, locator, title, version, created_at, updated_at) VALUES (?, ?, ?, ?, ?, ?, ?, ?)`, item.ID, userID, bookID, item.Locator, item.Title, item.Version, now, now)
	return item, err
}

func (s Store) ListAnnotations(userID, bookID string) ([]Annotation, error) {
	rows, err := s.DB.Query(`SELECT id, book_id, type, locator, selected_text, note, metadata, version, created_at, updated_at FROM annotations WHERE user_id = ? AND book_id = ? AND deleted_at IS NULL ORDER BY created_at`, userID, bookID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var result []Annotation
	for rows.Next() {
		var item Annotation
		if err := rows.Scan(&item.ID, &item.BookID, &item.Type, &item.Locator, &item.SelectedText, &item.Note, &item.Metadata, &item.Version, &item.CreatedAt, &item.UpdatedAt); err != nil {
			return nil, err
		}
		result = append(result, item)
	}
	return result, rows.Err()
}

func (s Store) CreateAnnotation(userID, bookID string, item Annotation) (Annotation, error) {
	if item.Type != "highlight" && item.Type != "note" {
		return Annotation{}, fmt.Errorf("invalid annotation type")
	}
	if len(item.Locator) == 0 || !json.Valid(item.Locator) {
		return Annotation{}, fmt.Errorf("invalid locator")
	}
	if item.ID == "" {
		item.ID = uuid.NewString()
	}
	if len(item.Metadata) == 0 {
		item.Metadata = json.RawMessage(`{}`)
	}
	now := time.Now().UnixMilli()
	item.BookID, item.Version, item.CreatedAt, item.UpdatedAt = bookID, 1, now, now
	_, err := s.DB.Exec(`INSERT INTO annotations (id, user_id, book_id, type, locator, selected_text, note, metadata, version, created_at, updated_at) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`, item.ID, userID, bookID, item.Type, item.Locator, item.SelectedText, item.Note, item.Metadata, item.Version, now, now)
	return item, err
}
