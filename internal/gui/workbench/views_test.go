package workbench

import (
	"reflect"
	"testing"
	"time"

	"github.com/rm4n0s/glutter/foundation"
	"github.com/rm4n0s/glutter/gluttertest"
	"github.com/rm4n0s/glutter/services"
	"github.com/rm4n0s/glutter/widgets"

	"github.com/rm4n0s/emetos/internal/core/command"
	"github.com/rm4n0s/emetos/internal/core/contextkey"
	"github.com/rm4n0s/emetos/internal/gui/kit"
	"github.com/rm4n0s/emetos/internal/gui/kit/fonts"
)

// testViewRegistry has three containers of the side bar and a container of the panel.
func testViewRegistry() *command.ViewRegistry {
	r := command.NewViewRegistry()
	r.MustRegisterContainer(command.ViewContainer{ID: "explorer", Title: "Explorer", Icon: fonts.CodiconFiles, Order: 0})
	r.MustRegisterContainer(command.ViewContainer{ID: "search", Title: "Search", Icon: fonts.CodiconSearch, Order: 1})
	r.MustRegisterContainer(command.ViewContainer{ID: "scm", Title: "Source Control", Icon: fonts.CodiconSourceControl, Order: 2})
	r.MustRegisterContainer(command.ViewContainer{ID: "problems", Title: "Problems", Order: 0, Location: command.ViewLocationPanel})
	r.MustRegisterViews(
		command.View{ID: "folders", Name: "Folders", Container: "explorer", Order: 1},
		command.View{ID: "outline", Name: "Outline", Container: "explorer", Order: 2, Collapsed: true},
		command.View{ID: "timeline", Name: "Timeline", Container: "explorer", Order: 3, Collapsed: true},
		command.View{ID: "hidden", Name: "Hidden", Container: "explorer", Order: 4, When: contextkey.Has("never")},
		command.View{ID: "results", Name: "Results", Container: "search"},
	)
	return r
}

func TestViewsModel(t *testing.T) {
	layout := NewLayout(DefaultLayoutState())
	v := NewViews(testViewRegistry(), layout)
	changes := 0
	stop := v.OnChange(func() { changes++ })

	if v.Active() != "explorer" {
		t.Fatalf("the first container of the activity bar is active: %q", v.Active())
	}
	if c, ok := v.ActiveContainer(); !ok || c.Title != "Explorer" {
		t.Errorf("active container %+v %v", c, ok)
	}

	steps := []struct {
		name        string
		do          func()
		active      string
		sideBar     bool
		wantChanges int
	}{
		{"opening another container shows it", func() { v.Open("search") }, "search", true, 1},
		{"opening the one that is open changes nothing", func() { v.Open("search") }, "search", true, 1},
		{"a click on the active one hides the side bar", func() { v.Toggle("search") }, "search", false, 1},
		{"a click on it again shows the side bar", func() { v.Toggle("search") }, "search", true, 1},
		{"a click on another one switches", func() { v.Toggle("scm") }, "scm", true, 2},
		{"a container of the panel is not for the side bar", func() { v.Open("problems") }, "scm", true, 2},
		{"an unknown container does nothing", func() { v.Open("nope") }, "scm", true, 2},
		{"a click on another one while the side bar is hidden shows it with that one", func() {
			layout.SetSideBarVisible(false)
			v.Toggle("explorer")
		}, "explorer", true, 3},
	}
	for _, s := range steps {
		s.do()
		if v.Active() != s.active || layout.State().SideBarVisible != s.sideBar || changes != s.wantChanges {
			t.Errorf("%s: active %q, side bar %v, %d changes; want %q, %v, %d", s.name, v.Active(), layout.State().SideBarVisible, changes, s.active, s.sideBar, s.wantChanges)
		}
	}

	// views start as their descriptor says, and the user's choice wins
	cases := []struct {
		view      string
		collapsed bool
	}{{"folders", false}, {"outline", true}, {"nope", false}}
	for _, tc := range cases {
		if got := v.IsCollapsed(tc.view); got != tc.collapsed {
			t.Errorf("IsCollapsed(%s) = %v", tc.view, got)
		}
	}
	before := changes
	v.SetCollapsed("folders", false) // already so
	v.SetCollapsed("folders", true)
	v.SetCollapsed("outline", false)
	if !v.IsCollapsed("folders") || v.IsCollapsed("outline") || changes != before+2 {
		t.Errorf("folders %v, outline %v, %d changes", v.IsCollapsed("folders"), v.IsCollapsed("outline"), changes-before)
	}
	stop()
	v.Open("search")
	if changes != before+2 {
		t.Error("a listener that was stopped is still told")
	}
}

// viewsHarness is a workbench with the views of testViewRegistry and the Manage menu.
func viewsHarness(t *testing.T, tune func(*Options)) (*harness, *Views) {
	t.Helper()
	var views *Views
	h := newHarness(t, testSize, kit.DarkModern(), DefaultLayoutState(), nil, func(o *Options) {
		views = NewViews(testViewRegistry(), o.Layout)
		o.Views, o.ViewContext = views, o.Keys
		if tune != nil {
			tune(o)
		}
	})
	return h, views
}

func (h *harness) tapKey(key string) {
	h.t.Helper()
	h.ts.Tap(gluttertest.ByKey(ActivityItemKey(key)))
	h.settle()
}

func (h *harness) active(id string) bool {
	h.t.Helper()
	el := h.ts.Element(gluttertest.ByKey(ActivityItemKey(id)))
	return el.Widget().(*activityItem).active
}

func TestActivityBarShowsTheContainersInOrder(t *testing.T) {
	h, _ := viewsHarness(t, nil)
	var tops []float64
	for _, id := range []string{"explorer", "search", "scm"} {
		f := gluttertest.ByKey(ActivityItemKey(id))
		if !h.ts.Exists(f) {
			t.Fatalf("no icon for %s", id)
		}
		if size := h.ts.Size(f); size.Width != 48 || size.Height != 48 {
			t.Errorf("%s is %v, want 48x48", id, size)
		}
		tops = append(tops, h.ts.TopLeft(f).Dy)
	}
	if !reflect.DeepEqual(tops, []float64{30, 78, 126}) {
		t.Errorf("the icons are at %v, one under the other from the menu bar", tops)
	}
	if h.ts.Exists(gluttertest.ByKey(ActivityItemKey("problems"))) {
		t.Error("a container of the panel is in the activity bar")
	}
	if !h.active("explorer") || h.active("search") {
		t.Error("the container the side bar shows is the active one")
	}
	noErrors(t, h)
}

func TestActivityBarClicksOpenAndToggleTheSideBar(t *testing.T) {
	h, views := viewsHarness(t, nil)
	if !h.ts.Exists(gluttertest.ByText("EXPLORER")) {
		t.Fatal("the side bar is not titled with the container")
	}
	// a click on the active icon hides the side bar, and none is active then
	h.tapKey("explorer")
	if h.layout.State().SideBarVisible || h.shown(PartSideBar) || h.active("explorer") {
		t.Errorf("side bar visible %v, shown %v, explorer active %v", h.layout.State().SideBarVisible, h.shown(PartSideBar), h.active("explorer"))
	}
	if h.key("sideBarVisible") != false {
		t.Errorf("sideBarVisible = %v", h.key("sideBarVisible"))
	}
	// a click on another icon shows it with that container
	h.tapKey("search")
	if !h.layout.State().SideBarVisible || views.Active() != "search" || !h.active("search") || h.active("explorer") {
		t.Errorf("visible %v, active %q", h.layout.State().SideBarVisible, views.Active())
	}
	if !h.ts.Exists(gluttertest.ByText("SEARCH")) || h.ts.Exists(gluttertest.ByText("EXPLORER")) {
		t.Error("the title of the side bar did not change")
	}
	if !h.ts.Exists(gluttertest.ByText("RESULTS")) || h.ts.Exists(gluttertest.ByText("FOLDERS")) {
		t.Error("the side bar did not show the views of the container")
	}
	// the command that hides the side bar leaves the icon inactive, and the icon brings it back
	h.run("workbench.action.toggleSidebarVisibility")
	if h.active("search") {
		t.Error("the icon is active under a hidden side bar")
	}
	h.tapKey("search")
	if !h.layout.State().SideBarVisible || !h.active("search") {
		t.Error("a click on the icon of the container did not bring the side bar back")
	}
	// the keyboard: the icons take the focus and Enter opens
	views.Open("explorer")
	h.settle()
	scm := h.ts.Element(gluttertest.Last(gluttertest.Descendant(gluttertest.ByKey(ActivityItemKey("scm")), gluttertest.ByType[*widgets.MouseRegion]())))
	widgets.FocusOf(scm).RequestFocus()
	h.settle()
	h.ts.SendKeyEvent(services.LogicalKeyboardKeyEnter)
	h.settle()
	if views.Active() != "scm" {
		t.Errorf("Enter on the icon of scm: active %q", views.Active())
	}
	noErrors(t, h)
}

func TestActivityBarIconsHaveTooltips(t *testing.T) {
	h, _ := viewsHarness(t, nil)
	h.ts.Hover(h.ts.Center(gluttertest.ByKey(ActivityItemKey("scm"))))
	h.ts.Pump(2 * time.Second)
	if !h.ts.Exists(gluttertest.ByText("Source Control")) {
		t.Error("the tooltip of the icon does not show")
	}
	noErrors(t, h)
}

func TestViewHeadersCollapseAndExpand(t *testing.T) {
	h, views := viewsHarness(t, nil)
	headers := []string{"FOLDERS", "OUTLINE", "TIMELINE"}
	for _, text := range headers {
		if !h.ts.Exists(gluttertest.ByText(text)) {
			t.Errorf("no header %s", text)
		}
	}
	if h.ts.Exists(gluttertest.ByText("HIDDEN")) {
		t.Error("a view whose when does not hold is shown")
	}
	// the headers are 22 px high, one under the other when the others are collapsed
	f := h.ts.Size(gluttertest.ByKey(ViewHeaderKey("outline")))
	if f.Height != ViewHeaderHeight {
		t.Errorf("a header is %v high", f.Height)
	}
	if views.IsCollapsed("folders") || !views.IsCollapsed("outline") {
		t.Fatal("views start as their descriptors say")
	}

	h.ts.Tap(gluttertest.ByKey(ViewHeaderKey("folders")))
	h.settle()
	if !views.IsCollapsed("folders") {
		t.Error("a click on the header did not collapse the view")
	}
	h.ts.Tap(gluttertest.ByKey(ViewHeaderKey("outline")))
	h.settle()
	if views.IsCollapsed("outline") {
		t.Error("a click on the header did not expand the view")
	}

	// the keyboard: the header has the focus after a click; Left folds, Right unfolds, Enter toggles
	keys := []struct {
		key  services.LogicalKeyboardKey
		want bool
	}{
		{services.LogicalKeyboardKeyArrowLeft, true},
		{services.LogicalKeyboardKeyArrowLeft, true}, // already folded
		{services.LogicalKeyboardKeyArrowRight, false},
		{services.LogicalKeyboardKeyArrowRight, false},
		{services.LogicalKeyboardKeyEnter, true},
		{services.LogicalKeyboardKeySpace, false},
	}
	for i, k := range keys {
		h.ts.SendKeyEvent(k.key)
		h.settle()
		if views.IsCollapsed("outline") != k.want {
			t.Errorf("key %d: collapsed = %v, want %v", i, views.IsCollapsed("outline"), k.want)
		}
	}
	noErrors(t, h)
}

func TestExpandedViewsShareTheHeight(t *testing.T) {
	h, views := viewsHarness(t, func(o *Options) {
		o.ViewBody = func(_ widgets.BuildContext, id string) widgets.Widget {
			return widgets.NewKeyedSubtree(widgets.NewSizedBox(nil), widgets.WidgetOpts{Key: bodyKey(id)})
		}
	})
	views.SetCollapsed("outline", false)
	h.settle()
	folders := h.ts.Size(gluttertest.ByKey(bodyKey("folders")))
	outline := h.ts.Size(gluttertest.ByKey(bodyKey("outline")))
	if folders.Height <= 0 || folders.Height != outline.Height {
		t.Errorf("bodies %v and %v: the open views share the height", folders, outline)
	}
	if h.ts.Exists(gluttertest.ByKey(bodyKey("timeline"))) {
		t.Error("the body of a collapsed view is built")
	}
	// the side bar is 588 high with a title of 35 and three headers of 22
	if got, want := folders.Height+outline.Height, 588.0-35-3*22; got != want {
		t.Errorf("bodies take %v, want %v", got, want)
	}
	noErrors(t, h)
}

func bodyKey(id string) foundation.Key { return foundation.NewValueKey("workbench:body:" + id) }

func TestManageMenu(t *testing.T) {
	var ran []string
	items := []kit.MenuItem{{ID: "settings", Label: "&Settings", Keybinding: "Ctrl+,"}, kit.SeparatorItem(), {ID: "themes", Label: "&Themes"}}
	h, _ := viewsHarness(t, func(o *Options) {
		o.ManageItems = func() []kit.MenuItem { return items }
		o.OnRunMenu = func(it kit.MenuItem) { ran = append(ran, it.ID) }
	})
	f := gluttertest.ByKey(ManageKey)
	if !h.ts.Exists(f) {
		t.Fatal("no Manage button")
	}
	if r := h.ts.TopLeft(f); r.Dx != 0 || r.Dy != 30+588-48 {
		t.Errorf("the gear is at %v, at the bottom of the activity bar", r)
	}
	h.ts.Tap(f)
	h.settle()
	for _, text := range []string{"Settings", "Themes", "Ctrl+,"} {
		if !h.ts.Exists(gluttertest.ByText(text)) {
			t.Errorf("the Manage menu does not show %q", text)
		}
	}
	// the menu opens beside the activity bar, above the bottom of the gear
	menu := h.ts.TopLeft(gluttertest.ByType[*kit.Menu]())
	if menu.Dx != 48 {
		t.Errorf("the menu is at x = %v, beside the activity bar", menu.Dx)
	}
	h.ts.Tap(gluttertest.ByText("Themes"))
	h.settle()
	if !reflect.DeepEqual(ran, []string{"themes"}) {
		t.Errorf("ran %v", ran)
	}
	if h.ts.Exists(gluttertest.ByText("Settings")) {
		t.Error("the menu is still open after an item ran")
	}
	// a second click on the gear closes the menu it opened
	h.ts.Tap(f)
	h.settle()
	h.ts.Tap(f)
	h.settle()
	if h.ts.Exists(gluttertest.ByText("Settings")) {
		t.Error("a click on the gear does not close the menu")
	}
	noErrors(t, h)
}

func TestWithoutManageItemsThereIsNoGear(t *testing.T) {
	h, _ := viewsHarness(t, nil)
	if h.ts.Exists(gluttertest.ByKey(ManageKey)) {
		t.Error("a Manage button without a Manage menu")
	}
}

func TestViewsPanelModel(t *testing.T) {
	st := DefaultLayoutState()
	l := NewLayout(st)
	v := NewViews(testViewRegistry(), l)
	if v.ActivePanel() != "problems" {
		t.Errorf("the panel starts on %q, want the first container of the panel", v.ActivePanel())
	}
	changes := 0
	v.OnChange(func() { changes++ })
	// the side bar is not the panel: a container of the other location does nothing
	v.OpenPanel("search")
	v.OpenPanel("nothing")
	if v.ActivePanel() != "problems" || l.State().PanelVisible || changes != 0 {
		t.Errorf("a container that is not of the panel changed it: %q visible %v changes %d", v.ActivePanel(), l.State().PanelVisible, changes)
	}
	// opening the active one shows the panel and tells nobody about a tab that did not change
	v.OpenPanel("problems")
	if !l.State().PanelVisible || changes != 0 {
		t.Errorf("visible %v, changes %d", l.State().PanelVisible, changes)
	}
	if v.Active() != "explorer" {
		t.Errorf("opening a tab of the panel changed the side bar to %q", v.Active())
	}
	empty := NewViews(command.NewViewRegistry(), nil)
	if empty.ActivePanel() != "" {
		t.Errorf("a registry with no panel container has %q", empty.ActivePanel())
	}
	empty.OpenPanel("problems") // no layout, no container: nothing to do
}
