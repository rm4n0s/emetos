package debug

import (
	"context"
	"hash/fnv"
	"strconv"
	"sync"

	"github.com/rm4n0s/emetos/internal/core/dap"
	"github.com/rm4n0s/errors"
)

// UnknownSourceLabel is the name of the source of a frame that has none.
const UnknownSourceLabel = "Unknown Source"

// Source is the source of a stack frame, a breakpoint or an output: a file, or something the adapter serves.
//
// Ported from VS Code: src/vs/workbench/contrib/debug/common/debugSource.ts
type Source struct {
	// Raw is the source as the adapter gave it.
	Raw dap.Source
	// Path is the file when the source is one, else empty.
	Path string
	// Name is the name to show.
	Name string
	// Available is false for the source of a frame the adapter gave no source for (and for one known to be unavailable).
	Available bool
	// SessionID is the session the source comes from, which a source of the adapter's needs to be read.
	SessionID string
}

func newSource(raw *dap.Source, sessionID string) Source {
	if raw == nil {
		return Source{Raw: dap.Source{Name: UnknownSourceLabel}, Name: UnknownSourceLabel, SessionID: sessionID}
	}
	name := raw.Name
	if name == "" {
		name = baseName(raw.Path)
	}
	return Source{Raw: *raw, Path: raw.Path, Name: name, Available: true, SessionID: sessionID}
}

// InMemory reports whether the source is not a file (the adapter serves its content with the source request).
func (s Source) InMemory() bool { return s.Path == "" }

// Reference is the sourceReference of a source the adapter serves, or 0.
func (s Source) Reference() int { return s.Raw.SourceReference }

// PresentationHint is the adapter's hint (normal, emphasize, deemphasize).
func (s Source) PresentationHint() string { return s.Raw.PresentationHint }

// Key identifies the source: its path, or debug:<name>?session=<id>&ref=<reference> for one the adapter serves.
func (s Source) Key() string {
	if s.Path != "" {
		return s.Path
	}
	return "debug:" + s.Name + "?session=" + s.SessionID + "&ref=" + strconv.Itoa(s.Raw.SourceReference)
}

// StackFrame is a frame of the call stack of a thread; its scopes are loaded when asked.
//
// Ported from VS Code: src/vs/workbench/contrib/debug/common/debugModel.ts (StackFrame)
type StackFrame struct {
	Thread *Thread
	// ID is the adapter's frame id, valid while the thread is stopped.
	ID     int
	Source Source
	Name   string
	// PresentationHint is normal, label or subtle.
	PresentationHint string
	// Range is the place in the source, counted from 1.
	Range Range
	// Index is the position in the call stack, 0 for the top.
	Index      int
	CanRestart bool
	// InstructionPointerReference is the adapter's reference of the frame's instruction.
	InstructionPointerReference string

	mu     sync.Mutex
	scopes *scopesLoad
}

type scopesLoad struct {
	done   chan struct{}
	scopes []*Scope
}

// Key is the id of the frame that stays the same across stops of the same shape, so that a view keeps its selection.
func (f *StackFrame) Key() string {
	return "stackframe:" + f.Thread.Key() + ":" + strconv.Itoa(f.Index) + ":" + f.Source.Name
}

// Deemphasized reports whether the frame is shown dimmed: a subtle frame, or one in a source the adapter marks deemphasized.
//
// Ported from VS Code: src/vs/workbench/contrib/debug/common/debug.ts (isFrameDeemphasized)
func (f *StackFrame) Deemphasized() bool {
	hint := f.PresentationHint
	if hint == "" {
		hint = f.Source.PresentationHint()
	}
	return hint == "deemphasize" || hint == "subtle"
}

// Equals reports whether two frames are the same frame of the same thread.
func (f *StackFrame) Equals(o *StackFrame) bool {
	return o != nil && f.Name == o.Name && f.Thread == o.Thread && f.ID == o.ID && f.Source.Key() == o.Source.Key() && f.Range == o.Range
}

// Scopes loads the scopes of the frame, once. A failure of the adapter gives one scope whose Err says why.
func (f *StackFrame) Scopes(ctx context.Context) ([]*Scope, error) {
	f.mu.Lock()
	if load := f.scopes; load != nil {
		f.mu.Unlock()
		select {
		case <-load.done:
			return load.scopes, nil
		case <-ctx.Done():
			return nil, errors.NewErr("DebugScopesCancelled", ctx.Err()).SetMetadata("frame", f.ID)
		}
	}
	load := &scopesLoad{done: make(chan struct{})}
	f.scopes = load
	f.mu.Unlock()

	s := f.Thread.sess
	body, err := s.scopes(ctx, f.ID, f.Thread.ID)
	if err != nil {
		load.scopes = []*Scope{f.errorScope(s, err.Error())}
	} else {
		used := map[uint32]bool{}
		for _, rs := range body.Scopes {
			// The id comes from the name and the place, so that it is the same across pauses and a view keeps what is open.
			h := hashScope(rs.Name, rs.Line, rs.Column, 0)
			for used[h] {
				h = hashScope(rs.Name, rs.Line, rs.Column, h)
			}
			used[h] = true
			sc := &Scope{Frame: f, Name: rs.Name, Hint: rs.PresentationHint, Expensive: rs.Expensive}
			sc.id = "scope:" + rs.Name + ":" + strconv.FormatUint(uint64(h), 10)
			sc.threadID = f.Thread.ID
			sc.sess = s
			sc.reference, sc.namedVariables, sc.indexedVariables = rs.VariablesReference, rs.NamedVariables, rs.IndexedVariables
			if rs.Line != 0 && rs.Column != 0 && rs.EndLine != 0 && rs.EndColumn != 0 {
				sc.Range = &Range{rs.Line, rs.Column, rs.EndLine, rs.EndColumn}
			}
			load.scopes = append(load.scopes, sc)
		}
	}
	close(load.done)
	return load.scopes, nil
}

func (f *StackFrame) errorScope(s *Session, msg string) *Scope {
	sc := &Scope{Frame: f, Name: msg, Err: msg}
	sc.id = "scope:" + msg + ":0"
	sc.threadID = f.Thread.ID
	sc.sess = s
	return sc
}

func hashScope(name string, line, column int, seed uint32) uint32 {
	h := fnv.New32a()
	_, _ = h.Write([]byte(strconv.FormatUint(uint64(seed), 10) + "|" + name + ":" + strconv.Itoa(line) + ":" + strconv.Itoa(column)))
	return h.Sum32()
}

// MostSpecificScopes returns the scopes that are not expensive and contain the range, the smallest first; when no scope has a
// range, all the non-expensive scopes (the inline values and the hover use them).
func (f *StackFrame) MostSpecificScopes(ctx context.Context, r Range) ([]*Scope, error) {
	scopes, err := f.Scopes(ctx)
	if err != nil {
		return nil, err
	}
	var cheap []*Scope
	haveRange := false
	for _, s := range scopes {
		if !s.Expensive {
			cheap = append(cheap, s)
			haveRange = haveRange || s.Range != nil
		}
	}
	if !haveRange {
		return cheap, nil
	}
	var containing []*Scope
	for _, s := range cheap {
		if s.Range != nil && s.Range.Contains(r) {
			containing = append(containing, s)
		}
	}
	if len(containing) == 0 {
		return cheap, nil
	}
	size := func(s *Scope) int { return s.Range.EndLine - s.Range.Line }
	for i := 1; i < len(containing); i++ {
		for j := i; j > 0 && size(containing[j]) < size(containing[j-1]); j-- {
			containing[j], containing[j-1] = containing[j-1], containing[j]
		}
	}
	return containing, nil
}

// Restart restarts the frame (restartFrame), when the adapter can.
func (f *StackFrame) Restart(ctx context.Context) error {
	return f.Thread.sess.RestartFrame(ctx, f.ID, f.Thread.ID)
}

// String is "name (path:line)", or the name alone for a frame with no source.
func (f *StackFrame) String() string {
	src := f.Source.Path
	if f.Source.InMemory() {
		src = f.Source.Name
	}
	where := src + ":" + strconv.Itoa(f.Range.Line)
	if where == UnknownSourceLabel+":"+strconv.Itoa(f.Range.Line) || src == UnknownSourceLabel {
		return f.Name
	}
	return f.Name + " (" + where + ")"
}

// StoppedDetails says why a thread stopped: the stopped event, with the number of frames the stack trace reported and the message
// of a failed stack trace.
type StoppedDetails struct {
	dap.StoppedEventBody
	// TotalFrames is the totalFrames of the stack trace, or 0 when the adapter did not say.
	TotalFrames int
	// FramesErrorMessage is the message of the adapter when the stack trace failed.
	FramesErrorMessage string
}

// Thread is a thread of the debuggee. It is stopped or running; a stopped thread has a call stack, loaded from the top as asked.
//
// Ported from VS Code: src/vs/workbench/contrib/debug/common/debugModel.ts (Thread)
type Thread struct {
	sess *Session
	// ID is the adapter's id of the thread.
	ID int

	// Guarded by the lock of the Model.
	name            string
	stopped         bool
	details         *StoppedDetails
	callStack       []*StackFrame
	stale           []*StackFrame
	reachedEnd      bool
	lastGranularity string
	gen             int // changes when the call stack is cleared, so that a load that was in flight is dropped
}

// Key is the id of the thread that views use as a key.
func (t *Thread) Key() string { return "thread:" + t.sess.id + ":" + strconv.Itoa(t.ID) }

// Session is the session of the thread.
func (t *Thread) Session() *Session { return t.sess }

// Name is the name of the thread.
func (t *Thread) Name() string {
	t.sess.m.mu.Lock()
	defer t.sess.m.mu.Unlock()
	return t.name
}

// Stopped tells whether the thread is stopped.
func (t *Thread) Stopped() bool {
	t.sess.m.mu.Lock()
	defer t.sess.m.mu.Unlock()
	return t.stopped
}

// StoppedDetails returns why the thread stopped, or nil while it runs.
func (t *Thread) StoppedDetails() *StoppedDetails {
	t.sess.m.mu.Lock()
	defer t.sess.m.mu.Unlock()
	if t.details == nil {
		return nil
	}
	d := *t.details
	return &d
}

// CallStack returns the frames loaded so far, the top first.
func (t *Thread) CallStack() []*StackFrame {
	t.sess.m.mu.Lock()
	defer t.sess.m.mu.Unlock()
	return append([]*StackFrame(nil), t.callStack...)
}

// StaleCallStack returns the frames the thread had before it last ran, which views compare to find what changed.
func (t *Thread) StaleCallStack() []*StackFrame {
	t.sess.m.mu.Lock()
	defer t.sess.m.mu.Unlock()
	return append([]*StackFrame(nil), t.stale...)
}

// ReachedEndOfCallStack tells whether the whole call stack is loaded.
func (t *Thread) ReachedEndOfCallStack() bool {
	t.sess.m.mu.Lock()
	defer t.sess.m.mu.Unlock()
	return t.reachedEnd
}

// LastSteppingGranularity is the granularity of the last step of the thread.
func (t *Thread) LastSteppingGranularity() string {
	t.sess.m.mu.Lock()
	defer t.sess.m.mu.Unlock()
	return t.lastGranularity
}

// keepSubtleFrameAtTop lists the reasons for which a subtle frame may be the top frame.
var keepSubtleFrameAtTop = map[string]bool{dap.StopBreakpoint: true, dap.StopStep: true, dap.StopFunctionBreakpoint: true}

// TopStackFrame is the first frame of the call stack that has a source and, unless the stop is a breakpoint or a step, is not
// deemphasized: where the thread is shown to be.
func (t *Thread) TopStackFrame() *StackFrame {
	t.sess.m.mu.Lock()
	defer t.sess.m.mu.Unlock()
	return t.topFrameLocked()
}

func (t *Thread) topFrameLocked() *StackFrame {
	reason := ""
	if t.details != nil {
		reason = t.details.Reason
	}
	for _, f := range t.callStack {
		if f.Source.Available && (keepSubtleFrameAtTop[reason] || !f.Deemphasized()) {
			return f
		}
	}
	return nil
}

// StateLabel is "Running", "Paused" or "Paused on <reason>" (or the adapter's description).
func (t *Thread) StateLabel() string {
	t.sess.m.mu.Lock()
	defer t.sess.m.mu.Unlock()
	if t.details != nil {
		if t.details.Description != "" {
			return t.details.Description
		}
		if t.details.Reason != "" {
			return "Paused on " + t.details.Reason
		}
		return "Paused"
	}
	return "Running"
}

// clearCallStackLocked empties the call stack, keeping it as the stale one, and invalidates the loads in flight.
func (t *Thread) clearCallStackLocked() {
	if len(t.callStack) > 0 {
		t.stale = t.callStack
	}
	t.callStack = nil
	t.gen++
}

// FetchCallStack loads the next levels frames of the call stack of a stopped thread. Calling it again loads the next ones.
//
// Ported from VS Code: src/vs/workbench/contrib/debug/common/debugModel.ts (Thread.fetchCallStack)
func (t *Thread) FetchCallStack(ctx context.Context, levels int) error {
	s := t.sess
	s.m.mu.Lock()
	if !t.stopped {
		s.m.mu.Unlock()
		return nil
	}
	start, gen := len(t.callStack), t.gen
	s.m.mu.Unlock()

	frames, total, failure := t.fetchFrames(ctx, start, levels, gen)
	s.m.mu.Lock()
	defer s.m.mu.Unlock()
	if t.gen != gen {
		return nil // the thread ran in the meantime
	}
	if failure != "" && t.details != nil {
		t.details.FramesErrorMessage = failure
	}
	if total > 0 && t.details != nil {
		t.details.TotalFrames = total
	}
	t.reachedEnd = len(frames) < levels
	if start < len(t.callStack) {
		// A request that ran at the same time loaded these; this one's answer replaces them, so no frame is there twice.
		t.callStack = t.callStack[:start]
	}
	t.callStack = append(t.callStack, frames...)
	if t.details != nil && t.details.TotalFrames > 0 && t.details.TotalFrames == len(t.callStack) {
		t.reachedEnd = true
	}
	return nil
}

func (t *Thread) fetchFrames(ctx context.Context, start, levels, gen int) (frames []*StackFrame, total int, failure string) {
	s := t.sess
	body, err := s.stackTrace(ctx, t.ID, start, levels)
	if err != nil {
		return nil, 0, err.Error()
	}
	for i, rsf := range body.StackFrames {
		endLine, endCol := rsf.EndLine, rsf.EndColumn
		if endLine == 0 {
			endLine = rsf.Line
		}
		if endCol == 0 {
			endCol = rsf.Column
		}
		src := s.source(rsf.Source)
		frames = append(frames, &StackFrame{
			Thread: t, ID: rsf.ID, Source: src, Name: rsf.Name, PresentationHint: rsf.PresentationHint,
			Range: Range{rsf.Line, rsf.Column, endLine, endCol}, Index: start + i, CanRestart: rsf.canRestart(),
			InstructionPointerReference: rsf.InstructionPointerReference,
		})
	}
	return frames, body.TotalFrames, ""
}

// ExceptionInfo asks for the exception the thread stopped on (stopped for an exception only). Without exceptionInfo support the
// description is the stop's text.
func (t *Thread) ExceptionInfo(ctx context.Context) (*ExceptionInfo, error) {
	d := t.StoppedDetails()
	if d == nil || d.Reason != dap.StopException {
		return nil, nil
	}
	if t.sess.Capabilities().SupportsExceptionInfoRequest {
		return t.sess.ExceptionInfo(ctx, t.ID)
	}
	return &ExceptionInfo{Description: d.Text}, nil
}

// ExceptionInfo is the exception a thread stopped on.
type ExceptionInfo struct {
	ID          string
	Description string
	BreakMode   string
	Details     *dap.ExceptionDetails
}

// Next steps over, StepIn into, StepOut out of the current function; Continue resumes, Pause pauses and Terminate ends the thread.
// StepBack and ReverseContinue need an adapter with reverse debugging.

// Next steps over.
func (t *Thread) Next(ctx context.Context, granularity string) error {
	return t.sess.Next(ctx, t.ID, granularity)
}

// StepIn steps into.
func (t *Thread) StepIn(ctx context.Context, granularity string) error {
	return t.sess.StepIn(ctx, t.ID, 0, granularity)
}

// StepOut steps out.
func (t *Thread) StepOut(ctx context.Context, granularity string) error {
	return t.sess.StepOut(ctx, t.ID, granularity)
}

// StepBack steps back.
func (t *Thread) StepBack(ctx context.Context, granularity string) error {
	return t.sess.StepBack(ctx, t.ID, granularity)
}

// Continue resumes the thread.
func (t *Thread) Continue(ctx context.Context) error { return t.sess.Continue(ctx, t.ID) }

// Pause pauses the thread.
func (t *Thread) Pause(ctx context.Context) error { return t.sess.Pause(ctx, t.ID) }

// Terminate ends the thread (terminateThreads).
func (t *Thread) Terminate(ctx context.Context) error {
	return t.sess.TerminateThreads(ctx, []int{t.ID})
}

// ReverseContinue resumes backwards.
func (t *Thread) ReverseContinue(ctx context.Context) error {
	return t.sess.ReverseContinue(ctx, t.ID)
}
