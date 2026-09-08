package httpapi

import (
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"pocikode/bookshelf/server/internal/config"
)

func staticAPI(t *testing.T) *API {
	t.Helper()
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "index.html"), []byte("<!DOCTYPE html><html></html>"), 0o600); err != nil {
		t.Fatal(err)
	}
	return &API{Config: config.Config{StaticDir: dir}}
}

// The static export never emits `/runtime-config.js`, but the app shell loads it
// with `beforeInteractive`. It must be JavaScript, never the SPA shell.
func TestRuntimeConfigServesJavaScript(t *testing.T) {
	rec := httptest.NewRecorder()
	staticAPI(t).Handler().ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/runtime-config.js", nil))

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rec.Code)
	}
	if ct := rec.Header().Get("Content-Type"); !strings.HasPrefix(ct, "application/javascript") {
		t.Fatalf("content-type = %q", ct)
	}
	body := rec.Body.String()
	if strings.Contains(body, "<") {
		t.Fatalf("body must not contain HTML, got %q", body)
	}
	if !strings.Contains(body, "window.__READEST_RUNTIME_CONFIG") {
		t.Fatalf("body does not define the runtime config global: %q", body)
	}
}

// A missing asset returning index.html is what turned a 404 into
// "expected expression, got '<'" in the browser.
func TestMissingAssetIsNotSPAFallback(t *testing.T) {
	rec := httptest.NewRecorder()
	staticAPI(t).Handler().ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/_next/static/missing.js", nil))

	if rec.Code != http.StatusNotFound {
		t.Fatalf("status = %d, want 404", rec.Code)
	}
}

// Real routes still have to reach the SPA shell for client-side routing.
func TestUnknownRouteServesSPAShell(t *testing.T) {
	rec := httptest.NewRecorder()
	staticAPI(t).Handler().ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/library", nil))

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rec.Code)
	}
	if !strings.Contains(rec.Body.String(), "<!DOCTYPE html>") {
		t.Fatalf("expected SPA shell, got %q", rec.Body.String())
	}
}

// `/auth` exists as both `auth.html` and an `auth/` directory of child routes.
// The directory must not shadow the page, or the login route boots the root
// document and renders the library instead of the sign-in form.
func TestRoutePageWinsOverSameNamedDirectory(t *testing.T) {
	api := staticAPI(t)
	dir := api.Config.StaticDir
	if err := os.Mkdir(filepath.Join(dir, "auth"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "auth.html"), []byte("<html>auth page</html>"), 0o600); err != nil {
		t.Fatal(err)
	}

	rec := httptest.NewRecorder()
	api.Handler().ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/auth", nil))

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rec.Code)
	}
	if !strings.Contains(rec.Body.String(), "auth page") {
		t.Fatalf("expected auth.html, got %q", rec.Body.String())
	}
}

func TestDynamicReaderPathServesExportedReaderPage(t *testing.T) {
	api := staticAPI(t)
	dir := filepath.Join(api.Config.StaticDir, "reader")
	if err := os.Mkdir(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "[ids].html"), []byte("<html>reader page</html>"), 0o600); err != nil {
		t.Fatal(err)
	}
	rec := httptest.NewRecorder()
	api.Handler().ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/reader/book-hash", nil))

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusOK)
	}
	if !strings.Contains(rec.Body.String(), "reader page") {
		t.Fatalf("expected reader page, got %q", rec.Body.String())
	}
}
