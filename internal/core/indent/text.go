package indent

import (
	"github.com/rm4n0s/emetos/internal/core/buffer"
	"github.com/rm4n0s/emetos/internal/core/textmate"
	"github.com/rm4n0s/emetos/internal/core/tokens"
)

// Ported from VS Code: src/vs/editor/common/languages/supports/indentationLineProcessor.ts and
// src/vs/editor/common/languages/supports.ts (MIT)

// Model is the text that the indentation rules read, with its tokens. Lines count from zero and
// columns are byte offsets.
type Model interface {
	// LineCount is the number of lines (at least one).
	LineCount() int
	// LineContent is the text of a line without its line break.
	LineContent(line int) string
	// LineTokens are the tokens of a line, or nil when it has none (a language without a
	// grammar, or a line not tokenized): the line is then code in the language of the document.
	// A model that tokenizes in the background brings the line up to date first.
	LineTokens(line int) []tokens.Token
	// Language is the code of the language of the document.
	Language() int
	// CheapToTokenize reports whether the tokens of the line are known, or can be had without
	// waiting (VS Code's isCheapToTokenize); the rules are not applied to a line that is not.
	CheapToTokenize(line int) bool
}

// text is a Model read with one-based line numbers, as VS Code's code does, with the content of
// one line (and optionally its tokens) replaced: VS Code's virtual model. The functions of this
// package keep VS Code's numbering, so that they can be compared with it line by line; their
// public forms take zero-based lines.
type text struct {
	m Model
	// override is the one-based line whose content and tokens are replaced; 0 for none.
	override       int
	overrideText   string
	overrideToks   []tokens.Token
	overrideHasTok bool
}

// withLine gives the text with the content of line ln replaced. With toks true the tokens of the
// line are replaced too; otherwise the tokens of the model are still the line's.
func (t text) withLine(ln int, content string, toks []tokens.Token, replaceTokens bool) text {
	t.override, t.overrideText, t.overrideToks, t.overrideHasTok = ln, content, toks, replaceTokens
	return t
}

// content is the text of line ln.
func (t text) content(ln int) string {
	if ln == t.override {
		return t.overrideText
	}
	return t.m.LineContent(ln - 1)
}

// tokensOf are the tokens that line ln is read with; nil means all of the line is in the language
// of the document.
func (t text) tokensOf(ln int) []tokens.Token {
	if ln == t.override && t.overrideHasTok {
		return t.overrideToks
	}
	return t.m.LineTokens(ln - 1)
}

// scope is a run of tokens of one language in a line.
type scope struct {
	start, end int
	language   int
}

// tokenIndexAt is the index of the token that has the offset, the last one when it is past the
// end.
func tokenIndexAt(toks []tokens.Token, offset int) int {
	for i, tk := range toks {
		if offset < int(tk.End) {
			return i
		}
	}
	return len(toks) - 1
}

func tokenStart(toks []tokens.Token, i int) int {
	if i == 0 {
		return 0
	}
	return int(toks[i-1].End)
}

// scopeAt is the run of tokens of one language that holds the offset of line ln (VS Code's
// createScopedLineTokens). It always reads the tokens of the model, not those of a replaced line.
func (t text) scopeAt(ln, offset int) scope {
	toks := t.m.LineTokens(ln - 1)
	if len(toks) == 0 {
		return scope{0, len(t.m.LineContent(ln - 1)), t.m.Language()}
	}
	idx := tokenIndexAt(toks, offset)
	lang := toks[idx].Language
	first, last := idx, idx
	for last+1 < len(toks) && toks[last+1].Language == lang {
		last++
	}
	for first > 0 && toks[first-1].Language == lang {
		first--
	}
	return scope{tokenStart(toks, first), int(toks[last].End), int(lang)}
}

// languageAt is the code of the language at an offset of line ln.
func (t text) languageAt(ln, offset int) int { return t.scopeAt(ln, offset).language }

// firstLanguage is the language of the first token of line ln.
func (t text) firstLanguage(ln int) int {
	toks := t.m.LineTokens(ln - 1)
	if len(toks) == 0 {
		return t.m.Language()
	}
	return int(toks[0].Language)
}

// languageDiffersFromLineStart reports whether the offset is in a language other than the one the
// line starts in, and not at the start of a scope that begins the line.
func (t text) languageDiffersFromLineStart(ln, offset int) bool {
	sc := t.scopeAt(ln, offset)
	return sc.start != 0 && t.firstLanguage(ln) != sc.language
}

// tokenTypeAt is the standard type of the token that has the offset of line ln.
func (t text) tokenTypeAt(ln, offset int) textmate.TokenType {
	toks := t.m.LineTokens(ln - 1)
	if len(toks) == 0 {
		return textmate.TokenOther
	}
	return toks[tokenIndexAt(toks, offset)].Type
}

// lineData is a part of a line with the tokens that cover it.
type lineData struct {
	text string
	toks []tokens.Token
}

// slice is the part [from, to) of line ln with the brackets of the language taken out of its
// strings, regular expressions and comments, so that the rules do not take them for code (VS Code's
// IndentationLineProcessor.getProcessedTokens).
func (ix *Indenter) slice(t text, ln, from, to int) lineData {
	content := t.content(ln)
	from = min(max(from, 0), len(content))
	to = min(max(to, from), len(content))
	toks := t.tokensOf(ln)
	if len(toks) == 0 {
		return lineData{text: content[from:to]}
	}
	// The brackets removed are those of the language of the first token of the part.
	first := tokenIndexAt(toks, from)
	br := ix.langs.Language(int(toks[first].Language)).Brackets
	var out lineData
	buf := make([]byte, 0, to-from)
	for i := first; i < len(toks); i++ {
		start, end := max(tokenStart(toks, i), from), min(int(toks[i].End), to)
		if start >= to {
			break
		}
		if end <= start {
			continue
		}
		part := content[start:end]
		switch toks[i].Type {
		case textmate.TokenString, textmate.TokenRegEx, textmate.TokenComment:
			part = br.Strip(part)
		}
		buf = append(buf, part...)
		tk := toks[i]
		tk.End = int32(len(buf))
		out.toks = append(out.toks, tk)
	}
	// The text past the last token, if the tokens are older than the text, stays as it is.
	if last := toks[len(toks)-1]; int(last.End) < to {
		buf = append(buf, content[max(int(last.End), from):to]...)
		if n := len(out.toks); n > 0 {
			out.toks[n-1].End = int32(len(buf))
		}
	}
	out.text = string(buf)
	return out
}

// processedLine is line ln as the rules read it, with the leading white space replaced by
// newIndentation when it is not nil.
func (ix *Indenter) processedLine(t text, ln int, newIndentation *string) string {
	line := ix.slice(t, ln, 0, len(t.content(ln))).text
	if newIndentation != nil {
		line = *newIndentation + line[len(leading(line)):]
	}
	return line
}

// processed asks the indentation rules about the lines of a text, as VS Code's
// ProcessedIndentRulesSupport does.
type processed struct {
	ix    *Indenter
	t     text
	rules *Rules
}

func (ix *Indenter) processedRules(t text, rules *Rules) processed {
	return processed{ix: ix, t: t, rules: rules}
}

func (p processed) line(ln int, newIndentation *string) string {
	return p.ix.processedLine(p.t, ln, newIndentation)
}

// shouldIncrease reports whether the lines after line ln are indented one level more, with the
// indentation of ln replaced by newIndentation when it is not nil.
func (p processed) shouldIncrease(ln int, newIndentation *string) bool {
	return p.rules.ShouldIncrease(p.line(ln, newIndentation))
}

// shouldDecrease reports whether line ln is indented one level less than the line before it.
func (p processed) shouldDecrease(ln int, newIndentation *string) bool {
	return p.rules.ShouldDecrease(p.line(ln, newIndentation))
}

// shouldIgnore reports whether the indentation of line ln is left alone.
func (p processed) shouldIgnore(ln int, newIndentation *string) bool {
	return p.rules.ShouldIgnore(p.line(ln, newIndentation))
}

// shouldIndentNextLine reports whether only the line after ln is indented one level more.
func (p processed) shouldIndentNextLine(ln int, newIndentation *string) bool {
	return p.rules.ShouldIndentNextLine(p.line(ln, newIndentation))
}

// context is the text around a range, as the rules read it.
type context struct {
	before, after, previous lineData
}

// contextAround reads the text before the start of r on its line, after its end on its line, and
// the line above it, all in the language at the start of r. r is one-based in its lines and has
// byte columns.
func (ix *Indenter) contextAround(t text, startLn, startCol, endLn, endCol int) context {
	var c context
	sc := t.scopeAt(startLn, startCol)
	// The text before.
	if t.languageDiffersFromLineStart(startLn, startCol) {
		c.before = ix.slice(t, startLn, sc.start, startCol)
	} else {
		c.before = ix.slice(t, startLn, 0, startCol)
	}
	// The text after.
	end := t.scopeAt(endLn, endCol)
	c.after = ix.slice(t, endLn, endCol, end.end)
	// The line above, when the language goes on from it.
	if prev := startLn - 1; prev > 0 && sc.start == 0 {
		endOfPrev := t.scopeAt(prev, len(t.content(prev)))
		if endOfPrev.language == sc.language {
			c.previous = ix.slice(t, prev, endOfPrev.start, endOfPrev.end)
		}
	}
	return c
}

// rangeLines converts a range of the document to the one-based lines and byte columns of this
// package.
func rangeLines(r buffer.Range) (startLn, startCol, endLn, endCol int) {
	return r.Start.Line + 1, r.Start.Col, r.End.Line + 1, r.End.Col
}
