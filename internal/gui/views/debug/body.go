package debug

import (
	"github.com/rm4n0s/glutter/foundation"
	"github.com/rm4n0s/glutter/painting"
	"github.com/rm4n0s/glutter/rendering"
	"github.com/rm4n0s/glutter/ui"
	"github.com/rm4n0s/glutter/widgets"

	"github.com/rm4n0s/emetos/internal/gui/kit"
)

// Keys of the widgets of the views, which tests find them by.
var (
	// TreeKey is the key of the tree of a view, by the id of the view.
	TreeKeyPrefix = "debug:tree:"
	// MessageKey is the key of the text that stands in place of the tree of a view that has no rows.
	MessageKeyPrefix = "debug:message:"
)

// TreeKey is the key of the tree of the view with the id.
func TreeKey(viewID string) foundation.Key { return foundation.NewValueKey(TreeKeyPrefix + viewID) }

// MessageKey is the key of the message of the view with the id.
func MessageKey(viewID string) foundation.Key {
	return foundation.NewValueKey(MessageKeyPrefix + viewID)
}

// RowKey is the key of the widget of a row of the tree of a view, which tests find it by.
func RowKey(viewID, rowKey string) foundation.Key {
	return foundation.NewValueKey("debug:row:" + viewID + ":" + rowKey)
}

// TreePane is what a view shows as a tree.
type TreePane interface {
	ID() string
	Model() *kit.TreeModel
	Controller() *kit.TreeController
	Message() string
	OnChange(fn func()) func()
	Row(key string) (Row, bool)
	Render(bc widgets.BuildContext, n kit.TreeNode, st kit.RowState) widgets.Widget
	Editing() bool
}

// BodyOpts are the options of NewBody.
type BodyOpts struct {
	// Select is called when the user selects a row: a click, Enter. Open is called for Enter and a double click.
	Select func(Row)
	Open   func(Row)
	// Menu opens the context menu of a row at a position of the window; the row is zero for the part below the rows.
	Menu func(row Row, ok bool, at ui.Offset)
	// Actions gives the buttons of a row, shown while the pointer is over it or it is selected; RunAction runs one.
	Actions   func(Row) []kit.Action
	RunAction func(row Row, id string)
	// OnFocusChange is told when the tree gets or loses the keyboard focus.
	OnFocusChange func(has bool)
	// ToggleOnClick makes a click on an expandable row toggle it; the default of kit's tree.
	ToggleOnClick foundation.Opt[bool]
}

// Body is the body of a view: the tree of its pane, or the pane's message while it has no rows.
type Body struct {
	widgets.StatefulWidget
	pane TreePane
	opts BodyOpts
}

// NewBody makes the body of a pane.
func NewBody(p TreePane, opts ...BodyOpts) *Body {
	b := &Body{StatefulWidget: widgets.NewStatefulWidgetBase(), pane: p}
	if len(opts) > 0 {
		b.opts = opts[0]
	}
	return b
}

// CreateState implements widgets.StatefulWidget.
func (b *Body) CreateState() widgets.State { return &bodyState{} }

type bodyState struct {
	widgets.StateOf[*Body]
	stop func()
}

// InitState follows the pane.
func (s *bodyState) InitState() { s.follow() }

func (s *bodyState) follow() {
	s.stop = s.Widget().pane.OnChange(func() {
		if s.Mounted() {
			s.SetState(func() {})
		}
	})
}

// DidUpdateWidget follows another pane.
func (s *bodyState) DidUpdateWidget(old *Body) {
	if old.pane != s.Widget().pane {
		s.stop()
		s.follow()
	}
}

// Dispose stops following the pane.
func (s *bodyState) Dispose() { s.stop() }

// Build implements widgets.State.
func (s *bodyState) Build(bc widgets.BuildContext) widgets.Widget {
	w := s.Widget()
	p := w.pane
	colors := kit.ThemeOf(bc)
	if p.Model().Len() == 0 {
		msg := p.Message()
		var child widgets.Widget = widgets.NewSizedBox(nil)
		if msg != "" {
			child = widgets.NewPadding(painting.NewEdgeInsetsFromLTRB(20, 8, 20, 8),
				widgets.NewText(msg, widgets.TextOpts{Style: kit.TextStyleOf(bc, 13, colors.DescriptionForeground)}))
		}
		return widgets.NewKeyedSubtree(child, widgets.WidgetOpts{Key: MessageKey(p.ID())})
	}
	return kit.NewTree(p.Model(), kit.TreeOpts{
		Key: TreeKey(p.ID()), Controller: p.Controller(), TypeToFind: foundation.Some(false), Editing: p.Editing(),
		ToggleOnClick: w.opts.ToggleOnClick,
		Render: func(bc widgets.BuildContext, n kit.TreeNode, st kit.RowState) widgets.Widget {
			return s.render(bc, n, st)
		},
		OnSelect: func(n kit.TreeNode) {
			if r, ok := p.Row(n.Key); ok && w.opts.Select != nil {
				w.opts.Select(r)
			}
		},
		OnOpen: func(n kit.TreeNode) {
			if r, ok := p.Row(n.Key); ok && w.opts.Open != nil {
				w.opts.Open(r)
			}
		},
		OnContextMenu: func(key string, at ui.Offset) {
			if w.opts.Menu == nil {
				return
			}
			r, ok := p.Row(key)
			w.opts.Menu(r, ok, at)
		},
	})
}

// render draws a row of the pane and, while the pointer is over it or it is selected, the buttons of the row after it.
func (s *bodyState) render(bc widgets.BuildContext, n kit.TreeNode, st kit.RowState) widgets.Widget {
	w := s.Widget()
	inner := w.pane.Render(bc, n, st)
	if w.opts.Actions == nil || (!st.Hovered && !st.Focused && !st.Selected) {
		return widgets.NewKeyedSubtree(inner, widgets.WidgetOpts{Key: RowKey(w.pane.ID(), n.Key)})
	}
	r, ok := w.pane.Row(n.Key)
	if !ok {
		return inner
	}
	actions := w.opts.Actions(r)
	if len(actions) == 0 {
		return widgets.NewKeyedSubtree(inner, widgets.WidgetOpts{Key: RowKey(w.pane.ID(), n.Key)})
	}
	bar := kit.NewActionBar(actions, kit.ActionBarOpts{OnRun: func(a kit.Action) {
		if w.opts.RunAction != nil {
			w.opts.RunAction(r, a.ID)
		}
	}})
	return widgets.NewKeyedSubtree(widgets.NewRow([]widgets.Widget{widgets.NewExpanded(inner), bar},
		widgets.RowOpts{CrossAxisAlignment: rendering.CrossAxisAlignmentCenter}), widgets.WidgetOpts{Key: RowKey(w.pane.ID(), n.Key)})
}
