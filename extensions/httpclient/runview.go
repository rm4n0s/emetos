package httpclient

import (
	"strconv"
	"strings"

	"github.com/rm4n0s/glutter/foundation"
	"github.com/rm4n0s/glutter/painting"
	"github.com/rm4n0s/glutter/rendering"
	gui "github.com/rm4n0s/glutter/ui"
	"github.com/rm4n0s/glutter/widgets"

	"github.com/rm4n0s/emetos/extension/ui"
	"github.com/rm4n0s/emetos/extensions/httpclient/store"
	"github.com/rm4n0s/emetos/extensions/internal/pageui"
)

// The widgets of the runner tab.

// runConfigWidth is the width of the column with the choices.
const runConfigWidth = 330.0

// build makes the widget of the tab.
func (t *runTab) build(bc widgets.BuildContext) widgets.Widget {
	colors := ui.Theme(bc)
	dim := ui.TextStyle(bc, 12, colors.DescriptionForeground)
	switch {
	case t.e.tree == nil:
		return tabPad(pageui.Paragraph("Reading the collections...", dim))
	case t.gone:
		return tabPad(pageui.Paragraph("This collection or folder was deleted, renamed or moved. Close the tab.", ui.TextStyle(bc, 13, colors.ErrorForeground)))
	}
	vline := widgets.NewColoredBox(colors.PanelBorder, widgets.NewSizedBox(nil, widgets.SizedBoxOpts{Width: foundation.Some(1.0)}))
	return widgets.NewRow([]widgets.Widget{
		wide(ui.NewScrollView(tabPad(t.config(bc, colors)), &t.leftScroll), runConfigWidth), vline, widgets.NewExpanded(t.resultsPane(bc, colors)),
	}, widgets.RowOpts{CrossAxisAlignment: rendering.CrossAxisAlignmentStretch})
}

// label is a small heading above a box.
func label(bc widgets.BuildContext, colors ui.Colors, text string) widgets.Widget {
	return widgets.NewPadding(painting.NewEdgeInsetsFromLTRB(0, 10, 0, 3), widgets.NewText(text, widgets.TextOpts{Style: ui.TextStyle(bc, 12, colors.DescriptionForeground)}))
}

// config is the column of the choices of a run.
func (t *runTab) config(bc widgets.BuildContext, colors ui.Colors) widgets.Widget {
	e := t.e
	n := t.node()
	dim := ui.TextStyle(bc, 12, colors.DescriptionForeground)
	locked := t.running
	title := widgets.NewText("Run "+n.Name, widgets.TextOpts{Style: pageui.Bold(ui.TextStyle(bc, 15, colors.Foreground)), MaxLines: foundation.Some(2), Overflow: painting.TextOverflowEllipsis})

	options := []ui.DropdownOption{{Label: "No environment"}}
	selected := 0
	for i, env := range e.tree.Environments {
		options = append(options, ui.DropdownOption{Label: env.Name})
		if env.Key == t.envKey {
			selected = i + 1
		}
	}
	envBox := ui.NewDropdown(options, ui.DropdownOpts{Selected: selected, Disabled: locked, OnChanged: func(i int) {
		t.envKey = ""
		if i > 0 {
			t.envKey = e.tree.Environments[i-1].Key
		}
		t.tab.Refresh()
	}})

	box := func(c *ui.InputBoxController, placeholder string, disabled bool) widgets.Widget {
		return ui.NewInputBox(nil, nil, ui.InputBoxOpts{
			Controller: c, Placeholder: placeholder, Disabled: disabled || locked, Monospace: true,
			OnChanged: func(string) { t.note = pageui.Note{} }, OnSubmit: func(string) { t.start() },
		})
	}
	rowsKnown := t.table != nil && t.dataFor == t.dataPath()
	iterHint := "1"
	if rowsKnown {
		iterHint = strconv.Itoa(len(t.table.Rows))
	}
	iterBox := box(t.iterations, iterHint, false)
	if rowsKnown {
		t.iterShown.SetValue(iterHint)
		iterBox = ui.NewInputBox(nil, nil, ui.InputBoxOpts{Controller: t.iterShown, Disabled: true, Monospace: true})
	}

	dataBox := ui.NewInputBox(nil, nil, ui.InputBoxOpts{
		Controller: t.data, Placeholder: "optional: a .csv or .json file", Disabled: locked, Monospace: true,
		OnChanged: func(string) { t.dataEdited() }, OnSubmit: func(string) { t.loadData() },
	})
	dataRow := center(flex(dataBox, 1), pageui.HGap(4),
		ui.NewIconButton("folder-opened", ui.IconButtonOpts{Tooltip: "Choose the data file", Disabled: locked, OnPressed: t.chooseData}),
		ui.NewIconButton("close", ui.IconButtonOpts{Tooltip: "Run without a data file", Disabled: locked || t.data.Value() == "", OnPressed: t.clearData}))
	dataNote := pageui.Nothing()
	switch {
	case t.dataErr != "":
		dataNote = pageui.Paragraph(t.dataErr, ui.TextStyle(bc, 12, colors.ErrorForeground))
	case rowsKnown:
		dataNote = pageui.Paragraph(plural(len(t.table.Rows), "row", "rows")+": "+strings.Join(t.table.Columns, ", ")+". One iteration for each row; the columns are variables.", dim)
	}

	reqs := t.requests()
	picked := len(t.selected())
	list := []widgets.Widget{
		center(widgets.NewText("Requests ("+strconv.Itoa(picked)+" of "+strconv.Itoa(len(reqs))+")", widgets.TextOpts{Style: dim}), widgets.NewSpacer(),
			ui.NewButton("All", ui.ButtonOpts{OnPressed: func() { t.setAll(true) }, Small: true, Secondary: true, Disabled: locked, Tooltip: "Run every request"}), pageui.HGap(4),
			ui.NewButton("None", ui.ButtonOpts{OnPressed: func() { t.setAll(false) }, Small: true, Secondary: true, Disabled: locked, Tooltip: "Run no request"})),
		pageui.Gap(4),
	}
	if len(reqs) == 0 {
		list = append(list, pageui.Paragraph("This has no requests yet.", dim))
	}
	for _, r := range reqs {
		r := r
		ok := runnable(r.node)
		row := center(
			ui.NewCheckbox("", ui.CheckboxOpts{Checked: ok && !t.skip[r.node.Path], Disabled: locked || !ok, OnChanged: func(on bool) { t.toggle(r.node.Path, on) }}),
			pageui.HGap(2), methodBadge(bc, methodOf(r.node)),
			flex(rowText(bc, r.rel, colors.Foreground), 1))
		if !ok {
			row = ui.NewHover(row, ui.HoverOpts{Text: "This request cannot be run: " + problemOf(r.node), Compact: true})
		}
		list = append(list, row)
	}
	return stretch(
		title,
		label(bc, colors, "Environment"), envBox,
		label(bc, colors, "Iterations"), iterBox,
		label(bc, colors, "Delay between requests (ms)"), box(t.delay, "0", false),
		pageui.Gap(10),
		ui.NewCheckbox("Stop on the first failure", ui.CheckboxOpts{Checked: t.stop, Disabled: locked, OnChanged: func(on bool) { t.stop = on; t.tab.Refresh() }}),
		label(bc, colors, "Data file"), dataRow, pageui.Gap(2), dataNote,
		pageui.Gap(14),
		stretch(list...),
	)
}

// problemOf is why a request of the list cannot be run.
func problemOf(n *store.Node) string {
	switch {
	case n.ReadOnly:
		return "it is from a newer version of the format."
	case n.Err != nil:
		return n.Err.Error()
	}
	return "it cannot be read."
}

// methodOf is the method of a request of the list, "?" when its file could not be read.
func methodOf(n *store.Node) string {
	if n.Request == nil {
		return "?"
	}
	return n.Request.Method
}

// resultsPane is the column of the buttons, the summary and the table of results.
func (t *runTab) resultsPane(bc widgets.BuildContext, colors ui.Colors) widgets.Widget {
	dim := ui.TextStyle(bc, 12, colors.DescriptionForeground)
	run := ui.NewButton("Run", ui.ButtonOpts{OnPressed: t.start, Disabled: t.running || len(t.selected()) == 0, Tooltip: "Send the checked requests in order"})
	if t.running {
		run = ui.NewButton("Run", ui.ButtonOpts{Disabled: true})
	}
	cancel := ui.NewButton("Cancel", ui.ButtonOpts{OnPressed: t.cancelRun, Secondary: true, Disabled: !t.running, Tooltip: "Stop the run: the request that is being sent is canceled and no other is sent"})
	save := ui.NewButton("Save Results...", ui.ButtonOpts{OnPressed: t.saveResults, Secondary: true, Disabled: t.running || len(t.results) == 0, Tooltip: "Write the results of the run to a JSON file"})
	bar := center(run, pageui.HGap(6), cancel, pageui.HGap(6), save, widgets.NewSpacer(), t.iterationPicker())
	head := []widgets.Widget{bar, pageui.NoteLine(bc, colors, t.note), pageui.Gap(6), t.summary(bc, colors)}
	body := widgets.NewExpanded(widgets.NewPadding(painting.NewEdgeInsetsFromLTRB(0, 8, 0, 0), t.table_(bc, colors)))
	if len(t.results) == 0 && !t.running {
		n := len(t.selected())
		body = widgets.NewExpanded(widgets.NewPadding(painting.NewEdgeInsetsFromLTRB(0, 16, 0, 0), pageui.Paragraph(
			"Run sends "+plural(n, "request", "requests")+" in order, one at a time. A value that a request extracts is there for the next one. "+
				"The results are shown here; they are gone when this tab closes (Save Results keeps them in a file).", dim)))
	}
	return widgets.NewPadding(painting.NewEdgeInsetsFromLTRB(16, 12, 16, 8), widgets.NewColumn(append(head, body), widgets.ColumnOpts{CrossAxisAlignment: rendering.CrossAxisAlignmentStretch}))
}

// iterationPicker is the dropdown of the iterations, only when a run has more than one.
func (t *runTab) iterationPicker() widgets.Widget {
	if t.total < 2 {
		return pageui.Nothing()
	}
	options := []ui.DropdownOption{{Label: "All iterations"}}
	for i := 1; i <= t.total; i++ {
		options = append(options, ui.DropdownOption{Label: "Iteration " + strconv.Itoa(i)})
	}
	return wide(ui.NewDropdown(options, ui.DropdownOpts{Selected: t.filter, OnChanged: func(i int) {
		t.filter = i
		t.tab.Refresh()
	}}), 170)
}

// summary is the line over the table: how the run is going or how it ended, and the totals.
func (t *runTab) summary(bc widgets.BuildContext, colors ui.Colors) widgets.Widget {
	if len(t.results) == 0 && !t.running && t.end == 0 {
		return pageui.Nothing()
	}
	tot := totalsOf(t.results)
	state, color := "", colors.DescriptionForeground
	switch {
	case t.running:
		state = "Running"
		if last := len(t.results); last > 0 && t.total > 1 {
			state += " iteration " + strconv.Itoa(t.results[last-1].Iteration) + " of " + strconv.Itoa(t.total)
		}
		state += "..."
	case t.end == runCompleted && tot.Failed == 0 && tot.Errors == 0:
		state, color = "Completed", colGood
	case t.end == runCompleted:
		state, color = "Completed with failures", colors.ErrorForeground
	case t.end == runStopped:
		state, color = "Stopped on the first failure", colors.ErrorForeground
	case t.end == runCanceled:
		state, color = "Canceled", colors.EditorWarningForeground
	case t.end == runNotStarted:
		state, color = "Not started", colors.ErrorForeground
	}
	parts := []string{plural(tot.Requests, "request", "requests")}
	if t.total > 1 {
		parts = append([]string{plural(t.total, "iteration", "iterations")}, parts...)
	}
	if tot.Passed+tot.Failed > 0 {
		parts = append(parts, strconv.Itoa(tot.Passed)+" checks passed, "+strconv.Itoa(tot.Failed)+" failed")
	}
	if tot.Errors > 0 {
		parts = append(parts, plural(tot.Errors, "error", "errors"))
	}
	parts = append(parts, durationText(tot.Time))
	return stretch(
		widgets.NewText(state, widgets.TextOpts{Style: pageui.Bold(ui.TextStyle(bc, 13, color))}),
		widgets.NewText(strings.Join(parts, "  ·  "), widgets.TextOpts{Style: ui.TextStyle(bc, 12, colors.DescriptionForeground), MaxLines: foundation.Some(2), Overflow: painting.TextOverflowEllipsis}),
	)
}

// resLine is a line of the table: the heading of an iteration, or a result.
type resLine struct {
	header bool
	// iteration is the iteration of a heading, result the index of a result.
	iteration, result int
}

// lines lists what the table shows: with several iterations, a heading before the first result of each.
func (t *runTab) lines() []resLine {
	var out []resLine
	last := 0
	for i, r := range t.results {
		if t.filter > 0 && r.Iteration != t.filter {
			continue
		}
		if t.total > 1 && r.Iteration != last {
			out = append(out, resLine{header: true, iteration: r.Iteration})
			last = r.Iteration
		}
		out = append(out, resLine{result: i})
	}
	return out
}

// The columns of the table of results.
const (
	colMark   = 28.0
	colState  = 70.0
	colTime   = 66.0
	colSize   = 66.0
	colChecks = 60.0
)

// table_ is the header and the lazily built rows of the results.
func (t *runTab) table_(bc widgets.BuildContext, colors ui.Colors) widgets.Widget {
	head := pageui.Bold(ui.TextStyle(bc, 12, colors.DescriptionForeground))
	text := func(s string) widgets.Widget {
		return widgets.NewText(s, widgets.TextOpts{Style: head, MaxLines: foundation.Some(1)})
	}
	header := widgets.NewRow([]widgets.Widget{
		wide(nil, colMark), wide(text("Result"), colState), wide(nil, 44), flex(text("Request"), 3), flex(text("Status"), 2),
		wide(text("Time"), colTime), wide(text("Size"), colSize), wide(text("Checks"), colChecks),
	}, widgets.RowOpts{})
	lines := t.lines()
	item := func(_ widgets.BuildContext, i int) widgets.Widget {
		if i < 0 || i >= len(lines) {
			return nil
		}
		l := lines[i]
		if l.header {
			return t.iterationHeader(bc, colors, l.iteration)
		}
		return t.resultRow(bc, colors, l.result)
	}
	sliver := widgets.NewSliverListBuilder(item, widgets.SliverBuilderOpts{
		ItemCount: foundation.Some(len(lines)), AddAutomaticKeepAlives: foundation.Some(false), AddRepaintBoundaries: foundation.Some(false),
		AddSemanticIndexes: foundation.Some(false),
	})
	view := widgets.NewCustomScrollView([]widgets.Widget{sliver}, widgets.ScrollViewOpts{Controller: t.listCtl})
	return widgets.NewColumn([]widgets.Widget{header, pageui.Gap(4), widgets.NewExpanded(ui.NewScrollable(view, ui.ScrollableOpts{Controller: t.listCtl}))},
		widgets.ColumnOpts{CrossAxisAlignment: rendering.CrossAxisAlignmentStretch})
}

// iterationHeader is the heading of an iteration: its number and the first values of its row of the data file.
func (t *runTab) iterationHeader(bc widgets.BuildContext, colors ui.Colors, it int) widgets.Widget {
	s := "Iteration " + strconv.Itoa(it) + " of " + strconv.Itoa(t.total)
	if t.table != nil && it-1 < len(t.table.Rows) {
		row := t.table.Rows[it-1]
		var vals []string
		for _, c := range t.table.Columns {
			if len(vals) == 4 {
				vals = append(vals, "...")
				break
			}
			vals = append(vals, c+"="+clipText(row[c], 24))
		}
		s += "   " + strings.Join(vals, ", ")
	}
	return widgets.NewPadding(painting.NewEdgeInsetsFromLTRB(0, 10, 0, 3), widgets.NewText(s, widgets.TextOpts{
		Style: pageui.Bold(ui.TextStyle(bc, 12, colors.TextLinkForeground)), MaxLines: foundation.Some(1), Overflow: painting.TextOverflowEllipsis,
	}))
}

// resultState is the word of a result's first column and its color.
func resultState(colors ui.Colors, r runResult) (string, gui.Color) {
	switch {
	case r.Canceled:
		return "Canceled", colors.EditorWarningForeground
	case r.Err != "" && r.Status == 0:
		return "Error", colors.ErrorForeground
	case r.failed():
		return "Failed", colors.ErrorForeground
	}
	return "OK", colGood
}

// resultRow is a result and, when it is expanded, its details.
func (t *runTab) resultRow(bc widgets.BuildContext, colors ui.Colors, i int) widgets.Widget {
	r := t.results[i]
	state, stateColor := resultState(colors, r)
	cell := func(s string, color gui.Color) widgets.Widget {
		return widgets.NewText(s, widgets.TextOpts{Style: ui.TextStyle(bc, 12, color), MaxLines: foundation.Some(1), Overflow: painting.TextOverflowEllipsis})
	}
	status := "-"
	statusColor := colors.DescriptionForeground
	if r.Status > 0 {
		status = strconv.Itoa(r.Status) + " " + r.StatusText
		statusColor = respStatusColor(colors, r.Status, false)
	}
	checksText, checksColor := "-", colors.DescriptionForeground
	if p, n := checkCounts(r.Checks); n > 0 {
		checksText, checksColor = strconv.Itoa(p)+"/"+strconv.Itoa(n), colGood
		if p < n {
			checksColor = colors.ErrorForeground
		}
	}
	chevron := "chevron-right"
	if t.open[i] {
		chevron = "chevron-down"
	}
	row := widgets.NewRow([]widgets.Widget{
		wide(ui.NewIconButton(chevron, ui.IconButtonOpts{Tooltip: "Show the details", OnPressed: func() { t.open[i] = !t.open[i]; t.tab.Refresh() }}), colMark),
		wide(widgets.NewText(state, widgets.TextOpts{Style: pageui.Bold(ui.TextStyle(bc, 12, stateColor))}), colState),
		methodBadge(bc, r.Method), pageui.HGap(6), flex(cell(r.Name, colors.Foreground), 3), flex(cell(status, statusColor), 2),
		wide(cell(durationText(r.Duration), colors.EditorForeground), colTime), wide(cell(sizeText(r), colors.EditorForeground), colSize),
		wide(cell(checksText, checksColor), colChecks),
	}, widgets.RowOpts{CrossAxisAlignment: crossCenter})
	if !t.open[i] {
		return row
	}
	return widgets.NewColumn([]widgets.Widget{row, t.details(bc, colors, r)}, widgets.ColumnOpts{CrossAxisAlignment: rendering.CrossAxisAlignmentStretch})
}

// sizeText is the size of a result's body, "-" for a request with no response.
func sizeText(r runResult) string {
	if r.Status == 0 {
		return "-"
	}
	return byteSize(r.Size)
}

// details is what a result's row shows when it is expanded: the URL, the error, the notes, each check and each extraction.
func (t *runTab) details(bc widgets.BuildContext, colors ui.Colors, r runResult) widgets.Widget {
	mono := ui.TextStyle(bc, 12, colors.EditorForeground).CopyWith(painting.TextStyleOpts{FontFamily: pageui.MonoFamily(bc)})
	line := func(s string, style *painting.TextStyle) widgets.Widget {
		return widgets.NewPadding(painting.NewEdgeInsetsFromLTRB(0, 1, 0, 1), widgets.NewText(s, widgets.TextOpts{Style: style, MaxLines: foundation.Some(3), Overflow: painting.TextOverflowEllipsis}))
	}
	var rows []widgets.Widget
	if r.URL != "" {
		rows = append(rows, line(r.Method+" "+r.URL, mono))
	}
	if r.Err != "" {
		rows = append(rows, line(r.Err, ui.TextStyle(bc, 12, colors.ErrorForeground)))
	}
	for _, n := range r.Notes {
		rows = append(rows, line(n, ui.TextStyle(bc, 12, colors.EditorWarningForeground)))
	}
	for _, c := range r.Checks {
		word, color := "Passed", colGood
		if !c.Passed {
			word, color = "Failed", colors.ErrorForeground
		}
		s := c.What + " " + c.Expected
		switch {
		case c.Problem != "":
			s += "  -  " + c.Problem
		case !c.Passed:
			s += "  -  got " + clipText(c.Actual, 80)
		}
		rows = append(rows, center(wide(widgets.NewText(word, widgets.TextOpts{Style: pageui.Bold(ui.TextStyle(bc, 12, color))}), 56),
			flex(widgets.NewText(s, widgets.TextOpts{Style: ui.TextStyle(bc, 12, colors.EditorForeground), MaxLines: foundation.Some(2), Overflow: painting.TextOverflowEllipsis}), 1)))
	}
	for _, o := range r.Extracted {
		switch {
		case !o.OK():
			rows = append(rows, line("Extract "+o.Rule.To+": "+o.Problem, ui.TextStyle(bc, 12, colors.EditorWarningForeground)))
		default:
			rows = append(rows, line("Extracted "+o.Rule.To+" = "+clipText(o.shown(), 80)+"  ("+o.Scope+" current value)", ui.TextStyle(bc, 12, colGood)))
		}
	}
	if len(rows) == 0 {
		rows = append(rows, line("Nothing more to show.", ui.TextStyle(bc, 12, colors.DescriptionForeground)))
	}
	return widgets.NewPadding(painting.NewEdgeInsetsFromLTRB(28, 2, 0, 8), widgets.NewColumn(rows, widgets.ColumnOpts{CrossAxisAlignment: rendering.CrossAxisAlignmentStretch}))
}
