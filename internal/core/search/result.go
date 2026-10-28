package search

import (
	"bytes"
	"unicode/utf8"
)

// Match is one match of a search. Lines and columns count from 0, and a column counts the bytes of the UTF-8 line, as the
// positions of the core events do (SPEC §10.15 "As built (step 11.2)"): a match is at the same place in the document that
// opens.
type Match struct {
	// Line, Col, EndLine and EndCol are the range of the match.
	Line, Col, EndLine, EndCol int
	// Preview is the text of the line where the match starts, with no line end. A line longer than MaxPreview bytes is
	// cut around the match.
	Preview string
	// PreviewStart and PreviewEnd are the match in Preview (bytes); a match that goes on to the next lines ends at the
	// end of Preview.
	PreviewStart, PreviewEnd int
	// Text is the matched text, with "\n" for each line break.
	Text string
}

// FileResult is the matches of one file.
type FileResult struct {
	// Path is the path relative to the folder, with forward slashes.
	Path string
	// Matches are the matches in the order of the text.
	Matches []Match
}

// MaxPreview is the most bytes of a line a Match keeps in its Preview.
const MaxPreview = 1000

// locator turns the offsets of the matches of a text, which only grow, into lines.
type locator struct {
	data []byte
	pos  int // the offset where the line begins
	line int
}

// at returns the line of the offset and the offset where it begins.
func (l *locator) at(off int) (line, lineStart int) {
	gap := l.data[l.pos:off]
	if n := bytes.Count(gap, nlByte); n > 0 {
		l.line += n
		l.pos += bytes.LastIndexByte(gap, '\n') + 1
	}
	return l.line, l.pos
}

var nlByte = []byte{'\n'}

// match makes the Match of the bytes [s, e) of the text.
func (l *locator) match(s, e int) Match {
	data := l.data
	line, ls := l.at(s)
	le := len(data)
	if i := bytes.IndexByte(data[ls:], '\n'); i >= 0 {
		le = ls + i
	}
	if le > ls && data[le-1] == '\r' {
		le--
	}
	m := Match{Line: line, Col: s - ls, EndLine: line, Text: string(data[s:e])}
	endCol := e - ls
	if i := bytes.LastIndexByte(data[s:e], '\n'); i >= 0 {
		m.EndLine += bytes.Count(data[s:e], nlByte)
		endCol = e - (s + i + 1)
	}
	m.EndCol = endCol
	ms, me := s, min(e, le) // the match on its first line
	if m.EndLine != line {
		me = le
	}
	ps, pe := ls, le
	if pe-ps > MaxPreview {
		ps = max(ls, s-MaxPreview/10)
		for ps > ls && !utf8.RuneStart(data[ps]) {
			ps--
		}
		pe = min(le, ps+MaxPreview)
		for pe > ps && pe < le && !utf8.RuneStart(data[pe]) {
			pe--
		}
		me = min(me, pe)
		ms = min(ms, pe)
	}
	m.Preview = string(data[ps:pe])
	m.PreviewStart, m.PreviewEnd = ms-ps, me-ps
	return m
}
