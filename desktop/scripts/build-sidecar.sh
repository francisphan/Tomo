#!/usr/bin/env bash
# Build the Go `tomo` CLI as a Tauri sidecar for the host platform.
#
# Tauri expects external binaries named "<name>-<target-triple>[.exe]" under
# src-tauri/binaries/. We derive the triple from rustc and build there.
set -euo pipefail

cd "$(dirname "$0")/.." # -> desktop/

triple="$(rustc -vV | sed -n 's/^host: //p')"
ext=""
case "$triple" in
  *windows*) ext=".exe" ;;
esac

dest="src-tauri/binaries"
mkdir -p "$dest"
out="$dest/tomo-${triple}${ext}"

echo "building tomo sidecar -> $out"
# The Go module is the repo root, one level up from desktop/.
(cd .. && CGO_ENABLED=0 go build -trimpath -ldflags="-s -w" \
  -o "desktop/${out}" ./cmd/tomo)

echo "done: $out"
