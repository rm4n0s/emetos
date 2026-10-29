package ui_test

import (
	"testing"
	"time"

	"github.com/rm4n0s/glutter/foundation"
	"github.com/rm4n0s/glutter/gluttertest"
	"github.com/rm4n0s/glutter/painting"
	"github.com/rm4n0s/glutter/services"
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
		ui.NewToggle("regex", ui.ToggleOpts{Tooltip: "Use Regular Expression", OnChanged: func(on bool) {
			if on {
				pressed += 100
			}
		}}),
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
	ts.Tap(gluttertest.ByType[*ui.Toggle]())
	if pressed != 101 {
		t.Errorf("after the toggle was tapped, pressed = %d", pressed)
	}
}

func TestAScrollViewScrollsAndTheHandleMeasuresIt(t *testing.T) {
	rows := make([]widgets.Widget, 40)
	for i := range rows {
		rows[i] = widgets.NewSizedBox(widgets.NewText("row"), widgets.SizedBoxOpts{Height: foundation.Some(20.0)})
	}
	var handle ui.ScrollHandle
	if _, max := handle.MaxPosition(); max != 0 {
		t.Errorf("a handle no view has measures %v", max)
	}
	ts := uitest.Pump(t, uitest.Themes()[0], gui.Size{Width: 200, Height: 200}, ui.NewScrollView(widgets.NewColumn(rows), &handle))
	ts.Pump()
	_, max := handle.MaxPosition()
	if max != 40*20-200 {
		t.Fatalf("MaxPosition = %v, want %d", max, 40*20-200)
	}
	handle.ScrollTo(0, 1e9) // past the end: clamped to it
	if _, top := handle.Position(); top != max {
		t.Errorf("top = %v, want %v", top, max)
	}
}

// TestAScrollableFollowsTheControllersOfItsViews holds NewScrollable: the scrollbars of the element follow Glutter scroll views that
// use its controllers, and a list of rows made by a builder builds only the rows in view.
func TestAScrollableFollowsTheControllersOfItsViews(t *testing.T) {
	vertical, horizontal := widgets.NewScrollController(), widgets.NewScrollController()
	built := map[int]bool{}
	sliver := widgets.NewSliverFixedExtentListBuilder(func(_ widgets.BuildContext, i int) widgets.Widget {
		built[i] = true
		return widgets.NewSizedBox(widgets.NewText("row"), widgets.SizedBoxOpts{Height: foundation.Some(20.0)})
	}, 20, widgets.SliverBuilderOpts{ItemCount: foundation.Some(10_000)})
	list := widgets.NewCustomScrollView([]widgets.Widget{sliver}, widgets.ScrollViewOpts{Controller: vertical})
	wide := widgets.NewSingleChildScrollView(widgets.NewSizedBox(list, widgets.SizedBoxOpts{Width: foundation.Some(600.0)}),
		widgets.SingleChildScrollViewOpts{ScrollDirection: painting.AxisHorizontal, Controller: horizontal})
	ts := uitest.Pump(t, uitest.Themes()[0], gui.Size{Width: 300, Height: 200}, ui.NewScrollable(wide, ui.ScrollableOpts{Controller: vertical, HorizontalController: horizontal}))
	ts.Pump()
	if len(built) == 0 || len(built) > 30 {
		t.Errorf("%d of 10,000 rows were built for a view of 10", len(built))
	}
	ts.ScrollAt(gui.Offset{Dx: 100, Dy: 100}, gui.Offset{Dy: 530})
	ts.Pump(200 * time.Millisecond)
	if vertical.Offset() <= 0 {
		t.Error("the wheel did not scroll the rows")
	}
	if got := horizontal.Position().MaxScrollExtent(); got != 300 {
		t.Errorf("the content is 300 px wider than the view, MaxScrollExtent = %v", got)
	}
}

func TestADropdownChoosesAnOption(t *testing.T) {
	chosen := -1
	d := ui.NewDropdown([]ui.DropdownOption{{Label: "Manual"}, {Label: "Plan"}}, ui.DropdownOpts{Selected: 0, OnChanged: func(i int) { chosen = i }})
	ts := uitest.Pump(t, uitest.Themes()[0], gui.Size{Width: 300, Height: 200}, d)
	if !ts.Exists(gluttertest.ByText("Manual")) {
		t.Fatal("the selected option is not shown")
	}
	ts.Tap(gluttertest.ByType[*ui.Dropdown]())
	ts.Pump(time.Millisecond)
	if !ts.Exists(gluttertest.ByText("Plan")) {
		t.Fatal("the list did not open")
	}
	ts.SendKeyEvent(services.LogicalKeyboardKeyArrowDown)
	ts.SendKeyEvent(services.LogicalKeyboardKeyEnter)
	ts.Pump(time.Millisecond)
	if chosen != 1 {
		t.Errorf("chosen = %d", chosen)
	}
}

func TestTheSuggestListShowsItsRowsAndTapsAccept(t *testing.T) {
	th := uitest.Themes()[0]
	accepted, toggled, scrolled := -1, 0, 0
	rows := []ui.SuggestRow{
		{Label: "checkout", Positions: []int{0, 1, 2}, LabelDescription: "Switch branches", Icon: "symbol-method", IconColor: "method", CanExpand: true},
		{Label: "cherry-pick", Positions: []int{0, 1, 2}, LabelDetail: " <commit>", Kind: 2},
		{Label: "archive", Kind: 17},
	}
	opts := ui.SuggestListOpts{
		Rows: rows, Focus: 0, Total: 3, Width: ui.SuggestWidth, RowHeight: 20, FontSize: 13, ShowIcons: true, ShowInlineDetails: true,
		OnAccept: func(i int) { accepted = i }, OnToggleDetails: func() { toggled++ }, OnScroll: func(n int) { scrolled += n },
	}
	body := widgets.NewStack([]widgets.Widget{
		widgets.NewBuilder(func(bc widgets.BuildContext) widgets.Widget { return ui.NewSuggestList(bc, opts) }),
	}, widgets.StackOpts{})
	ts := uitest.Pump(t, th, gui.Size{Width: 500, Height: 200}, body)
	for _, text := range []string{"Switch branches"} {
		if !ts.Exists(gluttertest.ByText(text)) {
			t.Errorf("%q is not drawn", text)
		}
	}
	// the rows are rich text: tap the third by its place
	ts.TapAt(gui.Offset{Dx: 100, Dy: 1 + 2*20 + 10})
	if accepted != 2 {
		t.Errorf("tapping the third row accepted %d", accepted)
	}
	ts.TapAt(gui.Offset{Dx: 100, Dy: 10})
	if accepted != 0 {
		t.Errorf("tapping the first row accepted %d", accepted)
	}
	// a message has the box and its text
	msgTs := uitest.Pump(t, th, gui.Size{Width: 500, Height: 100}, widgets.NewStack([]widgets.Widget{
		widgets.NewBuilder(func(bc widgets.BuildContext) widgets.Widget { return ui.NewSuggestMessage(bc, "No suggestions.", opts) }),
	}, widgets.StackOpts{}))
	if !msgTs.Exists(gluttertest.ByText("No suggestions.")) {
		t.Errorf("the message is not drawn")
	}
	_ = toggled
	_ = scrolled
}

func TestTheSuggestMatcherScoresAndKeepsItsTables(t *testing.T) {
	var m ui.SuggestMatcher
	if _, pos, ok := m.Match("che", "checkout"); !ok || len(pos) != 3 {
		t.Errorf("che matches checkout with positions %v, %v", pos, ok)
	}
	if _, _, ok := m.Match("zzz", "checkout"); ok {
		t.Errorf("zzz matches checkout")
	}
	if n := testing.AllocsPerRun(20, func() { m.Match("che", "checkout") }); n > 10 {
		t.Errorf("a match allocates %v times", n)
	}
}
