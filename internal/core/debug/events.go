package debug

import (
	"encoding/binary"
	"encoding/hex"
	"math/rand/v2"
	"strconv"
	"sync"

	"github.com/rm4n0s/emetos/internal/core/dap"
)

// State is the state of a session, or of the debugger as a whole: the values of the context key debugState.
type State int

const (
	// Inactive is no session at all, or a session whose adapter is gone.
	Inactive State = iota
	// Initializing is a session being started (the adapter is not initialized yet).
	Initializing
	// Stopped is a session with a thread that stopped.
	Stopped
	// Running is a session whose threads run.
	Running
)

// String is the label of the context key debugState: inactive, initializing, stopped or running.
func (s State) String() string {
	switch s {
	case Initializing:
		return "initializing"
	case Stopped:
		return "stopped"
	case Running:
		return "running"
	}
	return "inactive"
}

// EventKind says what an Event is about.
type EventKind int

const (
	// EventSessions is a change of the list of sessions or of a session's name or state.
	EventSessions EventKind = iota + 1
	// EventCallStack is a change of the threads or their stack frames.
	EventCallStack
	// EventBreakpoints is a change of breakpoints (Event.Breakpoints says which).
	EventBreakpoints
	// EventWatch is a change of the list of watch expressions.
	EventWatch
	// EventWatchValue is a watch expression whose value changed (Event.Expression).
	EventWatchValue
	// EventVariables is a variable whose value was changed or loaded lazily.
	EventVariables
	// EventFocus is a change of the focused session, thread or stack frame.
	EventFocus
	// EventOutput is an output event of the adapter (Event.Output); for a variablesReference Event.Node holds the container.
	EventOutput
	// EventCapabilities is a capabilities event.
	EventCapabilities
	// EventLoadedSource is a loadedSource event (Event.DAP).
	EventLoadedSource
	// EventCustom is an event no one else handles: an adapter's own (Event.DAP).
	EventCustom
	// EventProgress is a progressStart, progressUpdate or progressEnd event (Event.DAP).
	EventProgress
	// EventInvalidated is an invalidated event.
	EventInvalidated
	// EventExited is the exit of the debuggee (Event.ExitCode).
	EventExited
	// EventSessionEnded is the end of a session's adapter (Event.Restart is true when a restart follows).
	EventSessionEnded
	// EventConfiguration is a change of the launches or of the selected configuration.
	EventConfiguration
	// EventSessionStarted is a session whose launch or attach request was answered (VS Code's onDidNewSession).
	EventSessionStarted
	// EventStopped is a thread that stopped and has its top frame (Event.Thread), after the stack is loaded; the window opens
	// the source then.
	EventStopped
)

// AffectsState tells whether an event changes what is saved with the workspace (SaveState).
func (k EventKind) AffectsState() bool {
	switch k {
	case EventBreakpoints, EventWatch, EventConfiguration:
		return true
	}
	return false
}

// BreakpointsChange lists the breakpoints an EventBreakpoints is about, by id.
type BreakpointsChange struct {
	Added, Removed, Changed []string
	// SessionOnly is true when only the data of the sessions changed (verified, moved): nothing to save.
	SessionOnly bool
}

// Event is a change of the model. See Service.Subscribe.
type Event struct {
	Kind        EventKind
	Session     *Session
	Thread      *Thread
	Breakpoints BreakpointsChange
	Expression  *Expression
	Output      *dap.OutputEventBody
	Node        Node
	DAP         *dap.Event
	ExitCode    int
	Restart     bool
	// Err is why an adapter ended when it was not asked to: a crash (EventSessionEnded).
	Err error
}

// subscribers is the list of the listeners of a model.
type subscribers struct {
	mu   sync.Mutex
	next int
	fns  map[int]func(Event)
}

func (s *subscribers) add(fn func(Event)) (cancel func()) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.fns == nil {
		s.fns = map[int]func(Event){}
	}
	s.next++
	id := s.next
	s.fns[id] = fn
	return func() {
		s.mu.Lock()
		defer s.mu.Unlock()
		delete(s.fns, id)
	}
}

func (s *subscribers) emit(ev Event) {
	s.mu.Lock()
	ids := make([]int, 0, len(s.fns))
	for id := range s.fns {
		ids = append(ids, id)
	}
	// In the order the listeners subscribed.
	for i := 1; i < len(ids); i++ {
		for j := i; j > 0 && ids[j] < ids[j-1]; j-- {
			ids[j], ids[j-1] = ids[j-1], ids[j]
		}
	}
	fns := make([]func(Event), 0, len(ids))
	for _, id := range ids {
		fns = append(fns, s.fns[id])
	}
	s.mu.Unlock()
	for _, fn := range fns {
		fn(ev)
	}
}

// newID makes a random id (the form of a version 4 UUID) for a breakpoint, a watch expression or a session. Ids only have to differ
// from each other, so the generator of math/rand/v2 is enough.
func newID() string {
	var b [16]byte
	binary.BigEndian.PutUint64(b[:8], rand.Uint64())
	binary.BigEndian.PutUint64(b[8:], rand.Uint64())
	b[6] = b[6]&0x0f | 0x40
	b[8] = b[8]&0x3f | 0x80
	h := hex.EncodeToString(b[:])
	return h[0:8] + "-" + h[8:12] + "-" + h[12:16] + "-" + h[16:20] + "-" + h[20:]
}

func itoa(n int) string { return strconv.Itoa(n) }
