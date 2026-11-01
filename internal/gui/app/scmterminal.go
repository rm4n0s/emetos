package app

import (
	"github.com/rm4n0s/errors"

	"github.com/rm4n0s/emetos/internal/core/command"
	"github.com/rm4n0s/emetos/internal/core/externalterminal"
	"github.com/rm4n0s/emetos/internal/core/settings"
)

// Open in Integrated Terminal and Open in External Terminal of a repository (SPEC §10.30, step 24.23): the commands of the context menu of
// a repository of the Source Control view. The integrated one asks the Terminal extension for a terminal in the folder of the repository;
// the external one starts the terminal program of the desktop there.
//
// Ported from VS Code: src/vs/workbench/contrib/scm/browser/scm.contribution.ts (scm.openInIntegratedTerminal, scm.openInTerminal) and
// src/vs/workbench/contrib/externalTerminal/browser/externalTerminal.contribution.ts

// terminalNewCommand is the Terminal extension's command that makes a terminal; its argument {cwd} is the folder.
const terminalNewCommand = "workbench.action.terminal.new"

// scmRoot reads the folder of the repository the arguments of a menu name: the first source control the model has. A command run with
// no source control (from the palette, or by a key) does nothing, as VS Code's does without a provider.
func (w *Workspace) scmRoot(args any) (string, bool) {
	for _, a := range scmArgsOf(args) {
		if src := w.SCM.Source(a.sourceControl); src != nil && src.RootPath() != "" {
			return src.RootPath(), true
		}
	}
	return "", false
}

// registerSCMTerminalCommands binds the two commands.
func (w *Workspace) registerSCMTerminalCommands(set func(string, command.Handler)) {
	set("scm.openInIntegratedTerminal", func(c command.Context, args any) error {
		root, ok := w.scmRoot(args)
		if !ok {
			return nil
		}
		err := w.Commands.Run(command.Context{Ctx: c.Ctx, Keys: c.Keys, Source: command.SourceAPI}, terminalNewCommand, map[string]any{"cwd": root})
		if err != nil {
			if appErr, isApp := errors.FromError(err); isApp && (appErr.Tag == "CommandNotFound" || appErr.Tag == "CommandHandlerMissing") {
				// the Terminal extension is turned off (extensions.disabled)
				w.Notify.Warn("Emetos", "The terminal is turned off, so there is none to open in "+root+".")
				return nil
			}
			return err
		}
		return nil
	})
	set("scm.openInTerminal", func(c command.Context, args any) error {
		root, ok := w.scmRoot(args)
		if !ok {
			return nil
		}
		exec := settings.Get[string](w.Settings, "terminal.external.linuxExec", "")
		if err := externalterminal.Open(c.Ctx, externalterminal.Options{Exec: exec, Dir: root}); err != nil {
			w.Notify.Error("Emetos", "The external terminal could not be opened in "+root+": "+describe(err))
			w.Process.Logger.Warn("the external terminal could not be opened", "dir", root, "err", err)
		}
		return nil
	})
}
