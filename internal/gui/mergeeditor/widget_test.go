package mergeeditor

import (
	"fmt"
	"math"
	"slices"
	"strings"
	"testing"

	"github.com/rm4n0s/glutter/gluttertest"
	"github.com/rm4n0s/glutter/ui"

	"github.com/rm4n0s/emetos/internal/core/buffer"
	"github.com/rm4n0s/emetos/internal/core/diff"
	"github.com/rm4n0s/emetos/internal/gui/kit"
)

func TestWidgetLayout(t *testing.T) {
	inputs, _ := goMerge()
	r := newRig(t, rigConfig{inputs: inputs})
	r.noErrors()
	m := r.model
	if m.View(Incoming).Document() != r.in1 || m.View(Current).Document() != r.in2 || m.View(Result).Document() != r.result {
		t.Fatal("the editors show the wrong documents")
	}
	if !m.View(Incoming).ReadOnly() || !m.View(Current).ReadOnly() || m.View(Result).ReadOnly() {
		t.Error("the inputs are not read-only, or the result is")
	}
	if got := r.session.Model().ConflictCount(); got != 2 {
		t.Errorf("%d conflicts", got)
	}
	if m.Remaining() != "2 Conflicts Remaining" || m.Label(Incoming) != "incoming.go" || m.Label(Current) != "current.go" || m.Label(Result) != "result.go" {
		t.Errorf("headers %q %q %q %q", m.Remaining(), m.Label(Incoming), m.Label(Current), m.Label(Result))
	}
	// the headers say so on screen
	for _, text := range []string{"Incoming", "incoming.go", "Current", "current.go", "Result", "result.go", "2 Conflicts Remaining"} {
		if !r.ts.Exists(gluttertest.ByText(text)) {
			t.Errorf("no header text %q", text)
		}
	}
	// the inputs are in the top half, side by side with the same width, and the result is below
	inc, cur, res := r.ts.TopLeft(gluttertest.ByText("Incoming")), r.ts.TopLeft(gluttertest.ByText("Current")), r.ts.TopLeft(gluttertest.ByText("Result"))
	if inc.Dy != cur.Dy || res.Dy <= inc.Dy+100 || cur.Dx < 400 || cur.Dx > 600 {
		t.Errorf("headers at %v %v %v", inc, cur, res)
	}
	w1, _ := m.View(Incoming).ViewportSize()
	w2, _ := m.View(Current).ViewportSize()
	if math.Abs(w1-w2) > 1 {
		t.Errorf("the inputs are %v and %v wide", w1, w2)
	}
	_, h1 := m.View(Incoming).ViewportSize()
	_, h3 := m.View(Result).ViewportSize()
	if math.Abs(h1-h3) > 2 {
		t.Errorf("the top half is %v high and the result %v", h1, h3)
	}
}

func TestInputsAreLinedUp(t *testing.T) {
	inputs, _ := goMerge()
	r := newRig(t, rigConfig{inputs: inputs})
	m := r.model
	// the comment line that only the incoming side has: the current side gets a row for it, after its line 8
	if got := m.Spacers(Current); len(got) != 1 || got[0].Before != 9 || got[0].Rows != 1 {
		t.Errorf("spacers of the current side %v", got)
	}
	if got := m.Spacers(Incoming); len(got) != 0 {
		t.Errorf("spacers of the incoming side %v", got)
	}
	// the same lines are on the same rows: 'func add' is line 10 of the incoming side and line 9 of the current side
	p1, p2 := m.View(Incoming).Projection(), m.View(Current).Projection()
	row1, _ := p1.LineRows(9)
	row2, _ := p2.LineRows(8)
	if p1.RowTop(row1) != p2.RowTop(row2) {
		t.Errorf("func add is at %v and %v", p1.RowTop(row1), p2.RowTop(row2))
	}
}

func TestDecorations(t *testing.T) {
	inputs, _ := goMerge()
	r := newRig(t, rigConfig{inputs: inputs})
	m := r.model
	c := kit.DarkModern()
	// each input marks the lines it changed in a conflict and the characters that differ, and outlines the conflicts:
	// not the change it makes without asking
	for _, side := range []Side{Incoming, Current} {
		var whole, words int
		for _, mk := range m.View(side).BackgroundMarks() {
			if mk.WholeLine {
				whole++
				if mk.Background != c.MergeChange {
					t.Errorf("%v: a line mark in %v", side, mk.Background)
				}
			} else {
				words++
				if mk.Background != c.MergeChangeWord {
					t.Errorf("%v: a word mark in %v", side, mk.Background)
				}
			}
		}
		if whole != 2 || words == 0 {
			t.Errorf("%v: %d line marks, %d word marks", side, whole, words)
		}
		blocks := m.View(side).LineBlocks()
		if len(blocks) != 2 {
			t.Fatalf("%v: %d blocks", side, len(blocks))
		}
		for _, b := range blocks {
			if b.Color != c.MergeUnhandledUnfocusedBorder || b.Width != 1 {
				t.Errorf("%v: a block of %v, %v px", side, b.Color, b.Width)
			}
		}
	}
	// the result outlines the conflicts, which still hold the base: nothing is colored
	blocks := m.View(Result).LineBlocks()
	if len(blocks) != 2 || blocks[0].StartLine != 5 || blocks[0].EndLine != 6 || blocks[1].StartLine != 14 {
		t.Errorf("result blocks %+v", blocks)
	}
	if len(m.View(Result).BackgroundMarks()) != 0 {
		t.Errorf("result marks %v", m.View(Result).BackgroundMarks())
	}
	// a toggled conflict is handled (the gray border) and its lines in the result are colored
	m.Toggle(0, diff.Input1)
	r.settle()
	if got := m.View(Result).LineBlocks()[0]; got.Color != c.MergeHandledUnfocusedBorder && got.Color != c.MergeHandledFocusedBorder {
		t.Errorf("a handled conflict has the border %v", got.Color)
	}
	if marks := m.View(Result).BackgroundMarks(); len(marks) != 1 || !marks[0].WholeLine || marks[0].Range.Start.Line != 5 {
		t.Errorf("result marks after a toggle: %+v", marks)
	}
	if m.Remaining() != "1 Conflict Remaining" {
		t.Errorf("the count says %q", m.Remaining())
	}
	r.noErrors()
}

func TestNavigationMovesTheCursorsAndWraps(t *testing.T) {
	inputs, _ := goMerge()
	r := newRig(t, rigConfig{inputs: inputs})
	m := r.model
	cursorLines := func() [3]int {
		var out [3]int
		for i := range out {
			out[i] = m.View(Side(i)).Cursors().Primary().Position().Line + 1
		}
		return out
	}
	// the result starts with the cursor on its line 1
	for i, want := range []struct {
		next  bool
		lines [3]int // incoming, current, result
		conf  int
	}{
		{true, [3]int{6, 6, 6}, 0},    // the first conflict
		{true, [3]int{15, 14, 15}, 1}, // the second
		{true, [3]int{6, 6, 6}, 0},    // wraps around
		{false, [3]int{15, 14, 15}, 1},
		{false, [3]int{6, 6, 6}, 0},
	} {
		if !m.GoToConflict(want.next) {
			t.Fatalf("step %d: nothing to go to", i)
		}
		r.settle()
		if got := cursorLines(); got != want.lines {
			t.Errorf("step %d: cursors on lines %v, want %v", i, got, want.lines)
		}
		if a, ok := m.Active(); !ok || a.ConflictIndex != want.conf {
			t.Errorf("step %d: the active region is %+v", i, a)
		}
		// the conflict is outlined as the focused one: 2 px, in the focused color
		blocks := m.View(Result).LineBlocks()
		if got := blocks[want.conf]; got.Width != 2 || got.Color != kit.DarkModern().MergeUnhandledFocusedBorder {
			t.Errorf("step %d: the active conflict has %v px of %v", i, got.Width, got.Color)
		}
		if got := blocks[1-want.conf]; got.Width != 1 {
			t.Errorf("step %d: the other conflict has %v px", i, got.Width)
		}
		// and it is in view in the editor that was used
		vm := m.View(Result)
		_, h := vm.ViewportSize()
		line := vm.Cursors().Primary().Position().Line
		row, _ := vm.Projection().LineRows(line)
		if top := vm.Projection().RowTop(row); top < vm.ScrollTop()-1 || top > vm.ScrollTop()+h {
			t.Errorf("step %d: the conflict is not in view (top %v, scroll %v, viewport %v)", i, top, vm.ScrollTop(), h)
		}
	}
	// once the conflicts are handled there is nothing to go to
	m.Toggle(0, diff.Input1)
	m.Toggle(1, diff.Input2)
	if m.GoToConflict(true) || m.GoToConflict(false) {
		t.Error("went to a conflict that is handled")
	}
	r.noErrors()
}

func TestNavigationFollowsTheEditorThatWasUsed(t *testing.T) {
	inputs, _ := goMerge()
	r := newRig(t, rigConfig{inputs: inputs})
	m := r.model
	// a cursor in the incoming editor, on its line 6 (in the first conflict): the next conflict is the second one
	m.FocusSide(Incoming)
	r.settle()
	vm := m.View(Incoming)
	vm.Cursors().SetSelections([]buffer.Selection{buffer.NewCursor(buffer.Position{Line: 5, Col: 2})})
	vm.SelectionsChanged()
	if a, ok := m.Active(); !ok || a.ConflictIndex != 0 {
		t.Fatalf("the active region is %+v after a cursor in the first conflict", a)
	}
	if !m.GoToConflict(true) {
		t.Fatal("nothing to go to")
	}
	if a, _ := m.Active(); a.ConflictIndex != 1 {
		t.Errorf("went to conflict %d", a.ConflictIndex)
	}
	if got := m.View(Incoming).Cursors().Primary().Position().Line; got != 14 {
		t.Errorf("the incoming cursor is on line %d", got+1)
	}
}

// longMerge is a merge of 120 lines with a conflict at line 30, a change that is not one at line 50 (three more lines
// in the incoming side) and a conflict at line 90.
func longMerge() diff.MergeInputs {
	var base, in1, in2 []string
	for i := 1; i <= 120; i++ {
		l := fmt.Sprintf("line %d", i)
		base = append(base, l)
		switch i {
		case 30:
			in1, in2 = append(in1, "line 30 incoming"), append(in2, "line 30 current")
		case 50:
			in1, in2 = append(in1, l, "added 1", "added 2", "added 3"), append(in2, l)
		case 90:
			in1, in2 = append(in1, "line 90 incoming", "line 90 incoming too"), append(in2, "line 90 current")
		default:
			in1, in2 = append(in1, l), append(in2, l)
		}
	}
	return diff.MergeInputsFromFiles(base, in1, in2)
}

func TestScrollSync(t *testing.T) {
	r := newRig(t, rigConfig{inputs: longMerge(), size: ui.Size{Width: 900, Height: 360}})
	m := r.model
	in, cur, res := m.View(Incoming), m.View(Current), m.View(Result)
	regions := r.session.Model().Regions()
	expect := func(from Side, to Side) float64 {
		src, dst := m.View(from), m.View(to)
		var lm LineMap
		switch {
		case from != Result:
			in, _ := from.Input()
			lm = InputMap(regions, in)
		default:
			in, _ := to.Input()
			lm = InputMap(regions, in).Reverse()
		}
		return lm.ScrollFor(topLine(src), src.ScrollTop(), lineTops(src), lineTops(dst))
	}
	if cur.ScrollTop() != 0 || res.ScrollTop() != 0 {
		t.Fatalf("the editors start at %v %v", cur.ScrollTop(), res.ScrollTop())
	}
	for _, tc := range []struct {
		name string
		from Side
		to   float64
	}{
		{"the incoming editor to the first conflict", Incoming, 28 * 19},
		{"the incoming editor past the added lines", Incoming, 70 * 19},
		{"the incoming editor to the second conflict", Incoming, 88 * 19},
		{"the current editor", Current, 60 * 19},
		{"the result", Result, 40 * 19},
		{"the result near the end", Result, 100 * 19},
		{"the result back to the top", Result, 0},
	} {
		t.Run(tc.name, func(t *testing.T) {
			m.View(tc.from).ScrollTo(tc.to)
			r.settle()
			src := m.View(tc.from)
			if math.Abs(src.ScrollTop()-tc.to) > 1 {
				t.Fatalf("the scrolled editor is at %v, not %v", src.ScrollTop(), tc.to)
			}
			for _, other := range []Side{Incoming, Current, Result} {
				if other == tc.from {
					continue
				}
				var want float64
				if _, fromInput := tc.from.Input(); fromInput {
					if _, toInput := other.Input(); toInput {
						want = src.ScrollTop() // the inputs are locked
					} else {
						want = expect(tc.from, other)
					}
				} else {
					want = expect(tc.from, other)
				}
				got := m.View(other).ScrollTop()
				if math.Abs(got-want) > 1 && want <= m.View(other).ContentHeight() {
					t.Errorf("%v is at %v, want %v", other, got, want)
				}
			}
			// no echo: another turn of the loop changes nothing
			before := [3]float64{in.ScrollTop(), cur.ScrollTop(), res.ScrollTop()}
			r.settle()
			if after := [3]float64{in.ScrollTop(), cur.ScrollTop(), res.ScrollTop()}; after != before {
				t.Errorf("the scroll moved on its own: %v then %v", before, after)
			}
			if tc.from == Result {
				// the result keeps the place it was scrolled to
				if math.Abs(res.ScrollTop()-tc.to) > 1 {
					t.Errorf("the result moved from %v to %v", tc.to, res.ScrollTop())
				}
			}
		})
	}
	r.noErrors()
}

func TestScrollSyncKeepsTheHorizontalPosition(t *testing.T) {
	long := strings.Repeat("0123456789", 40)
	inputs := diff.MergeInputsFromFiles([]string{long, "b"}, []string{long, "b"}, []string{long, "b"})
	r := newRig(t, rigConfig{inputs: inputs, size: ui.Size{Width: 700, Height: 300}})
	m := r.model
	m.View(Result).SetScrollLeft(120)
	r.settle()
	if m.View(Incoming).ScrollLeft() != 120 || m.View(Current).ScrollLeft() != 120 {
		t.Errorf("the inputs are at %v and %v", m.View(Incoming).ScrollLeft(), m.View(Current).ScrollLeft())
	}
}

func TestCheckboxes(t *testing.T) {
	inputs, _ := goMerge()
	r := newRig(t, rigConfig{inputs: inputs})
	m := r.model
	items := m.GutterItems(Incoming)
	if len(items) != 2 || items[0].Check != CheckEmpty || items[0].Handled || items[0].Focused {
		t.Fatalf("incoming items %+v", items)
	}
	// a press on the checkbox of the incoming side of the first conflict takes its lines into the result
	box := gutterBox(items[0], 300)
	_, h := m.View(Incoming).ViewportSize()
	press := ui.Offset{Dx: box.Left + 9, Dy: HeaderHeight + gutterBox(items[0], h).Top + 9}
	r.ts.TapAt(press)
	r.settle()
	if got := r.result.Text(); !strings.Contains(got, "fmt.Println(\"hello,\", name)") || strings.Contains(got, "\"hello\", name") {
		t.Errorf("the result after the press:\n%s", got)
	}
	items = m.GutterItems(Incoming)
	if items[0].Check != CheckFirst || !items[0].Handled || !items[0].Focused {
		t.Errorf("the incoming checkbox after the press: %+v", items[0])
	}
	if cur := m.GutterItems(Current); cur[0].Check != CheckEmpty {
		t.Errorf("the current checkbox after the press: %+v", cur[0])
	}
	// the current side too: both, the side that was there first first, so the current one shows two marks
	if !m.ClickGutter(Current, ui.Offset{Dx: box.Left + 9, Dy: gutterBox(m.GutterItems(Current)[0], h).Top + 9}, h) {
		t.Fatal("no checkbox for the current side")
	}
	items, cur := m.GutterItems(Incoming), m.GutterItems(Current)
	if items[0].Check != CheckFirst || cur[0].Check != CheckSecond {
		t.Errorf("checks after both: incoming %v current %v", items[0].Check, cur[0].Check)
	}
	if got := strings.Split(r.result.Text(), "\n")[5:7]; !slices.Equal(got, []string{"\tfmt.Println(\"hello,\", name)", "\tfmt.Println(\"hi\", name)"}) {
		t.Errorf("the lines of both: %q", got)
	}
	// a press that misses a checkbox does nothing
	if m.ClickGutter(Incoming, ui.Offset{Dx: 2, Dy: 2}, h) {
		t.Error("a press on nothing toggled")
	}
	// one undo takes the second toggle back, a second one the first
	if _, err := r.result.Undo(); err != nil {
		t.Fatal(err)
	}
	r.settle()
	if got := m.GutterItems(Current)[0].Check; got != CheckEmpty {
		t.Errorf("the current checkbox after an undo: %v", got)
	}
	r.noErrors()
}

func TestCheckboxShowsAnEditedResult(t *testing.T) {
	inputs, _ := goMerge()
	r := newRig(t, rigConfig{inputs: inputs})
	m := r.model
	// typing inside a conflict leaves it none of the states: a dot
	typeAt(t, r.result, 5, 4, "!")
	r.settle()
	if got := m.GutterItems(Incoming)[0].Check; got != CheckUnrecognized {
		t.Errorf("the checkbox of an edited conflict is %v", got)
	}
}

func TestRemainingConflictsGoesToTheNext(t *testing.T) {
	inputs, _ := goMerge()
	r := newRig(t, rigConfig{inputs: inputs})
	r.ts.Tap(gluttertest.ByText("2 Conflicts Remaining"))
	r.settle()
	if a, ok := r.model.Active(); !ok || a.ConflictIndex != 0 {
		t.Errorf("the active region after the press is %+v", a)
	}
	r.model.Toggle(0, diff.Input1)
	r.model.Toggle(1, diff.Input1)
	r.settle()
	if !r.ts.Exists(gluttertest.ByText("0 Conflicts Remaining")) {
		t.Error("the header does not say that no conflict is left")
	}
	if r.model.RemainingTooltip() != "All conflicts handled, the merge can be completed now." {
		t.Errorf("tooltip %q", r.model.RemainingTooltip())
	}
}

func TestTypingInTheResultKeepsTheRegions(t *testing.T) {
	inputs, _ := goMerge()
	r := newRig(t, rigConfig{inputs: inputs})
	typeAt(t, r.result, 0, 0, "// header\n")
	r.settle()
	blocks := r.model.View(Result).LineBlocks()
	if len(blocks) != 2 || blocks[0].StartLine != 6 || blocks[1].StartLine != 15 {
		t.Errorf("the conflicts did not move down a line: %+v", blocks)
	}
	if r.session.Resyncs() != 0 {
		t.Errorf("%d resyncs", r.session.Resyncs())
	}
}

func TestSessionWithMarkersInTheRig(t *testing.T) {
	inputs, err := diff.MergeInputsFromMarkerText(twoConflicts)
	if err != nil {
		t.Fatal(err)
	}
	r := newRig(t, rigConfig{inputs: inputs, resultText: twoConflicts, size: ui.Size{Width: 800, Height: 400}})
	if got := r.result.Text(); got != "a\nb\nc" {
		t.Errorf("the result is %q", got)
	}
	// an unhandled conflict whose base is empty is a line between two lines in the result
	blocks := r.model.View(Result).LineBlocks()
	if len(blocks) != 2 || blocks[0].StartLine != blocks[0].EndLine {
		t.Errorf("blocks %+v", blocks)
	}
	if !r.model.GoToConflict(true) {
		t.Error("cannot go to the first conflict")
	}
	if got := r.model.View(Result).Cursors().Primary().Position().Line; got != 1 {
		t.Errorf("the cursor is on line %d", got+1)
	}
	r.noErrors()
}

func TestDisposeTakesEverythingOffTheEditors(t *testing.T) {
	inputs, _ := goMerge()
	r := newRig(t, rigConfig{inputs: inputs})
	m := r.model
	if len(m.View(Current).Zones()) == 0 || len(m.View(Result).LineBlocks()) == 0 {
		t.Fatal("the editors have no zones or blocks to take off")
	}
	m.Dispose()
	for _, s := range []Side{Incoming, Current, Result} {
		vm := m.View(s)
		if len(vm.Zones()) != 0 || len(vm.LineBlocks()) != 0 || len(vm.BackgroundMarks()) != 0 {
			t.Errorf("%v: zones %d, blocks %d, marks %d", s, len(vm.Zones()), len(vm.LineBlocks()), len(vm.BackgroundMarks()))
		}
	}
	// a session that changes afterwards does not reach the model
	r.model.Toggle(0, diff.Input1)
	if len(m.View(Result).LineBlocks()) != 0 {
		t.Error("a disposed model drew a block")
	}
	m.Dispose() // twice is fine
}
