package editor

import (
	"strings"
	"testing"
	"time"

	"github.com/rm4n0s/emetos/internal/gui/kit"
	"github.com/rm4n0s/emetos/internal/gui/testutil"
	"github.com/rm4n0s/glutter/ui"
)

// A repaint is a call of the view model's listeners: the editor sets its state, which is a frame. A keystroke in a tokenized line is
// one repaint, the one the edit itself makes: the tokens the frame needs are made in the frame, and the view model is not told
// about them (a second frame for every key of a held key costs more than the period of its repeat under a software renderer).
// A view that did not make the edit, and lines the background loop tokenized, repaint.
func TestTokensRepaintOnlyForWhatTheFrameDidNotDraw(t *testing.T) {
	var text strings.Builder
	for range 30 {
		text.WriteString("var x = 1 // a line\n")
	}
	tests := []struct {
		name string
		// slow leaves the lines of the screen but the first to the background loop (the frame's budget is spent at once).
		slow bool
		// act does what the test is about, after the editor is shown and settled.
		act func(t *testing.T, vm, other *ViewModel)
		// own and others are the repaints the editor and a second view of the document should make.
		own, others func(n int) bool
	}{
		{
			name: "a key typed in a tokenized line",
			act:  func(t *testing.T, vm, _ *ViewModel) { typeX(t, vm) },
			own:  func(n int) bool { return n == 1 },
			// the second view is not in a window, but it shows the lines
			others: func(n int) bool { return n >= 1 },
		},
		{
			name: "two keys typed one after the other",
			act: func(t *testing.T, vm, _ *ViewModel) {
				typeX(t, vm)
				typeX(t, vm)
			},
			own:    func(n int) bool { return n == 2 },
			others: func(n int) bool { return n >= 1 },
		},
		{
			name:   "the background loop tokenizes the lines on the screen",
			slow:   true,
			act:    func(t *testing.T, vm, _ *ViewModel) { vm.Tokenizer().Settle() },
			own:    func(n int) bool { return n >= 1 },
			others: func(n int) bool { return n >= 1 },
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			vm, tok := highlighted(t, text.String())
			if tt.slow {
				vm.tok.budget = time.Nanosecond
			}
			ts := testutil.New(t, ui.Size{Width: 500, Height: 300})
			pumpHighlighted(t, ts, vm, kit.DarkModern(), darkModernTokens(t))
			for range 3 {
				ts.Pump(16 * time.Millisecond)
			}
			if !tt.slow {
				tok.Settle()
				ts.Pump(16 * time.Millisecond)
				ts.Pump(16 * time.Millisecond)
			}
			// a second view of the document, as a split editor has: it shows the first lines
			other := NewViewModel(vm.Document(), goOptions(), nil)
			other.SetTokenizer(tok)
			other.tok.first, other.tok.last, other.tok.visible = 0, 20, true

			var own, others int
			vm.AddListener(func() { own++ })
			other.AddListener(func() { others++ })
			tt.act(t, vm, other)
			for range 3 {
				ts.Pump(16 * time.Millisecond)
			}
			if !tt.own(own) {
				t.Errorf("the editor repainted %d times", own)
			}
			if !tt.others(others) {
				t.Errorf("the second view repainted %d times", others)
			}
			noErrors(t, ts)
		})
	}
}

func typeX(t *testing.T, vm *ViewModel) {
	t.Helper()
	if err := vm.RunCommand("type", "x"); err != nil {
		t.Fatal(err)
	}
}
