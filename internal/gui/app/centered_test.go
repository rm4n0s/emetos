package app

import (
	"math"
	"testing"

	"github.com/rm4n0s/glutter/gluttertest"

	"github.com/rm4n0s/emetos/internal/core/editorgroup"
	"github.com/rm4n0s/emetos/internal/core/settings"
	"github.com/rm4n0s/emetos/internal/gui/kit"
	"github.com/rm4n0s/emetos/internal/gui/workbench"
)

// The tests of the centered layout (step 8.5, SPEC §10.9).

func TestCenteredMargins(t *testing.T) {
	cases := []struct {
		name        string
		width       float64
		fixed       bool
		left, right float64
	}{
		{"the golden ratio of a wide window", 1000, false, 190.9, 190.9},
		{"the ratio of a narrower one", 600, false, 114.54, 114.54},
		{"a room for the least editor only", 250, false, 15, 15},
		{"the editor keeps its least width", 300, false, 40, 40},
		{"too narrow for any margin", 200, false, 0, 0},
		{"the fixed width of 900 in a wide window", 1100, true, 100, 100},
		{"an odd room is shared with the larger half on the right", 1101, true, 100, 101},
		{"the fixed width in a window that is not wider", 900, true, 60, 60},
		{"the fixed width in a narrow window", 500, true, 60, 60},
		{"nothing to center in no room", 0, false, 0, 0},
		{"or in a room that is not a number", math.NaN(), false, 0, 0},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			l, r := centeredMargins(tc.width, tc.fixed)
			if math.Abs(l-tc.left) > 0.01 || math.Abs(r-tc.right) > 0.01 {
				t.Errorf("centeredMargins(%v, %v) = %v, %v; want %v, %v", tc.width, tc.fixed, l, r, tc.left, tc.right)
			}
		})
	}
}

func TestTheCenteredLayoutShowsWithOneColumnOfGroups(t *testing.T) {
	one := editorgroup.Layout{Orientation: editorgroup.Horizontal, Groups: []editorgroup.LayoutNode{{}}}
	columns := editorgroup.Layout{Orientation: editorgroup.Horizontal, Groups: []editorgroup.LayoutNode{{}, {}}}
	rows := editorgroup.Layout{Orientation: editorgroup.Vertical, Groups: []editorgroup.LayoutNode{{}, {}}}
	rowsOfColumns := editorgroup.Layout{Orientation: editorgroup.Vertical, Groups: []editorgroup.LayoutNode{{Groups: []editorgroup.LayoutNode{{}, {}}}, {}}}
	cases := []struct {
		name                   string
		requested, auto, maxed bool
		layout                 editorgroup.Layout
		want                   bool
	}{
		{"not asked for", false, true, false, one, false},
		{"one group", true, true, false, one, true},
		{"two columns", true, true, false, columns, false},
		{"two columns, auto resize off", true, false, false, columns, true},
		{"two columns, one maximized", true, true, true, columns, true},
		{"two rows are one column", true, true, false, rows, true},
		{"a column split in two", true, true, false, rowsOfColumns, false},
	}
	for _, tc := range cases {
		if got := centeredActive(tc.requested, tc.auto, tc.layout, tc.maxed); got != tc.want {
			t.Errorf("%s: %v, want %v", tc.name, got, tc.want)
		}
	}
}

// groupWidth is the width of the first group on the screen and areaWidth the editor area's.
func (a *groupApp) groupWidth() float64 {
	return a.ts.Size(gluttertest.First(gluttertest.ByType[*kit.Tabs]())).Width
}

// groupLeft is how far the first group is from the left edge of the editor area.
func (a *groupApp) groupLeft() float64 {
	return a.ts.TopLeft(gluttertest.First(gluttertest.ByType[*kit.Tabs]())).Dx - a.ts.TopLeft(partOf(workbench.PartEditor)).Dx
}

func (a *groupApp) areaWidth() float64 { return a.ts.Size(partOf(workbench.PartEditor)).Width }

func TestTheCenteredLayoutMarginsOfTheWindow(t *testing.T) {
	a := zoomApp(t, map[string]any{"files.hotExit": "off"})
	full := a.areaWidth()
	if got := a.groupWidth(); got != full {
		t.Fatalf("the group is %v wide in an area of %v before the centered layout", got, full)
	}
	a.run("workbench.action.toggleCenteredLayout")
	if !a.win.Layout.CenteredLayout() || a.win.Keys.Get("isCenteredLayout") != true {
		t.Fatal("the centered layout is not on")
	}
	l, r := centeredMargins(full, false)
	if got := a.groupWidth(); math.Abs(got-(full-l-r)) > 0.01 {
		t.Errorf("the group is %v wide, want %v", got, full-l-r)
	}
	// two groups side by side: the margins go, the layout stays asked for
	a.run("workbench.action.splitEditorRight")
	if got := a.groupWidth(); got >= full-l-r+1 && got > full/2 {
		t.Errorf("the first of two groups is %v wide: the margins stayed", got)
	}
	if !a.win.Layout.CenteredLayout() {
		t.Error("the request for the centered layout was lost with the second group")
	}
	if got := a.groupLeft(); got > 1 {
		t.Errorf("the first group starts at %v: the margins stayed", got)
	}
	// ... and they come back with one group again
	a.run("workbench.action.closeActiveEditor") // the second group is empty and closes
	if a.shell().group.grid.Count() != 1 {
		t.Fatalf("%d groups", a.shell().group.grid.Count())
	}
	if got := a.groupWidth(); math.Abs(got-(full-l-r)) > 0.01 {
		t.Errorf("with one group again it is %v wide, want %v", got, full-l-r)
	}
	a.run("workbench.action.toggleCenteredLayout")
	if got := a.groupWidth(); got != full {
		t.Errorf("after turning it off the group is %v wide", got)
	}
	a.noErrors()
}

func TestTheCenteredLayoutSettings(t *testing.T) {
	t.Run("a fixed width", func(t *testing.T) {
		a := zoomApp(t, map[string]any{"workbench.editor.centeredLayoutFixedWidth": true})
		a.run("workbench.action.toggleCenteredLayout")
		full := a.areaWidth()
		l, r := centeredMargins(full, true)
		if got := a.groupWidth(); math.Abs(got-(full-l-r)) > 0.01 || full-l-r > 900.01 {
			t.Errorf("the group is %v wide in %v, want %v", got, full, full-l-r)
		}
	})
	t.Run("auto resize off keeps the margins with two groups", func(t *testing.T) {
		a := zoomApp(t, map[string]any{"workbench.editor.centeredLayoutAutoResize": false})
		a.run("workbench.action.toggleCenteredLayout")
		a.run("workbench.action.splitEditorRight")
		full := a.areaWidth()
		l, _ := centeredMargins(full, false)
		if got := a.groupLeft(); math.Abs(got-l) > 1 {
			t.Errorf("the groups start at %v, want %v", got, l)
		}
	})
	t.Run("the settings are followed while it shows", func(t *testing.T) {
		a := zoomApp(t, nil)
		a.run("workbench.action.toggleCenteredLayout")
		if err := a.win.Settings.Update(a.win.ctx, "workbench.editor.centeredLayoutFixedWidth", true, settings.TargetUser); err != nil {
			t.Fatal(err)
		}
		a.pump()
		full := a.areaWidth()
		l, r := centeredMargins(full, true)
		if got := a.groupWidth(); math.Abs(got-(full-l-r)) > 0.01 {
			t.Errorf("the group is %v wide, want %v", got, full-l-r)
		}
	})
}

func TestTheCenteredLayoutComesBackAfterARestart(t *testing.T) {
	env := newStateEnv(t, nil)
	one := env.start(env.root)
	one.command("workbench.action.toggleCenteredLayout", nil)
	one.close()
	st, ok := env.savedWindow(env.root)
	if !ok || string(st.Layout) == "" {
		t.Fatal("the state was not written")
	}
	two := env.start(env.root)
	if !two.win.Layout.CenteredLayout() || two.win.Keys.Get("isCenteredLayout") != true {
		t.Error("the centered layout did not come back")
	}
	two.close()
}
