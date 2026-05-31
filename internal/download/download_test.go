package download

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"testing"
)

func TestPagesStreamsToDiskInOrder(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "image/jpeg")
		fmt.Fprintf(w, "data-for-%s", r.URL.Path)
	}))
	defer srv.Close()

	urls := []string{srv.URL + "/a.jpg", srv.URL + "/b.png", srv.URL + "/c.webp"}
	res, err := Pages(context.Background(), urls, Options{Concurrency: 3, Retries: 1})
	if err != nil {
		t.Fatalf("Pages: %v", err)
	}
	defer res.Cleanup()

	if len(res.Pages) != 3 {
		t.Fatalf("got %d pages, want 3", len(res.Pages))
	}
	wantExt := []string{".jpg", ".png", ".webp"}
	for i, p := range res.Pages {
		if p.Index != i+1 {
			t.Errorf("page %d has Index %d", i, p.Index)
		}
		if p.Ext != wantExt[i] {
			t.Errorf("page %d ext = %q, want %q", i, p.Ext, wantExt[i])
		}
		b, err := os.ReadFile(p.Path)
		if err != nil {
			t.Fatalf("page %d file: %v", i, err)
		}
		if len(b) == 0 {
			t.Errorf("page %d file is empty", i)
		}
	}

	dir := res.dir
	res.Cleanup()
	if _, err := os.Stat(dir); !os.IsNotExist(err) {
		t.Errorf("Cleanup did not remove temp dir %s", dir)
	}
}

func TestPagesFailFastCleansUp(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/bad.jpg" {
			http.Error(w, "nope", http.StatusNotFound)
			return
		}
		w.Write([]byte("ok"))
	}))
	defer srv.Close()

	urls := []string{srv.URL + "/ok.jpg", srv.URL + "/bad.jpg"}
	res, err := Pages(context.Background(), urls, Options{Concurrency: 2, Retries: 0})
	if err == nil {
		t.Fatal("expected an error when a page 404s")
	}
	if res != nil {
		t.Errorf("expected nil result on failure, got %+v", res)
	}
}
