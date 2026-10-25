package diffeditor

import (
	"github.com/rm4n0s/glutter/rendering"
	"github.com/rm4n0s/glutter/ui"
	"github.com/rm4n0s/glutter/widgets"

	"github.com/rm4n0s/emetos/internal/gui/kit"
)

// Ported from VS Code: src/vs/editor/browser/widget/diffEditor/style.css (.diagonal-fill: a repeating linear
// gradient of 8 by 8 pixels with stripes 12.5% of the tile's diagonal wide)

// fillerTile is the size of the tile of the diagonal fill and fillerStripe the width of a stripe: 12.5% of the
// diagonal of the tile.
const (
	fillerTile   = 8.0
	fillerStripe = fillerTile * 1.4142135623730951 * 0.125 * 1.0
)

// fillerWidget is the widget of a filler zone: the diagonal lines in the color diffEditor.diagonalFill.
type fillerWidget struct {
	widgets.StatelessWidget
}

// NewFiller is the widget of a filler zone, for the other editors that line texts up with view zones (the inputs of the
// merge editor, step 9.7, use the same diagonal fill).
func NewFiller() widgets.Widget { return newFiller() }

func newFiller() *fillerWidget {
	return &fillerWidget{StatelessWidget: widgets.NewStatelessWidgetBase()}
}

// Build implements widgets.StatelessWidget.
func (w *fillerWidget) Build(bc widgets.BuildContext) widgets.Widget {
	return widgets.NewCustomPaint(nil, widgets.CustomPaintOpts{Painter: &fillerPainter{color: kit.ThemeOf(bc).DiffDiagonalFill}})
}

// fillerPainter draws the stripes: lines that rise to the right, a tile apart, in the zone's box.
type fillerPainter struct {
	rendering.CustomPainterBase
	color ui.Color
}

// Paint implements rendering.CustomPainter.
func (p *fillerPainter) Paint(canvas *ui.Canvas, size ui.Size) {
	if p.color.Alpha() == 0 || size.Width <= 0 || size.Height <= 0 {
		return
	}
	canvas.Save()
	defer canvas.Restore()
	canvas.ClipRect(ui.NewRectFromLTWH(0, 0, size.Width, size.Height))
	paint := ui.NewPaint()
	paint.SetColor(p.color)
	paint.SetStyle(ui.PaintingStyleStroke)
	paint.SetStrokeWidth(fillerStripe)
	// the stripe that starts at the bottom left corner of a tile rises to its top right corner
	for x := -size.Height; x < size.Width+fillerTile; x += fillerTile {
		canvas.DrawLine(ui.Offset{Dx: x, Dy: size.Height}, ui.Offset{Dx: x + size.Height, Dy: 0}, paint)
	}
}

// ShouldRepaint implements rendering.CustomPainter.
func (p *fillerPainter) ShouldRepaint(old rendering.CustomPainter) bool {
	o, ok := old.(*fillerPainter)
	return !ok || o.color != p.color
}
