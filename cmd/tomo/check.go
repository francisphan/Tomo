package main

import (
	"archive/zip"
	"flag"
	"fmt"
	"image"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"tomo/internal/cbz"
)

// runCheck implements `tomo check`: it looks a downloaded chapter over before
// it goes into the library and prints "ok: <path>" or "held: <path>: <why>".
// It exits non-zero when any chapter is held. A chapter is held when:
//
//   - a page can't be opened;
//   - a page with real content appears twice (repeated blank or logo pages
//     are normal and allowed);
//   - it has fewer than half the pages the series usually has (counting a
//     spread as two). Extras with decimal numbers (12.5) are often short and
//     are exempt. This needs -library to know what "usually" is.
func runCheck(argv []string) error {
	fset := flag.NewFlagSet("check", flag.ContinueOnError)
	library := fset.String("library", "", "library dir; page counts are compared with the same series there")
	if err := fset.Parse(argv); err != nil {
		return err
	}
	if fset.NArg() == 0 {
		return fmt.Errorf("usage: tomo check [-library dir] <dir|cbz>...")
	}
	held := 0
	for _, arg := range fset.Args() {
		err := filepath.WalkDir(arg, func(path string, d fs.DirEntry, err error) error {
			if err != nil || d.IsDir() || !strings.EqualFold(filepath.Ext(path), ".cbz") {
				return err
			}
			if problems := checkChapter(path, *library); len(problems) > 0 {
				held++
				fmt.Printf("held: %s: %s\n", path, strings.Join(problems, "; "))
			} else {
				fmt.Printf("ok: %s\n", path)
			}
			return nil
		})
		if err != nil {
			return err
		}
	}
	if held > 0 {
		return fmt.Errorf("%d chapter(s) held", held)
	}
	return nil
}

func checkChapter(path, library string) []string {
	pages, err := cbz.Pages(path)
	if err != nil {
		return []string{fmt.Sprintf("can't open the archive: %v", err)}
	}
	if len(pages) > 0 && strings.HasPrefix(pages[0].Name, "0000.") && pages[0].Blank {
		pages = pages[1:] // Tomo's padding page
	}
	if len(pages) == 0 {
		return []string{"no pages"}
	}
	var problems []string
	seen := map[[20]byte]string{}
	count := 0
	for _, p := range pages {
		if p.Err != nil {
			problems = append(problems, fmt.Sprintf("page %s won't open (%v)", p.Name, p.Err))
			continue
		}
		if first, ok := seen[p.Hash]; ok && p.Std > 40 && p.White < 0.85 {
			problems = append(problems, fmt.Sprintf("page %s repeats page %s", p.Name, first))
		}
		seen[p.Hash] = p.Name
		count++
		if p.Wide() {
			count++
		}
	}
	m := chapterFileName.FindStringSubmatch(filepath.Base(path))
	extra := m != nil && strings.Contains(m[1], ".")
	if library != "" && !extra {
		usual := usualPageCount(filepath.Join(library, filepath.Base(filepath.Dir(path))))
		if usual > 0 && count*2 < usual {
			problems = append(problems, fmt.Sprintf("only %d pages; this series usually has %d", count, usual))
		}
	}
	return problems
}

// usualPageCount is the median page count (spreads count as two, padding
// pages don't) of the series' 30 latest whole-numbered chapters in dir, or 0
// if it has none.
func usualPageCount(dir string) int {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return 0
	}
	var files []string
	for _, e := range entries {
		m := chapterFileName.FindStringSubmatch(e.Name())
		if m != nil && !strings.Contains(m[1], ".") {
			files = append(files, filepath.Join(dir, e.Name()))
		}
	}
	sort.Strings(files)
	if len(files) > 30 {
		files = files[len(files)-30:]
	}
	var counts []int
	for _, f := range files {
		if n := pageCount(f); n > 0 {
			counts = append(counts, n)
		}
	}
	if len(counts) == 0 {
		return 0
	}
	sort.Ints(counts)
	return counts[len(counts)/2]
}

// pageCount counts a chapter's pages from their headers alone, spreads as two.
func pageCount(path string) int {
	zr, err := zip.OpenReader(path)
	if err != nil {
		return 0
	}
	defer zr.Close()
	n := 0
	for _, f := range zr.File {
		ext := strings.ToLower(filepath.Ext(f.Name))
		if ext != ".jpg" && ext != ".jpeg" && ext != ".png" && ext != ".webp" && ext != ".gif" {
			continue
		}
		if strings.HasPrefix(f.Name, "0000.") {
			continue // the padding page, when there is one
		}
		n++
		rc, err := f.Open()
		if err != nil {
			continue
		}
		if cfg, _, err := image.DecodeConfig(rc); err == nil && cfg.Width > cfg.Height {
			n++
		}
		rc.Close()
	}
	return n
}
