CREATE TABLE books (
    id INTEGER PRIMARY KEY,
    title TEXT NOT NULL,
    author TEXT,
    category TEXT NOT NULL DEFAULT 'Uncategorized',
    format TEXT NOT NULL CHECK (format IN ('epub','pdf')),
    file_hash TEXT NOT NULL UNIQUE,
    file_path TEXT NOT NULL,
    file_size INTEGER NOT NULL,
    cover_path TEXT,
    page_count INTEGER,
    language TEXT,
    publisher TEXT,
    created_at TEXT NOT NULL
);

CREATE TABLE reading_progress (
    book_id INTEGER PRIMARY KEY REFERENCES books(id) ON DELETE CASCADE,
    position TEXT,
    page INTEGER,
    percent REAL NOT NULL DEFAULT 0 CHECK (percent BETWEEN 0 AND 1),
    device_label TEXT,
    updated_at TEXT NOT NULL
);

CREATE TABLE sessions (
    token_hash TEXT PRIMARY KEY,
    password_binding TEXT NOT NULL,
    created_at TEXT NOT NULL,
    last_seen_at TEXT NOT NULL,
    expires_at TEXT NOT NULL,
    user_agent TEXT
);

CREATE INDEX books_category_idx ON books(category);
CREATE INDEX books_created_at_idx ON books(created_at);
CREATE INDEX reading_progress_updated_at_idx ON reading_progress(updated_at);
CREATE INDEX sessions_expires_at_idx ON sessions(expires_at);
