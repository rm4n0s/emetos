//go:build emetos_display

package main

import (
	"testing"
	"time"

	"github.com/rm4n0s/emetos/internal/testenv/display"
)

// The compiled-in extensions as a user meets them: Notes, TODOs and Tasks (step 17b.5).

var extProject = map[string]string{"todo.go": "package main\n\n// TODO: fix the thing\nfunc main() {\n\t// FIXME: and this\n}\n"}

func TestDesktopNotes(t *testing.T) {
	a := display.Start(t, display.Options{Args: []string{".", "todo.go"}, Project: extProject, KeyDelay: 25 * time.Millisecond})
	a.Settle()
	a.Click(24, 155) // the Notes icon of the activity bar
	a.WaitText("No notes yet. Use New Note to make one.")
	// the action of the header shows while the pointer is over it
	a.MoveTo(200, 81)
	a.Click(329, 81)
	a.Eventually("a note to open", func() (bool, string) {
		_, ok := a.Focused()
		return ok, joinNames(tabNames(a))
	})
	a.Type("Shopping list")
	a.WaitFocusedText("Shopping list")
	inputs := sortedInputs(a)
	if len(inputs) < 2 {
		t.Fatalf("the note has %d text fields: %+v", len(inputs), inputs)
	}
	a.ClickInput(inputs[1])
	a.Type("milk and eggs")
	a.WaitFocusedText("milk and eggs")
	a.Settle()
	shot(t, a, "notes")
	// the list in the side bar shows the note by its title, and a click on it comes back to it
	a.WaitText("Shopping list")
	a.Press("ctrl+w")
	a.Settle()
	a.ClickText("Shopping list")
	a.Eventually("the note to open again with its text", func() (bool, string) {
		for _, in := range a.Inputs() {
			if in.Text == "milk and eggs" {
				return true, ""
			}
		}
		return false, a.Dump()
	})
	shot(t, a, "notes-reopened")
}

func TestDesktopTodos(t *testing.T) {
	a := display.Start(t, display.Options{Args: []string{".", "todo.go"}, Project: extProject})
	a.Settle()
	a.Click(24, 107) // the TODOs icon
	a.WaitText("fix the thing")
	shot(t, a, "todos")
	a.ClickText("and this")
	a.Eventually("the cursor on the FIXME", func() (bool, string) { return a.Editor().Line == 5, itoa(a.Editor().Line) })
	a.ClickText("fix the thing")
	a.Eventually("the cursor on the TODO", func() (bool, string) { return a.Editor().Line == 3, itoa(a.Editor().Line) })
	shot(t, a, "todos-opened")
}

func TestDesktopTasks(t *testing.T) {
	a := display.Start(t, display.Options{
		Args: []string{".", "todo.go"}, Project: extProject, KeyDelay: 25 * time.Millisecond,
		Config: map[string]string{"settings.json": `{"emetos.tasks.commands": ["echo todo.go:3:6: boom"]}`},
	})
	a.Settle()
	a.Press("ctrl+shift+p")
	a.Type("Tasks: Run Task")
	a.WaitText("Tasks: Run Task")
	a.Press("Enter")
	a.WaitText("todo.go:3:6: boom")
	a.WaitText("done, under 1s")
	shot(t, a, "tasks")
	// the error line is a problem: it is in the Problems panel, and a click on it goes to its place
	a.Press("ctrl+shift+m")
	a.WaitText("boom")
	a.ClickText("boom")
	a.Eventually("the cursor at the problem", func() (bool, string) { return a.Editor().Line == 3, itoa(a.Editor().Line) }) //nolint:mnd
	shot(t, a, "tasks-problem")
}
