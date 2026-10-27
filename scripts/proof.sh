#!/usr/bin/env bash
# Makes a proof screenshot of Emetos (proof methods B and C in
# docs/IMPLEMENTATION.md).
#
# Usage: scripts/proof.sh <out.png> [--config <dir>] [--state <dir>] [--after <duration>]
#                         [--commands '<json>'] [--launch '<args>']... [--output] [-- emetos args...]
#
# It builds a debug binary (so the dev commands work) into a temporary folder,
# and runs it, isolated (scripts/isolated.sh) and in a headless Wayland compositor
# (scripts/headless-wayland.sh), never on the user's desktop and never with the
# user's configuration.
#
#   --config <dir>      copied into the configuration folder of the run
#                       (testdata/proof/<step id>/ holds the fixtures)
#   --state <dir>       copied into the state folder of the run ($XDG_STATE_HOME/emetos, modes 0600 and 0700;
#                       scripts/isolated.sh), for a proof that starts from a prepared state file
#   --after <duration>  how long Glutter waits before it captures (GLUTTER_SCREENSHOT_AFTER,
#                       such as 500ms or 2s). With --commands it is 1s unless given
#   --commands <json>   EMETOS_DEV_COMMANDS: a JSON array of ["command id", args]
#                       or ["sleep", "duration"] steps run after the first frame
#   --launch <args>     after the first instance is listening, run `emetos <args>` again in the same
#                       isolated environment (the same runtime folder, so the same socket), which hands
#                       its arguments over to the first instance and ends (SPEC §10.11, §10.14; see
#                       scripts/run-with-launches.sh). Repeatable: the launches run in the order given.
#                       <args> is split as a shell splits it, and relative paths are relative to the
#                       repository root, as for the first instance. With --launch the capture waits 1 s
#                       by default (--after changes it), so that it happens after the hand-over
#   --output            capture the whole headless output with grim, for several
#                       windows (method C), instead of the first window (method B).
#                       The windows are tiled (HEADLESS_FLOATING=0 unless the
#                       environment sets it), so that none hides another, and --after
#                       is how long the capture waits once the first window is mapped
#                       (SCREENSHOT_SETTLE), for the windows the commands open
#
# GLUTTER_RENDERER is gles for the run unless the environment sets it (the headless compositor has no GPU, and Glutter
# would try Vulkan first, 600 ms for nothing). PROOF_TIMEOUT (seconds, default 90) stops a run that hangs. Needs sway (and grim
# and jq for --output): sudo dnf install sway grim jq
#
# Exit status: 0 and a non-empty PNG on success; 1 when no image was written; 2 for
# a missing tool; 64 for a usage mistake.
set -euo pipefail

usage() {
  echo "usage: $0 <out.png> [--config <dir>] [--state <dir>] [--after <duration>] [--commands '<json>'] [--launch '<args>']... [--output] [-- emetos args...]" >&2
  exit 64
}

[[ $# -ge 1 ]] || usage
out="$1"
shift
[[ "$out" == *.png ]] || usage

config="" state="" after="" commands="" output=0 launches=()
while [[ $# -gt 0 ]]; do
  case "$1" in
    --config)
      [[ $# -ge 2 ]] || usage
      config="$2"
      shift 2
      ;;
    --state)
      [[ $# -ge 2 ]] || usage
      state="$2"
      shift 2
      ;;
    --after)
      [[ $# -ge 2 ]] || usage
      after="$2"
      shift 2
      ;;
    --commands)
      [[ $# -ge 2 ]] || usage
      commands="$2"
      shift 2
      ;;
    --launch)
      [[ $# -ge 2 ]] || usage
      launches+=(--launch "$2")
      shift 2
      ;;
    --output)
      output=1
      shift
      ;;
    --)
      shift
      break
      ;;
    *) usage ;;
  esac
done
args=("$@")

# Paths given relative to where the user stands are fixed before moving to the repository.
mkdir -p "$(dirname "$out")"
out="$(cd "$(dirname "$out")" && pwd)/$(basename "$out")"
if [[ -n "$config" ]]; then
  [[ -d "$config" ]] || { echo "error: --config $config is not a folder" >&2; exit 64; }
  config="$(cd "$config" && pwd)"
fi

if [[ -n "$state" ]]; then
  [[ -d "$state" ]] || { echo "error: --state $state is not a folder" >&2; exit 64; }
  state="$(cd "$state" && pwd)"
fi

root="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
cd "$root"

for tool in go sway; do
  command -v "$tool" >/dev/null 2>&1 || { echo "error: $tool is not installed (sudo dnf install sway)" >&2; exit 2; }
done
if [[ "$output" == 1 ]]; then
  for tool in grim jq swaymsg; do
    command -v "$tool" >/dev/null 2>&1 || { echo "error: $tool is not installed (sudo dnf install sway grim jq)" >&2; exit 2; }
  done
fi

tmp="$(mktemp -d "${TMPDIR:-/tmp}/emetos-proof.XXXXXX")"
trap 'rm -rf "$tmp"' EXIT
trap 'exit 130' INT
trap 'exit 143' TERM

echo "proof: building"
go build -o "$tmp/emetos" ./cmd/emetos

rm -f "$out"
if [[ (-n "$commands" || ${#launches[@]} -gt 0) && -z "$after" ]]; then
  after=1s
fi

# The run must not reach the user's compositor: the headless one replaces it.
run=(env -u WAYLAND_DISPLAY -u DISPLAY)
# The headless compositor presents no GPU buffers, and Glutter's `auto` tries Vulkan first (SPEC §9.6 of Glutter: the owner
# accepted that a headless run pays about 600 ms for the try), so the run asks for GLES. GLUTTER_RENDERER in the
# environment of the caller wins (GLUTTER_RENDERER=auto, or vulkan, for a proof of the renderer itself).
run+=(GLUTTER_RENDERER="${GLUTTER_RENDERER:-gles}")
[[ -n "$commands" ]] && run+=(EMETOS_DEV_COMMANDS="$commands")
isolated=("$root/scripts/isolated.sh")
[[ -n "$config" ]] && isolated+=(--config "$config")
[[ -n "$state" ]] && isolated+=(--state "$state")

# The command that runs inside the compositor: the editor, or the editor with its launches handed over to it.
editor=("$tmp/emetos" "${args[@]}")
if ((${#launches[@]} > 0)); then
  editor=("$root/scripts/run-with-launches.sh" "${launches[@]}" -- "$tmp/emetos" "${args[@]}")
fi

set +e
if [[ "$output" == 1 ]]; then
  # Method C: the whole output, captured by grim once a window is mapped. Tiled windows share the output, so
  # several windows are all in the picture; the wait is --after (seconds, from 1s or 500ms).
  settle=""
  case "$after" in
    "") ;;
    *ms) settle="$(awk -v v="${after%ms}" 'BEGIN { printf "%.3f", v / 1000 }')" ;;
    *s) settle="${after%s}" ;;
    *) echo "error: --after $after is not a duration such as 500ms or 2s" >&2; exit 64 ;;
  esac
  [[ -n "$settle" ]] && run+=(SCREENSHOT_SETTLE="$settle")
  run+=(HEADLESS_FLOATING="${HEADLESS_FLOATING:-0}")
  timeout -k 5 "${PROOF_TIMEOUT:-90}" "${run[@]}" "${isolated[@]}" -- \
    "$root/scripts/screenshot-headless.sh" "$out" -- "${editor[@]}"
else
  # Method B: the app saves its own first window and exits.
  [[ -n "$after" ]] && run+=(GLUTTER_SCREENSHOT_AFTER="$after")
  timeout -k 5 "${PROOF_TIMEOUT:-90}" "${run[@]}" GLUTTER_SCREENSHOT="$out" "${isolated[@]}" -- \
    "$root/scripts/headless-wayland.sh" -- "${editor[@]}"
fi
status=$?
set -e

if [[ ! -s "$out" ]]; then
  echo "error: no image was written to $out (exit status $status)" >&2
  exit 1
fi
if ((status != 0)); then
  echo "warning: the run ended with status $status, but the image was written" >&2
fi
echo "proof: wrote $out"
