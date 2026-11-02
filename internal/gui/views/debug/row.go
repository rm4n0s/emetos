package debug

import (
	"strconv"

	"github.com/rm4n0s/emetos/internal/core/debug"
)

// RowKind says what a row of a view shows.
type RowKind int

// The kinds of rows.
const (
	// RowMessage is a line of text (a failure to read the scopes, the message of a failed stack trace).
	RowMessage RowKind = iota + 1
	// RowScope is a scope of the focused frame, RowVariable a variable or property, RowExpression a watch expression.
	RowScope
	RowVariable
	RowExpression
	// RowSession, RowThread and RowFrame are the rows of the Call Stack.
	RowSession
	RowThread
	RowFrame
	// RowShowMore is "Show N More Stack Frames" (deemphasized frames that are collapsed) and RowLoadMore "Load More Stack Frames".
	RowShowMore
	RowLoadMore
	// RowException, RowFunctionBreakpoint and RowBreakpoint are the rows of the Breakpoints.
	RowException
	RowFunctionBreakpoint
	RowBreakpoint
	// RowSource is a loaded script.
	RowSource
)

// Row is the data of a row of a view: what the commands of its menus work on. Exactly the member of its Kind is set.
type Row struct {
	Kind RowKind
	// Key is the key of the row in its tree.
	Key string
	// Text is the text of a message, or the label of RowShowMore and RowLoadMore.
	Text string

	Scope      *debug.Scope
	Variable   *debug.Variable
	Expression *debug.Expression
	Session    *debug.Session
	Thread     *debug.Thread
	Frame      *debug.StackFrame
	// Hidden are the deemphasized frames a RowShowMore stands for.
	Hidden     []*debug.StackFrame
	Exception  debug.ExceptionBreakpoint
	Function   debug.FunctionBreakpoint
	Breakpoint debug.Breakpoint
	Source     debug.Source
	// Parent is the node a variable is a child of (a scope, a variable or an expression), for Set Value.
	Parent debug.Node
}

// Node is the value node of the row (a scope, a variable or an expression), or nil.
func (r Row) Node() debug.Node {
	switch r.Kind {
	case RowScope:
		return r.Scope
	case RowVariable:
		return r.Variable
	case RowExpression:
		return r.Expression
	}
	return nil
}

// ItemType is the value of the context key that names the kind of the focused row of the Call Stack (callStackItemType), of the
// Watch (watchItemType) or of the Breakpoints (breakpointItemType): "session", "thread", "stackFrame"; "expression", "variable";
// "breakpoint", "functionBreakpoint", "exceptionBreakpoint".
func (r Row) ItemType() string {
	switch r.Kind {
	case RowSession:
		return "session"
	case RowThread:
		return "thread"
	case RowFrame:
		return "stackFrame"
	case RowExpression:
		return "expression"
	case RowVariable, RowScope:
		return "variable"
	case RowBreakpoint:
		return "breakpoint"
	case RowFunctionBreakpoint:
		return "functionBreakpoint"
	case RowException:
		return "exceptionBreakpoint"
	}
	return ""
}

// CallStackArgs is the argument of a command run for a row of the Call Stack (VS Code's CallStackContext): the ids of the session,
// the thread and the frame.
type CallStackArgs struct {
	SessionID string
	ThreadID  int
	FrameID   int
}

// Args is the argument a command of a menu is run with for the row.
func (r Row) Args() any {
	switch r.Kind {
	case RowSession:
		return CallStackArgs{SessionID: r.Session.ID()}
	case RowThread:
		return CallStackArgs{SessionID: r.Thread.Session().ID(), ThreadID: r.Thread.ID}
	case RowFrame:
		return CallStackArgs{SessionID: r.Frame.Thread.Session().ID(), ThreadID: r.Frame.Thread.ID, FrameID: r.Frame.ID}
	}
	return r
}

// keyOf makes the key of a node of a tree unique: the id, and a counter for the id that was seen already (two variables of the same
// name under a parent).
type keys map[string]int

func (k keys) of(id string) string {
	n := k[id]
	k[id] = n + 1
	if n == 0 {
		return id
	}
	return id + "#" + strconv.Itoa(n)
}
