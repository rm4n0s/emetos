#!/usr/bin/env bash
# Runs a command so that it can't touch the user's real Emetos configuration,
# state, cache, trash or socket (SPEC §15.1).
#
# Usage: scripts/isolated.sh [--config <dir>] -- <command> [args...]
#
# HOME and the XDG folders point into a fresh temporary folder, which is removed
# when the command ends. --config copies <dir> into $XDG_CONFIG_HOME/emetos/, so
# a proof can start from a prepared settings.json or keybindings.json.
#
# Two things stay reachable. Go keeps its caches (GOCACHE, GOMODCACHE and GOPATH
# are fixed before the folders move), so `go run` and `go build` still work. A
# Wayland compositor stays reachable: when WAYLAND_DISPLAY names a socket, a
# link to it is put in the private runtime folder. The Emetos socket of the
# user's session (in the real runtime folder) is not.
#
# fontconfig rebuilds its cache in the temporary folder, and does not see the
# user's own fonts, so the first run is a little slower and reproducible.
#
# Exit status: the command's own; 64 for a usage mistake.
set -euo pipefail

usage() {
  echo "usage: $0 [--config <dir>] -- <command> [args...]" >&2
  exit 64
}

config=""
while [[ $# -gt 0 ]]; do
  case "$1" in
    --config)
      [[ $# -ge 2 ]] || usage
      config="$2"
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

if [[ -n "$config" ]]; then
  if [[ ! -d "$config" ]]; then
    echo "error: --config $config is not a folder" >&2
    exit 64
  fi
  config="$(cd "$config" && pwd)"
fi

# Go's folders, while the real ones are still in force.
if command -v go >/dev/null 2>&1; then
  GOCACHE="$(go env GOCACHE)"
  GOMODCACHE="$(go env GOMODCACHE)"
  GOPATH="$(go env GOPATH)"
  export GOCACHE GOMODCACHE GOPATH
fi

# A short path: a Unix socket's path is limited to about 100 bytes.
tmp="$(mktemp -d "${TMPDIR:-/tmp}/emetos-iso.XXXXXX")"
trap 'rm -rf "$tmp"' EXIT
trap 'exit 130' INT
trap 'exit 143' TERM

mkdir -p "$tmp/home" "$tmp/config" "$tmp/state" "$tmp/cache" "$tmp/data" "$tmp/run"
chmod 700 "$tmp/run"
if [[ -n "$config" ]]; then
  mkdir -p "$tmp/config/emetos"
  cp -a "$config/." "$tmp/config/emetos/"
fi

# Keep the Wayland compositor reachable through the private runtime folder.
if [[ -n "${WAYLAND_DISPLAY:-}" ]]; then
  case "$WAYLAND_DISPLAY" in
    /*) socket="$WAYLAND_DISPLAY" ;;
    *) socket="${XDG_RUNTIME_DIR:-}/$WAYLAND_DISPLAY" ;;
  esac
  if [[ -S "$socket" ]]; then
    ln -s "$socket" "$tmp/run/$(basename "$socket")"
    export WAYLAND_DISPLAY="$(basename "$socket")"
  fi
fi

export HOME="$tmp/home"
export XDG_CONFIG_HOME="$tmp/config"
export XDG_STATE_HOME="$tmp/state"
export XDG_CACHE_HOME="$tmp/cache"
export XDG_DATA_HOME="$tmp/data"
export XDG_RUNTIME_DIR="$tmp/run"

# Not exec: the trap must run afterwards to remove the folder.
set +e
"$@"
status=$?
exit "$status"
