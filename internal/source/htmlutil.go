package source

import (
	"net/url"
	"strings"

	"github.com/PuerkitoBio/goquery"
)

// lazyAttrs are the attributes scanlation sites use to defer real image URLs.
var lazyAttrs = []string{
	"src", "data-src", "data-lazy-src", "data-original", "data-cfsrc",
	"data-url", "data-image", "nitro-lazy-src",
}

// imgSrc extracts an image URL from an <img>, accounting for the many
// lazy-loading and responsive attributes scanlation sites use. It returns ""
// if none look usable.
func imgSrc(s *goquery.Selection) string {
	for _, attr := range lazyAttrs {
		if v, ok := s.Attr(attr); ok {
			v = strings.TrimSpace(v)
			// Skip inline placeholders and 1px spacers.
			if v != "" && !strings.HasPrefix(v, "data:") {
				return v
			}
		}
	}
	// Fall back to the highest-resolution srcset candidate.
	for _, attr := range []string{"srcset", "data-srcset"} {
		if v, ok := s.Attr(attr); ok {
			if u := lastSrcset(v); u != "" {
				return u
			}
		}
	}
	return ""
}

// lastSrcset returns the last (typically highest-resolution) URL in a srcset.
func lastSrcset(v string) string {
	parts := strings.Split(v, ",")
	for i := len(parts) - 1; i >= 0; i-- {
		fields := strings.Fields(strings.TrimSpace(parts[i]))
		if len(fields) > 0 && !strings.HasPrefix(fields[0], "data:") {
			return fields[0]
		}
	}
	return ""
}

// abs resolves href against base; if href is already absolute it is returned
// unchanged.
func abs(base, href string) string {
	b, err := url.Parse(base)
	if err != nil {
		return href
	}
	r, err := url.Parse(strings.TrimSpace(href))
	if err != nil {
		return href
	}
	return b.ResolveReference(r).String()
}

// origin returns the scheme://host of a URL, useful as a Referer.
func origin(u string) string {
	p, err := url.Parse(u)
	if err != nil || p.Host == "" {
		return u
	}
	return p.Scheme + "://" + p.Host
}
