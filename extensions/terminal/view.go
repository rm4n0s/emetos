package terminal

import (
	"github.com/rm4n0s/glutter/foundation"
	"github.com/rm4n0s/glutter/gestures"
	"github.com/rm4n0s/glutter/rendering"
	"github.com/rm4n0s/glutter/services"
	"github.com/rm4n0s/glutter/widgets"
)

// paneWidget is the widget of a terminal in a pane: the grid, with the keyboard, the pointer and the input method going to the
// terminal, and over it the find widget and the tooltip of a link when they are shown. The focus node and the state of the pane
// live in the paneView, so the widget can be built again without losing the focus or the selection.
func paneWidget(pv *paneView) widgets.Widget {
	return widgets.NewStatefulBuilder(func(bc widgets.BuildContext, setState widgets.StateSetter) widgets.Widget {
		pv.setState = setState
		pv.setContext(bc)
		cursor := services.SystemMouseCursors.Text
		if pv.hover.link != nil && pv.hover.modDown || pv.sticky.hover {
			cursor = services.SystemMouseCursors.Click
		}
		grid := widgets.NewMouseRegion(
			widgets.NewListener(newGridWidget(pv), widgets.ListenerOpts{
				Behavior:        rendering.HitTestBehaviorOpaque,
				OnPointerDown:   pv.onPointerDown,
				OnPointerMove:   pv.onPointerMove,
				OnPointerUp:     pv.onPointerUp,
				OnPointerHover:  pv.onPointerHover,
				OnPointerSignal: pv.onPointerSignal,
			}),
			widgets.MouseRegionOpts{Cursor: cursor, OnExit: func(*gestures.PointerExitEvent) { pv.leaveHover() }})
		terminal := widgets.NewFocus(grid, widgets.FocusOpts{
			FocusNode: pv.node, OnKeyEvent: pv.onKey, OnFocusChange: pv.focusChanged, DebugLabel: "Terminal",
			Autofocus: false, SkipTraversal: foundation.Some(false),
		})
		layers := []widgets.Widget{terminal}
		// the find widget is beside the terminal's Focus, not inside it: its keys are the input's, not the shell's
		if pv.find.visible {
			layers = append(layers, widgets.NewPositioned(
				widgets.NewKeyedSubtree(pv.findWidget(bc), widgets.WidgetOpts{Key: foundation.NewValueKey("find")}),
				widgets.PositionedOpts{Top: foundation.Some(4.0), Right: foundation.Some(scrollbarW + 4)}))
		}
		layers = append(layers, pv.suggestLayers(bc)...)
		if tip := pv.tooltip(bc); tip != nil {
			layers = append(layers, tip)
		}
		return widgets.NewStack(layers, widgets.StackOpts{Fit: rendering.StackFitExpand})
	})
}
