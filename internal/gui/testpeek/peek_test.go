package testpeek

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
	"github.com/rm4n0s/emetos/internal/gui/editor"
	"github.com/rm4n0s/emetos/internal/gui/kit"
	"github.com/rm4n0s/emetos/internal/gui/testutil"
)

// The peek of a failed test (step 26.3). testingOutputPeek.ts has no unit test in VS Code that runs without its workbench: 0 ported.

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

// rig is a peek over an editor whose file has messages at the lines the test sets.
type rig struct {
	t       *testing.T
	vm      *editor.ViewModel
	c       *Controller
	list    []Subject
	made    []BodySpec
	opened  []StackFrame
	changes int
	errs    []error
	noBody  bool
	focuses int
}

func newRig(t *testing.T, list ...Subject) *rig {
	t.Helper()
	r := &rig{t: t, list: list}
	doc := document.New(document.FileURI("/a_test.go"), numbered(40), document.NewOptions())
	r.vm = editor.NewViewModel(doc, testOptions(), nil)
	r.vm.SetViewportHeight(19 * 30)
	r.c = New(Config{
		Editor: r.vm,
		List:   func() []Subject { return r.list },
		NewBody: func(s BodySpec) (Body, error) {
			if r.noBody {
				return Body{}, errors.New("FakeBodyFailed", "no body")
			}
			r.made = append(r.made, s)
			return Body{Widget: widgets.NewSizedBox(nil), Dispose: func() {}}, nil
		},
		OpenFrame:   func(f StackFrame) { r.opened = append(r.opened, f) },
		FocusEditor: func() { r.focuses++ },
		OnChange:    func() { r.changes++ },
		OnError:     func(err error) { r.errs = append(r.errs, err) },
	})
	t.Cleanup(r.c.Dispose)
	return r
}

func msg(key string, line int) Subject {
	return Subject{Key: key, TestID: "t-" + key, Title: "Test " + key, Line: line, Text: "failed " + key, Failure: true}
}

func (r *rig) zones() []editor.ZoneInfo { return r.vm.Zones() }

func TestShowingAMessageMakesAZoneUnderItsLineAndMovesTheCursor(t *testing.T) {
	r := newRig(t, msg("a", 5), msg("b", 20))
	if !r.c.Show("b") {
		t.Fatal("Show did not find the message")
	}
	if !r.c.Visible() {
		t.Fatal("the peek is not visible")
	}
	zs := r.zones()
	if len(zs) != 1 || zs[0].AfterLine != 21 || zs[0].Rows < 6 {
		t.Errorf("zones: %+v, want one after line 21 of at least six rows", zs)
	}
	if p := r.vm.Cursors().Primary().Position(); p.Line != 20 {
		t.Errorf("the cursor is at %v, want line 20", p)
	}
	if cur, ok := r.c.Current(); !ok || cur.Key != "b" {
		t.Errorf("current %+v", cur)
	}
	if i, n := r.c.Position(); i != 2 || n != 2 {
		t.Errorf("position %d of %d", i, n)
	}
	if r.c.Show("none") {
		t.Error("Show found a message that is not in the list")
	}
}

func TestStepGoesRoundTheMessagesAndOpensOnTheNearestOneWhileClosed(t *testing.T) {
	r := newRig(t, msg("a", 5), msg("b", 12), msg("c", 30))
	tests := []struct {
		name   string
		cursor int
		next   bool
		want   string
	}{
		{"forward from the start", 0, true, "a"},
		{"forward after the first", 8, true, "b"},
		{"forward past the last goes to the first", 35, true, "a"},
		{"back from the middle", 15, false, "b"},
		{"back before the first goes to the last", 2, false, "c"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			r.c.Close()
			r.vm.PlaceCursor(buffer.Position{Line: tc.cursor})
			r.c.Step(tc.next)
			if cur, ok := r.c.Current(); !ok || cur.Key != tc.want {
				t.Errorf("opened on %q, want %q", cur.Key, tc.want)
			}
		})
	}
	// open: next and previous go round
	r.c.Close()
	r.c.Show("a")
	var seen []string
	for range 4 {
		r.c.Step(true)
		cur, _ := r.c.Current()
		seen = append(seen, cur.Key)
	}
	if !slices.Equal(seen, []string{"b", "c", "a", "b"}) {
		t.Errorf("next goes through %v", seen)
	}
	r.c.Step(false)
	if cur, _ := r.c.Current(); cur.Key != "a" {
		t.Errorf("previous went to %q", cur.Key)
	}
	if len(r.zones()) != 1 {
		t.Errorf("%d zones while moving, want one", len(r.zones()))
	}
}

func TestStepWithNoMessagesClosesThePeek(t *testing.T) {
	r := newRig(t, msg("a", 5))
	r.c.Show("a")
	r.list = nil
	r.c.Step(true)
	if r.c.Visible() || len(r.zones()) != 0 {
		t.Error("the peek stays with no messages")
	}
}

func TestTheSmallDiffEditorIsMadeOnlyForAMessageWithADiffAndOnlyWhenItsTextChanged(t *testing.T) {
	diff := msg("d", 5)
	diff.HasDiff, diff.Expected, diff.Actual, diff.Language = true, "a\nb", "a\nc", "go"
	r := newRig(t, diff, msg("p", 10))
	r.c.Show("d")
	if len(r.made) != 1 || r.made[0].Expected != "a\nb" || r.made[0].Actual != "a\nc" || r.made[0].Language != "go" {
		t.Fatalf("bodies made: %+v", r.made)
	}
	if r.c.Body() == nil {
		t.Error("the peek has no body for a message with a diff")
	}
	r.c.Show("d") // the same text again
	if len(r.made) != 1 {
		t.Errorf("the body was made again for the same text: %d", len(r.made))
	}
	r.c.Show("p")
	if r.c.Body() != nil {
		t.Error("a message with no diff has a body")
	}
}

func TestAFailureToMakeTheBodyIsReportedWithItsRoute(t *testing.T) {
	diff := msg("d", 5)
	diff.HasDiff, diff.Expected, diff.Actual = true, "a", "b"
	r := newRig(t, diff)
	r.noBody = true
	r.c.Show("d")
	if len(r.errs) != 1 {
		t.Fatalf("errors: %v", r.errs)
	}
	appErr, ok := errors.FromError(r.errs[0])
	if !ok || appErr.Tag != "FakeBodyFailed" {
		t.Errorf("the error is %v", r.errs[0])
	}
	if !r.c.Visible() {
		t.Error("the peek is not shown without its diff")
	}
	// a peek with no way to make the body says so
	r2 := newRig(t, diff)
	r2.c.cfg.NewBody = nil
	r2.c.Show("d")
	if len(r2.errs) != 1 {
		t.Fatalf("errors: %v", r2.errs)
	}
	appErr, ok = errors.FromError(r2.errs[0])
	if !ok {
		t.Fatalf("expected *errors.Error, got %T", r2.errs[0])
	}
	if !appErr.HasRoute("Controller.ensureBody.TestPeekNoBody") {
		t.Errorf("unexpected failure path: %s", appErr.Route())
	}
}

func TestRefreshFollowsTheMessageAndClosesWhenItGoes(t *testing.T) {
	r := newRig(t, msg("a", 5), msg("b", 12))
	r.c.Show("b")
	// the message moved to another line, and its text changed
	moved := msg("b", 14)
	moved.Text = "failed again"
	r.list = []Subject{msg("a", 5), moved}
	r.c.Refresh()
	if cur, _ := r.c.Current(); cur.Text != "failed again" {
		t.Errorf("the peek shows %q", cur.Text)
	}
	if zs := r.zones(); len(zs) != 1 || zs[0].AfterLine != 15 {
		t.Errorf("zones %+v, want one after line 15", zs)
	}
	r.list = []Subject{msg("a", 5)}
	r.c.Refresh()
	if r.c.Visible() || len(r.zones()) != 0 {
		t.Error("the peek stays after its message went")
	}
	r.c.Refresh() // closed: nothing happens
}

func TestCloseGivesTheKeyboardBackAndTellsTheListeners(t *testing.T) {
	r := newRig(t, msg("a", 5))
	notified := 0
	h := r.c.AddListener(func() { notified++ })
	r.c.Show("a")
	changes := r.changes
	r.c.Close()
	if r.focuses != 1 || r.c.Visible() || notified < 2 || r.changes != changes+1 {
		t.Errorf("focus %d, visible %v, notified %d, changes %d (was %d)", r.focuses, r.c.Visible(), notified, r.changes, changes)
	}
	r.c.Close() // closed already
	if r.focuses != 1 {
		t.Error("closing a closed peek gave the keyboard again")
	}
	r.c.RemoveListener(h)
}

func TestOutputAndFramesAreForTheWidget(t *testing.T) {
	m := msg("a", 5)
	m.Output, m.Stack = "out", []StackFrame{{Label: "f", Path: "/x.go", Line: 3, Col: 2}}
	r := newRig(t, m)
	r.c.Show("a")
	if r.c.ShowOutput() {
		t.Error("the output shows at first for a message with a text")
	}
	r.c.ToggleOutput()
	if !r.c.ShowOutput() {
		t.Error("ToggleOutput did not show the output")
	}
	r.c.Show("a") // the same message: the pane stays
	if !r.c.ShowOutput() {
		t.Error("the same message went back to the diff")
	}
	r.c.OpenFrame(m.Stack[0])
	if len(r.opened) != 1 || r.opened[0].Line != 3 {
		t.Errorf("frames opened: %+v", r.opened)
	}
	r.c.Close()
	r.c.ToggleOutput() // closed: nothing
	if r.c.ShowOutput() {
		t.Error("the output toggled while closed")
	}
}

func TestTheWidgetDrawsTheTitleTheMessageTheStackAndTheButtons(t *testing.T) {
	m := msg("a", 3)
	m.Place, m.Text = "a_test.go:4", "Divide(5, 2) = 2, want 2.5"
	m.Stack = []StackFrame{{Label: "calc.TestDivide", Path: "/a_test.go", Line: 4, Col: 2}, {Label: "testing.tRunner"}}
	r := newRig(t, m, msg("b", 9))
	r.c.cfg.Actions = func(Subject) []Action { return []Action{{Icon: 0xEB2C, Tooltip: "Run Test", Run: func() {}}} }
	r.c.Show("a")
	ts := testutil.New(t, ui.Size{Width: 700, Height: 300})
	ts.SetPlatformConfiguration(func(c *ui.PlatformConfiguration) { c.UIFontFamily, c.UIFontSize = testutil.MonoFamily, 12 })
	pumpPeek(ts, r, kit.DarkModern())
	for _, want := range []string{"Test a", "a_test.go:4 - 1 of 2", "Divide(5, 2) = 2, want 2.5", "calc.TestDivide", "/a_test.go:4:2", "testing.tRunner"} {
		if !ts.Exists(gluttertest.ByTextContaining(want, gluttertest.FindOpts{FindRichText: true})) {
			t.Errorf("the peek does not show %q", want)
		}
	}
	for _, tip := range []string{"Run Test", "Go to Next Test Failure", "Go to Previous Test Failure", "Close"} {
		if !ts.Exists(button(tip)) {
			t.Errorf("the title has no button %q", tip)
		}
	}
	if errs := ts.TakeErrors(); len(errs) != 0 {
		t.Errorf("errors: %v", errs)
	}
}

// button finds the icon button with a tooltip.
func button(tooltip string) gluttertest.Finder {
	return gluttertest.ByPredicate("the button "+tooltip, func(w widgets.Widget) bool {
		b, ok := w.(*kit.IconButton)
		return ok && b.Tooltip() == tooltip
	})
}

// pumpPeek shows the editor of the rig, which has the peek in a zone.
func pumpPeek(ts *gluttertest.Tester, r *rig, colors kit.Colors) {
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
