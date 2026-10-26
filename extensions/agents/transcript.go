package agents

import (
	"encoding/json"
	"path/filepath"
	"strconv"
	"strings"
	"unicode/utf8"

	"github.com/rm4n0s/emetos/extensions/agents/claude"
)

// The kinds of item of a conversation.
const (
	// kindUser is a message of the user.
	kindUser = "user"
	// kindText is text of the agent, shown as Markdown.
	kindText = "text"
	// kindThinking is the thinking of the agent, folded away.
	kindThinking = "thinking"
	// kindTool is a tool call of the agent with its result.
	kindTool = "tool"
	// kindNotice is a line of the tab itself (a turn that was stopped, a permission that was denied).
	kindNotice = "notice"
	// kindError is an error, shown in the color of errors.
	kindError = "error"
)

// The bounds of what a conversation keeps, in memory and in the state that is saved (SPEC §10.16 "As built (step 13.2)").
const (
	// maxItemText is the longest text of one item that is kept; the rest is cut.
	maxItemText = 64 * 1024
	// maxResultText is the longest result of a tool call that is kept.
	maxResultText = 4 * 1024
	// maxItems is how many items a conversation holds; the oldest go first.
	maxItems = 400
	// maxSavedBytes is how much of a conversation the state of a tab holds; the oldest items are left out first.
	maxSavedBytes = 256 * 1024
	// maxSnapshotBytes is how much of a conversation the session index keeps for a session whose tab is closed.
	maxSnapshotBytes = 48 * 1024
)

// item is one thing in a conversation. The fields with a tag are what the state of the tab saves.
type item struct {
	// Kind is one of the kinds above.
	Kind string `json:"k"`
	// ID is the ID of the tool call of a tool item, and the UUID of the message of a user item (what a rewind needs, step
	// 13.3).
	ID string `json:"id,omitempty"`
	// Text is the text of a user, text, thinking, notice or error item.
	Text string `json:"t,omitempty"`
	// Tool is the name of the tool of a tool item and Summary the one line that says what it does (the file, the command).
	Tool    string `json:"tool,omitempty"`
	Summary string `json:"sum,omitempty"`
	// Result is the text a tool call returned, cut; Done says that it returned, Failed that it reported an error.
	Result string `json:"res,omitempty"`
	Done   bool   `json:"done,omitempty"`
	Failed bool   `json:"fail,omitempty"`
	// Depth is 1 for a tool call of a sub-agent.
	Depth int `json:"d,omitempty"`

	// open says that the item is unfolded; it is not saved.
	open bool
}

// clip cuts text to at most max bytes, at a character boundary, and says so.
func clip(text string, max int) string {
	if len(text) <= max {
		return text
	}
	cut := max
	for cut > 0 && !utf8.RuneStart(text[cut]) {
		cut--
	}
	return text[:cut] + "\n... (cut)"
}

// toolSummary is the line that says what a tool call does: the file it reads or changes, the command, the pattern, the
// address. folder, when it is not empty, is taken off the front of a path inside it.
func toolSummary(name string, input json.RawMessage, folder string) string {
	var in map[string]any
	if json.Unmarshal(input, &in) != nil {
		return ""
	}
	str := func(keys ...string) string {
		for _, k := range keys {
			if s, ok := in[k].(string); ok && strings.TrimSpace(s) != "" {
				return s
			}
		}
		return ""
	}
	var s string
	switch name {
	case "Read", "Edit", "Write", "NotebookEdit", "MultiEdit":
		s = relative(str("file_path", "notebook_path", "path"), folder)
	case "Bash", "BashOutput":
		s = str("command", "bash_id")
	case "Grep", "Glob":
		s = str("pattern")
		if p := str("path"); p != "" {
			s += " in " + relative(p, folder)
		}
	case "WebFetch":
		s = str("url")
	case "WebSearch":
		s = str("query")
	case "Task", "Agent":
		s = str("description", "prompt")
	case "TodoWrite":
		if todos, ok := in["todos"].([]any); ok {
			s = strconv.Itoa(len(todos)) + " items"
		}
	default:
		s = str("file_path", "path", "command", "pattern", "url", "query", "description", "prompt")
	}
	if line, _, found := strings.Cut(s, "\n"); found {
		s = line + " ..."
	}
	return clip(s, 300)
}

// relative makes a path inside folder relative to it.
func relative(path, folder string) string {
	if path == "" || folder == "" || !filepath.IsAbs(path) {
		return path
	}
	if rel, err := filepath.Rel(folder, path); err == nil && rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
		return filepath.ToSlash(rel)
	}
	return path
}

// toolIcon is the Codicon of a tool.
func toolIcon(name string) string {
	switch name {
	case "Read":
		return "file-text"
	case "Edit", "MultiEdit", "NotebookEdit":
		return "edit"
	case "Write":
		return "new-file"
	case "Bash", "BashOutput":
		return "terminal"
	case "Grep", "Glob", "WebSearch":
		return "search"
	case "WebFetch":
		return "globe"
	case "Task", "Agent":
		return "hubot"
	case "TodoWrite":
		return "checklist"
	}
	return "tools"
}

// transcript is the conversation of a session: the items that are final, and the blocks that are still being written.
type transcript struct {
	items    []*item
	partials claude.Partials
	// folder is taken off the front of the paths of tool calls.
	folder string
}

// live returns the items for the blocks that are being written, which the tab shows after the final ones. They are made
// again at each build.
func (tr *transcript) live() []*item {
	var out []*item
	for _, b := range tr.partials.Blocks("") {
		switch b.Type {
		case "text":
			if b.Text != "" {
				out = append(out, &item{Kind: kindText, Text: b.Text})
			}
		case "thinking":
			out = append(out, &item{Kind: kindThinking, Text: b.Thinking})
		case "tool_use":
			it := &item{Kind: kindTool, ID: b.ToolID, Tool: b.ToolName}
			it.Summary = toolSummary(b.ToolName, json.RawMessage(b.InputJSON), tr.folder)
			out = append(out, it)
		}
	}
	return out
}

// add appends an item and drops the oldest when there are too many.
func (tr *transcript) add(it *item) *item {
	it.Text = clip(it.Text, maxItemText)
	tr.items = append(tr.items, it)
	if len(tr.items) > maxItems {
		tr.items = append([]*item(nil), tr.items[len(tr.items)-maxItems:]...)
	}
	return it
}

// tool finds the tool item with the ID, the newest first.
func (tr *transcript) tool(id string) *item {
	for i := len(tr.items) - 1; i >= 0; i-- {
		if it := tr.items[i]; it.Kind == kindTool && it.ID == id {
			return it
		}
	}
	return nil
}

// assistant takes a whole message of the agent: its blocks become items and its partial blocks go. A message of a
// sub-agent shows its tool calls only, one level in.
func (tr *transcript) assistant(m *claude.AssistantMessage) {
	tr.partials.Finish(m.ParentToolUseID)
	depth := 0
	if m.ParentToolUseID != "" {
		depth = 1
	}
	for _, b := range m.Blocks {
		switch b.Type {
		case "text":
			if depth == 0 && strings.TrimSpace(b.Text) != "" {
				tr.add(&item{Kind: kindText, Text: b.Text})
			}
		case "thinking":
			if depth == 0 && strings.TrimSpace(b.Thinking) != "" {
				tr.add(&item{Kind: kindThinking, Text: b.Thinking})
			}
		case "tool_use", "server_tool_use":
			if tr.tool(b.ID) != nil {
				continue // the same message again
			}
			tr.add(&item{Kind: kindTool, ID: b.ID, Tool: b.Name, Summary: toolSummary(b.Name, b.Input, tr.folder), Depth: depth})
		}
	}
}

// results takes the results of tool calls from a message of the user.
func (tr *transcript) results(m *claude.UserMessage) {
	for _, b := range m.Blocks {
		if b.Type != "tool_result" {
			continue
		}
		if it := tr.tool(b.ToolUseID); it != nil {
			it.Done, it.Failed = true, b.IsError
			it.Result = clip(b.ResultText(), maxResultText)
		}
	}
}

// finishTools marks the tool calls that did not return as done: the turn is over.
func (tr *transcript) finishTools() {
	for _, it := range tr.items {
		if it.Kind == kindTool && !it.Done {
			it.Done = true
		}
	}
}

// saved returns the items for the state of a tab: the newest that fit in max bytes of JSON.
func (tr *transcript) saved(max int) []*item {
	size := 0
	start := len(tr.items)
	for i := len(tr.items) - 1; i >= 0; i-- {
		b, err := json.Marshal(tr.items[i])
		if err != nil {
			break
		}
		if size += len(b); size > max {
			break
		}
		start = i
	}
	return tr.items[start:]
}
