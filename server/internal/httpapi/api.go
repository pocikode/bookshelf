package httpapi

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
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
	"pocikode/bookshelf/server/internal/progress"
	"pocikode/bookshelf/server/internal/storage"
	"pocikode/bookshelf/server/internal/sync"

	"github.com/google/uuid"
)

type API struct {
	DB       *sql.DB
	Config   config.Config
	Files    storage.FileStore
	Books    books.Store
	Progress progress.Store
	Notes    *sql.DB
	Sync     sync.Store
}

func New(db *sql.DB, cfg config.Config) *API {
	return &API{DB: db, Config: cfg, Files: storage.FileStore{Root: cfg.BooksDir(), MaxUpload: cfg.MaxUpload}, Books: books.Store{DB: db}, Progress: progress.Store{DB: db}, Notes: db, Sync: sync.Store{DB: db}}
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
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.HasPrefix(r.URL.Path, "/api/") {
			mux.ServeHTTP(w, r)
			return
		}
		a.staticFile(w, r)
	})
}

type contextKey string

const sessionKey contextKey = "session"

func (a *API) withAuth(next func(http.ResponseWriter, *http.Request, auth.Session)) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		session, err := auth.LoadSession(a.DB, r)
		if err != nil {
			writeError(w, http.StatusUnauthorized, "unauthorized")
			return
		}
		if r.Method != http.MethodGet && r.Method != http.MethodHead && !auth.VerifyCSRF(session, r) {
			writeError(w, http.StatusForbidden, "csrf validation failed")
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
	if !decodeJSON(w, r, &input) || input.Username == "" || input.Password == "" {
		writeError(w, http.StatusBadRequest, "username and password are required")
		return
	}
	var user auth.User
	var hash string
	if err := a.DB.QueryRow(`SELECT id, username, password_hash FROM users WHERE username = ?`, input.Username).Scan(&user.ID, &user.Username, &hash); err != nil || !auth.CheckPassword(hash, input.Password) {
		writeError(w, http.StatusUnauthorized, "invalid credentials")
		return
	}
	session, err := auth.CreateSession(a.DB, user, a.Config.SecureCookie)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "could not create session")
		return
	}
	auth.SetCookie(w, session.Token, a.Config.SecureCookie)
	auth.SetCSRFCookie(w, session.CSRFToken, a.Config.SecureCookie)
	writeJSON(w, http.StatusOK, map[string]any{"user": user})
}

func (a *API) logout(w http.ResponseWriter, r *http.Request, session auth.Session) {
	_, _ = a.DB.Exec(`DELETE FROM sessions WHERE token_hash = ?`, session.TokenHash)
	auth.ClearCookie(w, a.Config.SecureCookie)
	w.WriteHeader(http.StatusNoContent)
}

func (a *API) me(w http.ResponseWriter, r *http.Request) {
	session, err := auth.LoadSession(a.DB, r)
	if err != nil {
		writeError(w, http.StatusUnauthorized, "unauthorized")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"user": session.User})
}

func userFrom(r *http.Request) auth.User {
	session, _ := r.Context().Value(sessionKey).(auth.Session)
	return session.User
}
func (a *API) listBooks(w http.ResponseWriter, r *http.Request, session auth.Session) {
	result, err := a.Books.List(session.User.ID, r.URL.Query().Get("q"))
	if err != nil {
		writeError(w, 500, "could not list books")
		return
	}
	writeJSON(w, 200, result)
}
func (a *API) getBook(w http.ResponseWriter, r *http.Request, session auth.Session) {
	book, err := a.Books.Get(session.User.ID, r.PathValue("id"))
	if errors.Is(err, sql.ErrNoRows) {
		writeError(w, 404, "book not found")
		return
	}
	if err != nil {
		writeError(w, 500, "could not load book")
		return
	}
	writeJSON(w, 200, book)
}

func (a *API) uploadBook(w http.ResponseWriter, r *http.Request, session auth.Session) {
	r.Body = http.MaxBytesReader(w, r.Body, a.Config.MaxUpload+1)
	if err := r.ParseMultipartForm(a.Config.MaxUpload); err != nil {
		writeError(w, 413, "invalid upload")
		return
	}
	file, header, err := r.FormFile("file")
	if err != nil {
		writeError(w, 400, "file is required")
		return
	}
	defer file.Close()
	uploaded, err := a.Files.Save(file, header.Filename, header.Header.Get("Content-Type"))
	if err != nil {
		writeError(w, 415, err.Error())
		return
	}
	metadata := json.RawMessage(r.FormValue("metadata"))
	if len(metadata) > 0 && !json.Valid(metadata) {
		_ = a.Files.Remove(uploaded.Path)
		writeError(w, 400, "metadata must be JSON")
		return
	}
	title := r.FormValue("title")
	if title == "" {
		title = strings.TrimSuffix(filepath.Base(header.Filename), filepath.Ext(header.Filename))
	}
	book, err := a.Books.Create(session.User.ID, books.Uploaded{Path: uploaded.Path, OriginalName: uploaded.OriginalName, MimeType: uploaded.MimeType, Size: uploaded.Size, Hash: uploaded.Hash}, title, r.FormValue("author"), metadata)
	if err != nil {
		_ = a.Files.Remove(uploaded.Path)
		writeError(w, 500, "could not save book")
		return
	}
	writeJSON(w, http.StatusCreated, book)
}

func (a *API) deleteBook(w http.ResponseWriter, r *http.Request, session auth.Session) {
	path, err := a.Books.Delete(session.User.ID, r.PathValue("id"))
	if errors.Is(err, sql.ErrNoRows) {
		writeError(w, 404, "book not found")
		return
	}
	if err != nil {
		writeError(w, 500, "could not delete book")
		return
	}
	_ = a.Files.Remove(path)
	w.WriteHeader(http.StatusNoContent)
}
func (a *API) bookFile(w http.ResponseWriter, r *http.Request, session auth.Session) {
	book, err := a.Books.Get(session.User.ID, r.PathValue("id"))
	if err != nil {
		writeError(w, 404, "book not found")
		return
	}
	file, err := os.Open(book.Path)
	if err != nil {
		writeError(w, 404, "book file not found")
		return
	}
	defer file.Close()
	info, _ := file.Stat()
	w.Header().Set("ETag", fmt.Sprintf("%q", book.Hash))
	w.Header().Set("Content-Type", book.MimeType)
	w.Header().Set("Accept-Ranges", "bytes")
	w.Header().Set("Cache-Control", "private, max-age=0")
	http.ServeContent(w, r, book.OriginalName, info.ModTime(), file)
}
func (a *API) bookCover(w http.ResponseWriter, _ *http.Request, _ auth.Session) {
	writeError(w, 404, "cover not available")
}

func (a *API) getProgress(w http.ResponseWriter, r *http.Request, session auth.Session) {
	value, err := a.Progress.Get(session.User.ID, r.PathValue("id"))
	if errors.Is(err, sql.ErrNoRows) {
		writeJSON(w, 200, nil)
		return
	}
	if err != nil {
		writeError(w, 500, "could not load progress")
		return
	}
	writeJSON(w, 200, value)
}
func (a *API) putProgress(w http.ResponseWriter, r *http.Request, session auth.Session) {
	var value progress.Progress
	if !decodeJSON(w, r, &value) {
		return
	}
	value.BookID = r.PathValue("id")
	saved, err := a.Progress.Upsert(session.User.ID, value.BookID, value)
	if err != nil {
		writeError(w, 400, err.Error())
		return
	}
	writeJSON(w, 200, saved)
}

func (a *API) listBookmarks(w http.ResponseWriter, r *http.Request, session auth.Session) {
	rows, err := annotations.Store{DB: a.DB}.ListBookmarks(session.User.ID, r.PathValue("id"))
	if err != nil {
		writeError(w, 500, "could not list bookmarks")
		return
	}
	writeJSON(w, 200, rows)
}
func (a *API) createBookmark(w http.ResponseWriter, r *http.Request, session auth.Session) {
	var value annotationsBookmark
	if !decodeJSON(w, r, &value) {
		return
	}
	row, err := annotations.Store{DB: a.DB}.CreateBookmark(session.User.ID, r.PathValue("id"), value.toModel())
	if err != nil {
		writeError(w, 400, err.Error())
		return
	}
	writeJSON(w, 201, row)
}
func (a *API) listAnnotations(w http.ResponseWriter, r *http.Request, session auth.Session) {
	rows, err := annotations.Store{DB: a.DB}.ListAnnotations(session.User.ID, r.PathValue("id"))
	if err != nil {
		writeError(w, 500, "could not list annotations")
		return
	}
	writeJSON(w, 200, rows)
}
func (a *API) createAnnotation(w http.ResponseWriter, r *http.Request, session auth.Session) {
	var value annotationsAnnotation
	if !decodeJSON(w, r, &value) {
		return
	}
	row, err := annotations.Store{DB: a.DB}.CreateAnnotation(session.User.ID, r.PathValue("id"), value.toModel())
	if err != nil {
		writeError(w, 400, err.Error())
		return
	}
	writeJSON(w, 201, row)
}

func (a *API) pullSync(w http.ResponseWriter, r *http.Request, session auth.Session) {
	cursor, _ := strconv.ParseInt(r.URL.Query().Get("cursor"), 10, 64)
	rows, next, err := a.Sync.Changes(session.User.ID, cursor, 500)
	if err != nil {
		writeError(w, 500, "could not read sync changes")
		return
	}
	writeJSON(w, 200, map[string]any{"cursor": next, "changes": rows})
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
		writeError(w, 500, "could not start sync")
		return
	}
	for _, change := range body.Changes {
		if change.Entity != "reading_progress" || change.Operation != "upsert" {
			_ = tx.Rollback()
			writeError(w, 400, "unsupported sync entity")
			return
		}
		var value progress.Progress
		if err := json.Unmarshal(change.Data, &value); err != nil || value.Progress < 0 || value.Progress > 1 || !json.Valid(value.Locator) {
			_ = tx.Rollback()
			writeError(w, 400, "invalid sync progress")
			return
		}
		now := time.Now().UnixMilli()
		_, err = tx.Exec(`INSERT INTO reading_progress (user_id, book_id, locator, progress, device_id, version, updated_at) VALUES (?, ?, ?, ?, ?, 1, ?) ON CONFLICT(user_id, book_id) DO UPDATE SET locator = excluded.locator, progress = excluded.progress, device_id = excluded.device_id, version = reading_progress.version + 1, updated_at = excluded.updated_at`, session.User.ID, change.EntityID, value.Locator, value.Progress, body.DeviceID, now)
		if err != nil {
			_ = tx.Rollback()
			writeError(w, 500, "could not apply sync change")
			return
		}
		var version int64
		if err := tx.QueryRow(`SELECT version FROM reading_progress WHERE user_id = ? AND book_id = ?`, session.User.ID, change.EntityID).Scan(&version); err != nil || a.Sync.Record(tx, session.User.ID, body.DeviceID, change.Entity, change.EntityID, change.Operation, version, value) != nil {
			_ = tx.Rollback()
			writeError(w, 500, "could not record sync change")
			return
		}
	}
	if err := tx.Commit(); err != nil {
		writeError(w, 500, "could not commit sync")
		return
	}
	changes, cursor, err := a.Sync.Changes(session.User.ID, body.Cursor, 500)
	if err != nil {
		writeError(w, 500, "could not read sync changes")
		return
	}
	writeJSON(w, 200, map[string]any{"cursor": cursor, "changes": changes, "deviceId": body.DeviceID})
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
func (a *API) runtimeConfig(w http.ResponseWriter, _ *http.Request) {
	w.Header().Set("Content-Type", "application/javascript; charset=utf-8")
	w.Header().Set("Cache-Control", "no-store, max-age=0")
	_, _ = w.Write([]byte("window.__READEST_RUNTIME_CONFIG={};\n"))
}

func (a *API) staticFile(w http.ResponseWriter, r *http.Request) {
	if r.URL.Path == "/runtime-config.js" {
		a.runtimeConfig(w, r)
		return
	}
	clean := filepath.Clean("/" + r.URL.Path)
	path := filepath.Join(a.Config.StaticDir, clean)
	if info, err := os.Stat(path); err == nil && !info.IsDir() {
		http.ServeFile(w, r, path)
		return
	}
	// The static export writes a route as `<route>.html`, and also as a
	// directory when the route has children — `/auth` is both `auth.html` and
	// `auth/` (holding `callback`, `recovery`, ...). Statting the path alone
	// therefore matched the directory, fell through to the SPA shell, and served
	// the root document for `/auth`: the app booted on `/` and rendered the
	// library instead of the login form.
	if page := path + ".html"; clean != "/" {
		if info, err := os.Stat(page); err == nil && !info.IsDir() {
			http.ServeFile(w, r, page)
			return
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
		http.Error(w, "frontend not built", 503)
		return
	}
	http.ServeFile(w, r, index)
}

func decodeJSON(w http.ResponseWriter, r *http.Request, value any) bool {
	r.Body = http.MaxBytesReader(w, r.Body, 2<<20)
	if err := json.NewDecoder(r.Body).Decode(value); err != nil {
		writeError(w, 400, "invalid JSON")
		return false
	}
	return true
}
func writeJSON(w http.ResponseWriter, status int, value any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(value)
}
func writeError(w http.ResponseWriter, status int, message string) {
	writeJSON(w, status, map[string]string{"error": message})
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
