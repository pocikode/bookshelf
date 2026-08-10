package web

import (
	"archive/zip"
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"io"
	"log/slog"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"pocikode/bookshelf/internal/auth"
	"pocikode/bookshelf/internal/config"
	"pocikode/bookshelf/internal/database"
	"pocikode/bookshelf/internal/library"
	"pocikode/bookshelf/internal/progress"
	"pocikode/bookshelf/internal/ratelimit"
	"pocikode/bookshelf/internal/version"
)

type integrationApp struct {
	handler http.Handler
	session auth.Session
	csrf    string
	book    database.Book
	content []byte
	repo    *database.Repository
	auth    *auth.Service
	dbClose func()
}

func newIntegrationApp(t *testing.T) *integrationApp {
	t.Helper()
	dir := t.TempDir()
	cfg := config.Config{Password: "correct horse battery", DataDir: dir, Port: 8080, MaxUploadBytes: 1 << 20, SessionDays: 90, LogLevel: "error"}
	if err := cfg.PrepareDataDir(); err != nil {
		t.Fatal(err)
	}
	db, err := database.Open(context.Background(), dir)
	if err != nil {
		t.Fatal(err)
	}
	repo := database.NewRepository(db)
	content := []byte("%PDF-0123456789abcdefghijklmnopqrstuvwxyz")
	sum := sha256.Sum256(content)
	hash := hex.EncodeToString(sum[:])
	rel := filepath.ToSlash(filepath.Join("books", hash[:2], hash+".pdf"))
	abs := filepath.Join(dir, filepath.FromSlash(rel))
	if err = os.MkdirAll(filepath.Dir(abs), 0o750); err != nil {
		t.Fatal(err)
	}
	if err = os.WriteFile(abs, content, 0o640); err != nil {
		t.Fatal(err)
	}
	book := database.Book{Title: "Range Test", Category: "Tests", Format: "pdf", FileHash: hash, FilePath: rel, FileSize: int64(len(content)), PageCount: 10, CreatedAt: time.Now().UTC()}
	if err = repo.InsertBook(context.Background(), &book); err != nil {
		t.Fatal(err)
	}
	authSvc := auth.New(repo, cfg.Password, cfg.SessionDays)
	if err := authSvc.Initialize(context.Background(), cfg.Password); err != nil {
		t.Fatal(err)
	}
	session, err := authSvc.Create(context.Background(), "test")
	if err != nil {
		t.Fatal(err)
	}
	handler, err := NewServer(Dependencies{Config: cfg, Repository: repo, Auth: authSvc, Limiter: ratelimit.New(nil), Library: library.New(repo, dir, cfg.MaxUploadBytes), Progress: progress.New(repo), Logger: slog.New(slog.NewTextHandler(io.Discard, nil))})
	if err != nil {
		t.Fatal(err)
	}
	return &integrationApp{handler: handler, session: session, csrf: authSvc.CSRFToken(session), book: book, content: content, repo: repo, auth: authSvc, dbClose: func() { db.Close() }}
}
func (a *integrationApp) request(method, path string, body io.Reader, authn bool) *httptest.ResponseRecorder {
	req := httptest.NewRequest(method, path, body)
	req.RemoteAddr = "192.0.2.1:1234"
	if body != nil {
		req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	}
	if authn {
		req.AddCookie(&http.Cookie{Name: auth.CookieName, Value: a.session.Token})
	}
	rec := httptest.NewRecorder()
	a.handler.ServeHTTP(rec, req)
	return rec
}
func TestLoginFailureKeepsUsername(t *testing.T) {
	app := newIntegrationApp(t)
	defer app.dbClose()

	rec := app.request("POST", "/login", strings.NewReader(url.Values{
		"username": {"admin"},
		"password": {"wrong password"},
	}.Encode()), false)
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("login status=%d", rec.Code)
	}
	if !strings.Contains(rec.Body.String(), `name="username" value="admin"`) {
		t.Fatalf("username was not preserved: %s", rec.Body.String())
	}
}

func TestVersionIsPublicAndRendered(t *testing.T) {
	app := newIntegrationApp(t)
	defer app.dbClose()

	api := app.request("GET", "/api/version", nil, false)
	if api.Code != http.StatusOK || api.Header().Get("Content-Type") != "application/json" {
		t.Fatalf("version response=%d content-type=%q", api.Code, api.Header().Get("Content-Type"))
	}
	var payload struct {
		Version string `json:"version"`
	}
	if err := json.Unmarshal(api.Body.Bytes(), &payload); err != nil || payload.Version != version.Version {
		t.Fatalf("version payload=%q err=%v", payload.Version, err)
	}

	login := app.request("GET", "/login", nil, false)
	if !strings.Contains(login.Body.String(), "private library · "+version.Version) {
		t.Fatalf("login page omitted version: %s", login.Body.String())
	}

	library := app.request("GET", "/", nil, true)
	if !strings.Contains(library.Body.String(), "private library · "+version.Version) {
		t.Fatalf("library header omitted version: %s", library.Body.String())
	}
}
func TestAuthenticatedFileRangesAndConditionals(t *testing.T) {
	app := newIntegrationApp(t)
	defer app.dbClose()
	path := "/api/books/" + stringID(app.book.ID) + "/file"
	if got := app.request("GET", path, nil, false); got.Code != 401 {
		t.Fatalf("unauthenticated=%d", got.Code)
	}
	full := app.request("GET", path, nil, true)
	if full.Code != 200 || !bytes.Equal(full.Body.Bytes(), app.content) {
		t.Fatalf("full=%d %q", full.Code, full.Body.Bytes())
	}
	etag := full.Header().Get("ETag")
	req := httptest.NewRequest("GET", path, nil)
	req.AddCookie(&http.Cookie{Name: auth.CookieName, Value: app.session.Token})
	req.Header.Set("Range", "bytes=5-9")
	partial := httptest.NewRecorder()
	app.handler.ServeHTTP(partial, req)
	if partial.Code != 206 || partial.Body.String() != string(app.content[5:10]) {
		t.Fatalf("range=%d %q", partial.Code, partial.Body.String())
	}
	req = httptest.NewRequest("GET", path, nil)
	req.AddCookie(&http.Cookie{Name: auth.CookieName, Value: app.session.Token})
	req.Header.Set("Range", "bytes=-4")
	suffix := httptest.NewRecorder()
	app.handler.ServeHTTP(suffix, req)
	if suffix.Code != 206 || suffix.Body.String() != string(app.content[len(app.content)-4:]) {
		t.Fatalf("suffix=%d %q", suffix.Code, suffix.Body.String())
	}
	req = httptest.NewRequest("GET", path, nil)
	req.AddCookie(&http.Cookie{Name: auth.CookieName, Value: app.session.Token})
	req.Header.Set("Range", "bytes=999-1000")
	invalid := httptest.NewRecorder()
	app.handler.ServeHTTP(invalid, req)
	if invalid.Code != 416 {
		t.Fatalf("invalid range=%d", invalid.Code)
	}
	req = httptest.NewRequest("GET", path, nil)
	req.AddCookie(&http.Cookie{Name: auth.CookieName, Value: app.session.Token})
	req.Header.Set("If-None-Match", etag)
	cached := httptest.NewRecorder()
	app.handler.ServeHTTP(cached, req)
	if cached.Code != 304 {
		t.Fatalf("If-None-Match=%d", cached.Code)
	}
	req = httptest.NewRequest("GET", path, nil)
	req.AddCookie(&http.Cookie{Name: auth.CookieName, Value: app.session.Token})
	req.Header.Set("Range", "bytes=0-3")
	req.Header.Set("If-Range", `"different"`)
	ifRange := httptest.NewRecorder()
	app.handler.ServeHTTP(ifRange, req)
	if ifRange.Code != 200 {
		t.Fatalf("If-Range=%d", ifRange.Code)
	}
}

func TestLibraryFilterFragment(t *testing.T) {
	app := newIntegrationApp(t)
	defer app.dbClose()
	req := httptest.NewRequest("GET", "/?q=Range", nil)
	req.Header.Set("X-Requested-With", "fetch")
	req.AddCookie(&http.Cookie{Name: auth.CookieName, Value: app.session.Token})
	rec := httptest.NewRecorder()
	app.handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("filter fragment status=%d", rec.Code)
	}
	if strings.Contains(rec.Body.String(), "<!doctype html>") || !strings.Contains(rec.Body.String(), `id="library-results"`) || !strings.Contains(rec.Body.String(), "Range Test") || !strings.Contains(rec.Body.String(), `aria-label="File format: pdf">pdf</span>`) {
		t.Fatalf("unexpected filter fragment: %s", rec.Body.String())
	}
}

func TestProgressCSRFAndServerDerivedPDFPercent(t *testing.T) {
	app := newIntegrationApp(t)
	defer app.dbClose()
	path := "/api/books/" + stringID(app.book.ID) + "/progress"
	body := `{"page":3,"device_label":"Safari on iOS"}`
	without := app.request("PUT", path, strings.NewReader(body), true)
	if without.Code != 403 {
		t.Fatalf("without csrf=%d %s", without.Code, without.Body.String())
	}
	req := httptest.NewRequest("PUT", path, strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-CSRF-Token", app.csrf)
	req.AddCookie(&http.Cookie{Name: auth.CookieName, Value: app.session.Token})
	saved := httptest.NewRecorder()
	app.handler.ServeHTTP(saved, req)
	if saved.Code != 200 {
		t.Fatalf("save=%d %s", saved.Code, saved.Body.String())
	}
	var p database.Progress
	if err := json.Unmarshal(saved.Body.Bytes(), &p); err != nil {
		t.Fatal(err)
	}
	if p.Page != 3 || p.Percent != .3 {
		t.Fatalf("progress=%+v", p)
	}
}

func TestIndexedEPUBUploadMetadataDuplicateAndFlash(t *testing.T) {
	app := newIntegrationApp(t)
	defer app.dbClose()
	epub := epubFixture(t, "The Extracted Title", "A. Writer")
	upload := func(token string) *httptest.ResponseRecorder {
		var body bytes.Buffer
		writer := multipart.NewWriter(&body)
		if token != "" {
			if err := writer.WriteField("csrf_token", token); err != nil {
				t.Fatal(err)
			}
		}
		if err := writer.WriteField("category", "Fiction"); err != nil {
			t.Fatal(err)
		}
		part, err := writer.CreateFormFile("books[0]", "fixture.epub")
		if err != nil {
			t.Fatal(err)
		}
		if _, err = part.Write(epub); err != nil {
			t.Fatal(err)
		}
		if err = writer.Close(); err != nil {
			t.Fatal(err)
		}
		req := httptest.NewRequest("POST", "/api/books", &body)
		req.Header.Set("Content-Type", writer.FormDataContentType())
		req.Header.Set("Accept", "application/json")
		req.AddCookie(&http.Cookie{Name: auth.CookieName, Value: app.session.Token})
		rec := httptest.NewRecorder()
		app.handler.ServeHTTP(rec, req)
		return rec
	}
	if denied := upload(""); denied.Code != 403 {
		t.Fatalf("missing csrf=%d %s", denied.Code, denied.Body.String())
	}
	created := upload(app.csrf)
	if created.Code != 200 {
		t.Fatalf("created=%d %s", created.Code, created.Body.String())
	}
	var envelope struct {
		Results []UploadResult `json:"results"`
	}
	if err := json.Unmarshal(created.Body.Bytes(), &envelope); err != nil {
		t.Fatal(err)
	}
	if len(envelope.Results) != 1 || envelope.Results[0].Status != "created" || envelope.Results[0].Title != "The Extracted Title" {
		t.Fatalf("results=%+v", envelope.Results)
	}
	book, err := app.repo.FindBook(context.Background(), envelope.Results[0].BookID)
	if err != nil {
		t.Fatal(err)
	}
	if book.Author != "A. Writer" || book.Category != "Fiction" {
		t.Fatalf("metadata=%+v", book)
	}
	duplicate := upload(app.csrf)
	if duplicate.Code != 200 {
		t.Fatalf("duplicate=%d", duplicate.Code)
	}
	if err = json.Unmarshal(duplicate.Body.Bytes(), &envelope); err != nil {
		t.Fatal(err)
	}
	if envelope.Results[0].Error == nil || envelope.Results[0].Error.Code != "duplicate_book" {
		t.Fatalf("duplicate result=%+v", envelope.Results[0])
	}
	libraryPage := app.request("GET", "/", nil, true)
	if libraryPage.Code != 200 || !strings.Contains(libraryPage.Body.String(), "Already in library as The Extracted Title") {
		t.Fatalf("flash missing: %d", libraryPage.Code)
	}
}

func TestChangePasswordRequiresCSRFAndRevokesSessions(t *testing.T) {
	app := newIntegrationApp(t)
	defer app.dbClose()
	settings := app.request("GET", "/settings", nil, true)
	if settings.Code != http.StatusOK || !strings.Contains(settings.Body.String(), `action="/settings/password"`) {
		t.Fatalf("password form missing: %d", settings.Code)
	}
	values := func(csrf, current, next, confirmation string) io.Reader {
		return strings.NewReader(url.Values{
			"csrf_token":                {csrf},
			"current_password":          {current},
			"new_password":              {next},
			"new_password_confirmation": {confirmation},
		}.Encode())
	}
	withoutCSRF := app.request("POST", "/settings/password", values("", "correct horse battery", "a sufficiently long password", "a sufficiently long password"), true)
	if withoutCSRF.Code != http.StatusForbidden {
		t.Fatalf("without csrf=%d", withoutCSRF.Code)
	}
	wrong := app.request("POST", "/settings/password", values(app.csrf, "wrong", "a sufficiently long password", "a sufficiently long password"), true)
	if wrong.Code != http.StatusBadRequest || !strings.Contains(wrong.Body.String(), "Current password is incorrect") {
		t.Fatalf("wrong password=%d %s", wrong.Code, wrong.Body.String())
	}
	success := app.request("POST", "/settings/password", values(app.csrf, "correct horse battery", "p@ssw0rd", "p@ssw0rd"), true)
	if success.Code != http.StatusSeeOther || success.Header().Get("Location") != "/login" {
		t.Fatalf("success=%d location=%q", success.Code, success.Header().Get("Location"))
	}
	cookies := success.Result().Cookies()
	if len(cookies) != 1 || cookies[0].Name != auth.CookieName || cookies[0].MaxAge >= 0 {
		t.Fatalf("cookie was not cleared: %+v", cookies)
	}
	if _, err := app.auth.Resolve(context.Background(), app.session.Token); err == nil {
		t.Fatal("old session remained valid")
	}
	restarted := auth.New(app.repo, "old configured password", 90)
	if err := restarted.Initialize(context.Background(), "old configured password"); err != nil || !restarted.ComparePassword("p@ssw0rd") {
		t.Fatalf("persisted password was not loaded: %v", err)
	}
}

func epubFixture(t *testing.T, title, author string) []byte {
	t.Helper()
	var out bytes.Buffer
	zw := zip.NewWriter(&out)
	header := &zip.FileHeader{Name: "mimetype", Method: zip.Store}
	part, err := zw.CreateHeader(header)
	if err != nil {
		t.Fatal(err)
	}
	_, _ = io.WriteString(part, "application/epub+zip")
	part, err = zw.Create("META-INF/container.xml")
	if err != nil {
		t.Fatal(err)
	}
	_, _ = io.WriteString(part, `<?xml version="1.0"?><container><rootfiles><rootfile full-path="OPS/content.opf"/></rootfiles></container>`)
	part, err = zw.Create("OPS/content.opf")
	if err != nil {
		t.Fatal(err)
	}
	_, _ = io.WriteString(part, `<?xml version="1.0"?><package xmlns:dc="http://purl.org/dc/elements/1.1/"><metadata><dc:title>`+title+`</dc:title><dc:creator>`+author+`</dc:creator><dc:language>en</dc:language></metadata><manifest></manifest></package>`)
	if err = zw.Close(); err != nil {
		t.Fatal(err)
	}
	return out.Bytes()
}
func stringID(id int64) string { return strconv.FormatInt(id, 10) }
