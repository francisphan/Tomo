package main

import (
	"bytes"
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"

	"tomo/internal/arcs"
	"tomo/internal/cbz"
)

// chapterFileName splits a Tomo chapter file name into its number and the
// chapter title the source gave it, if any.
var chapterFileName = regexp.MustCompile(`(?i) - chapter ([0-9]+(?:\.[0-9]+)?)(?: - (.*))?\.cbz$`)

// runArcs implements `tomo arcs`: it writes the arc guide into a series'
// chapters so a reader shows it without the guide open.
//
//   - Each chapter's title becomes "<arc> · <match> · <source title>" (the
//     parts it has), so the chapter list reads like the guide.
//   - Each chapter's story arc is set to "<series> · <arc>" with its position
//     in the arc, which Komga turns into one reading list per arc.
//   - The series gets a series.json whose description is the whole guide.
//
// Folders the guide doesn't cover are skipped, so it can run over a whole
// library. Only chapters whose info actually changes are rewritten.
func runArcs(argv []string) error {
	fs := flag.NewFlagSet("arcs", flag.ContinueOnError)
	data := fs.String("data", arcs.DefaultSource, "arc guide data (site/arcs.json): a file path or URL")
	if err := fs.Parse(argv); err != nil {
		return err
	}
	if fs.NArg() == 0 {
		return fmt.Errorf("usage: tomo arcs [-data path|url] <series-dir>...")
	}
	guide, err := arcs.Load(*data)
	if err != nil {
		return err
	}
	failed := 0
	for _, dir := range fs.Args() {
		folder := filepath.Base(filepath.Clean(dir))
		series, ok := arcs.Find(guide, folder)
		if !ok {
			continue
		}
		changed, same, outside, errs := applyArcs(dir, series)
		failed += errs
		desc, err := writeSeriesJSON(dir, folder, series)
		if err != nil {
			failed++
			fmt.Fprintf(os.Stderr, "failed: %s: series.json: %v\n", dir, err)
		}
		note := ""
		if desc {
			note = ", description updated"
		}
		fmt.Printf("%s: %d chapters updated, %d unchanged, %d outside the guide%s\n", folder, changed, same, outside, note)
	}
	if failed > 0 {
		return fmt.Errorf("%d update(s) failed", failed)
	}
	return nil
}

type chapterFile struct {
	path   string
	number float64
	title  string
}

func applyArcs(dir string, s arcs.Series) (changed, same, outside, failed int) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		fmt.Fprintf(os.Stderr, "failed: %s: %v\n", dir, err)
		return 0, 0, 0, 1
	}
	var chapters []chapterFile
	for _, e := range entries {
		m := chapterFileName.FindStringSubmatch(e.Name())
		if e.IsDir() || m == nil {
			continue
		}
		n, err := strconv.ParseFloat(m[1], 64)
		if err != nil {
			continue
		}
		chapters = append(chapters, chapterFile{filepath.Join(dir, e.Name()), n, strings.TrimSpace(m[2])})
	}
	sort.Slice(chapters, func(i, j int) bool { return chapters[i].number < chapters[j].number })

	position := map[string]int{} // chapters seen so far in each arc
	for _, c := range chapters {
		title, list, number := chapterArcInfo(s, c, position)
		if list == "" {
			outside++
		}
		ok, err := cbz.SetComicInfo(c.path,
			cbz.Field{Name: "Title", Value: title},
			cbz.Field{Name: "StoryArc", Value: list},
			cbz.Field{Name: "StoryArcNumber", Value: number},
		)
		switch {
		case err != nil:
			failed++
			fmt.Fprintf(os.Stderr, "failed: %s: %v\n", c.path, err)
		case ok:
			changed++
		default:
			same++
		}
	}
	return changed, same, outside, failed
}

// chapterArcInfo works out a chapter's title, reading-list name and position
// in that list. Chapters outside every arc keep their source title and get no
// reading list.
func chapterArcInfo(s arcs.Series, c chapterFile, position map[string]int) (title, list, number string) {
	a, ok := s.ArcOf(c.number)
	if !ok {
		return c.title, "", ""
	}
	parts := []string{a.Name}
	if m, ok := a.MatchOf(c.number); ok {
		parts = append(parts, m.Name)
	}
	if c.title != "" {
		parts = append(parts, c.title)
	}
	list = s.ReadingList(a)
	position[list]++
	return strings.Join(parts, " · "), list, strconv.Itoa(position[list])
}

// writeSeriesJSON writes the guide as the series description, in the Mylar
// series.json format Komga imports. It reports whether the file changed.
func writeSeriesJSON(dir, folder string, s arcs.Series) (bool, error) {
	status := "Continuing"
	if s.Status == "complete" {
		status = "Ended"
	}
	doc := map[string]any{
		"version": "1.0.2",
		"metadata": map[string]any{
			"type":                  "comicSeries",
			"publisher":             "",
			"imprint":               nil,
			"name":                  folder,
			"comicid":               0,
			"year":                  0,
			"description_text":      s.Guide(),
			"description_formatted": nil,
			"volume":                nil,
			"booktype":              "Print",
			"age_rating":            nil,
			"collects":              nil,
			"ComicImage":            "",
			"total_issues":          0,
			"publication_run":       "",
			"status":                status,
		},
	}
	body, err := json.MarshalIndent(doc, "", "  ")
	if err != nil {
		return false, err
	}
	body = append(body, '\n')
	path := filepath.Join(dir, "series.json")
	if old, err := os.ReadFile(path); err == nil && bytes.Equal(old, body) {
		return false, nil
	}
	tmp, err := os.CreateTemp(dir, ".tomo-*.part")
	if err != nil {
		return false, err
	}
	if _, err := tmp.Write(body); err != nil {
		tmp.Close()
		os.Remove(tmp.Name())
		return false, err
	}
	if err := tmp.Close(); err != nil {
		os.Remove(tmp.Name())
		return false, err
	}
	if err := os.Chmod(tmp.Name(), 0o644); err != nil {
		os.Remove(tmp.Name())
		return false, err
	}
	return true, os.Rename(tmp.Name(), path)
}
