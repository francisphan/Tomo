// Package httpx is a thin HTTP helper shared by sources and the downloader.
// It centralises the User-Agent, timeouts and a couple of convenience getters
// so every site adapter behaves like a normal browser.
package httpx

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"sync"
	"time"

	"github.com/PuerkitoBio/goquery"
)

// UserAgent mimics a recent desktop Chrome. Some scanlation sites reject
// requests without a browser-like UA.
const UserAgent = "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 " +
	"(KHTML, like Gecko) Chrome/124.0.0.0 Safari/537.36"

// Client is the shared HTTP client. A generous timeout covers slow image CDNs.
var Client = &http.Client{Timeout: 90 * time.Second}

// MinInterval is the minimum spacing between requests to the same host. It
// keeps Tomo polite and under documented rate limits (e.g. MangaDex's ~5 req/s)
// so we don't get throttled or IP-banned. ~5 req/s per host.
var MinInterval = 200 * time.Millisecond

// hostGate spaces out requests to a single host.
type hostGate struct {
	mu   sync.Mutex
	next time.Time
}

// reserve returns how long the caller must wait for its turn, advancing the
// gate so concurrent callers are serialised MinInterval apart.
func (g *hostGate) reserve() time.Duration {
	g.mu.Lock()
	defer g.mu.Unlock()
	now := time.Now()
	if g.next.Before(now) {
		g.next = now
	}
	wait := g.next.Sub(now)
	g.next = g.next.Add(MinInterval)
	return wait
}

var (
	gatesMu sync.Mutex
	gates   = map[string]*hostGate{}
)

func gateFor(host string) *hostGate {
	gatesMu.Lock()
	defer gatesMu.Unlock()
	g := gates[host]
	if g == nil {
		g = &hostGate{}
		gates[host] = g
	}
	return g
}

// throttle blocks until this host's rate-limit slot is available (or ctx ends).
func throttle(ctx context.Context, rawURL string) error {
	if MinInterval <= 0 {
		return nil
	}
	host := ""
	if u, err := url.Parse(rawURL); err == nil {
		host = u.Host
	}
	wait := gateFor(host).reserve()
	if wait <= 0 {
		return nil
	}
	t := time.NewTimer(wait)
	defer t.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-t.C:
		return nil
	}
}

// Get performs a GET with the shared client and browser headers, after waiting
// for the host's rate-limit slot. Extra headers (e.g. a Referer) override the
// defaults. The caller must close the body.
func Get(ctx context.Context, url string, headers map[string]string) (*http.Response, error) {
	if err := throttle(ctx, url); err != nil {
		return nil, err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("User-Agent", UserAgent)
	req.Header.Set("Accept", "*/*")
	req.Header.Set("Accept-Language", "en-US,en;q=0.9")
	for k, v := range headers {
		req.Header.Set(k, v)
	}
	return Client.Do(req)
}

// GetBytes reads the full body of a successful GET.
func GetBytes(ctx context.Context, url string, headers map[string]string) ([]byte, error) {
	resp, err := Get(ctx, url, headers)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, fmt.Errorf("GET %s: status %d", url, resp.StatusCode)
	}
	return io.ReadAll(resp.Body)
}

// GetJSON decodes a JSON GET response into v.
func GetJSON(ctx context.Context, url string, headers map[string]string, v interface{}) error {
	resp, err := Get(ctx, url, headers)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 512))
		return fmt.Errorf("GET %s: status %d: %s", url, resp.StatusCode, string(body))
	}
	return json.NewDecoder(resp.Body).Decode(v)
}

// GetDoc fetches a URL and parses it as an HTML document.
func GetDoc(ctx context.Context, url string, headers map[string]string) (*goquery.Document, error) {
	resp, err := Get(ctx, url, headers)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, fmt.Errorf("GET %s: status %d", url, resp.StatusCode)
	}
	return goquery.NewDocumentFromReader(resp.Body)
}
