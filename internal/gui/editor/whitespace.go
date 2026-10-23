package editor

import (
	"unicode/utf8"

	"github.com/rm4n0s/emetos/internal/core/buffer"
)

// Ported from VS Code: src/vs/editor/browser/viewParts/whitespace/whitespace.ts and
// src/vs/editor/common/viewLayout/viewLineRenderer.ts (isControlCharacter)

// wsMark is a space or a tab that is drawn as a dot or an arrow: Col is its byte column in the
// text of the row.
type wsMark struct {
	Col int
	Tab bool
}

// colSpan is a range of byte columns of the text of a row, from Start up to End.
type colSpan struct{ Start, End int }

// whitespaceMarks are the spaces and tabs of the text of a row that editor.renderWhitespace
// draws. mode is "none", "boundary", "selection", "trailing" or "all"; selections are the parts of
// the text selected, in order, for "selection"; continues says that the row is followed by the
// next row of the same line; stopAfter is editor.stopRenderingLineAfter (negative: no limit).
//
// Ported from VS Code: whitespace.ts (WhitespaceOverlay._applyRenderWhitespace)
func whitespaceMarks(text, mode string, selections []colSpan, continues bool, stopAfter int) []wsMark {
	if mode != "boundary" && mode != "selection" && mode != "trailing" && mode != "all" {
		return nil
	}
	if mode == "selection" && len(selections) == 0 {
		return nil
	}
	if mode == "trailing" && continues {
		return nil
	}
	n := len(text)
	if stopAfter >= 0 {
		n = min(n, stopAfter)
	}
	onlyBoundary, onlyTrailing := mode == "boundary", mode == "trailing"
	first, last := firstAndLastNonBlank(text)
	emptyOrBlank := first < 0
	if emptyOrBlank {
		first, last = n, n
	}
	var marks []wsMark
	si := 0
	for i := range n {
		c := text[i]
		for si < len(selections) && selections[si].End <= i {
			si++
		}
		if c != '\t' && c != ' ' {
			continue
		}
		if onlyTrailing && !emptyOrBlank && i <= last {
			continue
		}
		if onlyBoundary && i >= first && i <= last && c == ' ' {
			prevSpace := i > 0 && text[i-1] == ' '
			nextSpace := i+1 < n && text[i+1] == ' '
			if !prevSpace && !nextSpace {
				continue
			}
		}
		if onlyBoundary && continues && i == n-1 {
			if c == ' ' && (i == 0 || (text[i-1] != ' ' && text[i-1] != '\t')) {
				continue
			}
		}
		if mode == "selection" && (si >= len(selections) || selections[si].Start > i || i >= selections[si].End) {
			continue
		}
		marks = append(marks, wsMark{Col: i, Tab: c == '\t'})
	}
	return marks
}

// firstAndLastNonBlank are the byte indexes of the first and the last character that is not a
// space or a tab, or -1 and -1 when the text has none.
func firstAndLastNonBlank(text string) (first, last int) {
	first, last = -1, -1
	for i := range len(text) {
		if c := text[i]; c != ' ' && c != '\t' {
			if first < 0 {
				first = i
			}
			last = i
		}
	}
	return first, last
}

// selectionSpans are the parts of the text shown by a row (the bytes start to end of its line)
// that the selections cover, in columns of the row's text, in order. A selection that leaves the
// row by its end or by its start covers up to the end or from the start of the row.
//
// Ported from VS Code: whitespace.ts (WhitespaceOverlay.prepareRender)
func selectionSpans(sels []buffer.Selection, line, start, end int) []colSpan {
	var out []colSpan
	for _, s := range sels {
		a, b := s.Start(), s.End()
		if b.Line < line || a.Line > line {
			continue
		}
		from, to := start, end
		if a.Line == line {
			from = max(a.Col, start)
		}
		if b.Line == line {
			to = min(b.Col, end)
		}
		if from < to {
			out = append(out, colSpan{from - start, to - start})
		}
	}
	return mergeSpans(out)
}

// mergeSpans sorts spans by their start and joins the ones that overlap or touch.
func mergeSpans(spans []colSpan) []colSpan {
	if len(spans) < 2 {
		return spans
	}
	for i := 1; i < len(spans); i++ {
		for j := i; j > 0 && spans[j].Start < spans[j-1].Start; j-- {
			spans[j], spans[j-1] = spans[j-1], spans[j]
		}
	}
	out := spans[:1]
	for _, s := range spans[1:] {
		if last := &out[len(out)-1]; s.Start <= last.End {
			last.End = max(last.End, s.End)
		} else {
			out = append(out, s)
		}
	}
	return out
}

// ctrlMark is a control character drawn as a box: Col and Len are its byte column and length in
// the text of the row, and Picture the character of the Control Pictures block that stands for
// it, or 0 when it has none.
type ctrlMark struct {
	Col, Len int
	Picture  rune
}

// isControlCharacter tells whether editor.renderControlCharacters draws a character: the C0
// controls except the tab, DEL and the Unicode directional formatting characters.
//
// Ported from VS Code: viewLineRenderer.ts (isControlCharacter)
func isControlCharacter(r rune) bool {
	switch {
	case r < 0x20:
		return r != '\t'
	case r == 0x7F:
		return true
	case r >= 0x202A && r <= 0x202E, r >= 0x2066 && r <= 0x2069, r >= 0x200E && r <= 0x200F, r == 0x061C:
		return true
	}
	return false
}

// controlMarks are the control characters of a text.
func controlMarks(text string) []ctrlMark {
	var marks []ctrlMark
	for i := 0; i < len(text); {
		c := text[i]
		if c >= utf8.RuneSelf {
			r, size := utf8.DecodeRuneInString(text[i:])
			if isControlCharacter(r) {
				marks = append(marks, ctrlMark{Col: i, Len: size})
			}
			i += size
			continue
		}
		if isControlCharacter(rune(c)) {
			picture := rune(0x2400 + int(c))
			if c == 0x7F {
				picture = 0x2421
			}
			marks = append(marks, ctrlMark{Col: i, Len: 1, Picture: picture})
		}
		i++
	}
	return marks
}
