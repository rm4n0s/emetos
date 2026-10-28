//go:build emetos_display

package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/rm4n0s/emetos/internal/gui/app"
	"github.com/rm4n0s/emetos/internal/testenv/display"
)

// The settings editor, the keybindings editor and the theme picker (step 17b.5).

// userFile reads a file of the user's configuration folder of the run.
func userFile(a *display.App, name string) string {
	data, err := os.ReadFile(filepath.Join(filepath.Dir(a.Project), "config", "emetos", name))
	if err != nil {
		return ""
	}
	return string(data)
}

func TestDesktopSettingsEditor(t *testing.T) {
	a := display.Start(t, display.Options{Args: []string{".", "app.go"}, Project: navProject})
	a.Settle()
	a.Press("ctrl+comma")
	a.Settle()
	a.Type("minimap enabled")
	a.WaitFocusedText("minimap enabled")
	a.WaitText("1 Setting Found")
	// a check box
	checkbox, ok := textWhere(a, "", inRect(560, 300, 700, 360))
	if !ok {
		t.Fatalf("no check box on screen: %s", a.Dump())
	}
	a.Click(checkbox.X+8, checkbox.Y+8)
	a.Eventually("the setting to be written", func() (bool, string) {
		got := userFile(a, "settings.json")
		return strings.Contains(got, `"editor.minimap.enabled": false`), got
	})
	shot(t, a, "settings-checkbox")
	a.Click(checkbox.X+8, checkbox.Y+8)
	a.Eventually("the check box to set it back", func() (bool, string) {
		got := userFile(a, "settings.json")
		return !strings.Contains(got, `"editor.minimap.enabled": false`), got
	})
	// a number typed in its box
	search := a.Inputs()[0]
	a.ClickInput(search)
	a.Press("ctrl+a")
	a.Type("tab size")
	a.WaitFocusedText("tab size")
	a.Eventually("the Tab Size setting", func() (bool, string) {
		_, ok := a.Text("Tab Size")
		return ok, a.Dump()
	})
	shot(t, a, "settings-tabsize")
	var box app.DevInput
	for _, in := range a.Inputs() {
		if in.Y > 200 && in.Text == "4" {
			box, ok = in, true
		}
	}
	if !ok {
		t.Fatalf("no number box holding 4: %+v", a.Inputs())
	}
	a.ClickInput(box)
	a.Press("ctrl+a")
	a.Type("2")
	a.WaitFocusedText("2")
	a.Press("Enter")
	a.Eventually("tab size 2 to be written", func() (bool, string) {
		got := userFile(a, "settings.json")
		return strings.Contains(got, `"editor.tabSize": 2`), got
	})
	shot(t, a, "settings-tabsize-set")
}

func TestDesktopKeybindingsEditor(t *testing.T) {
	a := display.Start(t, display.Options{Args: []string{".", "app.go"}, Project: navProject})
	a.Settle()
	a.Press("ctrl+k", "ctrl+s")
	a.Settle()
	a.Type("toggle word wrap")
	a.WaitFocusedText("toggle word wrap")
	a.Eventually("the command in the list", func() (bool, string) { _, ok := a.Text("View: Toggle Word Wrap"); return ok, a.Dump() })
	shot(t, a, "keybindings-search")
	// a double click on the row asks for a key
	row, _ := a.Text("View: Toggle Word Wrap")
	a.DoubleClick(row.X+10, row.Y+row.Height/2)
	a.Eventually("the box for the key", func() (bool, string) {
		_, ok := textWhere(a, "Press desired key combination and then press ENTER.", func(app.DevText) bool { return true })
		return ok, a.Dump()
	})
	shot(t, a, "keybindings-define")
	a.Press("ctrl+alt+w")
	a.Settle()
	shot(t, a, "keybindings-recorded")
	a.Press("Enter")
	a.Eventually("the keybinding to be written", func() (bool, string) {
		got := userFile(a, "keybindings.json")
		return strings.Contains(got, "ctrl+alt+w") && strings.Contains(got, "editor.action.toggleWordWrap"), got
	})
	shot(t, a, "keybindings-set")
}

func TestDesktopThemePicker(t *testing.T) {
	a := display.Start(t, display.Options{Args: []string{".", "app.go"}, Project: navProject})
	a.Settle()
	a.Press("ctrl+k", "ctrl+t")
	a.WaitText("Dark Modern Default Dark")
	shot(t, a, "themes")
	a.Press("Down")
	a.Settle()
	shot(t, a, "themes-preview")
	// the theme under the cursor is previewed: the settings do not change until Enter
	if got := userFile(a, "settings.json"); strings.Contains(got, "workbench.colorTheme") {
		t.Errorf("a preview wrote the theme: %s", got)
	}
	a.Press("Enter")
	a.Eventually("the theme to be written", func() (bool, string) {
		got := userFile(a, "settings.json")
		return strings.Contains(got, "workbench.colorTheme"), got
	})
	shot(t, a, "themes-set")
}
