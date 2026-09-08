package logging

import (
	"encoding/json"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func capture(t *testing.T) (*slog.Logger, func() []map[string]any) {
	t.Helper()
	var buf strings.Builder
	logger := slog.New(slog.NewJSONHandler(&buf, &slog.HandlerOptions{Level: slog.LevelDebug}))
	return logger, func() []map[string]any {
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

func TestMiddlewareLogsEveryRequest(t *testing.T) {
	logger, events := capture(t)
	handler := Middleware(logger, http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusNoContent)
	}))

	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/api/books", nil))

	logged := events()
	if len(logged) != 1 {
		t.Fatalf("got %d events, want 1: %v", len(logged), logged)
	}
	event := logged[0]
	if event["level"] != "INFO" {
		t.Errorf("level = %v, want INFO", event["level"])
	}
	if event["status"] != float64(http.StatusNoContent) {
		t.Errorf("status = %v, want 204", event["status"])
	}
	if event["path"] != "/api/books" || event["method"] != http.MethodGet {
		t.Errorf("missing request attributes: %v", event)
	}
	if event["requestId"] == "" || event["requestId"] == nil {
		t.Error("requestId is not set")
	}
	if rec.Header().Get(RequestIDHeader) != event["requestId"] {
		t.Errorf("response header %q does not match logged id", rec.Header().Get(RequestIDHeader))
	}
}

// A 5xx must be loud, since it is the signal an operator greps for.
func TestServerErrorsLogAtErrorLevel(t *testing.T) {
	logger, events := capture(t)
	handler := Middleware(logger, http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		http.Error(w, "boom", http.StatusInternalServerError)
	}))

	handler.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest(http.MethodGet, "/api/books", nil))

	logged := events()
	if got := logged[len(logged)-1]["level"]; got != "ERROR" {
		t.Fatalf("level = %v, want ERROR", got)
	}
}

func TestClientErrorsLogAtWarnLevel(t *testing.T) {
	logger, events := capture(t)
	handler := Middleware(logger, http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		http.Error(w, "nope", http.StatusNotFound)
	}))

	handler.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest(http.MethodGet, "/missing", nil))

	logged := events()
	if got := logged[len(logged)-1]["level"]; got != "WARN" {
		t.Fatalf("level = %v, want WARN", got)
	}
}

// Without recovery a panic reaches net/http, which logs unstructured and
// leaves the client with a dropped connection.
func TestPanicIsRecoveredAndLogged(t *testing.T) {
	logger, events := capture(t)
	handler := Middleware(logger, http.HandlerFunc(func(http.ResponseWriter, *http.Request) {
		panic("kaboom")
	}))

	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/api/books", nil))

	if rec.Code != http.StatusInternalServerError {
		t.Fatalf("status = %d, want 500", rec.Code)
	}
	logged := events()
	if len(logged) != 2 {
		t.Fatalf("got %d events, want panic + access log: %v", len(logged), logged)
	}
	if logged[0]["msg"] != "panic recovered" {
		t.Errorf("msg = %v", logged[0]["msg"])
	}
	if logged[0]["panic"] != "kaboom" {
		t.Errorf("panic value = %v", logged[0]["panic"])
	}
	if stack, _ := logged[0]["stack"].(string); stack == "" {
		t.Error("stack is missing")
	}
}

// An inbound X-Request-ID is honoured so logs correlate across a proxy.
func TestExistingRequestIDIsPreserved(t *testing.T) {
	logger, events := capture(t)
	handler := Middleware(logger, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if got := RequestID(r.Context()); got != "abc-123" {
			t.Errorf("RequestID = %q", got)
		}
		From(r.Context()).Info("handler event")
	}))

	req := httptest.NewRequest(http.MethodGet, "/api/health", nil)
	req.Header.Set(RequestIDHeader, "abc-123")
	handler.ServeHTTP(httptest.NewRecorder(), req)

	for _, event := range events() {
		if event["requestId"] != "abc-123" {
			t.Fatalf("requestId = %v, want abc-123", event["requestId"])
		}
	}
}

func TestParseLevel(t *testing.T) {
	for input, want := range map[string]slog.Level{
		"debug": slog.LevelDebug,
		"INFO":  slog.LevelInfo,
		"warn":  slog.LevelWarn,
		"error": slog.LevelError,
		"":      slog.LevelInfo,
		"junk":  slog.LevelInfo,
	} {
		if got := ParseLevel(input); got != want {
			t.Errorf("ParseLevel(%q) = %v, want %v", input, got, want)
		}
	}
}
