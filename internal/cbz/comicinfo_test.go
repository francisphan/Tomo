package cbz

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestSetElement(t *testing.T) {
	doc := []byte("<ComicInfo>\n  <Series>S</Series>\n  <Title>Old</Title>\n</ComicInfo>")
	got := string(setElement(doc, "Title", "Interhigh · A & B"))
	if got != "<ComicInfo>\n  <Series>S</Series>\n  <Title>Interhigh · A &amp; B</Title>\n</ComicInfo>" {
		t.Errorf("replace: %q", got)
	}
	got = string(setElement(doc, "StoryArc", "Haikyu!! · Interhigh"))
	if !strings.Contains(got, "  <StoryArc>Haikyu!! · Interhigh</StoryArc>\n</ComicInfo>") {
		t.Errorf("insert: %q", got)
	}
	got = string(setElement(doc, "Title", ""))
	if got != "<ComicInfo>\n  <Series>S</Series>\n</ComicInfo>" {
		t.Errorf("remove: %q", got)
	}
}

func TestSetComicInfoOnlyRewritesOnChange(t *testing.T) {
	path := filepath.Join(t.TempDir(), "c.cbz")
	writeZip(t, path, map[string][]byte{
		"0001.png":      pagePNG(t, 30, 45, 128),
		"ComicInfo.xml": []byte("<?xml version=\"1.0\"?>\n<ComicInfo>\n  <Series>S</Series>\n  <Notes>tomo:true-black</Notes>\n</ComicInfo>"),
	}, []string{"0001.png", "ComicInfo.xml"})
	fields := []Field{{"Title", "Interhigh"}, {"StoryArc", "S · Interhigh"}, {"StoryArcNumber", "3"}}
	if ok, err := SetComicInfo(path, fields...); err != nil || !ok {
		t.Fatalf("SetComicInfo = %v, %v", ok, err)
	}
	_, data := readZip(t, path)
	ci := string(data["ComicInfo.xml"])
	for _, want := range []string{"<Series>S</Series>", "<Notes>tomo:true-black</Notes>", "<Title>Interhigh</Title>", "<StoryArc>S · Interhigh</StoryArc>", "<StoryArcNumber>3</StoryArcNumber>"} {
		if !strings.Contains(ci, want) {
			t.Errorf("ComicInfo.xml missing %s:\n%s", want, ci)
		}
	}
	before, _ := os.ReadFile(path)
	if ok, err := SetComicInfo(path, fields...); err != nil || ok {
		t.Fatalf("second SetComicInfo = %v, %v; want unchanged", ok, err)
	}
	after, _ := os.ReadFile(path)
	if string(before) != string(after) {
		t.Error("an unchanged update rewrote the file")
	}
}

func TestSetComicInfoDoesNotCreateEmptyInfo(t *testing.T) {
	path := filepath.Join(t.TempDir(), "c.cbz")
	writeZip(t, path, map[string][]byte{"0001.png": pagePNG(t, 30, 45, 128)}, []string{"0001.png"})
	if ok, err := SetComicInfo(path, Field{"StoryArc", ""}); err != nil || ok {
		t.Fatalf("SetComicInfo = %v, %v; want unchanged", ok, err)
	}
	if got := entryNames(t, path); got != "0001.png" {
		t.Errorf("entries = %s", got)
	}
}
