package books

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"time"

	"github.com/google/uuid"
)

type Book struct {
	ID           string          `json:"id"`
	OwnerID      string          `json:"ownerId"`
	Title        string          `json:"title"`
	Author       string          `json:"author"`
	Metadata     json.RawMessage `json:"metadata"`
	OriginalName string          `json:"filename"`
	MimeType     string          `json:"mimeType"`
	Size         int64           `json:"size"`
	Hash         string          `json:"hash"`
	Visibility   string          `json:"visibility"`
	CoverPath    string          `json:"-"`
	CreatedAt    int64           `json:"createdAt"`
	UpdatedAt    int64           `json:"updatedAt"`
	Path         string          `json:"-"`
}

type Store struct{ DB *sql.DB }

func (s Store) List(userID, query string) ([]Book, error) {
	pattern := "%" + query + "%"
	rows, err := s.DB.Query(`SELECT id, user_id, title, author, metadata, original_name, mime_type, file_size, file_hash, visibility, created_at, updated_at, file_path, COALESCE(cover_path, '') FROM books WHERE (user_id = ? OR visibility = 'public') AND (title LIKE ? OR author LIKE ?) ORDER BY title COLLATE NOCASE`, userID, pattern, pattern)
	if err != nil {
		return nil, fmt.Errorf("query books: %w", err)
	}
	defer rows.Close()
	result := make([]Book, 0)
	for rows.Next() {
		var book Book
		if err := rows.Scan(&book.ID, &book.OwnerID, &book.Title, &book.Author, &book.Metadata, &book.OriginalName, &book.MimeType, &book.Size, &book.Hash, &book.Visibility, &book.CreatedAt, &book.UpdatedAt, &book.Path, &book.CoverPath); err != nil {
			return nil, fmt.Errorf("scan book: %w", err)
		}
		result = append(result, book)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate books: %w", err)
	}
	return result, nil
}

func (s Store) Get(userID, id string) (Book, error) {
	var book Book
	err := s.DB.QueryRow(`SELECT id, user_id, title, author, metadata, original_name, mime_type, file_size, file_hash, visibility, created_at, updated_at, file_path, COALESCE(cover_path, '') FROM books WHERE id = ? AND (user_id = ? OR visibility = 'public')`, id, userID).Scan(&book.ID, &book.OwnerID, &book.Title, &book.Author, &book.Metadata, &book.OriginalName, &book.MimeType, &book.Size, &book.Hash, &book.Visibility, &book.CreatedAt, &book.UpdatedAt, &book.Path, &book.CoverPath)
	if err != nil {
		// Wrapped, not replaced: callers still match sql.ErrNoRows with
		// errors.Is while the log gets the operation that failed.
		return Book{}, fmt.Errorf("get book %s: %w", id, err)
	}
	return book, nil
}

func (s Store) Create(userID string, uploaded Uploaded, title, author string, metadata json.RawMessage) (Book, error) {
	if len(metadata) == 0 {
		metadata = json.RawMessage(`{}`)
	}
	now := time.Now().UnixMilli()
	book := Book{ID: uuid.NewString(), OwnerID: userID, Title: title, Author: author, Metadata: metadata, OriginalName: uploaded.OriginalName, MimeType: uploaded.MimeType, Size: uploaded.Size, Hash: uploaded.Hash, Visibility: "public", CoverPath: uploaded.CoverPath, CreatedAt: now, UpdatedAt: now, Path: uploaded.Path}
	_, err := s.DB.Exec(`INSERT INTO books (id, user_id, title, author, metadata, original_name, file_path, file_hash, mime_type, file_size, cover_path, visibility, created_at, updated_at) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`, book.ID, userID, book.Title, book.Author, book.Metadata, book.OriginalName, book.Path, book.Hash, book.MimeType, book.Size, nullableString(book.CoverPath), book.Visibility, now, now)
	if err != nil {
		return Book{}, fmt.Errorf("insert book %s: %w", book.ID, err)
	}
	return book, nil
}

func (s Store) Update(userID, id, title, author, visibility string, metadata json.RawMessage) (Book, error) {
	if visibility != "public" && visibility != "private" {
		return Book{}, fmt.Errorf("invalid visibility %q", visibility)
	}
	if len(metadata) == 0 {
		metadata = json.RawMessage(`{}`)
	}
	if !json.Valid(metadata) {
		return Book{}, fmt.Errorf("metadata must be JSON")
	}
	now := time.Now().UnixMilli()
	result, err := s.DB.Exec(`UPDATE books SET title = ?, author = ?, metadata = ?, visibility = ?, updated_at = ? WHERE user_id = ? AND id = ?`, title, author, metadata, visibility, now, userID, id)
	if err != nil {
		return Book{}, fmt.Errorf("update book %s: %w", id, err)
	}
	if count, _ := result.RowsAffected(); count == 0 {
		return Book{}, sql.ErrNoRows
	}
	return s.Get(userID, id)
}

func (s Store) Delete(userID, id string, admin bool) (Book, error) {
	var book Book
	var err error
	if admin {
		book, err = s.getAny(id)
	} else {
		book, err = s.getOwned(userID, id)
	}
	if err != nil {
		return Book{}, err
	}
	if _, err := s.DB.Exec(`DELETE FROM books WHERE id = ?`, id); err != nil {
		return Book{}, fmt.Errorf("delete book %s: %w", id, err)
	}
	return book, nil
}

func (s Store) getOwned(userID, id string) (Book, error) {
	var book Book
	err := s.DB.QueryRow(`SELECT id, user_id, title, author, metadata, original_name, mime_type, file_size, file_hash, visibility, created_at, updated_at, file_path, COALESCE(cover_path, '') FROM books WHERE user_id = ? AND id = ?`, userID, id).Scan(&book.ID, &book.OwnerID, &book.Title, &book.Author, &book.Metadata, &book.OriginalName, &book.MimeType, &book.Size, &book.Hash, &book.Visibility, &book.CreatedAt, &book.UpdatedAt, &book.Path, &book.CoverPath)
	if err != nil {
		return Book{}, fmt.Errorf("get book %s: %w", id, err)
	}
	return book, nil
}

func (s Store) getAny(id string) (Book, error) {
	var book Book
	err := s.DB.QueryRow(`SELECT id, user_id, title, author, metadata, original_name, mime_type, file_size, file_hash, visibility, created_at, updated_at, file_path, COALESCE(cover_path, '') FROM books WHERE id = ?`, id).Scan(&book.ID, &book.OwnerID, &book.Title, &book.Author, &book.Metadata, &book.OriginalName, &book.MimeType, &book.Size, &book.Hash, &book.Visibility, &book.CreatedAt, &book.UpdatedAt, &book.Path, &book.CoverPath)
	if err != nil {
		return Book{}, fmt.Errorf("get book %s: %w", id, err)
	}
	return book, nil
}

func nullableString(value string) any {
	if value == "" {
		return nil
	}
	return value
}

type Uploaded struct {
	OriginalName string
	MimeType     string
	Size         int64
	Hash         string
	Path         string
	CoverPath    string
}
