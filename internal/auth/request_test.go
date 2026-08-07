package auth

import (
	"net/http/httptest"
	"testing"
)

func TestClientIPAndSecureProxyBoundary(t *testing.T) {
	r := httptest.NewRequest("GET", "http://example.test/", nil)
	r.RemoteAddr = "192.0.2.10:1234"
	r.Header.Set("X-Forwarded-For", "203.0.113.8, 192.0.2.10")
	r.Header.Set("X-Forwarded-Proto", "https")
	if got := ClientIP(r, false); got != "192.0.2.10" {
		t.Fatal(got)
	}
	if IsSecure(r, false) {
		t.Fatal("trusted spoofed proto")
	}
	if got := ClientIP(r, true); got != "203.0.113.8" {
		t.Fatal(got)
	}
	if !IsSecure(r, true) {
		t.Fatal("ignored trusted proto")
	}
	r.Header.Set("X-Forwarded-For", "not-an-ip")
	if got := ClientIP(r, true); got != "192.0.2.10" {
		t.Fatalf("malformed XFF fallback = %s", got)
	}
}
func TestSameOrigin(t *testing.T) {
	r := httptest.NewRequest("POST", "https://books.example/api", nil)
	r.Header.Set("Origin", "https://books.example")
	if !SameOrigin(r) {
		t.Fatal("same origin rejected")
	}
	r.Header.Set("Sec-Fetch-Site", "cross-site")
	if SameOrigin(r) {
		t.Fatal("cross-site accepted")
	}
}
