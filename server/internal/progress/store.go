package progress

import (
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"time"
)

type Progress struct {
	BookID   string          `json:"bookId"`
	Locator  json.RawMessage `json:"locator"`
	Progress float64         `json:"progress"`
	DeviceID string          `json:"deviceId"`
	Version  int64           `json:"version"`
	Updated  int64           `json:"updatedAt"`
}

type Store struct{ DB *sql.DB }

func (s Store) Get(userID, bookID string) (Progress, error) {
	var p Progress
	err := s.DB.QueryRow(`SELECT book_id, locator, progress, device_id, version, updated_at FROM reading_progress WHERE user_id = ? AND book_id = ?`, userID, bookID).Scan(&p.BookID, &p.Locator, &p.Progress, &p.DeviceID, &p.Version, &p.Updated)
	if err != nil {
		return Progress{}, fmt.Errorf("get progress for book %s: %w", bookID, err)
	}
	return p, nil
}

// ErrInvalidProgress separates a bad client payload from a storage failure so
// the HTTP layer can answer 400 without hiding a 500.
var ErrInvalidProgress = errors.New("invalid progress")

func (s Store) Upsert(userID, bookID string, p Progress) (Progress, error) {
	if p.Progress < 0 || p.Progress > 1 || len(p.Locator) == 0 || !json.Valid(p.Locator) {
		return Progress{}, ErrInvalidProgress
	}
	now := time.Now().UnixMilli()
	var version int64
	err := s.DB.QueryRow(`SELECT version FROM reading_progress WHERE user_id = ? AND book_id = ?`, userID, bookID).Scan(&version)
	switch {
	case errors.Is(err, sql.ErrNoRows):
		version = 1
		if _, err := s.DB.Exec(`INSERT INTO reading_progress (user_id, book_id, locator, progress, device_id, version, updated_at) VALUES (?, ?, ?, ?, ?, ?, ?)`, userID, bookID, p.Locator, p.Progress, p.DeviceID, version, now); err != nil {
			return Progress{}, fmt.Errorf("insert progress for book %s: %w", bookID, err)
		}
	case err != nil:
		// Previously this fell through to the shared error check and could
		// silently skip both writes; report the read failure explicitly.
		return Progress{}, fmt.Errorf("read progress version for book %s: %w", bookID, err)
	default:
		version++
		if _, err := s.DB.Exec(`UPDATE reading_progress SET locator = ?, progress = ?, device_id = ?, version = ?, updated_at = ? WHERE user_id = ? AND book_id = ?`, p.Locator, p.Progress, p.DeviceID, version, now, userID, bookID); err != nil {
			return Progress{}, fmt.Errorf("update progress for book %s: %w", bookID, err)
		}
	}
	p.BookID, p.Version, p.Updated = bookID, version, now
	return p, nil
}
