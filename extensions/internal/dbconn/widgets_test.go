package dbconn

import (
	"testing"

	"github.com/rm4n0s/glutter/foundation"
	"github.com/rm4n0s/glutter/gluttertest"
	gui "github.com/rm4n0s/glutter/ui"
	"github.com/rm4n0s/glutter/widgets"

	"github.com/rm4n0s/emetos/extension"
	"github.com/rm4n0s/emetos/extension/ui"
	"github.com/rm4n0s/emetos/extension/ui/uitest"
)

// viewWidget builds the widget of the view as the box does.
func viewWidget(r *rig) widgets.Widget {
	return widgets.NewBuilder(func(bc widgets.BuildContext) widgets.Widget { return r.api.View(ViewID).Content.Build(bc) })
}

// pumpView shows the view in a window of the dark theme.
func pumpView(t *testing.T, r *rig, w, h float64) *gluttertest.Tester {
	t.Helper()
	return uitest.Pump(t, uitest.Themes()[0], gui.Size{Width: w, Height: h}, viewWidget(r))
}

// formWidget builds the widget of the form tab.
func formWidget(f *formTab) widgets.Widget {
	return widgets.NewBuilder(func(bc widgets.BuildContext) widgets.Widget { return f.build(bc) })
}

// hasText tells whether a text is on screen.
func hasText(ts *gluttertest.Tester, text string) bool {
	return ts.Exists(gluttertest.ByTextContaining(text))
}

// inputAt is the input box number i of a form, in the order of the fields.
func inputAt(i int) gluttertest.Finder { return gluttertest.At(gluttertest.ByType[*ui.InputBox](), i) }

// refreshTab is an editor tab whose Refresh builds the widget under test again, as the window does with the box of a tab.
type refreshTab struct {
	extension.EditorTab
	changed *foundation.ChangeNotifier
}

// Refresh implements extension.EditorTab.
func (t refreshTab) Refresh() {
	t.EditorTab.Refresh()
	t.changed.NotifyListeners()
}

// showForm shows the form of a tab in a window; the form builds again when the tab is refreshed.
func showForm(t *testing.T, f *formTab, w, h float64) *gluttertest.Tester {
	t.Helper()
	changed := foundation.NewChangeNotifier()
	f.tab = refreshTab{f.tab, changed}
	body := widgets.NewListenableBuilder(changed, func(bc widgets.BuildContext, _ widgets.Widget) widgets.Widget { return f.build(bc) }, nil)
	return uitest.Pump(t, uitest.Themes()[0], gui.Size{Width: w, Height: h}, body)
}
