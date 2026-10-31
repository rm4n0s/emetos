package timeline

import (
	"testing"
	"time"

	"github.com/rm4n0s/glutter/animation"
	"github.com/rm4n0s/glutter/foundation"
	"github.com/rm4n0s/glutter/gestures"
	"github.com/rm4n0s/glutter/gluttertest"
	"github.com/rm4n0s/glutter/painting"
	"github.com/rm4n0s/glutter/ui"
	"github.com/rm4n0s/glutter/widgets"

	"github.com/rm4n0s/emetos/internal/core/timeline"
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

// commits is a history of a file as the Git extension gives it.
func commits() []timeline.Item {
	mk := func(id, label, author string, mins int) timeline.Item {
		return timeline.Item{
			ID: id, Label: label, Description: author, Timestamp: at(mins), Icon: "git-commit", ContextValue: "git:file:commit",
			Detail:  "**" + author + "**, 3 days ago\n\n" + label,
			Command: timeline.Command{ID: "git.timeline.openDiff"},
		}
	}
	return []timeline.Item{
		mk("c5", "Rename the loader to reader", "Ada Example", 990),
		mk("c4", "Fix the off-by-one in the loader", "Grace Hopper", 960),
		mk("c3", "Read the configuration once", "Ada Example", 400),
		mk("c2", "Add the loader", "Ada Example", 100),
		mk("c1", "Initial commit", "Linus Example", 0),
	}
}

// show shows the body of the rig's pane and settles.
func (r *rig) show(colors kit.Colors, size ui.Size, opts BodyOpts) *gluttertest.Tester {
	r.t.Helper()
	if opts.Post == nil {
		opts.Post = func(f func()) { r.after = append(r.after, f) }
	}
	ts := pump(r.t, colors, size, NewBody(r.pane, opts))
	r.ts = ts
	r.settleUI()
	return ts
}

func (r *rig) settleUI() {
	for range 3 {
		r.ts.Pump(time.Millisecond)
		for len(r.after) > 0 {
			f := r.after[0]
			r.after = r.after[1:]
			f()
		}
		r.run()
	}
}

func (r *rig) noErrors() {
	r.t.Helper()
	if errs := r.ts.TakeErrors(); len(errs) != 0 {
		r.t.Errorf("errors: %v", errs)
	}
}

func inline(row Row) []kit.Action {
	code, _ := fonts.CodiconByName("diff-multiple")
	return []kit.Action{{ID: "git.timeline.viewCommit", Label: "Open Commit", Icon: code}}
}

var viewSize = ui.Size{Width: 320, Height: 200}

func TestGoldenItems(t *testing.T) {
	for _, th := range goldenThemes {
		t.Run(th.name, func(t *testing.T) {
			r := newRig(t)
			r.pageSz = 4
			r.pane.opts.PageOnScroll = func() bool { return false }
			r.provider("git", &fakeProvider{answer: pager(commits())})
			r.run()
			r.pane.SetActive(fileA)
			r.settle()
			r.show(th.colors, viewSize, BodyOpts{Actions: inline})
			testutil.MatchesGolden(r.ts, "items_"+th.name)
			r.noErrors()
		})
	}
}

func TestGoldenHoverAndSelection(t *testing.T) {
	for _, th := range goldenThemes {
		t.Run(th.name, func(t *testing.T) {
			r := newRig(t)
			r.pageSz = 10
			r.provider("git", &fakeProvider{answer: pager(commits())})
			r.run()
			r.pane.SetActive(fileA)
			r.settle()
			r.show(th.colors, viewSize, BodyOpts{Actions: inline, ForceHover: "git|c4"})
			testutil.MatchesGolden(r.ts, "hover_"+th.name)
			r.noErrors()
		})
	}
}

func TestGoldenLoadingMore(t *testing.T) {
	for _, th := range goldenThemes {
		t.Run(th.name, func(t *testing.T) {
			r := newRig(t)
			r.pageSz = 4
			r.pane.opts.PageOnScroll = func() bool { return false }
			f := &fakeProvider{answer: pager(commits())}
			r.provider("git", f)
			r.run()
			r.pane.SetActive(fileA)
			r.settle()
			f.mu.Lock()
			f.gate = make(chan struct{})
			f.mu.Unlock()
			r.pane.LoadMore()
			r.show(th.colors, viewSize, BodyOpts{Actions: inline})
			testutil.MatchesGolden(r.ts, "loading_more_"+th.name)
			r.noErrors()
			close(f.gate)
		})
	}
}

func TestGoldenMessages(t *testing.T) {
	for _, th := range goldenThemes {
		t.Run("loading "+th.name, func(t *testing.T) {
			r := newRig(t)
			f := &fakeProvider{gate: make(chan struct{})}
			r.provider("git", f)
			r.run()
			r.pane.SetActive(fileA)
			r.show(th.colors, viewSize, BodyOpts{})
			testutil.MatchesGolden(r.ts, "loading_"+th.name)
			r.noErrors()
			close(f.gate)
		})
		t.Run("empty "+th.name, func(t *testing.T) {
			r := newRig(t)
			r.provider("git", &fakeProvider{})
			r.run()
			r.pane.SetActive(fileA)
			r.settle()
			r.show(th.colors, viewSize, BodyOpts{})
			testutil.MatchesGolden(r.ts, "empty_"+th.name)
			r.noErrors()
		})
		t.Run("no editor "+th.name, func(t *testing.T) {
			r := newRig(t)
			r.show(th.colors, viewSize, BodyOpts{})
			testutil.MatchesGolden(r.ts, "no_editor_"+th.name)
			r.noErrors()
		})
	}
}

func TestClickingARowRunsItsCommand(t *testing.T) {
	r := newRig(t)
	r.pageSz = 10
	r.provider("git", &fakeProvider{answer: pager(commits())})
	r.run()
	r.pane.SetActive(fileA)
	r.settle()
	r.show(kit.DarkModern(), viewSize, BodyOpts{})
	r.ts.Pump(time.Second)
	r.ts.TapAt(ui.Offset{Dx: 100, Dy: 22*1 + 11}, gluttertest.GestureOpts{Kind: ui.PointerDeviceKindMouse})
	r.settleUI()
	if len(r.runs) != 1 || r.runs[0].ID != "git.timeline.openDiff" {
		t.Fatalf("runs = %+v", r.runs)
	}
	args, _ := r.runs[0].Args.([]any)
	if len(args) != 1 || args[0].(map[string]any)["id"] != "c4" {
		t.Errorf("args = %+v", r.runs[0].Args)
	}
}

func TestARightClickOpensTheMenuOfTheRow(t *testing.T) {
	r := newRig(t)
	r.pageSz = 10
	r.provider("git", &fakeProvider{answer: pager(commits())})
	r.run()
	r.pane.SetActive(fileA)
	r.settle()
	var got []string
	r.show(kit.DarkModern(), viewSize, BodyOpts{ShowMenu: func(row Row, _ ui.Offset) { got = append(got, row.ID) }})
	r.ts.TapAt(ui.Offset{Dx: 100, Dy: 11}, gluttertest.GestureOpts{Kind: ui.PointerDeviceKindMouse, Buttons: foundation.Some(gestures.SecondaryButton)})
	r.settleUI()
	if len(got) != 1 || got[0] != "c5" {
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
			r := newRig(t)
			r.pageSz = 2
			r.pane.opts.PageOnScroll = func() bool { return onScroll }
			f := &fakeProvider{answer: pager(commits())}
			r.provider("git", f)
			r.run()
			r.pane.SetActive(fileA)
			r.settle()
			r.show(kit.DarkModern(), viewSize, BodyOpts{})
			r.settle()
			r.settleUI()
			calls := len(f.Calls())
			if onScroll && calls != 3 {
				t.Errorf("calls = %d: the list was not filled by scrolling to the end", calls)
			}
			if !onScroll && calls != 1 {
				t.Errorf("calls = %d: the list paged without being asked", calls)
			}
			if onScroll && len(r.pane.Rows()) != 5 {
				t.Errorf("rows = %d", len(r.pane.Rows()))
			}
		})
	}
}
