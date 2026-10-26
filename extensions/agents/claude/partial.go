package claude

import (
	"sort"
	"strings"
)

// PartialBlock is a content block that is being written: the pieces of its stream so far.
type PartialBlock struct {
	// Index is the index of the block in its message.
	Index int
	// Type is "text", "thinking" or "tool_use".
	Type string
	// Text and Thinking are the pieces joined; for a tool call ToolID and ToolName say which, and InputJSON is the pieces
	// of its input so far (JSON that may still be cut off).
	Text, Thinking, ToolID, ToolName, InputJSON string
}

// Partials joins the StreamEvents of the conversation into the blocks that are being written, so that the session tab
// shows text as it comes. Feed it every StreamEvent with Apply and every AssistantMessage with Finish: the whole message
// replaces its partial blocks. It is not safe for concurrent use; the code that handles the events of a session (one
// goroutine) owns it. The zero value is ready.
type Partials struct {
	// byParent holds the blocks of the message being written, per parent tool call ("" is the main conversation).
	byParent map[string]map[int]*partialAcc
}

type partialAcc struct {
	b                     PartialBlock
	text, thinking, input strings.Builder
}

// Apply takes one stream event into account: message_start clears the blocks of its parent, content_block_start opens a
// block, content_block_delta adds to it, and message_stop closes the message (its blocks stay until Finish or the next
// message_start, because the whole assistant message follows).
func (p *Partials) Apply(ev *StreamEvent) {
	if p.byParent == nil {
		p.byParent = map[string]map[int]*partialAcc{}
	}
	blocks := p.byParent[ev.ParentToolUseID]
	switch ev.EventType {
	case "message_start":
		p.byParent[ev.ParentToolUseID] = map[int]*partialAcc{}
	case "content_block_start":
		if blocks == nil {
			blocks = map[int]*partialAcc{}
			p.byParent[ev.ParentToolUseID] = blocks
		}
		blocks[ev.Index] = &partialAcc{b: PartialBlock{Index: ev.Index, Type: ev.BlockType, ToolID: ev.BlockID, ToolName: ev.BlockName}}
	case "content_block_delta":
		if blocks == nil {
			blocks = map[int]*partialAcc{}
			p.byParent[ev.ParentToolUseID] = blocks
		}
		acc := blocks[ev.Index]
		if acc == nil { // a delta with no start: the stream began in the middle
			acc = &partialAcc{b: PartialBlock{Index: ev.Index}}
			blocks[ev.Index] = acc
		}
		switch ev.DeltaType {
		case "text_delta":
			if acc.b.Type == "" {
				acc.b.Type = "text"
			}
			acc.text.WriteString(ev.Text)
		case "thinking_delta":
			if acc.b.Type == "" {
				acc.b.Type = "thinking"
			}
			acc.thinking.WriteString(ev.Thinking)
		case "input_json_delta":
			if acc.b.Type == "" {
				acc.b.Type = "tool_use"
			}
			acc.input.WriteString(ev.PartialJSON)
		}
	}
}

// Finish drops the partial blocks of the parent (the whole message arrived).
func (p *Partials) Finish(parentToolUseID string) {
	delete(p.byParent, parentToolUseID)
}

// Reset drops every partial block (a new turn, a dead program).
func (p *Partials) Reset() { p.byParent = nil }

// Blocks returns the blocks being written for the parent ("" is the main conversation), in the order of their index.
func (p *Partials) Blocks(parentToolUseID string) []PartialBlock {
	blocks := p.byParent[parentToolUseID]
	out := make([]PartialBlock, 0, len(blocks))
	for _, acc := range blocks {
		b := acc.b
		b.Text, b.Thinking, b.InputJSON = acc.text.String(), acc.thinking.String(), acc.input.String()
		out = append(out, b)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Index < out[j].Index })
	return out
}

// Text is the text of the text blocks being written for the parent, joined: what the session tab shows of the message so
// far.
func (p *Partials) Text(parentToolUseID string) string {
	var sb strings.Builder
	for _, b := range p.Blocks(parentToolUseID) {
		if b.Type == "text" {
			sb.WriteString(b.Text)
		}
	}
	return sb.String()
}
