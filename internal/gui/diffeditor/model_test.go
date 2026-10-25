package diffeditor

import (
	"context"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/rm4n0s/errors"
	"github.com/rm4n0s/glutter/gluttertest"
	"github.com/rm4n0s/glutter/ui"

	"github.com/rm4n0s/emetos/internal/core/buffer"
	"github.com/rm4n0s/emetos/internal/core/document"
	"github.com/rm4n0s/emetos/internal/core/settings"
	"github.com/rm4n0s/emetos/internal/gui/editor"
	"github.com/rm4n0s/emetos/internal/gui/kit"
)

// typeAt inserts text at a position of a document, as typing does.
func typeAt(t *testing.T, doc *document.Document, line, col int, text string) {
	t.Helper()
	at := buffer.Position{Line: line, Col: col}
	if _, err := doc.Edit([]document.Edit{{Range: buffer.Range{Start: at, End: at}, Text: text}}, document.EditOptions{}); err != nil {
		t.Fatal(err)
	}
}

func TestTheDiffEditorHasTwoEditorsThatAreAlikeInWidth(t *testing.T) {
	r := newRig(t, "a\nb\nc\n", "a\nB\nc\n", rigConfig{})
	finder := gluttertest.ByType[*editor.Editor]()
	editors := r.ts.Widgets(finder)
	if len(editors) != 2 {
		t.Fatalf("%d editors", len(editors))
	}
	orig, mod := editors[0].(*editor.Editor), editors[1].(*editor.Editor)
	if orig.ViewModel() != r.model.View(Original) || mod.ViewModel() != r.model.View(Modified) {
		t.Error("the editors are not the model's, original on the left")
	}
	wo, wm := r.ts.Size(gluttertest.ByWidget(editors[0])).Width, r.ts.Size(gluttertest.ByWidget(editors[1])).Width
	if wo != wm || wo < 300 {
		t.Errorf("the editors are %v and %v wide", wo, wm)
	}
	// both show line numbers
	if r.model.View(Original).Options().LineNumbers != editor.LineNumbersOn || r.model.View(Modified).Options().LineNumbers != editor.LineNumbersOn {
		t.Error("line numbers are off")
	}
	r.noErrors()
}

func TestTheOriginalEditorIsReadOnlyAndTheModifiedOneEdits(t *testing.T) {
	r := newRig(t, "a\nb\nc\n", "a\nB\nc\n", rigConfig{})
	orig, mod := r.model.View(Original), r.model.View(Modified)
	if !orig.ReadOnly() || mod.ReadOnly() {
		t.Fatalf("read-only: original %v, modified %v", orig.ReadOnly(), mod.ReadOnly())
	}
	err := orig.RunCommand("type", "zz")
	if appErr, ok := errors.FromError(err); !ok || !appErr.HasRoute("EditorReadOnly") {
		t.Errorf("typing in the original: %v", err)
	}
	if r.orig.Text() != "a\nb\nc\n" || r.orig.Dirty() {
		t.Errorf("the original changed: %q", r.orig.Text())
	}
	if err := mod.RunCommand("type", "zz"); err != nil {
		t.Fatal(err)
	}
	if !r.mod.Dirty() || !strings.HasPrefix(r.mod.Text(), "zz") {
		t.Errorf("the modified text is %q (dirty %v)", r.mod.Text(), r.mod.Dirty())
	}
	r.noErrors()
}

func TestAWritableOriginalDocumentIsNotReadOnlyForOthers(t *testing.T) {
	r := newRig(t, "a\n", "b\n", rigConfig{})
	if r.orig.ReadOnly() {
		t.Error("the diff editor made the original document read-only")
	}
}

func TestAModifiedDocumentThatIsReadOnlyIsReadOnlyInTheEditor(t *testing.T) {
	r := newRig(t, "a\n", "b\n", rigConfig{readOnlyModified: true})
	if !r.model.View(Modified).ReadOnly() {
		t.Error("a read-only document is editable in the diff editor")
	}
}

func TestFillerZonesGiveBothEditorsTheSameHeight(t *testing.T) {
	// two lines are inserted, and later three are deleted
	original := "1\n2\n3\n4\n5\n6\n7\n8\n"
	modified := "1\n2\nnew a\nnew b\n3\n4\n5\n8\n"
	r := newRig(t, original, modified, rigConfig{})
	orig, mod := r.model.View(Original), r.model.View(Modified)
	if got, want := r.model.ZonesOf(Original), []Zone{{AfterLine: 2, Rows: 2}}; !equalZones(got, want) {
		t.Errorf("original zones %v, want %v", got, want)
	}
	if got, want := r.model.ZonesOf(Modified), []Zone{{AfterLine: 7, Rows: 2}}; !equalZones(got, want) {
		t.Errorf("modified zones %v, want %v", got, want)
	}
	if a, b := orig.Projection().Height(), mod.Projection().Height(); a != b {
		t.Errorf("the editors are %v and %v tall", a, b)
	}
	// the lines that are the same are on the same row of the screen: original line n and its modified line
	for _, pair := range [][2]int{{0, 0}, {1, 1}, {2, 4}, {3, 5}, {4, 6}, {7, 7}} {
		ro, rm := orig.RowOfPosition(buffer.Position{Line: pair[0]}), mod.RowOfPosition(buffer.Position{Line: pair[1]})
		if ro != rm {
			t.Errorf("original line %d is on row %d, modified line %d on row %d", pair[0]+1, ro, pair[1]+1, rm)
		}
	}
	// the zones have the filler in them
	for _, z := range orig.Zones() {
		if z.Widget == nil {
			t.Error("a filler zone has no widget")
		}
	}
	r.noErrors()
}

func equalZones(a, b []Zone) bool { return sameZones(a, b) }

func TestChangedLinesAndCharactersAreMarked(t *testing.T) {
	r := newRig(t, "keep\nold value\nkeep\ngone\nkeep\n", "keep\nnew value\nkeep\nkeep\nadded\n", rigConfig{})
	colors := ColorsOf(kit.DarkModern())
	type mark struct {
		start, end buffer.Position
		whole      bool
		bg         ui.Color
	}
	got := func(s Side) (out []mark) {
		for _, m := range r.model.View(s).BackgroundMarks() {
			out = append(out, mark{m.Range.Start, m.Range.End, m.WholeLine, m.Background})
		}
		return out
	}
	// "old value" is line 2 of the original: the characters "old" differ, and line 4 is deleted
	origMarks, modMarks := got(Original), got(Modified)
	var wholeOrig, wholeMod []mark
	for _, m := range origMarks {
		if m.whole {
			wholeOrig = append(wholeOrig, m)
		}
	}
	for _, m := range modMarks {
		if m.whole {
			wholeMod = append(wholeMod, m)
		}
	}
	if len(wholeOrig) != 2 || wholeOrig[0].start.Line != 1 || wholeOrig[0].end.Line != 1 || wholeOrig[1].start.Line != 3 || wholeOrig[0].bg != colors.RemovedLine {
		t.Errorf("original line marks %+v", wholeOrig)
	}
	if len(wholeMod) != 2 || wholeMod[0].start.Line != 1 || wholeMod[1].start.Line != 4 || wholeMod[0].bg != colors.InsertedLine {
		t.Errorf("modified line marks %+v", wholeMod)
	}
	hasChars := func(marks []mark, line int, bg ui.Color) bool {
		for _, m := range marks {
			if !m.whole && m.start.Line == line && m.end.Line == line && m.end.Col > m.start.Col && m.bg == bg {
				return true
			}
		}
		return false
	}
	if !hasChars(origMarks, 1, colors.RemovedText) || !hasChars(modMarks, 1, colors.InsertedText) {
		t.Errorf("character marks of the changed line: original %+v modified %+v", origMarks, modMarks)
	}
	// the gutter along a changed line is colored as the line is
	for _, m := range r.model.View(Modified).BackgroundMarks() {
		if m.WholeLine && m.Gutter != colors.InsertedGutter {
			t.Errorf("gutter color %v, want %v", m.Gutter, colors.InsertedGutter)
		}
	}
	r.noErrors()
}

func TestIdenticalTextsHaveNoChanges(t *testing.T) {
	r := newRig(t, "same\n", "same\n", rigConfig{})
	if !r.model.NoChanges() || len(r.model.Changes()) != 0 || r.model.HitTimeout() {
		t.Errorf("no changes %v changes %v timeout %v", r.model.NoChanges(), r.model.Changes(), r.model.HitTimeout())
	}
	if len(r.model.View(Original).Zones())+len(r.model.View(Modified).Zones()) != 0 || len(r.model.View(Modified).BackgroundMarks()) != 0 {
		t.Error("identical texts have zones or marks")
	}
	// both editors are there, plain
	if n := len(r.ts.Widgets(gluttertest.ByType[*editor.Editor]())); n != 2 {
		t.Errorf("%d editors", n)
	}
	if marks, total := r.model.RulerMarks(); len(marks) != 0 || total <= 0 {
		t.Errorf("ruler marks %v of %v", marks, total)
	}
}

func TestTheDiffIsComputedAgainAfterTheEditsStop(t *testing.T) {
	r := newRig(t, "a\nb\nc\n", "a\nb\nc\n", rigConfig{})
	if !r.model.NoChanges() {
		t.Fatal("the texts differ")
	}
	applied := r.model.Applied()
	// typing: the old diff stays while the new one waits for the edits to stop
	typeAt(t, r.mod, 1, 0, "x")
	if !r.model.Computing() || r.model.Applied() != applied || !r.model.NoChanges() {
		t.Fatalf("after one edit: computing %v applied %d no changes %v", r.model.Computing(), r.model.Applied(), r.model.NoChanges())
	}
	r.ts.Pump(DebounceDelay - 50*time.Millisecond)
	r.settle()
	typeAt(t, r.mod, 1, 1, "y") // the wait starts again
	r.ts.Pump(DebounceDelay - 50*time.Millisecond)
	r.settle()
	if r.model.Applied() != applied || !r.model.NoChanges() {
		t.Fatalf("the diff was computed before the edits stopped: applied %d", r.model.Applied())
	}
	r.ts.Pump(100 * time.Millisecond)
	r.waitDiff()
	if r.model.Applied() != applied+1 {
		t.Errorf("results applied: %d, want %d", r.model.Applied(), applied+1)
	}
	changes := r.model.Changes()
	if len(changes) != 1 || changes[0].Modified.StartLine != 2 || r.model.NoChanges() {
		t.Errorf("changes %+v", changes)
	}
	if len(r.model.View(Modified).BackgroundMarks()) == 0 || len(r.model.View(Original).BackgroundMarks()) == 0 {
		t.Error("the new changes are not marked")
	}
	r.noErrors()
}

func TestAnEditThatComesWhileTheDiffRunsCancelsIt(t *testing.T) {
	r := newRig(t, "a\nb\nc\nd\n", "a\nb\nc\nd\n", rigConfig{})
	applied := r.model.Applied()
	typeAt(t, r.mod, 0, 0, "first ")
	r.model.Recompute() // the computation starts: its result is on its way
	typeAt(t, r.mod, 2, 0, "second ")
	r.ts.Pump(DebounceDelay + 10*time.Millisecond)
	r.waitDiff()
	r.settle()
	if got := r.model.Applied(); got != applied+1 {
		t.Fatalf("%d results applied, want only the last one (%d)", got-applied, 1)
	}
	var lines []int
	for _, c := range r.model.Changes() {
		lines = append(lines, c.Modified.StartLine)
	}
	if len(lines) != 2 || lines[0] != 1 || lines[1] != 3 {
		t.Errorf("the changes are at %v, the result of the last text is lines 1 and 3", lines)
	}
	r.noErrors()
}

func TestZonesFollowTheEditsUntilTheNewDiffArrives(t *testing.T) {
	r := newRig(t, "a\nb\nc\nd\n", "a\nb\nx\ny\nc\nd\n", rigConfig{})
	if got := r.model.ZonesOf(Original); !equalZones(got, []Zone{{2, 2}}) {
		t.Fatalf("zones %v", got)
	}
	typeAt(t, r.orig, 0, 0, "top\n") // a line above, in the original
	if got := r.model.ZonesOf(Original); !equalZones(got, []Zone{{3, 2}}) {
		t.Errorf("zones after an edit above: %v", got)
	}
	r.ts.Pump(DebounceDelay + 10*time.Millisecond)
	r.waitDiff()
	// the new diff has the line that was added at the top as a change of its own
	if got := r.model.ZonesOf(Modified); !equalZones(got, []Zone{{0, 1}}) {
		t.Errorf("modified zones %v", got)
	}
	if got := r.model.ZonesOf(Original); !equalZones(got, []Zone{{3, 2}}) {
		t.Errorf("original zones %v", got)
	}
}

func TestIgnoreTrimWhitespaceIsFollowedLive(t *testing.T) {
	r := newRig(t, "a\n  b\nc\n", "a\nb\nc\n", rigConfig{})
	if !r.model.NoChanges() {
		t.Fatalf("white space counts by default: %+v", r.model.Changes())
	}
	set := func(v bool) {
		t.Helper()
		if err := r.store.Update(context.Background(), "diffEditor.ignoreTrimWhitespace", v, settings.TargetUser); err != nil {
			t.Fatal(err)
		}
		r.waitDiff()
	}
	applied := r.model.Applied()
	set(false)
	if r.model.NoChanges() || r.model.Applied() <= applied {
		t.Errorf("the change of white space is not shown: %+v (applied %d)", r.model.Changes(), r.model.Applied())
	}
	set(true)
	if !r.model.NoChanges() {
		t.Errorf("the change of white space is still shown: %+v", r.model.Changes())
	}
	r.noErrors()
}

func TestTheComputationHasATimeLimit(t *testing.T) {
	// the clock stands still until the test makes it run, 10 ms with every look at it
	var now time.Time
	var running bool
	clock := func() time.Time {
		if running {
			now = now.Add(10 * time.Millisecond)
		}
		return now
	}
	original := lines(40, func(i int) string { return strings.Repeat("a", i%7+1) + " same" })
	modified := lines(40, func(i int) string { return strings.Repeat("b", i%5+1) + " other" })
	r := newRig(t, original, modified, rigConfig{now: clock})
	if r.model.HitTimeout() {
		t.Fatal("the default limit of 5 s is hit")
	}
	set := func(ms float64) {
		t.Helper()
		applied := r.model.Applied()
		if err := r.store.Update(context.Background(), "diffEditor.maxComputationTime", ms, settings.TargetUser); err != nil {
			t.Fatal(err)
		}
		r.waitFor("a new diff", func() bool { return r.model.Applied() > applied && !r.model.Computing() })
	}
	running = true
	set(1)
	if !r.model.HitTimeout() || len(r.model.Changes()) == 0 {
		t.Errorf("a limit of 1 ms: hit %v, changes %d", r.model.HitTimeout(), len(r.model.Changes()))
	}
	// the changes of a diff that ran out of time are shown as computed: one whole block, no notice
	if got := r.model.Changes(); len(got) != 1 || got[0].Original.StartLine != 1 || got[0].Original.EndLine != 41 {
		t.Errorf("changes %+v", got)
	}
	set(0)
	if r.model.HitTimeout() {
		t.Error("no limit still hit the limit")
	}
	set(-5)
	if r.model.HitTimeout() {
		t.Error("a negative limit is no limit")
	}
	r.noErrors()
}

func TestTimeLimitOfASetting(t *testing.T) {
	for _, tc := range []struct {
		ms   float64
		want time.Duration
	}{{0, 0}, {-1, 0}, {1, time.Millisecond}, {5000, 5 * time.Second}, {0.5, 500 * time.Microsecond}} {
		if got := timeLimit(tc.ms); got != tc.want {
			t.Errorf("timeLimit(%v) = %v, want %v", tc.ms, got, tc.want)
		}
	}
}

func TestACancelledContextReportsNothing(t *testing.T) {
	r := newRig(t, "a\n", "b\n", rigConfig{})
	ctx, cancel := context.WithCancel(context.Background())
	r.model.opts.Context = ctx
	cancel()
	applied := r.model.Applied()
	r.model.Recompute()
	for range 20 {
		r.settle()
		time.Sleep(time.Millisecond)
	}
	if r.model.Applied() != applied || len(r.errs) != 0 {
		t.Errorf("applied %d (was %d), errors %v", r.model.Applied(), applied, r.errs)
	}
}

func TestModelWithoutSettingsUsesTheDefaults(t *testing.T) {
	r := newRig(t, "a\n  b\n", "a\nb\n", rigConfig{})
	r.model.opts.Settings = nil
	if o := r.model.diffOptions(); !o.IgnoreTrimWhitespace || o.MaxComputationTime != 5*time.Second {
		t.Errorf("options %+v", o)
	}
}

func TestScrollingOneEditorScrollsTheOther(t *testing.T) {
	long := lines(200, func(i int) string { return strings.Repeat("word ", 80) + string(rune('a'+i%26)) })
	r := newRig(t, long, long+"extra\n", rigConfig{size: ui.Size{Width: 800, Height: 300}})
	orig, mod := r.model.View(Original), r.model.View(Modified)
	r.settle()
	// the modified editor scrolls the original
	mod.ScrollTo(400)
	r.settle()
	if orig.ScrollTop() != 400 || mod.ScrollTop() != 400 {
		t.Fatalf("after scrolling the modified editor: original %v modified %v", orig.ScrollTop(), mod.ScrollTop())
	}
	// and the original scrolls the modified one, without the scroll coming back
	var back int
	h := mod.AddScrollListener(func() { back++ })
	defer mod.RemoveScrollListener(h)
	var origTold int
	h2 := orig.AddScrollListener(func() { origTold++ })
	defer orig.RemoveScrollListener(h2)
	orig.ScrollTo(150)
	r.settle()
	if orig.ScrollTop() != 150 || mod.ScrollTop() != 150 {
		t.Fatalf("after scrolling the original: original %v modified %v", orig.ScrollTop(), mod.ScrollTop())
	}
	if back != 1 || origTold != 1 {
		t.Errorf("a scroll told the other editor %d times and came back %d times, want once and once", back, origTold)
	}
	// sideways
	orig.SetScrollLeft(260)
	r.settle()
	if orig.ScrollLeft() != 260 || mod.ScrollLeft() != 260 {
		t.Errorf("horizontal: original %v modified %v", orig.ScrollLeft(), mod.ScrollLeft())
	}
	mod.SetScrollLeft(90)
	r.settle()
	if orig.ScrollLeft() != 90 || mod.ScrollLeft() != 90 {
		t.Errorf("horizontal back: original %v modified %v", orig.ScrollLeft(), mod.ScrollLeft())
	}
	r.noErrors()
}

func TestSelectionsAreIndependent(t *testing.T) {
	r := newRig(t, "abc\ndef\n", "abc\nxyz\n", rigConfig{})
	orig, mod := r.model.View(Original), r.model.View(Modified)
	orig.Cursors().SetSelections([]buffer.Selection{{Anchor: buffer.Position{Line: 1, Col: 0}, Active: buffer.Position{Line: 1, Col: 2}}})
	orig.SelectionsChanged()
	r.settle()
	if got := mod.Cursors().Primary().Position(); got != (buffer.Position{}) {
		t.Errorf("selecting in the original moved the modified cursor to %v", got)
	}
	mod.Cursors().SetSelections([]buffer.Selection{{Anchor: buffer.Position{Line: 0, Col: 1}, Active: buffer.Position{Line: 0, Col: 1}}})
	mod.SelectionsChanged()
	r.settle()
	if sel := orig.Cursors().Primary().Selection; sel.Active != (buffer.Position{Line: 1, Col: 2}) {
		t.Errorf("the original selection is %v", sel)
	}
}

func TestSwitchSideMapsTheSelection(t *testing.T) {
	// the original has a line the modified text does not: lines after it move up by one
	r := newRig(t, "a\nremoved\nb\nc\n", "a\nb\nc\n", rigConfig{})
	orig, mod := r.model.View(Original), r.model.View(Modified)
	orig.Cursors().SetSelections([]buffer.Selection{{Anchor: buffer.Position{Line: 3, Col: 0}, Active: buffer.Position{Line: 3, Col: 1}}})
	if got := r.model.MapToOtherSide(Original); got.Anchor != (buffer.Position{Line: 2, Col: 0}) || got.Active != (buffer.Position{Line: 2, Col: 1}) {
		t.Errorf("mapped selection %v", got)
	}
	r.model.Focus(Original).Request()
	r.settle()
	if side, ok := r.model.FocusedSide(); !ok || side != Original {
		t.Fatalf("focused side %v %v", side, ok)
	}
	r.model.SwitchSide()
	r.settle()
	if side, ok := r.model.FocusedSide(); !ok || side != Modified {
		t.Errorf("after switching: focused side %v %v", side, ok)
	}
	if sel := mod.Cursors().Primary().Selection; sel.Anchor != (buffer.Position{Line: 2, Col: 0}) || sel.Active != (buffer.Position{Line: 2, Col: 1}) {
		t.Errorf("the modified selection is %v", sel)
	}
	// and back
	r.model.SwitchSide()
	r.settle()
	if side, _ := r.model.FocusedSide(); side != Original {
		t.Errorf("after switching back: %v", side)
	}
	if sel := orig.Cursors().Primary().Selection; sel.Anchor != (buffer.Position{Line: 3, Col: 0}) {
		t.Errorf("the original selection came back as %v", sel)
	}
	r.noErrors()
}

func TestWrappedLinesAreAlignedByTheirRows(t *testing.T) {
	short := "short line"
	long := strings.Repeat("word ", 40) // wraps into several rows of the 400 pixel editors
	original := "one\n" + short + "\ntwo\nthree\n"
	modified := "one\n" + long + "\ntwo\nthree\n"
	wrap := func(o *editor.Options) {
		o.WordWrap, o.WrappingIndent, o.WrappingStrategy, o.StopRenderingLineAfter = "on", "same", "simple", 10000
	}
	r := newRig(t, original, modified, rigConfig{size: ui.Size{Width: 800, Height: 300}, tweak: wrap})
	orig, mod := r.model.View(Original), r.model.View(Modified)
	r.waitFor("the lines to be wrapped", func() bool {
		_, n := mod.Projection().LineRows(1)
		return n > 2 && len(r.model.ZonesOf(Original)) > 0
	})
	_, rows := mod.Projection().LineRows(1)
	zones := r.model.ZonesOf(Original)
	if len(zones) != 1 || zones[0].AfterLine != 2 || zones[0].Rows != rows-1 {
		t.Errorf("the long line has %d rows; original zones %v", rows, zones)
	}
	for _, line := range []int{0, 2, 3} {
		if a, b := orig.RowOfPosition(buffer.Position{Line: line}), mod.RowOfPosition(buffer.Position{Line: line}); a != b {
			t.Errorf("line %d is on row %d and row %d", line+1, a, b)
		}
	}
	// a narrower window wraps more: the filler follows
	r.ts.SetViewSize(ui.Size{Width: 500, Height: 300})
	r.waitFor("the filler to follow the wrapping", func() bool {
		_, now := mod.Projection().LineRows(1)
		z := r.model.ZonesOf(Original)
		return now > rows && len(z) == 1 && z[0].Rows == now-1
	})
	for _, line := range []int{2, 3} {
		if a, b := orig.RowOfPosition(buffer.Position{Line: line}), mod.RowOfPosition(buffer.Position{Line: line}); a != b {
			t.Errorf("after resizing, line %d is on row %d and row %d", line+1, a, b)
		}
	}
	r.noErrors()
}

func TestTheOverviewRulerMarksTheChanges(t *testing.T) {
	original := lines(100, func(i int) string { return "line " + strconv.Itoa(i) })
	changed := map[int]string{19: "changed a", 79: "changed b"}
	modified := lines(100, func(i int) string {
		if s, ok := changed[i]; ok {
			return s
		}
		return "line " + strconv.Itoa(i)
	})
	r := newRig(t, original, modified, rigConfig{size: ui.Size{Width: 800, Height: 300}})
	marks, total := r.model.RulerMarks()
	if total != 101*19 { // the text ends with a line break: a last empty line
		t.Errorf("total %v", total)
	}
	var origMarks, modMarks []RulerMark
	for _, m := range marks {
		if m.Side == Original {
			origMarks = append(origMarks, m)
		} else {
			modMarks = append(modMarks, m)
		}
	}
	if len(origMarks) != 2 || len(modMarks) != 2 {
		t.Fatalf("marks %+v", marks)
	}
	if origMarks[0].Top != 19*19 || origMarks[0].Bottom != 20*19 || modMarks[1].Top != 79*19 || modMarks[1].Bottom != 80*19 {
		t.Errorf("marks %+v", marks)
	}
	// a click on the ruler scrolls the editors there; the ruler is the last 14 pixels of the window
	mod := r.model.View(Modified)
	x := 800 - RulerWidth/2
	r.ts.TapAt(ui.Offset{Dx: x, Dy: 150}, gluttertest.GestureOpts{Kind: ui.PointerDeviceKindMouse})
	r.settle()
	if got, want := mod.ScrollTop(), 150.0/300*total-300.0/2; absDiff(got, want) > 25 {
		t.Errorf("a click in the middle of the ruler scrolled to %v, want about %v", got, want)
	}
	if r.model.View(Original).ScrollTop() != mod.ScrollTop() {
		t.Errorf("the original is at %v, the modified at %v", r.model.View(Original).ScrollTop(), mod.ScrollTop())
	}
	g := r.ts.StartGesture(ui.Offset{Dx: x, Dy: 20}, gluttertest.GestureOpts{Kind: ui.PointerDeviceKindMouse})
	g.MoveTo(ui.Offset{Dx: x, Dy: 280})
	g.Up()
	r.settle()
	if got := mod.ScrollTop(); got < 1200 {
		t.Errorf("dragging the ruler down scrolled to %v", got)
	}
	r.noErrors()
}

func TestTheModelCleansUpAfterItself(t *testing.T) {
	r := newRig(t, "a\nb\n", "a\nc\nd\n", rigConfig{})
	orig, mod := r.model.View(Original), r.model.View(Modified)
	if len(orig.Zones())+len(mod.Zones()) == 0 || len(mod.BackgroundMarks()) == 0 {
		t.Fatal("nothing to clean up")
	}
	r.model.Dispose()
	r.model.Dispose() // twice does no harm
	if len(orig.Zones())+len(mod.Zones()) != 0 || len(orig.BackgroundMarks())+len(mod.BackgroundMarks()) != 0 {
		t.Error("the zones or the marks are still there")
	}
	// edits after it ended do not start a computation
	typeAt(t, r.mod, 0, 0, "x")
	if r.model.Computing() {
		t.Error("a disposed model computes")
	}
}

func TestColorsFollowTheTheme(t *testing.T) {
	r := newRig(t, "a\nb\n", "a\nc\n", rigConfig{})
	dark := ColorsOf(kit.DarkModern())
	if got := r.model.Colors(); got != dark {
		t.Fatalf("colors %+v", got)
	}
	r.pump(kit.LightModern())
	r.settle()
	light := ColorsOf(kit.LightModern())
	if got := r.model.Colors(); got != light {
		t.Fatalf("colors after the theme changed: %+v", got)
	}
	for _, m := range r.model.View(Modified).BackgroundMarks() {
		if m.WholeLine && m.Background != light.InsertedLine {
			t.Errorf("a line mark has %v, want %v", m.Background, light.InsertedLine)
		}
		if !m.WholeLine && m.Background != light.InsertedText {
			t.Errorf("a character mark has %v, want %v", m.Background, light.InsertedText)
		}
	}
}
