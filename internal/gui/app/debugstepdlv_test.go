//go:build emetos_dlv

package app

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"

	"github.com/rm4n0s/emetos/extension"
	"github.com/rm4n0s/emetos/extensions/golang"
	"github.com/rm4n0s/emetos/internal/core/command"
	"github.com/rm4n0s/emetos/internal/core/debug"
	"github.com/rm4n0s/emetos/internal/core/keybinding"
)

// TestF10IsHonouredWithDlvWhenTheLineIsShown is TestF10IsHonouredWhenTheLineOfTheStepIsShown with the real Delve: the loop of the stepping
// measurement, F10 pressed 60 ms after the line of the editor moved, a hundred times.
//
//	go test -tags emetos_dlv ./internal/gui/app -run TestF10IsHonouredWithDlv -v -count=1
func TestF10IsHonouredWithDlvWhenTheLineIsShown(t *testing.T) {
	if st, err := os.Stat(pinnedDlv); err != nil || st.IsDir() {
		t.Skipf("the pinned dlv is not at %s (scripts/fetch-tools.sh dlv)", pinnedDlv)
	}
	if _, err := exec.LookPath("go"); err != nil {
		t.Skip("go is not installed")
	}
	folder := t.TempDir()
	const source = "package main\n\nfunc main() {\n\ttotal := 0\n\tfor i := 0; i < 1000; i++ {\n\t\ttotal += i * 2\n\t\ttotal %= 7919\n\t}\n\tprintln(total)\n}\n"
	launch := `{"version": "0.2.0", "configurations": [{"name": "Loop", "type": "go", "request": "launch", "mode": "debug", "program": "${workspaceFolder}"}]}`
	for name, text := range map[string]string{"main.go": source, "go.mod": "module example.com/loop\n\ngo 1.21\n", ".emetos/launch.json": launch} {
		if err := os.MkdirAll(filepath.Dir(filepath.Join(folder, name)), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(folder, name), []byte(text), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	r := newDebugRigWith(t, folder, rigOpts{open: []string{"main.go"}, extensions: []extension.Factory{golang.New}})
	r.win.Extensions().Activate(golang.ID)
	r.pump()
	r.setSetting("go.alternateTools", map[string]any{"dlv": pinnedDlv})
	r.setSetting("go.toolsEnvVars", map[string]any{"GOCACHE": toolsDir + "/gocache", "GOPATH": toolsDir + "/gopath", "GOMODCACHE": toolsDir + "/gomodcache",
		"GOFLAGS": "", "GOTOOLCHAIN": "local", "GOPROXY": "off", "CGO_ENABLED": "0"})
	if _, err := r.svc().AddBreakpoints(context.Background(), filepath.Join(folder, "main.go"), []debug.BreakpointData{{Line: 6}}); err != nil {
		t.Fatal(err)
	}
	r.run(command.ViewContainerDebug, nil)
	r.run(command.DebugStartCommand, nil)
	frameLine := func() int {
		if f := r.svc().Model().Focus().Frame; f != nil {
			return f.Range.Line
		}
		return 0
	}
	r.waitFor("the stop", func() bool { return frameLine() != 0 && r.win.Keys.Get("debugState") == "stopped" })
	delay := 440 * time.Millisecond
	ignored := 0
	for i := 1; i <= 100; i++ {
		before := frameLine()
		state := r.win.Keys.Get("debugState")
		for end := time.Now().Add(delay); time.Now().Before(end); {
			r.ts.FlushPosted()
			r.ts.Pump(5 * time.Millisecond)
			time.Sleep(time.Millisecond)
		}
		if !sendChord(r.ts, keybinding.Chord{Key: keybinding.F10}) {
			ignored++
			t.Errorf("step %d: F10 ignored at line %d, debugState was %v, is %v", i, before, state, r.win.Keys.Get("debugState"))
			r.waitState("stopped")
			continue
		}
		for dl := time.Now().Add(15 * time.Second); frameLine() == before || frameLine() == 0; {
			if time.Now().After(dl) {
				t.Fatalf("step %d: the line stays at %d", i, before)
			}
			r.ts.FlushPosted()
			r.ts.Pump(2 * time.Millisecond)
			time.Sleep(time.Millisecond)
		}
	}
	t.Logf("F10 ignored %d times in 100", ignored)
}
