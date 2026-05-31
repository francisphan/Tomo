# Tomo

Download every chapter of a manga/manhwa series as **CBZ files** for offline
reading in [Tachimanga](https://www.tachimanga.app/) on your iPad (or any
Tachiyomi/Mihon-style reader).

Give it a series URL; Tomo detects the source, lists all chapters, downloads
the pages concurrently, and writes one CBZ per chapter into a per-series folder
ready to import.

## Install / build

Requires Go 1.18+.

```bash
cd Tomo
go build -o tomo ./cmd/tomo
```

This produces a single static `tomo` binary.

## Usage

```bash
tomo [flags] <series-url>
```

Examples:

```bash
# Download every chapter
tomo https://mangadex.org/title/<uuid>/<slug>

# Only chapters 1–20, into a chosen folder
tomo -out ~/Manga -chapters 1-20 https://mangadex.org/title/<uuid>/<slug>

# Just list what's available (no download)
tomo -list https://weebcentral.com/series/<id>/<slug>

# Mixed selection: 1 to 10, plus 25, plus everything from 40 on
tomo -chapters "1-10,25,40-" <series-url>
```

Run `tomo` with no arguments to see all flags and supported sites.

### Flags

| Flag | Default | Description |
|------|---------|-------------|
| `-out` | `./manga` | Output directory; a `<Series Title>/` subfolder is created inside it |
| `-chapters` | (all) | Range spec: `N`, `A-B`, `A-`, `-B`, comma-separated |
| `-lang` | `en` | Translated language code (**MangaDex only**) |
| `-concurrency` | `8` | Parallel image downloads per chapter |
| `-chapter-workers` | `2` | Chapters downloaded in parallel |
| `-retries` | `3` | Retry attempts per failed image |
| `-list` | `false` | List chapters without downloading |
| `-overwrite` | `false` | Re-download chapters whose CBZ already exists |

Re-running is safe and resumable: chapters whose CBZ already exists are skipped
unless `-overwrite` is set. A chapter that fails to download leaves no partial
file behind (CBZs are written atomically), so just run the command again.

## Supported sites

| Source | URL host | Method | Notes |
|--------|----------|--------|-------|
| **MangaDex** | `mangadex.org` | Official JSON API | Most robust. Use `-lang` to pick the translation. |
| **Asura Scans** | `asuracomic.net` | HTML scrape | Manhwa scanlation site. |
| **Weeb Central** | `weebcentral.com` | HTML/HTMX fragments | |
| **Bato.to** | `bato.to` + mirrors | HTML scrape | Many mirror domains supported. |

> **HTML scrapers are fragile.** MangaDex uses a documented API and is the most
> reliable. The other three parse site HTML, so when a site changes its markup
> the selectors may need updating — each adapter keeps its selectors in one
> small, clearly-commented file under `internal/source/` for exactly that reason.

### Known limitations

- **Bato.to** may require a per-image access token (derived from encrypted JS
  values) on some mirrors. Tomo doesn't evaluate that JS, so on token-gated
  mirrors images can return 403. If a chapter fails, try a different mirror or
  another source.
- **MangaDex** collapses multiple uploads of the same chapter number (different
  scanlation groups) to one, keeping the most complete (most pages). Use `-lang`
  to choose the translation. Genuinely split releases that share a number are
  also collapsed.
- **Rate limiting:** Tomo spaces requests to each host (~5/s) and honours
  `Retry-After` on 429/503 responses, to stay polite and avoid IP bans. This
  caps raw download speed by design.
- **Image formats:** pages are stored in their original format. JPG/PNG/WebP/GIF
  display everywhere; **AVIF** is rarely served but may not render in all
  readers if it is.

## Getting the CBZs onto your iPad (Tachimanga)

Tomo writes this layout:

```
<out>/
  <Series Title>/
    <Series Title> - Chapter 0001 - <chapter title>.cbz
    <Series Title> - Chapter 0002 - <chapter title>.cbz
    ...
```

This is exactly the layout Tachimanga's **local library** import expects: one
folder per series, one archive per chapter, with a zero-padded chapter number
in each file name so chapters sort and are recognised correctly.

To import:

1. Copy the `<Series Title>` folder to your iPad — via **Finder** (plug in,
   drag into the Tachimanga app's Documents), **AirDrop**, or the **Files app**
   (e.g. iCloud Drive / a USB drive).
2. In Tachimanga, go to the **Local** source / **Import** and point it at the
   folder.
3. The series appears with all chapters; download/cache them for offline
   reading and you're done.

## Project layout

```
cmd/tomo/            CLI entry point and orchestration
internal/
  model/             Manga + Chapter types
  source/            Source interface, registry, and per-site adapters
  download/          Concurrent page downloader with retries
  cbz/               Atomic CBZ (zip) writer
  naming/            Filesystem- and Tachimanga-friendly names
  selection/         Chapter range spec parser ("1-10,25,40-")
  httpx/             Shared HTTP client + helpers
```

### Adding a new site

Implement the `source.Source` interface (four methods) in a new file under
`internal/source/`, call `source.Register(...)` from its `init()`, and that's
it — the CLI picks it up automatically via `Matches(url)`.

## Legal

For personal, offline reading of content you have the right to access. Don't
redistribute downloaded files. Respect each site's terms of service.
