//go:build emetos_display

package main

import (
	"strings"
	"testing"

	"github.com/rm4n0s/emetos/internal/testenv/display"
)

// The diff editor and the merge editor, opened as a user opens them (step 17b.5).

var diffProject = map[string]string{
	"a.txt":        "one\ntwo\nthree\nfour\n",
	"b.txt":        "one\n2\nthree\nfour\nfive\n",
	"conflict.txt": "head\n<<<<<<< HEAD\nours line\n||||||| base\nbase line\n=======\ntheirs line\n>>>>>>> feature\ntail\n",
}

func TestDesktopDiffEditor(t *testing.T) {
	a := display.Start(t, display.Options{Args: []string{"."}, Project: diffProject})
	a.Settle()
	rows := inRect(0, 90, 348, 750)
	// the context menu of a file: Select for Compare, then Compare with Selected on the other
	a.RightClickText("a.txt")
	a.WaitText("Select for Compare")
	shot(t, a, "diff-menu")
	a.ClickText("Select for Compare")
	a.Settle()
	tx, _ := textWhere(a, "b.txt", rows)
	a.RightClick(tx.X+10, tx.Y+tx.Height/2)
	a.WaitText("Compare with Selected")
	a.ClickText("Compare with Selected")
	a.Eventually("the diff editor to open", func() (bool, string) { return strings.Contains(activeTab(a), "a.txt"), joinNames(tabNames(a)) })
	a.Settle()
	shot(t, a, "diff-editor")
	if w := a.Workbench(); len(w.Groups) != 1 || w.Groups[0].Tabs[0].Kind == "emetos.textEditor" {
		t.Errorf("the diff editor is not an editor of its own kind: %+v", w.Groups)
	}
	// edit the modified side (b.txt): click in its text, type, save with Ctrl+S
	a.Click(1000, 150)
	a.Type("// edited\n")
	a.Press("ctrl+s")
	a.Eventually("b.txt to be saved with the edit", func() (bool, string) {
		got := readProjectFile(t, a, "b.txt")
		return strings.Contains(got, "// edited"), got
	})
	shot(t, a, "diff-edited")
	// the next change (Alt+F5) and the inline view toggle do not break the editor
	a.Click(1000, 80) // line 1 of the modified side, before the first change
	a.Press("alt+f5")
	a.Eventually("Alt+F5 to go to the next change", func() (bool, string) {
		ed := a.Editor()
		return ed.Line == 2, itoa(ed.Line) + ":" + itoa(ed.Column)
	})
	shot(t, a, "diff-next-change")
}

func TestDesktopMergeEditor(t *testing.T) {
	a := display.Start(t, display.Options{Args: []string{".", "conflict.txt"}, Project: diffProject})
	a.Settle()
	a.ClickText("Resolve in Merge Editor")
	a.WaitText("Accept Incoming")
	shot(t, a, "merge-editor")
	a.ClickText("Accept Incoming")
	a.WaitText("0 Conflicts Remaining")
	shot(t, a, "merge-accepted")
	a.ClickText("Complete Merge")
	a.Eventually("the merge to be written", func() (bool, string) {
		got := readProjectFile(t, a, "conflict.txt")
		return got == "head\ntheirs line\ntail\n", got
	})
	a.Eventually("the merge editor to close", func() (bool, string) {
		for _, n := range tabNames(a) {
			if strings.HasPrefix(n, "Merging") {
				return false, joinNames(tabNames(a))
			}
		}
		return true, ""
	})
	shot(t, a, "merge-done")
}
