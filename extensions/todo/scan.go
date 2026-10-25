package todo

import (
	"strings"
	"unicode"
	"unicode/utf16"
	"unicode/utf8"
)

// Item is a TODO or FIXME comment: the tag, the text after it and its place in the file.
type Item struct {
	// Tag is the word that marks the comment: TODO, FIXME.
	Tag string
	// Message is the text of the comment after the tag, without the punctuation that follows the tag and the end of the
	// comment.
	Message string
	// Line counts from 1, and Column from 1 in UTF-16 units: where the tag starts.
	Line, Column int
}

// commentStarts are the strings that start a comment in the languages of the bundled packs (C-like, shell and Python,
// SQL and Lua, HTML).
var commentStarts = []string{"//", "#", "/*", "<!--", "--"}

// scanText finds the comments of the text that carry one of the tags. A tag counts only inside a comment: after a comment
// starter on its line, or on a line that continues a block comment (it starts with a *), and only as a whole word. A line
// that is too long to be a comment of a human is skipped.
func scanText(text string, tags []string) []Item {
	if len(tags) == 0 {
		return nil
	}
	var items []Item
	line := 0
	for len(text) > 0 {
		var l string
		l, text, _ = strings.Cut(text, "\n")
		line++
		l = strings.TrimSuffix(l, "\r")
		if len(l) > maxLine {
			continue
		}
		if it, ok := scanLine(l, tags); ok {
			it.Line = line
			items = append(items, it)
		}
	}
	return items
}

// maxLine is the longest line looked at: minified files have lines of megabytes.
const maxLine = 1000

// scanLine finds the first tag in the comment part of a line.
func scanLine(l string, tags []string) (Item, bool) {
	start := commentStart(l)
	if start < 0 {
		return Item{}, false
	}
	best, bestTag := -1, ""
	for _, tag := range tags {
		for from := start; ; {
			i := strings.Index(l[from:], tag)
			if i < 0 {
				break
			}
			i += from
			if wholeWord(l, i, len(tag)) {
				if best < 0 || i < best {
					best, bestTag = i, tag
				}
				break
			}
			from = i + len(tag)
		}
	}
	if best < 0 {
		return Item{}, false
	}
	rest := l[best+len(bestTag):]
	return Item{Tag: bestTag, Message: message(rest), Column: utf16Column(l, best)}, true
}

// commentStart is the index where the comment of a line starts, or -1 when the line has none.
func commentStart(l string) int {
	trimmed := strings.TrimLeftFunc(l, unicode.IsSpace)
	if strings.HasPrefix(trimmed, "*") {
		return len(l) - len(trimmed)
	}
	first := -1
	for _, c := range commentStarts {
		if i := strings.Index(l, c); i >= 0 && (first < 0 || i < first) {
			first = i
		}
	}
	return first
}

// wholeWord tells whether the n bytes of the text at i are a word of their own: no letter, digit or underscore on either
// side.
func wholeWord(l string, i, n int) bool {
	if i > 0 {
		r, _ := utf8.DecodeLastRuneInString(l[:i])
		if isWord(r) {
			return false
		}
	}
	if i+n < len(l) {
		r, _ := utf8.DecodeRuneInString(l[i+n:])
		if isWord(r) {
			return false
		}
	}
	return true
}

func isWord(r rune) bool { return r == '_' || unicode.IsLetter(r) || unicode.IsDigit(r) }

// message is the text after the tag: the "(owner)" and the punctuation that follows the tag are dropped, and so is the end
// of a block comment.
func message(rest string) string {
	rest = strings.TrimSpace(rest)
	if strings.HasPrefix(rest, "(") {
		if i := strings.Index(rest, ")"); i >= 0 {
			rest = rest[i+1:]
		}
	}
	rest = strings.TrimLeft(rest, " \t:-–—")
	for _, end := range []string{"*/", "-->"} {
		if i := strings.Index(rest, end); i >= 0 {
			rest = rest[:i]
		}
	}
	return strings.TrimSpace(rest)
}

// utf16Column is the column, from 1 and in UTF-16 units, of the byte at index i of a line.
func utf16Column(l string, i int) int {
	col := 1
	for _, r := range l[:i] {
		col += utf16.RuneLen(r)
	}
	return col
}
