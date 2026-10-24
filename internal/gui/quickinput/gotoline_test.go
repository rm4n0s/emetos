package quickinput

import (
	"reflect"
	"testing"
)

func TestParseGoToLine(t *testing.T) {
	// the text has 4 lines: "one", "two", "\tthree" (a tab, 4 wide, and 5 letters) and "four"
	tests := []struct {
		name  string
		text  string
		line  int
		col   int
		label string
		// offset is true for the modes that start with a second colon
		offset bool
	}{
		{name: "empty", text: "", label: "Type a line number to go to (from 1 to 4)."},
		{name: "not a number", text: "abc", label: "Type a line number to go to (from 1 to 4)."},
		{name: "a line", text: "2", line: 2, col: 1, label: "Press 'Enter' to go to line 2 or enter colon : to add a column number."},
		{name: "a line with spaces", text: " 3 ", line: 3, col: 1, label: "Press 'Enter' to go to line 3 or enter colon : to add a column number."},
		{name: "past the end is clipped", text: "99", line: 4, col: 1, label: "Press 'Enter' to go to line 4 or enter colon : to add a column number."},
		{name: "zero is the first line", text: "0", line: 1, col: 1, label: "Press 'Enter' to go to line 1 or enter colon : to add a column number."},
		{name: "negative counts from the end", text: "-1", line: 4, col: 1, label: "Press 'Enter' to go to line 4 or enter colon : to add a column number."},
		{name: "negative before the start", text: "-9", line: 1, col: 1, label: "Press 'Enter' to go to line 1 or enter colon : to add a column number."},
		{name: "a column", text: "1:3", line: 1, col: 3, label: "Press 'Enter' to go to line 1 at column 3."},
		{name: "comma", text: "1,2", line: 1, col: 2, label: "Press 'Enter' to go to line 1 at column 2."},
		{name: "hash", text: "1#2", line: 1, col: 2, label: "Press 'Enter' to go to line 1 at column 2."},
		{name: "the column is clipped", text: "1:40", line: 1, col: 4, label: "Press 'Enter' to go to line 1 at column 4."},
		{name: "a negative column counts from the end", text: "1:-1", line: 1, col: 3, label: "Press 'Enter' to go to line 1 at column 3."},
		{name: "an empty column", text: "2:", line: 2, col: 1, label: "Press 'Enter' to go to line 2 or enter a column number (from 1 to 4)."},
		{name: "a column that is not a number", text: "2:x", line: 2, col: 1, label: "Press 'Enter' to go to line 2 or enter a column number (from 1 to 4)."},
		{name: "the column is a visible column: a tab is 4 wide", text: "3:5", line: 3, col: 2, label: "Press 'Enter' to go to line 3 at column 5."},
		{name: "a visible column inside a tab goes to its nearer end", text: "3:2", line: 3, col: 1, label: "Press 'Enter' to go to line 3 at column 2."},
		{name: "the end of a line with a tab", text: "3:99", line: 3, col: 7, label: "Press 'Enter' to go to line 3 at column 10."},
		{name: "text after the number is ignored", text: "2abc", line: 2, col: 1, label: "Press 'Enter' to go to line 2 or enter colon : to add a column number."},
		{name: "an offset prompt", text: ":", offset: true, label: "Type a character position to go to (from 1 to 19)."},
		{name: "an offset", text: ":1", line: 1, col: 1, offset: true, label: "Press 'Enter' to go to line 1 at column 1."},
		{name: "an offset in the second line", text: ":6", line: 2, col: 2, offset: true, label: "Press 'Enter' to go to line 2 at column 2."},
		{name: "a negative offset counts from the end", text: ":-1", line: 4, col: 5, offset: true, label: "Press 'Enter' to go to line 4 at column 5."},
		{name: "an offset past the end is clipped", text: ":999", line: 4, col: 5, offset: true, label: "Press 'Enter' to go to line 4 at column 5."},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			ed := newFakeEditor("one\ntwo\n\tthree\nfour")
			got := ParseGoToLine(ed, tc.text)
			if got.Line != tc.line || got.Column != tc.col || got.Label != tc.label || got.InOffsetMode != tc.offset {
				t.Errorf("got line %d col %d offset %v %q\nwant line %d col %d offset %v %q", got.Line, got.Column, got.InOffsetMode, got.Label, tc.line, tc.col, tc.offset, tc.label)
			}
		})
	}
	t.Run("without an editor", func(t *testing.T) {
		got := ParseGoToLine(nil, "3")
		if got.Line != 0 || got.Label != "Open a text editor first to go to a line or an offset." {
			t.Errorf("got %+v", got)
		}
	})
}

func newGoToController(ed GoToLineEditor) *Controller {
	reg := NewRegistry()
	reg.MustRegister(NewGoToLineProvider(GoToLinePrefix, func() GoToLineEditor { return ed }), NewGoToLineProvider(GoToOffsetPrefix, func() GoToLineEditor { return ed }))
	return NewController(ControllerOptions{Registry: reg})
}

func TestGoToLineProviderRevealsWhileTypingAndRestores(t *testing.T) {
	ed := newFakeEditor("one\ntwo\n\tthree\nfour")
	c := newGoToController(ed)
	c.Show(":", ShowOptions{})
	if got := labelsOf(c.Picker().Rows()); !reflect.DeepEqual(got, []string{"Type a line number to go to (from 1 to 4)."}) {
		t.Fatalf("rows %v", got)
	}
	if len(ed.revealed) != 0 || ed.highlighted {
		t.Errorf("revealed %v with nothing typed", ed.revealed)
	}
	c.SetValue(":3")
	if !reflect.DeepEqual(ed.revealed, [][2]int{{3, 1}}) || !ed.highlighted {
		t.Errorf("revealed %v highlighted %v", ed.revealed, ed.highlighted)
	}
	c.SetValue(":2:2")
	if !reflect.DeepEqual(ed.revealed, [][2]int{{3, 1}, {2, 2}}) {
		t.Errorf("revealed %v", ed.revealed)
	}
	// a text that names no line clears the highlight
	c.SetValue(":x")
	if ed.highlighted {
		t.Errorf("still highlighted")
	}
	// giving up puts the editor back
	c.SetValue(":4")
	c.Hide()
	if ed.restored != 1 || ed.highlighted || len(ed.went) != 0 {
		t.Errorf("restored %d highlighted %v went %v", ed.restored, ed.highlighted, ed.went)
	}
}

func TestGoToLineProviderAccept(t *testing.T) {
	ed := newFakeEditor("one\ntwo\n\tthree\nfour")
	c := newGoToController(ed)
	c.Show(":", ShowOptions{})
	// there is nothing to go to yet
	c.Accept(KeyMods{})
	if !c.Visible() || len(ed.went) != 0 {
		t.Errorf("visible %v went %v", c.Visible(), ed.went)
	}
	c.SetValue(":3:5")
	c.Accept(KeyMods{})
	if c.Visible() || !reflect.DeepEqual(ed.went, [][2]int{{3, 2}}) || ed.restored != 0 || ed.highlighted {
		t.Errorf("visible %v went %v restored %d highlighted %v", c.Visible(), ed.went, ed.restored, ed.highlighted)
	}
}

func TestGoToLineSecondColonIsTheOffsetMode(t *testing.T) {
	ed := newFakeEditor("one\ntwo\n\tthree\nfour")
	c := newGoToController(ed)
	c.Show(":", ShowOptions{})
	c.SetValue("::")
	if c.Provider().Info().Prefix != "::" {
		t.Fatalf("prefix %q", c.Provider().Info().Prefix)
	}
	if got := labelsOf(c.Picker().Rows()); !reflect.DeepEqual(got, []string{"Type a character position to go to (from 1 to 19)."}) {
		t.Errorf("rows %v", got)
	}
	c.SetValue("::6")
	c.Accept(KeyMods{})
	if !reflect.DeepEqual(ed.went, [][2]int{{2, 2}}) {
		t.Errorf("went %v", ed.went)
	}
}

func TestGoToLineWithoutAnEditor(t *testing.T) {
	c := newGoToController(nil)
	c.Show(":12", ShowOptions{})
	if got := labelsOf(c.Picker().Rows()); !reflect.DeepEqual(got, []string{"Open a text editor first to go to a line or an offset."}) {
		t.Errorf("rows %v", got)
	}
	c.Accept(KeyMods{})
	if !c.Visible() {
		t.Errorf("accepting with no editor closed the picker")
	}
	c.Hide()
}

func TestGoToLineProviderInfo(t *testing.T) {
	line := NewGoToLineProvider(":", nil).Info()
	off := NewGoToLineProvider("::", nil).Info()
	if line.Help[0].Description != "Go to Line/Column" || line.Help[0].Command != "workbench.action.gotoLine" ||
		off.Help[0].Description != "Go to Offset" || off.Help[0].Command != "workbench.action.gotoOffset" {
		t.Errorf("help %+v %+v", line.Help, off.Help)
	}
}
