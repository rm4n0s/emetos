package pageui

import (
	"context"
	"strconv"

	"github.com/rm4n0s/glutter/foundation"
	"github.com/rm4n0s/glutter/painting"
	"github.com/rm4n0s/glutter/rendering"
	"github.com/rm4n0s/glutter/services"
	"github.com/rm4n0s/glutter/widgets"

	"github.com/rm4n0s/emetos/extension/ui"
)

// GuideStep is a step of the first-time guide of a hosting page (SPEC §10.29 "The first time").
type GuideStep struct {
	// Done shows a check mark in place of the number of the step.
	Done bool
	// Title is the step in one line ("Add the public key of the computer that will connect.") and Detail what helps to do it.
	Title, Detail string
	// Extra are rows under the detail: the addresses and the fingerprint of step 3.
	Extra []widgets.Widget
}

// Guide is the numbered list of steps in a box, with a heading. A step that is done has a check mark.
func Guide(bc widgets.BuildContext, colors ui.Colors, title string, steps []GuideStep) widgets.Widget {
	rows := []widgets.Widget{widgets.NewText(title, widgets.TextOpts{Style: Bold(ui.TextStyle(bc, 15, colors.Foreground))}), Gap(10)}
	for i, st := range steps {
		var mark widgets.Widget
		if st.Done {
			mark = ui.Icon("check", 16, colors.TerminalDecorationSuccess)
		} else {
			mark = widgets.NewText(strconv.Itoa(i+1), widgets.TextOpts{Style: Bold(ui.TextStyle(bc, BodySize, colors.DescriptionForeground))})
		}
		body := []widgets.Widget{widgets.NewText(st.Title, widgets.TextOpts{Style: ui.TextStyle(bc, BodySize, colors.Foreground)})}
		if st.Detail != "" {
			body = append(body, Gap(2), widgets.NewText(st.Detail, widgets.TextOpts{Style: ui.TextStyle(bc, 12, colors.DescriptionForeground)}))
		}
		for _, x := range st.Extra {
			body = append(body, Gap(6), x)
		}
		rows = append(rows, widgets.NewRow([]widgets.Widget{
			widgets.NewSizedBox(widgets.NewAlign(mark, widgets.AlignOpts{Alignment: painting.AlignmentTopCenter}), widgets.SizedBoxOpts{Width: foundation.Some(22.0)}),
			HGap(8),
			widgets.NewExpanded(widgets.NewColumn(body, widgets.ColumnOpts{CrossAxisAlignment: rendering.CrossAxisAlignmentStart})),
		}, widgets.RowOpts{CrossAxisAlignment: rendering.CrossAxisAlignmentStart}), Gap(10))
	}
	return widgets.NewColoredBox(colors.EditorWidgetBackground, widgets.NewPadding(painting.NewEdgeInsetsFromLTRB(14, 12, 14, 2),
		widgets.NewColumn(rows, widgets.ColumnOpts{CrossAxisAlignment: rendering.CrossAxisAlignmentStretch})))
}

// CopyRow is a line of text with a Copy button after it: an address or a fingerprint. copy puts the text on the clipboard.
func CopyRow(bc widgets.BuildContext, colors ui.Colors, text string, copyText func(string)) widgets.Widget {
	mono := ui.TextStyle(bc, 12, colors.Foreground).CopyWith(painting.TextStyleOpts{FontFamily: MonoFamily(bc)})
	return widgets.NewRow([]widgets.Widget{
		widgets.NewText(text, widgets.TextOpts{Style: mono}),
		HGap(10),
		ui.NewButton("Copy", ui.ButtonOpts{Small: true, Secondary: true, OnPressed: func() { copyText(text) }}),
	}, widgets.RowOpts{CrossAxisAlignment: rendering.CrossAxisAlignmentCenter})
}

// Copier returns the function that puts a text on the clipboard of the desktop; bc is the context the page is built in, ctx ends the request. It is a
// no-op when the window has no clipboard.
func Copier(ctx context.Context, bc widgets.BuildContext) func(string) {
	binding := widgets.WidgetsBindingOf(bc)
	return func(text string) {
		if binding == nil {
			return
		}
		binding.Clipboard().SetData(ctx, services.NewClipboardData(text), func(error) {})
	}
}
