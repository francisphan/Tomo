package source

import (
	"regexp"
	"sort"
	"strconv"
	"strings"

	"tomo/internal/model"
)

// sortChaptersAscending orders chapters by their parsed numeric value. Most
// sites list newest-first, but rather than blindly reversing (which breaks if a
// site is already ascending) we sort explicitly. Chapters whose number can't be
// parsed keep their original relative order via a stable sort.
func sortChaptersAscending(chs []model.Chapter) {
	sort.SliceStable(chs, func(i, j int) bool {
		ni, ei := strconv.ParseFloat(strings.TrimSpace(chs[i].Number), 64)
		nj, ej := strconv.ParseFloat(strings.TrimSpace(chs[j].Number), 64)
		if ei == nil && ej == nil {
			return ni < nj
		}
		return false
	})
}

// chapterPrefix matches a leading "Vol.2 Ch.10" / "Chapter 10" / "Episode 3"
// style prefix so we don't repeat it in the CBZ name.
var chapterPrefix = regexp.MustCompile(`(?i)^\s*(?:vol(?:ume)?[\s.]*[\d.]+[\s.]*)?(?:chapter|chap|episode|ep|ch)[\s.]*[\d.]+\s*`)

// cleanChapterTitle strips a leading chapter/volume prefix so the CBZ name
// doesn't duplicate the number. Returns "" when nothing meaningful remains.
func cleanChapterTitle(text, number string) string {
	t := chapterPrefix.ReplaceAllString(strings.TrimSpace(text), "")
	t = strings.TrimLeft(t, " -–—:•")
	t = strings.TrimSpace(t)
	if t == "" || t == number {
		return ""
	}
	return t
}

// hasImageExt reports whether a (lowercased) URL path ends in an image type.
func hasImageExt(low string) bool {
	low = stripQ(low)
	for _, e := range []string{".jpg", ".jpeg", ".png", ".webp", ".gif", ".avif"} {
		if strings.HasSuffix(low, e) {
			return true
		}
	}
	return false
}

// nonPageMarkers are substrings that flag an image as site chrome rather than a
// manga page (logos, avatars, ads, social icons, thumbnails).
var nonPageMarkers = []string{
	"logo", "avatar", "/icon", "icon.", "banner", "/ads", "advert",
	"discord", "facebook", "twitter", "telegram", "/thumb", "favicon", "sprite",
}

// isNonPageAsset reports whether a (lowercased) image URL looks like site
// chrome we should skip when collecting reader pages.
func isNonPageAsset(low string) bool {
	for _, m := range nonPageMarkers {
		if strings.Contains(low, m) {
			return true
		}
	}
	return false
}

func stripQ(u string) string {
	if i := strings.IndexAny(u, "?#"); i >= 0 {
		return u[:i]
	}
	return u
}

// cleanText collapses runs of whitespace (including the newlines and indent
// that show up when scraping a card's full text) into single spaces.
func cleanText(s string) string {
	return strings.Join(strings.Fields(s), " ")
}
