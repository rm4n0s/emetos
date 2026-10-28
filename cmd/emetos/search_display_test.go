//go:build emetos_display

package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/rm4n0s/emetos/internal/testenv/display"
)

// The Search view as a user meets it (step 17e.2, SPEC §10.27): keys and clicks through the compositor.

var searchProject = map[string]string{
	"main.go":        "package main\n\nfunc main() {\n\tneedle()\n\tNeedle()\n}\n",
	"util.go":        "package main\n\n// needle is here\nfunc util() { needle() }\n",
	"sub/deep.go":    "package sub\n\nvar needle = 1\n",
	"docs/readme.md": "# readme\n\nplain text\n",
}

func startSearch(t *testing.T) *display.App {
	t.Helper()
	a := display.Start(t, display.Options{Args: []string{".", "main.go"}, Project: searchProject})
	a.Settle()
	return a
}

// openSearch opens the Search view with Ctrl+Shift+F and waits until its box has the keyboard.
func openSearch(a *display.App) {
	a.Press("ctrl+shift+f")
	a.Eventually("the search box to have the keyboard", func() (bool, string) {
		_, ok := a.Focused()
		return ok, a.Dump()
	})
}

func TestRealInputSearch(t *testing.T) {
	a := startSearch(t)
	openSearch(a)
	// the results show while typing, with no Enter
	a.Type("needle")
	a.WaitText("5 results in 3 files")
	a.WaitText("deep.go")
	a.Settle()
	shot(t, a, "search")

	// F4 opens the next match, in the order of the list
	a.Press("f4")
	a.Eventually("the first match", func() (bool, string) {
		e := a.Editor()
		return strings.HasSuffix(e.Path, "main.go") && e.Line == 4, e.Path + ":" + itoa(e.Line)
	})
	a.Press("f4")
	a.Eventually("the second match", func() (bool, string) { return a.Editor().Line == 5, itoa(a.Editor().Line) })
	a.Press("f4")
	a.Eventually("the match of the next file", func() (bool, string) {
		e := a.Editor()
		return strings.HasSuffix(e.Path, filepath.Join("sub", "deep.go")) && e.Line == 3, e.Path + ":" + itoa(e.Line)
	})
	a.Press("shift+f4")
	a.Eventually("the match before", func() (bool, string) {
		return strings.HasSuffix(a.Editor().Path, "main.go") && a.Editor().Line == 5, a.Editor().Path
	})

	// Alt+C narrows the results: the search box takes the key
	openSearch(a)
	a.Press("alt+c")
	a.WaitText("4 results in 3 files")

	// Ctrl+Shift+H with a replacement, then Replace All and confirm
	a.Press("ctrl+shift+h")
	a.Eventually("the replace box", func() (bool, string) { return len(a.Inputs()) >= 2, a.Dump() })
	a.Press("ctrl+down")
	a.Eventually("the replace box to have the focus", func() (bool, string) {
		in, ok := a.Focused()
		return ok && in.Text == "" && len(a.Inputs()) >= 2, a.FocusedText()
	})
	a.Type("pin")
	a.WaitFocusedText("pin")
	a.Settle()
	a.Screenshot(filepath.Join(proofDir(t), "17e.2-search.png"))
	a.Press("ctrl+alt+Enter")
	a.WaitText("Replace 4 occurrences across 3 files with 'pin'?")
	a.Press("Enter")
	a.WaitText("Replaced 4 occurrences across 3 files with 'pin'.")
	waitFileContains(a, "util.go", "// pin is here")
	if got := readProjectFile(t, a, "util.go"); got != "package main\n\n// pin is here\nfunc util() { pin() }\n" {
		t.Errorf("util.go = %q", got)
	}
	if got := readProjectFile(t, a, "sub/deep.go"); got != "package sub\n\nvar pin = 1\n" {
		t.Errorf("sub/deep.go = %q", got)
	}
	// main.go is open: its part was edited in the document and saved; the capital Needle did not match the case
	waitFileContains(a, "main.go", "pin()")
	if got := readProjectFile(t, a, "main.go"); got != "package main\n\nfunc main() {\n\tpin()\n\tNeedle()\n}\n" {
		t.Errorf("main.go = %q", got)
	}
	a.Eventually("the editor to show the replacement", func() (bool, string) { return strings.Contains(a.Editor().Text, "pin()"), a.Editor().Text })

	// Ctrl+Z in the open file undoes its part
	a.Press("ctrl+1")
	a.Eventually("the editor to have the keyboard", func() (bool, string) {
		_, in := a.Focused()
		return !in, a.FocusedText()
	})
	a.Settle()
	a.Press("ctrl+z")
	a.Eventually("the undo of the replacement", func() (bool, string) {
		return a.Editor().Text == searchProject["main.go"], a.Editor().Text
	})
	shot(t, a, "search-replaced")
}

func TestRealInputFindInFolder(t *testing.T) {
	a := startSearch(t)
	a.RightClickText("sub")
	a.ClickText("Find in Folder...")
	a.Eventually("the files to include box to have the folder", func() (bool, string) {
		for _, in := range a.Inputs() {
			if in.Text == "./sub" {
				return true, ""
			}
		}
		return false, a.Dump()
	})
	a.Type("needle")
	a.WaitText("1 result in 1 file")
	shot(t, a, "search-in-folder")
}

// waitFileContains waits until a file of the project has the text.
func waitFileContains(a *display.App, rel, text string) {
	a.Eventually(rel+" to have "+text, func() (bool, string) {
		data, err := readFileOf(a, rel)
		return err == nil && strings.Contains(data, text), data
	})
}

func readFileOf(a *display.App, rel string) (string, error) {
	data, err := os.ReadFile(filepath.Join(a.Project, filepath.FromSlash(rel)))
	return string(data), err
}
