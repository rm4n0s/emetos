package edit

import (
	"strings"

	"github.com/rm4n0s/emetos/internal/core/buffer"
	"github.com/rm4n0s/emetos/internal/core/document"
)

// ConvertIndentation is Convert Indentation to Spaces (toSpaces) and to Tabs: in the
// indentation of every line, which is all of a line that has only white space, each tab
// becomes as many spaces as the tab size, or each run of as many spaces as the tab size becomes a
// tab. The selection is moved along with the text; the other cursors are not part of the
// command, as in VS Code, which works on the primary selection only.
//
// Ported from VS Code: src/vs/editor/contrib/indentation/browser/indentation.ts
// (IndentationToSpacesCommand, IndentationToTabsCommand, getIndentationEditOperations)
func (c Commands) ConvertIndentation(sel buffer.Selection, toSpaces bool) Result {
	m := c.Model
	if m.LineCount() == 1 && m.LineContent(0) == "" {
		return Result{Selections: []buffer.Selection{sel}, Kind: document.KindOther}
	}
	spaces := strings.Repeat(" ", c.Options.tabSize())
	var edits []document.Edit
	for line := 0; line < m.LineCount(); line++ {
		text := m.LineContent(line)
		end := firstNonWhitespace(text)
		if end < 0 {
			end = len(text)
		}
		if end == 0 {
			continue
		}
		old := text[:end]
		var now string
		if toSpaces {
			now = strings.ReplaceAll(old, "\t", spaces)
		} else {
			now = strings.ReplaceAll(old, spaces, "\t")
		}
		if now != old {
			edits = append(edits, document.Edit{Range: rangeOf(line, 0, line, end), Text: now})
		}
	}
	if len(edits) == 0 {
		return Result{Selections: []buffer.Selection{sel}, Kind: document.KindOther}
	}
	p := plan{edits: edits, custom: true, selection: func(mp *mapper, _ []buffer.Range) buffer.Selection { return mp.track(sel, false) }}
	return build([]buffer.Selection{sel}, []plan{p}, document.KindOther)
}
