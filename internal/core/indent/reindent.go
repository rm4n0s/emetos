package indent

import (
	"strings"

	"github.com/rm4n0s/emetos/internal/core/buffer"
	"github.com/rm4n0s/emetos/internal/core/textmate"
)

// Ported from VS Code: src/vs/editor/contrib/indentation/common/indentation.ts and
// src/vs/editor/contrib/indentation/browser/indentation.ts (AutoIndentOnPaste) (MIT)

// Edit replaces the bytes [Start, End) of a line (zero-based) by Text: a change of the
// indentation of the line.
type Edit struct {
	Line       int
	Start, End int
	Text       string
}

// SpaceCount is the width of the white space str in columns, a tab counting as tabSize.
func SpaceCount(str string, tabSize int) int {
	n := 0
	for i := 0; i < len(str); i++ {
		if str[i] == '\t' {
			n += tabSize
		} else {
			n++
		}
	}
	return n
}

// GenerateIndent is the white space of the given width: with tabs for as many whole tab sizes
// as it holds, unless insertSpaces, and spaces for the rest.
func GenerateIndent(width, tabSize int, insertSpaces bool) string {
	width = max(width, 0)
	tabs := 0
	if !insertSpaces {
		tabs = width / tabSize
		width %= tabSize
	}
	return strings.Repeat("\t", tabs) + strings.Repeat(" ", width)
}

// ReindentEdits gives the edits that indent the lines first to last (zero-based, both included)
// as the indentation rules say, taking the first line as it is, or none when the language has no
// rules. It is the command Reindent Lines.
//
// Ported from VS Code: contrib/indentation/common/indentation.ts (getReindentEditOperations)
func (ix *Indenter) ReindentEdits(m Model, conv Converter, first, last int) []Edit {
	if m.LineCount() == 1 && m.LineContent(0) == "" {
		return nil
	}
	rules := ix.langs.Language(m.Language()).Rules
	if rules == nil {
		return nil
	}
	t := text{m: m}
	p := ix.processedRules(t, rules)
	start, end := first+1, min(last+1, m.LineCount())
	// Skip the lines of unIndentedLinePattern.
	for start <= end {
		if !p.shouldIgnore(start, nil) {
			break
		}
		start++
	}
	if start > end-1 {
		return nil
	}
	var edits []Edit
	// The indentation that is passed to the lines below, starting from that of the first line.
	globalIndent := leading(t.content(start))
	// The indentation the next line should have; it differs from globalIndent after a line that
	// matches indentNextLinePattern.
	ideal := globalIndent
	if p.shouldIncrease(start, nil) {
		ideal = conv.Shift(ideal)
		globalIndent = conv.Shift(globalIndent)
	} else if p.shouldIndentNextLine(start, nil) {
		ideal = conv.Shift(ideal)
	}
	start++
	for ln := start; ln <= end; ln++ {
		if ix.startsWithString(t, ln) {
			continue
		}
		old := leading(t.content(ln))
		current := ideal
		if p.shouldDecrease(ln, &current) {
			ideal = conv.Unshift(ideal)
			globalIndent = conv.Unshift(globalIndent)
		}
		if old != ideal {
			edits = append(edits, Edit{Line: ln - 1, End: len(old), Text: conv.Normalize(ideal)})
		}
		switch {
		case p.shouldIgnore(ln, nil):
			// A line of unIndentedLinePattern takes the indentation from above but changes neither
			// globalIndent nor the ideal indentation.
		case p.shouldIncrease(ln, &current):
			globalIndent = conv.Shift(globalIndent)
			ideal = globalIndent
		case p.shouldIndentNextLine(ln, &current):
			ideal = conv.Shift(ideal)
		default:
			ideal = globalIndent
		}
	}
	return edits
}

// startsWithString reports whether line ln begins in a string.
func (ix *Indenter) startsWithString(t text, ln int) bool {
	if !t.m.CheapToTokenize(ln - 1) {
		return false
	}
	toks := t.m.LineTokens(ln - 1)
	return len(toks) > 0 && toks[0].Type == textmate.TokenString
}

// PasteEdits gives the edits that fit the text just pasted over r into the indentation around it
// (editor.autoIndentOnPaste): the first line, when only white space comes before the pasted text,
// gets the indentation the rules want, and the other lines move by as much as the second line
// does. It does nothing when the pasted text is only white space, when it starts or ends in a
// string (unless withinString), when the first line's tokens are not at hand, or at a level below
// "full".
//
// Ported from VS Code: contrib/indentation/browser/indentation.ts (AutoIndentOnPaste.trigger)
func (ix *Indenter) PasteEdits(auto AutoIndent, m Model, conv Converter, tabSize int, insertSpaces, withinString bool, r buffer.Range) []Edit {
	if auto < AutoIndentFull {
		return nil
	}
	t := text{m: m}
	startLn, startCol, endLn, endCol := rangeLines(r)
	if onlyWhitespace(t, startLn, startCol, endLn, endCol) {
		return nil
	}
	if !withinString && (t.tokenTypeAt(startLn, startCol) == textmate.TokenString || t.tokenTypeAt(endLn, endCol) == textmate.TokenString) {
		return nil
	}
	if !m.CheapToTokenize(startLn - 1) {
		return nil
	}
	var edits []Edit
	firstLineText := t.content(startLn)
	if blank(firstLineText[:min(startCol, len(firstLineText))]) {
		if good, ok := ix.goodIndentForLine(auto, t, startLn, conv); ok {
			old := leading(firstLineText)
			newCount, oldCount := SpaceCount(good, tabSize), SpaceCount(old, tabSize)
			if newCount != oldCount {
				indent := GenerateIndent(newCount, tabSize, insertSpaces)
				edits = append(edits, Edit{Line: startLn - 1, End: len(old), Text: indent})
				firstLineText = indent + firstLineText[len(old):]
			} else if meta, _ := ix.IndentMetadata(m, startLn-1); meta == 0 || meta == UnindentMask {
				// Text pasted into a line of white space only: the indentation of the first line
				// is right and the line matches no rule.
				return nil
			}
		}
	}
	// Ignore empty lines.
	s := startLn
	for s < endLn {
		if blank(t.content(s + 1)) {
			s++
			continue
		}
		break
	}
	if s != endLn {
		virtual := t.withLine(startLn, firstLineText, nil, false)
		if good, ok := ix.goodIndentForLine(auto, virtual, s+1, conv); ok {
			newCount := SpaceCount(good, tabSize)
			oldCount := SpaceCount(leading(t.content(s+1)), tabSize)
			if newCount != oldCount {
				offset := newCount - oldCount
				for i := s + 1; i <= endLn; i++ {
					content := t.content(i)
					original := leading(content)
					indent := GenerateIndent(SpaceCount(original, tabSize)+offset, tabSize, insertSpaces)
					if indent != original {
						edits = append(edits, Edit{Line: i - 1, End: len(original), Text: indent})
					}
				}
			}
		}
	}
	return edits
}

// onlyWhitespace reports whether the text of the range has nothing but white space.
func onlyWhitespace(t text, startLn, startCol, endLn, endCol int) bool {
	part := func(s string, from, to int) string {
		return s[min(from, len(s)):min(to, len(s))]
	}
	if startLn == endLn {
		return blank(part(t.content(startLn), startCol, endCol))
	}
	for i := startLn; i <= endLn; i++ {
		content := t.content(i)
		switch i {
		case startLn:
			if !blank(part(content, startCol, len(content))) {
				return false
			}
		case endLn:
			if !blank(part(content, 0, endCol)) {
				return false
			}
		default:
			if !blank(content) {
				return false
			}
		}
	}
	return true
}
