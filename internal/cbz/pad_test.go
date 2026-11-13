package cbz

import (
	"bytes"
	"image"
	"image/png"
	"path/filepath"
	"strings"
	"testing"

	"tomo/internal/download"
)

// pagePNG is a mid-grey PNG of the given size: a real page, not a blank one.
func pagePNG(t *testing.T, w, h int, level uint8) []byte {
	t.Helper()
	img := image.NewGray(image.Rect(0, 0, w, h))
	for i := range img.Pix {
		img.Pix[i] = level
	}
	var buf bytes.Buffer
	if err := png.Encode(&buf, img); err != nil {
		t.Fatal(err)
	}
	return buf.Bytes()
}

func entryNames(t *testing.T, path string) string {
	t.Helper()
	names, _ := readZip(t, path)
	return strings.Join(names, ",")
}

func pngSize(t *testing.T, data []byte) (int, int) {
	t.Helper()
	cfg, err := png.DecodeConfig(bytes.NewReader(data))
	if err != nil {
		t.Fatal(err)
	}
	return cfg.Width, cfg.Height
}

func TestBlankPageIgnoresSpreads(t *testing.T) {
	sizes := []image.Config{
		{Width: 2133, Height: 1600}, {Width: 2133, Height: 1600}, {Width: 2133, Height: 1600},
		{Width: 1067, Height: 1600}, {Width: 1067, Height: 1600},
	}
	data, err := blankPage(sizes)
	if err != nil {
		t.Fatal(err)
	}
	if w, h := pngSize(t, data); w != 1067 || h != 1600 {
		t.Errorf("blank page is %dx%d, want the single-page 1067x1600", w, h)
	}
}

func TestPadSkipsChapterThatOpensWithSpread(t *testing.T) {
	path := filepath.Join(t.TempDir(), "c.cbz")
	writeZip(t, path, map[string][]byte{
		"0001.png": pagePNG(t, 60, 30, 128),
		"0002.png": pagePNG(t, 30, 45, 128),
	}, []string{"0001.png", "0002.png"})
	if res, err := Pad(path); err != nil || res != PadUnchanged {
		t.Fatalf("Pad = %v, %v; want PadUnchanged", res, err)
	}
	if got := entryNames(t, path); got != "0001.png,0002.png" {
		t.Errorf("entries = %s", got)
	}
}

func TestPadRemovesBlankBeforeOpeningSpread(t *testing.T) {
	path := filepath.Join(t.TempDir(), "c.cbz")
	writeZip(t, path, map[string][]byte{
		"0000.png":      pagePNG(t, 30, 45, 0),
		"0001.png":      pagePNG(t, 60, 30, 128),
		"0002.png":      pagePNG(t, 30, 45, 128),
		"ComicInfo.xml": []byte("<ComicInfo><Number>1</Number></ComicInfo>"),
	}, []string{"0000.png", "0001.png", "0002.png", "ComicInfo.xml"})
	if res, err := Pad(path); err != nil || res != PadRemoved {
		t.Fatalf("Pad = %v, %v; want PadRemoved", res, err)
	}
	if got := entryNames(t, path); got != "0001.png,0002.png,ComicInfo.xml" {
		t.Errorf("entries = %s", got)
	}
	if res, _ := Pad(path); res != PadUnchanged {
		t.Errorf("second Pad = %v, want PadUnchanged", res)
	}
}

func TestPadResizesSpreadSizedBlank(t *testing.T) {
	path := filepath.Join(t.TempDir(), "c.cbz")
	writeZip(t, path, map[string][]byte{
		"0000.png": pagePNG(t, 60, 30, 0), // an older Tomo sized it like the spreads
		"0001.png": pagePNG(t, 30, 45, 128),
		"0002.png": pagePNG(t, 60, 30, 128),
		"0003.png": pagePNG(t, 60, 30, 128),
	}, []string{"0000.png", "0001.png", "0002.png", "0003.png"})
	if res, err := Pad(path); err != nil || res != PadResized {
		t.Fatalf("Pad = %v, %v; want PadResized", res, err)
	}
	_, data := readZip(t, path)
	if w, h := pngSize(t, data["0000.png"]); w != 30 || h != 45 {
		t.Errorf("blank page is %dx%d, want 30x45", w, h)
	}
	if got := entryNames(t, path); got != "0000.png,0001.png,0002.png,0003.png" {
		t.Errorf("entries = %s", got)
	}
}

func TestPadLeavesARealPageZeroAlone(t *testing.T) {
	path := filepath.Join(t.TempDir(), "c.cbz")
	writeZip(t, path, map[string][]byte{
		"0000.png": pagePNG(t, 30, 45, 128), // a source that numbers from zero
		"0001.png": pagePNG(t, 30, 45, 128),
	}, []string{"0000.png", "0001.png"})
	if res, err := Pad(path); err != nil || res != PadUnchanged {
		t.Fatalf("Pad = %v, %v; want PadUnchanged", res, err)
	}
}

func TestUnpadRemovesAndPadRespectsIt(t *testing.T) {
	path := filepath.Join(t.TempDir(), "c.cbz")
	writeZip(t, path, map[string][]byte{
		"0000.png":      pagePNG(t, 30, 45, 0),
		"0001.png":      pagePNG(t, 30, 45, 128),
		"ComicInfo.xml": []byte("<ComicInfo>\n  <Series>S</Series>\n</ComicInfo>"),
	}, []string{"0000.png", "0001.png", "ComicInfo.xml"})
	if res, err := Unpad(path); err != nil || res != PadRemoved {
		t.Fatalf("Unpad = %v, %v; want PadRemoved", res, err)
	}
	_, data := readZip(t, path)
	if ci := string(data["ComicInfo.xml"]); !strings.Contains(ci, NoPadMark) || !strings.Contains(ci, "<Series>S</Series>") {
		t.Errorf("ComicInfo.xml = %q", ci)
	}
	if res, err := Pad(path); err != nil || res != PadUnchanged {
		t.Errorf("Pad after Unpad = %v, %v; want PadUnchanged", res, err)
	}
	if res, err := Unpad(path); err != nil || res != PadUnchanged {
		t.Errorf("second Unpad = %v, %v; want PadUnchanged", res, err)
	}
	if got := entryNames(t, path); got != "0001.png,ComicInfo.xml" {
		t.Errorf("entries = %s", got)
	}
}

func TestWritePadStartSkipsChapterThatOpensWithSpread(t *testing.T) {
	src := t.TempDir()
	pages := []download.Page{
		{Index: 1, Ext: ".png", Path: tempPage(t, src, "1.png", string(pagePNG(t, 60, 30, 128)))},
		{Index: 2, Ext: ".png", Path: tempPage(t, src, "2.png", string(pagePNG(t, 30, 45, 128)))},
	}
	out := filepath.Join(t.TempDir(), "c.cbz")
	if err := Write(out, pages, true, nil); err != nil {
		t.Fatal(err)
	}
	if got := entryNames(t, out); got != "0001.png,0002.png" {
		t.Errorf("entries = %s, want no blank page", got)
	}
}
