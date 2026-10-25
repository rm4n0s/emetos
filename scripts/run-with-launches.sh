#!/usr/bin/env bash
# Runs Emetos and, once it listens on its socket, runs the same binary again once for each --launch, so that
# every launch is handed over to the first instance (SPEC §10.11, §10.14). It is what `scripts/proof.sh --launch`
# puts around the editor; it must run inside the environment of the first instance (scripts/isolated.sh and
# scripts/headless-wayland.sh), because the instance is found through XDG_RUNTIME_DIR.
#
# Usage: scripts/run-with-launches.sh [--launch '<args>']... -- <emetos binary> [args...]
#
# <args> is split as a shell would split it (quotes work), and relative paths are relative to the working
# directory, as for the first instance. The first instance starts in the background; the launches run one after
# the other, each as a normal `emetos <args>` that hands over and ends (a launch with --wait ends when its files are
# closed, so it is stopped after PROOF_LAUNCH_TIMEOUT seconds, default 30). A launch that arrives before the
# window is on screen waits in the instance's queue, so nothing has to wait for the first frame. The script ends with
# the status of the first instance, or 1 when that is 0 and a launch failed; it stops the instance when it is
# stopped itself.
set -euo pipefail

usage() {
  echo "usage: $0 [--launch '<args>']... -- <emetos binary> [args...]" >&2
  exit 64
}

launches=()
while [[ $# -gt 0 ]]; do
  case "$1" in
    --launch)
      [[ $# -ge 2 ]] || usage
      launches+=("$2")
      shift 2
      ;;
    --)
      shift
      break
      ;;
    *) usage ;;
  esac
done
[[ $# -ge 1 ]] || usage
binary="$1"
[[ -x "$binary" ]] || { echo "error: $binary is not an executable" >&2; exit 64; }
[[ -n "${XDG_RUNTIME_DIR:-}" ]] || { echo "error: XDG_RUNTIME_DIR is not set (run inside scripts/isolated.sh)" >&2; exit 64; }

"$@" &
app=$!
# The trap runs after the script's own code, so the process id is expanded now.
trap "kill $app 2>/dev/null || true; wait $app 2>/dev/null || true" EXIT
trap 'exit 143' TERM
trap 'exit 130' INT

socket="$XDG_RUNTIME_DIR/emetos-$(id -u).sock"
ready=0
for _ in $(seq 1 300); do
  if [[ -S "$socket" ]]; then
    ready=1
    break
  fi
  if ! kill -0 "$app" 2>/dev/null; then
    break
  fi
  sleep 0.1
done
if [[ "$ready" != 1 ]]; then
  echo "error: the first instance did not listen on $socket" >&2
  wait "$app" 2>/dev/null || true
  exit 1
fi

failed=0
for launch in "${launches[@]}"; do
  # shellcheck disable=SC2086
  eval "set -- $launch"
  echo "launch: emetos $launch" >&2
  if ! timeout -k 2 "${PROOF_LAUNCH_TIMEOUT:-30}" "$binary" "$@"; then
    echo "error: the launch 'emetos $launch' failed" >&2
    failed=1
  fi
done

set +e
wait "$app"
status=$?
trap - EXIT
if [[ "$status" == 0 && "$failed" == 1 ]]; then
  status=1
fi
exit "$status"
