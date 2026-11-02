//go:build emetos_dlv

package app

import (
	"os"
	"os/exec"
	"path/filepath"
	"testing"

	"github.com/rm4n0s/emetos/extension"
	"github.com/rm4n0s/emetos/extensions/golang"
	"github.com/rm4n0s/emetos/internal/core/command"
	"github.com/rm4n0s/emetos/internal/gui/testutil"
)

// The Remote Work test of D46 with the real Delve (step 25.12, tag emetos_dlv): a client sets a breakpoint in the Go fixture of the proofs,
// starts a session with the Go extension's debugger, steps over and stops it, and the host's window sees the same session. It is skipped
// when the pinned dlv (scripts/fetch-tools.sh dlv) or go is missing.
//
//	go test -tags emetos_dlv ./internal/gui/app -run Dlv -v

const (
	pinnedDlv = "/var/tmp/emetos-tools/bin/dlv"
	toolsDir  = "/var/tmp/emetos-tools"
)

func TestAClientDebugsTheGoFixtureWithDlvAndTheHostSeesTheSameSession(t *testing.T) {
	if st, err := os.Stat(pinnedDlv); err != nil || st.IsDir() {
		t.Skipf("the pinned dlv is not at %s (scripts/fetch-tools.sh dlv)", pinnedDlv)
	}
	if _, err := exec.LookPath("go"); err != nil {
		t.Skip("go is not installed")
	}
	folder := t.TempDir()
	copyTree(t, filepath.Join(testutil.RepoRoot(t), "testdata", "proof", "25.7", "project"), folder)
	launch := `{"version": "0.2.0", "configurations": [{"name": "Debug the order", "type": "go", "request": "launch", "mode": "debug", "program": "${workspaceFolder}"}]}`
	if err := os.MkdirAll(filepath.Join(folder, ".emetos"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(folder, ".emetos", "launch.json"), []byte(launch), 0o644); err != nil {
		t.Fatal(err)
	}
	r := newDebugRigWith(t, folder, rigOpts{
		open:       []string{"main.go"},
		extensions: []extension.Factory{golang.New},
	})
	// the settings of the Go extension are registered by it, so it starts first
	r.win.Extensions().Activate(golang.ID)
	r.pump()
	r.setSetting("go.alternateTools", map[string]any{"dlv": pinnedDlv})
	r.setSetting("go.toolsEnvVars", map[string]any{"GOCACHE": toolsDir + "/gocache", "GOPATH": toolsDir + "/gopath", "GOMODCACHE": toolsDir + "/gomodcache",
		"GOFLAGS": "", "GOTOOLCHAIN": "local", "GOPROXY": "off", "CGO_ENABLED": "0"})
	host := r.mainEditor()
	client := r.clientView()
	client.shell.openPathAt(client.shell.binding, filepath.Join(folder, "main.go"), 12, 1, openOptions{TakeFocus: true})
	r.waitFor("the client's editor", func() bool { e := client.shell.activeEditor(); return e != nil && e.dbg != nil })
	mine := client.shell.activeEditor()
	r.cursorAt(mine, 11) // line 12: amount := order.Total * rate

	runOn(t, client, command.DebugToggleBreakpoint)
	r.waitFor("the breakpoint on both screens", func() bool {
		return len(r.bps()) == 1 && r.bps()[0].Line == 12 && len(host.view.DebugBreakpoints()) == 1 && len(mine.view.DebugBreakpoints()) == 1
	})

	runOn(t, client, command.DebugStartCommand)
	r.waitFor("the stop at the breakpoint on both screens", func() bool {
		return len(host.view.DebugFrames()) == 1 && len(mine.view.DebugFrames()) == 1 &&
			r.win.Keys.Get("debugState") == "stopped" && client.Keys.Get("debugState") == "stopped"
	})
	sessions := r.svc().Model().Sessions(false)
	if len(sessions) != 1 {
		t.Fatalf("the host's service has %d sessions after the client's start", len(sessions))
	}
	if h, c := host.view.DebugFrames()[0], mine.view.DebugFrames()[0]; h.Line != 11 || c.Line != 11 {
		t.Errorf("the stopped line is %d on the host's screen and %d on the client's, want 11 (line 12)", h.Line, c.Line)
	}
	variable := func(w *Workspace, name string) string {
		for _, n := range w.shell.debug.panes.Variables.Model().Rows() {
			if row, ok := n.Data.(dbgviewRow); ok && row.Variable != nil && row.Variable.Name == name {
				return row.Variable.Value()
			}
		}
		return ""
	}
	r.waitFor("the variables on both screens", func() bool { return variable(r.win, "rate") == "0.1" && variable(client, "rate") == "0.1" })

	// the client steps over: the program is stopped on the next line on both screens, and `amount` is there
	runOn(t, client, command.DebugStepOverCommand)
	r.waitFor("the step on both screens", func() bool {
		return len(host.view.DebugFrames()) == 1 && len(mine.view.DebugFrames()) == 1 &&
			host.view.DebugFrames()[0].Line == 12 && mine.view.DebugFrames()[0].Line == 12
	})
	r.waitFor("the new value on both screens", func() bool {
		return variable(r.win, "amount") == "4.95" && variable(client, "amount") == "4.95"
	})
	if now := r.svc().Model().Sessions(false); len(now) != 1 || now[0] != sessions[0] {
		t.Errorf("the step changed the session: %v", now)
	}

	runOn(t, client, command.DebugStopCommand)
	r.waitFor("the end on both screens", func() bool {
		return r.win.Keys.Get("debugState") == "inactive" && client.Keys.Get("debugState") == "inactive" &&
			len(host.view.DebugFrames()) == 0 && len(mine.view.DebugFrames()) == 0
	})
	if n := len(r.svc().Model().Sessions(false)); n != 0 {
		t.Errorf("%d sessions are left after the client's stop", n)
	}
}
