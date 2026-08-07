package library

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"

	"pocikode/bookshelf/internal/database"
)

type trashManifest struct {
	BookID   int64       `json:"book_id"`
	FileHash string      `json:"file_hash"`
	Files    []trashFile `json:"files"`
}
type trashFile struct {
	Original string `json:"original"`
	Trashed  string `json:"trashed"`
}

func (s *Service) Delete(ctx context.Context, id int64) error {
	book, err := s.repo.FindBook(ctx, id)
	if err != nil {
		return err
	}
	dir, err := os.MkdirTemp(filepath.Join(s.dataDir, "trash"), "delete-")
	if err != nil {
		return err
	}
	manifest := trashManifest{BookID: id, FileHash: book.FileHash}
	for _, rel := range []string{book.FilePath, book.CoverPath} {
		if rel == "" {
			continue
		}
		abs, err := s.absolute(rel)
		if err != nil {
			_ = s.restoreTrash(dir, manifest)
			return err
		}
		dst := filepath.Join(dir, filepath.Base(abs))
		manifest.Files = append(manifest.Files, trashFile{Original: rel, Trashed: filepath.Base(dst)})
	}
	raw, _ := json.Marshal(manifest)
	if err = os.WriteFile(filepath.Join(dir, "manifest.json"), raw, 0o600); err != nil {
		os.RemoveAll(dir)
		return err
	}
	for _, file := range manifest.Files {
		src, _ := s.absolute(file.Original)
		if err = os.Rename(src, filepath.Join(dir, file.Trashed)); err != nil {
			_ = s.restoreTrash(dir, manifest)
			return err
		}
	}
	if err = s.repo.DeleteBookTx(ctx, id); err != nil {
		_ = s.restoreTrash(dir, manifest)
		return err
	}
	_ = os.RemoveAll(dir)
	return nil
}
func (s *Service) restoreTrash(dir string, m trashManifest) error {
	var first error
	for _, file := range m.Files {
		src := filepath.Join(dir, file.Trashed)
		dst, err := s.absolute(file.Original)
		if err != nil {
			if first == nil {
				first = err
			}
			continue
		}
		_ = os.MkdirAll(filepath.Dir(dst), 0o750)
		if err = os.Rename(src, dst); err != nil && !errors.Is(err, os.ErrNotExist) && first == nil {
			first = err
		}
	}
	if first == nil {
		_ = os.RemoveAll(dir)
	}
	return first
}
func (s *Service) ReconcileTrash(ctx context.Context) error {
	root := filepath.Join(s.dataDir, "trash")
	entries, err := os.ReadDir(root)
	if err != nil {
		return err
	}
	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}
		dir := filepath.Join(root, entry.Name())
		raw, err := os.ReadFile(filepath.Join(dir, "manifest.json"))
		if err != nil {
			continue
		}
		var m trashManifest
		if json.Unmarshal(raw, &m) != nil || m.BookID < 1 || m.FileHash == "" {
			continue
		}
		book, findErr := s.repo.FindBook(ctx, m.BookID)
		if findErr == nil && book.FileHash == m.FileHash {
			_ = s.restoreTrash(dir, m)
		} else if databaseNotFound(findErr) {
			_ = os.RemoveAll(dir)
		}
	}
	return nil
}
func (s *Service) absolute(rel string) (string, error) {
	if filepath.IsAbs(rel) {
		return "", errors.New("absolute storage path rejected")
	}
	root := filepath.Clean(s.dataDir)
	abs := filepath.Join(root, filepath.FromSlash(rel))
	check, err := filepath.Rel(root, abs)
	if err != nil || check == ".." || strings.HasPrefix(check, ".."+string(filepath.Separator)) {
		return "", errors.New("storage path escapes DATA_DIR")
	}
	current := root
	for _, component := range strings.Split(check, string(filepath.Separator)) {
		current = filepath.Join(current, component)
		info, statErr := os.Lstat(current)
		if errors.Is(statErr, os.ErrNotExist) {
			break
		}
		if statErr != nil {
			return "", statErr
		}
		if info.Mode()&os.ModeSymlink != 0 {
			return "", errors.New("storage path contains a symbolic link")
		}
	}
	return abs, nil
}
func databaseNotFound(err error) bool { return database.IsNotFound(err) }
