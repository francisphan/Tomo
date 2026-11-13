package cbz

import (
	"archive/zip"
	"bytes"
	"crypto/sha1"
	"image"
	"image/color"
	"math"
	"sort"
)

// PageInfo describes one page of a chapter, for checking a download.
type PageInfo struct {
	Name          string
	Width, Height int
	Hash          [20]byte // of the stored bytes
	Err           error    // set when the page can't be decoded
	Blank         bool     // all black (Tomo's padding page)
	Std           float64  // spread of grey levels: low for blank or flat pages
	White         float64  // share of near-white pixels
}

// Wide reports whether the page is a spread stored as one image.
func (p PageInfo) Wide() bool { return p.Width > p.Height }

// Pages decodes every page of a chapter, in reading order.
func Pages(path string) ([]PageInfo, error) {
	zr, err := zip.OpenReader(path)
	if err != nil {
		return nil, err
	}
	defer zr.Close()
	var files []*zip.File
	for _, f := range zr.File {
		if isImage(f.Name) {
			files = append(files, f)
		}
	}
	sort.Slice(files, func(i, j int) bool { return files[i].Name < files[j].Name })
	out := make([]PageInfo, 0, len(files))
	for _, f := range files {
		p := PageInfo{Name: f.Name}
		data, err := readEntry(f)
		if err != nil {
			p.Err = err
			out = append(out, p)
			continue
		}
		p.Hash = sha1.Sum(data)
		img, _, err := image.Decode(bytes.NewReader(data))
		if err != nil {
			p.Err = err
			out = append(out, p)
			continue
		}
		b := img.Bounds()
		p.Width, p.Height = b.Dx(), b.Dy()
		p.Std, p.White, p.Blank = greyStats(img)
		out = append(out, p)
	}
	return out, nil
}

// greyStats samples every fourth pixel in each direction for the spread of
// grey levels, the share of near-white pixels, and whether all are black.
func greyStats(img image.Image) (std, white float64, blank bool) {
	b := img.Bounds()
	var n, sum, sumSq, whites float64
	blank = true
	for y := b.Min.Y; y < b.Max.Y; y += 4 {
		for x := b.Min.X; x < b.Max.X; x += 4 {
			v := float64(color.GrayModel.Convert(img.At(x, y)).(color.Gray).Y)
			n++
			sum += v
			sumSq += v * v
			if v > 240 {
				whites++
			}
			if v != 0 {
				blank = false
			}
		}
	}
	if n == 0 {
		return 0, 0, false
	}
	mean := sum / n
	return math.Sqrt(math.Max(0, sumSq/n-mean*mean)), whites / n, blank
}
