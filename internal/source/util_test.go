package source

import (
	"testing"

	"tomo/internal/model"
)

func TestDedupeByNumberPrefersMostPages(t *testing.T) {
	in := []model.Chapter{
		{Number: "1", ID: "a", Pages: 10},
		{Number: "1", ID: "b", Pages: 18}, // more complete — should win
		{Number: "2", ID: "c", Pages: 12},
		{Number: "", ID: "oneshot", Pages: 5}, // numberless always kept
	}
	out := dedupeByNumber(in)
	if len(out) != 3 {
		t.Fatalf("got %d chapters, want 3: %+v", len(out), out)
	}
	if out[0].Number != "1" || out[0].ID != "b" {
		t.Errorf("chapter 1 should be the 18-page upload (ID b), got ID %q (%d pages)", out[0].ID, out[0].Pages)
	}
	if out[2].ID != "oneshot" {
		t.Errorf("numberless chapter should be preserved, got %+v", out[2])
	}
}

func TestSortChaptersAscending(t *testing.T) {
	chs := []model.Chapter{
		{Number: "10"}, {Number: "2"}, {Number: "10.5"}, {Number: "1"},
	}
	sortChaptersAscending(chs)
	want := []string{"1", "2", "10", "10.5"}
	for i, w := range want {
		if chs[i].Number != w {
			t.Errorf("position %d: got %q want %q", i, chs[i].Number, w)
		}
	}
}

func TestCleanText(t *testing.T) {
	cases := map[string]string{
		"  Solo   Leveling ":  "Solo Leveling",
		"Official\n\n  Title": "Official Title",
		"line1\n\t line2":     "line1 line2",
		"":                    "",
	}
	for in, want := range cases {
		if got := cleanText(in); got != want {
			t.Errorf("cleanText(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestAllRegisteredSourcesSearchable(t *testing.T) {
	for _, s := range All() {
		if _, ok := s.(Searcher); !ok {
			t.Errorf("source %q does not implement Searcher", s.Name())
		}
	}
}

func TestCleanChapterTitle(t *testing.T) {
	cases := []struct{ text, number, want string }{
		{"Chapter 10 - The Title", "10", "The Title"},
		{"Ch.10 The Title", "10", "The Title"},
		{"Vol.2 Ch.10: The Title", "10", "The Title"},
		{"Episode 3", "3", ""},
		{"Chapter 10", "10", ""},
	}
	for _, c := range cases {
		if got := cleanChapterTitle(c.text, c.number); got != c.want {
			t.Errorf("cleanChapterTitle(%q, %q) = %q, want %q", c.text, c.number, got, c.want)
		}
	}
}
