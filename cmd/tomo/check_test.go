package main

import (
	"archive/zip"
	"bytes"
	"fmt"
	"image"
	"image/png"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// noisyPage is a page with real content: varied grey levels, little white.
func noisyPage(t *testing.T, seed uint8) []byte {
	t.Helper()
	img := image.NewGray(image.Rect(0, 0, 40, 60))
	for i := range img.Pix {
		img.Pix[i] = uint8(i*37) + seed
	}
	var buf bytes.Buffer
	png.Encode(&buf, img)
	return buf.Bytes()
}

func flatPage(t *testing.T, level uint8) []byte {
	t.Helper()
	img := image.NewGray(image.Rect(0, 0, 40, 60))
	for i := range img.Pix {
		img.Pix[i] = level
	}
	var buf bytes.Buffer
	png.Encode(&buf, img)
	return buf.Bytes()
}

func chapterZip(t *testing.T, path string, pages ...[]byte) {
	t.Helper()
	os.MkdirAll(filepath.Dir(path), 0o755)
	f, err := os.Create(path)
	if err != nil {
		t.Fatal(err)
	}
	zw := zip.NewWriter(f)
	for i, p := range pages {
		w, _ := zw.Create(fmt.Sprintf("%04d.png", i+1)) // pages start at 0001, as Tomo writes them
		w.Write(p)
	}
	zw.Close()
	f.Close()
}

func TestCheckChapter(t *testing.T) {
	dir := t.TempDir()
	ok := filepath.Join(dir, "S - Chapter 0001.cbz")
	chapterZip(t, ok, flatPage(t, 0), noisyPage(t, 1), flatPage(t, 255), noisyPage(t, 2), flatPage(t, 255))
	if p := checkChapter(ok, ""); len(p) != 0 {
		t.Errorf("clean chapter (repeated blank white page) held: %v", p)
	}

	rep := filepath.Join(dir, "S - Chapter 0002.cbz")
	chapterZip(t, rep, noisyPage(t, 1), noisyPage(t, 2), noisyPage(t, 1))
	if p := checkChapter(rep, ""); len(p) != 1 || !strings.Contains(p[0], "0003.png repeats page 0001.png") {
		t.Errorf("repeated content page: %v", p)
	}

	broken := filepath.Join(dir, "S - Chapter 0003.cbz")
	chapterZip(t, broken, noisyPage(t, 1), []byte("not an image"))
	if p := checkChapter(broken, ""); len(p) != 1 || !strings.Contains(p[0], "won't open") {
		t.Errorf("broken page: %v", p)
	}
}

func TestCheckChapterShortAgainstLibrary(t *testing.T) {
	lib := t.TempDir()
	var pages [][]byte
	for i := 0; i < 8; i++ {
		pages = append(pages, noisyPage(t, uint8(i)))
	}
	for _, n := range []string{"0001", "0002", "0003"} {
		chapterZip(t, filepath.Join(lib, "S", "S - Chapter "+n+".cbz"), pages...)
	}
	hold := t.TempDir()
	short := filepath.Join(hold, "S", "S - Chapter 0004.cbz")
	chapterZip(t, short, pages[:3]...)
	if p := checkChapter(short, lib); len(p) != 1 || !strings.Contains(p[0], "only 3 pages; this series usually has 8") {
		t.Errorf("short chapter: %v", p)
	}
	extra := filepath.Join(hold, "S", "S - Chapter 0004.5.cbz")
	chapterZip(t, extra, pages[:3]...)
	if p := checkChapter(extra, lib); len(p) != 0 {
		t.Errorf("short extra held: %v", p)
	}
}
