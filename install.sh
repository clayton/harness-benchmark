#!/bin/sh
# Install the hb Go binary onto PATH. No Python, no venv.
set -eu

PREFIX="${HB_PREFIX:-$HOME/.local/bin}"
REPO="https://github.com/clayton/harness-benchmark"
OS=$(uname -s | tr '[:upper:]' '[:lower:]')
ARCH=$(uname -m)
case "$ARCH" in
  x86_64) ARCH=amd64 ;;
  aarch64|arm64) ARCH=arm64 ;;
esac

mkdir -p "$PREFIX"
BIN="$PREFIX/hb"

install_from_release() {
  url="$REPO/releases/latest/download/hb-${OS}-${ARCH}"
  if command -v curl >/dev/null 2>&1; then
    curl -fsSL "$url" -o "$BIN" || return 1
  else
    wget -qO "$BIN" "$url" || return 1
  fi
  chmod +x "$BIN"
}

install_from_source() {
  if ! command -v go >/dev/null 2>&1; then
    echo "hb: no prebuilt binary and go is not installed" >&2
    return 1
  fi
  here=$(CDPATH= cd -- "$(dirname -- "$0")" && pwd)
  if [ -f "$here/cmd/hb/main.go" ]; then
    (cd "$here" && go build -mod=mod -o "$BIN" ./cmd/hb)
    return
  fi
  tmp=$(mktemp -d)
  trap 'rm -rf "$tmp"' EXIT
  git clone --depth 1 "$REPO.git" "$tmp/src"
  (cd "$tmp/src" && go build -mod=mod -o "$BIN" ./cmd/hb)
}

if ! install_from_release; then
  echo "hb: no GitHub release binary, building from source…"
  install_from_source
fi

echo "installed $BIN"
if ! echo ":$PATH:" | grep -q ":$PREFIX:"; then
  echo "add to PATH:  export PATH=\"$PREFIX:\$PATH\""
fi
echo "next:  hb"
