package dap

import (
	"encoding/json"
	"regexp"
	"strconv"
)

// The types of this file and of types_requests.go, types_events.go and capabilities.go are ported from VS Code's
// debugProtocol.d.ts (Debug Adapter Protocol 1.65 and later: the file has supportsANSIStyling and the location
// references of 1.69).
//
// Ported from VS Code: src/vs/workbench/contrib/debug/common/debugProtocol.d.ts
//
// Conventions of the port:
//   - An optional member is omitted from the JSON when it is empty. An optional number whose zero is not a valid value
//     of the protocol (ids, references, positions: lines and columns start at 1 here) is an int that is omitted when it is
//     zero, and reads as zero when it is absent. An optional boolean is a bool, except where "absent" and "false"
//     differ (Disconnect's terminateDebuggee, the allThreadsContinued of a continue response): there it is a *bool.
//   - A member the protocol types as "any" is a json.RawMessage.
//   - The body of a response or event is decoded by the caller (Call, ParseEvent) from the raw JSON the client keeps,
//     because the client cannot know the shape before it knows the command.

// The values of the type member of a message.
const (
	TypeRequest  = "request"
	TypeResponse = "response"
	TypeEvent    = "event"
)

// Response messages that mean something (the message member of an unsuccessful response).
const (
	// MessageCancelled is the message of a response to a request that was cancelled.
	MessageCancelled = "cancelled"
	// MessageNotStopped is the message of a response to a request that needs the adapter to be stopped.
	MessageNotStopped = "notStopped"
)

// Response is the response to a request of the client, or (as RawResponse of a reverse request) the answer the client
// gives. A failed response is also returned by Client.Request, together with the error.
type Response struct {
	// Seq is the sequence number of the message.
	Seq int `json:"seq"`
	// RequestSeq is the sequence number of the request it answers.
	RequestSeq int `json:"request_seq"`
	// Success says whether the request succeeded; when it did not, Message says why in short form and Body may hold an
	// ErrorBody.
	Success bool `json:"success"`
	// Command is the command of the request.
	Command string `json:"command"`
	// Message is the error in short form of a failed response ("cancelled" and "notStopped" are predefined).
	Message string `json:"message,omitempty"`
	// Body is the result, as JSON.
	Body json.RawMessage `json:"body,omitempty"`
}

// Event is an event of the adapter.
type Event struct {
	// Seq is the sequence number of the message.
	Seq int `json:"seq"`
	// Event is the name of the event (the Event... constants).
	Event string `json:"event"`
	// Body is the data of the event, as JSON; see ParseEvent.
	Body json.RawMessage `json:"body,omitempty"`
}

// ErrorBody is the body of a failed response.
type ErrorBody struct {
	// Error is the structured description of the failure.
	Error *Message `json:"error,omitempty"`
}

// Message is a structured message of the adapter (the error of a failed response). Its format has {name} placeholders
// that Variables fill; see FormatMessage.
type Message struct {
	ID            int               `json:"id"`
	Format        string            `json:"format"`
	Variables     map[string]string `json:"variables,omitempty"`
	SendTelemetry bool              `json:"sendTelemetry,omitempty"`
	ShowUser      bool              `json:"showUser,omitempty"`
	URL           string            `json:"url,omitempty"`
	URLLabel      string            `json:"urlLabel,omitempty"`
}

var placeholder = regexp.MustCompile(`\{([^}]+)\}`)

// FormatMessage is the text of a message with its placeholders replaced by its variables; a placeholder with no variable
// stays as it is.
//
// Ported from VS Code: src/vs/workbench/contrib/debug/common/debugUtils.ts (formatPII)
func FormatMessage(m Message) string {
	return placeholder.ReplaceAllStringFunc(m.Format, func(match string) string {
		if v, ok := m.Variables[match[1:len(match)-1]]; ok {
			return v
		}
		return match
	})
}

// ModuleID is the id of a module, which the protocol allows to be a number or a string. It is kept as text.
type ModuleID string

// UnmarshalJSON reads a number or a string.
func (m *ModuleID) UnmarshalJSON(data []byte) error {
	if len(data) > 0 && data[0] == '"' {
		var s string
		if err := json.Unmarshal(data, &s); err != nil {
			return err
		}
		*m = ModuleID(s)
		return nil
	}
	var n json.Number
	if err := json.Unmarshal(data, &n); err != nil {
		return err
	}
	*m = ModuleID(n.String())
	return nil
}

// MarshalJSON writes a number when the id is one, else a string.
func (m ModuleID) MarshalJSON() ([]byte, error) {
	if _, err := strconv.ParseInt(string(m), 10, 64); err == nil {
		return []byte(m), nil
	}
	return json.Marshal(string(m))
}

// Source is a source: a file (Path) or something the adapter serves (SourceReference).
type Source struct {
	Name             string          `json:"name,omitempty"`
	Path             string          `json:"path,omitempty"`
	SourceReference  int             `json:"sourceReference,omitempty"`
	PresentationHint string          `json:"presentationHint,omitempty"` // normal, emphasize, deemphasize
	Origin           string          `json:"origin,omitempty"`
	Sources          []Source        `json:"sources,omitempty"`
	AdapterData      json.RawMessage `json:"adapterData,omitempty"`
	Checksums        []Checksum      `json:"checksums,omitempty"`
}

// Checksum is the checksum of a source.
type Checksum struct {
	Algorithm string `json:"algorithm"` // MD5, SHA1, SHA256, timestamp
	Checksum  string `json:"checksum"`
}

// Thread is a thread of the debuggee.
type Thread struct {
	ID   int    `json:"id"`
	Name string `json:"name"`
}

// StackFrame is a frame of a stack trace.
type StackFrame struct {
	ID                          int      `json:"id"`
	Name                        string   `json:"name"`
	Source                      *Source  `json:"source,omitempty"`
	Line                        int      `json:"line"`
	Column                      int      `json:"column"`
	EndLine                     int      `json:"endLine,omitempty"`
	EndColumn                   int      `json:"endColumn,omitempty"`
	CanRestart                  bool     `json:"canRestart,omitempty"`
	InstructionPointerReference string   `json:"instructionPointerReference,omitempty"`
	ModuleID                    ModuleID `json:"moduleId,omitempty"`
	PresentationHint            string   `json:"presentationHint,omitempty"` // normal, label, subtle
}

// Scope is a named group of variables of a frame.
type Scope struct {
	Name               string  `json:"name"`
	PresentationHint   string  `json:"presentationHint,omitempty"` // arguments, locals, registers, returnValue
	VariablesReference int     `json:"variablesReference"`
	NamedVariables     int     `json:"namedVariables,omitempty"`
	IndexedVariables   int     `json:"indexedVariables,omitempty"`
	Expensive          bool    `json:"expensive"`
	Source             *Source `json:"source,omitempty"`
	Line               int     `json:"line,omitempty"`
	Column             int     `json:"column,omitempty"`
	EndLine            int     `json:"endLine,omitempty"`
	EndColumn          int     `json:"endColumn,omitempty"`
}

// Variable is a variable or a property.
type Variable struct {
	Name                         string                    `json:"name"`
	Value                        string                    `json:"value"`
	Type                         string                    `json:"type,omitempty"`
	PresentationHint             *VariablePresentationHint `json:"presentationHint,omitempty"`
	EvaluateName                 string                    `json:"evaluateName,omitempty"`
	VariablesReference           int                       `json:"variablesReference"`
	NamedVariables               int                       `json:"namedVariables,omitempty"`
	IndexedVariables             int                       `json:"indexedVariables,omitempty"`
	MemoryReference              string                    `json:"memoryReference,omitempty"`
	DeclarationLocationReference int                       `json:"declarationLocationReference,omitempty"`
	ValueLocationReference       int                       `json:"valueLocationReference,omitempty"`
}

// VariablePresentationHint says how to show a variable.
type VariablePresentationHint struct {
	Kind       string   `json:"kind,omitempty"`
	Attributes []string `json:"attributes,omitempty"`
	Visibility string   `json:"visibility,omitempty"`
	Lazy       bool     `json:"lazy,omitempty"`
}

// Module is a module of the debuggee.
type Module struct {
	ID             ModuleID `json:"id"`
	Name           string   `json:"name"`
	Path           string   `json:"path,omitempty"`
	IsOptimized    bool     `json:"isOptimized,omitempty"`
	IsUserCode     bool     `json:"isUserCode,omitempty"`
	Version        string   `json:"version,omitempty"`
	SymbolStatus   string   `json:"symbolStatus,omitempty"`
	SymbolFilePath string   `json:"symbolFilePath,omitempty"`
	DateTimeStamp  string   `json:"dateTimeStamp,omitempty"`
	AddressRange   string   `json:"addressRange,omitempty"`
}

// ColumnDescriptor describes a column of the modules view.
type ColumnDescriptor struct {
	AttributeName string `json:"attributeName"`
	Label         string `json:"label"`
	Format        string `json:"format,omitempty"`
	Type          string `json:"type,omitempty"` // string, number, boolean, unixTimestampUTC
	Width         int    `json:"width,omitempty"`
}

// Breakpoint is the adapter's information about a breakpoint of any kind.
type Breakpoint struct {
	ID                   int     `json:"id,omitempty"`
	Verified             bool    `json:"verified"`
	Message              string  `json:"message,omitempty"`
	Source               *Source `json:"source,omitempty"`
	Line                 int     `json:"line,omitempty"`
	Column               int     `json:"column,omitempty"`
	EndLine              int     `json:"endLine,omitempty"`
	EndColumn            int     `json:"endColumn,omitempty"`
	InstructionReference string  `json:"instructionReference,omitempty"`
	Offset               int     `json:"offset,omitempty"`
	Reason               string  `json:"reason,omitempty"` // pending, failed
}

// SourceBreakpoint is a breakpoint at a line of a source.
type SourceBreakpoint struct {
	Line         int    `json:"line"`
	Column       int    `json:"column,omitempty"`
	Condition    string `json:"condition,omitempty"`
	HitCondition string `json:"hitCondition,omitempty"`
	LogMessage   string `json:"logMessage,omitempty"`
	Mode         string `json:"mode,omitempty"`
}

// FunctionBreakpoint is a breakpoint at a function.
type FunctionBreakpoint struct {
	Name         string `json:"name"`
	Condition    string `json:"condition,omitempty"`
	HitCondition string `json:"hitCondition,omitempty"`
}

// DataBreakpoint is a breakpoint on a change of data.
type DataBreakpoint struct {
	DataID       string `json:"dataId"`
	AccessType   string `json:"accessType,omitempty"` // read, write, readWrite
	Condition    string `json:"condition,omitempty"`
	HitCondition string `json:"hitCondition,omitempty"`
}

// InstructionBreakpoint is a breakpoint at an instruction.
type InstructionBreakpoint struct {
	InstructionReference string `json:"instructionReference"`
	Offset               int    `json:"offset,omitempty"`
	Condition            string `json:"condition,omitempty"`
	HitCondition         string `json:"hitCondition,omitempty"`
	Mode                 string `json:"mode,omitempty"`
}

// BreakpointLocation is a possible location of a source breakpoint.
type BreakpointLocation struct {
	Line      int `json:"line"`
	Column    int `json:"column,omitempty"`
	EndLine   int `json:"endLine,omitempty"`
	EndColumn int `json:"endColumn,omitempty"`
}

// ExceptionBreakpointsFilter is a way to break on exceptions that the adapter offers.
type ExceptionBreakpointsFilter struct {
	Filter               string `json:"filter"`
	Label                string `json:"label"`
	Description          string `json:"description,omitempty"`
	Default              bool   `json:"default,omitempty"`
	SupportsCondition    bool   `json:"supportsCondition,omitempty"`
	ConditionDescription string `json:"conditionDescription,omitempty"`
}

// ExceptionFilterOptions is the choice of one filter, with its condition.
type ExceptionFilterOptions struct {
	FilterID  string `json:"filterId"`
	Condition string `json:"condition,omitempty"`
	Mode      string `json:"mode,omitempty"`
}

// ExceptionOptions is the break mode for the exceptions of a path.
type ExceptionOptions struct {
	Path      []ExceptionPathSegment `json:"path,omitempty"`
	BreakMode string                 `json:"breakMode"` // never, always, unhandled, userUnhandled
}

// ExceptionPathSegment is a segment of the path of an exception category.
type ExceptionPathSegment struct {
	Negate bool     `json:"negate,omitempty"`
	Names  []string `json:"names"`
}

// ExceptionDetails describes an exception.
type ExceptionDetails struct {
	Message        string             `json:"message,omitempty"`
	TypeName       string             `json:"typeName,omitempty"`
	FullTypeName   string             `json:"fullTypeName,omitempty"`
	EvaluateName   string             `json:"evaluateName,omitempty"`
	StackTrace     string             `json:"stackTrace,omitempty"`
	InnerException []ExceptionDetails `json:"innerException,omitempty"`
}

// StepInTarget is a place a step in can go to.
type StepInTarget struct {
	ID        int    `json:"id"`
	Label     string `json:"label"`
	Line      int    `json:"line,omitempty"`
	Column    int    `json:"column,omitempty"`
	EndLine   int    `json:"endLine,omitempty"`
	EndColumn int    `json:"endColumn,omitempty"`
}

// GotoTarget is a place execution can jump to.
type GotoTarget struct {
	ID                          int    `json:"id"`
	Label                       string `json:"label"`
	Line                        int    `json:"line"`
	Column                      int    `json:"column,omitempty"`
	EndLine                     int    `json:"endLine,omitempty"`
	EndColumn                   int    `json:"endColumn,omitempty"`
	InstructionPointerReference string `json:"instructionPointerReference,omitempty"`
}

// CompletionItem is a completion of the debug console's input.
type CompletionItem struct {
	Label           string `json:"label"`
	Text            string `json:"text,omitempty"`
	SortText        string `json:"sortText,omitempty"`
	Detail          string `json:"detail,omitempty"`
	Type            string `json:"type,omitempty"`
	Start           int    `json:"start,omitempty"`
	Length          int    `json:"length,omitempty"`
	SelectionStart  int    `json:"selectionStart,omitempty"`
	SelectionLength int    `json:"selectionLength,omitempty"`
}

// ValueFormat is how a value is shown.
type ValueFormat struct {
	Hex bool `json:"hex,omitempty"`
}

// StackFrameFormat is how a stack frame is shown; it includes the ValueFormat members.
type StackFrameFormat struct {
	Hex             bool `json:"hex,omitempty"`
	Parameters      bool `json:"parameters,omitempty"`
	ParameterTypes  bool `json:"parameterTypes,omitempty"`
	ParameterNames  bool `json:"parameterNames,omitempty"`
	ParameterValues bool `json:"parameterValues,omitempty"`
	Line            bool `json:"line,omitempty"`
	Module          bool `json:"module,omitempty"`
	IncludeAll      bool `json:"includeAll,omitempty"`
}

// DisassembledInstruction is an instruction of the disassembly.
type DisassembledInstruction struct {
	Address          string  `json:"address"`
	InstructionBytes string  `json:"instructionBytes,omitempty"`
	Instruction      string  `json:"instruction"`
	Symbol           string  `json:"symbol,omitempty"`
	Location         *Source `json:"location,omitempty"`
	Line             int     `json:"line,omitempty"`
	Column           int     `json:"column,omitempty"`
	EndLine          int     `json:"endLine,omitempty"`
	EndColumn        int     `json:"endColumn,omitempty"`
	PresentationHint string  `json:"presentationHint,omitempty"` // normal, invalid
}

// BreakpointMode is a mode a breakpoint can have (Capabilities.BreakpointModes).
type BreakpointMode struct {
	Mode        string   `json:"mode"`
	Label       string   `json:"label"`
	Description string   `json:"description,omitempty"`
	AppliesTo   []string `json:"appliesTo"` // source, exception, data, instruction
}

// The enumerations of the protocol as strings.
const (
	// Stepping granularity.
	GranularityStatement   = "statement"
	GranularityLine        = "line"
	GranularityInstruction = "instruction"

	// Exception break modes.
	BreakModeNever         = "never"
	BreakModeAlways        = "always"
	BreakModeUnhandled     = "unhandled"
	BreakModeUserUnhandled = "userUnhandled"

	// Data breakpoint access types.
	AccessRead      = "read"
	AccessWrite     = "write"
	AccessReadWrite = "readWrite"

	// Evaluate contexts.
	ContextWatch     = "watch"
	ContextRepl      = "repl"
	ContextHover     = "hover"
	ContextClipboard = "clipboard"
	ContextVariables = "variables"
)
