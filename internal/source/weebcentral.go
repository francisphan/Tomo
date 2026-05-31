package source

import (
	"context"
	"fmt"
	"regexp"
	"strings"

	"github.com/PuerkitoBio/goquery"

	"tomo/internal/httpx"
	"tomo/internal/model"
)

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
)

const wcBase = "https://weebcentral.com"

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

		text := strings.TrimSpace(s.Text())
		num := ""
		if mt := wcChapNum.FindStringSubmatch(text); len(mt) == 2 {
			num = mt[1]
		}
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
