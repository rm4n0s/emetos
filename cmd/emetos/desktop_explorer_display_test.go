//go:build emetos_display

package main

import (
	"fmt"
	"os"
	"path/filepath"
	"testing"

	"github.com/rm4n0s/emetos/internal/gui/app"
	"github.com/rm4n0s/emetos/internal/testenv/display"
)

// The explorer, used with the mouse and the keyboard (step 17b.5): expand, open with a click, new file and folder, rename,
// delete to the trash, undo, drag.

var explorerProject = map[string]string{
	"app.go":         "package main\n\nfunc main() {\n}\n",
	"main.go":        "package main\n",
	"notes/idea.md":  "# Idea\n",
	"sub/deep/x.txt": "x\n",
	"junk.txt":       "junk\n",
}

func TestDesktopExplorer(t *testing.T) {
	a := display.Start(t, display.Options{Args: []string{"."}, Project: explorerProject})
	a.Settle()
	explorerRows := inRect(0, 90, 348, 750)

	t.Run("expand and open with a click", func(t *testing.T) {
		clickTextWhere(t, a, "sub", explorerRows)
		a.WaitText("deep") // compact folders: sub has only deep, so it shows as one row or as two
		shot(t, a, "explorer-expanded")
	})
	t.Run("open with one click is a preview, two keep it", func(t *testing.T) {
		clickTextWhere(t, a, "main.go", explorerRows)
		a.Eventually("main.go to open", func() (bool, string) { return activeTab(a) == "main.go", joinNames(tabNames(a)) })
		w := a.Workbench()
		if len(w.Groups[0].Tabs) != 1 || !w.Groups[0].Tabs[0].Preview {
			t.Errorf("after one click the tab is %+v, want a preview", w.Groups[0].Tabs)
		}
		clickTextWhere(t, a, "app.go", explorerRows)
		a.Eventually("app.go to replace the preview", func() (bool, string) { return activeTab(a) == "app.go", joinNames(tabNames(a)) })
		if names := tabNames(a); len(names) != 1 {
			t.Errorf("tabs %v: the second click should have replaced the preview", names)
		}
		tx, _ := textWhere(a, "app.go", explorerRows)
		a.DoubleClick(tx.X+tx.Width/2, tx.Y+tx.Height/2)
		a.Eventually("app.go to be kept", func() (bool, string) {
			g := a.Workbench().Groups[0].Tabs
			return len(g) == 1 && !g[0].Preview, joinNames(tabNames(a))
		})
		shot(t, a, "explorer-open")
	})
	t.Run("new file", func(t *testing.T) {
		clickTextWhere(t, a, "", inExplorerHeader)
		a.Type("fresh.txt")
		a.WaitFocusedText("fresh.txt")
		a.Press("Enter")
		waitFile(a, "fresh.txt", true)
		a.Eventually("fresh.txt to open", func() (bool, string) { return activeTab(a) == "fresh.txt", joinNames(tabNames(a)) })
		shot(t, a, "explorer-newfile")
	})
	t.Run("new folder", func(t *testing.T) {
		a.Click(200, 600) // the empty part of the tree: the root folder
		clickTextWhere(t, a, "", inExplorerHeader)
		a.Type("fresh-dir")
		a.WaitFocusedText("fresh-dir")
		a.Press("Enter")
		waitFile(a, "fresh-dir", true)
		if st, err := os.Stat(filepath.Join(a.Project, "fresh-dir")); err != nil || !st.IsDir() {
			t.Errorf("fresh-dir is not a folder: %v", err)
		}
		shot(t, a, "explorer-newfolder")
	})
	t.Run("rename", func(t *testing.T) {
		clickTextWhere(t, a, "junk.txt", explorerRows)
		a.Press("F2")
		a.Settle()
		a.Type("renamed")
		a.WaitFocusedText("renamed.txt")
		a.Press("Enter")
		waitFile(a, "renamed.txt", true)
		waitFile(a, "junk.txt", false)
		shot(t, a, "explorer-rename")
	})
	t.Run("delete to the trash and undo", func(t *testing.T) {
		clickTextWhere(t, a, "renamed.txt", explorerRows)
		a.Press("Delete")
		a.WaitText("Move to Trash")
		shot(t, a, "explorer-delete-confirm")
		a.ClickText("Move to Trash")
		waitFile(a, "renamed.txt", false)
		if _, err := os.Stat(filepath.Join(trashFiles(a), "renamed.txt")); err != nil {
			t.Errorf("renamed.txt is not in the trash of the run: %v", err)
		}
		shot(t, a, "explorer-deleted")
		a.Press("ctrl+z")
		a.Settle()
		shot(t, a, "explorer-undo-pressed")
		waitFile(a, "renamed.txt", true)
		shot(t, a, "explorer-undo")
	})
	t.Run("drag onto a folder", func(t *testing.T) {
		src, ok := textWhere(a, "renamed.txt", explorerRows)
		if !ok {
			t.Fatal("no renamed.txt row")
		}
		dst, ok := textWhere(a, "notes", explorerRows)
		if !ok {
			t.Fatal("no notes row")
		}
		a.Drag(src.X+10, src.Y+src.Height/2, dst.X+10, dst.Y+dst.Height/2)
		a.Settle()
		shot(t, a, "explorer-drag")
		if _, ok := textWhere(a, "Move", func(tx app.DevText) bool { return true }); ok {
			a.ClickText("Move")
		}
		waitFile(a, "notes/renamed.txt", true)
	})
}

// TestDesktopExplorerClickKeepsTheFocus: a click or an arrow key on a file in the Explorer shows it as a preview and the tree
// keeps the keyboard focus (VS Code's preserveFocus), so F2, Delete and the arrows still act on the tree; Enter and a double
// click move the focus into the editor. The editor took the focus on every click, so F2 after a click did nothing and what was
// typed went into the file.
func TestDesktopExplorerClickKeepsTheFocus(t *testing.T) {
	a := display.Start(t, display.Options{Args: []string{"."}, Project: explorerProject})
	a.Settle()
	rows := inRect(0, 90, 348, 750)
	clickTextWhere(t, a, "junk.txt", rows)
	a.Eventually("junk.txt to open", func() (bool, string) { return activeTab(a) == "junk.txt", joinNames(tabNames(a)) })
	// a letter typed in the tree starts its find box (as VS Code's), and does not go into the file
	a.Type("zz")
	a.Eventually("the find box of the tree to show zz", func() (bool, string) {
		in := a.Inputs()
		return len(in) == 1 && in[0].Text == "zz", a.Log()
	})
	if got := editorText(a); got != "junk\n" {
		t.Fatalf("a click on the file moved the focus into the editor: the text is %q", got)
	}
	a.Press("Escape")
	a.Eventually("the find box to close", func() (bool, string) { return len(a.Inputs()) == 0, "" })
	// an arrow key moves the focus to the next row (it opens nothing, as in VS Code); F2 renames the row that has the focus
	a.Press("Down")
	a.Settle()
	if got := activeTab(a); got != "junk.txt" {
		t.Errorf("an arrow key opened %q", got)
	}
	a.Press("F2")
	a.Settle() // a person types after the input is up
	a.Type("x")
	a.WaitFocusedText("x.go")
	a.Press("Escape")
	shot(t, a, "explorer-focus-escape")
	// Enter opens the file and the focus goes to the editor
	a.Press("Enter")
	a.Eventually("main.go to open", func() (bool, string) { return activeTab(a) == "main.go", joinNames(tabNames(a)) })
	a.Type("// hi")
	a.Eventually("the text to be typed in the editor", func() (bool, string) {
		return editorText(a) == "// hipackage main\n", fmt.Sprintf("%q", editorText(a))
	})
	shot(t, a, "explorer-focus")
}
