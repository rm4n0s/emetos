package golang

import (
	"context"

	"github.com/rm4n0s/emetos/extension"
)

// "Go: Fill Struct" (SPEC §10.17 "As built (step 14.2)"): the code action of gopls that fills a struct literal with the fields it
// lacks, run on the literal at the cursor. gopls does the work; without it there is nothing to run.

// FillStructCommand is the command that fills the struct literal at the cursor.
const FillStructCommand = ID + ".fillStruct"

// fillStructKind is the kind of the code action of gopls.
const fillStructKind = "refactor.rewrite.fillStruct"

// fillStructCommand is the command "Go: Fill Struct".
func (g *golang) fillStructCommand(ctx context.Context, _ any) error {
	const what = "Fill Struct"
	file, ok := g.activeGoFile(ctx, what)
	if !ok {
		return nil
	}
	if !g.editor.hasSel {
		g.api.Notifications().Info(what + ": click in the struct literal first.")
		return nil
	}
	if g.server.State != extension.ServerRunning {
		g.api.Notifications().Warn(what + " needs gopls, which is " + serverWord(g.server.State) + ". The state of the language server is in the status bar.")
		return nil
	}
	start, end := file.sel.Anchor, file.sel.Active
	if offsetOf(file.text, start) > offsetOf(file.text, end) {
		start, end = end, start
	}
	req := extension.CodeActionRequest{Range: extension.Range{Start: start, End: end}, Only: []string{fillStructKind}}
	err := g.api.LanguageServers().ApplyCodeAction(ctx, ServerID, file.uri, req, func(title string, err error) {
		switch {
		case err != nil:
			g.api.Notifications().Error(what + ": gopls failed: " + describe(err))
		case title == "":
			g.api.Notifications().Info(what + ": gopls offers nothing to fill at the cursor. Put it in a struct literal with fields that are not set.")
		}
	})
	if err != nil {
		g.api.Notifications().Error(what + ": " + describe(err))
	}
	return nil
}

// serverWord says in words what state the server is in.
func serverWord(s extension.ServerState) string {
	switch s {
	case extension.ServerStarting:
		return "starting"
	case extension.ServerMissing:
		return "not installed (see Go: Install/Update Tools)"
	case extension.ServerFailed:
		return "stopped after crashing (see Go: Restart Language Server)"
	case extension.ServerStopped:
		return "stopped"
	}
	return "not running"
}
