package editor

import (
	"slices"
	"strings"
	"testing"

	"github.com/rm4n0s/emetos/internal/core/command"
)

// appCommands are the commands of the grid of editor groups and of the paths of files that have no
// "workbench." id: gui/app binds them, and its tests check that it does.
var appCommands = map[string]bool{
	"moveActiveEditor": true, "copyActiveEditor": true, "layoutEditorGroups": true,
	// the toggle of the input mode is the process's (gui/app/zen.go)
	"editor.action.toggleOvertypeInsertMode": true,
	"copyFilePath":                           true, "copyRelativeFilePath": true, "revealInExplorer": true,
	// the file operations of the Explorer (gui/app/fileops.go)
	"explorer.newFile": true, "explorer.newFolder": true, "renameFile": true, "moveFileToTrash": true, "deleteFile": true,
	"filesExplorer.cut": true, "filesExplorer.copy": true, "filesExplorer.paste": true, "filesExplorer.cancelCut": true,
	"explorer.openToSide": true, "revealFileInOS": true,
	// the indentation picker of the status bar (gui/app/statuspickers.go)
	"changeEditorIndentation": true,
	// Save All, whose id has no "workbench." (gui/app/save.go)
	"saveAll": true,
	// the rows of File > Open Recent (gui/app/recent.go)
	"openRecentFolder": true, "openRecentFile": true,
	// Switch Side of the diff editor (gui/app/diffpane.go)
	"diffEditor.switchSide": true,
	// the inline view and the ways to open a diff (gui/app/compare.go)
	"toggle.diff.renderSideBySide": true, "diffEditor.toggleUseInlineViewWhenSpaceIsLimited": true,
	"diffEditor.setViewMode.inline": true, "diffEditor.setViewMode.sideBySide": true, "diffEditor.setViewMode.automatic": true,
	"selectForCompare": true, "compareFiles": true, "compareSelected": true,
	// the commands of the merge editor (gui/app/mergepane.go)
	"merge.goToNextUnhandledConflict": true, "merge.goToPreviousUnhandledConflict": true,
	"git.openMergeEditor": true, "_open.mergeEditor": true,
	// the commands that resolve and complete a merge (step 9.8, gui/app/mergepane.go)
	"merge.acceptAllInput1": true, "merge.acceptAllInput2": true, "merge.toggleActiveConflictInput1": true,
	"merge.toggleActiveConflictInput2": true, "mergeEditor.acceptAllCombination": true,
	"mergeEditor.resetResultToBaseAndAutoMerge": true, "mergeEditor.acceptMerge": true,
	// the commands of the workspace tabs of the title bar (step 9b.2, gui/app/titlebar.go)
	"emetos.workspaceTab.close": true, "emetos.workspaceTab.closeOthers": true, "emetos.workspaceTab.closeToTheRight": true,
	"emetos.workspaceTab.copyPath": true, "emetos.workspaceTab.revealInFileManager": true, "emetos.workspaceTab.openFolder": true,
	// the commands that switch and move the workspace tabs (step 9b.5, gui/app/workspaces_commands.go)
	"emetos.workspaceTab.next": true, "emetos.workspaceTab.previous": true, "emetos.workspaceTab.switch": true,
	"emetos.workspaceTab.moveLeft": true, "emetos.workspaceTab.moveRight": true,
	"emetos.workspaceTab.open1": true, "emetos.workspaceTab.open2": true, "emetos.workspaceTab.open3": true,
	"emetos.workspaceTab.open4": true, "emetos.workspaceTab.open5": true, "emetos.workspaceTab.open6": true,
	"emetos.workspaceTab.open7": true, "emetos.workspaceTab.open8": true, "emetos.workspaceTab.open9": true,
	// the page of the links between window tabs (step 11.3, gui/app/eventsharingpane.go)
	"emetos.extensions.manageEventSharing": true,
	// the command that opens a window tab of an extension (step 11.7, gui/app/exttab.go)
	"emetos.windowTab.open": true,
	// Go to Next Problem and the like go through the files of the window (step 12.4, gui/app/diagnostics.go)
	"editor.action.marker.next": true, "editor.action.marker.prev": true,
	"editor.action.marker.nextInFiles": true, "editor.action.marker.prevInFiles": true,
	// the Outline view of the Explorer (step 12.7, gui/app/outline.go)
	"outline.focus": true, "outline.collapse": true, "outline.followCursor": true,
}

// keybindingsEditorPrefix starts the ids of the commands of the keybindings editor (gui/app/keybindingspane.go).
const keybindingsEditorPrefix = "keybindings.editor."

func TestEveryEditorCommandHasAHandler(t *testing.T) {
	reg := command.DefaultRegistry()
	have := CommandIDs()
	var missing []string
	for _, meta := range reg.All() {
		if strings.HasPrefix(meta.ID, "workbench.") || strings.HasPrefix(meta.ID, "notifications.") ||
			strings.HasPrefix(meta.ID, "problems.") || strings.HasPrefix(meta.ID, keybindingsEditorPrefix) {
			// the commands of the workbench, of the notifications and of the filter of the Problems panel are the workbench's and the window's (gui/workbench and gui/app bind them, and their tests check that they do)
			continue
		}
		if appCommands[meta.ID] {
			continue
		}
		if !slices.Contains(have, meta.ID) {
			missing = append(missing, meta.ID)
		}
	}
	if len(missing) > 0 {
		t.Errorf("commands without a handler: %v", missing)
	}
	for _, id := range have {
		if strings.HasPrefix(id, "dev.") {
			continue // registered only in debug and emetos_dev builds (SPEC D15)
		}
		if _, ok := reg.Lookup(id); !ok {
			t.Errorf("%s is implemented but not registered", id)
		}
	}
}
