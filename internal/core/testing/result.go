package testing

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"github.com/rm4n0s/errors"
	"slices"
	"strconv"
	"time"
)

// NoDuration is the duration of a test that has none (VS Code's undefined).
const NoDuration time.Duration = -1

// newID makes the id of a result or a task: random, with the counter so that two made in a row differ.
func newID(seq int) string {
	var b [8]byte
	_, _ = rand.Read(b[:])
	return hex.EncodeToString(b[:]) + "-" + strconv.Itoa(seq)
}

// MessageType says what a Message is.
type MessageType int

const (
	// MessageError is the message of a failure.
	MessageError MessageType = iota + 1
	// MessageOutput is a piece of the output of the run that belongs to a test or a place.
	MessageOutput
)

// StackFrame is a line of the stack trace of a failure.
type StackFrame struct {
	Label string
	URI   string
	Pos   *Pos
}

// Message is something a run says about a test: why it failed, or the output that belongs to it (VS Code's ITestMessage and
// ITestOutputMessage).
type Message struct {
	Type MessageType
	// Text is the message, Markdown says it is Markdown; for an output message it is a preview of the first bytes without ANSI.
	Text     string
	Markdown bool
	// Expected and Actual are shown as a diff when HasDiff is true.
	Expected, Actual string
	HasDiff          bool
	// Location is the place the message belongs to, nil for none.
	Location *Location
	// StackTrace are the frames of the failure, the innermost first.
	StackTrace []StackFrame
	// ContextValue is a value of the controller's for the message's menus.
	ContextValue string
	// Offset and Length are the place of an output message in the output of its task (counted from the start of the run's output,
	// also when the oldest bytes were dropped); TestID is the test it belongs to.
	Offset, Length int64
	TestID         string
}

// TaskState is the state of a test in one task of a run.
type TaskState struct {
	State    State
	Duration time.Duration
	Messages []Message
}

// TestResult is a test in a result: the item as it was when the run first knew it, and its states.
type TestResult struct {
	Item Item
	// OwnState is the state the tasks gave the test, the state of the highest priority among them; State is the state the test shows,
	// which also holds the states of its children in the run. Duration and OwnDuration are the same for the durations: the own one is
	// the longest a task gave, the shown one the sum of the children's when the test has none of its own; NoDuration for none.
	OwnState, State       State
	OwnDuration, Duration time.Duration
	// Tasks are the states by task, in the order of the result's tasks.
	Tasks []TaskState
	// Retired says that the controller invalidated the result after the run.
	Retired bool
	// Children are the ids of the children that are in the run.
	Children []string
}

// TaskInfo describes a task of a result: one CreateTestRun of an extension, or the run of a controller.
type TaskInfo struct {
	ID, Name, ControllerID string
	Running                bool
	HasCoverage            bool
}

// node is a test of a result.
type node struct {
	item        Item
	parent      string // the id of the parent when it is in the run
	children    []string
	tasks       []TaskState
	own, shown  State
	ownDur, dur time.Duration
	retired     bool
}

// task is a part of a result.
type task struct {
	id, name, controller string
	running              bool
	output               []byte
	outStart             int64 // the offset of the first byte of output in the run's output
	other                []Message
	coverage             *Coverage
	loader               CoverageLoader
}

// Result is the result of a run (VS Code's LiveTestResult and HydratedTestResult). It is made by the model; its methods are safe for
// concurrent use and return copies.
type Result struct {
	svc          *Service
	id, name     string
	persist      bool
	request      RunRequest
	startedAt    time.Time
	completedAt  time.Time
	insertOrder  int
	hydrated     bool
	autoComplete bool
	cancel       context.CancelFunc
	nodes        map[string]*node
	order        []string
	counts       Counts
	tasks        []*task
	idle         chan struct{} // closed, and replaced, whenever a task ends
}

// ID is the id of the result.
func (r *Result) ID() string { return r.id }

// Name is the name of the result: "Test run at" and the time it started.
func (r *Result) Name() string { return r.name }

// Persist reports whether the result is kept in the saved state.
func (r *Result) Persist() bool { return r.persist }

// Request is the request that made the result.
func (r *Result) Request() RunRequest {
	r.svc.mu.Lock()
	defer r.svc.mu.Unlock()
	return r.request.clone()
}

// StartedAt is the time the run started; for a saved result it is the zero time.
func (r *Result) StartedAt() time.Time { return r.startedAt }

// CompletedAt is the time the run ended, and the zero time while it runs.
func (r *Result) CompletedAt() time.Time {
	r.svc.mu.Lock()
	defer r.svc.mu.Unlock()
	return r.completedAt
}

// IsComplete reports whether the run ended.
func (r *Result) IsComplete() bool { return !r.CompletedAt().IsZero() }

// Hydrated reports whether the result was read from the saved state.
func (r *Result) Hydrated() bool { return r.hydrated }

// Counts are the numbers of tests in each state, by their own states.
func (r *Result) Counts() Counts {
	r.svc.mu.Lock()
	defer r.svc.mu.Unlock()
	return r.counts
}

// Tasks lists the tasks of the result.
func (r *Result) Tasks() []TaskInfo {
	r.svc.mu.Lock()
	defer r.svc.mu.Unlock()
	out := make([]TaskInfo, len(r.tasks))
	for i, t := range r.tasks {
		out[i] = TaskInfo{ID: t.id, Name: t.name, ControllerID: t.controller, Running: t.running, HasCoverage: t.coverage != nil}
	}
	return out
}

// Test returns the test with the id in the run.
func (r *Result) Test(id string) (TestResult, bool) {
	r.svc.mu.Lock()
	defer r.svc.mu.Unlock()
	n := r.nodes[id]
	if n == nil {
		return TestResult{}, false
	}
	return n.export(), true
}

// Tests lists the tests of the run, in the order they were added: parents before their children.
func (r *Result) Tests() []TestResult {
	r.svc.mu.Lock()
	defer r.svc.mu.Unlock()
	out := make([]TestResult, 0, len(r.order))
	for _, id := range r.order {
		out = append(out, r.nodes[id].export())
	}
	return out
}

func (n *node) export() TestResult {
	tr := TestResult{
		Item: n.item, OwnState: n.own, State: n.shown, OwnDuration: n.ownDur, Duration: n.dur, Retired: n.retired,
		Children: slices.Clone(n.children), Tasks: make([]TaskState, len(n.tasks)),
	}
	for i, t := range n.tasks {
		tr.Tasks[i] = TaskState{State: t.State, Duration: t.Duration, Messages: slices.Clone(t.Messages)}
	}
	return tr
}

func (r *Result) taskLocked(taskID string) *task {
	for _, t := range r.tasks {
		if t.id == taskID {
			return t
		}
	}
	return nil
}

// Output is the output of the task that is kept, and the offset of its first byte in the output of the run (it is not zero when the
// oldest bytes were dropped); nil for an unknown task.
func (r *Result) Output(taskID string) ([]byte, int64) {
	r.svc.mu.Lock()
	defer r.svc.mu.Unlock()
	if t := r.taskLocked(taskID); t != nil {
		return slices.Clone(t.output), t.outStart
	}
	return nil, 0
}

// OtherMessages are the output messages of the task that belong to no test.
func (r *Result) OtherMessages(taskID string) []Message {
	r.svc.mu.Lock()
	defer r.svc.mu.Unlock()
	if t := r.taskLocked(taskID); t != nil {
		return slices.Clone(t.other)
	}
	return nil
}

// Coverage is the coverage the task reported, or nil.
func (r *Result) Coverage(taskID string) *Coverage {
	r.svc.mu.Lock()
	defer r.svc.mu.Unlock()
	if t := r.taskLocked(taskID); t != nil {
		return t.coverage
	}
	return nil
}

// LatestCoverage is the coverage of the last task that has any, or nil.
func (r *Result) LatestCoverage() *Coverage {
	r.svc.mu.Lock()
	defer r.svc.mu.Unlock()
	for i := len(r.tasks) - 1; i >= 0; i-- {
		if r.tasks[i].coverage != nil {
			return r.tasks[i].coverage
		}
	}
	return nil
}

// ---- the list of results

func (s *Service) newResultLocked(req RunRequest, persist, autoComplete bool) *Result {
	now := s.now()
	r := &Result{
		svc: s, id: s.newIDLocked(), name: "Test run at " + now.Format("2006-01-02 15:04:05"), persist: persist, request: req.clone(),
		startedAt: now, insertOrder: s.insertSeq, autoComplete: autoComplete, nodes: map[string]*node{}, idle: make(chan struct{}),
	}
	s.insertSeq++
	s.pushLocked(r)
	return r
}

// pushLocked adds a result to the list: a running one at the front, a finished one in the place its time gives it. Results beyond
// MaxResults are dropped from the end.
func (s *Service) pushLocked(r *Result) {
	if r.completedAt.IsZero() {
		s.results = append([]*Result{r}, s.results...)
	} else {
		i := 0
		for i < len(s.results) && (s.results[i].completedAt.IsZero() || s.results[i].completedAt.After(r.completedAt)) {
			i++
		}
		s.results = slices.Insert(s.results, i, r)
	}
	var removed []*Result
	for len(s.results) > MaxResults {
		last := s.results[len(s.results)-1]
		s.results = s.results[:len(s.results)-1]
		removed = append(removed, last)
	}
	if r.hydrated {
		s.emitLocked(Event{Kind: EventResult, Change: ResultInserted, Result: r})
	} else {
		s.emitLocked(Event{Kind: EventResult, Change: ResultStarted, Result: r})
	}
	if len(removed) > 0 {
		s.emitLocked(Event{Kind: EventResult, Change: ResultRemoved, Removals: removed})
	}
	s.refreshRunningLocked()
}

// Results lists the results: those still running first, then the others, the newest first.
func (s *Service) Results() []*Result {
	s.mu.Lock()
	defer s.mu.Unlock()
	return slices.Clone(s.results)
}

// Result returns the result with the id.
func (s *Service) Result(id string) *Result {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.resultLocked(id)
}

func (s *Service) resultLocked(id string) *Result {
	for _, r := range s.results {
		if r.id == id {
			return r
		}
	}
	return nil
}

// HasAnyResults is the context key testing.hasAnyResults.
func (s *Service) HasAnyResults() bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	return len(s.results) > 0
}

// ClearResults discards the results that ended (VS Code's clear). A run that goes on stays.
func (s *Service) ClearResults() {
	s.mu.Lock()
	var keep, removed []*Result
	for _, r := range s.results {
		if r.completedAt.IsZero() {
			keep = append(keep, r)
		} else {
			removed = append(removed, r)
		}
	}
	s.results = keep
	if len(removed) > 0 {
		s.emitLocked(Event{Kind: EventResult, Change: ResultRemoved, Removals: removed})
	}
	s.unlock()
}

// StateOf returns the state of the test in the newest result that gave it one, with that result (VS Code's getStateById).
func (s *Service) StateOf(id string) (*Result, TestResult, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	for _, r := range s.results {
		if n := r.nodes[id]; n != nil && n.shown != StateUnset {
			return r, n.export(), true
		}
	}
	return nil, TestResult{}, false
}

// InvalidateResults marks the tests of the results as retired: the tests with the ids and their descendants, or every test for none.
func (s *Service) InvalidateResults(ids ...string) {
	s.mu.Lock()
	for _, r := range s.results {
		for _, id := range r.order {
			n := r.nodes[id]
			if n.retired {
				continue
			}
			hit := len(ids) == 0
			for _, x := range ids {
				if x == id || IsAncestorID(x, id) {
					hit = true
					break
				}
			}
			if hit {
				n.retired = true
				s.emitLocked(Event{Kind: EventTest, Test: &ItemChange{ResultID: r.id, TestID: id, Reason: ChangeComputedState}})
			}
		}
	}
	s.unlock()
}

// ---- adding to a result

// TaskSpec describes a task.
type TaskSpec struct {
	// Name is the name of the task, ControllerID the controller that runs it.
	Name, ControllerID string
	// LoadCoverage loads the details of the coverage the task reports.
	LoadCoverage CoverageLoader
}

// Run is a task of a result that a controller reports to (VS Code's TestRun, on the model's side). Its methods do nothing once the task
// ended or the result was completed.
type Run struct {
	res    *Result
	taskID string
}

// AddTask starts a task in the result and returns what to report to. It fails with TestingResultComplete when the result ended.
func (r *Result) AddTask(spec TaskSpec) (*Run, error) {
	s := r.svc
	s.mu.Lock()
	if !r.completedAt.IsZero() || r.hydrated {
		s.mu.Unlock()
		return nil, errors.New("TestingResultComplete", "the result ended; a task cannot be added to it", "result", r.id)
	}
	t := &task{id: s.newIDLocked(), name: spec.Name, controller: spec.ControllerID, running: true, loader: spec.LoadCoverage}
	r.tasks = append(r.tasks, t)
	for _, n := range r.nodes {
		n.tasks = append(n.tasks, TaskState{Duration: NoDuration})
	}
	s.unlock()
	return &Run{res: r, taskID: t.id}, nil
}

// ResultID and TaskID identify the task.
func (u *Run) ResultID() string { return u.res.id }

// TaskID is the id of the task in its result.
func (u *Run) TaskID() string { return u.taskID }

// Ended reports whether the task ended.
func (u *Run) Ended() bool {
	s := u.res.svc
	s.mu.Lock()
	defer s.mu.Unlock()
	t := u.res.taskLocked(u.taskID)
	return t == nil || !t.running
}

// live returns the task when it still takes reports.
func (u *Run) liveLocked() *task {
	r := u.res
	if !r.completedAt.IsZero() {
		return nil
	}
	if t := r.taskLocked(u.taskID); t != nil && t.running {
		return t
	}
	return nil
}

// Ensure adds the test and the tests above it to the result, as the tree knows them (VS Code's addTestChainToRun); a test already in
// the result stays as it is. It reports false when the tree does not have the test.
func (u *Run) Ensure(id string) bool {
	s := u.res.svc
	s.mu.Lock()
	defer s.mu.Unlock()
	if u.liveLocked() == nil {
		return false
	}
	return u.res.ensureLocked(id)
}

func (r *Result) ensureLocked(id string) bool {
	if r.nodes[id] != nil {
		return true
	}
	chain := IDsToRoot(id)
	slices.Reverse(chain)
	for _, cid := range chain {
		if cid == ControllerOf(id) && len(SplitID(cid)) == 1 {
			continue
		}
		if r.nodes[cid] != nil {
			continue
		}
		item, ok := r.svc.itemLocked(cid)
		if !ok {
			return false
		}
		n := &node{item: item, own: StateUnset, shown: StateUnset, ownDur: NoDuration, dur: NoDuration, tasks: make([]TaskState, len(r.tasks))}
		for i := range n.tasks {
			n.tasks[i].Duration = NoDuration
		}
		if p := r.nodes[item.ParentID]; p != nil {
			n.parent = item.ParentID
			p.children = append(p.children, cid)
		}
		r.nodes[cid] = n
		r.order = append(r.order, cid)
		r.counts[StateUnset]++
	}
	return r.nodes[id] != nil
}

func (r *Result) taskIndexLocked(taskID string) int {
	for i, t := range r.tasks {
		if t.id == taskID {
			return i
		}
	}
	return -1
}

// SetState gives the test a state in this task, and the duration or NoDuration. A test that has a terminal state keeps it or takes a
// terminal state of a higher priority (VS Code's rule: passed, skipped, failed, errored). The test is added to the result first.
func (u *Run) SetState(id string, state State, d time.Duration) {
	s := u.res.svc
	s.mu.Lock()
	if u.liveLocked() != nil && state.Valid() && u.res.ensureLocked(id) {
		r := u.res
		n := r.nodes[id]
		ti := r.taskIndexLocked(u.taskID)
		oldP, oldTerminal := terminalPriority(n.tasks[ti].State)
		newP, newTerminal := terminalPriority(state)
		if !oldTerminal || (newTerminal && newP >= oldP) {
			r.setStateLocked(n, ti, state, d)
		}
	}
	s.unlock()
}

// setStateLocked is VS Code's fireUpdateAndRefresh: it sets the state of the test in the task, computes the test's own state and
// duration, and refreshes the test and the ones above it.
func (r *Result) setStateLocked(n *node, ti int, state State, d time.Duration) {
	s := r.svc
	prevOwn, prevDur := n.own, n.ownDur
	n.tasks[ti].State = state
	if d >= 0 {
		n.tasks[ti].Duration = d
		n.ownDur = max(max(n.ownDur, 0), d)
	}
	states := make([]State, len(n.tasks))
	for i, t := range n.tasks {
		states[i] = t.State
	}
	newOwn := MaxPriority(states...)
	if newOwn == prevOwn && n.ownDur == prevDur {
		return
	}
	n.own = newOwn
	r.counts[prevOwn]--
	r.counts[newOwn]++
	change := ItemChange{ResultID: r.id, TestID: n.item.ID, Reason: ChangeOwnState, PreviousState: prevOwn, PreviousOwnDuration: prevDur}
	for x := n; x != nil; x = r.nodes[x.parent] {
		ns, nd := r.computeLocked(x)
		if x != n && ns == x.shown && nd == x.dur {
			break
		}
		changed := ns != x.shown || nd != x.dur
		x.shown, x.dur = ns, nd
		switch {
		case x == n:
			c := change
			s.emitLocked(Event{Kind: EventTest, Test: &c})
		case changed:
			s.emitLocked(Event{Kind: EventTest, Test: &ItemChange{ResultID: r.id, TestID: x.item.ID, Reason: ChangeComputedState}})
		}
	}
}

// computeLocked is the state and the duration a test shows (VS Code's getComputedState and getComputedDuration): the highest state of
// its own and of its children's, with a test that has no state of its own showing skipped when its children are; the duration is its
// own or the sum of the children's.
func (r *Result) computeLocked(n *node) (State, time.Duration) {
	state := n.own
	dur := NoDuration
	if n.ownDur >= 0 {
		dur = n.ownDur
	}
	for _, id := range n.children {
		c := r.nodes[id]
		if c == nil {
			continue
		}
		if c.shown == StateSkipped && state == StateUnset {
			state = StateSkipped
		} else {
			state = MaxPriority(state, c.shown)
		}
		if n.ownDur < 0 && c.dur >= 0 {
			dur = max(dur, 0) + c.dur
		}
	}
	return state, dur
}

// AddMessage adds a message about the failure of the test to this task. The test is added to the result first.
func (u *Run) AddMessage(id string, m Message) {
	s := u.res.svc
	s.mu.Lock()
	if u.liveLocked() != nil && u.res.ensureLocked(id) {
		r := u.res
		m.TestID = id
		if m.Type == 0 {
			m.Type = MessageError
		}
		n := r.nodes[id]
		ti := r.taskIndexLocked(u.taskID)
		n.tasks[ti].Messages = append(n.tasks[ti].Messages, m)
		s.emitLocked(Event{Kind: EventTest, Test: &ItemChange{ResultID: r.id, TestID: id, Reason: ChangeNewMessage, Message: &m}})
	}
	s.unlock()
}

// AppendOutput adds to the output of the task. With a location or a test the text is remembered as a message of its own that the views
// can jump to; the test is added to the result when it is in the tree.
func (u *Run) AppendOutput(data []byte, loc *Location, testID string) {
	s := u.res.svc
	s.mu.Lock()
	if t := u.liveLocked(); t != nil && len(data) > 0 {
		r := u.res
		offset := t.outStart + int64(len(t.output))
		t.output = append(t.output, data...)
		if len(t.output) > MaxLiveOutput+MaxLiveOutput/4 {
			drop := len(t.output) - MaxLiveOutput
			t.output = slices.Clone(t.output[drop:])
			t.outStart += int64(drop)
			t.other = slices.DeleteFunc(t.other, func(m Message) bool { return m.Offset < t.outStart })
		}
		if loc != nil || testID != "" {
			m := Message{Type: MessageOutput, Text: previewOutput(data), Location: loc, Offset: offset, Length: int64(len(data)), TestID: testID}
			if testID != "" && r.ensureLocked(testID) {
				n := r.nodes[testID]
				ti := r.taskIndexLocked(u.taskID)
				n.tasks[ti].Messages = append(n.tasks[ti].Messages, m)
				s.emitLocked(Event{Kind: EventTest, Test: &ItemChange{ResultID: r.id, TestID: testID, Reason: ChangeNewMessage, Message: &m}})
			} else {
				m.TestID = ""
				t.other = append(t.other, m)
			}
		}
	}
	s.unlock()
}

// previewOutput is the first 100 bytes of the output as plain text, with an ellipsis when there is more.
func previewOutput(data []byte) string {
	cut := len(data) > 100
	if cut {
		data = data[:100]
	}
	p := stripANSI(string(data))
	if cut {
		p += "…"
	}
	return p
}

// AddCoverage reports the coverage of a file in this task.
func (u *Run) AddCoverage(fc FileCoverage) {
	s := u.res.svc
	s.mu.Lock()
	if t := u.liveLocked(); t != nil {
		if t.coverage == nil {
			t.coverage = newCoverage(t.loader)
		}
		t.coverage.Append(fc)
		s.emitLocked(Event{Kind: EventCoverage, Result: u.res})
	}
	s.unlock()
}

// End ends the task: the tests that are still queued or running in it become skipped. The result completes with its last task when it
// was made by a controller on its own (a run the user asked for completes when its handlers returned).
func (u *Run) End() {
	s := u.res.svc
	s.mu.Lock()
	if u.liveLocked() != nil {
		u.res.endTaskLocked(u.taskID)
		if u.res.autoComplete && !u.res.anyRunningLocked() {
			u.res.completeLocked()
		}
	}
	s.unlock()
}

func (r *Result) anyRunningLocked() bool {
	for _, t := range r.tasks {
		if t.running {
			return true
		}
	}
	return false
}

// endTaskLocked is VS Code's markTaskComplete.
func (r *Result) endTaskLocked(taskID string) {
	ti := r.taskIndexLocked(taskID)
	if ti < 0 || !r.tasks[ti].running {
		return
	}
	r.tasks[ti].running = false
	for _, id := range r.order {
		n := r.nodes[id]
		if st := n.tasks[ti].State; st == StateQueued || st == StateRunning {
			r.setStateLocked(n, ti, StateSkipped, NoDuration)
		}
	}
	close(r.idle)
	r.idle = make(chan struct{})
}

// completeLocked is VS Code's markComplete: it ends the tasks that run and sets the time of the end.
func (r *Result) completeLocked() {
	if !r.completedAt.IsZero() {
		return
	}
	for _, t := range r.tasks {
		if t.running {
			r.endTaskLocked(t.id)
		}
	}
	r.completedAt = r.svc.now()
	s := r.svc
	// running results come first, then by the order they were made, the newest first (VS Code's resort)
	slices.SortStableFunc(s.results, func(a, b *Result) int {
		switch {
		case a.completedAt.IsZero() != b.completedAt.IsZero():
			if a.completedAt.IsZero() {
				return -1
			}
			return 1
		case a.hydrated != b.hydrated:
			if a.hydrated {
				return 1
			}
			return -1
		}
		return b.insertOrder - a.insertOrder
	})
	s.emitLocked(Event{Kind: EventResult, Change: ResultCompleted, Result: r})
	s.refreshRunningLocked()
}

// stripANSI removes the escape sequences of the colors and of the cursor from the text.
func stripANSI(s string) string {
	out := make([]byte, 0, len(s))
	for i := 0; i < len(s); i++ {
		if s[i] != 0x1b {
			out = append(out, s[i])
			continue
		}
		if i+1 < len(s) && s[i+1] == '[' {
			j := i + 2
			for j < len(s) && (s[j] < 0x40 || s[j] > 0x7e) {
				j++
			}
			i = j
			continue
		}
		if i+1 < len(s) && s[i+1] == ']' {
			j := i + 2
			for j < len(s) && s[j] != 0x07 && s[j] != 0x1b {
				j++
			}
			i = j
			continue
		}
	}
	return string(out)
}
