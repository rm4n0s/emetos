package ui_test

import (
	"testing"

	"github.com/rm4n0s/glutter/foundation"
	"github.com/rm4n0s/glutter/gluttertest"
	gui "github.com/rm4n0s/glutter/ui"
	"github.com/rm4n0s/glutter/widgets"

	"github.com/rm4n0s/emetos/extension/ui"
	"github.com/rm4n0s/emetos/extension/ui/uitest"
)

func TestCodiconsAreFoundByName(t *testing.T) {
	for name, want := range map[string]bool{"checklist": true, "play": true, "debug-stop": true, "no-such-icon": false, "": false} {
		if code, ok := ui.CodiconCode(name); ok != want || (ok && code == 0) {
			t.Errorf("CodiconCode(%q) = %d, %v", name, code, ok)
		}
	}
}

func TestTheWidgetsAreTheKitsAndUseTheTheme(t *testing.T) {
	th := uitest.Themes()[0]
	var seen ui.Colors
	pressed := 0
	list := ui.NewListViewModel()
	if err := list.SetItems([]ui.ListViewItem{{Key: "a", Data: "alpha"}, {Key: "b", Data: "beta"}}); err != nil {
		t.Fatal(err)
	}
	tree := ui.NewTreeModel()
	if err := tree.SetItems([]ui.TreeItem{{Key: "root", Data: "root", Children: []ui.TreeItem{{Key: "leaf", Data: "leaf"}}}}); err != nil {
		t.Fatal(err)
	}
	body := widgets.NewColumn([]widgets.Widget{
		widgets.NewBuilder(func(bc widgets.BuildContext) widgets.Widget {
			seen = ui.Theme(bc)
			return ui.Icon("checklist", 16, seen.Foreground)
		}),
		ui.Icon("no-such-icon", 16, gui.NewColor(0xFFFFFFFF)),
		ui.NewButton("Press me", ui.ButtonOpts{OnPressed: func() { pressed++ }}),
		ui.NewIconButton("play", ui.IconButtonOpts{Tooltip: "Play", OnPressed: func() { pressed += 10 }}),
		ui.NewBadge(7),
		ui.NewCheckbox("Check"),
		widgets.NewSizedBox(ui.NewList(list), widgets.SizedBoxOpts{Height: foundation.Some(60.0)}),
		widgets.NewSizedBox(ui.NewTree(tree), widgets.SizedBoxOpts{Height: foundation.Some(60.0)}),
	})
	ts := uitest.Pump(t, th, gui.Size{Width: 300, Height: 400}, body)
	if seen != th.Colors {
		t.Error("Theme did not give the colors of the window")
	}
	for _, text := range []string{"Press me", "7", "Check", "alpha", "beta", "root"} {
		if !ts.Exists(gluttertest.ByText(text)) {
			t.Errorf("%q is not shown", text)
		}
	}
	ts.Tap(gluttertest.ByText("Press me"))
	if pressed != 1 {
		t.Errorf("the button was pressed %d times", pressed)
	}
}

func TestNewBuilderFailsTheBoxAboveAndOnlyThere(t *testing.T) {
	th := uitest.Themes()[0]
	var failed []any
	guarded := widgets.NewColumn([]widgets.Widget{
		ui.NewBuilder(func(widgets.BuildContext) widgets.Widget { return widgets.NewText("fine") }),
		ui.NewBuilder(func(widgets.BuildContext) widgets.Widget { panic("deep") }),
		widgets.NewText("after"),
	})
	scope := ui.NewBoxScope("v.id", func(r any) { failed = append(failed, r) }, guarded)
	if scope.ViewID() != "v.id" || scope.UpdateShouldNotify(nil) {
		t.Error("the scope has the wrong id or notifies")
	}
	ts := uitest.Pump(t, th, gui.Size{Width: 200, Height: 100}, scope)
	if len(failed) != 1 || failed[0] != "deep" {
		t.Errorf("failed = %v, want the one panic", failed)
	}
	if !ts.Exists(gluttertest.ByText("fine")) || !ts.Exists(gluttertest.ByText("after")) {
		t.Error("the widgets around the failure are gone")
	}
}

func TestNewBuilderOutsideABoxBuildsNormally(t *testing.T) {
	ts := uitest.Pump(t, uitest.Themes()[0], gui.Size{Width: 200, Height: 50}, ui.NewBuilder(func(widgets.BuildContext) widgets.Widget {
		return widgets.NewText("plain")
	}))
	if !ts.Exists(gluttertest.ByText("plain")) {
		t.Error("the builder shows nothing")
	}
}
