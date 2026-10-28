# Third-party assets

Every asset that Emetos bundles has a folder here, `third_party/<name>/`, with the
license text of the asset as its upstream ships it. `NOTICE` at the repository root
lists all assets, the step that adds each one, and its license.

Assets are copied from the pinned sources in `reference/` by
`scripts/update-assets.sh` (docs/IMPLEMENTATION.md step 0.2). Never edit a copied
asset by hand; change the script and run it again.

A grammar, theme or font whose license is not permissive is not bundled. It is
recorded in `NOTICE` instead.

`third_party/glutter/LICENSE` is Glutter's license, for the two scripts adapted from
it (`scripts/headless-wayland.sh` and `scripts/screenshot-headless.sh`); it is copied
from Glutter's module, not from `reference/`.

`third_party/vscode-textmate/LICENSE.md` is the license of vscode-textmate, whose tokenizer and theme algorithms are
ported (`// Ported from vscode-textmate` comments); no file of it is bundled. The licenses of the Go modules that are linked
into the program come with their sources (`go list -m -f '{{.Dir}}' <module>`) and are listed in `NOTICE`.

`third_party/gomodifytags/` holds the licenses of gomodifytags and its two small libraries, `fatih/structtag` and `fatih/camelcase`, whose algorithms are ported into `extensions/golang/tags.go` (`// Ported from` comments); they are copied from the Go module cache of those modules, not from `reference/`, and no file of them is bundled.

`third_party/gozxing/` holds the license of gozxing (MIT) and the Apache-2.0 text of ZXing, which it ports; both are copied from the Go module cache (`LICENSE` of `github.com/makiuchi-d/gozxing`, and the generic Apache-2.0 text from another module of the cache), and no file of them is bundled: the module is linked in (step 15.5).

`third_party/vscode-shell-integration/` holds the license of VS Code (MIT) and the `cgmanifest.json` that VS Code keeps next to its shell integration scripts. The scripts themselves (bash, zsh, fish and PowerShell) are bundled in `extensions/terminal/shellintegration/` by `scripts/update-assets.sh` and embedded in the program (step 17d.5). They are VS Code's own and not xterm.js's.

`third_party/vscode-terminal-suggest/` holds the license of VS Code (MIT) and the `cgmanifest.json` and `ThirdPartyNotices.txt` of its `terminal-suggest` extension, which name the projects its completion specs come from (withfig/autocomplete and the Amazon Q Developer CLI, both MIT). 49 of the specs, and the builtins of zsh and fish, are converted to JSON by `scripts/specs2json` (called by `scripts/update-assets.sh`) and embedded from `extensions/terminal/suggestdata/` (step 17d.6).
