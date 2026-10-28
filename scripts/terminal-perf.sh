#!/usr/bin/env bash
# Measures the speed of the terminal (SPEC §14 "Terminal output", "Terminal frame" and "Terminal typing", step 17d.2) with real
# input in a release build: `cat` of a 100 MB file and a stream of full screens in a terminal of 160 x 45 cells or more, a
# line typed at a prompt, and the same `cat` in foot in the same compositor. It prints the lines that start with PERF.
#
# Usage: scripts/terminal-perf.sh [--desktop] [--size WxH]
#
# Without --desktop the compositor is headless and the renderer is software (Mesa's llvmpipe); with --desktop the tests run in a
# nested sway window on the desktop you are in, with the GPU (the window has to stay visible). The shells are bash with --norc
# and --noprofile and a home folder of the run: nothing of yours is read. The size of the output is 1920x1080 by default, so
# that the terminal has 160 x 45 cells.
set -euo pipefail

root="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
cd "$root"
desktop=() size=1920x1080
while [[ $# -gt 0 ]]; do
  case "$1" in
    --desktop)
      desktop=(--desktop)
      shift
      ;;
    --size)
      size="$2"
      shift 2
      ;;
    *)
      echo "usage: $0 [--desktop] [--size WxH]" >&2
      exit 64
      ;;
  esac
done

work="$(mktemp -d)"
trap 'rm -rf "$work"' EXIT
CGO_ENABLED=0 go build -tags glutter_release,emetos_dev -o "$work/emetos" ./cmd/emetos
export EMETOS_DISPLAY_BINARY="$work/emetos"
export EMETOS_REALINPUT_RACE=0
scripts/realinput.sh "${desktop[@]}" --size "$size" -- -tags emetos_display ./cmd/emetos -run "${PERF_RUN:-TestPerfTerminal}" -v -count=1 -timeout 20m
