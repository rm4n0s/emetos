#!/usr/bin/env bash
# Checks that every reference/... path named in docs/IMPLEMENTATION.md and
# docs/SPEC.md exists in reference/ (run scripts/fetch-reference.sh first).
#
# Usage: scripts/check-references.sh
#
# A path is a match of reference/<source>/<chars>, where <source> is one of the
# sources in scripts/reference-versions.txt and <chars> are letters, digits and
# _ . / { } , * @ + - . A trailing , . or : is dropped (prose punctuation).
# {a,b} lists and * globs are expanded by the shell; a pattern that matches
# nothing is missing. Exit status: 0 all there, 1 some missing.
set -euo pipefail

root="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
cd "$root"

if [[ ! -d reference ]]; then
  echo "error: reference/ is missing; run scripts/fetch-reference.sh" >&2
  exit 1
fi

missing=0
checked=0
while read -r pattern; do
  pattern="${pattern%%[,.:]}"
  while [[ "$pattern" == *[,.:] ]]; do pattern="${pattern%[,.:]}"; done
  [[ -z "$pattern" ]] && continue
  # eval expands {a,b} and * ; the pattern holds only whitelisted characters.
  while read -r path; do
    checked=$((checked + 1))
    if [[ "$path" == *"*"* || ! -e "$path" ]]; then
      echo "missing: $path"
      missing=$((missing + 1))
    fi
  done < <(eval "printf '%s\n' $pattern")
done < <(grep -ohE 'reference/(vscode|vscode-textmate|emmet|vscode-go|codicons)/[A-Za-z0-9_./{},*@+-]*' \
  docs/IMPLEMENTATION.md docs/SPEC.md | sort -u)

if ((missing > 0)); then
  echo "$missing of $checked reference paths are missing" >&2
  exit 1
fi
echo "all $checked reference paths exist"
