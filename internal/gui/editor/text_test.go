package editor

import (
	"math"
	"testing"
	"time"

	"github.com/rm4n0s/glutter/ui"

	"github.com/rm4n0s/emetos/internal/gui/testutil"
)

func TestFamiliesAreThoseOfTheOptions(t *testing.T) {
	tests := []struct {
		name         string
		families     []string
		wantFamily   string
		wantFallback []string
	}{
		{"the first is the family, the rest the fallback", []string{"Go Mono", "monospace"}, "Go Mono", []string{"monospace"}},
		{"one family", []string{"Go Mono"}, "Go Mono", nil},
		{"no families", nil, "", nil},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			s := textStyle{opts: Options{FontFamilies: tt.families}}
			family, fallback := s.families()
			if family != tt.wantFamily || len(fallback) != len(tt.wantFallback) {
				t.Fatalf("families() = %q, %q; want %q, %q", family, fallback, tt.wantFamily, tt.wantFallback)
			}
			for i := range fallback {
				if fallback[i] != tt.wantFallback[i] {
					t.Errorf("fallback = %q, want %q", fallback, tt.wantFallback)
				}
			}
		})
	}
}

// TestATabIsATabStopWide pins that a tab takes the room of its tab stop (the width of a space, TabSize times) and
// adds no height: a row with a tab is as wide and as high as the characters and the stops make it.
func TestATabIsATabStopWide(t *testing.T) {
	ts := testutil.New(t, ui.Size{Width: 800, Height: 400})
	opts := goOptions()
	s := newTextStyle(ts.Dispatcher().TextEngine(), opts, ui.Color{})
	one := s.build("a", nil)
	charWidth := one.LongestLine()
	tests := []struct {
		text      string
		wantWidth float64
	}{
		{"\tfoo", (float64(opts.TabSize) + 3) * charWidth},
		{"ab\tc", (float64(opts.TabSize) + 1) * charWidth},
		{"\t\tx", (2*float64(opts.TabSize) + 1) * charWidth},
	}
	for _, tt := range tests {
		p := s.build(tt.text, nil)
		if math.Abs(p.LongestLine()-tt.wantWidth) > 0.5 {
			t.Errorf("width of %q = %v, want %v", tt.text, p.LongestLine(), tt.wantWidth)
		}
		if math.Abs(p.Height()-one.Height()) > 0.01 {
			t.Errorf("height of %q = %v, want %v", tt.text, p.Height(), one.Height())
		}
	}
}

// TestAFrameTokenizesWithinOneBudget pins that the layout of the text, its paint and the minimap, which
// all ask for tokens in one frame, share the budget of the frame (step 10.1: each had its own, so a page
// of lines nobody had tokenized took the sum of them, 8 ms), and that the next frame has a budget again.
// The file has far more lines than a budget can do, so the first request uses the whole budget and the
// others find none left: one slice of tokenizing in the frame, not three.
func TestAFrameTokenizesWithinOneBudget(t *testing.T) {
	vm, _ := highlighted(t, goLines(40_000))
	var posted []func()
	vm.SetPoster(func(f func()) { posted = append(posted, f) }) // the functions that end the frames
	tok := vm.Tokenizer()
	tok.Ensure(0, time.Second) // the first line pays for making the rules of the grammar
	slices := tok.Slices()

	vm.ensureTokens(0, 39_999)        // the layout
	vm.ensureTokens(0, 39_999)        // the paint
	vm.ensureMinimapTokens(0, 39_999) // the minimap
	if got := tok.Slices() - slices; got != 1 {
		t.Errorf("a frame tokenized in %d slices, want 1 (one budget)", got)
	}
	done := 0
	for done < 40_000 && tok.Line(done) != nil {
		done++
	}
	if done < 2 || done >= 40_000 {
		t.Fatalf("%d lines are done: the budget did not bound the frame", done)
	}

	for _, f := range posted { // the frame ends (with the repaint the tokenizer asked for)
		f()
	}
	slices = tok.Slices()
	vm.ensureTokens(0, 39_999)
	if got := tok.Slices() - slices; got != 1 {
		t.Errorf("the next frame tokenized in %d slices, want 1", got)
	}
	if tok.Line(done) == nil {
		t.Error("the next frame tokenized nothing")
	}
}
