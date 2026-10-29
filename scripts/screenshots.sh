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
#          docker sql mongodb redis
#
# What the shots need besides what scripts/proof.sh needs (sway):
#   go-hover    gopls on PATH (it is skipped, with a message, when gopls is missing; this script never installs it)
#   remote-work, relay   two Emetos instances on 127.0.0.1, with keys made on the run (nothing secret is kept)
#   docker      nothing: a fake Docker engine (the fakeengine of extensions/internal/containerapi/containerapitest) on a Unix
#               socket; the Docker of the computer is never asked
#   sql         nothing: a SQLite database made on the run from testdata/screenshots/sql/library.sql
#   mongodb, redis   Docker, and the pinned image (containerapitest/images.go; docker pulls it when it is missing): a throwaway
#               container named emetos-test-<random>, labelled dev.emetos.test=1, published on 127.0.0.1 only, with a random
#               password that reaches Emetos through an environment variable; the container is removed at the end, also when the
#               run fails. Nothing else of the user's Docker is listed, changed or removed. The shot is skipped, with a message,
#               when Docker does not answer
#
# Exit status: that of the first failing proof, 64 for an unknown name, 1 when the work folder exists already.
set -euo pipefail

root="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
cd "$root"
mkdir -p docs/screenshots

all=(editor workspace-tabs switch-picker diff merge settings tasks go-hover terminal remote-work relay docker sql mongodb redis)
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


# The functions the shots below share. They are declared into the shell that runs a shot (declare -f).

# image_of <name>: the pinned image of containerapitest/images.go (ImageMongoDB, ImageRedis).
image_of() {
  sed -n "s/^[[:space:]]*$1 *= *\"\(.*\)\"$/\1/p" extensions/internal/containerapi/containerapitest/images.go
}

# random_hex <bytes>
random_hex() {
  od -An -N"$1" -tx1 /dev/urandom | tr -d ' \n'
}

# server_start <image> <container port> [docker run options...] [-- command...]: starts a throwaway container (named
# emetos-test-<random>, labelled dev.emetos.test=1, published on 127.0.0.1 only) and sets srv_name and srv_port. The caller
# removes it with server_stop (in a trap).
srv_name="" srv_port=""
server_start() {
  local image="$1" cport="$2" opts=() cmd=()
  shift 2
  while [[ $# -gt 0 && "$1" != -- ]]; do
    opts+=("$1")
    shift
  done
  [[ "${1:-}" == -- ]] && shift
  cmd=("$@")
  [[ -n "$image" ]] || { echo "error: no pinned image" >&2; return 1; }
  srv_name="emetos-test-$(random_hex 6)"
  docker run -d --name "$srv_name" --label dev.emetos.test=1 -p "127.0.0.1::$cport" "${opts[@]}" "$image" "${cmd[@]}" >/dev/null
  srv_port="$(docker port "$srv_name" "$cport/tcp" | sed -n 's/^127\.0\.0\.1:\([0-9]*\)$/\1/p' | head -n 1)"
  [[ -n "$srv_port" ]] || { echo "error: no port on 127.0.0.1 for $srv_name" >&2; return 1; }
}

# server_stop: removes the container of server_start (only that one, by its name).
server_stop() {
  if [[ -n "$srv_name" ]]; then
    docker rm -f "$srv_name" >/dev/null 2>&1 || true
    srv_name=""
  fi
}

# docker_ready: succeeds when the Docker engine answers.
docker_ready() {
  command -v docker >/dev/null 2>&1 && docker version --format '{{.Server.Version}}' >/dev/null 2>&1
}

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
    docker)
      # the Containers view over a fake Docker engine, and the logs of a container in the panel
      local t web
      fake_pid=""
      t="$(mktemp -d "$w/docker.XXXXXX")"
      trap '[[ -z "$fake_pid" ]] || kill -TERM -- "-$fake_pid" 2>/dev/null || true' EXIT
      trap 'exit 143' TERM
      go build -o "$t/fakeengine" ./extensions/internal/containerapi/containerapitest/fakeengine
      setsid "$t/fakeengine" -socket "$t/e.sock" -ids "$t/ids.json" -project /srv/shop &
      fake_pid=$!
      for _ in $(seq 1 100); do
        [[ -s "$t/ids.json" ]] && break
        sleep 0.1
      done
      [[ -s "$t/ids.json" ]] || { echo "error: the fake engine did not start" >&2; return 1; }
      web="$(sed -E 's/.*"shop-web-1":"([0-9a-f]+)".*/\1/' "$t/ids.json")"
      mkdir -p "$t/config"
      sed "s|@SOCKET@|$t/e.sock|" "$s/docker/settings.json" >"$t/config/settings.json"
      scripts/proof.sh docs/screenshots/docker.png --config "$t/config" --after 4s \
        --commands '[["dev.openFile",{"path":"'$p'/api/main.go"}],["workbench.view.extension.emetos.containers.container"],["sleep","1s"],["containers.containers.viewLogs",{"key":"c:'$web'"}],["sleep","1500ms"]]' \
        -- "$p/api"
      ;;
    sql)
      # a query on a SQLite file in the Databases view, with its results in the panel
      local t
      t="$(mktemp -d "$w/sql.XXXXXX")"
      go run ./scripts/screenshot-sqlite "$t/library.db" "$s/sql/library.sql"
      mkdir -p "$t/state"
      cat >"$t/state/db-connections.json" <<JSON
{"version": 1, "connections": [
  {"id": "lib1", "name": "Library", "engine": "sqlite", "scope": "user", "file": "$t/library.db", "color": "#4a90d9"}
]}
JSON
      scripts/proof.sh docs/screenshots/sql.png --config "$s/sql" --state "$t/state" --after 18s \
        --commands '[["dev.openFile",{"path":"'$p'/api/main.go"}],["workbench.view.extension.emetos.databases.container"],["emetos.databases.connect","lib1"],["sleep","1500ms"],["emetos.sql.newQuery",{"connection":"lib1"}],["sleep","1s"],["dev.type","SELECT a.name AS author, b.title, b.year FROM books b JOIN authors a ON a.id = b.author_id ORDER BY b.year;"],["sleep","8s"],["emetos.sql.run"],["sleep","2500ms"]]' \
        -- "$p/api"
      ;;
    mongodb)
      # a playground on a collection of a MongoDB server, and its results (a labelled test container)
      local t password
      if ! docker_ready; then
        echo "skipped mongodb: the Docker engine does not answer; docs/screenshots/mongodb.png was not made" >&2
        return 0
      fi
      trap server_stop EXIT
      trap 'exit 143' TERM
      t="$(mktemp -d "$w/mongodb.XXXXXX")"
      password="$(random_hex 12)"
      server_start "$(image_of ImageMongoDB)" 27017 -e MONGO_INITDB_ROOT_USERNAME=root -e MONGO_INITDB_ROOT_PASSWORD="$password"
      mongosh_run() {
        docker exec -i -e MP="$password" "$srv_name" sh -c 'mongosh -u root -p "$MP" --authenticationDatabase admin --quiet' "$@"
      }
      for _ in $(seq 1 120); do
        echo 'db.runCommand({ping: 1}).ok' | mongosh_run >/dev/null 2>&1 && break
        sleep 1
      done
      mongosh_run <"$s/mongodb/seed.js" >/dev/null
      mkdir -p "$t/state"
      cat >"$t/state/db-connections.json" <<JSON
{"version": 1, "connections": [
  {"id": "mongo1", "name": "Shop", "engine": "mongodb", "scope": "user", "host": "localhost", "port": $srv_port, "user": "root",
   "database": "shop", "color": "#13aa52", "passwordEnv": "\${env:EMETOS_SHOTS_PASSWORD}",
   "options": {"authSource": "admin", "directConnection": "true"}}
]}
JSON
      EMETOS_SHOTS_PASSWORD="$password" scripts/proof.sh docs/screenshots/mongodb.png --config "$s/mongodb" --state "$t/state" --after 12s \
        --commands '[["workbench.view.extension.emetos.databases.container"],["emetos.databases.connect","mongo1"],["sleep","2s"],["mongodb.newPlayground",{"connection":"mongo1","database":"shop","collection":"orders"}],["sleep","1s"],["mongodb.runAll"],["sleep","3s"]]' \
        -- "$p/api"
      ;;
    redis)
      # the key editor on a hash of a Redis server (a labelled test container)
      local t password
      if ! docker_ready; then
        echo "skipped redis: the Docker engine does not answer; docs/screenshots/redis.png was not made" >&2
        return 0
      fi
      trap server_stop EXIT
      trap 'exit 143' TERM
      t="$(mktemp -d "$w/redis.XXXXXX")"
      password="$(random_hex 12)"
      server_start "$(image_of ImageRedis)" 6379 -- redis-server --requirepass "$password"
      redis_cli() {
        docker exec -i -e REDISCLI_AUTH="$password" "$srv_name" redis-cli "$@"
      }
      for _ in $(seq 1 60); do
        redis_cli PING >/dev/null 2>&1 && break
        sleep 1
      done
      redis_cli <"$s/redis/seed.redis" >/dev/null
      mkdir -p "$t/state"
      cat >"$t/state/db-connections.json" <<JSON
{"version": 1, "connections": [
  {"id": "cache1", "name": "cache", "engine": "redis", "scope": "user", "host": "localhost", "port": $srv_port,
   "passwordEnv": "\${env:EMETOS_SHOTS_PASSWORD}", "options": {"separator": ":"}}
]}
JSON
      EMETOS_SHOTS_PASSWORD="$password" scripts/proof.sh docs/screenshots/redis.png --config "$s/redis" --state "$t/state" --after 9s \
        --commands '[["workbench.view.extension.emetos.databases.container"],["emetos.databases.connect","cache1"],["sleep","2s"],["emetos.redis.openKey",{"connection":"cache1","db":0,"key":"user:42:profile"}],["sleep","2s"]]' \
        -- "$p/api"
      ;;
  esac
}

for n in "${names[@]}"; do
  timeout -k 10 300 bash -c "$(declare -f shoot image_of random_hex server_start server_stop docker_ready); srv_name='' srv_port=''; root='$root'; p='$p'; s='$s'; w='$w'; cd '$root'; shoot $n"
done
