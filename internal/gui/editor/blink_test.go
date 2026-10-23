package editor

import (
	"math"
	"testing"
	"time"
)

func TestBlinkStyleOfSetting(t *testing.T) {
	cases := []struct {
		focused bool
		setting string
		want    blinkStyle
	}{
		{false, "blink", blinkHidden}, {false, "smooth", blinkHidden},
		{true, "solid", blinkSolid}, {true, "blink", blinkFlat}, {true, "", blinkFlat}, {true, "nonsense", blinkFlat},
		{true, "smooth", blinkSmooth}, {true, "phase", blinkPhase}, {true, "expand", blinkExpand},
	}
	for _, tc := range cases {
		if got := blinkStyleOf(tc.focused, tc.setting); got != tc.want {
			t.Errorf("blinkStyleOf(%v, %q) = %v, want %v", tc.focused, tc.setting, got, tc.want)
		}
	}
}

func near01(a, b float64) bool { return math.Abs(a-b) < 1e-9 }

func TestAnimationFrames(t *testing.T) {
	const it = BlinkInterval
	cases := []struct {
		name    string
		style   blinkStyle
		elapsed time.Duration
		want    cursorFrame
		done    bool
	}{
		{"shown during the first half second", blinkSmooth, 0, cursorFrame{1, 1}, false},
		{"still shown just before it starts", blinkSmooth, it - time.Millisecond, cursorFrame{1, 1}, false},
		{"held at the start of an iteration", blinkSmooth, it + it/10, cursorFrame{1, 1}, false},
		{"smooth is gone at 60%", blinkSmooth, it + it*6/10, cursorFrame{0, 1}, false},
		{"smooth is half gone halfway between 20% and 60%", blinkSmooth, it + it*4/10, cursorFrame{0.5, 1}, false},
		{"smooth stays gone until the iteration ends", blinkSmooth, it + it*9/10, cursorFrame{0, 1}, false},
		{"the next iteration runs backwards: gone at its start", blinkSmooth, 2 * it, cursorFrame{0, 1}, false},
		{"and whole at its end", blinkSmooth, 2*it + it - time.Millisecond/2, cursorFrame{1, 1}, false},
		{"phase is still partly there at 60%", blinkPhase, it + it*6/10, cursorFrame{1 - easeInOut(4.0/7), 1}, false},
		{"phase is gone at 90%", blinkPhase, it + it*9/10, cursorFrame{0, 1}, false},
		{"expand shrinks the height and keeps the opacity", blinkExpand, it + it*5/10, cursorFrame{1, 0.5}, false},
		{"expand is flat at 80%", blinkExpand, it + it*8/10, cursorFrame{1, 0}, false},
		{"the animation ends after 20 iterations", blinkSmooth, it + 20*it, cursorFrame{1, 1}, true},
		{"and not before", blinkSmooth, it + 20*it - time.Millisecond, cursorFrame{1, 1}, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, done := animationFrame(tc.style, tc.elapsed)
			if !near01(got.Opacity, tc.want.Opacity) && math.Abs(got.Opacity-tc.want.Opacity) > 0.02 ||
				!near01(got.ScaleY, tc.want.ScaleY) && math.Abs(got.ScaleY-tc.want.ScaleY) > 0.02 || done != tc.done {
				t.Errorf("animationFrame(%v, %v) = %+v, %v; want %+v, %v", tc.style, tc.elapsed, got, done, tc.want, tc.done)
			}
		})
	}
}

// easeInOut is CSS's ease-in-out for the tests that check a point of the curve by its value.
func easeInOut(t float64) float64 {
	// cubic-bezier(.42, 0, .58, 1) solved for x = t by bisection.
	lo, hi := 0.0, 1.0
	for range 60 {
		u := (lo + hi) / 2
		x := 3*(1-u)*(1-u)*u*0.42 + 3*(1-u)*u*u*0.58 + u*u*u
		if x < t {
			lo = u
		} else {
			hi = u
		}
	}
	u := (lo + hi) / 2
	return 3*(1-u)*u*u*1 + u*u*u
}

func TestEaseInOutIsSymmetric(t *testing.T) {
	if got := easeInOut(0.5); math.Abs(got-0.5) > 1e-6 {
		t.Errorf("the middle of ease-in-out: %v", got)
	}
}

// animatedRig shows an editor whose cursor blinks with an animation.
func animatedRig(t *testing.T, style string) *mouseRig {
	t.Helper()
	return newMouseRig(t, "hello world", func(o *Options) { o.CursorBlinking = style })
}

func TestAnimatedCursorFollowsTheFrames(t *testing.T) {
	r := animatedRig(t, "smooth")
	r.ts.Pump() // the first frame begins the animation
	op, scale := r.vm.CursorFrame()
	if op != 1 || scale != 1 {
		t.Errorf("shown at the start: %v %v", op, scale)
	}
	r.ts.Pump(BlinkInterval + BlinkInterval*6/10)
	if op, _ := r.vm.CursorFrame(); op > 0.05 {
		t.Errorf("the cursor fades out: %v", op)
	}
	r.ts.Pump(BlinkInterval / 2) // the iteration goes on at its end, then back
	r.ts.Pump(BlinkInterval)
	if op, _ := r.vm.CursorFrame(); op < 0 || op > 1 {
		t.Errorf("opacity out of range: %v", op)
	}
}

func TestAnimatedCursorRestartsWhenTheCursorMoves(t *testing.T) {
	r := animatedRig(t, "smooth")
	r.ts.Pump()
	r.ts.Pump(BlinkInterval + BlinkInterval*6/10)
	if op, _ := r.vm.CursorFrame(); op > 0.05 {
		t.Fatalf("faded out: %v", op)
	}
	if err := r.vm.RunCommand("cursorRight", nil); err != nil {
		t.Fatal(err)
	}
	if op, scale := r.vm.CursorFrame(); op != 1 || scale != 1 {
		t.Errorf("a move shows the cursor at once: %v %v", op, scale)
	}
	r.ts.Pump()
	r.ts.Pump(BlinkInterval - time.Millisecond)
	if op, _ := r.vm.CursorFrame(); op != 1 {
		t.Errorf("and it stays shown for half a second: %v", op)
	}
}

func TestAnimatedCursorEndsAndStopsTheFrames(t *testing.T) {
	r := animatedRig(t, "expand")
	r.ts.Pump()
	r.ts.Pump(BlinkInterval * (animationIterations + 2))
	r.ts.Pump(time.Second)
	if op, scale := r.vm.CursorFrame(); op != 1 || scale != 1 {
		t.Errorf("after the animation the cursor is whole: %v %v", op, scale)
	}
	state := r.editorState(t)
	if state.cursorAnim.running {
		t.Error("the frame loop runs after the animation is over")
	}
	// A move starts it again.
	if err := r.vm.RunCommand("cursorRight", nil); err != nil {
		t.Fatal(err)
	}
	if !state.cursorAnim.running {
		t.Error("a move starts the animation again")
	}
}

func TestNoAnimationWithoutFocusOrWithOtherStyles(t *testing.T) {
	for _, style := range []string{"blink", "solid"} {
		r := animatedRig(t, style)
		r.ts.Pump(BlinkInterval * 3)
		if op, scale := r.vm.CursorFrame(); op != 1 || scale != 1 {
			t.Errorf("%s: %v %v", style, op, scale)
		}
		if r.editorState(t).cursorAnim.running {
			t.Errorf("%s: a frame loop runs", style)
		}
	}
	r := animatedRig(t, "smooth")
	r.vm.SetFocused(false)
	r.ts.Pump(BlinkInterval * 3)
	if r.editorState(t).cursorAnim.running {
		t.Error("an editor without the focus animates nothing")
	}
}
