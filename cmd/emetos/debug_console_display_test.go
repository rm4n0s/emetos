//go:build emetos_display

package main

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/rm4n0s/emetos/extensions/devdebug"
	"github.com/rm4n0s/emetos/internal/core/dap/daptest"
	"github.com/rm4n0s/emetos/internal/testenv/display"
)

// The Debug Console, with real input (step 25.6, SPEC §10.31): F5 starts the fake adapter of daptest through the dev debugger, its output
// shows in the console that opens with the first session, Ctrl+Shift+Y gives the input the keyboard, a line typed and Enter are
// evaluated by the adapter and the result shows, and Up brings the line back.

const consoleLoader = `package src

func Load(text string) []string { return nil }
`

func consoleScript(t *testing.T) []byte {
	t.Helper()
	s := daptest.Script{
		daptest.Reply("initialize", map[string]any{"supportsConfigurationDoneRequest": true}),
		daptest.Expect("launch", nil),
		daptest.Event("initialized", nil),
		daptest.Reply("configurationDone", nil),
		daptest.Respond("launch", nil),
		daptest.Reply("threads", map[string]any{"threads": []map[string]any{{"id": 1, "name": "main"}}}),
		daptest.Event("output", map[string]any{"category": "stdout", "output": "hello from the program\n"}),
		daptest.Event("output", map[string]any{"category": "stderr", "output": "careful with the prices\n"}),
		daptest.ReplyArgs("evaluate", map[string]any{"expression": "40 + 2", "context": "repl"}, map[string]any{"result": "forty-two", "type": "string"}),
		daptest.Reply("disconnect", nil),
		daptest.WaitClose(),
	}
	lines, err := s.Lines()
	if err != nil {
		t.Fatal(err)
	}
	return lines
}

func TestRealInputDebugConsoleEvaluatesWhatIsTyped(t *testing.T) {
	adapter := daptest.Program(t)
	script := consoleScript(t)
	launch := `{"version":"0.2.0","configurations":[{"type":"dev.debug.fake","request":"launch","name":"Debug the loader","script":"${workspaceFolder}/fake.jsonl"}]}`
	a := display.Start(t, display.Options{
		Args: []string{".", "src/loader.go"},
		Project: map[string]string{
			"src/loader.go":       consoleLoader,
			".emetos/launch.json": launch,
		},
		Prepare: func(dir string) {
			if err := os.WriteFile(filepath.Join(dir, "fake.jsonl"), script, 0o644); err != nil {
				t.Fatal(err)
			}
		},
		Env: []string{devdebug.AdapterEnv + "=" + adapter},
	})
	a.Settle()

	// F5 with the keyboard in the editor: the console opens with the first session and shows what the program printed
	a.Click(editorColumnX(8), editorLine(1))
	a.Press("f5")
	a.EventuallyFor(60*time.Second, "the output of the program in the Debug Console", func() (bool, string) {
		_, ok := a.Text("hello from the program")
		return ok, a.Dump()
	})
	if _, ok := a.Text("careful with the prices"); !ok {
		t.Errorf("the line of stderr is not in the console: %s", a.Dump())
	}

	// Ctrl+Shift+Y gives the input the keyboard; the line is evaluated by the adapter on Enter
	a.Press("ctrl+shift+y")
	a.Eventually("the input of the console to have the keyboard", func() (bool, string) {
		in, ok := a.Focused()
		return ok && in.Y > 450, a.Dump() // the input of the panel is low in the window
	})
	a.Type("40 + 2")
	a.Press("enter")
	a.EventuallyFor(30*time.Second, "the result of the evaluation", func() (bool, string) {
		_, ok := a.Text("forty-two")
		return ok, a.Dump()
	})
	if _, ok := a.Text("40 + 2"); !ok {
		t.Errorf("the line that was typed is not in the console: %s", a.Dump())
	}

	// Up brings the line back from the history
	a.Press("up")
	a.Eventually("the line from the history", func() (bool, string) { return a.FocusedText() == "40 + 2", a.Dump() })
}
