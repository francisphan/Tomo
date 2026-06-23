#!/usr/bin/env bash
# refresh-library.sh — fetch new chapters for a list of ongoing series into the
# Komga library. Built to run unattended from cron.
#
# Tomo only writes chapters whose CBZ doesn't already exist, so every pass is
# cheap and idempotent — it just appends whatever is new, then Komga picks the
# files up on its next scan.
#
# Usage:
#   ./refresh-library.sh                     # uses series.txt next to this script
#   SERIES_FILE=/path/to/list ./refresh-library.sh
#
# Config (environment variables, or a .env file in this folder):
#   MANGA_LIBRARY   output dir Komga serves from (required; same as `tomo -out`)
#   TOMO_BIN        path to the tomo binary            (default: tomo on PATH)
#   SERIES_FILE     newline-delimited series URLs,
#                   blank lines and # comments ignored (default: ./series.txt)
#   LOG_FILE        append run output here             (default: stdout)

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

log() { printf '%s %s\n' "$(date '+%Y-%m-%d %H:%M:%S')" "$*"; }

# Trim leading/trailing whitespace without the word-splitting xargs would do
# (series URLs can contain characters xargs would mangle).
trim() {
  local s="$1"
  s="${s#"${s%%[![:space:]]*}"}"
  s="${s%"${s##*[![:space:]]}"}"
  printf '%s' "$s"
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

  local failures=0 count=0 line url
  while IFS= read -r line || [[ -n "$line" ]]; do
    url="$(trim "${line%%#*}")"   # drop inline comments, then trim
    [[ -z "$url" ]] && continue
    count=$((count + 1))
    log "refreshing: $url"
    if "$TOMO_BIN" -out "$MANGA_LIBRARY" "$url"; then
      log "ok: $url"
    else
      log "FAILED: $url"
      failures=$((failures + 1))
    fi
  done < "$SERIES_FILE"

  log "finished: $count series processed, $failures failed"
  [[ "$failures" -eq 0 ]]
}

if [[ -n "${LOG_FILE:-}" ]]; then
  main >> "$LOG_FILE" 2>&1
else
  main
fi
