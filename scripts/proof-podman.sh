#!/usr/bin/env bash
# Makes the proof screenshot of step 18.4 (proof method B): the Podman view over a fake Podman (the pods web and batch with their
# containers, a podman-compose project, a lone container, and the connections), and the Kube YAML that "Generate Kube YAML" of the pod
# web opened as an untitled editor.
#
# Usage: scripts/proof-podman.sh <out.png>
#
# It starts the fake Podman of extensions/internal/containerapi/containerapitest (fakeengine -kind podman) on a Unix socket in a
# temporary folder, puts a fake `podman` program (testdata/proof/18.4/bin/podman, which prints a Kube YAML and a list of connections and
# runs nothing) first on PATH, points the setting podman.host at the fake engine, and runs scripts/proof.sh: the side bar shows the
# Podman view and the command Generate Kube YAML opens the YAML of the pod web. The engine and the podman program of the machine are
# never used, and the fake engine is stopped (SIGTERM to its process group) at the end.
#
# PROOF_TIMEOUT (seconds, default 120) stops a run that hangs. Needs sway: sudo dnf install sway
set -euo pipefail

[[ $# -eq 1 && "$1" == *.png ]] || { echo "usage: $0 <out.png>" >&2; exit 64; }
mkdir -p "$(dirname "$1")"
out="$(cd "$(dirname "$1")" && pwd)/$(basename "$1")"
root="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
cd "$root"
command -v go >/dev/null 2>&1 || { echo "error: go is not installed" >&2; exit 2; }

tmp="$(mktemp -d "${TMPDIR:-/tmp}/emetos-pp.XXXXXX")"
fake_pid=""
cleanup() {
  if [[ -n "$fake_pid" ]]; then
    kill -TERM -- "-$fake_pid" 2>/dev/null || true
    wait "$fake_pid" 2>/dev/null || true
  fi
  rm -rf "$tmp"
}
trap cleanup EXIT

project="$root/testdata/proof/18.4/project"
go build -o "$tmp/fakeengine" ./extensions/internal/containerapi/containerapitest/fakeengine
setsid "$tmp/fakeengine" -kind podman -socket "$tmp/e.sock" -ids "$tmp/ids.json" -project "$project" &
fake_pid=$!
for _ in $(seq 1 100); do
  [[ -s "$tmp/ids.json" ]] && break
  sleep 0.1
done
[[ -s "$tmp/ids.json" ]] || { echo "error: the fake engine did not start" >&2; exit 1; }

web="$(sed -E 's/.*"pod:web":"([^"]+)".*/\1/' "$tmp/ids.json")"

mkdir -p "$tmp/config" "$tmp/bin"
cp -r testdata/proof/18.4/config/. "$tmp/config/"
cp testdata/proof/18.4/bin/podman "$tmp/bin/podman"
cat > "$tmp/config/settings.json" <<JSON
{
    "workbench.colorTheme": "Dark Modern",
    "podman.host": "unix://$tmp/e.sock"
}
JSON

commands="[[\"workbench.view.extension.emetos.podman.container\"],[\"sleep\",\"1500ms\"],[\"podman.pods.generateKube\",{\"key\":\"$web\"}],[\"sleep\",\"1500ms\"]]"
PATH="$tmp/bin:$PATH" PROOF_TIMEOUT="${PROOF_TIMEOUT:-120}" scripts/proof.sh "$out" --config "$tmp/config" --after 3s --commands "$commands" -- "$project"
