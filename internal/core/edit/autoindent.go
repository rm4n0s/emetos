package edit

import (
	"strings"
	"unicode/utf8"

	"github.com/rm4n0s/emetos/internal/core/buffer"
	"github.com/rm4n0s/emetos/internal/core/document"
	"github.com/rm4n0s/emetos/internal/core/indent"
	"github.com/rm4n0s/emetos/internal/core/language"
	"github.com/rm4n0s/emetos/internal/core/tokens"
)

// TokenSource gives the indentation rules the tokens of a document and its language (SPEC
// §8.6.2). A source that tokenizes in the background brings a line up to date in LineTokens.
type TokenSource interface {
	// LineTokens are the tokens of a line (zero-based), or nil when it has none.
	LineTokens(line int) []tokens.Token
	// Language is the code of the language of the document.
	Language() int
	// CheapToTokenize reports whether the tokens of the line are known or can be had at once.
	CheapToTokenize(line int) bool
}

// indentModel is the text of the commands with the tokens of the source: an indent.Model.
type indentModel struct {
	Model
	src TokenSource
}

func (m indentModel) LineTokens(line int) []tokens.Token { return m.src.LineTokens(line) }
func (m indentModel) Language() int                      { return m.src.Language() }
func (m indentModel) CheapToTokenize(line int) bool      { return m.src.CheapToTokenize(line) }

// indentModel is the text with its tokens, or false when the options have no indentation rules.
func (c Commands) indentModel() (indentModel, bool) {
	if c.Options.Indenter == nil || c.Options.Tokens == nil {
		return indentModel{}, false
	}
	return indentModel{Model: c.Model, src: c.Options.Tokens}, true
}

// converter shifts indentations by the tab size and the use of spaces of the options.
type converter struct{ o Options }

func (c converter) Shift(indentation string) string {
	return shiftedIndent(indentation, len(indentation), c.o.tabSize(), c.o.indentSize(), c.o.InsertSpaces, false)
}

func (c converter) Unshift(indentation string) string {
	return shiftedIndent(indentation, len(indentation), c.o.tabSize(), c.o.indentSize(), c.o.InsertSpaces, true)
}

func (c converter) Normalize(text string) string {
	return NormalizeIndentation(text, c.o.indentSize(), c.o.InsertSpaces)
}

func (c Commands) converter() converter { return converter{c.Options} }

// normalize is the Normalize of the converter.
func (c Commands) normalize(text string) string { return c.converter().Normalize(text) }

// textPlan is the plan of a line break: replace r by text and leave the cursor after it, or,
// with keepPosition, before it.
func textPlan(r buffer.Range, text string, keepPosition bool) plan {
	p := replacePlan(r, text)
	if keepPosition {
		p.selection = func(_ *mapper, now []buffer.Range) buffer.Selection { return buffer.NewCursor(now[0].Start) }
	}
	return p
}

// enterPlan replaces r by a line break and the indentation of the new line, as editor.autoIndent
// says: nothing, the indentation of the line before r, that and the indentation after an opening
// bracket or between a pair of brackets, the on-enter rules of the language, and the indentation
// rules of the language. With keepPosition the cursor stays before the line break.
//
// Ported from VS Code: cursorTypeEditOperations.ts (EnterOperation._enter)
func (c Commands) enterPlan(r buffer.Range, keepPosition bool) plan {
	o := c.Options
	if o.AutoIndent == indent.AutoIndentNone {
		return textPlan(r, "\n", keepPosition)
	}
	line := c.Model.LineContent(r.Start.Line)
	kept := leadingWhitespace(line)
	kept = kept[:min(len(kept), r.Start.Col)]
	im, ok := c.indentModel()
	if o.AutoIndent == indent.AutoIndentKeep || !ok || !im.CheapToTokenize(r.Start.Line) {
		return textPlan(r, "\n"+c.normalize(kept), keepPosition)
	}
	ix := o.Indenter
	conv := c.converter()
	if res, found := ix.EnterAction(o.AutoIndent, im, r); found {
		switch res.Indent {
		case language.IndentOutdent:
			normal := c.normalize(res.Indentation)
			increased := c.normalize(res.Indentation + res.AppendText)
			p := textPlan(r, "\n"+increased+"\n"+normal, keepPosition)
			if !keepPosition {
				// The cursor goes to the end of the middle line.
				p.selection = func(_ *mapper, now []buffer.Range) buffer.Selection {
					return buffer.NewCursor(buffer.Position{Line: now[0].Start.Line + 1, Col: len(increased)})
				}
			}
			return p
		case language.IndentOutdentOne:
			return textPlan(r, "\n"+c.normalize(conv.Unshift(res.Indentation)+res.AppendText), keepPosition)
		default:
			return textPlan(r, "\n"+c.normalize(res.Indentation+res.AppendText), keepPosition)
		}
	}
	if o.AutoIndent >= indent.AutoIndentFull {
		if _, after, found := ix.IndentForEnter(o.AutoIndent, im, r, conv); found {
			endLine := c.Model.LineContent(r.End.Line)
			oldEndVisible := VisibleColumn(endLine, r.End.Col, o.tabSize())
			oldEndCol := r.End.Col
			wide := r
			if first := firstNonWhitespace(endLine); first >= 0 {
				wide.End.Col = max(r.End.Col, first)
			} else {
				wide.End.Col = len(endLine)
			}
			text := "\n" + c.normalize(after)
			if keepPosition {
				return textPlan(wide, text, true)
			}
			// When the cursor was inside the indentation of the line it goes on, at the same
			// place, into the new indentation.
			offset := 0
			if first := firstNonWhitespace(endLine); oldEndCol <= first {
				if !o.InsertSpaces {
					oldEndVisible = (oldEndVisible + o.indentSize() - 1) / o.indentSize()
				}
				offset = min(oldEndVisible-len(c.normalize(after)), 0)
			}
			p := replacePlan(wide, text)
			p.selection = func(_ *mapper, now []buffer.Range) buffer.Selection {
				return buffer.NewCursor(now[0].End.Delta(0, offset))
			}
			return p
		}
	}
	return textPlan(r, "\n"+c.normalize(kept), keepPosition)
}

// typeAutoIndent types ch at every cursor and, when that makes the line match
// decreaseIndentPattern of its language (the closing brace of a block, `end`), re-indents the line
// at once (editor.autoIndent "full"). It reports false, and does nothing, when the level is lower,
// the rules say nothing for a cursor, or the line already has the indentation.
//
// Ported from VS Code: cursorTypeEditOperations.ts (AutoIndentOperation)
func (c Commands) typeAutoIndent(sels []buffer.Selection, ch string) (Result, bool) {
	o := c.Options
	im, ok := c.indentModel()
	if !ok || o.AutoIndent < indent.AutoIndentFull {
		return Result{}, false
	}
	ranges := make([]buffer.Range, len(sels))
	for i, s := range sels {
		ranges[i] = c.validRange(s)
		if !im.CheapToTokenize(ranges[i].End.Line) {
			return Result{}, false
		}
	}
	plans := make([]plan, len(sels))
	for i, r := range ranges {
		actual, found := o.Indenter.IndentActionForType(o.AutoIndent, im, r, ch, c.converter())
		if !found {
			return Result{}, false
		}
		line := c.Model.LineContent(r.Start.Line)
		current := leadingWhitespace(line)
		current = current[:min(len(current), r.Start.Col)]
		if actual == c.normalize(current) {
			return Result{}, false
		}
		text := c.normalize(actual)
		if first := firstNonWhitespace(line); first >= 0 {
			// JavaScript's substring swaps its arguments when the first is the larger.
			text += line[min(first, r.Start.Col):max(first, r.Start.Col)]
		}
		plans[i] = replacePlan(buffer.Range{Start: buffer.Position{Line: r.Start.Line}, End: r.End}, text+ch)
	}
	return build(sels, plans, document.KindOther), true
}

// Reindent is Reindent Lines: the lines first to last (zero-based, both included) get the
// indentation the rules of the language want, taking the first line as it is. The cursors follow
// the text.
//
// Ported from VS Code: src/vs/editor/contrib/indentation/browser/indentation.ts
// (ReindentLinesAction, ReindentSelectedLinesAction)
func (c Commands) Reindent(sels []buffer.Selection, first, last int) Result {
	return c.reindent(sels, [][2]int{{first, last}})
}

// ReindentSelected is Reindent Selected Lines: the lines of each selection are reindented with
// the line above the first one as the base, and the last line of a selection that ends at the
// start of a line is left out.
func (c Commands) ReindentSelected(sels []buffer.Selection) Result {
	var spans [][2]int
	for _, s := range sels {
		r := c.validRange(s)
		start, end := r.Start.Line, r.End.Line
		if start != end && r.End.Col == 0 {
			end--
		}
		if start == 0 {
			if start == end {
				continue
			}
		} else {
			start--
		}
		spans = append(spans, [2]int{start, end})
	}
	return c.reindent(sels, spans)
}

func (c Commands) reindent(sels []buffer.Selection, spans [][2]int) Result {
	im, ok := c.indentModel()
	if !ok {
		return Result{Selections: append([]buffer.Selection(nil), sels...), Kind: document.KindOther}
	}
	var edits []document.Edit
	seen := map[buffer.Range]bool{}
	for _, span := range spans {
		for _, e := range c.Options.Indenter.ReindentEdits(im, c.converter(), span[0], span[1]) {
			r := buffer.Range{Start: buffer.Position{Line: e.Line, Col: e.Start}, End: buffer.Position{Line: e.Line, Col: e.End}}
			if !seen[r] {
				seen[r] = true
				edits = append(edits, document.Edit{Range: r, Text: e.Text})
			}
		}
	}
	return c.followEdits(sels, edits, false)
}

// followEdits applies edits (sorted by position and not overlapping) and moves the selections
// along with the text, as decorations move.
func (c Commands) followEdits(sels []buffer.Selection, edits []document.Edit, caretStaysBefore bool) Result {
	if len(edits) == 0 || len(sels) == 0 {
		return Result{Selections: append([]buffer.Selection(nil), sels...), Kind: document.KindOther}
	}
	plans := make([]plan, len(sels))
	plans[0] = plan{edits: edits, selection: func(m *mapper, _ []buffer.Range) buffer.Selection { return m.track(sels[0], caretStaysBefore) }}
	for i := 1; i < len(sels); i++ {
		s := sels[i]
		plans[i] = plan{custom: true, selection: func(m *mapper, _ []buffer.Range) buffer.Selection { return m.track(s, caretStaysBefore) }}
	}
	return build(sels, plans, document.KindOther)
}

// AutoIndentPaste fits the text just pasted into range r to the indentation around it
// (editor.autoIndentOnPaste). sel is the selection after the paste. It works for one selection
// only, and not at all below the level "full".
//
// Ported from VS Code: contrib/indentation/browser/indentation.ts (AutoIndentOnPaste)
func (c Commands) AutoIndentPaste(sel buffer.Selection, r buffer.Range) Result {
	o := c.Options
	im, ok := c.indentModel()
	if !ok || !o.AutoIndentOnPaste || o.AutoIndent < indent.AutoIndentFull {
		return Result{Selections: []buffer.Selection{sel}, Kind: document.KindOther}
	}
	found := o.Indenter.PasteEdits(o.AutoIndent, im, c.converter(), o.tabSize(), o.InsertSpaces, o.AutoIndentOnPasteWithinString, r)
	edits := make([]document.Edit, len(found))
	for i, e := range found {
		edits[i] = document.Edit{Range: rangeOf(e.Line, e.Start, e.Line, e.End), Text: e.Text}
	}
	stays := len(edits) == 1 && sel.IsEmpty() && edits[0].Range.Start == sel.End()
	return c.followEdits([]buffer.Selection{sel}, edits, stays)
}

// isSingleChar reports whether text is one character (a code point, with no line break).
func isSingleChar(text string) bool {
	_, n := utf8.DecodeRuneInString(text)
	return n > 0 && n == len(text) && !strings.ContainsAny(text, "\r\n")
}

// cheapToTokenize reports whether the rules of the languages may read line: they cannot when
// there are none, and then Tab uses one level.
func (c Commands) cheapToTokenize(line int) bool {
	im, ok := c.indentModel()
	return !ok || im.CheapToTokenize(line)
}

// blankLineIndent is the indentation Tab puts on the blank line (normalized): what the rules of the
// language suggest, or else one level.
//
// Ported from VS Code: cursorTypeEditOperations.ts (TabOperation)
func (c Commands) blankLineIndent(line int) string {
	good := "\t"
	if im, ok := c.indentModel(); ok {
		if g, found := c.Options.Indenter.IndentForBlankLine(c.Options.AutoIndent, im, line, c.converter()); found {
			good = g
		}
	}
	return c.normalize(good)
}
