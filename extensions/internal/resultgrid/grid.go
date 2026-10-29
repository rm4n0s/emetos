package resultgrid

import (
	"strconv"
	"time"

	"github.com/rm4n0s/glutter/foundation"
	"github.com/rm4n0s/glutter/gestures"
	"github.com/rm4n0s/glutter/painting"
	"github.com/rm4n0s/glutter/rendering"
	"github.com/rm4n0s/glutter/services"
	gui "github.com/rm4n0s/glutter/ui"
	"github.com/rm4n0s/glutter/widgets"

	"github.com/rm4n0s/emetos/extension/ui"
)

// The look of the grid.
const (
	// DefaultRowHeight is the height of a row, as the lines of a VS Code list (22 px).
	DefaultRowHeight = 22.0
	// headerHeight is the height of the line of the titles.
	headerHeight = 26.0
	// gutterWidth is the width of the column of the row numbers.
	gutterWidth = 52.0
	// footerHeight is the height of the status line under the rows.
	footerHeight = 26.0
	// fontSize is the size of the text of the cells.
	fontSize = 13.0
	// handleWidth is the width of the place at the right edge of a title where a drag resizes the column.
	handleWidth = 6.0
	// cellPad is the room at the left and right of the text of a cell.
	cellPad = 8.0
)

// Options configure a Grid.
type Options struct {
	// Model are the options of the text of the cells and of a copy.
	Model ModelOptions
	// RowHeight is the height of a row; DefaultRowHeight when 0.
	RowHeight float64
	// OnCopy receives the text of a copy: the extension writes it to the clipboard. A grid without it copies nothing.
	OnCopy func(text string)
	// OnFetchMore, when set, is called by the button "Fetch More" of the footer, which shows while the grid is told there are more
	// rows (SetMore).
	OnFetchMore func()
	// MenuItems, when set, adds entries to the context menu of a cell, after those of the grid (the panel adds "Export...").
	MenuItems func() []ui.MenuItem
}

// Grid is the widget of a result: a header, the rows in view, a footer. It is used on the UI thread.
type Grid struct {
	opts  Options
	model *Model
	more  bool

	vCtl, hCtl widgets.ScrollController
	focus      *widgets.FocusNode
	menu       *ui.ContextMenuController

	// state is the state of the mounted widget; nil while it is not on screen.
	state *gridState
	// resize is the drag of the edge of a title.
	resize struct {
		active     bool
		col        int
		startX     float64
		startWidth float64
	}
	// lastEdge is the last press on the edge of a title, for the double click that fits the column.
	lastEdge struct {
		col   int
		at    time.Duration
		valid bool
		// dragged says that the press moved the edge: it was a drag, and the next press is not the second of a double click.
		dragged bool
	}
	// rowBuf is the scratch of the row builder; builds is how many rows the builder made since ResetBuilds (the tests and the
	// benchmark count them, to show that rows off screen are never built).
	rowBuf []Value
	builds int
}

// New makes a grid with no result.
func New(opts Options) *Grid {
	if opts.RowHeight <= 0 {
		opts.RowHeight = DefaultRowHeight
	}
	return &Grid{
		opts: opts, model: NewModel(opts.Model),
		vCtl: widgets.NewScrollController(), hCtl: widgets.NewScrollController(),
		focus: widgets.NewFocusNode(widgets.FocusNodeOpts{DebugLabel: "result grid"}),
		menu:  ui.NewContextMenuController(),
	}
}

// Model is the model of the grid, for the code that reads the selection or the sort, and for the export.
func (g *Grid) Model() *Model { return g.model }

// SetSource shows a result.
func (g *Grid) SetSource(src Source) {
	g.model.SetSource(src)
	g.rowBuf = make([]Value, len(g.model.Columns()))
	g.jumpTo(0, 0)
	g.refresh()
}

// Appended tells the grid that its source has more rows (a fetch).
func (g *Grid) Appended() {
	g.model.Appended()
	g.refresh()
}

// SetMore says whether there are more rows to fetch: the footer then offers "Fetch More" when OnFetchMore is set.
func (g *Grid) SetMore(more bool) {
	g.more = more
	g.refresh()
}

// Focus gives the grid the keyboard.
func (g *Grid) Focus() { g.focus.RequestFocus() }

// Dispose lets go of what the grid holds.
func (g *Grid) Dispose() {
	g.vCtl.Dispose()
	g.hCtl.Dispose()
	g.focus.Dispose()
}

// RowsBuilt is the number of rows the builder made since the grid was made (a counter for the tests and the benchmark).
func (g *Grid) RowsBuilt() int { return g.builds }

// refresh builds the widget again when it is on screen.
func (g *Grid) refresh() {
	if g.state != nil && g.state.Mounted() {
		g.state.SetState(func() {})
	}
}

// jumpTo scrolls to a position.
func (g *Grid) jumpTo(top, left float64) {
	if g.vCtl.HasClients() {
		g.vCtl.JumpTo(top)
	}
	if g.hCtl.HasClients() {
		g.hCtl.JumpTo(left)
	}
}

// copyText hands a text to the clipboard.
func (g *Grid) copyText(text string) {
	if g.opts.OnCopy != nil && text != "" {
		g.opts.OnCopy(text)
	}
}

// ---- the widget ----

// Widget is the grid as a widget; build it once and keep it, or build it again: the state is in the Grid.
func (g *Grid) Widget() widgets.Widget {
	return &gridWidget{StatefulWidget: widgets.NewStatefulWidgetBase(), g: g}
}

type gridWidget struct {
	widgets.StatefulWidget
	g *Grid
}

// CreateState implements widgets.StatefulWidget.
func (w *gridWidget) CreateState() widgets.State { return &gridState{} }

type gridState struct {
	widgets.StateOf[*gridWidget]
	hasFocus bool
}

// InitState implements widgets.State.
func (s *gridState) InitState() { s.Widget().g.state = s }

// Dispose implements widgets.State.
func (s *gridState) Dispose() {
	if g := s.Widget().g; g.state == s {
		g.state = nil
	}
}

// DidUpdateWidget implements widgets.State.
func (s *gridState) DidUpdateWidget(old *gridWidget) {
	if old.g != s.Widget().g {
		if old.g.state == s {
			old.g.state = nil
		}
		s.Widget().g.state = s
	}
}

// Build implements widgets.State.
func (s *gridState) Build(bc widgets.BuildContext) widgets.Widget {
	g := s.Widget().g
	m := g.model
	colors := ui.Theme(bc)
	total := gutterWidth + m.TotalWidth()
	b := &builder{g: g, bc: bc, colors: colors, active: s.hasFocus, opts: m.Display()}
	b.bounds.r0, b.bounds.c0, b.bounds.r1, b.bounds.c1, b.bounds.ok = m.sel.Bounds(m.Len(), len(m.Columns()))
	b.style = ui.TextStyle(bc, fontSize, colors.Foreground)

	var body widgets.Widget
	if m.Len() == 0 {
		msg := "No rows."
		if m.src == nil {
			msg = ""
		}
		body = widgets.NewPadding(painting.NewEdgeInsetsFromLTRB(cellPad+4, 8, cellPad, 0),
			widgets.NewText(msg, widgets.TextOpts{Style: ui.TextStyle(bc, fontSize, colors.DescriptionForeground)}))
	} else {
		sliver := widgets.NewSliverFixedExtentListBuilder(b.row, g.opts.RowHeight, widgets.SliverBuilderOpts{
			ItemCount: foundation.Some(m.Len()), AddAutomaticKeepAlives: foundation.Some(false),
			AddRepaintBoundaries: foundation.Some(false), AddSemanticIndexes: foundation.Some(false),
		})
		body = widgets.NewCustomScrollView([]widgets.Widget{sliver}, widgets.ScrollViewOpts{Controller: g.vCtl})
	}
	header := b.header()
	// the content is as wide as the columns, and at least as wide as the view, so that the rows reach the right edge and the wheel
	// scrolls over the empty part at the right of narrow columns too
	hscroll := widgets.NewLayoutBuilder(func(_ widgets.BuildContext, c rendering.BoxConstraints) widgets.Widget {
		content := widgets.NewSizedBox(widgets.NewColumn([]widgets.Widget{
			header, widgets.NewExpanded(body),
		}, widgets.ColumnOpts{CrossAxisAlignment: rendering.CrossAxisAlignmentStretch}), widgets.SizedBoxOpts{Width: foundation.Some(max(total, c.MaxWidth))})
		return widgets.NewSingleChildScrollView(content, widgets.SingleChildScrollViewOpts{
			ScrollDirection: painting.AxisHorizontal, Controller: g.hCtl,
		})
	})
	element := ui.NewScrollable(hscroll, ui.ScrollableOpts{Controller: g.vCtl, HorizontalController: g.hCtl})
	area := widgets.NewColoredBox(colors.EditorBackground, widgets.NewColumn([]widgets.Widget{
		widgets.NewExpanded(element), b.footer(),
	}, widgets.ColumnOpts{CrossAxisAlignment: rendering.CrossAxisAlignmentStretch}))
	focused := widgets.NewFocus(area, widgets.FocusOpts{
		FocusNode: g.focus, OnKeyEvent: s.handleKey, OnFocusChange: func(has bool) {
			if has != s.hasFocus && s.Mounted() {
				s.SetState(func() { s.hasFocus = has })
			}
		},
	})
	return ui.NewContextMenu(focused, ui.ContextMenuOpts{Controller: g.menu})
}

// builder builds the pieces of one build of the grid.
type builder struct {
	g      *Grid
	bc     widgets.BuildContext
	colors ui.Colors
	style  *painting.TextStyle
	active bool
	opts   DisplayOptions
	bounds struct {
		r0, c0, r1, c1 int
		ok             bool
	}
}

// selected tells whether the cell is selected.
func (b *builder) selected(r, c int) bool {
	k := b.bounds
	return k.ok && r >= k.r0 && r <= k.r1 && c >= k.c0 && c <= k.c1
}

// row builds the row r of the list: the number, then a cell for each column. It is called only for the rows in view.
func (b *builder) row(_ widgets.BuildContext, r int) widgets.Widget {
	g := b.g
	m := g.model
	if r < 0 || r >= m.Len() {
		return nil
	}
	g.builds++
	if len(g.rowBuf) != len(m.Columns()) {
		g.rowBuf = make([]Value, len(m.Columns()))
	}
	m.Row(r, g.rowBuf)
	cells := make([]widgets.Widget, 0, len(g.rowBuf)+1)
	cells = append(cells, b.gutterCell(r))
	focus, hasFocus := m.Focus()
	for c, v := range g.rowBuf {
		cells = append(cells, b.cell(r, c, v, hasFocus && focus.Row == r && focus.Col == c))
	}
	row := widgets.NewSizedBox(widgets.NewRow(cells, widgets.RowOpts{CrossAxisAlignment: rendering.CrossAxisAlignmentStretch}),
		widgets.SizedBoxOpts{Height: foundation.Some(g.opts.RowHeight)})
	return widgets.NewListener(row, widgets.ListenerOpts{
		Behavior:      rendering.HitTestBehaviorOpaque,
		OnPointerDown: func(e *gestures.PointerDownEvent) { g.pressRow(r, e) },
	})
}

// gutterCell is the number of a row.
func (b *builder) gutterCell(r int) widgets.Widget {
	sel := b.g.model.sel
	selected := sel.Kind == SelAll || (sel.Kind == SelRows && r >= min(sel.Anchor.Row, sel.Focus.Row) && r <= max(sel.Anchor.Row, sel.Focus.Row))
	color := b.colors.EditorLineNumberForeground
	if selected {
		color = b.colors.EditorLineNumberActiveForeground
	}
	label := widgets.NewText(strconv.Itoa(r+1), widgets.TextOpts{Style: ui.TextStyle(b.bc, 12, color), MaxLines: foundation.Some(1)})
	var deco *painting.BoxDecoration
	if selected {
		deco = painting.NewBoxDecoration(painting.BoxDecorationOpts{Color: foundation.Some(b.colors.ListInactiveSelectionBackground)})
	}
	return widgets.NewContainer(widgets.NewAlign(widgets.NewPadding(painting.NewEdgeInsetsFromLTRB(0, 0, cellPad, 0), label),
		widgets.AlignOpts{Alignment: painting.AlignmentCenterRight}),
		containerOpts(gutterWidth, deco))
}

// containerOpts are the options of a box of a width with a decoration, or with none when deco is nil (a nil *BoxDecoration in the
// interface of the options is not "no decoration").
func containerOpts(width float64, deco *painting.BoxDecoration) widgets.ContainerOpts {
	o := widgets.ContainerOpts{Width: foundation.Some(width)}
	if deco != nil {
		o.Decoration = deco
	}
	return o
}

// cell is one cell of a row.
func (b *builder) cell(r, c int, v Value, focused bool) widgets.Widget {
	g := b.g
	text, cut := v.Display(b.opts)
	style := b.style
	selected := b.selected(r, c)
	switch {
	case selected && b.active:
		style = ui.TextStyle(b.bc, fontSize, b.colors.ListActiveSelectionForeground)
	case selected:
		// list.inactiveSelectionForeground has no default in the themes: the text keeps its color then, as in a tree (kit/tree.go).
		if fg := b.colors.ListInactiveSelectionForeground; fg.Alpha() != 0 {
			style = ui.TextStyle(b.bc, fontSize, fg)
		}
	case v.IsNull():
		style = style.CopyWith(painting.TextStyleOpts{Color: foundation.Some(b.colors.DescriptionForeground), FontStyle: gui.FontStyleItalic})
	}
	var label widgets.Widget = widgets.NewText(text, widgets.TextOpts{Style: style, MaxLines: foundation.Some(1), Overflow: painting.TextOverflowEllipsis})
	if width := g.model.Width(c) - 2*cellPad; cut || float64(len(text))*charWidth > width {
		label = ui.NewHover(label, ui.HoverOpts{Text: v.Tip(b.g.opts.Model.NullText)})
	}
	align := painting.AlignmentCenterLeft
	if v.IsNumber() {
		align = painting.AlignmentCenterRight
	}
	var deco *painting.BoxDecoration
	switch {
	case selected && b.active:
		deco = painting.NewBoxDecoration(painting.BoxDecorationOpts{Color: foundation.Some(b.colors.ListActiveSelectionBackground)})
	case selected:
		deco = painting.NewBoxDecoration(painting.BoxDecorationOpts{Color: foundation.Some(b.colors.ListInactiveSelectionBackground)})
	}
	if focused {
		side := painting.BorderSideOpts{Color: foundation.Some(b.colors.ListFocusOutline), Width: foundation.Some(1.0)}
		opts := painting.BoxDecorationOpts{Border: painting.NewBorderAll(side)}
		if deco != nil {
			opts.Color = foundation.Some(b.colors.ListActiveSelectionBackground)
			if !b.active {
				opts.Color = foundation.Some(b.colors.ListInactiveSelectionBackground)
			}
		}
		deco = painting.NewBoxDecoration(opts)
	}
	return widgets.NewContainer(widgets.NewAlign(widgets.NewPadding(painting.NewEdgeInsetsFromLTRB(cellPad, 0, cellPad, 0), label),
		widgets.AlignOpts{Alignment: align}),
		containerOpts(g.model.Width(c), deco))
}

// header is the line of the titles: the corner that selects all, and a title for each column that sorts when it is clicked and
// resizes when its right edge is dragged.
func (b *builder) header() widgets.Widget {
	g := b.g
	m := g.model
	colors := b.colors
	cells := []widgets.Widget{
		widgets.NewListener(widgets.NewSizedBox(nil, widgets.SizedBoxOpts{Width: foundation.Some(gutterWidth)}), widgets.ListenerOpts{
			Behavior: rendering.HitTestBehaviorOpaque,
			OnPointerDown: func(e *gestures.PointerDownEvent) {
				if isPrimary(e) {
					g.focus.RequestFocus()
					m.SelectAll()
					g.refresh()
				}
			},
		}),
	}
	sortCol, sortDir := m.SortState()
	bold := b.style.CopyWith(painting.TextStyleOpts{FontWeight: gui.FontWeightW600})
	for c, col := range m.Columns() {
		title := []widgets.Widget{widgets.NewFlexible(widgets.NewText(col.Name, widgets.TextOpts{Style: bold, MaxLines: foundation.Some(1), Overflow: painting.TextOverflowEllipsis}))}
		if sortCol == c && sortDir != SortNone {
			icon := "arrow-up"
			if sortDir == SortDesc {
				icon = "arrow-down"
			}
			title = append(title, widgets.NewSizedBox(nil, widgets.SizedBoxOpts{Width: foundation.Some(4.0)}), ui.Icon(icon, 12, colors.Foreground))
		}
		tip := col.Name
		if col.Type != "" {
			tip += "  (" + col.Type + ")"
		}
		label := ui.NewHover(widgets.NewRow(title, widgets.RowOpts{CrossAxisAlignment: rendering.CrossAxisAlignmentCenter}), ui.HoverOpts{Text: tip, Compact: true})
		sortArea := widgets.NewListener(
			widgets.NewPadding(painting.NewEdgeInsetsFromLTRB(cellPad, 0, 2, 0), widgets.NewAlign(label, widgets.AlignOpts{Alignment: painting.AlignmentCenterLeft})),
			widgets.ListenerOpts{Behavior: rendering.HitTestBehaviorOpaque, OnPointerDown: func(e *gestures.PointerDownEvent) {
				switch {
				case isSecondary(e):
					g.showMenu(e.Position(), -1, c)
				case isPrimary(e):
					g.focus.RequestFocus()
					m.CycleSort(c)
					g.refresh()
				}
			}})
		line := widgets.NewAlign(widgets.NewColoredBox(colors.PanelBorder, widgets.NewSizedBox(nil, widgets.SizedBoxOpts{Width: foundation.Some(1.0)})),
			widgets.AlignOpts{Alignment: painting.AlignmentCenterRight})
		handle := widgets.NewMouseRegion(widgets.NewListener(line,
			widgets.ListenerOpts{
				Behavior:      rendering.HitTestBehaviorOpaque,
				OnPointerDown: func(e *gestures.PointerDownEvent) { g.startResize(c, e) },
				OnPointerMove: func(e *gestures.PointerMoveEvent) { g.moveResize(e) },
				OnPointerUp:   func(*gestures.PointerUpEvent) { g.resize.active = false },
			}), widgets.MouseRegionOpts{Cursor: services.SystemMouseCursors.ResizeColumn})
		cells = append(cells, widgets.NewSizedBox(widgets.NewRow([]widgets.Widget{
			widgets.NewExpanded(sortArea),
			widgets.NewSizedBox(handle, widgetsSizedWidth(handleWidth)),
		}, widgets.RowOpts{CrossAxisAlignment: rendering.CrossAxisAlignmentStretch}), widgets.SizedBoxOpts{Width: foundation.Some(m.Width(c))}))
	}
	row := widgets.NewSizedBox(widgets.NewRow(cells, widgets.RowOpts{CrossAxisAlignment: rendering.CrossAxisAlignmentStretch}),
		widgets.SizedBoxOpts{Height: foundation.Some(headerHeight)})
	border := painting.NewBoxDecoration(painting.BoxDecorationOpts{
		Color:  foundation.Some(colors.SideBarSectionHeaderBackground),
		Border: painting.Border{Bottom: painting.NewBorderSide(painting.BorderSideOpts{Color: foundation.Some(colors.PanelBorder), Width: foundation.Some(1.0)})},
	})
	return widgets.NewDecoratedBox(border, row)
}

// widgetsSizedWidth is the options of a box of a width.
func widgetsSizedWidth(w float64) widgets.SizedBoxOpts {
	return widgets.SizedBoxOpts{Width: foundation.Some(w)}
}

// footer is the status line: the rows, what is selected, and the button that fetches more.
func (b *builder) footer() widgets.Widget {
	g := b.g
	m := g.model
	colors := b.colors
	var parts []string
	parts = append(parts, plural(m.Len(), "row"))
	if sel := m.SelectedCells(); sel > 1 {
		parts = append(parts, plural(sel, "cell")+" selected")
	}
	if col, dir := m.SortState(); dir != SortNone {
		arrow := "ascending"
		if dir == SortDesc {
			arrow = "descending"
		}
		parts = append(parts, "sorted by "+m.Columns()[col].Name+" "+arrow)
	}
	text := ""
	for i, p := range parts {
		if i > 0 {
			text += "  ·  "
		}
		text += p
	}
	row := []widgets.Widget{
		widgets.NewPadding(painting.NewEdgeInsetsFromLTRB(cellPad, 0, 0, 0),
			widgets.NewText(text, widgets.TextOpts{Style: ui.TextStyle(b.bc, 12, colors.DescriptionForeground), MaxLines: foundation.Some(1), Overflow: painting.TextOverflowEllipsis})),
		widgets.NewSpacer(),
	}
	if g.more && g.opts.OnFetchMore != nil {
		row = append(row, ui.NewButton("Fetch More", ui.ButtonOpts{Small: true, Secondary: true, OnPressed: g.opts.OnFetchMore}),
			widgets.NewSizedBox(nil, widgets.SizedBoxOpts{Width: foundation.Some(6.0)}))
	}
	deco := painting.NewBoxDecoration(painting.BoxDecorationOpts{
		Color:  foundation.Some(colors.EditorBackground),
		Border: painting.Border{Top: painting.NewBorderSide(painting.BorderSideOpts{Color: foundation.Some(colors.PanelBorder), Width: foundation.Some(1.0)})},
	})
	return widgets.NewDecoratedBox(deco, widgets.NewSizedBox(widgets.NewRow(row, widgets.RowOpts{CrossAxisAlignment: rendering.CrossAxisAlignmentCenter}),
		widgets.SizedBoxOpts{Height: foundation.Some(footerHeight)}))
}

// plural writes a count and its noun: "1 row", "2 rows", "100,000 rows".
func plural(n int, noun string) string {
	s := commas(n) + " " + noun
	if n != 1 {
		s += "s"
	}
	return s
}

// commas writes a whole number with a comma between the thousands.
func commas(n int) string {
	s := strconv.Itoa(n)
	if n < 1000 {
		return s
	}
	var out []byte
	for i := range s {
		if i > 0 && (len(s)-i)%3 == 0 {
			out = append(out, ',')
		}
		out = append(out, s[i])
	}
	return string(out)
}
