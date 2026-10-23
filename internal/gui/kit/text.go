package kit

import (
	"github.com/rm4n0s/glutter/foundation"
	"github.com/rm4n0s/glutter/painting"
	"github.com/rm4n0s/glutter/ui"
	"github.com/rm4n0s/glutter/widgets"
)

// UIFontOf is the family of the workbench's text: the one the nearest Theme names, else the
// desktop's UI font, else the generic sans-serif (SPEC D14).
func UIFontOf(bc widgets.BuildContext) string {
	if t, ok := widgets.DependOn[*Theme](bc); ok && t.uiFont != "" {
		return t.uiFont
	}
	if family, _ := widgets.WidgetsBindingOf(bc).PlatformDispatcher().UIFont(); family != "" {
		return family
	}
	return "sans-serif"
}

// TextStyleOf is a text style in the UI font of the workbench.
func TextStyleOf(bc widgets.BuildContext, size float64, color ui.Color) *painting.TextStyle {
	return painting.NewTextStyle(painting.TextStyleOpts{
		FontFamily:         UIFontOf(bc),
		FontFamilyFallback: []string{"sans-serif"},
		FontSize:           foundation.Some(size),
		Color:              foundation.Some(color),
	})
}
