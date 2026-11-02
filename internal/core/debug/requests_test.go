package debug_test

import (
	"sync"
	"testing"
	"time"

	"github.com/rm4n0s/emetos/internal/core/dap"
	"github.com/rm4n0s/emetos/internal/core/dap/daptest"
	"github.com/rm4n0s/emetos/internal/core/debug"
)

func TestAdapterRequestsOfTheViews(t *testing.T) {
	e := newEnv(t, nil)
	ctx := ctxT(t)
	caps := with(fullCaps,
		"supportsGotoTargetsRequest", true, "supportsStepInTargetsRequest", true, "supportsTerminateThreadsRequest", true,
		"supportsLoadedSourcesRequest", true, "supportsBreakpointLocationsRequest", true, "supportsStepBack", true)
	e.script("views", join(
		handshake(caps, noBreakpoints(caps)),
		stoppedAt("exception", 2),
		steps(
			daptest.ReplyArgs("exceptionInfo", map[string]any{"threadId": 1}, map[string]any{"exceptionId": "E1", "description": "boom", "breakMode": "always"}),
			daptest.ReplyArgs("completions", map[string]any{"frameId": 100, "text": "fo", "column": 3}, map[string]any{"targets": []map[string]any{{"label": "foo"}, {"label": "for"}}}),
			daptest.ReplyArgs("gotoTargets", map[string]any{"source": map[string]any{"path": "/p/a.go"}, "line": 30}, map[string]any{"targets": []map[string]any{{"id": 5, "label": "here", "line": 30}}}),
			daptest.ReplyArgs("stepInTargets", map[string]any{"frameId": 100}, map[string]any{"targets": []map[string]any{{"id": 6, "label": "g()"}}}),
			daptest.ReplyArgs("source", map[string]any{"sourceReference": 77}, map[string]any{"content": "package x\n"}),
			daptest.Reply("loadedSources", map[string]any{"sources": []map[string]any{{"path": "/p/a.go"}, {"name": "gen", "sourceReference": 9}}}),
			daptest.ReplyArgs("breakpointLocations", map[string]any{"line": 10}, map[string]any{"breakpoints": []map[string]any{{"line": 10, "column": 3}, {"line": 10, "column": 9}}}),
			daptest.ReplyArgs("cancel", map[string]any{"progressId": "p1"}, nil),
			daptest.ReplyArgs("terminateThreads", map[string]any{"threadIds": []int{2}}, nil),
			// A goto, a step back and a frame restart are steps: the thread runs, and the adapter says so at once.
			daptest.ReplyArgs("evaluate", map[string]any{"expression": "1+1", "context": "repl", "frameId": 100}, map[string]any{"result": "2", "type": "int", "variablesReference": 0}),
			daptest.ReplyArgs("goto", map[string]any{"threadId": 1, "targetId": 5}, nil),
			daptest.Expect("custom", map[string]any{"x": 1}),
			daptest.Respond("custom", map[string]any{"y": 2}),
		),
		ending(),
	))
	sess, ok, err := e.start(fakeConfig("views"))
	if err != nil || !ok {
		t.Fatalf("start: %v %v", ok, err)
	}
	th := e.wait(debug.EventStopped, nil).Thread
	info, err := th.ExceptionInfo(ctx)
	if err != nil || info == nil || info.ID != "E1" || info.Description != "boom" || info.BreakMode != "always" {
		t.Fatalf("exception info = %+v %v", info, err)
	}
	items, err := sess.Completions(ctx, 100, "fo", 3)
	if err != nil || len(items) != 2 || items[1].Label != "for" {
		t.Errorf("completions = %v %v", items, err)
	}
	targets, err := sess.GotoTargets(ctx, "/p/a.go", 30, 0)
	if err != nil || len(targets) != 1 || targets[0].ID != 5 {
		t.Errorf("goto targets = %v %v", targets, err)
	}
	in, err := sess.StepInTargets(ctx, 100)
	if err != nil || len(in) != 1 || in[0].Label != "g()" {
		t.Errorf("step in targets = %v %v", in, err)
	}
	src := debug.Source{Raw: dap.Source{SourceReference: 77, Name: "mem"}}
	if text, err := sess.LoadSource(ctx, src); err != nil || text != "package x\n" {
		t.Errorf("source = %q %v", text, err)
	}
	loaded, err := sess.LoadedSources(ctx)
	if err != nil || len(loaded) != 2 || loaded[0].Path != "/p/a.go" || !loaded[1].InMemory() {
		t.Errorf("loaded sources = %+v %v", loaded, err)
	}
	locs, err := sess.BreakpointLocations(ctx, "/p/a.go", 10)
	if err != nil || len(locs) != 2 || locs[1].Column != 9 {
		t.Errorf("breakpoint locations = %v %v", locs, err)
	}
	if err := sess.CancelProgress(ctx, "p1"); err != nil {
		t.Error(err)
	}
	if err := sess.TerminateThreads(ctx, []int{2}); err != nil {
		t.Error(err)
	}
	// The REPL evaluates in the focused frame; its result is an Expression.
	expr, err := sess.Evaluate(ctx, "1+1", th.TopStackFrame(), dap.ContextRepl)
	if err != nil || expr.Value() != "2" || expr.Type() != "int" {
		t.Errorf("evaluate = %+v %v", expr, err)
	}
	if err := sess.Goto(ctx, 1, 5); err != nil {
		t.Error(err)
	}
	body, err := sess.CustomRequest(ctx, "custom", map[string]any{"x": 1})
	if err != nil || !containsJSON(body, `"y":2`) {
		t.Errorf("custom = %s %v", body, err)
	}
	if err := e.svc.Stop(ctx, sess, true, false); err != nil {
		t.Fatal(err)
	}
	e.wait(debug.EventSessionEnded, nil)
}

func TestRequestsWithoutSupportAreNotSentOrAreEmpty(t *testing.T) {
	e := newEnv(t, nil)
	ctx := ctxT(t)
	e.script("bare", join(handshake(map[string]any{}, steps(daptest.ReplyArgs("setExceptionBreakpoints", map[string]any{"filters": []string{}}, map[string]any{}))), ending()))
	sess, _, err := e.start(fakeConfig("bare"))
	if err != nil {
		t.Fatal(err)
	}
	idle(t, sess)
	if src, err := sess.LoadedSources(ctx); err != nil || src != nil {
		t.Errorf("loaded sources = %v %v", src, err)
	}
	if locs, err := sess.BreakpointLocations(ctx, "/p/a.go", 1); err != nil || locs != nil {
		t.Errorf("breakpoint locations = %v %v", locs, err)
	}
	if err := e.svc.Stop(ctx, sess, true, false); err != nil {
		t.Fatal(err)
	}
	e.wait(debug.EventSessionEnded, nil)
}

// A second reader of a list that is loading waits for the first one and gives up with its own context.
func TestSecondLoadOfScopesAndVariablesCanBeCancelled(t *testing.T) {
	e := newEnv(t, nil)
	ctx := ctxT(t)
	e.script("slow", join(
		handshake(fullCaps, noBreakpoints(fullCaps)),
		stoppedAt("breakpoint", 1),
		steps(
			daptest.Delay(400),
			daptest.ReplyArgs("scopes", map[string]any{"frameId": 100}, map[string]any{"scopes": []map[string]any{{"name": "Locals", "variablesReference": 1000}}}),
			daptest.Delay(400),
			daptest.ReplyArgs("variables", map[string]any{"variablesReference": 1000}, map[string]any{"variables": []map[string]any{{"name": "a", "value": "1"}}}),
		),
		ending(),
	))
	sess, _, err := e.start(fakeConfig("slow"))
	if err != nil {
		t.Fatal(err)
	}
	frame := e.wait(debug.EventStopped, nil).Thread.TopStackFrame()

	var wg sync.WaitGroup
	var scopes []*debug.Scope
	wg.Add(1)
	go func() {
		defer wg.Done()
		var err error
		scopes, err = frame.Scopes(ctx)
		if err != nil {
			t.Error(err)
		}
	}()
	time.Sleep(100 * time.Millisecond)
	short, cancel := shortCtx(50 * time.Millisecond)
	_, err = frame.Scopes(short)
	cancel()
	_ = wantRoute(t, err, "StackFrame.Scopes.DebugScopesCancelled")
	wg.Wait()
	if len(scopes) != 1 {
		t.Fatalf("scopes = %v", scopes)
	}

	var vars []*debug.Variable
	wg.Add(1)
	go func() {
		defer wg.Done()
		var err error
		vars, err = scopes[0].Children(ctx)
		if err != nil {
			t.Error(err)
		}
	}()
	time.Sleep(100 * time.Millisecond)
	short, cancel = shortCtx(50 * time.Millisecond)
	_, err = scopes[0].Children(short)
	cancel()
	_ = wantRoute(t, err, "container.Children.DebugChildrenCancelled")
	wg.Wait()
	if len(vars) != 1 {
		t.Fatalf("variables = %v", vars)
	}
	if err := e.svc.Stop(ctx, sess, true, false); err != nil {
		t.Fatal(err)
	}
	e.wait(debug.EventSessionEnded, nil)
}
