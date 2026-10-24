package app

import (
	"context"

	"github.com/rm4n0s/emetos/internal/core/document"
	"github.com/rm4n0s/emetos/internal/core/keybinding"
	"github.com/rm4n0s/emetos/internal/core/settings"
)

// The `emetos-virtual:` documents of the process (SPEC §7.1): generated, read-only text that opens in a text editor.
//
// Ported from VS Code: src/vs/workbench/services/preferences/browser/preferencesService.ts (openRawDefaultSettings,
// the defaultsettings: resource) and src/vs/workbench/services/preferences/common/preferencesModels.ts

const (
	// DefaultSettingsPath is the path of the URI of the default settings document, emetos-virtual:/defaultSettings.json.
	DefaultSettingsPath = "/defaultSettings.json"
	// DefaultSettingsLabel is the name of the default settings document in its tab.
	DefaultSettingsLabel = "Default Settings"
)

// registerVirtualDocuments makes the registry of the virtual documents with the providers Emetos has: the default
// settings, made from the registry of settings each time the document is opened, and the default keyboard
// shortcuts, made from the keymap.
func (p *Process) registerVirtualDocuments() {
	p.Virtual = document.NewVirtualRegistry()
	err := p.Virtual.Register(DefaultSettingsPath, document.VirtualProvider{
		Label: DefaultSettingsLabel, Language: "jsonc",
		Text: func(context.Context) (string, error) { return settings.DefaultSettingsJSON(p.Settings), nil },
	})
	if err != nil {
		// the path is fixed and the registry is new: this is a broken build
		panic(err)
	}
	err = p.Virtual.Register(DefaultKeybindingsPath, document.VirtualProvider{
		Label: DefaultKeybindingsLabel, Language: "jsonc",
		Text: func(context.Context) (string, error) { return keybinding.DefaultKeybindingsJSON(p.Keymap), nil },
	})
	if err != nil {
		panic(err)
	}
}
