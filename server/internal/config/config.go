package config

import (
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"os"
	"path/filepath"
	"strconv"

	"github.com/joho/godotenv"
)

type Config struct {
	DataDir       string
	Port          string
	SessionSecret string
	StaticDir     string
	LogLevel      string
	LogFormat     string
	MaxUpload     int64
	SecureCookie  bool
}

func Load() (Config, error) {
	// A missing .env is normal; a malformed one is not, and silently ignoring
	// it hides why configuration looks wrong. Only report the second case.
	if err := godotenv.Load(".env"); err != nil && !os.IsNotExist(err) {
		return Config{}, fmt.Errorf("load .env: %w", err)
	}
	dataDir := getenv("DATA_DIR", "./data")
	port := getenv("PORT", "3000")
	secret := os.Getenv("SESSION_SECRET")
	if secret == "" {
		if os.Getenv("ENV") == "production" {
			return Config{}, fmt.Errorf("SESSION_SECRET is required in production")
		}
		buf := make([]byte, 32)
		if _, err := rand.Read(buf); err != nil {
			return Config{}, err
		}
		secret = hex.EncodeToString(buf)
	}
	maxUpload := int64(512 << 20)
	if value := os.Getenv("MAX_UPLOAD_BYTES"); value != "" {
		parsed, err := strconv.ParseInt(value, 10, 64)
		if err != nil {
			return Config{}, fmt.Errorf("invalid MAX_UPLOAD_BYTES %q: %w", value, err)
		}
		if parsed < 1 {
			return Config{}, fmt.Errorf("invalid MAX_UPLOAD_BYTES %q: must be positive", value)
		}
		maxUpload = parsed
	}
	staticDir := getenv("STATIC_DIR", "./web")
	logFormat := getenv("LOG_FORMAT", "json")
	if logFormat != "json" && logFormat != "text" {
		return Config{}, fmt.Errorf("invalid LOG_FORMAT %q: want json or text", logFormat)
	}
	return Config{
		DataDir: dataDir, Port: port, SessionSecret: secret, StaticDir: staticDir,
		LogLevel: getenv("LOG_LEVEL", "info"), LogFormat: logFormat,
		MaxUpload: maxUpload, SecureCookie: os.Getenv("ENV") == "production",
	}, nil
}

func (c Config) DatabasePath() string { return filepath.Join(c.DataDir, "app.sqlite") }
func (c Config) BooksDir() string     { return filepath.Join(c.DataDir, "books") }

func getenv(key, fallback string) string {
	if value := os.Getenv(key); value != "" {
		return value
	}
	return fallback
}
