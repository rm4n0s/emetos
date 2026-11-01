package csharp

import (
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/rm4n0s/errors"

	"github.com/rm4n0s/emetos/extension"
)

// "C#: Select Solution": the picker, the choice kept in the state, the restart, and the refusals.

// chosenState is the file kept in the state, "" when none.
func (r *rig) chosenState() string {
	v, _ := extension.StateValue[string](r.api.State(), StateKey)
	return v
}

// solutionRig is a tab whose folder has three solutions and a project, with the server running.
func solutionRig(t *testing.T) *rig {
	t.Helper()
	r := newRig(t, rigOptions{files: map[string]string{"A.sln": "", "B.sln": "", "tools/T.sln": "", "App/App.csproj": ""}})
	if _, err := r.run(); err != nil {
		t.Fatal(err)
	}
	return r
}

func TestThePickerListsTheSolutionsThenTheProjects(t *testing.T) {
	r := solutionRig(t)
	if err := r.api.Run(SelectSolutionCommand, nil); err != nil {
		t.Fatal(err)
	}
	r.settle()
	pick := r.api.LastPick()
	if pick == nil {
		t.Fatal("no list was shown")
	}
	want := []string{"$(root-folder) A.sln", "$(root-folder) B.sln", "$(root-folder) T.sln", "$(project) App.csproj"}
	if got := pick.Labels(); !slices.Equal(got, want) {
		t.Errorf("labels = %v, want %v", got, want)
	}
	var descriptions []string
	for _, it := range pick.Options().Items {
		descriptions = append(descriptions, it.Description)
	}
	if !slices.Equal(descriptions, []string{".", ".", "tools", "App"}) {
		t.Errorf("descriptions = %v: the folder of each, relative to the folder of the tab", descriptions)
	}
	if pick.Options().Title != "Select Solution" {
		t.Errorf("title = %q", pick.Options().Title)
	}
}

func TestChoosingASolutionKeepsItAndRestartsTheServer(t *testing.T) {
	r := solutionRig(t)
	if err := r.api.Run(SelectSolutionCommand, nil); err != nil {
		t.Fatal(err)
	}
	r.settle()
	r.api.LastPick().Choose(2) // tools/T.sln
	r.settle()
	want := filepath.Join(r.folder, "tools", "T.sln")
	if got := r.chosenState(); got != want {
		t.Errorf("state = %q, want %q", got, want)
	}
	if got := r.api.ServerRestarts(ServerID); got != 1 {
		t.Errorf("restarts = %d", got)
	}
	// the next start loads it, from its folder
	l, err := r.run()
	if err != nil {
		t.Fatal(err)
	}
	if l.Root != filepath.Join(r.folder, "tools") || !slices.Equal(l.Args, []string{"--solution", "T.sln"}) {
		t.Errorf("root %s args %v", l.Root, l.Args)
	}
	if tip := r.item().Tooltip; !strings.Contains(tip, "Loads tools/T.sln (chosen with Select Solution)") {
		t.Errorf("tooltip = %q", tip)
	}
	// the list marks the solution in use and offers to forget the choice
	if err := r.api.Run(SelectSolutionCommand, nil); err != nil {
		t.Fatal(err)
	}
	r.settle()
	labels := r.api.LastPick().Labels()
	if labels[0] != "$(discard) Use the automatic choice" || !slices.Contains(labels, "$(check) T.sln") {
		t.Errorf("labels = %v", labels)
	}
	r.api.LastPick().Choose(0)
	r.settle()
	if got := r.chosenState(); got != "" {
		t.Errorf("state after forgetting = %q", got)
	}
	if got := r.api.ServerRestarts(ServerID); got != 2 {
		t.Errorf("restarts = %d", got)
	}
}

func TestAProjectCanBeChosen(t *testing.T) {
	r := solutionRig(t)
	if err := r.api.Run(SelectSolutionCommand, nil); err != nil {
		t.Fatal(err)
	}
	r.settle()
	r.api.LastPick().Choose(3)
	r.settle()
	l, err := r.run()
	if err != nil {
		t.Fatal(err)
	}
	if l.Root != filepath.Join(r.folder, "App") || len(l.Args) != 0 {
		t.Errorf("root %s args %v: a project is loaded from its folder", l.Root, l.Args)
	}
}

func TestTheCommandTakesAFileAsItsArgument(t *testing.T) {
	r := solutionRig(t)
	if err := r.api.Run(SelectSolutionCommand, "B.sln"); err != nil { // relative to the folder
		t.Fatal(err)
	}
	r.settle()
	if got := r.chosenState(); got != filepath.Join(r.folder, "B.sln") {
		t.Errorf("state = %q", got)
	}
	if len(r.api.Picks()) != 0 {
		t.Error("a picker was shown")
	}
	if err := r.api.Run(SelectSolutionCommand, filepath.Join(r.folder, "A.sln")); err != nil {
		t.Fatal(err)
	}
	if got := r.chosenState(); got != filepath.Join(r.folder, "A.sln") {
		t.Errorf("state = %q", got)
	}
	// a file that is not a solution is said
	writeFile(t, r.folder, "notes.txt", "", 0o644)
	if err := r.api.Run(SelectSolutionCommand, "notes.txt"); err != nil {
		t.Fatal(err)
	}
	r.settle()
	if n := r.notes(); len(n) == 0 || !strings.Contains(n[len(n)-1], "The solution cannot be chosen: ") || !strings.Contains(n[len(n)-1], "is not a .sln, .slnx or .csproj file") {
		t.Errorf("notes = %q", n)
	}
	if got := r.chosenState(); got != filepath.Join(r.folder, "A.sln") {
		t.Errorf("state = %q: a wrong file changed the choice", got)
	}
}

func TestAChoiceThatFailsIsSaidAndKeepsNothing(t *testing.T) {
	tests := []struct {
		name  string
		file  string
		fail  error
		route string
		msg   string
	}{
		{"a file that is not there", "Gone.sln", nil, "choose.CSharpSolutionNotLoadable", "no such file"},
		{"a folder", "tools", nil, "choose.CSharpSolutionNotLoadable", "is not a .sln, .slnx or .csproj file"},
		{"a file that is no solution", "tools/T.txt", nil, "choose.CSharpSolutionNotLoadable", "is not a .sln, .slnx or .csproj file"},
		{"a state that cannot be written", "A.sln", errors.New("TestDiskFull", "the disk is full"), "TestDiskFull", "the disk is full"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			r := solutionRig(t)
			writeFile(t, r.folder, "tools/T.txt", "", 0o644)
			if tt.fail != nil {
				r.api.FailState(tt.fail)
			}
			err := r.ext.choose(filepath.Join(r.folder, tt.file))
			wantRoute(t, err, tt.route)
			if !strings.Contains(err.Error(), tt.msg) {
				t.Errorf("error = %v, want it to say %q", err, tt.msg)
			}
			if got := r.api.ServerRestarts(ServerID); got != 0 {
				t.Errorf("restarts = %d", got)
			}
		})
	}
}

func TestAnUnwritableStateIsToldWhenForgettingToo(t *testing.T) {
	r := solutionRig(t)
	r.api.SetState(StateKey, filepath.Join(r.folder, "A.sln"))
	r.api.FailState(errors.New("TestDiskFull", "the disk is full"))
	if err := r.api.Run(SelectSolutionCommand, nil); err != nil {
		t.Fatal(err)
	}
	r.settle()
	r.api.LastPick().Choose(0)
	r.settle()
	if n := r.notes(); len(n) == 0 || !strings.Contains(n[len(n)-1], "The choice cannot be forgotten: the disk is full") {
		t.Errorf("notes = %q", n)
	}
}

func TestAClientOfAHostedWorkspaceCannotChooseTheHostsSolution(t *testing.T) {
	r := solutionRig(t)
	client := extension.Origin(3)
	r.api.RunAs(client, func() {
		if err := r.api.Run(SelectSolutionCommand, nil); err != nil {
			t.Error(err)
		}
		if err := r.api.Run(SelectSolutionCommand, "B.sln"); err != nil {
			t.Error(err)
		}
	})
	r.settle()
	if r.chosenState() != "" || len(r.api.Picks()) != 0 {
		t.Errorf("state %q, picks %d", r.chosenState(), len(r.api.Picks()))
	}
	// the question about the two solutions came first
	if n := r.notes(); len(n) != 3 || !strings.Contains(n[1], "choice of the host") || !strings.Contains(n[2], "choice of the host") {
		t.Errorf("notes = %q", n)
	}
}

func TestSelectSolutionNeedsAFolder(t *testing.T) {
	r := newRig(t, rigOptions{noFolder: true})
	if err := r.api.Run(SelectSolutionCommand, nil); err != nil {
		t.Fatal(err)
	}
	r.settle()
	if n := r.notes(); len(n) != 1 || !strings.Contains(n[0], "needs a folder") || len(r.api.Picks()) != 0 {
		t.Errorf("notes = %q, picks %d", n, len(r.api.Picks()))
	}
}

func TestAFolderWithNothingToChooseSaysSo(t *testing.T) {
	r := newRig(t, rigOptions{files: map[string]string{"a.cs": ""}})
	if err := r.api.Run(SelectSolutionCommand, nil); err != nil {
		t.Fatal(err)
	}
	r.settle()
	if pick := r.api.LastPick(); pick != nil {
		t.Fatalf("a list of %v was shown for a folder with nothing to choose", pick.Labels())
	}
	if n := r.notes(); len(n) != 1 || !strings.Contains(n[0], "No solution (.sln, .slnx) or project (.csproj) was found") {
		t.Errorf("notes = %q", n)
	}
}

func TestSeveralSolutionsAreOfferedOnceAndAChoiceEndsTheOffer(t *testing.T) {
	r := newRig(t, rigOptions{files: map[string]string{"A.sln": "", "B.sln": ""}})
	if _, err := r.run(); err != nil {
		t.Fatal(err)
	}
	notes := r.api.Notes()
	if len(notes) != 1 || !strings.Contains(notes[0].Message, "This folder has several solutions (A.sln, B.sln)") || !slices.Equal(notes[0].Actions, []string{"Select Solution..."}) {
		t.Fatalf("notes = %+v", notes)
	}
	// a second start says nothing more
	if _, err := r.run(); err != nil {
		t.Fatal(err)
	}
	if len(r.api.Notes()) != 1 {
		t.Errorf("notes = %q", r.notes())
	}
	// the button opens the picker
	r.api.RunNoteAction(0, "Select Solution...")
	r.settle()
	if r.api.LastPick() == nil {
		t.Error("the button showed no list")
	}
	// with a choice kept from an earlier session there is nothing to offer
	r2 := newRig(t, rigOptions{files: map[string]string{"A.sln": "", "B.sln": ""}, state: map[string]any{StateKey: ""}})
	r2.api.SetState(StateKey, filepath.Join(r2.folder, "B.sln"))
	if _, err := r2.run(); err != nil {
		t.Fatal(err)
	}
	if len(r2.api.Notes()) != 0 {
		t.Errorf("notes = %q", r2.notes())
	}
}

func TestTheMenuOfTheItemHasSelectSolution(t *testing.T) {
	cfg := New().(*csharp).serverConfig()
	found := false
	for _, e := range cfg.Menu {
		found = found || strings.Contains(e.Label, "Select Solution")
	}
	if !found {
		t.Errorf("menu = %+v", cfg.Menu)
	}
}

func TestEveryCommandOfThePackHasAHandlerAndATitle(t *testing.T) {
	r := newRig(t, rigOptions{})
	for _, spec := range r.ext.Manifest().Contributes.Commands {
		if !r.api.HasHandler(spec.ID) || spec.Title == "" || spec.Description == "" {
			t.Errorf("command %+v: no handler, title or description", spec)
		}
	}
}
