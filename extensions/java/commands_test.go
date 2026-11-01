package java

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/rm4n0s/errors"

	"github.com/rm4n0s/emetos/extension"
	"github.com/rm4n0s/emetos/extensions/internal/fakeapi"
)

// The commands over the fake API: what they ask the server, and what they show of the answer.

// commandCoverage says which test runs each command of the manifest. TestEveryCommandHasAHandlerAndATest keeps it whole: a command added to
// the manifest without a line here, or a line without a command, fails.
var commandCoverage = map[string]string{
	RestartCommand:                    "TestRestartRestartsTheServer",
	CleanWorkspaceCommand:             "TestCleanWorkspaceDeletesTheWorkspaceAtTheNextStartAfterAsking",
	CompileWorkspaceCommand:           "TestForceCompilationAsksForTheKindAndShowsTheOutcome",
	UpdateProjectConfigurationCommand: "TestUpdateProjectConfigurationAsksTheServerAboutTheProjectOfTheFile",
	OrganizeImportsCommand:            "TestOrganizeImportsAsksForTheSourceActionOfTheFileInFront",
}

func TestEveryCommandHasAHandlerAndATest(t *testing.T) {
	r := newRig(t, rigOptions{javaVersion: "21"})
	declared := map[string]bool{}
	for _, c := range r.ext.Manifest().Contributes.Commands {
		declared[c.ID] = true
		if c.Category != "Java" || c.Title == "" || c.Description == "" {
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
	for _, k := range r.ext.Manifest().Contributes.Keybindings {
		if !declared[k.Command] {
			t.Errorf("the key %s runs %s, which the manifest does not declare", k.Key, k.Command)
		}
	}
}

func TestRestartRestartsTheServer(t *testing.T) {
	r := newRig(t, rigOptions{javaVersion: "21"})
	if err := r.api.Run(RestartCommand, nil); err != nil {
		t.Fatal(err)
	}
	if got := r.api.ServerRestarts(ServerID); got != 1 {
		t.Errorf("restarts = %d", got)
	}
}

func TestCleanWorkspaceDeletesTheWorkspaceAtTheNextStartAfterAsking(t *testing.T) {
	cleaned := func(t *testing.T, answer func(d *fakeapi.FakeDialog)) (restarts int, marker bool) {
		t.Helper()
		r := newRig(t, rigOptions{javaVersion: "21"})
		l, err := r.run()
		if err != nil {
			t.Fatal(err)
		}
		_ = l
		dirs, _ := filepath.Glob(filepath.Join(r.cache, "emetos", "jdtls", "work-*"))
		if len(dirs) != 1 {
			t.Fatalf("workspace folders = %v", dirs)
		}
		writeFile(t, dirs[0], "workspace/.metadata/index", "the index", 0o644)
		if err := r.api.Run(CleanWorkspaceCommand, nil); err != nil {
			t.Fatal(err)
		}
		r.settle()
		dialogs := r.api.OpenDialogs()
		if len(dialogs) != 1 || dialogs[0].Spec().Type != extension.DialogWarning || !strings.Contains(dialogs[0].Spec().Detail, "Nothing in your folder changes") {
			t.Fatalf("dialogs = %+v", dialogs)
		}
		if _, err := os.Stat(filepath.Join(dirs[0], "workspace", ".metadata", "index")); err != nil {
			t.Fatalf("the workspace is gone before the answer: %v", err)
		}
		answer(dialogs[0])
		r.settle()
		restarts = r.api.ServerRestarts(ServerID)
		// nothing is deleted before the start: the old server may still hold the files
		_, statErr := os.Stat(filepath.Join(dirs[0], "workspace", ".metadata", "index"))
		if restarts == 1 && statErr != nil {
			t.Errorf("the workspace was deleted by the command, not by the start: %v", statErr)
		}
		if _, err := r.api.Launch(t.Context(), ServerID); err != nil {
			t.Fatal(err)
		}
		_, statErr = os.Stat(filepath.Join(dirs[0], "workspace", ".metadata", "index"))
		return restarts, statErr == nil
	}
	t.Run("confirmed", func(t *testing.T) {
		restarts, kept := cleaned(t, func(d *fakeapi.FakeDialog) { d.Press("Clean and Restart") })
		if restarts != 1 || kept {
			t.Errorf("restarts = %d, workspace kept = %v", restarts, kept)
		}
	})
	t.Run("cancelled", func(t *testing.T) {
		restarts, kept := cleaned(t, func(d *fakeapi.FakeDialog) { d.Cancel() })
		if restarts != 0 || !kept {
			t.Errorf("restarts = %d, workspace kept = %v", restarts, kept)
		}
	})
	t.Run("only the next start deletes", func(t *testing.T) {
		r := newRig(t, rigOptions{javaVersion: "21"})
		r.ext.cleanNext.Store(true)
		if _, err := r.api.Launch(t.Context(), ServerID); err != nil {
			t.Fatal(err)
		}
		dirs, _ := filepath.Glob(filepath.Join(r.cache, "emetos", "jdtls", "work-*"))
		if len(dirs) != 1 {
			t.Fatalf("workspace folders = %v", dirs)
		}
		writeFile(t, dirs[0], "workspace/index", "x", 0o644)
		if _, err := r.api.Launch(t.Context(), ServerID); err != nil {
			t.Fatal(err)
		}
		if _, err := os.Stat(filepath.Join(dirs[0], "workspace", "index")); err != nil {
			t.Errorf("the second start deleted the workspace too: %v", err)
		}
	})
}

func TestForceCompilationAsksForTheKindAndShowsTheOutcome(t *testing.T) {
	tests := []struct {
		name    string
		arg     any
		choose  int
		params  string
		answer  string
		want    string
		pickAsk bool
	}{
		{"incremental from the picker", nil, 0, "[false]", "1", "The Java build finished.", true},
		{"full from the picker", nil, 1, "[true]", "1", "The Java build finished.", true},
		{"full by the argument", true, 0, "[true]", "1", "The Java build finished.", false},
		{"incremental by the argument", false, 0, "[false]", "1", "The Java build finished.", false},
		{"it failed", true, 0, "[true]", "0", "The Java build failed.", false},
		{"it has errors", true, 0, "[true]", "2", "The Java build finished with errors; they are in the Problems view.", false},
		{"it was cancelled", true, 0, "[true]", "3", "The Java build was cancelled.", false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			r := newRig(t, rigOptions{javaVersion: "21"})
			if _, err := r.run(); err != nil {
				t.Fatal(err)
			}
			r.answer(map[string]string{buildWorkspaceMethod: tt.answer})
			if err := r.api.Run(CompileWorkspaceCommand, tt.arg); err != nil {
				t.Fatal(err)
			}
			r.settle()
			if tt.pickAsk {
				pick := r.api.LastPick()
				if pick == nil || strings.Join(pick.Labels(), ",") != "Incremental,Full" {
					t.Fatalf("pick = %+v", pick)
				}
				pick.Choose(tt.choose)
				r.settle()
			} else if r.api.LastPick() != nil {
				t.Error("the command asked although it was told")
			}
			if got := r.requests(buildWorkspaceMethod); len(got) != 1 || got[0] != tt.params {
				t.Errorf("requests = %v, want %s", got, tt.params)
			}
			if r.note() != tt.want {
				t.Errorf("note = %q, want %q", r.note(), tt.want)
			}
		})
	}
	t.Run("a pick that is dismissed builds nothing", func(t *testing.T) {
		r := newRig(t, rigOptions{javaVersion: "21"})
		if _, err := r.run(); err != nil {
			t.Fatal(err)
		}
		if err := r.api.Run(CompileWorkspaceCommand, nil); err != nil {
			t.Fatal(err)
		}
		r.api.LastPick().Close()
		r.settle()
		if got := r.requests(buildWorkspaceMethod); len(got) != 0 {
			t.Errorf("requests = %v", got)
		}
	})
	t.Run("the server fails", func(t *testing.T) {
		r := newRig(t, rigOptions{javaVersion: "21"})
		if _, err := r.run(); err != nil {
			t.Fatal(err)
		}
		r.api.OnServerRequest(ServerID, func(fakeapi.ServerRequest) (json.RawMessage, error) { return nil, errors.New("TestBoom", "boom") })
		if err := r.api.Run(CompileWorkspaceCommand, true); err != nil {
			t.Fatal(err)
		}
		r.settle()
		if !strings.Contains(r.note(), "Force Java Compilation: the language server failed") {
			t.Errorf("note = %q", r.note())
		}
	})
	t.Run("the server does not run", func(t *testing.T) {
		r := newRig(t, rigOptions{javaVersion: "21"})
		if err := r.api.Run(CompileWorkspaceCommand, true); err != nil {
			t.Fatal(err)
		}
		r.settle()
		if !strings.Contains(r.note(), "needs jdtls, which is not running yet (open a Java file)") || len(r.requests(buildWorkspaceMethod)) != 0 {
			t.Errorf("note = %q", r.note())
		}
	})
}

func TestUpdateProjectConfigurationAsksTheServerAboutTheProjectOfTheFile(t *testing.T) {
	t.Run("the file in front", func(t *testing.T) {
		r := newRig(t, rigOptions{javaVersion: "21"})
		if _, err := r.run(); err != nil {
			t.Fatal(err)
		}
		uri := r.editor("src/main/java/A.java", "java", "class A {}\n")
		if err := r.api.Run(UpdateProjectConfigurationCommand, nil); err != nil {
			t.Fatal(err)
		}
		r.settle()
		if got := r.requests(projectConfigUpdateMethod); len(got) != 1 || got[0] != `{"uri":"`+uri+`"}` {
			t.Errorf("requests = %v", got)
		}
		if !strings.Contains(r.note(), "A.java is being updated") {
			t.Errorf("note = %q", r.note())
		}
	})
	t.Run("a build file given as the argument", func(t *testing.T) {
		r := newRig(t, rigOptions{javaVersion: "21"})
		if _, err := r.run(); err != nil {
			t.Fatal(err)
		}
		uri := "file://" + filepath.Join(r.folder, "pom.xml")
		if err := r.api.Run(UpdateProjectConfigurationCommand, uri); err != nil {
			t.Fatal(err)
		}
		r.settle()
		if got := r.requests(projectConfigUpdateMethod); len(got) != 1 || got[0] != `{"uri":"`+uri+`"}` {
			t.Errorf("requests = %v", got)
		}
	})
	t.Run("nothing in front", func(t *testing.T) {
		r := newRig(t, rigOptions{javaVersion: "21"})
		if _, err := r.run(); err != nil {
			t.Fatal(err)
		}
		if err := r.api.Run(UpdateProjectConfigurationCommand, nil); err != nil {
			t.Fatal(err)
		}
		r.settle()
		if !strings.Contains(r.note(), "needs a Java file or a build file") || len(r.requests(projectConfigUpdateMethod)) != 0 {
			t.Errorf("note = %q", r.note())
		}
	})
	t.Run("a document that is no file", func(t *testing.T) {
		r := newRig(t, rigOptions{javaVersion: "21"})
		if _, err := r.run(); err != nil {
			t.Fatal(err)
		}
		if err := r.api.Run(UpdateProjectConfigurationCommand, "emetos-virtual:/jdt/contents/x/Y.class"); err != nil {
			t.Fatal(err)
		}
		r.settle()
		if !strings.Contains(r.note(), "is not one") || len(r.requests(projectConfigUpdateMethod)) != 0 {
			t.Errorf("note = %q", r.note())
		}
	})
	t.Run("the server is not running", func(t *testing.T) {
		r := newRig(t, rigOptions{javaVersion: "21"})
		r.editor("A.java", "java", "class A {}\n")
		if err := r.api.Run(UpdateProjectConfigurationCommand, nil); err != nil {
			t.Fatal(err)
		}
		r.settle()
		if !strings.Contains(r.note(), "needs jdtls") {
			t.Errorf("note = %q", r.note())
		}
	})
	t.Run("the server fails", func(t *testing.T) {
		r := newRig(t, rigOptions{javaVersion: "21"})
		if _, err := r.run(); err != nil {
			t.Fatal(err)
		}
		r.editor("A.java", "java", "class A {}\n")
		r.api.FailNotifications(ServerID, errors.New("TestBoom", "boom"))
		if err := r.api.Run(UpdateProjectConfigurationCommand, nil); err != nil {
			t.Fatal(err)
		}
		r.settle()
		if !strings.Contains(r.note(), "Update Project Configuration: the language server failed") {
			t.Errorf("note = %q", r.note())
		}
	})
}

func TestOrganizeImportsAsksForTheSourceActionOfTheFileInFront(t *testing.T) {
	t.Run("it applies the action", func(t *testing.T) {
		r := newRig(t, rigOptions{javaVersion: "21"})
		if _, err := r.run(); err != nil {
			t.Fatal(err)
		}
		uri := r.editor("A.java", "java", "import b.B;\nimport a.A;\n", extension.Selection{
			Anchor: extension.Position{Line: 0, Col: 2}, Active: extension.Position{Line: 1, Col: 3},
		})
		r.api.OnCodeAction(func(fakeapi.CodeActionCall) (string, error) { return "Organize imports", nil })
		if err := r.api.Run(OrganizeImportsCommand, nil); err != nil {
			t.Fatal(err)
		}
		r.settle()
		calls := r.api.CodeActionCalls()
		if len(calls) != 1 || calls[0].URI != uri || len(calls[0].Req.Only) != 1 || calls[0].Req.Only[0] != "source.organizeImports" {
			t.Fatalf("calls = %+v", calls)
		}
		if rng := calls[0].Req.Range; rng.Start.Col != 2 || rng.End.Line != 1 || rng.End.Col != 3 {
			t.Errorf("range = %+v: the selection", rng)
		}
		if len(r.notes()) != 0 {
			t.Errorf("notes = %v", r.notes())
		}
	})
	t.Run("nothing to organize", func(t *testing.T) {
		r := newRig(t, rigOptions{javaVersion: "21"})
		if _, err := r.run(); err != nil {
			t.Fatal(err)
		}
		r.editor("A.java", "java", "class A {}\n")
		if err := r.api.Run(OrganizeImportsCommand, nil); err != nil {
			t.Fatal(err)
		}
		r.settle()
		if r.note() != "Organize Imports: nothing to change in A.java." {
			t.Errorf("note = %q", r.note())
		}
	})
	t.Run("the server fails", func(t *testing.T) {
		r := newRig(t, rigOptions{javaVersion: "21"})
		if _, err := r.run(); err != nil {
			t.Fatal(err)
		}
		r.editor("A.java", "java", "class A {}\n")
		r.api.OnCodeAction(func(fakeapi.CodeActionCall) (string, error) { return "", errors.New("TestBoom", "boom") })
		if err := r.api.Run(OrganizeImportsCommand, nil); err != nil {
			t.Fatal(err)
		}
		r.settle()
		if !strings.Contains(r.note(), "the language server failed: boom") {
			t.Errorf("note = %q", r.note())
		}
	})
	t.Run("not a Java file", func(t *testing.T) {
		r := newRig(t, rigOptions{javaVersion: "21"})
		if _, err := r.run(); err != nil {
			t.Fatal(err)
		}
		r.editor("a.txt", "plaintext", "x\n")
		if err := r.api.Run(OrganizeImportsCommand, nil); err != nil {
			t.Fatal(err)
		}
		r.settle()
		if !strings.Contains(r.note(), "needs a Java file") || len(r.api.CodeActionCalls()) != 0 {
			t.Errorf("note = %q", r.note())
		}
	})
	t.Run("the server is not running", func(t *testing.T) {
		r := newRig(t, rigOptions{javaVersion: "21"})
		r.editor("A.java", "java", "class A {}\n")
		if err := r.api.Run(OrganizeImportsCommand, nil); err != nil {
			t.Fatal(err)
		}
		r.settle()
		if !strings.Contains(r.note(), "needs jdtls") || len(r.api.CodeActionCalls()) != 0 {
			t.Errorf("note = %q", r.note())
		}
	})
}

func TestTheStateWordsNameEveryStateOfTheServer(t *testing.T) {
	tests := map[extension.ServerState]string{
		extension.ServerStarting: "starting",
		extension.ServerMissing:  "not available",
		extension.ServerFailed:   "stopped after crashing",
		extension.ServerStopped:  "stopped",
		extension.ServerIdle:     "not running yet",
	}
	for state, want := range tests {
		if got := stateWord(state); !strings.Contains(got, want) {
			t.Errorf("stateWord(%v) = %q, want it to say %q", state, got, want)
		}
	}
}
