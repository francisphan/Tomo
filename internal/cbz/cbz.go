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
// land side by side instead of split across two screens. A chapter that opens
// with a spread gets no blank page: the spread shows on its own anyway.
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
	if padStart && !startsWithSpread(pages) {
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

// PadResult says what Pad or Unpad did to a chapter.
type PadResult int

const (
	PadUnchanged PadResult = iota // already right, or left alone on purpose
	PadAdded                      // blank page added in front
	PadResized                    // a spread-sized blank page redrawn page-sized
	PadRemoved                    // blank page taken out
)

// NoPadMark in ComicInfo.xml's Notes means the chapter should have no blank
// page. Unpad sets it for chapters whose split spreads pair up without one,
// so a later Pad doesn't put the page back.
const NoPadMark = "tomo:no-pad"

// Pad makes an existing CBZ's leading blank page (0000.png, the page Write
// adds with padStart) right for two-page reading, and reports what it did:
//
//   - adds one when the chapter has none;
//   - removes it when the chapter opens with a spread (a wide page shows on its
//     own anyway, so the blank page would just sit alone on screen) or is
//     marked NoPadMark;
//   - redraws it page-sized when an older Tomo made it spread-sized.
//
// A 0000 page that isn't all black is a real page and is never touched. Other
// entries are copied as-is, and the rewrite goes through a temp file so a
// failure never damages the original.
func Pad(path string) (PadResult, error) {
	zr, err := zip.OpenReader(path)
	if err != nil {
		return PadUnchanged, err
	}
	defer zr.Close()

	c, err := scanChapter(zr)
	if err != nil {
		return PadUnchanged, err
	}
	want := !c.opensWithSpread && !bytes.Contains(c.info, []byte(NoPadMark))

	switch {
	case c.blank != nil && !want:
		return PadRemoved, rewrite(path, zr, func(zw *zip.Writer) error {
			return copyExcept(zw, zr, c.blank)
		})
	case c.blank != nil && c.blankSize.Width > c.blankSize.Height:
		blank, err := blankPage(c.sizes)
		if err != nil {
			return PadUnchanged, err
		}
		return PadResized, rewrite(path, zr, func(zw *zip.Writer) error {
			if err := writeBlank(zw, blank); err != nil {
				return err
			}
			return copyExcept(zw, zr, c.blank)
		})
	case c.blank == nil && want && !c.realZero:
		blank, err := blankPage(c.sizes)
		if err != nil {
			return PadUnchanged, err
		}
		return PadAdded, rewrite(path, zr, func(zw *zip.Writer) error {
			if err := writeBlank(zw, blank); err != nil {
				return err
			}
			return copyExcept(zw, zr, nil)
		})
	}
	return PadUnchanged, nil
}

// Unpad takes the leading blank page out of a chapter and marks it NoPadMark
// so Pad leaves it without one. It reports PadUnchanged if the chapter was
// already unpadded and marked.
func Unpad(path string) (PadResult, error) {
	zr, err := zip.OpenReader(path)
	if err != nil {
		return PadUnchanged, err
	}
	defer zr.Close()

	c, err := scanChapter(zr)
	if err != nil {
		return PadUnchanged, err
	}
	marked := bytes.Contains(c.info, []byte(NoPadMark))
	if c.blank == nil && marked {
		return PadUnchanged, nil
	}
	result := PadRemoved
	if c.blank == nil {
		result = PadUnchanged // only the mark is new
	}
	return result, rewrite(path, zr, func(zw *zip.Writer) error {
		for _, f := range zr.File {
			if f == c.blank || f.Name == "ComicInfo.xml" {
				continue
			}
			if err := zw.Copy(f); err != nil {
				return err
			}
		}
		info := c.info
		if !marked {
			info = markComicInfo(info, NoPadMark)
		}
		w, err := zw.CreateHeader(&zip.FileHeader{Name: "ComicInfo.xml", Method: zip.Deflate, Modified: time.Now()})
		if err != nil {
			return err
		}
		_, err = w.Write(info)
		return err
	})
}

// chapter is what Pad and Unpad need to know about an archive.
type chapter struct {
	blank           *zip.File // Tomo's all-black 0000 page, if present
	blankSize       image.Config
	realZero        bool           // a 0000 page that is real content
	sizes           []image.Config // sizes of the real pages
	opensWithSpread bool           // the first real page is wider than tall
	info            []byte         // ComicInfo.xml, if present
}

func scanChapter(zr *zip.ReadCloser) (chapter, error) {
	var c chapter
	seenFirst := false
	for _, f := range zr.File {
		if f.Name == "ComicInfo.xml" {
			b, err := readEntry(f)
			if err != nil {
				return c, err
			}
			c.info = b
			continue
		}
		if !isImage(f.Name) {
			continue
		}
		if strings.HasPrefix(f.Name, "0000.") && c.blank == nil && !seenFirst {
			if size, ok := blankSize(f); ok {
				c.blank, c.blankSize = f, size
				continue
			}
			c.realZero = true
		}
		cfg, ok := entrySize(f)
		if ok {
			c.sizes = append(c.sizes, cfg)
		}
		if !seenFirst {
			seenFirst = true
			c.opensWithSpread = ok && cfg.Width > cfg.Height
		}
	}
	return c, nil
}

// blankSize reports the size of f if it is an all-black image.
func blankSize(f *zip.File) (image.Config, bool) {
	rc, err := f.Open()
	if err != nil {
		return image.Config{}, false
	}
	defer rc.Close()
	img, _, err := image.Decode(rc)
	if err != nil {
		return image.Config{}, false
	}
	b := img.Bounds()
	if g, ok := img.(*image.Gray); ok { // Tomo's own blank pages: check the bytes directly
		for _, v := range g.Pix {
			if v != 0 {
				return image.Config{}, false
			}
		}
		return image.Config{Width: b.Dx(), Height: b.Dy()}, true
	}
	for y := b.Min.Y; y < b.Max.Y; y++ {
		for x := b.Min.X; x < b.Max.X; x++ {
			if r, g, bl, _ := img.At(x, y).RGBA(); r|g|bl != 0 {
				return image.Config{}, false
			}
		}
	}
	return image.Config{Width: b.Dx(), Height: b.Dy()}, true
}

// copyExcept copies every entry of zr except skip into zw, in order.
func copyExcept(zw *zip.Writer, zr *zip.ReadCloser, skip *zip.File) error {
	for _, f := range zr.File {
		if f == skip {
			continue
		}
		if err := zw.Copy(f); err != nil {
			return err
		}
	}
	return nil
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

// blankPage renders an all-black PNG at the chapter's most common single-page
// size, so the reader scales it like a real page (a chapter's first page is
// often an odd-sized cover). Spreads don't count, even in a chapter that has
// more spreads than single pages: a spread-sized blank would fill a whole
// screen. With no readable portrait sizes it falls back to a typical 2:3 page.
func blankPage(sizes []image.Config) ([]byte, error) {
	w, h := 800, 1200
	counts := map[[2]int]int{}
	best := 0
	for _, c := range sizes {
		if c.Width > c.Height {
			continue
		}
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

// startsWithSpread reports whether the chapter's first page is wider than tall.
func startsWithSpread(pages []download.Page) bool {
	cfg, ok := fileSize(pages[0].Path)
	return ok && cfg.Width > cfg.Height
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
