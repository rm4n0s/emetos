package kit

import (
	"testing"

	"github.com/rm4n0s/glutter/ui"
)

func TestMergeEditorColors(t *testing.T) {
	d, l := DarkModern(), LightModern()
	for _, tc := range []struct {
		name string
		got  ui.Color
		want uint32
	}{
		{"dark mergeEditor.change.background", d.MergeChange, 0x339BB955},
		{"dark mergeEditor.change.word.background", d.MergeChangeWord, 0x339CCC2C},
		{"light mergeEditor.change.word.background", l.MergeChangeWord, 0x669CCC2C},
		{"dark unhandled unfocused border", d.MergeUnhandledUnfocusedBorder, 0x7AFFA600},
		{"light unhandled unfocused border", l.MergeUnhandledUnfocusedBorder, 0xFFFFA600},
		{"unhandled focused border", d.MergeUnhandledFocusedBorder, 0xFFFFA600},
		{"handled unfocused border", d.MergeHandledUnfocusedBorder, 0x49868686},
		{"handled focused border", l.MergeHandledFocusedBorder, 0xCCC1C1C1},
		{"dark input 1 background", d.MergeConflictInput1Background, 0x3340C8AE},
		{"light input 2 background", l.MergeConflictInput2Background, 0x3340A6FF},
	} {
		if tc.got != ui.NewColor(tc.want) {
			t.Errorf("%s = %v, want %#08x", tc.name, tc.got, tc.want)
		}
	}
}
