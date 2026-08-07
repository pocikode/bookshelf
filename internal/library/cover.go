package library

import (
	"image"
	_ "image/gif"
	_ "image/jpeg"
	"image/png"
	"io"
	"os"
	"path/filepath"
)

func installValidatedCover(src, dst string) (bool, error) {
	in, err := os.Open(src)
	if err != nil {
		return false, err
	}
	defer in.Close()
	tmp, err := writeValidatedCoverTemp(filepath.Dir(dst), in, 5<<20)
	if err != nil {
		return false, err
	}
	defer os.Remove(tmp)
	return installNoReplace(tmp, dst)
}
func writeValidatedCoverTemp(dir string, src io.Reader, max int64) (string, error) {
	limited := io.LimitReader(src, max+1)
	cfgData, err := io.ReadAll(limited)
	if err != nil {
		return "", err
	}
	if int64(len(cfgData)) > max {
		return "", ErrTooLarge
	}
	cfg, _, err := image.DecodeConfig(bytesReader(cfgData))
	if err != nil || cfg.Width < 1 || cfg.Height < 1 || cfg.Width > 4096 || cfg.Height > 4096 {
		return "", ErrInvalidFormat
	}
	img, _, err := image.Decode(bytesReader(cfgData))
	if err != nil {
		return "", ErrInvalidFormat
	}
	f, err := os.CreateTemp(dir, "cover-*.png")
	if err != nil {
		return "", err
	}
	name := f.Name()
	ok := false
	defer func() {
		_ = f.Close()
		if !ok {
			_ = os.Remove(name)
		}
	}()
	if err = png.Encode(f, img); err != nil {
		return "", err
	}
	if err = f.Sync(); err != nil {
		return "", err
	}
	if err = f.Close(); err != nil {
		return "", err
	}
	ok = true
	return name, nil
}

type byteReader struct {
	b []byte
	i int
}

func bytesReader(b []byte) io.Reader { return &byteReader{b: b} }
func (r *byteReader) Read(p []byte) (int, error) {
	if r.i >= len(r.b) {
		return 0, io.EOF
	}
	n := copy(p, r.b[r.i:])
	r.i += n
	return n, nil
}
