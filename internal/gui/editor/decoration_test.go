package editor

import (
	"slices"
	"testing"
	"time"

	"github.com/rm4n0s/emetos/internal/core/buffer"
)

func TestStyleSelectionRows(t *testing.T) {
	const (
		e = cornerExtern
		i = cornerIntern
		f = cornerFlat
	)
	cases := []struct {
		name string
		rows []selectionRow
		// want is the Start and End style of each row.
		want [][2]endStyle
	}{
		{"one row", []selectionRow{{Left: 10, Right: 50}}, [][2]endStyle{{{e, e}, {e, e}}}},
		{"two rows with the same ends", []selectionRow{{Left: 0, Right: 50}, {Left: 0, Right: 50}},
			[][2]endStyle{{{e, f}, {e, f}}, {{f, e}, {f, e}}}},
		{"the ends differ by less than a quarter of a character", []selectionRow{{Left: 0, Right: 50}, {Left: 1.5, Right: 51}},
			[][2]endStyle{{{e, f}, {e, f}}, {{f, e}, {f, e}}}},
		{"starts in the middle, ends before the end of the line above", []selectionRow{{Left: 40, Right: 100}, {Left: 0, Right: 60}},
			[][2]endStyle{{{e, i}, {e, e}}, {{e, e}, {i, e}}}},
		{"the second row reaches further", []selectionRow{{Left: 0, Right: 50}, {Left: 0, Right: 100}},
			[][2]endStyle{{{e, f}, {e, i}}, {{f, e}, {e, e}}}},
		{"the rows do not overlap", []selectionRow{{Left: 60, Right: 100}, {Left: 0, Right: 40}},
			[][2]endStyle{{{e, e}, {e, e}}, {{e, e}, {e, e}}}},
		{"three rows", []selectionRow{{Left: 30, Right: 90}, {Left: 0, Right: 90}, {Left: 0, Right: 40}},
			[][2]endStyle{{{e, i}, {e, f}}, {{e, f}, {f, e}}, {{f, e}, {i, e}}}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			rows := slices.Clone(tc.rows)
			styleSelectionRows(rows, 8)
			for n, r := range rows {
				if r.Start != tc.want[n][0] || r.End != tc.want[n][1] {
					t.Errorf("row %d: start %v end %v, want start %v end %v", n, r.Start, r.End, tc.want[n][0], tc.want[n][1])
				}
			}
		})
	}
}

func TestSelectionPieces(t *testing.T) {
	rows := []selectionRow{{Row: 0, Left: 40, Right: 100}, {Row: 1, Left: 0, Right: 60}}
	styleSelectionRows(rows, 8)

	t.Run("rounded", func(t *testing.T) {
		corners, rest := selectionPieces(rows, true, false)
		// The first row has a reversed corner at its start: the selection piece and a piece in
		// the color of the editor with its bottom right corner rounded.
		want := []selectionPiece{
			{Left: 30, Right: 40},
			{Left: 30, Right: 40, Background: true, BottomRight: true},
		}
		if !slices.Equal(corners[0], want) {
			t.Errorf("corners of row 0: %+v, want %+v", corners[0], want)
		}
		wantRest := selectionPiece{Left: 40, Right: 100, TopLeft: true, TopRight: true, BottomRight: true}
		if len(rest[0]) != 1 || rest[0][0] != wantRest {
			t.Errorf("rest of row 0: %+v, want %+v", rest[0], wantRest)
		}
		// The second row has one at its end, with the top left corner rounded.
		want = []selectionPiece{
			{Left: 60, Right: 70},
			{Left: 60, Right: 70, Background: true, TopLeft: true},
		}
		if !slices.Equal(corners[1], want) {
			t.Errorf("corners of row 1: %+v, want %+v", corners[1], want)
		}
		wantRest = selectionPiece{Left: 0, Right: 60, TopLeft: true, BottomLeft: true, BottomRight: true}
		if len(rest[1]) != 1 || rest[1][0] != wantRest {
			t.Errorf("rest of row 1: %+v, want %+v", rest[1], wantRest)
		}
	})
	t.Run("not rounded", func(t *testing.T) {
		corners, rest := selectionPieces(rows, false, false)
		for i := range rows {
			if len(corners[i]) != 0 || len(rest[i]) != 1 || rest[i][0].TopLeft || rest[i][0].BottomRight {
				t.Errorf("row %d: %+v %+v", i, corners[i], rest[i])
			}
		}
	})
	t.Run("several selections", func(t *testing.T) {
		_, rest := selectionPieces(rows, true, true)
		if rest[0][0].TopInset != 1 || rest[0][0].BottomInset != 0 || rest[1][0].TopInset != 0 || rest[1][0].BottomInset != 1 {
			t.Errorf("insets: %+v %+v", rest[0][0], rest[1][0])
		}
		single := []selectionRow{{Left: 0, Right: 10}}
		_, rest = selectionPieces(single, true, true)
		if rest[0][0].TopInset != 1 || rest[0][0].BottomInset != 0 {
			t.Errorf("a selection of one row is inset at the top only: %+v", rest[0][0])
		}
	})
}

func TestHighlightPlacement(t *testing.T) {
	cases := []struct {
		name                 string
		setting              string
		onlyWhenFocus        bool
		focused, emptySel    bool
		wantText, wantMargin bool
	}{
		{"line", "line", false, true, true, true, false},
		{"line with a selection", "line", false, true, false, false, false},
		{"gutter with a selection", "gutter", false, true, false, false, true},
		{"all", "all", false, true, true, true, true},
		{"none", "none", false, true, true, false, false},
		{"only when focused and focused", "all", true, true, true, true, true},
		{"only when focused and not", "all", true, false, true, false, false},
		{"not focused without the option", "line", false, false, true, true, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			o := testOptions()
			o.RenderLineHighlight, o.RenderLineHighlightOnlyWhenFocus = tc.setting, tc.onlyWhenFocus
			if got := highlightInContent(o, tc.focused, tc.emptySel); got != tc.wantText {
				t.Errorf("in the text: %v", got)
			}
			if got := highlightInMargin(o, tc.focused); got != tc.wantMargin {
				t.Errorf("in the gutter: %v", got)
			}
		})
	}
}

// fakeTimers is a clock whose timers fire when a test advances it.
type fakeTimers struct {
	now      time.Duration
	timers   []*fakeTimer
	canceled int
}

type fakeTimer struct {
	at   time.Duration
	fn   func()
	done bool
}

func (f *fakeTimers) after(d time.Duration, fn func()) func() {
	t := &fakeTimer{at: f.now + d, fn: fn}
	f.timers = append(f.timers, t)
	return func() {
		if !t.done {
			t.done = true
			f.canceled++
		}
	}
}

// pending is the number of timers that have not fired or been canceled.
func (f *fakeTimers) pending() int {
	n := 0
	for _, t := range f.timers {
		if !t.done {
			n++
		}
	}
	return n
}

// advance moves the clock and fires the timers that fall due, in order, including those they
// start.
func (f *fakeTimers) advance(d time.Duration) {
	end := f.now + d
	for {
		var next *fakeTimer
		for _, t := range f.timers {
			if !t.done && t.at <= end && (next == nil || t.at < next.at) {
				next = t
			}
		}
		if next == nil {
			break
		}
		f.now, next.done = next.at, true
		next.fn()
	}
	f.now = end
}

func newBlinkingViewModel(t *testing.T, blinking string) (*ViewModel, *fakeTimers) {
	t.Helper()
	opts := testOptions()
	opts.CursorBlinking = blinking
	vm := NewViewModel(newTestDoc("one\ntwo"), opts, nil)
	clock := &fakeTimers{}
	vm.SetTimer(clock.after)
	return vm, clock
}

func TestCursorBlinks(t *testing.T) {
	vm, clock := newBlinkingViewModel(t, "blink")
	toggles := 0
	vm.AddBlinkListener(func() { toggles++ })
	if !vm.CursorsVisible() {
		t.Fatal("the cursor is shown at first")
	}
	steps := []struct {
		advance time.Duration
		visible bool
	}{
		{BlinkInterval - time.Millisecond, true},
		{time.Millisecond, false},
		{BlinkInterval, true},
		{BlinkInterval, false},
		{2 * BlinkInterval, false},
	}
	for i, s := range steps {
		clock.advance(s.advance)
		if vm.CursorsVisible() != s.visible {
			t.Errorf("step %d (%v): visible %v, want %v", i, s.advance, vm.CursorsVisible(), s.visible)
		}
	}
	if toggles != 5 {
		t.Errorf("the listeners heard %d changes, want 5", toggles)
	}
	if clock.pending() != 1 {
		t.Errorf("%d timers wait, want 1", clock.pending())
	}
}

func TestCursorBlinkRestartsOnMoveAndEdit(t *testing.T) {
	cases := []struct {
		name string
		act  func(*ViewModel)
	}{
		{"selections changed", func(vm *ViewModel) { vm.SelectionsChanged() }},
		{"document changed", func(vm *ViewModel) { vm.DocumentChanged() }},
		{"options changed", func(vm *ViewModel) {
			o := vm.Options()
			o.FontSize++
			vm.SetOptions(o)
		}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			vm, clock := newBlinkingViewModel(t, "blink")
			clock.advance(BlinkInterval) // hidden now
			if vm.CursorsVisible() {
				t.Fatal("the cursor is hidden in the second half")
			}
			clock.advance(BlinkInterval - time.Millisecond)
			tc.act(vm)
			if !vm.CursorsVisible() {
				t.Error("an action shows the cursor")
			}
			clock.advance(BlinkInterval - time.Millisecond)
			if !vm.CursorsVisible() {
				t.Error("the blinking counts again from the action")
			}
			clock.advance(time.Millisecond)
			if vm.CursorsVisible() {
				t.Error("and the cursor hides half a second after it")
			}
			if clock.pending() != 1 {
				t.Errorf("%d timers wait, want 1", clock.pending())
			}
		})
	}
}

func TestCursorBlinkStopsWithoutFocus(t *testing.T) {
	vm, clock := newBlinkingViewModel(t, "blink")
	vm.SetFocused(false)
	if vm.CursorsVisible() || clock.pending() != 0 {
		t.Errorf("an editor without focus has no cursor (visible %v) and no timer (%d)", vm.CursorsVisible(), clock.pending())
	}
	clock.advance(10 * BlinkInterval)
	if vm.CursorsVisible() {
		t.Error("nothing brings the cursor back while unfocused")
	}
	vm.SelectionsChanged()
	if vm.CursorsVisible() || clock.pending() != 0 {
		t.Error("a move without focus shows nothing and starts no timer")
	}
	vm.SetFocused(true)
	if !vm.CursorsVisible() || clock.pending() != 1 {
		t.Errorf("focus shows the cursor (%v) and starts the timer (%d)", vm.CursorsVisible(), clock.pending())
	}
	vm.SetFocused(true) // no change
	if clock.pending() != 1 {
		t.Errorf("%d timers wait after a repeated focus", clock.pending())
	}
}

func TestCursorBlinkStyles(t *testing.T) {
	cases := []struct {
		blinking    string
		wantPending int
	}{
		// The smooth, phase and expand styles animate every frame instead of toggling on a timer.
		{"blink", 1}, {"smooth", 0}, {"phase", 0}, {"expand", 0}, {"solid", 0}, {"", 1},
	}
	for _, tc := range cases {
		t.Run(tc.blinking, func(t *testing.T) {
			vm, clock := newBlinkingViewModel(t, tc.blinking)
			clock.advance(BlinkInterval)
			if clock.pending() != tc.wantPending {
				t.Errorf("%d timers wait, want %d", clock.pending(), tc.wantPending)
			}
			if (tc.blinking == "solid" || tc.blinking == "smooth" || tc.blinking == "phase" || tc.blinking == "expand") && !vm.CursorsVisible() {
				t.Error("a solid or animated cursor stays shown")
			}
			if tc.wantPending == 1 && vm.CursorsVisible() {
				t.Error("a blinking cursor is hidden after half a second")
			}
		})
	}
}

func TestCursorBlinkNeedsATimer(t *testing.T) {
	vm, clock := newBlinkingViewModel(t, "blink")
	vm.SetTimer(nil)
	if clock.pending() != 0 {
		t.Errorf("removing the timers cancels the one that waits: %d", clock.pending())
	}
	if !vm.CursorsVisible() {
		t.Error("without timers a focused editor shows its cursor")
	}
	clock.advance(5 * BlinkInterval)
	if !vm.CursorsVisible() {
		t.Error("and does not blink")
	}
	vm.SelectionsChanged()
	if clock.pending() != 0 {
		t.Error("an action starts no timer without a source")
	}
}

func TestFocusAndSelectionListeners(t *testing.T) {
	vm, _ := newBlinkingViewModel(t, "blink")
	changes := 0
	h := vm.AddSelectionListener(func() { changes++ })
	vm.SelectionsChanged()
	vm.SetFocused(false)
	vm.SetFocused(false)
	vm.DocumentChanged()
	if changes != 3 {
		t.Errorf("heard %d changes, want 3", changes)
	}
	vm.RemoveSelectionListener(h)
	vm.SelectionsChanged()
	if changes != 3 {
		t.Error("a removed listener hears nothing")
	}
	if vm.Focused() {
		t.Error("the editor lost the focus")
	}
}

func TestRowOfPosition(t *testing.T) {
	vm := NewViewModel(newTestDoc("one\ntwo\n\nfour"), testOptions(), nil)
	cases := []struct {
		line, col, want int
	}{{0, 0, 0}, {1, 2, 1}, {2, 0, 2}, {3, 4, 3}, {9, 0, 0}}
	for _, tc := range cases {
		if got := vm.RowOfPosition(positionOf(tc.line, tc.col)); got != tc.want {
			t.Errorf("line %d: row %d, want %d", tc.line, got, tc.want)
		}
	}
}

func positionOf(line, col int) buffer.Position { return buffer.Position{Line: line, Col: col} }
