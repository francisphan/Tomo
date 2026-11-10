package source

import (
	"strings"
	"testing"

	"github.com/PuerkitoBio/goquery"
)

// wcChapterAnchor mirrors the real WeebCentral full-chapter-list markup: the
// label span, an always-rendered read-status SVG (with inline <style>), a
// JS-hidden "Last Read" marker, a "new chapter" marker, and a <time> stamp.
const wcChapterAnchor = `
<a href="https://weebcentral.com/chapters/01KSTMTWZE2WHXAP7E18YEH80N" class="flex items-center p-2">
  <span class="me-2">
    <svg class="w-4 h-4" viewBox="0 0 24 24"><path d="M8.5 12.5"/></svg>
  </span>
  <span class="grow flex items-center gap-2">
    <span class="">%s</span>
    <span class="link-info" x-show="last_read_chapter === '01KSTMTWZE2WHXAP7E18YEH80N'">
      <svg viewBox="0 0 512 512"><g><style type="text/css">.st0 { fill: #d3d629; }</style><path/></g></svg>
      <span class="hidden md:inline">Last Read</span>
    </span>
    <span x-show="new_chapter"><svg viewBox="0 0 24 24"><path/></svg></span>
  </span>
  <time class="opacity-50" datetime="2026-05-29T19:54:30.766Z">2026-05-29T19:54:30.766746Z</time>
</a>`

func wcAnchorSelection(t *testing.T, label string) *goquery.Selection {
	t.Helper()
	html := strings.Replace(wcChapterAnchor, "%s", label, 1)
	doc, err := goquery.NewDocumentFromReader(strings.NewReader(html))
	if err != nil {
		t.Fatalf("parse fixture: %v", err)
	}
	return doc.Find(`a[href*="/chapters/"]`).First()
}

func TestWCChapterTextStripsChrome(t *testing.T) {
	// A bare numbered chapter — none of the SVG/style/"Last Read"/timestamp
	// chrome must survive.
	got := wcChapterText(wcAnchorSelection(t, "Chapter 1184"))
	if got != "Chapter 1184" {
		t.Fatalf("wcChapterText = %q, want %q", got, "Chapter 1184")
	}
	for _, junk := range []string{"Last Read", ".st0", "fill", "2026-", "#d3d629"} {
		if strings.Contains(got, junk) {
			t.Errorf("chrome %q leaked into chapter text %q", junk, got)
		}
	}
}

func TestWCChapterTextKeepsRealTitle(t *testing.T) {
	// When a chapter has a genuine title, it should be preserved (and the
	// downstream cleanChapterTitle still strips the numbered prefix).
	got := wcChapterText(wcAnchorSelection(t, "Chapter 303 - Adventure in the Land of Long Legs"))
	want := "Chapter 303 - Adventure in the Land of Long Legs"
	if got != want {
		t.Fatalf("wcChapterText = %q, want %q", got, want)
	}
	if title := cleanChapterTitle(got, "303"); title != "Adventure in the Land of Long Legs" {
		t.Errorf("cleanChapterTitle = %q, want the title without the prefix", title)
	}
}

func TestWCCustomLabelNumbers(t *testing.T) {
	// Some series label chapters with a custom prefix instead of "Chapter"
	// (Spy x Family uses "Mission", Ichi the Witch uses "Hunt"). The number
	// must still be extracted or every CBZ lands on "Chapter 0000".
	cases := []struct {
		label, num string
	}{
		{"Mission 12", "12"},
		{"Mission 112.2", "112.2"},
		{"Hunt 7", "7"},
		{"Chapter 208", "208"},    // primary regex still wins
		{"S2 Chapter 44", "44"},   // prefix noise before "Chapter"
		{"One Shot", ""},          // no number → unnumbered as before
		{"Epilogue Part Two", ""}, // words only, no trailing number
		{"Side Story 3", ""},      // not a chapter number: would collide with ch 3
		{"Extra 2", ""},
		{"Season 2", ""},
		{"Special Chapter 5", "5"}, // explicit "Chapter" still wins
	}
	for _, c := range cases {
		if num := wcNumber(c.label); num != c.num {
			t.Errorf("label %q: number = %q, want %q", c.label, num, c.num)
		}
	}
}
