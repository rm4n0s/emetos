#!/usr/bin/env bash
# checks.sh <worktree> <log prefix>: the Definition-of-done checks of a step (CLAUDE.md), one after another, through heavy.sh,
# logged to <log prefix>-{quick,race,nocgo,real}.log with the lines RACE-EXIT, NOCGO-EXIT and REAL-EXIT. Read them before a merge.
#
#   setsid nohup scripts/checks.sh /path/to/worktree /var/tmp/emetos-logs/c211 &
#
# CHECKS_P (default 1) is the -p of the test runs; Go's build scratch goes to GOTMPDIR (default /var/tmp/emetos-go, on disk: /tmp
# is a tmpfs on Fedora, so the work folders a killed run leaves there stay in RAM).
W=$1; P=$2
if [[ -z "$W" || -z "$P" ]]; then
	echo "usage: scripts/checks.sh <worktree> <log prefix>" >&2
	exit 2
fi
mkdir -p "$(dirname "$P")"
export GOTMPDIR=${GOTMPDIR:-/var/tmp/emetos-go}
mkdir -p "$GOTMPDIR"
J=${CHECKS_P:-1}
cd "$W" || exit 2
{ echo "gofmt: $(gofmt -l cmd internal assets extension extensions | wc -l)"; go vet ./... && go vet -tags emetos_display ./... && go vet -tags 'emetos_display emetos_gopls' ./... && echo VET-OK; scripts/heavy.sh -- env GOMEMLIMIT=3GiB golangci-lint run --allow-parallel-runners && scripts/heavy.sh -- env GOMEMLIMIT=3GiB golangci-lint run --allow-parallel-runners --build-tags emetos_display ./cmd/... ./internal/testenv/... && echo LINT-OK; scripts/error-tags.sh --untested; echo "ET $?"; go mod tidy -diff && echo TIDY-OK; } > "$P-quick.log" 2>&1
scripts/heavy.sh -- env GOMEMLIMIT=3GiB go test -race -p "$J" -timeout 60m ./... > "$P-race.log" 2>&1; echo "RACE-EXIT $?" >> "$P-race.log"
{ scripts/heavy.sh -- env CGO_ENABLED=0 go build ./... && scripts/heavy.sh -- env GOMEMLIMIT=3GiB CGO_ENABLED=0 go test -p "$J" -timeout 60m ./...; } > "$P-nocgo.log" 2>&1; echo "NOCGO-EXIT $?" >> "$P-nocgo.log"
rm -rf "$P-shots"; mkdir -p "$P-shots"
scripts/heavy.sh --alone -- timeout -k 30 4000 scripts/realinput.sh --shots "$P-shots" > "$P-real.log" 2>&1; echo "REAL-EXIT $?" >> "$P-real.log"
