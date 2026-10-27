// Package pageui holds the small widgets that the pages and the tabs of Remote Work and Relay share: the gaps, the section
// with its heading and the line above it, the line under a control that tells what its action answered, and the box of a
// problem (SPEC §10.18 "As built (step 15.2)", §10.19 "As built (step 16.2)"). They are built on extension/ui only.
package pageui

import (
	"github.com/rm4n0s/glutter/foundation"
	"github.com/rm4n0s/glutter/painting"
	"github.com/rm4n0s/glutter/rendering"
	gui "github.com/rm4n0s/glutter/ui"
	"github.com/rm4n0s/glutter/widgets"

	"github.com/rm4n0s/emetos/extension/ui"
)

// BodySize is the size of the text of a page.
const BodySize = 13.0

// Note is a line under a control: what an action answered. An error shows in the color of errors.
type Note struct {
	// Text is the line; empty shows nothing.
	Text string
	// Bad makes the line an error.
	Bad bool
}

// Fine is the Note of an action that went well.
func Fine(text string) Note { return Note{Text: text} }

// Bad is the Note of an action that failed.
func Bad(text string) Note { return Note{Text: text, Bad: true} }

// Gap is empty space of a height.
func Gap(h float64) widgets.Widget {
	return widgets.NewSizedBox(nil, widgets.SizedBoxOpts{Height: foundation.Some(h)})
}

// HGap is empty space of a width.
func HGap(w float64) widgets.Widget {
	return widgets.NewSizedBox(nil, widgets.SizedBoxOpts{Width: foundation.Some(w)})
}

// Nothing is a widget that takes no room.
func Nothing() widgets.Widget { return widgets.NewSizedBox(nil, widgets.SizedBoxOpts{}) }

// MonoFamily is the monospace font of the desktop, or "monospace".
func MonoFamily(bc widgets.BuildContext) string {
	family, _ := widgets.WidgetsBindingOf(bc).PlatformDispatcher().MonospaceFont()
	if family == "" {
		return "monospace"
	}
	return family
}

// Bold is the style in a heavier weight.
func Bold(s *painting.TextStyle) *painting.TextStyle {
	return s.CopyWith(painting.TextStyleOpts{FontWeight: gui.FontWeightW600})
}

// Paragraph is a text in a style.
func Paragraph(text string, style *painting.TextStyle) widgets.Widget {
	return widgets.NewText(text, widgets.TextOpts{Style: style})
}

// Section is a heading over its rows, with a line above it.
func Section(bc widgets.BuildContext, colors ui.Colors, title string, rows ...widgets.Widget) widgets.Widget {
	head := widgets.NewText(title, widgets.TextOpts{Style: Bold(ui.TextStyle(bc, 15, colors.Foreground))})
	line := widgets.NewColoredBox(colors.PanelBorder, widgets.NewSizedBox(nil, widgets.SizedBoxOpts{Height: foundation.Some(1.0)}))
	children := []widgets.Widget{Gap(18), line, Gap(14), head, Gap(10)}
	children = append(children, rows...)
	return widgets.NewColumn(children, widgets.ColumnOpts{CrossAxisAlignment: rendering.CrossAxisAlignmentStretch})
}

// NoteLine shows an answer under a control.
func NoteLine(bc widgets.BuildContext, colors ui.Colors, n Note) widgets.Widget {
	if n.Text == "" {
		return Nothing()
	}
	color := colors.DescriptionForeground
	if n.Bad {
		color = colors.ErrorForeground
	}
	return widgets.NewPadding(painting.NewEdgeInsetsFromLTRB(0, 6, 0, 0), widgets.NewText(n.Text, widgets.TextOpts{Style: ui.TextStyle(bc, 12, color)}))
}

// Problem is a message in a box with the color of an error.
func Problem(bc widgets.BuildContext, colors ui.Colors, msg string) widgets.Widget {
	return widgets.NewColoredBox(colors.InputValidationErrorBackground, widgets.NewPadding(painting.NewEdgeInsetsAll(10),
		widgets.NewText(msg, widgets.TextOpts{Style: ui.TextStyle(bc, BodySize, colors.InputValidationErrorForeground)})))
}

// Field is a labelled input box of a width; submit is called when Enter is pressed in it.
func Field(bc widgets.BuildContext, colors ui.Colors, label string, c *ui.InputBoxController, width float64, disabled bool, submit func()) widgets.Widget {
	return widgets.NewColumn([]widgets.Widget{
		widgets.NewText(label, widgets.TextOpts{Style: ui.TextStyle(bc, 12, colors.DescriptionForeground)}),
		Gap(4),
		widgets.NewSizedBox(ui.NewInputBox(nil, nil, ui.InputBoxOpts{
			Controller: c, Disabled: disabled, OnSubmit: func(string) { submit() },
		}), widgets.SizedBoxOpts{Width: foundation.Some(width)}),
	}, widgets.ColumnOpts{CrossAxisAlignment: rendering.CrossAxisAlignmentStart})
}
