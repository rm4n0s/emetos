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
