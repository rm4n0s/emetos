#!/usr/bin/env bash
# Makes the screenshots of README.md in docs/screenshots/ with scripts/proof.sh (and, for the two shots of two computers,
# scripts/proof-remotework.sh and scripts/proof-relay-client.sh): isolated from the user's configuration, in a headless Wayland
# compositor, from the fixtures in testdata/screenshots/. Every image is made again, so look at them afterwards.
#
# No image may show a path of a home folder: the fixtures are copied to a neutral folder, ${TMPDIR:-/tmp}/emetos-shots (which must
# not exist; it is removed at the end), and every run opens the copies there.
#
# Usage: scripts/screenshots.sh [name...]
#   names: editor workspace-tabs switch-picker diff merge settings tasks go-hover terminal remote-work relay
#
# What the shots need besides what scripts/proof.sh needs (sway):
#   go-hover    gopls on PATH (it is skipped, with a message, when gopls is missing; this script never installs it)
#   remote-work, relay   two Emetos instances on 127.0.0.1, with keys made on the run (nothing secret is kept)
#
# Exit status: that of the first failing proof, 64 for an unknown name, 1 when the work folder exists already.
set -euo pipefail

root="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
cd "$root"
mkdir -p docs/screenshots

all=(editor workspace-tabs switch-picker diff merge settings tasks go-hover terminal remote-work relay)
names=("$@")
[[ ${#names[@]} -gt 0 ]] || names=("${all[@]}")
for n in "${names[@]}"; do
  case " ${all[*]} " in
    *" $n "*) ;;
    *)
      echo "unknown screenshot: $n" >&2
      exit 64
      ;;
  esac
done

# The neutral folder of the copies. mkdir without -p fails when the folder exists, so that a run never mixes with another or
# follows a link somebody left there.
w="${TMPDIR:-/tmp}/emetos-shots"
if ! mkdir "$w" 2>/dev/null; then
  echo "error: $w exists (another run, or what a killed run left): remove it and run again" >&2
  exit 1
fi
trap 'rm -rf "$w"' EXIT
trap 'exit 130' INT
trap 'exit 143' TERM
cp -r testdata/screenshots/. "$w/"
p="$w/projects"
s="$w"

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
    tasks)
      # the Tasks tab of the panel after "go vet ./..." (the problems it finds are in the folder on purpose)
      PROOF_TIMEOUT=240 scripts/proof.sh docs/screenshots/tasks.png --config "$s/tools/config" --after 8s \
        --commands '[["dev.openFile",{"path":"'$p'/tools/main.go","line":13,"column":20}],["emetos.tasks.run"],["sleep","4s"]]' \
        -- "$p/tools"
      ;;
    go-hover)
      if ! command -v gopls >/dev/null 2>&1; then
        echo "skipped go-hover: gopls is not installed (install it yourself, for example with go install golang.org/x/tools/gopls@latest); docs/screenshots/go-hover.png was not made" >&2
        return 0
      fi
      scripts/proof.sh docs/screenshots/go-hover.png --after 9s \
        --commands '[["dev.openFile",{"path":"'$p'/api/main.go","line":15,"column":20}],["sleep","6s"],["dev.showHover",{"line":15,"column":20}],["sleep","2s"]]' \
        -- "$p/api"
      ;;
    terminal)
      # two terminals side by side in the panel: the shell of the profile lists the folder in color and stays open
      scripts/proof.sh docs/screenshots/terminal.png --config "$s/terminal/config" --after 6s \
        --commands '[["dev.openFile",{"path":"'$p'/api/main.go"}],["workbench.action.terminal.new"],["sleep","1500ms"],["workbench.action.terminal.split"],["sleep","2s"]]' \
        -- "$p/api"
      ;;
    remote-work)
      PROOF_RW_PROJECT="$s/remote/project" PROOF_RW_LOCAL="$s/remote/local" scripts/proof-remotework.sh docs/screenshots/remote-work.png
      ;;
    relay)
      PROOF_PC_SERVER="$s/relay/server" PROOF_PC_NOTES="$s/relay/notes" PROOF_PC_LOCAL="$s/relay/local" \
        PROOF_PC_CONFIG="$s/relay/config" scripts/proof-relay-client.sh docs/screenshots/relay.png
      ;;
  esac
}

for n in "${names[@]}"; do
  timeout -k 10 300 bash -c "$(declare -f shoot); root='$root'; p='$p'; s='$s'; w='$w'; cd '$root'; shoot $n"
done
