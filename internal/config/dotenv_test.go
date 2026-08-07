package config

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestParseDotEnv(t *testing.T) {
	t.Parallel()
	values, err := parseDotEnv(strings.NewReader(`
# comment
APP_PASSWORD='  exact password value  '
export PORT=9090
DATA_DIR="/tmp/bookshelf data"
HASH=value#kept
COMMENTED=value # removed
ESCAPED="line\nvalue"
PORT=8081
`))
	if err != nil {
		t.Fatal(err)
	}
	if values["APP_PASSWORD"] != "  exact password value  " || values["PORT"] != "8081" || values["DATA_DIR"] != "/tmp/bookshelf data" || values["HASH"] != "value#kept" || values["COMMENTED"] != "value" || values["ESCAPED"] != "line\nvalue" {
		t.Fatalf("unexpected values: %#v", values)
	}
}

func TestLoadDotEnvPreservesExportedValues(t *testing.T) {
	path := filepath.Join(t.TempDir(), ".env")
	if err := os.WriteFile(path, []byte("BOOKSHELF_TEST_EXISTING=from-file\nBOOKSHELF_TEST_NEW=loaded\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("BOOKSHELF_TEST_EXISTING", "from-process")
	os.Unsetenv("BOOKSHELF_TEST_NEW")
	t.Cleanup(func() { os.Unsetenv("BOOKSHELF_TEST_NEW") })
	if err := LoadDotEnv(path); err != nil {
		t.Fatal(err)
	}
	if got := os.Getenv("BOOKSHELF_TEST_EXISTING"); got != "from-process" {
		t.Fatalf("existing value overwritten: %q", got)
	}
	if got := os.Getenv("BOOKSHELF_TEST_NEW"); got != "loaded" {
		t.Fatalf("new value not loaded: %q", got)
	}
}

func TestLoadDotEnvMissingAndInvalid(t *testing.T) {
	t.Parallel()
	if err := LoadDotEnv(filepath.Join(t.TempDir(), "missing")); err != nil {
		t.Fatal(err)
	}
	if _, err := parseDotEnv(strings.NewReader("NOT-VALID=value\n")); err == nil {
		t.Fatal("invalid key accepted")
	}
	if _, err := parseDotEnv(strings.NewReader("VALUE='unterminated\n")); err == nil {
		t.Fatal("unterminated value accepted")
	}
}
