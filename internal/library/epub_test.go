package library

import "testing"

func TestSecureEPUBPath(t *testing.T) {
	for _, bad := range []string{"/etc/passwd", "../cover.jpg", "a/../../b", "\\root\\x"} {
		if _, err := securePath(bad); err == nil {
			t.Errorf("accepted %q", bad)
		}
	}
	if got, err := securePath("OPS/../images/cover.jpg"); err != nil || got != "images/cover.jpg" {
		t.Fatalf("got %q, %v", got, err)
	}
}
func TestNormalizeCategory(t *testing.T) {
	if got := NormalizeCategory("  "); got != "Uncategorized" {
		t.Fatal(got)
	}
	if got := NormalizeCategory(" Fiction "); got != "Fiction" {
		t.Fatal(got)
	}
}
