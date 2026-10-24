package settingsui

import (
	"testing"
	"time"

	"github.com/rm4n0s/glutter/services"
	"github.com/rm4n0s/glutter/ui"

	"github.com/rm4n0s/emetos/internal/core/editorgroup"
	"github.com/rm4n0s/emetos/internal/gui/kit"
	"github.com/rm4n0s/emetos/internal/gui/testutil"
)

var goldenThemes = []struct {
	name   string
	colors func() kit.Colors
}{
	{"dark", kit.DarkModern},
	{"light", kit.LightModern},
}

var goldenSize = ui.Size{Width: 900, Height: 700}

// TestGoldenSettingsEditor looks at the settings editor in both themes: the top of the list with the modified rows
// (the mark at the left), a number, a string, enums, a value that is edited in the JSON file, the tabs and the
// tree of the groups; a number with a text that is not allowed; the workspace tab, which names the user's value;
// and the controls of the other kinds.
func TestGoldenSettingsEditor(t *testing.T) {
	for _, th := range goldenThemes {
		t.Run(th.name+" the top of the list", func(t *testing.T) {
			r := newRig(t, rigOpts{colors: th.colors(), size: goldenSize, userText: "{\n  \"editor.fontSize\": 16,\n  \"files.autoSave\": \"afterDelay\"\n}\n"})
			r.ts.Pump(pump)
			testutil.MatchesGolden(r.ts, "settings_top_"+th.name)
		})
		t.Run(th.name+" a number that is not allowed", func(t *testing.T) {
			r := newRig(t, rigOpts{colors: th.colors(), size: goldenSize, userText: "{\n  \"editor.tabSize\": 2\n}\n"})
			r.show("tab size")
			r.ts.EnterText(in[*kit.InputBox](rowOf("editor", "editor.tabSize")), "abc")
			r.press(services.LogicalKeyboardKeyEnter)
			// the caret blinks: the picture is taken less than half a second after the focus
			testutil.MatchesGolden(r.ts, "settings_invalid_"+th.name)
		})
		t.Run(th.name+" the workspace tab", func(t *testing.T) {
			r := newRig(t, rigOpts{
				colors: th.colors(), size: goldenSize, hasFolder: true,
				userText: "{\n  \"editor.tabSize\": 2,\n  \"editor.detectIndentation\": false\n}\n", wsText: "{\n  \"editor.detectIndentation\": true\n}\n",
				state: editorgroup.SettingsViewState{Target: editorgroup.SettingsWorkspace, Query: "tab size"},
			})
			r.ts.Pump(pump)
			testutil.MatchesGolden(r.ts, "settings_workspace_"+th.name)
		})
		t.Run(th.name+" a check box, a string and a link", func(t *testing.T) {
			r := newRig(t, rigOpts{colors: th.colors(), size: goldenSize, userText: "{\n  \"editor.insertSpaces\": false,\n  \"files.associations\": {\"*.x\": \"go\"}\n}\n"})
			r.show("files.associations")
			r.ts.Pump(pump)
			testutil.MatchesGolden(r.ts, "settings_link_"+th.name)
			r.show("editor.insertSpaces")
			// the pointer over a modified row shows the gear
			rect := r.rect(rowOf("editor", "editor.insertSpaces"))
			r.ts.Hover(ui.Offset{Dx: rect.Left + 200, Dy: rect.Top + 20})
			r.ts.Pump(2 * time.Millisecond)
			testutil.MatchesGolden(r.ts, "settings_bool_"+th.name)
		})
	}
}
