package quickdiffpeek

import (
	"context"
	"fmt"
	"slices"
	"strings"
	"testing"

	"github.com/rm4n0s/errors"
	"github.com/rm4n0s/glutter/animation"
	"github.com/rm4n0s/glutter/foundation"
	"github.com/rm4n0s/glutter/gluttertest"
	"github.com/rm4n0s/glutter/ui"
	"github.com/rm4n0s/glutter/widgets"

	"github.com/rm4n0s/emetos/internal/core/buffer"
	"github.com/rm4n0s/emetos/internal/core/document"
	"github.com/rm4n0s/emetos/internal/core/scm/quickdiff"
	"github.com/rm4n0s/emetos/internal/gui/editor"
	"github.com/rm4n0s/emetos/internal/gui/kit"
	"github.com/rm4n0s/emetos/internal/gui/kit/fonts"
	"github.com/rm4n0s/emetos/internal/gui/testutil"
)

// The peek of the changes (step 24.12). quickDiffWidget.ts has no unit test in VS Code: 0 ported, 0 skipped.

// fakeModel is a quick diff model whose result the test sets.
type fakeModel struct {
	res       quickdiff.Result
	originals map[string]string
	fns       []func()
}

func (m *fakeModel) Result() quickdiff.Result { return m.res }
func (m *fakeModel) OnChange(fn func()) func() {
	m.fns = append(m.fns, fn)
	i := len(m.fns) - 1
	return func() { m.fns[i] = nil }
}
func (m *fakeModel) OriginalText(id string) (string, bool) { s, ok := m.originals[id]; return s, ok }

// set replaces the result and tells the followers.
func (m *fakeModel) set(changes ...quickdiff.ProviderChange) {
	m.res.Changes = changes
	for _, fn := range m.fns {
		if fn != nil {
			fn()
		}
	}
}

// mod is a modified block of the provider.
func mod(provider string, first, last int) quickdiff.ProviderChange {
	return quickdiff.ProviderChange{ProviderID: provider, Change: quickdiff.Change{OriginalStart: first, OriginalEnd: last, ModifiedStart: first, ModifiedEnd: last}}
}

// fakeBody counts what the controller does with the body.
type fakeBody struct {
	made, disposed, revealed []string
	focused                  bool
	focusCalls               int
}

type peekRig struct {
	t       *testing.T
	doc     *document.Document
	vm      *editor.ViewModel
	model   *fakeModel
	c       *Controller
	body    *fakeBody
	focuses int
	errs    []error
	actions func(Shown) []Action
	noBody  bool
}

func testOptions() editor.Options {
	return editor.Options{
		FontFamilies: []string{testutil.MonoFamily, "monospace"}, FontSize: 14, LineHeight: 19, TabSize: 4,
		LineNumbers: editor.LineNumbersOn, LineNumbersMinChars: 5, GlyphMargin: true, LineDecorationsWidth: 10,
		ScrollBeyondLastLine: true, RenderLineHighlight: "line", RoundedSelection: true, CursorBlinking: "solid",
		MultiCursorModifier: "alt", MouseStyle: "text", MouseWheelScrollSensitivity: 1, FastScrollSensitivity: 5,
		DragAndDrop: true, MultiCursorPaste: "spread", EmptySelectionClipboard: true, FontWeight: 400, CursorStyle: "line",
		FontFeatures: []editor.FontFeature{{Tag: "liga"}, {Tag: "calt"}},
		FindLoop:     true,
	}
}

func numbered(n int) string {
	var b strings.Builder
	for i := 1; i <= n; i++ {
		fmt.Fprintf(&b, "line %d\n", i)
	}
	return b.String()
}

func newPeekRig(t *testing.T, providers []quickdiff.ProviderInfo, changes ...quickdiff.ProviderChange) *peekRig {
	t.Helper()
	r := &peekRig{t: t, body: &fakeBody{}}
	r.doc = document.New(document.FileURI("/a.go"), numbered(20), document.NewOptions())
	r.vm = editor.NewViewModel(r.doc, testOptions(), nil)
	r.vm.SetViewportHeight(19 * 30)
	r.model = &fakeModel{res: quickdiff.Result{Providers: providers, Changes: changes}, originals: map[string]string{}}
	for _, p := range providers {
		r.model.originals[p.ID] = "original of " + p.ID
	}
	r.c = New(Config{
		Editor: r.vm, Model: r.model, Name: func() string { return "a.go" },
		NewBody: func(s BodySpec) (Body, error) {
			if r.noBody {
				return Body{}, nil
			}
			r.body.made = append(r.body.made, s.ProviderID+":"+s.Text)
			return Body{
				Widget: widgets.NewBuilder(func(bc widgets.BuildContext) widgets.Widget {
					return widgets.NewColoredBox(kit.ThemeOf(bc).EditorBackground, widgets.NewSizedBox(nil))
				}),
				Reveal:   func(c quickdiff.Change) { r.body.revealed = append(r.body.revealed, fmt.Sprint(c.ModifiedStart)) },
				Focus:    func() { r.body.focused = true; r.body.focusCalls++ },
				HasFocus: func() bool { return r.body.focused },
				Dispose:  func() { r.body.disposed = append(r.body.disposed, s.ProviderID) },
			}, nil
		},
		Actions: func(s Shown) []Action {
			if r.actions != nil {
				return r.actions(s)
			}
			return nil
		},
		FocusEditor: func() { r.focuses++; r.body.focused = false },
		OnError:     func(err error) { r.errs = append(r.errs, err) },
	})
	r.vm.SetQuickDiffPeeker(r.c)
	t.Cleanup(r.c.Dispose)
	return r
}

func one() []quickdiff.ProviderInfo {
	return []quickdiff.ProviderInfo{{ID: "p1", Label: "Git Local Changes (Working Tree)", Original: "git:/a.go"}}
}

// zones are the lines the zones of the editor come after.
func (r *peekRig) zones() []int {
	var out []int
	for _, z := range r.vm.Zones() {
		out = append(out, z.AfterLine)
	}
	return out
}

func (r *peekRig) cursorAt(line int) {
	r.t.Helper()
	r.vm.Cursors().SetSelections([]buffer.Selection{{Anchor: buffer.Position{Line: line - 1}, Active: buffer.Position{Line: line - 1}}})
}

func TestStepOpensAtTheChangeAfterTheCursorAndGoesRound(t *testing.T) {
	r := newPeekRig(t, one(), mod("p1", 3, 4), mod("p1", 8, 8), mod("p1", 15, 16))
	r.cursorAt(6)
	if r.c.Visible() || r.c.Index() != -1 {
		t.Fatal("the peek is open before anything")
	}
	steps := []struct {
		next  bool
		index int
		after int
	}{{true, 1, 8}, {true, 2, 16}, {true, 0, 4}, {false, 2, 16}, {false, 1, 8}}
	for i, s := range steps {
		r.c.Step(s.next)
		if !r.c.Visible() || r.c.Index() != s.index || !slices.Equal(r.zones(), []int{s.after}) {
			t.Errorf("step %d: visible %v, index %d, zones %v; want index %d, zone after %d", i, r.c.Visible(), r.c.Index(), r.zones(), s.index, s.after)
		}
	}
	if got := r.vm.Cursors().Primary().Position().Line + 1; got != 8 {
		t.Errorf("the cursor is at line %d, want the line of the change", got)
	}
	if got := len(r.body.made); got != 1 {
		t.Errorf("%d bodies were made for one provider", got)
	}
	if want := []string{"8", "15", "3", "15", "8"}; !slices.Equal(r.body.revealed, want) {
		t.Errorf("revealed %v, want %v", r.body.revealed, want)
	}
	shown, ok := r.c.Shown()
	if !ok || shown.Index != 1 || len(shown.Changes) != 3 || shown.Total != 3 || shown.Label != "Git Local Changes (Working Tree)" {
		t.Errorf("shown %+v, %v", shown, ok)
	}
	if got := detail(shown); got != "Git Local Changes (Working Tree) - 2 of 3 changes" {
		t.Errorf("the heading is %q", got)
	}
	// going back from the line of the cursor opens at the change before it
	r.c.Close()
	r.cursorAt(10)
	r.c.Step(false)
	if r.c.Index() != 1 {
		t.Errorf("Shift+Alt+F3 at line 10 opened change %d, want 1", r.c.Index())
	}
}

func TestStepWithoutChangesOrWithOneStaysPut(t *testing.T) {
	r := newPeekRig(t, one())
	r.c.Step(true)
	if r.c.Visible() {
		t.Error("the peek opened without a change")
	}
	r.model.set(mod("p1", 5, 5))
	r.c.Step(true)
	if !r.c.Visible() {
		t.Fatal("the peek did not open on the only change")
	}
	if r.c.CanNavigate() {
		t.Error("the peek can move with one change")
	}
	r.c.Step(true)
	r.c.Step(false)
	if r.c.Index() != 0 || len(r.zones()) != 1 {
		t.Errorf("index %d, zones %v", r.c.Index(), r.zones())
	}
	if detail(mustShown(t, r.c)) != "Git Local Changes (Working Tree) - 1 of 1 change" {
		t.Errorf("the heading is %q", detail(mustShown(t, r.c)))
	}
}

func mustShown(t *testing.T, c *Controller) Shown {
	t.Helper()
	s, ok := c.Shown()
	if !ok {
		t.Fatal("nothing is shown")
	}
	return s
}

func TestTheArrowsMoveOnlyAmongTheChangesOfTheProvider(t *testing.T) {
	providers := []quickdiff.ProviderInfo{{ID: "p1", Label: "One", Original: "o1"}, {ID: "p2", Label: "Two", Original: "o2"}}
	r := newPeekRig(t, providers, mod("p1", 2, 2), mod("p2", 5, 5), mod("p1", 9, 9), mod("p2", 12, 12))
	r.c.Click(2)
	if r.c.ProviderID() != "p1" || r.c.Index() != 0 {
		t.Fatalf("provider %q, index %d", r.c.ProviderID(), r.c.Index())
	}
	r.c.Step(true)
	if r.c.Index() != 2 || r.c.ProviderID() != "p1" {
		t.Errorf("provider %q, index %d after next, want the next change of p1", r.c.ProviderID(), r.c.Index())
	}
	r.c.Step(true)
	if r.c.Index() != 0 {
		t.Errorf("index %d, want the first change of p1 again", r.c.Index())
	}
	// a click on another provider's change while the peek is open stays in the provider of the peek
	r.c.Click(5)
	if r.c.ProviderID() != "p1" {
		t.Errorf("provider %q after a click on a change of p2", r.c.ProviderID())
	}
	s := mustShown(t, r.c)
	if s.Total != 4 || len(s.Changes) != 2 || s.Label != "One" {
		t.Errorf("shown %+v", s)
	}
	// opening on the other provider's change from a closed peek makes a body for it
	r.c.Close()
	r.c.Click(12)
	if r.c.ProviderID() != "p2" || len(r.body.made) != 2 || r.body.made[1] != "p2:original of p2" {
		t.Errorf("provider %q, bodies %v", r.c.ProviderID(), r.body.made)
	}
}

func TestClickOpensTheChangeAndClosesItAgain(t *testing.T) {
	r := newPeekRig(t, one(), mod("p1", 3, 5), mod("p1", 10, 10))
	r.c.Click(7)
	if r.c.Visible() {
		t.Error("a click on a line no change touches opened the peek")
	}
	r.c.Click(4)
	if r.c.Index() != 0 || !slices.Equal(r.zones(), []int{5}) {
		t.Fatalf("index %d, zones %v", r.c.Index(), r.zones())
	}
	r.c.Click(10)
	if r.c.Index() != 1 || !slices.Equal(r.zones(), []int{10}) {
		t.Errorf("a click on the other change: index %d, zones %v", r.c.Index(), r.zones())
	}
	r.c.Click(10)
	if r.c.Visible() || len(r.zones()) != 0 {
		t.Errorf("a click on the change shown did not close the peek: %v, %v", r.c.Visible(), r.zones())
	}
}

func TestTheZoneIsAsTallAsTheChangeAndNeverMoreThanAThirdOfTheEditor(t *testing.T) {
	r := newPeekRig(t, one(), mod("p1", 2, 2), mod("p1", 5, 18))
	r.c.Click(2)
	got := r.vm.Zones()[0].Rows
	if want := mod("p1", 2, 2).Height() + 2 + 6; got != min(want, 30/3) {
		t.Errorf("a change of one line: %d rows", got)
	}
	r.c.Step(true)
	if got := r.vm.Zones()[0].Rows; got != 30/3 {
		t.Errorf("a change of 14 lines: %d rows, want a third of the 30 the editor shows", got)
	}
	r.vm.SetViewportHeight(19 * 6)
	r.c.Step(true)
	if got := r.vm.Zones()[0].Rows; got != 5 {
		t.Errorf("in a small editor: %d rows, want the least, 5", got)
	}
}

func TestThePeekFollowsTheModelAndGoesWithTheLastChange(t *testing.T) {
	r := newPeekRig(t, one(), mod("p1", 3, 3), mod("p1", 10, 10))
	r.c.Click(10)
	// an edit above moved the change down; the model says so
	r.model.set(mod("p1", 3, 3), mod("p1", 12, 12))
	if r.c.Index() != 1 || !slices.Equal(r.zones(), []int{12}) {
		t.Errorf("index %d, zones %v; want the peek at the change that moved", r.c.Index(), r.zones())
	}
	// the change the peek showed was undone: the peek goes to the next one in the text, as VS Code's does
	r.model.set(mod("p1", 3, 3))
	if !r.c.Visible() || r.c.Index() != 0 {
		t.Errorf("visible %v, index %d", r.c.Visible(), r.c.Index())
	}
	if len(r.body.made) != 1 {
		t.Errorf("the body was made again: %v", r.body.made)
	}
	// the original changed (an index update): the body is made again for the new text
	r.model.originals["p1"] = "new original"
	r.model.set(mod("p1", 3, 4))
	if len(r.body.made) != 2 || r.body.made[1] != "p1:new original" || !slices.Equal(r.body.disposed, []string{"p1"}) {
		t.Errorf("bodies %v, disposed %v", r.body.made, r.body.disposed)
	}
	r.model.set()
	if r.c.Visible() || len(r.zones()) != 0 {
		t.Errorf("the peek stays with no change: %v %v", r.c.Visible(), r.zones())
	}
	if r.focuses == 0 {
		t.Error("the editor did not get the keyboard back")
	}
}

func TestThePeekFollowsAnEditOfTheDocument(t *testing.T) {
	r := newPeekRig(t, one(), mod("p1", 6, 6))
	r.c.Click(6)
	if _, err := r.doc.Edit([]document.Edit{{Range: buffer.Range{}, Text: "new first line\nanother\n"}}, document.EditOptions{}); err != nil {
		t.Fatal(err)
	}
	if !slices.Equal(r.zones(), []int{8}) {
		t.Errorf("the zone is after line %v, want 8: it moves with the text", r.zones())
	}
}

func TestAChangeKeepsTheKeyboardOfTheBodyAndTheToggleSwitchesBetween(t *testing.T) {
	r := newPeekRig(t, one(), mod("p1", 3, 3), mod("p1", 10, 10))
	r.c.ToggleFocus() // nothing open
	if r.body.focusCalls != 0 {
		t.Error("the toggle focused a body that is not there")
	}
	r.c.Click(3)
	r.c.ToggleFocus()
	if !r.body.focused {
		t.Fatal("the toggle did not focus the small editor")
	}
	// a change while the user works in the small editor does not move the peek
	r.model.set(mod("p1", 2, 2), mod("p1", 10, 10))
	if r.c.Index() != 0 || !slices.Equal(r.zones(), []int{2}) {
		t.Errorf("index %d, zones %v", r.c.Index(), r.zones())
	}
	before := r.focuses
	r.c.ToggleFocus()
	if r.body.focused || r.focuses != before+1 {
		t.Errorf("the toggle did not give the keyboard to the editor: %v, %d", r.body.focused, r.focuses)
	}
}

func TestCloseAndDisposeLetGoOfTheZoneAndTheBody(t *testing.T) {
	r := newPeekRig(t, one(), mod("p1", 3, 3))
	var notified int
	h := r.c.AddListener(func() { notified++ })
	r.c.Click(3)
	if notified == 0 || r.c.Body() == nil || r.c.Title() != "a.go" {
		t.Errorf("notified %d, body %v, title %q", notified, r.c.Body(), r.c.Title())
	}
	if !r.vm.QuickDiffPeekVisible() {
		t.Error("the editor does not know about the peek")
	}
	r.c.Close()
	r.c.Close() // closing a closed peek is nothing
	if r.vm.QuickDiffPeekVisible() {
		t.Error("the editor still knows a peek")
	}
	if r.c.Body() != nil || r.c.Visible() || len(r.zones()) != 0 || !slices.Equal(r.body.disposed, []string{"p1"}) {
		t.Errorf("body %v, visible %v, zones %v, disposed %v", r.c.Body(), r.c.Visible(), r.zones(), r.body.disposed)
	}
	r.c.RemoveListener(h)
	r.c.Click(3)
	r.c.Dispose()
	focuses := r.focuses
	r.model.set(mod("p1", 3, 3))
	if r.c.Visible() || r.focuses != focuses {
		t.Errorf("a disposed peek follows the model: %v, %d", r.c.Visible(), r.focuses-focuses)
	}
}

func TestTheActionsOfTheTitleRunAndClose(t *testing.T) {
	r := newPeekRig(t, one(), mod("p1", 3, 3))
	var ran []string
	r.actions = func(s Shown) []Action {
		return []Action{{Icon: fonts.CodiconAdd, Tooltip: "Stage Change", Run: func() { ran = append(ran, fmt.Sprint("stage ", s.Index)) }}}
	}
	r.c.Click(3)
	acts := r.c.Actions()
	if len(acts) != 1 || acts[0].Tooltip != "Stage Change" {
		t.Fatalf("actions %+v", acts)
	}
	ts := testutil.New(t, ui.Size{Width: 700, Height: 400})
	pumpPeek(ts, r, kit.DarkModern())
	ts.Tap(button("Stage Change"), gluttertest.GestureOpts{Kind: ui.PointerDeviceKindMouse})
	ts.Pump()
	if !slices.Equal(ran, []string{"stage 0"}) || r.c.Visible() {
		t.Errorf("ran %v, visible %v", ran, r.c.Visible())
	}
}

func TestTheArrowsAndTheCloseButtonOfTheTitleWork(t *testing.T) {
	r := newPeekRig(t, one(), mod("p1", 3, 3), mod("p1", 10, 10))
	r.c.Click(3)
	ts := testutil.New(t, ui.Size{Width: 700, Height: 400})
	pumpPeek(ts, r, kit.DarkModern())
	mouse := gluttertest.GestureOpts{Kind: ui.PointerDeviceKindMouse}
	ts.Tap(button("Show Next Change"), mouse)
	ts.Pump()
	if r.c.Index() != 1 {
		t.Errorf("next: index %d", r.c.Index())
	}
	ts.Tap(button("Show Previous Change"), mouse)
	ts.Pump()
	if r.c.Index() != 0 {
		t.Errorf("previous: index %d", r.c.Index())
	}
	ts.Tap(button("Close"), mouse)
	ts.Pump()
	if r.c.Visible() {
		t.Error("the close button left the peek open")
	}
}

func TestABodyThatCannotBeMadeLeavesNoPeek(t *testing.T) {
	tests := []struct {
		name  string
		cfg   func(*Config)
		route string
	}{
		{"no function", func(c *Config) { c.NewBody = nil }, "Controller.ensureBody.QuickDiffPeekNoBody"},
		{"a function that fails", func(c *Config) {
			c.NewBody = func(BodySpec) (Body, error) { return Body{}, errors.New("TestBodyFailed", "no body") }
		}, "TestBodyFailed"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			r := newPeekRig(t, one(), mod("p1", 3, 3))
			cfg := r.c.cfg
			tt.cfg(&cfg)
			c := New(cfg)
			defer c.Dispose()
			c.Click(3)
			if c.Visible() || len(r.zones()) != 0 {
				t.Errorf("visible %v, zones %v", c.Visible(), r.zones())
			}
			if len(r.errs) != 1 {
				t.Fatalf("%d errors", len(r.errs))
			}
			appErr, ok := errors.FromError(r.errs[0])
			if !ok || !appErr.HasRoute(tt.route) {
				t.Errorf("unexpected failure %v", r.errs[0])
			}
		})
	}
}

func TestTheZoneOfAViewThatIsGoneReportsTheError(t *testing.T) {
	r := newPeekRig(t, one(), mod("p1", 3, 3))
	r.c.Click(3)
	if err := r.vm.RemoveZone(r.vm.Zones()[0].ID); err != nil { // the editor lost the zone behind the peek's back
		t.Fatal(err)
	}
	r.c.Close()
	if len(r.errs) == 0 {
		t.Error("removing a zone that is not there was not reported")
	}
}

// pumpPeek shows the editor of the rig, which has the peek in a zone.
func pumpPeek(ts *gluttertest.Tester, r *peekRig, colors kit.Colors) {
	ts.PumpWidget(kit.NewTheme(colors, widgets.NewWidgetsApp(colors.EditorBackground, widgets.WidgetsAppOpts{
		DebugShowCheckedModeBanner: foundation.Some(false),
		OnGenerateRoute: func(s widgets.RouteSettings) widgets.Route {
			return widgets.NewPageRouteBuilder(
				func(widgets.BuildContext, animation.Animation[float64], animation.Animation[float64]) widgets.Widget {
					return editor.NewEditor(r.vm, editor.EditorOpts{Context: context.Background()})
				},
				widgets.PageRouteBuilderOpts{PageRouteOpts: widgets.PageRouteOpts{RouteOpts: widgets.RouteOpts{Settings: s}}})
		},
	}), kit.ThemeOpts{UIFont: testutil.MonoFamily}))
}

// button finds the icon button with a tooltip.
func button(tooltip string) gluttertest.Finder {
	return gluttertest.ByPredicate("the button "+tooltip, func(w widgets.Widget) bool {
		b, ok := w.(*kit.IconButton)
		return ok && b.Tooltip() == tooltip
	})
}

func TestThePeekLooksLikeThePeekOfVSCode(t *testing.T) {
	tests := []struct {
		name   string
		change quickdiff.ProviderChange
		light  bool
	}{
		{"modified_dark", mod("p1", 6, 8), false},
		{"modified_light", mod("p1", 6, 8), true},
		{"added_dark", quickdiff.ProviderChange{ProviderID: "p1", Change: quickdiff.Change{OriginalStart: 5, OriginalEnd: 0, ModifiedStart: 6, ModifiedEnd: 7}}, false},
		{"deleted_light", quickdiff.ProviderChange{ProviderID: "p1", Change: quickdiff.Change{OriginalStart: 6, OriginalEnd: 7, ModifiedStart: 5, ModifiedEnd: 0}}, true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			r := newPeekRig(t, one(), mod("p1", 2, 2), tt.change, mod("p1", 15, 15))
			r.actions = func(Shown) []Action {
				return []Action{
					{Icon: fonts.CodiconDiscard, Tooltip: "Revert Change"},
					{Icon: fonts.CodiconAdd, Tooltip: "Stage Change"},
				}
			}
			colors := kit.DarkModern()
			if tt.light {
				colors = kit.LightModern()
			}
			ts := testutil.New(t, ui.Size{Width: 640, Height: 380})
			pumpPeek(ts, r, colors)
			r.c.Click(tt.change.ModifiedStart)
			if !r.c.Visible() {
				t.Fatal("no peek")
			}
			ts.Pump()
			ts.Pump()
			testutil.MatchesGolden(ts, "peek_"+tt.name)
		})
	}
}
