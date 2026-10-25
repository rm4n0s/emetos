#!/usr/bin/env bash
# Copies the assets that Emetos bundles out of reference/ into the packages that
# embed them, and their licenses into third_party/<name>/ (SPEC §17).
#
# Usage: scripts/update-assets.sh
#
# Run scripts/fetch-reference.sh first. The script is idempotent: a destination
# folder is replaced, so a file removed upstream is removed here. Never edit a
# copied asset by hand; change this script and run it again.
#
# Each step of docs/IMPLEMENTATION.md that bundles assets adds a block at the end
# of this file (the step id is in a comment above its block).
set -euo pipefail

root="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
ref="$root/reference"

if [[ ! -d "$ref" ]]; then
  echo "error: reference/ is missing; run scripts/fetch-reference.sh" >&2
  exit 1
fi

# copy_file <path under reference/> <destination under the repository>
copy_file() {
  local src="$ref/$1" dst="$root/$2"
  [[ -f "$src" ]] || { echo "error: reference/$1 is not a file" >&2; exit 1; }
  mkdir -p "$(dirname "$dst")"
  cp -f "$src" "$dst"
  chmod 0644 "$dst"
}

# copy_tree <folder under reference/> <destination folder under the repository>
copy_tree() {
  local src="$ref/$1" dst="$root/$2"
  [[ -d "$src" ]] || { echo "error: reference/$1 is not a folder" >&2; exit 1; }
  rm -rf "$dst"
  mkdir -p "$dst"
  cp -a "$src/." "$dst/"
  chmod -R u+rw,go+r "$dst"
}

# copy_license <third_party name> <path under reference/> [<path> ...]
# Copies license files into third_party/<name>/ under their own names.
copy_license() {
  local name=$1
  shift
  local p
  for p in "$@"; do
    copy_file "$p" "third_party/$name/$(basename "$p")"
  done
}

# copy_extension <extension folder under reference/vscode/extensions>
# Copies what the language registry reads of a VS Code extension (SPEC §8.1) into
# internal/core/language/assets/<name>/: package.json, the language configurations,
# the grammars (syntaxes/), languages/ when there is one (git-base) and cgmanifest.json, which names the upstream of each
# grammar. The build scripts, snippets and translations stay behind. A grammar in the
# plist format is converted to JSON (internal/core/language/internal/plist2json) and
# package.json is changed to name the .json file, as the loader reads JSON only.
copy_extension() {
  local name=$1 src="$ref/vscode/extensions/$1" dst="$root/internal/core/language/assets/$1"
  [[ -d "$src" ]] || { echo "error: reference/vscode/extensions/$name is not a folder" >&2; exit 1; }
  rm -rf "$dst"
  mkdir -p "$dst"
  cp -a "$src/package.json" "$src/cgmanifest.json" "$src/syntaxes" "$dst/"
  local f
  for f in "$src"/*language-configuration.json; do
    [[ -f "$f" ]] && cp -a "$f" "$dst/"
  done
  if [[ -d "$src/languages" ]]; then
    cp -a "$src/languages" "$dst/"
  fi
  rm -f "$dst"/syntaxes/Readme.md
  local base manifest
  for f in "$dst"/syntaxes/*; do
    case "$f" in
      *.json) ;;
      *)
        base="$(basename "$f")"
        (cd "$root" && go run ./internal/core/language/internal/plist2json "$f" "$f.json")
        rm -f "$f"
        manifest="$(<"$dst/package.json")"
        printf '%s\n' "${manifest//"$base\""/"$base.json\""}" >"$dst/package.json"
        ;;
    esac
  done
  chmod -R u+rw,go+r "$dst"
}

# ---- Assets, by the step that adds them -------------------------------------

# Step 0.4: the icon fonts, with the Codicon name table the generator reads.
copy_file codicons/package/dist/codicon.ttf internal/gui/kit/fonts/codicon.ttf
copy_file codicons/package/dist/codicon.csv internal/gui/kit/fonts/codicon.csv
copy_license codicons codicons/package/LICENSE codicons/package/LICENSE-CODE
copy_file vscode/extensions/theme-seti/icons/seti.woff internal/gui/kit/fonts/seti.woff
copy_license seti vscode/extensions/theme-seti/ThirdPartyNotices.txt vscode/LICENSE.txt

# Step 4.6: the first languages. The grammars of these extensions come from the upstream
# projects named in their cgmanifest.json, all permissive (MIT, and the TextMate bundle
# license); the texts are in VS Code's ThirdPartyNotices.txt.
for ext in go json yaml markdown-basics; do
  copy_extension "$ext"
done
copy_license vscode-extensions vscode/LICENSE.txt vscode/ThirdPartyNotices.txt

# Step 4.6b: Python, JavaScript (and JavaScript React) and TypeScript (and TypeScript React). The
# regular expression grammar of JavaScript, source.js.regexp, is a plist and is converted.
for ext in python javascript typescript-basics; do
  copy_extension "$ext"
done

# Step 4.10: web and script languages: HTML (which embeds CSS and JavaScript), CSS, SCSS, Less, XML (and
# XSL), SQL, Shell Script, Makefile, Dockerfile, INI (and Properties). Their grammars come from MIT
# projects, the TextMate bundles (TextMate bundle license) and moby (Apache-2.0), named in each cgmanifest.json.
for ext in html css scss less xml sql shellscript make docker ini; do
  copy_extension "$ext"
done

# Step 4.11: systems and git. C, C++ and CUDA C++ (the cpp extension), Rust, Diff, and the git-base
# extension (Git commit and rebase messages, Ignore files). The Go module grammars come from
# golang/vscode-go, which is not an extension in the format of VS Code's own: its package.json has
# much more than the three languages, so a package.json that names only go.mod, go.work (which uses
# the go.mod grammar) and go.sum is written here, with the grammars and the language configuration.
for ext in cpp rust diff git-base; do
  copy_extension "$ext"
done
gomod_dst="$root/internal/core/language/assets/go-mod"
rm -rf "$gomod_dst"
mkdir -p "$gomod_dst/syntaxes"
copy_file vscode-go/extension/syntaxes/go.mod.tmGrammar.json internal/core/language/assets/go-mod/syntaxes/go.mod.tmGrammar.json
copy_file vscode-go/extension/syntaxes/go.sum.tmGrammar.json internal/core/language/assets/go-mod/syntaxes/go.sum.tmGrammar.json
copy_file vscode-go/extension/languages/go.mod.language-configuration.json internal/core/language/assets/go-mod/go.mod.language-configuration.json
cat >"$gomod_dst/package.json" <<'JSON'
{
  "name": "go-mod",
  "description": "Go module, workspace and checksum files, from golang/vscode-go (generated by scripts/update-assets.sh)",
  "contributes": {
    "languages": [
      {
        "id": "go.mod",
        "filenames": ["go.mod"],
        "aliases": ["Go Module File"],
        "configuration": "./go.mod.language-configuration.json"
      },
      {
        "id": "go.work",
        "filenames": ["go.work"],
        "aliases": ["Go Work File"],
        "configuration": "./go.mod.language-configuration.json"
      },
      {
        "id": "go.sum",
        "filenames": ["go.sum"],
        "aliases": ["Go Checksum File"]
      }
    ],
    "grammars": [
      {"language": "go.mod", "scopeName": "go.mod", "path": "./syntaxes/go.mod.tmGrammar.json"},
      {"language": "go.work", "scopeName": "go.mod", "path": "./syntaxes/go.mod.tmGrammar.json"},
      {"language": "go.sum", "scopeName": "go.sum", "path": "./syntaxes/go.sum.tmGrammar.json"}
    ]
  }
}
JSON
copy_license vscode-go vscode-go/LICENSE

# Step 4.7: the bundled color themes (SPEC §8.4): the six that VS Code ships as its defaults, and
# the Visual Studio Dark and Light themes that Dark+ and Light+ include. The manifest and its
# translations give the names; the 2026 themes are not bundled.
themes_dst="$root/internal/core/theme/assets"
rm -rf "$themes_dst"
mkdir -p "$themes_dst/themes"
for f in package.json package.nls.json; do
  copy_file "vscode/extensions/theme-defaults/$f" "internal/core/theme/assets/$f"
done
for f in dark_modern dark_plus dark_vs light_modern light_plus light_vs hc_black hc_light; do
  copy_file "vscode/extensions/theme-defaults/themes/$f.json" "internal/core/theme/assets/themes/$f.json"
done
copy_license vscode-extensions vscode/LICENSE.txt vscode/ThirdPartyNotices.txt

# Step 5.10: Emmet (MIT). The markup snippets (html.json, and the xsl and pug ones for those syntaxes), the
# default variables and the Lorem ipsum vocabularies that internal/core/emmet embeds.
for f in html variables xsl pug; do
  copy_file "emmet/src/snippets/$f.json" "internal/core/emmet/assets/$f.json"
done
for f in latin russian spanish; do
  copy_file "emmet/src/markup/lorem/$f.json" "internal/core/emmet/assets/lorem/$f.json"
done
copy_license emmet emmet/LICENSE

# Step 5.11: Emmet (MIT). The stylesheet (CSS) snippets that internal/core/emmet embeds (the license is
# copied by the Step 5.10 block above).
copy_file "emmet/src/snippets/css.json" "internal/core/emmet/assets/css.json"

# Step 6.14: the Seti file icon theme (SPEC §10.8): the mapping from file names, extensions and languages
# to glyphs of the Seti font, in VS Code's icon theme format (the font itself is the Step 0.4 copy).
# internal/core/theme embeds it; its license is the Seti one of Step 0.4 (third_party/seti).
rm -rf "$root/internal/core/theme/iconassets"
copy_file vscode/extensions/theme-seti/icons/vs-seti-icon-theme.json internal/core/theme/iconassets/vs-seti-icon-theme.json

# Step 10.2: the license of vscode-textmate (MIT), whose tokenizer and theme algorithms are ported
# (core/textmate, core/theme; "Ported from vscode-textmate" comments). No file of it is bundled.
copy_license vscode-textmate vscode-textmate/LICENSE.md

echo "update-assets: done"
