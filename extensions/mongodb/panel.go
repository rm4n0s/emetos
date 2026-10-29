package mongodb

import (
	"strconv"
	"strings"

	"github.com/rm4n0s/glutter/foundation"
	"github.com/rm4n0s/glutter/gestures"
	"github.com/rm4n0s/glutter/painting"
	"github.com/rm4n0s/glutter/rendering"
	"github.com/rm4n0s/glutter/services"
	gui "github.com/rm4n0s/glutter/ui"
	"github.com/rm4n0s/glutter/widgets"

	"github.com/rm4n0s/emetos/extension/ui"
)

// The look of the panel (SPEC §10.22 "Results"; the same lines as the Query Results of SQL).
const (
	// toolbarHeight is the height of the line above the tabs, tabHeight the height of the tabs and footerHeight that of the line
	// under the documents.
	toolbarHeight = 30.0
	tabHeight     = 28.0
	footerHeight  = 28.0
	// fontSize is the size of the text of the panel.
	fontSize = 12.0
)

// build is the content of the panel tab "MongoDB Results": the toolbar, the tabs of the sets of documents and of the messages, and the
// view or the messages under them. It runs on the UI thread.
func (q *playgrounds) build(bc widgets.BuildContext) widgets.Widget {
	colors := ui.Theme(bc)
	var body widgets.Widget
	ed := q.shown
	if ed == nil || ed.rec == nil {
		body = q.empty(bc, colors)
	} else {
		rec := ed.rec
		rec.tab = min(max(rec.tab, 0), len(rec.sets))
		body = widgets.NewColumn([]widgets.Widget{
			q.toolbar(bc, colors, ed, rec),
			q.tabs(bc, colors, rec),
			widgets.NewExpanded(q.content(bc, colors, ed, rec)),
		}, widgets.ColumnOpts{CrossAxisAlignment: rendering.CrossAxisAlignmentStretch})
	}
	if q.focus == nil {
		q.focus = widgets.NewFocusNode(widgets.FocusNodeOpts{DebugLabel: "mongodb results"})
	}
	// a press anywhere in the panel gives it the keyboard, so that Escape reaches onKey
	pressed := widgets.NewListener(widgets.NewColoredBox(colors.PanelBackground, body), widgets.ListenerOpts{
		Behavior:      rendering.HitTestBehaviorTranslucent,
		OnPointerDown: func(*gestures.PointerDownEvent) { q.focus.RequestFocus() },
	})
	return ui.NewContextMenu(widgets.NewFocus(pressed, widgets.FocusOpts{FocusNode: q.focus, OnKeyEvent: q.onKey}), ui.ContextMenuOpts{Controller: q.menu})
}

// onKey cancels the running statement on Escape when the keyboard is in the results.
func (q *playgrounds) onKey(_ *widgets.FocusNode, e services.KeyEvent) widgets.KeyEventResult {
	if _, down := e.(*services.KeyDownEvent); !down || e.LogicalKey() != services.LogicalKeyboardKeyEscape {
		return widgets.KeyEventResultIgnored
	}
	if q.shown == nil || q.shown.running == nil {
		return widgets.KeyEventResultIgnored
	}
	q.cancel()
	return widgets.KeyEventResultHandled
}

// empty is what the panel shows before anything ran.
func (q *playgrounds) empty(bc widgets.BuildContext, colors ui.Colors) widgets.Widget {
	style := ui.TextStyle(bc, 13, colors.DescriptionForeground)
	return widgets.NewPadding(painting.NewEdgeInsetsAll(14), widgets.NewAlign(
		widgets.NewText("Run a playground with Ctrl+Enter, or all of it with Ctrl+Shift+Enter, or open a collection from the Databases view. The results show here.", widgets.TextOpts{Style: style}),
		widgets.AlignOpts{Alignment: painting.AlignmentTopLeft}))
}

// gapBox is a space of a width.
func gapBox(w float64) widgets.Widget {
	return widgets.NewSizedBox(nil, widgets.SizedBoxOpts{Width: foundation.Some(w)})
}

// toolbar is the line above the tabs: what ran, the three views, and the actions on a document.
func (q *playgrounds) toolbar(bc widgets.BuildContext, colors ui.Colors, ed *editor, rec *runRecord) widgets.Widget {
	headline := ed.name + " · " + rec.connection
	if rec.database != "" {
		headline += " · " + rec.database
	}
	text := headline + " · " + rec.summary()
	row := []widgets.Widget{widgets.NewExpanded(widgets.NewPadding(painting.NewEdgeInsetsFromLTRB(10, 0, 10, 0), widgets.NewText(text, widgets.TextOpts{
		Style: ui.TextStyle(bc, fontSize, colors.DescriptionForeground), MaxLines: foundation.Some(1), Overflow: painting.TextOverflowEllipsis,
	})))}
	button := func(label, tip string, secondary bool, on func()) widgets.Widget {
		return ui.NewButton(label, ui.ButtonOpts{Small: true, Secondary: secondary, Tooltip: tip, OnPressed: on})
	}
	if ed.busy() {
		row = append(row, button("Cancel", "Stop the statement (Escape)", true, q.cancel), gapBox(8))
	} else if sr := q.currentSet(rec); sr != nil {
		for _, m := range []struct {
			mode  viewMode
			label string
		}{{ModeTree, "Tree"}, {ModeJSON, "JSON"}, {ModeTable, "Table"}} {
			row = append(row, button(m.label, "Show the documents as "+m.label, q.mode != m.mode, func() { q.setMode(m.mode) }), gapBox(2))
		}
		row = append(row, gapBox(6))
		if sr.set.Editable && !q.readOnly(rec) {
			row = append(row,
				button("Edit", "Open the selected document as Extended JSON", true, q.editSelected), gapBox(4),
				button("Delete", "Delete the selected document from the collection", true, q.deleteSelected), gapBox(4))
		}
		row = append(row, button("Copy", "Copy the selected document, or all of them, as Extended JSON", true, q.copySelected), gapBox(8))
	}
	deco := painting.NewBoxDecoration(painting.BoxDecorationOpts{
		Border: painting.Border{Bottom: painting.NewBorderSide(painting.BorderSideOpts{Color: foundation.Some(colors.PanelBorder), Width: foundation.Some(1.0)})},
	})
	return widgets.NewDecoratedBox(deco, widgets.NewSizedBox(widgets.NewRow(row, widgets.RowOpts{CrossAxisAlignment: rendering.CrossAxisAlignmentCenter}),
		widgets.SizedBoxOpts{Height: foundation.Some(toolbarHeight)}))
}

// readOnly tells whether the connection of the run is read-only.
func (q *playgrounds) readOnly(rec *runRecord) bool {
	c, ok := q.connectionOf(rec.connID)
	return ok && c.ReadOnly
}

// currentSet is the set whose tab is open, or nil when the Messages are.
func (q *playgrounds) currentSet(rec *runRecord) *setRecord {
	if rec.tab >= 0 && rec.tab < len(rec.sets) {
		return rec.sets[rec.tab]
	}
	return nil
}

// setMode changes how the documents are shown.
func (q *playgrounds) setMode(m viewMode) {
	if q.mode != m {
		q.mode = m
		q.refresh()
	}
}

// tabs is the strip of the sets of documents and the Messages.
func (q *playgrounds) tabs(bc widgets.BuildContext, colors ui.Colors, rec *runRecord) widgets.Widget {
	var tabs []widgets.Widget
	add := func(i int, title string) {
		active := rec.tab == i
		color := colors.PanelTitleInactiveForeground
		border := painting.BorderSideOpts{Color: foundation.Some(colors.PanelBorder), Width: foundation.Some(0.0)}
		if active {
			color = colors.PanelTitleActiveForeground
			border = painting.BorderSideOpts{Color: foundation.Some(colors.PanelTitleActiveBorder), Width: foundation.Some(1.0)}
		}
		deco := painting.NewBoxDecoration(painting.BoxDecorationOpts{Border: painting.Border{Bottom: painting.NewBorderSide(border)}})
		label := widgets.NewDecoratedBox(deco, widgets.NewPadding(painting.NewEdgeInsetsSymmetric(0, 12), widgets.NewAlign(
			widgets.NewText(title, widgets.TextOpts{Style: ui.TextStyle(bc, fontSize, color), MaxLines: foundation.Some(1)}),
			widgets.AlignOpts{Alignment: painting.AlignmentCenter})))
		tabs = append(tabs, widgets.NewMouseRegion(widgets.NewListener(label, widgets.ListenerOpts{
			Behavior: rendering.HitTestBehaviorOpaque,
			OnPointerDown: func(e *gestures.PointerDownEvent) {
				if e.Buttons()&gestures.SecondaryButton == 0 {
					rec.tab = i
					q.refresh()
				}
			},
		}), widgets.MouseRegionOpts{Cursor: services.SystemMouseCursors.Click}))
	}
	for i, sr := range rec.sets {
		add(i, sr.title())
	}
	add(len(rec.sets), "Messages")
	return widgets.NewSizedBox(widgets.NewRow(tabs, widgets.RowOpts{CrossAxisAlignment: rendering.CrossAxisAlignmentStretch}), widgets.SizedBoxOpts{Height: foundation.Some(tabHeight)})
}

// content is the documents of the open set in the view that is chosen, or the messages.
func (q *playgrounds) content(bc widgets.BuildContext, colors ui.Colors, ed *editor, rec *runRecord) widgets.Widget {
	sr := q.currentSet(rec)
	if sr == nil {
		return q.messages(bc, colors, ed, rec)
	}
	var view widgets.Widget
	switch q.mode {
	case ModeJSON:
		view = q.jsonWidget(sr)
	case ModeTable:
		view = q.tableOf(ed, rec, sr).grid.Widget()
	default:
		view = q.treeWidget(sr)
	}
	children := []widgets.Widget{widgets.NewExpanded(view)}
	if q.mode != ModeTable {
		children = append(children, q.footer(bc, colors, ed, rec, sr))
	}
	return widgets.NewColumn(children, widgets.ColumnOpts{CrossAxisAlignment: rendering.CrossAxisAlignmentStretch})
}

// footer is the line under the tree and the JSON: how many documents are loaded, and More.
func (q *playgrounds) footer(bc widgets.BuildContext, colors ui.Colors, ed *editor, rec *runRecord, sr *setRecord) widgets.Widget {
	text := documentsText(len(sr.set.Docs), sr.set.More()) + " loaded"
	if sr.set.Truncated {
		text += ". Reading stops at " + groupDigits(maxSetDocs) + " documents."
	}
	row := []widgets.Widget{
		widgets.NewExpanded(widgets.NewPadding(painting.NewEdgeInsetsFromLTRB(10, 0, 10, 0), widgets.NewText(text, widgets.TextOpts{
			Style: ui.TextStyle(bc, fontSize, colors.DescriptionForeground), MaxLines: foundation.Some(1), Overflow: painting.TextOverflowEllipsis,
		}))),
	}
	if sr.set.More() {
		row = append(row, ui.NewButton("More", ui.ButtonOpts{
			Small: true, Secondary: true, Disabled: rec.fetching, Tooltip: "Read the next " + strconv.Itoa(sr.set.PageSize) + " documents",
			OnPressed: func() { q.fetchMore(ed, rec, sr) },
		}), gapBox(8))
	}
	deco := painting.NewBoxDecoration(painting.BoxDecorationOpts{
		Border: painting.Border{Top: painting.NewBorderSide(painting.BorderSideOpts{Color: foundation.Some(colors.PanelBorder), Width: foundation.Some(1.0)})},
	})
	return widgets.NewDecoratedBox(deco, widgets.NewSizedBox(widgets.NewRow(row, widgets.RowOpts{CrossAxisAlignment: rendering.CrossAxisAlignmentCenter}),
		widgets.SizedBoxOpts{Height: foundation.Some(footerHeight)}))
}

// messages lists what each statement said: what it returned or changed, how long it took, and its error with its code and the place of
// the statement in the editor.
func (q *playgrounds) messages(bc widgets.BuildContext, colors ui.Colors, ed *editor, rec *runRecord) widgets.Widget {
	var lines []widgets.Widget
	text := ui.TextStyle(bc, 13, colors.Foreground)
	dim := ui.TextStyle(bc, 12, colors.DescriptionForeground)
	line := func(icon string, iconColor gui.Color, style *painting.TextStyle, msg string, link func(), linkText string) {
		parts := []widgets.Widget{
			widgets.NewSizedBox(ui.Icon(icon, 14, iconColor), widgets.SizedBoxOpts{Width: foundation.Some(18.0)}),
			widgets.NewFlexible(widgets.NewText(msg, widgets.TextOpts{Style: style, SoftWrap: foundation.Some(true)})),
		}
		if link != nil {
			linkStyle := ui.TextStyle(bc, 12, colors.TextLinkForeground)
			parts = append(parts, gapBox(8), widgets.NewMouseRegion(widgets.NewListener(widgets.NewText(linkText, widgets.TextOpts{Style: linkStyle}), widgets.ListenerOpts{
				Behavior: rendering.HitTestBehaviorOpaque, OnPointerDown: func(*gestures.PointerDownEvent) { link() },
			}), widgets.MouseRegionOpts{Cursor: services.SystemMouseCursors.Click}))
		}
		lines = append(lines, widgets.NewPadding(painting.NewEdgeInsetsFromLTRB(12, 3, 12, 3), widgets.NewRow(parts, widgets.RowOpts{CrossAxisAlignment: rendering.CrossAxisAlignmentStart})))
	}
	// the place of a statement in the document links back to it, for a playground that is a document
	placeLink := func(st *stepRecord) (func(), string) {
		if st.line == 0 || strings.HasPrefix(ed.key, "collection:") {
			return nil, ""
		}
		uri, l, c := ed.key, st.line, st.column
		return func() {
			if err := q.api.Documents().Show(uri, l, c, false); err != nil {
				q.api.Log().Warn("the editor cannot be shown at the statement", "err", err)
			}
		}, "Line " + strconv.Itoa(l) + ", column " + strconv.Itoa(c)
	}
	if rec.refused != "" && len(rec.steps) == 0 {
		line("circle-slash", colors.ProblemsWarningIconForeground, text, rec.refused, nil, "")
	}
	for i, st := range rec.steps {
		res := st.res
		header := "[" + strconv.Itoa(i+1) + "] " + firstLine(res.Item.Text, 100)
		line("symbol-event", colors.DescriptionForeground, dim, header, nil, "")
		switch {
		case res.Cancelled:
			line("circle-slash", colors.ProblemsWarningIconForeground, text, "Cancelled after "+formatElapsed(res.Elapsed)+".", nil, "")
		case res.Err != nil:
			link, linkText := placeLink(st)
			line("error", colors.ProblemsErrorIconForeground, ui.TextStyle(bc, 13, colors.ErrorForeground), res.Err.Text(), link, linkText)
		default:
			line("pass", colors.ProblemsInfoIconForeground, text, res.Message+" in "+formatElapsed(res.Elapsed), nil, "")
		}
	}
	if rec.refused != "" && len(rec.steps) > 0 {
		line("circle-slash", colors.ProblemsWarningIconForeground, dim, rec.refused, nil, "")
	} else if rec.running {
		line("loading", colors.DescriptionForeground, dim, "Running statement "+strconv.Itoa(rec.current)+" of "+strconv.Itoa(rec.total)+"...", nil, "")
	} else if rec.skipped > 0 {
		line("warning", colors.ProblemsWarningIconForeground, dim, plural(rec.skipped, "statement")+" not run.", nil, "")
	}
	return ui.NewScrollView(widgets.NewPadding(painting.NewEdgeInsetsFromLTRB(0, 4, 0, 8), widgets.NewColumn(lines, widgets.ColumnOpts{
		CrossAxisAlignment: rendering.CrossAxisAlignmentStretch,
	})), nil)
}
