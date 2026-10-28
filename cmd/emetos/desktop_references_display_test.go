//go:build emetos_display

package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/rm4n0s/emetos/internal/core/lsp/lsptest"
	"github.com/rm4n0s/emetos/internal/gui/app"
	"github.com/rm4n0s/emetos/internal/testenv/display"
)

// Find All References and Find All Implementations through the context menu, the References view and its keys (step 17e.3,
// SPEC §10.28), with a fake language server that answers from a script (TestDesktopFindAllReferences) and with the real gopls
// (TestDesktopFindAllReferencesWithGopls, under the tag emetos_gopls).

// refProject is the module of the tests. It is indented with spaces, so that a column of the text is a column of the screen.
var refProject = map[string]string{
	"go.mod":   "module example.com/x\n\ngo 1.21\n",
	"main.go":  "package main\n\nimport \"fmt\"\n\nfunc main() {\n    fmt.Println(Greet(\"world\"))\n    fmt.Println(Greet(\"again\"))\n}\n",
	"util.go":  "package main\n\n// Greet says hello.\nfunc Greet(name string) string {\n    return \"hello \" + name\n}\n",
	"extra.go": "package main\n\nfunc Extra() string { return Greet(\"extra\") }\n",
}

// location is an LSP location of a word on a line (counted from 0) of a file of the project.
func location(t *testing.T, project, file string, line int, word string) string {
	t.Helper()
	text := strings.Split(refProject[file], "\n")[line]
	col := strings.Index(text, word)
	if col < 0 {
		t.Fatalf("%q is not on line %d of %s", word, line, file)
	}
	b, err := json.Marshal(map[string]any{
		"uri": "file://" + filepath.Join(project, file),
		"range": map[string]any{
			"start": map[string]int{"line": line, "character": col}, "end": map[string]int{"line": line, "character": col + len(word)},
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}

// withFakeReferences makes the fake language server answer references and implementations (as gopls does for Greet), besides
// what withFakeGopls knows.
func withFakeReferences(t *testing.T) func(projectDir string) {
	t.Helper()
	return func(projectDir string) {
		t.Helper()
		refs := "[" + strings.Join([]string{
			location(t, projectDir, "util.go", 3, "Greet"), location(t, projectDir, "main.go", 5, "Greet"),
			location(t, projectDir, "main.go", 6, "Greet"), location(t, projectDir, "extra.go", 2, "Greet"),
		}, ",") + "]"
		impls := "[" + strings.Join([]string{location(t, projectDir, "util.go", 3, "Greet"), location(t, projectDir, "extra.go", 2, "Extra")}, ",") + "]"
		script := lsptest.Script{
			Capabilities: json.RawMessage(`{"textDocumentSync":1,"referencesProvider":true,"implementationProvider":true}`),
			Responses: map[string]lsptest.Response{
				"textDocument/references":     {Result: json.RawMessage(refs)},
				"textDocument/implementation": {Result: json.RawMessage(impls)},
			},
		}
		writeFakeServer(t, projectDir, script)
	}
}

// writeFakeServer writes the script and the settings that make the editor run the fake server in place of gopls.
func writeFakeServer(t *testing.T, projectDir string, script lsptest.Script) {
	t.Helper()
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

// greetOnScreen is where the word Greet of line 6 of main.go is on the screen.
func greetOnScreen(line int) (x, y float64) { return editorColumnX(18), editorLine(line) }

// referencesMenu opens the context menu of the editor on the word until it has the entry (the language server needs a moment to
// start: the entries are there only with its provider).
func referencesMenu(t *testing.T, a *display.App, x, y float64, entry string) {
	t.Helper()
	a.EventuallyFor(60*time.Second, "the context menu to have "+entry, func() (bool, string) {
		a.RightClick(x, y)
		a.Settle()
		if _, ok := a.Text(entry); ok {
			return true, ""
		}
		a.Press("Escape")
		time.Sleep(500 * time.Millisecond)
		return false, a.Dump()
	})
}

// textContaining is the first text on screen that has the part in it, from the top.
func textContaining(a *display.App, part string) (app.DevText, bool) {
	var best app.DevText
	found := false
	for _, tx := range a.Texts() {
		if strings.Contains(tx.Text, part) && (!found || tx.Y < best.Y) {
			best, found = tx, true
		}
	}
	return best, found
}

func TestDesktopFindAllReferences(t *testing.T) {
	a := display.Start(t, display.Options{Args: []string{".", "main.go"}, Project: refProject, Prepare: withFakeReferences(t)})
	a.Settle()
	x, y := greetOnScreen(6)

	// the context menu has the two entries, after the editor's own navigation and Peek, once the server is there
	referencesMenu(t, a, x, y, "Find All References")
	a.WaitText("Find All Implementations")
	shot(t, a, "references-menu")
	a.ClickText("Find All References")
	a.WaitText("4 results in 3 files")
	a.WaitText("REFERENCES")
	shot(t, a, "references-view")

	// Find All Implementations replaces the result and the title of the view
	a.RightClick(x, y)
	a.WaitText("Find All Implementations")
	a.ClickText("Find All Implementations")
	a.WaitText("2 results in 2 files")
	a.WaitText("IMPLEMENTATIONS")
	shot(t, a, "implementations-view")

	// the key on the word runs Find All References again; the cursor is where the right click put it
	a.Click(x, y)
	a.Press("shift+alt+f12")
	a.WaitText("4 results in 3 files")
	a.WaitNoText("IMPLEMENTATIONS")

	// a click on a file row toggles it, a click on a location opens the file there. The row of extra.go is open already: a result
	// keeps the files that were open in the one before it (Find All Implementations opened it, as the file nearest to the
	// cursor), so the first click closes it, and the second, after the time in which two clicks on a row are one double click
	// (400 ms; a double click opens the row and toggles nothing), opens it again. The test used to click up to three times with a
	// pause, taking the first click for one that had not opened the row.
	extraPlace := func() (app.DevText, bool) { return textContaining(a, "Greet(\"extra\")") }
	if _, ok := extraPlace(); !ok {
		t.Fatalf("the file extra.go is not open in the view, as the result before this one left it:\n%s", a.Dump())
	}
	a.ClickText("extra.go")
	a.Eventually("a click to close the file extra.go in the view", func() (bool, string) {
		_, ok := extraPlace()
		return !ok, a.Dump()
	})
	time.Sleep(500 * time.Millisecond)
	a.ClickText("extra.go")
	var tx app.DevText
	a.Eventually("a click to open the file extra.go in the view", func() (bool, string) {
		var ok bool
		tx, ok = extraPlace()
		return ok, a.Dump()
	})
	a.Click(tx.X+tx.Width/2, tx.Y+tx.Height/2)
	a.Eventually("extra.go to open at the place", func() (bool, string) {
		return activeTab(a) == "extra.go" && a.Editor().Line == 3, activeTab(a) + " " + itoa(a.Editor().Line)
	})

	// F4 and Shift+F4 walk the places, round the files: after the only place of extra.go comes the first of main.go
	a.Press("f4")
	a.Eventually("F4 to go to main.go", func() (bool, string) {
		return activeTab(a) == "main.go" && a.Editor().Line == 6, activeTab(a) + " " + itoa(a.Editor().Line)
	})
	a.Press("shift+f4")
	a.Eventually("Shift+F4 to go back to extra.go", func() (bool, string) { return activeTab(a) == "extra.go", activeTab(a) })

	// Dismiss takes a place out of the result
	a.WaitText("4 results in 3 files")
	tx, _ = textContaining(a, "Greet(\"extra\")")
	a.MoveTo(tx.X+tx.Width/2, tx.Y+tx.Height/2)
	a.Settle()
	a.RightClick(tx.X+tx.Width/2, tx.Y+tx.Height/2)
	a.WaitText("Copy All")
	a.ClickText("Dismiss")
	a.WaitText("3 results in 2 files")

	// Clear shows the history
	a.Press("ctrl+shift+p")
	a.Eventually("the palette to open", func() (bool, string) { return len(a.Inputs()) > 0, a.Dump() })
	a.Type("References: Clear")
	a.WaitText("References: Clear")
	a.Press("Enter")
	a.WaitText("No results. Try running a previous search again:")
	a.WaitText("Greet")
	shot(t, a, "references-history")
}

// TestRealInputReferencesProof makes the proof image of step 17e.3: the References view with a result, and over the editor the
// context menu with the two entries.
func TestRealInputReferencesProof(t *testing.T) {
	a := display.Start(t, display.Options{Args: []string{".", "main.go"}, Project: refProject, Prepare: withFakeReferences(t)})
	a.Settle()
	x, y := greetOnScreen(6)
	referencesMenu(t, a, x, y, "Find All References")
	a.ClickText("Find All References")
	a.WaitText("4 results in 3 files")
	a.ClickText("extra.go")
	a.RightClick(x, y)
	a.WaitText("Find All Implementations")
	a.WaitText("Find All References")
	a.Settle()
	a.Screenshot(filepath.Join(proofDir(t), "17e.3-references.png"))
	a.Press("Escape")
}
