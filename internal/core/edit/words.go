package edit

import "unicode/utf8"

// DefaultWordSeparators is VS Code's default for editor.wordSeparators.
const DefaultWordSeparators = "`~!@#$%^&*()-=+[{]}\\|;:'\",.<>/?"

// CharClass is what a character is to word navigation (VS Code's
// WordCharacterClass).
type CharClass int

const (
	// ClassRegular is any character that is not a separator or white space.
	ClassRegular CharClass = iota
	// ClassWhitespace is a space or a tab.
	ClassWhitespace
	// ClassSeparator is a character of editor.wordSeparators.
	ClassSeparator
)

// WordClassifier tells the class of a character for a set of word separators.
type WordClassifier struct {
	ascii [128]CharClass
	other map[rune]CharClass
}

// NewWordClassifier makes a classifier from the characters of editor.wordSeparators.
func NewWordClassifier(separators string) *WordClassifier {
	c := new(WordClassifier)
	for _, r := range separators {
		if r < utf8.RuneSelf {
			c.ascii[r] = ClassSeparator
			continue
		}
		if c.other == nil {
			c.other = make(map[rune]CharClass)
		}
		c.other[r] = ClassSeparator
	}
	c.ascii[' '] = ClassWhitespace
	c.ascii['\t'] = ClassWhitespace
	return c
}

// Class is the class of r.
func (c *WordClassifier) Class(r rune) CharClass {
	if r < utf8.RuneSelf {
		return c.ascii[r]
	}
	return c.other[r]
}

// WordKind is what a word is made of: runs of regular characters, or runs of
// separators, are words for navigation.
type WordKind int

const (
	// RegularWord is a run of regular characters.
	RegularWord WordKind = iota + 1
	// SeparatorWord is a run of separators.
	SeparatorWord
)

// Word is a word found on a line: the byte columns where it starts and ends, what
// it is made of, and the class of the character that ended the search for it
// (the one before it when looking backward, the one after it when looking
// forward; white space when the line ended).
type Word struct {
	Start, End int
	Kind       WordKind
	NextClass  CharClass
}

// PrevWord finds the word that ends at or before col, looking backward as VS
// Code's word navigation does (_doFindPreviousWordOnLine). The second result is
// false when the line has no word before col. editor.wordSegmenterLocales is not
// supported, so words are never split by a language-aware segmenter.
func (c *WordClassifier) PrevWord(line string, col int) (Word, bool) {
	col = min(max(col, 0), len(line))
	kind := WordKind(0)
	for i := col; i > 0; {
		r, w := utf8.DecodeLastRuneInString(line[:i])
		i -= w
		class := c.Class(r)
		switch class {
		case ClassRegular:
			if kind == SeparatorWord {
				return c.wordForward(line, kind, class, i+w), true
			}
			kind = RegularWord
		case ClassSeparator:
			if kind == RegularWord {
				return c.wordForward(line, kind, class, i+w), true
			}
			kind = SeparatorWord
		case ClassWhitespace:
			if kind != 0 {
				return c.wordForward(line, kind, class, i+w), true
			}
		}
	}
	if kind != 0 {
		return c.wordForward(line, kind, ClassWhitespace, 0), true
	}
	return Word{}, false
}

// wordForward is the word of the given kind that starts at start.
func (c *WordClassifier) wordForward(line string, kind WordKind, next CharClass, start int) Word {
	return Word{Start: start, End: c.endOfWord(line, kind, start), Kind: kind, NextClass: next}
}

// endOfWord is where a word of the given kind that has reached start ends.
func (c *WordClassifier) endOfWord(line string, kind WordKind, start int) int {
	for i := start; i < len(line); {
		r, w := utf8.DecodeRuneInString(line[i:])
		class := c.Class(r)
		if class == ClassWhitespace ||
			(kind == RegularWord && class == ClassSeparator) ||
			(kind == SeparatorWord && class == ClassRegular) {
			return i
		}
		i += w
	}
	return len(line)
}

// NextWord finds the word that starts at or after col, looking forward as VS
// Code's word navigation does (_doFindNextWordOnLine). The second result is false
// when the line has no word after col.
func (c *WordClassifier) NextWord(line string, col int) (Word, bool) {
	col = min(max(col, 0), len(line))
	kind := WordKind(0)
	for i := col; i < len(line); {
		r, w := utf8.DecodeRuneInString(line[i:])
		class := c.Class(r)
		switch class {
		case ClassRegular:
			if kind == SeparatorWord {
				return Word{Start: c.startOfWord(line, kind, i), End: i, Kind: kind, NextClass: class}, true
			}
			kind = RegularWord
		case ClassSeparator:
			if kind == RegularWord {
				return Word{Start: c.startOfWord(line, kind, i), End: i, Kind: kind, NextClass: class}, true
			}
			kind = SeparatorWord
		case ClassWhitespace:
			if kind != 0 {
				return Word{Start: c.startOfWord(line, kind, i), End: i, Kind: kind, NextClass: class}, true
			}
		}
		i += w
	}
	if kind != 0 {
		return Word{Start: c.startOfWord(line, kind, len(line)), End: len(line), Kind: kind, NextClass: ClassWhitespace}, true
	}
	return Word{}, false
}

// startOfWord is where a word of the given kind that ends at end starts.
func (c *WordClassifier) startOfWord(line string, kind WordKind, end int) int {
	for i := end; i > 0; {
		r, w := utf8.DecodeLastRuneInString(line[:i])
		i -= w
		class := c.Class(r)
		if class == ClassWhitespace ||
			(kind == RegularWord && class == ClassSeparator) ||
			(kind == SeparatorWord && class == ClassRegular) {
			return i + w
		}
	}
	return 0
}

// WordAt finds the word of regular characters that touches col, as VS Code's
// getWordAtPosition does for the cursor commands: the word that ends at or after
// col and starts at or before it. The second result is false when col is not in
// or next to a word.
func (c *WordClassifier) WordAt(line string, col int) (Word, bool) {
	col = min(max(col, 0), len(line))
	if w, ok := c.PrevWord(line, col); ok && w.Kind == RegularWord && w.Start <= col && col <= w.End {
		return w, true
	}
	if w, ok := c.NextWord(line, col); ok && w.Kind == RegularWord && w.Start <= col && col <= w.End {
		return w, true
	}
	return Word{}, false
}

func isLowerASCII(b byte) bool { return b >= 'a' && b <= 'z' }
func isUpperASCII(b byte) bool { return b >= 'A' && b <= 'Z' }
func isDigitASCII(b byte) bool { return b >= '0' && b <= '9' }

// isPartStart reports whether a word part starts between line[o-1] and line[o]
// (0 < o < len(line)): after an underscore or a dash, before one, at the start of
// a capital after a lower-case letter or digit, and at the last capital of a run
// of capitals that is followed by a lower-case letter or digit.
func isPartStart(line string, o int, afterUnderscoreOrDash bool) bool {
	left, right := line[o-1], line[o]
	if afterUnderscoreOrDash {
		if left == '_' && right != '_' || left == '-' && right != '-' {
			return true
		}
	} else if left != '_' && right == '_' || left != '-' && right == '-' {
		return true
	}
	if (isLowerASCII(left) || isDigitASCII(left)) && isUpperASCII(right) {
		return true
	}
	if isUpperASCII(left) && isUpperASCII(right) && o+1 < len(line) {
		if next := line[o+1]; isLowerASCII(next) || isDigitASCII(next) {
			return true
		}
	}
	return false
}

// PrevWordPart returns the byte column of the start of the word part before col
// in line: parts of snake_case, kebab-case, camelCase and runs of capitals are
// split as VS Code's word-part navigation does (_moveWordPartLeft, the part that
// works inside a line). It returns 0 when there is no boundary before col, and
// then the caller moves to the previous line.
func PrevWordPart(line string, col int) int {
	col = min(max(col, 0), len(line))
	for o := col - 1; o > 0; o-- {
		if isPartStart(line, o, true) {
			return o
		}
	}
	return 0
}

// NextWordPart returns the byte column of the end of the word part after col in
// line (_moveWordPartRight inside a line), or len(line) when there is no
// boundary after col, and then the caller moves to the next line.
func NextWordPart(line string, col int) int {
	col = min(max(col, 0), len(line))
	for o := col + 1; o < len(line); o++ {
		if isPartStart(line, o, false) {
			return o
		}
	}
	return len(line)
}
