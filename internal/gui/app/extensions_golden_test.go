package app

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/rm4n0s/glutter/gluttertest"
	"github.com/rm4n0s/glutter/ui"

	"github.com/rm4n0s/emetos/extension"
	"github.com/rm4n0s/emetos/internal/gui/testutil"
)

// TestGoldenExtensionsPage shows the page "Extensions" with an extension that is active, one that waits for its event, one
// that failed and one the user turned off, in both themes.
func TestGoldenExtensionsPage(t *testing.T) {
	for _, th := range goldenThemesOfThePanels {
		t.Run(th.name, func(t *testing.T) {
			greeter, _ := greeterFactory([]string{extension.OnStartup}, nil)
			idle := func() extension.Extension {
				return &testExtension{manifest: extension.Manifest{
					ID: "emetos.rustfmt", Name: "Rust Format", Version: "0.4.0", Description: "Formats Rust files with rustfmt.",
					ActivationEvents: []string{extension.OnLanguage("rust"), extension.OnFile("Cargo.toml")},
				}}
			}
			broken := func() extension.Extension {
				return &testExtension{
					manifest: extension.Manifest{ID: "emetos.broken", Name: "Broken Tools", Version: "2.0.1", Description: "Panics when it starts.", ActivationEvents: []string{extension.OnStartup}},
					activate: func(context.Context, extension.API) error { panic("boom") },
				}
			}
			off := func() extension.Extension {
				return &testExtension{manifest: extension.Manifest{
					ID: "emetos.linter", Name: "Linter", Version: "1.0.0", Description: "Shows lint results in the Problems panel.", ActivationEvents: []string{extension.OnStartup},
				}}
			}
			paths := testutil.Isolate(t)
			testutil.Settings(t, paths, map[string]any{
				"window.menuBarVisibility": "classic", "workbench.colorTheme": th.theme, "editor.minimap.enabled": false,
				"breadcrumbs.enabled": false, "files.hotExit": "off", "extensions.disabled": []any{"emetos.linter"},
			})
			folder := filepath.Join(t.TempDir(), "project")
			if err := os.MkdirAll(folder, 0o755); err != nil {
				t.Fatal(err)
			}
			p := newTestProcess(ProcessOptions{Paths: paths, Extensions: []extension.Factory{greeter, idle, broken, off}})
			win, err := NewWorkspace(context.Background(), p, folder)
			if err != nil {
				t.Fatal(err)
			}
			t.Cleanup(win.release)
			a := newGroupAppWith(t, win, ui.Size{Width: 960, Height: 560}, nil, nil)
			a.ts.SetPlatformConfiguration(func(c *ui.PlatformConfiguration) { c.UIFontFamily, c.UIFontSize = testutil.MonoFamily, 12 })
			a.run("workbench.action.toggleSidebarVisibility")
			a.run("workbench.extensions.action.showInstalledExtensions")
			win.Notify.ClearAll()
			// the text that was built in the frames around the change of the font is drawn in the fallback font: build it again
			a.shell().SetState(func() {})
			a.pump()
			testutil.MatchesGolden(a.ts, "extensions_page_"+th.name)
		})
	}
}

// TestGoldenExtensionsPageProblem shows the page "Extensions" with an extension that was left out because it declared
// privileges that only emetos.terminal may have, in both themes.
func TestGoldenExtensionsPageProblem(t *testing.T) {
	for _, th := range goldenThemesOfThePanels {
		t.Run(th.name, func(t *testing.T) {
			greeter, _ := greeterFactory([]string{extension.OnStartup}, nil)
			sneaky := func() extension.Extension {
				return &testExtension{manifest: extension.Manifest{
					ID: "ext.sneaky", Name: "Sneaky Tools", Version: "1.0.0", ActivationEvents: []string{extension.OnStartup},
					Privileges: []string{extension.PrivilegeCommandsExecute},
				}}
			}
			paths := testutil.Isolate(t)
			testutil.Settings(t, paths, map[string]any{
				"window.menuBarVisibility": "classic", "workbench.colorTheme": th.theme, "editor.minimap.enabled": false,
				"breadcrumbs.enabled": false, "files.hotExit": "off",
			})
			folder := filepath.Join(t.TempDir(), "project")
			if err := os.MkdirAll(folder, 0o755); err != nil {
				t.Fatal(err)
			}
			p := newTestProcess(ProcessOptions{Paths: paths, Extensions: []extension.Factory{greeter, sneaky}})
			win, err := NewWorkspace(context.Background(), p, folder)
			if err != nil {
				t.Fatal(err)
			}
			t.Cleanup(win.release)
			a := newGroupAppWith(t, win, ui.Size{Width: 960, Height: 420}, nil, nil)
			a.ts.SetPlatformConfiguration(func(c *ui.PlatformConfiguration) { c.UIFontFamily, c.UIFontSize = testutil.MonoFamily, 12 })
			a.run("workbench.action.toggleSidebarVisibility")
			a.run("workbench.extensions.action.showInstalledExtensions")
			win.Notify.ClearAll()
			a.shell().SetState(func() {})
			a.pump()
			testutil.MatchesGolden(a.ts, "extensions_page_problem_"+th.name)
		})
	}
}

// TestGoldenExtensionsPageRequirements shows the requirements on the page "Extensions" (step 34.4): found, a required one missing
// with its buttons, an optional one missing, one that cannot be used, one the check could not tell, and one still being checked, in
// both themes.
func TestGoldenExtensionsPageRequirements(t *testing.T) {
	for _, th := range goldenThemesOfThePanels {
		t.Run(th.name, func(t *testing.T) {
			installs := 0
			tools := requirementTools(reqAnswer(extension.RequirementResult{State: extension.RequirementMissing, Detail: "gopls was not found in PATH"}), &installs)
			slow := newGate(extension.RequirementResult{State: extension.RequirementFound})
			slowFactory := func() extension.Extension {
				return &testExtension{manifest: extension.Manifest{
					ID: "emetos.slow", Name: "Slow Engine", Version: "0.2.0", Description: "Its engine takes a while to answer.", ActivationEvents: []string{extension.OnStartup},
					Requirements: []extension.Requirement{{ID: "engine", Name: "Engine", Kind: extension.RequirementService, Purpose: "everything", Check: slow.check}},
				}}
			}
			paths := testutil.Isolate(t)
			testutil.Settings(t, paths, map[string]any{
				"window.menuBarVisibility": "classic", "workbench.colorTheme": th.theme, "editor.minimap.enabled": false,
				"breadcrumbs.enabled": false, "files.hotExit": "off",
			})
			folder := filepath.Join(t.TempDir(), "project")
			if err := os.MkdirAll(folder, 0o755); err != nil {
				t.Fatal(err)
			}
			p := newTestProcess(ProcessOptions{Paths: paths, Extensions: []extension.Factory{tools, slowFactory}, SystemFamily: extension.FamilyFedora})
			win, err := NewWorkspace(context.Background(), p, folder)
			if err != nil {
				t.Fatal(err)
			}
			t.Cleanup(win.release)
			a := newGroupAppWith(t, win, ui.Size{Width: 960, Height: 560}, nil, nil)
			a.ts.SetPlatformConfiguration(func(c *ui.PlatformConfiguration) { c.UIFontFamily, c.UIFontSize = testutil.MonoFamily, 12 })
			a.run("workbench.action.toggleSidebarVisibility")
			a.run("workbench.extensions.action.showInstalledExtensions")
			waitForRequirements(t, a, func() bool {
				return a.ts.Exists(gluttertest.ByText("gopls  •  gopls was not found in PATH")) && a.ts.Exists(gluttertest.ByText("Camera (optional)  •  the check took longer than 10s"))
			})
			win.Notify.ClearAll()
			a.shell().SetState(func() {})
			a.pump()
			testutil.MatchesGolden(a.ts, "extensions_page_requirements_"+th.name)
		})
	}
}

// TestGoldenSystemCheckPage shows the page "System Check" (step 34.5): the system line, the summary, the three groups with a required
// and an optional requirement missing, one that cannot be used, one the check could not tell and one found, and the line of the
// turned-off extensions, in both themes.
func TestGoldenSystemCheckPage(t *testing.T) {
	for _, th := range goldenThemesOfThePanels {
		t.Run(th.name, func(t *testing.T) {
			installs := 0
			tools := requirementTools(reqAnswer(extension.RequirementResult{State: extension.RequirementMissing, Detail: "gopls was not found in PATH"}), &installs)
			paths := testutil.Isolate(t)
			testutil.Settings(t, paths, map[string]any{
				"window.menuBarVisibility": "classic", "workbench.colorTheme": th.theme, "editor.minimap.enabled": false,
				"breadcrumbs.enabled": false, "files.hotExit": "off", "extensions.disabled": []any{"emetos.redisx"},
			})
			folder := filepath.Join(t.TempDir(), "project")
			if err := os.MkdirAll(folder, 0o755); err != nil {
				t.Fatal(err)
			}
			p := newTestProcess(ProcessOptions{Paths: paths, Extensions: []extension.Factory{tools, redisLike}, SystemFamily: extension.FamilyFedora,
				SystemName: "Fedora Linux 45 (Workstation Edition), fedora, linux/amd64"})
			win, err := NewWorkspace(context.Background(), p, folder)
			if err != nil {
				t.Fatal(err)
			}
			t.Cleanup(win.release)
			a := newGroupAppWith(t, win, ui.Size{Width: 960, Height: 720}, nil, nil)
			a.ts.SetPlatformConfiguration(func(c *ui.PlatformConfiguration) { c.UIFontFamily, c.UIFontSize = testutil.MonoFamily, 12 })
			a.run("workbench.action.toggleSidebarVisibility")
			a.run("emetos.checkSystem")
			waitForRequirements(t, a, func() bool {
				return a.ts.Exists(gluttertest.ByText("2 missing, 1 cannot be used, 1 not checked, 1 found"))
			})
			win.Notify.ClearAll()
			a.shell().SetState(func() {})
			a.pump()
			testutil.MatchesGolden(a.ts, "system_check_page_"+th.name)
		})
	}
}
