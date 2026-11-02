package debug_test

import (
	"testing"

	"github.com/rm4n0s/emetos/internal/core/dap/daptest"
	"github.com/rm4n0s/emetos/internal/core/debug"
)

func bpsReply(args any, ids ...int) daptest.Step {
	var bps []map[string]any
	for _, id := range ids {
		bps = append(bps, map[string]any{"id": id, "verified": true})
	}
	if bps == nil {
		bps = []map[string]any{}
	}
	return daptest.ReplyArgs("setBreakpoints", args, map[string]any{"breakpoints": bps})
}

func srcA(bps ...map[string]any) map[string]any {
	if bps == nil {
		bps = []map[string]any{}
	}
	return map[string]any{"source": map[string]any{"path": "/p/a.go"}, "breakpoints": bps}
}

func TestBreakpointOperationsWhileASessionRuns(t *testing.T) {
	e := newEnv(t, nil)
	ctx := ctxT(t)
	e.script("ops", join(
		handshake(filterCaps, append(noBreakpoints(fullCaps)[:1:1], daptest.ReplyArgs("setExceptionBreakpoints", exOpts(map[string]any{"filterId": "all"}), map[string]any{}))),
		steps(
			// add
			bpsReply(srcA(map[string]any{"line": 3}), 21),
			// disable, enable
			bpsReply(srcA()),
			bpsReply(srcA(map[string]any{"line": 3}), 21),
			// remove the last one: an empty list goes out
			bpsReply(srcA()),
			// add two, update the condition of one at once
			bpsReply(srcA(map[string]any{"line": 3}, map[string]any{"line": 8}), 22, 23),
			bpsReply(srcA(map[string]any{"line": 3, "condition": "i > 2"}, map[string]any{"line": 8}), 22, 23),
			// deactivate: everything goes out empty; activate: everything is back
			bpsReply(srcA()),
			daptest.ReplyArgs("setFunctionBreakpoints", map[string]any{"breakpoints": []any{}}, map[string]any{"breakpoints": []any{}}),
			daptest.ReplyArgs("setExceptionBreakpoints", exOpts(map[string]any{"filterId": "all"}), map[string]any{}),
			bpsReply(srcA(map[string]any{"line": 3, "condition": "i > 2"}, map[string]any{"line": 8}), 22, 23),
			daptest.ReplyArgs("setFunctionBreakpoints", map[string]any{"breakpoints": []any{}}, map[string]any{"breakpoints": []any{}}),
			daptest.ReplyArgs("setExceptionBreakpoints", exOpts(map[string]any{"filterId": "all"}), map[string]any{}),
			// function breakpoints
			daptest.ReplyArgs("setFunctionBreakpoints", map[string]any{"breakpoints": []map[string]any{{"name": "main.run"}}}, map[string]any{"breakpoints": []map[string]any{{"id": 31, "verified": true}}}),
			daptest.ReplyArgs("setFunctionBreakpoints", map[string]any{"breakpoints": []map[string]any{{"name": "main.go", "condition": "x"}}}, map[string]any{"breakpoints": []map[string]any{{"id": 31, "verified": true}}}),
			daptest.ReplyArgs("setFunctionBreakpoints", map[string]any{"breakpoints": []any{}}, map[string]any{"breakpoints": []any{}}),
			// the exception filter: a condition is a filter option
			daptest.ReplyArgs("setExceptionBreakpoints", exOpts(map[string]any{"filterId": "all", "condition": "e.code == 7"}), map[string]any{}),
			// and disabled
			daptest.ReplyArgs("setExceptionBreakpoints", exOpts(), map[string]any{}),
			// a change on save, with sourceModified
			bpsReply(map[string]any{"source": map[string]any{"path": "/p/a.go"}, "sourceModified": true, "breakpoints": []map[string]any{{"line": 4, "condition": "i > 2"}, {"line": 8}}}, 22, 23),
		),
		ending(),
	))
	sess, ok, err := e.start(fakeConfig("ops"))
	if err != nil || !ok {
		t.Fatalf("start: %v %v", ok, err)
	}
	idle(t, sess)
	m := e.svc.Model()
	must := func(err error) {
		t.Helper()
		if err != nil {
			t.Fatal(err)
		}
	}

	added, err := e.svc.AddBreakpoints(ctx, "/p/a.go", []debug.BreakpointData{{Line: 3}})
	must(err)
	if b, _ := m.Breakpoint(added[0].ID); !b.Verified {
		t.Errorf("breakpoint = %+v", b)
	}
	must(e.svc.EnableOrDisableBreakpoints(ctx, false, added[0].ID))
	must(e.svc.EnableOrDisableBreakpoints(ctx, true, added[0].ID))
	must(e.svc.RemoveBreakpoints(ctx, added[0].ID))
	if len(m.Breakpoints(debug.BreakpointFilter{})) != 0 {
		t.Errorf("breakpoints left: %v", m.Breakpoints(debug.BreakpointFilter{}))
	}
	two, err := e.svc.AddBreakpoints(ctx, "/p/a.go", []debug.BreakpointData{{Line: 3}, {Line: 8}})
	must(err)
	cond := "i > 2"
	must(e.svc.UpdateBreakpoints(ctx, "/p/a.go", map[string]debug.BreakpointUpdate{two[0].ID: {Condition: &cond}}, false))

	must(e.svc.SetBreakpointsActivated(ctx, false))
	if m.BreakpointsActivated() {
		t.Error("still activated")
	}
	must(e.svc.SetBreakpointsActivated(ctx, true))

	fb, err := e.svc.AddFunctionBreakpoint(ctx, debug.FunctionBreakpointOptions{Name: "main.run"})
	must(err)
	name, fcond := "main.go", "x"
	must(e.svc.UpdateFunctionBreakpoint(ctx, fb.ID, &name, &fcond, nil))
	must(e.svc.RemoveFunctionBreakpoints(ctx, ""))
	if len(m.FunctionBreakpoints()) != 0 {
		t.Errorf("function breakpoints left: %v", m.FunctionBreakpoints())
	}

	ex := m.ExceptionBreakpoints(sess.ID())
	if len(ex) != 1 {
		t.Fatalf("exception breakpoints = %v", ex)
	}
	must(e.svc.SetExceptionBreakpointCondition(ctx, ex[0].ID, "e.code == 7"))
	must(e.svc.EnableOrDisableBreakpoints(ctx, false, ex[0].ID))

	// Moving with an edit: sent on save only.
	line := 4
	must(e.svc.UpdateBreakpoints(ctx, "/p/a.go", map[string]debug.BreakpointUpdate{two[0].ID: {Line: line}}, true))
	must(e.svc.DocumentSaved(ctx, "/p/a.go"))
	must(e.svc.DocumentSaved(ctx, "/p/a.go")) // nothing is pending any more
	// A file that is deleted loses its breakpoints.
	e.svc.DocumentDeleted("/p/a.go")
	if len(m.Breakpoints(debug.BreakpointFilter{})) != 0 {
		t.Errorf("breakpoints of a deleted file: %v", m.Breakpoints(debug.BreakpointFilter{}))
	}

	must(e.svc.Stop(ctx, sess, true, false))
	e.wait(debug.EventSessionEnded, nil)
	if got := len(e.requests(0, "setBreakpoints")); got != 9 {
		t.Errorf("setBreakpoints requests = %d", got)
	}
}

// filterCaps are the capabilities of an adapter that takes conditions on its exception filters.
var filterCaps = with(fullCaps, "supportsExceptionFilterOptions", true)

// exOpts are the arguments of setExceptionBreakpoints of an adapter with filter options: no plain filters.
func exOpts(options ...map[string]any) map[string]any {
	if len(options) == 0 {
		return map[string]any{"filters": []any{}}
	}
	return map[string]any{"filters": []any{}, "filterOptions": options}
}
