//go:build emetos_claude

package claude

import (
	"context"
	"os/exec"
	"testing"
	"time"
)

// TestRealClaude runs the adapter against the user's own claude program, found in PATH, with the user's own login. It is a
// manual check (docs/MANUAL-CHECKS.md, step 13.1): it makes one short request that uses the account. It is skipped when
// the program is not installed or the user is not logged in.
func TestRealClaude(t *testing.T) {
	if _, err := exec.LookPath(ProgramName); err != nil {
		t.Skip("the claude program is not installed")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Minute)
	defer cancel()
	a := &Adapter{Dir: t.TempDir()}
	info, err := a.AuthStatus(ctx)
	if err != nil {
		t.Fatalf("AuthStatus: %v", err)
	}
	t.Logf("logged in: %v, method: %q", info.LoggedIn, info.Method)
	if !info.LoggedIn {
		t.Skip("not logged in")
	}

	events := make(chan Event, 1024)
	s, err := a.Start(ctx, SessionOptions{
		PermissionMode: ModePlan,
		OnEvent:        func(ev Event) { events <- ev },
		CanUseTool: func(_ context.Context, req ToolRequest) (Decision, error) {
			t.Logf("tool request: %s", req.ToolName)
			return Decision{Message: "This check allows no tool."}, nil
		},
		Stderr: func(line string) { t.Logf("stderr: %s", line) },
	})
	if err != nil {
		t.Fatalf("Start: %v", err)
	}
	defer func() { _ = s.Close() }()
	t.Logf("commands: %d, models: %d", len(s.Handshake().Commands), len(s.Handshake().Models))

	if _, err := s.Send(ctx, "Reply with the single word: ok. Do not use any tool."); err != nil {
		t.Fatalf("Send: %v", err)
	}
	var partial Partials
	for {
		select {
		case ev := <-events:
			switch m := ev.(type) {
			case *InitMessage:
				t.Logf("init: capabilities %v", m.Capabilities)
			case *StreamEvent:
				partial.Apply(m)
			case *AssistantMessage:
				t.Logf("assistant: %q (streamed: %q)", m.Text(), partial.Text(m.ParentToolUseID))
				partial.Finish(m.ParentToolUseID)
			case *ProtocolErrorEvent:
				t.Errorf("protocol error: %v", m.Err)
			case *ExitEvent:
				t.Fatalf("the program ended before the result: %+v", m)
			case *ResultMessage:
				t.Logf("result: %s, %d turns, cost %.4f USD", m.Subtype, m.NumTurns, m.TotalCostUSD)
				if m.IsError {
					t.Errorf("the turn failed: %v", m.Errors)
				}
				return
			}
		case <-ctx.Done():
			t.Fatal("no result in time")
		}
	}
}
