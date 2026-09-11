package storage

import (
	"archive/zip"
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"mime"
	"net/http"
	"os"
	"path/filepath"
	"strings"
)

// ErrRejected marks an upload the client got wrong (format, MIME, size, a
// corrupt archive). Everything else returned by Save is an internal failure:
// the handler answers 500 and logs the cause instead of echoing it back.
var ErrRejected = errors.New("rejected upload")

const maxCoverSize = 20 << 20

type FileStore struct {
	// Root is DATA_DIR. Stored paths are relative to it so the database can
	// move with the data volume between hosts.
	Root      string
	MaxUpload int64
}

type UploadedFile struct {
	Path         string
	CoverPath    string
	OriginalName string
	MimeType     string
	Size         int64
	Hash         string
	Created      bool
	CoverCreated bool
}

func (s FileStore) Save(r io.Reader, name, contentType string) (UploadedFile, error) {
	ext := strings.ToLower(filepath.Ext(name))
	if ext != ".epub" && ext != ".pdf" {
		return UploadedFile{}, fmt.Errorf("%w: unsupported book format %q", ErrRejected, ext)
	}
	if contentType != "" && contentType != "application/epub+zip" && contentType != "application/pdf" && contentType != "application/octet-stream" {
		return UploadedFile{}, fmt.Errorf("%w: unsupported MIME type %q", ErrRejected, contentType)
	}
	uploadsRoot := filepath.Join(s.Root, "uploads")
	if err := os.MkdirAll(uploadsRoot, 0o750); err != nil {
		return UploadedFile{}, fmt.Errorf("create uploads dir %s: %w", uploadsRoot, err)
	}
	tmp, err := os.CreateTemp(uploadsRoot, ".upload-*")
	if err != nil {
		return UploadedFile{}, fmt.Errorf("create temp upload in %s: %w", uploadsRoot, err)
	}
	tmpName := tmp.Name()
	defer os.Remove(tmpName)
	limited := io.LimitReader(r, s.MaxUpload+1)
	hash := sha256.New()
	size, err := io.Copy(io.MultiWriter(tmp, hash), limited)
	if err != nil {
		tmp.Close()
		return UploadedFile{}, fmt.Errorf("write upload to %s: %w", tmpName, err)
	}
	if size > s.MaxUpload {
		tmp.Close()
		return UploadedFile{}, fmt.Errorf("%w: book exceeds upload limit of %d bytes", ErrRejected, s.MaxUpload)
	}
	if _, err := tmp.Seek(0, io.SeekStart); err != nil {
		tmp.Close()
		return UploadedFile{}, fmt.Errorf("rewind upload %s: %w", tmpName, err)
	}
	validType, err := validateBook(tmp, ext)
	if err != nil {
		tmp.Close()
		return UploadedFile{}, err
	}
	if err := tmp.Close(); err != nil {
		return UploadedFile{}, fmt.Errorf("close upload %s: %w", tmpName, err)
	}
	if contentType == "" || contentType == "application/octet-stream" {
		contentType = validType
	}
	hashValue := hex.EncodeToString(hash.Sum(nil))
	shard := hashValue[:2]
	booksRoot := filepath.Join(s.Root, "books", shard)
	if err := os.MkdirAll(booksRoot, 0o750); err != nil {
		return UploadedFile{}, fmt.Errorf("create book shard %s: %w", booksRoot, err)
	}
	finalName := hashValue + ext
	finalPath := filepath.Join(booksRoot, finalName)
	if _, err := os.Stat(finalPath); err == nil {
		return UploadedFile{Path: filepath.Join("books", shard, finalName), OriginalName: filepath.Base(name), MimeType: contentType, Size: size, Hash: hashValue}, nil
	} else if !errors.Is(err, os.ErrNotExist) {
		return UploadedFile{}, fmt.Errorf("check stored upload %s: %w", finalPath, err)
	}
	if err := os.Rename(tmpName, finalPath); err != nil {
		return UploadedFile{}, fmt.Errorf("move upload to %s: %w", finalPath, err)
	}
	return UploadedFile{Path: filepath.Join("books", shard, finalName), OriginalName: filepath.Base(name), MimeType: contentType, Size: size, Hash: hashValue, Created: true}, nil
}

func (s FileStore) Remove(path string) error {
	cleanPath, err := s.Resolve(path)
	if err != nil {
		return err
	}
	if err := os.Remove(cleanPath); err != nil {
		return fmt.Errorf("remove book file %s: %w", cleanPath, err)
	}
	return nil
}

func (s FileStore) SaveCover(r io.Reader, bookPath string) (string, error) {
	cleanBookPath, err := s.Resolve(bookPath)
	if err != nil {
		return "", err
	}

	header := make([]byte, 512)
	n, err := io.ReadFull(r, header)
	if errors.Is(err, io.EOF) {
		return "", fmt.Errorf("%w: empty cover image", ErrRejected)
	}
	if err != nil && !errors.Is(err, io.ErrUnexpectedEOF) {
		return "", fmt.Errorf("read cover header: %w", err)
	}
	header = header[:n]
	ext, ok := map[string]string{
		"image/gif":  ".gif",
		"image/jpeg": ".jpg",
		"image/png":  ".png",
		"image/webp": ".webp",
	}[http.DetectContentType(header)]
	if !ok {
		return "", fmt.Errorf("%w: unsupported cover image", ErrRejected)
	}

	uploadsRoot := filepath.Join(s.Root, "uploads")
	if err := os.MkdirAll(uploadsRoot, 0o750); err != nil {
		return "", fmt.Errorf("create uploads dir %s: %w", uploadsRoot, err)
	}
	tmp, err := os.CreateTemp(uploadsRoot, ".cover-*")
	if err != nil {
		return "", fmt.Errorf("create temporary cover in %s: %w", uploadsRoot, err)
	}
	tmpName := tmp.Name()
	defer os.Remove(tmpName)
	size, err := io.Copy(tmp, io.LimitReader(io.MultiReader(bytes.NewReader(header), r), maxCoverSize+1))
	if err != nil {
		tmp.Close()
		return "", fmt.Errorf("write cover to %s: %w", tmpName, err)
	}
	if size > maxCoverSize {
		tmp.Close()
		return "", fmt.Errorf("%w: cover exceeds upload limit of %d bytes", ErrRejected, maxCoverSize)
	}
	if err := tmp.Close(); err != nil {
		return "", fmt.Errorf("close cover %s: %w", tmpName, err)
	}

	bookHash := strings.TrimSuffix(filepath.Base(cleanBookPath), filepath.Ext(cleanBookPath))
	if len(bookHash) != sha256.Size*2 {
		return "", fmt.Errorf("invalid stored book path %s", bookPath)
	}
	coverDir := filepath.Join(s.Root, "covers", bookHash[:2])
	if err := os.MkdirAll(coverDir, 0o750); err != nil {
		return "", fmt.Errorf("create cover shard %s: %w", coverDir, err)
	}
	coverName := bookHash + ext
	coverPath := filepath.Join(coverDir, coverName)
	if _, err := os.Stat(coverPath); err == nil {
		return filepath.Join("covers", bookHash[:2], coverName), nil
	} else if !errors.Is(err, os.ErrNotExist) {
		return "", fmt.Errorf("check stored cover %s: %w", coverPath, err)
	}
	if err := os.Rename(tmpName, coverPath); err != nil {
		return "", fmt.Errorf("move cover to %s: %w", coverPath, err)
	}
	return filepath.Join("covers", bookHash[:2], coverName), nil
}

// Resolve turns a database path into an absolute path while keeping it inside
// DATA_DIR. Absolute paths are accepted for databases created before paths
// became relative, but new paths are always relative.
func (s FileStore) Resolve(path string) (string, error) {
	if path == "" {
		return "", fmt.Errorf("empty stored path")
	}
	root, err := filepath.Abs(s.Root)
	if err != nil {
		return "", fmt.Errorf("resolve data root %s: %w", s.Root, err)
	}
	candidates := []string{path}
	if !filepath.IsAbs(path) {
		candidates = []string{filepath.Join(root, path)}
		if absolute, absErr := filepath.Abs(path); absErr == nil {
			candidates = append(candidates, absolute)
		}
	}
	for _, candidate := range candidates {
		clean, err := filepath.Abs(candidate)
		if err != nil {
			continue
		}
		rel, err := filepath.Rel(root, clean)
		if err == nil && rel != "." && rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
			return clean, nil
		}
	}
	return "", fmt.Errorf("unsafe stored path %s: outside %s", path, root)
}

func validateBook(file *os.File, ext string) (string, error) {
	header := make([]byte, 8)
	if _, err := io.ReadFull(file, header); err != nil {
		return "", fmt.Errorf("%w: book is too short to identify: %v", ErrRejected, err)
	}
	if ext == ".pdf" && string(header[:5]) != "%PDF-" {
		return "", fmt.Errorf("%w: invalid PDF file", ErrRejected)
	}
	if ext == ".epub" {
		if string(header[:2]) != "PK" {
			return "", fmt.Errorf("%w: invalid EPUB archive", ErrRejected)
		}
		if _, err := file.Seek(0, io.SeekStart); err != nil {
			return "", fmt.Errorf("rewind book for validation: %w", err)
		}
		// A failed Stat here used to be reported as a corrupt archive; keep
		// the real cause so an I/O problem is not blamed on the user.
		size, err := file.Stat()
		if err != nil {
			return "", fmt.Errorf("stat book for validation: %w", err)
		}
		archive, err := zip.NewReader(file, size.Size())
		if err != nil {
			return "", fmt.Errorf("%w: invalid EPUB archive: %v", ErrRejected, err)
		}
		if len(archive.File) == 0 {
			return "", fmt.Errorf("%w: EPUB archive is empty", ErrRejected)
		}
		foundContainer := false
		for _, entry := range archive.File {
			if entry.Name == "META-INF/container.xml" {
				foundContainer = true
				break
			}
		}
		if !foundContainer {
			return "", fmt.Errorf("%w: EPUB container is missing", ErrRejected)
		}
	}
	return mime.TypeByExtension(ext), nil
}
