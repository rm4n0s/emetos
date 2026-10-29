#!/usr/bin/env bash
# Makes the proof screenshot of step 18.3 (proof method B): a build of a Dockerfile streamed into its tab of the panel, and the Compose
# group of the Containers view with the question that comes before it is taken down.
#
# Usage: scripts/proof-containers-workflows.sh <out.png>
#
# It starts the fake Docker engine of extensions/internal/containerapi/containerapitest (fakeengine) on a Unix socket in a temporary
# folder, puts a fake `docker` program (testdata/proof/18.3/bin/docker, which prints the log of a build and runs nothing) first on
# PATH, points the setting containers.host at the fake engine, and runs scripts/proof.sh: the side bar shows the Containers view, the
# command Build Image builds the Dockerfile of testdata/proof/18.3/project as shop-web:1.0 (the tag is given, so that nothing is
# asked), and Compose Down on the group "shop" asks its question. The engine and the docker program of the machine are never used,
# and the fake engine is stopped (SIGTERM to its process group) at the end.
#
# PROOF_TIMEOUT (seconds, default 120) stops a run that hangs. Needs sway: sudo dnf install sway
set -euo pipefail

[[ $# -eq 1 && "$1" == *.png ]] || { echo "usage: $0 <out.png>" >&2; exit 64; }
mkdir -p "$(dirname "$1")"
out="$(cd "$(dirname "$1")" && pwd)/$(basename "$1")"
root="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
cd "$root"
command -v go >/dev/null 2>&1 || { echo "error: go is not installed" >&2; exit 2; }

tmp="$(mktemp -d "${TMPDIR:-/tmp}/emetos-pw.XXXXXX")"
fake_pid=""
cleanup() {
  if [[ -n "$fake_pid" ]]; then
    kill -TERM -- "-$fake_pid" 2>/dev/null || true
    wait "$fake_pid" 2>/dev/null || true
  fi
  rm -rf "$tmp"
}
trap cleanup EXIT

project="$root/testdata/proof/18.3/project"
go build -o "$tmp/fakeengine" ./extensions/internal/containerapi/containerapitest/fakeengine
setsid "$tmp/fakeengine" -socket "$tmp/e.sock" -ids "$tmp/ids.json" -project "$project" &
fake_pid=$!
for _ in $(seq 1 100); do
  [[ -s "$tmp/ids.json" ]] && break
  sleep 0.1
done
[[ -s "$tmp/ids.json" ]] || { echo "error: the fake engine did not start" >&2; exit 1; }

mkdir -p "$tmp/config" "$tmp/bin"
cp -r testdata/proof/18.3/config/. "$tmp/config/"
cp testdata/proof/18.3/bin/docker "$tmp/bin/docker"
cat > "$tmp/config/settings.json" <<JSON
{
    "workbench.colorTheme": "Dark Modern",
    "containers.host": "unix://$tmp/e.sock"
}
JSON

commands="[[\"workbench.view.extension.emetos.containers.container\"],[\"sleep\",\"1s\"],[\"containers.images.build\",{\"uri\":\"file://$project/Dockerfile\",\"tag\":\"shop-web:1.0\"}],[\"sleep\",\"2500ms\"],[\"containers.containers.composeGroup.down\",{\"key\":\"g:shop\"}],[\"sleep\",\"500ms\"]]"
PATH="$tmp/bin:$PATH" PROOF_TIMEOUT="${PROOF_TIMEOUT:-120}" scripts/proof.sh "$out" --config "$tmp/config" --after 4s --commands "$commands" -- "$project"
