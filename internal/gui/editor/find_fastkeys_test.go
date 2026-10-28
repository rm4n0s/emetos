package editor

import (
	"testing"
	"time"

	"github.com/rm4n0s/glutter/gluttertest"
	"github.com/rm4n0s/glutter/services"
	"github.com/rm4n0s/glutter/ui"

	"github.com/rm4n0s/emetos/internal/gui/kit"
)

func TestFastKeysAfterCtrlFSearch(t *testing.T) {
	for _, tc := range []struct {
		name  string
		pumps []int // frames pumped before each key
	}{
		{"all before the first frame", []int{0, 0, 0}},
		{"one frame after the command", []int{1, 0, 0}},
		{"a frame between each", []int{0, 1, 1}},
		{"two frames then keys", []int{2, 0, 0}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			r := newFindRig(t, ui.Size{Width: 640, Height: 300}, kit.DarkModern())
			cursorAt(r.vm, 0, 0)
			if err := r.vm.RunCommand("actions.find", nil); err != nil {
				t.Fatal(err)
			}
			for i, c := range "gam" {
				for range tc.pumps[i] {
					r.ts.Pump(time.Millisecond)
				}
				l := services.LogicalKeyboardKey(c)
				r.ts.SendKeyDownEvent(l, gluttertest.KeyOpts{Character: string(c)})
				r.ts.SendKeyUpEvent(l)
			}
			for range 4 {
				r.ts.Pump(time.Millisecond)
			}
			st := r.vm.FindState()
			if st.SearchString() != "gam" {
				t.Errorf("search %q, want gam", st.SearchString())
			}
			if got := primary(r.vm); got != rng(1, 5, 1, 8) {
				t.Errorf("the cursor is at %v, want the first match", got)
			}
		})
	}
}
