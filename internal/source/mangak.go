package source

import (
	"context"
	"encoding/json"
	"fmt"
	"html"
	"net/url"
	"regexp"
	"strconv"
	"strings"

	"tomo/internal/httpx"
	"tomo/internal/model"
)

var _ Searcher = (*MangaK)(nil)

// MangaK scrapes mangak.io, a Next.js manga/manhwa aggregator. Most data is
// dug out of the server-rendered HTML; the full chapter list comes from the
// site's (undocumented) JSON API:
//
//	series page  : https://mangak.io/<slug>                 -> manga id + recent chapters
//	chapters API : https://api.mangak.io/titles/<id>/chapters -> full list w/ reader URLs
//	chapter page : https://mangak.io/<slug>/chapter-N       -> "images":[...] CDN array
//	search       : https://mangak.io/search?q=<query>       -> "url":"/<slug>","name":"<title>" pairs
//
// Page images live on a sharded CDN that 403s without a mangak.io Referer; the
// downloader sends the chapter URL as Referer, which satisfies it.
//
// The series page server-renders only the most recent ~40 chapters, so the full
// list is pulled from the JSON API (api.mangak.io/titles/<id>/chapters), keyed
// by the manga id embedded in the page. If that fails we fall back to scraping
// the (truncated) page and flag the missing chapters via Manga.Excluded so the
// caller can warn instead of silently under-reporting a long series.
//
// As an unofficial aggregator with a bespoke front end, this adapter is
// inherently fragile — the markup, embedded JSON, and API shape can change
// without notice. It carries no series Number dedupe because mangak lists each
// chapter once.
type MangaK struct{}

func init() { Register(&MangaK{}) }

const (
	mangakBase = "https://mangak.io"
	mangakAPI  = "https://api.mangak.io"
)

func (m *MangaK) Name() string { return "mangak" }

func (m *MangaK) Matches(u string) bool { return strings.Contains(u, "mangak.io") }

var (
	// A chapter slug is "chapter-N" (optionally a sub-part like "chapter-6-2").
	mkChapNum = regexp.MustCompile(`chapter-([\d.]+)`)
	// The chapter number is parsed from the human name ("Chapter 402.7"), which
	// keeps the decimal that the dash-joined slug ("chapter-402-7") loses.
	mkChapterName = regexp.MustCompile(`(?i)chapter\s*([0-9]+(?:\.[0-9]+)?)`)
	// The reader embeds its pages as a JSON array of CDN URLs. Require the array
	// to start with an http string so we don't grab an unrelated empty "images".
	mkImages = regexp.MustCompile(`"images"\s*:\s*(\["https[^\]]*\])`)
	// og:title carries the clean series title (with a " - MangaK" suffix).
	mkOgTitle = regexp.MustCompile(`property="og:title"\s+content="([^"]*)"`)
	// Search results are JSON objects pairing a single-segment slug with a name.
	mkSearchHit = regexp.MustCompile(`"url":"/([a-z0-9][a-z0-9-]*)","name":"((?:[^"\\]|\\.)*)"`)
	// The manga id (for the chapters API) and total chapter count are embedded
	// in the server-rendered series page.
	mkMangaID   = regexp.MustCompile(`"initialManga":\{"id":"([a-zA-Z0-9_-]+)"`)
	mkChapTotal = regexp.MustCompile(`"chaptersCount":(\d+)`)
)

// mkSlug extracts the leading single-segment series slug from a mangak.io URL.
func mkSlug(seriesURL string) (string, error) {
	u, err := url.Parse(seriesURL)
	if err != nil {
		return "", err
	}
	slug := strings.Trim(u.Path, "/")
	if i := strings.IndexByte(slug, '/'); i >= 0 {
		slug = slug[:i]
	}
	if slug == "" {
		return "", fmt.Errorf("no series slug in %q", seriesURL)
	}
	return slug, nil
}

func (m *MangaK) Series(ctx context.Context, seriesURL string) (*model.Manga, error) {
	slug, err := mkSlug(seriesURL)
	if err != nil {
		return nil, err
	}
	body, err := httpx.GetBytes(ctx, seriesURL, map[string]string{"Referer": mangakBase + "/"})
	if err != nil {
		return nil, err
	}
	page := string(body)

	manga := &model.Manga{Title: mkTitle(page), URL: seriesURL, Source: m.Name()}

	// Preferred path: the complete chapter list from the JSON API, keyed by the
	// manga id embedded in the page. The series HTML only renders the latest ~40
	// chapters, so scraping it alone truncates long series.
	if id := mkMangaID.FindStringSubmatch(page); len(id) == 2 {
		if chs, err := m.apiChapters(ctx, id[1]); err == nil && len(chs) > 0 {
			manga.Chapters = chs
			sortChaptersAscending(manga.Chapters)
			return manga, nil
		}
	}

	// Fallback: scrape the chapter links present in the page. This is incomplete
	// for long series, so flag the shortfall (against the page's own
	// "chaptersCount") via Excluded — the caller warns and may switch sources.
	manga.Chapters = mkScrapeChapters(page, slug)
	if total := mkChapTotalCount(page); total > len(manga.Chapters) {
		manga.Excluded = total - len(manga.Chapters)
		manga.ExcludedReason = "mangak.io only server-renders its most recent chapters and its API was unreachable"
	}
	sortChaptersAscending(manga.Chapters)
	if len(manga.Chapters) == 0 {
		return nil, fmt.Errorf("no chapters found on %s (markup may have changed)", seriesURL)
	}
	return manga, nil
}

// apiChapters fetches and parses the full chapter list for a manga id.
func (m *MangaK) apiChapters(ctx context.Context, mangaID string) ([]model.Chapter, error) {
	endpoint := mangakAPI + "/titles/" + url.PathEscape(mangaID) + "/chapters"
	body, err := httpx.GetBytes(ctx, endpoint, map[string]string{"Referer": mangakBase + "/"})
	if err != nil {
		return nil, err
	}
	return mkParseChapters(body)
}

// mkParseChapters turns the chapters API response into model.Chapters. Each
// entry carries its reader URL directly, so Pages() needs no further lookup.
func mkParseChapters(body []byte) ([]model.Chapter, error) {
	var resp struct {
		Success bool `json:"success"`
		Data    struct {
			Chapters []struct {
				ID   string `json:"id"`
				URL  string `json:"url"`
				Name string `json:"name"`
			} `json:"chapters"`
		} `json:"data"`
	}
	if err := json.Unmarshal(body, &resp); err != nil {
		return nil, err
	}
	if !resp.Success {
		return nil, fmt.Errorf("mangak chapters API returned success=false")
	}
	out := make([]model.Chapter, 0, len(resp.Data.Chapters))
	for _, c := range resp.Data.Chapters {
		if c.URL == "" {
			continue
		}
		num := ""
		if nm := mkChapterName.FindStringSubmatch(c.Name); len(nm) == 2 {
			num = nm[1]
		}
		out = append(out, model.Chapter{
			Number: num,
			Title:  cleanChapterTitle(c.Name, num),
			ID:     c.ID,
			URL:    abs(mangakBase, c.URL),
		})
	}
	return out, nil
}

// mkScrapeChapters extracts chapter links from the series page HTML (the
// degraded path; the page only renders the most recent chapters).
func mkScrapeChapters(page, slug string) []model.Chapter {
	chHref := regexp.MustCompile(`/` + regexp.QuoteMeta(slug) + `/(chapter-[a-z0-9][a-z0-9.-]*)`)
	var out []model.Chapter
	seen := map[string]bool{}
	for _, mm := range chHref.FindAllStringSubmatch(page, -1) {
		chSlug := mm[1]
		if seen[chSlug] {
			continue
		}
		seen[chSlug] = true
		num := ""
		if nm := mkChapNum.FindStringSubmatch(chSlug); len(nm) == 2 {
			num = nm[1]
		}
		out = append(out, model.Chapter{
			Number: num,
			URL:    mangakBase + "/" + slug + "/" + chSlug,
		})
	}
	return out
}

// mkChapTotalCount reads the embedded total chapter count, or 0 if absent.
func mkChapTotalCount(page string) int {
	if m := mkChapTotal.FindStringSubmatch(page); len(m) == 2 {
		if n, err := strconv.Atoi(m[1]); err == nil {
			return n
		}
	}
	return 0
}

func (m *MangaK) Pages(ctx context.Context, ch model.Chapter) ([]string, error) {
	body, err := httpx.GetBytes(ctx, ch.URL, map[string]string{"Referer": mangakBase + "/"})
	if err != nil {
		return nil, err
	}
	mm := mkImages.FindStringSubmatch(string(body))
	if len(mm) != 2 {
		return nil, fmt.Errorf("no page images found for %s (markup may have changed)", ch.URL)
	}
	var urls []string
	if err := json.Unmarshal([]byte(decodeUnicode(mm[1])), &urls); err != nil {
		return nil, fmt.Errorf("parse image array for %s: %w", ch.URL, err)
	}
	if len(urls) == 0 {
		return nil, fmt.Errorf("empty image array for %s", ch.URL)
	}
	return urls, nil
}

func (m *MangaK) Search(ctx context.Context, query string, limit int) ([]SearchResult, error) {
	if limit <= 0 {
		limit = 24
	}
	searchURL := mangakBase + "/search?q=" + url.QueryEscape(query)
	body, err := httpx.GetBytes(ctx, searchURL, map[string]string{"Referer": mangakBase + "/"})
	if err != nil {
		return nil, err
	}

	var out []SearchResult
	seen := map[string]bool{}
	for _, mm := range mkSearchHit.FindAllStringSubmatch(string(body), -1) {
		slug, rawName := mm[1], mm[2]
		if seen[slug] {
			continue
		}
		// Names are JSON string fragments (with \u escapes etc.); decode them.
		var name string
		if err := json.Unmarshal([]byte(`"`+rawName+`"`), &name); err != nil {
			name = rawName
		}
		name = strings.TrimSpace(name)
		if name == "" {
			continue
		}
		seen[slug] = true
		out = append(out, SearchResult{
			Title:  name,
			URL:    mangakBase + "/" + slug,
			Source: m.Name(),
		})
		if len(out) >= limit {
			break
		}
	}
	return out, nil
}

// mkTitle pulls the series title from og:title, dropping the site-name suffix.
func mkTitle(page string) string {
	if m := mkOgTitle.FindStringSubmatch(page); len(m) == 2 {
		t := html.UnescapeString(m[1])
		for _, suf := range []string{" - MangaK", " | MangaK"} {
			t = strings.TrimSuffix(t, suf)
		}
		if t = strings.TrimSpace(t); t != "" {
			return t
		}
	}
	return "Untitled"
}
