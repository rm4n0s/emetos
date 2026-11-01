#!/usr/bin/env bash
# Fetches a pinned tool of the real-server tests of milestone M24b into /var/tmp/emetos-tools/ (never into the user's own folders).
#
# Usage: scripts/fetch-tools.sh <name>...
#
# Reads scripts/tool-versions.txt:
#   gzbin <name> <version> <sha256 of the download> <url> <sha256 of the program>   gunzipped into /var/tmp/emetos-tools/bin/<name>
#   file  <name> <version> <sha256 of the download> <url>                           kept as /var/tmp/emetos-tools/download/<name>-<version>
# A download whose sha256 differs from the pin is refused. A tool that is there already with the right sha256 is left alone; the
# path of what was installed is printed. The tests of the packs skip when their program is not there (docs/IMPLEMENTATION.md "M24b").
set -euo pipefail

root="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
versions="$root/scripts/tool-versions.txt"
tools="${EMETOS_TOOLS:-/var/tmp/emetos-tools}"

[[ $# -ge 1 ]] || { echo "usage: $0 <name>..." >&2; exit 64; }
mkdir -p "$tools/bin" "$tools/download"

for want in "$@"; do
  found=0
  while read -r kind name version sha url extra; do
    [[ -z "${kind:-}" || "$kind" == \#* || "$name" != "$want" ]] && continue
    found=1
    case "$kind" in
      gzbin)
        dst="$tools/bin/$name"
        if [[ -x "$dst" && "$(sha256sum "$dst" | cut -d' ' -f1)" == "$extra" ]]; then
          echo "$dst"
          continue
        fi
        tmp="$(mktemp -d "$tools/download/.fetch.XXXXXX")"
        curl -fsSL --max-time 600 -o "$tmp/download.gz" "$url"
        [[ "$(sha256sum "$tmp/download.gz" | cut -d' ' -f1)" == "$sha" ]] || { echo "error: $name: the download does not match the pinned sha256" >&2; rm -rf "$tmp"; exit 1; }
        gunzip -c "$tmp/download.gz" >"$tmp/$name"
        [[ "$(sha256sum "$tmp/$name" | cut -d' ' -f1)" == "$extra" ]] || { echo "error: $name: the program does not match the pinned sha256" >&2; rm -rf "$tmp"; exit 1; }
        chmod 0755 "$tmp/$name"
        mv -f "$tmp/$name" "$dst"
        cp -f "$tmp/download.gz" "$tools/download/$name-$version.gz"
        rm -rf "$tmp"
        echo "$dst"
        ;;
      file)
        dst="$tools/download/$name-$version"
        if [[ -f "$dst" && "$(sha256sum "$dst" | cut -d' ' -f1)" == "$sha" ]]; then
          echo "$dst"
          continue
        fi
        tmp="$(mktemp -d "$tools/download/.fetch.XXXXXX")"
        curl -fsSL --max-time 600 -o "$tmp/file" "$url"
        [[ "$(sha256sum "$tmp/file" | cut -d' ' -f1)" == "$sha" ]] || { echo "error: $name: the download does not match the pinned sha256" >&2; rm -rf "$tmp"; exit 1; }
        mv -f "$tmp/file" "$dst"
        rm -rf "$tmp"
        echo "$dst"
        ;;
      *)
        echo "error: $versions: unknown kind $kind" >&2
        exit 1
        ;;
    esac
  done <"$versions"
  [[ $found -eq 1 ]] || { echo "error: $want is not in scripts/tool-versions.txt" >&2; exit 1; }
done
