package app

import (
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/rm4n0s/errors"
	"github.com/rm4n0s/glutter/gluttertest"
	"github.com/rm4n0s/glutter/widgets"

	"github.com/rm4n0s/emetos/extension"
	"github.com/rm4n0s/emetos/internal/core/editorgroup"
	"github.com/rm4n0s/emetos/internal/gui/diffeditor"
	"github.com/rm4n0s/emetos/internal/gui/kit"
)

// The proposed changes of extensions in the window (step 13.3): the diff editor with Accept and Reject in the title.

// proposedRun is a window with the proposed change of the file main.go open.
type proposedRun struct {
	*stateRun
	file    string
	answers []extension.ProposedResult
	closeFn func()
}

// proposeChange shows a proposed change of main.go with the host the window gives the extensions.
func proposeChange(t *testing.T, original, proposed string) *proposedRun {
	t.Helper()
	env := newStateEnv(t, nil)
	run := env.start(env.root)
	file := filepath.Join(env.root, "main.go")
	if err := os.WriteFile(file, []byte(original), 0o644); err != nil {
		t.Fatal(err)
	}
	p := &proposedRun{stateRun: run, file: file}
	closeFn, err := extEditorHost{w: run.win}.OpenProposed(extension.ProposedChange{
		URI: "file://" + file, Original: original, Proposed: proposed, Title: "main.go (proposed)",
	}, func(r extension.ProposedResult) { p.answers = append(p.answers, r) })
	if err != nil {
		t.Fatal(err)
	}
	p.closeFn = closeFn
	run.pump()
	p.waitDiffReady()
	return p
}

// pane is the diff pane of the proposal.
func (p *proposedRun) pane() *diffPane {
	panes := p.shell().diffPanes()
	if len(panes) == 0 {
		return nil
	}
	return panes[0]
}

func (p *proposedRun) waitDiffReady() {
	p.t().Helper()
	p.waitFor("the diff of the proposal", func() bool { return p.pane() != nil && p.pane().Model().Computed() && !p.pane().Model().Computing() })
}

func (p *proposedRun) t() *testing.T { return p.env.t }

// button finds the button of the title with a label.
func buttonLabelled(label string) gluttertest.Finder {
	return gluttertest.ByPredicate("the button "+label, func(w widgets.Widget) bool {
		b, ok := w.(*kit.Button)
		return ok && b != nil && b.Label() == label
	})
}

// tap presses a button of the title.
func (p *proposedRun) tap(label string) {
	p.t().Helper()
	if !p.ts.Exists(buttonLabelled(label)) {
		p.t().Fatalf("the title has no %q button", label)
	}
	p.ts.Tap(buttonLabelled(label))
	p.pump()
}

// tabs are the names of the editors of the active group.
func (p *proposedRun) tabs() []string {
	var out []string
	for _, e := range p.shell().activeGroup().Model.Editors(editorgroup.Sequential, false) {
		out = append(out, e.Name())
	}
	return out
}

func TestAProposedChangeOpensAsADiffWithAcceptAndRejectInTheTitle(t *testing.T) {
	p := proposeChange(t, "a\nb\nc\n", "a\nB\nc\n")
	if got := p.tabs(); !slices.Equal(got, []string{"main.go (proposed)"}) {
		t.Fatalf("the tabs are %q", got)
	}
	if groups := p.shell().group.grid.Groups(editorgroup.GridAppearance); len(groups) != 2 {
		t.Errorf("the change opened in %d groups, want it beside the active one (2)", len(groups))
	}
	m := p.pane().Model()
	if got := m.View(diffeditor.Original).Document().Text(); got != "a\nb\nc\n" {
		t.Errorf("the left side is %q", got)
	}
	if got := m.View(diffeditor.Modified).Document().Text(); got != "a\nB\nc\n" {
		t.Errorf("the right side is %q", got)
	}
	if !m.View(diffeditor.Original).ReadOnly() || m.View(diffeditor.Modified).ReadOnly() {
		t.Error("the left side must be read-only and the right side editable")
	}
	if len(m.Changes()) != 1 {
		t.Errorf("%d changes", len(m.Changes()))
	}
	for _, label := range []string{"Accept", "Reject"} {
		if !p.ts.Exists(buttonLabelled(label)) {
			t.Errorf("the title has no %s button", label)
		}
	}
	if len(p.answers) != 0 {
		t.Error("the proposal was answered before the user did")
	}
}

func TestAcceptAnswersWithTheTextOfTheRightSideAndClosesTheTab(t *testing.T) {
	tests := []struct {
		name string
		edit string // typed at the start of the right side
		want extension.ProposedResult
	}{
		{"as proposed", "", extension.ProposedResult{Accepted: true, Text: "a\nB\nc\n"}},
		{"after the user edited the text", "X", extension.ProposedResult{Accepted: true, Text: "Xa\nB\nc\n", Edited: true}},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			p := proposeChange(t, "a\nb\nc\n", "a\nB\nc\n")
			if tc.edit != "" {
				if err := p.pane().Model().View(diffeditor.Modified).RunCommand("type", map[string]any{"text": tc.edit}); err != nil {
					t.Fatal(err)
				}
				p.pump()
			}
			p.tap("Accept")
			if len(p.answers) != 1 || p.answers[0] != tc.want {
				t.Fatalf("the answers are %+v, want [%+v]", p.answers, tc.want)
			}
			if len(p.tabs()) != 0 || p.win.Dialogs.IsShowing() {
				t.Errorf("after Accept: tabs %q, dialog %v", p.tabs(), p.win.Dialogs.IsShowing())
			}
			if got, _ := os.ReadFile(p.file); string(got) != "a\nb\nc\n" {
				t.Errorf("Emetos wrote the file: %q", got)
			}
		})
	}
}

func TestRejectAndClosingAnswerARejectionAndAskNothing(t *testing.T) {
	tests := []struct {
		name string
		do   func(p *proposedRun)
	}{
		{"the Reject button", func(p *proposedRun) { p.tap("Reject") }},
		{"the close command", func(p *proposedRun) { p.command("workbench.action.closeActiveEditor", nil) }},
		{"the close command after an edit", func(p *proposedRun) {
			if err := p.pane().Model().View(diffeditor.Modified).RunCommand("type", map[string]any{"text": "X"}); err != nil {
				p.t().Fatal(err)
			}
			p.pump()
			p.command("workbench.action.closeActiveEditor", nil)
		}},
		{"closing all the editors", func(p *proposedRun) { p.command("workbench.action.closeAllEditors", nil) }},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			p := proposeChange(t, "a\nb\nc\n", "a\nB\nc\n")
			tc.do(p)
			if len(p.answers) != 1 || p.answers[0] != (extension.ProposedResult{}) {
				t.Fatalf("the answers are %+v, want one rejection", p.answers)
			}
			if len(p.tabs()) != 0 {
				t.Errorf("the tab is still open: %q", p.tabs())
			}
			if p.win.Dialogs.IsShowing() {
				t.Error("closing a proposed change asked about unsaved changes")
			}
			if got, _ := os.ReadFile(p.file); string(got) != "a\nb\nc\n" {
				t.Errorf("the file changed: %q", got)
			}
		})
	}
}

func TestTheFunctionThatClosesAProposedChangeGivesNoAnswer(t *testing.T) {
	p := proposeChange(t, "a\n", "b\n")
	p.closeFn()
	p.pump()
	if len(p.tabs()) != 0 {
		t.Errorf("the tab is open: %q", p.tabs())
	}
	if len(p.answers) != 0 {
		t.Errorf("a closed proposal answered %+v", p.answers)
	}
	p.closeFn() // a second call changes nothing
}

func TestAProposedChangeKeepsTheLineEndingsOfTheFile(t *testing.T) {
	p := proposeChange(t, "a\r\nb\r\n", "a\r\nB\r\n")
	if got := p.pane().Model().View(diffeditor.Modified).Document().Text(); got != "a\nB\n" {
		t.Errorf("the right side is %q", got)
	}
	p.tap("Accept")
	if len(p.answers) != 1 || p.answers[0].Text != "a\r\nB\r\n" || p.answers[0].Edited {
		t.Errorf("the answers are %+v", p.answers)
	}
}

func TestAProposedChangeOfANewFileHasAnEmptyLeftSide(t *testing.T) {
	p := proposeChange(t, "", "package main\n")
	if got := p.pane().Model().View(diffeditor.Original).Document().Text(); got != "" {
		t.Errorf("the left side is %q", got)
	}
	if lang := p.pane().Model().View(diffeditor.Modified).Document().LanguageID(); lang != "go" {
		t.Errorf("the language of the right side is %q", lang)
	}
}

func TestAProposedChangeNeedsAFileAndAShownWindowTab(t *testing.T) {
	env := newStateEnv(t, nil)
	run := env.start(env.root)
	host := extEditorHost{w: run.win}
	_, err := host.OpenProposed(extension.ProposedChange{URI: "untitled:1", Proposed: "x"}, func(extension.ProposedResult) {})
	appErr, ok := errors.FromError(err)
	if !ok {
		t.Fatalf("expected *errors.Error, got %T (%v)", err, err)
	}
	if !appErr.HasRoute("extEditorHost.OpenProposed.ExtEditorsProposalFileOnly") && !strings.Contains(appErr.Route(), "ExtEditorsProposalFileOnly") {
		t.Errorf("unexpected failure path: %s", appErr.Route())
	}
	run.close()
	_, err = host.OpenProposed(extension.ProposedChange{URI: "file:///x/y.go"}, func(extension.ProposedResult) {})
	route := func(want string) {
		t.Helper()
		e, ok := errors.FromError(err)
		if !ok || !e.HasRoute(want) {
			t.Errorf("unexpected error %v, want the route %s", err, want)
		}
	}
	route("extEditorHost.shellOrErr.ExtEditorsUnavailable")
}

func TestSeveralProposedChangesShareTheGroupBesideTheActiveOne(t *testing.T) {
	p := proposeChange(t, "a\n", "b\n")
	_, err := extEditorHost{w: p.win}.OpenProposed(extension.ProposedChange{URI: "file://" + p.file + ".other.go", Proposed: "c\n", Title: "other"}, func(extension.ProposedResult) {})
	if err != nil {
		t.Fatal(err)
	}
	p.pump()
	if groups := p.shell().group.grid.Groups(editorgroup.GridAppearance); len(groups) != 2 {
		t.Errorf("%d groups, want 2", len(groups))
	}
	if got := p.tabs(); !slices.Equal(got, []string{"main.go (proposed)", "other"}) {
		t.Errorf("the tabs of the side group are %q", got)
	}
}
