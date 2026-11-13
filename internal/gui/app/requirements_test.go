package app

import (
	"context"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/rm4n0s/glutter/gluttertest"
	"github.com/rm4n0s/glutter/widgets"

	"github.com/rm4n0s/emetos/extension"
	"github.com/rm4n0s/emetos/internal/core/document"
	"github.com/rm4n0s/emetos/internal/core/settings"
	"github.com/rm4n0s/emetos/internal/gui/extensionsui"
	"github.com/rm4n0s/emetos/internal/gui/testutil"
)

// The requirements on the Extensions page (step 34.4): each state, each button, a turned-off extension, and results that arrive while
// the page is open.

// gate is a check of a test that answers when the test opens it.
type gate struct {
	mu   sync.Mutex
	open chan struct{}
	res  extension.RequirementResult
	runs int
}

func newGate(res extension.RequirementResult) *gate {
	return &gate{open: make(chan struct{}), res: res}
}

func (g *gate) check(ctx context.Context, _ extension.RequirementEnv) extension.RequirementResult {
	g.mu.Lock()
	g.runs++
	open := g.open
	g.mu.Unlock()
	select {
	case <-open:
	case <-ctx.Done():
	}
	g.mu.Lock()
	defer g.mu.Unlock()
	return g.res
}

func (g *gate) release() { close(g.open) }

func (g *gate) count() int {
	g.mu.Lock()
	defer g.mu.Unlock()
	return g.runs
}

// reqAnswer is a check that answers at once.
func reqAnswer(res extension.RequirementResult) func(context.Context, extension.RequirementEnv) extension.RequirementResult {
	return func(context.Context, extension.RequirementEnv) extension.RequirementResult { return res }
}

// requirementTools is the extension of the tests that needs things: gopls (missing, with every kind of help), dlv (optional,
// missing), go (found), the linter (too old), the camera (unknown). The install command of gopls runs requirement.install.
func requirementTools(goplsCheck func(context.Context, extension.RequirementEnv) extension.RequirementResult, installs *int) extension.Factory {
	install := extension.Install{Docs: "https://example.com/gopls", Commands: []extension.InstallCommand{
		{Family: extension.FamilyFedora, Command: "sudo dnf install gopls"},
		{Family: extension.FamilyAny, Command: "go install golang.org/x/tools/gopls@latest"},
	}}
	return func() extension.Extension {
		return &testExtension{
			manifest: extension.Manifest{
				ID: "emetos.tools", Name: "Tools", Version: "1.0.0", Description: "Needs things.", ActivationEvents: []string{extension.OnStartup},
				Contributes: extension.Contributes{
					Commands: []extension.CommandSpec{{ID: "emetos.tools.install", Title: "Install Tools"}},
					Settings: []extension.SettingSpec{{Key: "tools.goplsPath", Type: "string", Description: "The path of gopls."}},
				},
				Requirements: []extension.Requirement{
					{ID: "gopls", Name: "gopls", Kind: extension.RequirementProgram, Purpose: "language features", Install: install,
						InstallAction: "emetos.tools.install", PathSetting: "tools.goplsPath", Check: goplsCheck},
					{ID: "dlv", Name: "dlv", Kind: extension.RequirementProgram, Purpose: "debugging", Optional: true, Install: install,
						Check: reqAnswer(extension.RequirementResult{State: extension.RequirementMissing, Detail: "dlv was not found in PATH"})},
					{ID: "go", Name: "go", Kind: extension.RequirementProgram, Purpose: "builds",
						Check: reqAnswer(extension.RequirementResult{State: extension.RequirementFound, Path: "/usr/bin/go", Version: "1.27.0"})},
					{ID: "lint", Name: "golint", Kind: extension.RequirementProgram, Purpose: "linting",
						Check: reqAnswer(extension.RequirementResult{State: extension.RequirementUnusable, Path: "/usr/bin/golint", Detail: "golint 0.1 is older than 0.2"})},
					{ID: "camera", Name: "Camera", Kind: extension.RequirementDevice, Purpose: "scanning", Optional: true,
						Check: reqAnswer(extension.RequirementResult{State: extension.RequirementUnknown, Detail: "the check took longer than 10s"})},
				},
			},
			activate: func(_ context.Context, api extension.API) error {
				return api.Commands().Handle("emetos.tools.install", func(context.Context, any) error {
					*installs++
					return nil
				})
			},
		}
	}
}

// requirementsApp opens the Extensions page in a window over the factories, with the settings given.
func requirementsApp(t *testing.T, values map[string]any, factories ...extension.Factory) *groupApp {
	t.Helper()
	paths := testutil.Isolate(t)
	all := map[string]any{"files.hotExit": "off"}
	for k, v := range values {
		all[k] = v
	}
	testutil.Settings(t, paths, all)
	p := newTestProcess(ProcessOptions{Paths: paths, Extensions: factories, SystemFamily: extension.FamilyFedora, SystemName: "Test OS 1, fedora, linux/amd64"})
	folder := filepath.Join(t.TempDir(), "proj")
	if err := os.MkdirAll(folder, 0o755); err != nil {
		t.Fatal(err)
	}
	win := newExtensionWorkspace(t, p, folder)
	a := &groupApp{t: t, win: win, files: map[string]string{}, docs: map[string]*document.Document{}}
	a.ts = testutil.New(t, testWindowSize)
	a.ts.PumpWidget(newAppDocs(context.Background(), win, nil, nil, ""))
	a.pump()
	return a
}

func TestTheExtensionsPageShowsTheRequirements(t *testing.T) {
	gopls := newGate(extension.RequirementResult{State: extension.RequirementMissing, Detail: "gopls was not found in PATH"})
	installs := 0
	a := requirementsApp(t, nil, requirementTools(gopls.check, &installs))
	if a.win.Process.Workspaces.reqs != nil {
		t.Fatal("the requirements are checked before a page shows them")
	}
	a.run("workbench.extensions.action.showInstalledExtensions")
	// gopls waits for the test: the others answered, gopls is still being checked
	a.pump()
	if !a.ts.Exists(gluttertest.ByText("gopls  •  Checking...")) {
		t.Errorf("gopls is not shown as being checked")
	}
	for _, want := range []string{
		"dlv (optional)  •  dlv was not found in PATH",
		"go  •  /usr/bin/go 1.27.0",
		"golint  •  golint 0.1 is older than 0.2",
		"Camera (optional)  •  the check took longer than 10s",
	} {
		if !a.ts.Exists(gluttertest.ByText(want)) {
			t.Errorf("the page does not show %q", want)
		}
	}
	if a.ts.Exists(gluttertest.ByKey(extensionsui.RequirementButtonKey("emetos.tools", "gopls", extensionsui.ActionCopyInstall))) {
		t.Error("a requirement being checked has buttons")
	}
	// the result arrives while the page is open
	gopls.release()
	waitForRequirements(t, a, func() bool { return a.ts.Exists(gluttertest.ByText("gopls  •  gopls was not found in PATH")) })
	// buttons: only for the missing and the unusable ones, and only what they have
	key := func(id, action string) gluttertest.Finder {
		return gluttertest.ByKey(extensionsui.RequirementButtonKey("emetos.tools", id, action))
	}
	for _, tt := range []struct {
		id, action string
		want       bool
	}{
		{"gopls", extensionsui.ActionCopyInstall, true}, {"gopls", extensionsui.ActionInstall, true}, {"gopls", extensionsui.ActionSetPath, true}, {"gopls", extensionsui.ActionInstructions, true},
		{"dlv", extensionsui.ActionCopyInstall, true}, {"dlv", extensionsui.ActionInstall, false}, {"dlv", extensionsui.ActionSetPath, false},
		{"go", extensionsui.ActionCopyInstall, false}, {"lint", extensionsui.ActionCopyInstall, false}, {"camera", extensionsui.ActionInstructions, false},
	} {
		if got := a.ts.Exists(key(tt.id, tt.action)); got != tt.want {
			t.Errorf("%s %s shown = %v", tt.id, tt.action, got)
		}
	}

	// Copy Install Command copies the command of this system's family
	a.ts.Tap(key("gopls", extensionsui.ActionCopyInstall))
	a.pump()
	if text, _ := a.ts.ClipboardText(); text != "sudo dnf install gopls" {
		t.Errorf("the clipboard has %q", text)
	}
	// Install runs the extension's command
	a.ts.Tap(key("gopls", extensionsui.ActionInstall))
	a.pump()
	if installs != 1 {
		t.Errorf("the install command ran %d times", installs)
	}
	// Instructions opens the address
	var opened []string
	a.win.OpenURI = func(_ context.Context, _ widgets.BuildContext, uri string) error {
		opened = append(opened, uri)
		return nil
	}
	a.ts.Tap(key("gopls", extensionsui.ActionInstructions))
	a.pump()
	if len(opened) != 1 || opened[0] != "https://example.com/gopls" {
		t.Errorf("opened %v", opened)
	}
	// Check Again checks everything again
	a.ts.Tap(gluttertest.ByKey(extensionsui.CheckAgainKey()))
	a.pump()
	waitForRequirements(t, a, func() bool {
		return gopls.count() == 2 && a.ts.Exists(gluttertest.ByText("gopls  •  gopls was not found in PATH"))
	})
	// Set Path... opens the Settings editor on the key
	a.ts.Tap(key("gopls", extensionsui.ActionSetPath))
	a.pump()
	if got := settingsQuery(a); got != "@id:tools.goplsPath" {
		t.Errorf("the Settings editor searches %q", got)
	}
	if a.active() != "Settings" {
		t.Errorf("the active tab is %q", a.active())
	}
}

func TestATurnedOffExtensionShowsItsRequirements(t *testing.T) {
	installs := 0
	a := requirementsApp(t, map[string]any{"extensions.disabled": []any{"emetos.tools"}},
		requirementTools(reqAnswer(extension.RequirementResult{State: extension.RequirementFound, Path: "/opt/gopls"}), &installs))
	a.run("workbench.extensions.action.showInstalledExtensions")
	waitForRequirements(t, a, func() bool { return a.ts.Exists(gluttertest.ByText("gopls  •  /opt/gopls")) })
	if !a.ts.Exists(gluttertest.ByText("Disabled")) {
		t.Error("the extension is not shown as turned off")
	}
}

func TestAPathSettingThatChangesIsCheckedAgain(t *testing.T) {
	var mu sync.Mutex
	runs := 0
	check := func(_ context.Context, env extension.RequirementEnv) extension.RequirementResult {
		mu.Lock()
		runs++
		mu.Unlock()
		if p := env.StringSetting("tools.goplsPath"); p != "" {
			return extension.RequirementResult{State: extension.RequirementFound, Path: p}
		}
		return extension.RequirementResult{State: extension.RequirementMissing, Detail: "gopls was not found in PATH"}
	}
	installs := 0
	a := requirementsApp(t, nil, requirementTools(check, &installs))
	a.win.Process.Workspaces.register(a.win)
	a.run("workbench.extensions.action.showInstalledExtensions")
	waitForRequirements(t, a, func() bool { return a.ts.Exists(gluttertest.ByText("gopls  •  gopls was not found in PATH")) })
	if err := a.win.Settings.Update(context.Background(), "tools.goplsPath", "/opt/gopls", settings.TargetUser); err != nil {
		t.Fatal(err)
	}
	waitForRequirements(t, a, func() bool { return a.ts.Exists(gluttertest.ByText("gopls  •  /opt/gopls")) })
	mu.Lock()
	defer mu.Unlock()
	if runs != 2 {
		t.Errorf("gopls was checked %d times", runs)
	}
}

// settingsQuery is the text of the search box of the Settings editor of the window, or "" without one.
func settingsQuery(a *groupApp) string {
	for _, p := range a.shell().group.panes {
		if sp, ok := p.(*settingsPane); ok {
			return sp.ctl.ViewState().Query
		}
	}
	return ""
}

// waitForRequirements pumps until cond holds, or fails the test.
func waitForRequirements(t *testing.T, a *groupApp, cond func() bool) {
	t.Helper()
	for range 200 {
		if cond() {
			return
		}
		a.pump()
		time.Sleep(5 * time.Millisecond) // the checks run on goroutines of their own, in real time
	}
	t.Fatal("the condition did not come true")
}
