package search

import (
	"testing"
	"time"

	gui "github.com/rm4n0s/glutter/ui"
	"github.com/rm4n0s/glutter/widgets"

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
