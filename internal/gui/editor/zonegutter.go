package editor

import (
	"github.com/rm4n0s/glutter/ui"

	"github.com/rm4n0s/emetos/internal/gui/kit/fonts"
)

// The margin of a view zone (SPEC §9.1): a fill along its rows and a glyph, as the diff editor's inline view gives the
// deleted lines.
//
// Ported from VS Code: src/vs/editor/browser/widget/diffEditor/components/diffEditorViewZones/diffEditorViewZones.ts
// (the margin of the deleted code zone: the class inline-deleted-margin-view-zone and the delete-sign icon)

// paintZoneGutter fills the gutter along the rows of the view zones that have a gutter color.
func (r *RenderEditorGutter) paintZoneGutter(canvas *ui.Canvas, offset ui.Offset, size ui.Size, top float64, first, last int) {
	proj := r.vm.Projection()
	for row := first; row <= last; row++ {
		id, _, ok := proj.RowZone(row)
		if !ok {
			continue
		}
		spec, found := r.vm.Zone(id)
		if !found || spec.Gutter.Alpha() == 0 {
			continue
		}
		y := offset.Dy + proj.RowTop(row) - top
		canvas.DrawRect(ui.NewRectFromLTWH(offset.Dx, y, size.Width, proj.RowHeight(row)), fill(spec.Gutter))
	}
}

// zoneGlyphSize is the size of the glyph of a view zone's margin (VS Code's insert and delete signs are 11 px).
const zoneGlyphSize = 11

// zoneGlyphOpacity is the opacity of those glyphs (VS Code's `.delete-sign` has opacity 0.7).
const zoneGlyphOpacity = 0.7

// paintZoneGlyphs draws the glyph of the view zones that have one, at the right of the line decorations column
// of each of their rows.
func (r *RenderEditorGutter) paintZoneGlyphs(canvas *ui.Canvas, offset ui.Offset, top float64, first, last int, widths GutterWidths) {
	proj := r.vm.Projection()
	var style textStyle
	made := false
	for row := first; row <= last; row++ {
		id, _, ok := proj.RowZone(row)
		if !ok {
			continue
		}
		spec, found := r.vm.Zone(id)
		if !found || spec.GutterGlyph == 0 {
			continue
		}
		if !made {
			icons := r.vm.Options()
			icons.FontFamilies, icons.FontSize, icons.FontWeight, icons.LetterSpacing, icons.FontFeatures = []string{fonts.Codicon}, zoneGlyphSize, 0, 0, nil
			color := r.colors.LineNumber
			style, made = newTextStyle(r.factory, icons, color.WithOpacity(zoneGlyphOpacity)), true
		}
		p := style.paragraph(r.vm.Paragraphs(), string(rune(spec.GutterGlyph)))
		x := offset.Dx + widths.GlyphMargin + widths.LineNumbers + widths.Decorations - p.LongestLine() - 1
		canvas.DrawParagraph(p, ui.Offset{Dx: x, Dy: offset.Dy + proj.RowTop(row) - top})
	}
}
