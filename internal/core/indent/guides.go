package indent

import (
	"math"
	"sort"

	"github.com/rm4n0s/emetos/internal/core/brackets"
	"github.com/rm4n0s/emetos/internal/core/buffer"
)

// Ported from VS Code: src/vs/editor/common/model/guidesTextModelPart.ts,
// src/vs/editor/common/textModelGuides.ts and
// src/vs/editor/browser/viewParts/indentGuides/indentGuides.ts (MIT)

// GuideColors is the number of levels the colors of the guides repeat in (VS Code's lcm of 1 to 6).
const GuideColors = 30

// Guide is a line the editor draws below the text: a vertical one at a column of a line, or a
// horizontal one that runs from a place of the line to another.
type Guide struct {
	// Bracket is true for a guide of a bracket pair, false for an indent guide.
	Bracket bool
	// Level is the level the color is taken from (reduced modulo GuideColors by the painter): the
	// indent level of an indent guide, the nesting level of a bracket pair.
	Level int
	// Active tells that the guide is highlighted: the indent guide of the scope the cursor is in,
	// or a guide of the bracket pair around it.
	Active bool
	// VisibleColumn is where a vertical guide stands, in columns counted from 1; -1 for a
	// horizontal guide, which stands at Column.
	VisibleColumn int
	// Column is the byte column of the line (from 0) where a horizontal guide starts; -1 for a
	// vertical guide.
	Column int
	// Horizontal, when it is not nil, makes the guide a horizontal line (the line from the bracket
	// to the vertical guide of its pair).
	Horizontal *HorizontalLine
	// ForWrappedAfter, when it is not -1, shows the guide only on the rows of a wrapped line that
	// do not hold this byte column and come after it; ForWrappedBeforeOrAt only on the rows
	// that hold it or come after it.
	ForWrappedAfter, ForWrappedBeforeOrAt int
}

// HorizontalLine is the horizontal part of a guide: a line at the top or at the bottom of the row
// that runs to a column.
type HorizontalLine struct {
	// Top puts the line at the top of the row; otherwise it is at the bottom.
	Top bool
	// EndColumn is the byte column of the line where it ends.
	EndColumn int
}

// ActiveGuide is the extent of the indent guide that is highlighted: the lines (zero-based, both
// included) of the scope and its indent level.
type ActiveGuide struct {
	Start, End, Indent int
}

// Guides computes the indent guides and the bracket pair guides of a text. The text must be the
// one its Levels read.
type Guides struct {
	// Levels holds the indentation of the lines.
	Levels *Levels
	// Text is the text of the Levels.
	Text LineSource
	// IndentSize is the size of an indent level, in columns.
	IndentSize int
	// OffSide makes a line of white space take the level of the region below it, as for languages
	// that are indented to nest (folding.offSide).
	OffSide bool
	// VisibleColumn gives the column of a byte offset of a line as it is drawn, from 0 (the
	// function edit.VisibleColumn); nil counts a column for each byte and tabs go to the next tab
	// stop.
	VisibleColumn func(line string, col, tabSize int) int
}

func (g Guides) indentSize() int { return max(g.IndentSize, 1) }

// ceilDiv is the division of a column count by the indent size, rounded up.
func (g Guides) ceilDiv(columns int) int { return (columns + g.indentSize() - 1) / g.indentSize() }

// levelForWhitespaceLine is the level of a line of white space only, from the indentation of the
// nearest lines with text above and below it (-1 when there is none).
func (g Guides) levelForWhitespaceLine(above, below int) int {
	size := g.indentSize()
	switch {
	case above == -1 || below == -1:
		// At the top or the bottom of the text.
		return 0
	case above < below:
		// Inside the region above.
		return 1 + above/size
	case above == below:
		// Between two regions.
		return g.ceilDiv(below)
	case g.OffSide:
		// The same level as the region below.
		return g.ceilDiv(below)
	}
	// Inside the region that ends below.
	return 1 + below/size
}

// ActiveIndentGuide finds the scope the guide that is highlighted belongs to for the cursor on a
// line (zero-based): the lines around it with the same or deeper indentation, looking at no
// line before minLine or after maxLine (the lines on the screen), at most 50,000 lines each way.
//
// Ported from VS Code: guidesTextModelPart.ts (getActiveIndentGuide)
func (g Guides) ActiveIndentGuide(line, minLine, maxLine int) ActiveGuide {
	lineCount := g.Text.LineCount()
	lineNumber := min(max(line+1, 1), lineCount)
	minLineNumber, maxLineNumber := minLine+1, maxLine+1
	lv := g.Levels

	upAboveIdx, upAboveIndent := -2, -1 // -2 marks what is not computed yet
	upBelowIdx, upBelowIndent := -2, -1
	upResolve := func(ln int) {
		if upAboveIdx != -1 && (upAboveIdx == -2 || upAboveIdx > ln-1) {
			upAboveIdx, upAboveIndent = -1, -1
			// Find the previous line with text.
			for i := ln - 2; i >= 0; i-- {
				if indent := lv.Level(i); indent >= 0 {
					upAboveIdx, upAboveIndent = i, indent
					break
				}
			}
		}
		if upBelowIdx == -2 {
			upBelowIdx, upBelowIndent = -1, -1
			// Find the next line with text.
			for i := ln; i < lineCount; i++ {
				if indent := lv.Level(i); indent >= 0 {
					upBelowIdx, upBelowIndent = i, indent
					break
				}
			}
		}
	}

	downAboveIdx, downAboveIndent := -2, -1
	downBelowIdx, downBelowIndent := -2, -1
	downResolve := func(ln int) {
		if downAboveIdx == -2 {
			downAboveIdx, downAboveIndent = -1, -1
			for i := ln - 2; i >= 0; i-- {
				if indent := lv.Level(i); indent >= 0 {
					downAboveIdx, downAboveIndent = i, indent
					break
				}
			}
		}
		if downBelowIdx != -1 && (downBelowIdx == -2 || downBelowIdx < ln-1) {
			downBelowIdx, downBelowIndent = -1, -1
			for i := ln; i < lineCount; i++ {
				if indent := lv.Level(i); indent >= 0 {
					downBelowIdx, downBelowIndent = i, indent
					break
				}
			}
		}
	}

	var startLine, endLine, indent, initialIndent int
	goUp, goDown := true, true
	for distance := 0; goUp || goDown; distance++ {
		upLine, downLine := lineNumber-distance, lineNumber+distance
		if distance > 1 && (upLine < 1 || upLine < minLineNumber) {
			goUp = false
		}
		if distance > 1 && (downLine > lineCount || downLine > maxLineNumber) {
			goDown = false
		}
		if distance > 50000 {
			goUp, goDown = false, false
		}

		upLevel := -1
		if goUp && upLine >= 1 {
			if current := lv.Level(upLine - 1); current >= 0 {
				// The line has text: its own indentation.
				upBelowIdx, upBelowIndent = upLine-1, current
				upLevel = g.ceilDiv(current)
			} else {
				upResolve(upLine)
				upLevel = g.levelForWhitespaceLine(upAboveIndent, upBelowIndent)
			}
		}
		downLevel := -1
		if goDown && downLine <= lineCount {
			if current := lv.Level(downLine - 1); current >= 0 {
				downAboveIdx, downAboveIndent = downLine-1, current
				downLevel = g.ceilDiv(current)
			} else {
				downResolve(downLine)
				downLevel = g.levelForWhitespaceLine(downAboveIndent, downBelowIndent)
			}
		}

		if distance == 0 {
			initialIndent = upLevel
			continue
		}
		if distance == 1 {
			if downLine <= lineCount && downLevel >= 0 && initialIndent+1 == downLevel {
				// The beginning of a scope: the guide of the child scope is the active one, not
				// the parent's.
				goUp = false
				startLine, endLine, indent = downLine, downLine, downLevel
				continue
			}
			if upLine >= 1 && upLevel >= 0 && upLevel-1 == initialIndent {
				// The end of a scope, as above.
				goDown = false
				startLine, endLine, indent = upLine, upLine, upLevel
				continue
			}
			startLine, endLine, indent = lineNumber, lineNumber, initialIndent
			if indent == 0 {
				return ActiveGuide{Start: startLine - 1, End: endLine - 1, Indent: indent}
			}
		}
		if goUp {
			if upLevel >= indent {
				startLine = upLine
			} else {
				goUp = false
			}
		}
		if goDown {
			if downLevel >= indent {
				endLine = downLine
			} else {
				goDown = false
			}
		}
	}
	return ActiveGuide{Start: startLine - 1, End: endLine - 1, Indent: indent}
}

// LinesIndentGuides gives, for each line from first to last (zero-based, both included), the
// number of indent guides it has: its indentation in levels, and for a line of white space only
// the level of the lines around it.
//
// Ported from VS Code: guidesTextModelPart.ts (getLinesIndentGuides)
func (g Guides) LinesIndentGuides(first, last int) []int {
	lineCount := g.Text.LineCount()
	first, last = min(max(first, 0), lineCount-1), min(max(last, 0), lineCount-1)
	if last < first {
		return nil
	}
	lv := g.Levels
	result := make([]int, last-first+1)
	aboveIdx, aboveIndent := -2, -1 // -2 marks what is not computed yet
	belowIdx, belowIndent := -2, -1
	for lineNumber := first + 1; lineNumber <= last+1; lineNumber++ {
		i := lineNumber - (first + 1)
		if current := lv.Level(lineNumber - 1); current >= 0 {
			aboveIdx, aboveIndent = lineNumber-1, current
			result[i] = g.ceilDiv(current)
			continue
		}
		if aboveIdx == -2 {
			aboveIdx, aboveIndent = -1, -1
			for j := lineNumber - 2; j >= 0; j-- {
				if indent := lv.Level(j); indent >= 0 {
					aboveIdx, aboveIndent = j, indent
					break
				}
			}
		}
		if belowIdx != -1 && (belowIdx == -2 || belowIdx < lineNumber-1) {
			belowIdx, belowIndent = -1, -1
			for j := lineNumber; j < lineCount; j++ {
				if indent := lv.Level(j); indent >= 0 {
					belowIdx, belowIndent = j, indent
					break
				}
			}
		}
		result[i] = g.levelForWhitespaceLine(aboveIndent, belowIndent)
	}
	return result
}

// HorizontalGuides says for which bracket pairs the horizontal guides are drawn.
type HorizontalGuides int

// The values of HorizontalGuides.
const (
	// HorizontalNone draws none.
	HorizontalNone HorizontalGuides = iota
	// HorizontalActive draws them for the active pair.
	HorizontalActive
	// HorizontalAll draws them for every pair.
	HorizontalAll
)

// BracketGuideOptions say what bracket pair guides are asked for.
type BracketGuideOptions struct {
	// IncludeInactive includes the pairs other than the active one.
	IncludeInactive bool
	// Horizontal says for which pairs the horizontal guides are included.
	Horizontal HorizontalGuides
	// HighlightActive marks the guides of the active pair.
	HighlightActive bool
	// IndependentColorPool takes the color of a pair from the number of pairs around it with the
	// same opening bracket, instead of from all the pairs around it.
	IndependentColorPool bool
}

// PairSource gives the bracket pairs that touch a range (a *brackets.Index is one).
type PairSource interface {
	Pairs(r buffer.Range) []brackets.PairInfo
}

// visibleColumn is the column a byte offset of a line is drawn at, counted from 1.
func (g Guides) visibleColumn(pos buffer.Position) int {
	line := g.Text.LineContent(pos.Line)
	if g.VisibleColumn != nil {
		return g.VisibleColumn(line, pos.Col, g.Levels.TabSize()) + 1
	}
	tab := g.Levels.TabSize()
	col := 0
	for i := 0; i < len(line) && i < pos.Col; i++ {
		if line[i] == '\t' {
			col = col - col%tab + tab
		} else {
			col++
		}
	}
	return col + 1
}

// BracketGuides gives, for each line from first to last (zero-based, both included), the guides
// of the bracket pairs that span it: a vertical guide at the smaller of the columns of the two
// brackets and the smallest indentation inside the pair, and the horizontal lines that join the
// brackets to it. active is the position of the cursor, whose innermost pair is the active one.
//
// Ported from VS Code: guidesTextModelPart.ts (getLinesBracketGuides)
func (g Guides) BracketGuides(pairs PairSource, first, last int, active *buffer.Position, opts BracketGuideOptions) [][]Guide {
	result := make([][]Guide, last-first+1)
	lastLine := g.Text.LineContent(last)
	inView := pairs.Pairs(buffer.Range{Start: buffer.Position{Line: first}, End: buffer.Position{Line: last, Col: len(lastLine)}})

	var activeRange *buffer.Range
	if active != nil && len(inView) > 0 {
		candidates := inView
		if active.Line < first || active.Line > last {
			// The cursor is not on the screen: ask for the pairs around it.
			candidates = pairs.Pairs(buffer.Range{Start: *active, End: *active})
		}
		for _, p := range candidates {
			if p.Range.StrictContainsPosition(*active) {
				r := p.Range
				activeRange = &r // the innermost one is the last
			}
		}
	}

	for _, pair := range inView {
		if !pair.HasClose {
			continue
		}
		isActive := activeRange != nil && pair.Range == *activeRange
		if !isActive && !opts.IncludeInactive {
			continue
		}
		level := pair.Level
		if opts.IndependentColorPool {
			level = pair.EqualLevel
		}
		base := Guide{Bracket: true, Level: level, Active: opts.HighlightActive && isActive, ForWrappedAfter: -1, ForWrappedBeforeOrAt: -1}
		start, end := pair.Open.Start, pair.Close.Start
		horizontal := opts.Horizontal == HorizontalAll || (opts.Horizontal == HorizontalActive && isActive)

		if pair.Range.Start.Line == pair.Range.End.Line {
			if horizontal {
				h := base
				h.VisibleColumn, h.Column = -1, pair.Open.End.Col
				h.Horizontal = &HorizontalLine{Top: false, EndColumn: end.Col}
				result[pair.Range.Start.Line-first] = append(result[pair.Range.Start.Line-first], h)
			}
			continue
		}

		startVisible, endVisible := g.visibleColumn(start), g.visibleColumn(end)
		guideColumn := min(startVisible, endVisible)
		if inside := g.Levels.MinLevel(start.Line+1, pair.Range.End.Line); inside != math.MaxInt {
			guideColumn = min(guideColumn, inside+1)
		}

		// With text before the closing bracket the guide goes on for one more line, and ends with
		// a horizontal line at the bottom of the last.
		bottom := firstNonWhitespace(g.Text.LineContent(end.Line)) < end.Col
		offset := 0
		if bottom {
			offset = 1
		}
		for l := max(start.Line, first); l < min(end.Line, last)+offset; l++ {
			v := base
			v.VisibleColumn, v.Column = guideColumn, -1
			if l == start.Line {
				v.ForWrappedAfter = start.Col
			}
			if l == end.Line {
				v.ForWrappedBeforeOrAt = end.Col
			}
			result[l-first] = append(result[l-first], v)
		}
		if horizontal {
			if start.Line >= first && startVisible > guideColumn {
				h := base
				h.VisibleColumn, h.Column = guideColumn, -1
				h.Horizontal = &HorizontalLine{Top: false, EndColumn: start.Col}
				result[start.Line-first] = append(result[start.Line-first], h)
			}
			if end.Line <= last && endVisible > guideColumn {
				h := base
				h.VisibleColumn, h.Column = guideColumn, -1
				h.Horizontal = &HorizontalLine{Top: !bottom, EndColumn: end.Col}
				result[end.Line-first] = append(result[end.Line-first], h)
			}
		}
	}
	for _, guides := range result {
		sort.SliceStable(guides, func(i, j int) bool { return guides[i].VisibleColumn < guides[j].VisibleColumn })
	}
	return result
}

// firstNonWhitespace is the byte offset of the first character of a line that is not a space or a
// tab, or -1.
func firstNonWhitespace(line string) int {
	for i := 0; i < len(line); i++ {
		if line[i] != ' ' && line[i] != '\t' {
			return i
		}
	}
	return -1
}

// Highlight says whether the active indent guide is highlighted.
type Highlight int

// The values of Highlight.
const (
	// HighlightOff does not highlight it.
	HighlightOff Highlight = iota
	// HighlightOn highlights it, unless the line has bracket pair guides.
	HighlightOn
	// HighlightAlways highlights it even where there are bracket pair guides.
	HighlightAlways
)

// BracketPairs says which bracket pairs get guides.
type BracketPairs int

// The values of BracketPairs.
const (
	// PairsOff draws no bracket pair guides.
	PairsOff BracketPairs = iota
	// PairsActive draws them for the active pair only.
	PairsActive
	// PairsAll draws them for every pair.
	PairsAll
)

// GuideOptions are the options editor.guides.*.
type GuideOptions struct {
	// Indentation draws the indent guides.
	Indentation bool
	// HighlightActiveIndentation highlights the guide of the scope the cursor is in.
	HighlightActiveIndentation Highlight
	// BracketPairs says which pairs get guides.
	BracketPairs BracketPairs
	// BracketPairsHorizontal says for which pairs the horizontal guides are drawn.
	BracketPairsHorizontal HorizontalGuides
	// HighlightActiveBracketPair highlights the guides of the pair around the cursor.
	HighlightActiveBracketPair bool
	// IndependentColorPool colors bracket pair guides by the count of pairs of the same bracket
	// (editor.bracketPairColorization.independentColorPoolPerBracketType).
	IndependentColorPool bool
}

// DefaultGuideOptions are VS Code's defaults: indent guides, the active one highlighted, bracket
// pair guides off with horizontal ones for the active pair only.
func DefaultGuideOptions() GuideOptions {
	return GuideOptions{
		Indentation: true, HighlightActiveIndentation: HighlightOn, BracketPairs: PairsOff,
		BracketPairsHorizontal: HorizontalActive, HighlightActiveBracketPair: true,
	}
}

// Off reports whether no guides are drawn at all.
func (o GuideOptions) Off() bool { return !o.Indentation && o.BracketPairs == PairsOff }

// LineGuides gives the guides of the lines from first to last (zero-based, both included): the
// indent guides of the levels the line has, the bracket pair guides, and where a bracket pair
// guide stands at the column of an indent guide it takes its place. cursor is the position of the
// primary cursor, if any.
//
// Ported from VS Code: indentGuides.ts (IndentGuidesOverlay.getGuidesByLine)
func (g Guides) LineGuides(pairs PairSource, first, last int, cursor *buffer.Position, opts GuideOptions) [][]Guide {
	var bracketGuides [][]Guide
	if opts.BracketPairs != PairsOff && pairs != nil {
		bracketGuides = g.BracketGuides(pairs, first, last, cursor, BracketGuideOptions{
			IncludeInactive: opts.BracketPairs == PairsAll, Horizontal: opts.BracketPairsHorizontal,
			HighlightActive: opts.HighlightActiveBracketPair, IndependentColorPool: opts.IndependentColorPool,
		})
	}
	var indentGuides []int
	if opts.Indentation {
		indentGuides = g.LinesIndentGuides(first, last)
	}
	var active ActiveGuide
	if opts.HighlightActiveIndentation != HighlightOff && cursor != nil {
		active = g.ActiveIndentGuide(cursor.Line, first, last)
	}
	indentSize := g.indentSize()
	result := make([][]Guide, last-first+1)
	for line := first; line <= last; line++ {
		i := line - first
		var inLine []Guide
		if bracketGuides != nil {
			inLine = bracketGuides[i]
		}
		queue := inLine
		var out []Guide
		levels := 0
		if indentGuides != nil {
			levels = indentGuides[i]
		}
		for level := 1; level <= levels; level++ {
			column := (level-1)*indentSize + 1
			isActive := (opts.HighlightActiveIndentation == HighlightAlways || len(inLine) == 0) &&
				active.Start <= line && line <= active.End && level == active.Indent && opts.HighlightActiveIndentation != HighlightOff
			for len(queue) > 0 && queue[0].VisibleColumn < column {
				out = append(out, queue[0])
				queue = queue[1:]
			}
			if len(queue) == 0 || queue[0].VisibleColumn != column || queue[0].Horizontal != nil {
				out = append(out, Guide{Level: (level - 1) % GuideColors, Active: isActive, VisibleColumn: column, Column: -1, ForWrappedAfter: -1, ForWrappedBeforeOrAt: -1})
			}
		}
		result[i] = append(out, queue...)
	}
	return result
}
