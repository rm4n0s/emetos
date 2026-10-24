package app

import (
	"context"
	"fmt"
	"math"
	"testing"

	"github.com/rm4n0s/glutter/gluttertest"
	"github.com/rm4n0s/glutter/ui"
	"github.com/rm4n0s/glutter/widgets"

	"github.com/rm4n0s/emetos/internal/core/buffer"
	"github.com/rm4n0s/emetos/internal/core/command"
	"github.com/rm4n0s/emetos/internal/core/settings"
	"github.com/rm4n0s/emetos/internal/gui/kit"
	"github.com/rm4n0s/emetos/internal/gui/workbench"
)

// The tests of the zoom of the window (step 8.5, SPEC §10.9).

// zoomApp is a window with an open file, with the settings the test gives it.
func zoomApp(t *testing.T, extra map[string]any) *groupApp {
	t.Helper()
	values := map[string]any{"editor.minimap.enabled": false, "breadcrumbs.enabled": false}
	for k, v := range extra {
		values[k] = v
	}
	win, _ := themedWindow(t, values)
	return newGroupAppWith(t, win, testWindowSize, []string{"a.go"}, nil, "a.go")
}

func TestTheZoomCommands(t *testing.T) {
	cases := []struct {
		name    string
		extra   map[string]any
		run     []string
		want    float64
		written any // window.zoomLevel in the user's settings afterwards; nil when it is not there
	}{
		{name: "zoom in", run: []string{"workbench.action.zoomIn"}, want: 1},
		{name: "zoom in twice", run: []string{"workbench.action.zoomIn", "workbench.action.zoomIn"}, want: 2},
		{name: "zoom out", run: []string{"workbench.action.zoomOut"}, want: -1},
		{name: "in and out", run: []string{"workbench.action.zoomIn", "workbench.action.zoomOut"}, want: 0},
		{name: "reset", run: []string{"workbench.action.zoomIn", "workbench.action.zoomIn", "workbench.action.zoomReset"}, want: 0},
		{name: "the window starts at window.zoomLevel", extra: map[string]any{"window.zoomLevel": 3}, want: 3},
		{name: "a level that is not whole is kept until a command", extra: map[string]any{"window.zoomLevel": 1.5}, want: 1.5},
		{name: "a command makes the level whole", extra: map[string]any{"window.zoomLevel": 1.5}, run: []string{"workbench.action.zoomIn"}, want: 3},
		{name: "reset goes back to window.zoomLevel", extra: map[string]any{"window.zoomLevel": 3}, run: []string{"workbench.action.zoomIn", "workbench.action.zoomReset"}, want: 3},
		{name: "no level above the top", extra: map[string]any{"window.zoomLevel": 8}, run: []string{"workbench.action.zoomIn"}, want: 8},
		{name: "no level below the bottom", extra: map[string]any{"window.zoomLevel": -8}, run: []string{"workbench.action.zoomOut"}, want: -8},
		{name: "the top is reached", extra: map[string]any{"window.zoomLevel": 7}, run: []string{"workbench.action.zoomIn"}, want: 8},
		{name: "a zoom of every window writes the setting", extra: map[string]any{"window.zoomPerWindow": false},
			run: []string{"workbench.action.zoomIn", "workbench.action.zoomIn", "workbench.action.zoomOut"}, want: 1, written: 1.0},
		{name: "a zoom of every window resets to 0", extra: map[string]any{"window.zoomPerWindow": false, "window.zoomLevel": 2},
			run: []string{"workbench.action.zoomReset"}, want: 0, written: 0.0},
		{name: "a zoom of one window writes nothing", run: []string{"workbench.action.zoomIn"}, want: 1},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			a := zoomApp(t, tc.extra)
			for _, id := range tc.run {
				a.run(id)
			}
			if got := a.win.ZoomLevel(); got != tc.want {
				t.Errorf("zoom level %v, want %v", got, tc.want)
			}
			// the setting is written only by a zoom of every window (the file of the test has the level it starts with)
			user := a.win.Settings.Inspect("window.zoomLevel", "").User
			switch {
			case tc.written != nil:
				if !user.Ok || user.Value != tc.written {
					t.Errorf("window.zoomLevel in the user's settings is %v (set: %v), want %v", user.Value, user.Ok, tc.written)
				}
			case tc.extra["window.zoomLevel"] == nil && user.Ok:
				t.Errorf("a zoom of the window wrote window.zoomLevel: %v", user.Value)
			}
			if math.Abs(tc.want) < 5 {
				a.noErrors()
			} else {
				a.ts.TakeErrors() // a window of 300 x 190 logical pixels does not hold the parts at the extreme levels
			}
		})
	}
}

func TestTheZoomLevelStaysInItsRange(t *testing.T) {
	win, _ := themedWindow(t, nil)
	for _, tc := range []struct{ set, want float64 }{{100, 8}, {-100, -8}, {2.5, 2.5}, {math.NaN(), 0}} {
		win.SetZoomLevel(tc.set)
		if got := win.ZoomLevel(); got != tc.want {
			t.Errorf("SetZoomLevel(%v) gives %v, want %v", tc.set, got, tc.want)
		}
	}
	var told int
	stop := win.OnZoomChange(func() { told++ })
	win.SetZoomLevel(2.5)
	win.SetZoomLevel(2.5)
	stop()
	win.SetZoomLevel(4)
	if told != 1 {
		t.Errorf("the listener was told %d times, want 1", told)
	}
}

func TestTheZoomKeys(t *testing.T) {
	cases := []struct {
		keys  string
		start float64
		want  float64
	}{
		{"ctrl+=", 0, 1},
		{"ctrl+shift+=", 0, 1},
		{"ctrl+numpad_add", 0, 1},
		{"ctrl+-", 0, -1},
		{"ctrl+numpad_subtract", 0, -1},
		{"ctrl+numpad0", 3, 0},
	}
	for _, tc := range cases {
		t.Run(tc.keys, func(t *testing.T) {
			a := zoomApp(t, nil)
			a.win.SetZoomLevel(tc.start)
			a.press(tc.keys)
			if got := a.win.ZoomLevel(); got != tc.want {
				t.Errorf("after %s the zoom level is %v, want %v", tc.keys, got, tc.want)
			}
			a.noErrors()
		})
	}
}

func TestTheZoomRowsOfTheViewMenu(t *testing.T) {
	a := zoomApp(t, nil)
	if err := a.win.Commands.Run(command.Context{Ctx: context.Background(), Keys: a.win.Keys}, "dev.openMenu", map[string]any{"id": command.MenubarViewMenu}); err != nil {
		t.Fatal(err)
	}
	a.pump()
	els := a.ts.Elements(gluttertest.ByType[*kit.Menu]())
	if len(els) == 0 {
		t.Fatal("the View menu did not open")
	}
	items := els[0].Widget().(*kit.Menu).Items()
	for id, key := range map[string]string{
		"workbench.action.zoomIn": "Ctrl+=", "workbench.action.zoomOut": "Ctrl+-", "workbench.action.zoomReset": "Ctrl+NumPad0",
		"workbench.action.toggleFullScreen": "F11", "workbench.action.toggleZenMode": "Ctrl+K Z", "workbench.action.toggleCenteredLayout": "",
	} {
		it, ok := menuItem(items, id)
		if !ok {
			t.Errorf("%s is not in the View menu", id)
			continue
		}
		if it.Keybinding != key {
			t.Errorf("%s shows the key %q, want %q", id, it.Keybinding, key)
		}
	}
}

func TestWindowZoomFactors(t *testing.T) {
	cases := []struct {
		level float64
		want  float64
	}{
		{-8, math.Pow(1.2, -8)}, {-1, 1 / 1.2}, {0, 1}, {1, 1.2}, {2, 1.44}, {8, math.Pow(1.2, 8)}, {100, math.Pow(1.2, 8)}, {-100, math.Pow(1.2, -8)}, {math.NaN(), 1},
	}
	for _, tc := range cases {
		if got := settings.ZoomFactor(tc.level); math.Abs(got-tc.want) > 1e-9 {
			t.Errorf("ZoomFactor(%v) = %v, want %v", tc.level, got, tc.want)
		}
	}
}

// partOf finds the widget of a part of the workbench.
func partOf(p workbench.Part) gluttertest.Finder { return gluttertest.ByKey(workbench.PartKey(p)) }

// centerOf is the place the text of a menu title has on the screen.
func centerOf(a *groupApp, text string) ui.Offset { return a.ts.Center(gluttertest.ByText(text)) }

func TestTheLayoutAndTheHitTestingAreScaledByTheZoom(t *testing.T) {
	for _, level := range []float64{2, -2} {
		t.Run(fmt.Sprint("level ", level), func(t *testing.T) {
			a := zoomApp(t, nil)
			factor := settings.ZoomFactor(level)
			before := centerOf(a, "File")
			// where the menu opens at the level 0
			a.ts.TapAt(before)
			a.pump()
			menu0 := a.ts.TopLeft(gluttertest.ByType[*kit.Menu]())
			a.press("escape")
			statusBefore := a.ts.Size(partOf(workbench.PartStatusBar))
			a.win.SetZoomLevel(level)
			a.pump()
			// the layout: the title is `factor` times farther from the corner and the window is laid out in `1/factor` of
			// its size, so the bars are `1/factor` as wide in logical pixels and as wide as the window on the screen
			after := centerOf(a, "File")
			if math.Abs(after.Dx-before.Dx*factor) > 1.5 || math.Abs(after.Dy-before.Dy*factor) > 1.5 {
				t.Errorf("the File title is at %v, want about %v", after, ui.Offset{Dx: before.Dx * factor, Dy: before.Dy * factor})
			}
			status := a.ts.Size(partOf(workbench.PartStatusBar))
			if want := testWindowSize.Width / factor; math.Abs(status.Width-want) > 1e-6 {
				t.Errorf("the status bar is %v wide (it was %v), want %v", status.Width, statusBefore.Width, want)
			}
			if status.Height != workbench.StatusBarHeight {
				t.Errorf("the status bar is %v high: the sizes of the parts are logical pixels, scaled by the zoom", status.Height)
			}
			// the hit test: a tap where the title is painted opens its menu
			a.ts.TapAt(after)
			a.pump()
			if !a.ts.Exists(gluttertest.ByType[*kit.Menu]()) {
				t.Fatal("a tap on the scaled File title did not open the menu")
			}
			// and the menu is where it was, scaled along
			menu := a.ts.TopLeft(gluttertest.ByType[*kit.Menu]())
			if math.Abs(menu.Dx-menu0.Dx*factor) > 1.5 || math.Abs(menu.Dy-menu0.Dy*factor) > 1.5 {
				t.Errorf("the menu is at %v, want about %v (it was at %v)", menu, ui.Offset{Dx: menu0.Dx * factor, Dy: menu0.Dy * factor}, menu0)
			}
			a.noErrors()
		})
	}
}

func TestChangingTheZoomKeepsTheStateOfTheWindow(t *testing.T) {
	a := zoomApp(t, nil)
	e := a.shell().activeEditor()
	e.view.Cursors().SetSelections([]buffer.Selection{sel(0, 2, 1, 3)})
	e.view.SelectionsChanged()
	a.run("workbench.action.toggleSidebarVisibility")
	a.run("workbench.action.togglePanel")
	for _, level := range []float64{2, -3, 0} {
		a.win.SetZoomLevel(level)
		a.pump()
		if a.shell().activeEditor() != e {
			t.Fatalf("the editor was made again at the level %v", level)
		}
		if got := e.view.Cursors().Selections(); len(got) != 1 || got[0] != sel(0, 2, 1, 3) {
			t.Errorf("the selection is %v at the level %v", got, level)
		}
		if st := a.win.Layout.State(); st.SideBarVisible || !st.PanelVisible {
			t.Errorf("the layout changed at the level %v: %+v", level, st)
		}
	}
	a.noErrors()
}

func TestAWindowThatWasZoomedComesBackZoomed(t *testing.T) {
	cases := []struct {
		name  string
		extra map[string]any
		run   []string
		want  float64
		saved bool
	}{
		{name: "a zoom of the window", run: []string{"workbench.action.zoomIn", "workbench.action.zoomIn"}, want: 2, saved: true},
		{name: "a zoom out", run: []string{"workbench.action.zoomOut"}, want: -1, saved: true},
		{name: "the level of the setting is not the window's own", extra: map[string]any{"window.zoomLevel": 2}, want: 2},
		{name: "back to the level of the setting", run: []string{"workbench.action.zoomIn", "workbench.action.zoomReset"}, want: 0},
		{name: "a zoom of every window is the setting's", extra: map[string]any{"window.zoomPerWindow": false}, run: []string{"workbench.action.zoomIn"}, want: 1},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			env := newStateEnv(t, tc.extra)
			one := env.start(env.root)
			for _, id := range tc.run {
				one.command(id, nil)
			}
			one.close() // writes the state
			st, ok := env.savedWindow(env.root)
			if !ok {
				t.Fatal("the state was not written")
			}
			if tc.saved != (st.ZoomLevel != nil) || (st.ZoomLevel != nil && *st.ZoomLevel != tc.want) {
				t.Errorf("the saved zoom level is %v, want %v (saved: %v)", st.ZoomLevel, tc.want, tc.saved)
			}
			two := env.start(env.root)
			if got := two.win.ZoomLevel(); got != tc.want {
				t.Errorf("the next run starts at the zoom level %v, want %v", got, tc.want)
			}
			two.close()
		})
	}
}

func TestAChangeOfWindowZoomLevelResetsEveryWindow(t *testing.T) {
	env := newMultiEnv(t, nil)
	one := env.folder("one", "a.go")
	two := env.folder("two", "b.go")
	a := env.run(Options{Folder: one})
	if _, err := a.m.Open(WindowSpec{Folder: two}); err != nil {
		t.Fatal(err)
	}
	a.settle()
	w1, w2 := a.windowOf(one), a.windowOf(two)
	// the windows have a zoom each
	if err := a.command(w1, "workbench.action.zoomIn", nil); err != nil {
		t.Fatal(err)
	}
	if got := []float64{w1.ZoomLevel(), w2.ZoomLevel()}; got[0] != 1 || got[1] != 0 {
		t.Fatalf("the zoom levels are %v, want [1 0]", got)
	}
	// the setting is the level of all of them
	if err := w1.Settings.Update(a.ctx, "window.zoomLevel", 2, settings.TargetUser); err != nil {
		t.Fatal(err)
	}
	a.settle()
	if got := []float64{w1.ZoomLevel(), w2.ZoomLevel()}; got[0] != 2 || got[1] != 2 {
		t.Errorf("after window.zoomLevel the zoom levels are %v, want [2 2]", got)
	}
	// with window.zoomPerWindow off a zoom of one is a zoom of all
	if err := w1.Settings.Update(a.ctx, "window.zoomPerWindow", false, settings.TargetUser); err != nil {
		t.Fatal(err)
	}
	if err := a.command(w2, "workbench.action.zoomIn", nil); err != nil {
		t.Fatal(err)
	}
	a.settle()
	if got := []float64{w1.ZoomLevel(), w2.ZoomLevel()}; got[0] != 3 || got[1] != 3 {
		t.Errorf("after a zoom of every window the levels are %v, want [3 3]", got)
	}
}

func TestTheLabelOfADraggedTabFollowsThePointerUnderAZoom(t *testing.T) {
	label := gluttertest.ByPredicate("the layout of the label of a drag", func(w widgets.Widget) bool {
		l, ok := w.(*widgets.CustomSingleChildLayout)
		if !ok {
			return false
		}
		_, ok = l.Delegate().(*dragLabelLayout)
		return ok
	})
	for _, level := range []float64{0, 2, -2} {
		t.Run(fmt.Sprint("level ", level), func(t *testing.T) {
			a := twoGroups(t)
			a.win.SetZoomLevel(level)
			a.pump()
			d := a.tabRect(2, "d.go")
			to := ui.Offset{Dx: d.Left + 10, Dy: d.Top + 15}
			g := a.dragTab(1, "c.go", to)
			if !a.ts.Exists(label) {
				t.Fatal("the label of the drag is not shown")
			}
			// the label is below and to the right of the pointer on the screen, at every level: 12 logical pixels
			at := a.ts.TopLeft(gluttertest.Descendant(label, gluttertest.ByType[*widgets.Container]()))
			f := settings.ZoomFactor(level)
			if want := (ui.Offset{Dx: to.Dx + dragLabelOffset*f, Dy: to.Dy + dragLabelOffset*f}); math.Abs(at.Dx-want.Dx) > 1.5 || math.Abs(at.Dy-want.Dy) > 1.5 {
				t.Errorf("the label is at %v, want %v (the pointer is at %v)", at, want, to)
			}
			g.Up()
			a.pump()
			a.noErrors()
		})
	}
}
