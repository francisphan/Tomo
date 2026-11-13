package cbz

import (
	"archive/zip"
	"bytes"
	"encoding/xml"
	"image"
	"image/png"
	"io"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"tomo/internal/download"
)

func tempPage(t *testing.T, dir, name, content string) string {
	t.Helper()
	p := filepath.Join(dir, name)
	if err := os.WriteFile(p, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
	return p
}

func TestWriteStreamsPagesIntoArchive(t *testing.T) {
	src := t.TempDir()
	pages := []download.Page{
		{Index: 1, Ext: ".jpg", Path: tempPage(t, src, "1.jpg", "page-one")},
		{Index: 2, Ext: ".png", Path: tempPage(t, src, "2.png", "page-two")},
	}

	out := filepath.Join(t.TempDir(), "chapter.cbz")
	if err := Write(out, pages, false, nil); err != nil {
		t.Fatalf("Write: %v", err)
	}

	zr, err := zip.OpenReader(out)
	if err != nil {
		t.Fatalf("open cbz: %v", err)
	}
	defer zr.Close()

	if len(zr.File) != 2 {
		t.Fatalf("got %d entries, want 2", len(zr.File))
	}
	wantName := []string{"0001.jpg", "0002.png"}
	wantBody := []string{"page-one", "page-two"}
	for i, f := range zr.File {
		if f.Name != wantName[i] {
			t.Errorf("entry %d name = %q, want %q", i, f.Name, wantName[i])
		}
		if f.Method != zip.Store {
			t.Errorf("entry %d not stored (method %d)", i, f.Method)
		}
		rc, err := f.Open()
		if err != nil {
			t.Fatal(err)
		}
		b, _ := io.ReadAll(rc)
		rc.Close()
		if string(b) != wantBody[i] {
			t.Errorf("entry %d body = %q, want %q", i, b, wantBody[i])
		}
	}

	// No leftover .part temp file next to the output.
	entries, _ := os.ReadDir(filepath.Dir(out))
	for _, e := range entries {
		if filepath.Ext(e.Name()) == ".part" {
			t.Errorf("leftover temp file: %s", e.Name())
		}
	}
}

func TestWriteRejectsEmpty(t *testing.T) {
	out := filepath.Join(t.TempDir(), "x.cbz")
	if err := Write(out, nil, false, nil); err == nil {
		t.Error("expected error for empty page list")
	}
	if _, err := os.Stat(out); !os.IsNotExist(err) {
		t.Error("no file should be created for empty page list")
	}
}

func TestWritePadStartPrependsBlankPageSizedLikeFirstPage(t *testing.T) {
	src := t.TempDir()
	var img bytes.Buffer
	if err := png.Encode(&img, image.NewRGBA(image.Rect(0, 0, 30, 45))); err != nil {
		t.Fatal(err)
	}
	pages := []download.Page{
		{Index: 1, Ext: ".png", Path: tempPage(t, src, "1.png", img.String())},
		{Index: 2, Ext: ".png", Path: tempPage(t, src, "2.png", img.String())},
	}

	out := filepath.Join(t.TempDir(), "chapter.cbz")
	if err := Write(out, pages, true, nil); err != nil {
		t.Fatalf("Write: %v", err)
	}
	zr, err := zip.OpenReader(out)
	if err != nil {
		t.Fatal(err)
	}
	defer zr.Close()

	var names []string
	for _, f := range zr.File {
		names = append(names, f.Name)
	}
	if want := []string{"0000.png", "0001.png", "0002.png"}; strings.Join(names, ",") != strings.Join(want, ",") {
		t.Fatalf("entries = %v, want %v", names, want)
	}
	rc, err := zr.File[0].Open()
	if err != nil {
		t.Fatal(err)
	}
	defer rc.Close()
	blank, err := png.Decode(rc)
	if err != nil {
		t.Fatalf("blank page is not a PNG: %v", err)
	}
	if b := blank.Bounds(); b.Dx() != 30 || b.Dy() != 45 {
		t.Errorf("blank page is %dx%d, want 30x45", b.Dx(), b.Dy())
	}
	if r, g, bl, _ := blank.At(10, 10).RGBA(); r|g|bl != 0 {
		t.Errorf("blank page is not black")
	}
}

func TestPadPrependsBlankOnceAndKeepsEntries(t *testing.T) {
	src := t.TempDir()
	pages := []download.Page{
		{Index: 1, Ext: ".jpg", Path: tempPage(t, src, "1.jpg", "page-one")},
		{Index: 2, Ext: ".jpg", Path: tempPage(t, src, "2.jpg", "page-two")},
	}
	out := filepath.Join(t.TempDir(), "chapter.cbz")
	if err := Write(out, pages, false, nil); err != nil {
		t.Fatal(err)
	}

	res, err := Pad(out)
	if err != nil || res != PadAdded {
		t.Fatalf("Pad = %v, %v; want PadAdded, nil", res, err)
	}
	res, err = Pad(out)
	if err != nil || res != PadUnchanged {
		t.Fatalf("second Pad = %v, %v; want PadUnchanged, nil", res, err)
	}

	zr, err := zip.OpenReader(out)
	if err != nil {
		t.Fatal(err)
	}
	defer zr.Close()
	var names []string
	for _, f := range zr.File {
		names = append(names, f.Name)
	}
	if want := "0000.png,0001.jpg,0002.jpg"; strings.Join(names, ",") != want {
		t.Fatalf("entries = %v, want %s", names, want)
	}
	rc, err := zr.File[2].Open()
	if err != nil {
		t.Fatal(err)
	}
	b, _ := io.ReadAll(rc)
	rc.Close()
	if string(b) != "page-two" {
		t.Errorf("copied entry body = %q, want page-two", b)
	}
	entries, _ := os.ReadDir(filepath.Dir(out))
	if len(entries) != 1 {
		t.Errorf("leftover files next to the CBZ: %v", entries)
	}
}

func TestWriteStoresComicInfo(t *testing.T) {
	src := t.TempDir()
	pages := []download.Page{{Index: 1, Ext: ".jpg", Path: tempPage(t, src, "1.jpg", "p")}}
	out := filepath.Join(t.TempDir(), "chapter.cbz")
	info := &ComicInfo{Series: "Haikyu!!", Number: "125.5", Title: "A & B <c>"}
	if err := Write(out, pages, false, info); err != nil {
		t.Fatalf("Write: %v", err)
	}
	zr, err := zip.OpenReader(out)
	if err != nil {
		t.Fatal(err)
	}
	defer zr.Close()
	last := zr.File[len(zr.File)-1]
	if last.Name != "ComicInfo.xml" {
		t.Fatalf("last entry = %q, want ComicInfo.xml", last.Name)
	}
	rc, err := last.Open()
	if err != nil {
		t.Fatal(err)
	}
	defer rc.Close()
	var got ComicInfo
	if err := xml.NewDecoder(rc).Decode(&got); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if got.Series != info.Series || got.Number != info.Number || got.Title != info.Title {
		t.Errorf("got %+v, want %+v", got, *info)
	}
}

func TestPadKeepsFileModeAndComicInfo(t *testing.T) {
	src := t.TempDir()
	pages := []download.Page{{Index: 1, Ext: ".jpg", Path: tempPage(t, src, "1.jpg", "p")}}
	out := filepath.Join(t.TempDir(), "chapter.cbz")
	if err := Write(out, pages, false, &ComicInfo{Series: "S", Number: "7"}); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(out, 0o644); err != nil {
		t.Fatal(err)
	}
	if res, err := Pad(out); err != nil || res != PadAdded {
		t.Fatalf("Pad = %v, %v", res, err)
	}
	fi, err := os.Stat(out)
	if err != nil {
		t.Fatal(err)
	}
	if fi.Mode().Perm() != 0o644 {
		t.Errorf("mode after Pad = %v, want 0644", fi.Mode().Perm())
	}
	zr, err := zip.OpenReader(out)
	if err != nil {
		t.Fatal(err)
	}
	defer zr.Close()
	if last := zr.File[len(zr.File)-1].Name; last != "ComicInfo.xml" {
		t.Errorf("last entry after Pad = %q, want ComicInfo.xml", last)
	}
}

func TestWriteMakesArchiveReadable(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("POSIX permission bits")
	}
	src := t.TempDir()
	pages := []download.Page{{Index: 1, Ext: ".jpg", Path: tempPage(t, src, "1.jpg", "p")}}
	out := filepath.Join(t.TempDir(), "chapter.cbz")
	if err := Write(out, pages, false, nil); err != nil {
		t.Fatal(err)
	}
	fi, err := os.Stat(out)
	if err != nil {
		t.Fatal(err)
	}
	if fi.Mode().Perm() != 0o644 {
		t.Errorf("mode = %v, want 0644", fi.Mode().Perm())
	}
}
