package buffer

import (
	"bytes"
	"iter"
	"unicode/utf8"

	"github.com/rm4n0s/errors"
)

// LineCount is the number of lines: one more than the line feeds, so an empty
// text has one empty line and a text that ends with a line feed ends with an
// empty line.
func (s Snapshot) LineCount() int { return lineFeeds(s.root) + 1 }

// lineBounds is the offset where a line starts and the offset where its text
// ends, before the line feed; line must be valid.
func (s Snapshot) lineBounds(line int) (start, end int) {
	if line > 0 {
		start = s.root.afterLineFeed(line)
	}
	if line < lineFeeds(s.root) {
		return start, s.root.afterLineFeed(line+1) - 1
	}
	return start, s.Len()
}

func (s Snapshot) checkLine(line int) error {
	if line < 0 || line >= s.LineCount() {
		return errors.New("LineOutOfRange", "the text has no such line", "line", line, "lines", s.LineCount())
	}
	return nil
}

// LineContent is the text of a line without its line feed. It fails with the
// tag LineOutOfRange unless 0 <= line < LineCount().
func (s Snapshot) LineContent(line int) (string, error) {
	if err := s.checkLine(line); err != nil {
		return "", err
	}
	start, end := s.lineBounds(line)
	return string(s.root.appendRange(make([]byte, 0, end-start), start, end)), nil
}

// LineLength is the length of a line in bytes, without its line feed. It fails
// with the tag LineOutOfRange unless 0 <= line < LineCount().
func (s Snapshot) LineLength(line int) (int, error) {
	if err := s.checkLine(line); err != nil {
		return 0, err
	}
	start, end := s.lineBounds(line)
	return end - start, nil
}

// LineStart is the offset where a line starts. It fails with the tag
// LineOutOfRange unless 0 <= line < LineCount().
func (s Snapshot) LineStart(line int) (int, error) {
	if err := s.checkLine(line); err != nil {
		return 0, err
	}
	start, _ := s.lineBounds(line)
	return start, nil
}

// clampPosition moves p into the text without looking at characters: a line
// before the first gives the start of the text, a line after the last the end,
// and a column outside its line the nearest end of it. It returns the bounds of
// the line it ended on.
func (s Snapshot) clampPosition(p Position) (pos Position, lineStart, lineEnd int) {
	if p.Line < 0 {
		return Position{}, 0, s.lineEndOf(0)
	}
	line := min(p.Line, s.LineCount()-1)
	start, end := s.lineBounds(line)
	if p.Line > line {
		return Position{Line: line, Col: end - start}, start, end
	}
	return Position{Line: line, Col: min(max(p.Col, 0), end-start)}, start, end
}

func (s Snapshot) lineEndOf(line int) int {
	_, end := s.lineBounds(line)
	return end
}

// insideCharacter reports whether the column of a position in a line that starts
// at lineStart is in the middle of a UTF-8 sequence.
func (s Snapshot) insideCharacter(p Position, lineStart, lineEnd int) bool {
	return p.Col > 0 && lineStart+p.Col < lineEnd && !utf8.RuneStart(s.root.byteAt(lineStart+p.Col))
}

// ValidatePosition moves p into the text, as VS Code's validatePosition does: a
// line before the first becomes the start of the text, a line after the last
// the end of the text, and a column outside its line the nearest end of it. A
// column in the middle of a UTF-8 sequence moves back to the start of the
// character, as VS Code moves out of the middle of a surrogate pair.
func (s Snapshot) ValidatePosition(p Position) Position {
	pos, start, end := s.clampPosition(p)
	for s.insideCharacter(pos, start, end) {
		pos.Col--
	}
	return pos
}

// ValidateRange moves both ends of r into the text, as VS Code's validateRange
// does. An end that is inside a UTF-8 sequence moves out of it: a range that
// covers text grows to take the whole character (the start moves back and the
// end moves forward), and an empty range moves back, so it does not grow.
func (s Snapshot) ValidateRange(r Range) Range {
	start, startLine, startEnd := s.clampPosition(r.Start)
	end, endLine, endEnd := s.clampPosition(r.End)
	if end.IsBefore(start) { // the ends were swapped by clamping a line far away
		start, end = end, start
		startLine, endLine = endLine, startLine
		startEnd, endEnd = endEnd, startEnd
	}
	startInside := s.insideCharacter(start, startLine, startEnd)
	endInside := s.insideCharacter(end, endLine, endEnd)
	collapsed := start == end
	for startInside && s.insideCharacter(start, startLine, startEnd) {
		start.Col--
	}
	switch {
	case collapsed:
		end = start
	case endInside:
		for s.insideCharacter(end, endLine, endEnd) {
			end.Col++
		}
	}
	return Range{Start: start, End: end}
}

// OffsetAt is the offset of a position, after ValidatePosition has moved it into
// the text.
func (s Snapshot) OffsetAt(p Position) int {
	p = s.ValidatePosition(p)
	start, _ := s.lineBounds(p.Line)
	return start + p.Col
}

// PositionAt is the position of an offset. An offset before the start or after
// the end of the text is moved to it. An offset in the middle of a UTF-8
// sequence gives the column in the middle of it.
func (s Snapshot) PositionAt(offset int) Position {
	offset = min(max(offset, 0), s.Len())
	line := s.root.lineFeedsBefore(offset)
	start := 0
	if line > 0 {
		start = s.root.afterLineFeed(line)
	}
	return Position{Line: line, Col: offset - start}
}

// Text is the text of a range, after ValidateRange has moved it into the text.
func (s Snapshot) Text(r Range) string {
	r = s.ValidateRange(r)
	start, end := s.OffsetAt(r.Start), s.OffsetAt(r.End)
	return string(s.root.appendRange(make([]byte, 0, end-start), start, end))
}

// Chunks goes through the text piece by piece, in order. The pieces are the
// buffer's own division of the text, so they have no meaning for the caller
// beyond being parts that put together give the text.
func (s Snapshot) Chunks() iter.Seq[string] {
	return func(yield func(string) bool) {
		s.root.walk(0, s.Len(), func(b []byte) bool { return yield(string(b)) })
	}
}

// Lines goes through the lines from line from to the last, giving each one's
// index and its text without the line feed. A from before the first line starts
// at the first; one after the last gives nothing. The last line is given even
// when it is empty.
func (s Snapshot) Lines(from int) iter.Seq2[int, string] {
	return func(yield func(int, string) bool) {
		from := max(from, 0)
		if from >= s.LineCount() {
			return
		}
		start, _ := s.lineBounds(from)
		line := from
		var partial []byte // the beginning of a line that goes on in the next piece
		finished := s.root.walk(start, s.Len(), func(b []byte) bool {
			for {
				i := bytes.IndexByte(b, '\n')
				if i < 0 {
					partial = append(partial, b...)
					return true
				}
				text := string(b[:i])
				if len(partial) > 0 {
					text = string(append(partial, b[:i]...))
					partial = partial[:0]
				}
				if !yield(line, text) {
					return false
				}
				line++
				b = b[i+1:]
			}
		})
		if finished {
			yield(line, string(partial))
		}
	}
}
