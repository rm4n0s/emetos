//go:build emetos_display

package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/rm4n0s/emetos/internal/testenv/display"
)

// Editing a Go file as a user does (step 17b.5): typing, the brackets the editor closes, completion and hover from a language
// server (the fake one; the real gopls has its own test under the tag emetos_gopls), the Problems it reports, save, undo, redo.

// startGoApp starts the editor on app.go with the fake language server.
func startGoApp(t *testing.T, opts goplsOptions) *display.App {
	t.Helper()
	a := display.Start(t, display.Options{
		Args: []string{".", "app.go"}, Project: map[string]string{"app.go": goApp}, Prepare: withFakeGopls(t, opts),
	})
	a.Settle()
	return a
}

func TestDesktopEditGoFile(t *testing.T) {
	a := startGoApp(t, goplsOptions{})
	want := goApp

	t.Run("typing and the brackets the editor closes", func(t *testing.T) {
		// the cursor goes to the end of the file, a new line inside main would be above the brace: type at the end of line 6
		a.Click(editorColumnX(20), editorLine(6))
		a.Press("End", "Enter")
		a.Type(`fmt.Println("x`)
		// the editor closed the quote and the parenthesis: the text is complete
		a.Eventually("the closed brackets", func() (bool, string) {
			got := editorText(a)
			return strings.Contains(got, "\tfmt.Println(\"hi\")\n\t\tfmt.Println(\"x\")") ||
				strings.Contains(got, "\tfmt.Println(\"hi\")\n\tfmt.Println(\"x\")"), got
		})
		a.Type(`")`)
		a.Eventually("the typed closing characters to go over the ones of the editor", func() (bool, string) {
			got := editorText(a)
			return strings.Count(got, `fmt.Println("x")`) == 1 && !strings.Contains(got, `"x"))`) && !strings.Contains(got, `"x"")`), got
		})
		want = editorText(a)
		if !a.Editor().Dirty {
			t.Error("the document is not dirty after typing")
		}
		shot(t, a, "edit-typing")
	})
	t.Run("save", func(t *testing.T) {
		a.Press("ctrl+s")
		a.Eventually("the file to be saved", func() (bool, string) {
			data, err := os.ReadFile(filepath.Join(a.Project, "app.go"))
			return err == nil && string(data) == want && !a.Editor().Dirty, string(data)
		})
		shot(t, a, "edit-saved")
	})
	t.Run("undo and redo", func(t *testing.T) {
		a.Type("// more")
		a.Eventually("the comment", func() (bool, string) {
			return strings.HasSuffix(strings.TrimRight(editorText(a), "\n"), "// more") || strings.Contains(editorText(a), "// more"), editorText(a)
		})
		// undo until the saved text is back: the typing of one word is one step or a few, but never more than ten
		undone := false
		for i := 0; i < 10 && !undone; i++ {
			a.Press("ctrl+z")
			a.Settle()
			undone = editorText(a) == want
		}
		if !undone {
			t.Fatalf("ten undos did not bring the saved text back: %q", editorText(a))
		}
		if a.Editor().Dirty {
			t.Error("the document is dirty at the saved text")
		}
		a.Press("ctrl+shift+z")
		a.Eventually("redo to bring a change back", func() (bool, string) { return editorText(a) != want, editorText(a) })
		a.Press("ctrl+z")
		a.Eventually("undo again", func() (bool, string) { return editorText(a) == want, editorText(a) })
	})
	t.Run("completion", func(t *testing.T) {
		a.Press("ctrl+End")
		a.Press("Enter")
		a.Type("fmt.")
		a.Eventually("the suggestions", func() (bool, string) { return a.Editor().SuggestVisible, editorText(a) })
		a.Type("Sprin")
		a.Settle()
		shot(t, a, "edit-completion")
		a.Press("Enter")
		a.Eventually("Sprintf to be inserted", func() (bool, string) {
			return strings.HasSuffix(strings.TrimRight(editorText(a), "\n\t "), "fmt.Sprintf"), editorText(a)
		})
		if a.Editor().SuggestVisible {
			t.Error("the suggestions stayed after Enter")
		}
		a.Press("ctrl+z")
		a.Settle()
	})
	t.Run("hover", func(t *testing.T) {
		// func main: the word main is on line 5 at columns 5 to 9
		a.MoveTo(editorColumnX(7), editorLine(5))
		a.Eventually("the hover to show", func() (bool, string) { return a.Editor().HoverVisible, "no hover" })
		shot(t, a, "edit-hover")
		a.MoveTo(editorColumnX(60), editorLine(14))
		a.Eventually("the hover to go away", func() (bool, string) { return !a.Editor().HoverVisible, "hover" })
	})
}

// TestDesktopEditProblems: a diagnostic of the language server is in the Problems panel, a click on it goes to its place.
func TestDesktopEditProblems(t *testing.T) {
	a := startGoApp(t, goplsOptions{Diagnostics: true})
	a.Press("ctrl+shift+m")
	a.WaitText("undefined: fmtt")
	shot(t, a, "problems-panel")
	if w := a.Workbench(); !w.PanelVisible || w.ActivePanel != "workbench.panel.markers" {
		t.Errorf("the panel: visible %v, %q", w.PanelVisible, w.ActivePanel)
	}
	a.ClickText("undefined: fmtt")
	a.Eventually("the cursor on the problem", func() (bool, string) {
		ed := a.Editor()
		return ed.Line == 6, strings.Join([]string{"line", itoa(ed.Line), "col", itoa(ed.Column)}, " ")
	})
	shot(t, a, "problems-opened")
}

// TestDesktopEditorMouseAndClipboard: a double click selects a word, a drag selects a range, the context menu opens, Ctrl+C and
// Ctrl+V copy a line, and closing a tab with unsaved changes asks (step 17b.5).
func TestDesktopEditorMouseAndClipboard(t *testing.T) {
	a := startGoApp(t, goplsOptions{})
	// a double click on main (line 5, columns 5 to 9) selects the word: the cursor is at its end
	a.DoubleClick(editorColumnX(7), editorLine(5))
	a.Eventually("the word to be selected", func() (bool, string) {
		ed := a.Editor()
		return ed.Line == 5 && ed.Column == 10, itoa(ed.Line) + ":" + itoa(ed.Column)
	})
	shot(t, a, "mouse-word")
	// a drag over the start of line 6 puts the cursor where it ends (line 6 starts with a tab, four columns wide: the pointer at
	// column 9 is after the tab and five characters, which is column 7)
	a.Drag(editorColumnX(0)+4, editorLine(6), editorColumnX(9), editorLine(6))
	a.Eventually("the drag to select", func() (bool, string) {
		ed := a.Editor()
		return ed.Line == 6 && ed.Column == 7, itoa(ed.Line) + ":" + itoa(ed.Column)
	})
	shot(t, a, "mouse-drag")
	// the context menu of the editor
	a.RightClick(editorColumnX(12), editorLine(6))
	a.WaitText("Select All")
	shot(t, a, "mouse-menu")
	a.Press("Escape")
	a.WaitNoText("Select All")
	// copy the line and paste it below
	a.Click(editorColumnX(20), editorLine(6))
	a.Press("Home", "shift+End", "ctrl+c", "End", "Enter", "ctrl+v")
	a.Eventually("the line to be pasted", func() (bool, string) {
		return strings.Count(editorText(a), `fmt.Println("hi")`) == 2, editorText(a)
	})
	shot(t, a, "clipboard-pasted")
	// closing the tab asks, and Don't Save leaves the file as it was
	a.Click(editorColumnX(20), editorLine(1))
	a.Press("ctrl+w")
	a.WaitText("Don't Save")
	shot(t, a, "close-dirty-dialog")
	a.ClickText("Don't Save")
	a.Eventually("the tab to close", func() (bool, string) { return len(tabNames(a)) == 0, joinNames(tabNames(a)) })
	if got := readProjectFile(t, a, "app.go"); got != goApp {
		t.Errorf("the file changed to %q", got)
	}
}
