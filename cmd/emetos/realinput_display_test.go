//go:build emetos_display

package main

import (
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"testing"

	"github.com/rm4n0s/emetos/internal/gui/app"
	"github.com/rm4n0s/emetos/internal/testenv/display"
)

// These tests type and click through the compositor (internal/testenv/realinput), the way a user does, and read what arrived
// through the dev server. Run them under a headless compositor:
//
//	scripts/headless-wayland.sh -- go test -race -tags emetos_display ./cmd/emetos
//
// Every input box of Emetos has one: the bug of 2026-10-27 (an input box that kept only its first key) was missed by every
// widget test, which type through the fake platform (SPEC §15.1 "Real input").

func TestMain(m *testing.M) { os.Exit(display.Main(m)) }

const hello = "hello world"

// projectFiles is the project the scenarios open: a Go file with a few lines, and others for Quick Open.
var projectFiles = map[string]string{
	"app.go":        "package main\n\nfunc main() {\n}\n",
	"main.go":       "package main\n",
	"notes/idea.md": "# Idea\n",
}

// startProject starts the editor on the project folder with app.go open.
func startProject(t *testing.T) *display.App {
	t.Helper()
	return display.Start(t, display.Options{Args: []string{".", "app.go"}, Project: projectFiles})
}

// sortedInputs are the text fields on screen from the top down.
func sortedInputs(a *display.App) []app.DevInput {
	list := a.Inputs()
	sort.SliceStable(list, func(i, j int) bool { return list[i].Y < list[j].Y })
	return list
}

// editorText is the text of the document in the editor.
func editorText(a *display.App) string { return a.Editor().Text }

func TestRealInputQuickOpenTakesEveryKey(t *testing.T) {
	a := startProject(t)
	before := editorText(a)
	// the keys follow the shortcut at once: they must go into the quick input, not the document
	a.Press("ctrl+p")
	a.Type("app.go")
	a.WaitFocusedText("app.go")
	a.Press("BackSpace", "BackSpace", "BackSpace", "BackSpace", "BackSpace", "BackSpace")
	a.Type(hello)
	a.WaitFocusedText(hello)
	if got := editorText(a); got != before {
		t.Errorf("the document changed to %q", got)
	}
	a.Press("Escape")
}

func TestRealInputCommandPaletteAfterDeletingTheGreaterThan(t *testing.T) {
	a := startProject(t)
	a.Press("ctrl+shift+p")
	a.Type("x")
	a.WaitFocusedText(">x")
	a.Press("BackSpace", "BackSpace")
	a.Type(hello)
	a.WaitFocusedText(hello)
	if got := editorText(a); got != projectFiles["app.go"] {
		t.Errorf("the document changed to %q", got)
	}
}

func TestRealInputFindAndReplace(t *testing.T) {
	a := startProject(t)
	a.Press("ctrl+f")
	a.Type(hello)
	a.WaitFocusedText(hello)
	if got := editorText(a); got != projectFiles["app.go"] {
		t.Errorf("the document changed to %q", got)
	}
	// Ctrl+H shows the replace row; the box that has the focus is the find box, which keeps its text
	a.Press("ctrl+h")
	a.Settle()
	inputs := sortedInputs(a)
	if len(inputs) < 2 {
		t.Fatalf("the replace row did not show: %+v", inputs)
	}
	a.ClickInput(inputs[1])
	a.Settle()
	a.Type(hello)
	a.WaitFocusedText(hello)
	if got := sortedInputs(a); got[0].Text != hello || got[1].Text != hello {
		t.Errorf("the find and replace boxes hold %q and %q", got[0].Text, got[1].Text)
	}
	// a click in the find box that has the focus keeps typing working
	a.ClickInput(sortedInputs(a)[0])
	a.Settle()
	a.Press("End", "BackSpace", "BackSpace", "BackSpace", "BackSpace", "BackSpace")
	a.Type("again")
	a.WaitFocusedText("hello again")
}

func TestRealInputGoToLine(t *testing.T) {
	a := startProject(t)
	a.Press("ctrl+g")
	a.Type("3")
	a.WaitFocusedText(":3")
	a.Press("Enter")
	a.Eventually("the cursor on line 3", func() (bool, string) {
		ed := a.Editor()
		return ed.Line == 3, "line " + strconv.Itoa(ed.Line)
	})
}

func TestRealInputSettingsSearch(t *testing.T) {
	a := startProject(t)
	a.Press("ctrl+comma")
	a.Settle()
	a.Eventually("the search box of the settings editor to have the focus", func() (bool, string) {
		_, ok := a.Focused()
		return ok, "no text field has the focus"
	})
	a.Type(hello)
	a.WaitFocusedText(hello)
	// a click in the box keeps typing working
	in, _ := a.Focused()
	a.ClickInput(in)
	a.Type("!")
	a.WaitFocusedText(hello + "!")
}

func TestRealInputKeybindingsSearch(t *testing.T) {
	a := startProject(t)
	a.Press("ctrl+k", "ctrl+s")
	a.Settle()
	a.Eventually("the search box of the keybindings editor to have the focus", func() (bool, string) {
		_, ok := a.Focused()
		return ok, "no text field has the focus"
	})
	a.Type(hello)
	a.WaitFocusedText(hello)
}

func TestRealInputExplorerRenameAndNewFile(t *testing.T) {
	a := startProject(t)
	a.Press("ctrl+shift+e")
	a.Settle()
	// the tree has the focus: the first row is the folder's first entry; walk down to main.go and rename it
	a.Press("Down", "Down")
	a.Press("F2")
	a.Type(hello)
	a.WaitFocusedText(hello + ".go")
	a.Press("Escape")
	a.Settle()
	a.Dev("explorer.newFile", nil)
	a.Type("brand-new.txt")
	a.WaitFocusedText("brand-new.txt")
	a.Press("Escape")
}

func TestRealInputProblemsFilter(t *testing.T) {
	a := startProject(t)
	// The tabs of the panel get the room first and the actions of the view what they leave (paneloverflow.go): with the tabs of the
	// Go, Redis, Containers and Podman extensions the filter of Problems has no room at 1280 px beside the side bar (the compositor
	// does not make the window wider than its output), so the side bar is closed.
	a.Press("ctrl+b")
	a.Settle()
	a.Press("ctrl+shift+m")
	a.Settle()
	a.Dev("problems.action.focusFilter", nil)
	a.Settle()
	a.Type(hello)
	a.WaitFocusedText(hello)
}

// TestRealInputRemoteWorkTabForm: the window tab of Remote Work has two boxes, the host and the port; each takes what is typed
// into it. It replaced the tests of the Notes and Scratchpad boxes, which went with those extensions (step 17e.1).
func TestRealInputRemoteWorkTabForm(t *testing.T) {
	a := startProject(t)
	a.Dev("dev.openWindowTab", map[string]any{"kind": "emetos.remotework.tab"})
	a.Settle()
	inputs := sortedInputs(a)
	if len(inputs) < 2 {
		t.Fatalf("the tab has %d text fields, want the host and the port: %+v", len(inputs), inputs)
	}
	a.ClickInput(inputs[0])
	a.Type(hello)
	a.WaitFocusedText(hello)
	a.ClickInput(inputs[1])
	a.Press("ctrl+a")
	a.Type("2222")
	a.WaitFocusedText("2222")
	if got := sortedInputs(a)[0].Text; got != hello {
		t.Errorf("the host holds %q", got)
	}
}

// proofDir returns where the proof images go when EMETOS_PROOF_DIR is set, else a folder of the test.
func proofDir(t *testing.T) string {
	if dir := os.Getenv("EMETOS_PROOF_DIR"); dir != "" {
		return dir
	}
	return t.TempDir()
}

func TestRealInputProofs(t *testing.T) {
	dir := proofDir(t)
	t.Run("quick open", func(t *testing.T) {
		a := startProject(t)
		a.Press("ctrl+p")
		a.Type("app.go")
		a.WaitFocusedText("app.go")
		a.Settle()
		a.Screenshot(filepath.Join(dir, "17b.1-quickopen.png"))
	})
}

// TestRealInputEditShortcutsActOnTheBox: Ctrl+A and Ctrl+Z in a box act on its text, not on the document or the files
// (the window binds them to the commands of the editor and of the explorer).
func TestRealInputEditShortcutsActOnTheBox(t *testing.T) {
	a := startProject(t)
	before := editorText(a)
	a.Press("ctrl+p")
	a.Type("abc")
	a.WaitFocusedText("abc")
	a.Press("ctrl+a")
	a.Settle()
	in, _ := a.Focused()
	if in.SelectionStart != 0 || in.SelectionEnd != 3 {
		t.Errorf("after Ctrl+A the selection is %d..%d, want 0..3", in.SelectionStart, in.SelectionEnd)
	}
	a.Type("x")
	a.WaitFocusedText("x")
	if got := editorText(a); got != before {
		t.Errorf("the document changed to %q", got)
	}
}
