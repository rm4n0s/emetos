package mergeeditor

import (
	"testing"

	"github.com/rm4n0s/emetos/internal/gui/editor"
)

func TestAdjustOptions(t *testing.T) {
	base := testOptions()
	base.Folding = true
	base.Minimap.Enabled = true
	base.StickyScroll.Enabled = true
	for _, tc := range []struct {
		side     Side
		readOnly bool
	}{{Incoming, true}, {Current, true}, {Result, false}} {
		t.Run(tc.side.Title(), func(t *testing.T) {
			got := AdjustOptions(base, tc.side)
			if got.ReadOnly != tc.readOnly || got.Folding || got.Minimap.Enabled || got.StickyScroll.Enabled || got.GlyphMargin || got.LineNumbersMinChars != 2 {
				t.Errorf("options %+v", got)
			}
			if got.FontSize != base.FontSize || got.LineNumbers != editor.LineNumbersOn {
				t.Error("the other options changed")
			}
		})
	}
	if !base.Folding || base.ReadOnly {
		t.Error("the options that were given changed")
	}
}

func TestSideNames(t *testing.T) {
	for _, tc := range []struct {
		side  Side
		title string
		input bool
	}{{Incoming, "Incoming", true}, {Current, "Current", true}, {Result, "Result", false}} {
		if tc.side.Title() != tc.title {
			t.Errorf("%d: title %q", tc.side, tc.side.Title())
		}
		if _, ok := tc.side.Input(); ok != tc.input {
			t.Errorf("%d: input %v", tc.side, ok)
		}
	}
}
