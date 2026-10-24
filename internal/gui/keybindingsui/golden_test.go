package keybindingsui

import (
	"testing"

	"github.com/rm4n0s/glutter/foundation"
	"github.com/rm4n0s/glutter/gluttertest"
	"github.com/rm4n0s/glutter/ui"

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

var goldenSize = ui.Size{Width: 900, Height: 520}

const goldenUserFile = `[
	{ "key": "ctrl+alt+d", "command": "editor.action.copyLinesDownAction" },
	{ "key": "ctrl+9", "command": "workbench.action.openSettings", "when": "editorFocus" }
]`

// TestGoldenKeybindingsEditor looks at the editor in both themes: the table with built-in and user keybindings,
// commands without a key and the odd rows; a search with highlights; the box that records a keybinding with the
// commands that have it; and a search that finds nothing.
func TestGoldenKeybindingsEditor(t *testing.T) {
	for _, th := range goldenThemes {
		t.Run(th.name+" the table", func(t *testing.T) {
			r := newRig(t, rigOpts{colors: th.colors(), size: goldenSize, userText: goldenUserFile})
			r.ts.Pump(pump)
			testutil.MatchesGolden(r.ts, "keybindings_table_"+th.name)
		})
		t.Run(th.name+" a search with highlights", func(t *testing.T) {
			r := newRig(t, rigOpts{colors: th.colors(), size: goldenSize, userText: goldenUserFile})
			r.search("copy line")
			r.tapRow("editor.action.copyLinesDownAction", "ctrl+alt+d")
			r.ts.Pump(pump)
			testutil.MatchesGolden(r.ts, "keybindings_search_"+th.name)
		})
		t.Run(th.name+" the id and the keys that matched", func(t *testing.T) {
			r := newRig(t, rigOpts{colors: th.colors(), size: goldenSize, userText: goldenUserFile})
			r.search("clipboardCut")
			r.ts.Pump(pump)
			testutil.MatchesGolden(r.ts, "keybindings_id_"+th.name)
			r.search(`ctrl x`)
			r.ts.Pump(pump)
			testutil.MatchesGolden(r.ts, "keybindings_keys_"+th.name)
		})
		t.Run(th.name+" nothing found", func(t *testing.T) {
			r := newRig(t, rigOpts{colors: th.colors(), size: goldenSize, userText: goldenUserFile})
			r.search("zzzz")
			r.ts.Pump(pump)
			testutil.MatchesGolden(r.ts, "keybindings_empty_"+th.name)
		})
		t.Run(th.name+" recording the search", func(t *testing.T) {
			r := newRig(t, rigOpts{colors: th.colors(), size: goldenSize, userText: goldenUserFile})
			r.ctl.RecordSearchKeys()
			r.press(letter('x'), ctrl)
			r.ts.Pump(pump)
			testutil.MatchesGolden(r.ts, "keybindings_recording_"+th.name)
		})
		t.Run(th.name+" defining a keybinding that others have", func(t *testing.T) {
			r := newRig(t, rigOpts{colors: th.colors(), size: goldenSize, userText: goldenUserFile})
			r.tapRow("editor.action.copyLinesUpAction", "")
			r.ctl.Define(false)
			r.press(letter('x'), ctrl)
			r.ts.Pump(pump)
			testutil.MatchesGolden(r.ts, "keybindings_define_"+th.name)
		})
		t.Run(th.name+" the dialog of the conflicts", func(t *testing.T) {
			r := newRig(t, rigOpts{colors: th.colors(), size: goldenSize, userText: goldenUserFile})
			r.tapRow("editor.action.copyLinesUpAction", "")
			r.ctl.Define(false)
			r.press(letter('x'), ctrl)
			r.press(enter)
			r.ts.Pump(pump)
			testutil.MatchesGolden(r.ts, "keybindings_conflicts_"+th.name)
		})
		t.Run(th.name+" the when-clause that is edited", func(t *testing.T) {
			r := newRig(t, rigOpts{colors: th.colors(), size: goldenSize, userText: goldenUserFile})
			r.tapRow("editor.action.copyLinesUpAction", "")
			r.ctl.DefineWhen()
			r.ts.Pump(pump)
			r.typeIn(gluttertest.ByKey(foundation.NewValueKey("keybindingsui-when")), "editorFocus &&")
			r.ts.Pump(pump)
			testutil.MatchesGolden(r.ts, "keybindings_when_"+th.name)
		})
	}
}
