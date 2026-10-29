package workbench

import (
	"math"
	"slices"
	"strconv"
	"strings"
	"testing"

	"github.com/rm4n0s/glutter/foundation"
	"github.com/rm4n0s/glutter/gluttertest"
	"github.com/rm4n0s/glutter/painting"
	"github.com/rm4n0s/glutter/rendering"
	"github.com/rm4n0s/glutter/ui"
	"github.com/rm4n0s/glutter/widgets"

	"github.com/rm4n0s/emetos/internal/gui/kit"
)

// the panel's tabs of a session with the Go extension and the terminal, with counts on Problems and Tasks (step 17b.6, D4)
var overflowTabs = []PanelTab{
	{ID: "problems", Title: "Problems", Badge: 12}, {ID: "output", Title: "Output"}, {ID: "tasks", Title: "Tasks", Badge: 3},
	{ID: "gotools", Title: "Go Tools"}, {ID: "gorun", Title: "Go Run"}, {ID: "gotests", Title: "Go Tests"}, {ID: "terminal", Title: "Terminal"},
}

// cutParagraphs are the texts of a widget tree that are drawn shorter than they are: an ellipsis or a clip hides the end.
func cutParagraphs(h *harness) []string {
	var cut []string
	for _, el := range h.ts.Elements(gluttertest.ByType[*widgets.Text]()) {
		r, ok := el.RenderObject().(*rendering.RenderParagraph)
		if !ok || r == nil || !r.HasSize() {
			continue
		}
		if r.Size().Width+0.01 < r.ComputeMaxIntrinsicWidth(1e9) {
			cut = append(cut, painting.InlineSpanToPlainText(r.Text(), painting.ToPlainTextOpts{}))
		}
	}
	return cut
}

// testActions are the actions of a view for the tests: a text of width px that gives way down to least px (and leaves out under
// it), as the filter of the Problems does.
type testActions struct {
	widgets.StatelessWidget
	width, least float64
	// child is what the actions show, a text when it is nil
	child widgets.Widget
}

func newTestActions(width, least float64) *testActions {
	return &testActions{StatelessWidget: widgets.NewStatelessWidgetBase(), width: width, least: least}
}

// TitleActionsWidth implements kit.TitleActions.
func (a *testActions) TitleActionsWidth(room float64) float64 {
	switch {
	case room >= a.width:
		return a.width
	case room >= a.least:
		return room
	}
	return 0
}

// Build implements widgets.StatelessWidget.
func (a *testActions) Build(widgets.BuildContext) widgets.Widget {
	return widgets.NewLayoutBuilder(func(_ widgets.BuildContext, c rendering.BoxConstraints) widgets.Widget {
		child := a.child
		if child == nil {
			child = widgets.NewText("view actions", widgets.TextOpts{MaxLines: foundation.Some(1)})
		}
		return widgets.NewSizedBox(child, widgets.SizedBoxOpts{Width: foundation.Some(c.MaxWidth), Height: foundation.Some(22.0)})
	})
}

// shownTabs are the titles of the tabs that are in the strip, left to right.
func shownTabs(h *harness) []string {
	var shown []string
	for _, tab := range overflowTabs {
		if h.ts.Exists(gluttertest.ByText(strings.ToUpper(tab.Title))) {
			shown = append(shown, tab.Title)
		}
	}
	return shown
}

// The actions of the view keep their room and the tabs get what is left (decision 370, VS Code's layoutCompositeBar): the actions
// are shown whole while the tabs still have the room for the active one, the tabs that do not fit go into the menu, the active tab
// shows, and nothing is cut.
func TestPanelTabsAreWholeAndOverflowIntoAMenu(t *testing.T) {
	st := DefaultLayoutState()
	st.PanelVisible, st.SideBarVisible, st.ActivityBarVisible = true, false, false // the panel is as wide as the window
	tests := []struct {
		name         string
		width        float64
		active       string
		actions      *testActions
		wantOverflow bool
		wantActions  bool
	}{
		{"1280 px wide: all the tabs and the actions", 1280, "problems", newTestActions(200, 150), false, true},
		{"700 px wide: the actions keep their 200 px (the old rule hid them), some tabs go into the menu", 700, "tasks", newTestActions(200, 150), true, true},
		{"600 px wide: more tabs go into the menu", 600, "problems", newTestActions(300, 150), true, true},
		{"450 px wide: the actions shrink, towards their least", 450, "gotools", newTestActions(300, 150), true, true},
		{"400 px wide: the active tab at the end shows, the actions do not fit", 400, "terminal", newTestActions(300, 300), true, false},
		{"500 px wide with no actions: the tabs take the strip", 500, "problems", nil, true, false},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			h := newHarness(t, ui.Size{Width: tc.width, Height: 500}, kit.DarkModern(), st, nil, func(o *Options) {
				o.PanelTabs, o.ActivePanel = overflowTabs, tc.active
				if tc.actions != nil {
					o.PanelActions = tc.actions
				}
			})
			if got := h.ts.Exists(gluttertest.ByKey(PanelOverflowKey)); got != tc.wantOverflow {
				t.Errorf("the \"...\" button is shown: %v, want %v", got, tc.wantOverflow)
			}
			actionsShown := h.ts.Exists(gluttertest.ByText("view actions"))
			if actionsShown != tc.wantActions {
				t.Errorf("the actions of the view are shown: %v, want %v", actionsShown, tc.wantActions)
			}
			shown := shownTabs(h)
			if !tc.wantOverflow && len(shown) != len(overflowTabs) {
				t.Errorf("%d of %d tabs are shown", len(shown), len(overflowTabs))
			}
			if tc.wantOverflow && (len(shown) == 0 || len(shown) == len(overflowTabs)) {
				t.Errorf("%d of %d tabs are shown with the \"...\" button", len(shown), len(overflowTabs))
			}
			if !slices.Contains(shown, titleOf(tc.active)) {
				t.Errorf("the active tab %q is not shown (shown: %v)", tc.active, shown)
			}
			if cut := cutParagraphs(h); len(cut) != 0 {
				t.Errorf("these texts are cut: %q", cut)
			}
			if !h.ts.Exists(button("Hide Panel")) || !h.ts.Exists(button("Maximize Panel")) {
				t.Error("the buttons of the panel are not all there")
			}
			// the places: the tabs, then the actions, then the buttons, none over another, all inside the strip
			maximize := h.ts.TopLeft(button("Maximize Panel")).Dx
			if actionsShown {
				at := h.ts.TopLeft(gluttertest.ByText("view actions")).Dx
				if at+h.ts.Size(gluttertest.ByText("view actions")).Width > maximize {
					t.Errorf("the actions (from %.1f) reach the buttons (from %.1f)", at, maximize)
				}
				for _, title := range shown {
					f := gluttertest.ByText(strings.ToUpper(title))
					if right := h.ts.TopLeft(f).Dx + h.ts.Size(f).Width; right > at {
						t.Errorf("the tab %s ends at %.1f, over the actions that start at %.1f", title, right, at)
					}
				}
			}
			noErrors(t, h)
		})
	}
}

// The actions are measured first: the width the tabs are left with depends on the width of the actions and on nothing else, and the
// least is 50 px, or the room of the active tab with the "..." button.
func TestPanelActionsKeepTheirRoomWhateverTheTabs(t *testing.T) {
	st := DefaultLayoutState()
	st.PanelVisible, st.SideBarVisible, st.ActivityBarVisible = true, false, false // the panel is as wide as the window
	tests := []struct {
		name   string
		width  float64
		tabs   []PanelTab
		active string
		// wantActions is the width the actions have (0 for left out)
		wantActions float64
		// wantTabsAtLeast is the width the tabs have at least, from the lead of the strip
		wantTabsAtLeast float64
	}{
		{"one tab and wide actions", 1000, overflowTabs[:1], "problems", 200, 50},
		{"many tabs, the actions keep 200 px", 1280, overflowTabs, "problems", 200, 50},
		{"a very wide strip gives the actions no more than they ask", 3000, overflowTabs, "problems", 200, 50},
		{"a strip where the actions would leave the tabs under the active tab", 330, overflowTabs, "terminal", 0, 0},
		{"no tab: the tabs are left 50 px, the actions fit exactly", 316, nil, "", 200, 50},
		{"no tab: under that the actions give way", 315, nil, "", 0, 0},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			h := newHarness(t, ui.Size{Width: tc.width, Height: 500}, kit.DarkModern(), st, nil, func(o *Options) {
				o.PanelTabs, o.ActivePanel, o.PanelActions = tc.tabs, tc.active, newTestActions(200, 200)
			})
			f := gluttertest.ByText("view actions")
			if got := h.ts.Exists(f); got != (tc.wantActions > 0) {
				t.Fatalf("the actions are shown: %v, want %v", got, tc.wantActions > 0)
			}
			if tc.wantActions > 0 {
				if w := h.ts.Size(f).Width; math.Abs(w-tc.wantActions) > 0.5 {
					t.Errorf("the actions are %.1f px wide, want %.1f", w, tc.wantActions)
				}
				// between the tabs and the buttons: 4 px of gap and the buttons of the panel
				maximize := h.ts.TopLeft(button("Maximize Panel")).Dx
				if gap := maximize - (h.ts.TopLeft(f).Dx + h.ts.Size(f).Width); math.Abs(gap-panelActionsGap) > 0.5 {
					t.Errorf("the gap between the actions and the buttons is %.1f, want %v", gap, panelActionsGap)
				}
				if room := h.ts.TopLeft(f).Dx - panelTitleLead; room+0.5 < tc.wantTabsAtLeast {
					t.Errorf("the tabs have %.1f px, want at least %.1f", room, tc.wantTabsAtLeast)
				}
			}
			if cut := cutParagraphs(h); len(cut) != 0 {
				t.Errorf("these texts are cut: %q", cut)
			}
			noErrors(t, h)
		})
	}
}

// menuLabel is how the menu of the "..." button shows a tab: its title and its count.
func menuLabel(t PanelTab) string {
	if t.Badge > 0 {
		return t.Title + " (" + strconv.Itoa(t.Badge) + ")"
	}
	return t.Title
}

func titleOf(id string) string {
	for _, t := range overflowTabs {
		if t.ID == id {
			return t.Title
		}
	}
	return ""
}

// The menu of the "..." button lists the tabs that are not shown, with their counts and the active one checked, and choosing one
// selects its view.
func TestPanelOverflowMenuListsTheHiddenTabs(t *testing.T) {
	st := DefaultLayoutState()
	st.PanelVisible = true
	var selected string
	h := newHarness(t, ui.Size{Width: 400, Height: 500}, kit.DarkModern(), st, nil, func(o *Options) {
		o.PanelTabs, o.ActivePanel = overflowTabs, "problems"
		o.OnSelectPanel = func(id string) { selected = id }
	})
	var hidden []PanelTab
	for _, tab := range overflowTabs {
		if !h.ts.Exists(gluttertest.ByText(strings.ToUpper(tab.Title))) {
			hidden = append(hidden, tab)
		}
	}
	if len(hidden) == 0 {
		t.Fatal("no tab is hidden at 400 px")
	}
	h.ts.Tap(button("More Views..."))
	h.settle()
	for _, tab := range hidden {
		if !h.ts.Exists(gluttertest.ByText(menuLabel(tab))) {
			t.Errorf("the menu does not list %q", menuLabel(tab))
		}
	}
	last := hidden[len(hidden)-1]
	h.ts.Tap(gluttertest.ByText(menuLabel(last)))
	h.settle()
	if selected != last.ID {
		t.Errorf("choosing %q selected %q", last.Title, selected)
	}
	noErrors(t, h)
}
