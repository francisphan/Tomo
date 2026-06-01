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

func TestMKParseChapters(t *testing.T) {
	// Number comes from the name (keeping the decimal the slug "chapter-402-7"
	// loses); specials/notices with no "Chapter N" get an empty Number and keep
	// their name as the title.
	body := []byte(`{"success":true,"data":{"chapters":[
		{"id":"eDkAb5z8","url":"/haikyuu/chapter-402-7","name":"Chapter 402.7","slug":"chapter-402-7"},
		{"id":"aaa","url":"/haikyuu/chapter-1-endings-and-beginnings","name":"Chapter 1","slug":"chapter-1-endings-and-beginnings"},
		{"id":"bbb","url":"/haikyuu/special-nobuyuki-kais-nightmare","name":"Special. : Nobuyuki Kai's Nightmare","slug":"special-nobuyuki-kais-nightmare"},
		{"id":"ccc","url":"","name":"Chapter 9 (no url, skipped)","slug":"chapter-9"}
	]}}`)
	chs, err := mkParseChapters(body)
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	if len(chs) != 3 {
		t.Fatalf("got %d chapters, want 3 (the url-less entry must be dropped): %+v", len(chs), chs)
	}
	if chs[0].Number != "402.7" {
		t.Errorf("decimal number from name: got %q want 402.7", chs[0].Number)
	}
	if chs[0].URL != "https://mangak.io/haikyuu/chapter-402-7" {
		t.Errorf("reader URL not absolute: %q", chs[0].URL)
	}
	if chs[2].Number != "" || chs[2].Title != "Special. : Nobuyuki Kai's Nightmare" {
		t.Errorf("special should have empty number and keep its name as title: %+v", chs[2])
	}
}

func TestMKParseChaptersRejectsFailure(t *testing.T) {
	if _, err := mkParseChapters([]byte(`{"success":false,"message":"Server error"}`)); err == nil {
		t.Error("expected error on success=false response")
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
