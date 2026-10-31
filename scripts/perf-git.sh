#!/usr/bin/env bash
# Measures the Source Control targets of SPEC §14 for a repository with 100,000 changed files (docs/IMPLEMENTATION.md step 24.5):
# git's status with the parse of its output, the grouping off the UI thread, the hand-over to the host on the UI thread, one status
# for a burst of 5,000 file changes, and the Changes view's first frame and scrolling (internal/gui/views/scm TestHugeChanges).
#
#   scripts/heavy.sh --alone -- scripts/perf-git.sh [-files N] [-keep]
#
#   -files N  the number of changed files of the fixture (default 100000)
#   -keep     keep the fixture folder (it is printed); by default it is removed at the end
#
# The fixture is built by scripts/git-fixture.sh many in a temporary folder under a timeout (the build is bounded: 10 minutes), with
# the real git in an environment made from scratch; nothing of the user's git configuration, credentials, keys or repositories is
# read or written. The measurements run the extension against the fake API of extensions/internal/fakeapi (no window) with the tag
# emetos_perf. The output is a Markdown table. Run it alone: the numbers are times.
set -euo pipefail

files=100000
keep=0
while [[ $# -gt 0 ]]; do
	case "$1" in
	-files) files="$2"; shift 2 ;;
	-keep) keep=1; shift ;;
	*) echo "usage: scripts/perf-git.sh [-files N] [-keep]" >&2; exit 64 ;;
	esac
done

root="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
cd "$root"
work="$(mktemp -d "${TMPDIR:-/tmp}/emetos-perf-git.XXXXXX")"
cleanup() {
	if [[ $keep -eq 1 ]]; then echo "fixture kept in $work/many" >&2; else rm -rf "$work"; fi
}
trap cleanup EXIT

echo "building the fixture ($files files)..." >&2
build_start=$(date +%s.%N)
timeout -k 10 600 scripts/git-fixture.sh many "$work/many" -files "$files" >&2
build_end=$(date +%s.%N)

export GOMEMLIMIT=${GOMEMLIMIT:-3GiB}
export EMETOS_PERF_GIT_DIR="$work/many"
echo "measuring the extension..." >&2
log="$work/extension.log"
timeout -k 10 900 go test -tags emetos_perf ./extensions/gitscm -run TestPerfManyChangedFiles -count=1 -v >"$log" 2>&1 || { cat "$log" >&2; exit 1; }
echo "measuring the view..." >&2
viewlog="$work/view.log"
timeout -k 10 900 go test ./internal/gui/views/scm -run TestHugeChanges -count=1 -v >"$viewlog" 2>&1 || { cat "$viewlog" >&2; exit 1; }

value() { awk -v k="$1" '$1=="PERF" && $2==k {print $3}' "$log"; }

echo
echo "| Measurement | Value | Target (SPEC §14) |"
echo "|---|---|---|"
echo "| Changed files in the fixture | $(value entries) | 100,000 |"
echo "| Build of the fixture (not a target) | $(awk -v a="$build_start" -v b="$build_end" 'BEGIN{printf "%.1f s", b-a}') | |"
echo "| First status, activation to the groups on screen | $(value first-status-end-to-end) | |"
echo "| First status: git and the parse of its stream | $(value first-git-status-and-parse) | recorded, not a target |"
echo "| First status: grouping and sorting, off the UI thread | $(value first-group-off-ui-thread) | within 150 ms |"
echo "| First status: hand-over on the UI thread | $(value first-apply-on-ui-thread) | |"
echo "| Second status: git and the parse of its stream | $(value second-git-status-and-parse) | recorded, not a target |"
echo "| Second status: grouping and sorting, off the UI thread | $(value second-group-off-ui-thread) | within 150 ms |"
echo "| Second status: hand-over on the UI thread | $(value second-apply-on-ui-thread) | |"
echo "| Statuses for a burst of 5,000 file changes | $(value statuses-for-a-burst-of-5000-changes) | 1 |"
grep -E "attempt [0-9]+:|the longest" "$viewlog" | sed -E 's/^[[:space:]]*huge_test.go:[0-9]+: //' | while read -r line; do
	echo "| Changes view: $line | | first frame within 100 ms, no frame over 16 ms |"
done
