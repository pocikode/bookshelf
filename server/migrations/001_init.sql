PRAGMA journal_mode = WAL;
PRAGMA foreign_keys = ON;
PRAGMA busy_timeout = 5000;

CREATE TABLE IF NOT EXISTS schema_migrations (version INTEGER PRIMARY KEY, applied_at INTEGER NOT NULL);
CREATE TABLE IF NOT EXISTS users (id TEXT PRIMARY KEY, username TEXT NOT NULL UNIQUE, password_hash TEXT NOT NULL, role TEXT NOT NULL DEFAULT 'user' CHECK(role IN ('admin', 'user')), created_at INTEGER NOT NULL, updated_at INTEGER NOT NULL);
CREATE TABLE IF NOT EXISTS sessions (token_hash TEXT PRIMARY KEY, user_id TEXT NOT NULL REFERENCES users(id) ON DELETE CASCADE, csrf_hash TEXT NOT NULL, expires_at INTEGER NOT NULL, created_at INTEGER NOT NULL, last_seen_at INTEGER NOT NULL);
CREATE TABLE IF NOT EXISTS books (id TEXT PRIMARY KEY, user_id TEXT NOT NULL REFERENCES users(id) ON DELETE CASCADE, title TEXT NOT NULL, author TEXT NOT NULL DEFAULT '', metadata TEXT NOT NULL DEFAULT '{}', original_name TEXT NOT NULL, file_path TEXT NOT NULL UNIQUE, file_hash TEXT NOT NULL UNIQUE, mime_type TEXT NOT NULL, file_size INTEGER NOT NULL, cover_path TEXT, created_at INTEGER NOT NULL, updated_at INTEGER NOT NULL);
CREATE TABLE IF NOT EXISTS reading_progress (user_id TEXT NOT NULL REFERENCES users(id) ON DELETE CASCADE, book_id TEXT NOT NULL REFERENCES books(id) ON DELETE CASCADE, locator TEXT NOT NULL, progress REAL NOT NULL CHECK(progress >= 0 AND progress <= 1), device_id TEXT NOT NULL, version INTEGER NOT NULL, updated_at INTEGER NOT NULL, PRIMARY KEY(user_id, book_id));
CREATE TABLE IF NOT EXISTS bookmarks (id TEXT PRIMARY KEY, user_id TEXT NOT NULL REFERENCES users(id) ON DELETE CASCADE, book_id TEXT NOT NULL REFERENCES books(id) ON DELETE CASCADE, locator TEXT NOT NULL, title TEXT NOT NULL DEFAULT '', version INTEGER NOT NULL, deleted_at INTEGER, created_at INTEGER NOT NULL, updated_at INTEGER NOT NULL);
CREATE TABLE IF NOT EXISTS annotations (id TEXT PRIMARY KEY, user_id TEXT NOT NULL REFERENCES users(id) ON DELETE CASCADE, book_id TEXT NOT NULL REFERENCES books(id) ON DELETE CASCADE, type TEXT NOT NULL CHECK(type IN ('highlight','note')), locator TEXT NOT NULL, selected_text TEXT NOT NULL DEFAULT '', note TEXT NOT NULL DEFAULT '', metadata TEXT NOT NULL DEFAULT '{}', version INTEGER NOT NULL, deleted_at INTEGER, created_at INTEGER NOT NULL, updated_at INTEGER NOT NULL);
CREATE TABLE IF NOT EXISTS devices (user_id TEXT NOT NULL REFERENCES users(id) ON DELETE CASCADE, id TEXT NOT NULL, name TEXT NOT NULL DEFAULT '', last_seen INTEGER NOT NULL, created_at INTEGER NOT NULL, PRIMARY KEY(user_id, id));
CREATE TABLE IF NOT EXISTS sync_changes (id INTEGER PRIMARY KEY AUTOINCREMENT, user_id TEXT NOT NULL REFERENCES users(id) ON DELETE CASCADE, device_id TEXT NOT NULL, entity_type TEXT NOT NULL, entity_id TEXT NOT NULL, operation TEXT NOT NULL, version INTEGER NOT NULL, payload TEXT NOT NULL, created_at INTEGER NOT NULL);
CREATE INDEX IF NOT EXISTS sync_changes_user_cursor ON sync_changes(user_id, id);
CREATE INDEX IF NOT EXISTS books_search ON books(user_id, title, author);
