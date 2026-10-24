package diff

import (
	"slices"
	"sort"

	"github.com/rm4n0s/errors"
)

// Ported from VS Code: src/vs/workbench/contrib/mergeEditor/browser/model/modifiedBaseRange.ts
// Ported from VS Code: src/vs/workbench/contrib/mergeEditor/browser/model/mapping.ts
// Ported from VS Code: src/vs/workbench/contrib/mergeEditor/browser/model/lineRange.ts
// Ported from VS Code: src/vs/workbench/contrib/mergeEditor/browser/model/editing.ts

// Input names one of the two sides of a merge.
type Input int

// The inputs of a merge.
const (
	// Input1 is the incoming side (the left pane; "theirs").
	Input1 Input = 1
	// Input2 is the current side (the right pane; "ours", HEAD).
	Input2 Input = 2
)

// other returns the other input.
func (in Input) other() Input {
	if in == Input1 {
		return Input2
	}
	return Input1
}

// ModifiedBaseRange describes the modifications of input 1 and input 2 in one
// range of the base: the changes of the inputs that overlap or touch each
// other (a change of one input that shares a line with, or lies right next to,
// a change of the other is in the same range, as in VS Code). It is immutable;
// the slices are shared with the model and must not be changed.
type ModifiedBaseRange struct {
	// Base is the range of the base lines.
	Base LineRange
	// Input1 is the range of input 1 that corresponds to Base: Base with the
	// changes of input 1 applied (equal in length to Base when input 1 did not
	// change it).
	Input1 LineRange
	// Input2 is the same for input 2.
	Input2 LineRange
	// Input1Diffs are the changes from the base to input 1 inside Base, with
	// their character changes; Input2Diffs those to input 2. At least one of
	// the two is not empty.
	Input1Diffs, Input2Diffs []LineRangeMapping
	// IsEqualChange reports that both inputs changed the range in the same
	// way: the same changes of the same lines to the same text.
	IsEqualChange bool

	base, in1, in2 []string
}

// ModifiedBaseRangesFromDiffs aligns the changes from the base to input 1 and
// from the base to input 2 (both sorted, as Compute makes them) into
// ModifiedBaseRange values (VS Code's ModifiedBaseRange.fromDiffs and
// MappingAlignment.compute). The changes are sorted by their first base line
// (a change of input 1 before a change of input 2 on a tie) and are grouped
// as long as the base range of the next one intersects or touches the group
// (touching is: a change that ends at the line where the next begins; two
// insertions at the same place touch). The range of an input is the base
// range moved by the lines the earlier changes of that input added, and
// joined with its changes in the group.
//
// The texts are the lines of the base and of the inputs the changes were
// computed from.
func ModifiedBaseRangesFromDiffs(diffs1, diffs2 []LineRangeMapping, base, in1, in2 []string) ([]ModifiedBaseRange, error) {
	type tagged struct {
		source int
		m      LineRangeMapping
	}
	all := make([]tagged, 0, len(diffs1)+len(diffs2))
	for _, d := range diffs1 {
		all = append(all, tagged{0, d})
	}
	for _, d := range diffs2 {
		all = append(all, tagged{1, d})
	}
	sort.SliceStable(all, func(i, j int) bool { return all[i].m.Original.StartLine < all[j].m.Original.StartLine })

	var (
		ranges  []ModifiedBaseRange
		current [2][]LineRangeMapping
		delta   [2]int // the lines input k has more than the base, after its last change
		group   LineRange
		inGroup bool
	)
	flush := func() error {
		out := [2]LineRange{}
		for k := range out {
			if len(current[k]) == 0 {
				out[k] = LineRange{StartLine: group.StartLine + delta[k], EndLine: group.EndLine + delta[k]}
				continue
			}
			joined := joinMappings(current[k])
			out[k] = extendInputRange(joined, group)
		}
		r, err := newModifiedBaseRange(group, out[0], current[0], out[1], current[1], base, in1, in2)
		if err != nil {
			return err
		}
		ranges = append(ranges, r)
		current = [2][]LineRangeMapping{}
		return nil
	}
	for _, d := range all {
		r := d.m.Original
		if inGroup && !group.intersectsOrTouches(r) {
			if err := flush(); err != nil {
				return nil, err
			}
			inGroup = false
		}
		delta[d.source] = d.m.Modified.EndLine - d.m.Original.EndLine
		if inGroup {
			group = group.join(r)
		} else {
			group, inGroup = r, true
		}
		current[d.source] = append(current[d.source], d.m)
	}
	if inGroup {
		if err := flush(); err != nil {
			return nil, err
		}
	}
	return ranges, nil
}

// newModifiedBaseRange makes a range (VS Code's ModifiedBaseRange
// constructor).
func newModifiedBaseRange(baseRange, in1Range LineRange, diffs1 []LineRangeMapping, in2Range LineRange,
	diffs2 []LineRangeMapping, base, in1, in2 []string) (ModifiedBaseRange, error) {
	if len(diffs1) == 0 && len(diffs2) == 0 {
		return ModifiedBaseRange{}, errors.New("MergeRangeWithoutChange", "a modified base range needs a change of an input",
			"baseStart", baseRange.StartLine, "baseEnd", baseRange.EndLine)
	}
	return ModifiedBaseRange{
		Base: baseRange, Input1: in1Range, Input2: in2Range,
		Input1Diffs: diffs1, Input2Diffs: diffs2,
		IsEqualChange: slices.EqualFunc(diffs1, diffs2, func(a, b LineRangeMapping) bool {
			return a.Original == b.Original && slices.Equal(lines(in1, a.Modified.StartLine, a.Modified.EndLine),
				lines(in2, b.Modified.StartLine, b.Modified.EndLine))
		}),
		base: base, in1: in1, in2: in2,
	}, nil
}

// joinMappings returns the smallest mapping that covers all of ms (VS Code's
// LineRangeMapping.join; its character changes are not kept).
func joinMappings(ms []LineRangeMapping) LineRangeMapping {
	out := LineRangeMapping{Original: ms[0].Original, Modified: ms[0].Modified}
	for _, m := range ms[1:] {
		out.Original = out.Original.join(m.Original)
		out.Modified = out.Modified.join(m.Modified)
	}
	return out
}

// extendInputRange returns the output range of the mapping when its input
// range is extended to ext, which contains it: the lines added on either side
// are equal in input and output (VS Code's LineRangeMapping.extendInputRange).
func extendInputRange(m LineRangeMapping, ext LineRange) LineRange {
	return LineRange{
		StartLine: m.Modified.StartLine + ext.StartLine - m.Original.StartLine,
		EndLine:   m.Modified.EndLine + ext.EndLine - m.Original.EndLine,
	}
}

// IsConflicting reports that both inputs changed the range (VS Code's
// isConflicting; it is also true when they made the same change, see
// IsEqualChange).
func (r ModifiedBaseRange) IsConflicting() bool {
	return len(r.Input1Diffs) > 0 && len(r.Input2Diffs) > 0
}

// InputRange returns the range of the input that corresponds to Base.
func (r ModifiedBaseRange) InputRange(in Input) LineRange {
	if in == Input1 {
		return r.Input1
	}
	return r.Input2
}

// InputDiffs returns the changes from the base to the input in this range.
func (r ModifiedBaseRange) InputDiffs(in Input) []LineRangeMapping {
	if in == Input1 {
		return r.Input1Diffs
	}
	return r.Input2Diffs
}

// InputCombinedDiff returns the smallest change that covers the changes of
// the input in this range; ok is false when the input has none (VS Code's
// getInputCombinedDiff; the character changes are not kept).
func (r ModifiedBaseRange) InputCombinedDiff(in Input) (m LineRangeMapping, ok bool) {
	diffs := r.InputDiffs(in)
	if len(diffs) == 0 {
		return LineRangeMapping{}, false
	}
	return joinMappings(diffs), true
}

// BaseLines returns the lines of the base in this range.
func (r ModifiedBaseRange) BaseLines() []string {
	return lines(r.base, r.Base.StartLine, r.Base.EndLine)
}

// InputLines returns the lines of the input in this range (the lines of
// InputRange).
func (r ModifiedBaseRange) InputLines(in Input) []string {
	rg := r.InputRange(in)
	if in == Input1 {
		return lines(r.in1, rg.StartLine, rg.EndLine)
	}
	return lines(r.in2, rg.StartLine, rg.EndLine)
}

// TextForState returns the lines that replace the range in the result when
// it is in the given state, and the state the lines really are (VS Code's
// getEditForBase, which returns the lines of the edit that applied to the
// base lines gives these): an input without changes in the range adds
// nothing, so a state that includes only such inputs gives the base lines and
// the state Base, and a state that includes one input with changes gives the
// lines of that input and the state of that input alone. The state Both gives
// the lines of the first input and then the lines of the other, whole ranges
// (VS Code's "dumb" combination; the "smart" one, which weaves the character
// changes of both inputs into the base, is not built).
func (r ModifiedBaseRange) TextForState(s RangeState) (text []string, effective RangeState) {
	var with []Input
	for _, in := range []Input{Input1, Input2} {
		if s.IncludesInput(in) && len(r.InputDiffs(in)) > 0 {
			with = append(with, in)
		}
	}
	switch len(with) {
	case 0:
		return slices.Clone(r.BaseLines()), RangeState{}
	case 1:
		return slices.Clone(r.InputLines(with[0])), RangeState{}.WithInputValue(with[0], true)
	}
	first := s.FirstInput
	if first != Input1 && first != Input2 {
		first = Input1
	}
	text = slices.Clone(r.InputLines(first))
	text = append(text, r.InputLines(first.other())...)
	return text, s
}
