package httpapi

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"pocikode/bookshelf/server/internal/annotations"
	"pocikode/bookshelf/server/internal/auth"
	"pocikode/bookshelf/server/internal/books"
	"pocikode/bookshelf/server/internal/config"
	"pocikode/bookshelf/server/internal/logging"
	"pocikode/bookshelf/server/internal/progress"
	"pocikode/bookshelf/server/internal/storage"
	"pocikode/bookshelf/server/internal/sync"

	"github.com/google/uuid"
)

type API struct {
	DB          *sql.DB
	Config      config.Config
	Files       storage.FileStore
	Books       books.Store
	Progress    progress.Store
	Annotations annotations.Store
	Sync        sync.Store
	Logger      *slog.Logger
}

func New(db *sql.DB, cfg config.Config, logger *slog.Logger) *API {
	return &API{
		DB: db, Config: cfg,
		Files:       storage.FileStore{Root: cfg.BooksDir(), MaxUpload: cfg.MaxUpload},
		Books:       books.Store{DB: db},
		Progress:    progress.Store{DB: db},
		Annotations: annotations.Store{DB: db},
		Sync:        sync.Store{DB: db},
		Logger:      logger,
	}
}

// logger falls back to the default so an API built literally — as the tests
// do — still records events instead of panicking on a nil logger.
func (a *API) logger() *slog.Logger {
	if a.Logger != nil {
		return a.Logger
	}
	return slog.Default()
}

func (a *API) Handler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /api/health", func(w http.ResponseWriter, _ *http.Request) {
		writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
	})
	mux.HandleFunc("POST /api/auth/login", a.login)
	mux.HandleFunc("POST /api/auth/logout", a.withAuth(a.logout))
	mux.HandleFunc("GET /api/auth/me", a.me)
	mux.HandleFunc("GET /api/books", a.withAuth(a.listBooks))
	mux.HandleFunc("POST /api/books", a.withAuth(a.uploadBook))
	mux.HandleFunc("GET /api/books/{id}", a.withAuth(a.getBook))
	mux.HandleFunc("DELETE /api/books/{id}", a.withAuth(a.deleteBook))
	mux.HandleFunc("GET /api/books/{id}/file", a.withAuth(a.bookFile))
	mux.HandleFunc("GET /api/books/{id}/cover", a.withAuth(a.bookCover))
	mux.HandleFunc("GET /api/books/{id}/progress", a.withAuth(a.getProgress))
	mux.HandleFunc("PUT /api/books/{id}/progress", a.withAuth(a.putProgress))
	mux.HandleFunc("GET /api/books/{id}/bookmarks", a.withAuth(a.listBookmarks))
	mux.HandleFunc("POST /api/books/{id}/bookmarks", a.withAuth(a.createBookmark))
	mux.HandleFunc("GET /api/books/{id}/annotations", a.withAuth(a.listAnnotations))
	mux.HandleFunc("POST /api/books/{id}/annotations", a.withAuth(a.createAnnotation))
	mux.HandleFunc("GET /api/sync", a.withAuth(a.pullSync))
	mux.HandleFunc("POST /api/sync", a.withAuth(a.pushSync))
	root := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.HasPrefix(r.URL.Path, "/api/") {
			mux.ServeHTTP(w, r)
			return
		}
		a.staticFile(w, r)
	})
	// Every request — API and static alike — passes through the logging
	// middleware, so nothing reaches a handler without a request id and
	// nothing leaves without an access log line.
	return logging.Middleware(a.logger(), root)
}

type contextKey string

const sessionKey contextKey = "session"

func (a *API) withAuth(next func(http.ResponseWriter, *http.Request, auth.Session)) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		session, err := auth.LoadSession(a.DB, r)
		if err != nil {
			// A missing or expired session is routine; a lookup failure is an
			// outage wearing a 401 costume. Both are recorded, at the level
			// each deserves.
			if errors.Is(err, auth.ErrNoSession) {
				logEvent(r, slog.LevelWarn, "authentication rejected", err)
			} else {
				logEvent(r, slog.LevelError, "session lookup failed", err)
			}
			writeError(w, r, http.StatusUnauthorized, "unauthorized", nil)
			return
		}
		if r.Method != http.MethodGet && r.Method != http.MethodHead && !auth.VerifyCSRF(session, r) {
			writeError(w, r, http.StatusForbidden, "csrf validation failed", nil,
				slog.String("userId", session.User.ID))
			return
		}
		ctx := contextWithSession(r, session)
		next(w, ctx, session)
	}
}

func contextWithSession(r *http.Request, session auth.Session) *http.Request {
	return r.WithContext(context.WithValue(r.Context(), sessionKey, session))
}

func (a *API) login(w http.ResponseWriter, r *http.Request) {
	var input struct {
		Username string `json:"username"`
		Password string `json:"password"`
	}
	if !decodeJSON(w, r, &input) {
		return
	}
	if input.Username == "" || input.Password == "" {
		writeError(w, r, http.StatusBadRequest, "username and password are required", nil)
		return
	}
	var user auth.User
	var hash string
	err := a.DB.QueryRow(`SELECT id, username, password_hash FROM users WHERE username = ?`, input.Username).Scan(&user.ID, &user.Username, &hash)
	switch {
	case errors.Is(err, sql.ErrNoRows):
		// Unknown user and wrong password answer identically to the client,
		// but the log distinguishes them — and neither is confused with the
		// database being down, which the branch below now reports as a 500.
		logEvent(r, slog.LevelWarn, "login rejected: unknown user", nil,
			slog.String("username", input.Username))
		writeError(w, r, http.StatusUnauthorized, "invalid credentials", nil)
		return
	case err != nil:
		writeError(w, r, http.StatusInternalServerError, "could not sign in",
			fmt.Errorf("lookup user %q: %w", input.Username, err))
		return
	}
	if !auth.CheckPassword(hash, input.Password) {
		logEvent(r, slog.LevelWarn, "login rejected: bad password", nil,
			slog.String("username", input.Username), slog.String("userId", user.ID))
		writeError(w, r, http.StatusUnauthorized, "invalid credentials", nil)
		return
	}
	session, err := auth.CreateSession(a.DB, user, a.Config.SecureCookie)
	if err != nil {
		writeError(w, r, http.StatusInternalServerError, "could not create session", err,
			slog.String("userId", user.ID))
		return
	}
	auth.SetCookie(w, session.Token, a.Config.SecureCookie)
	auth.SetCSRFCookie(w, session.CSRFToken, a.Config.SecureCookie)
	logEvent(r, slog.LevelInfo, "login succeeded", nil,
		slog.String("userId", user.ID), slog.String("username", user.Username))
	writeJSON(w, http.StatusOK, map[string]any{"user": user})
}

func (a *API) logout(w http.ResponseWriter, r *http.Request, session auth.Session) {
	// The cookie is cleared regardless, but a session row that survives is a
	// security-relevant failure and must not be silent.
	if _, err := a.DB.Exec(`DELETE FROM sessions WHERE token_hash = ?`, session.TokenHash); err != nil {
		logEvent(r, slog.LevelError, "could not revoke session on logout", err,
			slog.String("userId", session.User.ID))
	}
	auth.ClearCookie(w, a.Config.SecureCookie)
	logEvent(r, slog.LevelInfo, "logout", nil, slog.String("userId", session.User.ID))
	w.WriteHeader(http.StatusNoContent)
}

func (a *API) me(w http.ResponseWriter, r *http.Request) {
	session, err := auth.LoadSession(a.DB, r)
	if err != nil {
		if errors.Is(err, auth.ErrNoSession) {
			logEvent(r, slog.LevelDebug, "anonymous identity check", err)
		} else {
			logEvent(r, slog.LevelError, "session lookup failed", err)
		}
		writeError(w, r, http.StatusUnauthorized, "unauthorized", nil)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"user": session.User})
}

func (a *API) listBooks(w http.ResponseWriter, r *http.Request, session auth.Session) {
	result, err := a.Books.List(session.User.ID, r.URL.Query().Get("q"))
	if err != nil {
		writeError(w, r, http.StatusInternalServerError, "could not list books", err,
			slog.String("userId", session.User.ID))
		return
	}
	writeJSON(w, 200, result)
}
func (a *API) getBook(w http.ResponseWriter, r *http.Request, session auth.Session) {
	id := r.PathValue("id")
	book, err := a.Books.Get(session.User.ID, id)
	if errors.Is(err, sql.ErrNoRows) {
		writeError(w, r, http.StatusNotFound, "book not found", nil,
			slog.String("userId", session.User.ID), slog.String("bookId", id))
		return
	}
	if err != nil {
		writeError(w, r, http.StatusInternalServerError, "could not load book", err,
			slog.String("userId", session.User.ID), slog.String("bookId", id))
		return
	}
	writeJSON(w, http.StatusOK, book)
}

func (a *API) uploadBook(w http.ResponseWriter, r *http.Request, session auth.Session) {
	r.Body = http.MaxBytesReader(w, r.Body, a.Config.MaxUpload+(20<<20)+(1<<20))
	if err := r.ParseMultipartForm(a.Config.MaxUpload); err != nil {
		var tooLarge *http.MaxBytesError
		status := http.StatusBadRequest
		if errors.As(err, &tooLarge) {
			status = http.StatusRequestEntityTooLarge
		}
		writeError(w, r, status, "invalid upload", err,
			slog.String("userId", session.User.ID))
		return
	}
	file, header, err := r.FormFile("file")
	if err != nil {
		writeError(w, r, http.StatusBadRequest, "file is required", err,
			slog.String("userId", session.User.ID))
		return
	}
	defer file.Close()
	uploaded, err := a.Files.Save(file, header.Filename, header.Header.Get("Content-Type"))
	if errors.Is(err, storage.ErrRejected) {
		writeError(w, r, http.StatusUnsupportedMediaType, err.Error(), nil,
			slog.String("userId", session.User.ID), slog.String("filename", header.Filename))
		return
	}
	if err != nil {
		// Storing the upload failed on our side; do not echo the path or the
		// syscall back to the client.
		writeError(w, r, http.StatusInternalServerError, "could not store upload", err,
			slog.String("userId", session.User.ID), slog.String("filename", header.Filename))
		return
	}
	cover, _, err := r.FormFile("cover")
	if err == nil {
		defer cover.Close()
		uploaded.CoverPath, err = a.Files.SaveCover(cover, uploaded.Path)
		if errors.Is(err, storage.ErrRejected) {
			a.removeFile(r, uploaded.Path)
			writeError(w, r, http.StatusUnsupportedMediaType, err.Error(), nil,
				slog.String("userId", session.User.ID), slog.String("filename", header.Filename))
			return
		}
		if err != nil {
			a.removeFile(r, uploaded.Path)
			writeError(w, r, http.StatusInternalServerError, "could not store cover", err,
				slog.String("userId", session.User.ID), slog.String("filename", header.Filename))
			return
		}
	} else if !errors.Is(err, http.ErrMissingFile) {
		a.removeFile(r, uploaded.Path)
		writeError(w, r, http.StatusBadRequest, "invalid cover", err,
			slog.String("userId", session.User.ID), slog.String("filename", header.Filename))
		return
	}
	metadata := json.RawMessage(r.FormValue("metadata"))
	if len(metadata) > 0 && !json.Valid(metadata) {
		a.removeFile(r, uploaded.Path)
		if uploaded.CoverPath != "" {
			a.removeFile(r, uploaded.CoverPath)
		}
		writeError(w, r, http.StatusBadRequest, "metadata must be JSON", nil,
			slog.String("userId", session.User.ID))
		return
	}
	title := r.FormValue("title")
	if title == "" {
		title = strings.TrimSuffix(filepath.Base(header.Filename), filepath.Ext(header.Filename))
	}
	book, err := a.Books.Create(session.User.ID, books.Uploaded{Path: uploaded.Path, CoverPath: uploaded.CoverPath, OriginalName: uploaded.OriginalName, MimeType: uploaded.MimeType, Size: uploaded.Size, Hash: uploaded.Hash}, title, r.FormValue("author"), metadata)
	if err != nil {
		a.removeFile(r, uploaded.Path)
		if uploaded.CoverPath != "" {
			a.removeFile(r, uploaded.CoverPath)
		}
		writeError(w, r, http.StatusInternalServerError, "could not save book", err,
			slog.String("userId", session.User.ID), slog.String("filename", header.Filename))
		return
	}
	logEvent(r, slog.LevelInfo, "book uploaded", nil,
		slog.String("userId", session.User.ID), slog.String("bookId", book.ID),
		slog.Int64("size", uploaded.Size))
	writeJSON(w, http.StatusCreated, book)
}

func (a *API) deleteBook(w http.ResponseWriter, r *http.Request, session auth.Session) {
	id := r.PathValue("id")
	book, err := a.Books.Delete(session.User.ID, id)
	if errors.Is(err, sql.ErrNoRows) {
		writeError(w, r, http.StatusNotFound, "book not found", nil,
			slog.String("userId", session.User.ID), slog.String("bookId", id))
		return
	}
	if err != nil {
		writeError(w, r, http.StatusInternalServerError, "could not delete book", err,
			slog.String("userId", session.User.ID), slog.String("bookId", id))
		return
	}
	// The row is already gone, so a failed unlink leaks a file on disk. The
	// request still succeeds, but the leak is now traceable.
	a.removeFile(r, book.Path)
	if book.CoverPath != "" {
		a.removeFile(r, book.CoverPath)
	}
	logEvent(r, slog.LevelInfo, "book deleted", nil,
		slog.String("userId", session.User.ID), slog.String("bookId", id))
	w.WriteHeader(http.StatusNoContent)
}
func (a *API) bookFile(w http.ResponseWriter, r *http.Request, session auth.Session) {
	id := r.PathValue("id")
	book, err := a.Books.Get(session.User.ID, id)
	if errors.Is(err, sql.ErrNoRows) {
		writeError(w, r, http.StatusNotFound, "book not found", nil,
			slog.String("userId", session.User.ID), slog.String("bookId", id))
		return
	}
	if err != nil {
		// This used to answer 404, so a database outage looked like a missing
		// book to both the client and, silently, to us.
		writeError(w, r, http.StatusInternalServerError, "could not load book", err,
			slog.String("userId", session.User.ID), slog.String("bookId", id))
		return
	}
	file, err := os.Open(book.Path)
	if errors.Is(err, os.ErrNotExist) {
		// A row without its file is a data-integrity problem, not a normal 404.
		writeError(w, r, http.StatusNotFound, "book file not found",
			fmt.Errorf("open book file: %w", err),
			slog.String("userId", session.User.ID), slog.String("bookId", id),
			slog.String("path", book.Path))
		return
	}
	if err != nil {
		writeError(w, r, http.StatusInternalServerError, "could not read book file",
			fmt.Errorf("open book file: %w", err),
			slog.String("userId", session.User.ID), slog.String("bookId", id),
			slog.String("path", book.Path))
		return
	}
	defer file.Close()
	info, err := file.Stat()
	if err != nil {
		writeError(w, r, http.StatusInternalServerError, "could not read book file",
			fmt.Errorf("stat book file %s: %w", book.Path, err),
			slog.String("userId", session.User.ID), slog.String("bookId", id))
		return
	}
	w.Header().Set("ETag", fmt.Sprintf("%q", book.Hash))
	w.Header().Set("Content-Type", book.MimeType)
	w.Header().Set("Accept-Ranges", "bytes")
	w.Header().Set("Cache-Control", "private, max-age=0")
	http.ServeContent(w, r, book.OriginalName, info.ModTime(), file)
}
func (a *API) bookCover(w http.ResponseWriter, r *http.Request, session auth.Session) {
	id := r.PathValue("id")
	book, err := a.Books.Get(session.User.ID, id)
	if errors.Is(err, sql.ErrNoRows) || (err == nil && book.CoverPath == "") {
		writeError(w, r, http.StatusNotFound, "cover not available", nil,
			slog.String("userId", session.User.ID), slog.String("bookId", id))
		return
	}
	if err != nil {
		writeError(w, r, http.StatusInternalServerError, "could not load cover", err,
			slog.String("userId", session.User.ID), slog.String("bookId", id))
		return
	}
	w.Header().Set("Cache-Control", "private, max-age=3600")
	http.ServeFile(w, r, book.CoverPath)
}

func (a *API) getProgress(w http.ResponseWriter, r *http.Request, session auth.Session) {
	id := r.PathValue("id")
	value, err := a.Progress.Get(session.User.ID, id)
	if errors.Is(err, sql.ErrNoRows) {
		writeJSON(w, http.StatusOK, nil)
		return
	}
	if err != nil {
		writeError(w, r, http.StatusInternalServerError, "could not load progress", err,
			slog.String("userId", session.User.ID), slog.String("bookId", id))
		return
	}
	writeJSON(w, http.StatusOK, value)
}
func (a *API) putProgress(w http.ResponseWriter, r *http.Request, session auth.Session) {
	var value progress.Progress
	if !decodeJSON(w, r, &value) {
		return
	}
	value.BookID = r.PathValue("id")
	saved, err := a.Progress.Upsert(session.User.ID, value.BookID, value)
	if errors.Is(err, progress.ErrInvalidProgress) {
		writeError(w, r, http.StatusBadRequest, err.Error(), nil,
			slog.String("userId", session.User.ID), slog.String("bookId", value.BookID))
		return
	}
	if err != nil {
		// A failed write used to be reported as a 400 with the raw error text.
		writeError(w, r, http.StatusInternalServerError, "could not save progress", err,
			slog.String("userId", session.User.ID), slog.String("bookId", value.BookID))
		return
	}
	writeJSON(w, http.StatusOK, saved)
}

func (a *API) listBookmarks(w http.ResponseWriter, r *http.Request, session auth.Session) {
	id := r.PathValue("id")
	rows, err := a.Annotations.ListBookmarks(session.User.ID, id)
	if err != nil {
		writeError(w, r, http.StatusInternalServerError, "could not list bookmarks", err,
			slog.String("userId", session.User.ID), slog.String("bookId", id))
		return
	}
	writeJSON(w, http.StatusOK, rows)
}
func (a *API) createBookmark(w http.ResponseWriter, r *http.Request, session auth.Session) {
	var value annotationsBookmark
	if !decodeJSON(w, r, &value) {
		return
	}
	id := r.PathValue("id")
	row, err := a.Annotations.CreateBookmark(session.User.ID, id, value.toModel())
	if errors.Is(err, annotations.ErrInvalidInput) {
		writeError(w, r, http.StatusBadRequest, err.Error(), nil,
			slog.String("userId", session.User.ID), slog.String("bookId", id))
		return
	}
	if err != nil {
		writeError(w, r, http.StatusInternalServerError, "could not create bookmark", err,
			slog.String("userId", session.User.ID), slog.String("bookId", id))
		return
	}
	writeJSON(w, http.StatusCreated, row)
}
func (a *API) listAnnotations(w http.ResponseWriter, r *http.Request, session auth.Session) {
	id := r.PathValue("id")
	rows, err := a.Annotations.ListAnnotations(session.User.ID, id)
	if err != nil {
		writeError(w, r, http.StatusInternalServerError, "could not list annotations", err,
			slog.String("userId", session.User.ID), slog.String("bookId", id))
		return
	}
	writeJSON(w, http.StatusOK, rows)
}
func (a *API) createAnnotation(w http.ResponseWriter, r *http.Request, session auth.Session) {
	var value annotationsAnnotation
	if !decodeJSON(w, r, &value) {
		return
	}
	id := r.PathValue("id")
	row, err := a.Annotations.CreateAnnotation(session.User.ID, id, value.toModel())
	if errors.Is(err, annotations.ErrInvalidInput) {
		writeError(w, r, http.StatusBadRequest, err.Error(), nil,
			slog.String("userId", session.User.ID), slog.String("bookId", id))
		return
	}
	if err != nil {
		writeError(w, r, http.StatusInternalServerError, "could not create annotation", err,
			slog.String("userId", session.User.ID), slog.String("bookId", id))
		return
	}
	writeJSON(w, http.StatusCreated, row)
}

func (a *API) pullSync(w http.ResponseWriter, r *http.Request, session auth.Session) {
	var cursor int64
	if raw := r.URL.Query().Get("cursor"); raw != "" {
		parsed, err := strconv.ParseInt(raw, 10, 64)
		if err != nil {
			// Silently resetting to 0 replays the entire history; refuse
			// instead, and record what the client sent.
			writeError(w, r, http.StatusBadRequest, "invalid cursor", err,
				slog.String("userId", session.User.ID), slog.String("cursor", raw))
			return
		}
		cursor = parsed
	}
	rows, next, err := a.Sync.Changes(session.User.ID, cursor, 500)
	if err != nil {
		writeError(w, r, http.StatusInternalServerError, "could not read sync changes", err,
			slog.String("userId", session.User.ID), slog.Int64("cursor", cursor))
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"cursor": next, "changes": rows})
}
func (a *API) pushSync(w http.ResponseWriter, r *http.Request, session auth.Session) {
	var body struct {
		DeviceID string `json:"deviceId"`
		Cursor   int64  `json:"cursor"`
		Changes  []struct {
			Entity    string          `json:"entity"`
			EntityID  string          `json:"entityId"`
			Operation string          `json:"operation"`
			Data      json.RawMessage `json:"data"`
		} `json:"changes"`
	}
	if !decodeJSON(w, r, &body) {
		return
	}
	if body.DeviceID == "" {
		body.DeviceID = uuid.NewString()
	}
	tx, err := a.DB.Begin()
	if err != nil {
		writeError(w, r, http.StatusInternalServerError, "could not start sync", err,
			slog.String("userId", session.User.ID))
		return
	}
	// Every early return below rolls back; rollbackTx logs a rollback that
	// itself fails, which would otherwise leave the connection wedged silently.
	for index, change := range body.Changes {
		attrs := []any{
			slog.String("userId", session.User.ID),
			slog.String("deviceId", body.DeviceID),
			slog.Int("changeIndex", index),
			slog.String("entity", change.Entity),
			slog.String("entityId", change.EntityID),
		}
		if change.Entity != "reading_progress" || change.Operation != "upsert" {
			rollbackTx(r, tx)
			writeError(w, r, http.StatusBadRequest, "unsupported sync entity", nil, attrs...)
			return
		}
		var value progress.Progress
		if err := json.Unmarshal(change.Data, &value); err != nil {
			rollbackTx(r, tx)
			writeError(w, r, http.StatusBadRequest, "invalid sync progress",
				fmt.Errorf("unmarshal sync change: %w", err), attrs...)
			return
		}
		if value.Progress < 0 || value.Progress > 1 || !json.Valid(value.Locator) {
			rollbackTx(r, tx)
			writeError(w, r, http.StatusBadRequest, "invalid sync progress", nil, attrs...)
			return
		}
		now := time.Now().UnixMilli()
		if _, err := tx.Exec(`INSERT INTO reading_progress (user_id, book_id, locator, progress, device_id, version, updated_at) VALUES (?, ?, ?, ?, ?, 1, ?) ON CONFLICT(user_id, book_id) DO UPDATE SET locator = excluded.locator, progress = excluded.progress, device_id = excluded.device_id, version = reading_progress.version + 1, updated_at = excluded.updated_at`, session.User.ID, change.EntityID, value.Locator, value.Progress, body.DeviceID, now); err != nil {
			rollbackTx(r, tx)
			writeError(w, r, http.StatusInternalServerError, "could not apply sync change",
				fmt.Errorf("upsert reading progress: %w", err), attrs...)
			return
		}
		var version int64
		if err := tx.QueryRow(`SELECT version FROM reading_progress WHERE user_id = ? AND book_id = ?`, session.User.ID, change.EntityID).Scan(&version); err != nil {
			rollbackTx(r, tx)
			writeError(w, r, http.StatusInternalServerError, "could not record sync change",
				fmt.Errorf("read applied version: %w", err), attrs...)
			return
		}
		if err := a.Sync.Record(tx, session.User.ID, body.DeviceID, change.Entity, change.EntityID, change.Operation, version, value); err != nil {
			rollbackTx(r, tx)
			writeError(w, r, http.StatusInternalServerError, "could not record sync change", err, attrs...)
			return
		}
	}
	if err := tx.Commit(); err != nil {
		writeError(w, r, http.StatusInternalServerError, "could not commit sync",
			fmt.Errorf("commit sync: %w", err),
			slog.String("userId", session.User.ID), slog.String("deviceId", body.DeviceID))
		return
	}
	changes, cursor, err := a.Sync.Changes(session.User.ID, body.Cursor, 500)
	if err != nil {
		writeError(w, r, http.StatusInternalServerError, "could not read sync changes", err,
			slog.String("userId", session.User.ID), slog.Int64("cursor", body.Cursor))
		return
	}
	logEvent(r, slog.LevelInfo, "sync applied", nil,
		slog.String("userId", session.User.ID), slog.String("deviceId", body.DeviceID),
		slog.Int("applied", len(body.Changes)), slog.Int("returned", len(changes)))
	writeJSON(w, http.StatusOK, map[string]any{"cursor": cursor, "changes": changes, "deviceId": body.DeviceID})
}

// runtimeConfig serves the `/runtime-config.js` that the app shell loads with
// `strategy='beforeInteractive'`. Upstream emits it from a Next.js route
// handler, but the personal build is a static export (`output: 'export'`), so
// no such file exists in `out/`. Without this route the request fell through to
// the SPA fallback below and the browser parsed `index.html` as JavaScript,
// failing with "expected expression, got '<'" before any app code ran.
//
// The personal deployment talks only to this server over same-origin `/api`,
// so an empty config is correct: it defines the global the consumers read and
// leaves every field unset, which is exactly the "fall back to build-time
// defaults" path they already handle.
func (a *API) runtimeConfig(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/javascript; charset=utf-8")
	w.Header().Set("Cache-Control", "no-store, max-age=0")
	if _, err := w.Write([]byte("window.__READEST_RUNTIME_CONFIG={};\n")); err != nil {
		logEvent(r, slog.LevelError, "could not write runtime config", err)
	}
}

func (a *API) staticFile(w http.ResponseWriter, r *http.Request) {
	if r.URL.Path == "/runtime-config.js" {
		a.runtimeConfig(w, r)
		return
	}
	if id := strings.TrimPrefix(r.URL.Path, "/reader/"); id != r.URL.Path && id != "" && !strings.Contains(id, "/") {
		page := filepath.Join(a.Config.StaticDir, "reader", "[ids].html")
		if info, err := os.Stat(page); err == nil && !info.IsDir() {
			http.ServeFile(w, r, page)
			return
		}
	}
	clean := filepath.Clean("/" + r.URL.Path)
	path := filepath.Join(a.Config.StaticDir, clean)
	info, err := os.Stat(path)
	switch {
	case err == nil && !info.IsDir():
		http.ServeFile(w, r, path)
		return
	case err != nil && !os.IsNotExist(err):
		// A permission or I/O problem is not a missing file; falling through
		// to the SPA shell would hide a broken deployment.
		logEvent(r, slog.LevelError, "could not stat static path", err, slog.String("path", path))
	}
	// The static export writes a route as `<route>.html`, and also as a
	// directory when the route has children — `/auth` is both `auth.html` and
	// `auth/` (holding `callback`, `recovery`, ...). Statting the path alone
	// therefore matched the directory, fell through to the SPA shell, and served
	// the root document for `/auth`: the app booted on `/` and rendered the
	// library instead of the login form.
	if page := path + ".html"; clean != "/" {
		pageInfo, err := os.Stat(page)
		switch {
		case err == nil && !pageInfo.IsDir():
			http.ServeFile(w, r, page)
			return
		case err != nil && !os.IsNotExist(err):
			logEvent(r, slog.LevelError, "could not stat static page", err, slog.String("path", page))
		}
	}
	// Never answer an asset request with the SPA shell. Returning HTML for a
	// missing `.js` surfaces as an opaque parse error in the browser; a 404 is
	// both correct and debuggable.
	if ext := filepath.Ext(r.URL.Path); ext != "" && ext != ".html" {
		http.NotFound(w, r)
		return
	}
	index := filepath.Join(a.Config.StaticDir, "index.html")
	if _, err := os.Stat(index); err != nil {
		logEvent(r, slog.LevelError, "SPA shell is missing", err, slog.String("path", index))
		http.Error(w, "frontend not built", http.StatusServiceUnavailable)
		return
	}
	http.ServeFile(w, r, index)
}

func decodeJSON(w http.ResponseWriter, r *http.Request, value any) bool {
	r.Body = http.MaxBytesReader(w, r.Body, 2<<20)
	if err := json.NewDecoder(r.Body).Decode(value); err != nil {
		writeError(w, r, http.StatusBadRequest, "invalid JSON", err)
		return false
	}
	return true
}

func writeJSON(w http.ResponseWriter, status int, value any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	if err := json.NewEncoder(w).Encode(value); err != nil {
		// The status line is already sent, so this cannot become a 500 —
		// recording it is the only remaining option, and better than nothing.
		slog.Default().Error("could not encode response body",
			slog.Int("status", status), slog.String("error", err.Error()))
	}
}

// writeError is the single exit for every failed request. It answers the
// client with a generic message and records the event, so no error response
// leaves the process without a matching log line. Pass cause when an
// underlying error exists; pass nil for pure client-side rejections, which
// are logged at warn rather than error.
func writeError(w http.ResponseWriter, r *http.Request, status int, message string, cause error, attrs ...any) {
	// Distinct from the middleware's access-log message so the two are easy to
	// separate: this line carries the cause and handler context, that one the
	// status and timing.
	level := slog.LevelWarn
	event := "error response"
	if status >= http.StatusInternalServerError {
		level = slog.LevelError
		event = "error response (server fault)"
	}
	attrs = append(attrs, slog.Int("status", status), slog.String("response", message))
	logEvent(r, level, event, cause, attrs...)
	writeJSON(w, status, map[string]string{"error": message})
}

// logEvent writes through the request-scoped logger, so the request id,
// method, and path are attached automatically.
func logEvent(r *http.Request, level slog.Level, message string, cause error, attrs ...any) {
	if cause != nil {
		attrs = append(attrs, slog.String("error", cause.Error()))
	}
	logging.From(r.Context()).Log(r.Context(), level, message, attrs...)
}

// removeFile deletes a stored book, logging the leak when it cannot.
func (a *API) removeFile(r *http.Request, path string) {
	if err := a.Files.Remove(path); err != nil {
		logEvent(r, slog.LevelError, "could not remove book file", err, slog.String("path", path))
	}
}

func rollbackTx(r *http.Request, tx *sql.Tx) {
	if err := tx.Rollback(); err != nil && !errors.Is(err, sql.ErrTxDone) {
		logEvent(r, slog.LevelError, "could not roll back transaction", err)
	}
}

// Small aliases keep HTTP decoding independent from the storage package's
// concrete types without duplicating persistence logic.
type annotationsBookmark struct {
	ID      string          `json:"id"`
	Locator json.RawMessage `json:"locator"`
	Title   string          `json:"title"`
}

func (v annotationsBookmark) toModel() annotations.Bookmark {
	return annotations.Bookmark{ID: v.ID, Locator: v.Locator, Title: v.Title}
}

type annotationsAnnotation struct {
	ID           string          `json:"id"`
	Type         string          `json:"type"`
	Locator      json.RawMessage `json:"locator"`
	SelectedText string          `json:"selectedText"`
	Note         string          `json:"note"`
	Metadata     json.RawMessage `json:"metadata"`
}

func (v annotationsAnnotation) toModel() annotations.Annotation {
	return annotations.Annotation{ID: v.ID, Type: v.Type, Locator: v.Locator, SelectedText: v.SelectedText, Note: v.Note, Metadata: v.Metadata}
}
