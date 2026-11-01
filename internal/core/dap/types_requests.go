package dap

import "encoding/json"

// Ported from VS Code: src/vs/workbench/contrib/debug/common/debugProtocol.d.ts

// The commands of the requests. The client sends the ones of the first group; the adapter sends the two of the second.
const (
	CmdCancel                    = "cancel"
	CmdInitialize                = "initialize"
	CmdConfigurationDone         = "configurationDone"
	CmdLaunch                    = "launch"
	CmdAttach                    = "attach"
	CmdRestart                   = "restart"
	CmdDisconnect                = "disconnect"
	CmdTerminate                 = "terminate"
	CmdBreakpointLocations       = "breakpointLocations"
	CmdSetBreakpoints            = "setBreakpoints"
	CmdSetFunctionBreakpoints    = "setFunctionBreakpoints"
	CmdSetExceptionBreakpoints   = "setExceptionBreakpoints"
	CmdDataBreakpointInfo        = "dataBreakpointInfo"
	CmdSetDataBreakpoints        = "setDataBreakpoints"
	CmdSetInstructionBreakpoints = "setInstructionBreakpoints"
	CmdContinue                  = "continue"
	CmdNext                      = "next"
	CmdStepIn                    = "stepIn"
	CmdStepOut                   = "stepOut"
	CmdStepBack                  = "stepBack"
	CmdReverseContinue           = "reverseContinue"
	CmdRestartFrame              = "restartFrame"
	CmdGoto                      = "goto"
	CmdPause                     = "pause"
	CmdStackTrace                = "stackTrace"
	CmdScopes                    = "scopes"
	CmdVariables                 = "variables"
	CmdSetVariable               = "setVariable"
	CmdSource                    = "source"
	CmdThreads                   = "threads"
	CmdTerminateThreads          = "terminateThreads"
	CmdModules                   = "modules"
	CmdLoadedSources             = "loadedSources"
	CmdEvaluate                  = "evaluate"
	CmdSetExpression             = "setExpression"
	CmdStepInTargets             = "stepInTargets"
	CmdGotoTargets               = "gotoTargets"
	CmdCompletions               = "completions"
	CmdExceptionInfo             = "exceptionInfo"
	CmdReadMemory                = "readMemory"
	CmdWriteMemory               = "writeMemory"
	CmdDisassemble               = "disassemble"
	CmdLocations                 = "locations"

	// ReverseRunInTerminal is the request of the adapter to run a command in a terminal.
	ReverseRunInTerminal = "runInTerminal"
	// ReverseStartDebugging is the request of the adapter to start a child session.
	ReverseStartDebugging = "startDebugging"
)

// CancelArguments are the arguments of cancel.
type CancelArguments struct {
	RequestID  int    `json:"requestId,omitempty"`
	ProgressID string `json:"progressId,omitempty"`
}

// RunInTerminalRequestArguments are the arguments of the adapter's runInTerminal request.
type RunInTerminalRequestArguments struct {
	Kind                        string             `json:"kind,omitempty"` // integrated, external
	Title                       string             `json:"title,omitempty"`
	Cwd                         string             `json:"cwd"`
	Args                        []string           `json:"args"`
	Env                         map[string]*string `json:"env,omitempty"` // a null value removes the variable
	ArgsCanBeInterpretedByShell bool               `json:"argsCanBeInterpretedByShell,omitempty"`
}

// RunInTerminalResponseBody is the answer to runInTerminal.
type RunInTerminalResponseBody struct {
	ProcessID      int `json:"processId,omitempty"`
	ShellProcessID int `json:"shellProcessId,omitempty"`
}

// StartDebuggingRequestArguments are the arguments of the adapter's startDebugging request.
type StartDebuggingRequestArguments struct {
	Configuration map[string]json.RawMessage `json:"configuration"`
	Request       string                     `json:"request"` // launch, attach
}

// InitializeRequestArguments are the arguments of initialize: who the client is and what it supports.
type InitializeRequestArguments struct {
	ClientID                            string `json:"clientID,omitempty"`
	ClientName                          string `json:"clientName,omitempty"`
	AdapterID                           string `json:"adapterID"`
	Locale                              string `json:"locale,omitempty"`
	LinesStartAt1                       bool   `json:"linesStartAt1"`
	ColumnsStartAt1                     bool   `json:"columnsStartAt1"`
	PathFormat                          string `json:"pathFormat,omitempty"` // path, uri
	SupportsVariableType                bool   `json:"supportsVariableType,omitempty"`
	SupportsVariablePaging              bool   `json:"supportsVariablePaging,omitempty"`
	SupportsRunInTerminalRequest        bool   `json:"supportsRunInTerminalRequest,omitempty"`
	SupportsMemoryReferences            bool   `json:"supportsMemoryReferences,omitempty"`
	SupportsProgressReporting           bool   `json:"supportsProgressReporting,omitempty"`
	SupportsInvalidatedEvent            bool   `json:"supportsInvalidatedEvent,omitempty"`
	SupportsMemoryEvent                 bool   `json:"supportsMemoryEvent,omitempty"`
	SupportsArgsCanBeInterpretedByShell bool   `json:"supportsArgsCanBeInterpretedByShell,omitempty"`
	SupportsStartDebuggingRequest       bool   `json:"supportsStartDebuggingRequest,omitempty"`
	SupportsANSIStyling                 bool   `json:"supportsANSIStyling,omitempty"`
}

// RestartArguments are the arguments of restart; Arguments are the arguments of the launch or attach that replace the
// original ones.
type RestartArguments struct {
	Arguments json.RawMessage `json:"arguments,omitempty"`
}

// DisconnectArguments are the arguments of disconnect.
type DisconnectArguments struct {
	Restart           bool  `json:"restart,omitempty"`
	TerminateDebuggee *bool `json:"terminateDebuggee,omitempty"`
	SuspendDebuggee   *bool `json:"suspendDebuggee,omitempty"`
}

// TerminateArguments are the arguments of terminate.
type TerminateArguments struct {
	Restart bool `json:"restart,omitempty"`
}

// BreakpointLocationsArguments are the arguments of breakpointLocations.
type BreakpointLocationsArguments struct {
	Source    Source `json:"source"`
	Line      int    `json:"line"`
	Column    int    `json:"column,omitempty"`
	EndLine   int    `json:"endLine,omitempty"`
	EndColumn int    `json:"endColumn,omitempty"`
}

// BreakpointLocationsResponseBody is the answer to breakpointLocations.
type BreakpointLocationsResponseBody struct {
	Breakpoints []BreakpointLocation `json:"breakpoints"`
}

// SetBreakpointsArguments are the arguments of setBreakpoints: all the breakpoints of one source, which replace the
// earlier ones (an empty list clears them).
type SetBreakpointsArguments struct {
	Source         Source             `json:"source"`
	Breakpoints    []SourceBreakpoint `json:"breakpoints"`
	SourceModified bool               `json:"sourceModified,omitempty"`
}

// BreakpointsResponseBody is the answer to setBreakpoints, setFunctionBreakpoints, setDataBreakpoints and
// setInstructionBreakpoints: one Breakpoint for each of the requested ones, in the same order.
type BreakpointsResponseBody struct {
	Breakpoints []Breakpoint `json:"breakpoints"`
}

// SetFunctionBreakpointsArguments are the arguments of setFunctionBreakpoints.
type SetFunctionBreakpointsArguments struct {
	Breakpoints []FunctionBreakpoint `json:"breakpoints"`
}

// SetExceptionBreakpointsArguments are the arguments of setExceptionBreakpoints.
type SetExceptionBreakpointsArguments struct {
	Filters          []string                 `json:"filters"`
	FilterOptions    []ExceptionFilterOptions `json:"filterOptions,omitempty"`
	ExceptionOptions []ExceptionOptions       `json:"exceptionOptions,omitempty"`
}

// SetExceptionBreakpointsResponseBody is the answer to setExceptionBreakpoints.
type SetExceptionBreakpointsResponseBody struct {
	Breakpoints []Breakpoint `json:"breakpoints,omitempty"`
}

// DataBreakpointInfoArguments are the arguments of dataBreakpointInfo.
type DataBreakpointInfoArguments struct {
	VariablesReference int    `json:"variablesReference,omitempty"`
	Name               string `json:"name"`
	FrameID            int    `json:"frameId,omitempty"`
	Bytes              int    `json:"bytes,omitempty"`
	AsAddress          bool   `json:"asAddress,omitempty"`
	Mode               string `json:"mode,omitempty"`
}

// DataBreakpointInfoResponseBody is the answer to dataBreakpointInfo; a nil DataID means the data cannot be watched.
type DataBreakpointInfoResponseBody struct {
	DataID      *string  `json:"dataId"`
	Description string   `json:"description"`
	AccessTypes []string `json:"accessTypes,omitempty"`
	CanPersist  bool     `json:"canPersist,omitempty"`
}

// SetDataBreakpointsArguments are the arguments of setDataBreakpoints.
type SetDataBreakpointsArguments struct {
	Breakpoints []DataBreakpoint `json:"breakpoints"`
}

// SetInstructionBreakpointsArguments are the arguments of setInstructionBreakpoints.
type SetInstructionBreakpointsArguments struct {
	Breakpoints []InstructionBreakpoint `json:"breakpoints"`
}

// ContinueArguments are the arguments of continue.
type ContinueArguments struct {
	ThreadID     int  `json:"threadId"`
	SingleThread bool `json:"singleThread,omitempty"`
}

// ContinueResponseBody is the answer to continue; AllThreadsContinued is true when it is absent (a nil pointer).
type ContinueResponseBody struct {
	AllThreadsContinued *bool `json:"allThreadsContinued,omitempty"`
}

// StepArguments are the arguments of next, stepOut and stepBack, and (with TargetID) of stepIn.
type StepArguments struct {
	ThreadID     int    `json:"threadId"`
	SingleThread bool   `json:"singleThread,omitempty"`
	TargetID     int    `json:"targetId,omitempty"` // stepIn only
	Granularity  string `json:"granularity,omitempty"`
}

// ReverseContinueArguments are the arguments of reverseContinue.
type ReverseContinueArguments = ContinueArguments

// RestartFrameArguments are the arguments of restartFrame.
type RestartFrameArguments struct {
	FrameID int `json:"frameId"`
}

// GotoArguments are the arguments of goto.
type GotoArguments struct {
	ThreadID int `json:"threadId"`
	TargetID int `json:"targetId"`
}

// PauseArguments are the arguments of pause.
type PauseArguments struct {
	ThreadID int `json:"threadId"`
}

// StackTraceArguments are the arguments of stackTrace; Levels zero means all.
type StackTraceArguments struct {
	ThreadID   int               `json:"threadId"`
	StartFrame int               `json:"startFrame,omitempty"`
	Levels     int               `json:"levels,omitempty"`
	Format     *StackFrameFormat `json:"format,omitempty"`
}

// StackTraceResponseBody is the answer to stackTrace.
type StackTraceResponseBody struct {
	StackFrames []StackFrame `json:"stackFrames"`
	TotalFrames int          `json:"totalFrames,omitempty"`
}

// ScopesArguments are the arguments of scopes.
type ScopesArguments struct {
	FrameID int `json:"frameId"`
}

// ScopesResponseBody is the answer to scopes.
type ScopesResponseBody struct {
	Scopes []Scope `json:"scopes"`
}

// VariablesArguments are the arguments of variables; Count zero means all.
type VariablesArguments struct {
	VariablesReference int          `json:"variablesReference"`
	Filter             string       `json:"filter,omitempty"` // indexed, named
	Start              int          `json:"start,omitempty"`
	Count              int          `json:"count,omitempty"`
	Format             *ValueFormat `json:"format,omitempty"`
}

// VariablesResponseBody is the answer to variables.
type VariablesResponseBody struct {
	Variables []Variable `json:"variables"`
}

// SetVariableArguments are the arguments of setVariable.
type SetVariableArguments struct {
	VariablesReference int          `json:"variablesReference"`
	Name               string       `json:"name"`
	Value              string       `json:"value"`
	Format             *ValueFormat `json:"format,omitempty"`
}

// SetVariableResponseBody is the answer to setVariable.
type SetVariableResponseBody struct {
	Value                  string `json:"value"`
	Type                   string `json:"type,omitempty"`
	VariablesReference     int    `json:"variablesReference,omitempty"`
	NamedVariables         int    `json:"namedVariables,omitempty"`
	IndexedVariables       int    `json:"indexedVariables,omitempty"`
	MemoryReference        string `json:"memoryReference,omitempty"`
	ValueLocationReference int    `json:"valueLocationReference,omitempty"`
}

// SourceArguments are the arguments of source.
type SourceArguments struct {
	Source          *Source `json:"source,omitempty"`
	SourceReference int     `json:"sourceReference"`
}

// SourceResponseBody is the answer to source.
type SourceResponseBody struct {
	Content  string `json:"content"`
	MimeType string `json:"mimeType,omitempty"`
}

// ThreadsResponseBody is the answer to threads.
type ThreadsResponseBody struct {
	Threads []Thread `json:"threads"`
}

// TerminateThreadsArguments are the arguments of terminateThreads.
type TerminateThreadsArguments struct {
	ThreadIDs []int `json:"threadIds,omitempty"`
}

// ModulesArguments are the arguments of modules.
type ModulesArguments struct {
	StartModule int `json:"startModule,omitempty"`
	ModuleCount int `json:"moduleCount,omitempty"`
}

// ModulesResponseBody is the answer to modules.
type ModulesResponseBody struct {
	Modules      []Module `json:"modules"`
	TotalModules int      `json:"totalModules,omitempty"`
}

// LoadedSourcesResponseBody is the answer to loadedSources.
type LoadedSourcesResponseBody struct {
	Sources []Source `json:"sources"`
}

// EvaluateArguments are the arguments of evaluate.
type EvaluateArguments struct {
	Expression string       `json:"expression"`
	FrameID    int          `json:"frameId,omitempty"`
	Line       int          `json:"line,omitempty"`
	Column     int          `json:"column,omitempty"`
	Source     *Source      `json:"source,omitempty"`
	Context    string       `json:"context,omitempty"` // the Context... constants
	Format     *ValueFormat `json:"format,omitempty"`
}

// EvaluateResponseBody is the answer to evaluate.
type EvaluateResponseBody struct {
	Result                 string                    `json:"result"`
	Type                   string                    `json:"type,omitempty"`
	PresentationHint       *VariablePresentationHint `json:"presentationHint,omitempty"`
	VariablesReference     int                       `json:"variablesReference"`
	NamedVariables         int                       `json:"namedVariables,omitempty"`
	IndexedVariables       int                       `json:"indexedVariables,omitempty"`
	MemoryReference        string                    `json:"memoryReference,omitempty"`
	ValueLocationReference int                       `json:"valueLocationReference,omitempty"`
}

// SetExpressionArguments are the arguments of setExpression.
type SetExpressionArguments struct {
	Expression string       `json:"expression"`
	Value      string       `json:"value"`
	FrameID    int          `json:"frameId,omitempty"`
	Format     *ValueFormat `json:"format,omitempty"`
}

// SetExpressionResponseBody is the answer to setExpression.
type SetExpressionResponseBody struct {
	Value                  string                    `json:"value"`
	Type                   string                    `json:"type,omitempty"`
	PresentationHint       *VariablePresentationHint `json:"presentationHint,omitempty"`
	VariablesReference     int                       `json:"variablesReference,omitempty"`
	NamedVariables         int                       `json:"namedVariables,omitempty"`
	IndexedVariables       int                       `json:"indexedVariables,omitempty"`
	MemoryReference        string                    `json:"memoryReference,omitempty"`
	ValueLocationReference int                       `json:"valueLocationReference,omitempty"`
}

// StepInTargetsArguments are the arguments of stepInTargets.
type StepInTargetsArguments struct {
	FrameID int `json:"frameId"`
}

// StepInTargetsResponseBody is the answer to stepInTargets.
type StepInTargetsResponseBody struct {
	Targets []StepInTarget `json:"targets"`
}

// GotoTargetsArguments are the arguments of gotoTargets.
type GotoTargetsArguments struct {
	Source Source `json:"source"`
	Line   int    `json:"line"`
	Column int    `json:"column,omitempty"`
}

// GotoTargetsResponseBody is the answer to gotoTargets.
type GotoTargetsResponseBody struct {
	Targets []GotoTarget `json:"targets"`
}

// CompletionsArguments are the arguments of completions.
type CompletionsArguments struct {
	FrameID int    `json:"frameId,omitempty"`
	Text    string `json:"text"`
	Column  int    `json:"column"`
	Line    int    `json:"line,omitempty"`
}

// CompletionsResponseBody is the answer to completions.
type CompletionsResponseBody struct {
	Targets []CompletionItem `json:"targets"`
}

// ExceptionInfoArguments are the arguments of exceptionInfo.
type ExceptionInfoArguments struct {
	ThreadID int `json:"threadId"`
}

// ExceptionInfoResponseBody is the answer to exceptionInfo.
type ExceptionInfoResponseBody struct {
	ExceptionID string            `json:"exceptionId"`
	Description string            `json:"description,omitempty"`
	BreakMode   string            `json:"breakMode"`
	Details     *ExceptionDetails `json:"details,omitempty"`
}

// ReadMemoryArguments are the arguments of readMemory.
type ReadMemoryArguments struct {
	MemoryReference string `json:"memoryReference"`
	Offset          int    `json:"offset,omitempty"`
	Count           int    `json:"count"`
}

// ReadMemoryResponseBody is the answer to readMemory; Data is base64.
type ReadMemoryResponseBody struct {
	Address         string `json:"address"`
	UnreadableBytes int    `json:"unreadableBytes,omitempty"`
	Data            string `json:"data,omitempty"`
}

// WriteMemoryArguments are the arguments of writeMemory; Data is base64.
type WriteMemoryArguments struct {
	MemoryReference string `json:"memoryReference"`
	Offset          int    `json:"offset,omitempty"`
	AllowPartial    bool   `json:"allowPartial,omitempty"`
	Data            string `json:"data"`
}

// WriteMemoryResponseBody is the answer to writeMemory.
type WriteMemoryResponseBody struct {
	Offset       int `json:"offset,omitempty"`
	BytesWritten int `json:"bytesWritten,omitempty"`
}

// DisassembleArguments are the arguments of disassemble.
type DisassembleArguments struct {
	MemoryReference   string `json:"memoryReference"`
	Offset            int    `json:"offset,omitempty"`
	InstructionOffset int    `json:"instructionOffset,omitempty"`
	InstructionCount  int    `json:"instructionCount"`
	ResolveSymbols    bool   `json:"resolveSymbols,omitempty"`
}

// DisassembleResponseBody is the answer to disassemble.
type DisassembleResponseBody struct {
	Instructions []DisassembledInstruction `json:"instructions"`
}

// LocationsArguments are the arguments of locations.
type LocationsArguments struct {
	LocationReference int `json:"locationReference"`
}

// LocationsResponseBody is the answer to locations.
type LocationsResponseBody struct {
	Source    Source `json:"source"`
	Line      int    `json:"line"`
	Column    int    `json:"column,omitempty"`
	EndLine   int    `json:"endLine,omitempty"`
	EndColumn int    `json:"endColumn,omitempty"`
}
