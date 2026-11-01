package python

import (
	"context"
	"strings"

	"github.com/rm4n0s/emetos/extension"
)

// Organize Imports from the editor (Shift+Alt+O, the Source Action menu and "source.organizeImports" on save) for Pyright and basedpyright
// (step 24b.5, SPEC §10.34 "As built (step 24b.5)", decision 985). Recent Pyright does not offer sorting the imports as a code action, only
// as the command pyright.organizeimports, so the editor found no action of the kind and the shortcut was disabled. The pack declares the
// kind and offers an action that runs its own command, python.organizeImports, which asks the server. python-lsp-server offers the action
// itself (when a plugin such as pylsp-isort is installed), so the pack adds none for it.

// organizeImportsProvider is the code action provider of the pack.
type organizeImportsProvider struct{ p *python }

// CodeActionKinds implements extension.CodeActionKinds.
func (organizeImportsProvider) CodeActionKinds() []string { return []string{organizeImportsKind} }

// ProvideCodeActions implements extension.CodeActionProvider: the action that organizes the imports, when that kind is wanted and the
// server in use is Pyright or basedpyright.
func (o organizeImportsProvider) ProvideCodeActions(_ context.Context, _ extension.TextDocument, req extension.CodeActionRequest) ([]extension.CodeAction, error) {
	if !wantsKind(req.Only, organizeImportsKind) {
		return nil, nil
	}
	switch server, _ := extension.SettingValue[string](o.p.api.Settings(), LanguageServerSetting); server {
	case ServerPylsp, ServerNone:
		return nil, nil
	}
	cmd, err := extension.NewCommand("Organize Imports", OrganizeImportsCommand)
	if err != nil {
		return nil, err
	}
	return []extension.CodeAction{{Title: "Organize Imports", Kind: organizeImportsKind, Command: &cmd}}, nil
}

// wantsKind tells whether a request for the kinds in only wants an action of the kind: an empty list wants every kind, and a kind wants the
// kinds below it (source wants source.organizeImports).
func wantsKind(only []string, kind string) bool {
	if len(only) == 0 {
		return true
	}
	for _, o := range only {
		if o == kind || o == "" || strings.HasPrefix(kind, o+".") {
			return true
		}
	}
	return false
}
