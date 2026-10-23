package edit

import (
	"github.com/rm4n0s/emetos/internal/core/buffer"
	"github.com/rm4n0s/emetos/internal/core/document"
)

// Paste replaces every selection by text and leaves a cursor after each copy. The text is
// inserted as it is, with its line breaks normalized and no indentation added, and the edit
// is an undo step of its own.
//
// Ported from VS Code: src/vs/editor/common/cursor/cursorTypeEditOperations.ts (PasteOperation, the plain case)
func (c Commands) Paste(sels []buffer.Selection, text string) Result {
	text = normalizeNewlines(text)
	plans := make([]plan, len(sels))
	for i, s := range sels {
		plans[i] = replacePlan(c.validRange(s), text)
	}
	return build(sels, plans, document.KindOther)
}
