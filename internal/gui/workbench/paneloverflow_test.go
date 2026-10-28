package workbench

import (
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

func TestPanelTabsAreWholeAndOverflowIntoAMenu(t *testing.T) {
	st := DefaultLayoutState()
	st.PanelVisible = true
	actions := widgets.NewSizedBox(widgets.NewText("view actions"), widgets.SizedBoxOpts{Width: foundation.Some(200.0), Height: foundation.Some(22.0)})
	tests := []struct {
		name         string
		width        float64
		active       string
		wantOverflow bool
		wantActions  bool
	}{
		{"1280 px wide: all tabs and the actions", 1280, "problems", false, true},
		{"1024 px wide: all tabs, the actions give way", 1024, "tasks", false, false},
		{"800 px wide: some tabs go into the menu", 800, "problems", true, false},
		{"600 px wide: more tabs go into the menu", 600, "gotools", true, false},
		{"400 px wide: the active tab at the end shows", 400, "terminal", true, false},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			h := newHarness(t, ui.Size{Width: tc.width, Height: 500}, kit.DarkModern(), st, nil, func(o *Options) {
				o.PanelTabs, o.ActivePanel, o.PanelActions = overflowTabs, tc.active, actions
			})
			if got := h.ts.Exists(gluttertest.ByKey(PanelOverflowKey)); got != tc.wantOverflow {
				t.Errorf("the \"...\" button is shown: %v, want %v", got, tc.wantOverflow)
			}
			if got := h.ts.Exists(gluttertest.ByText("view actions")); got != tc.wantActions {
				t.Errorf("the actions of the view are shown: %v, want %v", got, tc.wantActions)
			}
			shown := 0
			for _, tab := range overflowTabs {
				if h.ts.Exists(gluttertest.ByText(strings.ToUpper(tab.Title))) {
					shown++
				}
			}
			if !tc.wantOverflow && shown != len(overflowTabs) {
				t.Errorf("%d of %d tabs are shown", shown, len(overflowTabs))
			}
			if tc.wantOverflow && (shown == 0 || shown == len(overflowTabs)) {
				t.Errorf("%d of %d tabs are shown with the \"...\" button", shown, len(overflowTabs))
			}
			if !h.ts.Exists(gluttertest.ByText(strings.ToUpper(titleOf(tc.active)))) {
				t.Errorf("the active tab %q is not shown", tc.active)
			}
			if cut := cutParagraphs(h); len(cut) != 0 {
				t.Errorf("these texts are cut: %q", cut)
			}
			if !h.ts.Exists(button("Hide Panel")) || !h.ts.Exists(button("Maximize Panel")) {
				t.Error("the buttons of the panel are not all there")
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
