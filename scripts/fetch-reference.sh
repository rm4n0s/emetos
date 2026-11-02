#!/usr/bin/env bash
# Fetches the pinned reference sources into reference/ (SPEC §17).
#
# Usage: scripts/fetch-reference.sh
#
# Reads scripts/reference-versions.txt:
#   git <owner/repo> <version> <commit>   a sparse, shallow checkout of that commit
#   npm <package>   <version> <tarball>   an npm package unpacked into reference/<name>/
#   files <owner/name> <version> <base url>   three files of a release tag, fetched one by one into reference/<name>/ (lldb-dap)
#   vsix <owner/name> <version> "<sha256> <url>"   the package.json, package.nls.json and license of an extension package of a release (a
#                                             .vsix, a zip), unpacked into reference/<name>/ after its sha256 was compared (vscode-js-debug)
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
    microsoft/vscode-python-debugger) echo "src/extension/debugger" ;;
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

# fetch_files <name> <version> <base url>: LLDB's DAP server is not a repository of its own; its package.json (the attributes of the
# debugger), its README and the license of LLVM are fetched from the tag of llvm-project (step 25.9).
fetch_files() {
  local name=$1 version=$2 base=$3
  local short="${name##*/}"
  local dir="$ref/$short"
  if [[ -f "$dir/.version" && "$(cat "$dir/.version")" == "$version" ]]; then
    echo "reference/$short: $version, already there"
    return
  fi
  echo "reference/$short: fetching $version"
  rm -rf "$dir"
  mkdir -p "$dir"
  curl -fsSL "$base/lldb/tools/lldb-dap/package.json" -o "$dir/package.json"
  curl -fsSL "$base/lldb/tools/lldb-dap/README.md" -o "$dir/README.md"
  curl -fsSL "$base/llvm/LICENSE.TXT" -o "$dir/LICENSE.TXT"
  echo "$version" >"$dir/.version"
}

# fetch_vsix <name> <version> "<sha256> <url>": the contributions of vscode-js-debug are generated when it is built
# (src/build/generate-contributions.ts) and exist only in the package.json of its release package; that file, its translations and the
# license are taken from the .vsix of the release (step 25.10).
fetch_vsix() {
  local name=$1 version=$2 source=$3
  local sha="${source%% *}" url="${source#* }"
  local short="${name##*/}"
  local dir="$ref/$short"
  if [[ -f "$dir/.version" && "$(cat "$dir/.version")" == "$version" ]]; then
    echo "reference/$short: $version, already there"
    return
  fi
  echo "reference/$short: fetching $version"
  local tmp
  tmp="$(mktemp -d)"
  curl -fsSL --max-time 600 -o "$tmp/package.vsix" "$url"
  [[ "$(sha256sum "$tmp/package.vsix" | cut -d' ' -f1)" == "$sha" ]] || { echo "error: reference/$short: the download does not match the pinned sha256" >&2; rm -rf "$tmp"; exit 1; }
  rm -rf "$dir"
  mkdir -p "$dir"
  unzip -q -j "$tmp/package.vsix" extension/package.json extension/package.nls.json extension/LICENSE.txt -d "$dir"
  rm -rf "$tmp"
  echo "$version" >"$dir/.version"
}

mkdir -p "$ref"
printf 'module reference\n\ngo 1.27\n' >"$ref/go.mod"

while read -r kind name version source; do
  case "$kind" in
    "" | "#"*) continue ;;
    git) fetch_git "$name" "$version" "$source" ;;
    npm) fetch_npm "$name" "$version" "$source" ;;
    files) fetch_files "$name" "$version" "$source" ;;
    vsix) fetch_vsix "$name" "$version" "$source" ;;
    *)
      echo "error: unknown kind '$kind' in $versions" >&2
      exit 1
      ;;
  esac
done <"$versions"
echo "reference/ is ready"
