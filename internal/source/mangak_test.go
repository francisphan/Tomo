package source

import (
	"encoding/json"
	"testing"
)

func TestMKTitleStripsSiteSuffix(t *testing.T) {
	cases := map[string]string{
		`<meta property="og:title" content="The Dawn That Twists Your Neck - MangaK"/>`: "The Dawn That Twists Your Neck",
		`<meta property="og:title" content="Solo Leveling | MangaK">`:                   "Solo Leveling",
		`<meta property="og:title" content="Tom &amp; Jerry - MangaK">`:                 "Tom & Jerry",
		`<html>no og title here</html>`:                                                 "Untitled",
	}
	for in, want := range cases {
		if got := mkTitle(in); got != want {
			t.Errorf("mkTitle(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestMKSearchHitMatchesSeriesNotChapters(t *testing.T) {
	// A series entry has a single-segment slug; chapter entries carry a second
	// path segment and must not be picked up as search results.
	page := `...
		{"url":"/the-dawn-that-twists-your-neck","name":"The Dawn That Twists Your Neck"}
		{"url":"/the-dawn-that-twists-your-neck/chapter-47","name":"Chapter 47"}
		{"url":"/moscows-dawn","name":"Moscow’s Dawn"}
	...`
	got := map[string]string{}
	for _, m := range mkSearchHit.FindAllStringSubmatch(page, -1) {
		var name string
		_ = json.Unmarshal([]byte(`"`+m[2]+`"`), &name)
		got[m[1]] = name
	}
	if len(got) != 2 {
		t.Fatalf("got %d series hits, want 2: %+v", len(got), got)
	}
	if got["the-dawn-that-twists-your-neck"] != "The Dawn That Twists Your Neck" {
		t.Errorf("missing/!wrong target series: %+v", got)
	}
	if got["moscows-dawn"] != "Moscow’s Dawn" {
		t.Errorf("unicode name not decoded: %q", got["moscows-dawn"])
	}
	if _, ok := got["the-dawn-that-twists-your-neck/chapter-47"]; ok {
		t.Errorf("chapter entry leaked into search results")
	}
}

func TestMKImagesArrayExtraction(t *testing.T) {
	page := `something {"images":["https://rx.qvzra.org/r/p/a/b/1.webp","https://rx.qvzrb.org/r/p/a/b/2.webp"]} more`
	m := mkImages.FindStringSubmatch(page)
	if len(m) != 2 {
		t.Fatalf("images array not matched")
	}
	var urls []string
	if err := json.Unmarshal([]byte(decodeUnicode(m[1])), &urls); err != nil {
		t.Fatalf("unmarshal images: %v", err)
	}
	if len(urls) != 2 || urls[0] != "https://rx.qvzra.org/r/p/a/b/1.webp" {
		t.Errorf("unexpected image urls: %+v", urls)
	}
}
