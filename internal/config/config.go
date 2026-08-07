package config

import (
	"errors"
	"fmt"
	"math"
	"os"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"time"
)

// DefaultPort is the listen port used when PORT is not supplied.
const DefaultPort = 8070

const (
	appDirName         = "bookshelf"
	defaultMaxUploadMB = 400
	defaultSessionDays = 90
)

// DefaultPassword is used when APP_PASSWORD is not supplied. It is deliberately
// trivial so a fresh install needs no configuration at all; anything reachable
// beyond a trusted network should set APP_PASSWORD.
const DefaultPassword = "123456"

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
	// Any supplied APP_PASSWORD is used exactly as given, with no strength or
	// content rules. An absent or empty one falls back to DefaultPassword.
	password, ok := lookup("APP_PASSWORD")
	if !ok || password == "" {
		password = DefaultPassword
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

// UsingDefaultPassword reports whether the effective password is the built-in
// default, so startup can warn the operator.
func (c Config) UsingDefaultPassword() bool {
	return c.Password == DefaultPassword
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
