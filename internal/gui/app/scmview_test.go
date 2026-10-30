package app

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/rm4n0s/errors"
	"github.com/rm4n0s/glutter/gluttertest"

	"github.com/rm4n0s/emetos/internal/core/command"
	scmcore "github.com/rm4n0s/emetos/internal/core/scm"
	"github.com/rm4n0s/emetos/internal/gui/testutil"
	scmview "github.com/rm4n0s/emetos/internal/gui/views/scm"
	"github.com/rm4n0s/emetos/internal/gui/workbench"
)

// The tests of the Source Control view in the window (step 24.3): the dev command that fills it, the commands and the key that
// show it, the badge and the status bar items that follow the model, and the state that outlives the window.

// scmFixture writes a fixture of dev.scm.fake with two repositories under dir and returns its path.
func scmFixture(t *testing.T, dir string) string {
	t.Helper()
	fx := map[string]any{"sourceControls": []any{
		map[string]any{"id": "git", "label": "Git", "root": filepath.Join(dir, "one"),
			"status": []any{map[string]any{"text": "$(git-branch) main", "tooltip": "Checkout", "command": "dev.scm.checkout"},
				map[string]any{"text": "$(sync) 1↓ 2↑", "command": "dev.scm.sync"}},
			"groups": []any{
				map[string]any{"id": "index", "label": "Staged Changes", "resources": []any{
					map[string]any{"path": "a.go", "letter": "M", "color": "gitDecoration.stageModifiedResourceForeground"}}},
				map[string]any{"id": "workingTree", "label": "Changes", "resources": []any{
					map[string]any{"path": "b.go", "letter": "M"}, map[string]any{"path": "c.go", "letter": "D", "strikeThrough": true}}},
			}},
		map[string]any{"id": "git", "label": "Git", "root": filepath.Join(dir, "two"),
			"status": []any{map[string]any{"text": "$(git-branch) dev"}},
			"groups": []any{map[string]any{"id": "workingTree", "label": "Changes", "resources": []any{map[string]any{"path": "d.go", "letter": "U"}}}}},
	}}
	data, err := json.Marshal(fx)
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(t.TempDir(), "fixture.json")
	if err := os.WriteFile(path, data, 0o644); err != nil {
		t.Fatal(err)
	}
	return path
}

// scmApp is a window with the fake source controls of scmFixture.
func scmApp(t *testing.T) *saveApp {
	t.Helper()
	a := newSaveApp(t, map[string]string{"a.go": "package a\n"})
	if !DevCommandsEnabled() {
		t.Skip("dev commands are off in this build")
	}
	if err := a.win.Commands.Run(command.Context{Ctx: context.Background(), Source: command.SourceAPI}, "dev.scm.fake",
		map[string]any{"fixture": scmFixture(t, t.TempDir())}); err != nil {
		t.Fatal(err)
	}
	a.pump()
	return a
}

func TestDevSCMFakeFillsTheModel(t *testing.T) {
	a := scmApp(t)
	m := a.win.SCM
	if got := len(m.Sources()); got != 2 {
		t.Fatalf("%d source controls, want 2", got)
	}
	// three resources of the first repository and one of the second
	if got := m.ActivityBadge(); got != 4 {
		t.Errorf("the badge counts %d, want 4", got)
	}
	b, ok := a.win.Views.Badge(command.ViewContainerSCM)
	if !ok || b.Count != 4 || b.Tooltip != "4 pending changes" {
		t.Errorf("the badge of the icon is %+v (%v)", b, ok)
	}
	// the status bar items of the active repository and the one that names it
	first := a.status("status.scm.0")
	if !strings.Contains(first.Entry.Text, "main") || first.Entry.Command != "dev.scm.checkout" {
		t.Errorf("the first status item is %+v", first.Entry)
	}
	if got := a.statusText("status.scm.1"); !strings.Contains(got, "1↓ 2↑") {
		t.Errorf("the second status item is %q", got)
	}
	if got := a.statusText("status.scm.provider"); !strings.Contains(got, "one") && got == "" {
		t.Errorf("the item of the repository is %q", got)
	}
	// the badge follows a change of the model
	m.Sources()[1].SetCount(10)
	a.pump()
	if b, _ := a.win.Views.Badge(command.ViewContainerSCM); b.Count != 13 {
		t.Errorf("the badge after a change counts %d, want 13", b.Count)
	}
	a.noErrors()
}

func TestSCMViewShowsOnTheCommandAndTheKey(t *testing.T) {
	a := scmApp(t)
	a.run("workbench.view.explorer")
	if a.win.Views.Active() == command.ViewContainerSCM {
		t.Fatal("the container is open before it is asked for")
	}
	a.run("workbench.view.scm")
	if a.win.Views.Active() != command.ViewContainerSCM {
		t.Fatalf("the side bar shows %q", a.win.Views.Active())
	}
	for _, text := range []string{"Staged Changes", "Changes", "a.go", "b.go"} {
		if !a.ts.Exists(gluttertest.ByText(text)) {
			t.Errorf("the view does not show %q", text)
		}
	}
	if !a.shell().scm.view.HasFocus() {
		t.Error("the Changes view has no focus after the command")
	}
	a.run("workbench.view.explorer")
	a.press("ctrl+shift+g")
	if a.win.Views.Active() != command.ViewContainerSCM {
		t.Errorf("ctrl+shift+g shows %q", a.win.Views.Active())
	}
	// the tooltip of the icon names the container and the badge
	a.noErrors()
}

func TestSCMCollapseCommands(t *testing.T) {
	a := scmApp(t)
	a.run("workbench.view.scm")
	view := a.shell().scm.view
	rows := view.RowCount()
	if rows < 6 {
		t.Fatalf("the view has %d rows", rows)
	}
	a.run("workbench.scm.action.collapseAllRepositories")
	if got := view.RowCount(); got != 2 {
		t.Errorf("%d rows with every repository collapsed, want 2", got)
	}
	if !a.win.Keys.Get("scmViewAreAllRepositoriesCollapsed").(bool) {
		t.Error("the key for all repositories collapsed is not set")
	}
	a.run("workbench.scm.action.expandAllRepositories")
	if got := view.RowCount(); got != rows {
		t.Errorf("%d rows after expanding, want %d", got, rows)
	}
	a.noErrors()
}

func TestSCMToggleRepositoryVisibility(t *testing.T) {
	a := scmApp(t)
	src := a.win.SCM.Sources()[1]
	if len(a.win.SCM.Visible()) != 2 {
		t.Fatalf("%d repositories shown", len(a.win.SCM.Visible()))
	}
	err := a.win.Commands.Run(command.Context{Ctx: context.Background(), Source: command.SourceAPI}, "workbench.scm.action.toggleRepositoryVisibility",
		[]any{map[string]any{"sourceControl": src.Key()}})
	if err != nil {
		t.Fatal(err)
	}
	if got := a.win.SCM.Visible(); len(got) != 1 || got[0] == src {
		t.Errorf("the repository is still shown: %d", len(got))
	}
	a.noErrors()
}

func TestSCMStateOutlivesTheWindow(t *testing.T) {
	dir := t.TempDir()
	fixture := scmFixture(t, dir)
	run := func(a *saveApp) {
		t.Helper()
		if err := a.win.Commands.Run(command.Context{Ctx: context.Background(), Source: command.SourceAPI}, "dev.scm.fake", map[string]any{"fixture": fixture}); err != nil {
			t.Fatal(err)
		}
		a.pump()
	}
	first := newSaveApp(t, map[string]string{"a.go": "x\n"})
	if !DevCommandsEnabled() {
		t.Skip("dev commands are off in this build")
	}
	run(first)
	first.run("workbench.view.scm")
	view := first.shell().scm.view
	view.CollapseAllRepositories()
	first.win.SCM.SetViewMode(scmcore.ViewTree)
	first.win.SCM.ToggleVisibility(first.win.SCM.Sources()[1], nil)
	first.pump()
	raw := first.shell().scmWindowState()
	if len(raw) == 0 {
		t.Fatal("no state to save")
	}
	st, err := decodeSCMState(raw)
	if err != nil {
		t.Fatal(err)
	}
	if len(st.Model.All) != 2 || len(st.Model.Visible) != 1 || st.Model.ViewMode != scmcore.ViewTree || len(st.View) == 0 {
		t.Fatalf("the state is %s", raw)
	}

	// another window starts with the state, before its source controls exist
	win := newTestWorkspace(t, "")
	win.restoreSCM(raw)
	second := newSaveAppWith(t, win, []string{"a.go"}, map[string]string{"a.go": "x\n"})
	run(second)
	m := second.win.SCM
	if m.ViewMode() != scmcore.ViewTree {
		t.Errorf("the view mode is %v", m.ViewMode())
	}
	if got := m.Visible(); len(got) != 1 || got[0] != m.Sources()[0] {
		t.Errorf("the repositories shown are %d, want the first only", len(got))
	}
	second.run("workbench.view.scm")
	// the folds are of the repositories that are hidden too: show the other one to see the header of the first
	m.ToggleVisibility(m.Sources()[1], nil)
	second.pump()
	if !second.shell().scm.view.Collapsed("r:" + m.Sources()[0].Key()) {
		t.Error("the repository was not collapsed after the restart")
	}
	first.noErrors()
	second.noErrors()
}

func TestSCMErrorRoutes(t *testing.T) {
	a := newSaveApp(t, map[string]string{"a.go": "x\n"})
	for _, tc := range []struct {
		name  string
		err   func() error
		route string
	}{
		{"no fixture", func() error { _, err := parseFakeSCM(nil); return err }, "parseFakeSCM.DevSCMFakeInvalid"},
		{"an empty fixture path", func() error { _, err := parseFakeSCM(map[string]any{"fixture": ""}); return err }, "parseFakeSCM.DevSCMFakeInvalid"},
		{"a fixture that is not there", func() error { _, err := parseFakeSCM("/no/such/fixture.json"); return err }, "parseFakeSCM.DevSCMFakeReadFailed"},
		{"a fixture that is not JSON", func() error {
			p := filepath.Join(t.TempDir(), "f.json")
			if err := os.WriteFile(p, []byte("{"), 0o644); err != nil {
				t.Fatal(err)
			}
			_, err := parseFakeSCM(p)
			return err
		}, "parseFakeSCM.DevSCMFakeFixtureInvalid"},
		{"a saved state that is not JSON", func() error { _, err := decodeSCMState(json.RawMessage("{")); return err }, "decodeSCMState.ScmStateInvalid"},
		{"a state that cannot be written", func() error {
			_, err := encodeSCMState(scmWindowState{View: json.RawMessage("{")})
			return err
		}, "encodeSCMState.ScmStateEncodeFailed"},
		{"link arguments that are not JSON", func() error { _, err := linkArguments("{"); return err }, "linkArguments.ScmWelcomeLinkInvalid"},
		{"a drop on a group that is gone", func() error { return a.shell().scmOpenIn(nil, "file:///x.go", 999, 0) }, "shellState.scmOpenIn.ScmDropGroupMissing"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			err := tc.err()
			appErr, ok := errors.FromError(err)
			if !ok {
				t.Fatalf("expected *errors.Error, got %T (%v)", err, err)
			}
			if !appErr.HasRoute(tc.route) {
				t.Errorf("unexpected failure path: %s", appErr.Route())
			}
		})
	}
}

func TestLinkArguments(t *testing.T) {
	for _, tc := range []struct {
		query string
		want  any
	}{
		{"", nil},
		{`["a",1]`, []any{"a", 1.0}},
		{`{"k":true}`, map[string]any{"k": true}},
	} {
		got, err := linkArguments(tc.query)
		if err != nil {
			t.Fatal(err)
		}
		if a, b := marshal(t, got), marshal(t, tc.want); a != b {
			t.Errorf("%q gives %s, want %s", tc.query, a, b)
		}
	}
}

func marshal(t *testing.T, v any) string {
	t.Helper()
	data, err := json.Marshal(v)
	if err != nil {
		t.Fatal(err)
	}
	return string(data)
}

func TestSCMDefaultWelcome(t *testing.T) {
	a := newSaveApp(t, map[string]string{"a.go": "x\n"})
	a.run("workbench.view.scm")
	if !a.ts.Exists(gluttertest.ByKey(scmview.WelcomeKey)) {
		t.Error("the default text of the empty view is not shown")
	}
	a.noErrors()
}

var _ = workbench.StatusLeft

// TestGoldenSourceControlInTheWorkbench looks at the window with the Source Control view of two repositories (the dev command's
// fixture): the badge on the icon with the count of changes, the branch and sync items of the active repository at the left end of
// the status bar, the header with the title buttons, the groups with their letters and colors, and the repository that is folded.
func TestGoldenSourceControlInTheWorkbench(t *testing.T) {
	for _, th := range goldenThemesOfTheGrid {
		t.Run(th.name, func(t *testing.T) {
			a := goldenExplorer(t, th.theme, false)
			if !DevCommandsEnabled() {
				t.Skip("dev commands are off in this build")
			}
			ctx := command.Context{Ctx: context.Background(), Source: command.SourceAPI}
			if err := a.win.Commands.Run(ctx, "dev.scm.fake", map[string]any{"fixture": scmFixture(t, t.TempDir())}); err != nil {
				t.Fatal(err)
			}
			if err := a.win.Commands.Run(ctx, "workbench.view.scm", nil); err != nil {
				t.Fatal(err)
			}
			a.pump()
			a.ts.Pump(300 * time.Millisecond)
			testutil.MatchesGolden(a.ts, "scm_view_"+th.name)
			if errs := a.ts.TakeErrors(); len(errs) != 0 {
				t.Errorf("errors: %v", errs)
			}
		})
	}
}
