package kit

import (
	"github.com/rm4n0s/glutter/foundation"
	"github.com/rm4n0s/glutter/painting"
	"github.com/rm4n0s/glutter/ui"
	"github.com/rm4n0s/glutter/widgets"

	"github.com/rm4n0s/emetos/internal/core/theme"
)

// Metrics of the icon of a file in a list: VS Code's `.monaco-icon-label::before` is 16 px wide, and the
// glyph is drawn in the font of the icon theme at the scale the theme asks for (150 % of the 13 px text
// for Seti).
const (
	// FileIconWidth is the width of the icon of a file or folder.
	FileIconWidth = 16.0
	// fileIconFontSize is the size of the text of a row, which the scale of the icon applies to.
	fileIconFontSize = 13.0
	// fileIconHeight is the height of the box of the icon: a row of a list.
	fileIconHeight = 22.0
)

// NewFileIcon draws the icon of a file icon theme: its glyph in the font of the theme, in the color the
// theme gives it, or in fallback when it gives none.
func NewFileIcon(icon theme.FileIcon, fallback ui.Color) widgets.Widget {
	color := fallback
	if icon.HasColor {
		color = ui.NewColor(icon.Color.ARGB())
	}
	style := painting.NewTextStyle(painting.TextStyleOpts{
		FontFamily: icon.Family,
		FontSize:   foundation.Some(fileIconFontSize * icon.Scale),
		Color:      foundation.Some(color),
		Height:     foundation.Some(1.0),
	})
	return widgets.NewSizedBox(
		widgets.NewCenter(widgets.NewText(string(icon.Glyph), widgets.TextOpts{Style: style, MaxLines: foundation.Some(1), Overflow: painting.TextOverflowClip})),
		widgets.SizedBoxOpts{Width: foundation.Some(FileIconWidth), Height: foundation.Some(fileIconHeight)})
}
