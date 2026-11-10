package main

import (
	"os"
	"path/filepath"
	"testing"

	"tomo/internal/model"
)

func TestResolveJobsDisambiguatesCollisions(t *testing.T) {
	// Two genuinely distinct chapters that sanitize to the same file name must
	// not map to the same path (which would silently overwrite one).
	chapters := []model.Chapter{
		{Number: "10", ID: "a"},
		{Number: "10", ID: "b"},
		{Number: "11", ID: "c"},
	}
	jobs := resolveJobs("/out/Series", "Series", chapters)
	if len(jobs) != 3 {
		t.Fatalf("got %d jobs, want 3", len(jobs))
	}
	seen := map[string]bool{}
	for _, j := range jobs {
		if seen[j.path] {
			t.Errorf("duplicate path assigned: %s", j.path)
		}
		seen[j.path] = true
	}
	if filepath.Base(jobs[0].path) == filepath.Base(jobs[1].path) {
		t.Errorf("colliding chapters got identical names: %s", jobs[0].path)
	}
}

func TestTitleMatches(t *testing.T) {
	cases := []struct {
		a, b string
		want bool
	}{
		{"One Piece", "One Piece", true},
		{"One Piece", "  one   piece ", true}, // case + whitespace insensitive
		{"One Piece", "One Piece Party", false},
		{"One Piece", "One Piece (Color)", false},
		{"Solo Leveling", "solo leveling", true},
	}
	for _, c := range cases {
		if got := titleMatches(c.a, c.b); got != c.want {
			t.Errorf("titleMatches(%q, %q) = %v, want %v", c.a, c.b, got, c.want)
		}
	}
}

func TestExistingChapterNumbers(t *testing.T) {
	// A library fed from more than one source holds the same chapter under
	// different names: MangaDex adds a chapter-title suffix, Weeb Central
	// doesn't, and the series prefix can differ too. All must count as the
	// same chapter, while decimal sub-chapters stay distinct.
	dir := t.TempDir()
	for _, f := range []string{
		"Blue Lock - Chapter 0001 - Dream.cbz",
		"Blue Lock - Chapter 0002.cbz",
		"Blue Lock - Chapter 0002.1.cbz",
		"Sousou no Frieren - Chapter 0003.cbz", // other series prefix, same folder
		"Blue Lock - Chapter 0085 - 8.cbz",     // numeric-looking chapter title
		"not-a-chapter.txt",
	} {
		if err := os.WriteFile(filepath.Join(dir, f), nil, 0o644); err != nil {
			t.Fatal(err)
		}
	}

	nums := existingChapterNumbers(dir)
	for _, want := range []string{"0001", "0002", "0002.1", "0003", "0085"} {
		if !nums[want] {
			t.Errorf("chapter %s not detected in %v", want, nums)
		}
	}
	if nums["0008"] {
		t.Errorf("numeric chapter title '8' misread as chapter 0008: %v", nums)
	}
	if len(nums) != 5 {
		t.Errorf("got %d chapters, want 5: %v", len(nums), nums)
	}
}

func TestMarkExisting(t *testing.T) {
	jobs := []job{
		{chapter: model.Chapter{Number: "1"}},    // exists as "0001"
		{chapter: model.Chapter{Number: "2.1"}},  // exists as "0002.1"
		{chapter: model.Chapter{Number: "2"}},    // new
		{chapter: model.Chapter{Number: "10.5"}}, // new
		{chapter: model.Chapter{Number: ""}},     // unnumbered: exact path only
	}
	markExisting(jobs, map[string]bool{"0001": true, "0002.1": true, "0000": true})
	want := []bool{true, true, false, false, false}
	for i, j := range jobs {
		if j.exists != want[i] {
			t.Errorf("job %d (ch %s): exists = %v, want %v", i, j.chapter.Number, j.exists, want[i])
		}
	}
}

func TestResolveJobsDeterministic(t *testing.T) {
	chapters := []model.Chapter{{Number: "1"}, {Number: "2"}}
	a := resolveJobs("/out/S", "S", chapters)
	b := resolveJobs("/out/S", "S", chapters)
	for i := range a {
		if a[i].path != b[i].path {
			t.Errorf("non-deterministic path at %d: %q vs %q", i, a[i].path, b[i].path)
		}
	}
}
