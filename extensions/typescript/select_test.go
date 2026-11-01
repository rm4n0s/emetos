package typescript

import (
	"encoding/json"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/rm4n0s/errors"

	"github.com/rm4n0s/emetos/extension"
)

// The choice of the TypeScript: the offer of the folder's own, the picker and the command.

// chosenState is the choice kept in the state, "" when there is none.
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

func TestTheFoldersOwnTypeScriptIsOfferedOnceAndNeverTaken(t *testing.T) {
	r := newRig(t, rigOptions{globalTS: "6.0.3", workspaceTS: "5.9.3"})
	if _, err := r.run(); err != nil {
		t.Fatal(err)
	}
	r.editor("a.ts", "typescript", "")
	notes := r.api.Notes()
	if len(notes) != 1 || notes[0].Severity != "info" {
		t.Fatalf("notes = %+v", notes)
	}
	for _, want := range []string{"This folder has its own TypeScript, 5.9.3.", "Emetos then runs the TypeScript code in node_modules/typescript/lib", "came with the folder"} {
		if !strings.Contains(notes[0].Message, want) {
			t.Errorf("the offer lacks %q: %s", want, notes[0].Message)
		}
	}
	if !slices.Equal(notes[0].Actions, []string{"Use Workspace Version", "Select Another..."}) {
		t.Errorf("actions = %v", notes[0].Actions)
	}
	// nothing is chosen, so the one beside the server still runs
	if r.chosenState() != "" || r.item().Text != "$(check) TypeScript 6.0.3" {
		t.Errorf("state %q, item %q", r.chosenState(), r.item().Text)
	}
	// offered once in an activation, however many files come to the front
	r.editor("b.js", "javascript", "")
	r.editor("c.tsx", "typescriptreact", "")
	if len(r.api.Notes()) != 1 {
		t.Errorf("notes = %q", r.notes())
	}

	// accepting keeps the choice and restarts the server, which then runs the folder's
	restarts := r.api.ServerRestarts(ServerID)
	r.api.RunNoteAction(0, "Use Workspace Version")
	r.settle()
	if r.chosenState() != ChoiceWorkspace || r.api.ServerRestarts(ServerID) != restarts+1 {
		t.Errorf("state %q, restarts %d", r.chosenState(), r.api.ServerRestarts(ServerID))
	}
	l, err := r.run()
	if err != nil {
		t.Fatal(err)
	}
	if want := filepath.Join(r.folder, "node_modules", "typescript", "lib", "tsserver.js"); !strings.Contains(launchOptions(t, l), `"path":"`+want+`"`) {
		t.Errorf("options = %s", launchOptions(t, l))
	}
	if it := r.item(); it.Text != "$(check) TypeScript 5.9.3 (workspace)" || !strings.Contains(it.Tooltip, "(workspace)") {
		t.Errorf("item = %q\n%s", it.Text, it.Tooltip)
	}
}

func TestTheOfferIsLeftOutWhenThereIsNothingToOffer(t *testing.T) {
	tests := []struct {
		name     string
		opts     rigOptions
		language string
		before   func(r *rig)
	}{
		{"the setting is off", rigOptions{globalTS: "6.0.3", workspaceTS: "5.9.3", settings: map[string]any{PromptWorkspaceVersionSetting: false}}, "typescript", nil},
		{"the folder has none", rigOptions{globalTS: "6.0.3"}, "typescript", nil},
		{"the user chose the global one", rigOptions{globalTS: "6.0.3", workspaceTS: "5.9.3", state: ChoiceGlobal}, "typescript", nil},
		{"the user chose the folder's", rigOptions{globalTS: "6.0.3", workspaceTS: "5.9.3", state: ChoiceWorkspace}, "typescript", nil},
		{"the file is not one the server serves", rigOptions{globalTS: "6.0.3", workspaceTS: "5.9.3"}, "go", nil},
		{"the folder's is the one in use", rigOptions{globalTS: "6.0.3", workspaceTS: "5.9.3"}, "typescript", func(r *rig) {
			r.api.SetSetting(TsdkSetting, filepath.Join(r.folder, "node_modules", "typescript", "lib"))
		}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			r := newRig(t, tt.opts)
			if tt.before != nil {
				tt.before(r)
			}
			if _, err := r.run(); err != nil {
				t.Fatal(err)
			}
			r.editor("a.ts", tt.language, "")
			if n := r.notes(); len(n) != 0 {
				t.Errorf("notes = %q", n)
			}
		})
	}
}

func TestTheOfferIsMadeWhenNoOtherTypeScriptCanBeFound(t *testing.T) {
	r := newRig(t, rigOptions{workspaceTS: "5.9.3"})
	_, err := r.run()
	wantRoute(t, err, "Decide.TypeScriptNotFound")
	r.editor("a.ts", "typescript", "")
	// the start that failed is told, with Show Output, and the offer comes after it
	n := r.api.Notes()
	if len(n) != 2 || !strings.Contains(n[0].Message, "no TypeScript was found beside the language server") || !strings.Contains(n[1].Message, "This folder has its own TypeScript, 5.9.3.") {
		t.Fatalf("notes = %+v", n)
	}
	// the start failed: the item does not show a version that did not run
	if it := r.item(); strings.Contains(it.Text, "5.9.3") || !strings.Contains(it.Text, "cannot start") {
		t.Errorf("item = %q", it.Text)
	}
	r.api.RunNoteAction(1, "Use Workspace Version")
	r.settle()
	if l, err := r.run(); err != nil || !strings.Contains(launchOptions(t, l), "node_modules/typescript/lib/tsserver.js") {
		t.Errorf("launch %s, %v", launchOptions(t, l), err)
	}
}

func TestSelectTypeScriptVersionListsTheVersionsAndKeepsTheChoice(t *testing.T) {
	r := newRig(t, rigOptions{globalTS: "6.0.3", workspaceTS: "5.9.3"})
	if _, err := r.run(); err != nil {
		t.Fatal(err)
	}
	r.editor("a.ts", "typescript", "")
	if err := r.api.Run(SelectVersionCommand, nil); err != nil {
		t.Fatal(err)
	}
	pick := r.api.LastPick()
	if pick == nil {
		t.Fatal("no list was shown")
	}
	want := []string{"$(check) Use Global Version (TypeScript 6.0.3)", "$(star-full) Use Workspace Version (TypeScript 5.9.3 (workspace))"}
	if got := pick.Labels(); !slices.Equal(got, want) {
		t.Errorf("rows = %q, want %q", got, want)
	}
	if got := pick.Options().Items[1].Description; got != "node_modules/typescript/lib" {
		t.Errorf("description = %q", got)
	}
	restarts := r.api.ServerRestarts(ServerID)
	pick.Choose(1)
	r.settle()
	if r.chosenState() != ChoiceWorkspace || r.api.ServerRestarts(ServerID) != restarts+1 {
		t.Errorf("state %q, restarts %d", r.chosenState(), r.api.ServerRestarts(ServerID))
	}

	// the server runs the folder's now: its row has the check, and the choice can be forgotten
	if _, err := r.run(); err != nil {
		t.Fatal(err)
	}
	if err := r.api.Run(SelectVersionCommand, nil); err != nil {
		t.Fatal(err)
	}
	pick = r.api.LastPick()
	want = []string{"$(versions) Use Global Version (TypeScript 6.0.3)", "$(check) Use Workspace Version (TypeScript 5.9.3 (workspace))", "$(discard) Forget the choice"}
	if got := pick.Labels(); !slices.Equal(got, want) {
		t.Errorf("rows = %q, want %q", got, want)
	}
	pick.Choose(2)
	r.settle()
	if r.chosenState() != "" || r.api.ServerRestarts(ServerID) != restarts+2 {
		t.Errorf("state %q, restarts %d", r.chosenState(), r.api.ServerRestarts(ServerID))
	}
}

func TestSelectTypeScriptVersionWithAChoiceAsItsArgument(t *testing.T) {
	r := newRig(t, rigOptions{globalTS: "6.0.3", workspaceTS: "5.9.3"})
	if err := r.api.Run(SelectVersionCommand, ChoiceWorkspace); err != nil {
		t.Fatal(err)
	}
	if r.chosenState() != ChoiceWorkspace || len(r.api.Picks()) != 0 {
		t.Errorf("state %q, picks %d", r.chosenState(), len(r.api.Picks()))
	}
	if err := r.api.Run(SelectVersionCommand, "nightly"); err != nil {
		t.Fatal(err)
	}
	if r.chosenState() != ChoiceWorkspace || !strings.Contains(r.note(), "nightly is neither global nor workspace") {
		t.Errorf("state %q, note %q", r.chosenState(), r.note())
	}
	// a state that cannot be written is told
	r.api.FailState(errors.New("TestFailure", "the disk is full"))
	if err := r.api.Run(SelectVersionCommand, ChoiceGlobal); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(r.note(), "The TypeScript version cannot be chosen: the disk is full") {
		t.Errorf("note = %q", r.note())
	}
}

func TestAClientOfAHostedWorkspaceCannotChooseTheHostsTypeScript(t *testing.T) {
	r := newRig(t, rigOptions{globalTS: "6.0.3", workspaceTS: "5.9.3"})
	r.api.RunAs(extension.Origin(3), func() {
		if err := r.api.Run(SelectVersionCommand, ChoiceWorkspace); err != nil {
			t.Error(err)
		}
		if err := r.api.Run(SelectVersionCommand, nil); err != nil {
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

func TestSelectTypeScriptVersionWhenThereIsNoTypeScript(t *testing.T) {
	r := newRig(t, rigOptions{})
	if err := r.api.Run(SelectVersionCommand, nil); err != nil {
		t.Fatal(err)
	}
	if len(r.api.Picks()) != 0 || !strings.Contains(r.note(), "No TypeScript was found") || !strings.Contains(r.note(), "npm install -g typescript@6") {
		t.Errorf("picks %d, note %q", len(r.api.Picks()), r.note())
	}
}

func TestTheVersionTheServerReportsIsTheOneShown(t *testing.T) {
	r := newRig(t, rigOptions{globalTS: "6.0.3"})
	if _, err := r.run(); err != nil {
		t.Fatal(err)
	}
	r.editor("a.ts", "typescript", "")
	r.api.ServerNotify(ServerID, versionMethod, map[string]string{"version": "6.0.4", "source": "user-setting"})
	r.settle()
	if got := r.item().Text; got != "$(check) TypeScript 6.0.4" {
		t.Errorf("item = %q", got)
	}
	// garbage is ignored, and a restart forgets what the last start reported
	r.api.ServerNotify(ServerID, versionMethod, "not an object")
	r.settle()
	if got := r.item().Text; got != "$(check) TypeScript 6.0.4" {
		t.Errorf("item = %q", got)
	}
	if _, err := r.run(); err != nil {
		t.Fatal(err)
	}
	if got := r.item().Text; got != "$(check) TypeScript 6.0.3" {
		t.Errorf("item = %q", got)
	}
}
