package httpapi

import (
	"net/http/httptest"
	"testing"
	"time"
)

func TestLoginDelayProgressesAndCaps(t *testing.T) {
	tracker := loginDelayTracker{attempts: make(map[string]loginAttempt)}
	now := time.Unix(100, 0)
	key := "client\x00user"
	want := []time.Duration{0, 250 * time.Millisecond, 500 * time.Millisecond, time.Second, 2 * time.Second, 4 * time.Second, 8 * time.Second, 8 * time.Second}

	for index, expected := range want {
		if got := tracker.delay(key, now); got != expected {
			t.Fatalf("attempt %d delay = %s, want %s", index, got, expected)
		}
		tracker.failed(key, now)
	}
}

func TestLoginDelayExpires(t *testing.T) {
	tracker := loginDelayTracker{attempts: make(map[string]loginAttempt)}
	now := time.Unix(100, 0)
	key := "client\x00user"
	tracker.failed(key, now)

	if got := tracker.delay(key, now.Add(loginAttemptTTL)); got != 0 {
		t.Fatalf("expired delay = %s, want 0", got)
	}
}

func TestLoginDelaySuccessResets(t *testing.T) {
	tracker := loginDelayTracker{attempts: make(map[string]loginAttempt)}
	now := time.Unix(100, 0)
	key := "client\x00user"
	tracker.failed(key, now)
	tracker.failed(key, now)
	tracker.succeeded(key)

	if got := tracker.delay(key, now); got != 0 {
		t.Fatalf("reset delay = %s, want 0", got)
	}
}

func TestLoginAttemptKeyUsesTrustedProxyAddress(t *testing.T) {
	req := httptest.NewRequest("POST", "/api/auth/login", nil)
	req.RemoteAddr = "127.0.0.1:3000"
	req.Header.Set("X-Real-IP", "192.0.2.10")

	if got, want := loginAttemptKey(req, " Reader "), "192.0.2.10\x00reader"; got != want {
		t.Fatalf("key = %q, want %q", got, want)
	}
}
