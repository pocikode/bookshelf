CREATE TABLE bookmarks (
    id INTEGER PRIMARY KEY,
    user_id INTEGER NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    book_id INTEGER NOT NULL REFERENCES books(id) ON DELETE CASCADE,
    position TEXT NOT NULL,
    page INTEGER,
    label TEXT NOT NULL,
    percent REAL NOT NULL DEFAULT 0 CHECK (percent BETWEEN 0 AND 1),
    created_at TEXT NOT NULL
);

CREATE UNIQUE INDEX bookmarks_position_idx ON bookmarks(user_id, book_id, position);
CREATE INDEX bookmarks_book_idx ON bookmarks(user_id, book_id, created_at);
