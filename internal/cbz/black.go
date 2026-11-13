package cbz

import (
	"archive/zip"
	"bytes"
	"encoding/xml"
	"fmt"
	"image"
	"image/color"
	"image/jpeg"
	"image/png"
	"io"
	"os"
	"path/filepath"
	"runtime"
	"sort"
	"strings"
	"sync"
	"time"
)

// TrueBlackMark is written into ComicInfo.xml's Notes once a chapter has been
// converted, so TrueBlack never darkens the same chapter twice.
const TrueBlackMark = "tomo:true-black"

const (
	// blackPoint and below becomes 0: OLED pixels fully off. Scans usually put
	// their ink around 30 rather than 0, which glows faintly on an OLED screen.
	blackPoint = 40
	// shadowEnd is where the curve rejoins the original. Greys between the
	// black point and here are stretched down to meet black; everything
	// lighter (screentone, paper) is left exactly as it was.
	shadowEnd = 112
)

// trueBlackLUT maps an 8-bit grey level to its true-black value.
var trueBlackLUT = func() (lut [256]uint8) {
	for x := range lut {
		switch {
		case x <= blackPoint:
			lut[x] = 0
		case x < shadowEnd:
			lut[x] = uint8((x - blackPoint) * shadowEnd / (shadowEnd - blackPoint))
		default:
			lut[x] = uint8(x)
		}
	}
	return lut
}()

// TrueBlack rewrites an existing CBZ so every black-and-white page is
// greyscale with true blacks, for reading on an OLED screen. Colour pages
// (see isColour) are kept exactly as they are. JPEG pages stay JPEG, including
// JPEGs a source stored under a .png name; every other format is stored as
// PNG. It reports false and leaves the file alone if the chapter was already
// converted. Non-image entries are copied as-is, and the
// rewrite goes through a temp file so a failure never damages the original.
func TrueBlack(path string) (bool, error) {
	zr, err := zip.OpenReader(path)
	if err != nil {
		return false, err
	}
	defer zr.Close()

	var info []byte
	for _, f := range zr.File {
		if f.Name == "ComicInfo.xml" {
			if info, err = readEntry(f); err != nil {
				return false, err
			}
		}
	}
	if bytes.Contains(info, []byte(TrueBlackMark)) {
		return false, nil
	}

	// Decoding and re-encoding is the slow part, so pages convert in parallel
	// and are then written back in their original order.
	type page struct {
		name string
		data []byte
		err  error
	}
	pages := make([]page, len(zr.File))
	var wg sync.WaitGroup
	sem := make(chan struct{}, runtime.NumCPU())
	for i, f := range zr.File {
		if !isImage(f.Name) {
			continue
		}
		wg.Add(1)
		sem <- struct{}{}
		go func(i int, f *zip.File) {
			defer wg.Done()
			defer func() { <-sem }()
			name, data, err := convertPage(f)
			pages[i] = page{name, data, err}
		}(i, f)
	}
	wg.Wait()

	return true, rewrite(path, zr, func(zw *zip.Writer) error {
		for i, f := range zr.File {
			switch {
			case f.Name == "ComicInfo.xml":
				continue // rewritten with the mark below
			case isImage(f.Name):
				p := pages[i]
				if p.err != nil {
					return fmt.Errorf("%s: %w", f.Name, p.err)
				}
				w, err := zw.CreateHeader(&zip.FileHeader{Name: p.name, Method: zip.Store, Modified: time.Now()})
				if err != nil {
					return err
				}
				if _, err := w.Write(p.data); err != nil {
					return err
				}
			default:
				if err := zw.Copy(f); err != nil {
					return err
				}
			}
		}
		w, err := zw.CreateHeader(&zip.FileHeader{Name: "ComicInfo.xml", Method: zip.Deflate, Modified: time.Now()})
		if err != nil {
			return err
		}
		_, err = w.Write(markComicInfo(info, TrueBlackMark))
		return err
	})
}

func isImage(name string) bool {
	switch strings.ToLower(filepath.Ext(name)) {
	case ".jpg", ".jpeg", ".png", ".webp", ".gif":
		return true
	}
	return false
}

func readEntry(f *zip.File) ([]byte, error) {
	rc, err := f.Open()
	if err != nil {
		return nil, err
	}
	defer rc.Close()
	return io.ReadAll(rc)
}

// convertPage decodes one page, applies the true-black curve, and re-encodes
// it, returning the entry name to store it under.
func convertPage(f *zip.File) (string, []byte, error) {
	src, err := readEntry(f)
	if err != nil {
		return "", nil, err
	}
	img, format, err := image.Decode(bytes.NewReader(src))
	if err != nil {
		return "", nil, err
	}
	if isColour(img) {
		return f.Name, src, nil
	}
	gray := toTrueBlack(img)
	var buf bytes.Buffer
	name := f.Name
	// Follow the data, not the name: re-saving a JPEG scan as PNG doubles it.
	if format == "jpeg" {
		err = jpeg.Encode(&buf, gray, &jpeg.Options{Quality: jpegQuality(src)})
	} else {
		if ext := filepath.Ext(name); !strings.EqualFold(ext, ".png") {
			name = strings.TrimSuffix(name, ext) + ".png"
		}
		err = png.Encode(&buf, gray)
	}
	return name, buf.Bytes(), err
}

const (
	// A pixel counts as coloured when its chroma (|Cb-128| + |Cr-128|) is
	// above colourChroma: well clear of JPEG noise and slightly tinted paper.
	colourChroma = 24
	// A page counts as a colour page when more than colourShare of its pixels
	// are coloured, so a grey page with a small coloured logo still converts.
	colourShare = 0.10
)

// isColour reports whether img is a colour page that TrueBlack should leave
// alone. It samples every other pixel in each direction, which is plenty to
// tell a colour page from a black-and-white one.
func isColour(img image.Image) bool {
	b := img.Bounds()
	coloured, total := 0, 0
	count := func(cb, cr uint8) {
		total++
		if abs(int(cb)-128)+abs(int(cr)-128) > colourChroma {
			coloured++
		}
	}
	switch src := img.(type) {
	case *image.Gray, *image.Gray16:
		return false
	case *image.YCbCr:
		for y := b.Min.Y; y < b.Max.Y; y += 2 {
			for x := b.Min.X; x < b.Max.X; x += 2 {
				i := src.COffset(x, y)
				count(src.Cb[i], src.Cr[i])
			}
		}
	default:
		for y := b.Min.Y; y < b.Max.Y; y += 2 {
			for x := b.Min.X; x < b.Max.X; x += 2 {
				r, g, bl, _ := img.At(x, y).RGBA()
				_, cb, cr := color.RGBToYCbCr(uint8(r>>8), uint8(g>>8), uint8(bl>>8))
				count(cb, cr)
			}
		}
	}
	return total > 0 && float64(coloured) > colourShare*float64(total)
}

// stdLuminance is the JPEG standard's luminance quantization table, which
// libjpeg and Go both scale by quality.
var stdLuminance = [64]int{
	16, 11, 10, 16, 24, 40, 51, 61, 12, 12, 14, 19, 26, 58, 60, 55,
	14, 13, 16, 24, 40, 57, 69, 56, 14, 17, 22, 29, 51, 87, 80, 62,
	18, 22, 37, 56, 68, 109, 103, 77, 24, 35, 55, 64, 81, 104, 113, 92,
	49, 64, 78, 87, 103, 121, 120, 101, 72, 92, 95, 98, 112, 100, 103, 99,
}

// jpegQuality estimates the quality a JPEG was saved at from its luminance
// quantization table, so a page is re-saved at the quality it came with:
// higher only grows the file, lower throws away detail. It falls back to 85
// when the table can't be read.
func jpegQuality(data []byte) int {
	table, ok := lumaTable(data)
	if !ok {
		return 85
	}
	got := table[:]
	sort.Ints(got)
	best, bestErr := 85, -1
	for q := 1; q <= 100; q++ {
		scale := 200 - 2*q
		if q < 50 {
			scale = 5000 / q
		}
		want := make([]int, 64)
		for i, v := range stdLuminance {
			want[i] = clamp((v*scale+50)/100, 1, 255)
		}
		sort.Ints(want)
		e := 0
		for i := range want {
			e += abs(want[i] - got[i])
		}
		if bestErr < 0 || e < bestErr {
			best, bestErr = q, e
		}
	}
	return clamp(best, 50, 95)
}

// lumaTable returns the 8-bit quantization table with id 0 (luminance) from a
// JPEG's DQT segments.
func lumaTable(data []byte) ([64]int, bool) {
	var t [64]int
	for i := 2; i+4 <= len(data); {
		if data[i] != 0xFF {
			return t, false
		}
		marker := data[i+1]
		n := int(data[i+2])<<8 | int(data[i+3])
		if marker == 0xDA || i+2+n > len(data) { // start of scan: no table found
			return t, false
		}
		if marker == 0xDB {
			seg := data[i+4 : i+2+n]
			for len(seg) >= 65 {
				precision, id := seg[0]>>4, seg[0]&0x0F
				if precision != 0 {
					break
				}
				if id == 0 {
					for k := range t {
						t[k] = int(seg[1+k])
					}
					return t, true
				}
				seg = seg[65:]
			}
		}
		i += 2 + n
	}
	return t, false
}

func clamp(x, lo, hi int) int {
	if x < lo {
		return lo
	}
	if x > hi {
		return hi
	}
	return x
}

func abs(x int) int {
	if x < 0 {
		return -x
	}
	return x
}

// toTrueBlack converts img to greyscale through trueBlackLUT. JPEG scans
// decode to YCbCr, whose Y plane already is the luma, so that case reads it
// directly instead of converting pixel by pixel.
func toTrueBlack(img image.Image) *image.Gray {
	b := img.Bounds()
	out := image.NewGray(image.Rect(0, 0, b.Dx(), b.Dy()))
	for y := 0; y < b.Dy(); y++ {
		row := out.Pix[y*out.Stride : y*out.Stride+b.Dx()]
		switch src := img.(type) {
		case *image.YCbCr:
			for x := range row {
				row[x] = trueBlackLUT[src.Y[src.YOffset(b.Min.X+x, b.Min.Y+y)]]
			}
		case *image.Gray:
			for x := range row {
				row[x] = trueBlackLUT[src.Pix[src.PixOffset(b.Min.X+x, b.Min.Y+y)]]
			}
		default:
			for x := range row {
				row[x] = trueBlackLUT[color.GrayModel.Convert(img.At(b.Min.X+x, b.Min.Y+y)).(color.Gray).Y]
			}
		}
	}
	return out
}

// markComicInfo adds mark to ComicInfo.xml's Notes, editing the text in place
// so any fields Tomo doesn't know about survive.
func markComicInfo(x []byte, mark string) []byte {
	if i := bytes.Index(x, []byte("</Notes>")); i >= 0 {
		return insertAt(x, i, "; "+mark)
	}
	if i := bytes.LastIndex(x, []byte("</ComicInfo>")); i >= 0 {
		return insertAt(x, i, "  <Notes>"+mark+"</Notes>\n")
	}
	return []byte(xml.Header + "<ComicInfo>\n  <Notes>" + mark + "</Notes>\n</ComicInfo>")
}

func insertAt(x []byte, i int, s string) []byte {
	out := make([]byte, 0, len(x)+len(s))
	out = append(out, x[:i]...)
	out = append(out, s...)
	return append(out, x[i:]...)
}

// rewrite builds a replacement for the archive at path in a temp file next to
// it, then swaps it in, keeping the original's file mode. zr (the open
// original) is closed before the swap, because Windows can't rename over a
// file that still has an open handle.
func rewrite(path string, zr *zip.ReadCloser, build func(*zip.Writer) error) error {
	out, err := os.CreateTemp(filepath.Dir(path), ".tomo-*.part")
	if err != nil {
		return err
	}
	tmp := out.Name()
	zw := zip.NewWriter(out)
	if err := build(zw); err != nil {
		zw.Close()
		out.Close()
		os.Remove(tmp)
		return err
	}
	if err := zw.Close(); err != nil {
		out.Close()
		os.Remove(tmp)
		return err
	}
	if err := out.Close(); err != nil {
		os.Remove(tmp)
		return err
	}
	if fi, err := os.Stat(path); err == nil {
		if err := os.Chmod(tmp, fi.Mode().Perm()); err != nil {
			os.Remove(tmp)
			return err
		}
	}
	zr.Close()
	if err := os.Rename(tmp, path); err != nil {
		os.Remove(tmp)
		return err
	}
	return nil
}
