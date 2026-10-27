//go:build emetos_display

package main

import (
	"strings"
	"testing"
	"time"

	"github.com/rm4n0s/emetos/internal/testenv/display"
)

// Quick Open, the command palette, Go to Line, Go to Symbol, and Find and Replace in a file (step 17b.5).

var navProject = map[string]string{
	"app.go":           goApp,
	"main.go":          "package main\n",
	"notes/idea.md":    "# Idea\n",
	"sub/deep/more.go": "package deep\n",
	"words.txt":        "one two\nthree one\nfour one five\n",
}

func startNav(t *testing.T) *display.App {
	t.Helper()
	a := display.Start(t, display.Options{Args: []string{".", "app.go"}, Project: navProject, Prepare: withFakeGopls(t, goplsOptions{})})
	a.Settle()
	return a
}

func TestDesktopQuickOpen(t *testing.T) {
	a := startNav(t)
	a.Press("ctrl+p")
	a.Type("more")
	a.WaitFocusedText("more")
	a.WaitText("more.go sub/deep")
	shot(t, a, "quickopen-list")
	a.Press("Enter")
	a.Eventually("more.go to open", func() (bool, string) { return activeTab(a) == "more.go", joinNames(tabNames(a)) })
	// the focus went to the editor: typing changes the document
	a.Type("// x")
	a.Eventually("the typing to reach the editor", func() (bool, string) { return strings.Contains(editorText(a), "// x"), editorText(a) })
	// Escape closes the list and leaves the editor as it was
	a.Press("ctrl+p")
	a.Type("idea")
	a.WaitText("idea.md notes")
	a.Press("Escape")
	a.Eventually("the list to close", func() (bool, string) { return len(a.Inputs()) == 0, "" })
	if got := activeTab(a); got != "more.go" {
		t.Errorf("Escape changed the tab to %q", got)
	}
}

func TestDesktopCommandPalette(t *testing.T) {
	a := startNav(t)
	a.Press("ctrl+shift+p")
	a.Type("new untitled")
	a.WaitFocusedText(">new untitled")
	a.WaitText("File: New Untitled Text File")
	shot(t, a, "palette")
	a.Press("Enter")
	a.Eventually("an untitled editor", func() (bool, string) { return activeTab(a) == "Untitled-1", joinNames(tabNames(a)) })
	a.Type("typed in the new file")
	a.Eventually("the text in the new editor", func() (bool, string) { return editorText(a) == "typed in the new file", editorText(a) })
	// a command that has a key shows it and runs
	a.Press("ctrl+shift+p")
	a.Type("toggle word wrap")
	a.WaitText("View: Toggle Word Wrap")
	a.Press("Escape")
}

func TestDesktopGoToLineAndSymbol(t *testing.T) {
	a := startNav(t)
	a.Press("ctrl+g")
	a.Type("6:5")
	a.WaitFocusedText(":6:5")
	a.Press("Enter")
	a.Eventually("the cursor at 6:5", func() (bool, string) {
		ed := a.Editor()
		return ed.Line == 6 && ed.Column == 2, // the column is a visible one: after the tab
			itoa(ed.Line) + ":" + itoa(ed.Column)
	})
	shot(t, a, "gotoline")
	a.Press("ctrl+shift+o")
	a.Eventually("the symbol list", func() (bool, string) {
		_, ok := a.Text("main")
		return ok, ""
	})
	shot(t, a, "gotosymbol")
	a.Press("Enter")
	a.Eventually("the cursor on main", func() (bool, string) { return a.Editor().Line == 5, itoa(a.Editor().Line) })
}

func TestDesktopFindAndReplace(t *testing.T) {
	// a person types a key every 100 ms or so; the 10 ms of the harness races the find box (docs/DESKTOP-PASS.md, "keys 10 ms apart")
	a := display.Start(t, display.Options{Args: []string{".", "words.txt"}, Project: navProject, KeyDelay: 25 * time.Millisecond})
	a.Settle()
	a.Press("ctrl+f")
	// one key at a time, each handled before the next (D5 of docs/DESKTOP-PASS.md: keys a frame apart race the find box, most of
	// all on a loaded machine)
	for _, c := range "one" {
		a.Type(string(c))
		a.Settle()
	}
	a.WaitFocusedText("one")
	a.WaitText("1 of 3")
	a.Eventually("the cursor to be on the first match", func() (bool, string) { return a.Editor().Column == 4, itoa(a.Editor().Column) })
	shot(t, a, "find")
	a.Press("Enter")
	a.WaitText("2 of 3")
	a.Press("shift+Enter")
	a.WaitText("1 of 3")
	// replace
	a.Press("ctrl+h")
	a.Settle()
	inputs := sortedInputs(a)
	if len(inputs) < 2 {
		t.Fatalf("no replace box: %+v", inputs)
	}
	a.ClickInput(inputs[1])
	a.Type("1")
	a.WaitFocusedText("1")
	shot(t, a, "replace")
	a.Press("Enter") // replace the current match and go to the next
	a.Eventually("the first one to be replaced", func() (bool, string) {
		return editorText(a) == "1 two\nthree one\nfour one five\n", editorText(a)
	})
	a.Press("ctrl+alt+Enter") // replace all
	a.Eventually("every one replaced", func() (bool, string) {
		return editorText(a) == "1 two\nthree 1\nfour 1 five\n", editorText(a)
	})
	shot(t, a, "replaced")
	a.Press("Escape")
	a.Settle() // the focus goes back to the editor a frame later
	a.Press("ctrl+z")
	a.Eventually("replace all to be one undo step", func() (bool, string) {
		return editorText(a) == "1 two\nthree one\nfour one five\n" || editorText(a) == "one two\nthree one\nfour one five\n", editorText(a)
	})
}
