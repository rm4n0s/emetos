package terminal

import (
	"github.com/rm4n0s/glutter/foundation"
	"github.com/rm4n0s/glutter/rendering"
	"github.com/rm4n0s/glutter/services"
	"github.com/rm4n0s/glutter/widgets"
)

// paneWidget is the widget of a terminal in a pane: the grid, with the keyboard, the pointer and the input method going to the
// terminal. The focus node and the state of the pane live in the paneView, so the widget can be built again without losing the
// focus or the selection.
func paneWidget(pv *paneView) widgets.Widget {
	return widgets.NewBuilder(func(bc widgets.BuildContext) widgets.Widget {
		pv.setContext(bc)
		grid := widgets.NewMouseRegion(
			widgets.NewListener(newGridWidget(pv), widgets.ListenerOpts{
				Behavior:        rendering.HitTestBehaviorOpaque,
				OnPointerDown:   pv.onPointerDown,
				OnPointerMove:   pv.onPointerMove,
				OnPointerUp:     pv.onPointerUp,
				OnPointerHover:  pv.onPointerHover,
				OnPointerSignal: pv.onPointerSignal,
			}),
			widgets.MouseRegionOpts{Cursor: services.SystemMouseCursors.Text})
		return widgets.NewFocus(grid, widgets.FocusOpts{
			FocusNode: pv.node, OnKeyEvent: pv.onKey, OnFocusChange: pv.focusChanged, DebugLabel: "Terminal",
			Autofocus: false, SkipTraversal: foundation.Some(false),
		})
	})
}
