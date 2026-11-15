package app

import (
	"testing"

	"github.com/rm4n0s/glutter/services"

	"github.com/rm4n0s/emetos/extension"
)

// redrawnBy is the largest part of the picture, in percent, that a frame of the view redrew while do ran and the view settled.
// The frames of the view are counted with the pixels they redrew (headlessView.redrawn), so the answer is the mean of the
// frames, which is what a client of Remote Work pays for each of them.
func (r *viewRig) redrawnBy(t *testing.T, do func()) int {
	t.Helper()
	frames, pixels := r.view.frames.Load(), r.view.redrawn.Load()
	do()
	r.settle(t)
	n := r.view.frames.Load() - frames
	if n == 0 {
		return 0
	}
	size := r.view.Size()
	return int(100 * (r.view.redrawn.Load() - pixels) / (n * int64(size.Width*size.Height)))
}

// TestAChangeInOnePartOfTheViewRedrawsThatPartOnly guards what a frame of a view redraws (SPEC §10.18 "Speed"): the software
// renderer of a headless view redraws what a frame damages. Before the parts, the menu row and the sashes had layers of their
// own every frame damaged the whole picture; with them, and Glutter before 1.17.3, a frame damaged the whole part that changed
// (a typed character 75% of the picture). Glutter's damage is now by what a picture draws, so a frame redraws what changed.
func TestAChangeInOnePartOfTheViewRedrawsThatPartOnly(t *testing.T) {
	r := newProjectRig(t, extension.Size{Width: 1280, Height: 800}, "Dark Modern")
	r.settle(t)
	tests := []struct {
		name string
		do   func()
		// most is the largest part of the picture a frame may redraw, in percent
		most int
	}{
		// the pointer goes from one row of the Explorer to another (go.mod and README.md; the row between them is the selected
		// one, whose hover shows nothing): the tiles of the two rows, not the side bar
		{name: "the pointer moves over the Explorer", most: 6, do: func() {
			r.view.SendPointer(extension.PointerEvent{Kind: extension.PointerMove, X: 100, Y: 120})
			r.pump()
			r.view.SendPointer(extension.PointerEvent{Kind: extension.PointerMove, X: 100, Y: 164})
			r.pump()
		}},
		// a typed character: the row of the text, the minimap, the dirty mark of the tab and the status bar, not the editor
		{name: "a character is typed", most: 20, do: func() {
			r.key(services.LogicalKeyboardKeyKeyX, services.PhysicalKeyboardKeyKeyX, "x")
		}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// once before it is measured: the first time shows what later ones only move (a hover, a dirty mark on the tab)
			tt.do()
			r.settle(t)
			if got := r.redrawnBy(t, tt.do); got > tt.most {
				t.Errorf("a frame redrew %d%% of the picture, want at most %d%%", got, tt.most)
			} else {
				t.Logf("a frame redrew %d%% of the picture", got)
			}
		})
	}
}
