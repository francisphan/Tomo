# Tomo

Download every chapter of a manga/manhwa series as **CBZ files** for offline
reading in [Tachimanga](https://www.tachimanga.app/) on your iPad (or any
Tachiyomi/Mihon-style reader).

Give it a series URL; Tomo detects the source, lists all chapters, downloads
the pages concurrently, and writes one CBZ per chapter into a per-series folder
ready to import.

## Install

### Prebuilt binaries (no Go needed)

Download the binary for your OS from the [Releases](https://github.com/francisphan/Tomo/releases)
page — `tomo-darwin-arm64`, `tomo-darwin-amd64`, `tomo-windows-amd64.exe`, or
`tomo-linux-amd64` — make it executable, and run it. No runtime to install.

### Desktop app (GUI)

Prefer a window over the terminal? A small **Tauri desktop app** lives in
[`desktop/`](desktop/) — search, pick a series, choose chapters, watch progress.
It bundles the same `tomo` engine as a sidecar. Grab the installer for your OS
from Releases, or build it yourself (see [desktop/README.md](desktop/README.md)).

### Build the CLI from source

Requires Go 1.18+.

```bash
cd Tomo
go build -o tomo ./cmd/tomo            # single static binary
bash scripts/build-cli.sh v1.0.0       # or cross-compile all OS targets into dist/
```

## Usage

```bash
tomo [flags] <series-url>      # download a series
tomo search [flags] <query>    # find a series by title
tomo pad <dir|cbz>...          # add (or repair) the -pad-start blank page in chapters already downloaded
tomo unpad <cbz>...            # remove it where split spreads pair up without it
tomo black <dir|cbz>...        # make pages greyscale with true blacks, for OLED screens
tomo arcs <series-dir>...      # write the arc guide into chapter titles, reading lists and the description
tomo check [-library dir] <cbz>... # look a downloaded chapter over before it goes into the library
```

### Search

Don't have the URL? Search by title across the supported sites and copy a
result URL into a download:

```bash
tomo search chainsaw man              # all searchable sources
tomo search -source mangadex chainsaw man   # just one source
tomo search -limit 5 solo leveling          # cap results per source
```

Each hit prints the series title and the URL to paste back into `tomo <url>`.
Sources are queried concurrently; a source that errors (e.g. markup change or
timeout) is reported inline without failing the others. MangaDex search uses
its official API and is the most reliable.

### Download

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
| `-pad-start` | `false` | Add a blank black page at the start of each chapter, so double-page spreads line up when reading two pages at a time (e.g. iPad landscape) |
| `-true-black` | `false` | Convert black-and-white pages to greyscale with true blacks as each chapter is written (see [True blacks](#true-blacks-for-oled-screens)); colour pages are kept |
| `-library` | | With `-out` as a holding folder: also skip chapters already in `<library>/<series>` |

Re-running is safe and resumable: chapters whose CBZ already exists are skipped
unless `-overwrite` is set. A chapter that fails to download leaves no partial
file behind (CBZs are written atomically), so just run the command again.

### Two-page reading (spreads)

In landscape on an iPad, the reader shows two pages side by side. If a
chapter opens with a single title page, every double-page spread after it gets
split across two screens. `-pad-start` puts a blank black page in front so the
spreads pair up. Use it for page-based manga, not long-strip manhwa.

- A chapter that opens with a spread gets no blank page. A spread stored as one
  wide image shows on its own anyway, so the blank page would just sit alone.
- The blank page is sized like the chapter's single pages, never like a spread.

To pad chapters you already have, run `tomo pad ~/Manga/<Series>`. It also
repairs chapters an older Tomo padded wrongly: it removes the blank page in
front of an opening spread and redraws spread-sized blank pages. Chapters that
are already right are left alone.

Some chapters have spreads split across two files that only pair up *without*
the blank page. Run `tomo unpad` on those chapters. They're marked in
`ComicInfo.xml`, so a later `tomo pad` leaves them unpadded.

### Checking chapters before they reach the library

`tomo check -library ~/Manga <chapter.cbz>` prints `ok:` or `held:` with the
reason, and exits non-zero when a chapter is held. A chapter is held when a
page won't open, when a page with real content appears twice (repeated blank
or logo pages are normal), or when it has fewer than half the pages the series
usually has (spreads count as two; extras numbered like 12.5 are exempt). The
nightly refresh in `deploy/komga` downloads into a holding folder and only
moves chapters that pass.

### True blacks for OLED screens

Scans rarely use true black: the ink usually sits around level 30 of 255, which
an OLED screen (iPad Pro, iPhone) shows as a faint grey glow. `tomo black
~/Manga/<Series>` rewrites each chapter in place:

- Black-and-white pages become greyscale, which also removes faint colour
  noise in dark areas. Colour pages (more than 2% of the page in clear
  colour: colour openers, covers, and pages with a spot-colour title) are
  kept exactly as they are.
- Anything at level 40 or darker becomes pure black, and the darker greys up
  to level 112 are stretched down to meet it. Lighter greys, screentone and
  paper are left as they were.
- JPEG pages are re-saved at the quality they came with, so chapters stay
  about the same size. WebP pages become JPEG (quality 88); other formats are
  stored as PNG.

Converted chapters are marked in their `ComicInfo.xml` and skipped on a re-run,
so a chapter is never darkened twice. The originals are replaced, so keep a
copy (or re-download) if you might want them back.

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

Each CBZ also carries a `ComicInfo.xml` with the series name, chapter number
and title. Servers like Komga read it, so a chapter is numbered by its real
chapter number rather than by its position in the folder (which extras and
decimal chapters like 125.5 would otherwise shift).

To import:

1. Copy the `<Series Title>` folder to your iPad — via **Finder** (plug in,
   drag into the Tachimanga app's Documents), **AirDrop**, or the **Files app**
   (e.g. iCloud Drive / a USB drive).
2. In Tachimanga, go to the **Local** source / **Import** and point it at the
   folder.
3. The series appears with all chapters; download/cache them for offline
   reading and you're done.

## Arc guide

`site/` is a small static page listing where each story arc starts and ends
for series in the library, for picking a reread:
<https://francisphan.github.io/Tomo/>. Pushing a change under `site/` to
`main` redeploys it via `.github/workflows/pages.yml`. To preview, serve the
folder (`python3 -m http.server -d site`) and open it; the page fetches its
data, so opening the file directly won't work.

The data is `site/arcs.json`, shared by the site and `tomo arcs`:

- Chapter ranges come from each series' Fandom wiki (its Story Arcs page and
  per-arc chapter categories). Haikyu!! matches come from each match's wiki
  article, trimmed to the chapters from first serve to final point.
- `end: null` marks an arc that's still running; `latest` is the newest
  chapter out when the data was last updated (it sizes the running arc).
- `part` groups arcs under a saga heading, `matches` (`name`, `start`, `end`)
  lists the games inside an arc for sports series, and `folder` names the
  library folder when it differs from `title`.
- Summaries describe the setup only, so they're safe to skim before a reread.

### In the reader: `tomo arcs`

`tomo arcs ~/Manga/<Series>` writes the guide into the series itself, so
Komga (and Tachimanga through it) shows it without the site open:

- Each chapter's title becomes `<arc> · <match> · <source title>`, using the
  parts it has (for example `Interhigh · Karasuno vs Aoba Johsai`).
- Each chapter's `StoryArc` is set to `<Series> · <arc>`, which Komga turns
  into one reading list per arc.
- `series.json` gets the whole guide as the series description.

Folders the guide doesn't cover are skipped, and only chapters whose info
changes are rewritten, so it's safe to run over the whole library. It reads
the published guide by default; pass `-data site/arcs.json` to use a local
copy.

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
site/                Arc guide (static GitHub Pages site)
```

### Adding a new site

Implement the `source.Source` interface (four methods) in a new file under
`internal/source/`, call `source.Register(...)` from its `init()`, and that's
it — the CLI picks it up automatically via `Matches(url)`. To make the source
searchable, also implement the optional `source.Searcher` interface
(`Search(ctx, query, limit)`); `tomo search` discovers it via a type assertion.

## Legal

For personal, offline reading of content you have the right to access. Don't
redistribute downloaded files. Respect each site's terms of service.
