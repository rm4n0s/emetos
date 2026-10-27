#!/usr/bin/env bash
# Makes the proof screenshot of step 15.4 (proof method B): two isolated Emetos instances on loopback. The host
# instance (A) opens testdata/proof/15.4/project with Remote Work on; the client instance (B) opens its own
# folder (testdata/proof/15.4/local), opens a "Remote Work" window tab, connects to A with a key file and, once
# A's window is on B's tab, B saves its window.
#
# Usage: scripts/proof-remotework.sh <out.png>
#
# Both instances run in their own headless compositor and their own isolated environment (scripts/isolated.sh), never
# on the user's desktop and never with the user's state. The keys are made on every run by
# scripts/proof-remotework-setup (a fresh client key, a fresh host key) in a temporary folder that is removed at the
# end; nothing secret is kept. B's list of pinned host keys already holds A's host key, so that the proof does not
# stop at the dialog of the first connection (that dialog is tested in extensions/remotework/tab_session_test.go
# and shown in the 15.2 proof). Everything stays on 127.0.0.1 (A listens on every interface, the default of the product, but
# only the loopback address is used).
#
# PROOF_TIMEOUT (seconds, default 120) stops a run that hangs. Needs sway: sudo dnf install sway
set -euo pipefail

usage() {
  echo "usage: $0 <out.png>" >&2
  exit 64
}
[[ $# -eq 1 && "$1" == *.png ]] || usage
mkdir -p "$(dirname "$1")"
out="$(cd "$(dirname "$1")" && pwd)/$(basename "$1")"

root="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
cd "$root"
for tool in go sway; do
  command -v "$tool" >/dev/null 2>&1 || { echo "error: $tool is not installed (sudo dnf install sway)" >&2; exit 2; }
done

tmp="$(mktemp -d "${TMPDIR:-/tmp}/emetos-proof-rw.XXXXXX")"
a_pid=""
cleanup() {
  if [[ -n "$a_pid" ]]; then
    kill -TERM -- "-$a_pid" 2>/dev/null || true
    sleep 1
    kill -KILL -- "-$a_pid" 2>/dev/null || true
  fi
  rm -rf "$tmp"
}
trap cleanup EXIT
trap 'exit 130' INT
trap 'exit 143' TERM

project="$root/testdata/proof/15.4/project"
local_folder="$root/testdata/proof/15.4/local"

echo "proof: building"
go build -o "$tmp/emetos" ./cmd/emetos
port="$(go run ./scripts/proof-remotework-setup "$tmp/setup" "$project")"

echo "proof: starting the host instance (A) on port $port"
GLUTTER_RENDERER="${GLUTTER_RENDERER:-gles}" setsid timeout -k 5 "${PROOF_TIMEOUT:-120}" \
  env -u WAYLAND_DISPLAY -u DISPLAY \
  "$root/scripts/isolated.sh" --state "$tmp/setup/a-state" -- \
  "$root/scripts/headless-wayland.sh" -- "$tmp/emetos" "$project" "$project/main.go" >"$tmp/a.log" 2>&1 &
a_pid=$!

# A is ready when its SSH listener accepts a connection.
ready=0
for _ in $(seq 1 120); do
  if (exec 3<>"/dev/tcp/127.0.0.1/$port") 2>/dev/null; then
    ready=1
    break
  fi
  kill -0 "$a_pid" 2>/dev/null || break
  sleep 0.5
done
if ((ready == 0)); then
  echo "error: the host instance did not listen on 127.0.0.1:$port" >&2
  tail -n 30 "$tmp/a.log" >&2 || true
  exit 1
fi

echo "proof: starting the client instance (B)"
commands="$(printf '[["dev.openWindowTab",{"kind":"emetos.remotework.tab"}],["sleep","500ms"],["dev.remotework.connect",{"host":"127.0.0.1","port":%s,"keyFile":"%s"}],["sleep","10s"]]' \
  "$port" "$tmp/setup/client/id_ed25519")"
PROOF_TIMEOUT="${PROOF_TIMEOUT:-120}" "$root/scripts/proof.sh" "$out" --state "$tmp/setup/b-state" --after 11s \
  --commands "$commands" -- "$local_folder"
