//go:build emetos_display

package main

import (
	"testing"

	"github.com/rm4n0s/emetos/internal/testenv/display"
)

// The compiled-in extensions as a user meets them: Tasks (step 17b.5; its Notes and TODOs tests went with those extensions in step
// 17e.1).

var extProject = map[string]string{"tool.go": "package main\n\nfunc main() {\n}\n"}

func TestDesktopTasks(t *testing.T) {
	a := display.Start(t, display.Options{
		Args: []string{".", "tool.go"}, Project: extProject,
		Config: map[string]string{"settings.json": `{"emetos.tasks.commands": ["echo tool.go:3:6: boom"]}`},
	})
	a.Settle()
	a.Press("ctrl+shift+p")
	a.Type("Tasks: Run Task")
	a.WaitText("Tasks: Run Task")
	a.Press("Enter")
	a.WaitText("tool.go:3:6: boom")
	a.WaitText("done, under 1s")
	shot(t, a, "tasks")
	// the error line is a problem: it is in the Problems panel, and a click on it goes to its place
	a.Press("ctrl+shift+m")
	a.WaitText("boom")
	a.ClickText("boom")
	a.Eventually("the cursor at the problem", func() (bool, string) { return a.Editor().Line == 3, itoa(a.Editor().Line) }) //nolint:mnd
	shot(t, a, "tasks-problem")
}
