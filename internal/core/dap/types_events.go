package dap

import (
	"encoding/json"

	"github.com/rm4n0s/errors"
)

// Ported from VS Code: src/vs/workbench/contrib/debug/common/debugProtocol.d.ts

// The names of the events of the adapter.
const (
	EventInitialized    = "initialized"
	EventStopped        = "stopped"
	EventContinued      = "continued"
	EventExited         = "exited"
	EventTerminated     = "terminated"
	EventThread         = "thread"
	EventOutput         = "output"
	EventBreakpoint     = "breakpoint"
	EventModule         = "module"
	EventLoadedSource   = "loadedSource"
	EventProcess        = "process"
	EventCapabilities   = "capabilities"
	EventProgressStart  = "progressStart"
	EventProgressUpdate = "progressUpdate"
	EventProgressEnd    = "progressEnd"
	EventInvalidated    = "invalidated"
	EventMemory         = "memory"
)

// The reasons of a stopped event that the protocol names (an adapter may send others).
const (
	StopStep                  = "step"
	StopBreakpoint            = "breakpoint"
	StopException             = "exception"
	StopPause                 = "pause"
	StopEntry                 = "entry"
	StopGoto                  = "goto"
	StopFunctionBreakpoint    = "function breakpoint"
	StopDataBreakpoint        = "data breakpoint"
	StopInstructionBreakpoint = "instruction breakpoint"
)

// The categories of an output event.
const (
	OutputConsole   = "console"
	OutputImportant = "important"
	OutputStdout    = "stdout"
	OutputStderr    = "stderr"
	OutputTelemetry = "telemetry"
)

// StoppedEventBody is the body of the stopped event: the debuggee stopped. ThreadID zero means the event has none.
type StoppedEventBody struct {
	Reason            string `json:"reason"`
	Description       string `json:"description,omitempty"`
	ThreadID          int    `json:"threadId,omitempty"`
	PreserveFocusHint bool   `json:"preserveFocusHint,omitempty"`
	Text              string `json:"text,omitempty"`
	AllThreadsStopped bool   `json:"allThreadsStopped,omitempty"`
	HitBreakpointIDs  []int  `json:"hitBreakpointIds,omitempty"`
}

// ContinuedEventBody is the body of the continued event.
type ContinuedEventBody struct {
	ThreadID            int  `json:"threadId"`
	AllThreadsContinued bool `json:"allThreadsContinued,omitempty"`
}

// ExitedEventBody is the body of the exited event: the debuggee exited.
type ExitedEventBody struct {
	ExitCode int `json:"exitCode"`
}

// TerminatedEventBody is the body of the terminated event (its body is optional); Restart is what the client passes to
// the next launch or attach.
type TerminatedEventBody struct {
	Restart json.RawMessage `json:"restart,omitempty"`
}

// ThreadEventBody is the body of the thread event.
type ThreadEventBody struct {
	Reason   string `json:"reason"` // started, exited
	ThreadID int    `json:"threadId"`
}

// OutputEventBody is the body of the output event.
type OutputEventBody struct {
	Category           string          `json:"category,omitempty"`
	Output             string          `json:"output"`
	Group              string          `json:"group,omitempty"` // start, startCollapsed, end
	VariablesReference int             `json:"variablesReference,omitempty"`
	Source             *Source         `json:"source,omitempty"`
	Line               int             `json:"line,omitempty"`
	Column             int             `json:"column,omitempty"`
	Data               json.RawMessage `json:"data,omitempty"`
	LocationReference  int             `json:"locationReference,omitempty"`
}

// BreakpointEventBody is the body of the breakpoint event.
type BreakpointEventBody struct {
	Reason     string     `json:"reason"` // changed, new, removed
	Breakpoint Breakpoint `json:"breakpoint"`
}

// ModuleEventBody is the body of the module event.
type ModuleEventBody struct {
	Reason string `json:"reason"` // new, changed, removed
	Module Module `json:"module"`
}

// LoadedSourceEventBody is the body of the loadedSource event.
type LoadedSourceEventBody struct {
	Reason string `json:"reason"` // new, changed, removed
	Source Source `json:"source"`
}

// ProcessEventBody is the body of the process event.
type ProcessEventBody struct {
	Name            string `json:"name"`
	SystemProcessID int    `json:"systemProcessId,omitempty"`
	IsLocalProcess  bool   `json:"isLocalProcess,omitempty"`
	StartMethod     string `json:"startMethod,omitempty"` // launch, attach, attachForSuspendedLaunch
	PointerSize     int    `json:"pointerSize,omitempty"`
}

// CapabilitiesEventBody is the body of the capabilities event: only the capabilities that changed.
type CapabilitiesEventBody struct {
	Capabilities Capabilities `json:"capabilities"`
}

// ProgressStartEventBody is the body of the progressStart event.
type ProgressStartEventBody struct {
	ProgressID  string   `json:"progressId"`
	Title       string   `json:"title"`
	RequestID   int      `json:"requestId,omitempty"`
	Cancellable bool     `json:"cancellable,omitempty"`
	Message     string   `json:"message,omitempty"`
	Percentage  *float64 `json:"percentage,omitempty"`
}

// ProgressUpdateEventBody is the body of the progressUpdate event.
type ProgressUpdateEventBody struct {
	ProgressID string   `json:"progressId"`
	Message    string   `json:"message,omitempty"`
	Percentage *float64 `json:"percentage,omitempty"`
}

// ProgressEndEventBody is the body of the progressEnd event.
type ProgressEndEventBody struct {
	ProgressID string `json:"progressId"`
	Message    string `json:"message,omitempty"`
}

// InvalidatedEventBody is the body of the invalidated event; Areas are "all", "stacks", "threads", "variables" or others.
type InvalidatedEventBody struct {
	Areas        []string `json:"areas,omitempty"`
	ThreadID     int      `json:"threadId,omitempty"`
	StackFrameID int      `json:"stackFrameId,omitempty"`
}

// MemoryEventBody is the body of the memory event.
type MemoryEventBody struct {
	MemoryReference string `json:"memoryReference"`
	Offset          int    `json:"offset"`
	Count           int    `json:"count"`
}

// ParseEvent decodes the body of an event into the type of its name: *StoppedEventBody, *ContinuedEventBody,
// *ExitedEventBody, *TerminatedEventBody, *ThreadEventBody, *OutputEventBody, *BreakpointEventBody, *ModuleEventBody,
// *LoadedSourceEventBody, *ProcessEventBody, *CapabilitiesEventBody, *ProgressStartEventBody, *ProgressUpdateEventBody,
// *ProgressEndEventBody, *InvalidatedEventBody or *MemoryEventBody; *struct{} for initialized. An event the protocol does not
// define (an adapter's own) gives its body as a json.RawMessage. A body that does not fit fails with the tag
// DapBodyInvalid.
func ParseEvent(e Event) (any, error) {
	var out any
	switch e.Event {
	case EventInitialized:
		out = &struct{}{}
	case EventStopped:
		out = &StoppedEventBody{}
	case EventContinued:
		out = &ContinuedEventBody{}
	case EventExited:
		out = &ExitedEventBody{}
	case EventTerminated:
		out = &TerminatedEventBody{}
	case EventThread:
		out = &ThreadEventBody{}
	case EventOutput:
		out = &OutputEventBody{}
	case EventBreakpoint:
		out = &BreakpointEventBody{}
	case EventModule:
		out = &ModuleEventBody{}
	case EventLoadedSource:
		out = &LoadedSourceEventBody{}
	case EventProcess:
		out = &ProcessEventBody{}
	case EventCapabilities:
		out = &CapabilitiesEventBody{}
	case EventProgressStart:
		out = &ProgressStartEventBody{}
	case EventProgressUpdate:
		out = &ProgressUpdateEventBody{}
	case EventProgressEnd:
		out = &ProgressEndEventBody{}
	case EventInvalidated:
		out = &InvalidatedEventBody{}
	case EventMemory:
		out = &MemoryEventBody{}
	default:
		return e.Body, nil
	}
	if len(e.Body) == 0 || string(e.Body) == "null" {
		return out, nil
	}
	if err := json.Unmarshal(e.Body, out); err != nil {
		return nil, errors.NewErr("DapBodyInvalid", err).SetMetadata("event", e.Event)
	}
	return out, nil
}
