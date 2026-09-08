package httpapi

import (
	"encoding/json"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"pocikode/bookshelf/server/internal/auth"
	"pocikode/bookshelf/server/internal/config"
	"pocikode/bookshelf/server/internal/database"
	"pocikode/bookshelf/server/internal/logging"
)

func loggingAPI(t *testing.T) (*API, func() []map[string]any) {
	t.Helper()
	db, err := database.Open(t.TempDir() + "/app.sqlite")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Close() })

	var buf strings.Builder
	logger := slog.New(slog.NewJSONHandler(&buf, &slog.HandlerOptions{Level: slog.LevelDebug}))
	api := New(db.DB, config.Config{StaticDir: t.TempDir()}, logger)
	return api, func() []map[string]any {
		var events []map[string]any
		for _, line := range strings.Split(strings.TrimSpace(buf.String()), "\n") {
			if line == "" {
				continue
			}
			var event map[string]any
			if err := json.Unmarshal([]byte(line), &event); err != nil {
				t.Fatalf("log line is not JSON: %q", line)
			}
			events = append(events, event)
		}
		return events
	}
}

func find(events []map[string]any, msg string) map[string]any {
	for _, event := range events {
		if event["msg"] == msg {
			return event
		}
	}
	return nil
}

// An unauthenticated request is a routine rejection, but it must still leave
// a trace — previously nothing was recorded at all.
func TestUnauthorizedRequestIsLogged(t *testing.T) {
	api, events := loggingAPI(t)

	rec := httptest.NewRecorder()
	api.Handler().ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/api/books", nil))

	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d, want 401", rec.Code)
	}
	logged := events()
	if find(logged, "authentication rejected") == nil {
		t.Fatalf("no authentication event logged: %v", logged)
	}
	rejected := find(logged, "error response")
	if rejected == nil {
		t.Fatalf("no rejection event logged: %v", logged)
	}
	if rejected["status"] != float64(http.StatusUnauthorized) {
		t.Errorf("status = %v, want 401", rejected["status"])
	}
}

// A bad login must be distinguishable in the logs from a database failure;
// both used to collapse into the same silent 401.
func TestLoginFailureIsLoggedWithReason(t *testing.T) {
	api, events := loggingAPI(t)

	rec := httptest.NewRecorder()
	body := strings.NewReader(`{"username":"ghost","password":"secret"}`)
	api.Handler().ServeHTTP(rec, httptest.NewRequest(http.MethodPost, "/api/auth/login", body))

	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d, want 401", rec.Code)
	}
	event := find(events(), "login rejected: unknown user")
	if event == nil {
		t.Fatalf("login failure not logged: %v", events())
	}
	if event["username"] != "ghost" {
		t.Errorf("username = %v", event["username"])
	}
}

// A 500 must carry the underlying cause into the log while the client only
// sees the generic message.
func TestServerErrorLogsCauseButDoesNotLeakIt(t *testing.T) {
	api, events := loggingAPI(t)
	// Closing the database turns the next query into an operational failure.
	if _, err := api.DB.Exec(`DROP TABLE books`); err != nil {
		t.Fatal(err)
	}

	rec := httptest.NewRecorder()
	handler := logging.Middleware(api.Logger, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		api.listBooks(w, r, auth.Session{User: auth.User{ID: "user-1"}})
	}))
	handler.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/api/books", nil))

	if rec.Code != http.StatusInternalServerError {
		t.Fatalf("status = %d, want 500", rec.Code)
	}
	if strings.Contains(rec.Body.String(), "books") && strings.Contains(rec.Body.String(), "no such table") {
		t.Errorf("response leaks the internal error: %q", rec.Body.String())
	}
	event := find(events(), "error response (server fault)")
	if event == nil {
		t.Fatalf("server error not logged: %v", events())
	}
	if cause, _ := event["error"].(string); !strings.Contains(cause, "query books") {
		t.Errorf("logged error does not identify the operation: %v", event["error"])
	}
}

func TestBookListIsNotCached(t *testing.T) {
	api, _ := loggingAPI(t)
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/api/books", nil)

	api.listBooks(rec, req, auth.Session{User: auth.User{ID: "user-1"}})

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rec.Code)
	}
	if got := rec.Header().Get("Cache-Control"); got != "no-store" {
		t.Errorf("Cache-Control = %q, want no-store", got)
	}
	if got := strings.TrimSpace(rec.Body.String()); got != "[]" {
		t.Errorf("empty book list = %q, want []", got)
	}
}

// The SPA shell being absent is a deployment fault, not a normal 404.
func TestMissingSPAShellIsLogged(t *testing.T) {
	api, events := loggingAPI(t)

	rec := httptest.NewRecorder()
	api.Handler().ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/library", nil))

	if rec.Code != http.StatusServiceUnavailable {
		t.Fatalf("status = %d, want 503", rec.Code)
	}
	if find(events(), "SPA shell is missing") == nil {
		t.Fatalf("missing shell not logged: %v", events())
	}
}
