#!/usr/bin/env bash
# Cross-compile the tomo CLI for every release target into dist/.
#
# Pure-Go build (CGO disabled), so cross-compilation needs no C toolchain.
# Requires a reasonably recent Go (>= 1.21 recommended; the darwin/arm64 target
# fails to build on the very old Go 1.18 packaged on some distros).
set -euo pipefail

cd "$(dirname "$0")/.."

VERSION="${1:-dev}"
OUT="dist"
rm -rf "$OUT"
mkdir -p "$OUT"

# GOOS GOARCH pairs to build.
targets=(
  "darwin amd64"
  "darwin arm64"
  "windows amd64"
  "linux amd64"
)

for t in "${targets[@]}"; do
  read -r goos goarch <<<"$t"
  ext=""
  [ "$goos" = "windows" ] && ext=".exe"
  out="$OUT/tomo-${goos}-${goarch}${ext}"
  echo "building $out"
  CGO_ENABLED=0 GOOS="$goos" GOARCH="$goarch" \
    go build -trimpath -ldflags="-s -w -X main.version=${VERSION}" \
    -o "$out" ./cmd/tomo
done

echo
echo "Done. Artifacts in $OUT/:"
ls -1 "$OUT"
