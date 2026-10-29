package sql

import (
	"strconv"

	"github.com/rm4n0s/glutter/foundation"
	"github.com/rm4n0s/glutter/gestures"
	"github.com/rm4n0s/glutter/painting"
	"github.com/rm4n0s/glutter/rendering"
	"github.com/rm4n0s/glutter/services"
	gui "github.com/rm4n0s/glutter/ui"
	"github.com/rm4n0s/glutter/widgets"

	"github.com/rm4n0s/emetos/extension/ui"
)

// The look of the panel (SPEC §10.21 "The results panel").
const (
	// toolbarHeight is the height of the line above the tabs, and tabHeight the height of the tabs.
	toolbarHeight = 30.0
	tabHeight     = 28.0
	// fontSize is the size of the text of the panel.
	fontSize = 12.0
	// historyWidth is the width of the list of earlier runs.
	historyWidth = 230.0
)

// build is the content of the panel tab "Query Results": the toolbar (what ran, the earlier runs, export, cancel), the tabs of the
// result sets and of the messages, and the grid or the messages under them. It runs on the UI thread.
func (q *queries) build(bc widgets.BuildContext) widgets.Widget {
	colors := ui.Theme(bc)
	var body widgets.Widget
	ed := q.shown
	if ed == nil || ed.shown == nil {
		body = q.empty(bc, colors)
	} else {
		rec := ed.shown
		rec.tab = min(max(rec.tab, 0), len(rec.sets))
		body = widgets.NewColumn([]widgets.Widget{
			q.toolbar(bc, colors, ed, rec),
			q.tabs(bc, colors, rec),
			widgets.NewExpanded(q.content(bc, colors, ed, rec)),
		}, widgets.ColumnOpts{CrossAxisAlignment: rendering.CrossAxisAlignmentStretch})
	}
	if q.focus == nil {
		q.focus = widgets.NewFocusNode(widgets.FocusNodeOpts{DebugLabel: "query results"})
	}
	// a press anywhere in the panel gives it the keyboard, so that Escape reaches onKey; the grid and the buttons take it from here
	// when the press is theirs
	pressed := widgets.NewListener(widgets.NewColoredBox(colors.PanelBackground, body), widgets.ListenerOpts{
		Behavior:      rendering.HitTestBehaviorTranslucent,
		OnPointerDown: func(*gestures.PointerDownEvent) { q.focus.RequestFocus() },
	})
	return widgets.NewFocus(pressed, widgets.FocusOpts{FocusNode: q.focus, OnKeyEvent: q.onKey})
}

// onKey cancels the running statement on Escape when the keyboard is in the results.
func (q *queries) onKey(_ *widgets.FocusNode, e services.KeyEvent) widgets.KeyEventResult {
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
func (q *queries) empty(bc widgets.BuildContext, colors ui.Colors) widgets.Widget {
	style := ui.TextStyle(bc, 13, colors.DescriptionForeground)
	return widgets.NewPadding(painting.NewEdgeInsetsAll(14), widgets.NewAlign(
		widgets.NewText("Run a query with Ctrl+Enter, or all of them with Ctrl+Shift+Enter. The results show here.", widgets.TextOpts{Style: style}),
		widgets.AlignOpts{Alignment: painting.AlignmentTopLeft}))
}

// toolbar is the line above the tabs.
func (q *queries) toolbar(bc widgets.BuildContext, colors ui.Colors, ed *queryEditor, rec *runRecord) widgets.Widget {
	name := q.nameOf(ed.uri)
	headline := name + " · " + rec.connection
	if rec.database != "" {
		headline += " · " + rec.database
	}
	text := headline + " · " + rec.summary()
	children := []widgets.Widget{
		widgets.NewPadding(painting.NewEdgeInsetsFromLTRB(10, 0, 10, 0), widgets.NewText(text, widgets.TextOpts{
			Style: ui.TextStyle(bc, fontSize, colors.DescriptionForeground), MaxLines: foundation.Some(1), Overflow: painting.TextOverflowEllipsis,
		})),
	}
	row := []widgets.Widget{widgets.NewExpanded(children[0])}
	if len(ed.runs) > 1 {
		options := make([]ui.DropdownOption, len(ed.runs))
		selected := 0
		for i := range ed.runs {
			r := ed.runs[len(ed.runs)-1-i]
			options[i] = ui.DropdownOption{Label: "Run " + strconv.Itoa(r.n) + " · " + r.started.Format("15:04:05"), Description: r.summary()}
			if r == rec {
				selected = i
			}
		}
		row = append(row, widgets.NewSizedBox(ui.NewDropdown(options, ui.DropdownOpts{
			Selected: selected, Width: foundation.Some(historyWidth), Rows: historyRuns,
			OnChanged: func(i int) {
				if i >= 0 && i < len(ed.runs) {
					ed.shown = ed.runs[len(ed.runs)-1-i]
					q.refresh()
				}
			},
		}), widgets.SizedBoxOpts{Width: foundation.Some(historyWidth), Height: foundation.Some(toolbarHeight - 6)}), widgets.NewSizedBox(nil, widgets.SizedBoxOpts{Width: foundation.Some(6.0)}))
	}
	if ed.busy() {
		row = append(row, ui.NewButton("Cancel", ui.ButtonOpts{
			Small: true, Secondary: true, Tooltip: "Stop the statement (Escape)", OnPressed: q.cancel,
		}), widgets.NewSizedBox(nil, widgets.SizedBoxOpts{Width: foundation.Some(8.0)}))
	} else if _, _, sr := q.shownSet(); sr != nil && sr.set.Table.Len() > 0 {
		row = append(row, widgets.NewText("Export", widgets.TextOpts{Style: ui.TextStyle(bc, fontSize, colors.DescriptionForeground)}), widgets.NewSizedBox(nil, widgets.SizedBoxOpts{Width: foundation.Some(6.0)}))
		for _, f := range []struct{ label, format string }{{"CSV", formatCSV}, {"JSON", formatJSON}, {"SQL", formatSQL}} {
			row = append(row, ui.NewButton(f.label, ui.ButtonOpts{
				Small: true, Secondary: true, Tooltip: "Write the result as " + f.label + " to a file", OnPressed: func() { q.exportAs(f.format) },
			}), widgets.NewSizedBox(nil, widgets.SizedBoxOpts{Width: foundation.Some(4.0)}))
		}
		row = append(row, widgets.NewSizedBox(nil, widgets.SizedBoxOpts{Width: foundation.Some(6.0)}))
	}
	deco := painting.NewBoxDecoration(painting.BoxDecorationOpts{
		Border: painting.Border{Bottom: painting.NewBorderSide(painting.BorderSideOpts{Color: foundation.Some(colors.PanelBorder), Width: foundation.Some(1.0)})},
	})
	return widgets.NewDecoratedBox(deco, widgets.NewSizedBox(widgets.NewRow(row, widgets.RowOpts{CrossAxisAlignment: rendering.CrossAxisAlignmentCenter}),
		widgets.SizedBoxOpts{Height: foundation.Some(toolbarHeight)}))
}

// tabs is the strip of the result sets and the Messages.
func (q *queries) tabs(bc widgets.BuildContext, colors ui.Colors, rec *runRecord) widgets.Widget {
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
		add(i, sr.title(len(rec.sets)))
	}
	add(len(rec.sets), "Messages")
	return widgets.NewSizedBox(widgets.NewRow(tabs, widgets.RowOpts{CrossAxisAlignment: rendering.CrossAxisAlignmentStretch}), widgets.SizedBoxOpts{Height: foundation.Some(tabHeight)})
}

// content is the grid of the selected result set, or the messages.
func (q *queries) content(bc widgets.BuildContext, colors ui.Colors, ed *queryEditor, rec *runRecord) widgets.Widget {
	if rec.tab < len(rec.sets) {
		sr := rec.sets[rec.tab]
		if rec.discarded {
			return q.note(bc, colors, "The rows of this run were dropped to keep the memory of the editor small. Run the query again to see them.")
		}
		return q.gridOf(ed, rec, sr).Widget()
	}
	return q.messages(bc, colors, ed, rec)
}

// note is a line of dimmed text in the body.
func (q *queries) note(bc widgets.BuildContext, colors ui.Colors, text string) widgets.Widget {
	return widgets.NewPadding(painting.NewEdgeInsetsAll(12), widgets.NewAlign(
		widgets.NewText(text, widgets.TextOpts{Style: ui.TextStyle(bc, 13, colors.DescriptionForeground)}), widgets.AlignOpts{Alignment: painting.AlignmentTopLeft}))
}

// messages lists what each statement said: the rows it returned or changed, how long it took, and its error with the place of it in
// the editor.
func (q *queries) messages(bc widgets.BuildContext, colors ui.Colors, ed *queryEditor, rec *runRecord) widgets.Widget {
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
			parts = append(parts, widgets.NewSizedBox(nil, widgets.SizedBoxOpts{Width: foundation.Some(8.0)}),
				widgets.NewMouseRegion(widgets.NewListener(widgets.NewText(linkText, widgets.TextOpts{Style: linkStyle}), widgets.ListenerOpts{
					Behavior: rendering.HitTestBehaviorOpaque, OnPointerDown: func(*gestures.PointerDownEvent) { link() },
				}), widgets.MouseRegionOpts{Cursor: services.SystemMouseCursors.Click}))
		}
		lines = append(lines, widgets.NewPadding(painting.NewEdgeInsetsFromLTRB(12, 3, 12, 3), widgets.NewRow(parts, widgets.RowOpts{CrossAxisAlignment: rendering.CrossAxisAlignmentStart})))
	}
	for i, st := range rec.steps {
		res := st.res
		header := "[" + strconv.Itoa(i+1) + "] " + firstLine(res.Statement.Text, 100)
		line("symbol-event", colors.DescriptionForeground, dim, header, nil, "")
		switch {
		case res.Cancelled:
			line("circle-slash", colors.ProblemsWarningIconForeground, text, "Cancelled after "+formatElapsed(res.Elapsed)+". "+res.Err.Text(), nil, "")
		case res.Err != nil:
			var link func()
			linkText := ""
			if st.line > 0 {
				uri, l, c := ed.uri, st.line, st.column
				link = func() {
					if err := q.api.Documents().Show(uri, l, c, false); err != nil {
						q.api.Log().Warn("the editor cannot be shown at the error", "err", err)
					}
				}
				linkText = "Line " + strconv.Itoa(st.line) + ", column " + strconv.Itoa(st.column)
			}
			line("error", colors.ProblemsErrorIconForeground, ui.TextStyle(bc, 13, colors.ErrorForeground), res.Err.Text(), link, linkText)
		case len(res.Sets) > 0:
			for _, s := range res.Sets {
				msg := rowsText(s.Table.Len(), s.More()) + " returned in " + formatElapsed(res.Elapsed)
				if s.Note != "" {
					msg += ". " + s.Note
				}
				line("pass", colors.ProblemsInfoIconForeground, text, msg, nil, "")
			}
		case res.HasAffected:
			line("pass", colors.ProblemsInfoIconForeground, text, "("+rowsAffected(res.Affected)+") in "+formatElapsed(res.Elapsed), nil, "")
		default:
			line("pass", colors.ProblemsInfoIconForeground, text, "Statement executed in "+formatElapsed(res.Elapsed), nil, "")
		}
	}
	if rec.running {
		line("loading", colors.DescriptionForeground, dim, "Running statement "+strconv.Itoa(rec.current)+" of "+strconv.Itoa(rec.total)+"...", nil, "")
	} else if rec.skipped > 0 {
		line("warning", colors.ProblemsWarningIconForeground, dim, plural(rec.skipped, "statement")+" not run.", nil, "")
	}
	return ui.NewScrollView(widgets.NewPadding(painting.NewEdgeInsetsFromLTRB(0, 4, 0, 8), widgets.NewColumn(lines, widgets.ColumnOpts{
		CrossAxisAlignment: rendering.CrossAxisAlignmentStretch,
	})), nil)
}

// rowsAffected writes the count of rows a statement changed: "1 row affected", "250 rows affected".
func rowsAffected(n int64) string {
	if n == 1 {
		return "1 row affected"
	}
	return groupDigits(int(n)) + " rows affected"
}
