// Package javascript is the JavaScript language pack (SPEC §8.1, §10.15): JavaScript and JavaScript React. The grammars and language configurations
// are in assets/, copied by scripts/update-assets.sh; the licenses are in third_party/ and NOTICE. The language server of these languages is
// typescript-language-server, which the TypeScript pack (extensions/typescript) runs for both packs, with the javascript.* settings and the
// commands of JavaScript (SPEC §10.34 "As built (step 24b.5)"); this pack has nothing to run.
package javascript

import (
	"embed"

	"github.com/rm4n0s/emetos/extension"
	"github.com/rm4n0s/emetos/extensions/internal/langpack"
)

// ID is the id of the extension.
const ID = "emetos.javascript"

//go:embed all:assets
var assets embed.FS

// New is the Factory of the pack.
func New() extension.Extension {
	return langpack.New(langpack.Spec{
		ID: ID, Name: "JavaScript", Description: "Syntax highlighting and language configuration for JavaScript and JavaScript React; the TypeScript pack serves them with typescript-language-server.",
		Assets: assets,
	})
}
