package auth

import (
	"context"
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"io"
	"net/http"
	"time"

	"pocikode/bookshelf/internal/database"
)

const CookieName = "bookshelf_session"

var ErrInvalidSession = errors.New("invalid session")

type sessionRepository interface {
	CreateSession(context.Context, database.Session) error
	FindSession(context.Context, string) (database.Session, error)
	TouchSession(context.Context, string, time.Time) error
	DeleteSession(context.Context, string) error
	DeleteAllSessions(context.Context) error
}

type Service struct {
	repo       sessionRepository
	password   [sha256.Size]byte
	random     io.Reader
	now        func() time.Time
	sessionTTL time.Duration
}

type Session struct {
	Token     string
	TokenHash string
	RawToken  []byte
	ExpiresAt time.Time
}

func New(repo sessionRepository, password string, sessionDays int) *Service {
	return &Service{repo: repo, password: sha256.Sum256([]byte(password)), random: rand.Reader, now: time.Now, sessionTTL: time.Duration(sessionDays) * 24 * time.Hour}
}

func (s *Service) ComparePassword(submitted string) bool {
	digest := sha256.Sum256([]byte(submitted))
	return subtle.ConstantTimeCompare(digest[:], s.password[:]) == 1
}

func (s *Service) Create(ctx context.Context, userAgent string) (Session, error) {
	raw := make([]byte, 32)
	if _, err := io.ReadFull(s.random, raw); err != nil {
		return Session{}, err
	}
	token := base64.RawURLEncoding.EncodeToString(raw)
	hash := sha256.Sum256(raw)
	now := s.now().UTC()
	session := Session{Token: token, TokenHash: hex.EncodeToString(hash[:]), RawToken: raw, ExpiresAt: now.Add(s.sessionTTL)}
	record := database.Session{TokenHash: session.TokenHash, PasswordBinding: hex.EncodeToString(s.binding(raw)), CreatedAt: now, LastSeenAt: now, ExpiresAt: session.ExpiresAt, UserAgent: truncate(userAgent, 512)}
	if err := s.repo.CreateSession(ctx, record); err != nil {
		return Session{}, err
	}
	return session, nil
}

func (s *Service) Resolve(ctx context.Context, token string) (Session, error) {
	raw, err := base64.RawURLEncoding.DecodeString(token)
	if err != nil || len(raw) != 32 {
		return Session{}, ErrInvalidSession
	}
	hash := sha256.Sum256(raw)
	hashText := hex.EncodeToString(hash[:])
	record, err := s.repo.FindSession(ctx, hashText)
	if err != nil {
		return Session{}, ErrInvalidSession
	}
	expectedBinding := s.binding(raw)
	storedBinding, err := hex.DecodeString(record.PasswordBinding)
	if err != nil || len(storedBinding) != sha256.Size || subtle.ConstantTimeCompare(storedBinding, expectedBinding) != 1 {
		_ = s.repo.DeleteSession(ctx, hashText)
		return Session{}, ErrInvalidSession
	}
	now := s.now().UTC()
	if !now.Before(record.ExpiresAt) {
		_ = s.repo.DeleteSession(ctx, hashText)
		return Session{}, ErrInvalidSession
	}
	if now.Sub(record.LastSeenAt) >= 24*time.Hour {
		if err := s.repo.TouchSession(ctx, hashText, now); err != nil {
			return Session{}, err
		}
	}
	return Session{Token: token, TokenHash: hashText, RawToken: raw, ExpiresAt: record.ExpiresAt}, nil
}

func (s *Service) Delete(ctx context.Context, session Session) error {
	return s.repo.DeleteSession(ctx, session.TokenHash)
}
func (s *Service) DeleteAll(ctx context.Context) error { return s.repo.DeleteAllSessions(ctx) }

func (s *Service) CSRFToken(session Session) string {
	digest := csrfDigest(session.RawToken)
	return base64.RawURLEncoding.EncodeToString(digest[:])
}

func (s *Service) ValidCSRF(session Session, provided string) bool {
	got, err := base64.RawURLEncoding.DecodeString(provided)
	if err != nil || len(got) != sha256.Size {
		return false
	}
	want := csrfDigest(session.RawToken)
	return subtle.ConstantTimeCompare(got, want[:]) == 1
}

func csrfDigest(raw []byte) [sha256.Size]byte {
	h := sha256.New()
	_, _ = h.Write([]byte("bookshelf-csrf-v1"))
	_, _ = h.Write([]byte{0})
	_, _ = h.Write(raw)
	var out [sha256.Size]byte
	copy(out[:], h.Sum(nil))
	return out
}

func (s *Service) binding(raw []byte) []byte {
	h := hmac.New(sha256.New, s.password[:])
	_, _ = h.Write(raw)
	return h.Sum(nil)
}

func SetCookie(w http.ResponseWriter, session Session, secure bool, ttl time.Duration) {
	http.SetCookie(w, &http.Cookie{Name: CookieName, Value: session.Token, Path: "/", HttpOnly: true, Secure: secure, SameSite: http.SameSiteLaxMode, MaxAge: int(ttl.Seconds()), Expires: session.ExpiresAt})
}

func ClearCookie(w http.ResponseWriter, secure bool) {
	http.SetCookie(w, &http.Cookie{Name: CookieName, Value: "", Path: "/", HttpOnly: true, Secure: secure, SameSite: http.SameSiteLaxMode, MaxAge: -1, Expires: time.Unix(1, 0)})
}

func truncate(s string, n int) string {
	r := []rune(s)
	if len(r) > n {
		return string(r[:n])
	}
	return s
}
