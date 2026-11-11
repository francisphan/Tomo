// Package cbz writes a chapter's pages into a Comic Book Zip archive.
//
// A CBZ is simply a ZIP of image files. Readers display the images in name
// order, so pages are stored zero-padded (0001.jpg ...). Images are already
// compressed, so we Store them rather than Deflate — faster, same size.
package cbz

import (
	"archive/zip"
	"bytes"
	"encoding/xml"
	"fmt"
	"image"
	_ "image/gif"
	_ "image/jpeg"
	"image/png"
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"

	_ "golang.org/x/image/webp"

	"tomo/internal/download"
	"tomo/internal/naming"
)

// Write creates a CBZ at path containing the given pages, streaming each page's
// temp file into the archive so a whole chapter is never held in memory. It
// writes to a temporary file first and renames on success, so a crash mid-write
// never leaves a half-written archive that the reader would treat as complete.
//
// padStart prepends a blank black page (0000.png) so that, in a two-page
// reader, the chapter's first page sits alone and later double-page spreads
// land side by side instead of split across two screens.
//
// info, when non-nil, is stored as ComicInfo.xml so Komga (and readers that
// import it) number the chapter by its real chapter number rather than by its
// position in the series folder.
func Write(path string, pages []download.Page, padStart bool, info *ComicInfo) error {
	if len(pages) == 0 {
		return fmt.Errorf("refusing to write empty CBZ: %s", path)
	}
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}

	// Unique temp name so two writers targeting the same path can't corrupt
	// each other's partial archive.
	f, err := os.CreateTemp(dir, ".tomo-*.part")
	if err != nil {
		return err
	}
	tmp := f.Name()

	zw := zip.NewWriter(f)
	fail := func(err error) error {
		zw.Close()
		f.Close()
		os.Remove(tmp)
		return err
	}
	if padStart {
		var sizes []image.Config
		for _, p := range pages {
			if cfg, ok := fileSize(p.Path); ok {
				sizes = append(sizes, cfg)
			}
		}
		blank, err := blankPage(sizes)
		if err != nil {
			return fail(err)
		}
		if err := writeBlank(zw, blank); err != nil {
			return fail(err)
		}
	}
	for _, p := range pages {
		hdr := &zip.FileHeader{
			Name:     naming.PageName(p.Index, p.Ext),
			Method:   zip.Store,
			Modified: time.Now(),
		}
		w, err := zw.CreateHeader(hdr)
		if err != nil {
			return fail(err)
		}
		src, err := os.Open(p.Path)
		if err != nil {
			return fail(err)
		}
		if _, err := io.Copy(w, src); err != nil {
			src.Close()
			return fail(err)
		}
		src.Close()
	}
	if info != nil {
		if err := writeComicInfo(zw, info); err != nil {
			return fail(err)
		}
	}
	if err := zw.Close(); err != nil {
		f.Close()
		os.Remove(tmp)
		return err
	}
	if err := f.Close(); err != nil {
		os.Remove(tmp)
		return err
	}
	// CreateTemp makes the file 0600; a library is usually read by another
	// user (Komga, a file share), so publish it world-readable like any
	// normally created file.
	if err := os.Chmod(tmp, 0o644); err != nil {
		os.Remove(tmp)
		return err
	}
	if err := os.Rename(tmp, path); err != nil {
		os.Remove(tmp)
		return err
	}
	return nil
}

// Pad rewrites an existing CBZ with a blank page (0000.png) in front, the same
// page Write adds with padStart. It reports false and leaves the file alone if
// the archive already starts with a 0000 page. Other entries are copied as-is,
// and the rewrite goes through a temp file so a failure never damages the
// original.
func Pad(path string) (bool, error) {
	zr, err := zip.OpenReader(path)
	if err != nil {
		return false, err
	}
	defer zr.Close()

	var sizes []image.Config
	for _, f := range zr.File {
		if strings.HasPrefix(f.Name, "0000.") {
			return false, nil
		}
		if cfg, ok := entrySize(f); ok {
			sizes = append(sizes, cfg)
		}
	}
	blank, err := blankPage(sizes)
	if err != nil {
		return false, err
	}

	out, err := os.CreateTemp(filepath.Dir(path), ".tomo-*.part")
	if err != nil {
		return false, err
	}
	tmp := out.Name()
	zw := zip.NewWriter(out)
	fail := func(err error) (bool, error) {
		zw.Close()
		out.Close()
		os.Remove(tmp)
		return false, err
	}
	if err := writeBlank(zw, blank); err != nil {
		return fail(err)
	}
	for _, f := range zr.File {
		if err := zw.Copy(f); err != nil {
			return fail(err)
		}
	}
	if err := zw.Close(); err != nil {
		return fail(err)
	}
	if err := out.Close(); err != nil {
		os.Remove(tmp)
		return false, err
	}
	// CreateTemp makes the file 0600; keep the original's mode so a library
	// served by another user (Komga) can still read it.
	if fi, err := os.Stat(path); err == nil {
		if err := os.Chmod(tmp, fi.Mode().Perm()); err != nil {
			os.Remove(tmp)
			return false, err
		}
	}
	// Windows can't rename over a file that still has an open handle.
	zr.Close()
	if err := os.Rename(tmp, path); err != nil {
		os.Remove(tmp)
		return false, err
	}
	return true, nil
}

func writeBlank(zw *zip.Writer, blank []byte) error {
	w, err := zw.CreateHeader(&zip.FileHeader{
		Name:     naming.PageName(0, ".png"),
		Method:   zip.Store,
		Modified: time.Now(),
	})
	if err != nil {
		return err
	}
	_, err = w.Write(blank)
	return err
}

// blankPage renders an all-black PNG at the chapter's most common page size, so
// the reader scales it like a real page (a chapter's first page is often an
// odd-sized cover or spread). With no readable sizes it falls back to a typical
// 2:3 page.
func blankPage(sizes []image.Config) ([]byte, error) {
	w, h := 800, 1200
	counts := map[[2]int]int{}
	best := 0
	for _, c := range sizes {
		k := [2]int{c.Width, c.Height}
		counts[k]++
		if counts[k] > best {
			best = counts[k]
			w, h = c.Width, c.Height
		}
	}
	var buf bytes.Buffer
	if err := png.Encode(&buf, image.NewGray(image.Rect(0, 0, w, h))); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}

func fileSize(path string) (image.Config, bool) {
	f, err := os.Open(path)
	if err != nil {
		return image.Config{}, false
	}
	defer f.Close()
	return decodeSize(f)
}

func entrySize(f *zip.File) (image.Config, bool) {
	rc, err := f.Open()
	if err != nil {
		return image.Config{}, false
	}
	defer rc.Close()
	return decodeSize(rc)
}

func decodeSize(r io.Reader) (image.Config, bool) {
	cfg, _, err := image.DecodeConfig(r)
	return cfg, err == nil && cfg.Width > 0 && cfg.Height > 0
}

// ComicInfo is the subset of the ComicRack ComicInfo.xml schema Tomo fills in.
type ComicInfo struct {
	XMLName xml.Name `xml:"ComicInfo"`
	Series  string   `xml:"Series,omitempty"`
	Number  string   `xml:"Number,omitempty"`
	Title   string   `xml:"Title,omitempty"`
}

func writeComicInfo(zw *zip.Writer, info *ComicInfo) error {
	body, err := xml.MarshalIndent(info, "", "  ")
	if err != nil {
		return err
	}
	w, err := zw.CreateHeader(&zip.FileHeader{Name: "ComicInfo.xml", Method: zip.Deflate, Modified: time.Now()})
	if err != nil {
		return err
	}
	_, err = w.Write(append([]byte(xml.Header), body...))
	return err
}
