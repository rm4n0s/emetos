package debug

import (
	"context"

	"github.com/rm4n0s/glutter/foundation"
	"github.com/rm4n0s/glutter/painting"
	"github.com/rm4n0s/glutter/rendering"
	"github.com/rm4n0s/glutter/widgets"

	"github.com/rm4n0s/emetos/internal/core/debug"
	"github.com/rm4n0s/emetos/internal/gui/kit"
	"github.com/rm4n0s/emetos/internal/gui/kit/fonts"
)

// The content of the debug hover of the editor (step 25.5): the value under the pointer, and for a value that has children their tree,
// opened one level at a time.
//
// Ported from VS Code: src/vs/workbench/contrib/debug/browser/debugHover.ts (doShow, DebugHoverDataSource, DebugHoverDelegate: rows of 18
// pixels) and baseDebugView.ts (the renderer of the rows is the Variables view's)

// The metrics of the debug hover (debugHover.css).
const (
	// HoverRowHeight is the height of a row of the tree.
	HoverRowHeight = 18.0
	// HoverMinTreeHeight and HoverMaxTreeHeight bound the height of the tree; HoverWidth is the width of a box with a tree.
	HoverMinTreeHeight = 0.0
	HoverMaxTreeHeight = 266.0
	HoverWidth         = 420.0
	hoverIndent        = 8.0
	hoverTipHeight     = 20.0
)

// HoverOpts are the options of NewHover.
type HoverOpts struct {
	// Ctx is the life of the requests that load children.
	Ctx context.Context
	// Root is what was evaluated: an expression whose children are the rows of the tree.
	Root debug.Node
	// ShowTypes is debug.showVariableTypes.
	ShowTypes func() bool
	// Post hands work to the UI thread (a request of children runs on a goroutine of its own).
	Post func(func())
}

// HoverTree is the widget of the content of the debug hover.
type HoverTree struct {
	widgets.StatefulWidget
	opts HoverOpts
}

// NewHover makes the content of the debug hover: the value of the root alone when it has no children, else its value as a title, the
// tree of its children and the tip about the Alt key.
func NewHover(opts HoverOpts) *HoverTree {
	return &HoverTree{StatefulWidget: widgets.NewStatefulWidgetBase(), opts: opts}
}

// HoverSize is the size of the box for a root: the room of its rows (with the children that were loaded) and a title, a tip.
func HoverSize(root debug.Node, children int) (width, height float64) {
	if !root.HasChildren() {
		return 320, HoverRowHeight + 8
	}
	tree := min(float64(children)*HoverRowHeight, HoverMaxTreeHeight)
	return HoverWidth, HoverRowHeight + tree + hoverTipHeight + 8
}

// CreateState implements widgets.StatefulWidget.
func (h *HoverTree) CreateState() widgets.State {
	return &hoverState{open: map[string]bool{}, kids: map[string][]*debug.Variable{}, loading: map[string]bool{}}
}

type hoverState struct {
	widgets.StateOf[*HoverTree]
	open    map[string]bool
	kids    map[string][]*debug.Variable
	loading map[string]bool
	scroll  *widgets.ScrollControllerBase
}

// InitState loads the children of the root.
func (s *hoverState) InitState() {
	s.scroll = widgets.NewScrollController()
	if root := s.Widget().opts.Root; root.HasChildren() {
		s.load(root, "")
	}
}

// Dispose lets go of the scroll controller.
func (s *hoverState) Dispose() { s.scroll.Dispose() }

// load asks for the children of a node and shows them when they come.
func (s *hoverState) load(n debug.Node, key string) {
	if s.loading[key] || s.kids[key] != nil {
		return
	}
	s.loading[key] = true
	o := s.Widget().opts
	go func() {
		kids, _ := n.Children(o.Ctx)
		if o.Post == nil {
			return
		}
		o.Post(func() {
			if !s.Mounted() {
				return
			}
			s.SetState(func() {
				s.loading[key] = false
				if kids == nil {
					kids = []*debug.Variable{}
				}
				s.kids[key] = kids
			})
		})
	}()
}

// rowOf is a row of the tree being drawn: the variable, how deep it is.
type hoverRow struct {
	v     *debug.Variable
	depth int
}

// rows are the rows that show: the children of the root, and of each child that is open.
func (s *hoverState) rows() []hoverRow {
	var out []hoverRow
	var walk func(key string, depth int)
	walk = func(key string, depth int) {
		for _, v := range s.kids[key] {
			out = append(out, hoverRow{v, depth})
			if s.open[v.ID()] && v.HasChildren() {
				walk(v.ID(), depth+1)
			}
		}
	}
	walk("", 0)
	return out
}

func (s *hoverState) toggle(v *debug.Variable) {
	id := v.ID()
	if s.open[id] {
		s.SetState(func() { s.open[id] = false })
		return
	}
	s.SetState(func() { s.open[id] = true })
	s.load(v, id)
}

// Build implements widgets.State.
func (s *hoverState) Build(bc widgets.BuildContext) widgets.Widget {
	o := s.Widget().opts
	colors := kit.ThemeOf(bc)
	st := kit.RowState{Foreground: colors.EditorHoverWidgetForeground}
	showTypes := o.ShowTypes != nil && o.ShowTypes()
	root := LineOfNode(o.Root, "", showTypes)
	title := widgets.NewPadding(painting.NewEdgeInsetsSymmetric(2, 6), root.Widget(bc, st))
	if !o.Root.HasChildren() {
		return title
	}
	rows := s.rows()
	var list []widgets.Widget
	for _, r := range rows {
		list = append(list, s.row(bc, colors, st, r, showTypes))
	}
	height := min(float64(len(rows))*HoverRowHeight, HoverMaxTreeHeight)
	tree := widgets.NewSizedBox(widgets.NewRawScrollbar(
		widgets.NewSingleChildScrollView(widgets.NewColumn(list, widgets.ColumnOpts{CrossAxisAlignment: rendering.CrossAxisAlignmentStretch, MainAxisSize: rendering.MainAxisSizeMin}),
			widgets.SingleChildScrollViewOpts{Controller: s.scroll, Primary: foundation.Some(false)}),
		widgets.RawScrollbarOpts{Controller: s.scroll, Thickness: foundation.Some(6.0), ThumbColor: foundation.Some(colors.ScrollbarSliderBackground)}),
		widgets.SizedBoxOpts{Height: foundation.Some(height)})
	tip := widgets.NewPadding(painting.NewEdgeInsetsSymmetric(2, 6), widgets.NewText("Hold Alt key to switch to editor language hover",
		widgets.TextOpts{Style: kit.TextStyleOf(bc, 11, colors.DescriptionForeground), MaxLines: foundation.Some(1)}))
	divider := widgets.NewSizedBox(widgets.NewColoredBox(colors.EditorHoverWidgetBorder, nil), widgets.SizedBoxOpts{Height: foundation.Some(1.0)})
	return widgets.NewColumn([]widgets.Widget{title, divider, tree, tip}, widgets.ColumnOpts{CrossAxisAlignment: rendering.CrossAxisAlignmentStretch, MainAxisSize: rendering.MainAxisSizeMin})
}

// row is a row of the tree: the chevron of a node that has children, then its name and value.
func (s *hoverState) row(bc widgets.BuildContext, colors kit.Colors, st kit.RowState, r hoverRow, showTypes bool) widgets.Widget {
	line := LineOfNode(r.v, r.v.Name, showTypes)
	var twisty widgets.Widget = widgets.NewSizedBox(nil, widgets.SizedBoxOpts{Width: foundation.Some(16.0)})
	if r.v.HasChildren() {
		code := fonts.CodiconChevronRight
		if s.open[r.v.ID()] {
			code = fonts.CodiconChevronDown
		}
		twisty = widgets.NewIcon(fonts.CodiconIcon(code), widgets.IconOpts{Size: foundation.Some(16.0), Color: foundation.Some(st.Foreground)})
	}
	body := widgets.NewRow([]widgets.Widget{
		widgets.NewSizedBox(nil, widgets.SizedBoxOpts{Width: foundation.Some(float64(r.depth) * hoverIndent)}),
		twisty, widgets.NewSizedBox(nil, widgets.SizedBoxOpts{Width: foundation.Some(2.0)}), widgets.NewExpanded(line.Widget(bc, st)),
	}, widgets.RowOpts{CrossAxisAlignment: rendering.CrossAxisAlignmentCenter})
	var row widgets.Widget = widgets.NewSizedBox(widgets.NewPadding(painting.NewEdgeInsetsSymmetric(0, 6), body), widgets.SizedBoxOpts{Height: foundation.Some(HoverRowHeight)})
	if r.v.HasChildren() {
		row = widgets.NewGestureDetector(row, widgets.GestureDetectorOpts{
			Behavior: rendering.HitTestBehaviorOpaque, OnTap: func() { s.toggle(r.v) },
		})
	}
	return row
}
