package claude

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"strconv"
	"time"

	"github.com/rm4n0s/errors"
)

// The control channel (SPEC §10.16): lines of type control_request and control_response on the same streams as the
// messages. Emetos sends initialize, interrupt, set_permission_mode, set_model and rewind_files; the program sends
// can_use_tool. A request and its answer have the same request_id.

// PermissionRule is a rule of a PermissionUpdate: a tool, and optionally the part of its use the rule is about (a command
// prefix, a path pattern).
type PermissionRule struct {
	ToolName    string `json:"toolName"`
	RuleContent string `json:"ruleContent,omitempty"`
}

// PermissionUpdate is a change of the permission rules or mode, as the program proposes it in a ToolRequest ("Allow for
// this session" with the suggestion whose Destination is "session") and as Emetos returns it in a Decision. Type is
// addRules, replaceRules, removeRules, setMode, addDirectories or removeDirectories. An update read from the program is
// written back exactly as it came, so fields the adapter does not know are kept; WithDestination makes a changed copy.
type PermissionUpdate struct {
	Type string `json:"type"`
	// Rules and Behavior ("allow", "deny", "ask") belong to the rule types.
	Rules    []PermissionRule `json:"rules,omitempty"`
	Behavior string           `json:"behavior,omitempty"`
	// Mode belongs to setMode.
	Mode string `json:"mode,omitempty"`
	// Directories belong to addDirectories and removeDirectories.
	Directories []string `json:"directories,omitempty"`
	// Destination is "userSettings", "projectSettings", "localSettings" or "session".
	Destination string `json:"destination,omitempty"`

	raw json.RawMessage
}

// UnmarshalJSON reads the update and keeps its text.
func (u *PermissionUpdate) UnmarshalJSON(data []byte) error {
	type plain PermissionUpdate
	var p plain
	if err := json.Unmarshal(data, &p); err != nil {
		return err // decode, which called this, gives it the tag MessageInvalid
	}
	*u = PermissionUpdate(p)
	u.raw = append(json.RawMessage(nil), data...)
	return nil
}

// MarshalJSON writes the update as it was read, or from its fields when it was made here.
func (u PermissionUpdate) MarshalJSON() ([]byte, error) {
	if len(u.raw) > 0 {
		return u.raw, nil
	}
	type plain PermissionUpdate
	return json.Marshal(plain(u))
}

// WithDestination returns a copy of the update that is saved to the destination.
func (u PermissionUpdate) WithDestination(destination string) PermissionUpdate {
	u.Destination = destination
	u.raw = nil
	return u
}

// ToolRequest is a can_use_tool request: the program asks whether it may use a tool.
type ToolRequest struct {
	// RequestID is the ID of the control request.
	RequestID string
	// ToolName is "Bash", "Edit", "Write", "NotebookEdit", "WebFetch", "ExitPlanMode", "AskUserQuestion", ...; Input is the
	// input of the tool as JSON.
	ToolName string
	Input    json.RawMessage
	// ToolUseID is the ID of the tool_use block this is about; AgentID is set when a sub-agent asks.
	ToolUseID, AgentID string
	// Suggestions are the rule changes the program proposes for "Allow always" and "Allow for this session".
	Suggestions []PermissionUpdate
	// BlockedPath is the file that made the program ask; DecisionReason says why it asks.
	BlockedPath, DecisionReason string
	// Title is a sentence for the prompt, DisplayName a short noun phrase for the action, Description a subtitle.
	Title, DisplayName, Description string
}

// toolRequestJSON is the wire form of the request.
type toolRequestJSON struct {
	ToolName       string             `json:"tool_name"`
	Input          json.RawMessage    `json:"input"`
	ToolUseID      string             `json:"tool_use_id"`
	AgentID        string             `json:"agent_id"`
	Suggestions    []PermissionUpdate `json:"permission_suggestions"`
	BlockedPath    string             `json:"blocked_path"`
	DecisionReason string             `json:"decision_reason"`
	Title          string             `json:"title"`
	DisplayName    string             `json:"display_name"`
	Description    string             `json:"description"`
}

// Decision is the answer to a ToolRequest.
type Decision struct {
	// Allow is true to let the tool run, false to deny it.
	Allow bool
	// UpdatedInput replaces the input of the tool when Allow is true (the text the user changed in the proposed edit); nil
	// keeps the input of the request.
	UpdatedInput json.RawMessage
	// UpdatedPermissions are rule changes to apply when Allow is true (the suggestions the user accepted).
	UpdatedPermissions []PermissionUpdate
	// Message tells the agent why a denied tool was denied (what the user typed).
	Message string
	// Interrupt, with a denial, stops the turn.
	Interrupt bool
}

// CanUseToolFunc answers a ToolRequest. It may wait for the user as long as ctx lives: ctx ends when the program cancels
// the request or the session ends. An error is answered with a denial that says so.
type CanUseToolFunc func(ctx context.Context, req ToolRequest) (Decision, error)

// SlashCommand is a command of the program (`/compact`, a skill) from the handshake.
type SlashCommand struct {
	Name         string   `json:"name"`
	Description  string   `json:"description"`
	ArgumentHint string   `json:"argumentHint"`
	Aliases      []string `json:"aliases"`
}

// ModelInfo is a model of the handshake; Value is what SetModel takes.
type ModelInfo struct {
	Value       string `json:"value"`
	DisplayName string `json:"displayName"`
	Description string `json:"description"`
}

// AccountInfo is the account of the handshake, as the program reports it.
type AccountInfo struct {
	Email            string `json:"email"`
	Organization     string `json:"organization"`
	SubscriptionType string `json:"subscriptionType"`
}

// Handshake is the answer to the initialize request: what the session tab offers after "/" and in the model picker.
type Handshake struct {
	Commands    []SlashCommand `json:"commands"`
	Models      []ModelInfo    `json:"models"`
	Account     AccountInfo    `json:"account"`
	OutputStyle string         `json:"output_style"`
	// Raw is the whole answer.
	Raw json.RawMessage `json:"-"`
}

// RewindResult is the answer to RewindFiles.
type RewindResult struct {
	// CanRewind is false when the program could not restore the files; Error says why.
	CanRewind bool
	Error     string
	// FilesChanged, Insertions and Deletions describe the restore, when the program says.
	FilesChanged          []string
	Insertions, Deletions int
}

// controlResult is what a pending request waits for.
type controlResult struct {
	body json.RawMessage
	err  error
}

// pending is a control request that waits for its answer.
type pending struct {
	subtype string
	ch      chan controlResult // buffered: the reader never blocks on it
}

// newRequestID makes the ID of a control request: a counter and four random bytes, as "req_3_1a2b3c4d".
func (s *Session) newRequestID() string {
	var b [4]byte
	_, _ = rand.Read(b[:]) // crypto/rand does not fail on Linux; a zero suffix still has the counter
	return "req_" + strconv.FormatUint(s.reqCounter.Add(1), 10) + "_" + hex.EncodeToString(b[:])
}

// controlRequestLine is the line a request is written as.
type controlRequestLine struct {
	Type      string `json:"type"`
	RequestID string `json:"request_id"`
	Request   any    `json:"request"`
}

// control sends a control request and waits for its answer, for at most the request timeout. It fails with the tags
// SessionClosed (the session is over), SendFailed (the line was not written), ControlRequestTimeout,
// ControlRequestCanceled (ctx ended), ControlRequestFailed (the program answered with an error) and ProgramExited (the
// program died while the request waited).
func (s *Session) control(ctx context.Context, subtype string, body map[string]any) (json.RawMessage, error) {
	id := s.newRequestID()
	p := &pending{subtype: subtype, ch: make(chan controlResult, 1)}
	s.mu.Lock()
	if s.finished {
		s.mu.Unlock()
		return nil, errors.New("SessionClosed", "the session is over", "request", subtype)
	}
	s.pending[id] = p
	s.mu.Unlock()
	drop := func() {
		s.mu.Lock()
		delete(s.pending, id)
		s.mu.Unlock()
	}
	req := map[string]any{"subtype": subtype}
	for k, v := range body {
		req[k] = v
	}
	if err := s.write(controlRequestLine{Type: "control_request", RequestID: id, Request: req}); err != nil {
		drop()
		return nil, err
	}
	timer := time.NewTimer(s.requestTimeout)
	defer timer.Stop()
	select {
	case res := <-p.ch:
		return res.body, res.err
	case <-timer.C:
		drop()
		return nil, errors.New("ControlRequestTimeout", "the program did not answer the control request", "request", subtype, "id", id, "timeout", s.requestTimeout.String())
	case <-ctx.Done():
		drop()
		return nil, errors.NewErr("ControlRequestCanceled", ctx.Err()).SetMetadata("request", subtype, "id", id)
	}
}

// controlResponseBody is the inner part of a control_response line.
type controlResponseBody struct {
	Subtype   string          `json:"subtype"`
	RequestID string          `json:"request_id"`
	Response  json.RawMessage `json:"response"`
	Error     string          `json:"error"`
}

// handleControlResponse gives the answer to the request that waits for it. It fails with MessageInvalid when the answer
// is not an object; the answer of an unknown request is logged and dropped.
func (s *Session) handleControlResponse(env envelope) error {
	var body controlResponseBody
	if err := decode(env.Response, &body, "control_response"); err != nil {
		return err
	}
	s.mu.Lock()
	p := s.pending[body.RequestID]
	delete(s.pending, body.RequestID)
	s.mu.Unlock()
	if p == nil {
		s.adapter.logger().Debug("control response for no request", "id", body.RequestID)
		return nil
	}
	if body.Subtype == "error" {
		msg := body.Error
		if msg == "" {
			msg = "unknown error"
		}
		p.ch <- controlResult{err: errors.New("ControlRequestFailed", msg, "request", p.subtype, "id", body.RequestID)}
		return nil
	}
	p.ch <- controlResult{body: body.Response}
	return nil
}

// Interrupt stops the turn that is running. The program ends the turn with a result message.
func (s *Session) Interrupt(ctx context.Context) error {
	_, err := s.control(ctx, "interrupt", nil)
	return err
}

// SetPermissionMode switches the session to a mode. It fails with InvalidOptions for a mode the adapter does not offer.
func (s *Session) SetPermissionMode(ctx context.Context, mode PermissionMode) error {
	if !mode.Valid() {
		return errors.New("InvalidOptions", "unknown permission mode", "mode", string(mode))
	}
	_, err := s.control(ctx, "set_permission_mode", map[string]any{"mode": string(mode)})
	return err
}

// SetModel switches the session to a model (a Value of the handshake's Models); "" goes back to the default model.
func (s *Session) SetModel(ctx context.Context, model string) error {
	var m any
	if model != "" {
		m = model
	}
	_, err := s.control(ctx, "set_model", map[string]any{"model": m})
	return err
}

// RewindFiles restores the files the agent changed to their state at the user message with that UUID (the UUID of the
// UserMessage the program replayed). It needs the file checkpoints, which the adapter turns on. A program that cannot
// restore answers with CanRewind false and the reason in Error; that is a result, not an error.
func (s *Session) RewindFiles(ctx context.Context, userMessageID string) (RewindResult, error) {
	body, err := s.control(ctx, "rewind_files", map[string]any{"user_message_id": userMessageID})
	if err != nil {
		return RewindResult{}, err
	}
	var w struct {
		CanRewind    *bool    `json:"canRewind"`
		Error        string   `json:"error"`
		FilesChanged []string `json:"filesChanged"`
		Insertions   int      `json:"insertions"`
		Deletions    int      `json:"deletions"`
	}
	if len(bytes.TrimSpace(body)) > 0 {
		if err := decode(body, &w, "rewind_files response"); err != nil {
			return RewindResult{}, err
		}
	}
	// An answer with no canRewind (an empty success) means the files were restored.
	res := RewindResult{CanRewind: w.CanRewind == nil || *w.CanRewind, Error: w.Error, FilesChanged: w.FilesChanged, Insertions: w.Insertions, Deletions: w.Deletions}
	return res, nil
}

// initialize sends the initialize request and reads the handshake. It fails with the errors of control and with
// HandshakeInvalid.
func (s *Session) initialize(ctx context.Context) (Handshake, error) {
	body, err := s.control(ctx, "initialize", nil)
	if err != nil {
		return Handshake{}, err
	}
	var h Handshake
	if len(bytes.TrimSpace(body)) > 0 {
		if err := json.Unmarshal(body, &h); err != nil {
			return Handshake{}, errors.NewErr("HandshakeInvalid", err)
		}
	}
	h.Raw = append(json.RawMessage(nil), body...)
	return h, nil
}

// allowJSON and denyJSON are the bodies of the answer to a can_use_tool request.
type allowJSON struct {
	Behavior           string             `json:"behavior"`
	UpdatedInput       json.RawMessage    `json:"updatedInput"`
	UpdatedPermissions []PermissionUpdate `json:"updatedPermissions,omitempty"`
}

type denyJSON struct {
	Behavior  string `json:"behavior"`
	Message   string `json:"message"`
	Interrupt bool   `json:"interrupt,omitempty"`
}

// decisionBody is the body of the answer to a can_use_tool request.
func decisionBody(req ToolRequest, d Decision) any {
	if !d.Allow {
		return denyJSON{Behavior: "deny", Message: d.Message, Interrupt: d.Interrupt}
	}
	input := d.UpdatedInput
	if len(input) == 0 {
		input = req.Input // the program wants the input back whether or not it changed
	}
	if len(input) == 0 {
		input = json.RawMessage("{}")
	}
	return allowJSON{Behavior: "allow", UpdatedInput: input, UpdatedPermissions: d.UpdatedPermissions}
}

// controlResponseLine is the line an answer is written as.
type controlResponseLine struct {
	Type     string `json:"type"`
	Response any    `json:"response"`
}

type successResponse struct {
	Subtype   string `json:"subtype"`
	RequestID string `json:"request_id"`
	Response  any    `json:"response"`
}

type errorResponse struct {
	Subtype   string `json:"subtype"`
	RequestID string `json:"request_id"`
	Error     string `json:"error"`
}

// handleControlRequest starts the answer to a request of the program. A can_use_tool request is answered by
// CanUseTool, from a goroutine of its own; any other request gets an error answer (the adapter registers no hooks and
// no SDK servers, so the program has no reason to send one).
func (s *Session) handleControlRequest(env envelope) error {
	var head struct {
		Subtype string `json:"subtype"`
	}
	if err := decode(env.Request, &head, "control_request"); err != nil {
		s.respondError(env.RequestID, "the request is not an object")
		return err
	}
	if head.Subtype != "can_use_tool" {
		s.adapter.logger().Warn("control request not supported", "subtype", head.Subtype)
		s.respondError(env.RequestID, "unsupported control request: "+head.Subtype)
		return nil
	}
	var w toolRequestJSON
	if err := decode(env.Request, &w, "can_use_tool"); err != nil {
		s.respondError(env.RequestID, "the can_use_tool request is invalid")
		return err
	}
	if w.ToolName == "" {
		s.respondError(env.RequestID, "the can_use_tool request has no tool_name")
		return errors.New("ToolRequestInvalid", "a can_use_tool request has no tool_name", "id", env.RequestID)
	}
	req := ToolRequest{
		RequestID: env.RequestID, ToolName: w.ToolName, Input: w.Input, ToolUseID: w.ToolUseID, AgentID: w.AgentID,
		Suggestions: w.Suggestions, BlockedPath: w.BlockedPath, DecisionReason: w.DecisionReason, Title: w.Title,
		DisplayName: w.DisplayName, Description: w.Description,
	}
	ctx, cancel := context.WithCancel(s.hctx)
	s.mu.Lock()
	s.handlers[env.RequestID] = cancel
	s.mu.Unlock()
	go s.serveToolRequest(ctx, req)
	return nil
}

// serveToolRequest asks the handler and writes the answer, unless the request was cancelled meanwhile.
func (s *Session) serveToolRequest(ctx context.Context, req ToolRequest) {
	var dec Decision
	var err error
	if s.canUseTool == nil {
		dec = Decision{Message: "Emetos has no handler for tool requests"}
	} else {
		dec, err = s.canUseTool(ctx, req)
	}
	cancelled := ctx.Err() != nil // by the program or because the session ended
	s.mu.Lock()
	cancel := s.handlers[req.RequestID]
	delete(s.handlers, req.RequestID)
	s.mu.Unlock()
	if cancel != nil {
		cancel()
	}
	if cancelled {
		return // no answer is written
	}
	if err != nil {
		s.adapter.logger().Error("the tool request handler failed", "err", err, "tool", req.ToolName)
		dec = Decision{Message: "The request could not be answered: " + err.Error()}
	}
	if werr := s.write(controlResponseLine{Type: "control_response", Response: successResponse{
		Subtype: "success", RequestID: req.RequestID, Response: decisionBody(req, dec)}}); werr != nil {
		s.adapter.logger().Warn("the answer to a tool request was not written", "err", werr, "tool", req.ToolName)
	}
}

// cancelHandler ends the handler of a request the program cancelled (control_cancel_request).
func (s *Session) cancelHandler(id string) {
	s.mu.Lock()
	cancel := s.handlers[id]
	s.mu.Unlock()
	if cancel != nil {
		cancel()
	}
}

// respondError answers a request of the program with an error.
func (s *Session) respondError(id, msg string) {
	err := s.write(controlResponseLine{Type: "control_response", Response: errorResponse{Subtype: "error", RequestID: id, Error: msg}})
	if err != nil {
		s.adapter.logger().Warn("the error answer was not written", "err", err)
	}
}
