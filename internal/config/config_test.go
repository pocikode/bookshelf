package config

import (
	"path/filepath"
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
		{"missing password", nil, "APP_PASSWORD is required"},
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
