CREATE TABLE users (
    id INTEGER PRIMARY KEY,
    username TEXT NOT NULL UNIQUE COLLATE NOCASE,
    password_digest TEXT NOT NULL CHECK (length(password_digest) = 64),
    role TEXT NOT NULL CHECK (role IN ('admin','user')),
    disabled INTEGER NOT NULL DEFAULT 0 CHECK (disabled IN (0,1)),
    created_at TEXT NOT NULL
);

INSERT INTO users(id, username, password_digest, role, created_at)
SELECT 1, 'admin', password_digest, 'admin', updated_at FROM password_credentials WHERE id=1;
INSERT INTO users(id, username, password_digest, role, created_at)
SELECT 1, 'admin', '0000000000000000000000000000000000000000000000000000000000000000', 'admin', strftime('%Y-%m-%dT%H:%M:%fZ','now')
WHERE NOT EXISTS (SELECT 1 FROM users WHERE id=1);

ALTER TABLE sessions ADD COLUMN user_id INTEGER REFERENCES users(id) ON DELETE CASCADE;
UPDATE sessions SET user_id=1 WHERE user_id IS NULL;
CREATE INDEX sessions_user_idx ON sessions(user_id);

ALTER TABLE books ADD COLUMN owner_id INTEGER REFERENCES users(id) ON DELETE SET NULL;
ALTER TABLE books ADD COLUMN is_public INTEGER NOT NULL DEFAULT 1 CHECK (is_public IN (0,1));
UPDATE books SET owner_id=1 WHERE owner_id IS NULL;
CREATE INDEX books_owner_idx ON books(owner_id);

ALTER TABLE reading_progress RENAME TO reading_progress_old;
CREATE TABLE reading_progress (
    user_id INTEGER NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    book_id INTEGER NOT NULL REFERENCES books(id) ON DELETE CASCADE,
    position TEXT,
    page INTEGER,
    percent REAL NOT NULL DEFAULT 0 CHECK (percent BETWEEN 0 AND 1),
    device_label TEXT,
    updated_at TEXT NOT NULL,
    PRIMARY KEY (user_id, book_id)
);
INSERT INTO reading_progress(user_id, book_id, position, page, percent, device_label, updated_at)
SELECT 1, book_id, position, page, percent, device_label, updated_at FROM reading_progress_old;
DROP TABLE reading_progress_old;
CREATE INDEX reading_progress_updated_at_idx ON reading_progress(updated_at);
