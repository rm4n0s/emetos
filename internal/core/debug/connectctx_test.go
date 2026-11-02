package debug_test

import (
	"context"
	"sync"
	"testing"

	"github.com/rm4n0s/emetos/internal/core/dap"
	"github.com/rm4n0s/emetos/internal/core/debug"
)

func TestConnectGetsTheSessionInItsContext(t *testing.T) {
	var mu sync.Mutex
	var seen *debug.Session
	var e *env
	e = newEnv(t, func(o *debug.Options) {
		o.Debugger = func(typ string) (debug.Debugger, bool) {
			return debug.Debugger{Type: typ, Connect: func(ctx context.Context, cfg debug.Config, opts dap.ClientOptions) (*dap.Client, error) {
				s, _ := debug.SessionFromContext(ctx)
				mu.Lock()
				seen = s
				mu.Unlock()
				return e.connect(ctx, cfg, opts)
			}}, true
		}
	})
	e.script("app", plain("app"))
	sess, ok, err := e.start(fakeConfig("app"))
	if err != nil || !ok {
		t.Fatalf("start: %v %v", ok, err)
	}
	idle(t, sess)
	mu.Lock()
	defer mu.Unlock()
	if seen != sess {
		t.Errorf("Connect saw session %v, want %v", seen, sess)
	}
}

func TestSessionFromContext(t *testing.T) {
	tests := []struct {
		name string
		ctx  context.Context
		ok   bool
	}{
		{"a plain context", context.Background(), false},
		{"a context with a nil session", debug.WithSession(context.Background(), nil), false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if _, ok := debug.SessionFromContext(tt.ctx); ok != tt.ok {
				t.Errorf("ok = %v, want %v", ok, tt.ok)
			}
		})
	}
}

func TestAppendConsoleSendsAnOutputEventOfNoSession(t *testing.T) {
	e := newEnv(t, nil)
	e.svc.AppendConsole("hello\n")
	ev := e.wait(debug.EventOutput, nil)
	if ev.Session != nil || ev.Output == nil || ev.Output.Category != "console" || ev.Output.Output != "hello\n" {
		t.Errorf("event = %+v, output %+v", ev, ev.Output)
	}
}
