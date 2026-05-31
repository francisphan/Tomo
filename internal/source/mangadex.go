package source

import (
	"context"
	"fmt"
	"net/url"
	"regexp"
	"strings"

	"tomo/internal/httpx"
	"tomo/internal/model"
)

// MangaDex uses the official, documented JSON API at api.mangadex.org rather
// than scraping HTML, so it is by far the most robust adapter.
//
// Configure the translated language with the Lang field (default "en").
type MangaDex struct {
	Lang string
}

func init() { Register(&MangaDex{Lang: "en"}) }

func (m *MangaDex) Name() string { return "mangadex" }

func (m *MangaDex) Matches(u string) bool {
	return strings.Contains(u, "mangadex.org") || strings.Contains(u, "mangadex.to")
}

var mdUUID = regexp.MustCompile(`[0-9a-fA-F]{8}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{12}`)

func (m *MangaDex) lang() string {
	if m.Lang == "" {
		return "en"
	}
	return m.Lang
}

func (m *MangaDex) Series(ctx context.Context, seriesURL string) (*model.Manga, error) {
	id := mdUUID.FindString(seriesURL)
	if id == "" {
		return nil, fmt.Errorf("could not find a manga id in %q", seriesURL)
	}

	// Title.
	var meta struct {
		Data struct {
			Attributes struct {
				Title    map[string]string   `json:"title"`
				AltTitles []map[string]string `json:"altTitles"`
			} `json:"attributes"`
		} `json:"data"`
	}
	if err := httpx.GetJSON(ctx, "https://api.mangadex.org/manga/"+id, nil, &meta); err != nil {
		return nil, fmt.Errorf("fetch manga: %w", err)
	}
	title := pickTitle(meta.Data.Attributes.Title, meta.Data.Attributes.AltTitles, m.lang())

	manga := &model.Manga{Title: title, URL: seriesURL, Source: m.Name()}

	// Chapter feed, paginated.
	const limit = 500
	for offset := 0; ; offset += limit {
		q := url.Values{}
		q.Set("limit", fmt.Sprint(limit))
		q.Set("offset", fmt.Sprint(offset))
		q.Add("translatedLanguage[]", m.lang())
		q.Set("order[chapter]", "asc")
		q.Set("order[volume]", "asc")
		for _, r := range []string{"safe", "suggestive", "erotica", "pornographic"} {
			q.Add("contentRating[]", r)
		}
		q.Add("includes[]", "scanlation_group")

		var feed struct {
			Data []struct {
				ID         string `json:"id"`
				Attributes struct {
					Chapter            string `json:"chapter"`
					Title              string `json:"title"`
					TranslatedLanguage string `json:"translatedLanguage"`
					ExternalURL        string `json:"externalUrl"`
					Pages              int    `json:"pages"`
				} `json:"attributes"`
			} `json:"data"`
			Total int `json:"total"`
		}
		endpoint := "https://api.mangadex.org/manga/" + id + "/feed?" + q.Encode()
		if err := httpx.GetJSON(ctx, endpoint, nil, &feed); err != nil {
			return nil, fmt.Errorf("fetch chapter feed: %w", err)
		}
		for _, c := range feed.Data {
			// Skip chapters hosted off-site (no pages on MangaDex).
			if c.Attributes.ExternalURL != "" || c.Attributes.Pages == 0 {
				continue
			}
			manga.Chapters = append(manga.Chapters, model.Chapter{
				Number:   c.Attributes.Chapter,
				Title:    c.Attributes.Title,
				ID:       c.ID,
				Language: c.Attributes.TranslatedLanguage,
				Pages:    c.Attributes.Pages,
			})
		}
		if offset+limit >= feed.Total || len(feed.Data) == 0 {
			break
		}
	}

	manga.Chapters = dedupeByNumber(manga.Chapters)
	return manga, nil
}

func (m *MangaDex) Pages(ctx context.Context, ch model.Chapter) ([]string, error) {
	var ah struct {
		BaseURL string `json:"baseUrl"`
		Chapter struct {
			Hash string   `json:"hash"`
			Data []string `json:"data"`
		} `json:"chapter"`
	}
	if err := httpx.GetJSON(ctx, "https://api.mangadex.org/at-home/server/"+ch.ID, nil, &ah); err != nil {
		return nil, fmt.Errorf("at-home server: %w", err)
	}
	if ah.BaseURL == "" || ah.Chapter.Hash == "" {
		return nil, fmt.Errorf("empty at-home response for chapter %s", ch.ID)
	}
	urls := make([]string, 0, len(ah.Chapter.Data))
	for _, f := range ah.Chapter.Data {
		urls = append(urls, fmt.Sprintf("%s/data/%s/%s", ah.BaseURL, ah.Chapter.Hash, f))
	}
	return urls, nil
}

// pickTitle prefers the requested language, then English, then any title.
func pickTitle(title map[string]string, alts []map[string]string, lang string) string {
	if v := title[lang]; v != "" {
		return v
	}
	if v := title["en"]; v != "" {
		return v
	}
	for _, m := range alts {
		if v := m[lang]; v != "" {
			return v
		}
	}
	for _, v := range title {
		if v != "" {
			return v
		}
	}
	return "Untitled"
}

// dedupeByNumber collapses multiple uploads of the same chapter number (e.g.
// from different scanlation groups) to a single entry, deterministically
// keeping the one with the most pages — and falling back to the first seen on a
// tie. Chapters with no number are always kept. Input order is preserved.
func dedupeByNumber(chs []model.Chapter) []model.Chapter {
	idx := map[string]int{} // number -> position in out
	out := make([]model.Chapter, 0, len(chs))
	for _, c := range chs {
		if c.Number == "" {
			out = append(out, c)
			continue
		}
		if i, ok := idx[c.Number]; ok {
			if c.Pages > out[i].Pages {
				out[i] = c
			}
			continue
		}
		idx[c.Number] = len(out)
		out = append(out, c)
	}
	return out
}
