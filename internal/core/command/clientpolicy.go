package command

import (
	"slices"
	"strings"

	"github.com/rm4n0s/emetos/extension"
)

// Refusal says that a command may not be run by a client of a workspace (the extension that drives a workspace for a
// remote user, SPEC §10.19 "Confinement"), and why.
type Refusal struct {
	// Class is one of the extension.Refused constants.
	Class string
	// Reason says in words why the command is refused.
	Reason string
}

// refusedRule is one class of refused commands: the exact ids and the prefixes of the ids that belong to it.
type refusedRule struct {
	class    string
	reason   string
	ids      []string
	prefixes []string
}

// clientRules is the one list of the commands that are refused to clients (SPEC §10.19 "Confinement"): everything that
// reaches outside the workspace the client was given, the window it is in, the folder, the profile or the settings of the
// user. It is read-only. A command not listed is allowed: a client can do what the user can do in that workspace.
var clientRules = []refusedRule{
	{
		class: extension.RefusedWindowTab, reason: "window tabs belong to the window, not to the workspace",
		prefixes: []string{"emetos.workspaceTab.", "emetos.windowTab."},
	},
	{
		class: extension.RefusedQuit, reason: "a client may not quit Emetos or close the window",
		ids: []string{"workbench.action.quit", "workbench.action.closeWindow"},
	},
	{
		class: extension.RefusedWindow, reason: "a client may not open or switch windows",
		ids: []string{"workbench.action.newWindow", "workbench.action.switchWindow"},
	},
	{
		class: extension.RefusedFolder, reason: "a client may not open, replace or close the folder of the workspace, or open from the recent lists",
		ids: []string{
			"workbench.action.files.openFolder", "workbench.action.closeFolder", "workbench.action.openRecent",
			"workbench.action.clearRecentFiles", "openRecentFolder", "openRecentFile",
		},
	},
	{
		class: extension.RefusedProfile, reason: "a client may not touch the profiles",
		prefixes: []string{"workbench.profiles."},
	},
	{
		class: extension.RefusedSettings, reason: "a client may not open or write the user's settings, keybindings or themes, or the links between window tabs (the settings of the workspace are allowed)",
		ids: []string{
			"workbench.action.openSettings", "workbench.action.openSettings2", "workbench.action.openSettingsJson",
			"workbench.action.openGlobalSettings", "workbench.action.openGlobalKeybindings", "workbench.action.openGlobalKeybindingsFile",
			"workbench.action.toggleAutoSave", "workbench.action.zoomIn", "workbench.action.zoomOut", "workbench.action.zoomReset",
			"workbench.action.selectTheme", "workbench.action.selectIconTheme", "workbench.action.editor.changeLanguageMode",
			"emetos.extensions.manageEventSharing",
		},
		prefixes: []string{"keybindings.editor."},
	},
	{
		class: extension.RefusedRemote, reason: "a client may not drive the extensions that share a workspace or control another one",
		prefixes: []string{"emetos.remotework.", "emetos.hosting."},
	},
	{
		class: extension.RefusedTerminal, reason: "a terminal runs programs on the computer of the host: a client uses the terminal requests",
		ids:      []string{"workbench.action.createTerminalEditor"},
		prefixes: []string{"workbench.action.terminal."},
	},
	{
		// the shared Databases extension and the SQL, MongoDB and Redis extensions (SPEC §10.24): their commands are emetos.databases.*,
		// emetos.sql.*, mongodb.* and emetos.redis.*; sql.*, redis.* and emetos.mongodb.* are refused as well, so that a command that is
		// named after the plan of the spec instead of the one of the extension's id is refused too
		class: extension.RefusedOutside, reason: "a database is outside the workspace: a client may not connect to what the host's connections name (SPEC §10.24)",
		prefixes: []string{"emetos.databases.", "emetos.sql.", "sql.", "emetos.mongodb.", "mongodb.", "emetos.redis.", "redis."},
	},
	{
		class: extension.RefusedOutside, reason: "the socket of a container engine is power over the computer: a client may not drive the host's Docker or Podman (SPEC §10.20 \"Safety\", §10.24)",
		prefixes: []string{"containers.", "emetos.containers.", "podman.", "emetos.podman."},
	},
}

// ClientRefusal tells whether a client may not run the command, and why. The id is the command's, case as it is.
func ClientRefusal(id string) (Refusal, bool) {
	for _, rule := range clientRules {
		if slices.Contains(rule.ids, id) || slices.ContainsFunc(rule.prefixes, func(p string) bool { return strings.HasPrefix(id, p) }) {
			return Refusal{Class: rule.class, Reason: rule.reason}, true
		}
	}
	return Refusal{}, false
}

// RefusedClasses lists the classes of ClientRefusal, in the order of the rules, each once (a class may have several rules).
func RefusedClasses() []string {
	var out []string
	for _, rule := range clientRules {
		if !slices.Contains(out, rule.class) {
			out = append(out, rule.class)
		}
	}
	return out
}

// RefusedExactIDs lists the commands that ClientRefusal refuses by their whole id, and RefusedPrefixes the prefixes it
// refuses; the tests check that they name commands that exist.
func RefusedExactIDs() []string {
	var out []string
	for _, rule := range clientRules {
		out = append(out, rule.ids...)
	}
	return out
}

// RefusedPrefixes lists the prefixes of the ids that ClientRefusal refuses.
func RefusedPrefixes() []string {
	var out []string
	for _, rule := range clientRules {
		out = append(out, rule.prefixes...)
	}
	return out
}
