package settingsui

import (
	"github.com/rm4n0s/glutter/foundation"
	"github.com/rm4n0s/glutter/painting"
	"github.com/rm4n0s/glutter/ui"
	"github.com/rm4n0s/glutter/widgets"

	"github.com/rm4n0s/emetos/internal/core/settings"
	"github.com/rm4n0s/emetos/internal/gui/kit"
)

// descriptionFontSize is the size of the texts of a row (VS Code's 13 px).
const descriptionFontSize = 13.0

// descriptionLineHeight is the height of a line of a description, as a multiple of its font size (VS Code's
// line-height of 1.4 in the settings tree).
const descriptionLineHeight = 1.4

// descriptionWidget renders the Markdown of a description: code spans in the code font on the text
// preformat color, links in the link color (they do not open anything yet), and a line for each line of the text.
func descriptionWidget(bc widgets.BuildContext, text, codeFont string, colors kit.Colors) widgets.Widget {
	base := kit.TextStyleOf(bc, descriptionFontSize, colors.Foreground.WithOpacity(0.9)).CopyWith(painting.TextStyleOpts{Height: foundation.Some(descriptionLineHeight)})
	code := base.CopyWith(painting.TextStyleOpts{FontFamily: codeFont, FontFamilyFallback: []string{"monospace"}, Color: foundation.Some(colors.Foreground)})
	link := base.CopyWith(painting.TextStyleOpts{Color: foundation.Some(colors.TextLinkForeground), Decoration: foundation.Some(ui.TextDecorationUnderline)})
	var spans []painting.InlineSpan
	for i, line := range settings.RenderDescription(text) {
		if i > 0 {
			spans = append(spans, painting.NewTextSpan(painting.TextSpanOpts{Text: foundation.Some("\n")}))
		}
		for _, sp := range line {
			opts := painting.TextSpanOpts{Text: foundation.Some(sp.Text)}
			switch {
			case sp.Code:
				opts.Style = code
			case sp.Link != "":
				opts.Style = link
			}
			spans = append(spans, painting.NewTextSpan(opts))
		}
	}
	return widgets.NewRichText(painting.NewTextSpan(painting.TextSpanOpts{Style: base, Children: spans}))
}

// textOf is a one-line text of the workbench's font.
func textOf(bc widgets.BuildContext, text string, size float64, color ui.Color, weight ui.FontWeight) widgets.Widget {
	style := kit.TextStyleOf(bc, size, color)
	if weight != 0 {
		style = style.CopyWith(painting.TextStyleOpts{FontWeight: weight})
	}
	return widgets.NewText(text, widgets.TextOpts{Style: style})
}
