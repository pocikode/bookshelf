package web

import (
	"crypto/sha256"
	"embed"
	"encoding/hex"
	"errors"
	"fmt"
	"io/fs"
	"mime"
	"net/http"
	"path"
	"strings"
)

//go:embed dist/* templates/*.html
var embedded embed.FS

type Asset struct {
	Name, Hash, URL, ContentType string
	Data                         []byte
}
type Assets struct {
	byName map[string]Asset
	byURL  map[string]Asset
}

func LoadAssets() (*Assets, error) {
	a := &Assets{byName: map[string]Asset{}, byURL: map[string]Asset{}}
	entries, err := fs.ReadDir(embedded, "dist")
	if err != nil {
		return nil, err
	}
	for _, entry := range entries {
		if entry.IsDir() || strings.HasPrefix(entry.Name(), ".") {
			continue
		}
		data, err := fs.ReadFile(embedded, "dist/"+entry.Name())
		if err != nil {
			return nil, err
		}
		sum := sha256.Sum256(data)
		hash := hex.EncodeToString(sum[:])
		url := "/assets/" + hash[:16] + "/" + entry.Name()
		contentType := mime.TypeByExtension(path.Ext(entry.Name()))
		if contentType == "" {
			contentType = "application/octet-stream"
		}
		asset := Asset{Name: entry.Name(), Hash: hash, URL: url, ContentType: contentType, Data: data}
		a.byName[entry.Name()] = asset
		a.byURL[url] = asset
	}
	return a, nil
}
func (a *Assets) URL(name string) string {
	if asset, ok := a.byName[name]; ok {
		return asset.URL
	}
	return ""
}
func (a *Assets) Require(names ...string) error {
	for _, name := range names {
		if _, ok := a.byName[name]; !ok {
			return fmt.Errorf("built asset %s is missing; run bun run build", name)
		}
	}
	return nil
}
func (a *Assets) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	asset, ok := a.byURL[r.URL.Path]
	if !ok {
		http.NotFound(w, r)
		return
	}
	etag := `"` + asset.Hash + `"`
	if matchETag(r.Header.Get("If-None-Match"), etag) {
		w.Header().Set("ETag", etag)
		w.WriteHeader(http.StatusNotModified)
		return
	}
	w.Header().Set("Content-Type", asset.ContentType)
	w.Header().Set("Content-Length", fmt.Sprint(len(asset.Data)))
	w.Header().Set("Cache-Control", "public, max-age=31536000, immutable")
	w.Header().Set("ETag", etag)
	_, _ = w.Write(asset.Data)
}
func matchETag(header, etag string) bool {
	for _, part := range strings.Split(header, ",") {
		if strings.TrimSpace(part) == etag || strings.TrimSpace(part) == "*" {
			return true
		}
	}
	return false
}
func Embedded() fs.FS { return embedded }

var ErrAssetsMissing = errors.New("built assets missing")
