//go:build emetos_display

package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"github.com/rm4n0s/emetos/internal/core/lsp/lsptest"
	"github.com/rm4n0s/emetos/internal/gui/app"
	"github.com/rm4n0s/emetos/internal/testenv/display"
)

// Helpers of the desktop pass (step 17b.5, docs/DESKTOP-PASS.md): the tests use Emetos as a user does, through the compositor,
// find what to click from the texts on screen (dev.texts) and read the effect from dev.* state or the file system.

// shot saves a screenshot of the window as <name>.png in the folder of the proofs; the pass views every one.
func shot(t *testing.T, a *display.App, name string) {
	t.Helper()
	a.Settle()
	a.Screenshot(filepath.Join(proofDir(t), "desktop-"+name+".png"))
}

// textWhere is the first text on screen (from the top, then from the left) that is exactly text and passes keep.
func textWhere(a *display.App, text string, keep func(app.DevText) bool) (app.DevText, bool) {
	var best app.DevText
	found := false
	for _, tx := range a.Texts() {
		if tx.Text != text || !keep(tx) {
			continue
		}
		if !found || tx.Y < best.Y || (tx.Y == best.Y && tx.X < best.X) {
			best, found = tx, true
		}
	}
	return best, found
}

// clickTextWhere clicks the text that passes keep; the test fails when there is none.
func clickTextWhere(t *testing.T, a *display.App, text string, keep func(app.DevText) bool) {
	t.Helper()
	tx, ok := textWhere(a, text, keep)
	if !ok {
		t.Fatalf("no text %q on screen (that passes the filter)", text)
	}
	a.Click(tx.X+tx.Width/2, tx.Y+tx.Height/2)
}

// inRect keeps the texts whose top left corner is in a rectangle of the window.
func inRect(x0, y0, x1, y1 float64) func(app.DevText) bool {
	return func(tx app.DevText) bool { return tx.X >= x0 && tx.X < x1 && tx.Y >= y0 && tx.Y < y1 }
}

// inExplorerHeader keeps the texts of the header of the Explorer (its buttons).
var inExplorerHeader = inRect(0, 60, 348, 90)

// tabNames are the names of the tabs of the groups, as "group:name" in order.
func tabNames(a *display.App) []string {
	var out []string
	for _, g := range a.Workbench().Groups {
		for _, tab := range g.Tabs {
			out = append(out, tab.Name)
		}
	}
	return out
}

// activeTab is the name of the active tab of the active group.
func activeTab(a *display.App) string {
	for _, g := range a.Workbench().Groups {
		if !g.Active {
			continue
		}
		for _, tab := range g.Tabs {
			if tab.Active {
				return tab.Name
			}
		}
	}
	return ""
}

// waitFile waits until a file exists (or does not) in the project.
func waitFile(a *display.App, rel string, exists bool) {
	a.Eventually("the file "+rel+" to "+map[bool]string{true: "exist", false: "be gone"}[exists], func() (bool, string) {
		_, err := os.Stat(filepath.Join(a.Project, rel))
		return (err == nil) == exists, "stat error: " + errText(err)
	})
}

func errText(err error) string {
	if err == nil {
		return "none"
	}
	return err.Error()
}

// readProjectFile reads a file of the project.
func readProjectFile(t *testing.T, a *display.App, rel string) string {
	t.Helper()
	data, err := os.ReadFile(filepath.Join(a.Project, rel))
	if err != nil {
		t.Fatal(err)
	}
	return string(data)
}

// trashFiles is the folder files go to when the explorer moves them to the trash (the XDG data folder of the run).
func trashFiles(a *display.App) string {
	return filepath.Join(filepath.Dir(a.Project), "data", "Trash", "files")
}

// contains tells whether a list has an element.
func contains(list []string, want string) bool {
	for _, s := range list {
		if s == want {
			return true
		}
	}
	return false
}

// joinNames is a list in one line, for a message.
func joinNames(list []string) string { return strings.Join(list, ", ") }

// goApp is the Go file the editing tests open.
const goApp = "package main\n\nimport \"fmt\"\n\nfunc main() {\n\tfmt.Println(\"hi\")\n}\n"

// goplsOptions say what the fake language server of a desktop test answers.
type goplsOptions struct {
	// Diagnostics publishes one error for app.go when the file is opened.
	Diagnostics bool
}

// withFakeGopls writes the user settings that run the fake language server (internal/core/lsp/lsptest) in place of gopls
// (go.goplsPath), into the configuration folder of the run, and the script it follows: completion after a dot, hover, document
// symbols, signature help inside an open parenthesis, and (optionally) a diagnostic for app.go. It is a Prepare function of
// display.Options, since the diagnostic needs the project folder, which exists only when the run is made.
func withFakeGopls(t *testing.T, opts goplsOptions) func(projectDir string) {
	t.Helper()
	return func(projectDir string) {
		t.Helper()
		script := lsptest.Script{
			Capabilities: json.RawMessage(`{"textDocumentSync":1,"hoverProvider":true,"documentSymbolProvider":true,
				"completionProvider":{"triggerCharacters":["."]},
				"signatureHelpProvider":{"triggerCharacters":["("],"retriggerCharacters":[","]}}`),
			Responses: map[string]lsptest.Response{
				"textDocument/completion": {Result: json.RawMessage(`{"isIncomplete":false,"items":[
					{"label":"Println","kind":3,"detail":"func(a ...any) (n int, err error)","insertTextFormat":1,"insertText":"Println"},
					{"label":"Printf","kind":3,"detail":"func(format string, a ...any) (n int, err error)","insertTextFormat":1,"insertText":"Printf"},
					{"label":"Sprintf","kind":3,"detail":"func(format string, a ...any) string","insertTextFormat":1,"insertText":"Sprintf"}]}`)},
				"textDocument/hover": {Result: json.RawMessage(`{"contents":{"kind":"markdown","value":"` + "```go\\nfunc main()\\n```" + `\n\nThe entry point of the program."}}`)},
				"textDocument/documentSymbol": {Result: json.RawMessage(`[
					{"name":"main","kind":12,"range":{"start":{"line":4,"character":0},"end":{"line":6,"character":1}},
					 "selectionRange":{"start":{"line":4,"character":5},"end":{"line":4,"character":9}}}]`)},
				"textDocument/signatureHelp": {InsideParens: true, Result: json.RawMessage(`{"signatures":[
					{"label":"Println(a ...any) (n int, err error)","parameters":[{"label":"a ...any"}]}],"activeSignature":0,"activeParameter":0}`)},
			},
		}
		if opts.Diagnostics {
			params := `{"uri":"file://` + filepath.Join(projectDir, "app.go") + `","diagnostics":[{"range":{"start":{"line":5,"character":1},"end":{"line":5,"character":4}},
				"severity":1,"source":"gopls","code":"UndeclaredName","message":"undefined: fmtt"}]}`
			script.Send = []lsptest.Outgoing{{After: "textDocument/didOpen", Method: "textDocument/publishDiagnostics", Params: json.RawMessage(params)}}
		}
		data, err := json.Marshal(script)
		if err != nil {
			t.Fatal(err)
		}
		dir := t.TempDir()
		scriptPath := filepath.Join(dir, "script.json")
		if err := os.WriteFile(scriptPath, data, 0o600); err != nil {
			t.Fatal(err)
		}
		program := filepath.Join(dir, "gopls")
		launcher := "#!/bin/sh\nexec " + lsptest.BuildProgram(t) + " -script " + scriptPath + " \"$@\"\n"
		if err := os.WriteFile(program, []byte(launcher), 0o700); err != nil { //nolint:gosec // a script the test runs
			t.Fatal(err)
		}
		settings, err := json.Marshal(map[string]any{"go.goplsPath": program})
		if err != nil {
			t.Fatal(err)
		}
		config := filepath.Join(filepath.Dir(projectDir), "config", "emetos")
		if err := os.MkdirAll(config, 0o700); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(config, "settings.json"), settings, 0o600); err != nil {
			t.Fatal(err)
		}
	}
}

// editorLine is the window y of the middle of a line of the editor (counted from 1) when the document is not scrolled, and
// editorColumnX the x of the left edge of a column (counted from 0) of the editor's text with the Explorer open.
func editorLine(line int) float64   { return 102 + float64(line-1)*19 }
func editorColumnX(col int) float64 { return 435 + float64(col)*8.4 }

func itoa(n int) string { return strconv.Itoa(n) }
