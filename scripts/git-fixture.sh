#!/usr/bin/env bash
# Builds one of the deterministic Git fixtures that the proofs and the measurements of the Source Control milestone use
# (docs/IMPLEMENTATION.md step 24.1, SPEC §10.30 "Tests"):
#
#   scripts/git-fixture.sh <small|conflict|history|many> <dir> [-files N]
#
#   small     a repository with a change in every group of the Source Control view (staged, unstaged, untracked, deleted, renamed)
#   conflict  a repository stopped in the middle of a merge with src/loader.go conflicted
#   history   300 commits on main, two feature branches with merges, tags, a bare origin (<dir>.origin.git) that it is 1 commit
#             ahead of and 2 behind, three authors, and a linked worktree (<dir>.worktrees/feature)
#   many      100,000 changed files (-files N changes the number) for the measurements
#
# <dir> must be missing or empty. The program runs the real git in an environment made from scratch (its own HOME and
# configuration, the author Ada Example <ada@example.com>, fixed dates): nothing of the user's git configuration, credentials, keys
# or repositories is read or written, and two runs give the same hashes. It prints the kind, the folder, HEAD and a hash of the
# whole fixture. The exit status is 0 when the fixture was built, 1 when it failed, 64 for a usage mistake.
set -euo pipefail

root="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
cd "$root"
# Emetos is cgo-free; building the program without cgo is faster and needs no C compiler.
export CGO_ENABLED=0
exec go run ./cmd/gitfixture "$@"
