package python

import (
	"context"
	"log/slog"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"testing"

	"github.com/rm4n0s/emetos/extension"
	"github.com/rm4n0s/emetos/extensions/internal/fakeapi"
)

// The commands over the fake API.

// commandCoverage says which test runs each command of the manifest. TestEveryCommandHasAHandlerAndATest keeps it whole: a command added
// to the manifest without a line here, or a line without a command, fails.
var commandCoverage = map[string]string{
	SetInterpreterCommand:  "TestChoosingAnInterpreterKeepsItRestartsTheServerAndFollowsTheChoice",
	OrganizeImportsCommand: "TestOrganizeImportsAsksTheServerToSortTheImports",
	RestartCommand:         "TestRestartLanguageServerRestartsTheServer",
}

func TestEveryCommandHasAHandlerAndATest(t *testing.T) {
	r := newRig(t, rigOptions{})
	declared := map[string]bool{}
	for _, c := range r.ext.Manifest().Contributes.Commands {
		declared[c.ID] = true
		if c.Category != commandCategory || c.Title == "" || c.Description == "" {
			t.Errorf("command %+v lacks its category, title or description", c)
		}
		if _, ok := commandCoverage[c.ID]; !ok {
			t.Errorf("the command %s has no entry in commandCoverage", c.ID)
		}
		if !r.api.HasHandler(c.ID) {
			t.Errorf("the command %s has no handler", c.ID)
		}
	}
	for id := range commandCoverage {
		if !declared[id] {
			t.Errorf("commandCoverage names %s, which the manifest does not declare", id)
		}
	}
	for _, c := range r.ext.Manifest().Contributes.Commands {
		if c.ID == OrganizeImportsCommand && c.When != "editorLangId == python" {
			t.Errorf("%s is enabled when %q", c.ID, c.When)
		}
	}
	// the ids are those of VS Code's Python extension
	for _, id := range []string{"python.setInterpreter", "python.analysis.restartLanguageServer"} {
		if !declared[id] {
			t.Errorf("the command %s is not declared", id)
		}
	}
}

func TestOrganizeImportsAsksTheServerToSortTheImports(t *testing.T) {
	type call = fakeapi.CodeActionCall
	tests := []struct {
		name   string
		server string
		setup  func(r *rig) // before the command
		// command is what Pyright's server is asked to execute, answer its outcome; action is what python-lsp-server's code action answers
		commandErr  error
		action      func(c call) (string, error)
		wantCommand bool
		wantAction  bool
		wantNote    string
	}{
		{"basedpyright runs its command", ServerBasedpyright, func(r *rig) { r.editor("a.py", "python", "import os\n") }, nil, nil, true, false, ""},
		{"Pyright runs its command", ServerPyright, func(r *rig) { r.editor("a.py", "python", "import os\n") }, nil, nil, true, false, ""},
		{"the server fails", ServerBasedpyright, func(r *rig) { r.editor("a.py", "python", "import os\n") }, testErr("boom"), nil, true, false,
			"Organize Imports: the language server failed: boom"},
		{"python-lsp-server: the action is applied", ServerPylsp, func(r *rig) { r.editor("a.py", "python", "import os\n") }, nil, func(call) (string, error) { return "Sort imports", nil }, false, true, ""},
		{"python-lsp-server: nothing is offered", ServerPylsp, func(r *rig) { r.editor("a.py", "python", "import os\n") }, nil, func(call) (string, error) { return "", nil }, false, true,
			"Organize Imports: the language server offers nothing to change in a.py. python-lsp-server needs a plugin that sorts imports, such as pylsp-isort."},
		{"python-lsp-server: the action fails", ServerPylsp, func(r *rig) { r.editor("a.py", "python", "import os\n") }, nil, func(call) (string, error) { return "", testErr("boom") }, false, true,
			"Organize Imports: the language server failed: boom"},
		{"no file in front", ServerBasedpyright, func(r *rig) {}, nil, nil, false, false, "Organize Imports needs a Python file: open one first."},
		{"a file that is not Python", ServerBasedpyright, func(r *rig) { r.editor("a.md", "markdown", "x") }, nil, nil, false, false, "Organize Imports needs a Python file: open one first."},
		{"the server is not running", ServerBasedpyright, func(r *rig) {
			r.editor("a.py", "python", "")
			r.api.SetServerStatus(ServerID, extension.ServerStatus{State: extension.ServerMissing})
		}, nil, nil, false, false, "Organize Imports needs the language server, which is not available. Its state is in the status bar."},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			r := newRig(t, rigOptions{servers: []string{"basedpyright", "pyright", "pylsp"}, settings: map[string]any{LanguageServerSetting: tt.server}})
			if _, err := r.run(); err != nil {
				t.Fatal(err)
			}
			tt.setup(r)
			if tt.action != nil {
				r.api.OnCodeAction(tt.action)
			}
			r.api.OnServerCommand(func(fakeapi.ServerCall) error { return tt.commandErr })
			if err := r.api.Run(OrganizeImportsCommand, nil); err != nil {
				t.Fatal(err)
			}
			r.settle()
			calls, actions := r.api.ServerCalls(), r.api.CodeActionCalls()
			if tt.wantCommand != (len(calls) == 1) || tt.wantAction != (len(actions) == 1) {
				t.Fatalf("commands %+v, code actions %+v", calls, actions)
			}
			if tt.wantCommand {
				c := calls[0]
				want := map[string]string{ServerBasedpyright: "basedpyright.organizeimports", ServerPyright: "pyright.organizeimports"}[tt.server]
				if c.Server != ServerID || c.Command != want || len(c.Args) != 1 || !strings.HasSuffix(c.Args[0].(string), "/a.py") {
					t.Errorf("command = %+v", c)
				}
			}
			if tt.wantAction {
				c := actions[0]
				if c.Server != ServerID || !strings.HasSuffix(c.URI, "/a.py") || !slices.Equal(c.Req.Only, []string{"source.organizeImports"}) {
					t.Errorf("code action = %+v", c)
				}
			}
			var last string
			if n := r.notes(); len(n) > 0 {
				last = n[len(n)-1]
			}
			if last != tt.wantNote {
				t.Errorf("note = %q, want %q", last, tt.wantNote)
			}
		})
	}
}

func TestRestartLanguageServerRestartsTheServer(t *testing.T) {
	r := newRig(t, rigOptions{servers: []string{"basedpyright"}})
	if err := r.api.Run(RestartCommand, nil); err != nil {
		t.Fatal(err)
	}
	if got := r.api.ServerRestarts(ServerID); got != 1 {
		t.Errorf("restarts = %d", got)
	}
}

func TestThePackLogsNoWarningInNormalUse(t *testing.T) {
	var mu sync.Mutex
	var lines []string
	r := newRigWith(t, rigOptions{servers: []string{"basedpyright"}, files: map[string]string{"a.py": ""}}, func(a *fakeapi.API) {
		a.SetLogger(slog.New(slog.NewTextHandler(writerFunc(func(p []byte) {
			mu.Lock()
			lines = append(lines, string(p))
			mu.Unlock()
		}), &slog.HandlerOptions{Level: slog.LevelDebug})))
	})
	fakeEnv(t, filepath.Join(r.folder, ".venv"), "3.12.1", "")
	r.editor("a.py", "python", "")
	if _, err := r.run(); err != nil {
		t.Fatal(err)
	}
	if err := r.api.Run(SetInterpreterCommand, nil); err != nil {
		t.Fatal(err)
	}
	r.settle()
	r.api.LastPick().Choose(0)
	r.api.LastInput().Cancel()
	_ = r.api.Run(RestartCommand, nil)
	r.settle()
	mu.Lock()
	defer mu.Unlock()
	for _, l := range lines {
		if strings.Contains(l, "level=WARN") || strings.Contains(l, "level=ERROR") {
			t.Errorf("the pack logged: %s", l)
		}
	}
}

// writerFunc is a writer that calls a function.
type writerFunc func([]byte)

func (f writerFunc) Write(p []byte) (int, error) { f(p); return len(p), nil }

func TestATabWithoutAFolderHasAnInterpreterAndOffersNothing(t *testing.T) {
	r := newRigWith(t, rigOptions{servers: []string{"basedpyright"}, noFolder: true}, nil)
	r.editor("a.py", "python", "")
	if cur, ok := r.ext.interps.Current(); !ok || cur.Version != "3.11.2" {
		t.Errorf("Current = %+v, %v", cur, ok)
	}
	if n := r.notes(); len(n) != 0 {
		t.Errorf("notes = %q", n)
	}
	if err := r.ext.interps.Choose(filepath.Join(r.bin, "python3")); err != nil {
		t.Fatal(err)
	}
	if r.chosenState() != filepath.Join(r.bin, "python3") {
		t.Errorf("state = %q", r.chosenState())
	}
	_ = context.Background()
}
