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
	"sync"
	"time"
	"unicode/utf8"

	"pocikode/bookshelf/internal/database"
)

const CookieName = "bookshelf_session"

var ErrInvalidSession = errors.New("invalid session")
var ErrInvalidCurrentPassword = errors.New("invalid current password")
var ErrPasswordTooShort = errors.New("password is too short")

const MinPasswordLength = 8

type repository interface {
	CreateSession(context.Context, database.Session) error
	FindSession(context.Context, string) (database.Session, error)
	TouchSession(context.Context, string, time.Time) error
	DeleteSession(context.Context, string) error
	DeleteAllSessions(context.Context) error
	GetPasswordCredential(context.Context) (database.PasswordCredential, error)
	SetPasswordCredential(context.Context, database.PasswordCredential) error
}

type userRepository interface {
	FindUserByUsername(context.Context, string) (database.User, error)
	GetUserPasswordDigest(context.Context, int64) (string, error)
}
type adminRepository interface {
	EnsureAdmin(context.Context, string, time.Time) error
}
type accountRepository interface {
	GetUserPasswordDigest(context.Context, int64) (string, error)
	SetUserPassword(context.Context, int64, string) error
	DeleteUserSessions(context.Context, int64) error
}

type Service struct {
	repo       repository
	password   [sha256.Size]byte
	passwordMu sync.RWMutex
	random     io.Reader
	now        func() time.Time
	sessionTTL time.Duration
}

type Session struct {
	UserID    int64
	Token     string
	TokenHash string
	RawToken  []byte
	ExpiresAt time.Time
}

var ErrInvalidCredentials = errors.New("invalid credentials")

func New(repo repository, password string, sessionDays int) *Service {
	return &Service{repo: repo, password: sha256.Sum256([]byte(password)), random: rand.Reader, now: time.Now, sessionTTL: time.Duration(sessionDays) * 24 * time.Hour}
}

func (s *Service) Initialize(ctx context.Context, fallback string) error {
	credential, err := s.repo.GetPasswordCredential(ctx)
	if database.IsNotFound(err) {
		digest := sha256.Sum256([]byte(fallback))
		credential = database.PasswordCredential{Digest: hex.EncodeToString(digest[:]), UpdatedAt: s.now().UTC()}
		if err := s.repo.SetPasswordCredential(ctx, credential); err != nil {
			return err
		}
	} else if err != nil {
		return err
	}
	digest, err := hex.DecodeString(credential.Digest)
	if err != nil || len(digest) != sha256.Size {
		return errors.New("invalid stored password credential")
	}
	s.passwordMu.Lock()
	copy(s.password[:], digest)
	s.passwordMu.Unlock()
	if admins, ok := s.repo.(adminRepository); ok {
		if err := admins.EnsureAdmin(ctx, credential.Digest, s.now().UTC()); err != nil {
			return err
		}
	}
	return nil
}

func (s *Service) ComparePassword(submitted string) bool {
	digest := sha256.Sum256([]byte(submitted))
	s.passwordMu.RLock()
	defer s.passwordMu.RUnlock()
	return subtle.ConstantTimeCompare(digest[:], s.password[:]) == 1
}

func (s *Service) Authenticate(ctx context.Context, username, submitted string) (database.User, error) {
	r, ok := s.repo.(userRepository)
	if !ok {
		return database.User{}, ErrInvalidCredentials
	}
	user, err := r.FindUserByUsername(ctx, username)
	if err != nil || user.Disabled {
		return database.User{}, ErrInvalidCredentials
	}
	digest, err := r.GetUserPasswordDigest(ctx, user.ID)
	if err != nil {
		return database.User{}, ErrInvalidCredentials
	}
	want, err := hex.DecodeString(digest)
	got := sha256.Sum256([]byte(submitted))
	if err != nil || len(want) != sha256.Size || subtle.ConstantTimeCompare(got[:], want) != 1 {
		return database.User{}, ErrInvalidCredentials
	}
	return user, nil
}

func (s *Service) ChangePassword(ctx context.Context, current, next string) error {
	if utf8.RuneCountInString(next) < MinPasswordLength {
		return ErrPasswordTooShort
	}
	digest := sha256.Sum256([]byte(next))
	currentDigest := sha256.Sum256([]byte(current))
	s.passwordMu.Lock()
	defer s.passwordMu.Unlock()
	if subtle.ConstantTimeCompare(currentDigest[:], s.password[:]) != 1 {
		return ErrInvalidCurrentPassword
	}
	credential := database.PasswordCredential{Digest: hex.EncodeToString(digest[:]), UpdatedAt: s.now().UTC()}
	if err := s.repo.SetPasswordCredential(ctx, credential); err != nil {
		return err
	}
	s.password = digest
	return s.repo.DeleteAllSessions(ctx)
}

func (s *Service) ChangeUserPassword(ctx context.Context, userID int64, current, next string) error {
	if utf8.RuneCountInString(next) < MinPasswordLength {
		return ErrPasswordTooShort
	}
	r, ok := s.repo.(accountRepository)
	if !ok {
		return ErrInvalidCurrentPassword
	}
	if userID == 1 {
		if err := s.ChangePassword(ctx, current, next); err != nil {
			return err
		}
		digest := sha256.Sum256([]byte(next))
		return r.SetUserPassword(ctx, userID, hex.EncodeToString(digest[:]))
	}
	stored, err := r.GetUserPasswordDigest(ctx, userID)
	if err != nil {
		return ErrInvalidCurrentPassword
	}
	want, err := hex.DecodeString(stored)
	submitted := sha256.Sum256([]byte(current))
	if err != nil || len(want) != sha256.Size || subtle.ConstantTimeCompare(submitted[:], want) != 1 {
		return ErrInvalidCurrentPassword
	}
	digest := sha256.Sum256([]byte(next))
	if err := r.SetUserPassword(ctx, userID, hex.EncodeToString(digest[:])); err != nil {
		return err
	}
	return r.DeleteUserSessions(ctx, userID)
}

func (s *Service) Create(ctx context.Context, userAgent string) (Session, error) {
	return s.CreateForUser(ctx, 1, userAgent)
}
func (s *Service) CreateForUser(ctx context.Context, userID int64, userAgent string) (Session, error) {
	raw := make([]byte, 32)
	if _, err := io.ReadFull(s.random, raw); err != nil {
		return Session{}, err
	}
	token := base64.RawURLEncoding.EncodeToString(raw)
	hash := sha256.Sum256(raw)
	now := s.now().UTC()
	session := Session{UserID: userID, Token: token, TokenHash: hex.EncodeToString(hash[:]), RawToken: raw, ExpiresAt: now.Add(s.sessionTTL)}
	record := database.Session{UserID: userID, TokenHash: session.TokenHash, PasswordBinding: hex.EncodeToString(s.binding(raw)), CreatedAt: now, LastSeenAt: now, ExpiresAt: session.ExpiresAt, UserAgent: truncate(userAgent, 512)}
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
	return Session{UserID: record.UserID, Token: token, TokenHash: hashText, RawToken: raw, ExpiresAt: record.ExpiresAt}, nil
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
	s.passwordMu.RLock()
	defer s.passwordMu.RUnlock()
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
