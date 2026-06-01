package source

import (
	"context"
	"encoding/json"
	"fmt"
	"html"
	"net/url"
	"regexp"
	"strings"

	"tomo/internal/httpx"
	"tomo/internal/model"
)

var _ Searcher = (*MangaK)(nil)

// MangaK scrapes mangak.io, a Next.js manga/manhwa aggregator. Series and
// chapter data are embedded in the server-rendered HTML rather than fetched
// from a documented API:
//
//	series page  : https://mangak.io/<slug>           -> chapter links /<slug>/chapter-N
//	chapter page : https://mangak.io/<slug>/chapter-N -> "images":[...] CDN array
//	search       : https://mangak.io/search?q=<query> -> "url":"/<slug>","name":"<title>" pairs
//
// Page images live on a sharded CDN that 403s without a mangak.io Referer; the
// downloader sends the chapter URL as Referer, which satisfies it.
//
// As an unofficial aggregator with a bespoke front end, this adapter is
// inherently fragile — the markup and the embedded JSON shape can change
// without notice. It carries no series Number dedupe because mangak lists each
// chapter once.
type MangaK struct{}

func init() { Register(&MangaK{}) }

const mangakBase = "https://mangak.io"

func (m *MangaK) Name() string { return "mangak" }

func (m *MangaK) Matches(u string) bool { return strings.Contains(u, "mangak.io") }

var (
	// A chapter slug is "chapter-N" (optionally a sub-part like "chapter-6-2").
	mkChapNum = regexp.MustCompile(`chapter-([\d.]+)`)
	// The reader embeds its pages as a JSON array of CDN URLs. Require the array
	// to start with an http string so we don't grab an unrelated empty "images".
	mkImages = regexp.MustCompile(`"images"\s*:\s*(\["https[^\]]*\])`)
	// og:title carries the clean series title (with a " - MangaK" suffix).
	mkOgTitle = regexp.MustCompile(`property="og:title"\s+content="([^"]*)"`)
	// Search results are JSON objects pairing a single-segment slug with a name.
	mkSearchHit = regexp.MustCompile(`"url":"/([a-z0-9][a-z0-9-]*)","name":"((?:[^"\\]|\\.)*)"`)
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

	// Chapter links on the series page look like href="/<slug>/chapter-N".
	chHref := regexp.MustCompile(`/` + regexp.QuoteMeta(slug) + `/(chapter-[0-9.]+(?:-[0-9.]+)?)`)
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
		manga.Chapters = append(manga.Chapters, model.Chapter{
			Number: num,
			URL:    mangakBase + "/" + slug + "/" + chSlug,
		})
	}

	sortChaptersAscending(manga.Chapters)
	if len(manga.Chapters) == 0 {
		return nil, fmt.Errorf("no chapters found on %s (markup may have changed)", seriesURL)
	}
	return manga, nil
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
