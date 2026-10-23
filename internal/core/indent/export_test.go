package indent

import "github.com/rm4n0s/emetos/internal/core/buffer"

// ProcessedContext is the text before, after and above the range as the rules read it, with the
// brackets taken out of strings, comments and regular expressions.
func ProcessedContext(ix *Indenter, m Model, r buffer.Range) (before, after, previous string) {
	startLn, startCol, endLn, endCol := rangeLines(r)
	ctx := ix.contextAround(text{m: m}, startLn, startCol, endLn, endCol)
	return ctx.before.text, ctx.after.text, ctx.previous.text
}

// ProcessedShould asks the rules of the language of the document about a line (zero-based) as
// they are asked when the line is read, with the brackets of strings and comments taken out.
func ProcessedShould(ix *Indenter, m Model, line int) (increase, decrease, indentNext, ignore bool) {
	p := ix.processedRules(text{m: m}, ix.langs.Language(m.Language()).Rules)
	ln := line + 1
	return p.shouldIncrease(ln, nil), p.shouldDecrease(ln, nil), p.shouldIndentNextLine(ln, nil), p.shouldIgnore(ln, nil)
}
