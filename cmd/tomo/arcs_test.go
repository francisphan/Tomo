package main

import (
	"testing"

	"tomo/internal/arcs"
)

func TestChapterArcInfo(t *testing.T) {
	end := func(v float64) *float64 { return &v }
	s := arcs.Series{Title: "Ichi the Witch", Arcs: []arcs.Arc{
		{Name: "Hunting", Start: 1, End: end(10), Matches: []arcs.Match{{Name: "Duel", Start: 4, End: 5}}},
	}}
	pos := map[string]int{}
	cases := []struct {
		c                   chapterFile
		title, list, number string
	}{
		{chapterFile{number: 1, title: "Hunt 1"}, "Hunting · Hunt 1", "Ichi the Witch · Hunting", "1"},
		{chapterFile{number: 4}, "Hunting · Duel", "Ichi the Witch · Hunting", "2"},
		{chapterFile{number: 11, title: "Hunt 11"}, "Hunt 11", "", ""},
	}
	for _, c := range cases {
		title, list, number := chapterArcInfo(s, c.c, pos)
		if title != c.title || list != c.list || number != c.number {
			t.Errorf("chapter %g = %q, %q, %q; want %q, %q, %q", c.c.number, title, list, number, c.title, c.list, c.number)
		}
	}
	if m := chapterFileName.FindStringSubmatch("Ichi the Witch - Chapter 0060 - Hunt 60.cbz"); m == nil || m[1] != "0060" || m[2] != "Hunt 60" {
		t.Errorf("file name parse = %q", m)
	}
}
