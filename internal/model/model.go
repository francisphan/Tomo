// Package model holds the core data types shared across Tomo.
package model

// Manga is a series with its chapter list.
type Manga struct {
	Title    string
	URL      string
	Source   string
	Chapters []Chapter
}

// Chapter is a single chapter of a series. ID and URL are source-specific
// handles used by a Source to resolve the chapter's page images.
type Chapter struct {
	// Number is the parsed chapter number, e.g. "1", "10.5". May be empty
	// for one-shots or unparseable chapters.
	Number string
	// Title is the optional chapter title.
	Title string
	// ID is a source-internal identifier (e.g. a MangaDex chapter UUID).
	ID string
	// URL is the chapter's web URL, when the source works off page scraping.
	URL string
	// Language is the translated language code, when known.
	Language string
	// Pages is the page count when the source reports it (used to pick the most
	// complete version when several share a chapter number). 0 if unknown.
	Pages int
}
