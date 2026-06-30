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

skill_src="$repo_dir/skills/no-mistakes-lite/SKILL.md"
skill_dir="$HOME/.agents/skills/no-mistakes-lite"
if [ -f "$skill_src" ]; then
  mkdir -p "$skill_dir"
  install -m 0644 "$skill_src" "$skill_dir/SKILL.md"
fi

printf 'Installed nml to %s\n' "$bindir/nml"
if [ -f "$skill_src" ]; then
  printf 'Installed no-mistakes-lite skill to %s\n' "$skill_dir/SKILL.md"
fi
printf 'Run `%s doctor` to verify setup.\n' "$bindir/nml"
