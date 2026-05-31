// Package download fetches a chapter's page images concurrently with retries.
package download

import (
	"context"
	"fmt"
	"io"
	"math/rand"
	"net/http"
	"path"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"tomo/internal/httpx"
)

// Page is a single downloaded image, kept in memory until it is zipped.
type Page struct {
	Index int    // 1-based position in the chapter
	Ext   string // file extension including dot, e.g. ".jpg"
	Data  []byte
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

// Pages downloads all image URLs and returns them ordered by index. It fails
// fast: if any page cannot be fetched after retries, the whole chapter errors
// so we never write a CBZ with missing pages.
func Pages(ctx context.Context, urls []string, opts Options) ([]Page, error) {
	if opts.Concurrency <= 0 {
		opts.Concurrency = 8
	}
	if len(urls) == 0 {
		return nil, fmt.Errorf("no pages found")
	}

	// A child context lets us abort in-flight downloads the moment any page
	// fails, so "fail fast" actually cancels outstanding requests rather than
	// waiting out their timeouts.
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()

	results := make([]Page, len(urls))
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

			data, ct, err := fetchWithRetry(ctx, url, headers, opts.Retries)
			if err != nil {
				mu.Lock()
				if firstErr == nil {
					firstErr = fmt.Errorf("page %d (%s): %w", idx+1, url, err)
					cancel() // abort the rest of this chapter
				}
				mu.Unlock()
				return
			}
			results[idx] = Page{Index: idx + 1, Ext: extFor(url, ct), Data: data}
		}(i, u)
	}

	wg.Wait()
	if firstErr != nil {
		return nil, firstErr
	}
	sort.Slice(results, func(a, b int) bool { return results[a].Index < results[b].Index })
	return results, nil
}

func fetchWithRetry(ctx context.Context, url string, headers map[string]string, retries int) ([]byte, string, error) {
	var lastErr error
	var retryAfter time.Duration
	for attempt := 0; attempt <= retries; attempt++ {
		if attempt > 0 {
			select {
			case <-ctx.Done():
				return nil, "", ctx.Err()
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
			io.Copy(io.Discard, io.LimitReader(resp.Body, 4096))
			resp.Body.Close()
			lastErr = fmt.Errorf("status %d", resp.StatusCode)
			continue
		}
		ct := resp.Header.Get("Content-Type")
		data, err := readAll(resp)
		if err != nil {
			lastErr = err
			continue
		}
		return data, ct, nil
	}
	return nil, "", lastErr
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

func readAll(resp *http.Response) ([]byte, error) {
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, fmt.Errorf("status %d", resp.StatusCode)
	}
	data, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}
	if len(data) == 0 {
		return nil, fmt.Errorf("empty body")
	}
	return data, nil
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
