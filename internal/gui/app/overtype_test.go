package app

import (
	"context"
	"strings"
	"testing"

	"github.com/rm4n0s/glutter/ui"

	"github.com/rm4n0s/emetos/internal/core/buffer"
	"github.com/rm4n0s/emetos/internal/gui/workbench"
)

// The overtype mode in the window (step 10.2): the Insert key and the command toggle it, the editors type over, and the
// status bar shows "OVR" while it lasts.

func overtypeApp(t *testing.T) *groupApp {
	t.Helper()
	return newGroupAppWith(t, newTestWorkspace(t, ""), testWindowSize, []string{"a.txt"}, map[string]string{"a.txt": "abcdef\n"}, "a.txt")
}

func TestTheInsertKeyTogglesTheOvertypeModeAndTheStatusItem(t *testing.T) {
	a := overtypeApp(t)
	if a.win.InputMode.Overtype() {
		t.Fatal("the editors start in insert mode")
	}
	if it := a.status("status.editor.inputMode"); it.Visible {
		t.Error("OVR is shown in insert mode")
	}
	a.vm().Cursors().SetSelections([]buffer.Selection{cursorAt(0, 1)})
	a.vm().SelectionsChanged()
	a.pump()

	a.press("insert")
	if !a.win.InputMode.Overtype() {
		t.Fatal("the Insert key did not switch to overtype mode")
	}
	it := a.status("status.editor.inputMode")
	if !it.Visible || it.Entry.Text != "OVR" || it.Entry.Tooltip != "Enable Insert Mode" || it.Entry.Kind != workbench.StatusProminent ||
		it.Entry.Command != "editor.action.toggleOvertypeInsertMode" {
		t.Errorf("the item is %+v (visible %v)", it.Entry, it.Visible)
	}
	if got := a.vm().EffectiveCursorStyle(); got != "block" {
		t.Errorf("the cursor of overtype mode is %q, want block", got)
	}

	a.ts.SendTextInputEvent(ui.TextInputEvent{Commit: "X", PreeditCursorBegin: -1, PreeditCursorEnd: -1})
	a.pump()
	if got := a.vm().Document().Text(); got != "aXcdef\n" {
		t.Errorf("typing in overtype mode gave %q, want aXcdef", got)
	}

	// the item is a button that goes back to insert mode
	a.clickStatus("status.editor.inputMode")
	if a.win.InputMode.Overtype() || a.status("status.editor.inputMode").Visible {
		t.Error("clicking OVR did not go back to insert mode")
	}
	a.ts.SendTextInputEvent(ui.TextInputEvent{Commit: "Y", PreeditCursorBegin: -1, PreeditCursorEnd: -1})
	a.pump()
	if got := a.vm().Document().Text(); got != "aXYcdef\n" {
		t.Errorf("typing in insert mode gave %q, want aXYcdef", got)
	}
	a.noErrors()
}

func TestTheOvertypeCommandIsRegisteredAndBound(t *testing.T) {
	a := overtypeApp(t)
	meta, ok := a.win.Commands.Lookup("editor.action.toggleOvertypeInsertMode")
	if !ok || meta.Title != "Toggle Overtype/Insert Mode" || meta.Category != "View" {
		t.Errorf("command %+v (registered %v)", meta, ok)
	}
	var keys []string
	for _, item := range a.win.Process.Keymap.Defaults() {
		if item.Command == "editor.action.toggleOvertypeInsertMode" {
			keys = append(keys, item.Keybinding.UserSettingsLabel())
		}
	}
	if len(keys) != 1 || keys[0] != "insert" {
		t.Errorf("default keys %v, want [insert]", keys)
	}
	a.run("editor.action.toggleOvertypeInsertMode")
	if !a.win.InputMode.Overtype() {
		t.Error("the command did not toggle the mode")
	}
	a.run("editor.action.toggleOvertypeInsertMode")
	if a.win.InputMode.Overtype() {
		t.Error("the command did not toggle the mode back")
	}
}

// The mode belongs to the process: two workspace tabs see the same one, as VS Code's editors of one window do.
func TestTheOvertypeModeIsTheProcessesAndSharedByTheTabs(t *testing.T) {
	a := overtypeApp(t)
	second, err := NewWorkspace(context.Background(), a.win.Process, "")
	if err != nil {
		t.Fatal(err)
	}
	if a.win.InputMode == nil || a.win.InputMode != a.win.Process.InputMode || second.InputMode != a.win.InputMode {
		t.Fatal("the workspaces of a process do not share the mode of the process")
	}
	second.InputMode.Toggle()
	if !a.win.InputMode.Overtype() {
		t.Error("a change made through the second tab did not reach the first")
	}
}

func TestOvertypePasteAndCursorSettingsReachTheEditor(t *testing.T) {
	a := overtypeApp(t)
	a.vm().Cursors().SetSelections([]buffer.Selection{cursorAt(0, 1)})
	a.vm().SelectionsChanged()
	a.run("editor.action.toggleOvertypeInsertMode")
	a.setting("editor.overtypeCursorStyle", "underline")
	if got := a.vm().EffectiveCursorStyle(); got != "underline" {
		t.Errorf("after editor.overtypeCursorStyle the cursor is %q, want underline", got)
	}
	a.setting("editor.overtypeOnPaste", false)
	if a.vm().Options().OvertypeOnPaste {
		t.Error("editor.overtypeOnPaste false did not reach the editor")
	}
	a.ts.SetClipboardText("XY")
	a.run("editor.action.clipboardPasteAction")
	if got := a.vm().Document().Text(); !strings.HasPrefix(got, "aXYbcdef") {
		t.Errorf("a paste without overtypeOnPaste gave %q, want an insertion", got)
	}
}
