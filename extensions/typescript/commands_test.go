package typescript

import (
	"encoding/json"
	"log/slog"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"testing"

	"github.com/rm4n0s/errors"

	"github.com/rm4n0s/emetos/extension"
	"github.com/rm4n0s/emetos/extensions/internal/fakeapi"
)

// The commands over the fake API: what they ask the server, in which units, and what they show of the answer.

// commandCoverage says which test runs each command of the manifest. TestEveryCommandHasAHandlerAndATest keeps it whole: a command added to
// the manifest without a line here, or a line without a command, fails.
var commandCoverage = map[string]string{
	RestartCommand:               "TestRestartTSServerRestartsTheServer",
	SelectVersionCommand:         "TestSelectTypeScriptVersionListsTheVersionsAndKeepsTheChoice",
	GoToProjectConfigCommand:     "TestGoToProjectConfigurationOpensTheConfigOfTheFile",
	GoToJSProjectConfigCommand:   "TestGoToProjectConfigurationOpensTheConfigOfTheFile",
	GoToSourceDefinitionCommand:  "TestGoToSourceDefinitionShowsThePlaceInTheSourceOfTheLibrary",
	FindFileReferencesCommand:    "TestFindFileReferencesListsTheFilesThatImportTheFile",
	OrganizeImportsCommand:       "TestTheSourceActionsAskForTheirKind",
	SortImportsCommand:           "TestTheSourceActionsAskForTheirKind",
	JSSortImportsCommand:         "TestTheSourceActionsAskForTheirKind",
	RemoveUnusedImportsCommand:   "TestTheSourceActionsAskForTheirKind",
	JSRemoveUnusedImportsCommand: "TestTheSourceActionsAskForTheirKind",
}

func TestEveryCommandHasAHandlerAndATest(t *testing.T) {
	r := newRig(t, rigOptions{globalTS: "6.0.3"})
	declared := map[string]bool{}
	for _, c := range r.ext.Manifest().Contributes.Commands {
		declared[c.ID] = true
		if c.Category == "" || c.Title == "" || c.Description == "" {
			t.Errorf("command %+v lacks its category, title or description", c)
		}
		if _, ok := commandCoverage[c.ID]; !ok {
			t.Errorf("the command %s has no entry in commandCoverage", c.ID)
		}
		if !r.api.HasHandler(c.ID) {
			t.Errorf("the command %s has no handler", c.ID)
		}
		// the category follows the id, as VS Code's
		if want := map[bool]string{true: "JavaScript", false: "TypeScript"}[strings.HasPrefix(c.ID, "javascript.")]; c.Category != want {
			t.Errorf("%s is in the category %q, want %q", c.ID, c.Category, want)
		}
	}
	for id := range commandCoverage {
		if !declared[id] {
			t.Errorf("commandCoverage names %s, which the manifest does not declare", id)
		}
	}
	for _, c := range r.ext.Manifest().Contributes.Commands {
		switch {
		case strings.HasPrefix(c.ID, "javascript.") && c.When != whenJS:
			t.Errorf("%s is enabled when %q", c.ID, c.When)
		case c.ID == SortImportsCommand && c.When != whenTS:
			t.Errorf("%s is enabled when %q", c.ID, c.When)
		}
	}
}

func TestRestartTSServerRestartsTheServer(t *testing.T) {
	r := newRig(t, rigOptions{globalTS: "6.0.3"})
	if err := r.api.Run(RestartCommand, nil); err != nil {
		t.Fatal(err)
	}
	if got := r.api.ServerRestarts(ServerID); got != 1 {
		t.Errorf("restarts = %d", got)
	}
}

func TestTheSourceActionsAskForTheirKind(t *testing.T) {
	tests := []struct {
		command, kind, title, language string
	}{
		{OrganizeImportsCommand, "source.organizeImports", "Organize Imports", "typescript"},
		{OrganizeImportsCommand, "source.organizeImports", "Organize Imports", "javascriptreact"},
		{SortImportsCommand, "source.sortImports", "Sort Imports", "typescriptreact"},
		{JSSortImportsCommand, "source.sortImports", "Sort Imports", "javascript"},
		{RemoveUnusedImportsCommand, "source.removeUnusedImports", "Remove Unused Imports", "typescript"},
		{JSRemoveUnusedImportsCommand, "source.removeUnusedImports", "Remove Unused Imports", "javascript"},
	}
	for _, tt := range tests {
		t.Run(tt.command+" in "+tt.language, func(t *testing.T) {
			r := newRig(t, rigOptions{globalTS: "6.0.3"})
			if _, err := r.run(); err != nil {
				t.Fatal(err)
			}
			uri := r.editor("src/a.ts", tt.language, "import a from \"a\";\n", extension.Selection{
				Anchor: extension.Position{Line: 0, Col: 2}, Active: extension.Position{Line: 0, Col: 5},
			})
			r.api.OnCodeAction(func(fakeapi.CodeActionCall) (string, error) { return tt.title, nil })
			if err := r.api.Run(tt.command, nil); err != nil {
				t.Fatal(err)
			}
			r.settle()
			calls := r.api.CodeActionCalls()
			if len(calls) != 1 || calls[0].URI != uri || len(calls[0].Req.Only) != 1 || calls[0].Req.Only[0] != tt.kind {
				t.Fatalf("calls = %+v", calls)
			}
			if r := calls[0].Req.Range; r.Start.Col != 2 || r.End.Col != 5 {
				t.Errorf("range = %+v: the selection", r)
			}
			if n := r.notes(); len(n) != 0 {
				t.Errorf("notes = %q", n)
			}

			// the server offers nothing: the user is told
			r.api.OnCodeAction(func(fakeapi.CodeActionCall) (string, error) { return "", nil })
			_ = r.api.Run(tt.command, nil)
			r.settle()
			if !strings.Contains(r.note(), tt.title+": nothing to change in a.ts.") {
				t.Errorf("note = %q", r.note())
			}
			// and when it fails
			r.api.OnCodeAction(func(fakeapi.CodeActionCall) (string, error) { return "", errors.New("TestFailure", "tsserver crashed") })
			_ = r.api.Run(tt.command, nil)
			r.settle()
			if !strings.Contains(r.note(), tt.title+": the language server failed: tsserver crashed") {
				t.Errorf("note = %q", r.note())
			}
		})
	}
}

func TestTheSourceActionsNeedAFileOfTheServerAndARunningServer(t *testing.T) {
	r := newRig(t, rigOptions{globalTS: "6.0.3"})
	// no editor
	_ = r.api.Run(OrganizeImportsCommand, nil)
	if !strings.Contains(r.note(), "Organize Imports needs a TypeScript or JavaScript file") {
		t.Errorf("note = %q", r.note())
	}
	// a file of another language
	r.editor("a.go", "go", "package a\n")
	_ = r.api.Run(OrganizeImportsCommand, nil)
	if !strings.Contains(r.note(), "needs a TypeScript or JavaScript file") {
		t.Errorf("note = %q", r.note())
	}
	// the server is not running
	r.editor("a.ts", "typescript", "")
	_ = r.api.Run(SortImportsCommand, nil)
	if !strings.Contains(r.note(), "Sort Imports needs typescript-language-server, which is not running yet") {
		t.Errorf("note = %q", r.note())
	}
	if len(r.api.CodeActionCalls()) != 0 {
		t.Errorf("calls = %+v", r.api.CodeActionCalls())
	}
	for state, word := range map[extension.ServerState]string{
		extension.ServerStarting: "starting", extension.ServerMissing: "not available", extension.ServerFailed: "stopped after crashing", extension.ServerStopped: "stopped",
	} {
		r.api.SetServerStatus(ServerID, extension.ServerStatus{State: state})
		_ = r.api.Run(RemoveUnusedImportsCommand, nil)
		if !strings.Contains(r.note(), "which is "+word) {
			t.Errorf("%s: note = %q", state, r.note())
		}
	}
}

// loc is a Location of the protocol as the server answers it.
func loc(path string, line, character int) string {
	return `{"uri":"file://` + path + `","range":{"start":{"line":` + strconv.Itoa(line) + `,"character":` + strconv.Itoa(character) + `},"end":{"line":` + strconv.Itoa(line) + `,"character":` + strconv.Itoa(character+3) + `}}}`
}

func TestGoToSourceDefinitionShowsThePlaceInTheSourceOfTheLibrary(t *testing.T) {
	lib := "/work/node_modules/emetoslib/index.js"
	t.Run("one place is shown", func(t *testing.T) {
		r := newRig(t, rigOptions{globalTS: "6.0.3"})
		if _, err := r.run(); err != nil {
			t.Fatal(err)
		}
		uri := r.editor("src/main.ts", "typescript", "greet(1);\n", extension.Selection{Active: extension.Position{Line: 0, Col: 2}, Anchor: extension.Position{Line: 0, Col: 2}})
		r.answer(map[string]string{"workspace/executeCommand": "[" + loc(lib, 1, 16) + "]"})
		if err := r.api.Run(GoToSourceDefinitionCommand, nil); err != nil {
			t.Fatal(err)
		}
		r.settle()
		reqs := r.requests("workspace/executeCommand")
		if len(reqs) != 1 || reqs[0] != `{"arguments":["`+uri+`",{"line":0,"character":2}],"command":"_typescript.goToSourceDefinition"}` {
			t.Fatalf("requests = %v", reqs)
		}
		// the column of the protocol counts UTF-16 units, as Show does
		shown := r.api.ShownDocuments()
		if len(shown) != 1 || shown[0].URI != "file://"+lib || shown[0].Line != 2 || shown[0].Column != 17 || shown[0].Preview {
			t.Errorf("shown = %+v", shown)
		}
	})
	t.Run("the position is in the units the server chose", func(t *testing.T) {
		// "é" is two bytes and one UTF-16 unit; the cursor is on the f of "const é = f(1);"
		for encoding, want := range map[string]int{"utf-8": 11, "utf-16": 10, "utf-32": 10} {
			r := newRig(t, rigOptions{globalTS: "6.0.3"})
			if _, err := r.run(); err != nil {
				t.Fatal(err)
			}
			r.api.SetServerEncoding(ServerID, encoding)
			r.editor("src/main.ts", "typescript", "const é = f(1);\n", extension.Selection{Active: extension.Position{Line: 0, Col: 11}, Anchor: extension.Position{Line: 0, Col: 11}})
			if err := r.api.Run(GoToSourceDefinitionCommand, nil); err != nil {
				t.Fatal(err)
			}
			r.settle()
			reqs := r.requests("workspace/executeCommand")
			if len(reqs) != 1 || !strings.Contains(reqs[0], `{"line":0,"character":`+strconv.Itoa(want)+`}`) {
				t.Errorf("%s: requests = %v, want the column %d", encoding, reqs, want)
			}
		}
	})
	t.Run("several places are listed", func(t *testing.T) {
		r := newRig(t, rigOptions{globalTS: "6.0.3"})
		if _, err := r.run(); err != nil {
			t.Fatal(err)
		}
		r.editor("src/main.ts", "typescript", "greet(1);\n")
		r.answer(map[string]string{"workspace/executeCommand": "[" + loc(lib, 0, 4) + "," + loc("/work/node_modules/emetoslib/other.js", 3, 9) + "]"})
		_ = r.api.Run(GoToSourceDefinitionCommand, nil)
		r.settle()
		pick := r.api.LastPick()
		if pick == nil || len(pick.Labels()) != 2 || pick.Labels()[1] != "other.js" || pick.Options().Items[1].Description != "4:10" {
			t.Fatalf("pick = %+v", pick)
		}
		pick.Choose(1)
		if shown := r.api.ShownDocuments(); len(shown) != 1 || shown[0].URI != "file:///work/node_modules/emetoslib/other.js" || shown[0].Line != 4 || shown[0].Column != 10 {
			t.Errorf("shown = %+v", shown)
		}
	})
	t.Run("none, a failure, and a place that cannot be opened", func(t *testing.T) {
		r := newRig(t, rigOptions{globalTS: "6.0.3"})
		if _, err := r.run(); err != nil {
			t.Fatal(err)
		}
		r.editor("src/main.ts", "typescript", "greet(1);\n")
		for _, answer := range []string{"null", "[]"} {
			r.answer(map[string]string{"workspace/executeCommand": answer})
			_ = r.api.Run(GoToSourceDefinitionCommand, nil)
			r.settle()
			if r.note() != "No source definitions found." {
				t.Errorf("%s: note = %q", answer, r.note())
			}
		}
		r.api.OnServerRequest(ServerID, func(fakeapi.ServerRequest) (json.RawMessage, error) {
			return nil, errors.New("TestFailure", "Debug Failure")
		})
		_ = r.api.Run(GoToSourceDefinitionCommand, nil)
		r.settle()
		if !strings.Contains(r.note(), "Go to Source Definition: the language server failed: Debug Failure") {
			t.Errorf("note = %q", r.note())
		}
		r.answer(map[string]string{"workspace/executeCommand": "[" + loc(lib, 0, 0) + "]"})
		r.api.FailShow(errors.New("TestFailure", "no such file"))
		_ = r.api.Run(GoToSourceDefinitionCommand, nil)
		r.settle()
		if !strings.Contains(r.note(), "Cannot open index.js") {
			t.Errorf("note = %q", r.note())
		}
	})
	t.Run("it needs a file and a server", func(t *testing.T) {
		r := newRig(t, rigOptions{globalTS: "6.0.3"})
		_ = r.api.Run(GoToSourceDefinitionCommand, nil)
		if !strings.Contains(r.note(), "needs a TypeScript or JavaScript file") {
			t.Errorf("note = %q", r.note())
		}
		r.editor("src/main.ts", "typescript", "x\n")
		_ = r.api.Run(GoToSourceDefinitionCommand, nil)
		if !strings.Contains(r.note(), "needs typescript-language-server") || len(r.requests("workspace/executeCommand")) != 0 {
			t.Errorf("note = %q", r.note())
		}
	})
}

// response is the answer of typescript.tsserverRequest for a body.
func response(body string) string { return `{"type":"response","success":true,"body":` + body + `}` }

func TestGoToProjectConfigurationOpensTheConfigOfTheFile(t *testing.T) {
	tests := []struct {
		name, command, language, answer string
		shown                           string
		note                            string
	}{
		{"a TypeScript file in a project", GoToProjectConfigCommand, "typescript", response(`{"configFileName":"/work/tsconfig.json"}`), "file:///work/tsconfig.json", ""},
		{"a JavaScript file in a project", GoToJSProjectConfigCommand, "javascript", response(`{"configFileName":"/work/jsconfig.json"}`), "file:///work/jsconfig.json", ""},
		{"a TypeScript file in no project", GoToProjectConfigCommand, "typescriptreact", response(`{"configFileName":"/dev/null/inferredProject1*"}`), "", "File is not part of a TypeScript project. Add a tsconfig.json to the folder to make it one."},
		{"a JavaScript file in no project", GoToJSProjectConfigCommand, "javascriptreact", response(`{"configFileName":"/dev/null/inferredProject2*"}`), "", "File is not part of a JavaScript project. Add a jsconfig.json to the folder to make it one."},
		{"the server cannot tell", GoToProjectConfigCommand, "typescript", "null", "", "the language server could not answer for this file"},
		{"no configuration name", GoToProjectConfigCommand, "typescript", response(`{}`), "", "Could not determine the TypeScript or JavaScript project."},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			r := newRig(t, rigOptions{globalTS: "6.0.3"})
			if _, err := r.run(); err != nil {
				t.Fatal(err)
			}
			r.editor("src/main.ts", tt.language, "x\n")
			r.answer(map[string]string{"workspace/executeCommand": tt.answer})
			if err := r.api.Run(tt.command, nil); err != nil {
				t.Fatal(err)
			}
			r.settle()
			want := `{"arguments":["projectInfo",{"file":"` + filepath.Join(r.folder, "src/main.ts") + `","needFileNameList":false},{"isAsync":false,"lowPriority":false}],"command":"typescript.tsserverRequest"}`
			if reqs := r.requests("workspace/executeCommand"); len(reqs) != 1 || reqs[0] != want {
				t.Fatalf("requests = %v\nwant %s", reqs, want)
			}
			shown := r.api.ShownDocuments()
			switch {
			case tt.shown != "":
				if len(shown) != 1 || shown[0].URI != tt.shown || shown[0].Line != 1 || shown[0].Column != 1 {
					t.Errorf("shown = %+v", shown)
				}
			case len(shown) != 0 || !strings.Contains(r.note(), tt.note):
				t.Errorf("shown %+v, note %q; want %q", shown, r.note(), tt.note)
			}
		})
	}
	t.Run("a document that is not a file", func(t *testing.T) {
		r := newRig(t, rigOptions{globalTS: "6.0.3"})
		if _, err := r.run(); err != nil {
			t.Fatal(err)
		}
		r.api.SetDocument("untitled:1", "x")
		r.api.SetActive(&extension.ActiveDocument{URI: "untitled:1", LanguageID: "typescript", Selections: []extension.Selection{{}}})
		_ = r.api.Run(GoToProjectConfigCommand, nil)
		if !strings.Contains(r.note(), "untitled:1 is not a file") || len(r.requests("workspace/executeCommand")) != 0 {
			t.Errorf("note = %q", r.note())
		}
	})
}

func TestFindFileReferencesListsTheFilesThatImportTheFile(t *testing.T) {
	refs := response(`{"refs":[` +
		`{"file":"/work/src/main.ts","start":{"line":2,"offset":10},"end":{"line":2,"offset":28},"lineText":"  import { add } from \"./helpers\";"},` +
		`{"file":"/work/src/unsorted.ts","start":{"line":1,"offset":1},"end":{"line":1,"offset":30},"lineText":"import { sub } from \"./helpers\";"}],"symbolName":"\"/work/src/helpers\""}`)
	t.Run("for the file in front", func(t *testing.T) {
		r := newRig(t, rigOptions{globalTS: "6.0.3"})
		if _, err := r.run(); err != nil {
			t.Fatal(err)
		}
		r.editor("src/helpers.ts", "typescript", "export const x = 1;\n")
		r.answer(map[string]string{"workspace/executeCommand": refs})
		if err := r.api.Run(FindFileReferencesCommand, nil); err != nil {
			t.Fatal(err)
		}
		r.settle()
		want := `{"arguments":["fileReferences",{"file":"` + filepath.Join(r.folder, "src/helpers.ts") + `"},{"isAsync":false,"lowPriority":false}],"command":"typescript.tsserverRequest"}`
		if reqs := r.requests("workspace/executeCommand"); len(reqs) != 1 || reqs[0] != want {
			t.Fatalf("requests = %v\nwant %s", reqs, want)
		}
		pick := r.api.LastPick()
		if pick == nil || len(pick.Labels()) != 2 || pick.Labels()[0] != `import { add } from "./helpers";` {
			t.Fatalf("pick = %+v", pick)
		}
		if d := pick.Options().Items[0].Description; d != "/work/src/main.ts:2" {
			t.Errorf("description = %q", d)
		}
		pick.Choose(1)
		shown := r.api.ShownDocuments()
		if len(shown) != 1 || shown[0].URI != "file:///work/src/unsorted.ts" || shown[0].Line != 1 || shown[0].Column != 1 {
			t.Errorf("shown = %+v", shown)
		}
	})
	t.Run("for the file the command is given", func(t *testing.T) {
		r := newRig(t, rigOptions{globalTS: "6.0.3"})
		if _, err := r.run(); err != nil {
			t.Fatal(err)
		}
		r.answer(map[string]string{"workspace/executeCommand": refs})
		if err := r.api.Run(FindFileReferencesCommand, "file:///work/src/other.ts"); err != nil {
			t.Fatal(err)
		}
		r.settle()
		if reqs := r.requests("workspace/executeCommand"); len(reqs) != 1 || !strings.Contains(reqs[0], `"file":"/work/src/other.ts"`) {
			t.Errorf("requests = %v", reqs)
		}
		if r.api.LastPick() == nil {
			t.Error("no list")
		}
	})
	t.Run("nothing imports it, or the server cannot tell", func(t *testing.T) {
		r := newRig(t, rigOptions{globalTS: "6.0.3"})
		if _, err := r.run(); err != nil {
			t.Fatal(err)
		}
		r.editor("src/helpers.ts", "typescript", "")
		r.answer(map[string]string{"workspace/executeCommand": response(`{"refs":[],"symbolName":"x"}`)})
		_ = r.api.Run(FindFileReferencesCommand, nil)
		r.settle()
		if !strings.Contains(r.note(), "no file imports helpers.ts") {
			t.Errorf("note = %q", r.note())
		}
		r.answer(nil)
		_ = r.api.Run(FindFileReferencesCommand, nil)
		r.settle()
		if !strings.Contains(r.note(), "the language server could not answer for this file") {
			t.Errorf("note = %q", r.note())
		}
		if len(r.api.Picks()) != 0 {
			t.Errorf("picks = %d", len(r.api.Picks()))
		}
	})
	t.Run("the server is not running, or the document is not a file", func(t *testing.T) {
		r := newRig(t, rigOptions{globalTS: "6.0.3"})
		_ = r.api.Run(FindFileReferencesCommand, "file:///work/src/other.ts")
		if !strings.Contains(r.note(), "needs typescript-language-server") {
			t.Errorf("note = %q", r.note())
		}
		if _, err := r.run(); err != nil {
			t.Fatal(err)
		}
		r.api.SetDocument("untitled:1", "x")
		r.api.SetActive(&extension.ActiveDocument{URI: "untitled:1", LanguageID: "typescript", Selections: []extension.Selection{{}}})
		_ = r.api.Run(FindFileReferencesCommand, nil)
		if !strings.Contains(r.note(), "untitled:1 is not a file") {
			t.Errorf("note = %q", r.note())
		}
	})
}

// writerFunc is a writer that calls a function.
type writerFunc func([]byte)

func (f writerFunc) Write(p []byte) (int, error) { f(p); return len(p), nil }

func TestThePackLogsNoWarningInNormalUse(t *testing.T) {
	var mu sync.Mutex
	var lines []string
	r := newRigWith(t, rigOptions{globalTS: "6.0.3", workspaceTS: "5.9.3"}, func(a *fakeapi.API) {
		a.SetLogger(slog.New(slog.NewTextHandler(writerFunc(func(p []byte) {
			mu.Lock()
			lines = append(lines, string(p))
			mu.Unlock()
		}), &slog.HandlerOptions{Level: slog.LevelDebug})))
	})
	if _, err := r.run(); err != nil {
		t.Fatal(err)
	}
	r.editor("a.ts", "typescript", "")
	r.answer(nil)
	for _, id := range []string{SelectVersionCommand, RestartCommand, GoToSourceDefinitionCommand, FindFileReferencesCommand, GoToProjectConfigCommand, OrganizeImportsCommand} {
		_ = r.api.Run(id, nil)
		r.settle()
		if p := r.api.LastPick(); p != nil {
			p.Close()
		}
	}
	r.api.ServerNotify(ServerID, versionMethod, map[string]string{"version": "6.0.3", "source": "user-setting"})
	r.settle()
	mu.Lock()
	defer mu.Unlock()
	for _, l := range lines {
		if strings.Contains(l, "level=WARN") || strings.Contains(l, "level=ERROR") {
			t.Errorf("the pack logged: %s", l)
		}
	}
}
