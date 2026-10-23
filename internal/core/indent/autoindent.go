package indent

import (
	"strings"

	"github.com/rm4n0s/emetos/internal/core/language"
)

// AutoIndent is the level of editor.autoIndent: how much the editor does with the indentation
// of a line while the text is typed (SPEC §8.6.2). The values, and their order, are VS Code's.
type AutoIndent int

// The levels of editor.autoIndent.
const (
	// AutoIndentNone does nothing: a new line starts at the first column.
	AutoIndentNone AutoIndent = iota
	// AutoIndentKeep gives a new line the indentation of the line it was started on.
	AutoIndentKeep
	// AutoIndentBrackets also indents after an opening bracket and between a pair of brackets.
	AutoIndentBrackets
	// AutoIndentAdvanced also applies the on-enter rules of the language.
	AutoIndentAdvanced
	// AutoIndentFull also applies the indentation rules of the language, when a line is
	// started, typed and pasted.
	AutoIndentFull
)

// ParseAutoIndent reads a value of editor.autoIndent.
func ParseAutoIndent(s string) (AutoIndent, bool) {
	switch s {
	case "none":
		return AutoIndentNone, true
	case "keep":
		return AutoIndentKeep, true
	case "brackets":
		return AutoIndentBrackets, true
	case "advanced":
		return AutoIndentAdvanced, true
	case "full":
		return AutoIndentFull, true
	}
	return AutoIndentFull, false
}

// String is the value of editor.autoIndent.
func (a AutoIndent) String() string {
	switch a {
	case AutoIndentNone:
		return "none"
	case AutoIndentKeep:
		return "keep"
	case AutoIndentBrackets:
		return "brackets"
	case AutoIndentAdvanced:
		return "advanced"
	}
	return "full"
}

// Converter shifts an indentation by one level, as the editor does with the tab size and the
// use of spaces of the document in force.
type Converter interface {
	// Shift gives the indentation one level deeper.
	Shift(indentation string) string
	// Unshift gives the indentation one level shallower.
	Unshift(indentation string) string
	// Normalize writes the leading white space of the text with tabs or with spaces, as the
	// document does.
	Normalize(text string) string
}

// inherited is the indentation that a line inherits from the lines above it (VS Code's result of
// getInheritIndentForLine).
type inherited struct {
	indentation string
	// indent says that the line is one level deeper than the indentation.
	indent bool
	// line is the one-based number of the line the indentation was taken from, or 0.
	line int
}

// leading is the spaces and tabs a text starts with.
func leading(s string) string {
	for i := 0; i < len(s); i++ {
		if s[i] != ' ' && s[i] != '\t' {
			return s[:i]
		}
	}
	return s
}

// blank reports whether a text has nothing but white space (JavaScript's /^\s*$/).
func blank(s string) bool { return strings.TrimSpace(s) == "" }

// precedingValidLine is the nearest line above ln that is not ignored by the indentation rules
// and has something on it: -1 when the language changes on the way and nothing was skipped
// before, or when no line is left.
func (p processed) precedingValidLine(ln int) int {
	lang := p.t.languageAt(ln, 0)
	if ln > 1 {
		result := -1
		for last := ln - 1; last >= 1; last-- {
			if p.t.languageAt(last, 0) != lang {
				return result
			}
			if p.shouldIgnore(last, nil) || blank(p.t.content(last)) {
				result = last
				continue
			}
			return last
		}
	}
	return -1
}

// inheritIndentForLine is the indentation that line ln (one-based) gets from the lines above it:
//
//  1. Find the nearest preceding line that is not ignored by unIndentedLinePattern.
//  2. If it matches indentNextLinePattern or increaseIndentPattern, ln is one level deeper than it.
//  3. If it matches no rule, look at the line above it: when that one matches
//     indentNextLinePattern the indentation of the line is only temporary, so go up until a line
//     whose indentation is not temporary (or, with honorIntentional, keep the line's own).
//
// It does not look at whether ln itself should be indented less.
//
// Ported from VS Code: src/vs/editor/common/languages/autoIndent.ts (getInheritIndentForLine)
func (ix *Indenter) inheritIndentForLine(auto AutoIndent, t text, ln int, honorIntentional bool) (inherited, bool) {
	if auto < AutoIndentFull {
		return inherited{}, false
	}
	rules := ix.langs.Language(t.m.Language()).Rules
	if rules == nil {
		return inherited{}, false
	}
	p := ix.processedRules(t, rules)
	if ln <= 1 {
		return inherited{}, true
	}
	// No indentation when the line is the first with text.
	for prior := ln - 1; prior > 0; prior-- {
		if t.content(prior) != "" {
			break
		}
		if prior == 1 {
			return inherited{}, true
		}
	}
	preceding := p.precedingValidLine(ln)
	if preceding < 0 {
		return inherited{}, false
	} else if preceding < 1 {
		return inherited{}, true
	}
	if p.shouldIncrease(preceding, nil) || p.shouldIndentNextLine(preceding, nil) {
		return inherited{leading(t.content(preceding)), true, preceding}, true
	} else if p.shouldDecrease(preceding, nil) {
		return inherited{leading(t.content(preceding)), false, preceding}, true
	}
	// The preceding line neither increases the indentation of what follows nor of the next line
	// only, so ln is not affected by it, and the indentation comes from the lines above.
	if preceding == 1 {
		return inherited{leading(t.content(preceding)), false, preceding}, true
	}
	previous := preceding - 1
	meta := rules.Metadata(t.content(previous))
	if meta&(IncreaseMask|DecreaseMask) == 0 && meta&IndentNextLineMask != 0 {
		stop := 0
		for i := previous - 1; i > 0; i-- {
			if p.shouldIndentNextLine(i, nil) {
				continue
			}
			stop = i
			break
		}
		return inherited{leading(t.content(stop + 1)), false, stop + 1}, true
	}
	if honorIntentional {
		return inherited{leading(t.content(preceding)), false, preceding}, true
	}
	// Search from the preceding line for one whose indentation is not temporary.
	for i := preceding; i > 0; i-- {
		if p.shouldIncrease(i, nil) {
			return inherited{leading(t.content(i)), true, i}, true
		} else if p.shouldIndentNextLine(i, nil) {
			stop := 0
			for j := i - 1; j > 0; j-- {
				// VS Code asks about i here, not j, so this loop never stops early.
				if p.shouldIndentNextLine(i, nil) {
					continue
				}
				stop = j
				break
			}
			return inherited{leading(t.content(stop + 1)), false, stop + 1}, true
		} else if p.shouldDecrease(i, nil) {
			return inherited{leading(t.content(i)), false, i}, true
		}
	}
	return inherited{leading(t.content(1)), false, 1}, true
}

// goodIndentForLine is the indentation that line ln (one-based) of the text should have, from the
// lines above it, the on-enter rules of the line it inherits from, and its own content; false when
// the rules say nothing.
//
// Ported from VS Code: autoIndent.ts (getGoodIndentForLine)
func (ix *Indenter) goodIndentForLine(auto AutoIndent, t text, ln int, conv Converter) (string, bool) {
	if auto < AutoIndentFull {
		return "", false
	}
	lang := ix.langs.Language(t.m.Language())
	rules := lang.Rules
	if rules == nil {
		return "", false
	}
	p := ix.processedRules(t, rules)
	inherit, ok := ix.inheritIndentForLine(auto, t, ln, true)
	if !ok {
		return "", false
	}
	if inherit.line != 0 {
		// The enter rules apply while only white space lies between the inherited line and ln.
		apply := true
		for between := inherit.line; between < ln-1; between++ {
			if !blank(t.content(between)) {
				apply = false
				break
			}
		}
		if apply {
			if enter, found := lang.OnEnter.OnEnter(auto, "", t.content(inherit.line), ""); found {
				indentation := leading(t.content(inherit.line))
				if enter.RemoveText > 0 {
					indentation = indentation[:max(len(indentation)-enter.RemoveText, 0)]
				}
				switch enter.Indent {
				case language.IndentIndent, language.IndentOutdent:
					indentation = conv.Shift(indentation)
				case language.IndentOutdentOne:
					indentation = conv.Unshift(indentation)
				}
				if p.shouldDecrease(ln, nil) {
					indentation = conv.Unshift(indentation)
				}
				indentation += enter.AppendText
				return leading(indentation), true
			}
		}
	}
	if p.shouldDecrease(ln, nil) {
		if inherit.indent {
			return inherit.indentation, true
		}
		return conv.Unshift(inherit.indentation), true
	}
	if inherit.indent {
		return conv.Shift(inherit.indentation), true
	}
	return inherit.indentation, true
}
