package debug

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"github.com/rm4n0s/emetos/internal/core/dap"
	"github.com/rm4n0s/emetos/internal/core/dap/daptest"
	"github.com/rm4n0s/errors"
)

// bareSession is a session connected to a fake adapter that has done nothing, so its capabilities are empty.
func bareSession(t *testing.T, opts Options, cfg Config) (*Service, *Session) {
	t.Helper()
	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)
	svc := NewService(ctx, opts)
	h := daptest.Start(t, daptest.Options{Script: daptest.Script{}})
	s := newSession(svc, "s1", cfg, cfg.Clone(), "", Debugger{Type: cfg.Type()}, SessionOptions{})
	s.client = h.Client
	return svc, s
}

func routeOf(t *testing.T, err error, route string) {
	t.Helper()
	appErr, ok := errors.FromError(err)
	if !ok {
		t.Fatalf("expected *errors.Error, got %T: %v", err, err)
	}
	if !appErr.HasRoute(route) {
		t.Errorf("route = %s, want it to contain %s", appErr.Route(), route)
	}
}

func TestRequestsTheAdapterCannotAnswerFail(t *testing.T) {
	_, s := bareSession(t, Options{}, Config{"type": "fake", "request": "launch", "name": "n"})
	ctx := context.Background()
	tests := []struct {
		name  string
		call  func() error
		route string
	}{
		{"setVariable", func() error { _, err := s.setVariable(ctx, 1, "a", "1"); return err }, "Session.setVariable.DebugNotSupported"},
		{"setExpression", func() error { _, err := s.setExpression(ctx, 1, "a", "1"); return err }, "Session.setExpression.DebugNotSupported"},
		{"stepBack", func() error { return s.StepBack(ctx, 1, "") }, "Session.StepBack.DebugNotSupported"},
		{"reverseContinue", func() error { return s.ReverseContinue(ctx, 1) }, "Session.ReverseContinue.DebugNotSupported"},
		{"terminateThreads", func() error { return s.TerminateThreads(ctx, []int{1}) }, "Session.TerminateThreads.DebugNotSupported"},
		{"restartFrame", func() error { return s.RestartFrame(ctx, 1, 1) }, "Session.RestartFrame.DebugNotSupported"},
		{"gotoTargets", func() error { _, err := s.GotoTargets(ctx, "/p/a.go", 1, 1); return err }, "Session.GotoTargets.DebugNotSupported"},
		{"goto", func() error { return s.Goto(ctx, 1, 1) }, "Session.Goto.DebugNotSupported"},
		{"stepInTargets", func() error { _, err := s.StepInTargets(ctx, 1); return err }, "Session.StepInTargets.DebugNotSupported"},
		{"completions", func() error { _, err := s.Completions(ctx, 1, "a", 1); return err }, "Session.Completions.DebugNotSupported"},
		{"restart", func() error { return s.Restart(ctx) }, "Session.Restart.DebugNotSupported"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) { routeOf(t, tt.call(), tt.route) })
	}
}

func TestSessionWithoutAnAdapterFails(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	svc := NewService(ctx, Options{})
	s := newSession(svc, "s1", Config{"type": "fake"}, Config{}, "", Debugger{}, SessionOptions{})
	routeOf(t, s.Restart(ctx), "Session.Restart.DebugNoAdapter")
	routeOf(t, s.launchOrAttach(ctx, Config{"request": "launch"}), "Session.launchOrAttach.DebugNoAdapter")
	_, err := Debugger{Type: "fake"}.connect(ctx, Config{}, dap.ClientOptions{})
	routeOf(t, err, "Debugger.connect.DebugConnectMissing")
}

func TestLaunchOfAChildOfAnEndedSessionIsCancelled(t *testing.T) {
	svc, child := bareSession(t, Options{}, Config{"type": "fake", "request": "launch", "name": "child"})
	parent := newSession(svc, "p", Config{"type": "fake"}, Config{}, "", Debugger{}, SessionOptions{})
	parent.initialized = true // and no adapter any more: inactive
	child.parent = parent
	err := child.launchOrAttach(context.Background(), Config{"request": "launch"})
	routeOf(t, err, "Session.launchOrAttach.DebugParentInactive")
	if !errors.Is(err, context.Canceled) {
		t.Errorf("the failure does not wrap context.Canceled: %v", err)
	}
}

func TestWaitsThatAreCancelledFail(t *testing.T) {
	_, s := bareSession(t, Options{}, Config{"type": "fake", "request": "launch", "name": "n"})
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Millisecond)
	defer cancel()

	// A session whose work does not finish.
	release := make(chan struct{})
	s.enqueue(func() { <-release })
	routeOf(t, s.Idle(ctx), "Session.Idle.DebugIdleCancelled")
	close(release)

	// A session whose adapter does not go away.
	routeOf(t, waitEnded(ctx, s), "waitEnded.DebugRestartCancelled")
}

func TestReverseRequestsThatFail(t *testing.T) {
	failTask := func(context.Context, string, TaskRef) (TaskResult, error) { return TaskFailure, nil }
	_, s := bareSession(t, Options{
		Debugger: func(typ string) (Debugger, bool) { return Debugger{Type: typ, Label: typ}, true },
		RunTask:  failTask,
	}, Config{"type": "fake", "request": "launch", "name": "parent"})
	tests := []struct {
		name    string
		command string
		args    string
		route   string
	}{
		{"runInTerminal with arguments that are not", "runInTerminal", `{"args": "no"}`, "Session.handleReverse.DebugReverseRequestInvalid"},
		{"runInTerminal in an editor that has no terminal", "runInTerminal", `{"cwd": "/", "args": ["x"]}`, "Session.handleReverse.DebugRunInTerminalUnavailable"},
		{"startDebugging with arguments that are not", "startDebugging", `{"request": 5}`, "Session.handleReverse.DebugReverseRequestInvalid"},
		{"startDebugging that does not start", "startDebugging", `{"request": "launch", "configuration": {"preLaunchTask": "build"}}`, "Session.handleReverse.DebugStartDebuggingFailed"},
		{"a request nobody knows", "frobnicate", `{}`, "Session.handleReverse.DebugReverseRequestUnknown"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			raw := json.RawMessage(tt.args)
			_, err := s.handleReverse(context.Background(), tt.command, raw)
			routeOf(t, err, tt.route)
		})
	}
}
