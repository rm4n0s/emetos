package editor

import (
	"github.com/rm4n0s/glutter/foundation"
	"github.com/rm4n0s/glutter/rendering"
	"github.com/rm4n0s/glutter/widgets"
)

// zoneKey keys the widget of a view zone, so that it keeps its state while the editor scrolls.
type zoneKey ZoneID

// zoneLayerMargin is how many rows beyond the viewport get the widgets of their zones.
const zoneLayerMargin = 20

// newZoneLayer is the widgets of the view zones that are on the screen, over the text (SPEC §9.1):
// each is as wide as the text area and as high as the rows of its zone, and moves with the vertical
// scroll. The layer is built again when the editor scrolls.
//
// The layer is a stack of Positioned widgets, built again for every scroll offset and only for the
// zones near the viewport (the editor's own layers are render objects that read the offset when they
// paint; a render object that places children by the offset would do without the rebuild, which the
// few zones with a widget do not call for).
func newZoneLayer(vm *ViewModel, offset rendering.ViewportOffset) widgets.Widget {
	return widgets.NewListenableBuilder(offset, func(_ widgets.BuildContext, _ widgets.Widget) widgets.Widget {
		return widgets.NewLayoutBuilder(func(_ widgets.BuildContext, c rendering.BoxConstraints) widgets.Widget {
			return widgets.NewStack(zoneChildren(vm, offset, c.MaxHeight), widgets.StackOpts{Fit: rendering.StackFitExpand})
		})
	}, nil)
}

// zoneChildren positions the widgets of the zones that have rows in a viewport of a height.
func zoneChildren(vm *ViewModel, offset rendering.ViewportOffset, height float64) []widgets.Widget {
	if len(vm.zones.byID) == 0 {
		return nil
	}
	proj := vm.Projection()
	top := vm.ScrollTop()
	if offset.HasPixels() {
		top = offset.Pixels()
	}
	if height <= 0 || height > 1e6 {
		_, height = vm.ViewportSize()
	}
	var out []widgets.Widget
	for _, id := range proj.ZonesIn(proj.RowAt(top)-zoneLayerMargin, proj.RowAt(top+height)+zoneLayerMargin) {
		e := vm.zones.byID[id]
		first, count := proj.ZoneRows(id)
		if e == nil || e.spec.Widget == nil || count == 0 {
			continue
		}
		out = append(out, widgets.NewPositioned(widgets.NewClipRect(e.spec.Widget), widgets.PositionedOpts{
			Key:  foundation.NewValueKey(zoneKey(id)),
			Left: foundation.Some(0.0), Right: foundation.Some(0.0),
			Top: foundation.Some(proj.RowTop(first) - top), Height: foundation.Some(float64(count) * proj.RowHeight(first)),
		}))
	}
	return out
}
