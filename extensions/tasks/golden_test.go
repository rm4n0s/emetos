package tasks

import (
	"testing"

	gui "github.com/rm4n0s/glutter/ui"
	"github.com/rm4n0s/glutter/widgets"

	"github.com/rm4n0s/emetos/extension/ui/uitest"
)

// widgetOf builds the widget of the view as the box does.
func widgetOf(r *rig) widgets.Widget {
	return widgets.NewBuilder(func(bc widgets.BuildContext) widgets.Widget { return r.api.View(ViewID).Content.Build(bc) })
}

// TestGoldenTheTasksPanel looks at the view after a run that found problems, and before any run, in both themes.
func TestGoldenTheTasksPanel(t *testing.T) {
	for _, th := range uitest.Themes() {
		t.Run("after a run "+th.Name, func(t *testing.T) {
			r := newRig(t, "go test ./...", "go vet ./...")
			r.api.SetSetting(CommandsSetting, []string{"go test ./...", "go vet ./..."})
			r.api.SetSetting(CommandsSetting, []string{"printf 'ok  \\texample.com/app\\t0.012s\\n# example.com/app/util\\n./main.go:3:4: undefined: foo\\n./main.go:7:1: warning: unused result\\nFAIL\\texample.com/app/util [build failed]\\n'"})
			_ = r.api.Run(RunCommand, nil)
			r.done()
			ts := uitest.Pump(t, th, gui.Size{Width: 560, Height: 160}, widgetOf(r))
			uitest.MatchesGolden(ts, "tasks_panel_run_"+th.Name)
		})
		t.Run("before a run "+th.Name, func(t *testing.T) {
			r := newRig(t, "go test ./...", "go vet ./...")
			ts := uitest.Pump(t, th, gui.Size{Width: 560, Height: 100}, widgetOf(r))
			uitest.MatchesGolden(ts, "tasks_panel_idle_"+th.Name)
		})
	}
}
