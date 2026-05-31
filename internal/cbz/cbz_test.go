package cbz

import (
	"archive/zip"
	"io"
	"os"
	"path/filepath"
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
	if err := Write(out, pages); err != nil {
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
	if err := Write(out, nil); err == nil {
		t.Error("expected error for empty page list")
	}
	if _, err := os.Stat(out); !os.IsNotExist(err) {
		t.Error("no file should be created for empty page list")
	}
}
