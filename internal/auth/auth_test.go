package auth

import (
	"context"
	"database/sql"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"pocikode/bookshelf/internal/database"
)

type memoryRepo struct {
	sessions   map[string]database.Session
	credential database.PasswordCredential
	touches    int
}

func TestCookieFlags(t *testing.T) {
	expires := time.Now().Add(24 * time.Hour)
	recorder := httptest.NewRecorder()
	SetCookie(recorder, Session{Token: "token", ExpiresAt: expires}, true, 24*time.Hour)
	cookies := recorder.Result().Cookies()
	if len(cookies) != 1 {
		t.Fatalf("cookies=%d", len(cookies))
	}
	cookie := cookies[0]
	if cookie.Name != CookieName || cookie.Path != "/" || !cookie.HttpOnly || !cookie.Secure || cookie.SameSite != http.SameSiteLaxMode || cookie.Domain != "" || cookie.MaxAge <= 0 {
		t.Fatalf("unexpected cookie: %+v", cookie)
	}
}

func (m *memoryRepo) CreateSession(_ context.Context, s database.Session) error {
	m.sessions[s.TokenHash] = s
	return nil
}
func (m *memoryRepo) FindSession(_ context.Context, h string) (database.Session, error) {
	s, ok := m.sessions[h]
	if !ok {
		return s, ErrInvalidSession
	}
	return s, nil
}
func (m *memoryRepo) TouchSession(_ context.Context, h string, t time.Time) error {
	s := m.sessions[h]
	s.LastSeenAt = t
	m.sessions[h] = s
	m.touches++
	return nil
}
func (m *memoryRepo) DeleteSession(_ context.Context, h string) error {
	delete(m.sessions, h)
	return nil
}
func (m *memoryRepo) DeleteAllSessions(context.Context) error {
	m.sessions = map[string]database.Session{}
	return nil
}
func (m *memoryRepo) GetPasswordCredential(context.Context) (database.PasswordCredential, error) {
	if m.credential.Digest == "" {
		return database.PasswordCredential{}, sql.ErrNoRows
	}
	return m.credential, nil
}
func (m *memoryRepo) SetPasswordCredential(_ context.Context, credential database.PasswordCredential) error {
	m.credential = credential
	return nil
}

func TestSessionBindingExpiryTouchAndCSRF(t *testing.T) {
	now := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	repo := &memoryRepo{sessions: map[string]database.Session{}}
	svc := New(repo, "correct horse battery", 1)
	svc.now = func() time.Time { return now }
	svc.random = &zeroReader{}
	sess, err := svc.Create(context.Background(), "browser")
	if err != nil {
		t.Fatal(err)
	}
	if !svc.ComparePassword("correct horse battery") || svc.ComparePassword("wrong") {
		t.Fatal("password comparison failed")
	}
	if !svc.ValidCSRF(sess, svc.CSRFToken(sess)) || svc.ValidCSRF(sess, "bad") {
		t.Fatal("csrf comparison failed")
	}
	if _, err = svc.Resolve(context.Background(), sess.Token); err != nil {
		t.Fatal(err)
	}
	now = now.Add(25 * time.Hour)
	if _, err = svc.Resolve(context.Background(), sess.Token); err == nil {
		t.Fatal("expired session accepted")
	}

	repo = &memoryRepo{sessions: map[string]database.Session{}}
	svc = New(repo, "correct horse battery", 5)
	svc.now = func() time.Time { return now }
	svc.random = &zeroReader{}
	sess, _ = svc.Create(context.Background(), "")
	now = now.Add(23 * time.Hour)
	_, _ = svc.Resolve(context.Background(), sess.Token)
	if repo.touches != 0 {
		t.Fatal("touched too early")
	}
	now = now.Add(2 * time.Hour)
	_, _ = svc.Resolve(context.Background(), sess.Token)
	if repo.touches != 1 {
		t.Fatal("did not touch after 24h")
	}
	changed := New(repo, "another strong password", 5)
	changed.now = svc.now
	if _, err = changed.Resolve(context.Background(), sess.Token); err == nil {
		t.Fatal("password change did not revoke")
	}
}

func TestChangePasswordPersistsAndRevokesSessions(t *testing.T) {
	repo := &memoryRepo{sessions: map[string]database.Session{}}
	svc := New(repo, "correct horse battery", 1)
	svc.now = func() time.Time { return time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC) }
	session, err := svc.Create(context.Background(), "browser")
	if err != nil {
		t.Fatal(err)
	}
	if err := svc.ChangePassword(context.Background(), "wrong", "a sufficiently long password"); !errors.Is(err, ErrInvalidCurrentPassword) {
		t.Fatalf("wrong current password error=%v", err)
	}
	if err := svc.ChangePassword(context.Background(), "correct horse battery", "short"); !errors.Is(err, ErrPasswordTooShort) {
		t.Fatalf("short password error=%v", err)
	}
	if err := svc.ChangePassword(context.Background(), "correct horse battery", "a sufficiently long password"); err != nil {
		t.Fatal(err)
	}
	if !svc.ComparePassword("a sufficiently long password") || svc.ComparePassword("correct horse battery") {
		t.Fatal("password was not changed")
	}
	if _, err := svc.Resolve(context.Background(), session.Token); err == nil {
		t.Fatal("existing session survived password change")
	}

	restarted := New(repo, "old configured password", 1)
	if err := restarted.Initialize(context.Background(), "old configured password"); err != nil {
		t.Fatal(err)
	}
	if !restarted.ComparePassword("a sufficiently long password") {
		t.Fatal("persisted password was not loaded")
	}
}

type zeroReader struct{}

func (*zeroReader) Read(p []byte) (int, error) {
	for i := range p {
		p[i] = byte(i)
	}
	return len(p), nil
}
