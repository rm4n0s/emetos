package multidiff

import (
	"fmt"
	"strconv"
	"testing"
	"time"

	"github.com/rm4n0s/glutter/animation"
	"github.com/rm4n0s/glutter/foundation"
	"github.com/rm4n0s/glutter/gluttertest"
	"github.com/rm4n0s/glutter/painting"
	"github.com/rm4n0s/glutter/services"
	"github.com/rm4n0s/glutter/ui"
	"github.com/rm4n0s/glutter/widgets"

	"github.com/rm4n0s/emetos/internal/gui/kit"
	"github.com/rm4n0s/emetos/internal/gui/kit/fonts"
	"github.com/rm4n0s/emetos/internal/gui/testutil"
)

// probe is a body that counts itself while it is built: alive holds the sections whose body exists.
type probe struct {
	widgets.StatefulWidget
	i     int
	alive map[int]int
}

func (p *probe) CreateState() widgets.State { return &probeState{} }

type probeState struct{ widgets.StateOf[*probe] }

func (s *probeState) InitState() { s.Widget().alive[s.Widget().i]++ }

func (s *probeState) Dispose() {
	w := s.Widget()
	if w.alive[w.i]--; w.alive[w.i] == 0 {
		delete(w.alive, w.i)
	}
}

func (s *probeState) Build(bc widgets.BuildContext) widgets.Widget {
	w := s.Widget()
	return widgets.NewColoredBox(kit.ThemeOf(bc).EditorBackground, widgets.NewCenter(widgets.NewText("body "+strconv.Itoa(w.i),
		widgets.TextOpts{Style: kit.TextStyleOf(bc, 13, kit.ThemeOf(bc).EditorForeground)})))
}

type wrig struct {
	t      *testing.T
	ts     *gluttertest.Tester
	m      *Model
	alive  map[int]int
	opts   WidgetOpts
	colors kit.Colors
	toggle []int
	run    []string
}

func newWRig(t *testing.T, n int, body float64, size ui.Size, colors kit.Colors) *wrig {
	t.Helper()
	testutil.Isolate(t)
	r := &wrig{t: t, m: NewModel(100), alive: map[int]int{}, colors: colors}
	secs := sections(n)
	for i := range secs {
		secs[i].Letter, secs[i].Color, secs[i].Tooltip = "M", "gitDecoration.modifiedResourceForeground", "Modified"
	}
	if n > 2 {
		secs[2].Letter, secs[2].Color, secs[2].Tooltip = "U", "gitDecoration.untrackedResourceForeground", "Untracked"
	}
	r.m.SetSections(secs)
	for i := range n {
		r.m.SetBodyHeight(i, body)
	}
	r.opts = WidgetOpts{
		Body: func(_ widgets.BuildContext, i int) widgets.Widget {
			return &probe{StatefulWidget: widgets.NewStatefulWidgetBase(), i: i, alive: r.alive}
		},
		Actions: func(_ widgets.BuildContext, i int) []Action {
			return []Action{{Icon: fonts.CodiconAdd, Tooltip: "Stage Changes", Run: func() { r.run = append(r.run, "stage "+strconv.Itoa(i)) }}}
		},
		Message:  func() string { return "There are no changes to show." },
		OnToggle: func(i int) { r.toggle = append(r.toggle, i) },
	}
	r.ts = testutil.New(t, size)
	return r
}

func (r *wrig) pump() {
	r.t.Helper()
	r.ts.PumpWidget(kit.NewTheme(r.colors, widgets.NewWidgetsApp(r.colors.EditorBackground, widgets.WidgetsAppOpts{
		DebugShowCheckedModeBanner: foundation.Some(false),
		OnGenerateRoute: func(s widgets.RouteSettings) widgets.Route {
			return widgets.NewPageRouteBuilder(
				func(widgets.BuildContext, animation.Animation[float64], animation.Animation[float64]) widgets.Widget {
					return NewWidget(r.m, r.opts)
				},
				widgets.PageRouteBuilderOpts{PageRouteOpts: widgets.PageRouteOpts{RouteOpts: widgets.RouteOpts{Settings: s}}})
		},
	}), kit.ThemeOpts{UIFont: testutil.MonoFamily}))
	r.settle()
}

func (r *wrig) settle() {
	r.t.Helper()
	for range 3 {
		r.ts.Pump(time.Millisecond)
		r.ts.FlushPosted()
	}
	r.ts.Pump(time.Millisecond)
}

func (r *wrig) noErrors() {
	r.t.Helper()
	if errs := r.ts.TakeErrors(); len(errs) != 0 {
		r.t.Errorf("the framework reported errors: %v", errs)
	}
}

// aliveRange is the lowest and the highest section with a body, and how many there are.
func (r *wrig) aliveRange() (lo, hi, n int) {
	lo, hi = 1<<30, -1
	for i := range r.alive {
		lo, hi = min(lo, i), max(hi, i)
		n++
	}
	return lo, hi, n
}

// With 2,000 sections only the ones near the viewport have a body, at the top, in the middle and at the end.
func TestOnlyTheSectionsNearTheViewportAreBuilt(t *testing.T) {
	r := newWRig(t, 2000, 100, ui.Size{Width: 800, Height: 500}, kit.DarkModern())
	r.pump()
	h := HeaderHeight + 100
	tests := []struct {
		name    string
		section int
		// the sections in view are [first, last]; the widget builds those and the ones within the overscan
		first, last int
	}{
		{"top", 0, 0, 3},
		{"middle", 1000, 1000, 1003},
		{"end", 1999, 1999 - 3, 1999},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			r.m.ScrollToSection(tt.section)
			r.settle()
			lo, hi, n := r.aliveRange()
			slack := int(DefaultOverscan/h) + 1
			if n == 0 || lo < tt.first-slack || hi > tt.last+slack || n > 4+2*slack {
				t.Errorf("bodies %d..%d (%d of them), want about %d..%d", lo, hi, n, tt.first, tt.last)
			}
			if lo > tt.first || hi < tt.last {
				t.Errorf("bodies %d..%d do not cover the viewport %d..%d", lo, hi, tt.first, tt.last)
			}
		})
	}
	r.noErrors()
	if r.m.Len() != 2000 {
		t.Errorf("Len = %d", r.m.Len())
	}
}

func TestScrollingKeepsTheBodiesOfTheSectionsThatStay(t *testing.T) {
	r := newWRig(t, 50, 100, ui.Size{Width: 800, Height: 500}, kit.DarkModern())
	created := 0
	body := r.opts.Body
	r.opts.Body = func(bc widgets.BuildContext, i int) widgets.Widget {
		return body(bc, i)
	}
	r.pump()
	before := map[int]int{}
	for i, c := range r.alive {
		before[i] = c
	}
	r.m.scrollTo(r.m.ScrollTop() + 20)
	r.settle()
	for i := range before {
		if r.alive[i] != 1 {
			t.Errorf("section %d lost its body for a small scroll (%d)", i, r.alive[i])
		}
	}
	_ = created
	r.noErrors()
}

func TestTheWheelScrollsTheSections(t *testing.T) {
	r := newWRig(t, 50, 100, ui.Size{Width: 800, Height: 500}, kit.DarkModern())
	r.pump()
	r.ts.ScrollAt(ui.Offset{Dx: 400, Dy: 200}, ui.Offset{Dx: 0, Dy: 106})
	for range 20 {
		r.ts.Pump(20 * time.Millisecond)
	}
	if got := r.m.ScrollTop(); got < 90 || got > 120 {
		t.Errorf("two notches of the wheel scrolled to %v", got)
	}
	r.noErrors()
}

func TestAClickOnAHeaderTogglesItsSection(t *testing.T) {
	r := newWRig(t, 4, 100, ui.Size{Width: 800, Height: 500}, kit.DarkModern())
	r.pump()
	before := r.m.Total()
	r.ts.Tap(gluttertest.ByText("f1.go"))
	r.settle()
	if !r.m.Collapsed(1) || r.m.Total() != before-100 || len(r.toggle) != 1 || r.toggle[0] != 1 || r.m.Focused() != 1 {
		t.Errorf("collapsed %v total %v toggles %v focused %d", r.m.Collapsed(1), r.m.Total(), r.toggle, r.m.Focused())
	}
	if _, _, n := r.aliveRange(); n != 3 {
		t.Errorf("a collapsed section has no body: %d bodies", n)
	}
	r.ts.Tap(gluttertest.ByText("f1.go"))
	r.settle()
	if r.m.Collapsed(1) {
		t.Errorf("the second click opens it")
	}
	r.noErrors()
}

func TestTheActionsOfAHeaderRun(t *testing.T) {
	r := newWRig(t, 3, 100, ui.Size{Width: 800, Height: 500}, kit.DarkModern())
	r.pump()
	buttons := gluttertest.ByType[*kit.IconButton]()
	if r.ts.Count(buttons) != 3 {
		t.Fatalf("%d action buttons, want 3", r.ts.Count(buttons))
	}
	r.ts.TapAt(r.ts.Center(gluttertest.At(buttons, 1)))
	r.settle()
	if fmt.Sprint(r.run) != "[stage 1]" || r.m.Collapsed(1) {
		t.Errorf("run %v collapsed %v: a button of the header does not toggle the section", r.run, r.m.Collapsed(1))
	}
	r.noErrors()
}

func TestTheKeysMoveBetweenTheFiles(t *testing.T) {
	r := newWRig(t, 10, 100, ui.Size{Width: 800, Height: 300}, kit.DarkModern())
	node := widgets.NewFocusNode(widgets.FocusNodeOpts{})
	t.Cleanup(node.Dispose)
	r.opts.FocusNode = node
	r.pump()
	node.RequestFocus()
	r.settle()
	h := HeaderHeight + 100
	key := func(k services.LogicalKeyboardKey) {
		r.ts.SendKeyEvent(k)
		r.settle()
	}
	key(services.LogicalKeyboardKeyArrowDown)
	key(services.LogicalKeyboardKeyArrowDown)
	if r.m.Focused() != 2 {
		t.Errorf("focused %d, want 2 (the first Down starts from the section in view)", r.m.Focused())
	}
	for range 2 {
		key(services.LogicalKeyboardKeyArrowDown)
	}
	if r.m.Focused() != 4 || r.m.ScrollTop() <= 0 {
		t.Errorf("focused %d scroll %v: the focused header scrolls into view", r.m.Focused(), r.m.ScrollTop())
	}
	if top := r.m.Top(4); top < r.m.ScrollTop() || top+HeaderHeight > r.m.ScrollTop()+300 {
		t.Errorf("the header of 4 is at %v, outside the viewport at %v", top, r.m.ScrollTop())
	}
	key(services.LogicalKeyboardKeyArrowLeft)
	if !r.m.Collapsed(4) {
		t.Errorf("Left collapses")
	}
	key(services.LogicalKeyboardKeyArrowLeft) // already collapsed: no second toggle
	key(services.LogicalKeyboardKeyEnter)
	if r.m.Collapsed(4) {
		t.Errorf("Enter opens a collapsed file")
	}
	key(services.LogicalKeyboardKeySpace)
	key(services.LogicalKeyboardKeyArrowRight)
	if r.m.Collapsed(4) || len(r.toggle) != 4 {
		t.Errorf("Space closed, Right opened: collapsed %v toggles %v", r.m.Collapsed(4), r.toggle)
	}
	key(services.LogicalKeyboardKeyEnd)
	if r.m.Focused() != 9 {
		t.Errorf("End: %d", r.m.Focused())
	}
	key(services.LogicalKeyboardKeyHome)
	if r.m.Focused() != 0 || r.m.ScrollTop() != 0 {
		t.Errorf("Home: %d at %v", r.m.Focused(), r.m.ScrollTop())
	}
	key(services.LogicalKeyboardKeyPageDown)
	if r.m.ScrollTop() != 300 {
		t.Errorf("PageDown scrolls a page: %v", r.m.ScrollTop())
	}
	key(services.LogicalKeyboardKeyPageUp)
	if r.m.ScrollTop() != 0 {
		t.Errorf("PageUp: %v", r.m.ScrollTop())
	}
	_ = h
	r.noErrors()
}

func TestAnEmptyEditorShowsTheMessage(t *testing.T) {
	r := newWRig(t, 0, 100, ui.Size{Width: 800, Height: 300}, kit.DarkModern())
	r.pump()
	if !r.ts.Exists(gluttertest.ByText("There are no changes to show.")) {
		t.Errorf("no message")
	}
	r.opts.Message = nil
	r.pump()
	r.noErrors()
}

func TestAMeasuredHeightMovesTheSectionsBelow(t *testing.T) {
	r := newWRig(t, 4, 100, ui.Size{Width: 800, Height: 600}, kit.DarkModern())
	r.pump()
	second := r.ts.TopLeft(gluttertest.ByText("f1.go")).Dy
	r.m.SetBodyHeight(0, 300)
	r.settle()
	if got := r.ts.TopLeft(gluttertest.ByText("f1.go")).Dy; got != second+200 {
		t.Errorf("the header of section 1 moved from %v to %v, want %v", second, got, second+200)
	}
	r.noErrors()
}

var goldenColors = []struct {
	name   string
	colors kit.Colors
}{{"dark", kit.DarkModern()}, {"light", kit.LightModern()}}

func TestGoldenHeaders(t *testing.T) {
	for _, tc := range goldenColors {
		t.Run(tc.name, func(t *testing.T) {
			r := newWRig(t, 4, 60, ui.Size{Width: 640, Height: 330}, tc.colors)
			secs := []Section{
				{Key: "a", Name: "loader.go", Folder: "src", File: "file:///src/loader.go", Letter: "M", Color: "gitDecoration.modifiedResourceForeground", Tooltip: "Modified"},
				{Key: "b", Name: "notes.md", Folder: "docs", Letter: "U", Color: "gitDecoration.untrackedResourceForeground"},
				{Key: "c", Name: "a-file-with-a-rather-long-name-that-has-to-be-cut-short.go", Folder: "internal/some/deep/folder/that/is/long", Letter: "D", Color: "gitDecoration.deletedResourceForeground"},
				{Key: "d", Name: "README.md", Letter: "A", Color: "gitDecoration.addedResourceForeground"},
			}
			r.m.SetSections(secs)
			for i := range secs {
				r.m.SetBodyHeight(i, 60)
			}
			r.m.SetCollapsed(1, true)
			r.m.SetFocused(0)
			r.pump()
			testutil.MatchesGolden(r.ts, "headers_"+tc.name)
			r.noErrors()
		})
	}
	_ = painting.NewEdgeInsetsAll
}
