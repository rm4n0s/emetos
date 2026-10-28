//go:build emetos_display

package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/rm4n0s/emetos/extension/ui/uitest/mdfixture"
	"github.com/rm4n0s/emetos/internal/gui/app"
	"github.com/rm4n0s/emetos/internal/testenv/display"
)

// A session as a user has it: opening a folder, the Markdown preview, and a second launch handing a file over (step 17b.5).

// TestDesktopOpenFolder: an empty window offers to open a folder; File > Open Recent opens one. (The folder dialog of the
// desktop needs the session bus, which a run has none of: the button answers with a notification, which this test checks.)
func TestDesktopOpenFolder(t *testing.T) {
	var other string
	a := display.Start(t, display.Options{
		Project: map[string]string{},
		Prepare: func(projectDir string) {
			root := filepath.Dir(projectDir)
			other = filepath.Join(root, "other")
			if err := os.MkdirAll(other, 0o700); err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(filepath.Join(other, "hello.txt"), []byte("hello\n"), 0o600); err != nil {
				t.Fatal(err)
			}
			state := filepath.Join(root, "state", "emetos")
			if err := os.MkdirAll(state, 0o700); err != nil {
				t.Fatal(err)
			}
			data := `{"version":1,"recent":{"folders":["` + other + `"],"files":[]}}`
			if err := os.WriteFile(filepath.Join(state, "state.json"), []byte(data), 0o600); err != nil {
				t.Fatal(err)
			}
		},
	})
	a.Settle()
	a.WaitText("You have not yet opened a folder.")
	shot(t, a, "folder-none")
	if w := a.Workbench().WindowTabs; len(w) != 1 || w[0].Folder != "" {
		t.Fatalf("the window tabs of an empty window: %+v", w)
	}
	// the button asks the desktop for a folder: without a session bus that fails, and the user is told
	a.ClickText("Open Folder")
	a.Eventually("a notification that the dialog cannot be shown", func() (bool, string) {
		for _, tx := range a.Texts() {
			if strings.Contains(tx.Text, "Cannot show the dialog") {
				return true, ""
			}
		}
		return false, a.Dump()
	})
	shot(t, a, "folder-dialog-failed")
	// Ctrl+R lists the folders opened before
	a.Press("ctrl+r")
	a.WaitText("Select to open")
	a.Type("other")
	a.WaitFocusedText("other")
	shot(t, a, "folder-recent")
	a.Press("Enter")
	a.Eventually("the folder to open", func() (bool, string) {
		w, ok := a.TryWorkbench()
		return ok && len(w.WindowTabs) == 1 && w.WindowTabs[0].Folder == other, a.Dump()
	})
	a.WaitText("hello.txt")
	a.ClickText("hello.txt")
	a.Eventually("hello.txt to open", func() (bool, string) { return activeTab(a) == "hello.txt", joinNames(tabNames(a)) })
	shot(t, a, "folder-opened")
}

// TestDesktopMarkdownPreview: the preview opens beside the source, shows its blocks, scrolls with the wheel, and follows an edit.
func TestDesktopMarkdownPreview(t *testing.T) {
	a := display.Start(t, display.Options{
		Args:    []string{".", mdfixture.DocName},
		Prepare: func(dir string) { mdfixture.Generate(t, dir) },
	})
	a.Settle()
	// the action in the title of the editor, left of the others (the Notes extension had one too until step 17e.1)
	var actions []app.DevText
	for _, tx := range a.Texts() {
		if tx.Y > 36 && tx.Y < 70 && tx.X > 1100 && len([]rune(tx.Text)) == 1 {
			actions = append(actions, tx)
		}
	}
	if len(actions) < 1 {
		t.Fatalf("%d editor actions, want the preview's among them: %s", len(actions), a.Dump())
	}
	first := actions[0]
	for _, tx := range actions {
		if tx.X < first.X {
			first = tx
		}
	}
	a.MoveTo(first.X+8, first.Y+8)
	a.Click(first.X+8, first.Y+8)
	var p app.DevMarkdown
	a.Eventually("the preview to build its first blocks", func() (bool, string) {
		list := a.Previews()
		if len(list) == 1 && list[0].Built > 0 {
			p = list[0]
			return true, ""
		}
		return false, "no preview yet"
	})
	shot(t, a, "markdown-preview")
	// the wheel scrolls it
	a.Scroll(p.X+p.Width/2, p.Y+p.Height/2, 0, 8)
	a.Eventually("the preview to scroll", func() (bool, string) {
		list := a.Previews()
		return len(list) == 1 && list[0].ScrollTop > 0, a.Dump()
	})
	shot(t, a, "markdown-scrolled")
	// an edit of the source reaches the preview: the source has the focus again after a click in it
	before := a.Previews()[0].Blocks
	a.Click(500, 300)
	a.Press("ctrl+End")
	a.Type("\n\nA paragraph typed at the end.\n")
	a.Eventually("the preview to have the new block", func() (bool, string) {
		list := a.Previews()
		return len(list) == 1 && list[0].Blocks > before, itoa(before)
	})
}

// TestDesktopSecondLaunchHandsAFileOver: `emetos file` while Emetos runs opens the file in the running instance and ends
// (SPEC §10.11).
func TestDesktopSecondLaunchHandsAFileOver(t *testing.T) {
	a := display.Start(t, display.Options{
		Args: []string{".", "app.go"}, Project: map[string]string{"app.go": goApp, "late.txt": "arrived\n"},
	})
	a.Settle()
	if got := tabNames(a); !sameNames(got, "app.go") {
		t.Fatalf("tabs at the start: %v", got)
	}
	a.Launch("late.txt")
	a.Eventually("late.txt to open in the running editor", func() (bool, string) { return activeTab(a) == "late.txt", joinNames(tabNames(a)) })
	if ed := a.Editor(); ed.Text != "arrived\n" {
		t.Errorf("the file shows %q", ed.Text)
	}
	if n := len(a.Workbench().WindowTabs); n != 1 {
		t.Errorf("%d window tabs after the hand over, want 1", n)
	}
	// with a line and a column
	a.Launch("--goto", "app.go:6:3")
	a.Eventually("the cursor at the place", func() (bool, string) {
		ed := a.Editor()
		return activeTab(a) == "app.go" && ed.Line == 6 && ed.Column == 3, joinNames(tabNames(a)) + " " + itoa(a.Editor().Line)
	})
	shot(t, a, "second-launch")
}
