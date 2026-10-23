package edit

import (
	"github.com/rm4n0s/emetos/internal/core/buffer"
	"github.com/rm4n0s/emetos/internal/core/textmate"
	"github.com/rm4n0s/emetos/internal/core/tokens"
)

// LineTokens gives the tokens of a line, or nil when it has none (a *tokens.Tokenizer is one).
type LineTokens interface {
	Line(line int) []tokens.Token
}

// stringSelectionRanges is the provider of the steps of Expand Selection that a string around
// the position makes.
type stringSelectionRanges struct {
	src LineTokens
}

// NewStringSelectionRanges makes the provider of the string steps of Expand Selection: for a
// position in a string (the tokens of the standard type "string" that touch it, which may be
// several: the quotes, the text, the escapes), the text inside the quotes and the string with
// them. A string that has no quotes at its ends, or that continues on another line, has only
// the second step.
func NewStringSelectionRanges(src LineTokens) SelectionRangeProvider {
	return stringSelectionRanges{src: src}
}

func (p stringSelectionRanges) SelectionRanges(m Model, positions []buffer.Position) [][]buffer.Range {
	out := make([][]buffer.Range, len(positions))
	for i, pos := range positions {
		if pos.Line < 0 || pos.Line >= m.LineCount() {
			continue
		}
		toks := p.src.Line(pos.Line)
		// The run of string tokens that the position is in or next to.
		start, end := -1, -1
		from := 0
		for k, t := range toks {
			to := int(t.End)
			if t.Type == textmate.TokenString && from <= pos.Col && pos.Col <= to {
				first, last := k, k
				for first > 0 && toks[first-1].Type == textmate.TokenString {
					first--
				}
				for last+1 < len(toks) && toks[last+1].Type == textmate.TokenString {
					last++
				}
				if first > 0 {
					start = int(toks[first-1].End)
				} else {
					start = 0
				}
				end = int(toks[last].End)
				break
			}
			from = to
		}
		if start < 0 || start >= end {
			continue
		}
		line := m.LineContent(pos.Line)
		end = min(end, len(line))
		outer := rangeOf(pos.Line, start, pos.Line, end)
		var bucket []buffer.Range
		if end-start >= 2 && isQuote(line[start]) && line[start] == line[end-1] {
			bucket = append(bucket, rangeOf(pos.Line, start+1, pos.Line, end-1))
		}
		out[i] = append(bucket, outer)
	}
	return out
}

func isQuote(c byte) bool { return c == '"' || c == '\'' || c == '`' }
