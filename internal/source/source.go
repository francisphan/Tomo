// Package source defines the Source interface that every site adapter
// implements, plus a tiny registry used to pick an adapter from a URL.
package source

import (
	"context"

	"tomo/internal/model"
)

// Source is a manga/manhwa site adapter. A Source knows how to recognise its
// own URLs, list a series' chapters, and resolve a chapter's page images.
type Source interface {
	// Name is a short human-readable identifier, e.g. "mangadex".
	Name() string
	// Matches reports whether this source can handle the given series URL.
	Matches(url string) bool
	// Series fetches series metadata and the full chapter list.
	Series(ctx context.Context, seriesURL string) (*model.Manga, error)
	// Pages returns the ordered image URLs for a single chapter.
	Pages(ctx context.Context, ch model.Chapter) ([]string, error)
}

// SearchResult is one hit from a title search: a series and the URL to feed
// back into a download.
type SearchResult struct {
	Title  string
	URL    string
	Source string
	Info   string // optional extra detail, e.g. "ongoing · 2018"
}

// Searcher is an optional capability: a Source that can find series by title.
// Use a type assertion (s, ok := src.(Searcher)) to detect support.
type Searcher interface {
	Search(ctx context.Context, query string, limit int) ([]SearchResult, error)
}

var registry []Source

// Register adds a source to the global registry. Adapters call this from
// their init() so importing the package wires them up automatically.
func Register(s Source) { registry = append(registry, s) }

// For returns the first registered source that matches the URL, or nil.
func For(url string) Source {
	for _, s := range registry {
		if s.Matches(url) {
			return s
		}
	}
	return nil
}

// All returns every registered source (used for --list-sources / help).
func All() []Source { return registry }
