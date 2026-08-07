package config

import (
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

func TestFromEnv(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name string
		env  map[string]string
		want string
	}{
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

func TestPasswordDefaultsWhenAbsentOrEmpty(t *testing.T) {
	t.Parallel()
	for name, env := range map[string]map[string]string{
		"absent": {},
		"empty":  {"APP_PASSWORD": ""},
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			cfg, err := FromEnv(func(k string) (string, bool) { v, ok := env[k]; return v, ok })
			if err != nil {
				t.Fatal(err)
			}
			if cfg.Password != DefaultPassword || !cfg.UsingDefaultPassword() {
				t.Fatalf("password = %q, want %q", cfg.Password, DefaultPassword)
			}
		})
	}
}

func TestSuppliedPasswordIsAcceptedUnvalidated(t *testing.T) {
	t.Parallel()
	for _, password := range []string{"a", "bookshelf", "replace-me-with-a-strong-password", "1234567890猫"} {
		env := map[string]string{"APP_PASSWORD": password}
		cfg, err := FromEnv(func(k string) (string, bool) { v, ok := env[k]; return v, ok })
		if err != nil {
			t.Fatalf("password %q rejected: %v", password, err)
		}
		if cfg.Password != password || cfg.UsingDefaultPassword() {
			t.Fatalf("password = %q, want %q", cfg.Password, password)
		}
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
