package indent

import (
	"strings"

	"github.com/rm4n0s/emetos/internal/core/buffer"
	"github.com/rm4n0s/emetos/internal/core/language"
)

// Ported from VS Code: src/vs/editor/common/languages/enterAction.ts and
// src/vs/editor/common/languages/autoIndent.ts (MIT)

// EnterResult is what Enter does at a position, from the on-enter rules of the language there
// (VS Code's CompleteEnterAction).
type EnterResult struct {
	// Indent is the change of the indentation.
	Indent language.IndentAction
	// AppendText is typed after the indentation of the new line; it starts with a tab when the
	// indentation grows.
	AppendText string
	// RemoveText is the number of characters that Indentation had taken off.
	RemoveText int
	// Indentation is the white space of the line at the cursor before the cursor, less
	// RemoveText characters.
	Indentation string
}

// EnterAction finds the on-enter rule that applies to Enter pressed over r (the selection is
// replaced by the line break), or false. The language is the one at the start of r.
//
// Ported from VS Code: enterAction.ts (getEnterAction)
func (ix *Indenter) EnterAction(auto AutoIndent, m Model, r buffer.Range) (EnterResult, bool) {
	t := text{m: m}
	startLn, startCol, endLn, endCol := rangeLines(r)
	lang := ix.langs.Language(t.languageAt(startLn, startCol))
	ctx := ix.contextAround(t, startLn, startCol, endLn, endCol)
	enter, ok := lang.OnEnter.OnEnter(auto, ctx.previous.text, ctx.before.text, ctx.after.text)
	if !ok {
		return EnterResult{}, false
	}
	appendText := enter.AppendText
	if appendText == "" {
		if enter.Indent == language.IndentIndent || enter.Indent == language.IndentOutdent {
			appendText = "\t"
		}
	} else if enter.Indent == language.IndentIndent {
		appendText = "\t" + appendText
	}
	indentation := leading(t.content(startLn))
	if len(indentation) > startCol {
		indentation = indentation[:startCol]
	}
	if enter.RemoveText > 0 {
		indentation = indentation[:max(len(indentation)-enter.RemoveText, 0)]
	}
	return EnterResult{Indent: enter.Indent, AppendText: appendText, RemoveText: enter.RemoveText, Indentation: indentation}, true
}

// IndentForEnter gives the indentation of the line that Enter pressed over r leaves before the
// cursor and of the one it starts, from the indentation rules of the language, or false when the
// level is not "full" or the language has none.
//
// Ported from VS Code: autoIndent.ts (getIndentForEnter)
func (ix *Indenter) IndentForEnter(auto AutoIndent, m Model, r buffer.Range, conv Converter) (beforeEnter, afterEnter string, ok bool) {
	if auto < AutoIndentFull {
		return "", "", false
	}
	t := text{m: m}
	startLn, startCol, endLn, endCol := rangeLines(r)
	rules := ix.langs.Language(t.languageAt(startLn, startCol)).Rules
	if rules == nil {
		return "", "", false
	}
	ctx := ix.contextAround(t, startLn, startCol, endLn, endCol)
	beforeIndent := leading(ctx.before.text)
	virtual := t.withLine(startLn, ctx.before.text, ctx.before.toks, true)
	differs := t.languageDiffersFromLineStart(startLn, startCol)
	currentIndent := leading(t.content(startLn))
	action, found := ix.inheritIndentForLine(auto, virtual, startLn+1, true)
	if !found {
		before := beforeIndent
		if differs {
			before = currentIndent
		}
		return before, before, true
	}
	afterIndent := action.indentation
	before := beforeIndent
	if differs {
		afterIndent, before = currentIndent, currentIndent
	}
	if action.indent {
		afterIndent = conv.Shift(afterIndent)
	}
	if rules.ShouldDecrease(ctx.after.text) {
		afterIndent = conv.Unshift(afterIndent)
	}
	return before, afterIndent, true
}

// IndentActionForType gives the indentation that the line of r should have once the character ch
// is typed over r, when typing it makes the line match decreaseIndentPattern (a closing brace, a
// keyword such as `end`) or an auto-closed character starts a line after a line that indents only
// the next one; false when the indentation should stay. A line whose indentation was already
// changed on purpose stays as it is. It is the type-time re-indentation of VS Code.
//
// Ported from VS Code: autoIndent.ts (getIndentActionForType)
func (ix *Indenter) IndentActionForType(auto AutoIndent, m Model, r buffer.Range, ch string, conv Converter) (string, bool) {
	if auto < AutoIndentFull {
		return "", false
	}
	t := text{m: m}
	startLn, startCol, endLn, endCol := rangeLines(r)
	if t.languageDiffersFromLineStart(startLn, startCol) {
		// The line has several languages, and the rules would not work.
		return "", false
	}
	rules := ix.langs.Language(t.languageAt(startLn, startCol)).Rules
	if rules == nil {
		return "", false
	}
	ctx := ix.contextAround(t, startLn, startCol, endLn, endCol)
	around := ctx.before.text + ctx.after.text
	withCh := ctx.before.text + ch + ctx.after.text

	// If the line already matches decreaseIndentPattern, its indentation was already adjusted,
	// maybe on purpose, and it is left alone. Otherwise the character makes it match: adjust.
	if !rules.ShouldDecrease(around) && rules.ShouldDecrease(withCh) {
		inherit, found := ix.inheritIndentForLine(auto, t, startLn, false)
		if !found {
			return "", false
		}
		indentation := inherit.indentation
		if !inherit.indent {
			indentation = conv.Unshift(indentation)
		}
		return indentation, true
	}

	if prev := startLn - 1; r.IsEmpty() && prev > 0 {
		if rules.ShouldIndentNextLine(t.content(prev)) && rules.ShouldIncrease(withCh) {
			if inherit, found := ix.inheritIndentForLine(auto, t, startLn, false); found {
				actual := leading(t.content(startLn))
				inferred := conv.Shift(inherit.indentation)
				// A line whose indentation is not the inferred one was changed on purpose.
				lang := ix.langs.Language(t.m.Language())
				if inferred == actual && lang.ClosesOnTyping(ch) && blank(around) {
					return inherit.indentation, true
				}
			}
		}
	}
	return "", false
}

// IndentMetadata is the bits of the indentation rules (IncreaseMask and the others) that the
// line matches, or false when the language has no rules or the line does not exist.
//
// Ported from VS Code: autoIndent.ts (getIndentMetadata)
func (ix *Indenter) IndentMetadata(m Model, line int) (int, bool) {
	rules := ix.langs.Language(m.Language()).Rules
	if rules == nil || line < 0 || line >= m.LineCount() {
		return 0, false
	}
	return rules.Metadata(m.LineContent(line)), true
}

// GoodIndentForLine is the indentation that a line should have from the lines above it and the
// rules of the language, or false when the rules say nothing. line is zero-based.
//
// Ported from VS Code: autoIndent.ts (getGoodIndentForLine)
func (ix *Indenter) GoodIndentForLine(auto AutoIndent, m Model, line int, conv Converter) (string, bool) {
	return ix.goodIndentForLine(auto, text{m: m}, line+1, conv)
}

// IndentForBlankLine is the indentation that Tab puts on the empty line (zero-based) the cursor is
// on: the one the lines above give by the indentation rules, or else the one that Enter pressed at
// the end of the last line with text would give; false when neither says anything.
//
// Ported from VS Code: src/vs/editor/common/cursor/cursorTypeEditOperations.ts
// (TabOperation._goodIndentForLine)
func (ix *Indenter) IndentForBlankLine(auto AutoIndent, m Model, line int, conv Converter) (string, bool) {
	t := text{m: m}
	ln := line + 1
	var indentation string
	action := language.IndentNone
	if inherit, ok := ix.inheritIndentForLine(auto, t, ln, false); ok {
		if inherit.indent {
			action = language.IndentIndent
		}
		indentation = inherit.indentation
	} else if ln > 1 {
		last := ln - 1
		for ; last >= 1; last-- {
			if strings.TrimRight(t.content(last), " \t") != "" {
				break
			}
		}
		if last < 1 {
			return "", false
		}
		end := len(t.content(last))
		r := buffer.Range{Start: buffer.Position{Line: last - 1, Col: end}, End: buffer.Position{Line: last - 1, Col: end}}
		if enter, found := ix.EnterAction(auto, m, r); found {
			indentation = enter.Indentation + enter.AppendText
		}
	}
	if action == language.IndentIndent {
		indentation = conv.Shift(indentation)
		indentation = conv.Normalize(indentation)
	}
	if indentation == "" {
		return "", false
	}
	return indentation, true
}
