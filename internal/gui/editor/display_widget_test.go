package editor

import (
	"testing"
	"time"

	"github.com/rm4n0s/glutter/ui"

	"github.com/rm4n0s/emetos/internal/core/buffer"
	"github.com/rm4n0s/emetos/internal/core/edit"
)

// whitespaceText has tabs, spaces of every kind and control characters.
const whitespaceText = "func main() {\n" +
	"\tx := 1  \n" +
	"\t  y  :=  2\n" +
	"    z := \"a b\"\t// tab\n" +
	"   \n" +
	"ctl: a\x01b\x7fc\u202ad\n" +
	"end"

func newDisplayRig(t *testing.T, text string, tweak func(*Options)) *mouseRig {
	t.Helper()
	r := newMouseRigSized(t, ui.Size{Width: 480, Height: 220}, text, func(o *Options) {
		o.RenderWhitespace, o.RenderControlCharacters, o.StopRenderingLineAfter = "none", true, 10000
		o.OccurrencesHighlight, o.OccurrencesHighlightDelay, o.SelectionHighlight, o.SelectionHighlightMaxLength = "singleFile", 250, true, 200
		o.WordSeparators = edit.DefaultWordSeparators
		o.CursorBlinking = "solid"
		if tweak != nil {
			tweak(o)
		}
	})
	t.Cleanup(r.vm.DisposeHighlights)
	r.settle()
	return r
}

func TestGoldenRenderWhitespace(t *testing.T) {
	for _, mode := range []string{"all", "boundary", "trailing", "selection"} {
		t.Run(mode, func(t *testing.T) {
			r := newDisplayRig(t, whitespaceText, func(o *Options) { o.RenderWhitespace = mode })
			if mode == "selection" {
				selectText(r.vm, 1, 0, 3, 14)
				r.vm.SelectionsChanged()
			} else {
				cursorAt(r.vm, 6, 0)
				r.vm.SelectionsChanged()
			}
			r.settle()
			noErrors(t, r.ts)
			r.ts.MatchesGolden("whitespace_" + mode)
		})
	}
}

func TestGoldenRulersAndControlCharacters(t *testing.T) {
	r := newDisplayRig(t, whitespaceText, func(o *Options) {
		o.Rulers = []ColumnRuler{{Column: 20}, {Column: 40, Color: ui.NewColor(0xFFFF0000)}}
	})
	noErrors(t, r.ts)
	r.ts.MatchesGolden("rulers_control")
}

func TestGoldenOccurrenceHighlights(t *testing.T) {
	const text = "foo := bar(foo, foobar)\nfoo.baz(bar)\nreturn foo + bar\n"
	t.Run("word", func(t *testing.T) {
		r := newDisplayRig(t, text, func(o *Options) { o.SelectionHighlight = false })
		cursorAt(r.vm, 0, 1)
		r.vm.SelectionsChanged()
		r.ts.Pump(300 * time.Millisecond)
		r.settle()
		noErrors(t, r.ts)
		r.ts.MatchesGolden("highlight_word")
	})
	t.Run("selection", func(t *testing.T) {
		r := newDisplayRig(t, text, func(o *Options) { o.OccurrencesHighlight = "off" })
		selectText(r.vm, 0, 12, 0, 15)
		r.vm.SelectionsChanged()
		r.settle()
		noErrors(t, r.ts)
		r.ts.MatchesGolden("highlight_selection")
	})
}

func TestTypingMovesTheWordHighlightAway(t *testing.T) {
	r := newDisplayRig(t, "foo bar foo\n", func(o *Options) { o.SelectionHighlight = false })
	cursorAt(r.vm, 0, 1)
	r.vm.SelectionsChanged()
	r.ts.Pump(300 * time.Millisecond)
	if got := len(occurrences(r.vm, wordHighlightOwner)); got != 2 {
		t.Fatalf("%d marks", got)
	}
	if err := r.vm.RunCommand("type", map[string]any{"text": "x"}); err != nil {
		t.Fatal(err)
	}
	r.ts.Pump(time.Second)
	if got := len(occurrences(r.vm, wordHighlightOwner)); got != 0 {
		t.Errorf("%d marks after typing", got)
	}
	noErrors(t, r.ts)
}

func TestWhitespaceFollowsTheOptions(t *testing.T) {
	r := newDisplayRig(t, "a  b\n", nil)
	r.vm.SetOptions(func() Options { o := r.vm.Options(); o.RenderWhitespace = "all"; return o }())
	r.settle()
	noErrors(t, r.ts)
	// the gutter and text stay where they were: nothing moved
	if got := r.vm.Cursors().Primary().Position(); got != (buffer.Position{}) {
		t.Errorf("cursor %v", got)
	}
}
