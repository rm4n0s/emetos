#!/usr/bin/env bash
# Installs (or removes) the desktop entry and the icons of Emetos for the current user, so that
# launchers and application menus show it (SPEC §10.14, §17).
#
# Usage: scripts/install-desktop.sh [--exec <path to emetos>] [--uninstall]
#
# It copies assets/dev.emetos.Emetos.desktop to <data>/applications/ and each assets/icons/emetos-<N>.png
# to <data>/icons/hicolor/<N>x<N>/apps/dev.emetos.Emetos.png, where <data> is $XDG_DATA_HOME, else
# $HOME/.local/share. It never writes anywhere else, and never needs root.
#
# The Exec line of the desktop entry names the program. A launcher does not always have the folder of
# `go install` ($GOBIN, else $GOPATH/bin) in its PATH, so the entry gets the absolute path of the
# program: --exec gives it, else `emetos` from PATH is used, else the folder of `go install`; when
# none is found, the entry keeps the plain name `emetos` and the script says so.
#
# update-desktop-database and gtk-update-icon-cache are run when they are installed, and skipped when
# they are not (the desktop finds the files without them, a little later).
#
# Exit status: 0 on success, 1 on a failure, 64 for a usage mistake.
set -euo pipefail

app_id="dev.emetos.Emetos"
root="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"

usage() {
  echo "usage: $0 [--exec <path to emetos>] [--uninstall]" >&2
  exit 64
}

uninstall=0
exec_path=""
while [[ $# -gt 0 ]]; do
  case "$1" in
    --uninstall)
      uninstall=1
      shift
      ;;
    --exec)
      [[ $# -ge 2 ]] || usage
      exec_path="$2"
      shift 2
      ;;
    *) usage ;;
  esac
done

# The data folder: $XDG_DATA_HOME when it is an absolute path (the XDG specification ignores any
# other), else $HOME/.local/share.
data="${XDG_DATA_HOME:-}"
if [[ "$data" != /* ]]; then
  if [[ "${HOME:-}" != /* ]]; then
    echo "error: neither XDG_DATA_HOME nor HOME is an absolute path" >&2
    exit 1
  fi
  data="$HOME/.local/share"
fi

apps="$data/applications"
hicolor="$data/icons/hicolor"
desktop="$apps/$app_id.desktop"

# refresh runs the cache tools that are installed; a failure of one is a warning, not an error.
refresh() {
  if command -v update-desktop-database >/dev/null 2>&1; then
    update-desktop-database -q "$apps" || echo "warning: update-desktop-database failed" >&2
  fi
  if command -v gtk-update-icon-cache >/dev/null 2>&1 && [[ -d "$hicolor" ]]; then
    gtk-update-icon-cache -q -f -t "$hicolor" || echo "warning: gtk-update-icon-cache failed" >&2
  fi
}

icons=("$root"/assets/icons/emetos-*.png)

if [[ $uninstall -eq 1 ]]; then
  rm -f "$desktop"
  for png in "${icons[@]}"; do
    size="$(basename "$png" .png)"
    size="${size#emetos-}"
    rm -f "$hicolor/${size}x${size}/apps/$app_id.png"
    # Folders left empty are removed; a folder that holds other icons stays.
    rmdir "$hicolor/${size}x${size}/apps" "$hicolor/${size}x${size}" 2>/dev/null || true
  done
  refresh
  echo "removed $desktop and the icons $app_id.png under $hicolor"
  exit 0
fi

# The program the entry starts.
if [[ -z "$exec_path" ]]; then
  if command -v emetos >/dev/null 2>&1; then
    exec_path="$(command -v emetos)"
  elif command -v go >/dev/null 2>&1; then
    gobin="$(go env GOBIN)"
    [[ -n "$gobin" ]] || gobin="$(go env GOPATH)/bin"
    [[ -x "$gobin/emetos" ]] && exec_path="$gobin/emetos"
  fi
fi
if [[ -n "$exec_path" ]]; then
  # A path with a space or a character the Exec key treats as special would need quoting rules that
  # are not worth carrying here.
  if [[ ! "$exec_path" =~ ^/[A-Za-z0-9_./+,:@=-]*$ ]]; then
    echo "error: the path of emetos must be absolute and made of letters, digits and _ . / + , : @ = - only: $exec_path" >&2
    exit 1
  fi
  if [[ ! -x "$exec_path" ]]; then
    echo "error: $exec_path is not an executable file" >&2
    exit 1
  fi
else
  echo "warning: emetos was not found in PATH nor in the folder of 'go install'; the entry starts plain 'emetos'." >&2
  echo "         Install it (go install -tags glutter_release ./cmd/emetos) and run this script again, or use --exec." >&2
fi

mkdir -p "$apps"
if [[ -n "$exec_path" ]]; then
  sed "s|^Exec=emetos |Exec=$exec_path |" "$root/assets/$app_id.desktop" >"$desktop"
else
  cp -f "$root/assets/$app_id.desktop" "$desktop"
fi
chmod 0644 "$desktop"

for png in "${icons[@]}"; do
  size="$(basename "$png" .png)"
  size="${size#emetos-}"
  dir="$hicolor/${size}x${size}/apps"
  mkdir -p "$dir"
  cp -f "$png" "$dir/$app_id.png"
  chmod 0644 "$dir/$app_id.png"
done

refresh
echo "installed $desktop (Exec: ${exec_path:-emetos}) and ${#icons[@]} icons under $hicolor"
