#!/usr/bin/env bash
# Opens the project of testdata/ for one language server of milestone M24b in Emetos, on your desktop (the real GPU), isolated from
# your configuration, state and cache (docs/MANUAL-CHECKS.md 24b.10). The servers are the ones scripts/fetch-tools.sh put in
# /var/tmp/emetos-tools and the system's clangd, Java and .NET SDK; nothing is installed or written outside a temporary folder.
#
# Usage: scripts/langservers-desktop.sh <rust|cpp|python|typescript|java|csharp>
#
# A copy of the project is made in a temporary folder with a settings.json that points the server's path setting at the tool, and
# `go run ./cmd/emetos <folder> <file>` runs under scripts/isolated.sh (a HOME of its own). The NVIDIA card is kept out of it
# (scripts/gpu-env.sh). The temporary folder is removed when the editor ends. The .NET SDK gets folders of its own and no telemetry
# (the isolation of the C# step).
#
# Exit status: the editor's own; 64 for a usage mistake; 2 when the tool is missing.
set -euo pipefail

root="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
tools="${EMETOS_TOOLS:-/var/tmp/emetos-tools}"
usage() {
  echo "usage: $0 <rust|cpp|python|typescript|java|csharp>" >&2
  exit 64
}
[[ $# -eq 1 ]] || usage
lang="$1"

need() {
  [[ -e "$1" ]] || {
    echo "error: $1 is missing: scripts/fetch-tools.sh $2" >&2
    exit 2
  }
}

# shellcheck source=gpu-env.sh
source "$root/scripts/gpu-env.sh"
work="$(mktemp -d /tmp/emetos-m24b.XXXXXX)"
trap 'rm -rf "$work"' EXIT
trap 'exit 130' INT
trap 'exit 143' TERM
project="$work/project"
config="$work/config"
mkdir -p "$project" "$config"
env_vars=()

case "$lang" in
  rust)
    need "$tools/bin/rust-analyzer" rust-analyzer
    cp -a "$root/testdata/rust/crate/." "$project/"
    printf 'mod geometry;\n\nfn main() {\n    let unused = 5;\n    let wrong: i32 = "text";\n}\n' >"$project/src/main.rs"
    file=src/main.rs
    printf '{"rust-analyzer.server.path": "%s"}\n' "$tools/bin/rust-analyzer" >"$config/settings.json"
    env_vars=("CARGO_HOME=$work/cargo-home" "CARGO_TARGET_DIR=$work/cargo-target")
    ;;
  cpp)
    command -v clangd >/dev/null || {
      echo "error: clangd is not installed" >&2
      exit 2
    }
    cp -a "$root/testdata/cpp/project/." "$project/"
    sed "s|@DIR@|$project|g" "$project/compile_commands.json.in" >"$project/compile_commands.json"
    file=src/main.cpp
    printf '{}\n' >"$config/settings.json"
    ;;
  python)
    need "$tools/venv/bin/basedpyright-langserver" python-tools
    cp -a "$root/testdata/python/project/." "$project/"
    file=main.py
    printf '{"python.languageServer": "basedpyright", "basedpyright.path": "%s"}\n' "$tools/venv/bin/basedpyright-langserver" >"$config/settings.json"
    ;;
  typescript)
    need "$tools/node/node_modules/.bin/typescript-language-server" "typescript-language-server typescript"
    cp -a "$root/testdata/typescript/project/." "$project/"
    mkdir -p "$project/node_modules/emetoslib"
    printf '{"name":"emetoslib","version":"1.0.0","type":"module","main":"index.js","types":"index.d.ts"}\n' >"$project/node_modules/emetoslib/package.json"
    printf '/** Say hello to name. */\nexport declare function greet(name: string): string;\n' >"$project/node_modules/emetoslib/index.d.ts"
    printf 'export function greet(name) {\n  return "hello " + name;\n}\n' >"$project/node_modules/emetoslib/index.js"
    file=src/main.ts
    printf '{"typescript.languageServer.path": "%s", "typescript.disableAutomaticTypeAcquisition": true}\n' "$tools/node/node_modules/.bin/typescript-language-server" >"$config/settings.json"
    ;;
  java)
    need "$tools/jdtls/plugins" jdtls
    cp -a "$root/testdata/java/plain/." "$project/"
    file=src/main/java/dev/emetos/app/App.java
    printf '{"java.jdt.ls.path": "%s"}\n' "$tools/jdtls" >"$config/settings.json"
    ;;
  csharp)
    need "$tools/dotnet-tools/csharp-ls" csharp-ls
    command -v dotnet >/dev/null || {
      echo "error: dotnet is not installed" >&2
      exit 2
    }
    cp -a "$root/testdata/csharp/solution/." "$project/"
    file=App/Program.cs
    printf '{"csharp.languageServer.path": "%s"}\n' "$tools/dotnet-tools/csharp-ls" >"$config/settings.json"
    env_vars=("DOTNET_CLI_HOME=$work/dotnet-home" "NUGET_PACKAGES=$work/nuget-packages" "NUGET_HTTP_CACHE_PATH=$work/nuget-http"
      DOTNET_CLI_TELEMETRY_OPTOUT=1 DOTNET_NOLOGO=1 DOTNET_SKIP_FIRST_TIME_EXPERIENCE=1 DOTNET_ROOT=)
    ;;
  *) usage ;;
esac

cd "$root"
set +e
scripts/isolated.sh --config "$config" -- env "${env_vars[@]}" go run ./cmd/emetos "$project" "$project/$file"
exit $?
