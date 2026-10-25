package editor

import (
	"image/color"
	"reflect"
	"testing"

	"github.com/rm4n0s/glutter/ui"
)

func TestLineBlocksAreKeptAndNotified(t *testing.T) {
	vm := NewViewModel(newTestDoc("a\nb\nc\nd\n"), testOptions(), nil)
	red := ui.NewColor(0xFFFF0000)
	blocks := []LineBlock{{StartLine: 1, EndLine: 3, Color: red, Width: 2}, {StartLine: 4, EndLine: 4, Color: red}}
	notified := 0
	vm.AddListener(func() { notified++ })
	vm.SetLineBlocks(blocks)
	if got := vm.LineBlocks(); !reflect.DeepEqual(got, blocks) || notified != 1 {
		t.Fatalf("blocks %+v told %d times, want %+v", got, notified, blocks)
	}
	// the same blocks again are no change
	vm.SetLineBlocks(blocks)
	if notified != 1 {
		t.Errorf("told %d times for the same blocks", notified)
	}
	// the caller's slice is not shared
	blocks[0].StartLine = 0
	if vm.LineBlocks()[0].StartLine != 1 {
		t.Error("the blocks share the slice of the caller")
	}
	vm.SetLineBlocks(nil)
	if len(vm.LineBlocks()) != 0 || notified != 2 {
		t.Errorf("blocks after clearing: %+v, told %d", vm.LineBlocks(), notified)
	}
}

func TestLineBlocksAreDrawn(t *testing.T) {
	r := newMouseRig(t, "one\ntwo\nthree\nfour\nfive\n", func(o *Options) { o.RenderLineHighlight = "none" })
	r.settle()
	c := ui.NewColor(0xFF112233)
	r.vm.SetLineBlocks([]LineBlock{
		{StartLine: 1, EndLine: 3, Color: c, Width: 2}, // two lines, a thick border
		{StartLine: 4, EndLine: 4, Color: c},           // no lines: a line before the fifth
	})
	r.settle()
	rgba := color.NRGBA{R: 0x11, G: 0x22, B: 0x33, A: 0xFF}
	far := int(r.gutter) + 500
	row := func(n int) int { return int(float64(n) * r.lineH) }
	for _, tc := range []struct {
		name string
		x, y int
		want bool
	}{
		{"top border of the block", far, row(1), true},
		{"top border is two pixels thick", far, row(1) + 1, true},
		{"inside the block", far, row(1) + 8, false},
		{"between the lines of the block, away from the border", far, row(2), false},
		{"bottom border", far, row(3) - 1, true},
		{"below the block", far, row(3) + 5, false},
		{"the line of an empty block", far, row(4), true},
		{"above the empty block", far, row(4) - 4, false},
		{"under the empty block, past its line", far, row(4) + 4, false},
	} {
		if got := pixel(r.ts, tc.x, tc.y) == rgba; got != tc.want {
			t.Errorf("%s: drawn %v at (%d, %d), want %v", tc.name, got, tc.x, tc.y, tc.want)
		}
	}
	// the left border is drawn where the text starts, over the first column
	if got := pixel(r.ts, int(r.gutter), row(1)+8); got == (color.NRGBA{R: 31, G: 31, B: 31, A: 0xFF}) {
		t.Errorf("no left border: %v", got)
	}
	noErrors(t, r.ts)
}
