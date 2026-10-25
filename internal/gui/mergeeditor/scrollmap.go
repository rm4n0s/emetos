package mergeeditor

import (
	"slices"

	"github.com/rm4n0s/emetos/internal/core/diff"
)

// Ported from VS Code: src/vs/workbench/contrib/mergeEditor/browser/model/mapping.ts (DocumentLineRangeMap) and
// src/vs/workbench/contrib/mergeEditor/browser/view/scrollSynchronizer.ts (_synchronizeScrolling). This file imports no
// part of the toolkit: the maps and the scroll arithmetic are plain data.

// LinePair maps a range of lines of one text (In) to the range of lines of another text (Out) that corresponds to
// it (lines are counted from 1, the end is excluded).
type LinePair struct {
	In, Out diff.LineRange
}

// LineMap maps the lines of one text to the lines of another: the pairs are the ranges where the texts differ, in
// order, and the lines between two pairs correspond one to one (VS Code's DocumentLineRangeMap). In the merge
// editor the pairs come from the regions of the merge: the range of an input and the range of the result that
// stand for the same range of the base.
type LineMap []LinePair

// Project gives the pair a line is in: the pair that holds it, or else the line and the line it is moved to by the
// pairs before it.
func (m LineMap) Project(line int) LinePair {
	i := -1
	for j := range m {
		if m[j].In.StartLine <= line {
			i = j
		}
	}
	one := func(l int) diff.LineRange { return diff.LineRange{StartLine: l, EndLine: l + 1} }
	if i < 0 {
		return LinePair{In: one(line), Out: one(line)}
	}
	last := m[i]
	if line >= last.In.StartLine && line < last.In.EndLine {
		return last
	}
	return LinePair{In: one(line), Out: one(line + last.Out.EndLine - last.In.EndLine)}
}

// Reverse is the map from the other text to this one.
func (m LineMap) Reverse() LineMap {
	out := make(LineMap, len(m))
	for i, p := range m {
		out[i] = LinePair{In: p.Out, Out: p.In}
	}
	return out
}

// InputMap is the map from the lines of an input to the lines of the result: each region pairs the range of the
// input with the range the region has in the result now (VS Code's input1ResultMapping and input2ResultMapping,
// made from the regions that follow the edits of the result instead of from a diff of the result). Edits of the
// result outside the regions are not in the map.
func InputMap(regions []diff.Region, in diff.Input) LineMap {
	m := make(LineMap, 0, len(regions))
	for _, r := range regions {
		m = append(m, LinePair{In: r.Range.InputRange(in), Out: r.Result})
	}
	return m
}

// ScrollFor gives the vertical scroll position the other text has to be scrolled to when this one is scrolled to
// scrollTop and shows the line topLine at its top: the position is in the same place of the range the line is in, as
// a fraction of its height (VS Code's _synchronizeScrolling). srcTop and dstTop give the pixel at which a line begins
// in each text, the line after the last one included. A range with no height in the scrolled text puts the other at
// the start of its range.
func (m LineMap) ScrollFor(topLine int, scrollTop float64, srcTop, dstTop func(line int) float64) float64 {
	p := m.Project(topLine)
	srcStart, srcEnd := srcTop(p.In.StartLine), srcTop(p.In.EndLine)
	dstStart, dstEnd := dstTop(p.Out.StartLine), dstTop(p.Out.EndLine)
	factor := 0.0
	if srcEnd > srcStart {
		factor = min((scrollTop-srcStart)/(srcEnd-srcStart), 1)
	}
	return dstStart + (dstEnd-dstStart)*factor
}

// Sorted tells whether the pairs are in order, as a map from regions has to be for Project to find the right pair;
// the tests use it.
func (m LineMap) Sorted() bool {
	return slices.IsSortedFunc(m, func(a, b LinePair) int { return a.In.StartLine - b.In.StartLine })
}
