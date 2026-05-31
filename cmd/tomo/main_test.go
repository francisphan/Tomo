package main

import (
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
