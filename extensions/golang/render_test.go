package golang

import (
	"testing"

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
