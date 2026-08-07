package config

import (
	"errors"
	"fmt"
	"math"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"
)

const (
	defaultDataDir     = "/data"
	defaultPort        = 8080
	defaultMaxUploadMB = 400
	defaultSessionDays = 90
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
	password, ok := lookup("APP_PASSWORD")
	if !ok || password == "" {
		return Config{}, errors.New("APP_PASSWORD is required")
	}
	if _, rejected := weakPasswords[strings.ToLower(password)]; rejected {
		return Config{}, errors.New("APP_PASSWORD must not be a documented placeholder")
	}
	if utf8.RuneCountInString(password) < 12 {
		return Config{}, errors.New("APP_PASSWORD must contain at least 12 Unicode code points")
	}

	dataDir := valueOr(lookup, "DATA_DIR", defaultDataDir)
	if dataDir == "" || !filepath.IsAbs(dataDir) {
		return Config{}, errors.New("DATA_DIR must be a non-empty absolute path")
	}
	port, err := integer(lookup, "PORT", defaultPort, 1, 65535)
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
