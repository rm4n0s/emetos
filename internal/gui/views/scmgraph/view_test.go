package scmgraph

import (
	"runtime"
	"testing"
	"time"

	"github.com/rm4n0s/glutter/animation"
	"github.com/rm4n0s/glutter/foundation"
	"github.com/rm4n0s/glutter/gestures"
	"github.com/rm4n0s/glutter/gluttertest"
	"github.com/rm4n0s/glutter/painting"
	"github.com/rm4n0s/glutter/ui"
	"github.com/rm4n0s/glutter/widgets"

	"github.com/rm4n0s/emetos/internal/gui/kit"
	"github.com/rm4n0s/emetos/internal/gui/kit/fonts"
	"github.com/rm4n0s/emetos/internal/gui/testutil"
)

var goldenThemes = []struct {
	name   string
	colors kit.Colors
}{
	{"dark", kit.DarkModern()},
	{"light", kit.LightModern()},
}

// pump shows a widget at the top left of a window of a size, in the theme and with the test fonts, on the background of the side bar.
func pump(t testing.TB, colors kit.Colors, size ui.Size, child widgets.Widget) *gluttertest.Tester {
	t.Helper()
	ts := testutil.New(t, size)
	ts.PumpWidget(widgets.NewWidgetsApp(colors.SideBarBackground, widgets.WidgetsAppOpts{
		DebugShowCheckedModeBanner: foundation.Some(false),
		OnGenerateRoute: func(settings widgets.RouteSettings) widgets.Route {
			return widgets.NewPageRouteBuilder(func(widgets.BuildContext, animation.Animation[float64], animation.Animation[float64]) widgets.Widget {
				page := widgets.NewColoredBox(colors.SideBarBackground, widgets.NewAlign(
					widgets.NewSizedBox(child, widgets.SizedBoxOpts{Width: foundation.Some(size.Width), Height: foundation.Some(size.Height)}),
					widgets.AlignOpts{Alignment: painting.AlignmentTopLeft}))
				return kit.NewTheme(colors, page, kit.ThemeOpts{UIFont: testutil.MonoFamily})
			}, widgets.PageRouteBuilderOpts{PageRouteOpts: widgets.PageRouteOpts{RouteOpts: widgets.RouteOpts{Settings: settings}}})
		},
	}))
	ts.Pump(time.Millisecond)
	return ts
}

var viewSize = ui.Size{Width: 360, Height: 260}

// show shows the body of the rig's pane and settles.
func (r *rig) show(colors kit.Colors, size ui.Size, opts BodyOpts) *gluttertest.Tester {
	r.t.Helper()
	if opts.Post == nil {
		opts.Post = func(f func()) { r.mu.Lock(); r.posted = append(r.posted, f); r.mu.Unlock() }
	}
	ts := pump(r.t, colors, size, NewBody(r.pane, opts))
	r.ts = ts
	r.settleUI()
	return ts
}

func (r *rig) settleUI() {
	for range 3 {
		r.ts.Pump(time.Millisecond)
		r.run()
	}
}

func (r *rig) noErrors() {
	r.t.Helper()
	if errs := r.ts.TakeErrors(); len(errs) != 0 {
		r.t.Errorf("errors: %v", errs)
	}
}

func inline(Row) []kit.Action {
	code, _ := fonts.CodiconByName("diff-multiple")
	return []kit.Action{{ID: "git.viewCommit", Label: "Open Changes", Icon: code}}
}

func TestGoldenBranchingHistory(t *testing.T) {
	for _, th := range goldenThemes {
		t.Run(th.name, func(t *testing.T) {
			r := newRig(t, branching())
			r.settle()
			r.show(th.colors, viewSize, BodyOpts{Actions: inline})
			testutil.MatchesGolden(r.ts, "branching_"+th.name)
			r.noErrors()
		})
	}
}

func TestGoldenAllReferencesWithEveryBadge(t *testing.T) {
	for _, th := range goldenThemes {
		t.Run(th.name, func(t *testing.T) {
			r := newRig(t, branching())
			r.badges = "all"
			r.pane.SetFilter(Filter{Kind: FilterAll})
			r.settle()
			r.show(th.colors, viewSize, BodyOpts{})
			testutil.MatchesGolden(r.ts, "all_badges_"+th.name)
			r.noErrors()
		})
	}
}

func TestGoldenHoverAndSelection(t *testing.T) {
	for _, th := range goldenThemes {
		t.Run(th.name, func(t *testing.T) {
			r := newRig(t, branching())
			r.settle()
			r.show(th.colors, viewSize, BodyOpts{Actions: inline, ForceHover: "b2"})
			testutil.MatchesGolden(r.ts, "hover_row_"+th.name)
			r.noErrors()
		})
	}
}

func TestGoldenDetailsHover(t *testing.T) {
	for _, th := range goldenThemes {
		t.Run(th.name, func(t *testing.T) {
			r := newRig(t, branching())
			r.settle()
			r.show(th.colors, ui.Size{Width: 640, Height: 260}, BodyOpts{ShowHover: "m", ForceHover: "m"})
			r.ts.Pump(time.Second)
			testutil.MatchesGolden(r.ts, "details_"+th.name)
			r.noErrors()
		})
	}
}

func TestGoldenLoadMoreRow(t *testing.T) {
	for _, th := range goldenThemes {
		t.Run(th.name, func(t *testing.T) {
			r := newRig(t, branching())
			r.onScrl = false
			r.pageSz = 3
			r.pane.Refresh()
			r.settle()
			r.show(th.colors, viewSize, BodyOpts{})
			testutil.MatchesGolden(r.ts, "load_more_"+th.name)
			r.noErrors()
		})
	}
}

func TestGoldenMessages(t *testing.T) {
	for _, th := range goldenThemes {
		t.Run("loading "+th.name, func(t *testing.T) {
			h := branching()
			h.gate = make(chan struct{})
			r := newRig(t, h)
			r.show(th.colors, viewSize, BodyOpts{})
			testutil.MatchesGolden(r.ts, "loading_"+th.name)
			r.noErrors()
			close(h.gate)
		})
		t.Run("empty "+th.name, func(t *testing.T) {
			h := branching()
			h.all = nil
			r := newRig(t, h)
			r.settle()
			r.show(th.colors, viewSize, BodyOpts{})
			testutil.MatchesGolden(r.ts, "empty_"+th.name)
			r.noErrors()
		})
		t.Run("search found nothing "+th.name, func(t *testing.T) {
			r := newRig(t, branching())
			r.settle()
			r.pane.SetSearch("zzz")
			r.after[len(r.after)-1]()
			r.settle()
			r.show(th.colors, viewSize, BodyOpts{})
			testutil.MatchesGolden(r.ts, "no_match_"+th.name)
			r.noErrors()
		})
	}
}

func TestGoldenSearchResults(t *testing.T) {
	for _, th := range goldenThemes {
		t.Run(th.name, func(t *testing.T) {
			r := newRig(t, branching())
			r.settle()
			r.pane.SetSearch("grace")
			r.after[len(r.after)-1]()
			r.settle()
			r.show(th.colors, viewSize, BodyOpts{})
			testutil.MatchesGolden(r.ts, "search_"+th.name)
			r.noErrors()
		})
	}
}

func TestClickingARowRunsTheOpenCommand(t *testing.T) {
	r := newRig(t, branching())
	r.settle()
	r.show(kit.DarkModern(), viewSize, BodyOpts{})
	r.ts.Pump(time.Second)
	// the search box is the first 32 pixels; the rows are 22 high: the second row is the merge commit
	r.ts.TapAt(ui.Offset{Dx: 150, Dy: 32 + 22 + 11}, gluttertest.GestureOpts{Kind: ui.PointerDeviceKindMouse})
	r.settleUI()
	if len(r.runs) != 1 || r.runs[0].ID != "git.viewCommit" {
		t.Fatalf("runs = %+v", r.runs)
	}
	if arg, ok := historyArg(r.runs[0].Args); !ok || arg.Item == nil || arg.Item.ID != "m" {
		t.Errorf("args = %+v", r.runs[0].Args)
	}
}

func TestARightClickOpensTheMenuOfTheRow(t *testing.T) {
	r := newRig(t, branching())
	r.settle()
	var got []string
	r.show(kit.DarkModern(), viewSize, BodyOpts{ShowMenu: func(row Row, _ ui.Offset) { got = append(got, row.Item.ID) }})
	r.ts.TapAt(ui.Offset{Dx: 150, Dy: 32 + 22 + 11}, gluttertest.GestureOpts{Kind: ui.PointerDeviceKindMouse, Buttons: foundation.Some(gestures.SecondaryButton)})
	r.settleUI()
	if len(got) != 1 || got[0] != "m" {
		t.Errorf("menus for %v", got)
	}
}

func TestTheLoadMoreRowLoadsWhenItComesIntoView(t *testing.T) {
	for _, onScroll := range []bool{true, false} {
		name := "with paging on scroll"
		if !onScroll {
			name = "without paging on scroll"
		}
		t.Run(name, func(t *testing.T) {
			h := branching()
			r := newRig(t, h)
			r.onScrl = onScroll
			r.pageSz = 3
			r.pane.Refresh()
			r.settle()
			first := len(h.Queries())
			r.show(kit.DarkModern(), ui.Size{Width: 360, Height: 600}, BodyOpts{})
			r.settle()
			r.settleUI()
			r.settle()
			queries := len(h.Queries()) - first
			if onScroll && queries < 2 {
				t.Errorf("%d pages were read after the first: the list was not filled by scrolling to the end", queries)
			}
			if !onScroll && queries != 0 {
				t.Errorf("%d pages were read without being asked", queries)
			}
		})
	}
}

func TestTheSearchBoxSearches(t *testing.T) {
	r := newRig(t, branching())
	r.settle()
	r.show(kit.DarkModern(), viewSize, BodyOpts{})
	r.ts.EnterText(gluttertest.Descendant(gluttertest.ByKey(SearchKey), gluttertest.ByType[*kit.InputBox]()), "topic")
	r.settleUI()
	if got := r.pane.Search(); got != "topic" {
		t.Errorf("search = %q", got)
	}
}

// TestScrollingALongGraphIsSmooth is the budget of SPEC §14 for the Graph: with 5,000 commits loaded (five pages of the largest size) no frame of
// the UI thread goes over 16 ms while scrolling (build, layout and paint, without the renderer; ten times under the race detector),
// two of the 60 frames of the steady scroll may be late, and a jump has twice the budget. Only the rows in view are built.
func TestScrollingALongGraphIsSmooth(t *testing.T) {
	if testing.Short() {
		t.Skip("a long list")
	}
	runtime.LockOSThread()
	defer runtime.UnlockOSThread()
	scale := time.Duration(1)
	if raceEnabled {
		scale = 10
	}
	h := long(5000)
	r := newRig(t, h)
	r.pageSz = 1000
	r.pane.Refresh()
	r.settle()
	for range 4 {
		r.pane.ScrolledToEnd()
		r.settle()
	}
	ctl := kit.NewListController()
	r.show(kit.DarkModern(), ui.Size{Width: 400, Height: 600}, BodyOpts{Controller: ctl})
	if got := len(r.pane.Rows()); got < 5000 {
		t.Fatalf("%d rows", got)
	}
	frame := func() time.Duration {
		s := threadCPU(t)
		r.ts.Pump(16 * time.Millisecond)
		return threadCPU(t) - s
	}
	budget := 16 * time.Millisecond * scale
	var worst time.Duration
	late := 0
	at := ui.Offset{Dx: 100, Dy: 300}
	for range 60 {
		r.ts.ScrollAt(at, ui.Offset{Dy: 240})
		d := frame()
		worst = max(worst, d)
		if d > budget {
			late++
		}
	}
	t.Logf("the longest of 60 frames of a steady scroll: %v (%d over %v)", worst, late, budget)
	if late > 2 {
		t.Errorf("%d of 60 frames took more than %v while scrolling (the longest %v)", late, budget, worst)
	}
	var jump time.Duration
	for _, y := range []float64{2500 * RowHeight, 4999 * RowHeight, 0} {
		ctl.ScrollTo(y)
		jump = max(jump, frame())
	}
	t.Logf("the longest frame after a jump: %v", jump)
	if jump > 2*budget {
		t.Errorf("a frame after a jump took %v, budget %v", jump, 2*budget)
	}
	r.noErrors()
}
