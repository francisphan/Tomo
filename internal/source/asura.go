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

// Asura scrapes asuracomic.net (Asura Scans), a popular manhwa scanlation site.
//
// HTML scrapers are inherently fragile: when Asura changes their markup the
// selectors below may need updating. They are deliberately kept narrow and in
// one place for exactly that reason. Asura is a Next.js app but the chapter
// links and reader images are present in the server-rendered HTML.
type Asura struct{}

func init() { Register(&Asura{}) }

func (a *Asura) Name() string { return "asura" }

func (a *Asura) Matches(u string) bool {
	return strings.Contains(u, "asuracomic.net") || strings.Contains(u, "asurascans") ||
		strings.Contains(u, "asuratoon")
}

var (
	asuraChapterHref = regexp.MustCompile(`/chapter/[\w.\-]+`)
	asuraChapterNum  = regexp.MustCompile(`(?i)chapter\s*([\d.]+)`)
)

func (a *Asura) Series(ctx context.Context, seriesURL string) (*model.Manga, error) {
	doc, err := httpx.GetDoc(ctx, seriesURL, map[string]string{"Referer": origin(seriesURL)})
	if err != nil {
		return nil, err
	}

	title := strings.TrimSpace(doc.Find("h1, .text-center.text-xl.font-bold").First().Text())
	if title == "" {
		title = strings.TrimSpace(doc.Find("title").Text())
	}
	manga := &model.Manga{Title: title, URL: seriesURL, Source: a.Name()}

	seen := map[string]bool{}
	doc.Find(`a[href*="/chapter/"]`).Each(func(_ int, s *goquery.Selection) {
		href, _ := s.Attr("href")
		if !asuraChapterHref.MatchString(href) {
			return
		}
		full := abs(seriesURL, href)
		if seen[full] {
			return
		}
		seen[full] = true

		text := strings.TrimSpace(s.Text())
		num := ""
		if mt := asuraChapterNum.FindStringSubmatch(text); len(mt) == 2 {
			num = mt[1]
		} else if mt := asuraChapterNum.FindStringSubmatch(href); len(mt) == 2 {
			num = mt[1]
		}
		manga.Chapters = append(manga.Chapters, model.Chapter{
			Number: num,
			Title:  cleanChapterTitle(text, num),
			URL:    full,
		})
	})

	sortChaptersAscending(manga.Chapters)
	if len(manga.Chapters) == 0 {
		return nil, fmt.Errorf("no chapters found on %s (markup may have changed)", seriesURL)
	}
	return manga, nil
}

func (a *Asura) Pages(ctx context.Context, ch model.Chapter) ([]string, error) {
	doc, err := httpx.GetDoc(ctx, ch.URL, map[string]string{"Referer": origin(ch.URL)})
	if err != nil {
		return nil, err
	}
	var urls []string
	// Reader images are real image files; exclude site chrome (logos, icons,
	// ads) rather than allow-listing a CDN host, which breaks if Asura moves
	// its image storage.
	doc.Find("img").Each(func(_ int, s *goquery.Selection) {
		src := imgSrc(s)
		if src == "" {
			return
		}
		low := strings.ToLower(src)
		if !hasImageExt(low) || isNonPageAsset(low) {
			return
		}
		urls = append(urls, abs(ch.URL, src))
	})
	if len(urls) == 0 {
		return nil, fmt.Errorf("no page images found for %s (markup may have changed)", ch.URL)
	}
	return urls, nil
}
