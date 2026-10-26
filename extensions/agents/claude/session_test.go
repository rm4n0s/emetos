package claude

import (
	"context"
	"encoding/json"
	"os"
	"reflect"
	"strings"
	"sync"
	"syscall"
	"testing"
	"time"
)

func TestTurn(t *testing.T) {
	r := newRig(t, "turn.jsonl")
	o := r.opts()
	o.SessionID = "11111111-2222-4333-8444-555555555555"
	s := r.start(context.Background(), o)

	if hs := s.Handshake(); hs.Commands == nil && len(hs.Raw) == 0 {
		t.Errorf("no handshake: %+v", hs)
	}
	id, err := s.Send(context.Background(), "What is in main.go?")
	if err != nil {
		t.Fatal(err)
	}
	if !uuidPattern.MatchString(id) {
		t.Errorf("Send returned %q", id)
	}
	res := r.waitResult()
	if res.Subtype != "success" || res.NumTurns != 2 || res.TotalCostUSD != 0.0123 || res.Result != "It is a Go file." {
		t.Errorf("result = %+v", res)
	}

	// the events came in the order of the program, each of its type
	var kinds []string
	var parts Partials
	var streamedText string
	for _, ev := range r.snapshot() {
		switch m := ev.(type) {
		case *InitMessage:
			kinds = append(kinds, "init")
		case *UserMessage:
			if m.IsReplay {
				kinds = append(kinds, "user-replay")
				if m.Text() != "What is in main.go?" {
					t.Errorf("replayed text = %q", m.Text())
				}
				if m.UUID != id {
					t.Errorf("the replayed UUID %q is not the one Send returned (%q)", m.UUID, id)
				}
			} else {
				kinds = append(kinds, "tool-result")
				if b := m.Blocks[0]; b.ToolUseID != "toolu_1" || b.ResultText() != "package main\n" {
					t.Errorf("tool result = %+v", b)
				}
			}
		case *StreamEvent:
			if len(kinds) == 0 || kinds[len(kinds)-1] != "stream" {
				kinds = append(kinds, "stream")
			}
			parts.Apply(m)
			if m.EventType == "message_stop" {
				streamedText = parts.Text("")
			}
		case *AssistantMessage:
			kinds = append(kinds, "assistant")
			parts.Finish(m.ParentToolUseID)
		case *ResultMessage:
			kinds = append(kinds, "result")
		}
	}
	if want := []string{"init", "user-replay", "stream", "assistant", "tool-result", "assistant", "result"}; !reflect.DeepEqual(kinds, want) {
		t.Errorf("events = %v, want %v", kinds, want)
	}
	if streamedText != "Hello, world." {
		t.Errorf("the partial text joined to %q", streamedText)
	}

	// the capabilities and the session ID come from system/init
	if !s.Capabilities().Has(CapInterruptReceipt) || s.Capabilities().Has(CapMCPReadResource) {
		t.Errorf("capabilities = %v", s.Capabilities())
	}
	if s.SessionID() != "11111111-2222-4333-8444-555555555555" || s.Init() == nil || s.Init().Model != "claude-sonnet" {
		t.Errorf("session ID %q, init %+v", s.SessionID(), s.Init())
	}

	// what the program read: the user message as the protocol says
	users := r.incoming("user")
	if len(users) != 1 {
		t.Fatalf("user lines = %v", users)
	}
	msg, _ := users[0]["message"].(map[string]any)
	if users[0]["uuid"] != id || msg["role"] != "user" || msg["content"] != "What is in main.go?" || users[0]["parent_tool_use_id"] != nil {
		t.Errorf("user line = %v", users[0])
	}
	init := r.controlRequests("initialize")
	if len(init) != 1 {
		t.Errorf("initialize requests = %v", init)
	}
}

func TestStartedProgram(t *testing.T) {
	r := newRig(t, "idle.jsonl")
	o := r.opts()
	o.PermissionMode = ModePlan
	o.Model = "opus"
	o.Resume = "abc-123"
	s := r.start(context.Background(), o)
	if err := s.Close(); err != nil {
		t.Fatal(err)
	}

	recs := r.records()
	if len(recs) == 0 || recs[0].Kind != "start" {
		t.Fatalf("log = %+v", recs)
	}
	start := recs[0]
	wantArgs, err := Args(o)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(start.Argv, wantArgs) {
		t.Errorf("the program was started with %q, want %q", start.Argv, wantArgs)
	}
	for _, a := range start.Argv {
		if a == "--bare" {
			t.Error("--bare")
		}
	}
	if start.CWD != r.dir {
		t.Errorf("the program ran in %q, want %q", start.CWD, r.dir)
	}
	// The environment: the sentinel of the user's environment arrives unchanged, the checkpoint variable is added, and
	// nothing names a configuration folder, a key or a token.
	if start.Env[sentinelName] != sentinelValue || start.Env[CheckpointingVariable] != "true" {
		t.Errorf("environment = %v", start.Env)
	}
	for name := range start.Env {
		if name != sentinelName && name != CheckpointingVariable {
			t.Errorf("the program got %s", name)
		}
	}
	// its own process group, not ours
	if start.PGID != start.PID || start.PID != s.PID() || start.PGID == syscall.Getpgrp() {
		t.Errorf("pid %d, group %d, session pid %d, our group %d", start.PID, start.PGID, s.PID(), syscall.Getpgrp())
	}
}

func TestToolRequestRoundTrip(t *testing.T) {
	r := newRig(t, "permission.jsonl")
	var mu sync.Mutex
	var got []ToolRequest
	o := r.opts()
	o.CanUseTool = func(ctx context.Context, req ToolRequest) (Decision, error) {
		mu.Lock()
		got = append(got, req)
		mu.Unlock()
		switch req.ToolName {
		case "Edit":
			return Decision{
				Allow:              true,
				UpdatedInput:       json.RawMessage(`{"file_path":"/work/main.go","old_string":"a","new_string":"changed by the user"}`),
				UpdatedPermissions: []PermissionUpdate{req.Suggestions[0].WithDestination("session")},
			}, nil
		case "Bash":
			return Decision{Message: "Not with rm.", Interrupt: true}, nil
		}
		return Decision{}, os.ErrPermission // a handler that fails
	}
	s := r.start(context.Background(), o)
	if _, err := s.Send(context.Background(), "edit it"); err != nil {
		t.Fatal(err)
	}
	r.waitResult()

	mu.Lock()
	defer mu.Unlock()
	if len(got) != 3 {
		t.Fatalf("requests = %+v", got)
	}
	e := got[0]
	if e.RequestID != "perm_1" || e.ToolName != "Edit" || e.ToolUseID != "toolu_edit" || e.BlockedPath != "/work/main.go" || e.Title != "Edit main.go?" ||
		e.DisplayName != "Edit file" || e.Description != "Change a to b" || e.DecisionReason != "edits ask in this mode" ||
		!strings.Contains(string(e.Input), `"new_string":"b"`) {
		t.Errorf("request 1 = %+v", e)
	}
	if len(e.Suggestions) != 1 || e.Suggestions[0].Type != "addRules" || e.Suggestions[0].Rules[0].ToolName != "Edit" {
		t.Errorf("suggestions = %+v", e.Suggestions)
	}
	if got[1].ToolName != "Bash" || got[1].AgentID != "agent_9" {
		t.Errorf("request 2 = %+v", got[1])
	}

	a1 := r.answerTo("perm_1")
	body, _ := a1["response"].(map[string]any)
	if a1["subtype"] != "success" || body["behavior"] != "allow" {
		t.Fatalf("answer 1 = %v", a1)
	}
	if in, _ := body["updatedInput"].(map[string]any); in["new_string"] != "changed by the user" {
		t.Errorf("updatedInput = %v", body["updatedInput"])
	}
	perms, _ := body["updatedPermissions"].([]any)
	if len(perms) != 1 {
		t.Fatalf("updatedPermissions = %v", body["updatedPermissions"])
	}
	if p, _ := perms[0].(map[string]any); p["destination"] != "session" || p["type"] != "addRules" {
		t.Errorf("update = %v", perms[0])
	}

	a2 := r.answerTo("perm_2")
	body, _ = a2["response"].(map[string]any)
	if body["behavior"] != "deny" || body["message"] != "Not with rm." || body["interrupt"] != true {
		t.Errorf("answer 2 = %v", a2)
	}

	a3 := r.answerTo("perm_3")
	body, _ = a3["response"].(map[string]any)
	if body["behavior"] != "deny" || !strings.Contains(body["message"].(string), "could not be answered") {
		t.Errorf("answer 3 (a handler that failed) = %v", a3)
	}
}

func TestToolRequestWithoutHandlerIsDenied(t *testing.T) {
	r := newRig(t, "permission.jsonl")
	s := r.start(context.Background(), r.opts())
	if _, err := s.Send(context.Background(), "go"); err != nil {
		t.Fatal(err)
	}
	r.waitResult()
	for _, id := range []string{"perm_1", "perm_2", "perm_3"} {
		body, _ := r.answerTo(id)["response"].(map[string]any)
		if body["behavior"] != "deny" {
			t.Errorf("%s answered %v", id, body)
		}
	}
}

func TestToolRequestCancelledByTheProgram(t *testing.T) {
	r := newRig(t, "cancel_request.jsonl")
	started := make(chan struct{})
	ended := make(chan error, 1)
	o := r.opts()
	o.CanUseTool = func(ctx context.Context, req ToolRequest) (Decision, error) {
		close(started)
		select {
		case <-ctx.Done():
			ended <- ctx.Err()
		case <-time.After(waitLimit):
			ended <- nil
		}
		return Decision{Allow: true}, nil
	}
	s := r.start(context.Background(), o)
	if _, err := s.Send(context.Background(), "run it"); err != nil {
		t.Fatal(err)
	}
	<-started
	select {
	case err := <-ended:
		if err == nil {
			t.Fatal("the context of the handler was not cancelled")
		}
	case <-time.After(waitLimit):
		t.Fatal("the handler never ended")
	}
	// the script goes on after a second user message; by then no answer to c1 was written
	if _, err := s.Send(context.Background(), "next"); err != nil {
		t.Fatal(err)
	}
	r.waitResult()
	for _, m := range r.incoming("control_response") {
		if resp, _ := m["response"].(map[string]any); resp["request_id"] == "c1" {
			t.Errorf("an answer to the cancelled request was written: %v", m)
		}
	}
}

func TestBadRequestsOfTheProgram(t *testing.T) {
	r := newRig(t, "bad_requests.jsonl")
	s := r.start(context.Background(), r.opts())
	if _, err := s.Send(context.Background(), "go"); err != nil {
		t.Fatal(err)
	}
	r.waitResult()
	r.waitFor("a protocol error", func(e Event) bool {
		p, ok := e.(*ProtocolErrorEvent)
		return ok && tagOf(p.Err) == "ToolRequestInvalid"
	})
	for id, wantText := range map[string]string{"b1": "no tool_name", "b2": "unsupported control request: hook_callback"} {
		a := r.answerTo(id)
		if a["subtype"] != "error" || !strings.Contains(a["error"].(string), wantText) {
			t.Errorf("answer to %s = %v", id, a)
		}
	}
}

func TestInterrupt(t *testing.T) {
	r := newRig(t, "interrupt.jsonl")
	s := r.start(context.Background(), r.opts())
	if _, err := s.Send(context.Background(), "do a long thing"); err != nil {
		t.Fatal(err)
	}
	r.waitFor("partial text", func(e Event) bool {
		se, ok := e.(*StreamEvent)
		return ok && se.DeltaType == "text_delta"
	})
	if err := s.Interrupt(context.Background()); err != nil {
		t.Fatal(err)
	}
	res := r.waitResult()
	if res.Subtype != "error_during_execution" || !res.IsError || !reflect.DeepEqual(res.Errors, []string{"Interrupted by user"}) {
		t.Errorf("result = %+v", res)
	}
	if reqs := r.controlRequests("interrupt"); len(reqs) != 1 {
		t.Errorf("interrupt requests = %v", reqs)
	}
}

func TestControlRequests(t *testing.T) {
	r := newRig(t, "control.jsonl")
	s := r.start(context.Background(), r.opts())
	ctx := context.Background()

	hs := s.Handshake()
	if len(hs.Commands) != 2 || hs.Commands[0].Name != "compact" || hs.Commands[0].ArgumentHint != "[instructions]" || hs.Commands[1].Aliases[0] != "rv" {
		t.Errorf("commands = %+v", hs.Commands)
	}
	if len(hs.Models) != 2 || hs.Models[1].Value != "opus" || hs.Models[0].DisplayName != "Sonnet" || hs.Account.SubscriptionType != "max" || hs.OutputStyle != "default" {
		t.Errorf("handshake = %+v", hs)
	}
	if !strings.Contains(string(hs.Raw), `"agents":[]`) {
		t.Errorf("raw handshake = %s", hs.Raw)
	}

	if err := s.SetPermissionMode(ctx, ModeAcceptEdits); err != nil {
		t.Fatal(err)
	}
	if reqs := r.controlRequests("set_permission_mode"); len(reqs) != 1 || reqs[0]["mode"] != "acceptEdits" {
		t.Errorf("set_permission_mode = %v", reqs)
	}
	wantRoute(t, s.SetPermissionMode(ctx, "bypassPermissions"), "Session.SetPermissionMode.InvalidOptions")

	err := s.SetModel(ctx, "opus")
	appErr := routeOf(t, err, "Session.handleControlResponse.ControlRequestFailed")
	if appErr.Message != "unknown model" || appErr.Metadata["request"] != "set_model" {
		t.Errorf("error = %+v", appErr)
	}
	if err := s.SetModel(ctx, ""); err == nil {
		t.Error("the fake fails every set_model")
	}
	reqs := r.controlRequests("set_model")
	if len(reqs) != 2 || reqs[0]["model"] != "opus" {
		t.Fatalf("set_model = %v", reqs)
	}
	if v, present := reqs[1]["model"]; !present || v != nil {
		t.Errorf("SetModel(\"\") sent model %v, want null", v)
	}

	res, err := s.RewindFiles(ctx, "u-123")
	if err != nil {
		t.Fatal(err)
	}
	if !res.CanRewind || !reflect.DeepEqual(res.FilesChanged, []string{"a.go", "b.go"}) || res.Insertions != 3 || res.Deletions != 1 {
		t.Errorf("rewind = %+v", res)
	}
	if reqs := r.controlRequests("rewind_files"); len(reqs) != 1 || reqs[0]["user_message_id"] != "u-123" {
		t.Errorf("rewind_files = %v", reqs)
	}

	ids := map[string]bool{}
	for _, m := range r.incoming("control_request") {
		id, _ := m["request_id"].(string)
		if !strings.HasPrefix(id, "req_") || ids[id] {
			t.Errorf("request ID %q (repeated or malformed)", id)
		}
		ids[id] = true
	}
}

func TestRewindAnswers(t *testing.T) {
	t.Run("refused", func(t *testing.T) {
		r := newRig(t, "rewind_refused.jsonl")
		s := r.start(context.Background(), r.opts())
		res, err := s.RewindFiles(context.Background(), "u1")
		if err != nil {
			t.Fatal(err)
		}
		if res.CanRewind || res.Error != "no checkpoint for that message" {
			t.Errorf("rewind = %+v", res)
		}
	})
	t.Run("an empty success restored the files", func(t *testing.T) {
		r := newRig(t, "rewind_empty.jsonl")
		s := r.start(context.Background(), r.opts())
		res, err := s.RewindFiles(context.Background(), "u1")
		if err != nil || !res.CanRewind {
			t.Errorf("rewind = %+v, %v", res, err)
		}
	})
}

func TestControlRequestTimeoutAndCancel(t *testing.T) {
	t.Run("timeout of the handshake ends the process", func(t *testing.T) {
		r := newRig(t, "silent_init.jsonl")
		o := r.opts()
		o.RequestTimeout = 200 * time.Millisecond
		_, err := r.adapter.Start(context.Background(), o)
		appErr := routeOf(t, err, "Session.control.ControlRequestTimeout")
		if appErr.Metadata["request"] != "initialize" {
			t.Errorf("metadata = %v", appErr.Metadata)
		}
		recs := r.records()
		if len(recs) == 0 {
			t.Fatal("no log")
		}
		waitGone(t, "the program", recs[0].PID)
	})
	t.Run("timeout of a request", func(t *testing.T) {
		r := newRig(t, "silent_interrupt.jsonl")
		o := r.opts()
		o.RequestTimeout = 200 * time.Millisecond
		s := r.start(context.Background(), o)
		wantRoute(t, s.Interrupt(context.Background()), "Session.Interrupt->Session.control.ControlRequestTimeout")
	})
	t.Run("the context of the request ends", func(t *testing.T) {
		r := newRig(t, "silent_interrupt.jsonl")
		s := r.start(context.Background(), r.opts())
		ctx, cancel := context.WithTimeout(context.Background(), 150*time.Millisecond)
		defer cancel()
		wantRoute(t, s.Interrupt(ctx), "Session.control.ControlRequestCanceled")
	})
	t.Run("a handshake that is not valid", func(t *testing.T) {
		r := newRig(t, "bad_handshake.jsonl")
		_, err := r.adapter.Start(context.Background(), r.opts())
		wantRoute(t, err, "Session.initialize.HandshakeInvalid")
		waitGone(t, "the program", r.records()[0].PID)
	})
}

func TestDeadProgram(t *testing.T) {
	t.Run("the program dies in a turn", func(t *testing.T) {
		r := newRig(t, "crash.jsonl")
		s := r.start(context.Background(), r.opts())
		if _, err := s.Send(context.Background(), "hello"); err != nil {
			t.Fatal(err)
		}
		ev := r.waitFor("the exit", func(e Event) bool { _, ok := e.(*ExitEvent); return ok })
		exit := ev.(*ExitEvent)
		appErr := routeOf(t, exit.Err, "Session.finish.ProgramExited")
		if exit.Code != 7 || appErr.Metadata["exit"] != 7 || !strings.Contains(appErr.Metadata["stderr"].(string), "the model service is unreachable") {
			t.Errorf("exit = %+v, metadata %v", exit, appErr.Metadata)
		}
		select {
		case <-s.Done():
		case <-time.After(waitLimit):
			t.Fatal("Done was not closed")
		}
		// the exit is the last event
		evs := r.snapshot()
		if _, ok := evs[len(evs)-1].(*ExitEvent); !ok {
			t.Errorf("events = %s", describe(evs))
		}
		// nothing more can be sent
		wantRoute(t, s.Interrupt(context.Background()), "Session.control.SessionClosed")
		_, err := s.Send(context.Background(), "again")
		if err == nil {
			t.Fatal("Send to a dead program succeeded")
		}
		if tag := tagOf(err); tag != "SendFailed" {
			t.Errorf("Send failed with %s", tag)
		}
	})
	t.Run("a request that waits when the program dies", func(t *testing.T) {
		r := newRig(t, "crash_pending.jsonl")
		s := r.start(context.Background(), r.opts())
		err := s.Interrupt(context.Background())
		appErr := routeOf(t, err, "Session.finish.ProgramExited")
		if appErr.Metadata["exit"] != 9 || !strings.Contains(appErr.Metadata["stderr"].(string), "crashed while interrupting") {
			t.Errorf("metadata = %v", appErr.Metadata)
		}
	})
}

func TestMalformedLines(t *testing.T) {
	r := newRig(t, "malformed.jsonl")
	s := r.start(context.Background(), r.opts()) // MaxLineBytes 2048
	if _, err := s.Send(context.Background(), "go"); err != nil {
		t.Fatal(err)
	}
	r.waitResult()
	var tags []string
	var assistants int
	var unknown *UnknownMessage
	for _, ev := range r.snapshot() {
		switch m := ev.(type) {
		case *ProtocolErrorEvent:
			tags = append(tags, tagOf(m.Err))
			if len(m.Line) > 1024 {
				t.Errorf("the offending line was not cut: %d bytes", len(m.Line))
			}
		case *AssistantMessage:
			assistants++
		case *UnknownMessage:
			unknown = m
		}
	}
	if want := []string{"MalformedLine", "MessageInvalid", "LineTooLong"}; !reflect.DeepEqual(tags, want) {
		t.Errorf("protocol errors = %v, want %v", tags, want)
	}
	// the stream went on after the bad lines
	if assistants != 1 || unknown == nil || unknown.Type != "rate_limit_event" {
		t.Errorf("assistants %d, unknown %+v", assistants, unknown)
	}
	for _, ev := range r.snapshot() {
		if p, ok := ev.(*ProtocolErrorEvent); ok && tagOf(p.Err) == "LineTooLong" {
			wantRoute(t, p.Err, "Session.readLoop.LineTooLong")
		}
	}
	for _, ev := range r.snapshot() {
		if p, ok := ev.(*ProtocolErrorEvent); ok && tagOf(p.Err) == "MalformedLine" {
			wantRoute(t, p.Err, "Session.handleLine->parseEnvelope.MalformedLine")
		}
	}
}

func TestCancelKillsTheProcessGroup(t *testing.T) {
	for _, script := range []string{"hang.jsonl", "hang_ignore_term.jsonl"} {
		t.Run(script, func(t *testing.T) {
			r := newRig(t, script)
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			s := r.start(ctx, r.opts())
			child := r.readPID()
			leader := s.PID()
			if g := groupOf(child); g != leader {
				t.Fatalf("the child is in group %d, the program leads %d", g, leader)
			}
			if processGone(child) || processGone(leader) {
				t.Fatal("the processes are not running before the cancel")
			}
			cancel()
			select {
			case <-s.Done():
			case <-time.After(waitLimit):
				t.Fatal("the session did not end")
			}
			waitGone(t, "the program", leader)
			waitGone(t, "its child", child)
			exit, _ := r.snapshot()[len(r.snapshot())-1].(*ExitEvent)
			if exit == nil || exit.Err != nil {
				t.Errorf("last event = %+v, want an ExitEvent with no error (the end was asked for)", exit)
			}
		})
	}
}

func TestCloseKillsTheProcessGroup(t *testing.T) {
	for _, script := range []string{"hang.jsonl", "hang_ignore_term.jsonl"} {
		t.Run(script, func(t *testing.T) {
			r := newRig(t, script)
			s := r.start(context.Background(), r.opts())
			child := r.readPID()
			leader := s.PID()
			if err := s.Close(); err != nil {
				t.Fatal(err)
			}
			select {
			case <-s.Exited():
			default:
				t.Error("Close returned before the program ended")
			}
			waitGone(t, "the program", leader)
			waitGone(t, "its child", child)
			<-s.Done()
			if err := s.Close(); err != nil { // twice is fine
				t.Fatal(err)
			}
			_, err := s.Send(context.Background(), "late")
			wantRoute(t, err, "Session.Send->Session.write.SessionClosed")
			wantRoute(t, s.Interrupt(context.Background()), "Session.control.SessionClosed")
		})
	}
}

func TestStartFailures(t *testing.T) {
	t.Run("the program is missing", func(t *testing.T) {
		a := &Adapter{Path: "/nonexistent/claude"}
		_, err := a.Start(context.Background(), SessionOptions{})
		wantRoute(t, err, "Adapter.Start->Adapter.Find.ProgramNotFound")
	})
	t.Run("options that are not valid", func(t *testing.T) {
		a := &Adapter{Path: fakeProgram}
		_, err := a.Start(context.Background(), SessionOptions{PermissionMode: "nope"})
		wantRoute(t, err, "Adapter.Start->Args.InvalidOptions")
	})
	t.Run("the folder is missing", func(t *testing.T) {
		r := newRig(t, "idle.jsonl")
		r.adapter.Dir = r.dir + "/missing"
		_, err := r.adapter.Start(context.Background(), r.opts())
		wantRoute(t, err, "Adapter.Start->startProcess.ProcessStartFailed")
	})
	t.Run("a context that ended before the start", func(t *testing.T) {
		r := newRig(t, "idle.jsonl")
		ctx, cancel := context.WithCancel(context.Background())
		cancel()
		_, err := r.adapter.Start(ctx, r.opts())
		if err == nil {
			t.Fatal("Start with an ended context succeeded")
		}
		if recs := r.records(); len(recs) > 0 {
			waitGone(t, "the program", recs[0].PID)
		}
	})
}

func TestSendWithAnEndedContext(t *testing.T) {
	r := newRig(t, "idle.jsonl")
	s := r.start(context.Background(), r.opts())
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	_, err := s.Send(ctx, "x")
	wantRoute(t, err, "Session.Send.SendFailed")
}

func TestEventsAreDeliveredInOrderToASlowHandler(t *testing.T) {
	r := newRig(t, "turn.jsonl")
	var mu sync.Mutex
	var count int
	o := r.opts()
	o.OnEvent = func(ev Event) {
		time.Sleep(2 * time.Millisecond) // a UI that is slower than the program
		mu.Lock()
		count++
		mu.Unlock()
		r.collect(ev)
	}
	s := r.start(context.Background(), o)
	if _, err := s.Send(context.Background(), "go"); err != nil {
		t.Fatal(err)
	}
	r.waitResult()
	// the control channel keeps working while OnEvent is busy
	if err := s.Interrupt(context.Background()); err != nil {
		t.Fatal(err)
	}
	mu.Lock()
	defer mu.Unlock()
	if count == 0 {
		t.Error("OnEvent was never called")
	}
}
