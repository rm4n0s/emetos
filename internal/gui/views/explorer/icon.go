package explorer

import (
	"github.com/rm4n0s/glutter/foundation"
	"github.com/rm4n0s/glutter/painting"
	"github.com/rm4n0s/glutter/rendering"
	"github.com/rm4n0s/glutter/ui"
	"github.com/rm4n0s/glutter/widgets"

	"github.com/rm4n0s/emetos/internal/core/theme"
)

// Metrics of the icon of a row: VS Code's `.monaco-icon-label::before` is 16 px wide with 6 px of padding
// after it, and the glyph is drawn in the font of the icon theme at the scale the theme asks for (150 % of
// the 13 px text for Seti).
const (
	// IconWidth is the width of the icon of a file or folder.
	IconWidth = 16.0
	// IconGap is the room between the icon and the label.
	IconGap = 6.0
	// labelFontSize is the size of the text of a row.
	labelFontSize = 13.0
)

// FileIconWidget draws the icon of a file icon theme: its glyph in the font of the theme, in the color the
// theme gives it, or in fallback when it gives none.
func FileIconWidget(icon theme.FileIcon, fallback ui.Color) widgets.Widget {
	color := fallback
	if icon.HasColor {
		color = ui.NewColor(icon.Color.ARGB())
	}
	style := painting.NewTextStyle(painting.TextStyleOpts{
		FontFamily: icon.Family,
		FontSize:   foundation.Some(labelFontSize * icon.Scale),
		Color:      foundation.Some(color),
		Height:     foundation.Some(1.0),
	})
	return widgets.NewSizedBox(
		widgets.NewCenter(widgets.NewText(string(icon.Glyph), widgets.TextOpts{Style: style, MaxLines: foundation.Some(1), Overflow: painting.TextOverflowClip})),
		widgets.SizedBoxOpts{Width: foundation.Some(IconWidth), Height: foundation.Some(22.0)})
}

// iconRow is an icon, the room after it and a label, in a row; a missing icon leaves the label alone.
func iconRow(icon widgets.Widget, label ...widgets.Widget) widgets.Widget {
	var cells []widgets.Widget
	if icon != nil {
		cells = append(cells, icon, widgets.NewSizedBox(nil, widgets.SizedBoxOpts{Width: foundation.Some(IconGap)}))
	}
	cells = append(cells, label...)
	return widgets.NewRow(cells, widgets.RowOpts{CrossAxisAlignment: rendering.CrossAxisAlignmentCenter})
}
