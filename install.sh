#!/bin/sh
# Pinned installer for hb. Downloads a tagged release, checks SHA-256, then
# puts the binary in a directory already on PATH when it can.
# Read this file before piping it to sh.
set -eu

TAG="v0.2.4"
REPO="https://github.com/clayton/harness-benchmark"
OS=$(uname -s | tr '[:upper:]' '[:lower:]')
ARCH=$(uname -m)
case "$ARCH" in
  x86_64) ARCH=amd64 ;;
  aarch64|arm64) ARCH=arm64 ;;
esac

NAME="hb-${OS}-${ARCH}"
NEED_PATH=0
OTHER_HB=""

on_path() {
  case ":$PATH:" in
    *":$1:"*) return 0 ;;
    *) return 1 ;;
  esac
}

can_write() {
  dir=$1
  if [ -d "$dir" ]; then
    [ -w "$dir" ]
    return
  fi
  parent=$dir
  while [ "$parent" != "/" ] && [ "$parent" != "." ] && [ -n "$parent" ]; do
    parent=$(dirname "$parent")
    if [ -d "$parent" ]; then
      [ -w "$parent" ]
      return
    fi
  done
  return 1
}

is_our_hb() {
  bin=$1
  [ -x "$bin" ] || return 1
  "$bin" version 2>/dev/null | grep -q '(go)'
}

# Refuse to overwrite Honeybadger's `hb` or any other namesake.
slot_free() {
  dest="$1/hb"
  if [ -e "$dest" ] || [ -L "$dest" ]; then
    is_our_hb "$dest"
    return
  fi
  return 0
}

pick_prefix() {
  if [ -n "${HB_PREFIX:-}" ]; then
    PREFIX=$HB_PREFIX
    if on_path "$PREFIX"; then
      NEED_PATH=0
    else
      NEED_PATH=1
    fi
    return
  fi

  if [ -n "${HB_CANDIDATES:-}" ]; then
    oldifs=$IFS
    IFS=:
    # shellcheck disable=SC2086
    set -- $HB_CANDIDATES
    IFS=$oldifs
  else
    set -- \
      /opt/homebrew/bin \
      /usr/local/bin \
      "$HOME/.local/bin" \
      "$HOME/bin"
  fi

  for dir in "$@"; do
    [ -n "$dir" ] || continue
    on_path "$dir" || continue
    can_write "$dir" || continue
    slot_free "$dir" || continue
    PREFIX=$dir
    break
  done

  if [ -z "${PREFIX:-}" ]; then
    PREFIX="${HOME}/.local/bin"
  fi

  existing=$(command -v hb 2>/dev/null || true)
  if [ -n "$existing" ] && [ "$existing" != "$PREFIX/hb" ] && ! is_our_hb "$existing"; then
    OTHER_HB=$existing
  fi
  if on_path "$PREFIX" && [ -z "$OTHER_HB" ]; then
    NEED_PATH=0
  else
    # Prepend so a namesake earlier on PATH (Homebrew Honeybadger) does not win.
    NEED_PATH=1
  fi
}

pick_prefix

if [ "${HB_PRINT_PREFIX:-}" = "1" ]; then
  echo "$PREFIX"
  echo "need_path=$NEED_PATH"
  exit 0
fi

mkdir -p "$PREFIX"
BIN="$PREFIX/hb"

sha256_of() {
  if command -v openssl >/dev/null 2>&1; then
    openssl dgst -sha256 "$1" | awk '{print $NF}'
    return
  fi
  if command -v shasum >/dev/null 2>&1; then
    shasum -a 256 "$1" | awk '{print $1}'
    return
  fi
  if command -v sha256sum >/dev/null 2>&1; then
    sha256sum "$1" | awk '{print $1}'
    return
  fi
  echo "hb: need openssl, shasum, or sha256sum to verify $1" >&2
  return 1
}

fetch() {
  url=$1
  dest=$2
  if command -v curl >/dev/null 2>&1; then
    curl -fsSL "$url" -o "$dest"
  else
    wget -qO "$dest" "$url"
  fi
}

install_from_release() {
  tmp=$(mktemp -d)
  trap 'rm -rf "$tmp"' EXIT
  fetch "$REPO/releases/download/$TAG/SHA256SUMS" "$tmp/SHA256SUMS" || return 1
  fetch "$REPO/releases/download/$TAG/$NAME" "$tmp/$NAME" || return 1
  want=$(awk -v n="$NAME" '$2 == n { print $1; found=1 } END { exit !found }' "$tmp/SHA256SUMS") || return 1
  got=$(sha256_of "$tmp/$NAME") || return 1
  if [ "$want" != "$got" ]; then
    echo "hb: checksum mismatch for $NAME" >&2
    echo "hb: want $want" >&2
    echo "hb: got  $got" >&2
    return 1
  fi
  cp "$tmp/$NAME" "$BIN"
  chmod +x "$BIN"
  echo "verified SHA-256 $got"
}

install_from_source() {
  if ! command -v go >/dev/null 2>&1; then
    echo "hb: no pinned $TAG binary and go is not installed" >&2
    return 1
  fi
  here=$(CDPATH= cd -- "$(dirname -- "$0")" && pwd)
  if [ -f "$here/cmd/hb/main.go" ]; then
    (cd "$here" && go build -mod=mod -o "$BIN" ./cmd/hb)
    echo "built from local source (no release checksum)"
    return
  fi
  tmp=$(mktemp -d)
  trap 'rm -rf "$tmp"' EXIT
  git clone --depth 1 --branch "$TAG" "$REPO.git" "$tmp/src"
  (cd "$tmp/src" && go build -mod=mod -o "$BIN" ./cmd/hb)
  echo "built $TAG from source (no release checksum)"
}

rc_file() {
  case $(basename "${SHELL:-sh}") in
    zsh) echo "$HOME/.zshrc" ;;
    bash)
      if [ -f "$HOME/.bashrc" ]; then
        echo "$HOME/.bashrc"
      else
        echo "$HOME/.bash_profile"
      fi
      ;;
    *) echo "$HOME/.profile" ;;
  esac
}

persist_path() {
  if [ "${HB_SKIP_PATH_RC:-}" = "1" ]; then
    return
  fi
  rc=$(rc_file)
  marker="# hb (harness-benchmark)"
  if [ -f "$rc" ] && grep -F "$PREFIX" "$rc" >/dev/null 2>&1; then
    return
  fi
  {
    echo ""
    echo "$marker"
    echo "export PATH=\"$PREFIX:\$PATH\""
  } >>"$rc"
}

if ! install_from_release; then
  echo "hb: pinned $TAG download failed, building from source…"
  install_from_source
fi

ver="unknown"
if [ -x "$BIN" ]; then
  ver=$("$BIN" version 2>/dev/null || echo unknown)
fi
echo "installed $ver -> $BIN"
if [ "$NEED_PATH" -eq 1 ]; then
  echo "this terminal: export PATH=\"$PREFIX:\$PATH\""
  if [ -n "$OTHER_HB" ]; then
    echo "note: another hb is already $OTHER_HB; that export makes this one win"
  fi
  persist_path
else
  echo "on PATH already"
fi
echo "next: hb"
