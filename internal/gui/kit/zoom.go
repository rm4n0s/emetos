package kit

import (
	"math"

	"github.com/rm4n0s/glutter/foundation"
	"github.com/rm4n0s/glutter/painting"
	"github.com/rm4n0s/glutter/rendering"
	"github.com/rm4n0s/glutter/ui"
	"github.com/rm4n0s/glutter/widgets"
)

// ZoomOpts are the options of NewZoom.
type ZoomOpts struct {
	// Key is the key of the widget.
	Key foundation.Key
}

// Zoom scales everything below it by a factor, the way the zoom of VS Code's window does (SPEC §10.9): the
// child is laid out in an area of the size of the available room divided by the factor and painted
// scaled by it, so that at the factor 1.2 it has 20% less room in logical pixels and everything in it
// is 20% larger on the screen. Painting, hit testing and the positions of the overlays inside it follow the
// same transform; text is drawn from its outlines under the transform, so it stays sharp. The
// MediaQuery below it reports the size divided by the factor and a device pixel ratio multiplied by it,
// which is what code that reads the size of the window or snaps lines to physical pixels needs.
//
// The structure of the tree does not depend on the factor (1 is a transform too), so the state of the child
// is kept when the factor changes. The factor is the caller's: settings.ZoomFactor makes it from a zoom level.
// A factor that is not a positive number is 1.
type Zoom struct {
	widgets.StatelessWidget
	factor float64
	child  widgets.Widget
}

// NewZoom makes a Zoom of a child at a factor.
func NewZoom(factor float64, child widgets.Widget, opts ...ZoomOpts) *Zoom {
	var o ZoomOpts
	if len(opts) > 0 {
		o = opts[0]
	}
	if !(factor > 0) { // also true for NaN
		factor = 1
	}
	return &Zoom{StatelessWidget: widgets.NewStatelessWidgetBase(widgets.WidgetOpts{Key: o.Key}), factor: factor, child: child}
}

// Factor is the scale of the child.
func (z *Zoom) Factor() float64 { return z.factor }

// Build implements widgets.Widget.
func (z *Zoom) Build(bc widgets.BuildContext) widgets.Widget {
	media, hasMedia := widgets.MediaQueryMaybeOf(bc)
	f := z.factor
	return widgets.NewLayoutBuilder(func(_ widgets.BuildContext, c rendering.BoxConstraints) widgets.Widget {
		inner := scaledConstraints(c, f)
		child := z.child
		if hasMedia {
			size := ui.Size{Width: inner.MaxWidth, Height: inner.MaxHeight}
			if !inner.HasBoundedWidth() || !inner.HasBoundedHeight() {
				size = ui.Size{Width: media.Size.Width / f, Height: media.Size.Height / f}
			}
			child = widgets.NewMediaQuery(media.CopyWith(widgets.MediaQueryDataCopyOpts{
				Size:             foundation.Some(size),
				DevicePixelRatio: foundation.Some(media.DevicePixelRatio * f),
			}), child)
		}
		// The overflow box takes the room of its parent and gives the child its smaller one; the transform
		// scales what the child paints back to the room. A transform does not check its own bounds when it is
		// hit, so the area outside the child's unscaled size (all of the room, when f is above 1) is reached
		// through the box, which is as large as the room.
		return widgets.NewOverflowBox(
			widgets.NewTransformScale(child, widgets.TransformScaleOpts{
				TransformOpts: widgets.TransformOpts{Alignment: painting.AlignmentTopLeft},
				Scale:         foundation.Some(f),
			}),
			widgets.OverflowBoxOpts{
				Alignment: painting.AlignmentTopLeft,
				MinWidth:  foundation.Some(inner.MinWidth), MaxWidth: foundation.Some(inner.MaxWidth),
				MinHeight: foundation.Some(inner.MinHeight), MaxHeight: foundation.Some(inner.MaxHeight),
			})
	})
}

// scaledConstraints divides constraints by a factor; an unbounded side stays unbounded.
func scaledConstraints(c rendering.BoxConstraints, f float64) rendering.BoxConstraints {
	div := func(v float64) float64 {
		if math.IsInf(v, 1) {
			return v
		}
		return v / f
	}
	return rendering.NewBoxConstraints(rendering.BoxConstraintsOpts{
		MinWidth: foundation.Some(div(c.MinWidth)), MaxWidth: foundation.Some(div(c.MaxWidth)),
		MinHeight: foundation.Some(div(c.MinHeight)), MaxHeight: foundation.Some(div(c.MaxHeight)),
	})
}

// GlobalToOverlay turns a position in the coordinates of the window (where the pointer events are) into the
// coordinates of the root overlay above a context, where the menus, the hovers and the other overlays
// are laid out. They are the same until a Zoom is above the overlay; then a position of the window is the
// position of the overlay times the factor. A context with no overlay above it, or one that has not been laid
// out, gets the position as it is.
func GlobalToOverlay(bc widgets.BuildContext, global ui.Offset) ui.Offset {
	overlay, ok := widgets.OverlayMaybeOf(bc, widgets.OverlayOfOpts{RootOverlay: true})
	if !ok {
		return global
	}
	box, ok := overlay.Context().FindRenderObject().(rendering.Box)
	if !ok || box == nil || !box.HasSize() {
		return global
	}
	return box.GlobalToLocal(global, nil)
}
