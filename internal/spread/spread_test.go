package spread

import (
	"image"
	"image/color"
	"image/draw"
	"image/png"
	"math/rand"
	"os"
	"path/filepath"
	"testing"

	"tomo/internal/download"
)

const pw, ph = 200, 300

// art fills r with smooth, varied "artwork" that runs to the edges.
func art(r image.Rectangle, seed int64) *image.Gray {
	rng := rand.New(rand.NewSource(seed))
	img := image.NewGray(r)
	bands := make([]uint8, r.Dy()/10+1)
	for i := range bands {
		bands[i] = uint8(rng.Intn(256))
	}
	for y := r.Min.Y; y < r.Max.Y; y++ {
		for x := r.Min.X; x < r.Max.X; x++ {
			v := int(bands[(y-r.Min.Y)/10]) + (x-r.Min.X)/20
			img.SetGray(x, y, color.Gray{uint8(v % 256)})
		}
	}
	return img
}

// normal is an ordinary page: art inside a white margin.
func normal(seed int64) image.Image {
	img := image.NewGray(image.Rect(0, 0, pw, ph))
	draw.Draw(img, img.Bounds(), image.White, image.Point{}, draw.Src)
	inner := image.Rect(20, 20, pw-20, ph-20)
	draw.Draw(img, inner, art(inner, seed), inner.Min, draw.Src)
	return img
}

// spreadHalves returns the two pages of a right-to-left spread, in reading
// order: the right half first, then the left half.
func spreadHalves(seed int64) (image.Image, image.Image) {
	full := art(image.Rect(0, 0, pw*2, ph), seed)
	left := image.NewGray(image.Rect(0, 0, pw, ph))
	right := image.NewGray(image.Rect(0, 0, pw, ph))
	draw.Draw(left, left.Bounds(), full, image.Point{}, draw.Src)
	draw.Draw(right, right.Bounds(), full, image.Point{X: pw}, draw.Src)
	return right, left
}

func wide(seed int64) image.Image { return art(image.Rect(0, 0, pw*2, ph), seed) }

func profiles(imgs ...image.Image) []info {
	out := make([]info, len(imgs))
	for i, img := range imgs {
		out[i] = profile(img)
	}
	return out
}

func equal(a, b []int) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

func TestDetectsSpreadAndIgnoresNormalPages(t *testing.T) {
	r, l := spreadHalves(1)
	if !isSpread(profile(r), profile(l)) {
		t.Error("spread halves not detected")
	}
	if isSpread(profile(normal(2)), profile(normal(3))) {
		t.Error("two normal pages detected as a spread")
	}
	if isSpread(profile(normal(2)), profile(r)) {
		t.Error("normal page + spread half detected as a spread")
	}
}

func TestPadPositions(t *testing.T) {
	r, l := spreadHalves(9)
	cases := []struct {
		name string
		imgs []image.Image
		want []int
	}{
		{"spread already aligned", []image.Image{normal(1), normal(2), r, l, normal(3)}, nil},
		{"spread misaligned", []image.Image{normal(1), r, l, normal(2)}, []int{1}},
		{"wide page resets pairing", []image.Image{normal(1), wide(5), r, l}, nil},
		{"misaligned after wide page", []image.Image{wide(5), normal(1), r, l}, []int{2}},
		{"no spreads", []image.Image{normal(1), normal(2), normal(3)}, nil},
	}
	for _, c := range cases {
		if got := padPositions(profiles(c.imgs...)); !equal(got, c.want) {
			t.Errorf("%s: got %v, want %v", c.name, got, c.want)
		}
	}
}

func TestPadInsertsBlankAndReindexes(t *testing.T) {
	dir := t.TempDir()
	r, l := spreadHalves(4)
	var pages []download.Page
	for i, img := range []image.Image{normal(1), r, l, normal(2)} {
		p := filepath.Join(dir, string(rune('a'+i))+".png")
		f, err := os.Create(p)
		if err != nil {
			t.Fatal(err)
		}
		if err := png.Encode(f, img); err != nil {
			t.Fatal(err)
		}
		f.Close()
		pages = append(pages, download.Page{Index: i + 1, Ext: ".png", Path: p})
	}

	out, err := Pad(pages)
	if err != nil {
		t.Fatal(err)
	}
	if len(out) != 5 {
		t.Fatalf("got %d pages, want 5", len(out))
	}
	if filepath.Base(out[1].Path) != "blank-0001.png" {
		t.Errorf("page 2 = %s, want the blank", out[1].Path)
	}
	for i, p := range out {
		if p.Index != i+1 {
			t.Errorf("page %d has index %d", i, p.Index)
		}
	}
	if filepath.Dir(out[1].Path) != dir {
		t.Error("blank not written alongside the pages (would escape cleanup)")
	}
}
