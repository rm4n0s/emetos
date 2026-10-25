// Package yaml is the YAML language pack (SPEC §8.1, §10.15): YAML and Compose. The grammars and language configurations
// are in assets/, copied by scripts/update-assets.sh; the licenses are in third_party/ and NOTICE.
package yaml

import (
	"embed"

	"github.com/rm4n0s/emetos/extension"
	"github.com/rm4n0s/emetos/extensions/internal/langpack"
)

// ID is the id of the extension.
const ID = "emetos.yaml"

//go:embed all:assets
var assets embed.FS

// New is the Factory of the pack.
func New() extension.Extension {
	return langpack.New(langpack.Spec{
		ID: ID, Name: "YAML Language Basics", Description: "Syntax highlighting and language configuration for YAML and Compose.",
		Assets: assets,
	})
}
