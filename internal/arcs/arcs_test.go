package arcs

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func f(v float64) *float64 { return &v }

var haikyuu = Series{
	Title:  "Haikyu!!",
	Status: "complete",
	Arcs: []Arc{
		{Name: "Interhigh", Start: 36, End: f(71), Summary: "Tokonami, then Aoba Johsai.",
			Matches: []Match{{Name: "Karasuno vs Aoba Johsai", Start: 49, End: 68}}},
		{Name: "Final", Start: 370, End: f(402)},
	},
}

func TestArcOfAndMatchOf(t *testing.T) {
	cases := []struct {
		n     float64
		arc   string
		match string
	}{
		{36, "Interhigh", ""},
		{49, "Interhigh", "Karasuno vs Aoba Johsai"},
		{68.5, "Interhigh", "Karasuno vs Aoba Johsai"}, // an extra after the match's last chapter
		{69, "Interhigh", ""},
		{71.5, "Interhigh", ""},
		{402.3, "Final", ""},
		{35, "", ""},
		{200, "", ""},
	}
	for _, c := range cases {
		a, ok := haikyuu.ArcOf(c.n)
		if got := map[bool]string{true: a.Name}[ok]; got != c.arc {
			t.Errorf("ArcOf(%g) = %q, want %q", c.n, got, c.arc)
			continue
		}
		m, ok := a.MatchOf(c.n)
		if got := map[bool]string{true: m.Name}[ok]; got != c.match {
			t.Errorf("MatchOf(%g) = %q, want %q", c.n, got, c.match)
		}
	}
}

func TestOngoingArcHasNoEnd(t *testing.T) {
	s := Series{Arcs: []Arc{{Name: "Now", Start: 100}}}
	if a, ok := s.ArcOf(5000); !ok || a.Name != "Now" {
		t.Errorf("ArcOf(5000) = %v, %v", a, ok)
	}
}

func TestFindUsesFolderThenTitle(t *testing.T) {
	all := []Series{{ID: "frieren", Title: "Frieren: Beyond Journey's End", Folder: "Sousou no Frieren"}, haikyuu}
	if s, ok := Find(all, "sousou no frieren"); !ok || s.ID != "frieren" {
		t.Errorf("Find by folder = %v, %v", s.ID, ok)
	}
	if s, ok := Find(all, "Haikyu!!"); !ok || s.Title != "Haikyu!!" {
		t.Errorf("Find by title = %v, %v", s.Title, ok)
	}
	if _, ok := Find(all, "Eleceed"); ok {
		t.Error("found a series the guide doesn't have")
	}
}

func TestGuideText(t *testing.T) {
	want := "Arc guide\n\nCh. 36–71 · Interhigh\nTokonami, then Aoba Johsai.\n  • Ch. 49–68 · Karasuno vs Aoba Johsai\n\nCh. 370–402 · Final"
	if got := haikyuu.Guide(); got != want {
		t.Errorf("Guide() =\n%s\nwant\n%s", got, want)
	}
	parts := Series{Arcs: []Arc{{Part: "East Blue", Name: "Romance Dawn", Start: 1, End: f(7)}, {Part: "East Blue", Name: "Orange Town", Start: 8, End: f(21)}, {Name: "Now", Start: 22}}}
	g := parts.Guide()
	if strings.Count(g, "EAST BLUE") != 1 || !strings.Contains(g, "Ch. 22– · Now") {
		t.Errorf("parts guide:\n%s", g)
	}
}

func TestLoadReadsTheRealGuide(t *testing.T) {
	path := filepath.Join("..", "..", "site", "arcs.json")
	if _, err := os.Stat(path); err != nil {
		t.Skip("site/arcs.json not present")
	}
	all, err := Load(path)
	if err != nil {
		t.Fatal(err)
	}
	h, ok := Find(all, "Haikyu!!")
	if !ok {
		t.Fatal("Haikyu!! missing from the guide")
	}
	a, _ := h.ArcOf(290)
	if m, ok := a.MatchOf(290); a.Name != "Tokyo Nationals" || !ok || m.Name != "Karasuno vs Inarizaki (continued)" {
		t.Errorf("ch 290 = %q / %q", a.Name, m.Name)
	}
}
