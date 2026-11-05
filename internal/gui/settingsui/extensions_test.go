package settingsui

import (
	"strings"
	"testing"

	"github.com/rm4n0s/glutter/gluttertest"
	"github.com/rm4n0s/glutter/ui"

	"github.com/rm4n0s/emetos/internal/core/editorgroup"
	"github.com/rm4n0s/emetos/internal/core/settings"
	"github.com/rm4n0s/emetos/internal/gui/kit"
)

// The rows of the extensions (step 31.1, SPEC §10.15).

var rigExtensions = []settings.ExtensionInfo{
	{ID: "emetos.git", Name: "Git", Description: "Source control with git."},
	{ID: "emetos.terminal", Name: "Terminal", Description: "Your shell in the panel."},
}

func extRow(id string) gluttertest.Finder {
	return rowOf(settings.InstalledExtensionsID, settings.ExtensionRowPrefix+id)
}

func TestTheCheckBoxOfAnExtensionTurnsItOffAndOn(t *testing.T) {
	r := newRig(t, rigOpts{extensions: rigExtensions, userText: "{\n  \"extensions.disabled\": [\"other.ext\"]\n}\n"})
	r.show("@ext:emetos.git")
	row := extRow("emetos.git")
	if !r.ts.Exists(row) || r.ts.Exists(extRow("emetos.terminal")) {
		t.Fatal("@ext:emetos.git does not show the row of Git alone")
	}
	checked := func() bool { return r.ts.Widget(in[*kit.Checkbox](row)).(*kit.Checkbox).Checked() }
	modified := func() bool { return r.ts.Exists(gluttertest.Descendant(row, r.indicator())) }
	if !checked() || modified() {
		t.Fatalf("Git: checked %v, modified %v", checked(), modified())
	}
	// off: the id joins extensions.disabled, the row is modified
	r.ts.Tap(in[*kit.Checkbox](row))
	r.ts.Pump(pump)
	if got := r.read(r.userPath); !strings.Contains(got, `"emetos.git"`) || !strings.Contains(got, `"other.ext"`) {
		t.Errorf("the file is %q", got)
	}
	if checked() || !modified() {
		t.Errorf("after the click: checked %v, modified %v", checked(), modified())
	}
	// Reset Setting turns it on again
	rect := r.rect(row)
	r.ts.Hover(ui.Offset{Dx: rect.Left + 100, Dy: rect.Top + 10})
	r.ts.Pump(pump)
	r.ts.Tap(in[*kit.IconButton](row))
	r.ts.Pump(pump)
	r.ts.Tap(gluttertest.ByText(menuCopyIDLabel))
	r.ts.Pump(pump)
	r.ts.Hover(ui.Offset{Dx: rect.Left + 100, Dy: rect.Top + 10})
	r.ts.Pump(pump)
	r.ts.Tap(in[*kit.IconButton](row))
	r.ts.Pump(pump)
	r.ts.Tap(gluttertest.ByText(menuResetLabel))
	r.ts.Pump(pump)
	if got := r.read(r.userPath); strings.Contains(got, `"emetos.git"`) || !strings.Contains(got, `"other.ext"`) {
		t.Errorf("after Reset Setting the file is %q", got)
	}
	if !checked() || modified() {
		t.Errorf("after Reset Setting: checked %v, modified %v", checked(), modified())
	}
	// Copy Setting ID gives the search that finds the row
	if len(r.copied) != 1 || r.copied[0] != "@ext:emetos.git" {
		t.Errorf("copied %q", r.copied)
	}
	r.noErrors()
}

func TestTheRowsFollowAChangeOfTheFile(t *testing.T) {
	r := newRig(t, rigOpts{extensions: rigExtensions})
	r.show("@ext:emetos.terminal")
	row := extRow("emetos.terminal")
	r.write(r.userPath, "{\n  \"extensions.disabled\": [\"emetos.terminal\"]\n}\n")
	if _, err := r.store.Reload(t.Context()); err != nil {
		t.Fatal(err)
	}
	r.ts.FlushPosted()
	r.ts.Pump(pump)
	if r.ts.Widget(in[*kit.Checkbox](row)).(*kit.Checkbox).Checked() {
		t.Error("the Terminal is still checked after extensions.disabled lists it")
	}
}

func TestTheWorkspaceTabHasNoExtensionRows(t *testing.T) {
	r := newRig(t, rigOpts{extensions: rigExtensions, hasFolder: true})
	r.ctl.SetTarget(editorgroup.SettingsWorkspace)
	r.show("git")
	if r.ts.Exists(extRow("emetos.git")) {
		t.Error("the Workspace tab lists the row of Git")
	}
}
