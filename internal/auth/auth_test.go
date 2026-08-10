package auth

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
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
	user       database.User
	userDigest string
	getUserErr error
	setUserErr error
	deleteErr  error
	createErr  error
	ensureErr  error
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
	if m.createErr != nil {
		return m.createErr
	}
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
	if m.deleteErr != nil {
		return m.deleteErr
	}
	delete(m.sessions, h)
	return nil
}
func (m *memoryRepo) DeleteAllSessions(context.Context) error {
	if m.deleteErr != nil {
		return m.deleteErr
	}
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
func (m *memoryRepo) FindUserByUsername(context.Context, string) (database.User, error) {
	if m.user.Username == "" {
		return database.User{}, sql.ErrNoRows
	}
	return m.user, nil
}
func (m *memoryRepo) GetUserPasswordDigest(context.Context, int64) (string, error) {
	return m.userDigest, m.getUserErr
}
func (m *memoryRepo) SetUserPassword(context.Context, int64, string) error { return m.setUserErr }
func (m *memoryRepo) DeleteUserSessions(context.Context, int64) error      { return m.deleteErr }
func (m *memoryRepo) EnsureAdmin(context.Context, string, time.Time) error { return m.ensureErr }

type randomError struct{}

func (randomError) Read([]byte) (int, error) { return 0, errors.New("random failure") }

func TestInitializeAuthenticateAndCookieBranches(t *testing.T) {
	ctx := context.Background()
	now := time.Date(2026, 3, 4, 5, 6, 7, 0, time.UTC)
	repo := &memoryRepo{}
	svc := New(repo, "configured", 1)
	svc.now = func() time.Time { return now }
	if err := svc.Initialize(ctx, "fallback"); err != nil || !svc.ComparePassword("fallback") {
		t.Fatalf("initialize fallback: %v", err)
	}
	if repo.credential.UpdatedAt != now {
		t.Fatalf("credential timestamp = %v", repo.credential.UpdatedAt)
	}
	repo.credential.Digest = "invalid"
	if err := svc.Initialize(ctx, "fallback"); err == nil {
		t.Fatal("accepted invalid credential")
	}
	repo.credential.Digest = hexDigest("stored")
	repo.ensureErr = errors.New("ensure failed")
	if err := svc.Initialize(ctx, "fallback"); !errors.Is(err, repo.ensureErr) {
		t.Fatalf("ensure error = %v", err)
	}
	repo.ensureErr = nil
	if _, err := svc.Authenticate(ctx, "reader", "secret"); !errors.Is(err, ErrInvalidCredentials) {
		t.Fatalf("missing user error = %v", err)
	}
	repo.user = database.User{ID: 2, Username: "reader", Disabled: true}
	if _, err := svc.Authenticate(ctx, "reader", "secret"); !errors.Is(err, ErrInvalidCredentials) {
		t.Fatalf("disabled user error = %v", err)
	}
	repo.user.Disabled = false
	repo.getUserErr = errors.New("lookup failed")
	if _, err := svc.Authenticate(ctx, "reader", "secret"); !errors.Is(err, ErrInvalidCredentials) {
		t.Fatalf("digest lookup error = %v", err)
	}
	repo.getUserErr = nil
	repo.userDigest = "bad"
	if _, err := svc.Authenticate(ctx, "reader", "secret"); !errors.Is(err, ErrInvalidCredentials) {
		t.Fatalf("malformed digest error = %v", err)
	}
	repo.userDigest = hexDigest("other")
	if _, err := svc.Authenticate(ctx, "reader", "secret"); !errors.Is(err, ErrInvalidCredentials) {
		t.Fatalf("wrong password error = %v", err)
	}
	repo.userDigest = hexDigest("secret")
	user, err := svc.Authenticate(ctx, "reader", "secret")
	if err != nil || user.ID != 2 {
		t.Fatalf("authenticate = %+v, %v", user, err)
	}

	recorder := httptest.NewRecorder()
	ClearCookie(recorder, false)
	cookie := recorder.Result().Cookies()[0]
	if cookie.Name != CookieName || cookie.MaxAge != -1 || cookie.Value != "" || cookie.Expires.Unix() != 1 {
		t.Fatalf("clear cookie = %+v", cookie)
	}
}

func TestInitializeAndSessionRepositoryErrors(t *testing.T) {
	ctx := context.Background()
	repo := &memoryRepo{}
	svc := New(repo, "configured", 1)
	getErr := errors.New("credential lookup failed")
	repo.credential = database.PasswordCredential{}
	// A repository error other than sql.ErrNoRows must be returned unchanged.
	badRepo := &errorCredentialRepo{getErr: getErr}
	if err := New(badRepo, "configured", 1).Initialize(ctx, "fallback"); !errors.Is(err, getErr) {
		t.Fatalf("credential error = %v", err)
	}
	badRepo.getErr = sql.ErrNoRows
	badRepo.setErr = errors.New("credential write failed")
	if err := New(badRepo, "configured", 1).Initialize(ctx, "fallback"); !errors.Is(err, badRepo.setErr) {
		t.Fatalf("credential write error = %v", err)
	}

	svc.random = randomError{}
	if _, err := svc.Create(ctx, "agent"); err == nil {
		t.Fatal("random error was ignored")
	}
	repo.createErr = errors.New("session write failed")
	svc.random = &zeroReader{}
	if _, err := svc.Create(ctx, "agent"); !errors.Is(err, repo.createErr) {
		t.Fatalf("session write error = %v", err)
	}
	repo.deleteErr = errors.New("delete failed")
	if err := svc.Delete(ctx, Session{TokenHash: "hash"}); !errors.Is(err, repo.deleteErr) {
		t.Fatalf("delete error = %v", err)
	}
	if err := svc.DeleteAll(ctx); !errors.Is(err, repo.deleteErr) {
		t.Fatalf("delete all error = %v", err)
	}
}

func TestChangeUserPasswordBranches(t *testing.T) {
	ctx := context.Background()
	repo := &memoryRepo{userDigest: hexDigest("current")}
	svc := New(repo, "admin current", 1)
	if err := svc.ChangeUserPassword(ctx, 2, "current", "short"); !errors.Is(err, ErrPasswordTooShort) {
		t.Fatalf("short password = %v", err)
	}
	if err := svc.ChangeUserPassword(ctx, 2, "wrong", "new strong"); !errors.Is(err, ErrInvalidCurrentPassword) {
		t.Fatalf("wrong password = %v", err)
	}
	repo.userDigest = "bad"
	if err := svc.ChangeUserPassword(ctx, 2, "current", "new strong"); !errors.Is(err, ErrInvalidCurrentPassword) {
		t.Fatalf("bad digest = %v", err)
	}
	repo.userDigest = hexDigest("current")
	repo.setUserErr = errors.New("password write failed")
	if err := svc.ChangeUserPassword(ctx, 2, "current", "new strong"); !errors.Is(err, repo.setUserErr) {
		t.Fatalf("password write error = %v", err)
	}
	repo.setUserErr = nil
	repo.deleteErr = errors.New("session delete failed")
	if err := svc.ChangeUserPassword(ctx, 2, "current", "new strong"); !errors.Is(err, repo.deleteErr) {
		t.Fatalf("session delete error = %v", err)
	}
	repo.deleteErr = nil
	if err := svc.ChangeUserPassword(ctx, 2, "current", "new strong"); err != nil {
		t.Fatal(err)
	}

	repo.credential = database.PasswordCredential{Digest: hexDigest("admin current")}
	if err := svc.Initialize(ctx, "fallback"); err != nil {
		t.Fatal(err)
	}
	repo.setUserErr = errors.New("admin account write failed")
	if err := svc.ChangeUserPassword(ctx, 1, "admin current", "admin next"); !errors.Is(err, repo.setUserErr) {
		t.Fatalf("admin account write error = %v", err)
	}
	if err := svc.ChangeUserPassword(ctx, 1, "wrong", "admin next"); !errors.Is(err, ErrInvalidCurrentPassword) {
		t.Fatalf("admin current password error = %v", err)
	}
}

func hexDigest(value string) string {
	digest := sha256.Sum256([]byte(value))
	return hex.EncodeToString(digest[:])
}

type errorCredentialRepo struct {
	getErr error
	setErr error
}

func (r *errorCredentialRepo) GetPasswordCredential(context.Context) (database.PasswordCredential, error) {
	return database.PasswordCredential{}, r.getErr
}
func (r *errorCredentialRepo) SetPasswordCredential(context.Context, database.PasswordCredential) error {
	return r.setErr
}
func (r *errorCredentialRepo) CreateSession(context.Context, database.Session) error { return nil }
func (r *errorCredentialRepo) FindSession(context.Context, string) (database.Session, error) {
	return database.Session{}, nil
}
func (r *errorCredentialRepo) TouchSession(context.Context, string, time.Time) error { return nil }
func (r *errorCredentialRepo) DeleteSession(context.Context, string) error           { return nil }
func (r *errorCredentialRepo) DeleteAllSessions(context.Context) error               { return nil }

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
	if err := svc.ChangePassword(context.Background(), "correct horse battery", "1234567"); !errors.Is(err, ErrPasswordTooShort) {
		t.Fatalf("short password error=%v", err)
	}
	if err := svc.ChangePassword(context.Background(), "correct horse battery", "p@ssw0rd"); err != nil {
		t.Fatal(err)
	}
	if !svc.ComparePassword("p@ssw0rd") || svc.ComparePassword("correct horse battery") {
		t.Fatal("password was not changed")
	}
	if _, err := svc.Resolve(context.Background(), session.Token); err == nil {
		t.Fatal("existing session survived password change")
	}

	restarted := New(repo, "old configured password", 1)
	if err := restarted.Initialize(context.Background(), "old configured password"); err != nil {
		t.Fatal(err)
	}
	if !restarted.ComparePassword("p@ssw0rd") {
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
