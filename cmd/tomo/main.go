// Command tomo scrapes a manga/manhwa series and packs each chapter into a CBZ
// for offline reading in Tachimanga (or any Tachiyomi/Mihon-style reader).
//
// Usage:
//
//	tomo [flags] <series-url>
//
// Example:
//
//	tomo -out ~/Manga -chapters 1-20 https://mangadex.org/title/<uuid>/<slug>
package main

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io/fs"
	"math/rand"
	"os"
	"os/signal"
	"path/filepath"
	"regexp"
	"strings"
	"sync"
	"syscall"
	"time"

	"tomo/internal/cbz"
	"tomo/internal/download"
	"tomo/internal/model"
	"tomo/internal/naming"
	"tomo/internal/selection"
	"tomo/internal/source"
)

type config struct {
	out            string
	name           string
	chapters       string
	lang           string
	concurrency    int
	chapterWorkers int
	retries        int
	list           bool
	overwrite      bool
	json           bool
	noFallback     bool
	padStart       bool
	trueBlack      bool
	library        string
}

// version is overridden at build time via -ldflags "-X main.version=...".
var version = "dev"

func main() {
	if len(os.Args) > 1 && (os.Args[1] == "-version" || os.Args[1] == "--version" || os.Args[1] == "version") {
		fmt.Println("tomo", version)
		return
	}
	// Subcommands. The default (no subcommand) is download.
	if len(os.Args) > 1 && os.Args[1] == "search" {
		if err := runSearch(os.Args[2:]); err != nil {
			fmt.Fprintln(os.Stderr, "error:", err)
			os.Exit(1)
		}
		return
	}

	if len(os.Args) > 1 && os.Args[1] == "pad" {
		if err := runPad(os.Args[2:]); err != nil {
			fmt.Fprintln(os.Stderr, "error:", err)
			os.Exit(1)
		}
		return
	}

	if len(os.Args) > 1 && os.Args[1] == "unpad" {
		if err := runUnpad(os.Args[2:]); err != nil {
			fmt.Fprintln(os.Stderr, "error:", err)
			os.Exit(1)
		}
		return
	}

	if len(os.Args) > 1 && os.Args[1] == "check" {
		if err := runCheck(os.Args[2:]); err != nil {
			fmt.Fprintln(os.Stderr, "error:", err)
			os.Exit(1)
		}
		return
	}

	if len(os.Args) > 1 && os.Args[1] == "arcs" {
		if err := runArcs(os.Args[2:]); err != nil {
			fmt.Fprintln(os.Stderr, "error:", err)
			os.Exit(1)
		}
		return
	}

	if len(os.Args) > 1 && os.Args[1] == "black" {
		if err := runBlack(os.Args[2:]); err != nil {
			fmt.Fprintln(os.Stderr, "error:", err)
			os.Exit(1)
		}
		return
	}

	cfg := config{}
	flag.StringVar(&cfg.out, "out", "./manga", "output directory (a per-series subfolder is created)")
	flag.StringVar(&cfg.name, "name", "", "series name to use for the folder and file names (default: the source's title; use to keep naming stable across sources)")
	flag.StringVar(&cfg.chapters, "chapters", "", `chapters to fetch, e.g. "1-20,25,40-" (default: all)`)
	flag.StringVar(&cfg.lang, "lang", "en", "translated language code (MangaDex only)")
	flag.IntVar(&cfg.concurrency, "concurrency", 8, "parallel image downloads per chapter")
	flag.IntVar(&cfg.chapterWorkers, "chapter-workers", 2, "chapters downloaded in parallel")
	flag.IntVar(&cfg.retries, "retries", 3, "retry attempts per failed image")
	flag.BoolVar(&cfg.list, "list", false, "list chapters without downloading")
	flag.BoolVar(&cfg.overwrite, "overwrite", false, "re-download chapters whose CBZ already exists")
	flag.BoolVar(&cfg.json, "json", false, "emit machine-readable NDJSON progress (used by the desktop app)")
	flag.BoolVar(&cfg.noFallback, "no-fallback", false, "don't auto-search other sources when the chosen one has nothing downloadable")
	flag.BoolVar(&cfg.padStart, "pad-start", false, "add a blank black page at the start of each chapter so double-page spreads line up in two-page reading")
	flag.BoolVar(&cfg.trueBlack, "true-black", false, "convert black-and-white pages to greyscale with true blacks (see `tomo black`); colour pages are kept")
	flag.StringVar(&cfg.library, "library", "", "library dir to check for chapters you already have, when -out is a holding folder (skips chapters in <library>/<series>)")
	flag.Usage = usage
	flag.Parse()

	if flag.NArg() != 1 {
		usage()
		os.Exit(2)
	}
	seriesURL := flag.Arg(0)

	if err := run(cfg, seriesURL); err != nil {
		fmt.Fprintln(os.Stderr, "error:", err)
		os.Exit(1)
	}
}

func usage() {
	fmt.Fprintf(os.Stderr, "Tomo — download manga/manhwa chapters as CBZ for Tachimanga\n\n")
	fmt.Fprintf(os.Stderr, "Usage:\n")
	fmt.Fprintf(os.Stderr, "  tomo [flags] <series-url>      download a series\n")
	fmt.Fprintf(os.Stderr, "  tomo search [flags] <query>    find a series by title\n")
	fmt.Fprintf(os.Stderr, "  tomo pad <dir|cbz>...          add (or repair) the -pad-start blank page in CBZs already downloaded\n")
	fmt.Fprintf(os.Stderr, "  tomo unpad <cbz>...            remove the blank page where split spreads pair up without it\n")
	fmt.Fprintf(os.Stderr, "  tomo black <dir|cbz>...        make pages greyscale with true blacks, for OLED screens\n")
	fmt.Fprintf(os.Stderr, "  tomo arcs <series-dir>...      write the arc guide into chapter titles, reading lists and the description\n")
	fmt.Fprintf(os.Stderr, "  tomo check [-library dir] <cbz>...  look a downloaded chapter over before it goes into the library\n\n")
	fmt.Fprintf(os.Stderr, "Download flags:\n")
	flag.PrintDefaults()
	fmt.Fprintf(os.Stderr, "\nSupported sites:\n")
	for _, s := range source.All() {
		fmt.Fprintf(os.Stderr, "  - %s\n", s.Name())
	}
}

// runPad implements `tomo pad`: it gives chapters downloaded without
// -pad-start the same leading blank page, and repairs chapters an older Tomo
// padded wrongly (see cbz.Pad). Directories are searched for CBZs
// recursively; chapters that are already right are left alone, so it's safe to
// re-run.
func runPad(argv []string) error {
	return eachChapter(argv, "pad", cbz.Pad)
}

// runUnpad implements `tomo unpad`: it takes the blank page out of chapters
// whose split spreads pair up without it, and marks them so `tomo pad` leaves
// them unpadded.
func runUnpad(argv []string) error {
	return eachChapter(argv, "unpad", cbz.Unpad)
}

// eachChapter runs fix on every CBZ under argv and prints what changed.
func eachChapter(argv []string, verb string, fix func(string) (cbz.PadResult, error)) error {
	if len(argv) == 0 {
		return fmt.Errorf("usage: tomo %s <series-dir|chapter.cbz>...", verb)
	}
	labels := map[cbz.PadResult]string{cbz.PadAdded: "padded", cbz.PadResized: "resized blank page", cbz.PadRemoved: "removed blank page"}
	counts := map[cbz.PadResult]int{}
	failed := 0
	for _, arg := range argv {
		err := filepath.WalkDir(arg, func(path string, d fs.DirEntry, err error) error {
			if err != nil || d.IsDir() || !strings.EqualFold(filepath.Ext(path), ".cbz") {
				return err
			}
			res, err := fix(path)
			if err != nil {
				// One unreadable archive shouldn't stop the rest of the library.
				failed++
				fmt.Fprintf(os.Stderr, "failed: %s: %v\n", path, err)
				return nil
			}
			counts[res]++
			if l, ok := labels[res]; ok {
				fmt.Printf("%s: %s\n", l, path)
			}
			return nil
		})
		if err != nil {
			return err
		}
	}
	fmt.Printf("%d padded, %d resized, %d unpadded, %d unchanged, %d failed\n",
		counts[cbz.PadAdded], counts[cbz.PadResized], counts[cbz.PadRemoved], counts[cbz.PadUnchanged], failed)
	if failed > 0 {
		return fmt.Errorf("%d chapter(s) could not be processed", failed)
	}
	return nil
}

// runBlack implements `tomo black`: it converts downloaded chapters to
// greyscale with true blacks (see cbz.TrueBlack). Directories are searched for
// CBZs recursively; converted chapters are marked and skipped, so it's safe to
// re-run.
func runBlack(argv []string) error {
	if len(argv) == 0 {
		return fmt.Errorf("usage: tomo black <series-dir|chapter.cbz>...")
	}
	var converted, skipped, failed int
	for _, arg := range argv {
		err := filepath.WalkDir(arg, func(path string, d fs.DirEntry, err error) error {
			if err != nil || d.IsDir() || !strings.EqualFold(filepath.Ext(path), ".cbz") {
				return err
			}
			ok, err := cbz.TrueBlack(path)
			if err != nil {
				// One unreadable archive shouldn't stop the rest of the library.
				failed++
				fmt.Fprintf(os.Stderr, "failed: %s: %v\n", path, err)
				return nil
			}
			if ok {
				converted++
				fmt.Println("converted:", path)
			} else {
				skipped++
			}
			return nil
		})
		if err != nil {
			return err
		}
	}
	fmt.Printf("%d converted, %d already converted, %d failed\n", converted, skipped, failed)
	if failed > 0 {
		return fmt.Errorf("%d chapter(s) could not be converted", failed)
	}
	return nil
}

// runSearch implements `tomo search`. It queries every source that supports
// search (or just the one named with -source) concurrently and prints the hits
// with the URL to paste into a download.
func runSearch(argv []string) error {
	fs := flag.NewFlagSet("search", flag.ExitOnError)
	var srcName string
	var limit int
	var asJSON bool
	fs.StringVar(&srcName, "source", "", "limit to one source by name (e.g. mangadex)")
	fs.IntVar(&limit, "limit", 10, "max results per source")
	fs.BoolVar(&asJSON, "json", false, "emit results as JSON (used by the desktop app)")
	fs.Usage = func() {
		fmt.Fprintf(os.Stderr, "Usage:\n  tomo search [flags] <query>\n\nFlags:\n")
		fs.PrintDefaults()
	}
	if err := fs.Parse(argv); err != nil {
		return err
	}
	query := strings.TrimSpace(strings.Join(fs.Args(), " "))
	if query == "" {
		fs.Usage()
		return fmt.Errorf("a search query is required")
	}

	type namedSearcher struct {
		name string
		s    source.Searcher
	}
	var searchers []namedSearcher
	for _, s := range source.All() {
		se, ok := s.(source.Searcher)
		if !ok || (srcName != "" && s.Name() != srcName) {
			continue
		}
		searchers = append(searchers, namedSearcher{s.Name(), se})
	}
	if len(searchers) == 0 {
		if srcName != "" {
			return fmt.Errorf("source %q does not support search (or is unknown)", srcName)
		}
		return fmt.Errorf("no searchable sources registered")
	}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	type outcome struct {
		name    string
		results []source.SearchResult
		err     error
	}
	outs := make([]outcome, len(searchers))
	var wg sync.WaitGroup
	for i, ns := range searchers {
		wg.Add(1)
		go func(i int, ns namedSearcher) {
			defer wg.Done()
			res, err := ns.s.Search(ctx, query, limit)
			outs[i] = outcome{ns.name, res, err}
		}(i, ns)
	}
	wg.Wait()

	if asJSON {
		type srcOut struct {
			Source  string                `json:"source"`
			Error   string                `json:"error,omitempty"`
			Results []source.SearchResult `json:"results"`
		}
		sources := make([]srcOut, 0, len(outs))
		for _, o := range outs {
			so := srcOut{Source: o.name, Results: o.results}
			if so.Results == nil {
				so.Results = []source.SearchResult{}
			}
			if o.err != nil {
				so.Error = o.err.Error()
			}
			sources = append(sources, so)
		}
		return json.NewEncoder(os.Stdout).Encode(map[string]interface{}{
			"query": query, "sources": sources,
		})
	}

	total := 0
	for _, o := range outs {
		fmt.Printf("\n%s:\n", o.name)
		switch {
		case o.err != nil:
			fmt.Printf("  (search failed: %v)\n", o.err)
		case len(o.results) == 0:
			fmt.Printf("  (no results)\n")
		default:
			for _, r := range o.results {
				total++
				if r.Info != "" {
					fmt.Printf("  %s  [%s]\n", r.Title, r.Info)
				} else {
					fmt.Printf("  %s\n", r.Title)
				}
				fmt.Printf("      %s\n", r.URL)
			}
		}
	}
	fmt.Printf("\n%d result(s). Download with:  tomo <url>\n", total)
	return nil
}

func run(cfg config, seriesURL string) error {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	rep := newReporter(cfg.json)

	src := source.For(seriesURL)
	if src == nil {
		return fmt.Errorf("no source matches %q\nrun `tomo` with no args to see supported sites", seriesURL)
	}
	// Pass the language preference to MangaDex.
	if md, ok := src.(*source.MangaDex); ok {
		md.Lang = cfg.lang
	}

	rep.fetching(src.Name())
	manga, err := src.Series(ctx, seriesURL)
	if err != nil {
		return err
	}

	// Surface chapters the source can't download (e.g. licensed MangaDex
	// chapters that live on an external reader), then — when nothing at all is
	// downloadable here — look for the same series on another source.
	if manga.Excluded > 0 {
		reason := manga.ExcludedReason
		if reason != "" {
			reason = " (" + reason + ")"
		}
		rep.warn(fmt.Sprintf("%d chapter(s) on %s can't be downloaded%s",
			manga.Excluded, src.Name(), reason))
	}
	if len(manga.Chapters) == 0 && !cfg.noFallback {
		if fbSrc, fbManga := tryFallback(ctx, src, manga, rep, map[string]bool{src.Name(): true}); fbSrc != nil {
			src, manga = fbSrc, fbManga
		}
	}

	// `-list` just prints the chapter list for the chosen source; no download or
	// fallback.
	if cfg.list {
		sel, err := selection.Parse(cfg.chapters)
		if err != nil {
			return err
		}
		chapters := sel.Filter(manga.Chapters)
		rep.series(manga.Title, len(manga.Chapters), len(chapters))
		rep.chapters(chapters)
		return nil
	}

	// Download, and if a source fails to deliver a single chapter (e.g. a
	// MangaDex@Home node that 404s every page during a bad window), fall back to
	// the same series on another source rather than leaving the user with
	// nothing. A partial success is left alone — switching sources mid-series
	// would risk inconsistent chapter numbering.
	tried := map[string]bool{}
	for {
		tried[src.Name()] = true
		done, failed, interrupted, err := downloadFrom(ctx, src, manga, cfg, rep)
		if err != nil {
			return err
		}
		if interrupted {
			return nil
		}
		if failed > 0 && done == 0 && !cfg.noFallback {
			if fbSrc, fbManga := tryFallback(ctx, src, manga, rep, tried); fbSrc != nil {
				src, manga = fbSrc, fbManga
				continue
			}
		}
		if failed > 0 {
			return fmt.Errorf("%d chapter(s) failed", failed)
		}
		return nil
	}
}

// downloadFrom selects the requested chapters from one source and downloads
// them, returning per-source counts so run can decide whether to fall back.
func downloadFrom(ctx context.Context, src source.Source, manga *model.Manga, cfg config, rep reporter) (done, failed int, interrupted bool, err error) {
	sel, err := selection.Parse(cfg.chapters)
	if err != nil {
		return 0, 0, false, err
	}
	chapters := sel.Filter(manga.Chapters)
	title := cfg.name
	if title == "" {
		title = manga.Title
	}
	rep.series(title, len(manga.Chapters), len(chapters))
	if len(chapters) == 0 {
		return 0, 0, false, fmt.Errorf("no chapters selected")
	}

	seriesDir := filepath.Join(cfg.out, naming.Sanitize(title))
	if err := os.MkdirAll(seriesDir, 0o755); err != nil {
		return 0, 0, false, err
	}
	rep.saving(seriesDir)

	jobs := resolveJobs(seriesDir, title, chapters)
	if !cfg.overwrite {
		existing := existingChapterNumbers(seriesDir)
		if existing == nil {
			existing = map[string]bool{}
		}
		if cfg.library != "" {
			for n := range existingChapterNumbers(filepath.Join(cfg.library, naming.Sanitize(title))) {
				existing[n] = true
			}
		}
		markExisting(jobs, existing)
	}
	done, failed, interrupted = downloadAll(ctx, src, manga, jobs, cfg, rep)
	return done, failed, interrupted, nil
}

// tryFallback looks for the same series on another searchable source when the
// chosen one can't deliver — either it listed nothing downloadable (an
// officially licensed series on MangaDex whose chapters all live on an external
// reader) or every chapter failed to download (e.g. a flaky MangaDex@Home node
// 404ing every page). It searches every source not already in tried by title,
// accepts only an exact (normalized) title match that actually has downloadable
// chapters, and returns that source and its fetched Manga. Returns (nil, nil)
// when no usable alternative is found, leaving the caller's result intact.
func tryFallback(ctx context.Context, orig source.Source, origManga *model.Manga, rep reporter, tried map[string]bool) (source.Source, *model.Manga) {
	title := strings.TrimSpace(origManga.Title)
	if title == "" {
		return nil, nil
	}
	for _, s := range source.All() {
		if tried[s.Name()] {
			continue
		}
		se, ok := s.(source.Searcher)
		if !ok {
			continue
		}
		results, err := se.Search(ctx, title, 10)
		if err != nil {
			continue
		}
		for _, r := range results {
			if !titleMatches(title, r.Title) {
				continue
			}
			cand := source.For(r.URL)
			if cand == nil {
				continue
			}
			m, err := cand.Series(ctx, r.URL)
			if err != nil || len(m.Chapters) == 0 {
				continue
			}
			rep.fallback(orig.Name(), cand.Name(), m.Title, len(m.Chapters))
			return cand, m
		}
	}
	return nil, nil
}

// titleMatches reports whether two series titles refer to the same series,
// comparing case- and whitespace-insensitively. An exact match (rather than a
// substring) keeps "One Piece" from matching "One Piece Party".
func titleMatches(a, b string) bool {
	return normalizeTitle(a) == normalizeTitle(b)
}

func normalizeTitle(s string) string {
	return strings.ToLower(strings.Join(strings.Fields(s), " "))
}

// job pairs a chapter with the unique output path it will be written to.
// exists marks chapters already on disk under a different file name (see
// markExisting), which are skipped just like an exact-path match.
type job struct {
	chapter model.Chapter
	series  string // series title used for the file name and ComicInfo
	path    string
	exists  bool
}

// cbzChapterNum extracts the chapter number from any CBZ following the
// "<Series> - Chapter <number>[ - <title>].cbz" layout, whatever the series
// prefix or title suffix.
var cbzChapterNum = regexp.MustCompile(`(?i) - chapter ([0-9]+(?:\.[0-9]+)?)(?: - .*)?\.cbz$`)

// existingChapterNumbers scans seriesDir and returns the padded chapter
// numbers that already have a CBZ. Earlier runs may have used another source
// with a different series title or chapter-title suffix in the file names —
// matching on the chapter number keeps a refresh from re-downloading those
// chapters as duplicates.
func existingChapterNumbers(seriesDir string) map[string]bool {
	entries, err := os.ReadDir(seriesDir)
	if err != nil {
		return nil
	}
	nums := make(map[string]bool, len(entries))
	for _, e := range entries {
		if e.IsDir() {
			continue
		}
		if m := cbzChapterNum.FindStringSubmatch(e.Name()); m != nil {
			nums[naming.PadNumber(m[1], 4)] = true
		}
	}
	return nums
}

// markExisting flags jobs whose chapter number already has a CBZ on disk.
// Unnumbered chapters (one-shots, specials) all pad to "0000", so they're left
// to the exact-path check instead of shadowing each other.
func markExisting(jobs []job, existing map[string]bool) {
	for i := range jobs {
		if jobs[i].chapter.Number == "" {
			continue
		}
		if existing[naming.PadNumber(jobs[i].chapter.Number, 4)] {
			jobs[i].exists = true
		}
	}
}

// resolveJobs assigns every chapter a unique CBZ path within seriesDir. Two
// chapters whose sanitized names would otherwise collide (e.g. genuinely
// distinct releases sharing a number) get a " (2)", " (3)", … suffix so one
// never silently overwrites another. Assignment is deterministic for a given
// chapter list, so re-runs reproduce the same names and skip logic still works.
func resolveJobs(seriesDir, title string, chapters []model.Chapter) []job {
	used := map[string]bool{}
	jobs := make([]job, 0, len(chapters))
	for _, ch := range chapters {
		base := naming.ChapterFile(title, ch.Number, ch.Title)
		name := base
		for i := 2; used[strings.ToLower(name)]; i++ {
			name = fmt.Sprintf("%s (%d).cbz", strings.TrimSuffix(base, ".cbz"), i)
		}
		used[strings.ToLower(name)] = true
		jobs = append(jobs, job{chapter: ch, series: title, path: filepath.Join(seriesDir, name)})
	}
	return jobs
}

// result reports the outcome of one chapter for the final summary.
type result struct {
	chapter model.Chapter
	skipped bool
	err     error
}

// downloadAll runs the chapter jobs and reports a per-source summary. It returns
// how many chapters were downloaded and failed (and whether the run was
// interrupted) so the caller can decide whether a wholesale failure warrants
// falling back to another source.
func downloadAll(ctx context.Context, src source.Source, manga *model.Manga, jobsList []job, cfg config, rep reporter) (done, failed int, interrupted bool) {
	jobs := make(chan job)
	results := make(chan result)

	var wg sync.WaitGroup
	workers := cfg.chapterWorkers
	if workers < 1 {
		workers = 1
	}
	for i := 0; i < workers; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for j := range jobs {
				results <- doChapter(ctx, src, manga, j, cfg)
			}
		}()
	}

	go func() {
		defer close(jobs)
		for _, j := range jobsList {
			select {
			case <-ctx.Done():
				return
			case jobs <- j:
			}
		}
	}()

	go func() {
		wg.Wait()
		close(results)
	}()

	var skipped, canceled int
	total := len(jobsList)
	for r := range results {
		switch {
		case errors.Is(r.err, context.Canceled):
			canceled++
		case r.err != nil:
			failed++
		case r.skipped:
			skipped++
		default:
			done++
		}
		rep.chapterDone(r)
	}

	interrupted = ctx.Err() != nil
	rep.summary(done, skipped, failed, total, interrupted)
	return done, failed, interrupted
}

// chapterAttempts is how many times a chapter is tried end-to-end. Re-resolving
// the page list between attempts is what recovers from a bad page source — most
// importantly a MangaDex@Home node that 404s its pages during a bad window,
// where a fresh at-home request (after a short pause) hands back a working node.
// Such nodes can stay bad for more than one retry, so we try a few times.
const chapterAttempts = 4

// chapterRetryBackoff returns how long to wait before re-attempting a chapter,
// growing with each attempt and jittered so concurrent chapter workers don't
// re-resolve and re-hammer the same node in lockstep.
func chapterRetryBackoff(attempt int) time.Duration {
	return time.Duration(attempt)*800*time.Millisecond + time.Duration(rand.Intn(400))*time.Millisecond
}

func doChapter(ctx context.Context, src source.Source, manga *model.Manga, j job, cfg config) result {
	ch := j.chapter
	if ctx.Err() != nil {
		return result{chapter: ch, err: ctx.Err()}
	}

	if !cfg.overwrite {
		if j.exists {
			return result{chapter: ch, skipped: true}
		}
		if _, err := os.Stat(j.path); err == nil {
			return result{chapter: ch, skipped: true}
		}
	}

	var lastErr error
	for attempt := 0; attempt < chapterAttempts; attempt++ {
		if ctx.Err() != nil {
			return result{chapter: ch, err: ctx.Err()}
		}
		// Pause before a retry so a transiently-bad MangaDex@Home node has time to
		// recover (or hand back a different node), aborting promptly on Ctrl-C.
		if attempt > 0 {
			select {
			case <-ctx.Done():
				return result{chapter: ch, err: ctx.Err()}
			case <-time.After(chapterRetryBackoff(attempt)):
			}
		}
		// Re-resolving pages each attempt gives MangaDex a fresh at-home node and
		// scrapers a fresh reader page.
		urls, err := src.Pages(ctx, ch)
		if err != nil {
			lastErr = fmt.Errorf("list pages: %w", err)
			continue
		}

		// Use the full chapter (reader) URL as the Referer — that's what a browser
		// sends when loading the page images, so it satisfies both origin- and
		// path-based hotlink checks. MangaDex chapters have no URL (API-served)
		// and need no Referer.
		res, err := download.Pages(ctx, urls, download.Options{
			Concurrency: cfg.concurrency,
			Retries:     cfg.retries,
			Referer:     ch.URL,
		})
		if err != nil {
			lastErr = err
			continue
		}
		info := &cbz.ComicInfo{Series: j.series, Number: ch.Number, Title: ch.Title}
		if err := cbz.Write(j.path, res.Pages, cfg.padStart, info); err != nil {
			res.Cleanup()
			lastErr = fmt.Errorf("write cbz: %w", err)
			continue
		}
		res.Cleanup()
		if cfg.trueBlack {
			if _, err := cbz.TrueBlack(j.path); err != nil {
				os.Remove(j.path) // don't leave a half-processed chapter behind
				lastErr = fmt.Errorf("true black: %w", err)
				continue
			}
		}
		return result{chapter: ch}
	}
	return result{chapter: ch, err: lastErr}
}
