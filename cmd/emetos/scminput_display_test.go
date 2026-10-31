//go:build emetos_display

package main

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/rm4n0s/emetos/internal/gui/app"
	"github.com/rm4n0s/emetos/internal/testenv/display"
)

// The commit message box of the Source Control view as a user meets it (step 24.4, SPEC §10.30, §15.1): a click gives it the
// keyboard, the keys type into it, Ctrl+Enter runs the accept command with the text, Escape and the arrows act as in VS Code. The
// source control is the fake provider of dev.scm.fake (the Git extension comes in step 24.5).

// commitBoxFixture is the fixture of the fake provider: one repository with the box, a button and a staged file.
const commitBoxFixture = `{"sourceControls": [{"id": "git", "label": "Git", "root": ".",
 "status": [{"text": "$(git-branch) main", "command": "dev.scm.checkout"}],
 "validation": {"subjectLength": 20, "level": "warning"},
 "actionButton": {"title": "$(check) Commit", "secondary": [[{"id": "dev.scm.commitAmend", "title": "Commit (Amend)"}]]},
 "groups": [{"id": "index", "label": "Staged Changes", "resources": [{"path": "main.go", "letter": "M",
   "color": "gitDecoration.stageModifiedResourceForeground"}]}]}]}`

// boxState is what dev.scm.input answers.
type boxState struct {
	Value   string `json:"value"`
	Focused bool   `json:"focused"`
	Runs    []struct {
		Command string `json:"command"`
		Input   string `json:"input"`
	} `json:"runs"`
}

func readBox(t *testing.T, a *display.App) boxState {
	t.Helper()
	resp := a.Dev("dev.scm.input", nil)
	var st boxState
	if err := json.Unmarshal(resp.Result, &st); err != nil {
		t.Fatalf("dev.scm.input: %v (%s)", err, resp.Result)
	}
	return st
}

// commitBox is the text field of the box: the one in the side bar, the one at the top left of the window that holds the repository.
func commitBox(a *display.App) (app.DevInput, bool) {
	for _, in := range a.Inputs() {
		if in.X < 350 && in.Y > 60 && in.Width > 150 && in.Height >= 12 {
			return in, true
		}
	}
	return app.DevInput{}, false
}

func TestRealInputSCMCommitBox(t *testing.T) {
	a := display.Start(t, display.Options{Args: []string{".", "main.go"}, Project: map[string]string{"main.go": "package main\n", "box.json": commitBoxFixture}})
	a.Settle()
	a.Dev("dev.scm.fake", map[string]any{"fixture": "box.json"})
	a.Press("ctrl+shift+g")
	a.Eventually("the box to have the keyboard when the view opens", func() (bool, string) {
		in, ok := a.Focused()
		return ok && in.Text == "" && in.X < 350, a.Dump()
	})

	// the keyboard goes to the editor, and a click on the box gives it back
	a.Press("ctrl+1")
	a.Eventually("the editor to have the keyboard", func() (bool, string) {
		_, in := a.Focused()
		return !in, a.FocusedText()
	})
	box, ok := commitBox(a)
	if !ok {
		t.Fatalf("no commit box on screen: %+v\n%s", a.Inputs(), a.Dump())
	}
	a.ClickInput(box)
	a.Eventually("the click to give the box the keyboard", func() (bool, string) {
		in, ok := a.Focused()
		return ok && in.X < 350, a.FocusedText()
	})

	// typing: spaces and capitals; a long subject makes the validation message appear under the box
	const message = "Fix the cache key of the loader"
	a.Type(message)
	a.WaitFocusedText(message)
	a.WaitText("The subject has 31 characters; 20 or fewer are recommended.")
	if got := readBox(t, a).Value; got != message {
		t.Errorf("the model holds %q after typing", got)
	}
	// Enter makes a new line; Ctrl+Enter accepts and does not
	a.Press("Enter")
	a.Type("The key ignored the scheme.")
	a.WaitFocusedText(message + "\nThe key ignored the scheme.")
	if st := readBox(t, a); len(st.Runs) != 0 {
		t.Fatalf("Enter ran %+v", st.Runs)
	}
	a.Settle()
	shot(t, a, "scm-commit-box")

	// Ctrl+Enter runs the accept command once, with the whole text; the provider empties the box
	a.Press("ctrl+Enter")
	a.Eventually("the accept command to run", func() (bool, string) {
		st := readBox(t, a)
		return len(st.Runs) == 1, a.Dump()
	})
	st := readBox(t, a)
	if st.Runs[0].Command != "dev.scm.commit" || st.Runs[0].Input != message+"\nThe key ignored the scheme." {
		t.Errorf("the accept command ran as %+v", st.Runs[0])
	}
	a.Eventually("the box to be empty", func() (bool, string) {
		in, ok := a.Focused()
		return ok && in.Text == "", a.FocusedText()
	})

	// Up brings the message back (the box is empty: the caret is at its start), Escape clears the message of the validation first
	a.Press("up")
	a.WaitFocusedText(message + "\nThe key ignored the scheme.")
	a.Press("alt+down")
	a.Eventually("the box to be empty again", func() (bool, string) {
		in, ok := a.Focused()
		return ok && in.Text == "", a.FocusedText()
	})
	a.Type("a subject that is long enough to warn")
	a.WaitText("The subject has 37 characters; 20 or fewer are recommended.")
	a.Press("Escape")
	a.WaitNoText("The subject has 37 characters; 20 or fewer are recommended.")
	if in, _ := a.Focused(); in.Text != "a subject that is long enough to warn" {
		t.Errorf("the first Escape changed the text: %q", in.Text)
	}
	a.Press("Escape")
	a.Eventually("the second Escape to clear the box", func() (bool, string) {
		in, ok := a.Focused()
		return ok && in.Text == "", a.FocusedText()
	})

	// the text of the model with letters that are not on the keyboard layout of the test (the virtual keyboard types the US
	// layout only) goes through the accept command whole
	a.Dev("dev.scm.input", map[string]any{"text": "Corrige la caché: ñandú ü"})
	a.Press("ctrl+Enter")
	a.Eventually("the second commit", func() (bool, string) {
		return len(readBox(t, a).Runs) == 2, a.Dump()
	})
	if got := readBox(t, a).Runs[1].Input; !strings.Contains(got, "ñandú") {
		t.Errorf("the second commit ran with %q", got)
	}

	// a click on the button runs the same command
	a.Dev("dev.scm.input", map[string]any{"text": "from the button"})
	a.ClickText("Commit")
	a.Eventually("the button to run the command", func() (bool, string) {
		return len(readBox(t, a).Runs) == 3, a.Dump()
	})
	if got := readBox(t, a).Runs[2].Input; got != "from the button" {
		t.Errorf("the button ran with %q", got)
	}
}
