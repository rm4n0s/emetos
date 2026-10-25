package kit

import (
	"math"
	"slices"
	"time"

	"github.com/rm4n0s/glutter/foundation"
	"github.com/rm4n0s/glutter/rendering"
	"github.com/rm4n0s/glutter/ui"
	"github.com/rm4n0s/glutter/widgets"
)

// SplitOrientation says along which axis the views of a SplitView lie.
type SplitOrientation int

const (
	// SplitVertical stacks the views from top to bottom; the sashes between them are horizontal
	// lines (VS Code's `Orientation.VERTICAL`, which is its default).
	SplitVertical SplitOrientation = iota
	// SplitHorizontal puts the views side by side; the sashes between them are vertical lines.
	SplitHorizontal
)

// SplitPane is a view of a SplitView: the widget in it with its size, limits and priority along
// the axis of the split view. The size across the axis is the split view's.
type SplitPane struct {
	// Child is what the pane shows; it gets the pane's size as its constraints.
	Child widgets.Widget
	// Size is the size the pane starts with (and, for a Hidden pane, comes back with when it is
	// shown). The split view fits the sizes to its own when it is laid out; a pane with the High
	// priority and a size of 0 takes what the others leave.
	Size float64
	// MinSize is the least size of the pane; the default is 0.
	MinSize float64
	// MaxSize is the greatest size of the pane; 0 is no limit.
	MaxSize float64
	// Priority says in which order the panes take up or give back space when the split view is
	// resized, if the split view is not proportional: High first, Low last.
	Priority LayoutPriority
	// Snap hides the pane when a sash is dragged past half of its minimum size, and shows it again
	// when dragged back. Hide reports it through OnDidChange (a size of 0).
	Snap bool
	// PreferredSize is the size a double click on a sash next to the pane gives it; 0 is none.
	PreferredSize float64
	// Hidden keeps the pane out of sight and out of the layout, but alive (its state is kept). A
	// change of Hidden in a new SplitView shows or hides the pane.
	Hidden bool
}

func (p SplitPane) item() SplitItem {
	return SplitItem{Size: p.Size, Hidden: p.Hidden, MinSize: p.MinSize, MaxSize: p.MaxSize, Priority: p.Priority, Snap: p.Snap, PreferredSize: p.PreferredSize}
}

// SplitViewOpts configure a SplitView.
type SplitViewOpts struct {
	// Orientation is the axis of the views; the default is SplitVertical.
	Orientation SplitOrientation
	// ProportionalLayout keeps the panes in proportion to each other when the split view is
	// resized, instead of giving the change to the panes by priority. VS Code's split view is
	// proportional unless told not to; this one is not, so that the editor area of the workbench
	// absorbs the resizes of the window.
	ProportionalLayout bool
	// InverseAltBehavior makes a drag with Alt held the ordinary one, and a drag without Alt the
	// one that resizes the pane on both sides alike.
	InverseAltBehavior bool
	// Scrollbar says when the scrollbar shows for the panes that cannot all fit (their least sizes
	// add up to more than the split view); the default is ScrollbarAuto.
	Scrollbar ScrollbarVisibility
	// SashSize is the width of a vertical sash and the height of a horizontal one; the default is
	// DefaultSashSize.
	SashSize foundation.Opt[float64]
	// SeparatorBorder, when it is not transparent, draws a 1 px line of that color at the start of
	// every pane but the first (VS Code's `ISplitViewStyles.separatorBorder`).
	SeparatorBorder ui.Color
	// OnDidChange is called with the sizes of the panes (0 for a hidden one) after they changed:
	// when a drag ends, a sash is reset, the controller or a new SplitView changes a pane, or the
	// split view is laid out for a new size. It is called after the frame, not during a drag, and
	// not for a change that gives the same sizes.
	OnDidChange func(sizes []float64)
	// OnDidSashReset replaces what a double click on the sash at an index does (the sash after the
	// pane at that index): it is called instead of giving the panes their preferred sizes. It is
	// not called for the sash of a hidden pane that snaps.
	OnDidSashReset func(index int)
	// Controller reads and changes the sizes from outside.
	Controller *SplitViewController
}

// SplitViewController reads and changes the sizes of one SplitView from outside it. Its methods
// belong to the UI thread; before the split view is laid out they report the sizes of the panes.
type SplitViewController struct {
	state *splitViewState
}

// NewSplitViewController makes a controller; give it to the opts of one SplitView.
func NewSplitViewController() *SplitViewController { return &SplitViewController{} }

// Sizes are the sizes of the panes now, 0 for a hidden one.
func (c *SplitViewController) Sizes() []float64 {
	if c.state == nil {
		return nil
	}
	return c.state.sizes()
}

// ContentSize is the sum of the sizes of the panes; it is more than the size of the split view
// when the panes do not fit.
func (c *SplitViewController) ContentSize() float64 {
	if c.state == nil || c.state.model == nil {
		return 0
	}
	return c.state.model.ContentSize()
}

// SetSize sets the size of a pane (within its limits); the other panes give or take the
// difference by priority.
func (c *SplitViewController) SetSize(index int, size float64) {
	if c.state != nil {
		c.state.apply(func(m *SplitModel) { m.ResizeView(index, size) })
	}
}

// SetVisible shows or hides a pane; the others take up or give back its space. A pane comes back
// with the size it had.
func (c *SplitViewController) SetVisible(index int, visible bool) {
	if c.state != nil {
		c.state.apply(func(m *SplitModel) { m.SetViewVisible(index, visible) })
	}
}

// IsVisible tells whether a pane is shown.
func (c *SplitViewController) IsVisible(index int) bool {
	return c.state != nil && c.state.model != nil && c.state.model.IsViewVisible(index)
}

// CachedVisibleSize is the size a hidden pane comes back with; ok is false for a shown pane.
func (c *SplitViewController) CachedVisibleSize(index int) (size float64, ok bool) {
	if c.state == nil || c.state.model == nil {
		return 0, false
	}
	return c.state.model.CachedVisibleSize(index)
}

// Distribute shares the whole size equally among the panes that can be resized.
func (c *SplitViewController) Distribute() {
	if c.state != nil {
		c.state.apply(func(m *SplitModel) { m.DistributeViewSizes() })
	}
}

// IsExpanded tells whether all the other panes are at their least size.
func (c *SplitViewController) IsExpanded(index int) bool {
	return c.state != nil && c.state.model != nil && c.state.model.IsViewExpanded(index)
}

// SplitView lays out panes in a line along one axis, each with its own size, limits and layout
// priority, and puts a Sash between each two so that the user can resize them. Dragging a sash
// moves the boundary, the panes on both sides give and take within their limits (the neighbours
// first, then the ones beyond, as VS Code does), and a double click on a sash gives the panes
// next to it their preferred sizes. When the split view itself is resized, the panes take or give
// back the difference by priority (the pane with the High priority first), or keep their
// proportions. When the panes cannot all fit, the split view scrolls.
//
// Ported from VS Code: src/vs/base/browser/ui/splitview/splitview.ts, src/vs/base/browser/ui/sash/sash.ts
// (the layout is in SplitModel). Not ported: orthogonal and linked sashes, moving and swapping
// views, and adding or removing panes while the split view lives (a SplitView with another number
// of panes starts over from the sizes of the panes it is given).
type SplitView struct {
	widgets.StatefulWidget
	panes []SplitPane
	opts  SplitViewOpts
}

// NewSplitView makes a split view of panes.
func NewSplitView(panes []SplitPane, opts ...SplitViewOpts) *SplitView {
	w := &SplitView{StatefulWidget: widgets.NewStatefulWidgetBase(), panes: panes}
	if len(opts) > 0 {
		w.opts = opts[0]
	}
	return w
}

// CreateState implements widgets.StatefulWidget.
func (w *SplitView) CreateState() widgets.State { return &splitViewState{} }

// splitScroll is the ScrollModel of the panes that do not fit: the position along the axis of the
// split view, kept within the content.
type splitScroll struct {
	foundation.ChangeNotifier
	state *splitViewState
}

// Metrics implements ScrollModel.
func (m *splitScroll) Metrics() ScrollMetrics {
	s := m.state
	if s.model == nil {
		return ScrollMetrics{}
	}
	size, content := s.model.Size(), s.model.ContentSize()
	if s.Widget().opts.Orientation == SplitHorizontal {
		return ScrollMetrics{ViewportWidth: size, ContentWidth: content, Left: s.scroll}
	}
	return ScrollMetrics{ViewportHeight: size, ContentHeight: content, Top: s.scroll}
}

// ScrollTo implements ScrollModel.
func (m *splitScroll) ScrollTo(left, top float64) {
	s := m.state
	position := top
	if s.Widget().opts.Orientation == SplitHorizontal {
		position = left
	}
	if s.model != nil {
		position = min(max(position, 0), max(s.model.ContentSize()-s.model.Size(), 0))
	}
	if position == s.scroll {
		return
	}
	s.scroll = position
	m.NotifyListeners()
	s.SetState(func() {})
}

type splitViewState struct {
	widgets.StateOf[*SplitView]
	binding  *widgets.WidgetsBinding
	model    *SplitModel
	scroller *splitScroll
	// scroll is how far the panes are scrolled along the axis.
	scroll float64
	// reported are the sizes OnDidChange was last told of (or the first layout is compared to).
	reported []float64
	// notifying is whether a callback is waiting for the end of the frame.
	notifying bool
	// alt is whether Alt was held at the last event of the drag.
	alt bool
}

// InitState reads the binding and the controller.
func (s *splitViewState) InitState() {
	s.binding = widgets.WidgetsBindingOf(s.Context())
	s.scroller = &splitScroll{ChangeNotifier: *foundation.NewChangeNotifier(), state: s}
	if c := s.Widget().opts.Controller; c != nil {
		c.state = s
	}
	s.reported = s.initialSizes(s.Widget().panes)
}

// Dispose lets go of the controller.
func (s *splitViewState) Dispose() {
	if c := s.Widget().opts.Controller; c != nil && c.state == s {
		c.state = nil
	}
}

func (s *splitViewState) initialSizes(panes []SplitPane) []float64 {
	out := make([]float64, len(panes))
	for i, p := range panes {
		if !p.Hidden {
			out[i] = p.Size
		}
	}
	return out
}

// DidUpdateWidget follows new panes: other limits and visibility change the model; another number
// of panes, or another way of laying them out, starts over.
func (s *splitViewState) DidUpdateWidget(old *SplitView) {
	n := s.Widget()
	if old.opts.Controller != n.opts.Controller {
		if old.opts.Controller != nil && old.opts.Controller.state == s {
			old.opts.Controller.state = nil
		}
		if n.opts.Controller != nil {
			n.opts.Controller.state = s
		}
	}
	if s.model == nil {
		s.reported = s.initialSizes(n.panes)
		return
	}
	if len(old.panes) != len(n.panes) || old.opts.Orientation != n.opts.Orientation ||
		old.opts.ProportionalLayout != n.opts.ProportionalLayout || old.opts.InverseAltBehavior != n.opts.InverseAltBehavior {
		s.model = nil
		s.scroll = 0
		s.reported = s.initialSizes(n.panes)
		return
	}
	for i, p := range n.panes {
		if p.Hidden != old.panes[i].Hidden {
			s.model.SetViewVisible(i, !p.Hidden)
		}
	}
	for i, p := range n.panes {
		if c := s.model.Constraints(i).constraints(); c != p.item().constraints() {
			s.model.UpdateView(i, p.item(), math.NaN())
		}
	}
}

// constraints is the item without its sizes, to compare the limits.
func (it SplitItem) constraints() SplitItem {
	return SplitItem{MinSize: it.MinSize, MaxSize: it.MaxSize, Priority: it.Priority, Snap: it.Snap, PreferredSize: it.PreferredSize}
}

func (s *splitViewState) sizes() []float64 {
	if s.model == nil {
		return s.initialSizes(s.Widget().panes)
	}
	return s.model.Sizes()
}

// apply changes the model from outside and rebuilds.
func (s *splitViewState) apply(change func(m *SplitModel)) {
	if s.model == nil || !s.Mounted() {
		return
	}
	change(s.model)
	s.SetState(func() {})
	s.notify()
}

// notify tells OnDidChange of new sizes after the frame.
func (s *splitViewState) notify() {
	if s.notifying || s.model == nil || s.model.Dragging() {
		return
	}
	if slices.Equal(s.model.Sizes(), s.reported) {
		return
	}
	s.notifying = true
	scheduler := s.binding.Scheduler()
	scheduler.ScheduleFrame()
	scheduler.AddPostFrameCallback(func(time.Duration) {
		s.notifying = false
		if !s.Mounted() || s.model == nil || s.model.Dragging() {
			return
		}
		sizes := s.model.Sizes()
		if slices.Equal(sizes, s.reported) {
			return
		}
		s.reported = sizes
		if f := s.Widget().opts.OnDidChange; f != nil {
			f(slices.Clone(sizes))
		}
	})
}

// layout makes the model if there is none, and lays it out for a size. A size that is not positive (a
// window that has none yet, or one that is minimized) squeezes nothing: squeezing the panes into no
// room would leave them at their least sizes when the room comes. The first model then has the
// panes at the sizes they were given (but not below their least) and lays out for their sum, which
// the first real size grows or shrinks by priority; a model that has been laid out is left as it is.
func (s *splitViewState) layout(size float64) {
	w := s.Widget()
	if s.model == nil {
		items := make([]SplitItem, len(w.panes))
		var sum float64
		for i, p := range w.panes {
			items[i] = p.item()
			if size <= 0 && !p.Hidden {
				items[i].Size = clampSize(p.Size, p.MinSize, maxOrInf(p.MaxSize))
				sum += items[i].Size
			}
		}
		if size <= 0 {
			size = sum
		}
		s.model = NewSplitModel(items, size, SplitModelOpts{ProportionalLayout: w.opts.ProportionalLayout, InverseAltBehavior: w.opts.InverseAltBehavior})
		s.model.Layout(size)
	} else if size != s.model.Size() && size > 0 {
		s.model.Layout(size)
	}
	s.scroll = min(max(s.scroll, 0), max(s.model.ContentSize()-s.model.Size(), 0))
	s.notify()
}

func (s *splitViewState) horizontal() bool { return s.Widget().opts.Orientation == SplitHorizontal }

// along is the coordinate of an event along the axis of the split view.
func (s *splitViewState) along(x, y float64) float64 {
	if s.horizontal() {
		return x
	}
	return y
}

func (s *splitViewState) sashStart(index int, e SashEvent) {
	if s.model == nil {
		return
	}
	s.alt = e.Alt
	s.model.SashStart(index, s.along(e.StartX, e.StartY), e.Alt)
}

func (s *splitViewState) sashChange(e SashEvent) {
	if s.model == nil || !s.model.Dragging() {
		return
	}
	if e.Alt != s.alt {
		s.alt = e.Alt
		s.model.SashSetAlt(e.Alt)
	}
	s.model.SashChange(s.along(e.CurrentX, e.CurrentY))
	s.SetState(func() {})
}

func (s *splitViewState) sashEnd() {
	if s.model == nil {
		return
	}
	s.model.SashEnd()
	s.SetState(func() {})
	s.notify()
}

func (s *splitViewState) sashReset(index int) {
	if s.model == nil || !s.model.CanResetSash(index) {
		return
	}
	if f := s.Widget().opts.OnDidSashReset; f != nil {
		f(index)
		return
	}
	s.apply(func(m *SplitModel) { m.ResetSash(index) })
}

// Build implements widgets.State.
func (s *splitViewState) Build(widgets.BuildContext) widgets.Widget {
	return widgets.NewLayoutBuilder(func(_ widgets.BuildContext, c rendering.BoxConstraints) widgets.Widget {
		w := s.Widget()
		total := c.MaxHeight
		if s.horizontal() {
			total = c.MaxWidth
		}
		if math.IsInf(total, 0) {
			total = sumSizes(s.initialSizes(w.panes))
			if s.model != nil {
				total = s.model.ContentSize()
			}
		}
		s.layout(total)
		return s.content()
	})
}

// maxOrInf is a limit of a pane, where 0 is none.
func maxOrInf(max float64) float64 {
	if max <= 0 {
		return math.Inf(1)
	}
	return max
}

func sumSizes(sizes []float64) float64 {
	var total float64
	for _, v := range sizes {
		total += v
	}
	return total
}

// content is the panes, the separators and the sashes, in a scrollable element.
func (s *splitViewState) content() widgets.Widget {
	w, m := s.Widget(), s.model
	horizontal := s.horizontal()
	sashSize := w.opts.SashSize.OrElse(DefaultSashSize)
	span := func(child widgets.Widget, at, size float64) widgets.Widget {
		if horizontal {
			return widgets.NewPositioned(child, widgets.PositionedOpts{
				Left: foundation.Some(at - s.scroll), Width: foundation.Some(size), Top: foundation.Some(0.0), Bottom: foundation.Some(0.0)})
		}
		return widgets.NewPositioned(child, widgets.PositionedOpts{
			Top: foundation.Some(at - s.scroll), Height: foundation.Some(size), Left: foundation.Some(0.0), Right: foundation.Some(0.0)})
	}
	var children []widgets.Widget
	for i, p := range w.panes {
		visible := m.IsViewVisible(i)
		children = append(children, span(widgets.NewOffstage(p.Child, widgets.OffstageOpts{Offstage: foundation.Some(!visible)}), m.Offset(i), m.ViewSize(i)))
	}
	if w.opts.SeparatorBorder.Alpha() != 0 {
		for i := 1; i < len(w.panes); i++ {
			if m.IsViewVisible(i) {
				children = append(children, span(widgets.NewIgnorePointer(widgets.NewColoredBox(w.opts.SeparatorBorder, widgets.NewSizedBox(nil))), m.Offset(i), 1))
			}
		}
	}
	orientation := SashHorizontal
	if horizontal {
		orientation = SashVertical
	}
	for i, state := range m.SashStates() {
		children = append(children, span(NewSash(SashOpts{
			Orientation: orientation,
			Size:        foundation.Some(sashSize),
			State:       state,
			OnDidStart:  func(e SashEvent) { s.sashStart(i, e) },
			OnDidChange: s.sashChange,
			OnDidEnd:    s.sashEnd,
			OnDidReset:  func() { s.sashReset(i) },
		}), m.SashPosition(i)-sashSize/2, sashSize))
	}
	stack := widgets.NewStack(children, widgets.StackOpts{Fit: rendering.StackFitExpand})
	bars := ScrollableElementOpts{Model: s.scroller, Vertical: ScrollbarHidden, Horizontal: ScrollbarHidden, HandleMouseWheel: foundation.Some(true)}
	if horizontal {
		bars.Horizontal = w.opts.Scrollbar
	} else {
		bars.Vertical = w.opts.Scrollbar
	}
	return NewScrollableElement(stack, bars)
}
