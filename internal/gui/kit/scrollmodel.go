package kit

import (
	"github.com/rm4n0s/glutter/foundation"
	"github.com/rm4n0s/glutter/widgets"
)

// ScrollControllerModel is the ScrollModel of Glutter scroll views: it reads the sizes and the
// position of the scroll views that use a vertical and a horizontal ScrollController, moves them
// with the controllers, and tells its listeners when a controller's position changes. A
// ScrollableElement makes one for the controllers in its options; make one yourself for a Model.
//
// A controller that has no scroll view (yet), or that is nil, has nothing to scroll: its axis
// has no content.
type ScrollControllerModel struct {
	foundation.ChangeNotifier
	vertical, horizontal widgets.ScrollController
	handles              [2]foundation.ListenerHandle
	// reverse is set when the vertical view scrolls against the reading direction (a list whose end is at offset 0): the position
	// of the scrollbars counts from the other end.
	reverse bool
}

// NewScrollControllerModel makes a model of the controllers of a vertical and of a horizontal
// scroll view; either may be nil.
func NewScrollControllerModel(vertical, horizontal widgets.ScrollController) *ScrollControllerModel {
	m := &ScrollControllerModel{ChangeNotifier: *foundation.NewChangeNotifier(), vertical: vertical, horizontal: horizontal}
	if vertical != nil {
		m.handles[0] = vertical.AddListener(m.NotifyListeners)
	}
	if horizontal != nil {
		m.handles[1] = horizontal.AddListener(m.NotifyListeners)
	}
	return m
}

// Dispose stops following the controllers.
func (m *ScrollControllerModel) Dispose() {
	if m.vertical != nil {
		m.vertical.RemoveListener(m.handles[0])
	}
	if m.horizontal != nil {
		m.horizontal.RemoveListener(m.handles[1])
	}
}

// axisMetrics reads one controller: the viewport, the content and the position. A controller
// with no scroll view, or one that has not been laid out, has nothing.
func axisMetrics(c widgets.ScrollController) (viewport, content, position float64, ok bool) {
	if c == nil || !c.HasClients() {
		return 0, 0, 0, false
	}
	p := c.Positions()[0]
	if !p.HasViewportDimension() || !p.HasContentDimensions() || !p.HasPixels() {
		return 0, 0, 0, false
	}
	viewport = p.ViewportDimension()
	return viewport, viewport + p.MaxScrollExtent() - p.MinScrollExtent(), p.Pixels() - p.MinScrollExtent(), true
}

// Metrics implements ScrollModel.
func (m *ScrollControllerModel) Metrics() ScrollMetrics {
	var out ScrollMetrics
	out.ViewportHeight, out.ContentHeight, out.Top, _ = axisMetrics(m.vertical)
	if m.reverse {
		out.Top = max(out.ContentHeight-out.ViewportHeight-out.Top, 0)
	}
	out.ViewportWidth, out.ContentWidth, out.Left, _ = axisMetrics(m.horizontal)
	return out
}

// SetReverse tells the model that the vertical view scrolls against the reading direction (ListViewOpts.Reverse): its position is
// the distance from the end of the content, so the model turns it round for the scrollbar.
func (m *ScrollControllerModel) SetReverse(reverse bool) {
	if m.reverse != reverse {
		m.reverse = reverse
		m.NotifyListeners()
	}
}

// jump moves a controller to a position within its content.
func jump(c widgets.ScrollController, position float64) {
	if c == nil || !c.HasClients() {
		return
	}
	p := c.Positions()[0]
	if !p.HasContentDimensions() {
		return
	}
	target := min(max(p.MinScrollExtent()+position, p.MinScrollExtent()), p.MaxScrollExtent())
	if p.HasPixels() && target == p.Pixels() {
		return
	}
	c.JumpTo(target)
}

// ScrollTo implements ScrollModel.
func (m *ScrollControllerModel) ScrollTo(left, top float64) {
	if m.reverse {
		if viewport, content, _, ok := axisMetrics(m.vertical); ok {
			top = max(content-viewport-top, 0)
		}
	}
	jump(m.vertical, top)
	jump(m.horizontal, left)
}
