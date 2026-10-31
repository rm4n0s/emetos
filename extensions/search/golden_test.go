package search

import (
	"testing"
	"time"

	"github.com/rm4n0s/glutter/gluttertest"
	gui "github.com/rm4n0s/glutter/ui"
	"github.com/rm4n0s/glutter/widgets"

	"github.com/rm4n0s/emetos/extension/ui"
	"github.com/rm4n0s/emetos/extension/ui/uitest"
)

// widgetOf builds the widget of the view as the box does.
func widgetOf(r *rig) widgets.Widget {
	return widgets.NewBuilder(func(bc widgets.BuildContext) widgets.Widget { return r.api.View(ViewID).Content.Build(bc) })
}

// TestGoldenTheSearchView looks at the view in both themes: the results as a list and as a tree, the replace box with the details,
// and the messages.
func TestGoldenTheSearchView(t *testing.T) {
	cases := []struct {
		name  string
		setup func(r *rig)
	}{
		{"list", func(r *rig) { r.find(map[string]any{"query": "needle"}) }},
		{"tree", func(r *rig) {
			r.find(map[string]any{"query": "needle"})
			r.ext.setTreeView(true)
		}},
		{"replace", func(r *rig) {
			if err := r.api.Run(ReplaceInFilesCommand, map[string]any{"query": "needle", "replace": "pin", "filesToInclude": "*.go,*.sh", "isCaseSensitive": true}); err != nil {
				t.Fatal(err)
			}
			r.wait()
		}},
		{"noresults", func(r *rig) { r.find(map[string]any{"query": "zzzzz", "filesToExclude": "docs"}) }},
		{"empty", func(r *rig) {}},
	}
	for _, th := range uitest.Themes() {
		for _, c := range cases {
			t.Run(th.Name+"_"+c.name, func(t *testing.T) {
				r := newRig(t, project)
				c.setup(r)
				ts := uitest.Pump(t, th, gui.Size{Width: 340, Height: 400}, widgetOf(r))
				ts.Pump(50 * time.Millisecond)
				uitest.MatchesGolden(ts, "search_"+c.name+"_"+th.Name)
			})
		}
	}
}

// decorated gives the results the file decorations of Git: main.go modified, internal/db/db.go untracked, scripts/run.sh ignored (a color and no
// badge) and the folder internal/db changed (the dot, in the tree).
func (r *rig) decorated(child widgets.Widget) widgets.Widget {
	files := map[string]ui.FixedFileDecoration{}
	for _, f := range r.ext.res.files {
		switch f.path {
		case "main.go":
			files[f.uri] = ui.FixedFileDecoration{Letter: "M", Tooltip: "Modified", Color: "gitDecoration.modifiedResourceForeground", Weight: 12, Bubble: true}
		case "internal/db/db.go":
			files[f.uri] = ui.FixedFileDecoration{Letter: "U", Tooltip: "Untracked", Color: "gitDecoration.untrackedResourceForeground", Weight: 11, Bubble: true}
		case "scripts/run.sh":
			files[f.uri] = ui.FixedFileDecoration{Color: "gitDecoration.ignoredResourceForeground", Weight: 13}
		}
	}
	return ui.NewFixedFileDecorations(files, child)
}

// TestGoldenTheSearchResultsDecorated looks at the results with the decorations of Git, as a list and as a tree.
func TestGoldenTheSearchResultsDecorated(t *testing.T) {
	for _, th := range uitest.Themes() {
		for _, tree := range []bool{false, true} {
			name := "list"
			if tree {
				name = "tree"
			}
			t.Run(th.Name+"_"+name, func(t *testing.T) {
				r := newRig(t, project)
				r.find(map[string]any{"query": "needle"})
				if tree {
					r.ext.setTreeView(true)
				}
				ts := uitest.Pump(t, th, gui.Size{Width: 340, Height: 260}, r.decorated(widgetOf(r)))
				ts.Pump(50 * time.Millisecond)
				uitest.MatchesGolden(ts, "search_decorated_"+name+"_"+th.Name)
			})
		}
	}
}

// TestTheSearchDecorationSettingsAreFollowed checks that search.decorations.colors and .badges turn the color and the letter of a result off.
func TestTheSearchDecorationSettingsAreFollowed(t *testing.T) {
	for _, tc := range []struct {
		name           string
		colors, badges bool
	}{{"both", true, true}, {"colors only", true, false}, {"badges only", false, true}, {"neither", false, false}} {
		t.Run(tc.name, func(t *testing.T) {
			r := newRig(t, project)
			r.api.SetSetting(settingDecorationColors, tc.colors)
			r.api.SetSetting(settingDecorationBadges, tc.badges)
			r.find(map[string]any{"query": "needle"})
			ts := uitest.Pump(t, uitest.Themes()[0], gui.Size{Width: 340, Height: 260}, r.decorated(widgetOf(r)))
			ts.Pump(50 * time.Millisecond)
			letters := len(ts.Elements(gluttertest.ByText("M"))) + len(ts.Elements(gluttertest.ByText("U")))
			if (letters > 0) != tc.badges {
				t.Errorf("letters on screen = %d, badges setting %v", letters, tc.badges)
			}
		})
	}
}
