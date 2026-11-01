package python

import (
	"encoding/json"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/rm4n0s/emetos/extension"
	"github.com/rm4n0s/emetos/extensions/internal/langserver"
)

// The interpreter of a scope: the status item, the choice kept in the state, the picker, the entry of a path and the offer of the
// folder's own environment.

// chosenState is the interpreter the tab kept in the state, "" for none.
func (r *rig) chosenState() string {
	r.t.Helper()
	raw, ok := r.api.State().Get(StateKey)
	if !ok {
		return ""
	}
	var s string
	if err := json.Unmarshal(raw, &s); err != nil {
		r.t.Fatalf("the state holds %s", raw)
	}
	return s
}

// withEnv is a rig with a folder that has a .venv (3.12.1) and a tools environment elsewhere.
func withEnv(t *testing.T, o rigOptions) (*rig, string) {
	t.Helper()
	r := newRig(t, o)
	py := fakeEnv(t, filepath.Join(r.folder, ".venv"), "3.12.1", "")
	return r, py
}

func TestTheStatusItemShowsTheInterpreterAndWhyItWasChosen(t *testing.T) {
	t.Run("the first python3 on PATH", func(t *testing.T) {
		r := newRig(t, rigOptions{servers: []string{"basedpyright"}})
		r.editor("a.py", "python", "")
		if _, err := r.run(); err != nil {
			t.Fatal(err)
		}
		it := r.item()
		if it.Text != "$(check) Python 3.11.2" {
			t.Errorf("text = %q", it.Text)
		}
		if !strings.Contains(it.Tooltip, "Interpreter: "+filepath.Join(r.bin, "python3")+" (the first python3 on PATH)") {
			t.Errorf("tooltip:\n%s", it.Tooltip)
		}
	})
	t.Run("the environment the user chose", func(t *testing.T) {
		r, py := withEnv(t, rigOptions{servers: []string{"basedpyright"}, state: ""})
		r.api.SetState(StateKey, py)
		r.ext.interps.Refresh()
		r.editor("a.py", "python", "")
		if _, err := r.run(); err != nil {
			t.Fatal(err)
		}
		if it := r.item(); it.Text != "$(check) Python 3.12.1 ('.venv')" || !strings.Contains(it.Tooltip, "(chosen by you)") {
			t.Errorf("item = %q\n%s", it.Text, it.Tooltip)
		}
	})
	t.Run("no interpreter", func(t *testing.T) {
		r := newRig(t, rigOptions{servers: []string{"basedpyright"}, noPython: true})
		r.editor("a.py", "python", "")
		if _, err := r.run(); err != nil {
			t.Fatal(err)
		}
		if it := r.item(); it.Text != "$(check) Select Interpreter" || !strings.Contains(it.Tooltip, "No Python interpreter was found") {
			t.Errorf("item = %q\n%s", it.Text, it.Tooltip)
		}
	})
	t.Run("a missing server still shows the interpreter", func(t *testing.T) {
		r := newRig(t, rigOptions{})
		r.editor("a.py", "python", "")
		if _, err := r.run(); err == nil {
			t.Fatal("the server started")
		}
		if it := r.item(); it.Text != "$(warning) Python 3.11.2: basedpyright not found" {
			t.Errorf("item = %q", it.Text)
		}
	})
	t.Run("the setting changes it", func(t *testing.T) {
		r := newRig(t, rigOptions{servers: []string{"basedpyright"}})
		other := fakeEnv(t, filepath.Join(r.base, "envs", "ml"), "3.10.4", "")
		r.api.SetSetting(DefaultInterpreterSetting, other)
		r.settle()
		r.editor("a.py", "python", "")
		if it := r.item(); !strings.Contains(it.Text, "Python 3.10.4 ('ml')") || !strings.Contains(it.Tooltip, "(python.defaultInterpreterPath)") {
			t.Errorf("item = %q\n%s", it.Text, it.Tooltip)
		}
	})
}

func TestAnotherSettingDoesNotLookForTheInterpreterAgain(t *testing.T) {
	r := newRig(t, rigOptions{servers: []string{"basedpyright"}})
	// a program that records every run, as the interpreter in use
	log := filepath.Join(r.base, "runs")
	writeFile(t, r.bin, "python3", "#!/bin/sh\necho run >> '"+log+"'\necho 'Python 3.11.2'\n", 0o755)
	r.ext.interps.Refresh()
	r.settle()
	runs := func() int {
		data, _ := os.ReadFile(log)
		return strings.Count(string(data), "run")
	}
	before := runs()
	if before == 0 {
		t.Fatal("the interpreter was not asked for its version")
	}
	r.api.SetSetting("basedpyright.analysis.typeCheckingMode", "strict")
	r.api.SetSetting("editor.tabSize", 2)
	r.settle()
	if got := runs(); got != before {
		t.Errorf("the interpreter was run %d times more after settings that do not decide it", got-before)
	}
	r.api.SetSetting(DefaultInterpreterSetting, filepath.Join(r.bin, "python3"))
	r.settle()
	if got := runs(); got != before+1 {
		t.Errorf("the interpreter was run %d times more after python.defaultInterpreterPath changed, want 1", got-before)
	}
}

func TestChoosingAnInterpreterKeepsItRestartsTheServerAndFollowsTheChoice(t *testing.T) {
	r, py := withEnv(t, rigOptions{servers: []string{"basedpyright"}})
	r.editor("a.py", "python", "")
	var seen []string
	r.ext.interps.OnChange(func(i Interpreter, ok bool) {
		if ok {
			seen = append(seen, i.Version)
		} else {
			seen = append(seen, "none")
		}
	})
	if cur, ok := r.ext.interps.Current(); !ok || cur.Version != "3.11.2" {
		t.Fatalf("Current = %+v, %v", cur, ok)
	}
	if err := r.api.Run(SetInterpreterCommand, py); err != nil {
		t.Fatal(err)
	}
	r.settle()
	if got := r.chosenState(); got != py {
		t.Errorf("state = %q, want %q", got, py)
	}
	if got := r.api.ServerRestarts(ServerID); got != 1 {
		t.Errorf("restarts = %d", got)
	}
	if cur, ok := r.ext.interps.Current(); !ok || cur.Path != py || cur.Version != "3.12.1" || cur.Name != ".venv" || cur.Kind != KindWorkspace {
		t.Errorf("Current = %+v, %v", cur, ok)
	}
	if !slices.Equal(seen, []string{"3.12.1"}) {
		t.Errorf("OnChange told %v", seen)
	}
	// the environment is given to the server that the host starts next
	l, err := r.run()
	if err != nil {
		t.Fatal(err)
	}
	if got := configuration(t, l, "python"); got != `{"pythonPath":"`+py+`"}` {
		t.Errorf("python = %s", got)
	}
	// choosing the same one again changes nothing the subscribers see
	if err := r.api.Run(SetInterpreterCommand, py); err != nil {
		t.Fatal(err)
	}
	r.settle()
	if !slices.Equal(seen, []string{"3.12.1"}) {
		t.Errorf("OnChange told %v after the same choice", seen)
	}
	// forgetting the choice goes back to the rules
	if err := r.ext.interps.Choose(""); err != nil {
		t.Fatal(err)
	}
	r.settle()
	if r.chosenState() != "" || !slices.Equal(seen, []string{"3.12.1", "3.11.2"}) {
		t.Errorf("state %q, OnChange told %v", r.chosenState(), seen)
	}
}

func TestAChoiceThatIsNotAProgramIsRefusedAndKeptNowhere(t *testing.T) {
	r := newRig(t, rigOptions{servers: []string{"basedpyright"}})
	err := r.ext.interps.Choose("/nowhere/bin/python")
	wantRoute(t, err, "Normalize.PythonInterpreterInvalid")
	if r.chosenState() != "" || r.api.ServerRestarts(ServerID) != 0 {
		t.Errorf("state %q, restarts %d", r.chosenState(), r.api.ServerRestarts(ServerID))
	}
	// by the command, the user is told
	if err := r.api.Run(SetInterpreterCommand, "/nowhere/bin/python"); err != nil {
		t.Fatal(err)
	}
	if n := r.notes(); len(n) != 1 || n[0] != "The interpreter cannot be chosen: /nowhere/bin/python does not exist" {
		t.Errorf("notes = %q", n)
	}
	// and a state that cannot be written is told too
	py := fakeEnv(t, filepath.Join(r.folder, ".venv"), "3.12.1", "")
	r.api.FailState(testErr("the disk is full"))
	if err := r.api.Run(SetInterpreterCommand, py); err != nil {
		t.Fatal(err)
	}
	if n := r.notes(); len(n) != 2 || !strings.Contains(n[1], "the disk is full") {
		t.Errorf("notes = %q", n)
	}
}

func TestAClientOfAHostedWorkspaceCannotChooseTheHostsInterpreter(t *testing.T) {
	r, py := withEnv(t, rigOptions{servers: []string{"basedpyright"}})
	client := extension.Origin(3)
	r.api.RunAs(client, func() {
		if err := r.api.Run(SetInterpreterCommand, py); err != nil {
			t.Error(err)
		}
		if err := r.api.Run(SetInterpreterCommand, nil); err != nil {
			t.Error(err)
		}
	})
	r.settle()
	if r.chosenState() != "" || len(r.api.Picks()) != 0 {
		t.Errorf("state %q, picks %d", r.chosenState(), len(r.api.Picks()))
	}
	if n := r.notes(); len(n) != 2 || !strings.Contains(n[0], "choice of the host") {
		t.Errorf("notes = %q", n)
	}
}

func TestThePickerListsTheInterpretersByWhereTheyAreFrom(t *testing.T) {
	r, py := withEnv(t, rigOptions{servers: []string{"basedpyright"}})
	tools := fakeEnv(t, filepath.Join(r.base, "envs", "tools"), "3.10.4", "")
	t.Setenv("VIRTUAL_ENV", filepath.Join(r.base, "envs", "tools"))
	fakePython(t, filepath.Join(r.home, ".pyenv", "versions", "3.9.18", "bin"), "python", "3.9.18")
	r.ext.interps.Refresh() // the active environment is the one in use
	r.settle()
	r.editor("a.py", "python", "")
	if err := r.api.Run(SetInterpreterCommand, nil); err != nil {
		t.Fatal(err)
	}
	r.settle()
	pick := r.api.LastPick()
	if pick == nil {
		t.Fatal("no list was shown")
	}
	opts := pick.Options()
	var rows []string
	for _, it := range opts.Items {
		line := it.Label
		if it.Separator {
			line = "-- " + it.Label
		} else if it.Description != "" {
			line += " | " + it.Description
		}
		rows = append(rows, line)
	}
	want := []string{
		"$(add) Enter interpreter path... | a python program, or the folder of an environment",
		"-- Environment of this folder",
		"$(star-full) Python 3.12.1 ('.venv') | .venv/bin/python",
		"-- Active environment (VIRTUAL_ENV)",
		"$(check) Python 3.10.4 ('tools') | " + tools, // in use: VIRTUAL_ENV
		"-- pyenv",
		"$(python) Python 3.9.18 (pyenv) | ~/.pyenv/versions/3.9.18/bin/python",
		"-- Global (PATH)",
		"$(python) Python 3.11.2 | " + filepath.Join(r.bin, "python3"),
	}
	if !slices.Equal(rows, want) {
		t.Errorf("rows:\n  %s\nwant:\n  %s", strings.Join(rows, "\n  "), strings.Join(want, "\n  "))
	}
	if opts.Title != "Select Interpreter" || !opts.MatchOnDescription {
		t.Errorf("options = %+v", opts)
	}
	// choosing the environment of the folder
	pick.Choose(2)
	r.settle()
	if got := r.chosenState(); got != py {
		t.Errorf("state = %q, want %q", got, py)
	}
	// the next time the way to forget the choice is there, and the choice is marked
	if err := r.api.Run(SetInterpreterCommand, nil); err != nil {
		t.Fatal(err)
	}
	r.settle()
	next := r.api.LastPick().Labels()
	if next[1] != "$(discard) Use the default interpreter" || !slices.Contains(next, "$(check) Python 3.12.1 ('.venv')") {
		t.Errorf("labels = %q", next)
	}
	r.api.LastPick().Choose(1)
	r.settle()
	if r.chosenState() != "" {
		t.Errorf("the choice was not forgotten: %q", r.chosenState())
	}
	// closing the list chooses nothing
	if err := r.api.Run(SetInterpreterCommand, nil); err != nil {
		t.Fatal(err)
	}
	r.settle()
	notes := len(r.notes()) // the offer of the folder's environment, made when the Python file came to the front
	r.api.LastPick().Close()
	if r.chosenState() != "" || len(r.notes()) != notes {
		t.Errorf("state %q, notes %q", r.chosenState(), r.notes())
	}
}

func TestThePickerWithNoInterpreterAsksForAPath(t *testing.T) {
	r := newRig(t, rigOptions{servers: []string{"basedpyright"}, noPython: true})
	if err := r.api.Run(SetInterpreterCommand, nil); err != nil {
		t.Fatal(err)
	}
	r.settle()
	pick := r.api.LastPick()
	if pick == nil {
		t.Fatal("no list")
	}
	if pick.Options().Placeholder != "No Python interpreter was found: enter the path of one" || len(pick.Options().Items) != 1 {
		t.Errorf("options = %+v", pick.Options())
	}
	pick.Choose(0)
	in := r.api.LastInput()
	if in == nil {
		t.Fatal("no input box")
	}
	elsewhere := fakeEnv(t, filepath.Join(r.base, "elsewhere"), "3.8.5", "")
	for text, want := range map[string]string{
		"": "Enter the path of a python program, or of an environment's folder", "/nowhere": "/nowhere does not exist",
		elsewhere: "", filepath.Join(r.base, "elsewhere"): "",
	} {
		if got := in.Validate(text); got != want {
			t.Errorf("Validate(%q) = %q, want %q", text, got, want)
		}
	}
	in.Accept(filepath.Join(r.base, "elsewhere"))
	r.settle()
	if got := r.chosenState(); got != elsewhere {
		t.Errorf("state = %q, want %q", got, elsewhere)
	}
	if it := r.api.StatusItem(langserver.StatusID(ServerID)); it == nil || it.Text != "Python 3.8.5 ('elsewhere')" && !strings.Contains(it.Text, "Python 3.8.5 ('elsewhere')") {
		t.Errorf("item = %+v", it)
	}
	// Escape chooses nothing
	r.ext.interps.enterPath()
	r.api.LastInput().Cancel()
	if got := r.chosenState(); got != elsewhere {
		t.Errorf("state = %q", got)
	}
}

func TestTheFoldersOwnEnvironmentIsOfferedOnceAndUsedOnlyWhenAccepted(t *testing.T) {
	t.Run("accepted", func(t *testing.T) {
		r, py := withEnv(t, rigOptions{servers: []string{"basedpyright"}})
		r.editor("README.md", "markdown", "")
		if len(r.api.Notes()) != 0 {
			t.Fatalf("offered for a file that is not Python: %q", r.notes())
		}
		r.editor("a.py", "python", "")
		notes := r.api.Notes()
		if len(notes) != 1 || !strings.HasPrefix(notes[0].Message, "This folder has a Python environment, .venv.") {
			t.Fatalf("notes = %+v", notes)
		}
		if !slices.Equal(notes[0].Actions, []string{"Use .venv", "Select Another..."}) {
			t.Errorf("buttons = %v", notes[0].Actions)
		}
		if r.chosenState() != "" {
			t.Fatal("the environment was taken before the user said so")
		}
		l, err := r.run()
		if err != nil {
			t.Fatal(err)
		}
		if got := configuration(t, l, "python"); strings.Contains(got, ".venv") {
			t.Errorf("the server got the folder's environment before the user accepted: %s", got)
		}
		r.api.RunNoteAction(0, "Use .venv")
		r.settle()
		if got := r.chosenState(); got != py {
			t.Errorf("state = %q, want %q", got, py)
		}
		// once: another Python file asks nothing
		r.editor("b.py", "python", "")
		if len(r.api.Notes()) != 1 {
			t.Errorf("notes = %q", r.notes())
		}
	})
	t.Run("select another", func(t *testing.T) {
		r, _ := withEnv(t, rigOptions{servers: []string{"basedpyright"}})
		r.editor("a.py", "python", "")
		r.api.RunNoteAction(0, "Select Another...")
		r.settle()
		if r.api.LastPick() == nil || r.chosenState() != "" {
			t.Errorf("pick %v, state %q", r.api.LastPick(), r.chosenState())
		}
	})
	t.Run("a choice that was made is not asked again", func(t *testing.T) {
		r, py := withEnv(t, rigOptions{servers: []string{"basedpyright"}})
		r.api.SetState(StateKey, py)
		r.editor("a.py", "python", "")
		if len(r.api.Notes()) != 0 {
			t.Errorf("notes = %q", r.notes())
		}
	})
	t.Run("a folder without an environment", func(t *testing.T) {
		r := newRig(t, rigOptions{servers: []string{"basedpyright"}})
		r.editor("a.py", "python", "")
		if len(r.api.Notes()) != 0 {
			t.Errorf("notes = %q", r.notes())
		}
	})
	t.Run("the environment is the active one already", func(t *testing.T) {
		r := newRig(t, rigOptions{servers: []string{"basedpyright"}})
		fakeEnv(t, filepath.Join(r.folder, ".venv"), "3.12.1", "")
		t.Setenv("VIRTUAL_ENV", filepath.Join(r.folder, ".venv"))
		r.editor("a.py", "python", "")
		if len(r.api.Notes()) != 0 {
			t.Errorf("notes = %q", r.notes())
		}
	})
}

// testErr is an error with a message.
type testErr string

func (e testErr) Error() string { return string(e) }
