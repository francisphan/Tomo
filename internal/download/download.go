// Package download fetches a chapter's page images concurrently with retries.
//
// Pages are streamed to temporary files rather than buffered in memory, so a
// long manhwa chapter (often 50-150 full-width images) doesn't pin tens or
// hundreds of MB of RAM. The caller zips from those files and then calls
// Result.Cleanup to remove them.
package download

import (
	"context"
	"fmt"
	"io"
	"math/rand"
	"net/http"
	"os"
	"path"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"tomo/internal/httpx"
)

// Page is a single downloaded image on disk.
type Page struct {
	Index int    // 1-based position in the chapter
	Ext   string // file extension including dot, e.g. ".jpg"
	Path  string // path to the temporary file holding the image
}

// Result is a downloaded chapter's pages plus the temp dir holding them.
type Result struct {
	Pages []Page
	dir   string
}

// Cleanup removes the temporary files backing the pages. Safe to call on nil
// and to call more than once.
func (r *Result) Cleanup() {
	if r != nil && r.dir != "" {
		os.RemoveAll(r.dir)
	}
}

// Options tune a chapter download.
type Options struct {
	// Concurrency is the number of pages fetched in parallel.
	Concurrency int
	// Referer is sent with every image request; some CDNs require it.
	Referer string
	// Retries is the number of additional attempts per page after a failure.
	Retries int
}

// Pages downloads all image URLs to temp files and returns them ordered by
// index. It fails fast: if any page can't be fetched after retries, the whole
// chapter errors (and its temp files are removed) so we never write a CBZ with
// missing pages.
func Pages(ctx context.Context, urls []string, opts Options) (*Result, error) {
	if opts.Concurrency <= 0 {
		opts.Concurrency = 8
	}
	if len(urls) == 0 {
		return nil, fmt.Errorf("no pages found")
	}

	dir, err := os.MkdirTemp("", "tomo-chapter-*")
	if err != nil {
		return nil, err
	}

	// A child context lets us abort in-flight downloads the moment any page
	// fails, so "fail fast" actually cancels outstanding requests rather than
	// waiting out their timeouts.
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()

	pages := make([]Page, len(urls))
	sem := make(chan struct{}, opts.Concurrency)
	var wg sync.WaitGroup
	var mu sync.Mutex
	var firstErr error

	headers := map[string]string{}
	if opts.Referer != "" {
		headers["Referer"] = opts.Referer
	}

	for i, u := range urls {
		mu.Lock()
		stop := firstErr != nil
		mu.Unlock()
		if stop {
			break
		}

		wg.Add(1)
		sem <- struct{}{}
		go func(idx int, url string) {
			defer wg.Done()
			defer func() { <-sem }()

			p, err := fetchToFile(ctx, url, headers, opts.Retries, dir, idx)
			if err != nil {
				mu.Lock()
				if firstErr == nil {
					firstErr = fmt.Errorf("page %d (%s): %w", idx+1, url, err)
					cancel() // abort the rest of this chapter
				}
				mu.Unlock()
				return
			}
			pages[idx] = p
		}(i, u)
	}

	wg.Wait()
	if firstErr != nil {
		os.RemoveAll(dir)
		return nil, firstErr
	}
	sort.Slice(pages, func(a, b int) bool { return pages[a].Index < pages[b].Index })
	return &Result{Pages: pages, dir: dir}, nil
}

// fetchToFile downloads one image into dir, streaming the body to disk so the
// full image is never held in memory.
func fetchToFile(ctx context.Context, url string, headers map[string]string, retries int, dir string, index int) (Page, error) {
	var lastErr error
	var retryAfter time.Duration
	for attempt := 0; attempt <= retries; attempt++ {
		if attempt > 0 {
			select {
			case <-ctx.Done():
				return Page{}, ctx.Err()
			case <-time.After(backoff(attempt, retryAfter)):
			}
		}
		retryAfter = 0

		resp, err := httpx.Get(ctx, url, headers)
		if err != nil {
			lastErr = err
			continue
		}
		// Respect server-side throttling rather than hammering through it.
		if resp.StatusCode == http.StatusTooManyRequests || resp.StatusCode == http.StatusServiceUnavailable {
			retryAfter = parseRetryAfter(resp.Header.Get("Retry-After"))
			drainClose(resp.Body)
			lastErr = fmt.Errorf("status %d", resp.StatusCode)
			continue
		}
		if resp.StatusCode < 200 || resp.StatusCode >= 300 {
			drainClose(resp.Body)
			lastErr = fmt.Errorf("status %d", resp.StatusCode)
			continue
		}

		ext := extFor(url, resp.Header.Get("Content-Type"))
		dst := filepath.Join(dir, fmt.Sprintf("%04d%s", index+1, ext))
		f, err := os.Create(dst)
		if err != nil {
			resp.Body.Close()
			return Page{}, err // local FS error — not retryable
		}
		n, copyErr := io.Copy(f, resp.Body)
		resp.Body.Close()
		closeErr := f.Close()
		if copyErr != nil {
			os.Remove(dst)
			lastErr = copyErr
			continue
		}
		if closeErr != nil {
			os.Remove(dst)
			lastErr = closeErr
			continue
		}
		if n == 0 {
			os.Remove(dst)
			lastErr = fmt.Errorf("empty body")
			continue
		}
		return Page{Index: index + 1, Ext: ext, Path: dst}, nil
	}
	return Page{}, lastErr
}

// drainClose consumes a little of the body (to allow connection reuse) and
// closes it.
func drainClose(body io.ReadCloser) {
	io.Copy(io.Discard, io.LimitReader(body, 4096))
	body.Close()
}

// backoff returns how long to wait before the next attempt: at least the
// server-requested Retry-After, otherwise a linear backoff, plus jitter so
// concurrent workers don't retry in lockstep and re-spike the server.
func backoff(attempt int, retryAfter time.Duration) time.Duration {
	base := time.Duration(attempt) * 750 * time.Millisecond
	if retryAfter > base {
		base = retryAfter
	}
	return base + time.Duration(rand.Intn(250))*time.Millisecond
}

// parseRetryAfter understands the delta-seconds form of the Retry-After header.
func parseRetryAfter(v string) time.Duration {
	v = strings.TrimSpace(v)
	if v == "" {
		return 0
	}
	if secs, err := strconv.Atoi(v); err == nil && secs >= 0 {
		return time.Duration(secs) * time.Second
	}
	return 0
}

// extFor picks a sensible file extension from the URL, falling back to the
// Content-Type, then to .jpg.
func extFor(url, contentType string) string {
	if e := strings.ToLower(path.Ext(stripQuery(url))); isImageExt(e) {
		return e
	}
	switch {
	case strings.Contains(contentType, "png"):
		return ".png"
	case strings.Contains(contentType, "webp"):
		return ".webp"
	case strings.Contains(contentType, "gif"):
		return ".gif"
	case strings.Contains(contentType, "avif"):
		return ".avif"
	default:
		return ".jpg"
	}
}

func stripQuery(u string) string {
	if i := strings.IndexAny(u, "?#"); i >= 0 {
		return u[:i]
	}
	return u
}

func isImageExt(e string) bool {
	switch e {
	case ".jpg", ".jpeg", ".png", ".webp", ".gif", ".avif":
		return true
	}
	return false
}
