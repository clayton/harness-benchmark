#!/bin/sh
# Prefix picker tests. Does not download or write a real hb binary.
set -eu
ROOT=$(CDPATH= cd -- "$(dirname -- "$0")/.." && pwd)
INSTALL="$ROOT/install.sh"
fail=0

assert_eq() {
  got=$1
  want=$2
  msg=$3
  if [ "$got" != "$want" ]; then
    echo "FAIL $msg"
    echo "  got  $got"
    echo "  want $want"
    fail=1
  else
    echo "ok   $msg"
  fi
}

scratch=$(mktemp -d)
trap 'rm -rf "$scratch"' EXIT
HOME="$scratch/home"
mkdir -p "$HOME"

brew="$scratch/opt/homebrew/bin"
localbin="$HOME/.local/bin"
usrlocal="$scratch/usr/local/bin"
mkdir -p "$brew" "$localbin" "$usrlocal"

# Homebrew bin is on PATH and writable: pick it.
out=$(HOME="$HOME" PATH="$brew:/usr/bin:/bin" HB_CANDIDATES="$brew:$usrlocal:$localbin" HB_PRINT_PREFIX=1 sh "$INSTALL")
assert_eq "$(echo "$out" | sed -n '1p')" "$brew" "prefers Homebrew bin already on PATH"
assert_eq "$(echo "$out" | sed -n '2p')" "need_path=0" "Homebrew pick does not need PATH export"

# /usr/local/bin on PATH, no Homebrew: pick it.
out=$(HOME="$HOME" PATH="$usrlocal:/usr/bin:/bin" HB_CANDIDATES="$brew:$usrlocal:$localbin" HB_PRINT_PREFIX=1 sh "$INSTALL")
assert_eq "$(echo "$out" | sed -n '1p')" "$usrlocal" "prefers /usr/local/bin when on PATH"
assert_eq "$(echo "$out" | sed -n '2p')" "need_path=0" "usr/local pick does not need PATH export"

# ~/.local/bin already on PATH: pick it, no export.
out=$(HOME="$HOME" PATH="$localbin:/usr/bin:/bin" HB_CANDIDATES="$brew:$usrlocal:$localbin" HB_PRINT_PREFIX=1 sh "$INSTALL")
assert_eq "$(echo "$out" | sed -n '1p')" "$localbin" "uses ~/.local/bin when it is already on PATH"
assert_eq "$(echo "$out" | sed -n '2p')" "need_path=0" "existing local bin does not need PATH export"

# Nothing friendly on PATH: fall back to ~/.local/bin and flag PATH.
out=$(HOME="$HOME" PATH="/usr/bin:/bin" HB_CANDIDATES="$brew:$usrlocal" HB_PRINT_PREFIX=1 sh "$INSTALL")
assert_eq "$(echo "$out" | sed -n '1p')" "$localbin" "falls back to ~/.local/bin"
assert_eq "$(echo "$out" | sed -n '2p')" "need_path=1" "fallback asks for a one-time PATH export"

# Explicit override still wins.
out=$(HOME="$HOME" PATH="/usr/bin:/bin" HB_PREFIX="$scratch/custom" HB_PRINT_PREFIX=1 sh "$INSTALL")
assert_eq "$(echo "$out" | sed -n '1p')" "$scratch/custom" "HB_PREFIX wins"
assert_eq "$(echo "$out" | sed -n '2p')" "need_path=1" "override off PATH needs export"

if [ "$fail" -ne 0 ]; then
  exit 1
fi
echo "all prefix tests passed"
