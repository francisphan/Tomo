package cbz

import (
	"archive/zip"
	"bytes"
	"image"
	"image/color"
	"image/jpeg"
	"image/png"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

func TestTrueBlackCurve(t *testing.T) {
	cases := map[int]uint8{0: 0, 8: 0, 32: 0, 40: 0, 41: 1, 76: 56, 111: 110, 112: 112, 180: 180, 255: 255}
	for in, want := range cases {
		if got := trueBlackLUT[in]; got != want {
			t.Errorf("trueBlackLUT[%d] = %d, want %d", in, got, want)
		}
	}
	for x := 1; x < 256; x++ {
		if trueBlackLUT[x] < trueBlackLUT[x-1] {
			t.Fatalf("curve is not monotonic at %d", x)
		}
	}
}

// scanPage is an RGB JPEG like a downloaded scan: its left half is dark grey
// "ink" at level 32, its right half white paper.
func scanPage(t *testing.T) []byte {
	t.Helper()
	img := image.NewRGBA(image.Rect(0, 0, 64, 32))
	for y := 0; y < 32; y++ {
		for x := 0; x < 64; x++ {
			c := color.RGBA{32, 32, 32, 255}
			if x >= 32 {
				c = color.RGBA{255, 255, 255, 255}
			}
			img.Set(x, y, c)
		}
	}
	var buf bytes.Buffer
	if err := jpeg.Encode(&buf, img, &jpeg.Options{Quality: 95}); err != nil {
		t.Fatal(err)
	}
	return buf.Bytes()
}

func greyPNG(t *testing.T, level uint8) []byte {
	t.Helper()
	img := image.NewGray(image.Rect(0, 0, 8, 8))
	for i := range img.Pix {
		img.Pix[i] = level
	}
	var buf bytes.Buffer
	if err := png.Encode(&buf, img); err != nil {
		t.Fatal(err)
	}
	return buf.Bytes()
}

func writeZip(t *testing.T, path string, entries map[string][]byte, order []string) {
	t.Helper()
	f, err := os.Create(path)
	if err != nil {
		t.Fatal(err)
	}
	zw := zip.NewWriter(f)
	for _, name := range order {
		w, err := zw.Create(name)
		if err != nil {
			t.Fatal(err)
		}
		w.Write(entries[name])
	}
	if err := zw.Close(); err != nil {
		t.Fatal(err)
	}
	f.Close()
}

func readZip(t *testing.T, path string) ([]string, map[string][]byte) {
	t.Helper()
	zr, err := zip.OpenReader(path)
	if err != nil {
		t.Fatal(err)
	}
	defer zr.Close()
	var names []string
	data := map[string][]byte{}
	for _, f := range zr.File {
		b, err := readEntry(f)
		if err != nil {
			t.Fatal(err)
		}
		names = append(names, f.Name)
		data[f.Name] = b
	}
	return names, data
}

func TestTrueBlackConvertsPagesOnce(t *testing.T) {
	path := filepath.Join(t.TempDir(), "Series - Chapter 0001.cbz")
	info := "<?xml version=\"1.0\" encoding=\"UTF-8\"?>\n<ComicInfo>\n  <Series>Series</Series>\n  <Number>1</Number>\n</ComicInfo>"
	writeZip(t, path, map[string][]byte{
		"0000.png":      greyPNG(t, 0),
		"0001.jpg":      scanPage(t),
		"0002.png":      greyPNG(t, 76),
		"0003.png":      scanPage(t), // a JPEG under a .png name, as some sources serve them
		"ComicInfo.xml": []byte(info),
	}, []string{"0000.png", "0001.jpg", "0002.png", "0003.png", "ComicInfo.xml"})
	if runtime.GOOS != "windows" {
		os.Chmod(path, 0o640)
	}

	ok, err := TrueBlack(path)
	if err != nil || !ok {
		t.Fatalf("TrueBlack = %v, %v; want true, nil", ok, err)
	}

	names, data := readZip(t, path)
	if strings.Join(names, ",") != "0000.png,0001.jpg,0002.png,0003.png,ComicInfo.xml" {
		t.Fatalf("entries = %v", names)
	}

	scan, err := jpeg.Decode(bytes.NewReader(data["0001.jpg"]))
	if err != nil {
		t.Fatal(err)
	}
	g, isGray := scan.(*image.Gray)
	if !isGray {
		t.Fatalf("converted JPEG decodes as %T, want greyscale", scan)
	}
	if ink := g.GrayAt(8, 16).Y; ink > 2 {
		t.Errorf("ink at level 32 became %d, want true black", ink)
	}
	if paper := g.GrayAt(56, 16).Y; paper < 250 {
		t.Errorf("white paper became %d, want it left white", paper)
	}

	tone, _ := png.Decode(bytes.NewReader(data["0002.png"]))
	if got := tone.(*image.Gray).GrayAt(0, 0).Y; got != 56 {
		t.Errorf("level 76 became %d, want 56", got)
	}

	if _, format, err := image.DecodeConfig(bytes.NewReader(data["0003.png"])); err != nil || format != "jpeg" {
		t.Errorf("JPEG stored as .png was re-encoded as %q (%v), want it kept as JPEG", format, err)
	}

	ci := string(data["ComicInfo.xml"])
	if !strings.Contains(ci, "<Series>Series</Series>") || !strings.Contains(ci, "<Notes>"+TrueBlackMark+"</Notes>") {
		t.Errorf("ComicInfo.xml lost a field or the mark:\n%s", ci)
	}

	if runtime.GOOS != "windows" {
		if fi, _ := os.Stat(path); fi.Mode().Perm() != 0o640 {
			t.Errorf("mode = %v, want the original 0640", fi.Mode().Perm())
		}
	}

	// Running again must not darken the chapter a second time.
	before, _ := os.ReadFile(path)
	ok, err = TrueBlack(path)
	if err != nil || ok {
		t.Fatalf("second TrueBlack = %v, %v; want false, nil", ok, err)
	}
	after, _ := os.ReadFile(path)
	if !bytes.Equal(before, after) {
		t.Error("second run changed the file")
	}
}

func TestTrueBlackAddsComicInfoWhenMissing(t *testing.T) {
	path := filepath.Join(t.TempDir(), "c.cbz")
	writeZip(t, path, map[string][]byte{"0001.png": greyPNG(t, 30)}, []string{"0001.png"})
	if ok, err := TrueBlack(path); err != nil || !ok {
		t.Fatalf("TrueBlack = %v, %v", ok, err)
	}
	_, data := readZip(t, path)
	if !strings.Contains(string(data["ComicInfo.xml"]), TrueBlackMark) {
		t.Fatalf("no mark in new ComicInfo.xml: %q", data["ComicInfo.xml"])
	}
	if ok, _ := TrueBlack(path); ok {
		t.Error("converted a marked chapter again")
	}
}

func TestTrueBlackLeavesArchiveOnBadPage(t *testing.T) {
	path := filepath.Join(t.TempDir(), "c.cbz")
	writeZip(t, path, map[string][]byte{"0001.jpg": []byte("not an image")}, []string{"0001.jpg"})
	before, _ := os.ReadFile(path)
	if _, err := TrueBlack(path); err == nil {
		t.Fatal("want an error for an undecodable page")
	}
	after, _ := os.ReadFile(path)
	if !bytes.Equal(before, after) {
		t.Error("a failed conversion changed the original")
	}
	if left, _ := filepath.Glob(filepath.Join(filepath.Dir(path), ".tomo-*.part")); len(left) > 0 {
		t.Errorf("temp files left behind: %v", left)
	}
}

func TestMarkComicInfoAppendsToExistingNotes(t *testing.T) {
	got := string(markComicInfo([]byte("<ComicInfo><Notes>scanned by X</Notes></ComicInfo>"), TrueBlackMark))
	if got != "<ComicInfo><Notes>scanned by X; "+TrueBlackMark+"</Notes></ComicInfo>" {
		t.Errorf("got %q", got)
	}
}

func TestJPEGQualityMatchesEncoder(t *testing.T) {
	img := image.NewGray(image.Rect(0, 0, 16, 16))
	for _, q := range []int{60, 75, 90} {
		var buf bytes.Buffer
		if err := jpeg.Encode(&buf, img, &jpeg.Options{Quality: q}); err != nil {
			t.Fatal(err)
		}
		if got := jpegQuality(buf.Bytes()); got != q {
			t.Errorf("jpegQuality of a quality-%d JPEG = %d", q, got)
		}
	}
	if got := jpegQuality([]byte("not a jpeg")); got != 85 {
		t.Errorf("unreadable JPEG = %d, want the 85 fallback", got)
	}
}
