package diffeditor

import "github.com/rm4n0s/emetos/internal/gui/editor"

// AdjustOptions are the options an editor of the diff editor works with, from the options the settings give it
// (VS Code's DiffEditorWidget adjusts the options of its two editors the same way): the original editor is
// read-only, and the minimap, the folding controls and sticky scroll are off in both, since the diff editor has
// the overview ruler, and rows that fold or stick would not line up with the other side.
func AdjustOptions(o editor.Options, s Side) editor.Options {
	o.ReadOnly = s == Original
	o.Folding = false
	o.Minimap.Enabled = false
	o.StickyScroll.Enabled = false
	return o
}
