package claude

import (
	"bytes"
	"encoding/json"
	"strings"

	"github.com/rm4n0s/errors"
)

// Event is what a Session reports through SessionOptions.OnEvent. The messages of the program (InitMessage,
// SystemMessage, AssistantMessage, UserMessage, StreamEvent, ResultMessage, UnknownMessage) and the two events of the
// adapter itself (ProtocolErrorEvent, ExitEvent) implement it; switch on the concrete type.
type Event interface {
	isEvent()
}

// Message is an Event that is a line of the program's output, parsed. RawJSON is the line as the program wrote it, so that
// nothing the adapter does not know is lost.
type Message interface {
	Event
	// RawJSON is the whole line of the message.
	RawJSON() json.RawMessage
}

type raw struct{ line json.RawMessage }

func (raw) isEvent()                   {}
func (r raw) RawJSON() json.RawMessage { return r.line }

// Block is a content block of an assistant or user message. Type says which fields are set: "text" (Text), "thinking"
// (Thinking, Signature), "tool_use" and "server_tool_use" (ID, Name, Input), "tool_result" (ToolUseID, Content, IsError);
// any other type is kept in Raw only.
type Block struct {
	Type      string
	Text      string
	Thinking  string
	Signature string
	// ID, Name and Input describe a tool call.
	ID    string
	Name  string
	Input json.RawMessage
	// ToolUseID, Content and IsError describe the result of the tool call ToolUseID. Content is a JSON string or a list of
	// blocks; ResultText reads either.
	ToolUseID string
	Content   json.RawMessage
	IsError   bool
	// Raw is the block as the program wrote it.
	Raw json.RawMessage
}

// blockJSON is the wire form of a content block.
type blockJSON struct {
	Type      string          `json:"type"`
	Text      string          `json:"text"`
	Thinking  string          `json:"thinking"`
	Signature string          `json:"signature"`
	ID        string          `json:"id"`
	Name      string          `json:"name"`
	Input     json.RawMessage `json:"input"`
	ToolUseID string          `json:"tool_use_id"`
	Content   json.RawMessage `json:"content"`
	IsError   bool            `json:"is_error"`
}

// ResultText is the text of a "tool_result" block: its content when that is a string, else the texts of its "text" blocks
// joined by new lines. Other blocks (images) are left out.
func (b Block) ResultText() string {
	var s string
	if json.Unmarshal(b.Content, &s) == nil {
		return s
	}
	var blocks []blockJSON
	if json.Unmarshal(b.Content, &blocks) != nil {
		return ""
	}
	var parts []string
	for _, c := range blocks {
		if c.Type == "text" {
			parts = append(parts, c.Text)
		}
	}
	return strings.Join(parts, "\n")
}

// Capabilities are the features the program says it has, in the "capabilities" of system/init. The adapter and the UI test
// for a feature with Has, never against a version (SPEC §10.16).
type Capabilities []string

// Capability names documented by the Agent SDK. The list of the program is longer and grows; Has takes any name.
const (
	// CapInterruptReceipt: the answer to an interrupt tells which messages are still queued.
	CapInterruptReceipt = "interrupt_receipt_v1"
	// CapInterruptCancelQueued: an interrupt may also cancel the queued messages.
	CapInterruptCancelQueued = "interrupt_cancel_queued_v1"
	// CapMCPReadResource: MCP resources can be read through the control channel.
	CapMCPReadResource = "mcp_read_resource_v1"
)

// Has reports whether the program lists the capability.
func (c Capabilities) Has(name string) bool {
	for _, n := range c {
		if n == name {
			return true
		}
	}
	return false
}

// InitMessage is `{"type":"system","subtype":"init"}`: the first message of a conversation.
type InitMessage struct {
	raw
	UUID, SessionID string
	// CWD is the folder the program runs in; Model the model in use; PermissionMode its mode (as the program names it).
	CWD, Model, PermissionMode   string
	Tools, SlashCommands, Skills []string
	APIKeySource                 string
	OutputStyle                  string
	// MCPServers are the servers and their status ("connected", "failed", ...).
	MCPServers []MCPServer
	// Capabilities are the features of this program; see Capabilities.
	Capabilities Capabilities
	// ClaudeCodeVersion is for display and logs only; features are never chosen by it.
	ClaudeCodeVersion string
}

// MCPServer is a server of the Model Context Protocol the program started.
type MCPServer struct {
	Name, Status string
}

// SystemMessage is a `system` message other than init (status, compact boundaries, tasks, hooks): the subtype and the
// whole message, which the UI reads from RawJSON when it needs more.
type SystemMessage struct {
	raw
	Subtype, UUID, SessionID string
}

// AssistantMessage is a whole message of the agent. Blocks holds its text, thinking and tool calls.
type AssistantMessage struct {
	raw
	UUID, SessionID string
	// ParentToolUseID is set for a message of a sub-agent started by that tool call.
	ParentToolUseID string
	// MessageID, Model and StopReason come from the API message.
	MessageID, Model, StopReason string
	Blocks                       []Block
	// Error is the kind of API error ("rate_limit", "authentication_failed", ...) when the message reports one.
	Error string
}

// Text is the texts of the text blocks of the message, joined.
func (m *AssistantMessage) Text() string {
	var sb strings.Builder
	for _, b := range m.Blocks {
		if b.Type == "text" {
			sb.WriteString(b.Text)
		}
	}
	return sb.String()
}

// UserMessage is a message of the user, or the results of tool calls (Blocks of type "tool_result"). With
// --replay-user-messages the program writes back the messages it got, with IsReplay set; UUID is what RewindFiles takes.
type UserMessage struct {
	raw
	UUID, SessionID string
	ParentToolUseID string
	// Blocks are the content blocks; a content that was a plain string is one "text" block.
	Blocks []Block
	// IsReplay is true for the echo of a message sent with Send; IsSynthetic for one the program made itself.
	IsReplay, IsSynthetic bool
	// ToolUseResult is the structured result the program attaches to a tool result, as JSON (a diff of an edit, ...).
	ToolUseResult json.RawMessage
}

// Text is the texts of the text blocks of the message, joined.
func (m *UserMessage) Text() string {
	var sb strings.Builder
	for _, b := range m.Blocks {
		if b.Type == "text" {
			sb.WriteString(b.Text)
		}
	}
	return sb.String()
}

// StreamEvent is a `stream_event`: one event of the API's message stream, for text that appears while it is written.
// Partials joins them. EventType is message_start, content_block_start, content_block_delta, content_block_stop,
// message_delta or message_stop; the other fields are set by the events they belong to.
type StreamEvent struct {
	raw
	UUID, SessionID, ParentToolUseID string
	EventType                        string
	// Index is the index of the content block of the event (content_block_*).
	Index int
	// BlockType, BlockID and BlockName describe the block that starts (content_block_start).
	BlockType, BlockID, BlockName string
	// DeltaType is text_delta, thinking_delta, input_json_delta or signature_delta (content_block_delta); the text, the
	// thinking or the piece of JSON follows.
	DeltaType                   string
	Text, Thinking, PartialJSON string
	// MessageID and Model come from message_start; StopReason from message_delta.
	MessageID, Model, StopReason string
}

// ResultMessage ends a turn: its cost, its length and, when the turn failed, why.
type ResultMessage struct {
	raw
	// Subtype is "success", or "error_max_turns", "error_during_execution", "error_max_budget_usd", ...
	Subtype         string
	UUID, SessionID string
	IsError         bool
	NumTurns        int
	DurationMS      int64
	DurationAPIMS   int64
	TotalCostUSD    float64
	// Result is the final text of a successful turn; Errors the reasons of a failed one.
	Result     string
	Errors     []string
	StopReason string
	// TerminalReason says why the turn ended, when the program does.
	TerminalReason string
	// Usage, ModelUsage and PermissionDenials are kept as JSON.
	Usage, ModelUsage, PermissionDenials json.RawMessage
}

// UnknownMessage is a line of a type the adapter does not know (rate_limit_event, transcript_mirror, ...), kept whole.
type UnknownMessage struct {
	raw
	// Type is the "type" of the line.
	Type string
}

// ProtocolErrorEvent reports a line the adapter could not use: it was not JSON (tag MalformedLine), longer than the limit
// (LineTooLong), a known message of the wrong shape (MessageInvalid), or a request of the program it could not answer
// (ToolRequestInvalid). The stream goes on with the next line.
type ProtocolErrorEvent struct {
	// Err is the tagged error.
	Err error
	// Line is the start of the offending line (at most 1 KiB).
	Line []byte
}

func (*ProtocolErrorEvent) isEvent() {}

// ExitEvent is the last event of a session: the program ended. Err is nil when the end was asked for (Close, or the
// context of Start ending), else the error with the tag ProgramExited, whose metadata has the exit code and the last
// lines of the program's standard error.
type ExitEvent struct {
	// Code is the exit status, -1 when a signal ended the program.
	Code int
	// Err is nil for an end that was asked for.
	Err error
}

func (*ExitEvent) isEvent() {}

// envelope is the part of every line the adapter reads first.
type envelope struct {
	Type      string  `json:"type"`
	Subtype   string  `json:"subtype"`
	UUID      string  `json:"uuid"`
	SessionID string  `json:"session_id"`
	Parent    *string `json:"parent_tool_use_id"`
	// the fields of the control messages
	RequestID string          `json:"request_id"`
	Request   json.RawMessage `json:"request"`
	Response  json.RawMessage `json:"response"`
}

// parseLine turns a line of the program's output that is not a control message into a Message. It fails with the tags
// MalformedLine (not a JSON object) and MessageInvalid (a known type of the wrong shape, from decode).
func parseLine(line []byte) (Message, error) {
	env, err := parseEnvelope(line)
	if err != nil {
		return nil, err
	}
	return buildMessage(env, line)
}

// parseEnvelope reads the common part of a line; it fails with the tag MalformedLine.
func parseEnvelope(line []byte) (envelope, error) {
	var env envelope
	if err := json.Unmarshal(line, &env); err != nil {
		return env, errors.NewErr("MalformedLine", err)
	}
	return env, nil
}

// buildMessage types a line whose envelope was read; it fails with the tag MessageInvalid.
func buildMessage(env envelope, line []byte) (Message, error) {
	r := raw{line: append(json.RawMessage(nil), line...)}
	parent := ""
	if env.Parent != nil {
		parent = *env.Parent
	}
	switch env.Type {
	case "system":
		if env.Subtype == "init" {
			return parseInit(r, line)
		}
		return &SystemMessage{raw: r, Subtype: env.Subtype, UUID: env.UUID, SessionID: env.SessionID}, nil
	case "assistant":
		return parseAssistant(r, env, parent, line)
	case "user":
		return parseUser(r, env, parent, line)
	case "stream_event":
		return parseStream(r, env, parent, line)
	case "result":
		return parseResult(r, env, line)
	default:
		return &UnknownMessage{raw: r, Type: env.Type}, nil
	}
}

// decode reads JSON into v; it fails with the tag MessageInvalid, with the kind of message in the metadata.
func decode(data []byte, v any, kind string) error {
	if err := json.Unmarshal(data, v); err != nil {
		return errors.NewErr("MessageInvalid", err).SetMetadata("kind", kind)
	}
	return nil
}

func parseInit(r raw, line []byte) (Message, error) {
	var w struct {
		UUID              string   `json:"uuid"`
		SessionID         string   `json:"session_id"`
		CWD               string   `json:"cwd"`
		Model             string   `json:"model"`
		PermissionMode    string   `json:"permissionMode"`
		Tools             []string `json:"tools"`
		SlashCommands     []string `json:"slash_commands"`
		Skills            []string `json:"skills"`
		APIKeySource      string   `json:"apiKeySource"`
		OutputStyle       string   `json:"output_style"`
		Capabilities      []string `json:"capabilities"`
		ClaudeCodeVersion string   `json:"claude_code_version"`
		MCPServers        []struct {
			Name   string `json:"name"`
			Status string `json:"status"`
		} `json:"mcp_servers"`
	}
	if err := decode(line, &w, "system/init"); err != nil {
		return nil, err
	}
	m := &InitMessage{
		raw: r, UUID: w.UUID, SessionID: w.SessionID, CWD: w.CWD, Model: w.Model, PermissionMode: w.PermissionMode,
		Tools: w.Tools, SlashCommands: w.SlashCommands, Skills: w.Skills, APIKeySource: w.APIKeySource,
		OutputStyle: w.OutputStyle, Capabilities: w.Capabilities, ClaudeCodeVersion: w.ClaudeCodeVersion,
	}
	for _, s := range w.MCPServers {
		m.MCPServers = append(m.MCPServers, MCPServer{Name: s.Name, Status: s.Status})
	}
	return m, nil
}

// blocksOf reads the content of a message: a string is one text block, a list is its blocks.
func blocksOf(content json.RawMessage) ([]Block, error) {
	content = bytes.TrimSpace(content)
	if len(content) == 0 || bytes.Equal(content, []byte("null")) {
		return nil, nil
	}
	if content[0] == '"' {
		var s string
		if err := decode(content, &s, "content"); err != nil {
			return nil, err
		}
		return []Block{{Type: "text", Text: s, Raw: append(json.RawMessage(nil), content...)}}, nil
	}
	var items []json.RawMessage
	if err := decode(content, &items, "content"); err != nil {
		return nil, err
	}
	blocks := make([]Block, 0, len(items))
	for _, it := range items {
		var b blockJSON
		if err := decode(it, &b, "content block"); err != nil {
			return nil, err
		}
		blocks = append(blocks, Block{
			Type: b.Type, Text: b.Text, Thinking: b.Thinking, Signature: b.Signature, ID: b.ID, Name: b.Name,
			Input: b.Input, ToolUseID: b.ToolUseID, Content: b.Content, IsError: b.IsError, Raw: it,
		})
	}
	return blocks, nil
}

func parseAssistant(r raw, env envelope, parent string, line []byte) (Message, error) {
	var w struct {
		Error   string `json:"error"`
		Message struct {
			ID         string          `json:"id"`
			Model      string          `json:"model"`
			StopReason string          `json:"stop_reason"`
			Content    json.RawMessage `json:"content"`
		} `json:"message"`
	}
	if err := decode(line, &w, "assistant"); err != nil {
		return nil, err
	}
	blocks, err := blocksOf(w.Message.Content)
	if err != nil {
		return nil, err
	}
	return &AssistantMessage{
		raw: r, UUID: env.UUID, SessionID: env.SessionID, ParentToolUseID: parent, MessageID: w.Message.ID,
		Model: w.Message.Model, StopReason: w.Message.StopReason, Blocks: blocks, Error: w.Error,
	}, nil
}

func parseUser(r raw, env envelope, parent string, line []byte) (Message, error) {
	var w struct {
		IsReplay      bool            `json:"isReplay"`
		IsSynthetic   bool            `json:"isSynthetic"`
		ToolUseResult json.RawMessage `json:"tool_use_result"`
		Message       struct {
			Content json.RawMessage `json:"content"`
		} `json:"message"`
	}
	if err := decode(line, &w, "user"); err != nil {
		return nil, err
	}
	blocks, err := blocksOf(w.Message.Content)
	if err != nil {
		return nil, err
	}
	return &UserMessage{
		raw: r, UUID: env.UUID, SessionID: env.SessionID, ParentToolUseID: parent, Blocks: blocks,
		IsReplay: w.IsReplay, IsSynthetic: w.IsSynthetic, ToolUseResult: w.ToolUseResult,
	}, nil
}

func parseStream(r raw, env envelope, parent string, line []byte) (Message, error) {
	var w struct {
		Event struct {
			Type         string `json:"type"`
			Index        int    `json:"index"`
			ContentBlock struct {
				Type string `json:"type"`
				ID   string `json:"id"`
				Name string `json:"name"`
			} `json:"content_block"`
			Delta struct {
				Type        string `json:"type"`
				Text        string `json:"text"`
				Thinking    string `json:"thinking"`
				PartialJSON string `json:"partial_json"`
				StopReason  string `json:"stop_reason"`
			} `json:"delta"`
			Message struct {
				ID    string `json:"id"`
				Model string `json:"model"`
			} `json:"message"`
		} `json:"event"`
	}
	if err := decode(line, &w, "stream_event"); err != nil {
		return nil, err
	}
	e := w.Event
	return &StreamEvent{
		raw: r, UUID: env.UUID, SessionID: env.SessionID, ParentToolUseID: parent, EventType: e.Type, Index: e.Index,
		BlockType: e.ContentBlock.Type, BlockID: e.ContentBlock.ID, BlockName: e.ContentBlock.Name,
		DeltaType: e.Delta.Type, Text: e.Delta.Text, Thinking: e.Delta.Thinking, PartialJSON: e.Delta.PartialJSON,
		MessageID: e.Message.ID, Model: e.Message.Model, StopReason: e.Delta.StopReason,
	}, nil
}

func parseResult(r raw, env envelope, line []byte) (Message, error) {
	var w struct {
		IsError           bool            `json:"is_error"`
		NumTurns          int             `json:"num_turns"`
		DurationMS        int64           `json:"duration_ms"`
		DurationAPIMS     int64           `json:"duration_api_ms"`
		TotalCostUSD      float64         `json:"total_cost_usd"`
		Result            string          `json:"result"`
		Errors            []string        `json:"errors"`
		StopReason        string          `json:"stop_reason"`
		TerminalReason    string          `json:"terminal_reason"`
		Usage             json.RawMessage `json:"usage"`
		ModelUsage        json.RawMessage `json:"modelUsage"`
		PermissionDenials json.RawMessage `json:"permission_denials"`
	}
	if err := decode(line, &w, "result"); err != nil {
		return nil, err
	}
	return &ResultMessage{
		raw: r, Subtype: env.Subtype, UUID: env.UUID, SessionID: env.SessionID, IsError: w.IsError, NumTurns: w.NumTurns,
		DurationMS: w.DurationMS, DurationAPIMS: w.DurationAPIMS, TotalCostUSD: w.TotalCostUSD, Result: w.Result,
		Errors: w.Errors, StopReason: w.StopReason, TerminalReason: w.TerminalReason, Usage: w.Usage,
		ModelUsage: w.ModelUsage, PermissionDenials: w.PermissionDenials,
	}, nil
}
