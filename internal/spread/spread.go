// Package spread keeps two-page spreads together in dual-page (landscape)
// reading.
//
// Many sources publish a double-page spread as two separate portrait images.
// A reader in double-page mode pairs pages in order — (1,2), (3,4), ... — so
// whether a spread's halves share a screen depends on where they land. When a
// spread starts on the second slot of a pair, its halves end up on different
// screens. We fix that by inserting a blank page just before such a spread,
// which shifts it onto a pair boundary.
//
// Spreads are found by looking at the seam: the art of a spread runs right up
// to the inner edge of both halves, and the two edges line up. Ordinary pages
// almost always have a plain margin at the edge, so they don't match.
//
// Landscape images (spreads already merged into one image) are shown alone by
// readers and restart the pairing, which the simulation accounts for.
package spread

import (
	"fmt"
	"image"
	"image/color"
	_ "image/gif"  // register decoder
	_ "image/jpeg" // register decoder
	"image/png"
	"math"
	"os"
	"path/filepath"

	_ "golang.org/x/image/webp" // register decoder

	"tomo/internal/download"
)

const (
	edgeSamples  = 96 // rows sampled along each vertical edge
	edgeDepth    = 3  // columns averaged at each edge
	minEdgeStd   = 14 // edge must carry art, not a blank margin (0-255 scale)
	maxSeamDiff  = 22 // mean absolute luminance difference across the seam
	minSeamCorr  = 0.80
	maxHeightGap = 0.03 // halves of a spread have (near) identical heights
)

// info is what we keep of each page: its shape and its two edge profiles.
type info struct {
	ok          bool // decoded successfully
	w, h        int
	left, right []float64 // luminance down the left/right edge
}

func (p info) wide() bool { return p.ok && p.w > p.h }
func (p info) portrait() bool {
	return p.ok && float64(p.h)/float64(p.w) >= 1.2 && float64(p.h)/float64(p.w) <= 1.9
}

// Pad returns pages with blank pages inserted where needed so every detected
// spread lands on a single screen in double-page mode. Pages are re-indexed
// sequentially. Blank pages are written next to the existing page files, so the
// caller's normal cleanup removes them. If nothing needs padding, pages is
// returned unchanged.
func Pad(pages []download.Page) ([]download.Page, error) {
	if len(pages) < 2 {
		return pages, nil
	}
	infos := make([]info, len(pages))
	for i, p := range pages {
		infos[i] = load(p.Path)
	}
	at := padPositions(infos)
	if len(at) == 0 {
		return pages, nil
	}

	out := make([]download.Page, 0, len(pages)+len(at))
	next := 0
	for i, p := range pages {
		if next < len(at) && at[next] == i {
			next++
			blank, err := writeBlank(filepath.Dir(p.Path), i, infos[i])
			if err != nil {
				return nil, err
			}
			out = append(out, blank)
		}
		out = append(out, p)
	}
	for i := range out {
		out[i].Index = i + 1
	}
	return out, nil
}

// padPositions simulates double-page pairing and returns the indices of pages
// that need a blank page inserted before them.
func padPositions(infos []info) []int {
	var at []int
	slot := 0 // 0: next page starts a pair; 1: next page completes one
	for i := 0; i < len(infos); i++ {
		if infos[i].wide() {
			slot = 0 // shown alone; pairing restarts after it
			continue
		}
		if i+1 < len(infos) && isSpread(infos[i], infos[i+1]) {
			if slot == 1 {
				at = append(at, i)
			}
			i++ // both halves consumed as one pair
			slot = 0
			continue
		}
		slot ^= 1
	}
	return at
}

// isSpread reports whether a and b (consecutive pages) look like the two halves
// of one spread. The pair slot is the same regardless of reading direction, so
// both seams are checked: right-to-left (a's left edge meets b's right edge)
// and left-to-right (a's right edge meets b's left edge).
func isSpread(a, b info) bool {
	if !a.portrait() || !b.portrait() {
		return false
	}
	if math.Abs(float64(a.h)-float64(b.h))/float64(a.h) > maxHeightGap {
		return false
	}
	return seamMatches(a.left, b.right) || seamMatches(a.right, b.left)
}

func seamMatches(x, y []float64) bool {
	mx, sx := meanStd(x)
	my, sy := meanStd(y)
	if sx < minEdgeStd || sy < minEdgeStd {
		return false
	}
	var diff, cov float64
	for i := range x {
		diff += math.Abs(x[i] - y[i])
		cov += (x[i] - mx) * (y[i] - my)
	}
	diff /= float64(len(x))
	corr := cov / float64(len(x)) / (sx * sy)
	return diff <= maxSeamDiff && corr >= minSeamCorr
}

func meanStd(v []float64) (mean, std float64) {
	for _, x := range v {
		mean += x
	}
	mean /= float64(len(v))
	for _, x := range v {
		std += (x - mean) * (x - mean)
	}
	return mean, math.Sqrt(std / float64(len(v)))
}

// load decodes an image and extracts its edge profiles. Undecodable formats
// (e.g. AVIF) return a zero info, which is never treated as part of a spread.
func load(path string) info {
	f, err := os.Open(path)
	if err != nil {
		return info{}
	}
	defer f.Close()
	img, _, err := image.Decode(f)
	if err != nil {
		return info{}
	}
	return profile(img)
}

func profile(img image.Image) info {
	b := img.Bounds()
	w, h := b.Dx(), b.Dy()
	if w < edgeDepth*2 || h < edgeSamples {
		return info{}
	}
	edge := func(x0 int) []float64 {
		out := make([]float64, edgeSamples)
		for s := 0; s < edgeSamples; s++ {
			y0 := b.Min.Y + s*h/edgeSamples
			y1 := b.Min.Y + (s+1)*h/edgeSamples
			var sum float64
			var n int
			for y := y0; y < y1; y++ {
				for x := x0; x < x0+edgeDepth; x++ {
					sum += luma(img.At(x, y))
					n++
				}
			}
			out[s] = sum / float64(n)
		}
		return out
	}
	return info{
		ok:    true,
		w:     w,
		h:     h,
		left:  edge(b.Min.X),
		right: edge(b.Max.X - edgeDepth),
	}
}

func luma(c color.Color) float64 {
	r, g, b, _ := c.RGBA()
	return (0.299*float64(r) + 0.587*float64(g) + 0.114*float64(b)) / 257
}

// writeBlank writes a white page sized like the page it precedes, so the
// reader scales it the same way.
func writeBlank(dir string, before int, like info) (download.Page, error) {
	w, h := like.w, like.h
	if !like.ok {
		w, h = 800, 1200
	}
	img := image.NewGray(image.Rect(0, 0, w, h))
	for i := range img.Pix {
		img.Pix[i] = 0xff
	}
	f, err := os.Create(filepath.Join(dir, fmt.Sprintf("blank-%04d.png", before)))
	if err != nil {
		return download.Page{}, err
	}
	if err := png.Encode(f, img); err != nil {
		f.Close()
		return download.Page{}, err
	}
	if err := f.Close(); err != nil {
		return download.Page{}, err
	}
	return download.Page{Ext: ".png", Path: f.Name()}, nil
}
