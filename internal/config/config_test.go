package config

import (
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"unicode/utf8"
)

func TestFromEnv(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name string
		env  map[string]string
		want string
	}{
		{"short unicode password", map[string]string{"APP_PASSWORD": "1234567890猫"}, "12 Unicode"},
		{"placeholder case insensitive", map[string]string{"APP_PASSWORD": "BOOKSHELF"}, "placeholder"},
		{"relative data", map[string]string{"APP_PASSWORD": "correct horse battery", "DATA_DIR": "data"}, "absolute"},
		{"bad bool", map[string]string{"APP_PASSWORD": "correct horse battery", "TRUST_PROXY": "TRUE"}, "exactly true or false"},
		{"bad level", map[string]string{"APP_PASSWORD": "correct horse battery", "LOG_LEVEL": "verbose"}, "LOG_LEVEL"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			_, err := FromEnv(func(k string) (string, bool) { v, ok := tt.env[k]; return v, ok })
			if err == nil || !strings.Contains(err.Error(), tt.want) {
				t.Fatalf("error = %v, want containing %q", err, tt.want)
			}
		})
	}
}

func TestDefaultsWithoutEnvironment(t *testing.T) {
	t.Parallel()
	env := map[string]string{"APP_PASSWORD": "correct horse battery"}
	cfg, err := FromEnv(func(k string) (string, bool) { v, ok := env[k]; return v, ok })
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Port != DefaultPort {
		t.Fatalf("port = %d, want %d", cfg.Port, DefaultPort)
	}
	if !filepath.IsAbs(cfg.DataDir) || filepath.Base(cfg.DataDir) != appDirName {
		t.Fatalf("data dir = %q, want an absolute path ending in %q", cfg.DataDir, appDirName)
	}
}

func TestBlankDataDirFallsBackToDefault(t *testing.T) {
	t.Parallel()
	env := map[string]string{"APP_PASSWORD": "correct horse battery", "DATA_DIR": ""}
	cfg, err := FromEnv(func(k string) (string, bool) { v, ok := env[k]; return v, ok })
	if err != nil {
		t.Fatal(err)
	}
	if !filepath.IsAbs(cfg.DataDir) {
		t.Fatalf("data dir = %q, want an absolute path", cfg.DataDir)
	}
}

func TestDefaultDataDirUsesXDGOnUnix(t *testing.T) {
	t.Parallel()
	if runtime.GOOS == "darwin" || runtime.GOOS == "windows" {
		t.Skip("XDG_DATA_HOME is not consulted on this platform")
	}
	env := map[string]string{"XDG_DATA_HOME": "/xdg/share"}
	dir, err := defaultDataDir(func(k string) (string, bool) { v, ok := env[k]; return v, ok })
	if err != nil {
		t.Fatal(err)
	}
	if want := filepath.Join("/xdg/share", appDirName); dir != want {
		t.Fatalf("data dir = %q, want %q", dir, want)
	}
}

func TestEnsurePasswordGeneratesOnceAndPersists(t *testing.T) {
	t.Parallel()
	dir := filepath.Join(t.TempDir(), "library")
	env := map[string]string{"DATA_DIR": dir}
	cfg, err := FromEnv(func(k string) (string, bool) { v, ok := env[k]; return v, ok })
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Password != "" {
		t.Fatalf("password = %q, want empty before EnsurePassword", cfg.Password)
	}
	if err := cfg.PrepareDataDir(); err != nil {
		t.Fatal(err)
	}
	first, generated, err := cfg.EnsurePassword()
	if err != nil {
		t.Fatal(err)
	}
	if !generated {
		t.Fatal("generated = false, want true on first run")
	}
	if utf8.RuneCountInString(first.Password) < 12 {
		t.Fatalf("generated password %q is too short", first.Password)
	}
	info, err := os.Stat(first.BootstrapPasswordPath())
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode().Perm() != 0o600 {
		t.Fatalf("mode = %v, want 0600", info.Mode().Perm())
	}
	second, generated, err := cfg.EnsurePassword()
	if err != nil {
		t.Fatal(err)
	}
	if generated {
		t.Fatal("generated = true, want false on a later run")
	}
	if second.Password != first.Password {
		t.Fatalf("password changed across runs: %q then %q", first.Password, second.Password)
	}
}

func TestEnsurePasswordKeepsSuppliedValue(t *testing.T) {
	t.Parallel()
	dir := filepath.Join(t.TempDir(), "library")
	env := map[string]string{"APP_PASSWORD": "correct horse battery", "DATA_DIR": dir}
	cfg, err := FromEnv(func(k string) (string, bool) { v, ok := env[k]; return v, ok })
	if err != nil {
		t.Fatal(err)
	}
	if err := cfg.PrepareDataDir(); err != nil {
		t.Fatal(err)
	}
	resolved, generated, err := cfg.EnsurePassword()
	if err != nil {
		t.Fatal(err)
	}
	if generated || resolved.Password != env["APP_PASSWORD"] {
		t.Fatalf("generated = %v, password = %q", generated, resolved.Password)
	}
	if _, err := os.Stat(resolved.BootstrapPasswordPath()); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("stat error = %v, want not-exist when APP_PASSWORD is supplied", err)
	}
}

func TestValidConfigAndPrepare(t *testing.T) {
	t.Parallel()
	dir := filepath.Join(t.TempDir(), "library")
	env := map[string]string{"APP_PASSWORD": "  spaces are exact  ", "DATA_DIR": dir, "PORT": "9090", "MAX_UPLOAD_MB": "2", "TRUST_PROXY": "true", "SESSION_DAYS": "1", "LOG_LEVEL": "debug"}
	cfg, err := FromEnv(func(k string) (string, bool) { v, ok := env[k]; return v, ok })
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Password != env["APP_PASSWORD"] || cfg.MaxUploadBytes != 2*1024*1024 || !cfg.TrustProxy {
		t.Fatalf("unexpected config: %+v", cfg)
	}
	if err := cfg.PrepareDataDir(); err != nil {
		t.Fatal(err)
	}
}
