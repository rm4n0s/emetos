#!/usr/bin/env bash
# Makes the proof screenshot of step 18.2 (proof method B): the Containers view of the Docker extension over a fake engine.
#
# Usage: scripts/proof-containers.sh <out.png>
#
# It starts the fake Docker engine of extensions/internal/containerapi/containerapitest (fakeengine, two Compose projects, a
# lone container, images, a network, volumes) on a Unix socket in a temporary folder, writes a configuration that points the
# setting containers.host at it, and runs scripts/proof.sh: the side bar shows the Containers view, the logs of shop-web-1 are
# followed in the panel and the Remove of an image asks its question. The engine of the machine is never asked, and the
# fake is stopped (SIGTERM to its process group) at the end.
#
# PROOF_TIMEOUT (seconds, default 120) stops a run that hangs. Needs sway: sudo dnf install sway
set -euo pipefail

[[ $# -eq 1 && "$1" == *.png ]] || { echo "usage: $0 <out.png>" >&2; exit 64; }
mkdir -p "$(dirname "$1")"
out="$(cd "$(dirname "$1")" && pwd)/$(basename "$1")"
root="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
cd "$root"
command -v go >/dev/null 2>&1 || { echo "error: go is not installed" >&2; exit 2; }

tmp="$(mktemp -d "${TMPDIR:-/tmp}/emetos-pc.XXXXXX")"
fake_pid=""
cleanup() {
  if [[ -n "$fake_pid" ]]; then
    kill -TERM -- "-$fake_pid" 2>/dev/null || true
    wait "$fake_pid" 2>/dev/null || true
  fi
  rm -rf "$tmp"
}
trap cleanup EXIT

go build -o "$tmp/fakeengine" ./extensions/internal/containerapi/containerapitest/fakeengine
setsid "$tmp/fakeengine" -socket "$tmp/e.sock" -ids "$tmp/ids.json" &
fake_pid=$!
for _ in $(seq 1 100); do
  [[ -s "$tmp/ids.json" ]] && break
  sleep 0.1
done
[[ -s "$tmp/ids.json" ]] || { echo "error: the fake engine did not start" >&2; exit 1; }

web="$(sed -E 's/.*"shop-web-1":"([0-9a-f]+)".*/\1/' "$tmp/ids.json")"
image="$(sed -E 's/.*"image:busybox:1.37.0":"([^"]+)".*/\1/' "$tmp/ids.json")"

mkdir -p "$tmp/config"
cp -r testdata/proof/18.2/config/. "$tmp/config/"
cat > "$tmp/config/settings.json" <<JSON
{
    "workbench.colorTheme": "Dark Modern",
    "containers.host": "unix://$tmp/e.sock"
}
JSON

commands="[[\"workbench.view.extension.emetos.containers.container\"],[\"sleep\",\"1s\"],[\"containers.containers.viewLogs\",{\"key\":\"c:$web\"}],[\"sleep\",\"1s\"],[\"containers.images.remove\",{\"key\":\"$image\"}],[\"sleep\",\"500ms\"]]"
PROOF_TIMEOUT="${PROOF_TIMEOUT:-120}" scripts/proof.sh "$out" --config "$tmp/config" --after 4s --commands "$commands" -- "$root/testdata/proof/18.2/project"
