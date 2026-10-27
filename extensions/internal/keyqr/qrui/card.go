package qrui

import (
	"github.com/rm4n0s/glutter/foundation"
	"github.com/rm4n0s/glutter/painting"
	"github.com/rm4n0s/glutter/rendering"
	gui "github.com/rm4n0s/glutter/ui"
	"github.com/rm4n0s/glutter/widgets"

	"github.com/rm4n0s/emetos/extension/ui"
)

// cardWidth is the widest a card is, as the cards of the Remote Work tab are.
const cardWidth = 520.0

// Card puts content in a box over the middle of its owner, on a veil that keeps the pointer from what is under it.
func Card(bc widgets.BuildContext, content widgets.Widget) widgets.Widget {
	colors := ui.Theme(bc)
	veil := colors.QuickInputBackground.WithAlpha(0xB0)
	return widgets.NewListener(widgets.NewColoredBox(veil, widgets.NewCenter(
		widgets.NewConstrainedBox(rendering.NewBoxConstraints(rendering.BoxConstraintsOpts{MaxWidth: foundation.Some(cardWidth)}),
			widgets.NewPadding(painting.NewEdgeInsetsAll(16), widgets.NewColoredBox(colors.EditorBackground,
				widgets.NewPadding(painting.NewEdgeInsetsAll(18), content)))))),
		widgets.ListenerOpts{Behavior: rendering.HitTestBehaviorOpaque})
}

func column(children ...widgets.Widget) widgets.Widget {
	return widgets.NewColumn(children, widgets.ColumnOpts{CrossAxisAlignment: rendering.CrossAxisAlignmentStretch, MainAxisSize: rendering.MainAxisSizeMin})
}

func vgap(h float64) widgets.Widget {
	return widgets.NewSizedBox(nil, widgets.SizedBoxOpts{Height: foundation.Some(h)})
}

func hgap(w float64) widgets.Widget {
	return widgets.NewSizedBox(nil, widgets.SizedBoxOpts{Width: foundation.Some(w)})
}

func bold(s *painting.TextStyle) *painting.TextStyle {
	return s.CopyWith(painting.TextStyleOpts{FontWeight: gui.FontWeightW600})
}

// title is the heading of a card: an icon and a text.
func title(bc widgets.BuildContext, colors ui.Colors, icon string, iconColor gui.Color, text string) widgets.Widget {
	return widgets.NewRow([]widgets.Widget{
		ui.Icon(icon, 18, iconColor), hgap(8),
		widgets.NewExpanded(widgets.NewText(text, widgets.TextOpts{Style: bold(ui.TextStyle(bc, 16, colors.Foreground))})),
	}, widgets.RowOpts{CrossAxisAlignment: rendering.CrossAxisAlignmentCenter})
}

func paragraph(bc widgets.BuildContext, text string, color gui.Color) widgets.Widget {
	return widgets.NewText(text, widgets.TextOpts{Style: ui.TextStyle(bc, 13, color)})
}

func mono(bc widgets.BuildContext, text string, color gui.Color) widgets.Widget {
	family, _ := widgets.WidgetsBindingOf(bc).PlatformDispatcher().MonospaceFont()
	if family == "" {
		family = "monospace"
	}
	return widgets.NewText(text, widgets.TextOpts{Style: ui.TextStyle(bc, 12, color).CopyWith(painting.TextStyleOpts{FontFamily: family})})
}

func buttons(children ...widgets.Widget) widgets.Widget {
	return widgets.NewWrap(children, widgets.WrapOpts{Spacing: 8, RunSpacing: 8, Alignment: rendering.WrapAlignmentEnd})
}

// keyDetails lists what the user compares before a key is added or shown: the type, the fingerprint and the comment.
func keyDetails(bc widgets.BuildContext, colors ui.Colors, keyType, fingerprint, comment string) widgets.Widget {
	label := func(s string) widgets.Widget {
		return widgets.NewSizedBox(widgets.NewText(s, widgets.TextOpts{Style: ui.TextStyle(bc, 12, colors.DescriptionForeground)}), widgets.SizedBoxOpts{Width: foundation.Some(90.0)})
	}
	row := func(name string, value widgets.Widget) widgets.Widget {
		return widgets.NewPadding(painting.NewEdgeInsetsFromLTRB(0, 3, 0, 3), widgets.NewRow([]widgets.Widget{label(name), widgets.NewExpanded(value)},
			widgets.RowOpts{CrossAxisAlignment: rendering.CrossAxisAlignmentStart}))
	}
	if comment == "" {
		comment = "(none)"
	}
	return column(
		row("Type", mono(bc, keyType, colors.Foreground)),
		row("Fingerprint", mono(bc, fingerprint, colors.Foreground)),
		row("Comment", paragraph(bc, comment, colors.Foreground)),
	)
}
