package golang

import (
	"testing"

	"github.com/rm4n0s/glutter/gluttertest"
	gui "github.com/rm4n0s/glutter/ui"
	"github.com/rm4n0s/glutter/widgets"

	"github.com/rm4n0s/emetos/extension/ui/uitest"
)

// The goldens of the panel view "Go Tools" (step 14.1), in both themes: before any run, while one works and after one that
// failed on a tool.

func lines(run *installRun, text ...string) {
	run.output = append(run.output, text...)
}

func TestGoldenTheToolsPanel(t *testing.T) {
	states := []struct {
		name  string
		setup func(r *rig)
	}{
		{"idle", func(*rig) {}},
		{"running", func(r *rig) {
			run := &installRun{running: true, cancel: func() {}}
			for i, tl := range allTools() {
				run.rows = append(run.rows, toolRow{tool: tl, state: []int{toolInstalled, toolInstalling, toolPending}[i]})
			}
			lines(run, "$ go install golang.org/x/tools/gopls@latest", "go: downloading golang.org/x/tools/gopls@latest", "gopls is installed.",
				"$ go install github.com/go-delve/delve/cmd/dlv@latest", "go: downloading github.com/go-delve/delve v1.25.0")
			r.ext.tools.run = run
		}},
		{"failed", func(r *rig) {
			run := &installRun{summary: "Could not install: dlv."}
			for i, tl := range allTools() {
				run.rows = append(run.rows, toolRow{tool: tl, state: []int{toolInstalled, toolFailed, toolInstalled}[i]})
			}
			lines(run, "$ go install golang.org/x/tools/gopls@latest", "gopls is installed.",
				"$ go install github.com/go-delve/delve/cmd/dlv@latest", "go: github.com/go-delve/delve/cmd/dlv@latest: module lookup disabled by GOPROXY=off",
				"dlv: exit status 1", "$ go install honnef.co/go/tools/cmd/staticcheck@latest", "staticcheck is installed.")
			r.ext.tools.run = run
		}},
	}
	for _, th := range uitest.Themes() {
		for _, st := range states {
			t.Run(th.Name+"/"+st.name, func(t *testing.T) {
				r := newRig(t, rigOptions{})
				st.setup(r)
				panel := r.ext.tools
				host := widgets.NewStatefulBuilder(func(bc widgets.BuildContext, set widgets.StateSetter) widgets.Widget { return panel.build(bc) })
				ts := uitest.Pump(t, th, gui.Size{Width: 720, Height: 300}, host)
				uitest.MatchesGolden(ts, "tools_"+st.name+"_"+th.Name)
			})
		}
	}
}

// The goldens of the panel view "Go Run" (step 14.2), in both themes: before any run, while one works, and after one that ended well
// or badly.

func TestGoldenTheRunPanel(t *testing.T) {
	states := []struct {
		name  string
		setup func(r *rig)
	}{
		{"idle", func(*rig) {}},
		{"running", func(r *rig) {
			r.ext.runs.run = &runJob{label: "cmd/server", state: runRunning, cancel: func() {}, output: []string{
				"$ go run .", "2026/10/26 12:00:00 listening on :8080", "2026/10/26 12:00:03 GET /health 200 1.2ms"}}
		}},
		{"exited", func(r *rig) {
			r.ext.runs.run = &runJob{label: ".", state: runExited, status: "exit status 0", cancel: func() {}, output: []string{
				"$ go run .", "Hello, Emetos!", "3 + 4 = 7"}}
		}},
		{"failed", func(r *rig) {
			r.ext.runs.run = &runJob{label: ".", state: runFailed, status: "exit status 1", cancel: func() {}, output: []string{
				"$ go run .", "# example.com/p", "./main.go:7:2: undefined: greet"}}
		}},
	}
	for _, th := range uitest.Themes() {
		for _, st := range states {
			t.Run(th.Name+"/"+st.name, func(t *testing.T) {
				r := newRig(t, rigOptions{})
				st.setup(r)
				panel := r.ext.runs
				host := widgets.NewStatefulBuilder(func(bc widgets.BuildContext, set widgets.StateSetter) widgets.Widget { return panel.build(bc) })
				ts := uitest.Pump(t, th, gui.Size{Width: 720, Height: 220}, host)
				uitest.MatchesGolden(ts, "run_"+st.name+"_"+th.Name)
			})
		}
	}
}

func TestTheButtonsOfTheRunPanel(t *testing.T) {
	r := newRig(t, rigOptions{withGo: true})
	r.focus("main.go", "package main\n§\nfunc main() {}\n")
	panel := r.ext.runs
	var rebuild widgets.StateSetter
	host := widgets.NewStatefulBuilder(func(bc widgets.BuildContext, set widgets.StateSetter) widgets.Widget {
		rebuild = set
		return panel.build(bc)
	})
	ts := uitest.Pump(t, uitest.Themes()[0], gui.Size{Width: 720, Height: 220}, host)
	// Run, before any run
	ts.Tap(gluttertest.ByText("Run"))
	run := r.ended()
	if run.state != runExited || len(r.goRuns()) != 1 {
		t.Fatalf("state %d, runs %d", run.state, len(r.goRuns()))
	}
	// Run Again, after a run
	rebuild(func() {})
	ts.Pump()
	ts.Tap(gluttertest.ByText("Run Again"))
	r.api.WaitFor("the second run", func() bool { return len(r.goRuns()) == 2 })
	r.ended()
	// Stop, while one runs
	stopped := false
	panel.run = &runJob{label: ".", state: runRunning, cancel: func() { stopped = true }}
	rebuild(func() {})
	ts.Pump()
	ts.Tap(gluttertest.ByText("Stop"))
	if !stopped || !panel.run.stopping {
		t.Errorf("stopped %v, stopping %v", stopped, panel.run.stopping)
	}
}
