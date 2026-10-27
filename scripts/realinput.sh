#!/usr/bin/env bash
# Runs the display tests that type and click through the compositor (real input, SPEC §15.1, step 17b.1).
#
# Usage: scripts/realinput.sh [--desktop] [--size WxH] [--shots <dir>] [-- <go test arguments>]
#
# Without --desktop it is `scripts/headless-wayland.sh -- go test -tags emetos_display ...`: a headless sway of its own.
# With --desktop the tests run in a nested sway, which shows as a window on the desktop you are in, so that you can watch
# them and the GPU is the real one. The virtual keyboard and pointer of the tests are made on the nested sway only: the
# environment the tests get names its socket (WAYLAND_DISPLAY, SWAYSOCK, a private XDG_RUNTIME_DIR), never the desktop's,
# so nothing is typed into your own session. The window of the nested sway has to stay visible (a hidden one gets no frames).
# The nested sway and everything the tests started are stopped when the command ends.
#
#   --desktop       run in a nested sway window on the desktop (needs WAYLAND_DISPLAY)
#   --size WxH      the size of the output of the compositor, 1280x800 by default
#   --shots <dir>   EMETOS_PROOF_DIR for the tests: where the proof images go (cmd/emetos TestRealInputProofs)
#   -- args         go test arguments; ./cmd/emetos -run TestRealInput -v when none are given
#
# The go test runs with the race detector unless EMETOS_REALINPUT_RACE=0. A GPU renderer can be asked for with
# GLUTTER_RENDERER=auto (the tests use gles, which needs no GPU, unless it is set).
#
# Exit status: the tests' own; 2 when sway is missing; 64 for a usage mistake.
set -euo pipefail

usage() {
  echo "usage: $0 [--desktop] [--size WxH] [--shots <dir>] [-- <go test arguments>]" >&2
  exit 64
}

desktop=0 size="" shots=""
while [[ $# -gt 0 ]]; do
  case "$1" in
    --desktop)
      desktop=1
      shift
      ;;
    --size)
      [[ $# -ge 2 ]] || usage
      size="$2"
      shift 2
      ;;
    --shots)
      [[ $# -ge 2 ]] || usage
      shots="$2"
      shift 2
      ;;
    --)
      shift
      break
      ;;
    *) usage ;;
  esac
done
args=("$@")
if [[ ${#args[@]} -eq 0 ]]; then
  args=(./cmd/emetos -run TestRealInput -v)
fi

root="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
cd "$root"
command -v sway >/dev/null 2>&1 || { echo "error: sway is not installed (sudo dnf install sway grim)" >&2; exit 2; }
command -v grim >/dev/null 2>&1 || { echo "error: grim is not installed (sudo dnf install sway grim)" >&2; exit 2; }

if [[ -n "$shots" ]]; then
  mkdir -p "$shots"
  export EMETOS_PROOF_DIR
  EMETOS_PROOF_DIR="$(cd "$shots" && pwd)"
fi
[[ -n "$size" ]] && export HEADLESS_SIZE="$size"

race=(-race)
[[ "${EMETOS_REALINPUT_RACE:-1}" == 0 ]] && race=()
# a deadline for the whole run: the tests start processes of their own, and a hung one must not stay
deadline="${EMETOS_REALINPUT_TIMEOUT:-1500}"
run=(timeout -k 15 "$deadline" go test "${race[@]}" -tags emetos_display "${args[@]}")

if [[ "$desktop" == 0 ]]; then
  exec "$root/scripts/headless-wayland.sh" -- "${run[@]}"
fi

# ---- --desktop: a nested sway, a window of the compositor the script is started from ----
host="${WAYLAND_DISPLAY:-}"
if [[ -z "$host" ]]; then
  echo "error: --desktop needs a Wayland session: WAYLAND_DISPLAY is not set" >&2
  exit 64
fi
case "$host" in
  /*) host_socket="$host" ;;
  *) host_socket="${XDG_RUNTIME_DIR:-}/$host" ;;
esac
[[ -S "$host_socket" ]] || { echo "error: the Wayland socket $host_socket does not exist" >&2; exit 64; }

# The nested sway has a runtime folder of its own, so its socket and its IPC socket are found there and nothing else is
# mistaken for them. The host is reached through the absolute path of its socket.
runtime="$(mktemp -d "${TMPDIR:-/tmp}/emetos-nest.XXXXXX")"
chmod 700 "$runtime"
log="$runtime/sway.log"
pid=""
cleanup() {
  if [[ -n "$pid" ]] && kill -0 "$pid" 2>/dev/null; then
    kill "$pid" 2>/dev/null || true
    for _ in 1 2 3 4 5 6 7 8 9 10 11 12 13 14 15 16 17 18 19 20; do
      kill -0 "$pid" 2>/dev/null || break
      sleep 0.1
    done
    kill -9 "$pid" 2>/dev/null || true
    wait "$pid" 2>/dev/null || true
  fi
  rm -rf "$runtime"
}
trap cleanup EXIT
trap 'exit 130' INT
trap 'exit 143' TERM

cat >"$runtime/sway.conf" <<EOF
xwayland disable
output * bg #3b3b3b solid_color
for_window [title=".*"] floating enable
for_window [app_id=".*"] floating enable
EOF
if [[ -n "$size" ]]; then
  echo "output * resolution $size" >>"$runtime/sway.conf"
fi

env -u DISPLAY -u SWAYSOCK -u I3SOCK \
  WAYLAND_DISPLAY="$host_socket" XDG_RUNTIME_DIR="$runtime" WLR_BACKENDS=wayland \
  sway -c "$runtime/sway.conf" >"$log" 2>&1 &
pid=$!

socket="" ipc=""
for _ in $(seq 1 100); do
  if ! kill -0 "$pid" 2>/dev/null; then
    break
  fi
  for candidate in "$runtime"/wayland-*; do
    [[ -S "$candidate" ]] && socket="$(basename "$candidate")"
  done
  for candidate in "$runtime"/sway-ipc.*.sock; do
    [[ -S "$candidate" ]] && ipc="$candidate"
  done
  [[ -n "$socket" && -n "$ipc" ]] && break
  sleep 0.1
done
if [[ -z "$socket" || -z "$ipc" ]]; then
  echo "error: the nested sway did not start. Its output:" >&2
  sed 's/^/  /' "$log" >&2 || true
  exit 3
fi

# From here on the environment is the nested sway's: the tests cannot reach the desktop's socket.
set +e
env -u DISPLAY -u I3SOCK \
  WAYLAND_DISPLAY="$socket" XDG_RUNTIME_DIR="$runtime" SWAYSOCK="$ipc" \
  "${run[@]}"
status=$?
exit "$status"
