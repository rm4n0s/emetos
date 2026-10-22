#!/usr/bin/env bash
# Adapted from Glutter's scripts/screenshot-headless.sh (BSD-3-Clause, https://github.com/rm4n0s/glutter).
# Runs a command in headless sway, waits until it has mapped a window, and saves
# a screenshot of the whole output (proof method C in docs/IMPLEMENTATION.md).
#
# Usage: scripts/screenshot-headless.sh <out.png> -- <command> [args...]
#
# The window must be mapped within 10 seconds, so build first and pass the
# binary (`go run` compiles too slowly). The command is stopped after the
# screenshot. Environment: HEADLESS_SIZE, HEADLESS_SCALE and HEADLESS_FLOATING
# as for scripts/headless-wayland.sh, and SCREENSHOT_SETTLE (seconds to wait
# after the window appears, default 0.5).
#
# Needs sway, grim and jq: sudo dnf install sway grim jq
#
# Exit status: 0 on success; 1 when no window appeared or the command ended
# early; 2 when a tool is missing; 64 for a usage mistake.
set -euo pipefail

usage() {
  echo "usage: $0 <out.png> -- <command> [args...]" >&2
  exit 64
}

[[ $# -ge 3 && "$2" == "--" ]] || usage
out="$1"
shift 2

for tool in sway swaymsg grim jq; do
  if ! command -v "$tool" >/dev/null 2>&1; then
    echo "error: $tool is not installed." >&2
    echo "       install what is needed with: sudo dnf install sway grim jq" >&2
    exit 2
  fi
done

mkdir -p "$(dirname "$out")"
case "$out" in
  /*) ;;
  *) out="$PWD/$out" ;;
esac
here="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"

# inner runs inside the headless compositor.
inner() {
  local out="$1"
  shift
  "$@" &
  local app=$!
  # The trap runs after this function has returned and `app` is out of scope, so
  # the process ID is expanded now, in double quotes.
  trap "kill $app 2>/dev/null || true; wait $app 2>/dev/null || true" EXIT

  local mapped=0
  for _ in $(seq 1 100); do
    if ! kill -0 "$app" 2>/dev/null; then
      wait "$app" 2>/dev/null || true
      echo "error: the command ended before it mapped a window." >&2
      return 1
    fi
    # A window node has a process ID; workspaces and containers do not.
    if [[ "$(swaymsg -t get_tree 2>/dev/null | jq '[.. | objects | select((.pid? // 0) > 0)] | length')" -gt 0 ]]; then
      mapped=1
      break
    fi
    sleep 0.1
  done
  if [[ "$mapped" -ne 1 ]]; then
    echo "error: no window was mapped within 10 seconds." >&2
    return 1
  fi
  sleep "${SCREENSHOT_SETTLE:-0.5}"
  grim "$out"
}
export -f inner

exec "$here/headless-wayland.sh" -- bash -c 'inner "$@"' inner "$out" "$@"
