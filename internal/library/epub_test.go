package library

import (
	"archive/zip"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

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

func writeEPUB(t *testing.T, files map[string][]byte) string {
	t.Helper()
	name := filepath.Join(t.TempDir(), "book.epub")
	f, err := os.Create(name)
	if err != nil {
		t.Fatal(err)
	}
	z := zip.NewWriter(f)
	for path, data := range files {
		w, err := z.Create(path)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := w.Write(data); err != nil {
			t.Fatal(err)
		}
	}
	if err := z.Close(); err != nil {
		t.Fatal(err)
	}
	if err := f.Close(); err != nil {
		t.Fatal(err)
	}
	return name
}

func validEPUBFiles(cover []byte) map[string][]byte {
	return map[string][]byte{
		"mimetype":               []byte("application/epub+zip"),
		"META-INF/container.xml": []byte(`<?xml version="1.0"?><container><rootfiles><rootfile full-path="OPS/package.opf"/></rootfiles></container>`),
		"OPS/package.opf":        []byte(`<?xml version="1.0"?><package><metadata><title> EPUB Title </title><creator>Author</creator><language>en</language><publisher>Publisher</publisher><meta name="cover" content="cover-image"/></metadata><manifest><item id="cover-image" href="images%2Fcover.png" media-type="image/png" properties="cover-image"/></manifest></package>`),
		"OPS/images/cover.png":   cover,
	}
}

func TestOpenAndExtractValidEPUB(t *testing.T) {
	name := writeEPUB(t, validEPUBFiles(pngFixture(t)))
	z, err := openEPUB(name)
	if err != nil {
		t.Fatal(err)
	}
	if len(z.File) != 4 {
		t.Fatalf("entries = %d", len(z.File))
	}
	if err := z.Close(); err != nil {
		t.Fatal(err)
	}
	m, err := extractEPUB(name)
	if err != nil {
		t.Fatal(err)
	}
	if m.Title != " EPUB Title " || m.Author != "Author" || m.Language != "en" || m.Publisher != "Publisher" || len(m.Cover) == 0 {
		t.Fatalf("metadata = %+v", m)
	}
}

func TestEPUBMalformedEntries(t *testing.T) {
	tests := []struct {
		name  string
		files map[string][]byte
		want  string
	}{
		{"missing mimetype", map[string][]byte{}, "missing EPUB mimetype"},
		{"bad mimetype", map[string][]byte{"mimetype": []byte("text/plain")}, "invalid EPUB mimetype"},
		{"missing container", map[string][]byte{"mimetype": []byte("application/epub+zip")}, "missing EPUB container"},
		{"bad container", map[string][]byte{"mimetype": []byte("application/epub+zip"), "META-INF/container.xml": []byte("<broken")}, "invalid EPUB container"},
		{"missing package", map[string][]byte{"mimetype": []byte("application/epub+zip"), "META-INF/container.xml": []byte(`<container><rootfiles><rootfile full-path="OPS/no.opf"/></rootfiles></container>`)}, "missing EPUB package"},
		{"bad package", map[string][]byte{"mimetype": []byte("application/epub+zip"), "META-INF/container.xml": []byte(`<container><rootfiles><rootfile full-path="OPS/package.opf"/></rootfiles></container>`), "OPS/package.opf": []byte("<broken")}, "XML syntax error"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			name := writeEPUB(t, tt.files)
			if _, err := extractEPUB(name); err == nil || !strings.Contains(err.Error(), tt.want) {
				t.Fatalf("extractEPUB() error = %v, want %q", err, tt.want)
			}
		})
	}
	if _, err := extractEPUB(filepath.Join(t.TempDir(), "not-an-epub")); err == nil {
		t.Fatal("invalid ZIP succeeded")
	}
}

func TestEPUBHelpers(t *testing.T) {
	if !containsWord("landscape cover-image", "cover-image") || containsWord("cover-image-x", "cover-image") {
		t.Fatal("containsWord boundaries are incorrect")
	}
	if err := decodeXML([]byte("<broken"), &struct{}{}); err == nil {
		t.Fatal("malformed XML decoded")
	}
	if got, err := securePath(`OPS\\../images/cover.png`); err != nil || got != "images/cover.png" {
		t.Fatalf("securePath = %q, %v", got, err)
	}
	if _, err := openEPUB(" "); err == nil {
		t.Fatal("empty filename opened")
	}
	name := writeEPUB(t, validEPUBFiles(nil))
	z, err := openEPUB(name)
	if err != nil {
		t.Fatal(err)
	}
	if entry(z.File, "mimetype") == nil || entry(z.File, "missing") != nil {
		t.Fatal("entry lookup is incorrect")
	}
	if _, err := readEntry(entry(z.File, "mimetype"), 1); err == nil {
		t.Fatal("oversized entry accepted")
	}
	_ = z.Close()
}
