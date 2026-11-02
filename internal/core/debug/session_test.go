package debug_test

import (
	"encoding/json"
	"reflect"
	"testing"

	"github.com/rm4n0s/emetos/internal/core/dap/daptest"
	"github.com/rm4n0s/emetos/internal/core/debug"
)

func TestSessionSequenceSendsBreakpointsBeforeConfigurationDone(t *testing.T) {
	e := newEnv(t, nil)
	ctx := ctxT(t)
	// Breakpoints exist before the session: two files, a function breakpoint and the exception filter the adapter offers (enabled by
	// default).
	if _, err := e.svc.AddBreakpoints(ctx, "/p/b.go", []debug.BreakpointData{{Line: 5}}); err != nil {
		t.Fatal(err)
	}
	a, err := e.svc.AddBreakpoints(ctx, "/p/a.go", []debug.BreakpointData{{Line: 9}, {Line: 3, Condition: "x > 1"}, {Line: 7, Disabled: true}})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := e.svc.AddFunctionBreakpoint(ctx, debug.FunctionBreakpointOptions{Name: "main.main"}); err != nil {
		t.Fatal(err)
	}
	e.script("seq", join(
		handshake(fullCaps, steps(
			// The files in the order of the model; only the enabled breakpoints; the condition travels.
			daptest.ReplyArgs("setBreakpoints",
				map[string]any{"source": map[string]any{"path": "/p/a.go", "name": "a.go"}, "breakpoints": []map[string]any{{"line": 3, "condition": "x > 1"}, {"line": 9}}},
				map[string]any{"breakpoints": []map[string]any{{"id": 11, "verified": true, "line": 4}, {"id": 12, "verified": false, "message": "no code at line 9"}}}),
			daptest.ReplyArgs("setBreakpoints",
				map[string]any{"source": map[string]any{"path": "/p/b.go"}, "breakpoints": []map[string]any{{"line": 5}}},
				map[string]any{"breakpoints": []map[string]any{{"id": 13, "verified": true, "line": 5}}}),
			daptest.ReplyArgs("setFunctionBreakpoints",
				map[string]any{"breakpoints": []map[string]any{{"name": "main.main"}}},
				map[string]any{"breakpoints": []map[string]any{{"id": 14, "verified": true}}}),
			daptest.ReplyArgs("setExceptionBreakpoints", map[string]any{"filters": []string{"all"}}, map[string]any{}),
		)),
		ending(),
	))
	sess, ok, err := e.start(fakeConfig("seq"))
	if err != nil || !ok {
		t.Fatalf("start: %v %v", ok, err)
	}
	idle(t, sess)

	// The first answers: the breakpoint at line 3 moved to line 4 and is verified, the one at 9 is not.
	bps := e.svc.Model().Breakpoints(debug.BreakpointFilter{OriginalPath: "/p/a.go"})
	byReq := map[int]debug.Breakpoint{}
	for _, b := range bps {
		byReq[b.RequestedLine] = b
	}
	if b := byReq[3]; b.Line != 4 || !b.Verified || b.RequestedLine != 3 || b.Condition != "x > 1" {
		t.Errorf("breakpoint 3 = %+v", b)
	}
	if b := byReq[9]; b.Line != 9 || b.Verified || b.Message != "no code at line 9" {
		t.Errorf("breakpoint 9 = %+v", b)
	}
	if b := byReq[7]; b.Enabled || !b.Verified {
		t.Errorf("breakpoint 7 (disabled, never sent) = %+v", b)
	}
	if fb := e.svc.Model().FunctionBreakpoints()[0]; !fb.Verified {
		t.Errorf("function breakpoint = %+v", fb)
	}
	if ex := e.svc.Model().ExceptionBreakpoints(sess.ID()); len(ex) != 1 || !ex[0].Enabled || ex[0].Filter != "all" {
		t.Errorf("exception breakpoints = %+v", ex)
	}
	if st := sess.State(); st != debug.Running {
		t.Errorf("state = %v, want running", st)
	}
	_ = a
	if err := e.svc.Stop(ctx, sess, true, false); err != nil {
		t.Fatal(err)
	}
	e.wait(debug.EventSessionEnded, nil)
	if sess.State() != debug.Inactive {
		t.Errorf("state after the end = %v", sess.State())
	}
	// The data the adapter gave goes with the session.
	if b, _ := e.svc.Model().Breakpoint(a[0].ID); b.Line != b.RequestedLine {
		t.Errorf("after the end: %+v", b)
	}
	if got := e.requests(0, "initialize"); len(got) != 1 {
		t.Fatalf("initialize requests = %d", len(got))
	}
}

func TestSessionSendsWhatTheClientSupportsAndTheConfiguration(t *testing.T) {
	e := newEnv(t, nil)
	e.script("caps", join(handshake(map[string]any{}, steps(daptest.ReplyArgs("setExceptionBreakpoints", map[string]any{"filters": []string{}}, map[string]any{}))), ending()))
	cfg := fakeConfig("caps")
	cfg["program"] = "/p/main"
	cfg["args"] = []any{"-x"}
	sess, _, err := e.start(cfg)
	if err != nil {
		t.Fatal(err)
	}
	idle(t, sess)
	var init map[string]any
	if err := json.Unmarshal(e.requests(0, "initialize")[0], &init); err != nil {
		t.Fatal(err)
	}
	want := map[string]any{
		"clientID": "emetos", "clientName": "Emetos", "adapterID": "fake", "pathFormat": "path", "locale": "en",
		"linesStartAt1": true, "columnsStartAt1": true, "supportsVariableType": true, "supportsVariablePaging": true,
		"supportsRunInTerminalRequest": true, "supportsStartDebuggingRequest": true, "supportsProgressReporting": true,
		"supportsInvalidatedEvent": true,
	}
	if !reflect.DeepEqual(init, want) {
		t.Errorf("initialize arguments = %v\nwant %v", init, want)
	}
	var launch map[string]any
	if err := json.Unmarshal(e.requests(0, "launch")[0], &launch); err != nil {
		t.Fatal(err)
	}
	if launch["program"] != "/p/main" || launch["__sessionId"] != sess.ID() || launch["type"] != "fake" || launch["name"] != "caps" {
		t.Errorf("launch arguments = %v", launch)
	}
	if err := e.svc.Stop(ctxT(t), sess, true, false); err != nil {
		t.Fatal(err)
	}
	e.wait(debug.EventSessionEnded, nil)
}

func TestSessionStopInspectAndContinue(t *testing.T) {
	e := newEnv(t, nil)
	ctx := ctxT(t)
	e.script("run", join(
		handshake(fullCaps, noBreakpoints(fullCaps)),
		// First stop: the scopes, the variables (with a paged list), an evaluation, a change of a value.
		stoppedAt("breakpoint", 3),
		steps(
			daptest.ReplyArgs("scopes", map[string]any{"frameId": 100}, map[string]any{"scopes": []map[string]any{
				{"name": "Locals", "variablesReference": 1000, "line": 5, "column": 1, "endLine": 30, "endColumn": 1},
				{"name": "Globals", "variablesReference": 1001, "expensive": true},
			}}),
			daptest.ReplyArgs("variables", map[string]any{"variablesReference": 1000}, map[string]any{"variables": []map[string]any{
				{"name": "a", "value": "1", "type": "int", "evaluateName": "a"},
				{"name": "list", "value": "[250]", "variablesReference": 1002, "indexedVariables": 250},
				{"name": "dup", "value": "x"}, {"name": "dup", "value": "y"},
			}}),
			daptest.ReplyArgs("variables", map[string]any{"variablesReference": 1002, "filter": "indexed", "start": 100, "count": 100}, map[string]any{"variables": []map[string]any{
				{"name": "[100]", "value": "100"}, {"name": "[101]", "value": "101"},
			}}),
			daptest.ReplyArgs("evaluate", map[string]any{"expression": "a+1", "frameId": 100, "context": "watch"}, map[string]any{"result": "2", "type": "int"}),
			daptest.ReplyArgs("setVariable", map[string]any{"variablesReference": 1000, "name": "a", "value": "5"}, map[string]any{"value": "5", "type": "int"}),
			daptest.ReplyArgs("continue", map[string]any{"threadId": 1}, map[string]any{"allThreadsContinued": true}),
		),
		// Second stop: the variable has another value.
		steps(daptest.Expect("kick", nil), daptest.Event("stopped", map[string]any{"reason": "step", "threadId": 1, "allThreadsStopped": true}), daptest.Respond("kick", nil),
			daptest.Reply("threads", map[string]any{"threads": []map[string]any{{"id": 1, "name": "main"}, {"id": 2, "name": "worker"}}}),
			daptest.ReplyArgs("stackTrace", map[string]any{"threadId": 1, "levels": 20}, frames(3, 3)),
			daptest.ReplyArgs("scopes", map[string]any{"frameId": 100}, map[string]any{"scopes": []map[string]any{{"name": "Locals", "variablesReference": 1010, "line": 5, "column": 1, "endLine": 30, "endColumn": 1}}}),
			daptest.ReplyArgs("variables", map[string]any{"variablesReference": 1010}, map[string]any{"variables": []map[string]any{{"name": "a", "value": "6", "type": "int"}}}),
		),
		ending(),
	))
	sess, ok, err := e.start(fakeConfig("run"))
	if err != nil || !ok {
		t.Fatalf("start: %v %v", ok, err)
	}
	ev := e.wait(debug.EventStopped, nil)
	th := ev.Thread
	if th == nil || th.ID != 1 || !th.Stopped() || th.Name() != "main" {
		t.Fatalf("stopped thread = %+v", th)
	}
	if d := th.StoppedDetails(); d == nil || d.Reason != "breakpoint" || d.TotalFrames != 3 {
		t.Errorf("stopped details = %+v", d)
	}
	if got := th.StateLabel(); got != "Paused on breakpoint" {
		t.Errorf("state label = %q", got)
	}
	stack := th.CallStack()
	if len(stack) != 3 || stack[0].Name != "f0" || stack[2].Range.Line != 30 || !th.ReachedEndOfCallStack() {
		t.Fatalf("call stack = %v reached end %v", stack, th.ReachedEndOfCallStack())
	}
	if top := th.TopStackFrame(); top != stack[0] {
		t.Errorf("top frame = %v", top)
	}
	// The other thread is stopped too (allThreadsStopped) but has no frames yet.
	other := sess.Thread(2)
	if other == nil || !other.Stopped() || len(other.CallStack()) != 0 {
		t.Errorf("thread 2 = %+v", other)
	}
	if sess.State() != debug.Stopped {
		t.Errorf("state = %v", sess.State())
	}
	// The stopped session and its top frame are focused.
	if f := e.svc.Model().Focus(); f.Session != sess || f.Thread != th || f.Frame != stack[0] {
		t.Errorf("focus = %+v", f)
	}
	if e.svc.State() != debug.Stopped {
		t.Errorf("service state = %v", e.svc.State())
	}
	if got := stack[0].String(); got != "f0 (/p/a.go:10)" {
		t.Errorf("frame text = %q", got)
	}

	// Scopes, once; variables, once; the paged list.
	scopes, err := stack[0].Scopes(ctx)
	if err != nil || len(scopes) != 2 || scopes[0].Name != "Locals" || scopes[0].Range == nil || !scopes[1].Expensive {
		t.Fatalf("scopes = %+v %v", scopes, err)
	}
	if again, _ := stack[0].Scopes(ctx); again[0] != scopes[0] {
		t.Error("the scopes were loaded twice")
	}
	if got, err := stack[0].MostSpecificScopes(ctx, debug.Range{Line: 10, Column: 1, EndLine: 10, EndColumn: 2}); err != nil || len(got) != 1 || got[0] != scopes[0] {
		t.Errorf("most specific scopes = %v %v", got, err)
	}
	if got, _ := stack[0].MostSpecificScopes(ctx, debug.Range{Line: 99, Column: 1, EndLine: 99, EndColumn: 2}); len(got) != 1 {
		t.Errorf("most specific scopes outside every range = %v", got)
	}
	vars, err := scopes[0].Children(ctx)
	if err != nil || len(vars) != 4 {
		t.Fatalf("variables = %v %v", vars, err)
	}
	if again, _ := scopes[0].Children(ctx); len(again) != 4 || again[0] != vars[0] {
		t.Error("the variables were loaded twice")
	}
	if vars[0].Name != "a" || vars[0].Value() != "1" || vars[0].Type() != "int" || vars[0].HasChildren() || vars[0].String() != "a: 1" {
		t.Errorf("variable a = %+v", vars[0])
	}
	if vars[2].ID() == vars[3].ID() {
		t.Errorf("two variables with the same name have the id %q", vars[2].ID())
	}
	chunks, err := vars[1].Children(ctx)
	if err != nil || len(chunks) != 3 || chunks[0].Name != "[0..99]" || chunks[1].Name != "[100..199]" || chunks[2].Name != "[200..249]" || !chunks[1].HasChildren() {
		t.Fatalf("chunks = %v %v", chunks, err)
	}
	if h := chunks[1].PresentationHint(); h == nil || h.Kind != "virtual" || chunks[2].IndexedVariables() != 50 {
		t.Errorf("chunk = %+v", chunks[1])
	}
	items, err := chunks[1].Children(ctx)
	if err != nil || len(items) != 2 || items[0].Name != "[100]" {
		t.Fatalf("items = %v %v", items, err)
	}

	// A watch expression is evaluated in the focused frame.
	w := e.svc.Model().AddWatchExpression("a+1")
	e.svc.EvaluateWatchExpressions(ctx)
	if w.Value() != "2" || w.Type() != "int" || !w.Available() {
		t.Errorf("watch = %q %q %v", w.Value(), w.Type(), w.Available())
	}
	// Setting a value.
	if err := vars[0].SetValue(ctx, stack[0], "5"); err != nil || vars[0].Value() != "5" {
		t.Errorf("SetValue: %q %v", vars[0].Value(), err)
	}

	// Continue: no stopped event follows, so the threads are running.
	if err := th.Continue(ctx); err != nil {
		t.Fatal(err)
	}
	if th.Stopped() || len(th.CallStack()) != 0 || len(th.StaleCallStack()) != 3 || sess.Thread(2).Stopped() {
		t.Errorf("after continue: stopped %v, stack %d, stale %d, thread 2 stopped %v", th.Stopped(), len(th.CallStack()), len(th.StaleCallStack()), sess.Thread(2).Stopped())
	}
	if sess.State() != debug.Running {
		t.Errorf("state after continue = %v", sess.State())
	}
	// The frame is defocused soon after.
	waitUntil(t, "the frame to be defocused", func() bool { return e.svc.Model().Focus().Frame == nil })
	if f := e.svc.Model().Focus(); f.Session != sess || f.Thread != th {
		t.Errorf("focus after continue = %+v", f)
	}

	// The second stop: the value of a changed, and the node keeps its id.
	if _, err := sess.CustomRequest(ctx, "kick", nil); err != nil {
		t.Fatal(err)
	}
	ev = e.wait(debug.EventStopped, nil)
	scopes2, err := ev.Thread.TopStackFrame().Scopes(ctx)
	if err != nil {
		t.Fatal(err)
	}
	vars2, err := scopes2[0].Children(ctx)
	if err != nil || len(vars2) != 1 {
		t.Fatalf("variables = %v %v", vars2, err)
	}
	if vars2[0].ID() != vars[0].ID() || vars2[0].Value() != "6" || !vars2[0].ValueChanged() {
		t.Errorf("a after the second stop: id %q (was %q), value %q, changed %v", vars2[0].ID(), vars[0].ID(), vars2[0].Value(), vars2[0].ValueChanged())
	}
	if err := e.svc.Stop(ctx, sess, true, false); err != nil {
		t.Fatal(err)
	}
	e.wait(debug.EventSessionEnded, nil)
}
