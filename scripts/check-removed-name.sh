#!/usr/bin/env bash
# Checks that the repository does not hold the name of the extension that step 23.1 removed (SPEC D32): not in a file, not in the
# name of a file, in any case. The word is built here from its rot13 form so that this script does not hold it either.
#
# Usage: scripts/check-removed-name.sh
# Prints every hit (file:line:text, or the name of a file) and exits 1 when there is one; prints nothing and exits 0 when there is none.
set -euo pipefail

root="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
cd "$root"

word="$(echo cnenfvgr | tr 'a-z' 'n-za-m')"
status=0

if git grep -n -i --untracked -e "$word"; then
  status=1
fi
if git ls-files -c -o --exclude-standard | grep -i -e "$word"; then
  status=1
fi
exit "$status"
