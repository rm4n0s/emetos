package diff

import (
	"context"
	"slices"
	"time"

	"github.com/rm4n0s/errors"
)

// Ported from VS Code: src/vs/workbench/contrib/mergeEditor/browser/model/mergeEditorModel.ts
// Ported from VS Code: src/vs/workbench/contrib/mergeEditor/browser/model/textModelDiffs.ts
// Ported from VS Code: src/vs/workbench/contrib/mergeEditor/browser/model/diffComputer.ts

// Choice says what a conflict is resolved to.
type Choice int

// The choices of Accept.
const (
	// AcceptBase puts the lines of the base.
	AcceptBase Choice = iota + 1
	// AcceptInput1 puts the lines of input 1 (incoming).
	AcceptInput1
	// AcceptInput2 puts the lines of input 2 (current).
	AcceptInput2
	// AcceptBoth puts the lines of input 2 (current) and then those of input
	// 1 (incoming).
	AcceptBoth
)

// state returns the RangeState a choice stands for; ok is false for a value
// that is no choice.
func (c Choice) state() (s RangeState, ok bool) {
	switch c {
	case AcceptBase:
		return RangeState{}, true
	case AcceptInput1:
		return RangeState{Kind: StateInput1}, true
	case AcceptInput2:
		return RangeState{Kind: StateInput2}, true
	case AcceptBoth:
		return BothState(Input2), true
	}
	return RangeState{}, false
}

// Edit replaces the lines [StartLine, EndLine) (1-based, end exclusive, as
// LineRange) of the result by NewLines. An edit that a MergeModel method
// returns has been applied to the model already; the editor applies it to its
// document as one undoable edit.
type Edit struct {
	// StartLine is the first line replaced.
	StartLine int
	// EndLine is the line after the last one replaced.
	EndLine int
	// NewLines are the lines that take their place.
	NewLines []string
}

// MergeOptions are the options of NewMergeModel; the zero value is what
// VS Code's merge editor uses (no limit of time, white space counts).
type MergeOptions struct {
	// MaxComputationTime is the time limit of each of the two diffs (see
	// Options.MaxComputationTime); zero means no limit.
	MaxComputationTime time.Duration
	// Now is the clock of the limit; nil means time.Now.
	Now func() time.Time
}

// Region is a modified base range with its place in the result and its state
// (a snapshot: it does not change when the model does).
type Region struct {
	// Index is the index of the region in Regions.
	Index int
	// ConflictIndex is the index of the region in Conflicts, or -1.
	ConflictIndex int
	// Range holds the base range, the ranges of the inputs and the changes.
	Range ModifiedBaseRange
	// Result is where the region is in the result now (1-based, end
	// exclusive; it follows the edits of the result).
	Result LineRange
	// State tells what the result has there; StateUnrecognized when the
	// region was edited by hand and is none of the other states.
	State RangeState
	// Handled reports that the region is dealt with: it was applied by the
	// model (a change of one input) or accepted by the user; or the user said
	// so (SetHandled). A manual edit does not change it.
	Handled bool
	// Conflict reports that the user has to resolve the region: both inputs
	// changed it differently (or it was a conflict in the marker file).
	Conflict bool
}

// region is the model's data of a Region.
type region struct {
	r              ModifiedBaseRange
	start, end     int // in the result
	state          RangeState
	handled        bool
	conflict       bool
	conflictNumber int
}

// MergeModel is a three-way merge: the base and the two inputs, the ranges
// where the inputs differ from the base, and the result, which starts as the
// base with the changes of one input only (or the same change of both)
// applied. The ranges where both inputs changed the base differently are the
// conflicts; each holds the base lines and is unhandled until it is accepted.
// The model tracks the regions through edits of the result.
//
// All lines are without line breaks (see MergeInputs); the result is written
// with the EOL of the inputs. A MergeModel is not safe for concurrent use.
type MergeModel struct {
	inputs  MergeInputs
	eol     string
	regions []region
	result  []string
	// hitTimeout is set when a diff hit its time limit.
	hitTimeout bool
}

// NewMergeModel diffs the base against each input and makes the model: the
// ranges (ModifiedBaseRangesFromDiffs) and the result, which is the base with
// the following applied (VS Code's reset and computeAutoMergedResult): a range
// that only input 2 changed takes the lines of input 2, a range that only
// input 1 changed the lines of input 1, and a range both changed in the same
// way the lines of input 1. These regions are handled. A range both changed
// differently keeps the base lines, in the state Base and unhandled; so does
// every range that touches one of inputs.MarkedConflicts. A text with no line
// counts as one empty line. The inputs are copied.
//
// The error from the diffs (DiffCancelled when ctx is done) is returned as it
// is; MergeEOLInvalid reports an EOL that is neither "\n" nor "\r\n".
func NewMergeModel(ctx context.Context, inputs MergeInputs, opts MergeOptions) (*MergeModel, error) {
	if inputs.EOL == "" {
		inputs.EOL = "\n"
	}
	if inputs.EOL != "\n" && inputs.EOL != "\r\n" {
		return nil, errors.New("MergeEOLInvalid", "the line break of a merge must be \"\\n\" or \"\\r\\n\"", "eol", inputs.EOL)
	}
	inputs.Base = normalizeText(inputs.Base)
	inputs.Input1 = normalizeText(inputs.Input1)
	inputs.Input2 = normalizeText(inputs.Input2)
	inputs.MarkedConflicts = slices.Clone(inputs.MarkedConflicts)

	dopts := Options{MaxComputationTime: opts.MaxComputationTime, Now: opts.Now}
	d1, err := Compute(ctx, inputs.Base, inputs.Input1, dopts)
	if err != nil {
		return nil, err
	}
	d2, err := Compute(ctx, inputs.Base, inputs.Input2, dopts)
	if err != nil {
		return nil, err
	}
	ranges, err := ModifiedBaseRangesFromDiffs(d1.Changes, d2.Changes, inputs.Base, inputs.Input1, inputs.Input2)
	if err != nil {
		return nil, err
	}

	m := &MergeModel{inputs: inputs, eol: inputs.EOL, hitTimeout: d1.HitTimeout || d2.HitTimeout}
	m.buildInitialResult(ranges)
	return m, nil
}

// normalizeText copies a text; no line becomes one empty line.
func normalizeText(text []string) []string {
	if len(text) == 0 {
		return []string{""}
	}
	return slices.Clone(text)
}

// buildInitialResult makes the result and the regions (VS Code's
// computeAutoMergedResult and the initial states of reset).
func (m *MergeModel) buildInitialResult(ranges []ModifiedBaseRange) {
	base := m.inputs.Base
	next := 1 // the first line of the base that is not in the result yet
	conflicts := 0
	for _, r := range ranges {
		m.result = append(m.result, lines(base, next, r.Base.StartLine)...)
		next = r.Base.EndLine

		reg := region{r: r, start: len(m.result) + 1}
		forced := slices.ContainsFunc(m.inputs.MarkedConflicts, func(c LineRange) bool { return c.intersectsOrTouches(r.Base) })
		switch {
		case forced:
			reg.conflict = true
		case len(r.Input1Diffs) == 0:
			reg.state, reg.handled = RangeState{Kind: StateInput2}, true
		case len(r.Input2Diffs) == 0:
			reg.state, reg.handled = RangeState{Kind: StateInput1}, true
		case r.IsEqualChange:
			reg.state, reg.handled = RangeState{Kind: StateInput1}, true
		default:
			reg.conflict = true
		}
		switch {
		case reg.conflict:
			m.result = append(m.result, r.BaseLines()...)
			reg.conflictNumber = conflicts
			conflicts++
		default:
			text, _ := r.TextForState(reg.state)
			m.result = append(m.result, text...)
		}
		reg.end = len(m.result) + 1
		m.regions = append(m.regions, reg)
	}
	m.result = append(m.result, lines(base, next, len(base)+1)...)
}

// Inputs returns the (normalized) inputs of the model; the slices are shared
// with the model and must not be changed.
func (m *MergeModel) Inputs() MergeInputs { return m.inputs }

// EOL returns the line break the result is written with.
func (m *MergeModel) EOL() string { return m.eol }

// HitTimeout reports that a diff hit its time limit, so the ranges are an
// approximation.
func (m *MergeModel) HitTimeout() bool { return m.hitTimeout }

// snapshot returns the Region of the region at index i.
func (m *MergeModel) snapshot(i int) Region {
	g := m.regions[i]
	conflictIndex := -1
	if g.conflict {
		conflictIndex = g.conflictNumber
	}
	return Region{
		Index: i, ConflictIndex: conflictIndex, Range: g.r,
		Result: LineRange{StartLine: g.start, EndLine: g.end},
		State:  g.state, Handled: g.handled, Conflict: g.conflict,
	}
}

// Regions returns every modified base range, conflicts or not, in order. Those
// that are not conflicts were applied to the result when the model was made.
func (m *MergeModel) Regions() []Region {
	out := make([]Region, len(m.regions))
	for i := range m.regions {
		out[i] = m.snapshot(i)
	}
	return out
}

// Conflicts returns the regions the user has to resolve, in order; the index
// in the slice is the conflict index the other methods take.
func (m *MergeModel) Conflicts() []Region {
	var out []Region
	for i, g := range m.regions {
		if g.conflict {
			out = append(out, m.snapshot(i))
		}
	}
	return out
}

// ConflictCount returns the number of conflicts.
func (m *MergeModel) ConflictCount() int {
	n := 0
	for _, g := range m.regions {
		if g.conflict {
			n++
		}
	}
	return n
}

// UnhandledCount returns the number of conflicts that are not handled.
func (m *MergeModel) UnhandledCount() int {
	n := 0
	for _, g := range m.regions {
		if g.conflict && !g.handled {
			n++
		}
	}
	return n
}

// HandledCount returns the number of conflicts that are handled.
func (m *MergeModel) HandledCount() int { return m.ConflictCount() - m.UnhandledCount() }

// Result returns the lines of the result (a copy).
func (m *MergeModel) Result() []string { return slices.Clone(m.result) }

// ResultLineCount returns the number of lines of the result (without copying them, as Result does).
func (m *MergeModel) ResultLineCount() int { return len(m.result) }

// ResultText returns the result as a text with the model's line breaks: what
// a completed merge writes. The model does not forbid it while conflicts are
// unhandled (UnhandledCount); asking the user is for the caller.
func (m *MergeModel) ResultText() string { return JoinLines(m.result, m.eol) }

// RegionAtResultLine returns the region whose result range holds the line
// (1-based); ok is false when no region does. An empty region holds no line.
func (m *MergeModel) RegionAtResultLine(line int) (r Region, ok bool) {
	for i, g := range m.regions {
		if g.start <= line && line < g.end {
			return m.snapshot(i), true
		}
	}
	return Region{}, false
}

// conflictRegion returns the index in m.regions of the conflict, or the error
// MergeConflictIndexInvalid.
func (m *MergeModel) conflictRegion(conflict int) (int, error) {
	n := 0
	for i, g := range m.regions {
		if !g.conflict {
			continue
		}
		if n == conflict {
			return i, nil
		}
		n++
	}
	return 0, errors.New("MergeConflictIndexInvalid", "there is no such conflict", "conflict", conflict, "conflicts", n)
}

// Accept replaces the result of the conflict by what the choice says, marks
// the conflict handled and returns the edit made (see Edit). AcceptBoth puts
// the lines of input 2 (current) and then those of input 1 (incoming). A
// choice that includes only an input that did not change the range gives the
// base lines. Errors: MergeConflictIndexInvalid, MergeChoiceInvalid.
func (m *MergeModel) Accept(conflict int, choice Choice) (Edit, error) {
	s, ok := choice.state()
	if !ok {
		return Edit{}, errors.New("MergeChoiceInvalid", "the choice is none of base, input 1, input 2 and both", "choice", int(choice))
	}
	return m.SetState(conflict, s)
}

// AcceptAll accepts the choice for every conflict that is not handled and
// returns the edits, from the last conflict to the first: each edit is valid
// both when the edits are applied in this order and relative to the result
// before the call. Error: MergeChoiceInvalid.
func (m *MergeModel) AcceptAll(choice Choice) ([]Edit, error) {
	s, ok := choice.state()
	if !ok {
		return nil, errors.New("MergeChoiceInvalid", "the choice is none of base, input 1, input 2 and both", "choice", int(choice))
	}
	var edits []Edit
	for i := len(m.regions) - 1; i >= 0; i-- {
		g := m.regions[i]
		if !g.conflict || g.handled {
			continue
		}
		edits = append(edits, m.setRegionState(i, s))
	}
	return edits, nil
}

// SetState puts the result of the conflict in the state (the general form of
// Accept; with Toggle it serves the checkboxes of the merge editor), marks the
// conflict handled and returns the edit made. StateUnrecognized is not a state
// to put (error MergeStateInvalid, as for a Both without a first input);
// MergeConflictIndexInvalid for a conflict that does not exist.
func (m *MergeModel) SetState(conflict int, s RangeState) (Edit, error) {
	i, err := m.conflictRegion(conflict)
	if err != nil {
		return Edit{}, err
	}
	if s.Kind < StateBase || s.Kind > StateBoth || (s.Kind == StateBoth && s.FirstInput != Input1 && s.FirstInput != Input2) ||
		(s.Kind != StateBoth && s.FirstInput != 0) {
		return Edit{}, errors.New("MergeStateInvalid", "the state cannot be put in the result", "kind", s.Kind.String(), "first", int(s.FirstInput))
	}
	return m.setRegionState(i, s), nil
}

// Toggle includes the input in the conflict's result if it is not in, and
// excludes it if it is (RangeState.Toggle), as SetState does.
func (m *MergeModel) Toggle(conflict int, in Input) (Edit, error) {
	i, err := m.conflictRegion(conflict)
	if err != nil {
		return Edit{}, err
	}
	if in != Input1 && in != Input2 {
		return Edit{}, errors.New("MergeChoiceInvalid", "the input is neither input 1 nor input 2", "input", int(in))
	}
	return m.setRegionState(i, m.regions[i].state.Toggle(in)), nil
}

// Reset puts the base lines in the result of the conflict again, in the state
// Base and unhandled, and returns the edit made. Error:
// MergeConflictIndexInvalid.
func (m *MergeModel) Reset(conflict int) (Edit, error) {
	i, err := m.conflictRegion(conflict)
	if err != nil {
		return Edit{}, err
	}
	e := m.setRegionState(i, RangeState{})
	m.regions[i].handled = false
	return e, nil
}

// SetHandled marks the conflict handled or not without touching the result
// (VS Code's "mark as handled"). Error: MergeConflictIndexInvalid.
func (m *MergeModel) SetHandled(conflict int, handled bool) error {
	i, err := m.conflictRegion(conflict)
	if err != nil {
		return err
	}
	m.regions[i].handled = handled
	return nil
}

// setRegionState replaces the result of the region by the text of the state
// and marks it handled.
func (m *MergeModel) setRegionState(i int, s RangeState) Edit {
	g := &m.regions[i]
	text, effective := g.r.TextForState(s)
	e := Edit{StartLine: g.start, EndLine: g.end, NewLines: text}
	m.spliceResult(e.StartLine, e.EndLine, text)
	// The region keeps its place and takes all of the new lines; the ones
	// after it move by the difference. Regions never overlap, so the edit
	// touches only this one.
	delta := len(text) - (e.EndLine - e.StartLine)
	g.end += delta
	for j := i + 1; j < len(m.regions); j++ {
		m.regions[j].start += delta
		m.regions[j].end += delta
	}
	g.state = effective
	g.handled = true
	return e
}

// spliceResult replaces the result lines [start, end) by text.
func (m *MergeModel) spliceResult(start, end int, text []string) {
	m.result = slices.Replace(m.result, start-1, end-1, text...)
}

// ApplyResultEdit tells the model that the lines [startLine, endLine) of the
// result were replaced by newLines (a change made by the user to the result);
// the model makes the same change to its result. The regions follow the edit:
// those before it stay, those after it move, and those it overlaps (it
// replaces a line of them, or inserts inside them, or into an empty one) take
// the new lines (the first one overlapped) and are marked edited: their state
// becomes the one their lines are equal to (StateBase, StateInput1,
// StateInput2 or StateBoth with input 2 first, as VS Code's computeState does),
// or StateUnrecognized. The handled flag stays as it was. An edit at the very
// start or end of a region that does not replace a line of it is outside it.
// An edit that replaces what it replaces with the same lines changes nothing.
// Error: MergeEditInvalid for lines that are not in the result.
func (m *MergeModel) ApplyResultEdit(startLine, endLine int, newLines []string) error {
	if startLine < 1 || startLine > endLine || endLine > len(m.result)+1 {
		return errors.New("MergeEditInvalid", "the replaced lines are not in the result",
			"start", startLine, "end", endLine, "lines", len(m.result))
	}
	if slices.Equal(m.result[startLine-1:endLine-1], newLines) {
		return nil
	}
	m.spliceResult(startLine, endLine, newLines)

	s, e, n := startLine, endLine, len(newLines)
	delta := n - (e - s)
	first := true
	for i := range m.regions {
		g := &m.regions[i]
		overlapped := (g.start < e && g.end > s) || (s == e && g.start == g.end && g.start == s)
		switch {
		case overlapped:
			if first {
				g.start = min(g.start, s)
			} else {
				g.start = s + n
			}
			first = false
			if g.end > e {
				g.end += delta
			} else {
				g.end = s + n
			}
			g.state = m.recognize(g)
		case g.end <= s:
			// before the edit
		default: // g.start >= e: after it
			g.start += delta
			g.end += delta
		}
	}
	return nil
}

// recognize returns the state a region's lines are equal to, or
// StateUnrecognized (VS Code's computeState).
func (m *MergeModel) recognize(g *region) RangeState {
	have := m.result[g.start-1 : g.end-1]
	for _, s := range []RangeState{{}, {Kind: StateInput1}, {Kind: StateInput2}, BothState(Input2), BothState(Input1)} {
		text, effective := g.r.TextForState(s)
		if slices.Equal(text, have) {
			return effective
		}
	}
	return RangeState{Kind: StateUnrecognized}
}
