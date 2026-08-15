#!/bin/sh
# Build pinned hb binaries and SHA256SUMS for a GitHub release.
# Usage: scripts/release.sh v0.4.2
set -eu

TAG=${1:?usage: scripts/release.sh v0.4.2}
ROOT=$(CDPATH= cd -- "$(dirname -- "$0")/.." && pwd)
OUT=${HB_RELEASE_DIR:-"$ROOT/dist/$TAG"}
mkdir -p "$OUT"

cd "$ROOT"
for pair in darwin/arm64 darwin/amd64 linux/amd64 linux/arm64; do
  os=${pair%/*}
  arch=${pair#*/}
  echo "building hbench-${os}-${arch}"
  CGO_ENABLED=0 GOOS="$os" GOARCH="$arch" go build -mod=mod -trimpath -ldflags="-s -w" -o "$OUT/hbench-${os}-${arch}" ./cmd/hb
done

(
  cd "$OUT"
  if command -v shasum >/dev/null 2>&1; then
    shasum -a 256 hbench-*
  else
    sha256sum hbench-*
  fi
) >"$OUT/SHA256SUMS"

echo "assets in $OUT"
cat "$OUT/SHA256SUMS"
echo "next: gh release create $TAG --title \"hbench ${TAG#v}\" $OUT/hbench-* $OUT/SHA256SUMS"
