package source

import (
	"context"
	"fmt"
	"net/url"
	"regexp"
	"strings"

	"github.com/PuerkitoBio/goquery"

	"tomo/internal/httpx"
	"tomo/internal/model"
)

var _ Searcher = (*WeebCentral)(nil)

// WeebCentral scrapes weebcentral.com. The site is HTMX-driven: the full
// chapter list and the reader images are served from dedicated fragment
// endpoints rather than embedded in the series page, which actually makes them
// fairly stable to consume.
//
//	series page : https://weebcentral.com/series/{id}/{slug}
//	chapters    : GET /series/{id}/full-chapter-list   -> <a href="/chapters/{cid}">
//	pages       : GET /chapters/{cid}/images?...        -> <img> per page
type WeebCentral struct{}

func init() { Register(&WeebCentral{}) }

func (w *WeebCentral) Name() string { return "weebcentral" }

func (w *WeebCentral) Matches(u string) bool {
	return strings.Contains(u, "weebcentral.com")
}

var (
	wcSeriesID  = regexp.MustCompile(`/series/([^/?#]+)`)
	wcChapterID = regexp.MustCompile(`/chapters/([^/?#]+)`)
	wcChapNum   = regexp.MustCompile(`(?i)(?:chapter|episode|ch\.?)\s*([\d.]+)`)
	// wcLabelNum handles series whose chapter labels use a custom prefix
	// instead of "Chapter" — e.g. "Mission 12" (Spy x Family) or "Hunt 7"
	// (Ichi the Witch). It only fires when the whole label is
	// "<words> <number>", so real titles that merely contain digits
	// somewhere don't get misparsed.
	wcLabelNum = regexp.MustCompile(`(?i)^[a-z][a-z .'&-]*\s(\d+(?:\.\d+)?)$`)
	// wcNotChapter marks labels whose number isn't a chapter number
	// ("Side Story 3", "Extra 2", "Season 2"); they stay unnumbered rather
	// than colliding with the real chapter of the same number.
	wcNotChapter = regexp.MustCompile(`(?i)^(side|extra|special|bonus|omake|season|volume|vol|part|epilogue|prologue|afterword|oneshot|one shot)\b`)
)

// wcNumber extracts the chapter number from a Weeb Central chapter label, or
// "" when it has none.
func wcNumber(label string) string {
	if mt := wcChapNum.FindStringSubmatch(label); len(mt) == 2 {
		return mt[1]
	}
	if wcNotChapter.MatchString(label) {
		return ""
	}
	if mt := wcLabelNum.FindStringSubmatch(label); len(mt) == 2 {
		return mt[1]
	}
	return ""
}

const wcBase = "https://weebcentral.com"

func (w *WeebCentral) Search(ctx context.Context, query string, limit int) ([]SearchResult, error) {
	n := limit
	if n <= 0 {
		n = 32
	}
	q := url.Values{}
	q.Set("text", query)
	q.Set("limit", fmt.Sprint(n))
	q.Set("offset", "0")
	q.Set("sort", "Best Match")
	q.Set("order", "Descending")
	q.Set("official", "Any")
	q.Set("display_mode", "Full Display")
	searchURL := wcBase + "/search/data?" + q.Encode()

	doc, err := httpx.GetDoc(ctx, searchURL, map[string]string{
		"Referer":          wcBase + "/search",
		"HX-Request":       "true",
		"X-Requested-With": "XMLHttpRequest",
	})
	if err != nil {
		return nil, err
	}

	var out []SearchResult
	seen := map[string]bool{}
	doc.Find(`a[href*="/series/"]`).EachWithBreak(func(_ int, s *goquery.Selection) bool {
		href, _ := s.Attr("href")
		m := wcSeriesID.FindStringSubmatch(href)
		if len(m) != 2 || seen[m[1]] {
			return true
		}
		// Prefer the cover's alt text — the card's full text includes badge
		// labels ("Official") and layout whitespace.
		title := ""
		if alt, ok := s.Find("img").Attr("alt"); ok {
			// WeebCentral cover alts are "<Title> cover".
			title = strings.TrimSpace(alt)
			if strings.HasSuffix(strings.ToLower(title), " cover") {
				title = strings.TrimSpace(title[:len(title)-len(" cover")])
			}
		}
		if title == "" {
			title = cleanText(s.Text())
		}
		if title == "" {
			return true
		}
		seen[m[1]] = true
		out = append(out, SearchResult{Title: title, URL: abs(wcBase, href), Source: w.Name()})
		return limit <= 0 || len(out) < limit
	})
	return out, nil
}

func (w *WeebCentral) Series(ctx context.Context, seriesURL string) (*model.Manga, error) {
	m := wcSeriesID.FindStringSubmatch(seriesURL)
	if len(m) != 2 {
		return nil, fmt.Errorf("could not find series id in %q", seriesURL)
	}
	seriesID := m[1]

	// Title from the series page.
	title := ""
	if doc, err := httpx.GetDoc(ctx, seriesURL, map[string]string{"Referer": wcBase}); err == nil {
		title = strings.TrimSpace(doc.Find("h1").First().Text())
		if title == "" {
			title = strings.TrimSpace(doc.Find("title").Text())
		}
	}

	listURL := fmt.Sprintf("%s/series/%s/full-chapter-list", wcBase, seriesID)
	doc, err := httpx.GetDoc(ctx, listURL, map[string]string{
		"Referer":          seriesURL,
		"HX-Request":       "true",
		"X-Requested-With": "XMLHttpRequest",
	})
	if err != nil {
		return nil, fmt.Errorf("fetch chapter list: %w", err)
	}

	manga := &model.Manga{Title: title, URL: seriesURL, Source: w.Name()}
	seen := map[string]bool{}
	doc.Find(`a[href*="/chapters/"]`).Each(func(_ int, s *goquery.Selection) {
		href, _ := s.Attr("href")
		cm := wcChapterID.FindStringSubmatch(href)
		if len(cm) != 2 || seen[cm[1]] {
			return
		}
		seen[cm[1]] = true

		text := wcChapterText(s)
		num := wcNumber(text)
		manga.Chapters = append(manga.Chapters, model.Chapter{
			Number: num,
			Title:  cleanChapterTitle(text, num),
			ID:     cm[1],
			URL:    abs(wcBase, href),
		})
	})

	sortChaptersAscending(manga.Chapters)
	if len(manga.Chapters) == 0 {
		return nil, fmt.Errorf("no chapters found for %s (markup may have changed)", seriesURL)
	}
	return manga, nil
}

// wcChapterText extracts just the chapter label (e.g. "Chapter 303") from a
// WeebCentral chapter anchor. A naive s.Text() also slurps the read-status
// SVG icon (including its inline <style> CSS), an always-rendered-but-JS-hidden
// "Last Read" marker (Alpine x-show), the "new chapter" marker, and a <time>
// stamp — all of which leaked into CBZ filenames. We clone the node so we don't
// mutate the shared document, drop the non-label elements, then collapse the
// remaining whitespace.
func wcChapterText(s *goquery.Selection) string {
	sel := s.Clone()
	sel.Find("svg, style, script, time, [x-show]").Remove()
	return cleanText(sel.Text())
}

func (w *WeebCentral) Pages(ctx context.Context, ch model.Chapter) ([]string, error) {
	// reading_style=long_strip returns the whole chapter's images in one
	// fragment (current_page is the reader's initial scroll position, not a
	// server-side page window).
	imagesURL := fmt.Sprintf("%s/chapters/%s/images?is_prev=False&current_page=1&reading_style=long_strip",
		wcBase, ch.ID)
	doc, err := httpx.GetDoc(ctx, imagesURL, map[string]string{
		"Referer":          ch.URL,
		"HX-Request":       "true",
		"X-Requested-With": "XMLHttpRequest",
	})
	if err != nil {
		return nil, err
	}
	var urls []string
	doc.Find("img").Each(func(_ int, s *goquery.Selection) {
		if src := imgSrc(s); src != "" && hasImageExt(strings.ToLower(src)) {
			urls = append(urls, abs(wcBase, src))
		}
	})
	if len(urls) == 0 {
		return nil, fmt.Errorf("no page images found for %s (markup may have changed)", ch.URL)
	}
	return urls, nil
}
