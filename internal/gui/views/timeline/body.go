package timeline

import (
	"github.com/rm4n0s/glutter/foundation"
	"github.com/rm4n0s/glutter/painting"
	"github.com/rm4n0s/glutter/rendering"
	"github.com/rm4n0s/glutter/ui"
	"github.com/rm4n0s/glutter/widgets"

	"github.com/rm4n0s/emetos/extension/ui/markdown"
	"github.com/rm4n0s/emetos/internal/gui/kit"
	"github.com/rm4n0s/emetos/internal/gui/kit/fonts"
)

// Keys of the widgets of the view, which tests find them by.
var (
	// ListKey is the key of the list of items.
	ListKey = foundation.NewValueKey("timeline:list")
	// MessageKey is the key of the message that stands in place of the list.
	MessageKey = foundation.NewValueKey("timeline:message")
)

// ItemKey is the key of the row of an item, and LoadMoreKey that of the last row.
func ItemKey(source, id string) foundation.Key {
	return foundation.NewValueKey("timeline:item:" + source + "|" + id)
}

// LoadMoreKey is the key of the "Load more" row.
var LoadMoreKey = foundation.NewValueKey("timeline:load-more")

// BodyOpts are the options of NewBody.
type BodyOpts struct {
	// Actions gives the buttons of a row (the group "inline" of the menu of the items, as VS Code's); nil for none. They show while the
	// pointer is over the row or the row has the focus.
	Actions func(Row) []kit.Action
	// RunAction runs the button of a row.
	RunAction func(row Row, commandID string)
	// ShowMenu opens the context menu of an item at a position of the window.
	ShowMenu func(row Row, at ui.Offset)
	// Post runs a function on the UI thread, later.
	Post func(func())
	// Controller gives access to the list from outside (focus); NewController makes one.
	Controller *kit.ListController
	// ForceHover is the key of a row drawn as the one under the pointer, for a proof image.
	ForceHover string
	// OnHeight is told the height of the list when it is laid out (the automatic page size reads it).
	OnHeight func(h float64)
}

// Body is the body of the Timeline view: the list of items, or the message that says why there are none.
type Body struct {
	widgets.StatefulWidget
	pane *Pane
	opts BodyOpts
}

// NewBody makes the body of a pane.
func NewBody(p *Pane, opts ...BodyOpts) *Body {
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
	model *kit.ListViewModel
	ctl   *kit.ListController
	stop  func()
	rows  map[string]Row
}

// InitState follows the pane.
func (s *bodyState) InitState() {
	s.model = kit.NewListViewModel()
	s.ctl = s.Widget().opts.Controller
	if s.ctl == nil {
		s.ctl = kit.NewListController()
	}
	s.rows = map[string]Row{}
	s.sync()
	s.follow()
}

func (s *bodyState) follow() {
	s.stop = s.Widget().pane.OnChange(func() {
		s.sync()
		if s.Mounted() {
			s.SetState(func() {})
		}
	})
}

// DidUpdateWidget follows another pane.
func (s *bodyState) DidUpdateWidget(old *Body) {
	if old.pane != s.Widget().pane {
		s.stop()
		s.sync()
		s.follow()
	}
}

// Dispose stops following the pane.
func (s *bodyState) Dispose() { s.stop() }

// sync gives the model the rows of the pane.
func (s *bodyState) sync() {
	rows := s.Widget().pane.Rows()
	s.rows = make(map[string]Row, len(rows))
	items := make([]kit.ListViewItem, 0, len(rows))
	for _, r := range rows {
		s.rows[r.Key()] = r
		items = append(items, kit.ListViewItem{Key: r.Key(), Data: r, Label: r.Text()})
	}
	_ = s.model.SetItems(items) // keys are unique: a source and an ID name an item
}

// Build implements widgets.State.
func (s *bodyState) Build(bc widgets.BuildContext) widgets.Widget {
	w := s.Widget()
	p := w.pane
	colors := kit.ThemeOf(bc)
	if len(p.Rows()) == 0 {
		msg := p.Message()
		return widgets.NewKeyedSubtree(widgets.NewPadding(painting.NewEdgeInsetsFromLTRB(20, 8, 20, 8),
			widgets.NewText(msg, widgets.TextOpts{Style: kit.TextStyleOf(bc, 13, colors.DescriptionForeground)})),
			widgets.WidgetOpts{Key: MessageKey})
	}
	var inner widgets.Widget = kit.NewList(s.model, kit.ListOpts{
		Key: ListKey, Controller: s.ctl, ForceHover: w.opts.ForceHover, TypeToFind: foundation.Some(false),
		Render: func(bc widgets.BuildContext, lr kit.ListViewRow, st kit.RowState) widgets.Widget {
			return s.render(bc, lr, st)
		},
		OnSelect: func(lr kit.ListViewRow) {
			if r, ok := lr.Data.(Row); ok && !r.IsLoadMore() {
				p.Open(r)
			}
		},
		OnOpen: func(lr kit.ListViewRow) {
			if r, ok := lr.Data.(Row); ok {
				p.Open(r)
			}
		},
		OnContextMenu: func(lr kit.ListViewRow, at ui.Offset) {
			if r, ok := lr.Data.(Row); ok && !r.IsLoadMore() && w.opts.ShowMenu != nil {
				w.opts.ShowMenu(r, at)
			}
		},
	})
	if w.opts.OnHeight == nil {
		return inner
	}
	return widgets.NewLayoutBuilder(func(_ widgets.BuildContext, c rendering.BoxConstraints) widgets.Widget {
		w.opts.OnHeight(c.MaxHeight)
		return inner
	})
}

func gap(w float64) widgets.Widget {
	return widgets.NewSizedBox(nil, widgets.SizedBoxOpts{Width: foundation.Some(w)})
}

// render builds a row: the icon, the label with its description dimmed after it, the age at the end, and the buttons.
//
// Ported from VS Code: timelinePane.ts (TimelineTreeRenderer.renderElement)
func (s *bodyState) render(bc widgets.BuildContext, lr kit.ListViewRow, st kit.RowState) widgets.Widget {
	w := s.Widget()
	row, ok := lr.Data.(Row)
	if !ok {
		return kit.NewHighlightedLabel(bc, lr.Label, st)
	}
	colors := kit.ThemeOf(bc)
	base := kit.TextStyleOf(bc, 13, st.Foreground)
	if row.IsLoadMore() {
		// the row is in view: the next page is wanted when timeline.pageOnScroll is on (VS Code does it when the row is rendered)
		if w.opts.Post != nil {
			w.opts.Post(w.pane.ScrolledToEnd)
		}
		return widgets.NewKeyedSubtree(widgets.NewRow([]widgets.Widget{gap(19), widgets.NewExpanded(oneLine(row.Text(), base))},
			widgets.RowOpts{CrossAxisAlignment: rendering.CrossAxisAlignmentCenter}), widgets.WidgetOpts{Key: LoadMoreKey})
	}
	dim := base.CopyWith(painting.TextStyleOpts{Color: foundation.Some(dimmed(st, colors))})
	var cells []widgets.Widget
	if code, ok := fonts.CodiconByName(row.Icon); ok && row.Icon != "" {
		cells = append(cells, widgets.NewIcon(fonts.CodiconIcon(code), widgets.IconOpts{Size: foundation.Some(16.0), Color: foundation.Some(st.Foreground)}))
	} else {
		cells = append(cells, gap(16))
	}
	cells = append(cells, gap(3))
	spans := []painting.InlineSpan{painting.NewTextSpan(painting.TextSpanOpts{Text: foundation.Some(row.Label), Style: base})}
	if d := row.Description; d != "" {
		spans = append(spans, painting.NewTextSpan(painting.TextSpanOpts{Text: foundation.Some("  " + d), Style: dim.CopyWith(painting.TextStyleOpts{FontSize: foundation.Some(12.0)})}))
	}
	cells = append(cells, widgets.NewExpanded(widgets.NewRichText(painting.NewTextSpan(painting.TextSpanOpts{Children: spans}),
		widgets.RichTextOpts{MaxLines: foundation.Some(1), Overflow: painting.TextOverflowEllipsis})))
	if w.opts.Actions != nil && (st.Hovered || st.Focused || st.Selected) {
		if actions := w.opts.Actions(row); len(actions) > 0 {
			cells = append(cells, widgets.NewPadding(painting.NewEdgeInsetsFromLTRB(4, 0, 0, 0), kit.NewActionBar(actions, kit.ActionBarOpts{
				OnRun: func(a kit.Action) {
					if w.opts.RunAction != nil {
						w.opts.RunAction(row, a.ID)
					}
				}})))
		}
	}
	if row.Relative != "" && !row.HideRelative {
		cells = append(cells, widgets.NewPadding(painting.NewEdgeInsetsFromLTRB(8, 0, 0, 0),
			oneLine(row.Relative, dim.CopyWith(painting.TextStyleOpts{FontSize: foundation.Some(11.0)}))))
	}
	cells = append(cells, gap(8))
	var out widgets.Widget = widgets.NewKeyedSubtree(widgets.NewRow(cells, widgets.RowOpts{CrossAxisAlignment: rendering.CrossAxisAlignmentCenter}),
		widgets.WidgetOpts{Key: ItemKey(row.Source, row.ID)})
	if row.Detail != "" {
		detail := row.Detail
		out = kit.NewHover(out, kit.HoverOpts{Position: kit.HoverRight, Content: func(widgets.BuildContext) widgets.Widget {
			return markdown.NewView(detail, markdown.Options{Hover: true})
		}})
	}
	return out
}

// dimmed is the color of the description and the age: the description color, or the row's own while it is selected so that it stays readable.
func dimmed(st kit.RowState, colors kit.Colors) ui.Color {
	if st.Active && (st.Selected || st.Focused) {
		return st.Foreground.WithOpacity(0.8)
	}
	return colors.DescriptionForeground
}

func oneLine(text string, style *painting.TextStyle) widgets.Widget {
	return widgets.NewText(text, widgets.TextOpts{Style: style, MaxLines: foundation.Some(1), Overflow: painting.TextOverflowEllipsis})
}
