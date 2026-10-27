package markdown

import (
	"math"
	"strings"
)

// The numbers of the estimate: how tall a block is before it has been laid out. They only have to be near: a block that is
// built is measured, and the estimate is used for the blocks that were never on the screen (the length of the scroll bar, the
// place a jump to a line lands on before the blocks above it are measured).
const (
	// textCharWidth is the width of an average character of the text, in em; codeCharWidth that of the monospace font.
	textCharWidth = 0.55
	codeCharWidth = 0.6
	// blockGap is the space between two blocks of the page (the top margin of a block that is not the first).
	blockGap = 10.0
	// imageEstimate is the height of an image that has not been loaded.
	imageEstimate = 320.0
	// codePadding is the padding of a code block, on each side.
	codePadding = 12.0
)

// estimator estimates the height of blocks for a width of the page.
type estimator struct {
	// width is the width the top-level blocks have.
	width float64
}

// blockHeight is the height of a top-level block, without the gap that follows it.
func (e estimator) blockHeight(b Block) float64 { return e.block(b, e.width) }

// block is the height of a block in a width.
func (e estimator) block(b Block, width float64) float64 {
	width = max(width, 40)
	switch b := b.(type) {
	case Heading:
		level := min(max(b.Level, 1), 6)
		size := bodySize * headingSizes[level-1]
		lines, extra := e.textLines(b.Inlines, size, width)
		h := lines*size*bodyLineHeight + 14 + 2
		if level <= 2 {
			h += 5 // the padding and the line under the heading
		}
		return h + extra
	case Paragraph:
		lines, extra := e.textLines(b.Inlines, bodySize, width)
		return lines*bodySize*bodyLineHeight + extra
	case CodeBlock:
		lines := 0.0
		chars := (width - 2*codePadding) / (codeSize * codeCharWidth)
		for _, l := range strings.Split(expandTabs(b.Text, 4), "\n") {
			lines += math.Max(1, math.Ceil(float64(len([]rune(l)))/math.Max(chars, 1)))
		}
		return lines*codeSize*1.4 + 2*codePadding
	case Quote:
		return e.blocks(b.Blocks, width-31) + 8
	case List:
		h := 0.0
		for _, it := range b.Items {
			ih := e.blocks(it.Blocks, width-42)
			h += math.Max(ih, bodySize*bodyLineHeight)
		}
		return h
	case Table:
		cols := max(len(b.Header), 1)
		colWidth := math.Max(width/float64(cols)-24, 40)
		h := 1.0
		for _, row := range append([][]TableCell{b.Header}, b.Rows...) {
			rowLines := 1.0
			for _, c := range row {
				lines, _ := e.textLines(c.Inlines, bodySize, colWidth)
				rowLines = math.Max(rowLines, lines)
			}
			h += rowLines*bodySize*bodyLineHeight + 10 + 1
		}
		return h
	case Rule:
		return 8 + 2 + 8
	case HTMLBlock:
		lines := float64(strings.Count(strings.TrimRight(b.Text, "\n"), "\n") + 1)
		return lines * codeSize * 1.4
	}
	return 0
}

// blocks is the height of blocks one under the other.
func (e estimator) blocks(bs []Block, width float64) float64 {
	h := 0.0
	for i, b := range bs {
		if i > 0 {
			h += blockGap
		}
		h += e.block(b, width)
	}
	return h
}

// textLines is the number of lines the inlines take in a width at a size, and the height of the images in them.
func (e estimator) textLines(in []Inline, size, width float64) (lines, images float64) {
	chars, hard, imgs := countInlines(in)
	perLine := math.Max(width/(size*textCharWidth), 1)
	lines = math.Max(1, math.Ceil(float64(chars)/perLine)) + float64(hard)
	if imgs > 0 && chars == 0 {
		lines = 0 // a paragraph that is only images
	}
	return lines, float64(imgs) * imageEstimate
}

// countInlines counts the characters of text, the hard breaks and the images of inlines.
func countInlines(in []Inline) (chars, hard, images int) {
	for _, n := range in {
		switch n := n.(type) {
		case Text:
			chars += len(n.S)
		case Emph:
			c, h, i := countInlines(n.Children)
			chars, hard, images = chars+c, hard+h, images+i
		case Strong:
			c, h, i := countInlines(n.Children)
			chars, hard, images = chars+c, hard+h, images+i
		case Strike:
			c, h, i := countInlines(n.Children)
			chars, hard, images = chars+c, hard+h, images+i
		case Link:
			c, h, i := countInlines(n.Children)
			chars, hard, images = chars+c, hard+h, images+i
		case Code:
			chars += len(n.S)
		case HTMLInline:
			chars += len(n.S)
		case SoftBreak:
			chars++
		case HardBreak:
			hard++
		case Image:
			images++
		}
	}
	return chars, hard, images
}
