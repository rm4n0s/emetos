package mergeeditor

import "github.com/rm4n0s/emetos/internal/gui/editor"

// AdjustOptions are the options an editor of the merge editor works with, from the options the settings give it
// (VS Code's merge editor adjusts the options of its editors the same way): the minimap and the glyph margin are off,
// the line numbers need two characters at most, there is no folding, and the two inputs are read-only. Sticky scroll
// is off too, since rows that stick would not line up with the other editors.
//
// Ported from VS Code: src/vs/workbench/contrib/mergeEditor/browser/view/mergeEditor.ts (applyOptions) and
// view/editors/codeEditorView.ts (getEditorContributions leaves out folding)
func AdjustOptions(o editor.Options, s Side) editor.Options {
	o.ReadOnly = s != Result
	o.Folding = false
	o.Minimap.Enabled = false
	o.StickyScroll.Enabled = false
	o.GlyphMargin = false
	o.LineNumbersMinChars = 2
	return o
}
