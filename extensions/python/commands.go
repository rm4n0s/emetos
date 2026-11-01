package python

import (
	"context"
	"path/filepath"

	"github.com/rm4n0s/emetos/extension"
)

// The commands of the Python pack (SPEC §10.34 "Python"), with the ids and titles VS Code's Python extension gives them.

// The ids of the commands.
const (
	// SetInterpreterCommand lists the interpreters and keeps the one the user picks for the folder ("Python: Select Interpreter"). A string
	// argument is the path to choose without asking.
	SetInterpreterCommand = "python.setInterpreter"
	// OrganizeImportsCommand sorts the imports of the file in front with the language server (Pyright's command, or the code action source.organizeImports).
	OrganizeImportsCommand = "python.organizeImports"
	// RestartCommand restarts the language server ("Python: Restart Language Server").
	RestartCommand = "python.analysis.restartLanguageServer"
)

// organizeImportsKind is the kind of code action that sorts the imports.
const organizeImportsKind = "source.organizeImports"

// commandCategory is the category of the commands in the palette.
const commandCategory = "Python"

// commandSpecs are the commands the manifest declares.
func commandSpecs() []extension.CommandSpec {
	return []extension.CommandSpec{
		{ID: SetInterpreterCommand, Title: "Select Interpreter", Category: commandCategory, Description: "Chooses the Python interpreter of this folder: the language server, the debugger and the tests then run it."},
		{ID: OrganizeImportsCommand, Title: "Organize Imports", Category: commandCategory, When: "editorLangId == python", Description: "Sorts the imports of the file with the language server."},
		{ID: RestartCommand, Title: "Restart Language Server", Category: commandCategory, Description: "Stops the Python language server and starts it again."},
	}
}

// handlers are the commands of the pack and what runs them.
func (p *python) handlers() map[string]extension.CommandHandler {
	return map[string]extension.CommandHandler{
		SetInterpreterCommand:  p.setInterpreter,
		OrganizeImportsCommand: p.organizeImports,
		RestartCommand:         func(context.Context, any) error { p.restart(); return nil },
	}
}

// setInterpreter is python.setInterpreter: with a path as its argument the interpreter is chosen at once, else the picker is shown.
func (p *python) setInterpreter(_ context.Context, args any) error {
	path, _ := args.(string)
	if path == "" {
		p.interps.Select()
		return nil
	}
	if p.api.Origins().Current() != extension.HostOrigin {
		p.api.Notifications().Info("The Python interpreter is a choice of the host and cannot be changed from here.")
		return nil
	}
	if err := p.interps.Choose(path); err != nil {
		p.interps.fail("The interpreter cannot be chosen", err)
	}
	return nil
}

// restart restarts the language server.
func (p *python) restart() {
	if err := p.api.LanguageServers().Restart(ServerID); err != nil && !benign(err) {
		p.api.Log().Warn("the Python language server cannot be restarted", "err", err)
		p.api.Notifications().Error("The language server cannot be restarted: " + err.Error())
	}
}

// The commands of Pyright and of basedpyright (which renamed it) that sort the imports of a file the client has open: the server answers
// with workspace/applyEdit. Recent Pyright does not offer sorting the imports as a code action.
const (
	pyrightOrganizeImportsCommand      = "pyright.organizeimports"
	basedpyrightOrganizeImportsCommand = "basedpyright.organizeimports"
)

// organizeImports is python.organizeImports: Pyright and basedpyright sort the imports with their own commands, python-lsp-server through
// the code action source.organizeImports that its plugins (pylsp-isort) offer.
func (p *python) organizeImports(ctx context.Context, _ any) error {
	const what = "Organize Imports"
	doc, ok := p.api.Documents().Active()
	if !ok || doc.LanguageID != "python" {
		p.api.Notifications().Info(what + " needs a Python file: open one first.")
		return nil
	}
	st, err := p.api.LanguageServers().Status(ServerID)
	if err != nil {
		p.api.Notifications().Error(what + ": " + err.Error())
		return nil
	}
	if st.State != extension.ServerRunning {
		p.api.Notifications().Warn(what + " needs the language server, which is " + stateWord(st.State) + ". Its state is in the status bar.")
		return nil
	}
	server, _ := extension.SettingValue[string](p.api.Settings(), LanguageServerSetting)
	if server == ServerPylsp {
		p.organizeWithCodeAction(ctx, doc)
		return nil
	}
	command := pyrightOrganizeImportsCommand
	if server != ServerPyright {
		command = basedpyrightOrganizeImportsCommand // basedpyright is the default
	}
	err = p.api.LanguageServers().ExecuteCommand(ctx, ServerID, command, []any{doc.URI}, func(err error) {
		if err != nil {
			p.api.Notifications().Error(what + ": the language server failed: " + err.Error())
		}
	})
	if err != nil {
		p.api.Notifications().Error(what + ": " + err.Error())
	}
	return nil
}

// organizeWithCodeAction asks the server for the code action that sorts the imports of the file and applies it.
func (p *python) organizeWithCodeAction(ctx context.Context, doc extension.ActiveDocument) {
	const what = "Organize Imports"
	req := extension.CodeActionRequest{Only: []string{organizeImportsKind}}
	if len(doc.Selections) > 0 {
		req.Range = extension.Range{Start: doc.Selections[0].Anchor, End: doc.Selections[0].Active}
	}
	name := filepath.Base(doc.URI)
	err := p.api.LanguageServers().ApplyCodeAction(ctx, ServerID, doc.URI, req, func(title string, err error) {
		switch {
		case err != nil:
			p.api.Notifications().Error(what + ": the language server failed: " + err.Error())
		case title == "":
			p.api.Notifications().Info(what + ": the language server offers nothing to change in " + name + ". python-lsp-server needs a plugin that sorts imports, such as pylsp-isort.")
		}
	})
	if err != nil {
		p.api.Notifications().Error(what + ": " + err.Error())
	}
}

// stateWord says in words what state the server is in.
func stateWord(s extension.ServerState) string {
	switch s {
	case extension.ServerStarting:
		return "starting"
	case extension.ServerMissing:
		return "not available"
	case extension.ServerFailed:
		return "stopped after crashing"
	case extension.ServerStopped:
		return "stopped"
	}
	return "not running"
}
