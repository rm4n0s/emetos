#!/usr/bin/env bash
# Writes extensions/rust/ra-settings.json: the settings of rust-analyzer's VS Code extension that change what the server does (their
# keys, types, defaults, enums and descriptions), from the package.json of the pinned release (scripts/tool-versions.txt; MIT OR
# Apache-2.0, its LICENSE-MIT is third_party/rust-analyzer/LICENSE-MIT, NOTICE item 22). The settings of the client alone are left out:
# the debugger, the status bar of the VS Code extension, its trace and server.* (Emetos has its own, SPEC §10.34).
#
# Usage: scripts/update-rust-analyzer-settings.sh
#
# Needs jq. It fetches package.json through scripts/fetch-tools.sh (into /var/tmp/emetos-tools/download, checked against its pinned
# sha256). Never edit the output by hand.
set -euo pipefail

root="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
pkg="$("$root/scripts/fetch-tools.sh" rust-analyzer-package.json | tail -n 1)"
out="$root/extensions/rust/ra-settings.json"

jq -S '
  def types:
    if (.type | type) == "string" then [.type]
    elif (.type | type) == "array" then .type
    elif has("anyOf") then [.anyOf[] | types] | add
    else [] end;
  def uniq: reduce .[] as $x ([]; if index([$x]) then . else . + [$x] end);
  def client_only:
    test("^rust-analyzer\\.(debug\\.|server\\.|trace\\.|statusBar\\.|initializeStopped$|restartServerOnConfigChange$|showDependenciesExplorer$|showRequestFailedErrorNotification$|showSyntaxTree$|showUnlinkedFileNotification$|testExplorer$|runnables\\.problemMatcher$|runnables\\.askBeforeUpdateTest$|semanticHighlighting\\.nonStandardTokens$|typing\\.continueCommentsOnNewline$)");
  [.contributes.configuration[].properties // {} | to_entries[]
    | select(.key | client_only | not)
    | . as $e
    | ($e.value | types | uniq) as $t
    | ($t | map(select(. != "null"))) as $main
    | ($t | map(select(. == "null"))) as $nul
    | {
        key: $e.key,
        type: ($main[0] // "object"),
        alsoTypes: (($main[1:] + (if ($nul | length) > 0 or $e.value.default == null then ["null"] else [] end)) | uniq),
        default: $e.value.default,
        enum: $e.value.enum,
        enumDescriptions: $e.value.enumDescriptions,
        description: ($e.value.markdownDescription // $e.value.description // ""),
        minimum: $e.value.minimum,
        maximum: $e.value.maximum
      }
    | with_entries(select(.key == "default" or .value != null))
    | if (.alsoTypes | length) == 0 then del(.alsoTypes) else . end
  ] | sort_by(.key)' "$pkg" >"$out"
chmod 0644 "$out"
echo "$out: $(jq length "$out") settings"
