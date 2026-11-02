package debug_test

import (
	"context"
	"encoding/json"
	"strings"

	"github.com/rm4n0s/emetos/internal/core/dap/daptest"
	"github.com/rm4n0s/errors"
	"sync"
	"testing"
	"time"

	"github.com/rm4n0s/emetos/internal/core/dap"
	"github.com/rm4n0s/emetos/internal/core/debug"
)

const wait = 10 * time.Second

func ctxT(t *testing.T) context.Context {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	t.Cleanup(cancel)
	return ctx
}

// env is a debug service whose adapters are scripted fake adapters (daptest), one script for each configuration name.
type env struct {
	t   *testing.T
	svc *debug.Service

	mu        sync.Mutex
	cond      *sync.Cond
	scripts   map[string]daptest.Script
	harnesses []*daptest.Harness
	log       []debug.Event
	next      int
	connectsN map[string]int
	terminals []dap.RunInTerminalRequestArguments
	transport map[string]daptest.Transport
}

// newEnv makes the service; tune changes its options before it is made.
func newEnv(t *testing.T, tune func(*debug.Options)) *env {
	t.Helper()
	e := &env{t: t, scripts: map[string]daptest.Script{}, connectsN: map[string]int{}}
	e.cond = sync.NewCond(&e.mu)
	opts := debug.Options{
		FocusDelay: 20 * time.Millisecond, RunningDelay: 20 * time.Millisecond,
		Debugger: func(typ string) (debug.Debugger, bool) {
			if typ != "fake" {
				return debug.Debugger{}, false
			}
			return debug.Debugger{Type: "fake", Label: "Fake", Connect: e.connect, Variables: map[string]string{"pickProcess": "ext.pickProcess"}}, true
		},
		RunInTerminal: func(_ context.Context, args dap.RunInTerminalRequestArguments, _ string) (int, error) {
			e.mu.Lock()
			e.terminals = append(e.terminals, args)
			e.mu.Unlock()
			return 4242, nil
		},
	}
	if tune != nil {
		tune(&opts)
	}
	ctx, cancel := context.WithCancel(context.Background())
	e.svc = debug.NewService(ctx, opts)
	e.svc.Subscribe(func(ev debug.Event) {
		e.mu.Lock()
		e.log = append(e.log, ev)
		e.cond.Broadcast()
		e.mu.Unlock()
	})
	// Registered first, so that it runs last: the fake adapters have seen nothing wrong.
	t.Cleanup(func() {
		e.mu.Lock()
		hs := append([]*daptest.Harness(nil), e.harnesses...)
		e.mu.Unlock()
		for _, h := range hs {
			if h.Server != nil {
				if v := h.Server.Violations(); len(v) > 0 {
					t.Errorf("the fake adapter noticed: %v", v)
				}
			}
		}
	})
	t.Cleanup(cancel)
	return e
}

// script sets the script of the adapter that the configuration with the name gets.
func (e *env) script(name string, s daptest.Script) {
	e.mu.Lock()
	e.scripts[name] = s
	e.mu.Unlock()
}

func (e *env) connect(_ context.Context, cfg debug.Config, opts dap.ClientOptions) (*dap.Client, error) {
	e.mu.Lock()
	s, ok := e.scripts[cfg.Name()]
	e.connectsN[cfg.Name()]++
	e.mu.Unlock()
	if !ok {
		return nil, errors.New("TestNoScript", "no script for the configuration "+cfg.Name())
	}
	e.mu.Lock()
	tr := e.transport[cfg.Name()]
	e.mu.Unlock()
	h := daptest.Start(e.t, daptest.Options{Script: s, Client: opts, Transport: tr})
	e.mu.Lock()
	e.harnesses = append(e.harnesses, h)
	e.mu.Unlock()
	return h.Client, nil
}

// harness returns the n-th adapter that was started.
func (e *env) harness(n int) *daptest.Harness {
	e.mu.Lock()
	defer e.mu.Unlock()
	if n >= len(e.harnesses) {
		e.t.Fatalf("only %d adapters were started", len(e.harnesses))
	}
	return e.harnesses[n]
}

// wait waits for the first event after the last one that was waited for that is of the kind and satisfies match (nil: any).
func (e *env) wait(kind debug.EventKind, match func(debug.Event) bool) debug.Event {
	e.t.Helper()
	deadline := time.Now().Add(wait)
	timer := time.AfterFunc(wait, func() {
		e.mu.Lock()
		e.cond.Broadcast()
		e.mu.Unlock()
	})
	defer timer.Stop()
	e.mu.Lock()
	defer e.mu.Unlock()
	for {
		for i := e.next; i < len(e.log); i++ {
			if ev := e.log[i]; ev.Kind == kind && (match == nil || match(ev)) {
				e.next = i + 1
				return ev
			}
		}
		if time.Now().After(deadline) {
			e.t.Fatalf("no event of kind %d in %s; the events were %d", kind, wait, len(e.log))
		}
		e.cond.Wait()
	}
}

// waitUntil polls a condition for up to wait.
func waitUntil(t *testing.T, what string, cond func() bool) {
	t.Helper()
	deadline := time.Now().Add(wait)
	for !cond() {
		if time.Now().After(deadline) {
			t.Fatalf("timed out waiting for %s", what)
		}
		time.Sleep(2 * time.Millisecond)
	}
}

// count counts the events of a kind in the log so far.
func (e *env) count(kind debug.EventKind) int {
	e.mu.Lock()
	defer e.mu.Unlock()
	n := 0
	for _, ev := range e.log {
		if ev.Kind == kind {
			n++
		}
	}
	return n
}

// start starts the configuration with a name, as a user would from a launch.json that has it.
func (e *env) start(cfg debug.Config) (*debug.Session, bool, error) {
	e.t.Helper()
	ok, err := e.svc.StartDebugging(ctxT(e.t), nil, debug.Target{Config: cfg}, debug.SessionOptions{})
	if err != nil || !ok {
		return nil, ok, err
	}
	sessions := e.svc.Model().Sessions(false)
	if len(sessions) == 0 {
		return nil, ok, nil
	}
	return sessions[len(sessions)-1], ok, nil
}

func fakeConfig(name string) debug.Config {
	return debug.Config{"name": name, "type": "fake", "request": "launch"}
}

// idle waits for the work queued by the events of a session.
func idle(t *testing.T, s *debug.Session) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), wait)
	defer cancel()
	if err := s.Idle(ctx); err != nil {
		t.Fatal(err)
	}
}

// ---- scripts

// fullCaps are the capabilities of the fake adapter of most tests.
var fullCaps = map[string]any{
	"supportsConfigurationDoneRequest": true,
	"supportsFunctionBreakpoints":      true,
	"supportsConditionalBreakpoints":   true,
	"supportsLogPoints":                true,
	"supportsSetVariable":              true,
	"supportsRestartFrame":             true,
	"supportsCompletionsRequest":       true,
	"supportsExceptionInfoRequest":     true,
	"exceptionBreakpointFilters":       []map[string]any{{"filter": "all", "label": "All Exceptions", "default": true}},
}

func with(caps map[string]any, kv ...any) map[string]any {
	out := map[string]any{}
	for k, v := range caps {
		out[k] = v
	}
	for i := 0; i+1 < len(kv); i += 2 {
		out[kv[i].(string)] = kv[i+1]
	}
	return out
}

// handshake is the start of a session of the fake adapter: initialize, launch, the initialized event, whatever the adapter expects of
// the breakpoints (between), configurationDone, the answer to launch and the threads.
func handshake(caps map[string]any, between []daptest.Step, threads ...map[string]any) daptest.Script {
	s := daptest.Script{
		daptest.Reply("initialize", caps),
		daptest.Expect("launch", map[string]any{"type": "fake", "request": "launch"}),
		daptest.Event("initialized", nil),
	}
	s = append(s, between...)
	if caps["supportsConfigurationDoneRequest"] == true {
		s = append(s, daptest.Reply("configurationDone", nil))
	}
	s = append(s, daptest.Respond("launch", nil))
	if len(threads) == 0 {
		threads = []map[string]any{{"id": 1, "name": "main"}}
	}
	s = append(s, daptest.Reply("threads", map[string]any{"threads": threads}))
	return s
}

// ending is the end of a script for a session that is stopped with Disconnect or Terminate without support for terminate.
func ending() daptest.Script {
	return daptest.Script{daptest.Reply("disconnect", nil), daptest.WaitClose()}
}

func join(parts ...daptest.Script) daptest.Script {
	var out daptest.Script
	for _, p := range parts {
		out = append(out, p...)
	}
	return out
}

func steps(s ...daptest.Step) daptest.Script { return s }

// requests returns the arguments of the requests the n-th adapter got for a command, also after its session ended.
func (e *env) requests(n int, command string) []json.RawMessage {
	e.t.Helper()
	h := e.harness(n)
	if h.Server == nil {
		e.t.Fatal("the adapter is not in process")
	}
	return h.Server.Requests(command)
}

// noBreakpoints are the steps of the adapter for a session that has no breakpoints: what the client sends after initialized.
func noBreakpoints(caps map[string]any) []daptest.Step {
	var s []daptest.Step
	if caps["supportsFunctionBreakpoints"] == true {
		s = append(s, daptest.ReplyArgs("setFunctionBreakpoints", map[string]any{"breakpoints": []any{}}, map[string]any{"breakpoints": []any{}}))
	}
	filters, has := caps["exceptionBreakpointFilters"].([]map[string]any)
	if has || caps["supportsConfigurationDoneRequest"] != true {
		on := []string{}
		for _, f := range filters {
			if f["default"] == true {
				on = append(on, f["filter"].(string))
			}
		}
		s = append(s, daptest.ReplyArgs("setExceptionBreakpoints", map[string]any{"filters": on}, map[string]any{}))
	}
	return s
}

// frames is a stack trace body of n frames named f0, f1, ... in /p/a.go at lines 10, 20, ...
func frames(n, total int) map[string]any {
	var fs []map[string]any
	for i := 0; i < n; i++ {
		fs = append(fs, map[string]any{"id": 100 + i, "name": "f" + string(rune('0'+i)), "line": 10 * (i + 1), "column": 1, "source": map[string]any{"path": "/p/a.go", "name": "a.go"}})
	}
	return map[string]any{"stackFrames": fs, "totalFrames": total}
}

// stoppedAt is the events and requests of a stop of thread 1 with a call stack of n frames (no delayed stack loading).
func stoppedAt(reason string, n int) []daptest.Step {
	return []daptest.Step{
		daptest.Event("stopped", map[string]any{"reason": reason, "threadId": 1, "allThreadsStopped": true}),
		daptest.Reply("threads", map[string]any{"threads": []map[string]any{{"id": 1, "name": "main"}, {"id": 2, "name": "worker"}}}),
		daptest.ReplyArgs("stackTrace", map[string]any{"threadId": 1, "levels": 20}, frames(n, n)),
	}
}

// connects returns how many times each configuration's adapter was reached.
func (e *env) connects() map[string]int {
	e.mu.Lock()
	defer e.mu.Unlock()
	out := map[string]int{}
	for k, v := range e.connectsN {
		out[k] = v
	}
	return out
}

func containsJSON(raw []byte, part string) bool {
	return strings.Contains(string(raw), part)
}

func shortCtx(d time.Duration) (context.Context, context.CancelFunc) {
	return context.WithTimeout(context.Background(), d)
}
