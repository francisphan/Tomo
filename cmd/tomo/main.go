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
	"os"
	"os/signal"
	"path/filepath"
	"strings"
	"sync"
	"syscall"

	"tomo/internal/cbz"
	"tomo/internal/download"
	"tomo/internal/model"
	"tomo/internal/naming"
	"tomo/internal/selection"
	"tomo/internal/source"
)

type config struct {
	out            string
	chapters       string
	lang           string
	concurrency    int
	chapterWorkers int
	retries        int
	list           bool
	overwrite      bool
	json           bool
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

	cfg := config{}
	flag.StringVar(&cfg.out, "out", "./manga", "output directory (a per-series subfolder is created)")
	flag.StringVar(&cfg.chapters, "chapters", "", `chapters to fetch, e.g. "1-20,25,40-" (default: all)`)
	flag.StringVar(&cfg.lang, "lang", "en", "translated language code (MangaDex only)")
	flag.IntVar(&cfg.concurrency, "concurrency", 8, "parallel image downloads per chapter")
	flag.IntVar(&cfg.chapterWorkers, "chapter-workers", 2, "chapters downloaded in parallel")
	flag.IntVar(&cfg.retries, "retries", 3, "retry attempts per failed image")
	flag.BoolVar(&cfg.list, "list", false, "list chapters without downloading")
	flag.BoolVar(&cfg.overwrite, "overwrite", false, "re-download chapters whose CBZ already exists")
	flag.BoolVar(&cfg.json, "json", false, "emit machine-readable NDJSON progress (used by the desktop app)")
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
	fmt.Fprintf(os.Stderr, "  tomo search [flags] <query>    find a series by title\n\n")
	fmt.Fprintf(os.Stderr, "Download flags:\n")
	flag.PrintDefaults()
	fmt.Fprintf(os.Stderr, "\nSupported sites:\n")
	for _, s := range source.All() {
		fmt.Fprintf(os.Stderr, "  - %s\n", s.Name())
	}
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

	sel, err := selection.Parse(cfg.chapters)
	if err != nil {
		return err
	}
	chapters := sel.Filter(manga.Chapters)
	rep.series(manga.Title, len(manga.Chapters), len(chapters))

	if cfg.list {
		rep.chapters(chapters)
		return nil
	}
	if len(chapters) == 0 {
		return fmt.Errorf("no chapters selected")
	}

	seriesDir := filepath.Join(cfg.out, naming.Sanitize(manga.Title))
	if err := os.MkdirAll(seriesDir, 0o755); err != nil {
		return err
	}
	rep.saving(seriesDir)

	return downloadAll(ctx, src, manga, resolveJobs(seriesDir, manga.Title, chapters), cfg, rep)
}

// job pairs a chapter with the unique output path it will be written to.
type job struct {
	chapter model.Chapter
	path    string
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
		jobs = append(jobs, job{chapter: ch, path: filepath.Join(seriesDir, name)})
	}
	return jobs
}

// result reports the outcome of one chapter for the final summary.
type result struct {
	chapter model.Chapter
	skipped bool
	err     error
}

func downloadAll(ctx context.Context, src source.Source, manga *model.Manga, jobsList []job, cfg config, rep reporter) error {
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

	var done, skipped, failed, canceled int
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

	interrupted := ctx.Err() != nil
	rep.summary(done, skipped, failed, total, interrupted)
	if interrupted {
		return nil
	}
	if failed > 0 {
		return fmt.Errorf("%d chapter(s) failed", failed)
	}
	return nil
}

// chapterAttempts is how many times a chapter is tried end-to-end. Re-resolving
// the page list between attempts is what recovers from a bad page source — most
// importantly a MangaDex@Home node that's missing a page, where a fresh
// at-home request hands back a different (working) node.
const chapterAttempts = 2

func doChapter(ctx context.Context, src source.Source, manga *model.Manga, j job, cfg config) result {
	ch := j.chapter
	if ctx.Err() != nil {
		return result{chapter: ch, err: ctx.Err()}
	}

	if !cfg.overwrite {
		if _, err := os.Stat(j.path); err == nil {
			return result{chapter: ch, skipped: true}
		}
	}

	var lastErr error
	for attempt := 0; attempt < chapterAttempts; attempt++ {
		if ctx.Err() != nil {
			return result{chapter: ch, err: ctx.Err()}
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
		if err := cbz.Write(j.path, res.Pages); err != nil {
			res.Cleanup()
			lastErr = fmt.Errorf("write cbz: %w", err)
			continue
		}
		res.Cleanup()
		return result{chapter: ch}
	}
	return result{chapter: ch, err: lastErr}
}
