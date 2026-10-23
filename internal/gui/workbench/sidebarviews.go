package workbench

import (
	"strings"

	"github.com/rm4n0s/glutter/foundation"
	"github.com/rm4n0s/glutter/painting"
	"github.com/rm4n0s/glutter/rendering"
	"github.com/rm4n0s/glutter/services"
	"github.com/rm4n0s/glutter/ui"
	"github.com/rm4n0s/glutter/widgets"

	"github.com/rm4n0s/emetos/internal/core/contextkey"
	"github.com/rm4n0s/emetos/internal/gui/kit"
	"github.com/rm4n0s/emetos/internal/gui/kit/fonts"
)

// ViewHeaderHeight is the height of the header of a view in the side bar.
//
// Ported from VS Code: src/vs/base/browser/ui/splitview/paneview.ts (Pane.HEADER_SIZE)
const ViewHeaderHeight = 22.0

// ViewHeaderKey is the key of the header of a view, which a test finds it by.
func ViewHeaderKey(viewID string) foundation.Key {
	return foundation.NewValueKey("workbench:view-header:" + viewID)
}

// SideBarViewsOpts are the options of NewSideBarViews.
type SideBarViewsOpts struct {
	// Context is the context the `when` of the views is evaluated in; every view shows when nil.
	Context contextkey.Context
	// Body builds the body of a view; a view it gives nothing for (or no Body at all) has an empty
	// body, which is what the views are until the steps that build them.
	Body func(bc widgets.BuildContext, viewID string) widgets.Widget
}

// SideBarViews is the body of the side bar: the views of the container the side bar shows, each a
// header with a chevron and the title of the view in capitals (a click, Enter or Space on it folds the
// body away or brings it back) over the body of the view. The bodies of the views that are open share
// the height that is left (SPEC §10.4).
//
// Ported from VS Code: src/vs/base/browser/ui/splitview/paneview.ts (PaneView, Pane) and
// src/vs/workbench/browser/parts/views/viewPane.ts
type SideBarViews struct {
	widgets.StatefulWidget
	views *Views
	opts  SideBarViewsOpts
}

// NewSideBarViews makes the views of the active container of views.
func NewSideBarViews(views *Views, opts ...SideBarViewsOpts) *SideBarViews {
	w := &SideBarViews{StatefulWidget: widgets.NewStatefulWidgetBase(), views: views}
	if len(opts) > 0 {
		w.opts = opts[0]
	}
	return w
}

// CreateState implements widgets.StatefulWidget.
func (w *SideBarViews) CreateState() widgets.State { return &sideBarViewsState{} }

type sideBarViewsState struct {
	widgets.StateOf[*SideBarViews]
	stop func()
	// nodes are the focus nodes of the headers, by view.
	nodes map[string]*widgets.FocusNode
}

// InitState follows the views.
func (s *sideBarViewsState) InitState() {
	s.nodes = map[string]*widgets.FocusNode{}
	s.follow()
}

func (s *sideBarViewsState) follow() {
	if s.stop != nil {
		s.stop()
	}
	s.stop = s.Widget().views.OnChange(func() {
		if s.Mounted() {
			s.SetState(func() {})
		}
	})
}

// DidUpdateWidget follows new views.
func (s *sideBarViewsState) DidUpdateWidget(old *SideBarViews) {
	if old.views != s.Widget().views {
		s.follow()
	}
}

// Dispose stops following and lets go of the focus nodes.
func (s *sideBarViewsState) Dispose() {
	if s.stop != nil {
		s.stop()
	}
	for _, n := range s.nodes {
		n.Dispose()
	}
}

func (s *sideBarViewsState) node(viewID string) *widgets.FocusNode {
	n, ok := s.nodes[viewID]
	if !ok {
		n = widgets.NewFocusNode(widgets.FocusNodeOpts{DebugLabel: "ViewHeader"})
		s.nodes[viewID] = n
	}
	return n
}

// Build implements widgets.State.
func (s *sideBarViewsState) Build(bc widgets.BuildContext) widgets.Widget {
	w := s.Widget()
	colors := kit.ThemeOf(bc)
	views := w.views.Registry().Views(w.views.Active(), w.opts.Context)
	var rows []widgets.Widget
	for i, v := range views {
		collapsed := w.views.IsCollapsed(v.ID)
		rows = append(rows, s.header(bc, colors, v.ID, v.Name, collapsed, i > 0))
		if collapsed {
			continue
		}
		var body widgets.Widget
		if w.opts.Body != nil {
			body = w.opts.Body(bc, v.ID)
		}
		if body == nil {
			body = widgets.NewSizedBox(nil)
		}
		rows = append(rows, widgets.NewExpanded(body))
	}
	return widgets.NewColumn(rows, widgets.ColumnOpts{CrossAxisAlignment: rendering.CrossAxisAlignmentStretch})
}

// header is the header of a view.
func (s *sideBarViewsState) header(bc widgets.BuildContext, colors kit.Colors, viewID, name string, collapsed, line bool) widgets.Widget {
	fg := opaqueOr(colors.SideBarSectionHeaderForeground, opaqueOr(colors.SideBarForeground, colors.Foreground))
	chevron := fonts.CodiconChevronDown
	if collapsed {
		chevron = fonts.CodiconChevronRight
	}
	style := kit.TextStyleOf(bc, 11, fg).Merge(painting.NewTextStyle(painting.TextStyleOpts{FontWeight: ui.FontWeightW700}))
	row := widgets.NewRow([]widgets.Widget{
		widgets.NewIcon(fonts.CodiconIcon(chevron), widgets.IconOpts{Size: foundation.Some(16.0), Color: foundation.Some(fg)}),
		widgets.NewSizedBox(nil, widgets.SizedBoxOpts{Width: foundation.Some(2.0)}),
		widgets.NewExpanded(widgets.NewText(strings.ToUpper(name), widgets.TextOpts{Style: style, Overflow: painting.TextOverflowEllipsis, MaxLines: foundation.Some(1)})),
	}, widgets.RowOpts{CrossAxisAlignment: rendering.CrossAxisAlignmentCenter})
	var box widgets.Widget = widgets.NewPadding(painting.NewEdgeInsetsFromLTRB(2, 0, 8, 0), row)
	children := []widgets.Widget{widgets.NewColoredBox(colors.SideBarSectionHeaderBackground, box)}
	if line && colors.SideBarSectionHeaderBorder.Alpha() != 0 {
		children = append(children, widgets.NewPositioned(
			widgets.NewIgnorePointer(widgets.NewColoredBox(colors.SideBarSectionHeaderBorder, widgets.NewSizedBox(nil))),
			widgets.PositionedOpts{Left: foundation.Some(0.0), Right: foundation.Some(0.0), Top: foundation.Some(0.0), Height: foundation.Some(1.0)}))
	}
	toggle := func() { s.Widget().views.SetCollapsed(viewID, !s.Widget().views.IsCollapsed(viewID)) }
	node := s.node(viewID)
	var hdr widgets.Widget = widgets.NewSizedBox(
		widgets.NewStack(children, widgets.StackOpts{Fit: rendering.StackFitExpand}),
		widgets.SizedBoxOpts{Height: foundation.Some(ViewHeaderHeight)})
	hdr = widgets.NewMouseRegion(
		widgets.NewGestureDetector(hdr, widgets.GestureDetectorOpts{Behavior: rendering.HitTestBehaviorOpaque, OnTap: func() {
			node.RequestFocus()
			toggle()
		}}),
		widgets.MouseRegionOpts{Cursor: services.SystemMouseCursors.Click})
	hdr = widgets.NewFocus(hdr, widgets.FocusOpts{
		FocusNode: node, DebugLabel: "ViewHeader",
		OnKeyEvent: func(_ *widgets.FocusNode, e services.KeyEvent) widgets.KeyEventResult {
			if _, ok := e.(*services.KeyDownEvent); !ok {
				return widgets.KeyEventResultIgnored
			}
			collapsed := s.Widget().views.IsCollapsed(viewID)
			switch e.LogicalKey() {
			case services.LogicalKeyboardKeyEnter, services.LogicalKeyboardKeyNumpadEnter, services.LogicalKeyboardKeySpace:
				toggle()
			case services.LogicalKeyboardKeyArrowRight:
				if !collapsed {
					return widgets.KeyEventResultIgnored
				}
				toggle()
			case services.LogicalKeyboardKeyArrowLeft:
				if collapsed {
					return widgets.KeyEventResultIgnored
				}
				toggle()
			default:
				return widgets.KeyEventResultIgnored
			}
			return widgets.KeyEventResultHandled
		},
	})
	return widgets.NewKeyedSubtree(hdr, widgets.WidgetOpts{Key: ViewHeaderKey(viewID)})
}
