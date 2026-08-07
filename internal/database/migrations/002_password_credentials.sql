CREATE TABLE password_credentials (
    id INTEGER PRIMARY KEY CHECK (id = 1),
    password_digest TEXT NOT NULL CHECK (length(password_digest) = 64),
    updated_at TEXT NOT NULL
);
