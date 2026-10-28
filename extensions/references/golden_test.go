package references

import (
	"testing"

	gui "github.com/rm4n0s/glutter/ui"
	"github.com/rm4n0s/glutter/widgets"

	"github.com/rm4n0s/emetos/extension/ui/uitest"
)

// widgetOf builds the widget of the view as the box does.
func widgetOf(r *rig) widgets.Widget {
	return widgets.NewBuilder(func(bc widgets.BuildContext) widgets.Widget { return r.api.View(ViewID).Content.Build(bc) })
}

// TestGoldenTheResultsOfASearch looks at the view with the references of Greet, in both themes: the message over the tree, the
// files with their folders and counts, and the locations with the symbol marked; the declaration is selected.
func TestGoldenTheResultsOfASearch(t *testing.T) {
	for _, th := range uitest.Themes() {
		t.Run(th.Name, func(t *testing.T) {
			r := newRig(t)
			r.cursorAt("util.go", 3, "Greet")
			r.answer("references", r.greetings())
			r.run(FindReferences, nil)
			r.loaded()
			r.ext.view.tree.ExpandAll()
			ts := uitest.Pump(t, th, gui.Size{Width: 320, Height: 220}, widgetOf(r))
			uitest.MatchesGolden(ts, "references_results_"+th.Name)
		})
	}
}

// TestGoldenTheHistoryOfSearches looks at the empty view with the searches that were made, the newest first.
func TestGoldenTheHistoryOfSearches(t *testing.T) {
	for _, th := range uitest.Themes() {
		t.Run(th.Name, func(t *testing.T) {
			r := newRig(t)
			r.answer("references", r.greetings())
			r.cursorAt("util.go", 3, "Greet")
			r.run(FindReferences, nil)
			r.cursorAt("main.go", 4, "main")
			r.run(FindReferences, nil)
			r.run(Clear, nil)
			ts := uitest.Pump(t, th, gui.Size{Width: 400, Height: 120}, widgetOf(r))
			uitest.MatchesGolden(ts, "references_history_"+th.Name)
		})
	}
}

// TestGoldenTheEmptyView looks at the view before any search.
func TestGoldenTheEmptyView(t *testing.T) {
	for _, th := range uitest.Themes() {
		t.Run(th.Name, func(t *testing.T) {
			r := newRig(t)
			ts := uitest.Pump(t, th, gui.Size{Width: 320, Height: 60}, widgetOf(r))
			uitest.MatchesGolden(ts, "references_empty_"+th.Name)
		})
	}
}
