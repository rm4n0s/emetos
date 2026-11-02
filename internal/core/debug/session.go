package debug

import (
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"sync"
	"sync/atomic"
	"time"

	"github.com/rm4n0s/emetos/internal/core/dap"
	"github.com/rm4n0s/errors"
)

// TriggeredBreakpointMaxDelay is how long a step or continue waits for the breakpoints that a hit breakpoint enabled to reach the
// adapter.
const TriggeredBreakpointMaxDelay = 1500 * time.Millisecond

// DisconnectTimeout is how long the disconnect request of a shutdown may take; 200 ms when the adapter is already known to be gone.
const DisconnectTimeout = 2 * time.Second

// SessionOptions are the options a session is made with (VS Code's IDebugSessionOptions).
type SessionOptions struct {
	// Parent is the session that started this one (startDebugging); a child is shown under it.
	Parent *Session
	// LifecycleManagedByParent makes stop, disconnect and restart of the child act on the parent.
	LifecycleManagedByParent bool
	// Compact shows the child's threads under the parent's session in the call stack, without its own row.
	Compact bool
	// CompoundRoot ties the session to the others of a compound that stops all.
	CompoundRoot *CompoundRoot
	// NoDebug runs without debugging (also the configuration's noDebug).
	NoDebug *bool
	// NoSaveBeforeStart is set when the files are not saved before the session starts (a child never saves).
	NoSaveBeforeStart bool
	// NoSaveBeforeRestart is set when the files are not saved before a restart.
	NoSaveBeforeRestart bool
	// StartedByUser is set for a session the user started (the question about a second instance is asked).
	StartedByUser bool
	// SuppressDebugStatusbar, SuppressDebugToolbar and SuppressDebugView hide those parts for this session.
	SuppressDebugStatusbar, SuppressDebugToolbar, SuppressDebugView bool
	// Repl is "separate" or "mergeWithParent", for the Debug Console (step 25.6).
	Repl string
}

// CompoundRoot ends all the sessions of a compound with stopAll when one of them is stopped.
//
// Ported from VS Code: src/vs/workbench/contrib/debug/common/debug.ts (DebugCompoundRoot)
type CompoundRoot struct {
	mu      sync.Mutex
	stopped bool
	fns     []func()
}

// OnSessionStop registers fn to run, once, when a session of the compound is stopped.
func (c *CompoundRoot) OnSessionStop(fn func()) {
	c.mu.Lock()
	c.fns = append(c.fns, fn)
	c.mu.Unlock()
}

// SessionStopped tells the compound a session was stopped; the others stop, once.
func (c *CompoundRoot) SessionStopped() {
	c.mu.Lock()
	if c.stopped {
		c.mu.Unlock()
		return
	}
	c.stopped = true
	fns := append([]func(){}, c.fns...)
	c.mu.Unlock()
	for _, fn := range fns {
		fn()
	}
}

// Session is a debug session: one adapter, its threads and what the debuggee reported. The session driver is here: it runs VS Code's
// sequence (initialize, launch or attach, the initialized event, the breakpoints, configurationDone) and keeps the model up to date
// from the adapter's events.
//
// A session reads its adapter on a goroutine of its own and never makes a request from it: the follow-up work of an event (the
// threads and the stack after a stop, the breakpoints after initialized) runs on a serial queue on another goroutine, because the
// event channel of core/dap is bounded and a request from its consumer could wait behind events nobody takes (decision 1102).
//
// Ported from VS Code: src/vs/workbench/contrib/debug/browser/debugSession.ts and browser/rawDebugSession.ts
type Session struct {
	svc    *Service
	m      *Model
	id     string
	parent *Session
	opts   SessionOptions
	root   string
	dbg    Debugger

	life       context.Context // the life of the adapter's program; cancelled when the session is over
	cancelLife context.CancelFunc

	// Guarded by the lock of the Model.
	cfg         Config
	unresolved  Config
	name        string
	client      *dap.Client
	initialized bool
	ready       bool // the initialized event came: breakpoints can be sent
	inShutdown  bool
	terminated  bool // terminate was sent once
	exitFired   bool
	threads     map[int]*Thread
	threadIDs   []int
	stopped     []StoppedDetails
	unavailable map[string]bool
	lastCaps    dap.Capabilities
	reqCtx      map[int]*reqEpoch
	// stoppedSinceLastStep and allThreadsContinued are the raw session's: a step with no stop after it is a continue.
	stoppedSinceLastStep bool
	allThreadsContinued  bool
	lastContinuedThread  int
	hasContinuedThread   bool
	// waitResume is closed when the triggered breakpoints of a hit have been sent; nil when none is pending.
	waitResume chan struct{}
	fetchSched bool
	// stopRequested is set when Terminate or Disconnect was called, and cleared when the session is initialized again.
	stopRequested bool

	wmu      sync.Mutex
	wq       []func()
	wrunning bool
	widle    *sync.Cond

	// evRead counts the events the connection has read (in the order of the wire) and evHandled those the event loop has handled: a
	// response is read in order with the events, but handed over at once, so a caller that needs the events before it waits for them.
	evRead, evHandled atomic.Int64
	evMu              sync.Mutex
	evDone            bool
	evCond            *sync.Cond
}

// reqEpoch cancels the requests of a thread when it continues.
type reqEpoch struct {
	ctx    context.Context
	cancel context.CancelFunc
}

func newSession(svc *Service, id string, cfg, unresolved Config, root string, dbg Debugger, opts SessionOptions) *Session {
	s := &Session{
		svc: svc, m: svc.model, id: id, parent: opts.Parent, opts: opts, root: root, dbg: dbg,
		cfg: cfg, unresolved: unresolved, name: cfg.Name(),
		threads: map[int]*Thread{}, unavailable: map[string]bool{}, reqCtx: map[int]*reqEpoch{},
		allThreadsContinued: true,
	}
	s.life, s.cancelLife = context.WithCancel(svc.base)
	s.widle = sync.NewCond(&s.wmu)
	s.evCond = sync.NewCond(&s.evMu)
	return s
}

// ID is the id of the session.
func (s *Session) ID() string { return s.id }

// Parent is the session that started this one, or nil.
func (s *Session) Parent() *Session { return s.parent }

// Options returns the options the session was made with.
func (s *Session) Options() SessionOptions { return s.opts }

// Root is the folder of the launch the session belongs to, or "".
func (s *Session) Root() string { return s.root }

// Name is the name of the session (the configuration's name, made unique among the sessions).
func (s *Session) Name() string {
	s.m.mu.Lock()
	defer s.m.mu.Unlock()
	return s.name
}

// Label is the name shown in the views and the toolbar.
func (s *Session) Label() string {
	s.m.mu.Lock()
	defer s.m.mu.Unlock()
	return s.labelLocked()
}

func (s *Session) labelLocked() string { return s.name }

// SetName renames the session (an extension may).
func (s *Session) SetName(name string) {
	s.m.mu.Lock()
	s.name = name
	s.m.mu.Unlock()
	s.m.emit(Event{Kind: EventSessions, Session: s})
}

// Configuration returns the resolved configuration the session runs.
func (s *Session) Configuration() Config {
	s.m.mu.Lock()
	defer s.m.mu.Unlock()
	return s.cfg.Clone()
}

// UnresolvedConfiguration returns the configuration before its variables were substituted.
func (s *Session) UnresolvedConfiguration() Config {
	s.m.mu.Lock()
	defer s.m.mu.Unlock()
	return s.unresolved.Clone()
}

// SetConfiguration replaces the configurations (a restart that read launch.json again).
func (s *Session) SetConfiguration(resolved, unresolved Config) {
	s.m.mu.Lock()
	s.cfg, s.unresolved = resolved, unresolved
	s.m.mu.Unlock()
}

// IsAttach reports whether the session attached to a program instead of launching it; the toolbar then offers Disconnect.
func (s *Session) IsAttach() bool { return s.Configuration().Request() == "attach" }

// Type is the debug type of the session.
func (s *Session) Type() string { return s.Configuration().Type() }

// Capabilities returns the capabilities of the adapter, empty before initialize and after the session ended it keeps what it was.
func (s *Session) Capabilities() dap.Capabilities {
	s.m.mu.Lock()
	c := s.client
	last := s.lastCaps
	s.m.mu.Unlock()
	if c != nil {
		return c.Capabilities()
	}
	return last
}

// State is the state of the session.
func (s *Session) State() State {
	s.m.mu.Lock()
	defer s.m.mu.Unlock()
	return s.stateLocked()
}

func (s *Session) stateLocked() State {
	if !s.initialized {
		return Initializing
	}
	if s.client == nil {
		return Inactive
	}
	if f := s.m.focus; f.Thread != nil && f.Thread.sess == s {
		if f.Thread.stopped {
			return Stopped
		}
		return Running
	}
	for _, id := range s.threadIDs {
		if t := s.threads[id]; t != nil && t.stopped {
			return Stopped
		}
	}
	return Running
}

// Threads returns the threads in the order the adapter listed them.
func (s *Session) Threads() []*Thread {
	s.m.mu.Lock()
	defer s.m.mu.Unlock()
	return s.threadsLocked()
}

func (s *Session) threadsLocked() []*Thread {
	out := make([]*Thread, 0, len(s.threadIDs))
	for _, id := range s.threadIDs {
		if t := s.threads[id]; t != nil {
			out = append(out, t)
		}
	}
	return out
}

// Thread returns the thread with an id, or nil.
func (s *Session) Thread(id int) *Thread {
	s.m.mu.Lock()
	defer s.m.mu.Unlock()
	return s.threads[id]
}

// StoppedDetails returns the details of the first stop that is still in effect, or nil.
func (s *Session) StoppedDetails() *StoppedDetails {
	s.m.mu.Lock()
	defer s.m.mu.Unlock()
	if len(s.stopped) == 0 {
		return nil
	}
	d := s.stopped[0]
	return &d
}

func (s *Session) autoExpandLazy() bool {
	if s.svc.opts.AutoExpandLazyVariables == nil {
		return false
	}
	return s.svc.opts.AutoExpandLazyVariables()
}

// source makes the Source of an adapter's source, noting the ones known to be unavailable.
func (s *Session) source(raw *dap.Source) Source {
	src := newSource(raw, s.id)
	s.m.mu.Lock()
	if s.unavailable[src.Key()] {
		src.Available = false
	}
	s.m.mu.Unlock()
	return src
}

// SourceUnavailable notes that the source at a key (Source.Key) could not be opened.
func (s *Session) SourceUnavailable(key string) {
	s.m.mu.Lock()
	s.unavailable[key] = true
	s.m.mu.Unlock()
	s.m.emit(Event{Kind: EventCallStack, Session: s})
}

// ---- the work queue

// enqueue runs fn after the work already queued, on a goroutine that is not the one that reads the adapter.
func (s *Session) enqueue(fn func()) {
	s.wmu.Lock()
	s.wq = append(s.wq, fn)
	if s.wrunning {
		s.wmu.Unlock()
		return
	}
	s.wrunning = true
	s.wmu.Unlock()
	go s.runWork()
}

func (s *Session) runWork() {
	for {
		s.wmu.Lock()
		if len(s.wq) == 0 {
			s.wrunning = false
			s.widle.Broadcast()
			s.wmu.Unlock()
			return
		}
		fn := s.wq[0]
		s.wq[0] = nil
		s.wq = s.wq[1:]
		s.wmu.Unlock()
		fn()
	}
}

// Idle waits until the follow-up work of the events received so far is done: the breakpoints sent after initialized, the threads and
// stack loaded after a stop. It fails when ctx ends first.
func (s *Session) Idle(ctx context.Context) error {
	done := make(chan struct{})
	go func() {
		s.wmu.Lock()
		for s.wrunning || len(s.wq) > 0 {
			s.widle.Wait()
		}
		s.wmu.Unlock()
		close(done)
	}()
	select {
	case <-done:
		return nil
	case <-ctx.Done():
		s.wmu.Lock()
		s.widle.Broadcast()
		s.wmu.Unlock()
		return errors.NewErr("DebugIdleCancelled", ctx.Err()).SetMetadata("session", s.id)
	}
}

// ---- start

// initializeArgs are the capabilities of Emetos as a client: SPEC §10.31 "The session". supportsMemoryReferences stays false until
// a memory view exists.
func initializeArgs(adapterID string) dap.InitializeRequestArguments {
	return dap.InitializeRequestArguments{
		ClientID: "emetos", ClientName: "Emetos", AdapterID: adapterID, Locale: "en",
		LinesStartAt1: true, ColumnsStartAt1: true, PathFormat: "path",
		SupportsVariableType: true, SupportsVariablePaging: true, SupportsRunInTerminalRequest: true,
		SupportsStartDebuggingRequest: true, SupportsProgressReporting: true, SupportsInvalidatedEvent: true,
	}
}

// initialize connects to the adapter and sends initialize. On failure the session is shut down.
func (s *Session) initialize(ctx context.Context) error {
	s.m.mu.Lock()
	had := s.client != nil
	s.m.mu.Unlock()
	if had {
		s.shutdown(nil)
	}
	cfg := s.Configuration()
	client, err := s.dbg.connect(WithSession(s.life, s), cfg, dap.ClientOptions{
		HandleRequest: s.handleReverse,
		Logger:        s.svc.logger(),
		Trace: func(tm dap.TraceMessage) {
			if tm.Direction == dap.FromAdapter && tm.Type == dap.TypeEvent {
				s.evRead.Add(1)
			}
			if s.svc.opts.Trace != nil {
				s.svc.opts.Trace(s, tm)
			}
		},
	})
	if err != nil {
		s.m.mu.Lock()
		s.initialized = true
		s.m.mu.Unlock()
		s.m.emit(Event{Kind: EventSessions, Session: s})
		return err
	}
	s.m.mu.Lock()
	s.client = client
	s.evRead.Store(0)
	s.evHandled.Store(0)
	s.evMu.Lock()
	s.evDone = false
	s.evMu.Unlock()
	s.inShutdown, s.exitFired, s.terminated, s.ready, s.stopRequested = false, false, false, false, false
	s.m.mu.Unlock()
	go s.eventLoop(client)

	if _, err := client.Request(ctx, dap.CmdInitialize, initializeArgs(cfg.Type())); err != nil {
		s.m.mu.Lock()
		s.initialized = true
		s.m.mu.Unlock()
		s.m.emit(Event{Kind: EventSessions, Session: s})
		s.shutdown(err)
		return err
	}
	caps := client.Capabilities()
	s.m.mu.Lock()
	s.initialized = true
	s.lastCaps = caps
	s.m.mu.Unlock()
	s.m.emit(Event{Kind: EventSessions, Session: s})
	s.m.setExceptionBreakpointsForSession(s.id, caps.ExceptionBreakpointFilters)
	s.m.registerBreakpointModes(cfg.Type(), caps.BreakpointModes)
	return nil
}

// launchOrAttach sends the launch or attach request with the configuration. The adapter may answer it only after configurationDone,
// which the initialized event's follow-up sends meanwhile; so this waits for the response with the event loop running.
func (s *Session) launchOrAttach(ctx context.Context, cfg Config) error {
	s.m.mu.Lock()
	client := s.client
	parent := s.parent
	s.m.mu.Unlock()
	if client == nil {
		return errors.New("DebugNoAdapter", "No debugger available, can not send 'launch or attach'", "session", s.id)
	}
	if parent != nil && parent.State() == Inactive {
		return errors.NewErr("DebugParentInactive", context.Canceled).SetMetadata("session", s.id)
	}
	args := cfg.Clone()
	args["__sessionId"] = s.id
	if _, err := client.Request(ctx, cfg.Request(), map[string]any(args)); err != nil {
		s.shutdown(nil)
		return err
	}
	return nil
}

// ---- events

// eventLoop takes the events of the adapter until the connection ends. It never makes a request: what needs one is queued.
func (s *Session) eventLoop(c *dap.Client) {
	for ev := range c.Events() {
		s.handleEvent(ev)
		s.evHandled.Add(1)
		s.evMu.Lock()
		s.evCond.Broadcast()
		s.evMu.Unlock()
	}
	s.evMu.Lock()
	s.evDone = true
	s.evCond.Broadcast()
	s.evMu.Unlock()
	<-c.Done()
	s.adapterEnded(c)
}

func (s *Session) handleEvent(ev dap.Event) {
	body, err := dap.ParseEvent(ev)
	if err != nil {
		s.svc.logger().Warn("a debug adapter event could not be read", "event", ev.Event, "err", err)
		return
	}
	switch b := body.(type) {
	case *struct{}: // initialized
		s.m.mu.Lock()
		s.ready = true
		s.m.mu.Unlock()
		s.enqueue(s.afterInitialized)
	case *dap.StoppedEventBody:
		s.m.mu.Lock()
		s.stoppedSinceLastStep = true
		s.m.mu.Unlock()
		s.handleStop(StoppedDetails{StoppedEventBody: *b})
	case *dap.ContinuedEventBody:
		all := true
		var raw struct {
			AllThreadsContinued *bool `json:"allThreadsContinued"`
		}
		if json.Unmarshal(ev.Body, &raw) == nil && raw.AllThreadsContinued != nil {
			all = *raw.AllThreadsContinued
		}
		s.m.mu.Lock()
		s.allThreadsContinued = all
		s.m.mu.Unlock()
		s.onContinued(b.ThreadID, all)
	case *dap.ThreadEventBody:
		s.handleThread(*b)
	case *dap.TerminatedEventBody:
		s.handleTerminated(b)
	case *dap.ExitedEventBody:
		s.m.emit(Event{Kind: EventExited, Session: s, ExitCode: b.ExitCode})
	case *dap.OutputEventBody:
		s.handleOutput(b)
	case *dap.BreakpointEventBody:
		s.handleBreakpoint(b)
	case *dap.LoadedSourceEventBody:
		e := ev
		s.m.emit(Event{Kind: EventLoadedSource, Session: s, DAP: &e})
	case *dap.CapabilitiesEventBody:
		s.m.mu.Lock()
		if s.client != nil {
			s.lastCaps = s.client.Capabilities()
		}
		s.m.mu.Unlock()
		s.m.emit(Event{Kind: EventCapabilities, Session: s})
	case *dap.ProgressStartEventBody, *dap.ProgressUpdateEventBody, *dap.ProgressEndEventBody:
		e := ev
		s.m.emit(Event{Kind: EventProgress, Session: s, DAP: &e})
	case *dap.InvalidatedEventBody:
		s.handleInvalidated(b)
	case *dap.ModuleEventBody, *dap.ProcessEventBody:
		// Not shown.
	default:
		e := ev
		s.m.emit(Event{Kind: EventCustom, Session: s, DAP: &e})
	}
}

// afterInitialized is the work after the initialized event: all the breakpoints, configurationDone, and the threads.
func (s *Session) afterInitialized() {
	ctx := s.life
	if err := s.svc.sendAllBreakpoints(ctx, s); err != nil {
		s.svc.logger().Warn("sending the breakpoints failed", "session", s.id, "err", err)
	}
	if s.Capabilities().SupportsConfigurationDoneRequest {
		if _, err := s.do(ctx, dap.CmdConfigurationDone, nil); err != nil {
			// Disconnect the session on a configurationDone error (VS Code's issue 10596).
			s.svc.logger().Error("configurationDone failed", "session", s.id, "err", err)
			s.disconnectNow(ctx)
			return
		}
	}
	if err := s.fetchThreads(ctx, nil); err != nil {
		s.svc.logger().Warn("fetching the threads failed", "session", s.id, "err", err)
	}
}

func (s *Session) disconnectNow(ctx context.Context) {
	s.shutdownWith(ctx, nil, false, nil, nil)
}

// fetchThreads asks for the threads and updates the model; stopped, when given, is the stop that marks threads stopped.
func (s *Session) fetchThreads(ctx context.Context, stopped *StoppedDetails) error {
	body, err := callBody[dap.ThreadsResponseBody](ctx, s, dap.CmdThreads, nil)
	if err != nil {
		return err
	}
	s.rawUpdate(body.Threads, stopped)
	return nil
}

// rawUpdate replaces the list of threads and marks the threads a stop is about as stopped.
//
// Ported from VS Code: src/vs/workbench/contrib/debug/browser/debugSession.ts (rawUpdate)
func (s *Session) rawUpdate(threads []dap.Thread, stopped *StoppedDetails) {
	s.m.mu.Lock()
	s.threadIDs = s.threadIDs[:0]
	for _, th := range threads {
		s.threadIDs = append(s.threadIDs, th.ID)
		if t, ok := s.threads[th.ID]; !ok {
			s.threads[th.ID] = &Thread{sess: s, ID: th.ID, name: th.Name}
		} else if th.Name != "" {
			t.name = th.Name
		}
	}
	for id := range s.threads {
		found := false
		for _, tid := range s.threadIDs {
			found = found || tid == id
		}
		if !found {
			delete(s.threads, id) // threads no longer in the list are gone (VS Code's issue 75980)
		}
	}
	if stopped != nil {
		if stopped.AllThreadsStopped {
			for _, t := range s.threads {
				if t.ID == stopped.ThreadID {
					d := *stopped
					t.details = &d
				} else {
					t.details = &StoppedDetails{StoppedEventBody: dap.StoppedEventBody{Reason: reasonOf(t.details)}}
				}
				t.stopped = true
				t.clearCallStackLocked()
			}
		} else if t := s.threads[stopped.ThreadID]; stopped.ThreadID != 0 && t != nil {
			d := *stopped
			t.details = &d
			t.clearCallStackLocked()
			t.stopped = true
		}
	}
	s.m.mu.Unlock()
	s.m.emit(Event{Kind: EventCallStack, Session: s})
}

func reasonOf(d *StoppedDetails) string {
	if d == nil {
		return ""
	}
	return d.Reason
}

// handleStop is the work for a stopped event. The threads are fetched and the stack of each stopped thread is loaded on the queue.
func (s *Session) handleStop(d StoppedDetails) {
	s.cancelPassFocus()
	s.m.mu.Lock()
	s.stopped = append(s.stopped, d)
	s.m.mu.Unlock()
	hit := append([]int(nil), d.HitBreakpointIDs...)
	s.enqueue(func() {
		ctx := s.life
		if len(hit) > 0 {
			s.enableDependentBreakpoints(ctx, hit, nil)
		}
		if err := s.fetchThreads(ctx, &d); err != nil {
			s.svc.logger().Warn("fetching the threads of a stop failed", "session", s.id, "err", err)
			return
		}
		var affected []*Thread
		s.m.mu.Lock()
		if d.ThreadID == 0 {
			affected = s.threadsLocked()
		} else if t := s.threads[d.ThreadID]; t != nil {
			affected = []*Thread{t}
		}
		s.m.mu.Unlock()
		lots := d.ThreadID == 0 && len(affected) > 10
		for _, t := range affected {
			s.loadStack(ctx, t, d, len(hit) > 0, !lots)
		}
		s.m.emit(Event{Kind: EventSessions, Session: s})
	})
}

// loadStack loads the top of the call stack of a stopped thread, focuses it, and then loads the rest.
func (s *Session) loadStack(ctx context.Context, t *Thread, d StoppedDetails, hitHandled, fetchAll bool) {
	delayed := s.Capabilities().SupportsDelayedStackTraceLoading
	first := 20
	if delayed {
		first = 1
	}
	if err := t.FetchCallStack(ctx, first); err != nil {
		s.svc.logger().Warn("loading the call stack failed", "session", s.id, "err", err)
	}
	s.m.emit(Event{Kind: EventCallStack, Session: s, Thread: t})
	if !hitHandled {
		s.enableDependentBreakpoints(ctx, nil, t)
	}
	s.svc.focusAfterStop(s, t, d)
	if delayed && fetchAll {
		if delay := s.svc.opts.StackDelay; delay > 0 {
			select {
			case <-time.After(delay):
			case <-ctx.Done():
				return
			}
		}
		if !t.ReachedEndOfCallStack() {
			if err := t.FetchCallStack(ctx, 19); err != nil {
				s.svc.logger().Warn("loading the call stack failed", "session", s.id, "err", err)
			}
			s.m.emit(Event{Kind: EventCallStack, Session: s, Thread: t})
		}
	}
	s.m.emit(Event{Kind: EventStopped, Session: s, Thread: t})
}

// LoadMoreFrames loads more frames of the call stack of a stopped thread ("Show N More Stack Frames"): levels of them, but not more
// than the thread has left.
//
// Ported from VS Code: src/vs/workbench/contrib/debug/common/debugModel.ts (DebugModel.fetchCallstack)
func (s *Session) LoadMoreFrames(ctx context.Context, t *Thread, levels int) error {
	s.m.mu.Lock()
	if t.reachedEnd {
		s.m.mu.Unlock()
		return nil
	}
	remaining := 0
	if t.details != nil && t.details.TotalFrames > 0 {
		remaining = t.details.TotalFrames - len(t.callStack)
	}
	if levels == 0 || (remaining > 0 && levels > remaining) {
		levels = remaining
	}
	s.m.mu.Unlock()
	if levels <= 0 {
		return nil
	}
	if err := t.FetchCallStack(ctx, levels); err != nil {
		return err
	}
	s.m.emit(Event{Kind: EventCallStack, Session: s, Thread: t})
	return nil
}

// enableDependentBreakpoints sends the breakpoints that wait for the breakpoints just hit ("Wait for Breakpoint"): the breakpoint
// ids the adapter reported, or the breakpoints at the top frame of the thread.
func (s *Session) enableDependentBreakpoints(ctx context.Context, hitIDs []int, t *Thread) {
	var hit []Breakpoint
	if hitIDs != nil {
		s.m.mu.Lock()
		for _, b := range s.m.lines {
			if id, ok := b.adapterID(s.id); ok {
				for _, h := range hitIDs {
					if h == id {
						hit = append(hit, s.m.snapshotLineLocked(b))
					}
				}
			}
		}
		s.m.mu.Unlock()
	} else {
		frame := t.TopStackFrame()
		if frame == nil {
			return
		}
		if d := t.StoppedDetails(); d != nil && d.Reason != dap.StopBreakpoint {
			return
		}
		for _, b := range s.m.Breakpoints(BreakpointFilter{Path: frame.Source.Path}) {
			if b.Line < frame.Range.Line || b.Line > frame.Range.EndLine {
				continue
			}
			if b.Column > 0 && (b.Column < frame.Range.Column || b.Column > frame.Range.EndColumn) {
				continue
			}
			hit = append(hit, b)
		}
	}
	paths := map[string]bool{}
	var order []string
	for _, tb := range s.m.Breakpoints(BreakpointFilter{TriggeredOnly: true, EnabledOnly: true}) {
		for _, h := range hit {
			if tb.Enabled && tb.TriggeredBy == h.ID {
				s.m.setTriggered(tb.ID, s.id, true)
				if !paths[tb.Path] {
					paths[tb.Path] = true
					order = append(order, tb.Path)
				}
			}
		}
	}
	if len(order) == 0 {
		return
	}
	done := make(chan struct{})
	s.m.mu.Lock()
	s.waitResume = done
	s.m.mu.Unlock()
	for _, p := range order {
		if err := s.svc.sendBreakpoints(ctx, p, false, s); err != nil {
			s.svc.logger().Warn("sending the triggered breakpoints failed", "session", s.id, "err", err)
		}
	}
	close(done)
}

// waitForTriggered waits (at most TriggeredBreakpointMaxDelay) for the breakpoints a hit enabled to be sent.
func (s *Session) waitForTriggered(ctx context.Context) {
	s.m.mu.Lock()
	ch := s.waitResume
	s.m.mu.Unlock()
	if ch == nil {
		return
	}
	t := time.NewTimer(TriggeredBreakpointMaxDelay)
	defer t.Stop()
	select {
	case <-ch:
	case <-t.C:
	case <-ctx.Done():
	}
}

// onContinued handles a continued event, and the one a step with no stop after it stands for.
func (s *Session) onContinued(threadID int, all bool) {
	s.m.mu.Lock()
	if all {
		s.stopped = nil
	} else {
		kept := s.stopped[:0:0]
		for _, d := range s.stopped {
			if d.ThreadID != threadID {
				kept = append(kept, d)
			}
		}
		s.stopped = kept
	}
	var affected []*Thread
	if all {
		affected = s.threadsLocked()
	} else if t := s.threads[threadID]; t != nil {
		affected = []*Thread{t}
	}
	needFetch := !all && s.threads[threadID] == nil
	for _, t := range affected {
		s.cancelThreadRequestsLocked(t.ID)
		t.clearCallStackLocked()
		t.details = nil
		t.stopped = false
	}
	s.lastContinuedThread, s.hasContinuedThread = threadID, !all
	s.m.mu.Unlock()
	if needFetch {
		s.enqueue(func() {
			if err := s.fetchThreads(s.life, nil); err != nil {
				s.svc.logger().Warn("fetching the threads failed", "session", s.id, "err", err)
			}
		})
	}
	s.m.emit(Event{Kind: EventCallStack, Session: s})
	s.m.emit(Event{Kind: EventSessions, Session: s})
	s.svc.schedulePassFocus(s)
}

func (s *Session) handleThread(b dap.ThreadEventBody) {
	switch b.Reason {
	case "started":
		s.m.mu.Lock()
		pending := s.fetchSched
		s.fetchSched = true
		s.m.mu.Unlock()
		if !pending {
			s.enqueue(func() {
				s.m.mu.Lock()
				s.fetchSched = false
				s.m.mu.Unlock()
				if err := s.fetchThreads(s.life, nil); err != nil {
					s.svc.logger().Warn("fetching the threads failed", "session", s.id, "err", err)
				}
			})
		}
	case "exited":
		s.m.mu.Lock()
		if t := s.threads[b.ThreadID]; t != nil {
			s.cancelThreadRequestsLocked(b.ThreadID)
			t.clearCallStackLocked()
			t.details, t.stopped = nil, false
			delete(s.threads, b.ThreadID)
			ids := s.threadIDs[:0]
			for _, id := range s.threadIDs {
				if id != b.ThreadID {
					ids = append(ids, id)
				}
			}
			s.threadIDs = ids
		}
		f := s.m.focus
		s.m.mu.Unlock()
		s.cancelPassFocus()
		if f.Thread != nil && f.Thread.sess == s && f.Thread.ID == b.ThreadID {
			s.svc.FocusStackFrame(nil, nil, f.Session)
		}
		s.m.emit(Event{Kind: EventCallStack, Session: s})
	}
}

func (s *Session) handleTerminated(b *dap.TerminatedEventBody) {
	restart := len(b.Restart) > 0 && string(b.Restart) != "null" && string(b.Restart) != "false"
	if restart {
		var data any
		if err := json.Unmarshal(b.Restart, &data); err != nil {
			data = true
		}
		go func() {
			if err := s.svc.RestartSession(s.life, s, data); err != nil {
				s.svc.logger().Warn("restarting the session after terminated failed", "session", s.id, "err", err)
			}
		}()
		return
	}
	go func() {
		f := false
		s.shutdownWith(s.life, nil, false, &f, nil)
	}()
}

func (s *Session) handleOutput(b *dap.OutputEventBody) {
	if b.VariablesReference != 0 {
		ref := &Reference{}
		ref.id = newID()
		ref.sess = s
		ref.reference = b.VariablesReference
		s.m.emit(Event{Kind: EventOutput, Session: s, Output: b, Node: ref})
		return
	}
	s.m.emit(Event{Kind: EventOutput, Session: s, Output: b})
}

// Reference is a node for a variablesReference with no variable of its own: what an output event with a reference stands for.
type Reference struct{ container }

func (s *Session) handleBreakpoint(b *dap.BreakpointEventBody) {
	id := b.Breakpoint.ID
	s.m.mu.Lock()
	var (
		line *lineBP
		fn   *funcBP
		ex   *exceptionBP
	)
	for _, x := range s.m.lines {
		if got, ok := x.adapterID(s.id); ok && got == id {
			line = x
		}
	}
	for _, x := range s.m.funcs {
		if got, ok := x.adapterID(s.id); ok && got == id {
			fn = x
		}
	}
	for _, x := range s.m.exceptions {
		if got, ok := x.adapterID(s.id); ok && got == id {
			ex = x
		}
	}
	caps := s.lastCaps
	if s.client != nil {
		caps = s.client.Capabilities()
	}
	s.m.mu.Unlock()

	switch b.Reason {
	case "new":
		if b.Breakpoint.Source != nil && b.Breakpoint.Line != 0 && b.Breakpoint.Source.Path != "" {
			added := s.m.addBreakpoints(b.Breakpoint.Source.Path, []BreakpointData{{Line: b.Breakpoint.Line, Column: b.Breakpoint.Column}})
			if len(added) == 1 {
				s.m.setBreakpointSessionData(s.id, caps, map[string]dap.Breakpoint{added[0].ID: b.Breakpoint})
			}
		}
	case "removed":
		if line != nil {
			s.m.removeBreakpoints([]string{line.id})
		}
		if fn != nil {
			s.m.removeFunctionBreakpoints(fn.id)
		}
	case "changed":
		bp := b.Breakpoint
		data := map[string]dap.Breakpoint{}
		if line != nil {
			if line.column == 0 {
				bp.Column = 0
			}
			data[line.id] = bp
		}
		if fn != nil {
			data[fn.id] = bp
		}
		if ex != nil {
			data[ex.id] = bp
		}
		if len(data) > 0 {
			s.m.setBreakpointSessionData(s.id, caps, data)
		}
	}
}

func (s *Session) handleInvalidated(b *dap.InvalidatedEventBody) {
	areas := b.Areas
	if len(areas) == 0 {
		areas = []string{"all"}
	}
	refetch := false
	for _, a := range areas {
		refetch = refetch || a == "threads" || a == "stacks" || a == "all"
	}
	if refetch {
		s.cancelAllRequests()
		s.m.mu.Lock()
		details := append([]StoppedDetails(nil), s.stopped...)
		s.stopped = nil
		for _, t := range s.threads {
			t.clearCallStackLocked()
			t.details, t.stopped = nil, false
		}
		s.threads = map[int]*Thread{}
		s.threadIDs = nil
		s.m.mu.Unlock()
		s.m.forgetValues()
		if len(details) > 0 {
			for _, d := range details {
				s.handleStop(d)
			}
		} else {
			s.enqueue(func() {
				if err := s.fetchThreads(s.life, nil); err != nil {
					s.svc.logger().Warn("fetching the threads failed", "session", s.id, "err", err)
				}
			})
		}
	}
	s.m.emit(Event{Kind: EventInvalidated, Session: s})
}

// ---- reverse requests

// handleReverse answers the requests of the adapter: runInTerminal and startDebugging.
//
// Ported from VS Code: src/vs/workbench/contrib/debug/browser/rawDebugSession.ts (dispatchRequest)
func (s *Session) handleReverse(ctx context.Context, command string, raw json.RawMessage) (any, error) {
	switch command {
	case dap.ReverseRunInTerminal:
		var args dap.RunInTerminalRequestArguments
		if err := json.Unmarshal(raw, &args); err != nil {
			return nil, errors.NewErr("DebugReverseRequestInvalid", err).SetMetadata("command", command)
		}
		if s.svc.opts.RunInTerminal == nil {
			return nil, errors.New("DebugRunInTerminalUnavailable", "the editor cannot run a program in a terminal here", "session", s.id)
		}
		pid, err := s.svc.opts.RunInTerminal(ctx, args, s.id)
		if err != nil {
			return nil, err
		}
		body := dap.RunInTerminalResponseBody{}
		if pid > 0 {
			body.ShellProcessID = pid
		}
		return body, nil
	case dap.ReverseStartDebugging:
		var args dap.StartDebuggingRequestArguments
		if err := json.Unmarshal(raw, &args); err != nil {
			return nil, errors.NewErr("DebugReverseRequestInvalid", err).SetMetadata("command", command)
		}
		cfg := Config{}
		for k, v := range args.Configuration {
			var val any
			if err := json.Unmarshal(v, &val); err != nil {
				return nil, errors.NewErr("DebugReverseRequestInvalid", err).SetMetadata("command", command, "attribute", k)
			}
			cfg[k] = val
		}
		cfg["request"] = args.Request
		cfg["type"] = s.Type()
		if cfg.Name() == "" {
			cfg["name"] = s.Name()
		}
		ok, err := s.svc.startChild(ctx, s, cfg)
		if err != nil {
			return nil, err
		}
		if !ok {
			return nil, errors.New("DebugStartDebuggingFailed", "Failed to start debugging", "session", s.id)
		}
		return nil, nil
	}
	return nil, errors.New("DebugReverseRequestUnknown", "unknown request '"+command+"'", "command", command)
}

// ---- the end

// adapterEnded handles the end of the connection: the adapter exited, or the session shut it down.
func (s *Session) adapterEnded(c *dap.Client) {
	s.m.mu.Lock()
	if s.client != c {
		s.m.mu.Unlock()
		return
	}
	crashed := !s.inShutdown && !s.terminatedByAdapterLocked()
	s.m.mu.Unlock()
	var cause error
	if crashed {
		cause = adapterCrash(c)
	}
	s.fireAdapterExit(cause)
}

// terminatedByAdapterLocked reports whether the adapter ended normally: it has said terminated or been told to stop.
func (s *Session) terminatedByAdapterLocked() bool { return s.terminated }

// adapterCrash is the error of an adapter program that ended with a failure status or a broken stream, or nil when it ended with
// status 0 after closing the stream.
func adapterCrash(c *dap.Client) error {
	err := c.Err()
	code := 0
	if x, ok := c.Transport().(interface {
		Done() <-chan struct{}
		ExitCode() int
	}); ok {
		select {
		case <-x.Done():
			code = x.ExitCode()
		case <-time.After(300 * time.Millisecond):
		}
	}
	if code == 0 && (err == nil || errors.Is(err, io.EOF)) {
		return nil
	}
	if code != 0 {
		return errors.New("DebugAdapterCrashed", "Debug adapter process has terminated unexpectedly (exit code "+itoa(code)+")", "code", code)
	}
	if appErr, ok := errors.FromError(err); ok {
		return appErr
	}
	return errors.NewErr("DebugAdapterCrashed", err)
}

// fireAdapterExit ends the session's use of its adapter, once: the data the breakpoints had from it goes, the threads go, and the
// service is told so that it can run the task after the session and move the focus.
func (s *Session) fireAdapterExit(cause error) {
	s.m.mu.Lock()
	if s.exitFired {
		s.m.mu.Unlock()
		return
	}
	s.exitFired = true
	s.initialized = true
	caps := s.lastCaps
	if s.client != nil {
		caps = s.client.Capabilities()
		s.lastCaps = caps
	}
	s.m.mu.Unlock()
	s.m.setBreakpointSessionData(s.id, caps, nil)
	s.resetState()
	s.svc.sessionEnded(s, cause)
}

// resetState drops what the session knew of the debuggee: threads, sources, stops. The session can be initialized again.
func (s *Session) resetState() {
	s.cancelPassFocus()
	s.m.mu.Lock()
	client := s.client
	s.client = nil
	for _, t := range s.threads {
		t.clearCallStackLocked()
		t.details, t.stopped = nil, false
	}
	s.threads = map[int]*Thread{}
	s.threadIDs = nil
	s.stopped = nil
	s.unavailable = map[string]bool{}
	s.cancelAllRequestsLocked()
	s.ready = false
	s.m.mu.Unlock()
	if client != nil {
		if err := client.Close(); err != nil {
			s.svc.logger().Debug("closing the debug adapter failed", "session", s.id, "err", err)
		}
	}
	s.m.forgetValues()
	s.m.emit(Event{Kind: EventCallStack, Session: s})
	s.m.emit(Event{Kind: EventSessions, Session: s})
}

// shutdown ends the connection without a disconnect request worth waiting for; cause is the failure that led to it.
func (s *Session) shutdown(cause error) { s.shutdownWith(s.life, cause, false, nil, nil) }

// shutdownWith sends disconnect (waiting at most DisconnectTimeout) and closes the adapter, once.
//
// Ported from VS Code: src/vs/workbench/contrib/debug/browser/rawDebugSession.ts (shutdown, stopAdapter)
func (s *Session) shutdownWith(ctx context.Context, cause error, restart bool, terminateDebuggee, suspendDebuggee *bool) {
	s.m.mu.Lock()
	if s.inShutdown {
		s.m.mu.Unlock()
		return
	}
	s.inShutdown = true
	client := s.client
	s.m.mu.Unlock()
	if client != nil {
		caps := client.Capabilities()
		args := dap.DisconnectArguments{Restart: restart}
		if caps.SupportTerminateDebuggee {
			args.TerminateDebuggee = terminateDebuggee
		}
		if caps.SupportTerminateDebuggee && caps.SupportSuspendDebuggee {
			args.SuspendDebuggee = suspendDebuggee
		}
		timeout := DisconnectTimeout
		if cause != nil {
			timeout = 200 * time.Millisecond
		}
		dctx, cancel := context.WithTimeout(context.WithoutCancel(ctx), timeout)
		if _, err := client.Request(dctx, dap.CmdDisconnect, args); err != nil {
			// The adapter is shutting down: there is nothing to show the user.
			s.svc.logger().Debug("the disconnect request failed", "session", s.id, "err", err)
		}
		cancel()
	}
	s.fireAdapterExit(cause)
}

// Terminate ends the debuggee: terminate when the adapter supports it for a launch (twice, it disconnects with terminateDebuggee),
// else disconnect with terminateDebuggee. With restart the session is not over: a restart follows.
func (s *Session) Terminate(ctx context.Context, restart bool) error {
	s.m.mu.Lock()
	s.stopRequested = true
	s.m.mu.Unlock()
	s.m.mu.Lock()
	client := s.client
	s.m.mu.Unlock()
	if client == nil {
		// The adapter went down without a terminated event: act as if it had come.
		s.fireAdapterExit(nil)
	}
	s.cancelAllRequests()
	if s.opts.LifecycleManagedByParent && s.parent != nil {
		if err := s.parent.Terminate(ctx, restart); err != nil {
			return err
		}
	} else if client != nil {
		caps := client.Capabilities()
		if caps.SupportsTerminateRequest && s.Configuration().Request() == "launch" {
			s.m.mu.Lock()
			first := !s.terminated
			s.terminated = true
			s.m.mu.Unlock()
			if first {
				if _, err := client.Request(ctx, dap.CmdTerminate, dap.TerminateArguments{Restart: restart}); err != nil {
					return err
				}
			} else {
				t := true
				s.shutdownWith(ctx, nil, restart, &t, nil)
			}
		} else {
			t := true
			s.shutdownWith(ctx, nil, restart, &t, nil)
		}
	}
	if !restart && s.opts.CompoundRoot != nil {
		s.opts.CompoundRoot.SessionStopped()
	}
	return nil
}

// Disconnect ends the session without ending the debuggee (terminateDebuggee false), with suspendDebuggee when asked and supported.
func (s *Session) Disconnect(ctx context.Context, restart, suspend bool) error {
	s.m.mu.Lock()
	s.stopRequested = true
	s.m.mu.Unlock()
	s.m.mu.Lock()
	client := s.client
	s.m.mu.Unlock()
	if client == nil {
		s.fireAdapterExit(nil)
	}
	s.cancelAllRequests()
	if s.opts.LifecycleManagedByParent && s.parent != nil {
		if err := s.parent.Disconnect(ctx, restart, suspend); err != nil {
			return err
		}
	} else if client != nil {
		f := false
		var susp *bool
		if suspend {
			susp = &suspend
		}
		s.shutdownWith(ctx, nil, restart, &f, susp)
	}
	if !restart && s.opts.CompoundRoot != nil {
		s.opts.CompoundRoot.SessionStopped()
	}
	return nil
}

// Restart asks the adapter to restart the debuggee (restart); the adapter must support it.
func (s *Session) Restart(ctx context.Context) error {
	s.m.mu.Lock()
	client := s.client
	s.m.mu.Unlock()
	if client == nil {
		return errors.New("DebugNoAdapter", "No debugger available, can not send 'restart'", "session", s.id)
	}
	s.cancelAllRequests()
	if s.opts.LifecycleManagedByParent && s.parent != nil {
		return s.parent.Restart(ctx)
	}
	if !client.Capabilities().SupportsRestartRequest {
		return errors.New("DebugNotSupported", "restart not supported", "session", s.id, "request", "restart")
	}
	_, err := client.Request(ctx, dap.CmdRestart, dap.RestartArguments{Arguments: mustJSON(s.Configuration())})
	return err
}

func mustJSON(v any) json.RawMessage {
	b, err := json.Marshal(v)
	if err != nil {
		return nil
	}
	return b
}

// logger is the logger of the service.
func (svc *Service) logger() *slog.Logger {
	if svc.opts.Logger != nil {
		return svc.opts.Logger
	}
	return slog.New(slog.DiscardHandler)
}

// stoppedByUser tells whether the session was shut down while it started: the failure of its requests is then the user's Stop, not a
// problem to report.
func (s *Session) stoppedByUser() bool {
	s.m.mu.Lock()
	defer s.m.mu.Unlock()
	return s.stopRequested
}
