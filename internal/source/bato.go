package source

import (
	"context"
	"encoding/json"
	"fmt"
	"regexp"
	"strings"

	"github.com/PuerkitoBio/goquery"

	"tomo/internal/httpx"
	"tomo/internal/model"
)

// Bato scrapes bato.to and its many mirror domains. The reader page embeds the
// page image URLs in a script as an array (historically `imgHttps`); we extract
// that array directly.
//
// Caveat: some Bato deployments additionally require a per-image access token
// (derived from encrypted `batoWord`/`batoPass` values via client-side JS). We
// do not evaluate that JS, so on token-gated mirrors image requests may 403. If
// pages fail to download, try a different mirror or another source. Mirrors and
// markup change often, so Bato support is best-effort.
type Bato struct{}

func init() { Register(&Bato{}) }

// known bato mirror hosts.
var batoHosts = []string{
	"bato.to", "batotoo.com", "batotwo.com", "battwo.com", "comiko.net",
	"comiko.org", "batocomic.com", "batocomic.net", "batocomic.org",
	"readtoto.com", "mto.to", "bato.si", "wto.to", "dto.to", "jto.to",
}

func (b *Bato) Name() string { return "bato" }

func (b *Bato) Matches(u string) bool {
	for _, h := range batoHosts {
		if strings.Contains(u, h) {
			return true
		}
	}
	return false
}

var (
	batoChapterHref = regexp.MustCompile(`/chapter/\d+`)
	batoChapNum     = regexp.MustCompile(`(?i)(?:chapter|episode|vol[\s.]*\d+\s*ch\.?|ch\.?)\s*([\d.]+)`)
	// Match an array of quoted URLs assigned to imgHttps / images / etc. The
	// body is matched up to the statement terminator (`;`) rather than the first
	// `]`, so a `]` inside a URL doesn't truncate the capture.
	batoImgArray = regexp.MustCompile(`(?:imgHttps|"images"|"imageFiles"|const\s+images)\s*[=:]\s*(\[[^;]*\])`)
)

func (b *Bato) Series(ctx context.Context, seriesURL string) (*model.Manga, error) {
	doc, err := httpx.GetDoc(ctx, seriesURL, map[string]string{"Referer": origin(seriesURL)})
	if err != nil {
		return nil, err
	}

	title := strings.TrimSpace(doc.Find("h3 a, .item-title, h1").First().Text())
	if title == "" {
		title = strings.TrimSpace(doc.Find("title").Text())
	}
	manga := &model.Manga{Title: title, URL: seriesURL, Source: b.Name()}

	seen := map[string]bool{}
	doc.Find(`a[href*="/chapter/"]`).Each(func(_ int, s *goquery.Selection) {
		href, _ := s.Attr("href")
		if !batoChapterHref.MatchString(href) {
			return
		}
		full := abs(seriesURL, href)
		if seen[full] {
			return
		}
		seen[full] = true

		text := strings.TrimSpace(s.Text())
		num := ""
		if mt := batoChapNum.FindStringSubmatch(text); len(mt) == 2 {
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

func (b *Bato) Pages(ctx context.Context, ch model.Chapter) ([]string, error) {
	body, err := httpx.GetBytes(ctx, ch.URL, map[string]string{"Referer": origin(ch.URL)})
	if err != nil {
		return nil, err
	}
	html := string(body)

	if m := batoImgArray.FindStringSubmatch(html); len(m) == 2 {
		var urls []string
		// The captured group is a JSON array of strings.
		if err := json.Unmarshal([]byte(decodeUnicode(m[1])), &urls); err == nil && len(urls) > 0 {
			return urls, nil
		}
	}

	// Fallback: parse any reader <img> tags from the document.
	doc, err := goquery.NewDocumentFromReader(strings.NewReader(html))
	if err == nil {
		var urls []string
		doc.Find("img").Each(func(_ int, s *goquery.Selection) {
			if src := imgSrc(s); src != "" && hasImageExt(strings.ToLower(src)) {
				urls = append(urls, abs(ch.URL, src))
			}
		})
		if len(urls) > 0 {
			return urls, nil
		}
	}
	return nil, fmt.Errorf("no page images found for %s (markup may have changed)", ch.URL)
}

// decodeUnicode unescapes the `\/` sequence that appears in embedded JSON-in-JS.
// (`\uXXXX` escapes are left for json.Unmarshal, which decodes them natively.)
func decodeUnicode(s string) string {
	return strings.ReplaceAll(s, `\/`, `/`)
}
