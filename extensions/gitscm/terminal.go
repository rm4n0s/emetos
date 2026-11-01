package gitscm

import (
	"strings"

	"github.com/rm4n0s/emetos/extension"
)

// Git in the terminals of the window (SPEC §10.30 "Passwords and editors", "Terminals"; ported in idea from VS Code's
// extensions/git/src/terminal.ts, TerminalEnvironmentManager): the terminals of the Terminal extension get the variables that make the git
// typed in them ask the window for a password, so that `git push` in a terminal prompts in Emetos as the Git commands do. VS Code fills
// its environment variable collection when the extension activates and again when a setting changes; Emetos gives the Terminal a source
// that it asks when a terminal starts, which has the same effect and starts the server of the programs only when a terminal needs it.

// contributeTerminalEnvironment registers the source of the variables. It is taken away with the activation (the extension turned off).
func (x *gitscm) contributeTerminalEnvironment() error {
	_, err := x.api.TerminalEnvironment().Contribute(x.terminalVariables)
	return err
}

// terminalVariables is the source: the variables of the server of the window tab for a terminal that starts now, and what they enable
// (VS Code's featureDescription of askpass.ts and gitEditor.ts). It gives nothing when git.enabled is false, when the settings turn the
// programs off, or when the server cannot start (logged once). It is called on the UI thread when a terminal starts.
func (x *gitscm) terminalVariables() (map[string]string, string) {
	askpass, editor := x.terminalAskpassOn.Load(), x.terminalEditorOn.Load()
	if !askpass && !editor {
		return nil, ""
	}
	srv := x.helperServer()
	if srv == nil {
		return nil, ""
	}
	vars := map[string]string{}
	for _, kv := range srv.TerminalEnvironment(askpass, editor) {
		if name, value, ok := strings.Cut(kv, "="); ok {
			vars[name] = value
		}
	}
	var features []string
	if askpass {
		features = append(features, "git auth provider")
	}
	if editor {
		features = append(features, "git editor")
	}
	return vars, "Enables the following features: " + strings.Join(features, ", ")
}

var _ extension.TerminalVariables = (*gitscm)(nil).terminalVariables
