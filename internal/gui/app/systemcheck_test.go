package app

import (
	"slices"
	"strings"
	"testing"

	"github.com/rm4n0s/glutter/gluttertest"

	"github.com/rm4n0s/emetos/extension"
	"github.com/rm4n0s/emetos/internal/core/command"
	"github.com/rm4n0s/emetos/internal/core/editorgroup"
	"github.com/rm4n0s/emetos/internal/gui/extensionsui"
)

// The page "System Check" (step 34.5): the command and the menu row, the groups of the enabled extensions, the turned-off ones left
// out, Copy Report, Check Again, the Extensions page's Check System button, and the page coming back with the window.

// redisLike is a turned-off extension of the tests with one requirement.
func redisLike() extension.Extension {
	return &testExtension{manifest: extension.Manifest{
		ID: "emetos.redisx", Name: "Redis X", ActivationEvents: []string{extension.OnStartup},
		Requirements: []extension.Requirement{{ID: "keyring", Name: "Keyring", Kind: extension.RequirementService, Purpose: "secrets",
			Check: reqAnswer(extension.RequirementResult{State: extension.RequirementMissing, Detail: "no keyring"})}},
	}}
}

func TestTheSystemCheckPage(t *testing.T) {
	installs := 0
	a := requirementsApp(t, map[string]any{"extensions.disabled": []any{"emetos.redisx"}},
		requirementTools(reqAnswer(extension.RequirementResult{State: extension.RequirementMissing, Detail: "gopls was not found in PATH"}), &installs), redisLike)
	if _, ok := a.win.Commands.Lookup("emetos.checkSystem"); !ok {
		t.Fatal("emetos.checkSystem is not a command")
	}
	if !slices.Contains(entries(a.win, command.MenubarHelpMenu), "emetos.checkSystem") {
		t.Error("the Help menu has no Check System row")
	}
	a.run("emetos.checkSystem")
	if a.active() != "System Check" {
		t.Fatalf("the active tab is %q", a.active())
	}
	waitForRequirements(t, a, func() bool {
		return a.ts.Exists(gluttertest.ByText("2 missing, 1 cannot be used, 1 not checked, 1 found"))
	})
	for _, want := range []string{"System: Test OS 1, fedora, linux/amd64", "Missing or cannot be used (3)", "Not checked (1)", "Found (1)",
		"1 turned-off extension is not checked; see the Extensions page.", "Tools  •  language features"} {
		if !a.ts.Exists(gluttertest.ByText(want)) {
			t.Errorf("the page does not show %q", want)
		}
	}
	if a.ts.Exists(gluttertest.ByTextContaining("Keyring")) {
		t.Error("the requirement of a turned-off extension is shown")
	}
	// the same buttons as on the Extensions page
	a.ts.Tap(gluttertest.ByKey(extensionsui.RequirementButtonKey("emetos.tools", "gopls", extensionsui.ActionCopyInstall)))
	a.pump()
	if text, _ := a.ts.ClipboardText(); text != "sudo dnf install gopls" {
		t.Errorf("Copy Install Command copied %q", text)
	}
	// Copy Report copies the Markdown of the page
	a.ts.Tap(gluttertest.ByKey(extensionsui.CopyReportKey()))
	a.pump()
	report, _ := a.ts.ClipboardText()
	if !strings.HasPrefix(report, "# Emetos System Check\n\nSystem: Test OS 1, fedora, linux/amd64\n\n2 missing, 1 cannot be used, 1 not checked, 1 found\n") ||
		!strings.Contains(report, "| gopls | Tools | missing |  |  | gopls was not found in PATH |") || !strings.HasSuffix(report, "1 turned-off extension is not checked.\n") {
		t.Errorf("the report is\n%s", report)
	}
	// Check Again shows the checks running, then their results
	a.ts.Tap(gluttertest.ByKey(extensionsui.SystemCheckAgainKey()))
	waitForRequirements(t, a, func() bool {
		return a.ts.Exists(gluttertest.ByText("2 missing, 1 cannot be used, 1 not checked, 1 found"))
	})
	// the command again reveals the page
	a.run("workbench.extensions.action.showInstalledExtensions")
	a.run("emetos.checkSystem")
	count := 0
	for _, n := range a.tabNames() {
		if n == "System Check" {
			count++
		}
	}
	if count != 1 || a.active() != "System Check" {
		t.Errorf("tabs %v, active %q", a.tabNames(), a.active())
	}
}

func TestTheExtensionsPageOpensTheSystemCheck(t *testing.T) {
	installs := 0
	a := requirementsApp(t, nil, requirementTools(reqAnswer(extension.RequirementResult{State: extension.RequirementFound}), &installs))
	a.run("workbench.extensions.action.showInstalledExtensions")
	a.ts.Tap(gluttertest.ByKey(extensionsui.CheckSystemKey()))
	a.pump()
	if a.active() != "System Check" {
		t.Errorf("the active tab is %q", a.active())
	}
}

func TestTheSystemCheckPageIsAnEditorKindThatRestores(t *testing.T) {
	var found bool
	for _, k := range editorKinds() {
		if k.typeID == editorgroup.SystemCheckInputTypeID {
			found = true
			in, ok := k.restore("{}")
			if !ok || in.TypeID() != editorgroup.SystemCheckInputTypeID || k.icon == 0 || k.newPane == nil {
				t.Errorf("kind = %+v, restored %v %v", k, in, ok)
			}
			if _, ok := k.restore("null"); ok {
				t.Error("an invalid saved page came back")
			}
		}
	}
	if !found {
		t.Error("the System Check page is not an editor kind")
	}
}

func TestTheSystemCheckPageComesBackAfterARestart(t *testing.T) {
	env := newStateEnv(t, nil)
	first := env.start(env.root)
	first.command("emetos.checkSystem", nil)
	has := func(r *stateRun) bool {
		for _, in := range r.shell().activeGroup().Model.Editors(editorgroup.Sequential, false) {
			if _, ok := in.(*editorgroup.SystemCheckInput); ok {
				return true
			}
		}
		return false
	}
	if !has(first) {
		t.Fatal("the page did not open")
	}
	first.close()
	second := env.start(env.root)
	if !has(second) {
		t.Error("the page did not come back")
	}
}

func TestSystemLine(t *testing.T) {
	plist := `<dict><key>ProductName</key><string>macOS</string><key>ProductVersion</key>
	<string>14.6.1</string></dict>`
	tests := []struct {
		name, goos, osRelease, plist, family, want string
	}{
		{"Fedora", "linux", "NAME=\"Fedora Linux\"\nPRETTY_NAME=\"Fedora Linux 45 (Workstation Edition)\"\n", "", "fedora", "Fedora Linux 45 (Workstation Edition), fedora, linux/amd64"},
		{"NAME without PRETTY_NAME", "linux", "NAME='Void'\n", "", "", "Void, linux/amd64"},
		{"no os-release", "linux", "", "", "", "linux, linux/amd64"},
		{"macOS", "darwin", "", plist, "macos", "macOS 14.6.1, macos, darwin/amd64"},
		{"macOS without the file", "darwin", "", "", "macos", "macOS, macos, darwin/amd64"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := systemLine(tt.goos, "amd64", tt.osRelease, tt.plist, tt.family); got != tt.want {
				t.Errorf("systemLine = %q, want %q", got, tt.want)
			}
		})
	}
}
