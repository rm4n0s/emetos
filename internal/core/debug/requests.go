package debug

import (
	"context"
	"encoding/json"

	"github.com/rm4n0s/emetos/internal/core/dap"
	"github.com/rm4n0s/errors"
)

// rawClient returns the client of the session, or nil when it has none (not started, or over).
func (s *Session) rawClient() *dap.Client {
	s.m.mu.Lock()
	defer s.m.mu.Unlock()
	return s.client
}

// do sends a request. A failed response is an error with the adapter's message (the tags of core/dap).
func (s *Session) do(ctx context.Context, command string, args any) (dap.Response, error) {
	c := s.rawClient()
	if c == nil {
		return dap.Response{}, errors.New("DebugNoAdapter", "No debugger available, can not send '"+command+"'", "session", s.id, "command", command)
	}
	return c.Request(ctx, command, args)
}

// callBody sends a request and decodes the body of the response.
func callBody[R any](ctx context.Context, s *Session, command string, args any) (R, error) {
	c := s.rawClient()
	if c == nil {
		var zero R
		return zero, errors.New("DebugNoAdapter", "No debugger available, can not send '"+command+"'", "session", s.id, "command", command)
	}
	return dap.Call[R](ctx, c, command, args)
}

// ---- cancellation

// threadCtx returns a context that ends when ctx does, when the session is over, or when the thread continues (its requests are
// cancelled then, as VS Code does); the cancel function releases it.
func (s *Session) threadCtx(ctx context.Context, threadID int) (context.Context, context.CancelFunc) {
	s.m.mu.Lock()
	var epoch *reqEpoch
	if threadID != 0 {
		epoch = s.reqCtx[threadID]
		if epoch == nil {
			c, cancel := context.WithCancel(s.life)
			epoch = &reqEpoch{ctx: c, cancel: cancel}
			s.reqCtx[threadID] = epoch
		}
	}
	life := s.life
	s.m.mu.Unlock()
	c, cancel := context.WithCancel(ctx)
	stops := []func() bool{context.AfterFunc(life, cancel)}
	if epoch != nil {
		stops = append(stops, context.AfterFunc(epoch.ctx, cancel))
	}
	return c, func() {
		for _, stop := range stops {
			stop()
		}
		cancel()
	}
}

func (s *Session) cancelThreadRequestsLocked(threadID int) {
	if e := s.reqCtx[threadID]; e != nil {
		e.cancel()
		delete(s.reqCtx, threadID)
	}
}

func (s *Session) cancelAllRequestsLocked() {
	for id, e := range s.reqCtx {
		e.cancel()
		delete(s.reqCtx, id)
	}
}

func (s *Session) cancelAllRequests() {
	s.m.mu.Lock()
	s.cancelAllRequestsLocked()
	s.m.mu.Unlock()
}

// ---- typed requests used by the model

func (s *Session) stackTrace(ctx context.Context, threadID, start, levels int) (stackTraceBody, error) {
	tctx, done := s.threadCtx(ctx, threadID)
	defer done()
	return callBody[stackTraceBody](tctx, s, dap.CmdStackTrace, dap.StackTraceArguments{ThreadID: threadID, StartFrame: start, Levels: levels})
}

// stackTraceBody is the body of a stackTrace response, with canRestart as the adapter sent it (a frame that does not say can be
// restarted, as VS Code reads it).
type stackTraceBody struct {
	StackFrames []frameWire `json:"stackFrames"`
	TotalFrames int         `json:"totalFrames"`
}

type frameWire struct {
	dap.StackFrame
	CanRestartRaw *bool `json:"canRestart"`
}

func (f frameWire) canRestart() bool { return f.CanRestartRaw == nil || *f.CanRestartRaw }

func (s *Session) scopes(ctx context.Context, frameID, threadID int) (dap.ScopesResponseBody, error) {
	tctx, done := s.threadCtx(ctx, threadID)
	defer done()
	return callBody[dap.ScopesResponseBody](tctx, s, dap.CmdScopes, dap.ScopesArguments{FrameID: frameID})
}

func (s *Session) variables(ctx context.Context, ref, threadID int, filter string, start, count int) (dap.VariablesResponseBody, error) {
	tctx, done := s.threadCtx(ctx, threadID)
	defer done()
	return callBody[dap.VariablesResponseBody](tctx, s, dap.CmdVariables, dap.VariablesArguments{VariablesReference: ref, Filter: filter, Start: start, Count: count})
}

func (s *Session) evaluate(ctx context.Context, expr string, frameID int, evalContext string) (dap.EvaluateResponseBody, error) {
	return callBody[dap.EvaluateResponseBody](ctx, s, dap.CmdEvaluate, dap.EvaluateArguments{Expression: expr, FrameID: frameID, Context: evalContext})
}

func (s *Session) setVariable(ctx context.Context, ref int, name, value string) (dap.SetVariableResponseBody, error) {
	if !s.Capabilities().SupportsSetVariable {
		return dap.SetVariableResponseBody{}, errors.New("DebugNotSupported", dap.CmdSetVariable+" not supported", "session", s.id, "request", dap.CmdSetVariable)
	}
	return callBody[dap.SetVariableResponseBody](ctx, s, dap.CmdSetVariable, dap.SetVariableArguments{VariablesReference: ref, Name: name, Value: value})
}

func (s *Session) setExpression(ctx context.Context, frameID int, expr, value string) (dap.SetExpressionResponseBody, error) {
	if !s.Capabilities().SupportsSetExpression {
		return dap.SetExpressionResponseBody{}, errors.New("DebugNotSupported", dap.CmdSetExpression+" not supported", "session", s.id, "request", dap.CmdSetExpression)
	}
	return callBody[dap.SetExpressionResponseBody](ctx, s, dap.CmdSetExpression, dap.SetExpressionArguments{Expression: expr, Value: value, FrameID: frameID})
}

// ---- requests for the callers

// Evaluate evaluates an expression in a frame (or in the session alone with a zero frame id and the context "repl") and returns the
// result as an Expression node. The context is one of the dap.Context... values. A failure is the value of the node and the error.
func (s *Session) Evaluate(ctx context.Context, expr string, frame *StackFrame, evalContext string) (*Expression, error) {
	e := NewExpression(expr)
	err := e.Evaluate(ctx, s, frame, evalContext, false)
	return e, err
}

// ExceptionInfo asks for the exception a thread stopped on.
func (s *Session) ExceptionInfo(ctx context.Context, threadID int) (*ExceptionInfo, error) {
	body, err := callBody[dap.ExceptionInfoResponseBody](ctx, s, dap.CmdExceptionInfo, dap.ExceptionInfoArguments{ThreadID: threadID})
	if err != nil {
		return nil, err
	}
	return &ExceptionInfo{ID: body.ExceptionID, Description: body.Description, BreakMode: body.BreakMode, Details: body.Details}, nil
}

func (s *Session) setLastGranularity(threadID int, g string) {
	if g == "" {
		return
	}
	s.m.mu.Lock()
	if t := s.threads[threadID]; t != nil {
		t.lastGranularity = g
	}
	s.m.mu.Unlock()
}

// step sends a request that resumes the thread, and, when no stopped event came before its response, acts as if a continued event
// had come (the adapter did not say; VS Code does the same).
func (s *Session) step(ctx context.Context, command string, args any, threadID int, allThreads bool) error {
	s.waitForTriggered(ctx)
	s.m.mu.Lock()
	s.stoppedSinceLastStep = false
	s.m.mu.Unlock()
	resp, err := s.do(ctx, command, args)
	if err != nil {
		return err
	}
	if command == dap.CmdContinue {
		var body dap.ContinueResponseBody
		if len(resp.Body) > 0 && json.Unmarshal(resp.Body, &body) == nil && body.AllThreadsContinued != nil {
			s.m.mu.Lock()
			s.allThreadsContinued = *body.AllThreadsContinued
			s.m.mu.Unlock()
		}
	}
	s.waitForReadEvents(ctx)
	s.m.mu.Lock()
	stopped, all := s.stoppedSinceLastStep, s.allThreadsContinued
	s.m.mu.Unlock()
	if !stopped {
		if command != dap.CmdContinue {
			all = allThreads
		}
		s.onContinued(threadID, all)
	}
	return nil
}

// Next steps over.
func (s *Session) Next(ctx context.Context, threadID int, granularity string) error {
	s.setLastGranularity(threadID, granularity)
	return s.step(ctx, dap.CmdNext, dap.StepArguments{ThreadID: threadID, Granularity: granularity}, threadID, false)
}

// StepIn steps into; targetID picks the target when the adapter offered several (stepInTargets).
func (s *Session) StepIn(ctx context.Context, threadID, targetID int, granularity string) error {
	s.setLastGranularity(threadID, granularity)
	return s.step(ctx, dap.CmdStepIn, dap.StepArguments{ThreadID: threadID, TargetID: targetID, Granularity: granularity}, threadID, false)
}

// StepOut steps out.
func (s *Session) StepOut(ctx context.Context, threadID int, granularity string) error {
	s.setLastGranularity(threadID, granularity)
	return s.step(ctx, dap.CmdStepOut, dap.StepArguments{ThreadID: threadID, Granularity: granularity}, threadID, false)
}

// StepBack steps back; the adapter must support reverse debugging.
func (s *Session) StepBack(ctx context.Context, threadID int, granularity string) error {
	if !s.Capabilities().SupportsStepBack {
		return errors.New("DebugNotSupported", dap.CmdStepBack+" not supported", "session", s.id, "request", dap.CmdStepBack)
	}
	s.setLastGranularity(threadID, granularity)
	return s.step(ctx, dap.CmdStepBack, dap.StepArguments{ThreadID: threadID, Granularity: granularity}, threadID, false)
}

// Continue resumes the thread.
func (s *Session) Continue(ctx context.Context, threadID int) error {
	return s.step(ctx, dap.CmdContinue, dap.ContinueArguments{ThreadID: threadID}, threadID, true)
}

// ReverseContinue resumes the thread backwards.
func (s *Session) ReverseContinue(ctx context.Context, threadID int) error {
	if !s.Capabilities().SupportsStepBack {
		return errors.New("DebugNotSupported", dap.CmdReverseContinue+" not supported", "session", s.id, "request", dap.CmdReverseContinue)
	}
	return s.step(ctx, dap.CmdReverseContinue, dap.ContinueArguments{ThreadID: threadID}, threadID, true)
}

// Pause pauses the thread.
func (s *Session) Pause(ctx context.Context, threadID int) error {
	_, err := s.do(ctx, dap.CmdPause, dap.PauseArguments{ThreadID: threadID})
	return err
}

// TerminateThreads ends threads of the debuggee, when the adapter can.
func (s *Session) TerminateThreads(ctx context.Context, threadIDs []int) error {
	if !s.Capabilities().SupportsTerminateThreadsRequest {
		return errors.New("DebugNotSupported", dap.CmdTerminateThreads+" not supported", "session", s.id, "request", dap.CmdTerminateThreads)
	}
	_, err := s.do(ctx, dap.CmdTerminateThreads, dap.TerminateThreadsArguments{ThreadIDs: threadIDs})
	return err
}

// RestartFrame restarts a stack frame, when the adapter can.
func (s *Session) RestartFrame(ctx context.Context, frameID, threadID int) error {
	if !s.Capabilities().SupportsRestartFrame {
		return errors.New("DebugNotSupported", dap.CmdRestartFrame+" not supported", "session", s.id, "request", dap.CmdRestartFrame)
	}
	return s.step(ctx, dap.CmdRestartFrame, dap.RestartFrameArguments{FrameID: frameID}, threadID, false)
}

// GotoTargets asks where execution can jump to from a place of a source.
func (s *Session) GotoTargets(ctx context.Context, path string, line, column int) ([]dap.GotoTarget, error) {
	if !s.Capabilities().SupportsGotoTargetsRequest {
		return nil, errors.New("DebugNotSupported", dap.CmdGotoTargets+" not supported", "session", s.id, "request", dap.CmdGotoTargets)
	}
	body, err := callBody[dap.GotoTargetsResponseBody](ctx, s, dap.CmdGotoTargets, dap.GotoTargetsArguments{Source: dap.Source{Path: path, Name: baseName(path)}, Line: line, Column: column})
	return body.Targets, err
}

// Goto jumps to a target of GotoTargets.
func (s *Session) Goto(ctx context.Context, threadID, targetID int) error {
	if !s.Capabilities().SupportsGotoTargetsRequest {
		return errors.New("DebugNotSupported", dap.CmdGoto+" not supported", "session", s.id, "request", dap.CmdGoto)
	}
	return s.step(ctx, dap.CmdGoto, dap.GotoArguments{ThreadID: threadID, TargetID: targetID}, threadID, false)
}

// StepInTargets asks for the targets a step into can go to in a frame.
func (s *Session) StepInTargets(ctx context.Context, frameID int) ([]dap.StepInTarget, error) {
	if !s.Capabilities().SupportsStepInTargetsRequest {
		return nil, errors.New("DebugNotSupported", dap.CmdStepInTargets+" not supported", "session", s.id, "request", dap.CmdStepInTargets)
	}
	body, err := callBody[dap.StepInTargetsResponseBody](ctx, s, dap.CmdStepInTargets, dap.StepInTargetsArguments{FrameID: frameID})
	return body.Targets, err
}

// Completions asks the adapter for the completions of the Debug Console's input at a column (counted from 1).
func (s *Session) Completions(ctx context.Context, frameID int, text string, column int) ([]dap.CompletionItem, error) {
	if !s.Capabilities().SupportsCompletionsRequest {
		return nil, errors.New("DebugNotSupported", dap.CmdCompletions+" not supported", "session", s.id, "request", dap.CmdCompletions)
	}
	body, err := callBody[dap.CompletionsResponseBody](ctx, s, dap.CmdCompletions, dap.CompletionsArguments{FrameID: frameID, Text: text, Column: column})
	return body.Targets, err
}

// LoadSource reads a source the adapter serves (a source with a sourceReference).
func (s *Session) LoadSource(ctx context.Context, src Source) (string, error) {
	args := dap.SourceArguments{SourceReference: src.Raw.SourceReference}
	if src.Raw.SourceReference != 0 || src.Path != "" {
		raw := src.Raw
		args.Source = &raw
	}
	body, err := callBody[dap.SourceResponseBody](ctx, s, dap.CmdSource, args)
	return body.Content, err
}

// LoadedSources asks for the sources the debuggee has loaded (Loaded Scripts), when the adapter can.
func (s *Session) LoadedSources(ctx context.Context) ([]Source, error) {
	if !s.Capabilities().SupportsLoadedSourcesRequest {
		return nil, nil
	}
	body, err := callBody[dap.LoadedSourcesResponseBody](ctx, s, dap.CmdLoadedSources, nil)
	if err != nil {
		return nil, err
	}
	out := make([]Source, 0, len(body.Sources))
	for i := range body.Sources {
		out = append(out, s.source(&body.Sources[i]))
	}
	return out, nil
}

// BreakpointLocations asks where a breakpoint can be set on a line (the inline breakpoint candidates), when the adapter can.
func (s *Session) BreakpointLocations(ctx context.Context, path string, line int) ([]dap.BreakpointLocation, error) {
	if !s.Capabilities().SupportsBreakpointLocationsRequest {
		return nil, nil
	}
	body, err := callBody[dap.BreakpointLocationsResponseBody](ctx, s, dap.CmdBreakpointLocations, dap.BreakpointLocationsArguments{Source: dap.Source{Path: path, Name: baseName(path)}, Line: line})
	return body.Breakpoints, err
}

// CancelProgress cancels a progress the adapter started (cancel with a progressId).
func (s *Session) CancelProgress(ctx context.Context, progressID string) error {
	_, err := s.do(ctx, dap.CmdCancel, dap.CancelArguments{ProgressID: progressID})
	return err
}

// CustomRequest sends a request the protocol does not define, and returns the body of the response. It is the way of the extensions
// of a debugger.
func (s *Session) CustomRequest(ctx context.Context, command string, args any) (json.RawMessage, error) {
	resp, err := s.do(ctx, command, args)
	if err != nil {
		return nil, err
	}
	return resp.Body, nil
}

// ---- breakpoints, per session

// sourceFor is the DAP source of a file.
func sourceFor(path string, adapterData json.RawMessage) dap.Source {
	return dap.Source{Name: baseName(path), Path: path, AdapterData: adapterData}
}

// sendBreakpoints replaces the adapter's breakpoints of a file with these (all of them, so an empty list clears the file) and
// records the answers.
func (s *Session) sendBreakpoints(ctx context.Context, path string, bps []Breakpoint, sourceModified bool) error {
	s.m.mu.Lock()
	ready, client := s.ready, s.client
	s.m.mu.Unlock()
	if client == nil {
		return errors.New("DebugNoAdapter", "No debugger available, can not send 'setBreakpoints'", "session", s.id, "command", dap.CmdSetBreakpoints)
	}
	if !ready {
		return nil
	}
	var adapterData json.RawMessage
	if len(bps) > 0 {
		adapterData = bps[0].AdapterData
	}
	wire := make([]dap.SourceBreakpoint, 0, len(bps))
	for _, b := range bps {
		wire = append(wire, dap.SourceBreakpoint{
			Line: b.RequestedLine, Column: b.RequestedColumn, Condition: b.Condition, HitCondition: b.HitCondition,
			LogMessage: b.LogMessage, Mode: b.Mode,
		})
	}
	body, err := dap.Call[dap.BreakpointsResponseBody](ctx, client, dap.CmdSetBreakpoints, dap.SetBreakpointsArguments{
		Source: sourceFor(path, adapterData), Breakpoints: wire, SourceModified: sourceModified,
	})
	if err != nil {
		return err
	}
	data := map[string]dap.Breakpoint{}
	for i, b := range bps {
		if i < len(body.Breakpoints) {
			data[b.ID] = body.Breakpoints[i]
		}
	}
	s.m.setBreakpointSessionData(s.id, client.Capabilities(), data)
	return nil
}

func (s *Session) sendFunctionBreakpoints(ctx context.Context, fbs []FunctionBreakpoint) error {
	s.m.mu.Lock()
	ready, client := s.ready, s.client
	s.m.mu.Unlock()
	if client == nil {
		return errors.New("DebugNoAdapter", "No debugger available, can not send 'setFunctionBreakpoints'", "session", s.id, "command", dap.CmdSetFunctionBreakpoints)
	}
	if !ready {
		return nil
	}
	wire := make([]dap.FunctionBreakpoint, 0, len(fbs))
	for _, b := range fbs {
		wire = append(wire, dap.FunctionBreakpoint{Name: b.Name, Condition: b.Condition, HitCondition: b.HitCondition})
	}
	body, err := dap.Call[dap.BreakpointsResponseBody](ctx, client, dap.CmdSetFunctionBreakpoints, dap.SetFunctionBreakpointsArguments{Breakpoints: wire})
	if err != nil {
		return err
	}
	data := map[string]dap.Breakpoint{}
	for i, b := range fbs {
		if i < len(body.Breakpoints) {
			data[b.ID] = body.Breakpoints[i]
		}
	}
	s.m.setBreakpointSessionData(s.id, client.Capabilities(), data)
	return nil
}

func (s *Session) sendExceptionBreakpoints(ctx context.Context, ebs []ExceptionBreakpoint) error {
	s.m.mu.Lock()
	ready, client := s.ready, s.client
	s.m.mu.Unlock()
	if client == nil {
		return errors.New("DebugNoAdapter", "No debugger available, can not send 'setExceptionBreakpoints'", "session", s.id, "command", dap.CmdSetExceptionBreakpoints)
	}
	if !ready {
		return nil
	}
	args := dap.SetExceptionBreakpointsArguments{Filters: []string{}}
	if client.Capabilities().SupportsExceptionFilterOptions {
		args.FilterOptions = make([]dap.ExceptionFilterOptions, 0, len(ebs))
		for _, b := range ebs {
			args.FilterOptions = append(args.FilterOptions, dap.ExceptionFilterOptions{FilterID: b.Filter, Condition: b.Condition})
		}
	} else {
		for _, b := range ebs {
			args.Filters = append(args.Filters, b.Filter)
		}
	}
	body, err := dap.Call[dap.SetExceptionBreakpointsResponseBody](ctx, client, dap.CmdSetExceptionBreakpoints, args)
	if err != nil {
		return err
	}
	if len(body.Breakpoints) > 0 {
		data := map[string]dap.Breakpoint{}
		for i, b := range ebs {
			if i < len(body.Breakpoints) {
				data[b.ID] = body.Breakpoints[i]
			}
		}
		s.m.setBreakpointSessionData(s.id, client.Capabilities(), data)
	}
	return nil
}

// waitForReadEvents waits until the event loop has handled the events the connection had read when this was called. A response is
// handed over as soon as it is read, so it can be seen before the events that came before it on the wire; a step that needs to know
// whether a stopped event came before its response calls this first.
func (s *Session) waitForReadEvents(ctx context.Context) {
	target := s.evRead.Load()
	s.evMu.Lock()
	if s.evHandled.Load() >= target || s.evDone {
		s.evMu.Unlock()
		return
	}
	s.evMu.Unlock()
	stop := context.AfterFunc(ctx, func() {
		s.evMu.Lock()
		s.evCond.Broadcast()
		s.evMu.Unlock()
	})
	defer stop()
	s.evMu.Lock()
	for s.evHandled.Load() < target && !s.evDone && ctx.Err() == nil {
		s.evCond.Wait()
	}
	s.evMu.Unlock()
}
