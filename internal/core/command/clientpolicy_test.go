package command

import (
	"slices"
	"strings"
	"testing"

	"github.com/rm4n0s/emetos/extension"
)

// Every class of refused commands has an example that is refused with that class, and the commands a client may run are
// not touched.
func TestClientRefusal(t *testing.T) {
	tests := []struct {
		id    string
		class string // "" for a command that is allowed
	}{
		{"emetos.workspaceTab.close", extension.RefusedWindowTab},
		{"emetos.workspaceTab.open3", extension.RefusedWindowTab},
		{"emetos.windowTab.open", extension.RefusedWindowTab},
		{"workbench.action.quit", extension.RefusedQuit},
		{"workbench.action.closeWindow", extension.RefusedQuit},
		{"workbench.action.newWindow", extension.RefusedWindow},
		{"workbench.action.switchWindow", extension.RefusedWindow},
		{"workbench.action.files.openFolder", extension.RefusedFolder},
		{"workbench.action.closeFolder", extension.RefusedFolder},
		{"workbench.action.openRecent", extension.RefusedFolder},
		{"openRecentFile", extension.RefusedFolder},
		{"workbench.profiles.actions.switchProfile", extension.RefusedProfile},
		{"workbench.profiles.actions.deleteProfile", extension.RefusedProfile},
		{"workbench.action.openSettingsJson", extension.RefusedSettings},
		{"workbench.action.toggleAutoSave", extension.RefusedSettings},
		{"workbench.action.selectTheme", extension.RefusedSettings},
		{"keybindings.editor.removeKeybinding", extension.RefusedSettings},
		{"emetos.extensions.manageEventSharing", extension.RefusedSettings},
		{"emetos.relay.listen", extension.RefusedRemote},
		{"emetos.remotework.manage", extension.RefusedRemote},
		// the commands that host a workspace (step 17e.4): a client may not start or stop hosting, or open the menu that does
		{"emetos.remotework.host", extension.RefusedRemote},
		{"emetos.remotework.stopHosting", extension.RefusedRemote},
		{"emetos.relay.host", extension.RefusedRemote},
		{"emetos.relay.stopHosting", extension.RefusedRemote},
		{"emetos.relay.open", extension.RefusedRemote},
		{"emetos.hosting.menu", extension.RefusedRemote},
		// the connections to databases (step 19.1): a client may not open, edit or connect to what the host's connections name
		{"emetos.databases.newConnection", extension.RefusedOutside},
		{"emetos.databases.connect", extension.RefusedOutside},
		{"emetos.databases.deleteConnection", extension.RefusedOutside},
		{"emetos.sql.newQuery", extension.RefusedOutside},
		// the container engines (steps 18.2 and 18.4): their commands are the extensions' own, containers.* and podman.*
		{"containers.containers.remove", extension.RefusedOutside},
		{"podman.pods.remove", extension.RefusedOutside},
		{"podman.kube.play", extension.RefusedOutside},
		// MongoDB (step 20.1)
		{"mongodb.newPlayground", extension.RefusedOutside},
		{"workbench.action.terminal.new", extension.RefusedTerminal},
		{"workbench.action.terminal.sendSequence", extension.RefusedTerminal},
		// allowed: the workspace settings, editing, saving, extensions' commands
		{"workbench.action.openWorkspaceSettingsFile", ""},
		{"workbench.action.openWorkspaceSettings", ""},
		{"workbench.action.files.save", ""},
		{"editor.action.formatDocument", ""},
		{"emetos.tasks.run", ""},
		{"emetos.relayfoo", ""}, // a prefix needs its dot
		{"", ""},
	}
	for _, tt := range tests {
		t.Run(tt.id, func(t *testing.T) {
			got, refused := ClientRefusal(tt.id)
			if refused != (tt.class != "") || got.Class != tt.class {
				t.Fatalf("ClientRefusal(%q) = %+v, %v; want class %q", tt.id, got, refused, tt.class)
			}
			if refused && got.Reason == "" {
				t.Errorf("no reason for %q", tt.id)
			}
		})
	}
}

// Every class is in the list of classes, and every command id the rules name exists in the registry of Emetos (a rename
// would otherwise leave a rule that protects nothing); every prefix of a command of Emetos matches at least one.
func TestClientRefusalNamesRealCommands(t *testing.T) {
	reg := DefaultRegistry()
	have := map[string]bool{}
	for _, m := range reg.All() {
		have[m.ID] = true
	}
	// the extensions' commands are not in the registry of Emetos
	extensionPrefixes := []string{extension.RelayID + ".", "emetos.remotework.", "emetos.hosting.", "workbench.action.terminal.", "emetos.databases.", "emetos.sql.", "mongodb.", "containers.", "podman."}
	extensionIDs := []string{"workbench.action.createTerminalEditor"} // the terminal extension's
	for _, id := range RefusedExactIDs() {
		if !have[id] && !slices.Contains(extensionIDs, id) {
			t.Errorf("the rule names %q, which is not a command", id)
		}
	}
	for _, p := range RefusedPrefixes() {
		if slices.Contains(extensionPrefixes, p) {
			continue
		}
		if !slices.ContainsFunc(reg.All(), func(m Meta) bool { return strings.HasPrefix(m.ID, p) }) {
			t.Errorf("the prefix %q matches no command", p)
		}
	}
	classes := RefusedClasses()
	for _, want := range []string{
		extension.RefusedWindowTab, extension.RefusedQuit, extension.RefusedWindow, extension.RefusedFolder,
		extension.RefusedProfile, extension.RefusedSettings, extension.RefusedRemote, extension.RefusedTerminal, extension.RefusedOutside,
	} {
		if !slices.Contains(classes, want) {
			t.Errorf("class %q has no rule", want)
		}
	}
}

// A command of Emetos that is about windows, folders, profiles, quitting or the user's settings must be refused: a new
// command whose id looks like one of these and is not in the rules fails here, so that whoever adds it decides.
func TestNoRiskyCommandIsForgotten(t *testing.T) {
	risky := []string{"quit", "window", "folder", "profile", "workspacetab", "windowtab", "recent", "globalsettings", "settingsjson", "theme"}
	// allowed on purpose: they act inside the workspace
	allowed := map[string]bool{
		"workbench.action.closeActiveEditor": true,
		"explorer.newFolder":                 true, "workbench.files.action.createFolderFromExplorer": true,
		"workbench.files.action.collapseExplorerFolders": true, "revealFileInOS": true, "workbench.explorer.fileView.focus": true,
		"workbench.action.openNextRecentlyUsedEditor": true, "workbench.action.openNextRecentlyUsedEditorInGroup": true,
		"workbench.action.openPreviousRecentlyUsedEditor": true, "workbench.action.openPreviousRecentlyUsedEditorInGroup": true,
		"workbench.action.quickOpenLeastRecentlyUsedEditor": true, "workbench.action.quickOpenLeastRecentlyUsedEditorInGroup": true,
		"workbench.action.quickOpenPreviousRecentlyUsedEditor": true, "workbench.action.quickOpenPreviousRecentlyUsedEditorInGroup": true,
		"workbench.action.showAllEditorsByMostRecentlyUsed": true, "workbench.action.showEditorsInActiveGroup": true,
		"workbench.action.openPreviousEditorFromHistory": true,
		// Find in Folder... opens the Search view, which searches only the folder of the workspace (SPEC §10.27)
		"filesExplorer.findInFolder": true,
	}
	for _, m := range DefaultRegistry().All() {
		lower := strings.ToLower(m.ID)
		hit := slices.ContainsFunc(risky, func(w string) bool { return strings.Contains(lower, w) })
		if !hit || allowed[m.ID] {
			continue
		}
		if _, refused := ClientRefusal(m.ID); !refused {
			t.Errorf("%q looks like a command that reaches outside the workspace and is not refused; refuse it in clientRules or allow it here", m.ID)
		}
	}
}
