package fonts

import (
	_ "embed" // the font files below

	"github.com/rm4n0s/glutter/widgets"
)

//go:generate go run ./internal/gen

// The family names under which Sources loads the fonts: the ones that
// TextStyle.FontFamily and IconData.FontFamily use.
const (
	// Codicon is the family of VS Code's icon font (SPEC §10.2).
	Codicon = "codicon"
	// Seti is the family of the Seti file icon font (SPEC §10.8).
	Seti = "seti"
)

//go:embed codicon.ttf
var codiconData []byte

//go:embed seti.woff
var setiData []byte

// Sources returns the bundled fonts for RunAppOpts.Fonts, which loads them before
// the first frame, and for the fonts of a test.
func Sources() []widgets.FontSource {
	return []widgets.FontSource{
		{Family: Codicon, Data: codiconData},
		{Family: Seti, Data: setiData},
	}
}

// CodiconIcon returns the icon for a code point of the Codicon font; use the
// Codicon... constants (codicon_gen.go) for code.
func CodiconIcon(code int) widgets.IconData {
	return widgets.NewIconData(code, widgets.IconDataOpts{FontFamily: Codicon})
}
