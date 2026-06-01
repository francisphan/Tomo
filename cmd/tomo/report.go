package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"

	"tomo/internal/model"
)

// reporter renders download progress. The text implementation prints the
// human-readable output; the JSON implementation emits one JSON event per line
// (NDJSON) on stdout so the Tauri sidecar can stream progress to the UI.
type reporter interface {
	fetching(src string)
	series(title string, found, selected int)
	saving(dir string)
	chapters(chs []model.Chapter)
	chapterDone(r result)
	summary(done, skipped, failed, total int, interrupted bool)
	// warn surfaces a non-fatal advisory (e.g. chapters skipped as licensed).
	warn(msg string)
	// fallback announces switching to another source because the chosen one
	// had nothing downloadable.
	fallback(from, to, title string, found int)
}

func newReporter(asJSON bool) reporter {
	if asJSON {
		return &jsonReporter{enc: json.NewEncoder(os.Stdout)}
	}
	return textReporter{}
}

// textReporter is the original human-readable output.
type textReporter struct{}

func (textReporter) fetching(src string) { fmt.Printf("Source: %s\nFetching series…\n", src) }

func (textReporter) series(title string, found, selected int) {
	fmt.Printf("Series: %s\nChapters found: %d, selected: %d\n", title, found, selected)
}

func (textReporter) saving(dir string) { fmt.Printf("Saving to: %s\n\n", dir) }

func (textReporter) warn(msg string) { fmt.Fprintf(os.Stderr, "warning: %s\n", msg) }

func (textReporter) fallback(from, to, title string, found int) {
	fmt.Printf("No downloadable chapters on %s — falling back to %s (%q: %d chapters).\n",
		from, to, title, found)
}

func (textReporter) chapters(chs []model.Chapter) {
	for _, c := range chs {
		line := "  Ch. " + c.Number
		if c.Title != "" {
			line += " — " + c.Title
		}
		fmt.Println(line)
	}
}

func (textReporter) chapterDone(r result) {
	switch {
	case errors.Is(r.err, context.Canceled):
		// interruption is summarised, not printed per-chapter
	case r.err != nil:
		fmt.Printf("  ✗ Ch. %s — %v\n", r.chapter.Number, r.err)
	case r.skipped:
		fmt.Printf("  • Ch. %s — already downloaded\n", r.chapter.Number)
	default:
		fmt.Printf("  ✓ Ch. %s\n", r.chapter.Number)
	}
}

func (textReporter) summary(done, skipped, failed, total int, interrupted bool) {
	if interrupted {
		fmt.Printf("\nInterrupted. %d downloaded, %d skipped before stopping.\n", done, skipped)
		return
	}
	fmt.Printf("\nDone. %d downloaded, %d skipped, %d failed (of %d).\n",
		done, skipped, failed, total)
}

// jsonReporter emits NDJSON progress events on stdout.
type jsonReporter struct{ enc *json.Encoder }

func (j *jsonReporter) emit(v map[string]interface{}) { _ = j.enc.Encode(v) }

func (j *jsonReporter) fetching(src string) {
	j.emit(map[string]interface{}{"event": "fetching", "source": src})
}

func (j *jsonReporter) series(title string, found, selected int) {
	j.emit(map[string]interface{}{"event": "series", "title": title, "found": found, "selected": selected})
}

func (j *jsonReporter) saving(dir string) {
	j.emit(map[string]interface{}{"event": "saving", "dir": dir})
}

func (j *jsonReporter) warn(msg string) {
	j.emit(map[string]interface{}{"event": "warning", "message": msg})
}

func (j *jsonReporter) fallback(from, to, title string, found int) {
	j.emit(map[string]interface{}{
		"event": "fallback", "from": from, "to": to, "title": title, "found": found,
	})
}

func (j *jsonReporter) chapters(chs []model.Chapter) {
	list := make([]map[string]string, len(chs))
	for i, c := range chs {
		list[i] = map[string]string{"number": c.Number, "title": c.Title}
	}
	j.emit(map[string]interface{}{"event": "chapters", "chapters": list})
}

func (j *jsonReporter) chapterDone(r result) {
	status, errStr := "ok", ""
	switch {
	case errors.Is(r.err, context.Canceled):
		status = "canceled"
	case r.err != nil:
		status, errStr = "failed", r.err.Error()
	case r.skipped:
		status = "skipped"
	}
	j.emit(map[string]interface{}{
		"event": "chapter", "number": r.chapter.Number, "title": r.chapter.Title,
		"status": status, "error": errStr,
	})
}

func (j *jsonReporter) summary(done, skipped, failed, total int, interrupted bool) {
	j.emit(map[string]interface{}{
		"event": "done", "downloaded": done, "skipped": skipped,
		"failed": failed, "total": total, "interrupted": interrupted,
	})
}
