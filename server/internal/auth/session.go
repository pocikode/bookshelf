package auth

import (
	"crypto/rand"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"errors"
	"net/http"
	"time"

	"github.com/google/uuid"
)

const sessionCookie = "readest_session"

type User struct {
	ID       string `json:"id"`
	Username string `json:"username"`
}

type Session struct {
	User      User
	Token     string
	TokenHash string
	CSRFToken string
	ExpiresAt time.Time
}

type Store interface {
	Exec(query string, args ...any) (sql.Result, error)
	QueryRow(query string, args ...any) *sql.Row
}

func CreateSession(db Store, user User, secure bool) (Session, error) {
	token, err := randomToken(32)
	if err != nil {
		return Session{}, err
	}
	csrf, err := randomToken(32)
	if err != nil {
		return Session{}, err
	}
	now := time.Now().UTC()
	expires := now.Add(30 * 24 * time.Hour)
	_, err = db.Exec(`INSERT INTO sessions (token_hash, user_id, csrf_hash, expires_at, created_at, last_seen_at) VALUES (?, ?, ?, ?, ?, ?)`, hashToken(token), user.ID, hashToken(csrf), expires.UnixMilli(), now.UnixMilli(), now.UnixMilli())
	if err != nil {
		return Session{}, err
	}
	return Session{User: user, Token: token, TokenHash: hashToken(token), CSRFToken: csrf, ExpiresAt: expires}, nil
}

func SetCookie(w http.ResponseWriter, token string, secure bool) {
	http.SetCookie(w, &http.Cookie{Name: sessionCookie, Value: token, Path: "/", HttpOnly: true, Secure: secure, SameSite: http.SameSiteLaxMode, Expires: time.Now().Add(30 * 24 * time.Hour)})
}

func SetCSRFCookie(w http.ResponseWriter, token string, secure bool) {
	http.SetCookie(w, &http.Cookie{Name: "readest_csrf", Value: token, Path: "/", Secure: secure, SameSite: http.SameSiteLaxMode, Expires: time.Now().Add(30 * 24 * time.Hour)})
}

func ClearCookie(w http.ResponseWriter, secure bool) {
	http.SetCookie(w, &http.Cookie{Name: sessionCookie, Value: "", Path: "/", HttpOnly: true, Secure: secure, SameSite: http.SameSiteLaxMode, MaxAge: -1})
	http.SetCookie(w, &http.Cookie{Name: "readest_csrf", Value: "", Path: "/", Secure: secure, SameSite: http.SameSiteLaxMode, MaxAge: -1})
}

func LoadSession(db Store, r *http.Request) (Session, error) {
	cookie, err := r.Cookie(sessionCookie)
	if err != nil {
		return Session{}, errors.New("not authenticated")
	}
	var session Session
	var expires int64
	err = db.QueryRow(`SELECT s.token_hash, s.csrf_hash, s.expires_at, u.id, u.username FROM sessions s JOIN users u ON u.id = s.user_id WHERE s.token_hash = ?`, hashToken(cookie.Value)).Scan(&session.TokenHash, &session.CSRFToken, &expires, &session.User.ID, &session.User.Username)
	if err != nil || time.Now().UnixMilli() >= expires {
		return Session{}, errors.New("not authenticated")
	}
	session.ExpiresAt = time.UnixMilli(expires)
	return session, nil
}

func VerifyCSRF(session Session, r *http.Request) bool {
	return r.Header.Get("X-CSRF-Token") != "" && hashToken(r.Header.Get("X-CSRF-Token")) == session.CSRFToken
}

func SessionCookieName() string { return sessionCookie }
func NewID() string             { return uuid.NewString() }

func randomToken(size int) (string, error) {
	buf := make([]byte, size)
	if _, err := rand.Read(buf); err != nil {
		return "", err
	}
	return hex.EncodeToString(buf), nil
}

func hashToken(value string) string {
	hash := sha256.Sum256([]byte(value))
	return hex.EncodeToString(hash[:])
}
