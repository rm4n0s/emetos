#!/usr/bin/env bash
# Removes what the tests against a real Docker or Podman left behind (CLAUDE.md "Testing notes", step 18.1): the containers,
# networks, volumes and, for Podman, pods that carry the label dev.emetos.test=1, and nothing else. Every list is asked with
# --filter label=dev.emetos.test=1, so the engine's own filter decides, and a list that fails ends the script before anything
# is removed. It never prunes, never runs "system prune" and never stops or removes anything it did not find by the label.
#
#   scripts/container-test-cleanup.sh [--engine docker|podman|all] [--dry-run]
#
#   --engine X   which CLI to clean through; "all" (the default) cleans through every one that is installed
#   --dry-run    only print what would be removed
#
# The programs are $DOCKER and $PODMAN (default: docker and podman); the tests of the script point them at a fake CLI.
# Exit status: 0 when done (also when a CLI is not installed), 1 when a command failed, 64 for a usage mistake.
set -euo pipefail

label="dev.emetos.test=1"
engine=all
dry=0
while [[ $# -gt 0 ]]; do
  case "$1" in
    --engine)
      [[ $# -ge 2 ]] || { echo "usage: $0 [--engine docker|podman|all] [--dry-run]" >&2; exit 64; }
      engine=$2
      shift 2
      ;;
    --dry-run)
      dry=1
      shift
      ;;
    -h | --help)
      sed -n '2,15p' "$0"
      exit 0
      ;;
    *)
      echo "usage: $0 [--engine docker|podman|all] [--dry-run]" >&2
      exit 64
      ;;
  esac
done
case "$engine" in docker | podman | all) ;; *)
  echo "usage: $0 [--engine docker|podman|all] [--dry-run]" >&2
  exit 64
  ;;
esac

# list <cli> <what...>: the ids or names of the labelled things of one kind, one per line. A line that does not look like an id
# or a name is refused, so that nothing an engine prints can turn into an option of the remove command.
list() {
  local cli=$1
  shift
  local out line
  out=$("$cli" "$@" --quiet --filter "label=$label") || return 1
  while IFS= read -r line; do
    [[ -z "$line" ]] && continue
    if [[ ! "$line" =~ ^[A-Za-z0-9][A-Za-z0-9_.-]*$ ]]; then
      echo "container-test-cleanup: $cli $* printed \"$line\", which is not an id or a name; stopping" >&2
      return 1
    fi
    printf '%s\n' "$line"
  done <<<"$out"
}

# remove <cli> <what> <list command...> -- <remove command...>: lists, then removes what was listed.
remove() {
  local cli=$1 what=$2
  shift 2
  local listcmd=() rmcmd=() seen=0
  for a in "$@"; do
    if [[ "$a" == "--" ]]; then
      seen=1
    elif [[ $seen -eq 0 ]]; then
      listcmd+=("$a")
    else
      rmcmd+=("$a")
    fi
  done
  local ids
  ids=$(list "$cli" "${listcmd[@]}") || return 1
  local -a arr=()
  while IFS= read -r line; do
    [[ -n "$line" ]] && arr+=("$line")
  done <<<"$ids"
  if [[ ${#arr[@]} -eq 0 ]]; then
    echo "$cli: no labelled $what"
    return 0
  fi
  if [[ $dry -eq 1 ]]; then
    echo "$cli: would remove ${#arr[@]} $what: ${arr[*]}"
    return 0
  fi
  "$cli" "${rmcmd[@]}" "${arr[@]}" >/dev/null || return 1
  echo "$cli: removed ${#arr[@]} $what: ${arr[*]}"
}

clean() {
  local cli=$1 podman=$2
  # pods first: removing a pod removes its containers
  # (set -e does not reach into a function that is called in an || list, so every step says "|| return 1" itself: a list
  # that fails ends the cleanup of this CLI before the next kind of thing is touched)
  if [[ $podman -eq 1 ]]; then
    remove "$cli" pods pod ps -- pod rm --force || return 1
  fi
  remove "$cli" containers ps --all -- rm --force --volumes || return 1
  remove "$cli" networks network ls -- network rm || return 1
  remove "$cli" volumes volume ls -- volume rm --force || return 1
}

status=0
for which in docker podman; do
  [[ "$engine" == all || "$engine" == "$which" ]] || continue
  if [[ $which == docker ]]; then cli=${DOCKER:-docker}; podman=0; else cli=${PODMAN:-podman}; podman=1; fi
  if ! command -v "$cli" >/dev/null 2>&1; then
    echo "$which: $cli is not installed, skipped"
    continue
  fi
  clean "$cli" "$podman" || status=1
done
exit $status
