package httpclient

import (
	"strconv"

	"github.com/rm4n0s/glutter/foundation"
	"github.com/rm4n0s/glutter/painting"
	"github.com/rm4n0s/glutter/widgets"

	"github.com/rm4n0s/emetos/extension/ui"
	"github.com/rm4n0s/emetos/extensions/internal/pageui"
)

// checksLabel is the label of the Checks tab of the response: how many checks passed out of how many there are.
func (t *reqTab) checksLabel() string {
	passed, total := checkCounts(t.send.checkResults)
	if total == 0 {
		return "Checks"
	}
	return "Checks (" + strconv.Itoa(passed) + "/" + strconv.Itoa(total) + ")"
}

// checksView is the Checks tab of the response pane: each check of the request, passed or failed, with what it asked for and what the
// response had.
func (t *reqTab) checksView(bc widgets.BuildContext) widgets.Widget {
	colors := ui.Theme(bc)
	dim := ui.TextStyle(bc, 12, colors.DescriptionForeground)
	results := t.send.checkResults
	if len(results) == 0 {
		return tabPad(pageui.Paragraph("This request has no checks. Add them in the Checks tab of the request.", dim))
	}
	passed, total := checkCounts(results)
	summary := strconv.Itoa(passed) + " passed"
	summaryColor := colGood
	if passed < total {
		summary += ", " + strconv.Itoa(total-passed) + " failed"
		summaryColor = colors.ErrorForeground
	}
	cell := func(w widgets.Widget, n int) widgets.Widget {
		return flex(widgets.NewPadding(painting.NewEdgeInsetsFromLTRB(0, 0, 8, 0), w), n)
	}
	headStyle := pageui.Bold(ui.TextStyle(bc, 12, colors.DescriptionForeground))
	text := func(s string, style *painting.TextStyle) widgets.Widget {
		return widgets.NewText(s, widgets.TextOpts{Style: style, MaxLines: foundation.Some(3), Overflow: painting.TextOverflowEllipsis})
	}
	rows := []widgets.Widget{
		widgets.NewText(summary, widgets.TextOpts{Style: pageui.Bold(ui.TextStyle(bc, 13, summaryColor))}), pageui.Gap(8),
		widgets.NewRow([]widgets.Widget{wide(text("Result", headStyle), 70), cell(text("Check", headStyle), 3), cell(text("Expected", headStyle), 3), cell(text("Actual", headStyle), 4)}, widgets.RowOpts{}),
		pageui.Gap(4),
	}
	mono := ui.TextStyle(bc, 12, colors.EditorForeground).CopyWith(painting.TextStyleOpts{FontFamily: pageui.MonoFamily(bc)})
	for _, r := range results {
		label, color := "Passed", colGood
		if !r.Passed {
			label, color = "Failed", colors.ErrorForeground
		}
		actual := r.Actual
		var problem widgets.Widget
		if r.Problem != "" {
			actual = "-"
			problem = widgets.NewPadding(painting.NewEdgeInsetsFromLTRB(70, 0, 0, 2), text(r.Problem, ui.TextStyle(bc, 12, colors.ErrorForeground)))
		}
		line := widgets.NewRow([]widgets.Widget{
			wide(text(label, pageui.Bold(ui.TextStyle(bc, 12, color))), 70),
			cell(text(r.What, ui.TextStyle(bc, 12, colors.EditorForeground)), 3), cell(text(r.Expected, mono), 3), cell(text(actual, mono), 4),
		}, widgets.RowOpts{})
		rows = append(rows, widgets.NewPadding(painting.NewEdgeInsetsFromLTRB(0, 3, 0, 1), line))
		if problem != nil {
			rows = append(rows, problem)
		}
	}
	return t.scrolled(rows...)
}
