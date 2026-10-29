//go:build emetos_display

package main

import (
	"testing"

	"github.com/rm4n0s/emetos/internal/gui/app"
	"github.com/rm4n0s/emetos/internal/testenv/display"
)

// Split groups, dragging a tab, workspace tabs and the panel (step 17b.5).

func groupTabs(a *display.App, group int) []string {
	var out []string
	w := a.Workbench()
	if group < len(w.Groups) {
		for _, tab := range w.Groups[group].Tabs {
			out = append(out, tab.Name)
		}
	}
	return out
}

func sameNames(got []string, want ...string) bool {
	if len(got) != len(want) {
		return false
	}
	for i := range got {
		if got[i] != want[i] {
			return false
		}
	}
	return true
}

func TestDesktopSplitGroupsAndTabDrag(t *testing.T) {
	a := display.Start(t, display.Options{Args: []string{".", "a.txt"}, Project: diffProject})
	a.Settle()
	a.Press("ctrl+backslash")
	a.Eventually("a second group", func() (bool, string) { return len(a.Workbench().Groups) == 2, joinNames(tabNames(a)) })
	a.Settle()
	shot(t, a, "split")
	// a file opens in the active group, which is the new one
	clickTextWhere(t, a, "b.txt", inRect(0, 90, 348, 750))
	a.Eventually("b.txt in the second group", func() (bool, string) {
		return sameNames(groupTabs(a, 1), "a.txt", "b.txt") || sameNames(groupTabs(a, 1), "b.txt"), joinNames(groupTabs(a, 0)) + " | " + joinNames(groupTabs(a, 1))
	})
	// keep it, so the next open does not replace it
	a.DoubleClick(func() (float64, float64) {
		tx, _ := textWhere(a, "b.txt", inRect(0, 90, 348, 750))
		return tx.X + 10, tx.Y + 9
	}())
	a.Settle()
	shot(t, a, "split-opened")
	if g := groupTabs(a, 1); !contains(g, "b.txt") {
		t.Fatalf("the tabs of the groups: %v | %v", groupTabs(a, 0), g)
	}
	// drag the tab b.txt of the second group onto the tab strip of the first
	tab, ok := textWhere(a, "b.txt", inRect(814, 36, 1280, 70))
	if !ok {
		t.Fatalf("no tab b.txt in the second group: %s", a.Dump())
	}
	a.Drag(tab.X+10, tab.Y+9, 600, 52)
	a.Eventually("b.txt to move to the first group", func() (bool, string) {
		return contains(groupTabs(a, 0), "b.txt") && !contains(groupTabs(a, 1), "b.txt"), joinNames(groupTabs(a, 0)) + " | " + joinNames(groupTabs(a, 1))
	})
	shot(t, a, "tab-dragged")
	// drag the tab b.txt before a.txt in the same group: the order changes
	b, _ := textWhere(a, "b.txt", inRect(348, 36, 814, 70))
	first, _ := textWhere(a, "a.txt", inRect(348, 36, 814, 70))
	a.Drag(b.X+10, b.Y+9, first.X+4, first.Y+9)
	a.Eventually("b.txt to come first", func() (bool, string) {
		return len(groupTabs(a, 0)) == 2 && groupTabs(a, 0)[0] == "b.txt", joinNames(groupTabs(a, 0))
	})
	shot(t, a, "tab-reordered")
	// drag the tab to the edge of the editor area: it splits there
	tab, _ = textWhere(a, "a.txt", inRect(348, 36, 814, 70))
	a.Drag(tab.X+10, tab.Y+9, 600, 500)
	a.Settle()
	shot(t, a, "tab-dropped-in-editor")
}

func TestDesktopWorkspaceTabs(t *testing.T) {
	a := display.Start(t, display.Options{Args: []string{".", "a.txt"}, Project: diffProject})
	a.Settle()
	if n := len(a.Workbench().WindowTabs); n != 1 {
		t.Fatalf("%d window tabs at the start", n)
	}
	// the plus button opens the menu of the kinds of tabs
	a.Click(186, 18)
	a.WaitText("Remote Work...")
	shot(t, a, "wtabs-menu")
	a.ClickText("Remote Work...")
	a.Eventually("a second window tab", func() (bool, string) {
		w := a.Workbench().WindowTabs
		return len(w) == 2 && w[1].Active, a.Dump()
	})
	shot(t, a, "wtabs-remotework")
	// the first tab again: its folder, its editor
	a.Click(100, 18)
	a.Eventually("the first tab active", func() (bool, string) {
		w := a.Workbench().WindowTabs
		return len(w) == 2 && w[0].Active, a.Dump()
	})
	a.Settle()
	if got := activeTab(a); got != "a.txt" {
		t.Errorf("back in the first tab the editor shows %q", got)
	}
	// the keys of the strip: Ctrl+PageDown goes to the next tab
	a.Press("ctrl+alt+pagedown")
	a.Eventually("the next tab with the keys", func() (bool, string) { return a.Workbench().WindowTabs[1].Active, "" })
	shot(t, a, "wtabs-keys")
	// close the Remote Work tab with its button
	var closes []app.DevText
	for _, tx := range a.Texts() {
		if tx.Text == "\uea76" && tx.Y < 36 && tx.X > 100 && tx.X < 400 {
			closes = append(closes, tx)
		}
	}
	if len(closes) != 2 {
		t.Fatalf("%d close buttons in the strip, want 2: %s", len(closes), a.Dump())
	}
	last := closes[0]
	for _, c := range closes {
		if c.X > last.X {
			last = c
		}
	}
	a.Click(last.X+8, last.Y+8)
	a.Eventually("the Remote Work tab to close", func() (bool, string) {
		w := a.Workbench().WindowTabs
		return len(w) == 1 && w[0].Active, a.Dump()
	})
	shot(t, a, "wtabs-closed")
}

func TestDesktopPanelOutput(t *testing.T) {
	a := display.Start(t, display.Options{Args: []string{".", "a.txt"}, Project: diffProject})
	a.Settle()
	a.Dev("dev.log", map[string]any{"channel": "Emetos", "message": "a line for the Output panel"})
	a.Press("ctrl+k", "ctrl+h")
	a.Eventually("the Output panel", func() (bool, string) {
		w := a.Workbench()
		return w.PanelVisible && w.ActivePanel == "workbench.panel.output", w.ActivePanel
	})
	if w := a.Workbench(); !w.PanelVisible || w.ActivePanel != "workbench.panel.output" {
		t.Errorf("the panel: visible %v, %q", w.PanelVisible, w.ActivePanel)
	}
	shot(t, a, "output")
	// the panel closes with Ctrl+J and comes back; Problems switches with its key
	a.Press("ctrl+j")
	a.Eventually("the panel to close", func() (bool, string) { return !a.Workbench().PanelVisible, "" })
	a.Press("ctrl+shift+m")
	a.Eventually("the Problems panel", func() (bool, string) {
		w := a.Workbench()
		return w.PanelVisible && w.ActivePanel == "workbench.panel.markers", w.ActivePanel
	})
	shot(t, a, "panel-problems-empty")
}
