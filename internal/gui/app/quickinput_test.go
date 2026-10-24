package app

import (
	"testing"
	"time"

	"github.com/rm4n0s/glutter/gluttertest"
	"github.com/rm4n0s/glutter/widgets"

	"github.com/rm4n0s/emetos/internal/core/buffer"
	"github.com/rm4n0s/emetos/internal/core/command"
)

// quickInputType types text in the input of the quick input and lets the frames pass.
func (a *keyApp) quickInputType(text string) {
	a.ts.EnterText(gluttertest.First(gluttertest.ByType[*widgets.EditableText]()), text)
	a.ts.Pump(time.Millisecond)
	a.ts.Pump(time.Millisecond)
}

// settleQuickInput lets the frames that run the command of an accepted item pass.
func (a *keyApp) settleQuickInput() {
	for range 6 {
		a.ts.Pump(time.Millisecond)
	}
}

func TestGoToLineOnARealEditor(t *testing.T) {
	a := newKeyApp(t, "one\ntwo\nthree\nfour\n")
	a.vm.Cursors().SetSelections([]buffer.Selection{cursorAt(0, 1)})
	ctl := a.win.QuickInput

	a.press(t, "ctrl+g")
	if !ctl.Visible() || ctl.Picker().Value() != ":" {
		t.Fatalf("Ctrl+G: visible %v value %q", ctl.Visible(), ctl.Picker().Value())
	}
	// typing a line reveals it and marks it, without moving the cursor
	a.quickInputType(":3")
	if line, ok := a.vm.RangeHighlight(); !ok || line != 2 {
		t.Errorf("highlight %d %v", line, ok)
	}
	if got := a.selections(); len(got) != 1 || got[0] != cursorAt(0, 1) {
		t.Errorf("the cursor moved while typing: %v", got)
	}
	// Escape puts the view back and clears the mark
	a.press(t, "escape")
	if ctl.Visible() {
		t.Error("Escape did not close the quick input")
	}
	if _, ok := a.vm.RangeHighlight(); ok {
		t.Error("the mark stays after Escape")
	}
	if got := a.selections(); got[0] != cursorAt(0, 1) {
		t.Errorf("the cursor after Escape: %v", got)
	}
	if !a.vm.Focused() {
		t.Error("the editor does not have the focus after Escape")
	}

	// Enter goes to the line and column, and the editor has the focus again
	a.press(t, "ctrl+g")
	a.quickInputType(":4:2")
	a.press(t, "enter")
	a.settleQuickInput()
	if ctl.Visible() {
		t.Error("Enter did not close the quick input")
	}
	if got := a.selections(); len(got) != 1 || got[0] != cursorAt(3, 1) {
		t.Errorf("the cursor after Enter: %v", got)
	}
	if _, ok := a.vm.RangeHighlight(); ok {
		t.Error("the mark stays after Enter")
	}
	if !a.vm.Focused() {
		t.Error("the editor does not have the focus after Enter")
	}
}

func TestQuickOpenCommandTakesTheInitialText(t *testing.T) {
	tests := []struct {
		name    string
		args    any
		want    string
		visible bool
	}{
		{"a string", ">line", ">line", true},
		{"an object", map[string]any{"value": "view "}, "view ", true},
		{"no argument", nil, "", true},
		{"an empty string", "", "", true},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			a := newKeyApp(t, "text\n")
			err := a.win.Commands.Run(command.Context{Ctx: a.win.ctx, Keys: a.win.Keys}, "workbench.action.quickOpen", tc.args)
			if err != nil {
				t.Fatal(err)
			}
			a.settleQuickInput()
			ctl := a.win.QuickInput
			if ctl.Visible() != tc.visible || ctl.Picker().Value() != tc.want {
				t.Errorf("visible %v value %q, want %q", ctl.Visible(), ctl.Picker().Value(), tc.want)
			}
		})
	}
}

func TestCommandPaletteRunsACommandOnTheEditor(t *testing.T) {
	a := newKeyApp(t, "alpha\nbeta\n")
	a.vm.Cursors().SetSelections([]buffer.Selection{cursorAt(1, 2)})
	ctl := a.win.QuickInput

	a.press(t, "ctrl+shift+p")
	if !ctl.Visible() || ctl.Picker().Value() != ">" {
		t.Fatalf("Ctrl+Shift+P: visible %v value %q", ctl.Visible(), ctl.Picker().Value())
	}
	a.quickInputType(">select all")
	if rows := ctl.Picker().Rows(); len(rows) == 0 || rows[0].Item.ID != "editor.action.selectAll" {
		t.Fatalf("rows %v", ctl.Picker().Rows())
	}
	a.press(t, "enter")
	a.settleQuickInput()
	if ctl.Visible() {
		t.Error("the palette stays open")
	}
	if got := a.selections(); len(got) != 1 || got[0] != sel(0, 0, 2, 0) {
		t.Errorf("the selection after Select All: %v", got)
	}
	if !a.vm.Focused() {
		t.Error("the editor does not have the focus after the command")
	}
	// the command is the most recently used one now
	if ids := a.win.CommandHistory.IDs(); len(ids) != 1 || ids[0] != "editor.action.selectAll" {
		t.Errorf("history %v", ids)
	}

	// F1 opens it too, and the used command comes first
	a.press(t, "f1")
	if !ctl.Visible() || len(ctl.Picker().Rows()) == 0 || ctl.Picker().Rows()[0].Item.ID != "editor.action.selectAll" {
		t.Errorf("F1: visible %v", ctl.Visible())
	}
	a.press(t, "escape")
	if ctl.Visible() || !a.vm.Focused() {
		t.Errorf("Escape: visible %v focused %v", ctl.Visible(), a.vm.Focused())
	}
}

func TestInsertSnippetThroughTheQuickInput(t *testing.T) {
	a := newKeyApp(t, "")
	ctl := a.win.QuickInput
	if err := a.vm.RunCommand("editor.action.insertSnippet", nil); err != nil {
		t.Fatal(err)
	}
	a.settleQuickInput()
	if !ctl.Visible() {
		t.Fatal("the quick input is not open")
	}
	var found bool
	for _, r := range ctl.Picker().Rows() {
		if r.Item.Label == "alp" {
			found = true
		}
	}
	if !found {
		t.Fatalf("rows %v", ctl.Picker().Rows())
	}
	if ctl.Picker().Placeholder() != "Select a snippet" {
		t.Errorf("placeholder %q", ctl.Picker().Placeholder())
	}
	a.press(t, "enter")
	a.settleQuickInput()
	if got := a.text(); got != "ALP" {
		t.Errorf("text %q", got)
	}
	if ctl.Visible() {
		t.Error("the quick input stays open")
	}
}
