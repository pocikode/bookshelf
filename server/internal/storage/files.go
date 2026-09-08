package storage

import (
	"archive/zip"
	"crypto/sha256"
	"errors"
	"fmt"
	"io"
	"mime"
	"os"
	"path/filepath"
	"strings"

	"github.com/google/uuid"
)

// ErrRejected marks an upload the client got wrong (format, MIME, size, a
// corrupt archive). Everything else returned by Save is an internal failure:
// the handler answers 500 and logs the cause instead of echoing it back.
var ErrRejected = errors.New("rejected upload")

type FileStore struct {
	Root      string
	MaxUpload int64
}

type UploadedFile struct {
	Path         string
	OriginalName string
	MimeType     string
	Size         int64
	Hash         string
}

func (s FileStore) Save(r io.Reader, name, contentType string) (UploadedFile, error) {
	ext := strings.ToLower(filepath.Ext(name))
	if ext != ".epub" && ext != ".pdf" {
		return UploadedFile{}, fmt.Errorf("%w: unsupported book format %q", ErrRejected, ext)
	}
	if contentType != "" && contentType != "application/epub+zip" && contentType != "application/pdf" && contentType != "application/octet-stream" {
		return UploadedFile{}, fmt.Errorf("%w: unsupported MIME type %q", ErrRejected, contentType)
	}
	if err := os.MkdirAll(s.Root, 0o750); err != nil {
		return UploadedFile{}, fmt.Errorf("create books dir %s: %w", s.Root, err)
	}
	tmp, err := os.CreateTemp(s.Root, ".upload-*")
	if err != nil {
		return UploadedFile{}, fmt.Errorf("create temp upload in %s: %w", s.Root, err)
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
	id := uuid.NewString()
	finalName := id + ext
	finalPath := filepath.Join(s.Root, finalName)
	if err := os.Rename(tmpName, finalPath); err != nil {
		return UploadedFile{}, fmt.Errorf("move upload to %s: %w", finalPath, err)
	}
	return UploadedFile{Path: finalPath, OriginalName: filepath.Base(name), MimeType: contentType, Size: size, Hash: fmt.Sprintf("%x", hash.Sum(nil))}, nil
}

func (s FileStore) Remove(path string) error {
	cleanRoot, err := filepath.Abs(s.Root)
	if err != nil {
		return fmt.Errorf("resolve books root %s: %w", s.Root, err)
	}
	cleanPath, err := filepath.Abs(path)
	if err != nil {
		return fmt.Errorf("resolve book path %s: %w", path, err)
	}
	if filepath.Dir(cleanPath) != cleanRoot {
		return fmt.Errorf("unsafe book path %s: outside %s", cleanPath, cleanRoot)
	}
	if err := os.Remove(cleanPath); err != nil {
		return fmt.Errorf("remove book file %s: %w", cleanPath, err)
	}
	return nil
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
