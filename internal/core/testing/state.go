package testing

// State is the state of a test in a run (VS Code's TestResultState); the numbers are VS Code's.
type State int

// The states of a test.
const (
	// StateUnset is a test the run knows but did not touch.
	StateUnset State = iota
	// StateQueued is a test waiting for its turn.
	StateQueued
	// StateRunning is a test that runs now.
	StateRunning
	// StatePassed is a test that passed.
	StatePassed
	// StateFailed is a test that failed (an assertion).
	StateFailed
	// StateSkipped is a test that did not run.
	StateSkipped
	// StateErrored is a test that could not run to its end (a crash, a build error).
	StateErrored

	numStates = int(StateErrored) + 1
)

// String is the name of the state: unset, queued, running, passed, failed, skipped or errored.
func (s State) String() string {
	switch s {
	case StateQueued:
		return "queued"
	case StateRunning:
		return "running"
	case StatePassed:
		return "passed"
	case StateFailed:
		return "failed"
	case StateSkipped:
		return "skipped"
	case StateErrored:
		return "errored"
	}
	return "unset"
}

// Valid reports whether the value is one of the states.
func (s State) Valid() bool { return s >= StateUnset && int(s) < numStates }

// Priority is the order of importance of a state: the highest of the states of the children is the state a parent shows (VS Code's
// statePriority).
func (s State) Priority() int {
	switch s {
	case StateRunning:
		return 6
	case StateErrored:
		return 5
	case StateFailed:
		return 4
	case StateQueued:
		return 3
	case StatePassed:
		return 2
	case StateSkipped:
		return 1
	}
	return 0
}

// IsFailed reports whether the state is failed or errored.
func (s State) IsFailed() bool { return s == StateFailed || s == StateErrored }

// HasResult reports whether the state is one that has a result: passed, failed or errored.
func (s State) HasResult() bool { return s == StatePassed || s == StateFailed || s == StateErrored }

// MaxPriority is the state of the highest priority among the states, and StateUnset for none.
func MaxPriority(states ...State) State {
	max := StateUnset
	for i, s := range states {
		if i == 0 || max.Priority() < s.Priority() {
			max = s
		}
	}
	return max
}

// terminalPriority is the priority of the states a test keeps once it has them: it does not go back to a state that is not terminal or
// to a terminal one of a lower priority (VS Code's terminalStatePriorities). The second result is false for the other states.
func terminalPriority(s State) (int, bool) {
	switch s {
	case StatePassed:
		return 0, true
	case StateSkipped:
		return 1, true
	case StateFailed:
		return 2, true
	case StateErrored:
		return 3, true
	}
	return 0, false
}

// Counts is how many tests are in each state, by state (VS Code's TestStateCount).
type Counts [numStates]int

// MaxState is the state of the highest priority that has tests, and StateUnset for none (VS Code's maxCountPriority).
func (c Counts) MaxState() State {
	best := StateUnset
	for s := StateUnset; int(s) < numStates; s++ {
		if c[s] > 0 && (best == StateUnset || s.Priority() > best.Priority()) {
			best = s
		}
	}
	return best
}

// Total is the number of tests.
func (c Counts) Total() int {
	n := 0
	for _, v := range c {
		n += v
	}
	return n
}

// Group is a kind of run profile; the values are VS Code's TestRunProfileBitset.
type Group int

// The groups of run profiles.
const (
	// GroupRun runs the tests.
	GroupRun Group = 1 << 1
	// GroupDebug runs them under a debugger.
	GroupDebug Group = 1 << 2
	// GroupCoverage runs them and measures the coverage.
	GroupCoverage Group = 1 << 3
)

// Valid reports whether the value is one of the three groups.
func (g Group) Valid() bool { return g == GroupRun || g == GroupDebug || g == GroupCoverage }
