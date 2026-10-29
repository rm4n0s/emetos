package kit

import (
	"math"

	"github.com/rm4n0s/glutter/rendering"
	"github.com/rm4n0s/glutter/widgets"
)

// IconButtonSize is the width and the height of an IconButton.
const IconButtonSize = iconButtonSize

// TitleActions are the actions of a view in the title strip of a part (the panel's): a widget that says how wide it is in the
// room it is offered, so that the strip can keep that room for it and give the tabs what is left, as VS Code measures the
// toolbar of the active view before it lays out the composite bar.
//
// Ported from VS Code: src/vs/workbench/browser/parts/paneCompositePart.ts (layoutCompositeBar and getToolbarWidth)
type TitleActions interface {
	widgets.Widget
	// TitleActionsWidth is the width the actions take when they are given at most room: never more than room, and 0 when they
	// show nothing in it. Built with a width tight to this, they show what that width means: asking again with the width it
	// returned gives the same width.
	TitleActionsWidth(room float64) float64
}

// IconButtonRow is a row of icon buttons for a title strip. It shows as many whole buttons as the room holds.
type IconButtonRow struct {
	widgets.StatelessWidget
	buttons []*IconButton
}

// NewIconButtonRow makes a row of the buttons, in order.
func NewIconButtonRow(buttons []*IconButton) *IconButtonRow {
	r := &IconButtonRow{buttons: buttons}
	r.StatelessWidget = widgets.NewStatelessWidgetBase()
	return r
}

// fit is how many buttons the room holds.
func (r *IconButtonRow) fit(room float64) int {
	if math.IsNaN(room) || room <= 0 {
		return 0
	}
	return int(math.Min(float64(len(r.buttons)), math.Floor(room/IconButtonSize)))
}

// TitleActionsWidth implements TitleActions.
func (r *IconButtonRow) TitleActionsWidth(room float64) float64 {
	return float64(r.fit(room)) * IconButtonSize
}

// Build implements widgets.StatelessWidget: the buttons that fit the width the row is given.
func (r *IconButtonRow) Build(bc widgets.BuildContext) widgets.Widget {
	return widgets.NewLayoutBuilder(func(_ widgets.BuildContext, cons rendering.BoxConstraints) widgets.Widget {
		children := make([]widgets.Widget, 0, len(r.buttons))
		for _, b := range r.buttons[:r.fit(cons.MaxWidth)] {
			children = append(children, b)
		}
		return widgets.NewRow(children, widgets.RowOpts{MainAxisSize: rendering.MainAxisSizeMin})
	})
}
