#!/usr/bin/env bash
# refresh-library.sh — fetch new chapters for a list of ongoing series into the
# Komga library. Built to run unattended from cron.
#
# Tomo only writes chapters whose CBZ doesn't already exist, so every pass is
# cheap and idempotent — it just appends whatever is new, then Komga picks the
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
#   MANGA_LIBRARY   output dir Komga serves from (required; same as `tomo -out`)
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

count_cbz() { find "$MANGA_LIBRARY" -name '*.cbz' 2>/dev/null | wc -l; }

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
    local tomo_args=(-out "$MANGA_LIBRARY")
    [[ -n "$name" ]] && tomo_args+=(-name "$name")
    tomo_args+=("${flags[@]}")
    if "$TOMO_BIN" "${tomo_args[@]}" "$url"; then
      after="$(count_cbz)"
      new=$((after - before))
      new_total=$((new_total + new))
      log "ok: $url ($new new)"
    else
      log "FAILED: $url"
      failures=$((failures + 1))
    fi
  done < "$SERIES_FILE"

  log "finished: $checked checked, $skipped skipped (not their day), $failures failed, $new_total new chapters"
  [[ "$failures" -eq 0 ]]
}

if [[ -n "${LOG_FILE:-}" ]]; then
  mkdir -p "$(dirname "$LOG_FILE")"
  main >> "$LOG_FILE" 2>&1
else
  main
fi
