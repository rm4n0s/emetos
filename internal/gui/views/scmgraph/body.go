package scmgraph

import (
	"strconv"
	"strings"

	"github.com/rm4n0s/glutter/foundation"
	"github.com/rm4n0s/glutter/painting"
	"github.com/rm4n0s/glutter/rendering"
	"github.com/rm4n0s/glutter/ui"
	"github.com/rm4n0s/glutter/widgets"

	"github.com/rm4n0s/emetos/internal/core/timeline"
	"github.com/rm4n0s/emetos/internal/gui/kit"
	"github.com/rm4n0s/emetos/internal/gui/kit/fonts"
)

// Keys of the widgets of the view, which tests find them by.
var (
	// ListKey is the key of the list of rows.
	ListKey = foundation.NewValueKey("scmgraph:list")
	// MessageKey is the key of the message that stands in place of the list.
	MessageKey = foundation.NewValueKey("scmgraph:message")
	// SearchKey is the key of the search box.
	SearchKey = foundation.NewValueKey("scmgraph:search")
	// LoadMoreKey is the key of the "Load More..." row.
	LoadMoreKey = foundation.NewValueKey("scmgraph:load-more")
)

// RowKey is the key of the row of a commit.
func RowKey(id string) foundation.Key { return foundation.NewValueKey("scmgraph:row:" + id) }

// BodyOpts are the options of NewBody.
type BodyOpts struct {
	// Actions gives the buttons of a row (the group "inline" of the menu of the commits); nil for none. They show while the pointer is
	// over the row or the row has the focus.
	Actions func(Row) []kit.Action
	// RunAction runs the button of a row.
	RunAction func(row Row, commandID string)
	// ShowMenu opens the context menu of a row at a position of the window.
	ShowMenu func(row Row, at ui.Offset)
	// Post runs a function on the UI thread, later.
	Post func(func())
	// Controller gives access to the list from outside (focus, scroll); kit.NewListController makes one.
	Controller *kit.ListController
	// ForceHover is the key of a row drawn as the one under the pointer, for a proof image.
	ForceHover string
	// ShowHover is the id of the commit whose hover (the details) is shown at once, for a proof image and a test.
	ShowHover string
	// HideSearch leaves the search box out.
	HideSearch bool
}

// Body is the body of the Graph view: the search box and the list of rows, or the message that says why there are none.
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
	model  *kit.ListViewModel
	ctl    *kit.ListController
	stop   func()
	search *kit.InputBoxController
	hover  *kit.HoverController
}

// InitState follows the pane.
func (s *bodyState) InitState() {
	s.model = kit.NewListViewModel()
	s.ctl = s.Widget().opts.Controller
	if s.ctl == nil {
		s.ctl = kit.NewListController()
	}
	s.search = kit.NewInputBoxController(s.Widget().pane.Search())
	s.hover = kit.NewHoverController()
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

// sync gives the model the rows of the pane and scrolls to the row the pane asked for.
func (s *bodyState) sync() {
	rows := s.Widget().pane.Rows()
	items := make([]kit.ListViewItem, 0, len(rows))
	for _, r := range rows {
		items = append(items, kit.ListViewItem{Key: r.Key(), Data: r, Label: r.Item.Subject})
	}
	_ = s.model.SetItems(items) // keys are unique: the hash of a commit, or the key of a made-up row
	if key := s.Widget().pane.TakeReveal(); key != "" {
		if post := s.Widget().opts.Post; post != nil {
			post(func() {
				s.ctl.Reveal(key)
			})
		}
	}
}

// Build implements widgets.State.
func (s *bodyState) Build(bc widgets.BuildContext) widgets.Widget {
	w := s.Widget()
	p := w.pane
	colors := kit.ThemeOf(bc)
	var parts []widgets.Widget
	if !w.opts.HideSearch {
		parts = append(parts, widgets.NewKeyedSubtree(widgets.NewPadding(painting.NewEdgeInsetsFromLTRB(8, 4, 8, 4),
			kit.NewInputBox(nil, nil, kit.InputBoxOpts{
				Placeholder: "Search commits (message, author, hash)", Controller: s.search,
				OnChanged: func(text string) { p.SetSearch(text) },
			})), widgets.WidgetOpts{Key: SearchKey}))
	}
	if len(p.Rows()) == 0 {
		parts = append(parts, widgets.NewExpanded(widgets.NewKeyedSubtree(widgets.NewPadding(painting.NewEdgeInsetsFromLTRB(20, 8, 20, 8),
			widgets.NewText(p.Message(), widgets.TextOpts{Style: kit.TextStyleOf(bc, 13, colors.DescriptionForeground)})),
			widgets.WidgetOpts{Key: MessageKey})))
	} else {
		parts = append(parts, widgets.NewExpanded(kit.NewList(s.model, kit.ListOpts{
			Key: ListKey, Controller: s.ctl, ForceHover: w.opts.ForceHover, TypeToFind: foundation.Some(false),
			Render: func(bc widgets.BuildContext, lr kit.ListViewRow, st kit.RowState) widgets.Widget {
				return s.render(bc, lr, st)
			},
			OnSelect: func(lr kit.ListViewRow) {
				if r, ok := lr.Data.(Row); ok {
					p.Open(r)
				}
			},
			OnOpen: func(lr kit.ListViewRow) {
				if r, ok := lr.Data.(Row); ok {
					p.Open(r)
				}
			},
			OnContextMenu: func(lr kit.ListViewRow, at ui.Offset) {
				if r, ok := lr.Data.(Row); ok && r.Kind != RowLoadMore && w.opts.ShowMenu != nil {
					w.opts.ShowMenu(r, at)
				}
			},
		})))
	}
	return widgets.NewColumn(parts, widgets.ColumnOpts{CrossAxisAlignment: rendering.CrossAxisAlignmentStretch})
}

func gap(w float64) widgets.Widget {
	return widgets.NewSizedBox(nil, widgets.SizedBoxOpts{Width: foundation.Some(w)})
}

// haloOf is the color of the row's background, which the circles of the graph are outlined with.
func haloOf(colors kit.Colors, st kit.RowState) ui.Color {
	switch {
	case st.Selected && st.Active:
		return colors.ListActiveSelectionBackground
	case st.Selected:
		return colors.ListInactiveSelectionBackground
	case st.Hovered:
		return colors.ListHoverBackground
	}
	return colors.SideBarBackground
}

// laneColor gives the color a theme color id of a lane stands for.
func laneColor(colors kit.Colors) func(string) ui.Color {
	return func(id string) ui.Color {
		if c, ok := colors.Named(id); ok {
			return c
		}
		return colors.Foreground
	}
}

// render builds a row: the graph column, the subject with the author dimmed after it, the badges, the buttons and the age.
//
// Ported from VS Code: scmHistoryViewPane.ts (HistoryItemRenderer.renderElement, HistoryItemLoadMoreRenderer)
func (s *bodyState) render(bc widgets.BuildContext, lr kit.ListViewRow, st kit.RowState) widgets.Widget {
	w := s.Widget()
	row, ok := lr.Data.(Row)
	if !ok {
		return kit.NewHighlightedLabel(bc, lr.Label, st)
	}
	colors := kit.ThemeOf(bc)
	resolve := laneColor(colors)
	base := kit.TextStyleOf(bc, 13, st.Foreground)
	if row.Kind == RowLoadMore {
		// the row is in view: with scm.graph.pageOnScroll the next page is wanted (VS Code does it when the row is rendered)
		if w.opts.Post != nil {
			w.opts.Post(w.pane.ScrolledToEnd)
		}
		label := "Load More..."
		if s.Widget().pane.loadingMore() {
			label = "Loading..."
		}
		return widgets.NewKeyedSubtree(widgets.NewRow([]widgets.Widget{
			widgets.NewCustomPaint(nil, widgets.CustomPaintOpts{
				Painter: &placeholderPainter{columns: row.Columns, resolve: resolve}, Size: ui.Size{Width: ColumnWidth(len(row.Columns), len(row.Columns)), Height: RowHeight},
			}),
			widgets.NewExpanded(oneLine(label, base.CopyWith(painting.TextStyleOpts{Color: foundation.Some(colors.DescriptionForeground)}))),
		}, widgets.RowOpts{CrossAxisAlignment: rendering.CrossAxisAlignmentCenter}), widgets.WidgetOpts{Key: LoadMoreKey})
	}
	dim := base.CopyWith(painting.TextStyleOpts{Color: foundation.Some(dimmed(st, colors))})
	name := base
	if row.Current {
		name = base.CopyWith(painting.TextStyleOpts{FontWeight: ui.FontWeightW600})
	}
	var cells []widgets.Widget
	cells = append(cells, widgets.NewCustomPaint(nil, widgets.CustomPaintOpts{
		Painter: &lanePainter{row: row.Lane, resolve: resolve, halo: haloOf(colors, st)},
		Size:    ui.Size{Width: ColumnWidth(len(row.Lane.Input), len(row.Lane.Output)), Height: RowHeight},
	}))
	spans := []painting.InlineSpan{painting.NewTextSpan(painting.TextSpanOpts{Text: foundation.Some(row.Item.Subject), Style: name})}
	if a := row.Item.Author; a != "" {
		spans = append(spans, painting.NewTextSpan(painting.TextSpanOpts{Text: foundation.Some("  " + a), Style: dim.CopyWith(painting.TextStyleOpts{FontSize: foundation.Some(12.0)})}))
	}
	cells = append(cells, widgets.NewExpanded(widgets.NewRichText(painting.NewTextSpan(painting.TextSpanOpts{Children: spans}),
		widgets.RichTextOpts{MaxLines: foundation.Some(1), Overflow: painting.TextOverflowEllipsis})))
	for _, b := range row.Badges {
		cells = append(cells, gap(4), badge(bc, colors, b))
	}
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
	if age := s.age(row); age != "" {
		cells = append(cells, widgets.NewPadding(painting.NewEdgeInsetsFromLTRB(6, 0, 0, 0), oneLine(age, dim.CopyWith(painting.TextStyleOpts{FontSize: foundation.Some(11.0)}))))
	}
	cells = append(cells, gap(6))
	var out widgets.Widget = widgets.NewKeyedSubtree(widgets.NewRow(cells, widgets.RowOpts{CrossAxisAlignment: rendering.CrossAxisAlignmentCenter}),
		widgets.WidgetOpts{Key: RowKey(row.Item.ID)})
	if row.Kind == RowItem {
		hov := kit.HoverOpts{Position: kit.HoverRight, Content: func(bc widgets.BuildContext) widgets.Widget { return s.hoverContent(bc, row) }}
		if w.opts.ShowHover == row.Item.ID {
			hov.Controller = s.hover
			if w.opts.Post != nil {
				w.opts.Post(s.hover.Show)
			}
		}
		out = kit.NewHover(out, hov)
	}
	return out
}

// loadingMore says whether the next page is being read.
func (p *Pane) loadingMore() bool {
	st := p.state()
	return st != nil && st.loadingMore
}

// age is the age of a commit as the row writes it: "3 days ago"; none for a row without a date.
func (s *bodyState) age(r Row) string {
	if r.Item.Timestamp.IsZero() {
		return ""
	}
	p := s.Widget().pane
	return timeline.FromNow(p.o.Now(), r.Item.Timestamp, timeline.FromNowOptions{Ago: true, FullWords: true})
}

// dimmed is the color of the author and the age: the description color, or the row's own while it is selected so that it stays readable.
func dimmed(st kit.RowState, colors kit.Colors) ui.Color {
	if st.Active && (st.Selected || st.Focused) {
		return st.Foreground.WithOpacity(0.8)
	}
	return colors.DescriptionForeground
}

func oneLine(text string, style *painting.TextStyle) widgets.Widget {
	return widgets.NewText(text, widgets.TextOpts{Style: style, MaxLines: foundation.Some(1), Overflow: painting.TextOverflowEllipsis})
}

// badge draws a label of a row: a rounded box with the Codicon of the references, a count when there are several, and the name for the
// first colored one.
//
// Ported from VS Code: scmHistoryViewPane.ts (HistoryItemRenderer._renderBadge) and scm.css (.label-container > .label)
func badge(bc widgets.BuildContext, colors kit.Colors, b Badge) widgets.Widget {
	bg, fg := colors.BadgeBackground, colors.Foreground
	if b.Color != "" {
		if c, ok := colors.Named(b.Color); ok {
			bg = c
		}
		if c, ok := colors.Named("scmGraph.historyItemHoverLabelForeground"); ok {
			fg = c
		}
	} else if c, ok := colors.Named("scmGraph.historyItemHoverDefaultLabelBackground"); ok {
		bg = c
	}
	style := kit.TextStyleOf(bc, 12, fg)
	var cells []widgets.Widget
	if b.Count > 1 {
		cells = append(cells, widgets.NewPadding(painting.NewEdgeInsetsFromLTRB(4, 0, 0, 0), oneLine(strconv.Itoa(b.Count), style)))
	}
	if code, ok := fonts.CodiconByName(b.Icon); ok {
		cells = append(cells, widgets.NewPadding(painting.NewEdgeInsetsAll(2), widgets.NewIcon(fonts.CodiconIcon(code), widgets.IconOpts{Size: foundation.Some(12.0), Color: foundation.Some(fg)})))
	}
	if b.Name != "" {
		cells = append(cells, widgets.NewPadding(painting.NewEdgeInsetsFromLTRB(0, 0, 4, 0),
			widgets.NewConstrainedBox(rendering.NewBoxConstraints(rendering.BoxConstraintsOpts{MaxWidth: foundation.Some(100.0)}), oneLine(b.Name, style))))
	}
	return widgets.NewDecoratedBox(
		painting.NewBoxDecoration(painting.BoxDecorationOpts{Color: foundation.Some(bg), BorderRadius: painting.NewBorderRadiusCircular(10)}),
		widgets.NewRow(cells, widgets.RowOpts{MainAxisSize: rendering.MainAxisSizeMin, CrossAxisAlignment: rendering.CrossAxisAlignmentCenter}))
}

// hoverContent is the details of a commit: the author with the date, the message, the size of the change and the references.
//
// Ported from VS Code: scmHistory.ts (toHistoryItemHoverContent) and extensions/git/src/hover.ts (getHistoryItemHover)
func (s *bodyState) hoverContent(bc widgets.BuildContext, r Row) widgets.Widget {
	colors := kit.ThemeOf(bc)
	fg := colors.Foreground
	text := kit.TextStyleOf(bc, 13, fg)
	dim := kit.TextStyleOf(bc, 13, colors.DescriptionForeground)
	icon := func(name string, size float64, c ui.Color) widgets.Widget {
		code, _ := fonts.CodiconByName(name)
		return widgets.NewIcon(fonts.CodiconIcon(code), widgets.IconOpts{Size: foundation.Some(size), Color: foundation.Some(c)})
	}
	var parts []widgets.Widget
	add := func(w widgets.Widget) {
		if len(parts) > 0 {
			parts = append(parts, widgets.NewSizedBox(nil, widgets.SizedBoxOpts{Height: foundation.Some(6.0)}))
		}
		parts = append(parts, w)
	}
	rule := func() widgets.Widget {
		return widgets.NewColoredBox(fg.WithOpacity(0.25), widgets.NewSizedBox(nil, widgets.SizedBoxOpts{Height: foundation.Some(1.0)}))
	}
	it := r.Item
	if it.Author != "" {
		line := []widgets.Widget{icon("account", 14, fg), gap(4),
			widgets.NewText(it.Author, widgets.TextOpts{Style: text.CopyWith(painting.TextStyleOpts{FontWeight: ui.FontWeightBold})})}
		if !it.Timestamp.IsZero() {
			ago := timeline.FromNow(s.Widget().pane.o.Now(), it.Timestamp, timeline.FromNowOptions{Ago: true, FullWords: true})
			line = append(line, widgets.NewText(", ", widgets.TextOpts{Style: text}), icon("history", 14, fg), gap(3),
				widgets.NewText(ago+" ("+it.Timestamp.In(s.Widget().pane.o.Location).Format("January 2, 2006 at 3:04 PM")+")", widgets.TextOpts{Style: dim}))
		}
		add(widgets.NewWrap(line, widgets.WrapOpts{CrossAxisAlignment: rendering.WrapCrossAlignmentCenter}))
	}
	msg := strings.TrimSpace(it.Message)
	if msg == "" {
		msg = it.Subject
	}
	add(widgets.NewText(msg, widgets.TextOpts{Style: text}))
	if st := it.Stats; st != nil {
		add(rule())
		files := strconv.Itoa(st.Files) + " files changed"
		if st.Files == 1 {
			files = "1 file changed"
		}
		cells := []widgets.Widget{widgets.NewText(files, widgets.TextOpts{Style: text})}
		colorOf := func(id string, fallback ui.Color) ui.Color {
			if c, ok := colors.Named(id); ok {
				return c
			}
			return fallback
		}
		if st.Insertions > 0 {
			t := strconv.Itoa(st.Insertions) + " insertions(+)"
			if st.Insertions == 1 {
				t = "1 insertion(+)"
			}
			cells = append(cells, widgets.NewText(", ", widgets.TextOpts{Style: text}), widgets.NewText(t, widgets.TextOpts{Style: kit.TextStyleOf(bc, 13, colorOf("scmGraph.historyItemHoverAdditionsForeground", fg))}))
		}
		if st.Deletions > 0 {
			t := strconv.Itoa(st.Deletions) + " deletions(-)"
			if st.Deletions == 1 {
				t = "1 deletion(-)"
			}
			cells = append(cells, widgets.NewText(", ", widgets.TextOpts{Style: text}), widgets.NewText(t, widgets.TextOpts{Style: kit.TextStyleOf(bc, 13, colorOf("scmGraph.historyItemHoverDeletionsForeground", fg))}))
		}
		add(widgets.NewWrap(cells, widgets.WrapOpts{CrossAxisAlignment: rendering.WrapCrossAlignmentCenter}))
	}
	if len(r.Badges) > 0 {
		add(rule())
		var bs []widgets.Widget
		for _, ref := range it.Refs {
			color := ""
			for _, l := range r.Lane.Item.Refs {
				if l.ID == ref.ID {
					color = l.Color
				}
			}
			bs = append(bs, badge(bc, colors, Badge{Icon: ref.Icon, Color: color, Name: ref.Name, Count: 1}))
		}
		add(widgets.NewWrap(bs, widgets.WrapOpts{Spacing: 4, RunSpacing: 4}))
	}
	return widgets.NewConstrainedBox(rendering.NewBoxConstraints(rendering.BoxConstraintsOpts{MaxWidth: foundation.Some(360.0)}),
		widgets.NewColumn(parts, widgets.ColumnOpts{MainAxisSize: rendering.MainAxisSizeMin, CrossAxisAlignment: rendering.CrossAxisAlignmentStart}))
}
