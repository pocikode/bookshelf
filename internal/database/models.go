package database

import "time"

type Session struct {
	UserID          int64
	TokenHash       string
	PasswordBinding string
	CreatedAt       time.Time
	LastSeenAt      time.Time
	ExpiresAt       time.Time
	UserAgent       string
}

type User struct {
	ID        int64
	Username  string
	Role      string
	CreatedAt time.Time
	Disabled  bool
}

func (u User) IsAdmin() bool { return u.Role == "admin" }

type PasswordCredential struct {
	Digest    string
	UpdatedAt time.Time
}

type Book struct {
	ID         int64      `json:"id"`
	OwnerID    int64      `json:"owner_id"`
	Public     bool       `json:"public"`
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
	UserID      int64     `json:"user_id"`
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
	UserID                           int64
	Admin                            bool
}
