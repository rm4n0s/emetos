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

# copy_extension <extension folder under reference/vscode/extensions>
# Copies what the language registry reads of a VS Code extension (SPEC §8.1) into
# internal/core/language/assets/<name>/: package.json, the language configurations,
# the grammars (syntaxes/) and cgmanifest.json, which names the upstream of each
# grammar. The build scripts, snippets and translations stay behind.
copy_extension() {
  local name=$1 src="$ref/vscode/extensions/$1" dst="$root/internal/core/language/assets/$1"
  [[ -d "$src" ]] || { echo "error: reference/vscode/extensions/$name is not a folder" >&2; exit 1; }
  rm -rf "$dst"
  mkdir -p "$dst"
  cp -a "$src/package.json" "$src/cgmanifest.json" "$src/syntaxes" "$dst/"
  local f
  for f in "$src"/language-configuration*.json; do
    [[ -f "$f" ]] && cp -a "$f" "$dst/"
  done
  chmod -R u+rw,go+r "$dst"
}

# ---- Assets, by the step that adds them -------------------------------------

# Step 0.4: the icon fonts, with the Codicon name table the generator reads.
copy_file codicons/package/dist/codicon.ttf internal/gui/kit/fonts/codicon.ttf
copy_file codicons/package/dist/codicon.csv internal/gui/kit/fonts/codicon.csv
copy_license codicons codicons/package/LICENSE codicons/package/LICENSE-CODE
copy_file vscode/extensions/theme-seti/icons/seti.woff internal/gui/kit/fonts/seti.woff
copy_license seti vscode/extensions/theme-seti/ThirdPartyNotices.txt vscode/LICENSE.txt

# Step 4.6: the first languages. The grammars of these extensions come from the upstream
# projects named in their cgmanifest.json, all permissive (MIT, and the TextMate bundle
# license); the texts are in VS Code's ThirdPartyNotices.txt.
for ext in go json yaml markdown-basics; do
  copy_extension "$ext"
done
copy_license vscode-extensions vscode/LICENSE.txt vscode/ThirdPartyNotices.txt

echo "update-assets: done"
