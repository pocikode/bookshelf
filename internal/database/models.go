package database

import "time"

type Session struct {
	TokenHash       string
	PasswordBinding string
	CreatedAt       time.Time
	LastSeenAt      time.Time
	ExpiresAt       time.Time
	UserAgent       string
}

type PasswordCredential struct {
	Digest    string
	UpdatedAt time.Time
}

type Book struct {
	ID         int64      `json:"id"`
	Title      string     `json:"title"`
	Author     string     `json:"author,omitempty"`
	Category   string     `json:"category"`
	Format     string     `json:"format"`
	FileHash   string     `json:"file_hash"`
	FilePath   string     `json:"-"`
	FileSize   int64      `json:"file_size"`
	CoverPath  string     `json:"-"`
	PageCount  int        `json:"page_count,omitempty"`
	Language   string     `json:"language,omitempty"`
	Publisher  string     `json:"publisher,omitempty"`
	CreatedAt  time.Time  `json:"created_at"`
	Percent    float64    `json:"percent"`
	LastReadAt *time.Time `json:"last_read_at,omitempty"`
}

type Progress struct {
	BookID      int64     `json:"book_id"`
	Position    string    `json:"position,omitempty"`
	Page        int       `json:"page,omitempty"`
	Percent     float64   `json:"percent"`
	DeviceLabel string    `json:"device_label,omitempty"`
	UpdatedAt   time.Time `json:"updated_at"`
}

type BookListOptions struct {
	Query, Category, Sort, Direction string
	Page, Limit                      int
}
