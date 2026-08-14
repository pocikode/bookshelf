package web

import (
	"bytes"
	"context"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/go-chi/chi/v5"
	"pocikode/bookshelf/internal/auth"
	"pocikode/bookshelf/internal/database"
	"pocikode/bookshelf/internal/library"
)

func TestFlashStoreLifecycleAndCapacity(t *testing.T) {
	now := time.Unix(100, 0)
	f := newFlashStore()
	f.now = func() time.Time { return now }
	f.max = 2
	original := []UploadResult{{Filename: "a.pdf", Error: &APIError{Code: "bad", Message: "failed"}}}
	f.Put("one", original)
	original[0].Filename = "changed"
	got := f.Take("one")
	if len(got) != 1 || got[0].Filename != "a.pdf" || got[0].Message != "failed" {
		t.Fatalf("flash result=%+v", got)
	}
	if f.Take("one") != nil {
		t.Fatal("flash entry was not consumed")
	}
	f.Put("expired", nil)
	now = now.Add(5 * time.Minute)
	if f.Take("expired") != nil {
		t.Fatal("expired flash entry was returned")
	}
	now = time.Unix(200, 0)
	f.Put("old", nil)
	now = now.Add(time.Second)
	f.Put("new", nil)
	f.Put("latest", nil)
	if _, ok := f.entries["old"]; ok {
		t.Fatal("oldest flash entry was not evicted")
	}
}

func TestServerPureHelpers(t *testing.T) {
	for _, tc := range []struct {
		value string
		want  bool
	}{
		{"/books?page=2", true}, {"", false}, {"//evil", false}, {"http://evil", false}, {"/\\evil", true},
	} {
		if got := safeReturn(tc.value); got != tc.want {
			t.Errorf("safeReturn(%q)=%v, want %v", tc.value, got, tc.want)
		}
	}
	if safeReturnValue("//evil") != "/" || safeReturnValue("/ok") != "/ok" {
		t.Error("unexpected safe return value")
	}

	for _, tc := range []struct {
		value string
		want  bool
	}{
		{"admin-1_x", true}, {"", false}, {"has space", false}, {"é", false}, {strings.Repeat("a", 64), true}, {strings.Repeat("a", 65), false},
	} {
		if got := validUsername(tc.value); got != tc.want {
			t.Errorf("validUsername(%q)=%v, want %v", tc.value, got, tc.want)
		}
	}
	if passwordDigest("password") != "5e884898da28047151d0e56f8dc6292773603d0d6aabbdd62a11ef721d1542d8" {
		t.Error("passwordDigest returned the wrong SHA-256 digest")
	}

	r := httptest.NewRequest("GET", "/?q=a&sort=title", nil)
	if got := pageURL(r, 3); got != "/?page=3&q=a&sort=title" {
		t.Errorf("pageURL=%q", got)
	}
	for _, tc := range []struct {
		name  string
		field string
		index int
		valid bool
	}{
		{"books", "books", 0, true}, {"books[2]", "books", 2, true}, {"covers[12]", "covers", 12, true}, {"books[x]", "", 0, false}, {"other", "", 0, false},
	} {
		field, index, valid := parsePartName(tc.name)
		if field != tc.field || index != tc.index || valid != tc.valid {
			t.Errorf("parsePartName(%q)=(%q,%d,%v)", tc.name, field, index, valid)
		}
	}
	if got, err := readText(strings.NewReader("hello")); err != nil || got != "hello" {
		t.Errorf("readText small=(%q,%v)", got, err)
	}
	if _, err := readText(strings.NewReader(strings.Repeat("x", 64<<10+1))); err == nil {
		t.Fatal("readText accepted an oversized field")
	}
	if _, err := readText(errorReader{}); err == nil {
		t.Fatal("readText ignored a reader error")
	}

	duplicate := &library.DuplicateError{Book: database.Book{Title: "Existing"}}
	for _, tc := range []struct {
		err  error
		code string
		msg  string
	}{
		{duplicate, "duplicate_book", "Already in library as Existing"}, {library.ErrTooLarge, "file_too_large", "File exceeds the upload limit"}, {library.ErrInvalidFormat, "invalid_format", "Only valid EPUB and PDF files are accepted"}, {errors.New("other"), "upload_failed", "Book could not be added"},
	} {
		got := uploadError(tc.err)
		if got.Code != tc.code || got.Message != tc.msg {
			t.Errorf("uploadError(%v)=%+v", tc.err, got)
		}
	}
	if maxInt(1, 2) != 2 || maxInt(3, 2) != 3 {
		t.Error("maxInt returned the wrong maximum")
	}
}

type errorReader struct{}

func (errorReader) Read([]byte) (int, error) { return 0, errors.New("read failed") }

func TestDecodeJSONRejectsInvalidInput(t *testing.T) {
	for _, body := range []string{`{"unknown":1}`, `{"ok":1}{"ok":2}`, `{`} {
		rec := httptest.NewRecorder()
		req := httptest.NewRequest("POST", "/", strings.NewReader(body))
		var value struct {
			OK int `json:"ok"`
		}
		if err := decodeJSON(rec, req, &value); err == nil || rec.Code != http.StatusBadRequest {
			t.Errorf("decodeJSON(%q) err/code=%v/%d", body, err, rec.Code)
		}
	}
	valid := httptest.NewRecorder()
	request := httptest.NewRequest("POST", "/", strings.NewReader(`{"ok":7}`))
	var value struct {
		OK int `json:"ok"`
	}
	if err := decodeJSON(valid, request, &value); err != nil || value.OK != 7 {
		t.Fatalf("valid JSON err/value=%v/%+v", err, value)
	}
}

func TestAssetsServeHTTP(t *testing.T) {
	assets, err := LoadAssets()
	if err != nil {
		t.Fatal(err)
	}
	asset, ok := assets.byName["app.css"]
	if !ok {
		t.Fatal("app.css asset is missing")
	}
	request := httptest.NewRequest("GET", asset.URL, nil)
	rec := httptest.NewRecorder()
	assets.ServeHTTP(rec, request)
	if rec.Code != http.StatusOK || rec.Body.Len() != len(asset.Data) || rec.Header().Get("ETag") == "" {
		t.Fatalf("asset response=%d length=%d", rec.Code, rec.Body.Len())
	}
	request = httptest.NewRequest("GET", asset.URL, nil)
	request.Header.Set("If-None-Match", rec.Header().Get("ETag"))
	rec = httptest.NewRecorder()
	assets.ServeHTTP(rec, request)
	if rec.Code != http.StatusNotModified {
		t.Fatalf("cached asset=%d", rec.Code)
	}
	rec = httptest.NewRecorder()
	assets.ServeHTTP(rec, httptest.NewRequest("GET", "/assets/missing", nil))
	if rec.Code != http.StatusNotFound {
		t.Fatalf("missing asset=%d", rec.Code)
	}
	if assets.URL("missing.js") != "" {
		t.Fatal("unknown asset returned a URL")
	}
	if err := assets.Require("missing.js"); err == nil {
		t.Fatal("Require accepted a missing asset")
	}
}

func TestBookIDValidation(t *testing.T) {
	app := newIntegrationApp(t)
	defer app.dbClose()
	for _, raw := range []string{"", "+1", "01", "0", "nope", "-1"} {
		req := httptest.NewRequest("GET", "/api/books/"+url.PathEscape(raw)+"/file", nil)
		req.AddCookie(&http.Cookie{Name: auth.CookieName, Value: app.session.Token})
		rec := httptest.NewRecorder()
		app.handler.ServeHTTP(rec, req)
		if rec.Code != http.StatusNotFound {
			t.Errorf("book id %q status=%d", raw, rec.Code)
		}
	}
	ctx := chi.NewRouteContext()
	ctx.URLParams.Add("id", "42")
	req := httptest.NewRequest("GET", "/", nil).WithContext(context.WithValue(context.Background(), chi.RouteCtxKey, ctx))
	rec := httptest.NewRecorder()
	if id, ok := bookID(rec, req, &Server{}); !ok || id != 42 {
		t.Fatalf("bookID valid=(%d,%v)", id, ok)
	}
}

func TestPracticalHTTPErrorBranches(t *testing.T) {
	app := newIntegrationApp(t)
	defer app.dbClose()
	for _, tc := range []struct {
		path string
		want int
	}{
		{"/", http.StatusSeeOther}, {"/api/books/999/progress", http.StatusNotFound},
	} {
		rec := app.request("GET", tc.path, nil, tc.path != "/")
		if rec.Code != tc.want {
			t.Errorf("GET %s status=%d, want %d", tc.path, rec.Code, tc.want)
		}
	}
	req := httptest.NewRequest("PATCH", "/api/books/"+stringID(app.book.ID), strings.NewReader(`{"unknown":1}`))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-CSRF-Token", app.csrf)
	req.AddCookie(&http.Cookie{Name: auth.CookieName, Value: app.session.Token})
	rec := httptest.NewRecorder()
	app.handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusBadRequest || !bytes.Contains(rec.Body.Bytes(), []byte(`"invalid_json"`)) {
		t.Fatalf("unknown JSON response=%d %s", rec.Code, rec.Body.String())
	}
	if rec := app.request("GET", "/books/"+stringID(app.book.ID)+"/read", nil, true); rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), app.book.Title) {
		t.Fatalf("reader response=%d", rec.Code)
	} else if body := rec.Body.String(); !strings.Contains(body, `id="bookmark-toggle"`) || !strings.Contains(body, `id="bookmark-list"`) {
		t.Fatal("reader page is missing the bookmark controls")
	}
	if rec := app.request("GET", "/api/books/"+stringID(app.book.ID)+"/cover", nil, true); rec.Code != http.StatusNotFound {
		t.Fatalf("missing cover response=%d", rec.Code)
	}
	if rec := app.request("POST", "/api/books", strings.NewReader("not multipart"), true); rec.Code != http.StatusBadRequest || !strings.Contains(rec.Body.String(), "invalid_multipart") {
		t.Fatalf("invalid multipart response=%d %s", rec.Code, rec.Body.String())
	}
	if rec := app.request("POST", "/logout", strings.NewReader(url.Values{"csrf_token": {app.csrf}}.Encode()), true); rec.Code != http.StatusSeeOther || rec.Header().Get("Location") != "/login" {
		t.Fatalf("logout response=%d location=%q", rec.Code, rec.Header().Get("Location"))
	}
}

func TestAdminAndAuthenticationBranches(t *testing.T) {
	app := newIntegrationApp(t)
	defer app.dbClose()
	if rec := app.request("GET", "/admin/users", nil, true); rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), "Users") {
		t.Fatalf("users page=%d", rec.Code)
	}
	form := func(values url.Values) io.Reader { return strings.NewReader(values.Encode()) }
	invalid := url.Values{"csrf_token": {app.csrf}, "username": {"bad name"}, "password": {"short"}}
	if rec := app.request("POST", "/admin/users", form(invalid), true); rec.Code != http.StatusBadRequest {
		t.Fatalf("invalid user=%d", rec.Code)
	}
	valid := url.Values{"csrf_token": {app.csrf}, "username": {"reader"}, "password": {"long enough"}}
	if rec := app.request("POST", "/admin/users", form(valid), true); rec.Code != http.StatusSeeOther {
		t.Fatalf("create user=%d", rec.Code)
	}
	if rec := app.request("POST", "/admin/users", form(valid), true); rec.Code != http.StatusBadRequest {
		t.Fatalf("duplicate user=%d", rec.Code)
	}
	reset := url.Values{"csrf_token": {app.csrf}, "password": {"long enough"}}
	if rec := app.request("POST", "/admin/users/999/password", form(reset), true); rec.Code != http.StatusNotFound {
		t.Fatalf("missing user reset=%d", rec.Code)
	}
	mismatch := url.Values{"csrf_token": {app.csrf}, "current_password": {"correct horse battery"}, "new_password": {"long enough"}, "new_password_confirmation": {"different"}}
	if rec := app.request("POST", "/settings/password", form(mismatch), true); rec.Code != http.StatusBadRequest {
		t.Fatalf("password mismatch=%d", rec.Code)
	}
	if rec := app.request("POST", "/logout/all", form(url.Values{"csrf_token": {app.csrf}}), true); rec.Code != http.StatusSeeOther || rec.Header().Get("Location") != "/login" {
		t.Fatalf("logout all=%d location=%q", rec.Code, rec.Header().Get("Location"))
	}
}

var _ io.Reader = errorReader{}
