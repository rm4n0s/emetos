#!/usr/bin/env bash
# Copies the assets that Emetos bundles out of reference/ into the packages that
# embed them, and their licenses into third_party/<name>/ (SPEC §17).
#
# Usage: scripts/update-assets.sh
#
# Run scripts/fetch-reference.sh first. The script is idempotent: a destination
# folder is replaced, so a file removed upstream is removed here. Never edit a
# copied asset by hand; change this script and run it again.
#
# Each step of docs/IMPLEMENTATION.md that bundles assets adds a block at the end
# of this file (the step id is in a comment above its block).
set -euo pipefail

root="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
ref="$root/reference"

if [[ ! -d "$ref" ]]; then
  echo "error: reference/ is missing; run scripts/fetch-reference.sh" >&2
  exit 1
fi

# copy_file <path under reference/> <destination under the repository>
copy_file() {
  local src="$ref/$1" dst="$root/$2"
  [[ -f "$src" ]] || { echo "error: reference/$1 is not a file" >&2; exit 1; }
  mkdir -p "$(dirname "$dst")"
  cp -f "$src" "$dst"
  chmod 0644 "$dst"
}

# copy_tree <folder under reference/> <destination folder under the repository>
copy_tree() {
  local src="$ref/$1" dst="$root/$2"
  [[ -d "$src" ]] || { echo "error: reference/$1 is not a folder" >&2; exit 1; }
  rm -rf "$dst"
  mkdir -p "$dst"
  cp -a "$src/." "$dst/"
  chmod -R u+rw,go+r "$dst"
}

# copy_license <third_party name> <path under reference/> [<path> ...]
# Copies license files into third_party/<name>/ under their own names.
copy_license() {
  local name=$1
  shift
  local p
  for p in "$@"; do
    copy_file "$p" "third_party/$name/$(basename "$p")"
  done
}

# ---- Assets, by the step that adds them -------------------------------------

echo "update-assets: no assets yet"
