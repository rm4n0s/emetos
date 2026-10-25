#!/usr/bin/env bash
# Makes the screenshots of README.md in docs/screenshots/ with scripts/proof.sh: isolated from the user's
# configuration and in a headless Wayland compositor, from the fixtures in testdata/screenshots/. Every image is
# made again, so look at them afterwards (none may show a path of a home folder).
#
# Usage: scripts/screenshots.sh [name...]     names: editor workspace-tabs switch-picker diff merge settings
#
# Needs what scripts/proof.sh needs (sway). Exit status: that of the first failing proof.sh, 64 for an unknown name.
set -euo pipefail

root="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
cd "$root"
mkdir -p docs/screenshots

p=testdata/screenshots/projects
s=testdata/screenshots

shoot() {
  local name="$1"
  shift
  case "$name" in
    editor)
      scripts/proof.sh docs/screenshots/editor.png --after 8s \
        --commands '[["dev.openFile","'$p'/api/internal/server/server.go"],["dev.openFile","'$p'/api/main.go"]]' \
        -- "$p/api" "$p/api/main.go"
      ;;
    workspace-tabs)
      scripts/proof.sh docs/screenshots/workspace-tabs.png --after 8s \
        --commands '[["dev.openFolderInNewTab",{"path":"'$p'/notes"}],["dev.openFolderInNewTab",{"path":"'$p'/web"}],["dev.openFile",{"path":"'$p'/web/index.html"}],["dev.openFile",{"path":"'$p'/web/style.css"}]]' \
        -- "$p/api"
      ;;
    switch-picker)
      scripts/proof.sh docs/screenshots/switch-picker.png \
        --commands '[["dev.openFolderInNewTab",{"path":"'$p'/notes"}],["dev.openFolderInNewTab",{"path":"'$p'/web"}],["emetos.workspaceTab.switch"]]' \
        -- "$p/api"
      ;;
    diff)
      scripts/proof.sh docs/screenshots/diff.png -- --diff "$s/diff/old.go" "$s/diff/new.go"
      ;;
    merge)
      scripts/proof.sh docs/screenshots/merge.png \
        --commands '[["dev.openMergeEditor",{"path":"'$s'/merge/conflicted.go"}],["dev.acceptMerge",{"conflict":0,"side":"incoming"}]]' --
      ;;
    settings)
      scripts/proof.sh docs/screenshots/settings.png --config "$s/settings/config" \
        --commands '[["workbench.action.openSettings","font"]]' --
      ;;
    *)
      echo "unknown screenshot: $name" >&2
      exit 64
      ;;
  esac
}

names=("$@")
[[ ${#names[@]} -gt 0 ]] || names=(editor workspace-tabs switch-picker diff merge settings)
for n in "${names[@]}"; do
  timeout -k 10 200 bash -c "$(declare -f shoot); p='$p'; s='$s'; shoot $n"
done
