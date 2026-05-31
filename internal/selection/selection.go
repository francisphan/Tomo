// Package selection parses a chapter range spec like "1-10,15,40-" and filters
// a chapter list against it.
package selection

import (
	"fmt"
	"math"
	"strconv"
	"strings"

	"tomo/internal/model"
)

type span struct{ lo, hi float64 }

// Selector matches chapter numbers against a parsed spec.
type Selector struct {
	spans []span
	all   bool
}

// Parse builds a Selector from a spec. An empty spec selects every chapter.
// Tokens are comma-separated and may be:
//
//	N        a single chapter            (e.g. "12")
//	A-B      an inclusive range          (e.g. "1-10")
//	A-       from A to the end           (e.g. "100-")
//	-B       from the start to B         (e.g. "-5")
func Parse(spec string) (*Selector, error) {
	spec = strings.TrimSpace(spec)
	if spec == "" {
		return &Selector{all: true}, nil
	}
	s := &Selector{}
	for _, tok := range strings.Split(spec, ",") {
		tok = strings.TrimSpace(tok)
		if tok == "" {
			continue
		}
		if i := strings.IndexByte(tok, '-'); i >= 0 {
			loStr, hiStr := strings.TrimSpace(tok[:i]), strings.TrimSpace(tok[i+1:])
			lo, hi := math.Inf(-1), math.Inf(1)
			var err error
			if loStr != "" {
				if lo, err = strconv.ParseFloat(loStr, 64); err != nil {
					return nil, fmt.Errorf("bad range start %q", loStr)
				}
			}
			if hiStr != "" {
				if hi, err = strconv.ParseFloat(hiStr, 64); err != nil {
					return nil, fmt.Errorf("bad range end %q", hiStr)
				}
			}
			s.spans = append(s.spans, span{lo, hi})
		} else {
			v, err := strconv.ParseFloat(tok, 64)
			if err != nil {
				return nil, fmt.Errorf("bad chapter %q", tok)
			}
			s.spans = append(s.spans, span{v, v})
		}
	}
	return s, nil
}

// Filter returns the chapters that match the selector, preserving order. When
// the spec is empty every chapter is returned. Chapters whose number cannot be
// parsed as a float are included only when the spec is empty.
func (s *Selector) Filter(chs []model.Chapter) []model.Chapter {
	if s.all {
		return chs
	}
	var out []model.Chapter
	for _, c := range chs {
		n, err := strconv.ParseFloat(strings.TrimSpace(c.Number), 64)
		if err != nil {
			continue
		}
		for _, sp := range s.spans {
			if n >= sp.lo && n <= sp.hi {
				out = append(out, c)
				break
			}
		}
	}
	return out
}
