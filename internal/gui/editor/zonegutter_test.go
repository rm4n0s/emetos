package editor

import (
	"image/color"
	"testing"

	"github.com/rm4n0s/glutter/ui"

	"github.com/rm4n0s/emetos/internal/core/buffer"
	"github.com/rm4n0s/emetos/internal/gui/kit/fonts"
)

// pixelOf is the color of the pixel of the screenshot under a point of the window.
func (r *mouseRig) pixelOf(p ui.Offset) color.RGBA {
	return r.ts.Screenshot().RGBAAt(int(p.Dx), int(p.Dy))
}

func TestTheMarginOfAZoneHasItsFillAndItsGlyph(t *testing.T) {
	red := ui.NewColorFromARGB(0xFF, 0xC0, 0x20, 0x20)
	tests := []struct {
		name  string
		spec  ZoneSpec
		fill  bool
		glyph bool
	}{
		{"nothing", ZoneSpec{AfterLine: 3, Rows: 2}, false, false},
		{"a fill", ZoneSpec{AfterLine: 3, Rows: 2, Gutter: red}, true, false},
		{"a glyph", ZoneSpec{AfterLine: 3, Rows: 2, GutterGlyph: fonts.CodiconRemove}, false, true},
		{"both", ZoneSpec{AfterLine: 3, Rows: 2, Gutter: red, GutterGlyph: fonts.CodiconRemove}, true, true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			r := zoneRig(t, 12)
			plain := func(row int) color.RGBA { return r.pixelOf(ui.Offset{Dx: 3, Dy: float64(row)*r.lineH + 3}) }
			background := plain(8) // a row of text far from the cursor
			r.addZone(tt.spec)
			for row := 3; row <= 4; row++ {
				got := plain(row)
				filled := got.R > 150 && got.G < 90
				if filled != tt.fill {
					t.Errorf("row %d of the zone: pixel %v, filled = %v, want %v (background %v)", row, got, filled, tt.fill, background)
				}
			}
			// the glyph is in the last 12 pixels of the gutter, on every row of the zone, and nowhere else
			ink := func(row int) bool {
				shot := r.ts.Screenshot()
				base := shot.RGBAAt(int(r.gutter)-13, row*int(r.lineH)+2)
				for x := int(r.gutter) - 12; x < int(r.gutter)-1; x++ {
					for y := row*int(r.lineH) + 2; y < (row+1)*int(r.lineH)-2; y++ {
						if shot.RGBAAt(x, y) != base {
							return true
						}
					}
				}
				return false
			}
			for row := 3; row <= 4; row++ {
				if got := ink(row); got != tt.glyph {
					t.Errorf("row %d of the zone: glyph %v, want %v", row, got, tt.glyph)
				}
			}
			for _, row := range []int{2, 5} {
				if ink(row) {
					t.Errorf("a glyph on row %d, which is text", row)
				}
			}
			noErrors(t, r.ts)
		})
	}
}

func TestALinesViewDrawsTheLinesOfAnotherEditor(t *testing.T) {
	r := zoneRig(t, 12)
	src := NewViewModel(newTestDoc("alpha beta\n\tgamma\ndelta\n"), goOptions(), nil)
	pink := ui.NewColorFromARGB(0xFF, 0xB0, 0x30, 0x80)
	mark := ui.NewColorFromARGB(0xFF, 0x20, 0x90, 0xB0)
	r.addZone(ZoneSpec{AfterLine: 3, Rows: 2, Widget: NewLinesView(LinesViewOpts{
		Source: src, FirstLine: 1, Count: 2, ScrollWith: r.vm,
		Marks: []BackgroundMark{
			{Range: buffer.Range{Start: buffer.Position{Line: 1}, End: buffer.Position{Line: 2, Col: 5}}, WholeLine: true, Background: pink},
			// "gamma" after the tab, in the first row, and "delta" in the second
			{Range: buffer.Range{Start: buffer.Position{Line: 1, Col: 1}, End: buffer.Position{Line: 1, Col: 6}}, Background: mark},
			// outside the lines shown: nothing
			{Range: buffer.Range{Start: buffer.Position{Line: 0}, End: buffer.Position{Line: 0, Col: 3}}, Background: mark},
		},
	})})
	row3, row4 := 3*r.lineH, 4*r.lineH
	at := func(x, y float64) color.RGBA { return r.pixelOf(ui.Offset{Dx: r.gutter + x, Dy: y}) }
	// the whole-line fill reaches across the zone, rows 3 and 4
	for _, y := range []float64{row3 + 2, row4 + r.lineH - 3} {
		if got := at(600, y); got.R < 150 || got.G > 90 {
			t.Errorf("the fill is not under the zone at y=%v: %v", y, got)
		}
	}
	// the rows of text around it are untouched
	if got := at(600, row3-3); got.R > 120 {
		t.Errorf("the row above the zone is %v", got)
	}
	// the mark of the characters is over the fill, after the tab (4 characters wide)
	if got := at(4*r.cw+r.cw*2, row3+3); got.B < 150 || got.R > 90 {
		t.Errorf("no mark under 'gamma': %v", got)
	}
	if got := at(14*r.cw, row3+3); got.B > 150 {
		t.Errorf("the mark goes on after 'gamma': %v", got)
	}
	// text is drawn: some pixel of the first row differs from the fill
	text := false
	shot := r.ts.Screenshot()
	base := shot.RGBAAt(int(r.gutter+600), int(row3+3))
	for x := int(r.gutter); x < int(r.gutter+12*r.cw); x++ {
		for y := int(row4 + 3); y < int(row4+r.lineH-3); y++ {
			if shot.RGBAAt(x, y) != base {
				text = true
			}
		}
	}
	if !text {
		t.Error("the second line is not drawn")
	}
	noErrors(t, r.ts)
}
