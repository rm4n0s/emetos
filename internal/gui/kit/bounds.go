package kit

import (
	"github.com/rm4n0s/glutter/rendering"
	"github.com/rm4n0s/glutter/ui"
	"github.com/rm4n0s/glutter/widgets"
)

// Bounds remembers where a widget is in the window, for code that has to find the widget under a
// pointer (a drop target): a BoundsProbe keeps one up to date.
type Bounds struct {
	ctx widgets.BuildContext
}

// Rect is the place of the widget in the window's coordinates; false while the widget is not in
// the tree or has not been laid out.
func (b *Bounds) Rect() (ui.Rect, bool) {
	if b == nil || b.ctx == nil || !b.ctx.Mounted() {
		return ui.Rect{}, false
	}
	box, ok := b.ctx.FindRenderObject().(rendering.Box)
	if !ok || box == nil || !box.HasSize() {
		return ui.Rect{}, false
	}
	at := box.LocalToGlobal(ui.OffsetZero, nil)
	size := box.Size()
	return ui.NewRectFromLTWH(at.Dx, at.Dy, size.Width, size.Height), true
}

// boundsProbe is the widget of NewBoundsProbe.
type boundsProbe struct {
	widgets.StatefulWidget
	child  widgets.Widget
	bounds *Bounds
}

// NewBoundsProbe makes a widget that looks like its child and tells a Bounds where it is.
func NewBoundsProbe(child widgets.Widget, bounds *Bounds) widgets.Widget {
	return &boundsProbe{StatefulWidget: widgets.NewStatefulWidgetBase(), child: child, bounds: bounds}
}

// CreateState implements widgets.StatefulWidget.
func (p *boundsProbe) CreateState() widgets.State { return &boundsProbeState{} }

type boundsProbeState struct {
	widgets.StateOf[*boundsProbe]
}

// InitState attaches the bounds.
func (s *boundsProbeState) InitState() { s.Widget().bounds.ctx = s.Context() }

// DidUpdateWidget follows a new bounds.
func (s *boundsProbeState) DidUpdateWidget(old *boundsProbe) {
	if old.bounds != s.Widget().bounds {
		if old.bounds.ctx == s.Context() {
			old.bounds.ctx = nil
		}
		s.Widget().bounds.ctx = s.Context()
	}
}

// Dispose lets go of the bounds.
func (s *boundsProbeState) Dispose() {
	if b := s.Widget().bounds; b.ctx == s.Context() {
		b.ctx = nil
	}
}

// Build implements widgets.State.
func (s *boundsProbeState) Build(widgets.BuildContext) widgets.Widget { return s.Widget().child }
