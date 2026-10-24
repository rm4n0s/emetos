package editor

import (
	"strings"
	"testing"
	"time"

	"github.com/rm4n0s/glutter/gluttertest"
	"github.com/rm4n0s/glutter/ui"
	"github.com/rm4n0s/glutter/widgets"

	"github.com/rm4n0s/emetos/internal/core/buffer"
	"github.com/rm4n0s/emetos/internal/core/find"
	"github.com/rm4n0s/emetos/internal/gui/kit"
	"github.com/rm4n0s/emetos/internal/gui/kit/fonts"
	"github.com/rm4n0s/emetos/internal/gui/testutil"
)

// findRig is an editor on screen over findSample with the find widget available.
type findRig struct {
	t  *testing.T
	ts *gluttertest.Tester
	vm *ViewModel
}

func newFindRig(t *testing.T, size ui.Size, colors kit.Colors) *findRig {
	t.Helper()
	opts := goOptions()
	vm := NewViewModel(newTestDoc(findSample), opts, nil)
	ts := testutil.New(t, size)
	pumpEditorColors(t, ts, vm, EditorOpts{Autofocus: true}, colors)
	ts.Pump(time.Millisecond)
	t.Cleanup(vm.DisposeFind)
	return &findRig{t: t, ts: ts, vm: vm}
}

func (r *findRig) run(id string, args any) {
	r.t.Helper()
	if err := r.vm.RunCommand(id, args); err != nil {
		r.t.Fatalf("%s: %v", id, err)
	}
	r.ts.Pump(time.Millisecond)
	r.ts.Pump(time.Millisecond)
}

func iconButton(code int) gluttertest.Finder {
	return gluttertest.ByPredicate("icon button", func(w widgets.Widget) bool {
		b, ok := w.(*kit.IconButton)
		return ok && b.Code() == code
	})
}

func toggleWith(code int) gluttertest.Finder {
	return gluttertest.ByPredicate("toggle", func(w widgets.Widget) bool {
		b, ok := w.(*kit.Toggle)
		return ok && b.Code() == code
	})
}

func (r *findRig) tap(f gluttertest.Finder) {
	r.t.Helper()
	if !r.ts.Exists(f) {
		r.t.Fatalf("there is no %v", f)
	}
	r.ts.Tap(f)
	r.ts.Pump(time.Millisecond)
}

func (r *findRig) inputs() gluttertest.Finder { return gluttertest.ByType[*widgets.EditableText]() }

func TestTheWidgetShowsWhileFindIsOpen(t *testing.T) {
	r := newFindRig(t, ui.Size{Width: 640, Height: 300}, kit.DarkModern())
	if r.ts.Exists(r.inputs()) {
		t.Fatal("the widget shows before find is started")
	}
	cursorAt(r.vm, 0, 7)
	r.run("actions.find", nil)
	if got := r.ts.Count(r.inputs()); got != 1 {
		t.Fatalf("%d inputs: Find shows the find row only", got)
	}
	if find, _ := r.vm.findInputFocus(); !find {
		t.Error("the find input has the focus")
	}
	if r.vm.Focused() {
		t.Error("the editor still has the focus")
	}
	if !r.ts.Exists(gluttertest.ByText("1 of 5")) {
		t.Errorf("the match count shows 1 of 5 for beta")
	}
	r.run("closeFindWidget", nil)
	if r.ts.Exists(r.inputs()) {
		t.Error("the widget shows after closing")
	}
	if !r.vm.Focused() {
		t.Error("the editor does not have the focus back")
	}
	noErrors(t, r.ts)
}

func TestTypingInTheFindInputSearches(t *testing.T) {
	r := newFindRig(t, ui.Size{Width: 640, Height: 300}, kit.DarkModern())
	selectRange(r.vm, 0, 0, 1, 10)
	r.run("editor.actions.findWithArgs", map[string]any{"searchString": "x"})
	if !r.ts.Exists(gluttertest.ByText("No results")) {
		t.Error("no match for x")
	}
	r.ts.EnterText(gluttertest.First(r.inputs()), "gamma")
	st := r.vm.FindState()
	if st.SearchString() != "gamma" || st.MatchesCount() != 1 {
		t.Errorf("search %q with %d matches", st.SearchString(), st.MatchesCount())
	}
	if got := primary(r.vm); got != rng(1, 5, 1, 10) {
		t.Errorf("the cursor moves to the match while typing: %v", got)
	}
	if !r.ts.Exists(gluttertest.ByText("1 of 1")) {
		t.Error("the count shows 1 of 1")
	}
	r.ts.EnterText(gluttertest.First(r.inputs()), "")
	if r.vm.FindState().MatchesCount() != 0 || !r.ts.Exists(gluttertest.ByText("No results")) {
		t.Error("an empty search finds nothing")
	}
	noErrors(t, r.ts)
}

func TestTheStateOfTheEditorFillsTheInput(t *testing.T) {
	r := newFindRig(t, ui.Size{Width: 640, Height: 300}, kit.DarkModern())
	cursorAt(r.vm, 0, 7)
	r.run("actions.find", nil)
	editable := r.ts.Widget(gluttertest.First(r.inputs())).(*widgets.EditableText)
	_ = editable
	if got := r.vm.findw.state.SearchString(); got != "beta" {
		t.Fatalf("search %q", got)
	}
	// the strings of the state are in the controllers the widget shows
	found := false
	for _, state := range r.ts.TextInputStates() {
		if state.Text == "beta" {
			found = true
		}
	}
	if !found {
		t.Errorf("the input method does not have beta: %v", r.ts.TextInputStates())
	}
}

func TestTheButtonsOfTheWidget(t *testing.T) {
	r := newFindRig(t, ui.Size{Width: 640, Height: 300}, kit.DarkModern())
	cursorAt(r.vm, 0, 7)
	r.run("actions.find", nil)
	r.tap(iconButton(fonts.CodiconArrowDown))
	if got := primary(r.vm); got != rng(1, 0, 1, 4) {
		t.Errorf("next: %v", got)
	}
	r.tap(iconButton(fonts.CodiconArrowDown))
	r.tap(iconButton(fonts.CodiconArrowUp))
	if got := primary(r.vm); got != rng(1, 0, 1, 4) {
		t.Errorf("next, previous: %v", got)
	}
	r.tap(iconButton(fonts.CodiconClose))
	if r.vm.FindRevealed() {
		t.Error("the close button did not close")
	}
	noErrors(t, r.ts)
}

func TestTheSwitchesOfTheWidget(t *testing.T) {
	cases := []struct {
		name  string
		code  int
		check func(st *find.State) bool
	}{
		{"match case", fonts.CodiconCaseSensitive, func(st *find.State) bool { return st.MatchCase() }},
		{"whole word", fonts.CodiconWholeWord, func(st *find.State) bool { return st.WholeWord() }},
		{"regular expression", fonts.CodiconRegex, func(st *find.State) bool { return st.IsRegex() }},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			r := newFindRig(t, ui.Size{Width: 640, Height: 300}, kit.DarkModern())
			cursorAt(r.vm, 0, 7)
			r.run("actions.find", nil)
			r.tap(toggleWith(tc.code))
			if !tc.check(r.vm.FindState()) {
				t.Error("the toggle did not switch on")
			}
			if !r.ts.Widget(toggleWith(tc.code)).(*kit.Toggle).Checked() {
				t.Error("the toggle does not show it is on")
			}
			if find, _ := r.vm.findInputFocus(); !find {
				t.Error("the focus left the input")
			}
			r.tap(toggleWith(tc.code))
			if tc.check(r.vm.FindState()) {
				t.Error("the toggle did not switch off")
			}
		})
	}
}

func TestFindInSelectionButton(t *testing.T) {
	r := newFindRig(t, ui.Size{Width: 640, Height: 300}, kit.DarkModern())
	cursorAt(r.vm, 0, 7)
	r.run("actions.find", nil)
	selectRange(r.vm, 1, 0, 3, 0)
	r.ts.Pump(time.Millisecond)
	r.tap(toggleWith(fonts.CodiconListSelection))
	if len(r.vm.FindState().SearchScope()) != 1 {
		t.Errorf("scope %v", r.vm.FindState().SearchScope())
	}
	if !r.ts.Widget(toggleWith(fonts.CodiconListSelection)).(*kit.Toggle).Checked() {
		t.Error("the button does not show the scope")
	}
}

func TestTheReplaceRow(t *testing.T) {
	r := newFindRig(t, ui.Size{Width: 640, Height: 300}, kit.DarkModern())
	cursorAt(r.vm, 0, 7)
	r.run("actions.find", nil)
	r.tap(iconButton(fonts.CodiconChevronRight)) // shows the replace row
	if !r.vm.FindState().IsReplaceRevealed() || r.ts.Count(r.inputs()) != 2 {
		t.Fatalf("replace row: %v, %d inputs", r.vm.FindState().IsReplaceRevealed(), r.ts.Count(r.inputs()))
	}
	r.ts.EnterText(gluttertest.Last(r.inputs()), "BETA")
	if r.vm.FindState().ReplaceString() != "BETA" {
		t.Errorf("replace string %q", r.vm.FindState().ReplaceString())
	}
	r.tap(iconButton(fonts.CodiconReplace)) // selects the next match
	r.tap(iconButton(fonts.CodiconReplace)) // replaces it
	if got := r.vm.Document().Text(); !strings.Contains(r.vm.Document().Text(), "BETA") || strings.Count(got, "BETA") != 1 {
		t.Errorf("text %q", got)
	}
	r.tap(iconButton(fonts.CodiconReplaceAll))
	if got := strings.Count(r.vm.Document().Text(), "BETA"); got != 5 {
		t.Errorf("%d replaced", got)
	}
	r.tap(iconButton(fonts.CodiconChevronDown)) // hides it again
	if r.vm.FindState().IsReplaceRevealed() || r.ts.Count(r.inputs()) != 1 {
		t.Error("the replace row is still there")
	}
	noErrors(t, r.ts)
}

func TestReplaceIsDisabledInAReadOnlyDocument(t *testing.T) {
	r := newFindRig(t, ui.Size{Width: 640, Height: 300}, kit.DarkModern())
	r.vm.Document().SetReadOnly(true)
	cursorAt(r.vm, 0, 7)
	r.run("actions.find", nil)
	r.tap(iconButton(fonts.CodiconChevronRight))
	if r.vm.FindState().IsReplaceRevealed() {
		t.Error("the chevron works on a read-only document")
	}
}

func TestAClickInTheWidgetDoesNotMoveTheCursor(t *testing.T) {
	r := newFindRig(t, ui.Size{Width: 640, Height: 300}, kit.DarkModern())
	cursorAt(r.vm, 3, 2)
	r.run("editor.actions.findWithArgs", map[string]any{"searchString": "zzz"})
	before := r.vm.Cursors().Selections()
	r.ts.TapAt(r.ts.TopLeft(gluttertest.First(r.inputs())).Add(ui.Offset{Dx: 10, Dy: 22})) // the widget's own background
	if !buffer.SelectionsEqual(before, r.vm.Cursors().Selections()) {
		t.Errorf("selections %v, were %v", r.vm.Cursors().Selections(), before)
	}
}

func TestTheWidgetFitsANarrowEditor(t *testing.T) {
	r := newFindRig(t, ui.Size{Width: 300, Height: 200}, kit.DarkModern())
	cursorAt(r.vm, 0, 7)
	r.run("editor.action.startFindReplaceAction", nil)
	noErrors(t, r.ts)
}

func TestFindMatchesAreOnTheScreen(t *testing.T) {
	r := newFindRig(t, ui.Size{Width: 640, Height: 300}, kit.DarkModern())
	cursorAt(r.vm, 0, 7)
	r.run("actions.find", nil)
	r.run("editor.action.nextMatchFindAction", nil)
	decs := r.vm.findDecorationsIn(0, 5)
	var matches, current, lines int
	for _, d := range decs {
		switch d.Options.ClassName {
		case find.ClassFindMatch:
			matches++
		case find.ClassCurrentFindMatch:
			current++
		case find.ClassRangeHighlight:
			lines++
		}
	}
	if matches+current != 5 || current != 1 || lines != 1 {
		t.Errorf("%d matches, %d current, %d highlighted lines", matches, current, lines)
	}
}

// goldenFind opens the widget with the replace row, a regular expression and highlighted matches.
func goldenFind(t *testing.T, colors kit.Colors) *findRig {
	t.Helper()
	r := newFindRig(t, ui.Size{Width: 640, Height: 220}, colors)
	selectRange(r.vm, 0, 0, 0, 0)
	r.run("editor.actions.findWithArgs", map[string]any{
		"searchString": "b(e)ta", "replaceString": "<$1>", "isRegex": true,
	})
	r.run("editor.action.nextMatchFindAction", nil)
	r.run("editor.action.nextMatchFindAction", nil)
	return r
}

func TestGoldenFindWidgetWithReplace(t *testing.T) {
	for _, tc := range []struct{ golden, theme string }{
		{"find_replace_dark", "Dark Modern"},
		{"find_replace_light", "Light Modern"},
	} {
		t.Run(tc.theme, func(t *testing.T) {
			r := goldenFind(t, bundledColors(t, tc.theme))
			noErrors(t, r.ts)
			testutil.MatchesGolden(r.ts, tc.golden)
		})
	}
}

func TestGoldenFindWidgetNoResults(t *testing.T) {
	r := newFindRig(t, ui.Size{Width: 640, Height: 140}, kit.DarkModern())
	r.run("editor.actions.findWithArgs", map[string]any{"searchString": "nothing here", "matchWholeWord": true})
	noErrors(t, r.ts)
	testutil.MatchesGolden(r.ts, "find_no_results")
}

func TestGoldenFindInSelection(t *testing.T) {
	r := newFindRig(t, ui.Size{Width: 640, Height: 160}, kit.DarkModern())
	selectRange(r.vm, 1, 0, 2, 0)
	r.run("editor.actions.findWithArgs", map[string]any{"searchString": "beta", "findInSelection": true})
	noErrors(t, r.ts)
	testutil.MatchesGolden(r.ts, "find_in_selection")
}

// TestProofFind writes the proof image of step 5.2: the find widget with the replace row open,
// a regular expression and the matches highlighted.
func TestProofFind(t *testing.T) {
	if !testutil.ProofEnabled() {
		t.Skip("run with -proof to write the image")
	}
	r := goldenFind(t, kit.DarkModern())
	noErrors(t, r.ts)
	testutil.Proof(t, r.ts, "5.2-find.png")
}
