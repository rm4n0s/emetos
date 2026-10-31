package editor

import (
	"slices"
	"testing"

	"github.com/rm4n0s/glutter/ui"
)

// The hooks of the peek of a change in the editor (step 24.12): the click on a stripe, the commands and the context key. The peek itself is
// internal/gui/quickdiffpeek; the editor is given a fake one.

// fakePeeker records what the editor asks of the peek.
type fakePeeker struct {
	calls   []string
	visible bool
}

func (p *fakePeeker) Step(next bool) {
	if next {
		p.calls = append(p.calls, "next")
	} else {
		p.calls = append(p.calls, "previous")
	}
}
func (p *fakePeeker) Click(line int) { p.calls = append(p.calls, "click "+itoa(line)) }
func (p *fakePeeker) Close()         { p.calls = append(p.calls, "close") }
func (p *fakePeeker) ToggleFocus()   { p.calls = append(p.calls, "toggle") }
func (p *fakePeeker) Visible() bool  { return p.visible }

// gutterTap presses and releases the primary mouse button at a point of the editor.
func gutterTap(r *minimapRig, x, y int) {
	r.ts.TapAt(ui.Offset{Dx: float64(x), Dy: float64(y)}, r.mouse())
	r.ts.Pump()
}

func TestAClickOnAStripeAsksForThePeekAndMovesNoCursor(t *testing.T) {
	r := quickDiffRig(t, "Dark Modern", func(o *Options) { o.QuickDiff.GutterAction = "diff" }, nil)
	peek := &fakePeeker{}
	r.vm.SetQuickDiffPeeker(peek)
	before := r.vm.Cursors().Primary().Position()
	x := stripeX(r.vm, r.ts) + 1
	tests := []struct {
		name string
		x, y int
		want []string
	}{
		{"the first line of the added block", x, rowY(5), []string{"click 6"}},
		{"the last line of the modified block", x, rowY(11), []string{"click 12"}},
		{"the line the lines were deleted after", x, rowY(13), []string{"click 14"}},
		{"three pixels to the left of the stripe", x - 1 - 3, rowY(9), []string{"click 10"}},
		{"a line that has no mark", x, rowY(7), nil},
		{"far from the stripe", x + 20, rowY(5), nil},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			peek.calls = nil
			gutterTap(r, tt.x, tt.y)
			if !slices.Equal(peek.calls, tt.want) {
				t.Errorf("the peek was asked %v, want %v", peek.calls, tt.want)
			}
			if len(tt.want) > 0 && r.vm.Cursors().Primary().Position() != before {
				t.Errorf("a click on a stripe moved the cursor to %+v", r.vm.Cursors().Primary().Position())
			}
		})
	}
}

func TestAClickOnAStripeNeedsTheGutterActionAndTheGutterMarks(t *testing.T) {
	tests := []struct {
		name  string
		tweak func(o *Options)
		peek  bool
	}{
		{"the action is none", func(o *Options) { o.QuickDiff.GutterAction = "none" }, true},
		{"the marks are on the ruler only", func(o *Options) { o.QuickDiff.GutterAction = "diff"; o.QuickDiff.Decorations = "overview" }, true},
		{"no peek was made for the editor", func(o *Options) { o.QuickDiff.GutterAction = "diff" }, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			r := quickDiffRig(t, "Dark Modern", tt.tweak, nil)
			peek := &fakePeeker{}
			if tt.peek {
				r.vm.SetQuickDiffPeeker(peek)
			}
			gutterTap(r, stripeX(r.vm, r.ts)+1, rowY(5))
			if len(peek.calls) != 0 {
				t.Errorf("the peek was asked %v", peek.calls)
			}
		})
	}
}

func TestAPressOnAStripeThatIsReleasedElsewhereOpensNothing(t *testing.T) {
	r := quickDiffRig(t, "Dark Modern", func(o *Options) { o.QuickDiff.GutterAction = "diff" }, nil)
	peek := &fakePeeker{}
	r.vm.SetQuickDiffPeeker(peek)
	x := float64(stripeX(r.vm, r.ts) + 1)
	g := r.ts.StartGesture(ui.Offset{Dx: x, Dy: float64(rowY(5))}, r.mouse())
	g.MoveTo(ui.Offset{Dx: x, Dy: float64(rowY(10))})
	g.Up()
	r.ts.Pump()
	if len(peek.calls) != 0 {
		t.Errorf("the peek was asked %v after a release on another line", peek.calls)
	}
}

func TestThePeekCommandsGoToThePeekOfTheEditor(t *testing.T) {
	vm := quickDiffDoc(20)
	for _, id := range []string{"editor.action.dirtydiff.next", "editor.action.dirtydiff.previous", "closeQuickDiff", "toggleQuickDiffWidgetFocus"} {
		if err := vm.RunCommand(id, nil); err != nil {
			t.Errorf("%s with no peek: %v", id, err)
		}
	}
	peek := &fakePeeker{}
	vm.SetQuickDiffPeeker(peek)
	for _, id := range []string{"editor.action.dirtydiff.next", "editor.action.dirtydiff.previous", "closeQuickDiff", "toggleQuickDiffWidgetFocus"} {
		if err := vm.RunCommand(id, nil); err != nil {
			t.Errorf("%s: %v", id, err)
		}
	}
	if want := []string{"next", "previous", "close", "toggle"}; !slices.Equal(peek.calls, want) {
		t.Errorf("the peek was asked %v, want %v", peek.calls, want)
	}
	vm.SetQuickDiffPeeker(nil)
	if vm.QuickDiffPeekVisible() {
		t.Error("an editor with no peek shows one")
	}
}

func TestTheEditorSetsTheKeysOfTheWindowAndTheDiffEditor(t *testing.T) {
	vm := quickDiffDoc(5)
	vm.SetContextKeys("window", func() map[string]any { return map[string]any{"resourceScheme": "file"} })
	vm.SetContextKeys("diffEditor", func() map[string]any { return map[string]any{"isInDiffRightEditor": true} })
	if len(vm.contextKeys) != 2 {
		t.Fatalf("%d sets of keys", len(vm.contextKeys))
	}
	vm.SetContextKeys("window", func() map[string]any { return map[string]any{"resourceScheme": "git"} })
	if got := vm.contextKeys["window"]()["resourceScheme"]; got != "git" || len(vm.contextKeys) != 2 {
		t.Errorf("a second set replaced the first? %v, %d sets", got, len(vm.contextKeys))
	}
	vm.SetContextKeys("window", nil)
	vm.SetContextKeys("diffEditor", nil)
	vm.SetContextKeys("never", nil)
	if len(vm.contextKeys) != 0 {
		t.Errorf("%d sets of keys are left", len(vm.contextKeys))
	}
}
