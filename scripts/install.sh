#!/usr/bin/env sh
set -eu

prefix="${PREFIX:-$HOME/.local}"
bindir="$prefix/bin"
mkdir -p "$bindir"

tmpdir="$(mktemp -d)"
trap 'rm -rf "$tmpdir"' EXIT INT TERM

repo_dir="$(CDPATH= cd -- "$(dirname -- "$0")/.." && pwd)"

printf 'Building nml...\n' >&2
(
  cd "$repo_dir"
  go build -trimpath -o "$tmpdir/nml" ./cmd/nml
)

install -m 0755 "$tmpdir/nml" "$bindir/nml"

printf 'Installed nml to %s\n' "$bindir/nml"
printf 'Run `%s doctor` to verify setup.\n' "$bindir/nml"
