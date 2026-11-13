#!/usr/bin/env bash
# make-repo.sh <dir> makes the Git repository of the proof of step 35.1 (a repository cannot be committed under testdata): one
# commit by a test identity, made with a HOME and a global configuration of its own, so that the user's git configuration is neither
# read nor written.
set -euo pipefail
if [[ $# -ne 1 ]]; then
  echo "usage: $0 <dir>" >&2
  exit 64
fi
dir="$1"
home="$(mktemp -d)"
trap 'rm -rf "$home"' EXIT
export HOME="$home" XDG_CONFIG_HOME="$home/.config" GIT_CONFIG_GLOBAL="$home/gitconfig" GIT_CONFIG_NOSYSTEM=1
export GIT_AUTHOR_NAME="Ada Example" GIT_AUTHOR_EMAIL="ada@example.com"
export GIT_COMMITTER_NAME="Ada Example" GIT_COMMITTER_EMAIL="ada@example.com"
mkdir -p "$dir"
git -C "$dir" init -q -b main
printf 'package main\n\nfunc main() {}\n' >"$dir/main.go"
printf '# A repository from somewhere\n' >"$dir/README.md"
git -C "$dir" add main.go README.md
git -C "$dir" commit -q -m "Add the first files"
echo "made $dir"
