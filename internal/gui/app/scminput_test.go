package app

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/rm4n0s/errors"
	"github.com/rm4n0s/glutter/gluttertest"

	"github.com/rm4n0s/emetos/internal/core/command"
	"github.com/rm4n0s/emetos/internal/core/keybinding"
	scmcore "github.com/rm4n0s/emetos/internal/core/scm"
	"github.com/rm4n0s/emetos/internal/core/settings"
	"github.com/rm4n0s/emetos/internal/gui/kit"
	scmview "github.com/rm4n0s/emetos/internal/gui/views/scm"
)

// The commit message box in the window (step 24.4): its keys through the key dispatcher and the default keybindings, the commands
// they run, the settings that change it, and the dev command that sets its text.

// scmBoxFixture writes a fixture with one repository that has the box, an action button with a dropdown, and a validation rule
// (a subject of more than 12 characters), and returns its path.
func scmBoxFixture(t *testing.T, dir string) string {
	t.Helper()
	fx := map[string]any{"sourceControls": []any{
		map[string]any{"id": "git", "label": "Git", "root": filepath.Join(dir, "repo"),
			"status":     []any{map[string]any{"text": "$(git-branch) main", "command": "dev.scm.checkout"}},
			"validation": map[string]any{"subjectLength": 12, "level": "warning"},
			"actionButton": map[string]any{"title": "$(check) Commit", "description": "Commit the staged changes",
				"secondary": []any{[]any{map[string]any{"id": "dev.scm.commitAmend", "title": "Commit (Amend)"}}}},
			"groups": []any{map[string]any{"id": "index", "label": "Staged Changes", "resources": []any{
				map[string]any{"path": "a.go", "letter": "M", "color": "gitDecoration.stageModifiedResourceForeground"}}}},
		},
	}}
	data, err := json.Marshal(fx)
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(t.TempDir(), "box.json")
	if err := os.WriteFile(path, data, 0o644); err != nil {
		t.Fatal(err)
	}
	return path
}

// scmBoxApp is a window with the Source Control view open on the fixture of scmBoxFixture.
func scmBoxApp(t *testing.T) (*saveApp, *scmcore.Source) {
	t.Helper()
	a := newSaveApp(t, map[string]string{"a.go": "package a\n"})
	if !DevCommandsEnabled() {
		t.Skip("dev commands are off in this build")
	}
	ctx := command.Context{Ctx: context.Background(), Source: command.SourceAPI}
	if err := a.win.Commands.Run(ctx, "dev.scm.fake", map[string]any{"fixture": scmBoxFixture(t, t.TempDir())}); err != nil {
		t.Fatal(err)
	}
	a.run("workbench.view.scm")
	a.pump()
	return a, a.win.SCM.Sources()[0]
}

// boxFinder finds the text box of a source control.
func boxFinder(src *scmcore.Source) gluttertest.Finder {
	return gluttertest.Descendant(gluttertest.ByKey(scmview.InputKey(src)), gluttertest.ByType[*kit.InputBox]())
}

// typeInBox types text into the box (the whole text, as the input method sends it).
func (a *saveApp) typeInBox(src *scmcore.Source, text string) {
	a.t.Helper()
	a.ts.EnterText(boxFinder(src), text)
	a.pump()
}

func (a *saveApp) commits() []devSCMRun {
	var out []devSCMRun
	for _, r := range a.shell().scm.fakeRuns {
		if r.Command == "dev.scm.commit" {
			out = append(out, r)
		}
	}
	return out
}

func TestSCMViewFocusesTheBoxAndShowsTheKey(t *testing.T) {
	a, src := scmBoxApp(t)
	view := a.shell().scm.view
	if !view.InputFocused(src) {
		t.Error("the box does not have the keyboard when the view is shown")
	}
	// the placeholder names the key bound to scm.acceptInput, whichever context the lookup is made in
	if got := a.win.keyLabelOf("scm.acceptInput"); got != "Ctrl+Enter" {
		t.Errorf("the label of the key of scm.acceptInput is %q", got)
	}
	if !a.ts.Exists(gluttertest.ByText(`Message (Ctrl+Enter to commit on "main")`)) {
		t.Errorf("no placeholder with the key")
	}
	if !a.ts.Exists(gluttertest.ByText("Commit")) {
		t.Error("no action button")
	}
	a.noErrors()
}

func TestSCMAcceptInput(t *testing.T) {
	a, src := scmBoxApp(t)
	a.typeInBox(src, "Fix café")
	a.press("ctrl+enter")
	runs := a.commits()
	if len(runs) != 1 || runs[0].Input != "Fix café" {
		t.Fatalf("Ctrl+Enter ran %+v", runs)
	}
	// the provider emptied the box after the commit
	if got := src.Input().Value(); got != "" {
		t.Errorf("the box holds %q after the commit", got)
	}
	// only with the keyboard in the box: the editor has the keys when it has the focus
	a.typeInBox(src, "second")
	a.run("workbench.action.focusFirstEditorGroup")
	a.pump()
	if a.shell().scm.view.InputFocused(src) {
		t.Fatal("the box kept the keyboard after the focus moved to the editor")
	}
	kb, err := keybinding.ParseKeybinding("ctrl+enter")
	if err != nil {
		t.Fatal(err)
	}
	sendChord(a.ts, kb[0]) // no command is bound to it in the editor: the key is not used
	a.pump()
	if n := len(a.commits()); n != 1 {
		t.Errorf("Ctrl+Enter ran the accept command %d times in all, want 1 (no run while the editor has the keyboard)", n)
	}
	// the button runs the same command
	a.ts.Tap(gluttertest.ByText("Commit"))
	a.pump()
	if runs := a.commits(); len(runs) != 2 || runs[1].Input != "second" {
		t.Errorf("the button ran %+v", runs)
	}
	a.noErrors()
}

// TestSCMInputKeys runs key sequences through the dispatcher and the default keybindings and looks at the text: Up and Down go
// through the messages only at the very start and end of the text (Alt anywhere), Escape clears a validation message first and then
// the text, and Ctrl+Enter accepts.
func TestSCMInputKeys(t *testing.T) {
	type step struct {
		do   string // a key, or "type:<text>", or "commit", or "caret:<n>"
		want string // the text of the box after it
	}
	tests := []struct {
		name  string
		steps []step
	}{
		{"Up and Down go through the messages", []step{
			{"type:first", "first"}, {"commit", ""},
			{"type:second", "second"}, {"commit", ""},
			{"up", "second"}, {"down", "second"}, // the caret is at the start of "second" after Up; Down moves it to the end
			{"down", ""},
			{"up", "second"}, {"alt+up", "first"}, {"alt+up", "first"},
			{"alt+down", "second"}, {"alt+down", ""},
		}},
		{"Alt+Up goes back from the middle of the text", []step{
			{"type:one", "one"}, {"commit", ""},
			{"type:ab\ncd", "ab\ncd"}, {"caret:1", "ab\ncd"}, {"alt+up", "one"},
		}},
		{"Down at the end of the text with no later message does nothing", []step{
			{"type:text", "text"}, {"down", "text"}, {"alt+down", "text"},
		}},
		{"a draft is kept when going back and forth", []step{
			{"type:one", "one"}, {"commit", ""},
			{"type:draft", "draft"}, {"alt+up", "one"}, {"alt+down", "draft"},
		}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			a, src := scmBoxApp(t)
			view := a.shell().scm.view
			for i, s := range tt.steps {
				switch {
				case strings.HasPrefix(s.do, "type:"):
					a.typeInBox(src, strings.TrimPrefix(s.do, "type:"))
				case s.do == "commit":
					a.press("ctrl+enter")
				case strings.HasPrefix(s.do, "caret:"):
					n := int(s.do[len("caret:")] - '0')
					a.shell().scm.view.SetInputCaret(src, n)
					a.pump()
				default:
					a.press(s.do)
				}
				if got := view.InputText(src); got != s.want {
					t.Fatalf("step %d (%s): the box holds %q, want %q", i+1, s.do, got, s.want)
				}
				if got := src.Input().Value(); got != s.want {
					t.Fatalf("step %d (%s): the model holds %q, want %q", i+1, s.do, got, s.want)
				}
			}
			a.noErrors()
		})
	}
}

// TestSCMEscapeStages: Escape takes the validation message away, then (with no message) a selection is collapsed, then the text
// is cleared.
func TestSCMEscapeStages(t *testing.T) {
	a, src := scmBoxApp(t)
	view := a.shell().scm.view
	a.typeInBox(src, "A subject that is too long")
	if !view.ValidationShown(src) {
		t.Fatal("no validation message for a long subject")
	}
	if !a.ts.Exists(gluttertest.ByTextContaining("or fewer are recommended")) {
		t.Error("the message is not on screen")
	}
	a.press("escape")
	if view.ValidationShown(src) || a.ts.Exists(gluttertest.ByTextContaining("or fewer are recommended")) {
		t.Error("Escape left the message")
	}
	if got := src.Input().Value(); got != "A subject that is too long" {
		t.Errorf("the first Escape changed the text: %q", got)
	}
	// a selection is collapsed by the next one, and the text stays
	view.SetInputSelection(src, 2, 9)
	a.pump()
	a.press("escape")
	if got := src.Input().Value(); got != "A subject that is too long" {
		t.Errorf("Escape with a selection changed the text: %q", got)
	}
	if s, e := view.InputSelection(src); s != e {
		t.Errorf("the selection is still %d..%d", s, e)
	}
	// and the next one clears the text
	a.press("escape")
	if got := src.Input().Value(); got != "" {
		t.Errorf("Escape did not clear the text: %q", got)
	}
	// clearing is not a message for the history
	if h := src.Input().History(); len(h) != 1 {
		t.Errorf("clearing the box put %q in the history", h)
	}
	a.noErrors()
}

func TestSCMInputKeysDoNothingElsewhere(t *testing.T) {
	a, src := scmBoxApp(t)
	a.typeInBox(src, "keep")
	a.run("workbench.action.focusFirstEditorGroup")
	a.pump()
	// the commands run with no box in the context do nothing
	ctx := command.Context{Ctx: context.Background(), Keys: a.win.Keys, Source: command.SourceAPI}
	for _, id := range []string{"scm.acceptInput", "scm.clearInput", "scm.clearValidation", "scm.viewPreviousCommit", "scm.viewNextCommit",
		"scm.forceViewPreviousCommit", "scm.forceViewNextCommit"} {
		if err := a.win.Commands.Run(ctx, id, nil); err != nil {
			t.Errorf("%s: %v", id, err)
		}
	}
	if got := src.Input().Value(); got != "keep" {
		t.Errorf("the commands changed the text: %q", got)
	}
	if n := len(a.commits()); n != 0 {
		t.Errorf("scm.acceptInput ran the accept command %d times with no box", n)
	}
	a.noErrors()
}

// TestSCMInputSettingsWhileOpen: the settings of the box and the button take effect on the view that is open.
func TestSCMInputSettingsWhileOpen(t *testing.T) {
	a, src := scmBoxApp(t)
	view := a.shell().scm.view
	height := func() float64 { return view.List().RowHeightAt(view.List().IndexOf("i:" + src.Key())) }
	if got := height(); got != 34 {
		t.Fatalf("the row of the box is %v px high, want 34", got)
	}
	update := func(key string, v any) {
		t.Helper()
		if err := a.win.Settings.Update(context.Background(), key, v, settings.TargetUser); err != nil {
			t.Fatal(err)
		}
		a.pump()
	}
	update("scm.inputMinLineCount", 3)
	if got := height(); got != 2+4+3*20+8 {
		t.Errorf("with three lines the row is %v px high", got)
	}
	update("scm.inputMinLineCount", 1)
	update("scm.inputFontSize", 20)
	if got := height(); got != 2+4+30+8 {
		t.Errorf("with a font of 20 px the row is %v px high", got)
	}
	update("scm.inputFontSize", 13)
	update("scm.inputMaxLineCount", 2)
	a.typeInBox(src, "1\n2\n3\n4")
	if got := height(); got != 2+4+2*20+8 {
		t.Errorf("with at most two lines the row is %v px high", got)
	}
	update("scm.inputMaxLineCount", 10)
	update("scm.inputFontFamily", "editor")
	update("scm.inputFontFamily", "default")
	// the button
	if view.List().IndexOf("b:"+src.Key()) < 0 {
		t.Fatal("no button row")
	}
	update("scm.showActionButton", false)
	if view.List().IndexOf("b:"+src.Key()) >= 0 || a.ts.Exists(gluttertest.ByKey(scmview.ActionButtonKey(src))) {
		t.Error("scm.showActionButton false left the button")
	}
	update("scm.showActionButton", true)
	if view.List().IndexOf("b:"+src.Key()) < 0 {
		t.Error("the button does not come back")
	}
	a.noErrors()
}

func TestSCMBoxFontSetting(t *testing.T) {
	a, _ := scmBoxApp(t)
	win := a.win
	set := func(key string, v any) {
		t.Helper()
		if err := win.Settings.Update(context.Background(), key, v, settings.TargetUser); err != nil {
			t.Fatal(err)
		}
	}
	tests := []struct {
		family, editor string
		size           float64
		want           scmview.InputFont
	}{
		{"default", "", 13, scmview.InputFont{Size: 13}},
		{"DEFAULT", "", 13, scmview.InputFont{Size: 13}},
		{"", "", 14, scmview.InputFont{Size: 14}},
		{"editor", "'Fira Code', monospace", 13, scmview.InputFont{Family: "Fira Code", Size: 13}},
		{"Editor", "", 13, scmview.InputFont{Mono: true, Size: 13}},
		{"Iosevka", "monospace", 12, scmview.InputFont{Family: "Iosevka", Size: 12}},
	}
	for _, tt := range tests {
		set("scm.inputFontFamily", tt.family)
		set("editor.fontFamily", tt.editor)
		set("scm.inputFontSize", tt.size)
		if got := scmInputFont(win); got != tt.want {
			t.Errorf("family %q, editor %q: %+v, want %+v", tt.family, tt.editor, got, tt.want)
		}
	}
}

func TestDevSCMInput(t *testing.T) {
	a, src := scmBoxApp(t)
	ctx := command.Context{Ctx: context.Background(), Source: command.SourceAPI}
	if err := a.win.Commands.Run(ctx, "dev.scm.input", map[string]any{"text": "set by a script"}); err != nil {
		t.Fatal(err)
	}
	a.pump()
	if got := a.shell().scm.view.InputText(src); got != "set by a script" || src.Input().Value() != "set by a script" {
		t.Errorf("the box holds %q", got)
	}
	// with no text it answers with the box and the runs
	a.press("ctrl+enter")
	runs := a.commits()
	if len(runs) != 1 || runs[0].Input != "set by a script" {
		t.Errorf("runs %+v", runs)
	}
	for _, bad := range []any{"text", []any{1}} {
		if err := a.win.Commands.Run(ctx, "dev.scm.input", bad); err == nil {
			t.Errorf("dev.scm.input %v: no error", bad)
		}
	}
	if err := a.win.Commands.Run(ctx, "dev.scm.input", map[string]any{"text": "x", "repository": 3.0}); err == nil {
		t.Error("a repository that does not exist: no error")
	}
	a.noErrors()
}

// TestSCMErrorRoutesOfTheBox: every tag of this step has a test that asserts its route.
func TestSCMErrorRoutesOfTheBox(t *testing.T) {
	a, _ := scmBoxApp(t)
	ctx := command.Context{Ctx: context.Background(), Source: command.SourceAPI}
	check := func(name string, err error, route string) {
		t.Helper()
		appErr, ok := errors.FromError(err)
		if !ok {
			t.Errorf("%s: expected *errors.Error, got %T (%v)", name, err, err)
			return
		}
		if !appErr.HasRoute(route) {
			t.Errorf("%s: unexpected failure path: %s", name, appErr.Route())
		}
	}
	_, _, _, _, err := devSCMInputArgs("text")
	check("arguments that are not an object", err, "devSCMInputArgs.DevSCMInputInvalid")
	err = a.win.Commands.Run(ctx, "dev.scm.input", map[string]any{"text": "x", "repository": 7.0})
	check("a repository that does not exist", err, "shellState.devSCMInput.DevSCMInputNoRepository")
}

// TestSCMPlaceholderFollowsTheKeyOfTheAcceptCommand: the key in the placeholder is the one bound to scm.acceptInput, also when the
// user rebinds it (VS Code looks the binding up for the placeholder).
func TestSCMPlaceholderFollowsTheKeyOfTheAcceptCommand(t *testing.T) {
	a := newKeybindingsApp(t, "", `[{ "key": "ctrl+shift+enter", "command": "scm.acceptInput", "when": "scmRepository" },
		{ "key": "ctrl+enter", "command": "-scm.acceptInput" }]`)
	if !DevCommandsEnabled() {
		t.Skip("dev commands are off in this build")
	}
	ctx := command.Context{Ctx: context.Background(), Source: command.SourceAPI}
	if err := a.win.Commands.Run(ctx, "dev.scm.fake", map[string]any{"fixture": scmBoxFixture(t, t.TempDir())}); err != nil {
		t.Fatal(err)
	}
	a.run("workbench.view.scm")
	a.pump()
	if got := a.win.keyLabelOf("scm.acceptInput"); got != "Ctrl+Shift+Enter" {
		t.Fatalf("the label of the key is %q", got)
	}
	if !a.ts.Exists(gluttertest.ByText(`Message (Ctrl+Shift+Enter to commit on "main")`)) {
		t.Error("the placeholder does not name the key the user bound")
	}
	// and the new key accepts, the old one does not
	src := a.win.SCM.Sources()[0]
	a.ts.EnterText(boxFinder(src), "rebound")
	a.pump()
	kb, err := keybinding.ParseKeybinding("ctrl+enter")
	if err != nil {
		t.Fatal(err)
	}
	sendChord(a.ts, kb[0])
	a.pump()
	if len(a.shell().scm.fakeRuns) != 0 {
		t.Error("the old key still accepts")
	}
	a.press("ctrl+shift+enter")
	if len(a.shell().scm.fakeRuns) != 1 {
		t.Errorf("the new key ran %v", a.shell().scm.fakeRuns)
	}
}
