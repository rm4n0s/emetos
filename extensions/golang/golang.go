// Package golang is the Go language pack (SPEC §8.1, §10.15): Go. The grammars and language configurations
// are in assets/, copied by scripts/update-assets.sh; the licenses are in third_party/ and NOTICE. Since step 12.3 it also
// declares the language server gopls (SPEC §9.12 "The first server"); the rest of the Go extension is step 14.1 on.
package golang

import (
	"embed"

	"github.com/rm4n0s/emetos/extension"
	"github.com/rm4n0s/emetos/extensions/internal/langpack"
)

// ID is the id of the extension.
const ID = "emetos.golang"

//go:embed all:assets
var assets embed.FS

// New is the Factory of the pack.
func New() extension.Extension {
	return langpack.New(langpack.Spec{
		ID: ID, Name: "Go Language Basics", Description: "Syntax highlighting and language configuration for Go.",
		Assets: assets,
		LanguageServers: []extension.LanguageServerSpec{{
			ID: "gopls", Name: "gopls", Languages: []string{"go", "go.mod", "go.work"},
			Program: "gopls", PathSetting: "go.goplsPath", ConfigurationSection: "go.gopls",
		}},
	})
}
