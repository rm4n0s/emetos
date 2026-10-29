#!/usr/bin/env bash
# heavy.sh runs a heavy command (a full test run, the linter, a release build) in one of two slots that every agent on this
# machine shares, so that at most two heavy commands run at once (CLAUDE.md "Memory (OOM)"). With --alone it takes both slots
# and runs by itself: real-input display tests (scripts/realinput.sh), scripts/perf.sh and other timing measurements.
#
#   scripts/heavy.sh -- GOMEMLIMIT=3GiB go test -race -p 2 ./...
#   scripts/heavy.sh --alone -- scripts/realinput.sh --shots /tmp/shots
#
# It waits for a free slot (printing once that it waits), runs the command, and exits with the command's status. The locks are
# flock(1) locks on files in $XDG_RUNTIME_DIR (or /tmp), released by the kernel when the command ends, also when it is killed.
# A third lock, the gate, keeps an --alone command from waiting for ever: it holds the gate while it waits for the two slots,
# and a command that is not alone takes a free slot only through the gate, so none starts while an --alone command waits.
set -euo pipefail

alone=0
while [[ $# -gt 0 ]]; do
	case "$1" in
	--alone) alone=1; shift ;;
	--) shift; break ;;
	*) break ;;
	esac
done
if [[ $# -eq 0 ]]; then
	echo "usage: scripts/heavy.sh [--alone] -- <command> [args...]" >&2
	exit 2
fi

dir="${XDG_RUNTIME_DIR:-/tmp}"
exec {slot1}>"$dir/emetos-heavy.1.lock"
exec {slot2}>"$dir/emetos-heavy.2.lock"
exec {gate}>"$dir/emetos-heavy.gate.lock"

if [[ $alone -eq 1 ]]; then
	# The gate, then both slots, always in that order, holding nothing when it starts to wait (a slot that the quick try took
	# is given back first). A command that is not alone never waits while it holds a slot or the gate, so this cannot deadlock.
	if ! flock -n "$slot1" || ! flock -n "$slot2"; then
		flock -u "$slot1"
		echo "heavy.sh: waiting for both slots..." >&2
		flock -x "$gate"
		flock "$slot1"
		flock "$slot2"
	fi
	exec {gate}>&-
else
	waited=0
	while true; do
		# the gate is shared among the commands that are not alone and held only for the two tries
		flock -s "$gate"
		if flock -n "$slot1"; then
			exec {slot2}>&- {gate}>&-
			break
		fi
		if flock -n "$slot2"; then
			exec {slot1}>&- {gate}>&-
			break
		fi
		flock -u "$gate"
		if [[ $waited -eq 0 ]]; then
			echo "heavy.sh: waiting for a free slot..." >&2
			waited=1
		fi
		sleep 2
	done
fi

# env lets the command start with VAR=value assignments.
exec env "$@"
