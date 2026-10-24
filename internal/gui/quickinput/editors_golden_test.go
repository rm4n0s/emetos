package quickinput

import (
	"path/filepath"
	"testing"

	"github.com/rm4n0s/glutter/services"
	"github.com/rm4n0s/glutter/ui"

	"github.com/rm4n0s/emetos/internal/core/theme"
	"github.com/rm4n0s/emetos/internal/gui/testutil"
)

// withIcons gives the entries of a model the Seti icon of their file.
func withIcons(o EditorsOptions) EditorsOptions {
	th, err := theme.BundledFileIconTheme("vs-seti")
	if err != nil {
		panic(err)
	}
	entries := o.Entries
	o.Entries = func(mode EditorsMode) []EditorEntry {
		out := entries(mode)
		for i := range out {
			name := out[i].Label
			out[i].FileIcon = func(typ theme.Type) (theme.FileIcon, bool) {
				lang := map[string]string{".go": "go", ".md": "markdown", ".json": "json"}[filepath.Ext(name)]
				return th.IconFor(theme.FileIconQuery{Kind: theme.FileIconFile, Name: name, LanguageID: lang, Type: typ})
			}
		}
		return out
	}
	return o
}

func TestGoldenEditorPicker(t *testing.T) {
	cases := []struct {
		name    string
		model   func() *editorsModel
		prepare func(r *rig)
	}{
		// the switcher of Ctrl+Tab: no input, the editor used before the active one is active; an editor with
		// unsaved changes has a dot and a preview editor is in italics
		{"switcher", func() *editorsModel {
			m := newEditorsModel().group(1, []string{"main.go", "server.go", "util.go", "config.json", "README.md"},
				"server.go", "main.go", "README.md", "config.json", "util.go")
			m.dirty["main.go"] = true
			m.preview["README.md"] = true
			return m
		}, func(r *rig) {
			r.ts.SendKeyDownEvent(services.LogicalKeyboardKeyControl)
			r.show("edt active ", ShowOptions{QuickNavigate: ctrlTab})
		}},
		// all the editors by appearance with the input and a group heading for each group
		{"groups", func() *editorsModel {
			m := newEditorsModel().
				group(1, []string{"main.go", "server.go"}, "server.go", "main.go").
				group(2, []string{"README.md", "config.json"})
			m.dirty["server.go"] = true
			m.preview["config.json"] = true
			return m
		}, func(r *rig) { r.show("edt ", ShowOptions{}) }},
		{"filtered", func() *editorsModel {
			return newEditorsModel().
				group(1, []string{"main.go", "server.go", "util.go"}).
				group(2, []string{"main_test.go", "README.md"})
		}, func(r *rig) { r.show("edt main", ShowOptions{PreserveValue: true}) }},
	}
	for _, th := range goldenThemes {
		for _, tc := range cases {
			t.Run(th.name+" "+tc.name, func(t *testing.T) {
				m := tc.model()
				r := newRig(t, ui.Size{Width: 720, Height: 360}, th.colors())
				for _, mode := range []EditorsMode{EditorsByAppearance, EditorsByRecentlyUsed, EditorsInActiveGroup} {
					r.registry.MustRegister(NewEditorsProvider(mode, withIcons(m.options())))
				}
				tc.prepare(r)
				testutil.MatchesGolden(r.ts, "quickinput_editors_"+tc.name+"_"+th.name)
			})
		}
	}
}
