package snippet

import (
	"slices"
	"testing"

	"github.com/rm4n0s/emetos/internal/core/autoclose"
	"github.com/rm4n0s/emetos/internal/core/buffer"
	"github.com/rm4n0s/emetos/internal/core/document"
	"github.com/rm4n0s/emetos/internal/core/edit"
	"github.com/rm4n0s/emetos/internal/core/language"
	"github.com/rm4n0s/errors"
)

// ctlRig is a controller on a rig, which records the states it was told.
type ctlRig struct {
	*rig
	c      *Controller
	states []State
	chosen []string
}

func newCtlRig(t *testing.T, text string, sels ...buffer.Selection) *ctlRig {
	t.Helper()
	r := &ctlRig{rig: newRig(t, text, sels...)}
	r.c = NewController(r.ed, func() Env { return r.env }, func(s State) { r.states = append(r.states, s) })
	r.c.OnChoice(func(choice *Choice, replace buffer.Range) {
		r.chosen = append(r.chosen, r.doc.Snapshot().Text(replace)+"|"+choice.Options[0].Value)
	})
	// the editor tells the controller about every move of the cursors
	r.cur.SetOnChange(r.c.SelectionsChanged)
	return r
}

func (r *ctlRig) insert(template string) {
	r.t.Helper()
	must(r.t, r.c.Insert(template, DefaultInsertOptions()))
}

func TestControllerState(t *testing.T) {
	r := newCtlRig(t, "", sel(1, 1, 1, 1))
	r.insert("${1:a} ${2:b} ${3:c}$0")
	if got := r.c.State(); got != (State{InSnippet: true, HasNext: true}) {
		t.Fatalf("state %+v", got)
	}
	must(t, r.c.Next())
	if got := r.c.State(); got != (State{InSnippet: true, HasNext: true, HasPrev: true}) {
		t.Errorf("state %+v", got)
	}
	must(t, r.c.Next())
	must(t, r.c.Prev())
	if got := r.c.State(); !got.InSnippet || !got.HasPrev || !got.HasNext {
		t.Errorf("state %+v", got)
	}
	r.assertSelections(sel(1, 3, 1, 4))
	// the final tab stop ends the session
	must(t, r.c.Next())
	must(t, r.c.Next())
	if r.c.IsInSnippet() || r.c.State() != (State{}) {
		t.Errorf("state %+v at the final tab stop", r.c.State())
	}
	r.assertSelections(sel(1, 6, 1, 6))
	// every change was told, and the last is the end
	if last := r.states[len(r.states)-1]; last != (State{}) {
		t.Errorf("last state told: %+v", last)
	}
}

func TestControllerPlainTextIsNoSession(t *testing.T) {
	r := newCtlRig(t, "", sel(1, 1, 1, 1))
	r.insert("just text$0")
	if r.c.IsInSnippet() || len(r.states) != 0 {
		t.Errorf("state %+v, told %v", r.c.State(), r.states)
	}
	if r.value() != "just text" {
		t.Errorf("text %q", r.value())
	}
}

func TestControllerCursorLeavingEndsTheSession(t *testing.T) {
	for _, tc := range []struct {
		name string
		to   buffer.Selection
	}{
		{"between the placeholders", sel(1, 3, 1, 3)},
		{"after the snippet", sel(1, 9, 1, 9)},
	} {
		t.Run(tc.name, func(t *testing.T) {
			r := newCtlRig(t, "", sel(1, 1, 1, 1))
			r.insert("${1:a}--${2:b}$0") // $1 is 1-2, the dashes 2-4 and $2 is 4-5
			if !r.c.IsInSnippet() {
				t.Fatal("not in the snippet")
			}
			r.setSelections(tc.to)
			if r.c.IsInSnippet() {
				t.Error("still in the snippet with the cursor outside the placeholders")
			}
			if last := r.states[len(r.states)-1]; last != (State{}) {
				t.Errorf("the end was not told: %+v", last)
			}
		})
	}
}

func TestControllerCursorInsideThePlaceholderStays(t *testing.T) {
	r := newCtlRig(t, "", sel(1, 1, 1, 1))
	r.insert("${1:abcd} ${2:b}$0")
	r.setSelections(sel(1, 2, 1, 3)) // inside $1
	if !r.c.IsInSnippet() {
		t.Error("left the snippet by moving inside the placeholder")
	}
}

func TestControllerEscapeCollapsesTheCursors(t *testing.T) {
	r := newCtlRig(t, sessionText)
	r.insert("${1:a}$0")
	if r.cur.Len() != 2 {
		t.Fatalf("%d cursors", r.cur.Len())
	}
	r.c.Cancel(true)
	if r.c.IsInSnippet() || r.cur.Len() != 1 {
		t.Errorf("in snippet %v, %d cursors", r.c.IsInSnippet(), r.cur.Len())
	}
}

func TestControllerNestedInsert(t *testing.T) {
	r := newCtlRig(t, "", sel(1, 1, 1, 1))
	r.insert("This ${1:is ${2:nested}}.$0")
	must(t, r.c.Next())
	r.insert("really ${1:nested}$0")
	if !r.c.IsInSnippet() {
		t.Fatal("the nested snippet ended the session")
	}
	r.assertSelections(sel(1, 16, 1, 22))
	if r.value() != "This is really nested." {
		t.Errorf("text %q", r.value())
	}
}

func TestControllerUndoToTheStartEndsTheSession(t *testing.T) {
	r := newCtlRig(t, "", sel(1, 1, 1, 1))
	r.insert("${1:a} ${2:b}$0")
	if !r.c.IsInSnippet() {
		t.Fatal("not in the snippet")
	}
	res, err := r.doc.Undo()
	must(t, err)
	r.setSelections(res.Selections...)
	r.c.SelectionsChanged()
	if r.c.IsInSnippet() || r.value() != "" {
		t.Errorf("in snippet %v, text %q", r.c.IsInSnippet(), r.value())
	}
}

func TestControllerSnippetIsOneUndoStep(t *testing.T) {
	r := newCtlRig(t, "x", sel(1, 2, 1, 2))
	r.insert("${1:a} ${2:b}$0")
	if r.value() != "xa b" {
		t.Fatalf("text %q", r.value())
	}
	r.typeText("Q") // an edit after the snippet
	_, err := r.doc.Undo()
	must(t, err)
	if r.value() != "xa b" {
		t.Errorf("one undo went past the typing: %q", r.value())
	}
	_, err = r.doc.Undo()
	must(t, err)
	if r.value() != "x" {
		t.Errorf("the second undo did not take the snippet away: %q", r.value())
	}
}

func TestControllerReplacingTheDocumentEndsTheSession(t *testing.T) {
	r := newCtlRig(t, "", sel(1, 1, 1, 1))
	r.insert("${1:a} ${2:b}$0")
	r.doc.SetText("other")
	if r.c.IsInSnippet() {
		t.Error("still in the snippet")
	}
}

func TestControllerChoice(t *testing.T) {
	r := newCtlRig(t, "", sel(1, 1, 1, 1))
	r.insert("x ${1|one,two,three|} ${2:y}$0")
	if len(r.chosen) != 1 || r.chosen[0] != "one|one" {
		t.Fatalf("choice told: %v", r.chosen)
	}
	if !r.c.State().InChoice {
		t.Error("not in a choice")
	}
	choice, rg, ok := r.c.ActiveChoice()
	if !ok || len(choice.Options) != 3 || rg != rng(1, 3, 1, 6) {
		t.Errorf("active choice %v %v %v", choice, rg, ok)
	}
	must(t, r.c.Next())
	if r.c.State().InChoice {
		t.Error("still in a choice after next")
	}
	if len(r.chosen) != 1 {
		t.Errorf("choice told again: %v", r.chosen)
	}
	if _, _, ok := r.c.ActiveChoice(); ok {
		t.Error("an active choice at $2")
	}
	must(t, r.c.Prev())
	if len(r.chosen) != 2 {
		t.Errorf("the choice was not told again when it came back: %v", r.chosen)
	}
}

func TestControllerFinish(t *testing.T) {
	r := newCtlRig(t, "", sel(1, 1, 1, 1))
	r.insert("${1:a} ${2:b}$0")
	must(t, r.c.Finish())
	if r.c.IsInSnippet() {
		t.Error("still in the snippet")
	}
	r.assertSelections(sel(1, 4, 1, 4))
}

func TestControllerApplyEndsTheRunningSession(t *testing.T) {
	r := newCtlRig(t, "foo", sel(1, 4, 1, 4))
	r.insert("${1:a} ${2:b}$0")
	r2 := r.value()
	if !r.c.IsInSnippet() {
		t.Fatal("not in the snippet")
	}
	must(t, r.c.Apply([]Edit{{Range: rng(1, 1, 1, 4), Template: "${1:X}!"}}, DefaultInsertOptions()))
	if r.value() == r2 || !r.c.IsInSnippet() {
		t.Errorf("text %q, in snippet %v", r.value(), r.c.IsInSnippet())
	}
}

func TestControllerErrorsEndTheSession(t *testing.T) {
	r := newCtlRig(t, "abc", sel(1, 1, 1, 1))
	r.doc.SetReadOnly(true)
	err := r.c.Insert("${1:a}$0", DefaultInsertOptions())
	appErr, ok := errors.FromError(err)
	if !ok || !appErr.HasRoute("DocumentReadOnly") {
		t.Errorf("error %v", err)
	}
	if r.c.IsInSnippet() {
		t.Error("in a snippet after an error")
	}
	// the controller is usable again
	r.doc.SetReadOnly(false)
	must(t, r.c.Insert("${1:a}$0", DefaultInsertOptions()))
	if !r.c.IsInSnippet() {
		t.Error("cannot insert after the error")
	}
}

func TestControllerMethodsWithoutASession(t *testing.T) {
	r := newCtlRig(t, "abc", sel(1, 1, 1, 1))
	must(t, r.c.Next())
	must(t, r.c.Prev())
	must(t, r.c.Finish())
	r.c.Cancel(true)
	if _, ok := r.c.EnclosingRange(); ok {
		t.Error("an enclosing range without a session")
	}
	r.insert("${1:a} ${2:b}$0")
	if rg, ok := r.c.EnclosingRange(); !ok || rg != rng(1, 4, 1, 4).Union(rng(1, 1, 1, 4)) {
		t.Errorf("enclosing range %v %v", rg, ok)
	}
}

func TestControllerJoinUndoMakesTheInsertionPartOfTheOpenGroup(t *testing.T) {
	tests := []struct {
		name  string
		join  bool
		undos int
	}{
		{"joined", true, 1},
		{"apart", false, 2},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			r := newCtlRig(t, "x", sel(1, 2, 1, 2))
			r.doc.PushStackElement()
			_, err := r.doc.Edit([]document.Edit{{Range: rng(1, 1, 1, 1), Text: "import\n"}}, document.EditOptions{Join: true})
			must(t, err)
			r.cur.SetSelections([]buffer.Selection{sel(2, 2, 2, 2)})
			opts := DefaultInsertOptions()
			opts.JoinUndo, opts.UndoStopBefore, opts.UndoStopAfter = tt.join, false, false
			must(t, r.c.Insert("${1:a}$0", opts))
			steps := 0
			for r.doc.CanUndo() {
				_, err := r.doc.Undo()
				must(t, err)
				steps++
			}
			if steps != tt.undos || r.value() != "x" {
				t.Errorf("undone in %d steps to %q, want %d steps to x", steps, r.value(), tt.undos)
			}
		})
	}
}

// TestControllerTheFinalTabStopOnlyEndsTheSessionAtOnce: a snippet whose only tab stop is the final one has no session
// (VS Code's SnippetController2 _updateState: isAtLastPlaceholder), no decoration is left to grow with the typing that follows,
// and the cursor stays where $0 put it.
//
// Ported from VS Code: snippetController2.test.ts ('insert, insert plain text -> no snippet mode', 'insert, insert -> tab, tab,
// done' after the last tab stop)
func TestControllerTheFinalTabStopOnlyEndsTheSessionAtOnce(t *testing.T) {
	tests := []struct {
		name, template, typed, want string
	}{
		{"in parentheses", "Sprintf($0)", "abc\ndef", "Sprintf(abc\ndef)"},
		{"alone", "$0", "abc", "abc"},
		{"between texts", "a$0b", "x", "axb"},
		{"with a default", "f(${0:x})", "y", "f(y)"},
		{"with no tab stop", "plain()", "y", "plain()y"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			r := newCtlRig(t, "", sel(1, 1, 1, 1))
			r.insert(tt.template)
			if r.c.IsInSnippet() || r.c.State() != (State{}) {
				t.Fatalf("state %+v after the insertion", r.c.State())
			}
			whole := rng(1, 1, 99, 1)
			if left := r.doc.DecorationsIn(whole, DecorationOwner); len(left) != 0 {
				t.Errorf("%d decorations of the tab stops are left", len(left))
			}
			r.typeText(tt.typed)
			if r.value() != tt.want {
				t.Errorf("text %q, want %q", r.value(), tt.want)
			}
			if r.c.IsInSnippet() {
				t.Error("the session came back")
			}
			if left := r.doc.DecorationsIn(whole, DecorationOwner); len(left) != 0 {
				t.Errorf("%d decorations of the tab stops after typing", len(left))
			}
		})
	}
}

// TestEditorTellsTheClosingCharactersOfASnippet: an inserted snippet that ends with a pair tells the editor which closing
// character the user may type over, only when the cursor stays inside (VS Code's executeEdits with the source "snippet").
func TestEditorTellsTheClosingCharactersOfASnippet(t *testing.T) {
	tests := []struct {
		name, text, template string
		sels                 []buffer.Selection
		want                 []edit.AutoClosed
	}{
		{"a call", "fmt.", "Sprintf($0)", []buffer.Selection{sel(1, 5, 1, 5)}, []edit.AutoClosed{{
			Character: rng(1, 13, 1, 14), Enclosing: rng(1, 12, 1, 14)}}},
		{"a call with a tab stop", "fmt.", "Sprint(${1:})", []buffer.Selection{sel(1, 5, 1, 5)}, []edit.AutoClosed{{
			Character: rng(1, 12, 1, 13), Enclosing: rng(1, 11, 1, 13)}}},
		{"a call at every cursor", "a.\nb.", "f($0)", []buffer.Selection{sel(1, 3, 1, 3), sel(2, 3, 2, 3)}, []edit.AutoClosed{
			{Character: rng(1, 5, 1, 6), Enclosing: rng(1, 4, 1, 6)}, {Character: rng(2, 5, 2, 6), Enclosing: rng(2, 4, 2, 6)}}},
		{"a text with no pair", "fmt.", "Println", []buffer.Selection{sel(1, 5, 1, 5)}, nil},
		{"a snippet of lines", "", "if x {\n\t$0\n}", []buffer.Selection{sel(1, 1, 1, 1)}, nil},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			r := newCtlRig(t, tt.text, tt.sels...)
			var got []edit.AutoClosed
			r.ed.AutoClosing = func() *autoclose.Language {
				return autoclose.NewLanguage(&language.Configuration{Brackets: []language.Pair{{Open: "(", Close: ")"}, {Open: "{", Close: "}"}}})
			}
			r.ed.OnAutoClosed = func(c []edit.AutoClosed) { got = append(got, c...) }
			c := NewController(r.ed, func() Env { return r.env }, nil)
			must(t, c.Insert(tt.template, DefaultInsertOptions()))
			if !slices.Equal(got, tt.want) {
				t.Errorf("told %v, want %v", got, tt.want)
			}
		})
	}
}
