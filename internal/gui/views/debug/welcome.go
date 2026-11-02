package debug

import (
	"net/url"

	"github.com/rm4n0s/emetos/internal/core/command"
	"github.com/rm4n0s/emetos/internal/core/debug"
	"github.com/rm4n0s/emetos/internal/gui/views/welcome"
)

// WelcomeState is what the welcome content of the container depends on.
type WelcomeState struct {
	// DebuggersAvailable is the context key debuggersAvailable: an enabled extension contributes a debugger.
	DebuggersAvailable bool
	// ExtensionAvailable is debugExtensionAvailable; false says that no extension that contributes a debugger is enabled.
	ExtensionAvailable bool
	// InterestedInActiveEditor is debuggerInterestedInActiveEditor: a debugger serves the language of the active editor.
	InterestedInActiveEditor bool
	// NoFolder is workbenchState == 'empty'.
	NoFolder bool
}

// NoDebuggerText is the text of the welcome while no extension that contributes a debugger is enabled. VS Code's text sends the
// user to the Marketplace, which Emetos does not have.
const NoDebuggerText = "All debug extensions are disabled. Enable the extension of a language to debug it."

// WelcomePieces are the pieces of welcome content of the container in order: the link that opens a file, the button that starts, the
// text that sends to launch.json. The texts and the groups are VS Code's.
//
// Ported from VS Code: src/vs/workbench/contrib/debug/browser/welcomeView.ts (the content registered for WelcomeView.ID)
func WelcomePieces(s WelcomeState) []welcome.Piece {
	var out []welcome.Piece
	if s.DebuggersAvailable && !s.InterestedInActiveEditor {
		out = append(out, welcome.Piece{Contents: "[Open a file](command:workbench.action.files.openFile) which can be debugged or run."})
	}
	if s.DebuggersAvailable {
		out = append(out, welcome.Piece{Contents: "[Run and Debug](command:" + command.DebugStartCommand + ")"})
		if s.NoFolder {
			out = append(out, welcome.Piece{Contents: "To customize Run and Debug, [open a folder](command:workbench.action.files.openFolder) and create a launch.json file."})
		} else {
			args := url.QueryEscape(`{"addNew":true}`)
			out = append(out, welcome.Piece{Contents: "To customize Run and Debug [create a launch.json file](command:" + command.DebugConfigure + "?" + args + ")."})
		}
	}
	if !s.ExtensionAvailable {
		out = append(out, welcome.Piece{Contents: NoDebuggerText})
	}
	return out
}

// InDebugMode says whether the status bar takes the debugging colors: a session runs (not one that is being started) that does not
// hide the status bar or run without debugging.
//
// Ported from VS Code: src/vs/workbench/contrib/debug/browser/statusbarColorProvider.ts (isStatusbarInDebugMode)
func InDebugMode(state debug.State, sessions []*debug.Session) bool {
	if state == debug.Inactive || state == debug.Initializing {
		return false
	}
	for _, s := range sessions {
		if !s.Options().SuppressDebugStatusbar && !s.Configuration().NoDebug() {
			return true
		}
	}
	return false
}
