package debug

import (
	"encoding/json"
	"path/filepath"

	"github.com/rm4n0s/emetos/internal/core/dap"
)

// SessionData is what one session knows about a breakpoint: the adapter's answer to setBreakpoints (or its breakpoint event) with
// what the adapter supports, which decides whether the breakpoint is supported.
//
// Ported from VS Code: src/vs/workbench/contrib/debug/common/debugModel.ts (IBreakpointSessionData)
type SessionData struct {
	dap.Breakpoint
	SessionID                         string
	SupportsConditionalBreakpoints    bool
	SupportsHitConditionalBreakpoints bool
	SupportsLogPoints                 bool
	SupportsFunctionBreakpoints       bool
}

func sessionDataOf(b dap.Breakpoint, c dap.Capabilities) *SessionData {
	return &SessionData{
		Breakpoint:                        b,
		SupportsConditionalBreakpoints:    c.SupportsConditionalBreakpoints,
		SupportsHitConditionalBreakpoints: c.SupportsHitConditionalBreakpoints,
		SupportsLogPoints:                 c.SupportsLogPoints,
		SupportsFunctionBreakpoints:       c.SupportsFunctionBreakpoints,
	}
}

// bpBase is what every kind of breakpoint has. It is guarded by the lock of the Model that owns it.
//
// Ported from VS Code: src/vs/workbench/contrib/debug/common/debugModel.ts (BaseBreakpoint)
type bpBase struct {
	id           string
	enabled      bool
	condition    string
	hitCondition string
	logMessage   string
	mode         string
	modeLabel    string

	sessionData map[string]*SessionData
	order       []string // the sessions in the order they gave data
	data        *SessionData
}

func (b *bpBase) setSessionData(sessionID string, d *SessionData) {
	if b.sessionData == nil {
		b.sessionData = map[string]*SessionData{}
	}
	if d == nil {
		delete(b.sessionData, sessionID)
		for i, s := range b.order {
			if s == sessionID {
				b.order = append(b.order[:i], b.order[i+1:]...)
				break
			}
		}
	} else {
		d.SessionID = sessionID
		if _, had := b.sessionData[sessionID]; !had {
			b.order = append(b.order, sessionID)
		}
		b.sessionData[sessionID] = d
	}
	var all, verified []*SessionData
	seen := map[[2]int]bool{}
	for _, s := range b.order {
		sd := b.sessionData[s]
		all = append(all, sd)
		if sd.Verified {
			k := [2]int{sd.Line, sd.Column}
			if !seen[k] {
				seen[k] = true
				verified = append(verified, sd)
			}
		}
	}
	switch {
	case len(verified) == 1:
		// In case several sessions verified the breakpoint with different data the data the user set is shown (a corner case).
		b.data = verified[0]
	case len(verified) > 1:
		b.data = nil
	case len(all) > 0:
		b.data = all[0]
	default:
		b.data = nil
	}
}

func (b *bpBase) adapterID(sessionID string) (int, bool) {
	if d, ok := b.sessionData[sessionID]; ok {
		return d.ID, true
	}
	return 0, false
}

func (b *bpBase) verified() bool {
	if b.data != nil {
		return b.data.Verified
	}
	return true
}

func (b *bpBase) message() string {
	if b.data != nil {
		return b.data.Message
	}
	return ""
}

func (b *bpBase) sessionsThatVerified() []string {
	var out []string
	for _, s := range b.order {
		if b.sessionData[s].Verified {
			out = append(out, s)
		}
	}
	return out
}

// lineBP is a breakpoint at a line, or at a column of a line (an inline breakpoint).
//
// Ported from VS Code: src/vs/workbench/contrib/debug/common/debugModel.ts (Breakpoint)
type lineBP struct {
	bpBase
	path        string
	line        int // 1-based
	column      int // 1-based, 0 for the whole line
	triggeredBy string
	adapterData json.RawMessage
	triggered   map[string]bool // the sessions in which the triggering breakpoint was hit
}

type funcBP struct {
	bpBase
	name string
}

type exceptionBP struct {
	bpBase
	filter               string
	label                string
	description          string
	conditionDescription string
	supportsCondition    bool
	fallback             bool
	supported            map[string]bool
}

// matches tells whether the adapter's filter is this exception breakpoint.
func (e *exceptionBP) matches(f dap.ExceptionBreakpointsFilter) bool {
	return e.filter == f.Filter && e.label == f.Label && e.supportsCondition == f.SupportsCondition &&
		e.conditionDescription == f.ConditionDescription && e.description == f.Description
}

// Breakpoint is a snapshot of a breakpoint of a line or a column, as the views and the editor read it. Line and Column are where
// the breakpoint is shown (where the adapter put it when it verified it); RequestedLine and RequestedColumn are what the user set,
// moved with the edits of the document, which is what is saved and sent.
type Breakpoint struct {
	ID string
	// Path is the file the user set the breakpoint in; SourcePath is the file the adapter verified it in (the same, usually).
	Path       string
	SourcePath string
	// Line and Column count from 1; Column is 0 for a breakpoint of the whole line.
	Line, Column int
	// EndLine and EndColumn are the end of the range the adapter verified, or 0.
	EndLine, EndColumn int
	// RequestedLine and RequestedColumn are the position the user set.
	RequestedLine, RequestedColumn int
	Enabled                        bool
	Condition, HitCondition        string
	LogMessage                     string
	Mode, ModeLabel                string
	// TriggeredBy is the id of the breakpoint whose hit enables this one ("Wait for Breakpoint").
	TriggeredBy string
	// Verified is false while an adapter has not accepted the breakpoint, and for a file with unsaved changes; Message says why.
	Verified bool
	Message  string
	// Pending is true for a triggered breakpoint no session has been asked about yet.
	Pending bool
	// Supported is false when the session that knows the breakpoint cannot do what it asks (a condition, a log message).
	Supported bool
	// AdapterData is what the adapter attached to the source of the breakpoint.
	AdapterData json.RawMessage
}

// Inline reports whether the breakpoint is at a column.
func (b Breakpoint) Inline() bool { return b.Column > 0 }

// IsLogpoint reports whether the breakpoint prints a message instead of stopping.
func (b Breakpoint) IsLogpoint() bool { return b.LogMessage != "" }

// FunctionBreakpoint is a snapshot of a breakpoint on a function name.
type FunctionBreakpoint struct {
	ID                      string
	Name                    string
	Enabled                 bool
	Condition, HitCondition string
	Verified                bool
	Message                 string
	Supported               bool
}

// ExceptionBreakpoint is a snapshot of an exception filter of an adapter, with the user's choice.
type ExceptionBreakpoint struct {
	ID                   string
	Filter               string
	Label                string
	Description          string
	ConditionDescription string
	SupportsCondition    bool
	Enabled              bool
	Condition            string
	// Fallback marks the filters shown when no session runs: those of the session that was focused last.
	Fallback bool
}

// BreakpointData describes a breakpoint to add.
type BreakpointData struct {
	// ID is kept when it is not empty (a restored breakpoint); else one is made.
	ID           string
	Line, Column int
	Disabled     bool
	Condition    string
	HitCondition string
	LogMessage   string
	Mode         string
	ModeLabel    string
	TriggeredBy  string
}

// BreakpointUpdate describes what to change in a breakpoint: nil (and 0 for the line) leave a member as it is.
type BreakpointUpdate struct {
	Line         int
	Column       *int
	Condition    *string
	HitCondition *string
	LogMessage   *string
	Mode         *string
	ModeLabel    *string
	TriggeredBy  *string
}

// BreakpointFilter selects breakpoints; the zero value selects all.
type BreakpointFilter struct {
	// Path matches the path where the breakpoint is shown (SourcePath), OriginalPath the path the user set it in.
	Path, OriginalPath string
	Line, Column       int
	// EnabledOnly drops the disabled ones, and all when breakpoints are not activated.
	EnabledOnly bool
	// TriggeredOnly keeps the breakpoints that wait for another.
	TriggeredOnly bool
}

// FunctionBreakpointOptions describe a function breakpoint to add.
type FunctionBreakpointOptions struct {
	Name                    string
	Disabled                bool
	Condition, HitCondition string
}

func sp(s string) *string { return &s }
func ip(n int) *int       { return &n }

func baseName(path string) string { return filepath.Base(path) }
