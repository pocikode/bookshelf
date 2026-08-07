package library

import (
	"archive/zip"
	"bytes"
	"encoding/xml"
	"errors"
	"fmt"
	"image"
	_ "image/gif"
	_ "image/jpeg"
	_ "image/png"
	"io"
	"net/url"
	"path"
	"strings"
)

const maxXML = 4 << 20

type Metadata struct {
	Title, Author, Language, Publisher string
	Cover                              []byte
}
type containerXML struct {
	Rootfiles []struct {
		FullPath string `xml:"full-path,attr"`
	} `xml:"rootfiles>rootfile"`
}
type packageXML struct {
	Metadata struct {
		Title     string `xml:"title"`
		Creator   string `xml:"creator"`
		Language  string `xml:"language"`
		Publisher string `xml:"publisher"`
		Meta      []struct {
			Name    string `xml:"name,attr"`
			Content string `xml:"content,attr"`
		} `xml:"meta"`
	} `xml:"metadata"`
	Manifest []struct {
		ID         string `xml:"id,attr"`
		Href       string `xml:"href,attr"`
		MediaType  string `xml:"media-type,attr"`
		Properties string `xml:"properties,attr"`
	} `xml:"manifest>item"`
}

func openEPUB(filename string) (*zip.ReadCloser, error) {
	z, err := zip.OpenReader(filename)
	if err != nil {
		return nil, err
	}
	mimetype := entry(z.File, "mimetype")
	if mimetype == nil {
		z.Close()
		return nil, errors.New("missing EPUB mimetype")
	}
	b, err := readEntry(mimetype, 128)
	if err != nil || strings.TrimSpace(string(b)) != "application/epub+zip" {
		z.Close()
		return nil, errors.New("invalid EPUB mimetype")
	}
	return z, nil
}
func extractEPUB(filename string) (Metadata, error) {
	z, err := openEPUB(filename)
	if err != nil {
		return Metadata{}, err
	}
	defer z.Close()
	container := entry(z.File, "META-INF/container.xml")
	if container == nil {
		return Metadata{}, errors.New("missing EPUB container")
	}
	data, err := readEntry(container, maxXML)
	if err != nil {
		return Metadata{}, err
	}
	var c containerXML
	if err = decodeXML(data, &c); err != nil || len(c.Rootfiles) == 0 {
		return Metadata{}, errors.New("invalid EPUB container")
	}
	opfPath, err := securePath(c.Rootfiles[0].FullPath)
	if err != nil {
		return Metadata{}, err
	}
	opfEntry := entry(z.File, opfPath)
	if opfEntry == nil {
		return Metadata{}, errors.New("missing EPUB package")
	}
	data, err = readEntry(opfEntry, maxXML)
	if err != nil {
		return Metadata{}, err
	}
	var p packageXML
	if err = decodeXML(data, &p); err != nil {
		return Metadata{}, err
	}
	m := Metadata{Title: p.Metadata.Title, Author: p.Metadata.Creator, Language: p.Metadata.Language, Publisher: p.Metadata.Publisher}
	coverID := ""
	for _, meta := range p.Metadata.Meta {
		if strings.EqualFold(meta.Name, "cover") {
			coverID = meta.Content
			break
		}
	}
	var href string
	for _, item := range p.Manifest {
		if containsWord(item.Properties, "cover-image") {
			href = item.Href
			break
		}
	}
	if href == "" && coverID != "" {
		for _, item := range p.Manifest {
			if item.ID == coverID {
				href = item.Href
				break
			}
		}
	}
	if href == "" {
		for _, item := range p.Manifest {
			if strings.HasPrefix(item.MediaType, "image/") && strings.Contains(strings.ToLower(item.Href), "cover") {
				href = item.Href
				break
			}
		}
	}
	if href != "" {
		decoded, decErr := url.PathUnescape(href)
		if decErr == nil {
			coverPath, pathErr := securePath(path.Join(path.Dir(opfPath), decoded))
			if pathErr == nil {
				if f := entry(z.File, coverPath); f != nil {
					if raw, readErr := readEntry(f, 20<<20); readErr == nil {
						if cfg, _, imageErr := image.DecodeConfig(bytes.NewReader(raw)); imageErr == nil && cfg.Width > 0 && cfg.Height > 0 && cfg.Width <= 4096 && cfg.Height <= 4096 {
							m.Cover = raw
						}
					}
				}
			}
		}
	}
	return m, nil
}
func decodeXML(data []byte, v any) error {
	d := xml.NewDecoder(bytes.NewReader(data))
	d.Strict = true
	d.Entity = map[string]string{}
	return d.Decode(v)
}
func readEntry(f *zip.File, max int64) ([]byte, error) {
	if f.UncompressedSize64 > uint64(max) {
		return nil, errors.New("EPUB entry is too large")
	}
	r, err := f.Open()
	if err != nil {
		return nil, err
	}
	defer r.Close()
	b, err := io.ReadAll(io.LimitReader(r, max+1))
	if err != nil {
		return nil, err
	}
	if int64(len(b)) > max {
		return nil, errors.New("EPUB entry is too large")
	}
	return b, nil
}
func entry(files []*zip.File, name string) *zip.File {
	for _, f := range files {
		clean, err := securePath(f.Name)
		if err == nil && clean == name {
			return f
		}
	}
	return nil
}
func securePath(name string) (string, error) {
	name = strings.ReplaceAll(name, "\\", "/")
	if strings.HasPrefix(name, "/") {
		return "", errors.New("absolute EPUB path")
	}
	clean := path.Clean(name)
	if clean == "." || clean == ".." || strings.HasPrefix(clean, "../") {
		return "", fmt.Errorf("unsafe EPUB path %q", name)
	}
	return clean, nil
}
func containsWord(s, w string) bool {
	for _, part := range strings.Fields(s) {
		if part == w {
			return true
		}
	}
	return false
}
