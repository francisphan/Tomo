package naming

import (
	"strings"
	"testing"
	"unicode/utf8"
)

func TestSanitizeTruncatesOnRuneBoundary(t *testing.T) {
	// 200 multibyte runes; byte length far exceeds 150.
	in := strings.Repeat("あ", 200)
	got := Sanitize(in)
	if !utf8.ValidString(got) {
		t.Fatalf("Sanitize produced invalid UTF-8: %q", got)
	}
	if n := utf8.RuneCountInString(got); n > 150 {
		t.Errorf("got %d runes, want <= 150", n)
	}
}

func TestSanitizeReservedNames(t *testing.T) {
	for _, name := range []string{"CON", "nul", "Com1", "LPT9"} {
		if got := Sanitize(name); strings.EqualFold(got, name) {
			t.Errorf("Sanitize(%q) = %q, expected it to be escaped", name, got)
		}
	}
}

func TestPadNumber(t *testing.T) {
	cases := map[string]string{
		"7":    "0007",
		"10.5": "0010.5",
		"123":  "0123",
		"1000": "1000",
		"":     "0000",
	}
	for in, want := range cases {
		if got := PadNumber(in, 4); got != want {
			t.Errorf("PadNumber(%q): got %q want %q", in, got, want)
		}
	}
}

func TestSanitize(t *testing.T) {
	cases := map[string]string{
		`Solo Leveling`:       "Solo Leveling",
		`A/B: The "Test"?`:    "A-B - The 'Test'",
		`  trailing dots... `: "trailing dots",
		`Re:Zero <Arc 1>`:     "Re -Zero (Arc 1)",
	}
	for in, want := range cases {
		if got := Sanitize(in); got != want {
			t.Errorf("Sanitize(%q): got %q want %q", in, got, want)
		}
	}
}

func TestChapterFile(t *testing.T) {
	got := ChapterFile("Solo Leveling", "10.5", "")
	want := "Solo Leveling - Chapter 0010.5.cbz"
	if got != want {
		t.Errorf("got %q want %q", got, want)
	}
	got = ChapterFile("Solo Leveling", "1", "The Beginning")
	want = "Solo Leveling - Chapter 0001 - The Beginning.cbz"
	if got != want {
		t.Errorf("got %q want %q", got, want)
	}
}

func TestPageName(t *testing.T) {
	if got := PageName(1, ".jpg"); got != "0001.jpg" {
		t.Errorf("got %q", got)
	}
	if got := PageName(42, "png"); got != "0042.png" {
		t.Errorf("got %q", got)
	}
}
