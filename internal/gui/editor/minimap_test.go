package editor

import (
	"strings"
	"testing"
)

// minimapVM makes a view model of lines lines that is 600 pixels high, with a minimap of the options,
// and records where the pointer makes the editor scroll.
func minimapVM(t *testing.T, lines int, size string) (*ViewModel, *[]float64) {
	t.Helper()
	opts := testOptions()
	opts.LineHeight, opts.ScrollBeyondLastLine = 19, true
	vm := NewViewModel(newTestDoc(strings.Repeat("line\n", lines-1)+"line"), opts, nil)
	vm.SetTextMetrics(700, 600, max(vm.Projection().Height()-19, 0), 0, 8.4)
	in := minimapIn(func(in *minimapInput) { in.Size, in.ViewLineCount = size, lines })
	vm.SetMinimap(computeMinimapLayout(in), in)
	var scrolled []float64
	vm.SetScroller(func(top float64) { scrolled = append(scrolled, top) })
	vm.minimapLayoutNow(0)
	return vm, &scrolled
}

func TestMinimapPointerWhenThereIsNone(t *testing.T) {
	vm := NewViewModel(newTestDoc("a"), testOptions(), nil)
	if vm.MinimapPointerDown(10) {
		t.Error("a press on a minimap that is not there was taken")
	}
	in := minimapIn(func(in *minimapInput) { in.Enabled = false })
	vm.SetMinimap(computeMinimapLayout(in), in)
	if vm.MinimapPointerDown(10) {
		t.Error("a press on a minimap that is off was taken")
	}
	vm.MinimapPointerMove(10) // nothing is dragged
	vm.MinimapPointerUp()
}

func TestMinimapClickRevealsTheLineInTheMiddle(t *testing.T) {
	vm, scrolled := minimapVM(t, 500, "proportional")
	// the lines are 3 pixels high: y = 450 is on the line 151, the row 150
	if !vm.MinimapPointerDown(450) {
		t.Fatal("the press was not taken")
	}
	want := 150*19 + 19.0/2 - 600.0/2
	if len(*scrolled) != 1 || (*scrolled)[0] != want {
		t.Errorf("scrolled to %v, want %v", *scrolled, want)
	}
	if vm.mm.active {
		t.Error("a click outside the slider drags nothing")
	}
	// a press on the slider does not reveal anything (it starts a drag)
	*scrolled = nil
	vm.MinimapPointerUp()
	vm.minimapLayoutNow(0)
	vm.MinimapPointerDown(0)
	if len(*scrolled) != 0 || !vm.mm.active {
		t.Errorf("a press on the slider scrolled: %v", *scrolled)
	}
	vm.MinimapPointerUp()
	// a press past the last line reveals the last one
	*scrolled = nil
	vm.MinimapPointerDown(599)
	if want := 199*19 + 19.0/2 - 600.0/2; len(*scrolled) != 1 {
		t.Errorf("a press at the bottom: %v", *scrolled)
	} else if (*scrolled)[0] < want-1 {
		// 600 pixels are 200 lines, so the line is the 200th of the window, not the last of the text
		t.Errorf("a press at the bottom: scrolled to %v, want %v", (*scrolled)[0], want)
	}
}

func TestMinimapSliderDrag(t *testing.T) {
	vm, scrolled := minimapVM(t, 500, "proportional")
	l := vm.mm.layout
	if !l.SliderNeeded || l.SliderHeight < 90 {
		t.Fatalf("slider %+v", l)
	}
	y := l.SliderTop + 10
	if !vm.MinimapPointerDown(y) || !vm.mm.active {
		t.Fatal("a press on the slider did not start a drag")
	}
	if len(*scrolled) != 0 {
		t.Errorf("a press on the slider scrolled: %v", *scrolled)
	}
	vm.MinimapPointerMove(y + 60)
	if want := l.desiredScrollTopFromDelta(60); len(*scrolled) != 1 || (*scrolled)[0] != want {
		t.Errorf("dragged by 60 pixels: %v, want %v", *scrolled, want)
	}
	// the drag goes on from where it began, not from the last move
	vm.MinimapPointerMove(y - 5)
	if want := l.desiredScrollTopFromDelta(-5); (*scrolled)[1] != want {
		t.Errorf("dragged back: %v, want %v", (*scrolled)[1], want)
	}
	vm.MinimapPointerUp()
	if vm.mm.active {
		t.Error("still active after the release")
	}
	vm.MinimapPointerMove(y + 100)
	if len(*scrolled) != 2 {
		t.Errorf("a move after the release scrolled: %v", *scrolled)
	}
}

func TestMinimapFillDragsFromTheMiddleOfTheSlider(t *testing.T) {
	vm, scrolled := minimapVM(t, 30, "fill")
	l := vm.mm.layout
	if !vm.mm.info.HeightIsEditorHeight || !l.SliderNeeded {
		t.Fatalf("layout %+v slider %+v", vm.mm.info, l)
	}
	middle := l.SliderTop + l.SliderHeight/2
	// a press below the slider moves it there, as if it were pressed in its middle
	press := l.SliderTop + l.SliderHeight + 40
	if !vm.MinimapPointerDown(press) {
		t.Fatal("not taken")
	}
	if !vm.mm.active {
		t.Error("a press on a minimap that fills is a drag")
	}
	if want := l.desiredScrollTopFromDelta(press - middle); len(*scrolled) != 1 || (*scrolled)[0] != want {
		t.Errorf("scrolled to %v, want %v", *scrolled, want)
	}
	vm.MinimapPointerUp()
}

func TestMinimapSampledLinesRevealTheirRows(t *testing.T) {
	vm, scrolled := minimapVM(t, 2000, "fit")
	if s := vm.mm.sampling; s == nil || len(s.Lines) != 600 && len(s.Lines) < 300 {
		t.Fatalf("sampling %+v", vm.mm.sampling)
	}
	// the minimap line 1 shows a row near the first (VS Code's sampling puts the first line at the
	// middle of its ratio), the last shows the last row, one in the middle a row of the middle
	if got := vm.minimapRowOf(1); got > 2 {
		t.Errorf("line 1 shows the row %d", got)
	}
	if got := vm.minimapRowOf(len(vm.mm.sampling.Lines)); got != 1999 {
		t.Errorf("the last line shows the row %d", got)
	}
	if got := vm.minimapRowOf(len(vm.mm.sampling.Lines) / 2); got < 900 || got > 1100 {
		t.Errorf("the middle line shows the row %d", got)
	}
	if got := vm.minimapLineOfRow(1999); got != len(vm.mm.sampling.Lines) {
		t.Errorf("the last row is on the line %d", got)
	}
	if vm.minimapLineCount() != len(vm.mm.sampling.Lines) {
		t.Errorf("line count %d", vm.minimapLineCount())
	}
	_ = scrolled
}

func TestMinimapHover(t *testing.T) {
	vm, _ := minimapVM(t, 100, "proportional")
	changes := 0
	vm.mm.changed.AddListener(func() { changes++ })
	vm.MinimapHover(true)
	vm.MinimapHover(true)
	if !vm.mm.hover || changes != 1 {
		t.Errorf("hover %v after %d changes", vm.mm.hover, changes)
	}
	vm.MinimapHover(false)
	if vm.mm.hover || changes != 2 {
		t.Errorf("hover %v after %d changes", vm.mm.hover, changes)
	}
}

func TestSetMinimapOnlyChangesWhenSomethingDid(t *testing.T) {
	vm, _ := minimapVM(t, 100, "proportional")
	changes := 0
	vm.mm.changed.AddListener(func() { changes++ })
	in := vm.mm.in
	vm.SetMinimap(vm.mm.info, in)
	if changes != 0 {
		t.Errorf("the same layout changed something")
	}
	in.ViewLineCount = 101
	vm.SetMinimap(vm.mm.info, in)
	if changes != 1 {
		t.Errorf("%d changes", changes)
	}
}
