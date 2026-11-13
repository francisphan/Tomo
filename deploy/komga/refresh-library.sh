#!/usr/bin/env bash
# refresh-library.sh — fetch new chapters for a list of ongoing series into the
# Komga library. Built to run unattended from cron.
#
# New chapters are downloaded into a holding folder (STAGING_DIR), not the
# library. Each one is looked over with `tomo check` (pages open, no repeated
# content page, not far shorter than the series usually is); chapters that pass
# move into the library, and chapters that don't stay in the holding folder and
# are logged as HELD with the reason. Held chapters are re-checked every run and
# never re-downloaded. To accept one anyway, move it into the library yourself;
# to fetch it again, delete it.
#
# Tomo only downloads chapters that aren't already in the library or the
# holding folder, so every pass is cheap and idempotent, then Komga picks the
# files up on its next scan.
#
# Each series.txt line is:  URL [-flags...] [days [name...]]
#   flags extra tomo flags for this series, e.g. -pad-start for page-based
#         manga read two pages at a time. Flags that take a value must use
#         the -flag=value form (-lang=es), since each flag is one token.
#   days  weekday(s) the series updates (mon..sun, comma-separated, or
#         "daily"). Run this daily from cron and a series tagged "tue" is only
#         checked on Tuesdays; untagged (or "daily") series are checked every
#         run.
#   name  overrides the series folder/file naming (passed to `tomo -name`) —
#         use it when a source titles the series differently from the folder
#         you already have. Everything after the days field, spaces included.
# See series.txt.example.
#
# Usage:
#   ./refresh-library.sh                     # uses series.txt next to this script
#   SERIES_FILE=/path/to/list ./refresh-library.sh
#   FORCE_ALL=1 ./refresh-library.sh         # ignore day tags, check everything
#
# Config (environment variables, or a .env file in this folder):
#   MANGA_LIBRARY   library dir Komga serves from (required)
#   STAGING_DIR     holding folder for new chapters until they pass the check
#                   (default: ~/.local/state/tomo/staging; keep it outside the
#                   library so Komga never shows unchecked chapters)
#   TOMO_BIN        path to the tomo binary            (default: tomo on PATH)
#   SERIES_FILE     series URLs + optional update days (default: ./series.txt)
#   LOG_FILE        append run output here             (default: stdout)
#   FORCE_ALL       set to 1 to check every series regardless of day tags

set -euo pipefail

script_dir="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"

# Load .env if present so MANGA_LIBRARY etc. can be shared with docker-compose.
# Real environment variables already set take precedence is *not* guaranteed
# here — values in .env win — so prefer setting overrides in .env itself.
if [[ -f "$script_dir/.env" ]]; then
  set -a
  # shellcheck disable=SC1091
  source "$script_dir/.env"
  set +a
fi

TOMO_BIN="${TOMO_BIN:-tomo}"
SERIES_FILE="${SERIES_FILE:-$script_dir/series.txt}"
MANGA_LIBRARY="${MANGA_LIBRARY:-}"
STAGING_DIR="${STAGING_DIR:-$HOME/.local/state/tomo/staging}"
FORCE_ALL="${FORCE_ALL:-0}"

log() { printf '%s %s\n' "$(date '+%Y-%m-%d %H:%M:%S')" "$*"; }

# Trim leading/trailing whitespace without the word-splitting xargs would do
# (series URLs can contain characters xargs would mangle).
trim() {
  local s="$1"
  s="${s#"${s%%[![:space:]]*}"}"
  s="${s%"${s##*[![:space:]]}"}"
  printf '%s' "$s"
}

# "mon,thu" / "daily" → 0 if the series should be checked today.
due_today() {
  local days="$1" today d
  [[ -z "$days" || "$days" == "daily" || "$FORCE_ALL" == "1" ]] && return 0
  today="$(LC_ALL=C date +%a)"
  today="${today,,}"
  IFS=',' read -ra tokens <<< "$days"
  for d in "${tokens[@]}"; do
    [[ "$d" == "$today" ]] && return 0
  done
  return 1
}

valid_days() {
  [[ "$1" =~ ^(daily|(mon|tue|wed|thu|fri|sat|sun)(,(mon|tue|wed|thu|fri|sat|sun))*)$ ]]
}

count_cbz() { find "$STAGING_DIR" -name '*.cbz' 2>/dev/null | wc -l; }

# Check every chapter in the holding folder; move the ones that pass into the
# library. Sets moved and held.
release_checked() {
  local f series dest out
  moved=0 held=0
  while IFS= read -r -d '' f; do
    series="$(basename "$(dirname "$f")")"
    dest="$MANGA_LIBRARY/$series/$(basename "$f")"
    if [[ -e "$dest" ]]; then
      log "HELD: $series/$(basename "$f"): a chapter with this name is already in the library"
      held=$((held + 1))
      continue
    fi
    if out="$("$TOMO_BIN" check -library "$MANGA_LIBRARY" "$f" 2>/dev/null)"; then
      mkdir -p "$MANGA_LIBRARY/$series"
      mv -n -- "$f" "$dest"
      moved=$((moved + 1))
    else
      log "HELD: $series/$(basename "$f"): ${out#held: "$f": }"
      held=$((held + 1))
    fi
  done < <(find "$STAGING_DIR" -name '*.cbz' -print0 2>/dev/null | sort -z)
  find "$STAGING_DIR" -mindepth 1 -type d -empty -delete 2>/dev/null || true
}

main() {
  if [[ -z "$MANGA_LIBRARY" ]]; then
    echo "error: MANGA_LIBRARY is not set (export it or add it to .env)" >&2
    exit 1
  fi
  if ! command -v "$TOMO_BIN" >/dev/null 2>&1; then
    echo "error: tomo binary not found: $TOMO_BIN" >&2
    exit 1
  fi
  if [[ ! -f "$SERIES_FILE" ]]; then
    echo "error: series file not found: $SERIES_FILE" >&2
    echo "       create it from series.txt.example" >&2
    exit 1
  fi

  local failures=0 checked=0 skipped=0 new_total=0
  local line entry url days name rest first before after new
  local -a flags
  while IFS= read -r line || [[ -n "$line" ]]; do
    entry="$(trim "${line%%#*}")"   # drop inline comments, then trim
    [[ -z "$entry" ]] && continue
    url="${entry%%[[:space:]]*}"    # field 1: the series URL
    rest="$(trim "${entry#"$url"}")"
    flags=()
    while [[ "$rest" == -* ]]; do   # leading -flags go straight to tomo
      first="${rest%%[[:space:]]*}"
      case "$first" in
        -out|-name|-chapters|-lang|-source|-concurrency|-chapter-workers|-retries)
          log "WARNING: $url: $first needs a value; write it as $first=VALUE"
          ;;
      esac
      flags+=("$first")
      rest="$(trim "${rest#"$first"}")"
    done
    days="" name=""
    if [[ -n "$rest" ]]; then
      first="${rest%%[[:space:]]*}"
      if valid_days "${first,,}"; then
        days="${first,,}"           # field 2: update day(s)
        name="$(trim "${rest#"$first"}")"  # fields 3+: series name override
      else
        log "WARNING: '$first' is not a day tag (mon..sun or daily) — treating '$rest' as the series name, checking daily: $url"
        name="$rest"
      fi
    fi
    if ! due_today "$days"; then
      skipped=$((skipped + 1))
      log "skip (updates $days): $url"
      continue
    fi

    checked=$((checked + 1))
    log "checking: $url"
    before="$(count_cbz)"
    local tomo_args=(-out "$STAGING_DIR" -library "$MANGA_LIBRARY")
    [[ -n "$name" ]] && tomo_args+=(-name "$name")
    tomo_args+=(${flags[@]+"${flags[@]}"})  # bash <4.4 + set -u: empty array is "unbound"
    if "$TOMO_BIN" "${tomo_args[@]}" "$url"; then
      after="$(count_cbz)"
      new=$((after - before))
      new_total=$((new_total + new))
      log "ok: $url ($new downloaded)"
    else
      log "FAILED: $url"
      failures=$((failures + 1))
    fi
  done < "$SERIES_FILE"

  local moved held
  release_checked
  log "check: $moved chapter(s) moved into the library, $held held in $STAGING_DIR"

  # Write the arc guide into new chapters (titles, reading lists, series
  # description). It also picks up guide edits, and only rewrites chapters
  # whose arc info changed, so running it every time is cheap. A failure here
  # (say, the guide can't be fetched) is logged but doesn't fail the run.
  log "arcs: writing the arc guide into the library"
  if ! "$TOMO_BIN" arcs "$MANGA_LIBRARY"/*/; then
    log "WARNING: tomo arcs failed; chapters keep their previous arc info"
  fi

  log "finished: $checked checked, $skipped skipped (not their day), $failures failed, $new_total downloaded, $moved added, $held held"
  [[ "$failures" -eq 0 ]]
}

if [[ -n "${LOG_FILE:-}" ]]; then
  mkdir -p "$(dirname "$LOG_FILE")"
  main >> "$LOG_FILE" 2>&1
else
  main
fi
