#!/usr/bin/env bash
# Fetches a pinned tool of the real-server tests of milestones M24b and M25 into /var/tmp/emetos-tools/ (never into the user's own folders).
#
# Usage: scripts/fetch-tools.sh <name>...
#
# Reads scripts/tool-versions.txt:
#   gzbin <name> <version> <sha256 of the download> <url> <sha256 of the program>   gunzipped into /var/tmp/emetos-tools/bin/<name>
#   file  <name> <version> <sha256 of the download> <url>                           kept as /var/tmp/emetos-tools/download/<name>-<version>
#   venv  <name> <version> <sha256 of the file> <requirements file in the repository>  pip install --require-hashes into /var/tmp/emetos-tools/venv (python3 -m venv)
#   tgz   <name> <version> <sha256 of the download> <url> <folder>                  unpacked into /var/tmp/emetos-tools/<folder>
#   gobin <name> <version> <h1: hash of the module> <package> <module>              go install <package>@<version> into /var/tmp/emetos-tools/bin (caches below /var/tmp/emetos-tools)
#   dotnettool <name> <version> <sha256 of the .nupkg> <url of the .nupkg>         dotnet tool install --tool-path /var/tmp/emetos-tools/dotnet-tools, from the verified file
#   npm   <name> <version> <integrity> <package> [<spec>]                           npm install --prefix /var/tmp/emetos-tools/node --ignore-scripts <package>@<version> (or <spec>)
# A download whose sha256 differs from the pin is refused. A tool that is there already with the right sha256 is left alone; the
# path of what was installed is printed. The tests of the packs skip when their program is not there (docs/IMPLEMENTATION.md "M24b").
set -euo pipefail

root="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
versions="$root/scripts/tool-versions.txt"
tools="${EMETOS_TOOLS:-/var/tmp/emetos-tools}"

[[ $# -ge 1 ]] || { echo "usage: $0 <name>..." >&2; exit 64; }
mkdir -p "$tools/bin" "$tools/download"

for want in "$@"; do
  found=0
  while read -r kind name version sha url extra; do
    [[ -z "${kind:-}" || "$kind" == \#* || "$name" != "$want" ]] && continue
    found=1
    case "$kind" in
      gzbin)
        dst="$tools/bin/$name"
        if [[ -x "$dst" && "$(sha256sum "$dst" | cut -d' ' -f1)" == "$extra" ]]; then
          echo "$dst"
          continue
        fi
        tmp="$(mktemp -d "$tools/download/.fetch.XXXXXX")"
        curl -fsSL --max-time 600 -o "$tmp/download.gz" "$url"
        [[ "$(sha256sum "$tmp/download.gz" | cut -d' ' -f1)" == "$sha" ]] || { echo "error: $name: the download does not match the pinned sha256" >&2; rm -rf "$tmp"; exit 1; }
        gunzip -c "$tmp/download.gz" >"$tmp/$name"
        [[ "$(sha256sum "$tmp/$name" | cut -d' ' -f1)" == "$extra" ]] || { echo "error: $name: the program does not match the pinned sha256" >&2; rm -rf "$tmp"; exit 1; }
        chmod 0755 "$tmp/$name"
        mv -f "$tmp/$name" "$dst"
        cp -f "$tmp/download.gz" "$tools/download/$name-$version.gz"
        rm -rf "$tmp"
        echo "$dst"
        ;;
      file)
        dst="$tools/download/$name-$version"
        if [[ -f "$dst" && "$(sha256sum "$dst" | cut -d' ' -f1)" == "$sha" ]]; then
          echo "$dst"
          continue
        fi
        tmp="$(mktemp -d "$tools/download/.fetch.XXXXXX")"
        curl -fsSL --max-time 600 -o "$tmp/file" "$url"
        [[ "$(sha256sum "$tmp/file" | cut -d' ' -f1)" == "$sha" ]] || { echo "error: $name: the download does not match the pinned sha256" >&2; rm -rf "$tmp"; exit 1; }
        mv -f "$tmp/file" "$dst"
        rm -rf "$tmp"
        echo "$dst"
        ;;
      venv)
        req="$root/$url"
        [[ "$(sha256sum "$req" | cut -d' ' -f1)" == "$sha" ]] || { echo "error: $name: $url does not match the pinned sha256" >&2; exit 1; }
        if [[ -f "$tools/venv/.emetos-$name" && "$(cat "$tools/venv/.emetos-$name")" == "$sha" ]]; then
          echo "$tools/venv/bin"
          continue
        fi
        [[ -x "$tools/venv/bin/python3" ]] || python3 -m venv "$tools/venv" </dev/null
        "$tools/venv/bin/pip" install --disable-pip-version-check --require-hashes --no-deps -q -r "$req" </dev/null
        echo "$sha" >"$tools/venv/.emetos-$name"
        echo "$tools/venv/bin"
        ;;
      tgz)
        dst="$tools/$extra"
        archive="$tools/download/$name-$version.tar.gz"
        if [[ -f "$dst/.emetos-$name" && "$(cat "$dst/.emetos-$name")" == "$sha" ]]; then
          echo "$dst"
          continue
        fi
        tmp="$(mktemp -d "$tools/download/.fetch.XXXXXX")"
        curl -fsSL --max-time 900 -o "$tmp/download.tar.gz" "$url"
        [[ "$(sha256sum "$tmp/download.tar.gz" | cut -d' ' -f1)" == "$sha" ]] || { echo "error: $name: the download does not match the pinned sha256" >&2; rm -rf "$tmp"; exit 1; }
        # every member is a relative path with no .. in it
        if tar -tzf "$tmp/download.tar.gz" | grep -qE '^/|(^|/)\.\.(/|$)'; then
          echo "error: $name: the archive has a member outside its folder" >&2; rm -rf "$tmp"; exit 1
        fi
        mkdir -p "$tmp/unpacked"
        tar -xzf "$tmp/download.tar.gz" -C "$tmp/unpacked"
        echo "$sha" >"$tmp/unpacked/.emetos-$name"
        rm -rf "$dst"
        mv "$tmp/unpacked" "$dst"
        mv -f "$tmp/download.tar.gz" "$archive"
        rm -rf "$tmp"
        echo "$dst"
        ;;
      dotnettool)
        # A .NET tool is installed from the .nupkg that was downloaded and compared with the pinned sha256, through a NuGet.config whose only
        # source is the folder that holds it, so that nothing else is fetched (a tool package holds all its files). The SDK is the system's
        # (dotnet on PATH); it runs with a home folder, a package folder and a CLI home of its own under $tools, so that it writes nothing to
        # the user's home, and with its telemetry and first-run experience off.
        dst="$tools/dotnet-tools"
        if [[ -f "$dst/.emetos-$name" && "$(cat "$dst/.emetos-$name")" == "$sha" && -x "$dst/$name" ]]; then
          echo "$dst"
          continue
        fi
        command -v dotnet >/dev/null || { echo "error: $name: dotnet (the .NET SDK) is not installed" >&2; exit 1; }
        tmp="$(mktemp -d "$tools/download/.fetch.XXXXXX")"
        curl -fsSL --max-time 600 -o "$tmp/$name.$version.nupkg" "$url"
        [[ "$(sha256sum "$tmp/$name.$version.nupkg" | cut -d' ' -f1)" == "$sha" ]] || { echo "error: $name: the download does not match the pinned sha256" >&2; rm -rf "$tmp"; exit 1; }
        cat >"$tmp/NuGet.config" <<EOF_NUGET
<?xml version="1.0" encoding="utf-8"?>
<configuration>
  <packageSources>
    <clear />
    <add key="pinned" value="$tmp" />
  </packageSources>
</configuration>
EOF_NUGET
        mkdir -p "$tools/dotnet-home" "$tools/nuget-packages" "$dst"
        dotnet_env=(HOME="$tools/dotnet-home" DOTNET_CLI_HOME="$tools/dotnet-home" NUGET_PACKAGES="$tools/nuget-packages" NUGET_HTTP_CACHE_PATH="$tools/dotnet-home/http-cache"
          DOTNET_CLI_TELEMETRY_OPTOUT=1 DOTNET_NOLOGO=1 DOTNET_SKIP_FIRST_TIME_EXPERIENCE=1)
        env "${dotnet_env[@]}" dotnet tool uninstall "$name" --tool-path "$dst" >/dev/null 2>&1 || true
        env "${dotnet_env[@]}" dotnet tool install "$name" --version "$version" --tool-path "$dst" --configfile "$tmp/NuGet.config" </dev/null >/dev/null
        [[ -x "$dst/$name" ]] || { echo "error: $name: the tool was not installed" >&2; rm -rf "$tmp"; exit 1; }
        echo "$sha" >"$dst/.emetos-$name"
        mv -f "$tmp/$name.$version.nupkg" "$tools/download/$name-$version.nupkg"
        rm -rf "$tmp"
        echo "$dst"
        ;;
      npm)
        have="$(python3 -I -c 'import json,sys; d=json.load(open(sys.argv[1])); print(d["packages"].get("node_modules/"+sys.argv[2],{}).get("version","")+" "+d["packages"].get("node_modules/"+sys.argv[2],{}).get("integrity",""))' "$tools/node/package-lock.json" "$url" 2>/dev/null || true)"
        if [[ "$have" == "$version $sha" ]]; then
          echo "$tools/node/node_modules/.bin"
          continue
        fi
        mkdir -p "$tools/node"
        npm install --prefix "$tools/node" --ignore-scripts --no-audit --no-fund --save-exact -s "${extra:-$url@$version}" </dev/null
        have="$(python3 -I -c 'import json,sys; d=json.load(open(sys.argv[1])); print(d["packages"]["node_modules/"+sys.argv[2]]["version"]+" "+d["packages"]["node_modules/"+sys.argv[2]]["integrity"])' "$tools/node/package-lock.json" "$url")"
        [[ "$have" == "$version $sha" ]] || { echo "error: $name: the installed package does not match the pinned integrity" >&2; exit 1; }
        echo "$tools/node/node_modules/.bin"
        ;;
      gobin)
        # A Go program is built from the module version `go install <package>@<version>` names, with the module cache, build cache and
        # GOPATH below $tools (so that nothing goes to the user's folders), GOBIN=$tools/bin, the toolchain of the go on PATH (no download
        # of another) and the environment of the user's Go settings out of the way (GOFLAGS, GOENV). The module in the binary must have the
        # h1: hash that is pinned.
        dst="$tools/bin/$name"
        mod="${extra:?error: $name: the module path is missing in the pin}"
        have_mod() { go version -m "$dst" 2>/dev/null | awk -v m="$mod" -v v="$version" '$1 == "mod" && $2 == m && $3 == v { print $4 }'; }
        if [[ -x "$dst" && "$(have_mod)" == "$sha" ]]; then
          echo "$dst"
          continue
        fi
        command -v go >/dev/null || { echo "error: $name: go is not installed" >&2; exit 1; }
        mkdir -p "$tools/gopath" "$tools/gomodcache" "$tools/gocache"
        env GOBIN="$tools/bin" GOPATH="$tools/gopath" GOMODCACHE="$tools/gomodcache" GOCACHE="$tools/gocache" \
          GOFLAGS= GOENV=off GOTOOLCHAIN=local GO111MODULE=on CGO_ENABLED=0 GOWORK=off \
          go install "$url@$version" </dev/null
        [[ "$(have_mod)" == "$sha" ]] || { echo "error: $name: the installed module does not match the pinned hash" >&2; rm -f "$dst"; exit 1; }
        echo "$dst"
        ;;
      *)
        echo "error: $versions: unknown kind $kind" >&2
        exit 1
        ;;
    esac
  done <"$versions"
  [[ $found -eq 1 ]] || { echo "error: $want is not in scripts/tool-versions.txt" >&2; exit 1; }
done
