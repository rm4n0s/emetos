package editor

import (
	"slices"
	"strings"
	"testing"

	"github.com/rm4n0s/emetos/internal/core/folding"
)

// stickyVM is a view model of 70 lines with the scopes
//
//	A 0-39 { B 1-20 { C 2-10, D 12-18 } }, E 41-60
//
// in a window of 20 rows.
func stickyVM(t *testing.T, tweak func(*Options)) *ViewModel {
	t.Helper()
	opts := testOptions()
	opts.LineHeight, opts.ScrollBeyondLastLine = 19, true
	opts.StickyScroll = StickyScrollOptions{Enabled: true, MaxLineCount: 5, ScrollWithEditor: true}
	if tweak != nil {
		tweak(&opts)
	}
	vm := NewViewModel(newTestDoc(strings.Repeat("x\n", 69)+"x"), opts, nil)
	vm.StartFolding(nil, 0)
	regions, err := folding.NewRegions([]int{0, 1, 2, 12, 41}, []int{39, 20, 10, 18, 60}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if err := vm.UpdateFolding(regions); err != nil {
		t.Fatal(err)
	}
	vm.SetTextMetrics(700, 380, vm.Projection().Height()-19, 0, 8.4)
	return vm
}

func TestStickyLines(t *testing.T) {
	// A line sticks when the place it would take in the widget (a row for each scope around it) is
	// below where it is; the numbers below work that rule out by hand for rows of 19 pixels.
	cases := []struct {
		name   string
		top    float64 // in pixels
		lines  []int
		ends   []int
		offset float64
	}{
		{"at the top nothing has scrolled away", 0, nil, nil, 0},
		{"a line above the place of its scope sticks, and so do the ones inside it", 19, []int{0, 1, 2}, []int{40, 21, 11}, 0},
		{"three levels", 3 * 19, []int{0, 1, 2}, []int{40, 21, 11}, 0},
		{"the inner scope is scrolled until its end", 5 * 19, []int{0, 1, 2}, []int{40, 21, 11}, 0},
		{"the end of the inner scope pushes its line up", 9 * 19, []int{0, 1, 2}, []int{40, 21, 11}, -19},
		{"the inner scope is gone, and its sibling has not reached its place", 10 * 19, []int{0, 1}, []int{40, 21}, 0},
		{"it sticks as it passes it", 11 * 19, []int{0, 1, 12}, []int{40, 21, 19}, 0},
		{"the end of the middle scope", 20 * 19, []int{0, 1}, []int{40, 21}, -19},
		{"the outer scope has not been pushed yet at its end", 39 * 19, []int{0}, []int{40}, 0},
		{"the outer scope's end pushes it up, by what has gone by", 39*19 + 10, []int{0}, []int{40}, -10},
		{"past the outer scope", 41 * 19, nil, nil, 0},
		{"the next scope", 42 * 19, []int{41}, []int{61}, 0},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			vm := stickyVM(t, nil)
			got := vm.stickyLinesAt(tc.top)
			if !slices.Equal(got.Lines, tc.lines) || !slices.Equal(got.Ends, tc.ends) || got.Offset != tc.offset {
				t.Errorf("at %v: lines %v ends %v offset %v; want %v %v %v", tc.top, got.Lines, got.Ends, got.Offset, tc.lines, tc.ends, tc.offset)
			}
			if h := got.Height(19); h != float64(len(tc.lines))*19+tc.offset && len(tc.lines) > 0 {
				t.Errorf("height %v", h)
			}
		})
	}
}

func TestStickyLinesAreCappedBySettingAndHeight(t *testing.T) {
	// a deep stack of scopes: 8 nested, each starting a line after the last
	deep := func(t *testing.T, window float64, max int) *ViewModel {
		opts := testOptions()
		opts.LineHeight, opts.ScrollBeyondLastLine = 19, true
		opts.StickyScroll = StickyScrollOptions{Enabled: true, MaxLineCount: max}
		vm := NewViewModel(newTestDoc(strings.Repeat("x\n", 99)+"x"), opts, nil)
		vm.StartFolding(nil, 0)
		starts := []int{0, 1, 2, 3, 4, 5, 6, 7}
		ends := []int{90, 89, 88, 87, 86, 85, 84, 83}
		regions, err := folding.NewRegions(starts, ends, nil)
		if err != nil {
			t.Fatal(err)
		}
		if err := vm.UpdateFolding(regions); err != nil {
			t.Fatal(err)
		}
		vm.SetTextMetrics(700, window, vm.Projection().Height()-19, 0, 8.4)
		return vm
	}
	cases := []struct {
		name   string
		window float64
		max    int
		want   int
	}{
		{"the setting", 760, 5, 5},
		{"a lower setting", 760, 3, 3},
		{"a quarter of the editor: 760 / 19 / 4 = 10", 760, 20, 8},
		{"a small editor: 190 / 19 / 4 = 2.5 rounds to 3", 190, 20, 3},
		{"a tiny editor: 76 / 19 / 4 = 1", 76, 20, 1},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			vm := deep(t, tc.window, tc.max)
			if got := vm.stickyLinesAt(19 * 20); len(got.Lines) != tc.want {
				t.Errorf("%d lines, want %d: %v", len(got.Lines), tc.want, got.Lines)
			}
		})
	}
}

func TestStickyLinesNeedTheSetting(t *testing.T) {
	off := stickyVM(t, func(o *Options) { o.StickyScroll.Enabled = false })
	if got := off.stickyLinesAt(19 * 3); len(got.Lines) != 0 {
		t.Errorf("sticky lines while it is off: %v", got.Lines)
	}
	none := stickyVM(t, nil)
	none.stopFolding()
	if got := none.stickyLinesAt(19 * 3); len(got.Lines) != 0 {
		t.Errorf("sticky lines with no regions: %v", got.Lines)
	}
	if got := (stickyLines{}).Height(19); got != 0 {
		t.Errorf("height of nothing: %v", got)
	}
}

func TestStickyHidden(t *testing.T) {
	hidden := []folding.LineSpan{{First: 3, Last: 10}, {First: 20, Last: 25}}
	cases := []struct {
		name string
		c    stickyCandidate
		want bool
	}{
		{"inside a fold", stickyCandidate{Start: 4, End: 9}, true},
		{"the same lines as the fold", stickyCandidate{Start: 3, End: 10}, true},
		{"ending on the line after the fold", stickyCandidate{Start: 3, End: 11}, true},
		{"ending two lines after it", stickyCandidate{Start: 3, End: 12}, false},
		{"starting before it", stickyCandidate{Start: 2, End: 9}, false},
		{"in the second fold", stickyCandidate{Start: 21, End: 24}, true},
		{"between them", stickyCandidate{Start: 12, End: 18}, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := stickyHidden(hidden, tc.c); got != tc.want {
				t.Errorf("stickyHidden(%+v) = %v, want %v", tc.c, got, tc.want)
			}
		})
	}
	if stickyHidden(nil, stickyCandidate{Start: 1, End: 2}) {
		t.Error("hidden with no folds")
	}
}

func TestStickyLinesFollowTheFolds(t *testing.T) {
	vm := stickyVM(t, nil)
	// folding B (1-20) hides the lines 2-20 and with them C and D: no line of them sticks
	regions := vm.fold.model.Regions()
	for i := range regions.Len() {
		if regions.Start(i) == 1 {
			vm.fold.model.ToggleCollapseState([]folding.Region{regions.Region(i)})
		}
	}
	vm.Sync()
	if first, last, ok := vm.HiddenSpan(5); !ok || first != 2 || last != 20 {
		t.Fatalf("hidden span %d-%d %v", first, last, ok)
	}
	for _, top := range []float64{0, 19, 19 * 3, 19 * 8} {
		for _, l := range vm.stickyLinesAt(top).Lines {
			if l >= 2 && l <= 20 {
				t.Errorf("at %v the line %d of a folded scope sticks", top, l)
			}
		}
	}
}

func TestStickyTreeCandidates(t *testing.T) {
	regions, err := folding.NewRegions([]int{0, 1, 3, 5, 6, 9}, []int{20, 4, 3, 8, 7, 10}, nil)
	if err != nil {
		t.Fatal(err)
	}
	tree := newStickyTree(regions)
	// regions: 0-20 { 1-4 { 3-3 (one line: not a scope) }, 5-8 { 6-7 }, 9-10 }
	cases := []struct {
		name         string
		first, last  int
		wantStarts   []int
		wantTopsLine []int // the depth of each, in rows
	}{
		{"everything", 0, 20, []int{0, 1, 5, 6, 9}, []int{0, 1, 1, 2, 1}},
		{"the first lines", 0, 2, []int{0, 1}, []int{0, 1}},
		{"in the middle of 5-8", 6, 7, []int{0, 5, 6}, []int{0, 1, 2}},
		{"after everything", 30, 40, nil, nil},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := tree.candidates(tc.first, tc.last, 19)
			var starts, depths []int
			for _, c := range got {
				starts = append(starts, c.Start)
				depths = append(depths, int(c.Top/19))
				if c.Height != 19 {
					t.Errorf("height %v", c.Height)
				}
			}
			if !slices.Equal(starts, tc.wantStarts) || !slices.Equal(depths, tc.wantTopsLine) {
				t.Errorf("candidates %v at depths %v, want %v at %v", starts, depths, tc.wantStarts, tc.wantTopsLine)
			}
		})
	}
}

func TestStickyHover(t *testing.T) {
	vm := stickyVM(t, nil)
	changes := 0
	vm.sticky.changed.AddListener(func() { changes++ })
	vm.StickyHover(1)
	vm.StickyHover(1)
	if !vm.sticky.hover || vm.sticky.line != 1 || changes != 1 {
		t.Errorf("hover %v line %d after %d changes", vm.sticky.hover, vm.sticky.line, changes)
	}
	vm.StickyHover(2)
	if vm.sticky.line != 2 || changes != 2 {
		t.Errorf("line %d after %d changes", vm.sticky.line, changes)
	}
	vm.StickyHover(-1)
	if vm.sticky.hover || changes != 3 {
		t.Errorf("hover %v after %d changes", vm.sticky.hover, changes)
	}
}

func TestStickyTreeSkipsAScopeThatStartsOnTheLineOfTheLast(t *testing.T) {
	// two scopes that start on the same line (a region found twice): one line
	regions, err := folding.NewRegions([]int{0, 0, 1}, []int{20, 20, 4}, nil)
	if err != nil {
		t.Fatal(err)
	}
	var starts []int
	for _, c := range newStickyTree(regions).candidates(0, 20, 19) {
		starts = append(starts, c.Start)
	}
	if !slices.Equal(starts, []int{0}) {
		t.Errorf("candidates %v: the second scope on a line is skipped with what it holds", starts)
	}
}
