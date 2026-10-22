package edit

import (
	"sort"

	"github.com/rm4n0s/emetos/internal/core/buffer"
)

// SelectionRangeProvider says, for each position, the ranges around it that
// Expand Selection steps through, from the smallest to the largest (the bracket and
// string providers of the language steps implement it).
type SelectionRangeProvider interface {
	// SelectionRanges returns, for each position, ranges that contain it.
	SelectionRanges(m Model, positions []buffer.Position) [][]buffer.Range
}

// SelectionRangesOptions are editor.smartSelect.
type SelectionRangesOptions struct {
	// SelectLeadingAndTrailingWhitespace adds the line without its indentation and
	// trailing white space as a step before the whole line.
	SelectLeadingAndTrailingWhitespace bool
	// SelectSubwords adds the part of a camelCase, snake_case or kebab-case word
	// around the position as a step before the word.
	SelectSubwords bool
}

// DefaultSelectionRangesOptions are VS Code's defaults: both on.
func DefaultSelectionRangesOptions() SelectionRangesOptions {
	return SelectionRangesOptions{SelectLeadingAndTrailingWhitespace: true, SelectSubwords: true}
}

// wordSelectionRanges is the provider that is always there: the part of the word
// (optionally), the word, a line of white space only, and the whole text.
type wordSelectionRanges struct {
	opts     Options
	subwords bool
}

// NewWordSelectionRanges makes the word based provider.
//
// Ported from VS Code: src/vs/editor/contrib/smartSelect/browser/wordSelections.ts
func NewWordSelectionRanges(opts Options, selectSubwords bool) SelectionRangeProvider {
	return wordSelectionRanges{opts: opts, subwords: selectSubwords}
}

func (p wordSelectionRanges) SelectionRanges(m Model, positions []buffer.Position) [][]buffer.Range {
	out := make([][]buffer.Range, len(positions))
	for i, raw := range positions {
		pos := m.ValidatePosition(raw)
		var bucket []buffer.Range
		line := m.LineContent(pos.Line)
		start, end, hasWord := p.opts.wordAtPosition(line, pos.Col)
		if hasWord && p.subwords {
			if r, ok := inWordRange(line[start:end], start, pos); ok {
				bucket = append(bucket, r)
			}
		}
		if hasWord {
			bucket = append(bucket, rangeOf(pos.Line, start, pos.Line, end))
		}
		if len(line) > 0 && firstNonWhitespace(line) < 0 {
			bucket = append(bucket, rangeOf(pos.Line, 0, pos.Line, len(line)))
		}
		last := lastLine(m)
		bucket = append(bucket, rangeOf(0, 0, last, len(m.LineContent(last))))
		out[i] = bucket
	}
	return out
}

// inWordRange is the part of a camelCase, snake_case or kebab-case word that the
// position is in. wordStart is the column the word starts at.
func inWordRange(word string, wordStart int, pos buffer.Position) (buffer.Range, bool) {
	ch := func(i int) byte {
		if i < 0 || i >= len(word) {
			return 0
		}
		return word[i]
	}
	offset := pos.Col - wordStart
	start, end := offset, offset
	var last byte
	// The left anchor.
	for ; start >= 0; start-- {
		c := ch(start)
		if start != offset && (c == '_' || c == '-') {
			break
		}
		if isLowerASCII(c) && isUpperASCII(last) {
			break
		}
		last = c
	}
	start++
	// The right anchor.
	for ; end < len(word); end++ {
		c := ch(end)
		if isUpperASCII(c) && isLowerASCII(last) {
			break
		}
		if c == '_' || c == '-' {
			break
		}
		last = c
	}
	if start < end {
		return rangeOf(pos.Line, wordStart+start, pos.Line, wordStart+end), true
	}
	return buffer.Range{}, false
}

// compareRangesInnerFirst orders ranges from the one that starts last (and ends first).
func compareRangesInnerFirst(a, b buffer.Range) int {
	switch {
	case a.Start.IsBefore(b.Start):
		return 1
	case b.Start.IsBefore(a.Start):
		return -1
	case a.End.IsBefore(b.End):
		return -1
	case b.End.IsBefore(a.End):
		return 1
	}
	return 0
}

// ProvideSelectionRanges collects the ranges of the providers around each position,
// and the ones of the word provider, which is always used. For each position it
// returns the ranges from the smallest to the largest, each containing the one
// before and different from it; with SelectLeadingAndTrailingWhitespace, the lines
// of a range that wraps onto other lines are added without and with their white
// space.
//
// Ported from VS Code: src/vs/editor/contrib/smartSelect/browser/smartSelect.ts
// (provideSelectionRanges).
func ProvideSelectionRanges(m Model, opts Options, positions []buffer.Position, ropts SelectionRangesOptions, providers ...SelectionRangeProvider) [][]buffer.Range {
	valid := make([]buffer.Position, len(positions))
	for i, p := range positions {
		valid[i] = m.ValidatePosition(p)
	}
	all := append(append([]SelectionRangeProvider(nil), providers...), NewWordSelectionRanges(opts, ropts.SelectSubwords))
	raw := make([][]buffer.Range, len(valid))
	for _, p := range all {
		got := p.SelectionRanges(m, valid)
		if len(got) != len(valid) {
			continue
		}
		for i, ranges := range got {
			for _, r := range ranges {
				if r.ContainsPosition(valid[i]) {
					raw[i] = append(raw[i], r)
				}
			}
		}
	}

	out := make([][]buffer.Range, len(valid))
	for i, ranges := range raw {
		if len(ranges) == 0 {
			continue
		}
		sort.SliceStable(ranges, func(a, b int) bool { return compareRangesInnerFirst(ranges[a], ranges[b]) < 0 })
		var kept []buffer.Range
		for _, r := range ranges {
			if len(kept) == 0 || (r.ContainsRange(kept[len(kept)-1]) && r != kept[len(kept)-1]) {
				kept = append(kept, r)
			}
		}
		if !ropts.SelectLeadingAndTrailingWhitespace {
			out[i] = kept
			continue
		}
		withTrivia := []buffer.Range{kept[0]}
		for k := 1; k < len(kept); k++ {
			prev, cur := kept[k-1], kept[k]
			if cur.Start.Line != prev.Start.Line || cur.End.Line != prev.End.Line {
				first := firstNonWhitespace(m.LineContent(prev.Start.Line))
				if first < 0 {
					first = 0
				}
				lastText := m.LineContent(prev.End.Line)
				trailing := lastNonBlank(lastText, len(lastText)-1) + 1
				noBlank := rangeOf(prev.Start.Line, first, prev.End.Line, trailing)
				if noBlank.ContainsRange(prev) && noBlank != prev && cur.ContainsRange(noBlank) && cur != noBlank {
					withTrivia = append(withTrivia, noBlank)
				}
				full := rangeOf(prev.Start.Line, 0, prev.End.Line, len(lastText))
				if full.ContainsRange(prev) && full != noBlank && cur.ContainsRange(full) && cur != full {
					withTrivia = append(withTrivia, full)
				}
			}
			withTrivia = append(withTrivia, cur)
		}
		out[i] = withTrivia
	}
	return out
}

// selectionState is where an Expansion is in the ranges of one cursor.
type selectionState struct {
	index  int
	ranges []buffer.Range
}

// mov goes one range up or down, skipping a range equal to the current one.
func (s selectionState) mov(forward bool) selectionState {
	index := s.index - 1
	if forward {
		index = s.index + 1
	}
	if index < 0 || index >= len(s.ranges) {
		return s
	}
	next := selectionState{index: index, ranges: s.ranges}
	if next.ranges[index] == s.ranges[s.index] {
		return next.mov(forward)
	}
	return next
}

// Expansion is a series of Expand Selection and Shrink Selection (Shift+Alt+Right and
// Left): it remembers the ranges around each cursor, found when it began, so that
// Shrink goes back through the same steps. The editor keeps it until the cursors move
// in any other way.
//
// Ported from VS Code: smartSelect.ts (SmartSelectController).
type Expansion struct {
	states []selectionState
}

// NewExpansion finds the ranges around the active end of each selection. It returns
// nil when there are none.
func (c Commands) NewExpansion(sels []buffer.Selection, ropts SelectionRangesOptions, providers ...SelectionRangeProvider) *Expansion {
	if len(sels) == 0 {
		return nil
	}
	positions := make([]buffer.Position, len(sels))
	valid := make([]buffer.Selection, len(sels))
	for i, s := range sels {
		valid[i] = c.validSelection(s)
		positions[i] = valid[i].Active
	}
	ranges := ProvideSelectionRanges(c.Model, c.Options, positions, ropts, providers...)
	exp := &Expansion{}
	for i, rs := range ranges {
		own := valid[i].Range()
		list := []buffer.Range{own}
		for _, r := range rs {
			if r.ContainsPosition(own.Start) && r.ContainsPosition(own.End) {
				list = append(list, r)
			}
		}
		exp.states = append(exp.states, selectionState{ranges: list})
	}
	return exp
}

func (e *Expansion) selections() []buffer.Selection {
	out := make([]buffer.Selection, len(e.states))
	for i, s := range e.states {
		r := s.ranges[s.index]
		out[i] = buffer.Selection{Anchor: r.Start, Active: r.End}
	}
	return out
}

// Expand goes to the next larger range around each cursor and returns the selections.
func (e *Expansion) Expand() []buffer.Selection {
	for i, s := range e.states {
		e.states[i] = s.mov(true)
	}
	return e.selections()
}

// Shrink goes back to the previous smaller range around each cursor.
func (e *Expansion) Shrink() []buffer.Selection {
	for i, s := range e.states {
		e.states[i] = s.mov(false)
	}
	return e.selections()
}
