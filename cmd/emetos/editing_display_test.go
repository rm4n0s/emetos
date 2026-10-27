//go:build emetos_display

package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/rm4n0s/emetos/internal/core/lsp/lsptest"
	"github.com/rm4n0s/emetos/internal/testenv/display"
)

// These tests type into the editor through the compositor, as a user does (SPEC §15.1 "Real input", step 17b.3): the brackets
// the editor closes, the snippets a completion inserts and the signature help.

// startScratch starts the editor on an empty Go file, with the cursor in it.
func startScratch(t *testing.T, opts display.Options) *display.App {
	t.Helper()
	opts.Args = []string{".", "scratch.go"}
	if opts.Project == nil {
		opts.Project = map[string]string{}
	}
	opts.Project["scratch.go"] = ""
	a := display.Start(t, opts)
	a.Settle()
	return a
}

// TestRealInputTypingOverAClosingBracketTheUserTyped: `// z)`, Left, `)` makes `// z))`; only a closing character the editor
// typed is typed over (editor.autoClosingOvertype auto, SPEC §8.6.4).
func TestRealInputTypingOverAClosingBracketTheUserTyped(t *testing.T) {
	a := startScratch(t, display.Options{})
	a.Type("// z)")
	a.Press("Left")
	a.Type(")")
	a.Eventually("the text to be typed", func() (bool, string) {
		got := editorText(a)
		return got == "// z))", got
	})
}

// fakeGopls makes the user settings that run the fake language server (internal/core/lsp/lsptest) in place of gopls (go.goplsPath):
// a script that offers, after a dot, Sprintf as a snippet with only the final tab stop (insertText "Sprintf($0)") and Println
// as gopls offers a call (a text edit "Println(${1:})"), and signature help inside an open parenthesis and none outside it,
// where gopls has none either.
func fakeGopls(t *testing.T) map[string]string {
	t.Helper()
	script := lsptest.Script{
		Capabilities: json.RawMessage(`{"textDocumentSync":1,
			"completionProvider":{"triggerCharacters":["."]},
			"signatureHelpProvider":{"triggerCharacters":["("],"retriggerCharacters":[","]}}`),
		Responses: map[string]lsptest.Response{
			"textDocument/completion": {Result: json.RawMessage(`{"isIncomplete":false,"items":[
				{"label":"Sprintf","kind":3,"detail":"func(format string, a ...any) string","insertTextFormat":2,"insertText":"Sprintf($0)"},
				{"label":"Println","kind":3,"detail":"func(a ...any) (n int, err error)","insertTextFormat":2,
				 "textEdit":{"range":{"start":{"line":0,"character":4},"end":{"line":0,"character":4}},"newText":"Println(${1:})"}}]}`)},
			"textDocument/signatureHelp": {InsideParens: true, Result: json.RawMessage(`{"signatures":[
				{"label":"f(format string, a ...any) string","parameters":[{"label":"format string"},{"label":"a ...any"}]}],
				"activeSignature":0,"activeParameter":0}`)},
		},
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
	return map[string]string{"settings.json": string(settings)}
}

// completeWith types `fmt.` and the start of an item, waits for the suggestions, and accepts the item with a key.
func completeWith(t *testing.T, a *display.App, typed, key string) {
	t.Helper()
	a.Type("fmt.")
	a.Eventually("the suggestions to show", func() (bool, string) {
		return a.Editor().SuggestVisible, editorText(a)
	})
	a.Type(typed)
	a.Settle()
	a.Press(key)
	a.Settle()
}

// TestRealInputCompletionWithAFinalTabStopEndsTheSnippet: accepting Sprintf (Sprintf($0)) with Enter or Tab leaves no snippet
// session and no tab stop on screen, `("x")` types `fmt.Sprintf(("x"))`, and the signature help of the open parenthesis is gone
// when the cursor leaves the call (SPEC §9.9, §9.12). The bracket typed over is the user's own: only the editor's are typed
// over (§8.6.4).
func TestRealInputCompletionWithAFinalTabStopEndsTheSnippet(t *testing.T) {
	for _, key := range []string{"Enter", "Tab"} {
		t.Run(key, func(t *testing.T) {
			a := startScratch(t, display.Options{Config: fakeGopls(t)})
			completeWith(t, a, "Sprintf", key)
			a.Eventually("Sprintf to be inserted", func() (bool, string) {
				got := editorText(a)
				return got == "fmt.Sprintf()", got
			})
			if ed := a.Editor(); ed.InSnippet || ed.TabStops != 0 {
				t.Errorf("after the completion: in snippet %v, %d tab stops; want a session that ended", ed.InSnippet, ed.TabStops)
			}
			a.Type(`("x")`)
			a.Eventually("the call to be typed", func() (bool, string) {
				got := editorText(a)
				return got == `fmt.Sprintf(("x"))`, got
			})
			if ed := a.Editor(); ed.InSnippet || ed.TabStops != 0 {
				t.Errorf("after typing: in snippet %v, %d tab stops; want none", ed.InSnippet, ed.TabStops)
			}
			// the cursor is inside the outer call: the signature help shows; after End and Enter it is after the call, where the
			// server has no signature
			a.Eventually("the signature help to show inside the call", func() (bool, string) {
				return a.Editor().SignatureVisible, editorText(a)
			})
			a.Press("End", "Enter")
			a.Eventually("the signature help to close", func() (bool, string) {
				return !a.Editor().SignatureVisible, editorText(a)
			})
			a.Type("a")
			a.Settle()
			if ed := a.Editor(); ed.InSnippet || ed.TabStops != 0 {
				t.Errorf("after the next line: in snippet %v, %d tab stops; want none", ed.InSnippet, ed.TabStops)
			}
		})
	}
}

// TestRealInputTypingOverTheClosingBracketOfACompletion: gopls offers a call as `Println(${1:})`; the `)` of the snippet is
// typed over like the one the editor closes (VS Code tracks the pairs of the edits of a snippet), so `"x")` makes
// `fmt.Println("x")` and ends the snippet, and what is typed next does not grow into the tab stop.
func TestRealInputTypingOverTheClosingBracketOfACompletion(t *testing.T) {
	a := startScratch(t, display.Options{Config: fakeGopls(t)})
	completeWith(t, a, "Println", "Tab")
	a.Eventually("Println to be inserted", func() (bool, string) {
		got := editorText(a)
		return got == "fmt.Println()", got
	})
	if ed := a.Editor(); !ed.InSnippet {
		t.Error("the tab stop inside the parentheses has no session")
	}
	a.Type(`"x")`)
	a.Eventually("the call to be typed", func() (bool, string) {
		got := editorText(a)
		return got == `fmt.Println("x")`, got
	})
	a.Press("Enter")
	a.Type("ab")
	a.Eventually("the next line to be typed", func() (bool, string) {
		got := editorText(a)
		return got == "fmt.Println(\"x\")\nab", got
	})
	if ed := a.Editor(); ed.InSnippet || ed.TabStops != 0 {
		t.Errorf("after typing on: in snippet %v, %d tab stops; want none", ed.InSnippet, ed.TabStops)
	}
}

// TestRealInputSignatureHelpClosesWhenTheCursorLeavesTheArguments: the hints open with the parenthesis, stay while the cursor is
// in the arguments, close when it leaves them (the server has no signature there), and Escape closes them.
func TestRealInputSignatureHelpClosesWhenTheCursorLeavesTheArguments(t *testing.T) {
	a := startScratch(t, display.Options{Config: fakeGopls(t)})
	a.Type("f(")
	a.Eventually("the signature help to show", func() (bool, string) {
		return a.Editor().SignatureVisible, editorText(a)
	})
	a.Type("1")
	a.Settle()
	if !a.Editor().SignatureVisible {
		t.Error("the signature help closed while typing an argument")
	}
	a.Press("Right", "Right")
	a.Eventually("the signature help to close after the cursor left the call", func() (bool, string) {
		ed := a.Editor()
		return !ed.SignatureVisible, "text " + ed.Text
	})
	a.Press("Left")
	a.Settle()
	if a.Editor().SignatureVisible {
		t.Error("the signature help came back with a move of the cursor")
	}
	// Escape closes the hints and leaves the text
	a.Type("(")
	a.Eventually("the signature help to show again", func() (bool, string) {
		return a.Editor().SignatureVisible, editorText(a)
	})
	a.Press("Escape")
	a.Eventually("the signature help to close on Escape", func() (bool, string) {
		return !a.Editor().SignatureVisible, editorText(a)
	})
}
