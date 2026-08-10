package web

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"html/template"
	"io"
	"log/slog"
	"math"
	"mime"
	"net/http"
	"net/url"
	"os"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/go-chi/chi/v5"
	"pocikode/bookshelf/internal/auth"
	"pocikode/bookshelf/internal/config"
	"pocikode/bookshelf/internal/database"
	"pocikode/bookshelf/internal/library"
	"pocikode/bookshelf/internal/progress"
	"pocikode/bookshelf/internal/ratelimit"
)

const themeScript = `(()=>{try{const t=localStorage.getItem("bookshelf:v1:theme");const d=t==="dark"||(t!=="light"&&matchMedia("(prefers-color-scheme: dark)").matches);document.documentElement.classList.toggle("dark",d)}catch{}})();`

type Dependencies struct {
	Config     config.Config
	Repository *database.Repository
	Auth       *auth.Service
	Limiter    *ratelimit.Limiter
	Library    *library.Service
	Progress   *progress.Service
	Logger     *slog.Logger
}
type Server struct {
	dep       Dependencies
	assets    *Assets
	templates *template.Template
	flash     *flashStore
	csp       string
}
type contextKey int

const sessionKey contextKey = 1
const userKey contextKey = 2

func NewServer(dep Dependencies) (http.Handler, error) {
	assets, err := LoadAssets()
	if err != nil {
		return nil, err
	}
	if err = assets.Require("app.css", "reader.js", "upload.js", "pdf.worker.js", "favicon.svg"); err != nil {
		return nil, err
	}
	funcs := template.FuncMap{"asset": assets.URL, "percent": func(v float64) string { return strconv.FormatFloat(math.Max(0, math.Min(100, v*100)), 'f', 1, 64) }}
	templates, err := template.New("pages").Funcs(funcs).ParseFS(Embedded(), "templates/*.html")
	if err != nil {
		return nil, fmt.Errorf("parse templates: %w", err)
	}
	sum := sha256.Sum256([]byte(themeScript))
	s := &Server{dep: dep, assets: assets, templates: templates, flash: newFlashStore(), csp: "default-src 'self'; script-src 'self' 'sha256-" + base64.StdEncoding.EncodeToString(sum[:]) + "'; style-src 'self' 'unsafe-inline'; img-src 'self' data: blob:; connect-src 'self'; worker-src 'self' blob:; frame-src 'self' blob:; object-src 'none'; base-uri 'self'; form-action 'self'; frame-ancestors 'none'"}
	return s.routes(), nil
}

func (s *Server) routes() http.Handler {
	r := chi.NewRouter()
	r.Use(s.requestLog, s.recover, s.securityHeaders)
	r.Get("/healthz", func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "text/plain; charset=utf-8")
		w.Header().Set("Cache-Control", "no-store")
		w.WriteHeader(http.StatusOK)
		_, _ = io.WriteString(w, "ok\n")
	})
	r.Handle("/assets/*", s.assets)
	r.Get("/login", s.loginPage)
	r.Post("/login", s.login)
	r.Group(func(h chi.Router) {
		h.Use(s.requireHTML)
		h.Get("/", s.libraryPage)
		h.Get("/settings", s.settings)
		h.Post("/settings/password", s.changePassword)
		h.Post("/logout", s.logout)
		h.Post("/logout/all", s.logoutAll)
		h.Get("/books/{id}/read", s.readerPage)
		h.Get("/admin/users", s.usersPage)
		h.Post("/admin/users", s.createUser)
		h.Post("/admin/users/{id}/password", s.resetUserPassword)
	})
	r.Route("/api", func(api chi.Router) {
		api.Use(s.requireAPI)
		api.Post("/books", s.upload)
		api.Route("/books/{id}", func(b chi.Router) {
			b.Patch("/", s.mutateBook)
			b.Delete("/", s.mutateBook)
			b.Post("/", s.mutateBook)
			b.Get("/file", s.bookFile)
			b.Get("/cover", s.coverFile)
			b.Get("/progress", s.getProgress)
			b.Put("/progress", s.saveProgress)
			b.Post("/progress", s.saveProgress)
		})
	})
	return r
}

func (s *Server) securityHeaders(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Security-Policy", s.csp)
		w.Header().Set("X-Content-Type-Options", "nosniff")
		w.Header().Set("Referrer-Policy", "same-origin")
		next.ServeHTTP(w, r)
	})
}

type responseRecorder struct {
	http.ResponseWriter
	status, bytes int
}

func (r *responseRecorder) WriteHeader(status int) {
	r.status = status
	r.ResponseWriter.WriteHeader(status)
}
func (r *responseRecorder) Write(p []byte) (int, error) {
	if r.status == 0 {
		r.WriteHeader(http.StatusOK)
	}
	n, err := r.ResponseWriter.Write(p)
	r.bytes += n
	return n, err
}
func (s *Server) requestLog(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		start := time.Now()
		id := requestID()
		w.Header().Set("X-Request-ID", id)
		rec := &responseRecorder{ResponseWriter: w}
		next.ServeHTTP(rec, r)
		status := rec.status
		if status == 0 {
			status = 200
		}
		s.dep.Logger.Info("http_request", "event", "http_request", "request_id", id, "route", r.Pattern, "method", r.Method, "status", status, "duration_ms", time.Since(start).Milliseconds(), "client_ip", auth.ClientIP(r, s.dep.Config.TrustProxy))
	})
}
func (s *Server) recover(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		defer func() {
			if recovered := recover(); recovered != nil {
				s.dep.Logger.Error("request_panic", "event", "request_panic", "error", fmt.Sprint(recovered))
				s.jsonError(w, 500, "internal_error", "Unexpected server failure")
			}
		}()
		next.ServeHTTP(w, r)
	})
}
func requestID() string {
	var b [12]byte
	if _, err := rand.Read(b[:]); err == nil {
		return hex.EncodeToString(b[:])
	}
	return strconv.FormatInt(time.Now().UnixNano(), 36)
}

func (s *Server) requireHTML(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		session, ok := s.resolve(r)
		if !ok {
			target := r.URL.RequestURI()
			if !safeReturn(target) {
				target = "/"
			}
			http.Redirect(w, r, "/login?return_to="+url.QueryEscape(target), http.StatusSeeOther)
			return
		}
		user, err := s.dep.Repository.FindUser(r.Context(), session.UserID)
		if err != nil || user.Disabled {
			s.redirectLogin(w, r)
			return
		}
		ctx := context.WithValue(r.Context(), sessionKey, session)
		ctx = context.WithValue(ctx, userKey, user)
		next.ServeHTTP(w, r.WithContext(ctx))
	})
}
func (s *Server) requireAPI(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		session, ok := s.resolve(r)
		if !ok {
			s.jsonError(w, 401, "unauthenticated", "Authentication required")
			return
		}
		user, err := s.dep.Repository.FindUser(r.Context(), session.UserID)
		if err != nil || user.Disabled {
			s.jsonError(w, 401, "unauthenticated", "Authentication required")
			return
		}
		ctx := context.WithValue(r.Context(), sessionKey, session)
		ctx = context.WithValue(ctx, userKey, user)
		next.ServeHTTP(w, r.WithContext(ctx))
	})
}
func (s *Server) resolve(r *http.Request) (auth.Session, bool) {
	cookie, err := r.Cookie(auth.CookieName)
	if err != nil {
		return auth.Session{}, false
	}
	ctx, cancel := context.WithTimeout(r.Context(), 3*time.Second)
	defer cancel()
	session, err := s.dep.Auth.Resolve(ctx, cookie.Value)
	return session, err == nil
}
func currentSession(r *http.Request) auth.Session {
	return r.Context().Value(sessionKey).(auth.Session)
}
func currentUser(r *http.Request) database.User { return r.Context().Value(userKey).(database.User) }
func (s *Server) redirectLogin(w http.ResponseWriter, r *http.Request) {
	http.Redirect(w, r, "/login?return_to="+url.QueryEscape(safeReturnValue(r.URL.RequestURI())), http.StatusSeeOther)
}

func (s *Server) loginPage(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Cache-Control", "no-store")
	s.render(w, "login", map[string]any{"ReturnTo": safeReturnValue(r.URL.Query().Get("return_to"))})
}
func (s *Server) login(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Cache-Control", "no-store")
	if !auth.SameOrigin(r, s.dep.Config.TrustProxy) {
		s.loginFailure(w, r, "Sign-in failed", http.StatusForbidden, 0)
		return
	}
	ip := auth.ClientIP(r, s.dep.Config.TrustProxy)
	if retry, limited := s.dep.Limiter.Check(ip); limited {
		s.loginFailure(w, r, "Too many attempts. Please wait before trying again.", 429, ratelimit.RetryAfter(retry))
		return
	}
	r.Body = http.MaxBytesReader(w, r.Body, 64<<10)
	if err := r.ParseForm(); err != nil {
		s.loginFailure(w, r, "Sign-in failed", 400, 0)
		return
	}
	success, retry, limited, state := s.dep.Limiter.Attempt(ip, func() bool {
		_, err := s.dep.Auth.Authenticate(r.Context(), strings.TrimSpace(r.FormValue("username")), r.FormValue("password"))
		return err == nil
	})
	if limited {
		s.loginFailure(w, r, "Too many attempts. Please wait before trying again.", 429, ratelimit.RetryAfter(retry))
		return
	}
	if !success {
		s.dep.Logger.Warn("login_failed", "event", "login_failed", "client_ip", ip, "ip_failures", state.IPFailures, "global_failures", state.GlobalFailures, "lock_level", state.LockLevel)
		s.loginFailure(w, r, "Sign-in failed", 401, 0)
		return
	}
	user, err := s.dep.Auth.Authenticate(r.Context(), strings.TrimSpace(r.FormValue("username")), r.FormValue("password"))
	if err != nil {
		s.loginFailure(w, r, "Sign-in failed", 401, 0)
		return
	}
	session, err := s.dep.Auth.CreateForUser(r.Context(), user.ID, r.UserAgent())
	if err != nil {
		s.jsonError(w, 500, "internal_error", "Unexpected server failure")
		return
	}
	auth.SetCookie(w, session, auth.IsSecure(r, s.dep.Config.TrustProxy), time.Duration(s.dep.Config.SessionDays)*24*time.Hour)
	s.dep.Logger.Info("login_succeeded", "event", "login_succeeded", "client_ip", ip)
	target := safeReturnValue(r.FormValue("return_to"))
	http.Redirect(w, r, target, http.StatusSeeOther)
}
func (s *Server) loginFailure(w http.ResponseWriter, r *http.Request, message string, status, retry int) {
	if retry > 0 {
		w.Header().Set("Retry-After", strconv.Itoa(retry))
	}
	w.WriteHeader(status)
	returnTo := r.URL.Query().Get("return_to")
	if r.Form != nil {
		returnTo = r.Form.Get("return_to")
	}
	s.render(w, "login", map[string]any{
		"Error":    message,
		"ReturnTo": safeReturnValue(returnTo),
		"Username": r.FormValue("username"),
	})
}
func safeReturnValue(v string) string {
	if safeReturn(v) {
		return v
	}
	return "/"
}
func safeReturn(v string) bool {
	u, err := url.Parse(v)
	return err == nil && strings.HasPrefix(v, "/") && !strings.HasPrefix(v, "//") && u.Host == ""
}

type libraryView struct {
	ThemeScript           template.JS
	CSRF                  string
	Books, Continue       []bookView
	Categories            []string
	Flash                 []UploadResult
	Query, Category, Sort string
	Total, Page, Pages    int
	User                  database.User
	PrevURL, NextURL      string
}

type bookView struct {
	ID                      int64
	Title, Author, Category string
	HasCover                bool
	Percent                 float64
	CSRF                    string
	OwnerID                 int64
	Public                  bool
	CanManage               bool
}

func projectBooks(books []database.Book, csrf string, user database.User) []bookView {
	out := make([]bookView, 0, len(books))
	for _, book := range books {
		out = append(out, bookView{ID: book.ID, Title: book.Title, Author: book.Author, Category: book.Category, HasCover: book.CoverPath != "", Percent: book.Percent, CSRF: csrf, OwnerID: book.OwnerID, Public: book.Public, CanManage: user.IsAdmin() || book.OwnerID == user.ID})
	}
	return out
}

func (s *Server) libraryPage(w http.ResponseWriter, r *http.Request) {
	q := strings.TrimSpace(r.URL.Query().Get("q"))
	if utf8.RuneCountInString(q) > 200 {
		s.htmlError(w, 400, "Search is too long")
		return
	}
	page, _ := strconv.Atoi(r.URL.Query().Get("page"))
	if page < 1 {
		page = 1
	}
	sortKey := r.URL.Query().Get("sort")
	if sortKey == "" {
		sortKey = "added"
	}
	opts := database.BookListOptions{Query: q, Category: r.URL.Query().Get("category"), Sort: sortKey, Direction: r.URL.Query().Get("direction"), Page: page, Limit: 60}
	user := currentUser(r)
	opts.UserID, opts.Admin = user.ID, user.IsAdmin()
	ctx, cancel := context.WithTimeout(r.Context(), 3*time.Second)
	defer cancel()
	books, total, err := s.dep.Repository.ListBooks(ctx, opts)
	if err != nil {
		s.htmlError(w, 500, "Library unavailable")
		return
	}
	continued, _ := s.dep.Repository.ContinueReadingForUser(ctx, user.ID, user.IsAdmin())
	categories, _ := s.dep.Repository.CategoriesForUser(ctx, user.ID, user.IsAdmin())
	pages := maxInt(1, (total+59)/60)
	session := currentSession(r)
	csrf := s.dep.Auth.CSRFToken(session)
	view := libraryView{CSRF: csrf, Books: projectBooks(books, csrf, user), Continue: projectBooks(continued, csrf, user), Categories: categories, Flash: s.flash.Take(session.TokenHash), Query: q, Category: opts.Category, Sort: sortKey, Total: total, Page: page, Pages: pages, User: user}
	if page > 1 {
		view.PrevURL = pageURL(r, page-1)
	}
	if page < pages {
		view.NextURL = pageURL(r, page+1)
	}
	if r.Header.Get("X-Requested-With") == "fetch" {
		s.render(w, "library-results", view)
		return
	}
	s.render(w, "library", view)
}
func pageURL(r *http.Request, page int) string {
	q := r.URL.Query()
	q.Set("page", strconv.Itoa(page))
	return "/?" + q.Encode()
}
func (s *Server) settings(w http.ResponseWriter, r *http.Request) {
	s.render(w, "settings", map[string]any{"CSRF": s.dep.Auth.CSRFToken(currentSession(r)), "User": currentUser(r)})
}

func (s *Server) requireAdmin(w http.ResponseWriter, r *http.Request) bool {
	if !currentUser(r).IsAdmin() {
		s.htmlError(w, http.StatusForbidden, "Administrator access required")
		return false
	}
	return true
}
func validUsername(v string) bool {
	if utf8.RuneCountInString(v) < 1 || utf8.RuneCountInString(v) > 64 {
		return false
	}
	for _, r := range v {
		if !(r == '-' || r == '_' || r >= 'a' && r <= 'z' || r >= 'A' && r <= 'Z' || r >= '0' && r <= '9') {
			return false
		}
	}
	return true
}
func passwordDigest(password string) string {
	digest := sha256.Sum256([]byte(password))
	return hex.EncodeToString(digest[:])
}
func (s *Server) usersPage(w http.ResponseWriter, r *http.Request) {
	if !s.requireAdmin(w, r) {
		return
	}
	users, err := s.dep.Repository.ListUsers(r.Context())
	if err != nil {
		s.htmlError(w, 500, "Users unavailable")
		return
	}
	s.render(w, "users", map[string]any{"CSRF": s.dep.Auth.CSRFToken(currentSession(r)), "Users": users, "User": currentUser(r)})
}
func (s *Server) createUser(w http.ResponseWriter, r *http.Request) {
	if !s.requireAdmin(w, r) || !s.validFormCSRF(w, r) {
		return
	}
	username, password := strings.TrimSpace(r.FormValue("username")), r.FormValue("password")
	if !validUsername(username) || utf8.RuneCountInString(password) < auth.MinPasswordLength {
		s.htmlError(w, 400, "Username or password is invalid")
		return
	}
	if _, err := s.dep.Repository.CreateUser(r.Context(), username, passwordDigest(password), "user", time.Now().UTC()); err != nil {
		s.htmlError(w, 400, "Username is already in use")
		return
	}
	http.Redirect(w, r, "/admin/users", http.StatusSeeOther)
}
func (s *Server) resetUserPassword(w http.ResponseWriter, r *http.Request) {
	if !s.requireAdmin(w, r) || !s.validFormCSRF(w, r) {
		return
	}
	id, err := strconv.ParseInt(chi.URLParam(r, "id"), 10, 64)
	if err != nil || id < 1 || utf8.RuneCountInString(r.FormValue("password")) < auth.MinPasswordLength {
		s.htmlError(w, 400, "Invalid password or user")
		return
	}
	if _, err = s.dep.Repository.FindUser(r.Context(), id); database.IsNotFound(err) {
		s.htmlError(w, 404, "User not found")
		return
	}
	if err = s.dep.Repository.SetUserPassword(r.Context(), id, passwordDigest(r.FormValue("password"))); err != nil {
		s.htmlError(w, 500, "Password could not be changed")
		return
	}
	_ = s.dep.Repository.DeleteUserSessions(r.Context(), id)
	http.Redirect(w, r, "/admin/users", http.StatusSeeOther)
}
func (s *Server) changePassword(w http.ResponseWriter, r *http.Request) {
	if !s.validFormCSRF(w, r) {
		return
	}
	current, next, confirmation := r.FormValue("current_password"), r.FormValue("new_password"), r.FormValue("new_password_confirmation")
	if next != confirmation {
		s.htmlError(w, http.StatusBadRequest, "New passwords do not match")
		return
	}
	if err := s.dep.Auth.ChangeUserPassword(r.Context(), currentUser(r).ID, current, next); err != nil {
		switch {
		case errors.Is(err, auth.ErrInvalidCurrentPassword):
			s.htmlError(w, http.StatusBadRequest, "Current password is incorrect")
		case errors.Is(err, auth.ErrPasswordTooShort):
			s.htmlError(w, http.StatusBadRequest, fmt.Sprintf("New password must be at least %d characters", auth.MinPasswordLength))
		default:
			s.dep.Logger.Error("password_change_failed", "event", "password_change_failed", "error", err)
			s.htmlError(w, http.StatusInternalServerError, "Password could not be changed")
		}
		return
	}
	auth.ClearCookie(w, auth.IsSecure(r, s.dep.Config.TrustProxy))
	http.Redirect(w, r, "/login", http.StatusSeeOther)
}
func (s *Server) logout(w http.ResponseWriter, r *http.Request) {
	if !s.validFormCSRF(w, r) {
		return
	}
	session := currentSession(r)
	_ = s.dep.Auth.Delete(r.Context(), session)
	auth.ClearCookie(w, auth.IsSecure(r, s.dep.Config.TrustProxy))
	http.Redirect(w, r, "/login", http.StatusSeeOther)
}
func (s *Server) logoutAll(w http.ResponseWriter, r *http.Request) {
	if !s.validFormCSRF(w, r) {
		return
	}
	_ = s.dep.Auth.DeleteAll(r.Context())
	auth.ClearCookie(w, auth.IsSecure(r, s.dep.Config.TrustProxy))
	http.Redirect(w, r, "/login", http.StatusSeeOther)
}
func (s *Server) validFormCSRF(w http.ResponseWriter, r *http.Request) bool {
	if !auth.SameOrigin(r, s.dep.Config.TrustProxy) {
		s.htmlError(w, 403, "Request could not be verified")
		return false
	}
	r.Body = http.MaxBytesReader(w, r.Body, 64<<10)
	if err := r.ParseForm(); err != nil || !s.dep.Auth.ValidCSRF(currentSession(r), r.FormValue("csrf_token")) {
		s.htmlError(w, 403, "Request could not be verified")
		return false
	}
	return true
}

var indexedPart = regexp.MustCompile(`^(books|covers)\[(\d{1,2})\]$`)

type stagedResult struct {
	book library.StagedBook
	err  error
}

func (s *Server) upload(w http.ResponseWriter, r *http.Request) {
	if !auth.SameOrigin(r, s.dep.Config.TrustProxy) {
		s.jsonError(w, 403, "csrf_failed", "Request could not be verified")
		return
	}
	session := currentSession(r)
	headerToken := r.Header.Get("X-CSRF-Token")
	verified := headerToken != "" && s.dep.Auth.ValidCSRF(session, headerToken)
	maxRequest := int64(math.MaxInt64)
	const multipartOverhead = int64(101 << 20)
	if s.dep.Config.MaxUploadBytes <= (math.MaxInt64-multipartOverhead)/20 {
		maxRequest = s.dep.Config.MaxUploadBytes*20 + multipartOverhead
	}
	r.Body = http.MaxBytesReader(w, r.Body, maxRequest)
	reader, err := r.MultipartReader()
	if err != nil {
		s.jsonError(w, 400, "invalid_multipart", "A multipart upload is required")
		return
	}
	staged := map[int]stagedResult{}
	covers := map[int]string{}
	category := ""
	public := true
	nextIndex := 0
	cleanup := func() {
		for _, item := range staged {
			item.book.Cleanup()
		}
		for _, name := range covers {
			_ = os.Remove(name)
		}
	}
	defer cleanup()
	for {
		part, err := reader.NextPart()
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			var tooLarge *http.MaxBytesError
			if errors.As(err, &tooLarge) {
				s.jsonError(w, 413, "request_too_large", "Upload request is too large")
				return
			}
			s.jsonError(w, 400, "invalid_multipart", "Malformed multipart upload")
			return
		}
		name := part.FormName()
		if name == "csrf_token" {
			value, e := readText(part)
			if e != nil {
				s.jsonError(w, 400, "invalid_field", "Invalid CSRF field")
				return
			}
			verified = s.dep.Auth.ValidCSRF(session, value)
			continue
		}
		if !verified {
			s.jsonError(w, 403, "csrf_failed", "Request could not be verified")
			return
		}
		if name == "category" {
			category, err = readText(part)
			if err != nil {
				s.jsonError(w, 400, "invalid_field", "Category is too large")
				return
			}
			continue
		}
		if name == "private" {
			value, e := readText(part)
			if e != nil {
				s.jsonError(w, 400, "invalid_field", "Invalid visibility field")
				return
			}
			public = value != "true" && value != "1"
			continue
		}
		kind, index, ok := parsePartName(name)
		if !ok {
			_, _ = io.Copy(io.Discard, io.LimitReader(part, 64<<10))
			continue
		}
		if name == "books" {
			kind = "books"
			index = nextIndex
			nextIndex++
		}
		if index < 0 || index >= 20 {
			s.jsonError(w, 400, "too_many_books", "At most 20 books may be uploaded")
			return
		}
		if kind == "books" {
			if _, exists := staged[index]; exists {
				s.jsonError(w, 400, "duplicate_index", "Duplicate book index")
				return
			}
			book, stageErr := s.dep.Library.Stage(index, part.FileName(), part)
			staged[index] = stagedResult{book: book, err: stageErr}
			if len(staged) > 20 {
				s.jsonError(w, 400, "too_many_books", "At most 20 books may be uploaded")
				return
			}
		} else {
			cover, coverErr := s.dep.Library.StageCover(part)
			if coverErr == nil && cover != "" {
				covers[index] = cover
			}
		}
	}
	if !verified {
		s.jsonError(w, 403, "csrf_failed", "Request could not be verified")
		return
	}
	if len(staged) == 0 {
		s.jsonError(w, 400, "no_books", "Choose at least one book")
		return
	}
	indices := make([]int, 0, len(staged))
	for index := range staged {
		indices = append(indices, index)
	}
	sort.Ints(indices)
	results := make([]UploadResult, 0, len(indices))
	for _, index := range indices {
		item := staged[index]
		filename := item.book.Filename
		if filename == "" {
			filename = "upload"
		}
		if item.err != nil {
			results = append(results, UploadResult{Index: index, Filename: filename, Status: "error", Error: uploadError(item.err)})
			continue
		}
		book, createErr := s.dep.Library.CreateForUser(r.Context(), currentUser(r).ID, public, item.book, covers[index], category)
		item.book.Path = ""
		staged[index] = item
		if createErr != nil {
			results = append(results, UploadResult{Index: index, Filename: filename, Status: "error", Error: uploadError(createErr)})
			continue
		}
		results = append(results, UploadResult{Index: index, Filename: filename, Status: "created", BookID: book.ID, Title: book.Title})
		s.dep.Logger.Info("book_uploaded", "event", "book_uploaded", "book_id", book.ID, "hash_prefix", book.FileHash[:12], "bytes", book.FileSize, "format", book.Format, "outcome", "created")
	}
	if strings.Contains(r.Header.Get("Accept"), "application/json") {
		s.flash.Put(session.TokenHash, results)
		writeJSON(w, 200, map[string]any{"results": results})
		return
	}
	s.flash.Put(session.TokenHash, results)
	http.Redirect(w, r, "/", http.StatusSeeOther)
}
func parsePartName(name string) (string, int, bool) {
	if name == "books" {
		return "books", 0, true
	}
	m := indexedPart.FindStringSubmatch(name)
	if m == nil {
		return "", 0, false
	}
	index, err := strconv.Atoi(m[2])
	return m[1], index, err == nil
}
func readText(r io.Reader) (string, error) {
	data, err := io.ReadAll(io.LimitReader(r, (64<<10)+1))
	if err != nil || len(data) > 64<<10 {
		return "", errors.New("field too large")
	}
	return string(data), nil
}
func uploadError(err error) *APIError {
	var duplicate *library.DuplicateError
	switch {
	case errors.As(err, &duplicate):
		return &APIError{Code: "duplicate_book", Message: duplicate.Error()}
	case errors.Is(err, library.ErrTooLarge):
		return &APIError{Code: "file_too_large", Message: "File exceeds the upload limit"}
	case errors.Is(err, library.ErrInvalidFormat):
		return &APIError{Code: "invalid_format", Message: "Only valid EPUB and PDF files are accepted"}
	default:
		return &APIError{Code: "upload_failed", Message: "Book could not be added"}
	}
}

func (s *Server) mutateBook(w http.ResponseWriter, r *http.Request) {
	id, ok := bookID(w, r, s)
	if !ok {
		return
	}
	method := r.Method
	title, author, category, token, confirm := "", "", "", "", ""
	public := true
	visibilitySet := false
	if strings.Contains(r.Header.Get("Content-Type"), "application/json") {
		var body struct {
			Title, Author, Category, CSRFToken string
			Public                             *bool `json:"public"`
		}
		if err := decodeJSON(w, r, &body); err != nil {
			return
		}
		title, author, category, token = body.Title, body.Author, body.Category, body.CSRFToken
		if body.Public != nil {
			public = *body.Public
			visibilitySet = true
		}
	} else {
		r.Body = http.MaxBytesReader(w, r.Body, 64<<10)
		if err := r.ParseForm(); err != nil {
			s.jsonError(w, 400, "invalid_request", "Malformed form")
			return
		}
		title, author, category, token = r.FormValue("title"), r.FormValue("author"), r.FormValue("category"), r.FormValue("csrf_token")
		public = r.FormValue("public") == "true"
		visibilitySet = true
		confirm = r.FormValue("confirm")
		if r.Method == http.MethodPost {
			switch r.FormValue("_method") {
			case "PATCH":
				method = http.MethodPatch
			case "DELETE":
				method = http.MethodDelete
			default:
				s.jsonError(w, 400, "invalid_method", "Unsupported method override")
				return
			}
		}
	}
	if token == "" {
		token = r.Header.Get("X-CSRF-Token")
	}
	if !auth.SameOrigin(r, s.dep.Config.TrustProxy) || !s.dep.Auth.ValidCSRF(currentSession(r), token) {
		s.jsonError(w, 403, "csrf_failed", "Request could not be verified")
		return
	}
	book, findErr := s.dep.Repository.FindBookForUser(r.Context(), id, currentUser(r).ID, currentUser(r).IsAdmin())
	if database.IsNotFound(findErr) {
		s.jsonError(w, 404, "book_not_found", "Book not found")
		return
	}
	if findErr != nil {
		s.jsonError(w, 500, "internal_error", "Unexpected server failure")
		return
	}
	if !currentUser(r).IsAdmin() && book.OwnerID != currentUser(r).ID {
		s.jsonError(w, 403, "forbidden", "You cannot change this book")
		return
	}
	var err error
	if method == http.MethodPatch {
		err = s.dep.Library.Update(r.Context(), id, title, author, category)
		if err == nil && visibilitySet {
			err = s.dep.Library.UpdateVisibility(r.Context(), id, public)
		}
	} else if method == http.MethodDelete {
		if r.Method == http.MethodPost && confirm != "yes" {
			s.jsonError(w, 400, "confirmation_required", "Deletion must be confirmed")
			return
		}
		err = s.dep.Library.Delete(r.Context(), id)
	} else {
		s.jsonError(w, 405, "method_not_allowed", "Method not allowed")
		return
	}
	if database.IsNotFound(err) {
		s.jsonError(w, 404, "book_not_found", "Book not found")
		return
	}
	if err != nil {
		s.jsonError(w, 400, "invalid_request", err.Error())
		return
	}
	if r.Method == http.MethodPost {
		http.Redirect(w, r, "/", http.StatusSeeOther)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (s *Server) readerPage(w http.ResponseWriter, r *http.Request) {
	id, ok := bookID(w, r, s)
	if !ok {
		return
	}
	book, err := s.dep.Repository.FindBookForUser(r.Context(), id, currentUser(r).ID, currentUser(r).IsAdmin())
	if database.IsNotFound(err) {
		s.htmlError(w, 404, "Book not found")
		return
	}
	if err != nil {
		s.htmlError(w, 500, "Book unavailable")
		return
	}
	view := struct {
		ID        int64
		Title     string
		Format    string
		FileHash  string
		PageCount int
	}{book.ID, book.Title, book.Format, book.FileHash, book.PageCount}
	s.render(w, "reader", map[string]any{"Book": view, "CSRF": s.dep.Auth.CSRFToken(currentSession(r))})
}
func (s *Server) bookFile(w http.ResponseWriter, r *http.Request)  { s.serveBookData(w, r, false) }
func (s *Server) coverFile(w http.ResponseWriter, r *http.Request) { s.serveBookData(w, r, true) }
func (s *Server) serveBookData(w http.ResponseWriter, r *http.Request, cover bool) {
	id, ok := bookID(w, r, s)
	if !ok {
		return
	}
	book, err := s.dep.Repository.FindBookForUser(r.Context(), id, currentUser(r).ID, currentUser(r).IsAdmin())
	if database.IsNotFound(err) {
		s.jsonError(w, 404, "book_not_found", "Book not found")
		return
	}
	if err != nil {
		s.jsonError(w, 500, "internal_error", "Unexpected server failure")
		return
	}
	rel := book.FilePath
	etag := `"` + book.FileHash + `"`
	name := book.Title + "." + book.Format
	if cover {
		rel = book.CoverPath
		etag = `"` + book.FileHash + `-cover"`
		name = "cover.png"
		if rel == "" {
			s.jsonError(w, 404, "cover_not_found", "Cover not found")
			return
		}
	}
	abs, err := s.dep.Library.Path(rel)
	if err != nil {
		s.jsonError(w, 500, "internal_error", "Unexpected server failure")
		return
	}
	f, err := os.Open(abs)
	if err != nil {
		s.jsonError(w, 404, "book_not_found", "Book not found")
		return
	}
	defer f.Close()
	info, err := f.Stat()
	if err != nil {
		s.jsonError(w, 500, "internal_error", "Unexpected server failure")
		return
	}
	w.Header().Set("ETag", etag)
	if matchETag(r.Header.Get("If-None-Match"), etag) {
		w.WriteHeader(http.StatusNotModified)
		return
	}
	contentType := "application/epub+zip"
	if book.Format == "pdf" {
		contentType = "application/pdf"
	}
	if cover {
		contentType = "image/png"
	}
	w.Header().Set("Content-Type", contentType)
	w.Header().Set("Cache-Control", "private, max-age=86400")
	w.Header().Set("Content-Disposition", mime.FormatMediaType("inline", map[string]string{"filename": name}))
	http.ServeContent(w, r, name, info.ModTime(), f)
}

func (s *Server) getProgress(w http.ResponseWriter, r *http.Request) {
	id, ok := bookID(w, r, s)
	if !ok {
		return
	}
	if _, err := s.dep.Repository.FindBookForUser(r.Context(), id, currentUser(r).ID, currentUser(r).IsAdmin()); database.IsNotFound(err) {
		s.jsonError(w, 404, "book_not_found", "Book not found")
		return
	}
	p, err := s.dep.Progress.GetForUser(r.Context(), currentUser(r).ID, id)
	if database.IsNotFound(err) {
		s.jsonError(w, 404, "book_not_found", "Book not found")
		return
	}
	if err != nil {
		s.jsonError(w, 500, "internal_error", "Unexpected server failure")
		return
	}
	writeJSON(w, 200, p)
}
func (s *Server) saveProgress(w http.ResponseWriter, r *http.Request) {
	id, ok := bookID(w, r, s)
	if !ok {
		return
	}
	if _, err := s.dep.Repository.FindBookForUser(r.Context(), id, currentUser(r).ID, currentUser(r).IsAdmin()); database.IsNotFound(err) {
		s.jsonError(w, 404, "book_not_found", "Book not found")
		return
	}
	var in progress.SaveRequest
	if err := decodeJSON(w, r, &in); err != nil {
		return
	}
	token := r.Header.Get("X-CSRF-Token")
	if token == "" {
		token = in.CSRFToken
	}
	if !auth.SameOrigin(r, s.dep.Config.TrustProxy) || !s.dep.Auth.ValidCSRF(currentSession(r), token) {
		s.jsonError(w, 403, "csrf_failed", "Request could not be verified")
		return
	}
	p, err := s.dep.Progress.SaveForUser(r.Context(), currentUser(r).ID, id, in)
	if database.IsNotFound(err) {
		s.jsonError(w, 404, "book_not_found", "Book not found")
		return
	}
	if errors.Is(err, progress.ErrInvalid) {
		s.jsonError(w, 400, "invalid_progress", err.Error())
		return
	}
	if err != nil {
		s.jsonError(w, 500, "internal_error", "Unexpected server failure")
		return
	}
	writeJSON(w, 200, p)
}
func decodeJSON(w http.ResponseWriter, r *http.Request, v any) error {
	r.Body = http.MaxBytesReader(w, r.Body, 24<<10)
	d := json.NewDecoder(r.Body)
	d.DisallowUnknownFields()
	if err := d.Decode(v); err != nil {
		writeJSON(w, 400, map[string]any{"error": APIError{Code: "invalid_json", Message: "Malformed JSON request"}})
		return err
	}
	if err := d.Decode(&struct{}{}); !errors.Is(err, io.EOF) {
		writeJSON(w, 400, map[string]any{"error": APIError{Code: "invalid_json", Message: "Malformed JSON request"}})
		return errors.New("trailing JSON")
	}
	return nil
}

func bookID(w http.ResponseWriter, r *http.Request, s *Server) (int64, bool) {
	raw := chi.URLParam(r, "id")
	if raw == "" || strings.HasPrefix(raw, "+") || strings.HasPrefix(raw, "0") {
		s.jsonError(w, 404, "book_not_found", "Book not found")
		return 0, false
	}
	id, err := strconv.ParseInt(raw, 10, 64)
	if err != nil || id < 1 {
		s.jsonError(w, 404, "book_not_found", "Book not found")
		return 0, false
	}
	return id, true
}

type APIError struct {
	Code    string `json:"code"`
	Message string `json:"message"`
}

func (s *Server) jsonError(w http.ResponseWriter, status int, code, message string) {
	writeJSON(w, status, map[string]any{"error": APIError{Code: code, Message: message}})
}
func writeJSON(w http.ResponseWriter, status int, value any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(value)
}
func (s *Server) render(w http.ResponseWriter, name string, data any) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	wrapper := map[string]any{"ThemeScript": template.JS(themeScript)}
	switch value := data.(type) {
	case map[string]any:
		for k, v := range value {
			wrapper[k] = v
		}
		data = wrapper
	case libraryView:
		value.ThemeScript = template.JS(themeScript)
		data = value
	}
	if err := s.templates.ExecuteTemplate(w, name, data); err != nil {
		s.dep.Logger.Error("template_render_failed", "event", "template_render_failed", "template", name, "error", err)
	}
}
func (s *Server) htmlError(w http.ResponseWriter, status int, message string) {
	w.WriteHeader(status)
	s.render(w, "error", map[string]any{"Status": status, "Message": message})
}
func maxInt(a, b int) int {
	if a > b {
		return a
	}
	return b
}
