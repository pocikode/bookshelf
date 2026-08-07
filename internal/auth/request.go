package auth

import (
	"net"
	"net/http"
	"net/url"
	"strings"
)

func ClientIP(r *http.Request, trustProxy bool) string {
	if trustProxy {
		first, _, _ := strings.Cut(r.Header.Get("X-Forwarded-For"), ",")
		if ip, err := netip(strings.TrimSpace(first)); err == nil {
			return ip
		}
	}
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		host = r.RemoteAddr
	}
	if ip, err := netip(strings.TrimSpace(host)); err == nil {
		return ip
	}
	return "unknown"
}

func IsSecure(r *http.Request, trustProxy bool) bool {
	if r.TLS != nil {
		return true
	}
	if !trustProxy {
		return false
	}
	return strings.EqualFold(strings.TrimSpace(r.Header.Get("X-Forwarded-Proto")), "https")
}

func SameOrigin(r *http.Request, trustProxy ...bool) bool {
	if strings.EqualFold(r.Header.Get("Sec-Fetch-Site"), "cross-site") {
		return false
	}
	origin := r.Header.Get("Origin")
	if origin == "" {
		return true
	}
	u, err := url.Parse(origin)
	if err != nil || u.Host == "" {
		return false
	}
	trusted := false
	if len(trustProxy) > 0 {
		trusted = trustProxy[0]
	}
	scheme := "http"
	if IsSecure(r, trusted) {
		scheme = "https"
	}
	return strings.EqualFold(u.Host, r.Host) && strings.EqualFold(u.Scheme, scheme)
}

func netip(value string) (string, error) {
	ip := net.ParseIP(value)
	if ip == nil {
		return "", &net.AddrError{Err: "invalid IP address", Addr: value}
	}
	return ip.String(), nil
}
