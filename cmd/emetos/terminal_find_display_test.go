//go:build emetos_display

package main

import (
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"github.com/rm4n0s/emetos/internal/testenv/display"
)

// Real-input tests of step 17d.3: find in the terminal, the links under the pointer and the commands that put text in the
// terminal. The keys and the pointer go through the compositor, so the key dispatcher of the window (Ctrl+F, Enter, Escape of
// the find widget, with the keys that skip the shell) is part of what is tested.

// outputRow is the row of the screen of a terminal that is exactly the line, -1 when there is none.
func outputRow(tm display.Terminal, line string) int {
	for r, l := range strings.Split(tm.Text, "\n") {
		if l == line {
			return r
		}
	}
	return -1
}

func TestRealInputTerminalFindFindsAWordInTheOutput(t *testing.T) {
	a := startTerminalApp(t, nil)
	for _, cmd := range []string{"echo one needle", "echo two haystack", "echo three Needle"} {
		a.Type(cmd)
		a.Press("Return")
	}
	waitTermText(t, a, 0, "three Needle")
	a.Press("ctrl+f")
	a.Eventually("the find widget to show", func() (bool, string) {
		tm := a.Terminals()[0]
		in, ok := a.Focused()
		return tm.FindVisible && ok, "focused: " + in.Text
	})
	a.Type("needle")
	// the lines typed and the lines printed: four matches, the case is ignored; the last one is the current one
	a.Eventually("four matches with the last current", func() (bool, string) {
		tm := a.Terminals()[0]
		return tm.FindText == "needle" && tm.Matches == 4 && tm.Current == 4, "matches " + itoa(tm.Matches) + " current " + itoa(tm.Current)
	})
	a.WaitText("4 of 4")
	// Enter in the input goes to the previous match (VS Code's key table), Shift+Enter to the next one, around the ends
	a.Press("Return")
	a.Eventually("the third match", func() (bool, string) { return a.Terminals()[0].Current == 3, itoa(a.Terminals()[0].Current) })
	a.WaitText("3 of 4")
	a.Screenshot(filepath.Join(proofDir(t), "17d.3-find.png"))
	a.Press("shift+Return")
	a.Eventually("the fourth match again", func() (bool, string) { return a.Terminals()[0].Current == 4, "" })
	a.Press("shift+Return")
	a.Eventually("the first match, around the end", func() (bool, string) { return a.Terminals()[0].Current == 1, "" })
	// F3 and Shift+F3 move too
	a.Press("F3")
	a.Eventually("the second match with F3", func() (bool, string) { return a.Terminals()[0].Current == 2, "" })
	a.Press("shift+F3")
	a.Eventually("the first match with Shift+F3", func() (bool, string) { return a.Terminals()[0].Current == 1, "" })
	// Alt+C makes the case count: the typed line and the printed line of "needle", not those of "Needle"
	a.Press("alt+c")
	a.Eventually("two matches with the case counting", func() (bool, string) { return a.Terminals()[0].Matches == 2, itoa(a.Terminals()[0].Matches) })
	a.Press("alt+c")
	// a regular expression
	a.Press("alt+r")
	a.Press("ctrl+a")
	a.Type("t(wo|hree)")
	a.Eventually("the matches of the regular expression", func() (bool, string) {
		tm := a.Terminals()[0]
		return tm.FindText == "t(wo|hree)" && tm.Matches >= 4, "matches " + itoa(tm.Matches)
	})
	// Escape closes the widget and the terminal has the keyboard again: what is typed goes to the shell
	a.Press("Escape")
	a.Eventually("the widget to close and the terminal to have the focus", func() (bool, string) {
		tm := a.Terminals()[0]
		return !tm.FindVisible && tm.Focused, "visible " + strconv.FormatBool(tm.FindVisible) + ", focused " + strconv.FormatBool(tm.Focused)
	})
	a.Type("echo after")
	a.Press("Return")
	waitTermText(t, a, 0, "after")
}

func TestRealInputTerminalCtrlClickOnAFileLineOpensTheFileAtTheLine(t *testing.T) {
	a := startTerminalApp(t, map[string]string{"main.go": "package main\n\nfunc main() {\n\tprintln(1)\n}\n"})
	// the printed line is made by the shell, so that the typed command does not hold the path itself
	a.Type("printf '%s:%d:%d: boom\\n' main.go 3 1")
	a.Press("Return")
	a.Eventually("the printed line", func() (bool, string) { return outputRow(a.Terminals()[0], "main.go:3:1: boom") >= 0, termText(a, 0) })
	tm := a.Terminals()[0]
	row := outputRow(tm, "main.go:3:1: boom")
	x, y := cellPoint(tm, 4, row)
	a.MoveTo(x, y)
	want, _ := filepath.EvalSymlinks(filepath.Join(a.Project, "main.go"))
	a.Eventually("the link under the pointer", func() (bool, string) {
		tm := a.Terminals()[0]
		return tm.Link == want+":3:1" && tm.LinkCells == len("main.go:3:1"), "link " + tm.Link + " cells " + itoa(tm.LinkCells)
	})
	a.Eventually("the tooltip", func() (bool, string) {
		_, ok := a.Text("Follow link (ctrl + click)")
		return ok && a.Terminals()[0].Tooltip, "no tooltip"
	})
	a.Screenshot(filepath.Join(proofDir(t), "17d.3-link.png"))
	// a click without Ctrl opens nothing: the editor stays on app.go
	a.Click(x, y)
	if p := a.Editor().Path; !strings.HasSuffix(p, "app.go") {
		t.Fatalf("a click without Ctrl opened %s", p)
	}
	a.CtrlClick(x, y)
	a.Eventually("main.go to open at line 3", func() (bool, string) {
		ed := a.Editor()
		return strings.HasSuffix(ed.Path, "main.go") && ed.Line == 3 && ed.Column == 1, ed.Path + ":" + itoa(ed.Line) + ":" + itoa(ed.Column)
	})
}

func TestRealInputTerminalHoverOnAnAddressShowsItAsALink(t *testing.T) {
	a := startTerminalApp(t, nil)
	a.Type("printf 'docs at %s now\\n' https://example.com/guide")
	a.Press("Return")
	a.Eventually("the printed line", func() (bool, string) {
		return outputRow(a.Terminals()[0], "docs at https://example.com/guide now") >= 0, termText(a, 0)
	})
	tm := a.Terminals()[0]
	x, y := cellPoint(tm, 12, outputRow(tm, "docs at https://example.com/guide now"))
	a.MoveTo(x, y)
	a.Eventually("the address under the pointer", func() (bool, string) {
		return a.Terminals()[0].Link == "https://example.com/guide", a.Terminals()[0].Link
	})
}

func TestRealInputTerminalRunSelectedTextFromAnEditor(t *testing.T) {
	files := map[string]string{"run.sh": "echo first from the editor\n  echo second from the editor  \necho third\n"}
	a := display.Start(t, display.Options{Args: []string{".", "run.sh"}, Project: files, Config: terminalConfig})
	a.Settle()
	a.Press("ctrl+grave")
	waitTerminals(t, a, 1)
	waitPrompt(t, a, 0)
	// the terminal has the keyboard: Ctrl+Shift+P still opens the palette, and the editor is the one the text comes from
	a.Click(editorColumnX(30), editorLine(2))
	a.Eventually("the cursor in the second line", func() (bool, string) { return a.Editor().Line == 2, itoa(a.Editor().Line) })
	runCommand := func(title string) {
		a.Press("ctrl+shift+p")
		a.Type(title)
		a.WaitText("Terminal: " + title)
		a.Press("Return")
	}
	// no selection: the line of the cursor, without the blanks around it
	runCommand("Run Selected Text In Active Terminal")
	a.Eventually("the second line to run", func() (bool, string) { return hasLine(termText(a, 0), "second from the editor"), termText(a, 0) })
	if got := termText(a, 0); strings.Contains(got, "third") {
		t.Errorf("the line below ran:\n%s", got)
	}
	// the editor keeps the keyboard: the terminal does not take it
	if tm := a.Terminals()[0]; tm.Focused {
		t.Error("running the selection moved the keyboard to the terminal")
	}
	// a selection of two lines
	a.Press("ctrl+Home")
	a.Press("shift+Down", "shift+Down")
	runCommand("Run Selected Text In Active Terminal")
	a.Eventually("the two lines to run", func() (bool, string) {
		got := termText(a, 0)
		return hasLine(got, "first from the editor"), got
	})
}

func TestRealInputTerminalRunActiveFileAndSendSequenceAndRename(t *testing.T) {
	files := map[string]string{"run.sh": "echo hi\n"}
	a := display.Start(t, display.Options{Args: []string{".", "run.sh"}, Project: files, Config: terminalConfig})
	a.Settle()
	a.Press("ctrl+grave")
	waitTerminals(t, a, 1)
	waitPrompt(t, a, 0)
	a.Click(editorColumnX(3), editorLine(1))
	a.Press("ctrl+shift+p")
	a.Type("Run Active File In Active Terminal")
	a.WaitText("Terminal: Run Active File In Active Terminal")
	a.Press("Return")
	a.Eventually("the file to be run by the shell", func() (bool, string) {
		got := termText(a, 0)
		return strings.Contains(got, "run.sh") && strings.Contains(got, "Permission denied"), got
	})
	// rename asks for the name, and the tab shows it
	a.Press("ctrl+shift+grave")
	waitTerminals(t, a, 1)
	waitPrompt(t, a, 0)
	a.Press("ctrl+shift+p")
	a.Type("Terminal: Rename...")
	// "Rename..." and "Rename the Currently Active Terminal" both match: the palette sorts them by name, so click the one wanted
	a.WaitText("Terminal: Rename...")
	a.ClickText("Terminal: Rename...")
	a.Eventually("the box for the name, with the name of the terminal in it", func() (bool, string) {
		in, ok := a.Focused()
		return ok && in.Text == "bash", in.Text
	})
	a.Press("ctrl+a")
	a.Type("builder")
	a.Press("Return")
	a.WaitText("builder")
}

func TestRealInputTerminalSearchWorkspaceOpensTheSearchViewWithTheSelection(t *testing.T) {
	a := startTerminalApp(t, map[string]string{"notes.txt": "the zebraquartz is here\nnothing else\n"})
	a.Type("echo zebraquartz")
	a.Press("Return")
	a.Eventually("the printed word", func() (bool, string) { return outputRow(a.Terminals()[0], "zebraquartz") >= 0, termText(a, 0) })
	tm := a.Terminals()[0]
	x, y := cellPoint(tm, 3, outputRow(tm, "zebraquartz"))
	// a double click selects the word; Ctrl+Shift+F in the terminal searches the workspace for it, not the Search view's empty box
	a.DoubleClick(x, y)
	a.Press("ctrl+shift+f")
	a.WaitText("1 result in 1 file")
	a.WaitText("notes.txt")
	in, ok := a.Focused()
	if !ok || in.Text != "zebraquartz" {
		t.Errorf("the search box = %q (focused %v)", in.Text, ok)
	}
}
