package diff

// Ported from VS Code: src/vs/workbench/contrib/mergeEditor/browser/model/modifiedBaseRange.ts

// StateKind is the kind of a RangeState.
type StateKind int

// The kinds of state of a range of the result.
const (
	// StateBase: the result has the lines of the base (the zero value).
	StateBase StateKind = iota
	// StateInput1: the result has the changes of input 1 (incoming) only.
	StateInput1
	// StateInput2: the result has the changes of input 2 (current) only.
	StateInput2
	// StateBoth: the result has the lines of both inputs, FirstInput first.
	StateBoth
	// StateUnrecognized: the result is none of the above (it was edited by
	// hand). It is never set by the model's own operations.
	StateUnrecognized
)

// String returns the name of the kind.
func (k StateKind) String() string {
	switch k {
	case StateBase:
		return "base"
	case StateInput1:
		return "input1"
	case StateInput2:
		return "input2"
	case StateBoth:
		return "both"
	case StateUnrecognized:
		return "unrecognized"
	}
	return "invalid"
}

// InputState tells how an input takes part in a state (VS Code's InputState).
type InputState int

// The roles of an input in a state.
const (
	// InputExcluded: the result does not have the input's lines.
	InputExcluded InputState = iota
	// InputFirst: the input is the only one included, or comes first in Both.
	InputFirst
	// InputSecond: the input comes second in Both.
	InputSecond
)

// RangeState tells what the result has in the place of a ModifiedBaseRange
// (VS Code's ModifiedBaseRangeState); the zero value is the base. A state is
// a value; compare two with ==.
type RangeState struct {
	// Kind is the kind of state.
	Kind StateKind
	// FirstInput is the input whose lines come first in StateBoth; it is zero
	// in every other kind.
	FirstInput Input
}

// BothState returns the state that has the lines of both inputs, those of
// first before the others.
func BothState(first Input) RangeState {
	return RangeState{Kind: StateBoth, FirstInput: first}
}

// IncludesInput reports whether the result has the lines of the input.
func (s RangeState) IncludesInput(in Input) bool {
	switch s.Kind {
	case StateInput1:
		return in == Input1
	case StateInput2:
		return in == Input2
	case StateBoth:
		return in == Input1 || in == Input2
	}
	return false
}

// InputState returns the role of the input in the state.
func (s RangeState) InputState(in Input) InputState {
	if !s.IncludesInput(in) {
		return InputExcluded
	}
	if s.Kind == StateBoth && in != s.FirstInput {
		return InputSecond
	}
	return InputFirst
}

// Swap returns the state with the inputs exchanged.
func (s RangeState) Swap() RangeState {
	switch s.Kind {
	case StateInput1:
		return RangeState{Kind: StateInput2}
	case StateInput2:
		return RangeState{Kind: StateInput1}
	case StateBoth:
		return BothState(s.FirstInput.other())
	}
	return s
}

// WithInputValue returns the state that includes the input (value true) or not
// (value false) and the other input as before. Including the second input
// into a state of one input gives Both with the input of the state first.
// (VS Code's state of input 1 with input 2 excluded returns the state of input
// 2, which is a slip; here it stays as it is.)
func (s RangeState) WithInputValue(in Input, value bool) RangeState {
	switch s.Kind {
	case StateBase:
		if !value {
			return s
		}
		return inputState(in)
	case StateInput1, StateInput2:
		if s.IncludesInput(in) {
			if value {
				return s
			}
			return RangeState{}
		}
		if !value {
			return s
		}
		return BothState(in.other())
	case StateBoth:
		if value {
			return s
		}
		return inputState(in.other())
	}
	// StateUnrecognized
	if !value {
		return s
	}
	return inputState(in)
}

// Toggle returns the state with the input included if it was not and excluded
// if it was (VS Code's toggle).
func (s RangeState) Toggle(in Input) RangeState {
	return s.WithInputValue(in, !s.IncludesInput(in))
}

// inputState returns the state that has the lines of one input.
func inputState(in Input) RangeState {
	if in == Input1 {
		return RangeState{Kind: StateInput1}
	}
	return RangeState{Kind: StateInput2}
}

// String returns the kind of the state, with the first input for Both.
func (s RangeState) String() string {
	if s.Kind == StateBoth {
		return "both(" + inputState(s.FirstInput).Kind.String() + " first)"
	}
	return s.Kind.String()
}
