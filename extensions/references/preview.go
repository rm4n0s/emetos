package references

import (
	"strings"
	"unicode"
	"unicode/utf8"

	"github.com/rm4n0s/emetos/extension"
)

// The text of a line around a location, for the labels of the tree, the copy text and the history (VS Code's utils.ts).
//
// Ported from VS Code: extensions/references-view/src/utils.ts (getPreviewChunks, WordAnchor, isValidRequestPosition)

// wordSeparators are the characters that end a word in the default word pattern of VS Code's languages: a word is a run of
// the other characters that are not white space.
const wordSeparators = "`~!@#%^&*()-=+[{]}\\|;:'\",.<>/?"

// isWordRune tells whether a character is part of a word.
func isWordRune(r rune) bool {
	return !unicode.IsSpace(r) && !strings.ContainsRune(wordSeparators, r)
}

// wordAt is the word of the line that the byte column is in or touches, as the bytes [start, end) of the line (VS Code's
// getWordRangeAtPosition: a position at either end of a word is in it); false when there is none.
func wordAt(line string, col int) (start, end int, ok bool) {
	return spanAt(line, col, isWordRune)
}

// tokenAt is wordAt with a token being any run of characters that are not white space (the pattern /[^\s]+/ that VS Code
// falls back to).
func tokenAt(line string, col int) (start, end int, ok bool) {
	return spanAt(line, col, func(r rune) bool { return !unicode.IsSpace(r) })
}

// spanAt finds the first maximal run of characters that keep says yes to, from the left, with start <= col <= end.
func spanAt(line string, col int, keep func(rune) bool) (start, end int, ok bool) {
	col = min(max(col, 0), len(line))
	i := 0
	for i < len(line) {
		r, n := utf8.DecodeRuneInString(line[i:])
		if !keep(r) {
			i += n
			continue
		}
		j := i
		for j < len(line) {
			r2, n2 := utf8.DecodeRuneInString(line[j:])
			if !keep(r2) {
				break
			}
			j += n2
		}
		if i <= col && col <= j {
			return i, j, true
		}
		if i > col {
			return 0, 0, false
		}
		i = j
	}
	return 0, 0, false
}

// wordOrToken is the word at the position, else the run of characters that are not white space (isValidRequestPosition and
// the history use it); false when the position is on white space only.
func wordOrToken(line string, col int) (start, end int, ok bool) {
	if s, e, ok := wordAt(line, col); ok {
		return s, e, true
	}
	return tokenAt(line, col)
}

// stepBack is the byte column n characters before col.
func stepBack(line string, col, n int) int {
	col = min(max(col, 0), len(line))
	for ; n > 0 && col > 0; n-- {
		_, size := utf8.DecodeLastRuneInString(line[:col])
		col -= size
	}
	return col
}

// stepForward is the byte column n characters after col, at most the end of the line.
func stepForward(line string, col, n int) int {
	col = min(max(col, 0), len(line))
	for ; n > 0 && col < len(line); n-- {
		_, size := utf8.DecodeRuneInString(line[col:])
		col += size
	}
	return col
}

// afterLen is how many characters after the match the preview keeps (VS Code adds 331 UTF-16 units).
const afterLen = 331

// previewChunks cuts a line around a match: the text before it (beforeLen characters back, and on to the start of the word
// that is in), the match, and the text after it. With trim the white space at the start of the first and at the end of the
// last is taken away. The columns are bytes of the line and are clamped to it.
func previewChunks(line string, startCol, endCol, beforeLen int, trim bool) (before, inside, after string) {
	startCol = min(max(startCol, 0), len(line))
	endCol = min(max(endCol, startCol), len(line))
	previewStart := stepBack(line, startCol, beforeLen)
	from := previewStart
	if s, _, ok := wordAt(line, previewStart); ok && s <= startCol {
		from = s
	}
	before = line[from:startCol]
	inside = line[startCol:endCol]
	after = line[endCol:stepForward(line, endCol, afterLen)]
	if trim {
		before = strings.TrimLeftFunc(before, unicode.IsSpace)
		after = strings.TrimRightFunc(after, unicode.IsSpace)
	}
	return before, inside, after
}

// utf16Col is the column of the byte column in UTF-16 units, which is what VS Code counts and what Documents().Show takes.
func utf16Col(line string, byteCol int) int {
	byteCol = min(max(byteCol, 0), len(line))
	n := 0
	for _, r := range line[:byteCol] {
		if r >= 0x10000 {
			n += 2
		} else {
			n++
		}
	}
	return n
}

// lineOf is a line of a text (empty past its end).
func lineOf(lines []string, n int) string {
	if n < 0 || n >= len(lines) {
		return ""
	}
	return lines[n]
}

// splitLines splits a text into lines the way a buffer does: at "\n", with a "\r" before it dropped.
func splitLines(text string) []string {
	lines := strings.Split(text, "\n")
	for i, l := range lines {
		lines[i] = strings.TrimSuffix(l, "\r")
	}
	return lines
}

// rangeEnd is the byte column where a range ends on the line it starts on: its end, or the end of the line for a range of
// several lines.
func rangeEnd(line int, r extension.Range, lineLen int) int {
	if r.End.Line != line {
		return lineLen
	}
	return r.End.Col
}
