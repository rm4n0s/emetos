package folding

import (
	"context"
	"slices"

	"github.com/rm4n0s/errors"

	"github.com/rm4n0s/emetos/internal/core/indent"
	"github.com/rm4n0s/emetos/internal/core/regex"
)

// Ported from VS Code: src/vs/editor/contrib/folding/browser/indentRangeProvider.ts (MIT)

// Text is the lines of a text the providers read.
type Text interface {
	// LineCount is the number of lines, at least 1.
	LineCount() int
	// LineContent is the content of a line (zero-based) without its line break.
	LineContent(line int) string
}

// Markers are the patterns of the lines that start and end a region (`#region`, `// #endregion`).
type Markers struct {
	Start, End *regex.Regexp
}

// Limit is the most regions a provider may give, and where it is told how many it found.
type Limit struct {
	// Max is the most regions; DefaultLimit when 0.
	Max int
	// Update, when it is not nil, is told how many regions were found and how many were left out (0
	// when none).
	Update func(computed, limited int)
}

func (l Limit) max() int {
	if l.Max <= 0 {
		return DefaultLimit
	}
	return l.Max
}

// RangesCollector gathers the regions the indentation provider finds, last first.
type RangesCollector struct {
	starts, ends []int
	occurrences  []int // for each indentation, the number of regions that start on a line with it
	limit        Limit
}

// NewRangesCollector makes a collector with a limit.
func NewRangesCollector(limit Limit) *RangesCollector { return &RangesCollector{limit: limit} }

// InsertFirst adds a region that is before the ones added so far; indent is the indentation of its
// first line.
func (c *RangesCollector) InsertFirst(start, end, indent int) {
	if start > MaxLine || end > MaxLine {
		return
	}
	c.starts, c.ends = append(c.starts, start), append(c.ends, end)
	if indent < 1000 {
		for len(c.occurrences) <= indent {
			c.occurrences = append(c.occurrences, 0)
		}
		c.occurrences[indent]++
	}
}

// ToRegions makes the regions in the order of their lines. When there are more than the limit
// allows, the ones that start on the lines with the least indentation are kept.
func (c *RangesCollector) ToRegions(text Text, tabSize int) (*Regions, error) {
	limit := c.limit.max()
	n := len(c.starts)
	if n <= limit {
		if c.limit.Update != nil {
			c.limit.Update(n, 0)
		}
		return NewRegions(reversed(c.starts), reversed(c.ends), nil)
	}
	if c.limit.Update != nil {
		c.limit.Update(n, limit)
	}
	entries, maxIndent := 0, len(c.occurrences)
	for i, count := range c.occurrences {
		if count > 0 {
			if count+entries > limit {
				maxIndent = i
				break
			}
			entries += count
		}
	}
	starts, ends := make([]int, 0, limit), make([]int, 0, limit)
	for i := n - 1; i >= 0; i-- {
		level := indent.Level(text.LineContent(c.starts[i]), tabSize)
		if level < maxIndent || (level == maxIndent && entries < limit) {
			if level == maxIndent {
				entries++
			}
			starts, ends = append(starts, c.starts[i]), append(ends, c.ends[i])
		}
	}
	return NewRegions(starts, ends, nil)
}

func reversed(s []int) []int {
	out := slices.Clone(s)
	slices.Reverse(out)
	return out
}

// previousRegion is a block the scan has seen, from the bottom of the text up.
type previousRegion struct {
	indent   int // the indentation, or -2 for a marker
	endAbove int // the line the block above ends before
	line     int // the first line, for a marker region
}

// matches reports whether a pattern matches a line.
func matches(re *regex.Regexp, line string) bool {
	if re == nil {
		return false
	}
	_, ok, err := re.Find([]rune(line), 0)
	return ok && err == nil
}

// ComputeRanges finds the folding regions of a text by its indentation: a line with text that is
// followed by lines indented deeper starts a region that ends on the last of them. With offSide
// (languages like Python) the empty lines after a block belong to it. With markers, the lines that
// start and end a region (`#region`) make regions of their own, which end on the end marker.
//
// The start marker pattern is tested first, so a line that matches both starts a region. VS Code
// joins the two patterns when their flags agree and takes the leftmost match; the two give the same
// result for markers that are anchored at the start of the line, as the bundled ones are.
//
// Ported from VS Code: indentRangeProvider.ts (computeRanges)
func ComputeRanges(text Text, tabSize int, offSide bool, markers *Markers, limit Limit) (*Regions, error) {
	return computeRanges(context.Background(), text, tabSize, offSide, markers, limit)
}

// computeRanges is ComputeRanges that stops with the error of ctx when it is cancelled.
func computeRanges(ctx context.Context, text Text, tabSize int, offSide bool, markers *Markers, limit Limit) (*Regions, error) {
	result := NewRangesCollector(limit)
	count := text.LineCount()
	previous := []previousRegion{{indent: -1, endAbove: count, line: count}} // a sentinel, so that there is always one
	for line := count - 1; line >= 0; line-- {
		if line%4096 == 0 {
			if err := ctx.Err(); err != nil {
				return nil, errors.NewErr("FoldingCancelled", err)
			}
		}
		content := text.LineContent(line)
		level := indent.Level(content, tabSize)
		last := &previous[len(previous)-1]
		if level == -1 {
			if offSide {
				// The empty lines of the language are part of the block above them.
				last.endAbove = line
			}
			continue
		}
		isStart, isEnd := false, false
		if markers != nil {
			isStart = matches(markers.Start, content)
			if !isStart {
				isEnd = matches(markers.End, content)
			}
		}
		if isStart || isEnd {
			if isStart {
				// Throw away the regions until the end marker.
				i := len(previous) - 1
				for i > 0 && previous[i].indent != -2 {
					i--
				}
				if i > 0 {
					previous = previous[:i+1]
					last = &previous[i]
					// The region of the markers includes the end line.
					result.InsertFirst(line, last.line, level)
					last.line, last.indent, last.endAbove = line, level, line
					continue
				}
				// No end marker: the line is a regular one.
			} else {
				previous = append(previous, previousRegion{indent: -2, endAbove: line, line: line})
				continue
			}
		}
		if last.indent > level {
			// Throw away the regions with a deeper indentation.
			for {
				previous = previous[:len(previous)-1]
				last = &previous[len(previous)-1]
				if last.indent <= level {
					break
				}
			}
			end := last.endAbove - 1
			if end-line >= 1 {
				result.InsertFirst(line, end, level)
			}
		}
		if last.indent == level {
			last.endAbove = line
		} else {
			previous = append(previous, previousRegion{indent: level, endAbove: line, line: line})
		}
	}
	return result.ToRegions(text, tabSize)
}
