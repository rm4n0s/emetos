#!/usr/bin/env bash
# Fetches the pinned reference sources into reference/ (SPEC §17).
#
# Usage: scripts/fetch-reference.sh
#
# Reads scripts/reference-versions.txt:
#   git <owner/repo> <version> <commit>   a sparse, shallow checkout of that commit
#   npm <package>   <version> <tarball>   an npm package unpacked into reference/<name>/
#
# reference/ is git-ignored and is a Go module of its own (reference/go.mod), so
# VS Code's Go test files stay out of ./... . Running the script again is cheap: a
# source that is already at its pinned version is left alone.
set -euo pipefail

root="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
versions="$root/scripts/reference-versions.txt"
ref="$root/reference"

# sparse_dirs prints the folders to check out for a repository (cone mode: the
# files in the repository root, such as LICENSE, always come with them).
sparse_dirs() {
  case "$1" in
    microsoft/vscode) echo "src/vs extensions" ;;
    golang/vscode-go) echo "extension/syntaxes extension/languages" ;;
    *) echo "" ;;
  esac
}

fetch_git() {
  local name=$1 version=$2 commit=$3
  local dir="$ref/$(basename "$name")"
  if [[ -d "$dir/.git" && "$(git -C "$dir" rev-parse HEAD 2>/dev/null || true)" == "$commit" ]]; then
    echo "reference/$(basename "$name"): $version ($commit), already there"
    return
  fi
  echo "reference/$(basename "$name"): fetching $version ($commit)"
  rm -rf "$dir"
  mkdir -p "$dir"
  git -C "$dir" init -q
  git -C "$dir" remote add origin "https://github.com/$name.git"
  git -C "$dir" config extensions.partialClone origin
  git -C "$dir" config remote.origin.promisor true
  git -C "$dir" config remote.origin.partialclonefilter blob:none
  local dirs
  dirs="$(sparse_dirs "$name")"
  if [[ -n "$dirs" ]]; then
    # shellcheck disable=SC2086
    git -C "$dir" sparse-checkout set --cone $dirs
  fi
  git -C "$dir" fetch -q --depth 1 --filter=blob:none origin "$commit"
  git -C "$dir" -c advice.detachedHead=false checkout -q FETCH_HEAD
  if [[ "$(git -C "$dir" rev-parse HEAD)" != "$commit" ]]; then
    echo "error: reference/$(basename "$name") is not at $commit" >&2
    exit 1
  fi
}

fetch_npm() {
  local name=$1 version=$2 url=$3
  local short="${name##*/}"
  local dir="$ref/$short"
  if [[ -f "$dir/.version" && "$(cat "$dir/.version")" == "$version" ]]; then
    echo "reference/$short: $version, already there"
    return
  fi
  echo "reference/$short: fetching $name $version"
  rm -rf "$dir"
  mkdir -p "$dir"
  curl -fsSL "$url" | tar -xz -C "$dir"
  echo "$version" >"$dir/.version"
}

mkdir -p "$ref"
printf 'module reference\n\ngo 1.27\n' >"$ref/go.mod"

while read -r kind name version source; do
  case "$kind" in
    "" | "#"*) continue ;;
    git) fetch_git "$name" "$version" "$source" ;;
    npm) fetch_npm "$name" "$version" "$source" ;;
    *)
      echo "error: unknown kind '$kind' in $versions" >&2
      exit 1
      ;;
  esac
done <"$versions"
echo "reference/ is ready"
