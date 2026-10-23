package find

import (
	"strconv"
	"strings"
	"unicode"
	"unicode/utf8"
)

// Ported from VS Code: src/vs/editor/contrib/find/browser/replacePattern.ts and
// src/vs/base/common/search.ts (MIT)

// ReplacePattern is a parsed replace string: static text and references to the groups of a match
// (VS Code's ReplacePattern).
type ReplacePattern struct {
	// static is the whole value of a pattern that has no references, and pieces the parts of one
	// that has.
	static string
	pieces []replacePiece
}

// replacePiece is a static string, or a group of the match with the case operations of \u, \U, \l
// and \L before it.
type replacePiece struct {
	static   string
	isStatic bool
	group    int
	caseOps  []rune
}

// StaticReplacePattern is the pattern of a replace string with nothing special in it (a replace
// string of a search that is not a regular expression).
func StaticReplacePattern(value string) ReplacePattern { return ReplacePattern{static: value} }

// HasReplacementPatterns says whether the pattern refers to the match.
func (p ReplacePattern) HasReplacementPatterns() bool { return p.pieces != nil }

// Build makes the replacement of a match. groups are the text of the match and of its groups (nil
// when the pattern and preserveCase need none); preserveCase makes the replacement follow the case
// of the text it replaces.
func (p ReplacePattern) Build(groups []string, preserveCase bool) string {
	if p.pieces == nil {
		if preserveCase {
			return buildReplaceStringWithCasePreserved(groups, p.static)
		}
		return p.static
	}
	var b strings.Builder
	for _, piece := range p.pieces {
		if piece.isStatic {
			b.WriteString(piece.static)
			continue
		}
		match := substitute(piece.group, groups)
		if len(piece.caseOps) > 0 {
			match = applyCaseOps(match, piece.caseOps)
		}
		b.WriteString(match)
	}
	return b.String()
}

// applyCaseOps changes the case of the characters of a match: \u and \l the next one, \U and \L all
// the rest.
func applyCaseOps(match string, ops []rune) string {
	var b strings.Builder
	op := 0
	for i, r := range match {
		if op >= len(ops) {
			b.WriteString(match[i:])
			break
		}
		switch ops[op] {
		case 'U':
			b.WriteString(strings.ToUpper(string(r)))
		case 'u':
			b.WriteString(strings.ToUpper(string(r)))
			op++
		case 'L':
			b.WriteString(strings.ToLower(string(r)))
		case 'l':
			b.WriteString(strings.ToLower(string(r)))
			op++
		default:
			b.WriteRune(r)
		}
	}
	return b.String()
}

// substitute is the text of a group; a number with no such group is read as the group of its first
// digits followed by the others as text, and when no group has them, as "$" and the number.
func substitute(group int, groups []string) string {
	if groups == nil {
		return ""
	}
	if group == 0 {
		return groups[0]
	}
	remainder := ""
	for group > 0 {
		if group < len(groups) {
			return groups[group] + remainder
		}
		remainder = strconv.Itoa(group%10) + remainder
		group /= 10
	}
	return "$" + remainder
}

// pieceBuilder collects the pieces of a pattern as the replace string is read.
type pieceBuilder struct {
	source  string
	last    int
	pieces  []replacePiece
	current strings.Builder
}

func (b *pieceBuilder) emitUnchanged(to int) {
	b.current.WriteString(b.source[b.last:to])
	b.last = to
}

func (b *pieceBuilder) emitStatic(value string, to int) {
	b.current.WriteString(value)
	b.last = to
}

func (b *pieceBuilder) flushStatic() {
	if b.current.Len() != 0 {
		b.pieces = append(b.pieces, replacePiece{static: b.current.String(), isStatic: true})
		b.current.Reset()
	}
}

func (b *pieceBuilder) emitGroup(group, to int, ops []rune) {
	b.flushStatic()
	b.pieces = append(b.pieces, replacePiece{group: group, caseOps: append([]rune(nil), ops...)})
	b.last = to
}

func (b *pieceBuilder) finish() ReplacePattern {
	b.emitUnchanged(len(b.source))
	b.flushStatic()
	switch {
	case len(b.pieces) == 0:
		return ReplacePattern{}
	case len(b.pieces) == 1 && b.pieces[0].isStatic:
		return ReplacePattern{static: b.pieces[0].static}
	}
	return ReplacePattern{pieces: b.pieces}
}

// ParseReplaceString reads the replace string of a regular expression search:
//
//	\n            a line feed
//	\t            a tab
//	\\            a backslash
//	\u and \U     upper-case one character, or all the rest, of a group
//	\l and \L     lower-case one character, or all the rest, of a group
//	$$            a dollar sign
//	$& and $0     the match
//	$n and $nn    the group n (up to 99)
//
// and everything else stays as it is.
func ParseReplaceString(replace string) ReplacePattern {
	if replace == "" {
		return ReplacePattern{}
	}
	var ops []rune
	b := &pieceBuilder{source: replace}
	for i := 0; i < len(replace); i++ {
		switch replace[i] {
		case '\\':
			i++
			if i >= len(replace) {
				break
			}
			switch c := replace[i]; c {
			case '\\':
				b.emitUnchanged(i - 1)
				b.emitStatic("\\", i+1)
			case 'n':
				b.emitUnchanged(i - 1)
				b.emitStatic("\n", i+1)
			case 't':
				b.emitUnchanged(i - 1)
				b.emitStatic("\t", i+1)
			case 'u', 'U', 'l', 'L':
				b.emitUnchanged(i - 1)
				b.emitStatic("", i+1)
				ops = append(ops, rune(c))
			}
		case '$':
			i++
			if i >= len(replace) {
				break
			}
			c := replace[i]
			switch {
			case c == '$':
				b.emitUnchanged(i - 1)
				b.emitStatic("$", i+1)
			case c == '0' || c == '&':
				b.emitUnchanged(i - 1)
				b.emitGroup(0, i+1, ops)
				ops = ops[:0]
			case '1' <= c && c <= '9':
				group := int(c - '0')
				if i+1 < len(replace) && '0' <= replace[i+1] && replace[i+1] <= '9' {
					i++
					group = group*10 + int(replace[i]-'0')
					b.emitUnchanged(i - 2)
				} else {
					b.emitUnchanged(i - 1)
				}
				b.emitGroup(group, i+1, ops)
				ops = ops[:0]
			}
		}
	}
	return b.finish()
}

// buildReplaceStringWithCasePreserved gives the replacement the case of the text it replaces:
// all upper, all lower, a capital first letter, or, for words joined by hyphens or underscores,
// the case of each word.
func buildReplaceStringWithCasePreserved(groups []string, pattern string) string {
	if groups == nil || groups[0] == "" {
		return pattern
	}
	match := groups[0]
	hyphens := hasSpecial(match, pattern, "-")
	underscores := hasSpecial(match, pattern, "_")
	if hyphens && !underscores {
		return preserveCaseOfWords(match, pattern, "-")
	}
	if !hyphens && underscores {
		return preserveCaseOfWords(match, pattern, "_")
	}
	first, _ := utf8.DecodeRuneInString(match)
	switch {
	case strings.ToUpper(match) == match:
		return strings.ToUpper(pattern)
	case strings.ToLower(match) == match:
		return strings.ToLower(pattern)
	case unicode.ToLower(first) != first && pattern != "":
		return upperFirst(pattern)
	case unicode.ToUpper(first) != first && pattern != "":
		return lowerFirst(pattern)
	}
	return pattern // the case is not one that is understood
}

func upperFirst(s string) string {
	r, n := utf8.DecodeRuneInString(s)
	return strings.ToUpper(string(r)) + s[n:]
}

func lowerFirst(s string) string {
	r, n := utf8.DecodeRuneInString(s)
	return strings.ToLower(string(r)) + s[n:]
}

// hasSpecial says whether the match and the pattern both have the character, in as many words.
func hasSpecial(match, pattern, special string) bool {
	return strings.Contains(match, special) && strings.Contains(pattern, special) &&
		strings.Count(match, special) == strings.Count(pattern, special)
}

// preserveCaseOfWords follows the case of each word of the match in the word of the pattern.
func preserveCaseOfWords(match, pattern, special string) string {
	words := strings.Split(pattern, special)
	matchWords := strings.Split(match, special)
	out := make([]string, len(words))
	for i, w := range words {
		out[i] = buildReplaceStringWithCasePreserved([]string{matchWords[i]}, w)
	}
	return strings.Join(out, special)
}
