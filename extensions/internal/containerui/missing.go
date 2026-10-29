package containerui

import (
	"strings"

	"github.com/rm4n0s/glutter/foundation"
	"github.com/rm4n0s/glutter/painting"
	"github.com/rm4n0s/glutter/rendering"
	gui "github.com/rm4n0s/glutter/ui"
	"github.com/rm4n0s/glutter/widgets"

	"github.com/rm4n0s/emetos/extension/ui"
)

// gui_Color is the color type of Glutter's ui package.
type gui_Color = gui.Color

// bold is a style in a heavier weight.
func bold(s *painting.TextStyle) *painting.TextStyle {
	return s.CopyWith(painting.TextStyleOpts{FontWeight: gui.FontWeightW600})
}

// missingView is what the Containers section shows when no engine answers (SPEC §10.20 "Which engine"): the reason, one line for
// each place that was asked, and a Retry button. It is not an error box: Docker not running is a state, and starting it is
// what the user does next.
func (e *ext) missingView(bc widgets.BuildContext) widgets.Widget {
	colors := ui.Theme(bc)
	lines := strings.Split(strings.TrimSpace(e.reason), "\n")
	body := []widgets.Widget{
		widgets.NewText(e.cfg.Engine+" is not available", widgets.TextOpts{Style: bold(ui.TextStyle(bc, 13, colors.Foreground))}),
		vgap(6),
	}
	for _, l := range lines {
		body = append(body, widgets.NewText(l, widgets.TextOpts{Style: ui.TextStyle(bc, 12, colors.DescriptionForeground)}))
	}
	body = append(body, vgap(10), ui.NewButton("Retry", ui.ButtonOpts{Small: true, OnPressed: e.retry}))
	return widgets.NewPadding(painting.NewEdgeInsetsAll(12), ui.NewScrollView(
		widgets.NewColumn(body, widgets.ColumnOpts{CrossAxisAlignment: rendering.CrossAxisAlignmentStart}), &e.missingScroll))
}

// noteState is the first-run note.
type noteState struct {
	// seen says the user dismissed it.
	seen bool
}

// firstRunNote is the note at the top of the Containers section about what the access to the engine means. "Got it" keeps it away
// (it is saved with the state of the view).
func (e *ext) firstRunNote(bc widgets.BuildContext) widgets.Widget {
	colors := ui.Theme(bc)
	body := widgets.NewColumn([]widgets.Widget{
		widgets.NewText(e.cfg.FirstRunNote, widgets.TextOpts{Style: ui.TextStyle(bc, 12, colors.EditorWidgetForeground)}),
		vgap(8),
		widgets.NewRow([]widgets.Widget{ui.NewButton("Got it", ui.ButtonOpts{Small: true, Secondary: true, OnPressed: e.dismissNote})},
			widgets.RowOpts{MainAxisSize: rendering.MainAxisSizeMin}),
	}, widgets.ColumnOpts{CrossAxisAlignment: rendering.CrossAxisAlignmentStart})
	return panelBox(colors, colors.ProblemsInfoIconForeground, widgets.NewPadding(painting.NewEdgeInsetsAll(10), body))
}

// dismissNote hides the first-run note for good.
func (e *ext) dismissNote() {
	e.note.seen = true
	e.refreshViews()
}

var _ = foundation.Some[float64]
