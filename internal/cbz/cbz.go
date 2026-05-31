// Package cbz writes a chapter's pages into a Comic Book Zip archive.
//
// A CBZ is simply a ZIP of image files. Readers display the images in name
// order, so pages are stored zero-padded (0001.jpg ...). Images are already
// compressed, so we Store them rather than Deflate — faster, same size.
package cbz

import (
	"archive/zip"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"time"

	"tomo/internal/download"
	"tomo/internal/naming"
)

// Write creates a CBZ at path containing the given pages, streaming each page's
// temp file into the archive so a whole chapter is never held in memory. It
// writes to a temporary file first and renames on success, so a crash mid-write
// never leaves a half-written archive that the reader would treat as complete.
func Write(path string, pages []download.Page) error {
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
	if err := zw.Close(); err != nil {
		f.Close()
		os.Remove(tmp)
		return err
	}
	if err := f.Close(); err != nil {
		os.Remove(tmp)
		return err
	}
	if err := os.Rename(tmp, path); err != nil {
		os.Remove(tmp)
		return err
	}
	return nil
}
