package config

import (
	"crypto/rand"
	"errors"
	"fmt"
	"math"
	"os"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"
)

// DefaultPort is the listen port used when PORT is not supplied.
const DefaultPort = 8070

const (
	appDirName            = "bookshelf"
	bootstrapPasswordFile = ".bootstrap-password"
	defaultMaxUploadMB    = 400
	defaultSessionDays    = 90
)

var weakPasswords = map[string]struct{}{
	"password": {}, "changeme": {}, "change-me": {}, "bookshelf": {},
	"replace-me": {}, "replace-me-with-a-strong-password": {},
}

type Config struct {
	Password       string
	DataDir        string
	Port           int
	MaxUploadBytes int64
	TrustProxy     bool
	SessionDays    int
	LogLevel       string
}

func Load() (Config, error) {
	return FromEnv(os.LookupEnv)
}

func FromEnv(lookup func(string) (string, bool)) (Config, error) {
	// An absent APP_PASSWORD is resolved later by EnsurePassword, which needs a
	// prepared DataDir. A supplied one is validated here so a bad value fails
	// before any directory work happens.
	password, ok := lookup("APP_PASSWORD")
	if !ok || password == "" {
		password = ""
	} else {
		if _, rejected := weakPasswords[strings.ToLower(password)]; rejected {
			return Config{}, errors.New("APP_PASSWORD must not be a documented placeholder")
		}
		if utf8.RuneCountInString(password) < 12 {
			return Config{}, errors.New("APP_PASSWORD must contain at least 12 Unicode code points")
		}
	}

	dataDir, err := resolveDataDir(lookup)
	if err != nil {
		return Config{}, err
	}
	port, err := integer(lookup, "PORT", DefaultPort, 1, 65535)
	if err != nil {
		return Config{}, err
	}
	maxMB, err := integer(lookup, "MAX_UPLOAD_MB", defaultMaxUploadMB, 1, math.MaxInt32)
	if err != nil {
		return Config{}, err
	}
	if int64(maxMB) > math.MaxInt64/(1024*1024) {
		return Config{}, errors.New("MAX_UPLOAD_MB is too large")
	}
	trustProxy, err := strictBool(lookup, "TRUST_PROXY", false)
	if err != nil {
		return Config{}, err
	}
	maxSessionDays := int(math.MaxInt64 / int64(24*time.Hour))
	sessionDays, err := integer(lookup, "SESSION_DAYS", defaultSessionDays, 1, maxSessionDays)
	if err != nil {
		return Config{}, err
	}
	level := valueOr(lookup, "LOG_LEVEL", "info")
	switch level {
	case "debug", "info", "warn", "error":
	default:
		return Config{}, errors.New("LOG_LEVEL must be one of debug, info, warn, error")
	}

	return Config{
		Password: password, DataDir: filepath.Clean(dataDir), Port: port,
		MaxUploadBytes: int64(maxMB) * 1024 * 1024, TrustProxy: trustProxy,
		SessionDays: sessionDays, LogLevel: level,
	}, nil
}

// EnsurePassword fills in Password when APP_PASSWORD was not supplied, so a
// freshly pulled image starts with no configuration at all. The password is
// generated once and persisted under DataDir at 0600, which makes it stable
// across restarts and unique per deployment. The bool reports whether this call
// created it; callers surface that to the operator, since a generated password
// has no other delivery channel. Requires PrepareDataDir to have run.
func (c Config) EnsurePassword() (Config, bool, error) {
	if c.Password != "" {
		return c, false, nil
	}
	path := filepath.Join(c.DataDir, bootstrapPasswordFile)
	stored, err := os.ReadFile(path)
	switch {
	case err == nil:
		password := strings.TrimRight(string(stored), "\r\n")
		if utf8.RuneCountInString(password) < 12 {
			return Config{}, false, fmt.Errorf("%s is unusable: set APP_PASSWORD, or delete the file to regenerate", path)
		}
		c.Password = password
		return c, false, nil
	case !errors.Is(err, os.ErrNotExist):
		return Config{}, false, fmt.Errorf("read %s: %w", path, err)
	}

	password := rand.Text()
	file, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600)
	if err != nil {
		return Config{}, false, fmt.Errorf("create %s: %w", path, err)
	}
	if _, err := file.WriteString(password + "\n"); err != nil {
		file.Close()
		return Config{}, false, fmt.Errorf("write %s: %w", path, err)
	}
	if err := file.Close(); err != nil {
		return Config{}, false, fmt.Errorf("write %s: %w", path, err)
	}
	c.Password = password
	return c, true, nil
}

// BootstrapPasswordPath is where a generated password is stored.
func (c Config) BootstrapPasswordPath() string {
	return filepath.Join(c.DataDir, bootstrapPasswordFile)
}

func (c Config) PrepareDataDir() error {
	for _, name := range []string{"", "books", "covers", "uploads", "trash"} {
		path := filepath.Join(c.DataDir, name)
		if err := os.MkdirAll(path, 0o750); err != nil {
			return fmt.Errorf("DATA_DIR create %q: %w", name, err)
		}
		if name != "" {
			info, err := os.Lstat(path)
			if err != nil || !info.IsDir() || info.Mode()&os.ModeSymlink != 0 {
				return fmt.Errorf("DATA_DIR child %q must be a real directory", name)
			}
		}
	}
	f, err := os.CreateTemp(filepath.Join(c.DataDir, "uploads"), ".write-check-")
	if err != nil {
		return fmt.Errorf("DATA_DIR must be writable: %w", err)
	}
	name := f.Name()
	if closeErr := f.Close(); closeErr != nil {
		return fmt.Errorf("DATA_DIR write check: %w", closeErr)
	}
	if err := os.Remove(name); err != nil {
		return fmt.Errorf("DATA_DIR cleanup check: %w", err)
	}
	return nil
}

// resolveDataDir honours DATA_DIR when it carries a value and otherwise falls
// back to the per-user data directory for the host platform, so a plain binary
// run needs no environment at all. Container images pin DATA_DIR themselves.
func resolveDataDir(lookup func(string) (string, bool)) (string, error) {
	if value, ok := lookup("DATA_DIR"); ok && strings.TrimSpace(value) != "" {
		if !filepath.IsAbs(value) {
			return "", errors.New("DATA_DIR must be a non-empty absolute path")
		}
		return filepath.Clean(value), nil
	}
	dir, err := defaultDataDir(lookup)
	if err != nil {
		return "", err
	}
	if !filepath.IsAbs(dir) {
		return "", errors.New("DATA_DIR must be a non-empty absolute path")
	}
	return filepath.Clean(dir), nil
}

func defaultDataDir(lookup func(string) (string, bool)) (string, error) {
	if runtime.GOOS != "windows" && runtime.GOOS != "darwin" {
		if base := absOrEmpty(lookup, "XDG_DATA_HOME"); base != "" {
			return filepath.Join(base, appDirName), nil
		}
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return "", fmt.Errorf("DATA_DIR is unset and the home directory is unknown: %w", err)
	}
	switch runtime.GOOS {
	case "darwin":
		return filepath.Join(home, "Library", "Application Support", appDirName), nil
	case "windows":
		if base := absOrEmpty(lookup, "LOCALAPPDATA"); base != "" {
			return filepath.Join(base, appDirName), nil
		}
		return filepath.Join(home, "AppData", "Local", appDirName), nil
	default:
		return filepath.Join(home, ".local", "share", appDirName), nil
	}
}

func absOrEmpty(lookup func(string) (string, bool), name string) string {
	value, ok := lookup(name)
	if !ok || !filepath.IsAbs(value) {
		return ""
	}
	return value
}

func valueOr(lookup func(string) (string, bool), name, fallback string) string {
	if value, ok := lookup(name); ok {
		return value
	}
	return fallback
}

func integer(lookup func(string) (string, bool), name string, fallback, min, max int) (int, error) {
	value, ok := lookup(name)
	if !ok {
		return fallback, nil
	}
	n, err := strconv.Atoi(value)
	if err != nil || n < min || n > max {
		return 0, fmt.Errorf("%s must be an integer between %d and %d", name, min, max)
	}
	return n, nil
}

func strictBool(lookup func(string) (string, bool), name string, fallback bool) (bool, error) {
	value, ok := lookup(name)
	if !ok {
		return fallback, nil
	}
	if value == "true" {
		return true, nil
	}
	if value == "false" {
		return false, nil
	}
	return false, fmt.Errorf("%s must be exactly true or false", name)
}
