package storage

import (
	"archive/zip"
	"crypto/sha256"
	"fmt"
	"io"
	"mime"
	"os"
	"path/filepath"
	"strings"

	"github.com/google/uuid"
)

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
		return UploadedFile{}, fmt.Errorf("unsupported book format")
	}
	if contentType != "" && contentType != "application/epub+zip" && contentType != "application/pdf" && contentType != "application/octet-stream" {
		return UploadedFile{}, fmt.Errorf("unsupported MIME type")
	}
	if err := os.MkdirAll(s.Root, 0o750); err != nil {
		return UploadedFile{}, err
	}
	tmp, err := os.CreateTemp(s.Root, ".upload-*")
	if err != nil {
		return UploadedFile{}, err
	}
	tmpName := tmp.Name()
	defer os.Remove(tmpName)
	limited := io.LimitReader(r, s.MaxUpload+1)
	hash := sha256.New()
	size, err := io.Copy(io.MultiWriter(tmp, hash), limited)
	if err != nil {
		tmp.Close()
		return UploadedFile{}, err
	}
	if size > s.MaxUpload {
		tmp.Close()
		return UploadedFile{}, fmt.Errorf("book exceeds upload limit")
	}
	if _, err := tmp.Seek(0, io.SeekStart); err != nil {
		tmp.Close()
		return UploadedFile{}, err
	}
	validType, err := validateBook(tmp, ext)
	if err != nil {
		tmp.Close()
		return UploadedFile{}, err
	}
	if err := tmp.Close(); err != nil {
		return UploadedFile{}, err
	}
	if contentType == "" || contentType == "application/octet-stream" {
		contentType = validType
	}
	id := uuid.NewString()
	finalName := id + ext
	finalPath := filepath.Join(s.Root, finalName)
	if err := os.Rename(tmpName, finalPath); err != nil {
		return UploadedFile{}, err
	}
	return UploadedFile{Path: finalPath, OriginalName: filepath.Base(name), MimeType: contentType, Size: size, Hash: fmt.Sprintf("%x", hash.Sum(nil))}, nil
}

func (s FileStore) Remove(path string) error {
	cleanRoot, err := filepath.Abs(s.Root)
	if err != nil {
		return err
	}
	cleanPath, err := filepath.Abs(path)
	if err != nil || filepath.Dir(cleanPath) != cleanRoot {
		return fmt.Errorf("unsafe book path")
	}
	return os.Remove(cleanPath)
}

func validateBook(file *os.File, ext string) (string, error) {
	header := make([]byte, 8)
	if _, err := io.ReadFull(file, header); err != nil {
		return "", fmt.Errorf("invalid book")
	}
	if ext == ".pdf" && string(header[:5]) != "%PDF-" {
		return "", fmt.Errorf("invalid PDF file")
	}
	if ext == ".epub" {
		if string(header[:2]) != "PK" {
			return "", fmt.Errorf("invalid EPUB archive")
		}
		if _, err := file.Seek(0, io.SeekStart); err != nil {
			return "", err
		}
		archive, err := zip.NewReader(file, mustFileSize(file))
		if err != nil || len(archive.File) == 0 {
			return "", fmt.Errorf("invalid EPUB archive")
		}
		foundContainer := false
		for _, entry := range archive.File {
			if entry.Name == "META-INF/container.xml" {
				foundContainer = true
				break
			}
		}
		if !foundContainer {
			return "", fmt.Errorf("EPUB container is missing")
		}
	}
	return mime.TypeByExtension(ext), nil
}

func mustFileSize(file *os.File) int64 {
	info, err := file.Stat()
	if err != nil {
		return 0
	}
	return info.Size()
}
