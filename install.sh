#!/bin/sh
# Pinned installer for hbench. Downloads a tagged release, checks SHA-256, then
# puts the binary in a directory already on PATH when it can.
# Read this file before piping it to sh.
set -eu

TAG="v0.7.2"
REPO="https://github.com/clayton/harness-benchmark"
OS=$(uname -s | tr '[:upper:]' '[:lower:]')
ARCH=$(uname -m)
case "$ARCH" in
  x86_64) ARCH=amd64 ;;
  aarch64|arm64) ARCH=arm64 ;;
esac

NAME="hbench-${OS}-${ARCH}"
NEED_PATH=0
OTHER_HB=""

# Pinned in this script. GitHub hosts the binary; we do not fetch SUMS from it.
expected_sha() {
  case $1 in
    hbench-darwin-amd64) echo "920a1dc61dad47953b25f5fc3c6a60dc00eb561b64767d537814af086f977a48" ;;
    hbench-darwin-arm64) echo "f2f30c9385b7f7e75ac7b0dc054fd37da56cb746ad7c994d836b3c19f642c140" ;;
    hbench-linux-amd64) echo "8c0948430528c48d910a6a7241346d0cf6644c70bd53ac5a2eaf6c07a4235ae9" ;;
    hbench-linux-arm64) echo "2c282d1757b211f203808ba572c77b674c8caf4b8376d9de995b0322b289a471" ;;
    *) return 1 ;;
  esac
}

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

# Refuse to overwrite another program named hbench.
slot_free() {
  dest="$1/hbench"
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

  existing=$(command -v hbench 2>/dev/null || true)
  if [ -n "$existing" ] && [ "$existing" != "$PREFIX/hbench" ] && ! is_our_hb "$existing"; then
    OTHER_HB=$existing
  fi
  if on_path "$PREFIX" && [ -z "$OTHER_HB" ]; then
    NEED_PATH=0
  else
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
BIN="$PREFIX/hbench"

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
  echo "hbench: need openssl, shasum, or sha256sum to verify $1" >&2
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
  want=$(expected_sha "$NAME") || return 1
  tmp=$(mktemp -d)
  trap 'rm -rf "$tmp"' EXIT
  fetch "$REPO/releases/download/$TAG/$NAME" "$tmp/$NAME" || return 1
  got=$(sha256_of "$tmp/$NAME") || return 1
  if [ "$want" != "$got" ]; then
    echo "hbench: checksum mismatch for $NAME" >&2
    echo "hbench: want $want" >&2
    echo "hbench: got  $got" >&2
    return 1
  fi
  cp "$tmp/$NAME" "$BIN"
  chmod +x "$BIN"
  echo "verified SHA-256 $got"
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
  marker="# hbench (harness-benchmark)"
  if [ -f "$rc" ] && grep -F "$PREFIX" "$rc" >/dev/null 2>&1; then
    echo "new terminals: $PREFIX is already listed in $rc"
    return
  fi
  {
    echo ""
    echo "$marker"
    echo "export PATH=\"$PREFIX:\$PATH\""
  } >>"$rc"
  echo "new terminals: appended PATH to $rc"
}

if ! install_from_release; then
  echo "hbench: verified $TAG installation failed; nothing was installed" >&2
  exit 1
fi

ver="unknown"
if [ -x "$BIN" ]; then
  ver=$("$BIN" version 2>/dev/null || echo unknown)
fi
echo "installed $ver -> $BIN"
if [ "$NEED_PATH" -eq 1 ]; then
  if [ -n "$OTHER_HB" ]; then
    echo "note: another hbench is already $OTHER_HB"
  fi
  persist_path
  echo "this terminal: export PATH=\"$PREFIX:\$PATH\" && hbench"
else
  echo "on PATH already"
  echo "next: hbench"
fi
