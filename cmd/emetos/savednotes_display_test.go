//go:build emetos_display

package main

import (
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/rm4n0s/emetos/internal/testenv/display"
)

// savedNotesState is the session of an Emetos that still had the Notes, TODOs and Scratchpad extensions (step 17e.1, SPEC §10.26):
// the fixture of internal/gui/app, a window with the list of notes, a note tab and a TODOs view, a window whose note tab keeps
// its note, and the list of the tabs with Scratchpad tabs. It is the State of the run.
func savedNotesState(t *testing.T) map[string]string {
	t.Helper()
	root := filepath.Join("..", "..", "internal", "gui", "app", "testdata", "savednotes", "state", "emetos")
	state := map[string]string{}
	err := filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() {
			return err
		}
		rel, err := filepath.Rel(root, path)
		if err != nil {
			return err
		}
		data, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		state[filepath.ToSlash(rel)] = string(data)
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(state) != 3 {
		t.Fatalf("the fixture has %d files, want 3", len(state))
	}
	return state
}

// TestRealInputSavedNotesAreOfferedAsFilesAndOpenFolderShowsThem: an Emetos started over the session of the removed extensions says
// where it saved their text, and Open Folder opens that folder as a workspace tab that lists the files, with no dialog.
func TestRealInputSavedNotesAreOfferedAsFilesAndOpenFolderShowsThem(t *testing.T) {
	a := display.Start(t, display.Options{State: savedNotesState(t)})
	a.Eventually("the message of the notification", func() (bool, string) {
		for _, tx := range a.Texts() {
			if strings.HasPrefix(tx.Text, "Your notes and Scratchpad text were saved as files in ") && strings.HasSuffix(tx.Text, "saved-notes.") {
				return true, ""
			}
		}
		return false, a.Dump()
	})
	shot(t, a, "saved-notes-notification")
	before := len(a.Workbench().WindowTabs)
	// the Explorer of the empty window has a button of the same name, at the left; the notification's is at the right
	clickTextWhere(t, a, "Open Folder", inRect(800, 0, 1280, 800))
	a.Eventually("a tab for the folder", func() (bool, string) {
		w := a.Workbench().WindowTabs
		return len(w) == before+1 && w[len(w)-1].Active && strings.HasSuffix(w[len(w)-1].Folder, "/emetos/saved-notes"), a.Dump()
	})
	for _, name := range []string{"Groceries.md", "Groceries 2.md", "Untitled Note.md", "Empty idea.md", "Scratchpad.md", "Scratchpad Ideas.md"} {
		a.WaitText(name)
	}
	shot(t, a, "saved-notes-folder")
	// no dialog: its buttons are not on screen, and the editor answers
	for _, button := range []string{"Cancel", "OK", "Don't Save"} {
		if _, ok := a.Text(button); ok {
			t.Errorf("a dialog shows (%q is on screen)", button)
		}
	}
	// the tabs of the removed extensions were dropped: only workspace tabs are in the strip
	for _, tab := range a.Workbench().WindowTabs {
		if tab.Kind != "" {
			t.Errorf("a Scratchpad tab came back: %+v", tab)
		}
	}
}
