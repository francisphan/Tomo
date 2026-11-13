// Package arcs reads the arc guide (site/arcs.json) and says which story arc
// and match a chapter belongs to, so Tomo can write that into the chapters
// themselves and readers show it without the guide open.
package arcs

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"strings"
	"time"
)

// DefaultSource is the published arc guide's data.
const DefaultSource = "https://francisphan.github.io/Tomo/arcs.json"

// Series is one series in the guide.
type Series struct {
	ID     string `json:"id"`
	Title  string `json:"title"`
	Folder string `json:"folder,omitempty"` // library folder name, when it differs from Title
	Status string `json:"status"`
	Latest *int   `json:"latest,omitempty"`
	Source string `json:"source"`
	Note   string `json:"note,omitempty"`
	Arcs   []Arc  `json:"arcs"`
}

// Arc is a story arc: chapters Start to End, or still running when End is nil.
type Arc struct {
	Part    string   `json:"part,omitempty"`
	Name    string   `json:"name"`
	Start   float64  `json:"start"`
	End     *float64 `json:"end"`
	Summary string   `json:"summary,omitempty"`
	Matches []Match  `json:"matches,omitempty"`
}

// Match is a game inside an arc, for sports series.
type Match struct {
	Name  string  `json:"name"`
	Start float64 `json:"start"`
	End   float64 `json:"end"`
}

// Load reads the guide from a file path or an http(s) URL.
func Load(source string) ([]Series, error) {
	var r io.ReadCloser
	if strings.HasPrefix(source, "http://") || strings.HasPrefix(source, "https://") {
		c := &http.Client{Timeout: 30 * time.Second}
		resp, err := c.Get(source)
		if err != nil {
			return nil, err
		}
		if resp.StatusCode != http.StatusOK {
			resp.Body.Close()
			return nil, fmt.Errorf("fetching %s: %s", source, resp.Status)
		}
		r = resp.Body
	} else {
		f, err := os.Open(source)
		if err != nil {
			return nil, err
		}
		r = f
	}
	defer r.Close()
	var all []Series
	if err := json.NewDecoder(r).Decode(&all); err != nil {
		return nil, fmt.Errorf("reading arc guide %s: %w", source, err)
	}
	return all, nil
}

// Find returns the series whose library folder is folder: its Folder if set,
// otherwise its Title, compared without case.
func Find(all []Series, folder string) (Series, bool) {
	for _, s := range all {
		name := s.Folder
		if name == "" {
			name = s.Title
		}
		if strings.EqualFold(name, folder) {
			return s, true
		}
	}
	return Series{}, false
}

// covers reports whether chapter n falls in start..end. Decimal chapters
// (extras like 125.5) belong with the whole chapter before them, so the range
// runs up to, but not including, end+1. A nil end is still running.
func covers(n, start float64, end *float64) bool {
	return n >= start && (end == nil || n < *end+1)
}

// ArcOf returns the arc chapter n belongs to, if any.
func (s Series) ArcOf(n float64) (Arc, bool) {
	for _, a := range s.Arcs {
		if covers(n, a.Start, a.End) {
			return a, true
		}
	}
	return Arc{}, false
}

// MatchOf returns the match chapter n is part of, if any.
func (a Arc) MatchOf(n float64) (Match, bool) {
	for _, m := range a.Matches {
		end := m.End
		if covers(n, m.Start, &end) {
			return m, true
		}
	}
	return Match{}, false
}

// ReadingList is the name of the reading list for an arc, prefixed with the
// series so lists from different series never collide.
func (s Series) ReadingList(a Arc) string {
	return s.Title + " · " + a.Name
}

// Guide renders the arc guide as plain text for a series description: each
// arc with its chapter range and summary, matches indented under it, and saga
// headings where the guide groups arcs into parts.
func (s Series) Guide() string {
	var b strings.Builder
	b.WriteString("Arc guide\n")
	part := ""
	for _, a := range s.Arcs {
		if a.Part != "" && a.Part != part {
			part = a.Part
			fmt.Fprintf(&b, "\n%s\n", strings.ToUpper(part))
		}
		fmt.Fprintf(&b, "\n%s · %s\n", chapterRange(a.Start, a.End), a.Name)
		if a.Summary != "" {
			fmt.Fprintf(&b, "%s\n", a.Summary)
		}
		for _, m := range a.Matches {
			end := m.End
			fmt.Fprintf(&b, "  • %s · %s\n", chapterRange(m.Start, &end), m.Name)
		}
	}
	if s.Note != "" {
		fmt.Fprintf(&b, "\n%s\n", s.Note)
	}
	return strings.TrimRight(b.String(), "\n")
}

func chapterRange(start float64, end *float64) string {
	switch {
	case end == nil:
		return fmt.Sprintf("Ch. %g–", start)
	case *end == start:
		return fmt.Sprintf("Ch. %g", start)
	default:
		return fmt.Sprintf("Ch. %g–%g", start, *end)
	}
}
