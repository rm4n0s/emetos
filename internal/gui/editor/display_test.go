package editor

import (
	"image/color"
	"testing"
	"time"

	"github.com/rm4n0s/glutter/ui"

	"github.com/rm4n0s/emetos/internal/core/buffer"
)

// cursorColored says whether a pixel is the color of the cursor, up to the rounding of the
// renderer.
func cursorColored(c color.RGBA) bool {
	near := func(a uint8, b int) bool { return a >= uint8(max(b-2, 0)) && int(a) <= b+2 }
	return near(c.R, 0xAE) && near(c.G, 0xAF) && near(c.B, 0xAD)
}

func TestCursorStyleShapes(t *testing.T) {
	cases := []struct {
		style     string
		shape     cursorShape
		thickness float64
	}{
		{"line", shapeLine, 0}, {"line-thin", shapeLine, 1}, {"block", shapeBlock, 0},
		{"underline", shapeUnderline, 2}, {"underline-thin", shapeUnderline, 1}, {"block-outline", shapeOutline, 1},
		{"", shapeLine, 0}, {"nonsense", shapeLine, 0},
	}
	for _, tc := range cases {
		if shape, thickness := cursorStyleOf(tc.style); shape != tc.shape || thickness != tc.thickness {
			t.Errorf("cursorStyleOf(%q) = %v, %v; want %v, %v", tc.style, shape, thickness, tc.shape, tc.thickness)
		}
	}
}

func TestOppositeColor(t *testing.T) {
	got := oppositeColor(ui.NewColor(0xFFAEAFAD))
	if got != ui.NewColor(0xFF515052) {
		t.Errorf("the opposite of #AEAFAD is %v, want #515052", got)
	}
	if oppositeColor(ui.NewColor(0x80000000)).Alpha() != 0x80 {
		t.Error("the opposite keeps the alpha")
	}
}

// cursorRig is an editor with a solid cursor of a style, over a text with a tab and a line end.
func cursorRig(t *testing.T, style string, tweak func(*Options)) *mouseRig {
	t.Helper()
	r := newMouseRig(t, "hello world\na\tb", func(o *Options) {
		o.FontSize, o.LineHeight = 20, 27 // a character is 12 pixels wide: the cursors fall on whole pixels
		o.CursorBlinking = "solid"
		o.CursorStyle = style
		if tweak != nil {
			tweak(o)
		}
	})
	r.vm.Cursors().SetSelections([]buffer.Selection{cur(0, 4), cur(0, 11), cur(1, 1)})
	r.vm.SelectionsChanged()
	r.ts.Pump(time.Millisecond)
	return r
}

func TestGoldenCursorStyles(t *testing.T) {
	for _, style := range []string{"line", "line-thin", "block", "underline", "underline-thin", "block-outline"} {
		t.Run(style, func(t *testing.T) {
			r := cursorRig(t, style, nil)
			r.ts.MatchesGolden("cursor_" + style)
		})
	}
}

func TestGoldenWideLineCursorShowsTheCharacter(t *testing.T) {
	r := cursorRig(t, "line", func(o *Options) { o.CursorWidth = 6 })
	r.ts.MatchesGolden("cursor_line_wide")
}

// pixelAt reads a pixel of the screenshot.
func (r *mouseRig) cursorCell(line, col int) (x, y float64) {
	return r.gutter + float64(col)*r.cw, float64(line) * r.lineH
}

func TestCursorShapesCoverTheRightPixels(t *testing.T) {
	// The cursor at (0, 4) is in a cell 12 pixels wide and 27 high. The points are pixels,
	// relative to the top left of the cell, and say whether the cursor color is there.
	cases := []struct {
		style string
		in    map[[2]int]bool
	}{
		{"line", map[[2]int]bool{{-2, 13}: false, {-1, 13}: true, {0, 13}: true, {1, 13}: false, {-1, 0}: true, {-1, 26}: true}},
		{"line-thin", map[[2]int]bool{{-1, 13}: false, {0, 13}: true, {1, 13}: false}},
		{"underline", map[[2]int]bool{{3, 26}: true, {3, 25}: true, {3, 24}: false, {3, 13}: false, {11, 26}: true, {12, 26}: false}},
		{"underline-thin", map[[2]int]bool{{3, 26}: true, {3, 25}: false, {3, 13}: false}},
		{"block-outline", map[[2]int]bool{{0, 13}: true, {11, 13}: true, {5, 0}: true, {5, 26}: true, {5, 13}: false, {1, 13}: false}},
		{"block", map[[2]int]bool{{0, 0}: true, {11, 26}: true, {12, 13}: false}},
	}
	for _, tc := range cases {
		t.Run(tc.style, func(t *testing.T) {
			r := cursorRig(t, tc.style, nil)
			shot := r.ts.Screenshot()
			x0, y0 := r.cursorCell(0, 4)
			for p, want := range tc.in {
				c := shot.RGBAAt(int(x0)+p[0], int(y0)+p[1])
				got := cursorColored(c)
				if got != want {
					t.Errorf("pixel %v of the cell is %v: the cursor color is there: %v, want %v", p, c, got, want)
				}
			}
		})
	}
}

func TestBlockCursorFillsTheCellAndShowsTheLetter(t *testing.T) {
	r := cursorRig(t, "block", nil)
	shot := r.ts.Screenshot()
	x0, y0 := r.cursorCell(0, 4)
	// The corners of the cell are the cursor color; the letter inside is not.
	for _, p := range [][2]float64{{1, 1}, {r.cw - 1.5, 1}, {1, r.lineH - 1.5}, {r.cw - 1.5, r.lineH - 1.5}} {
		c := shot.RGBAAt(int(x0+p[0]), int(y0+p[1]))
		if !cursorColored(c) {
			t.Errorf("corner %v of the block is %v", p, c)
		}
	}
	dark := 0
	for y := 0; y < int(r.lineH); y++ {
		for x := 0; x < int(r.cw); x++ {
			if c := shot.RGBAAt(int(x0)+x, int(y0)+y); c.R < 0x80 {
				dark++
			}
		}
	}
	if dark == 0 {
		t.Error("the letter under the block cursor is not drawn in the opposite color")
	}
	// At the end of a line the block is one character wide and has no letter.
	endX, _ := r.cursorCell(0, 11)
	if c := shot.RGBAAt(int(endX+r.cw/2), int(r.lineH/2)); !cursorColored(c) {
		t.Errorf("the block at the end of the line: %v", c)
	}
}

func TestBlockCursorOnATabIsOneCharacterWide(t *testing.T) {
	r := cursorRig(t, "block", nil)
	shot := r.ts.Screenshot()
	x0, y0 := r.cursorCell(1, 1)
	y := int(y0 + r.lineH/2)
	if c := shot.RGBAAt(int(x0+r.cw-1.5), y); !cursorColored(c) {
		t.Errorf("inside the first character width of the tab: %v", c)
	}
	if c := shot.RGBAAt(int(x0+r.cw+2), y); cursorColored(c) {
		t.Errorf("past one character the block should have ended: %v", c)
	}
}

func TestCursorStyleChangesLive(t *testing.T) {
	r := cursorRig(t, "line", nil)
	x0, y0 := r.cursorCell(0, 4)
	if c := r.ts.Screenshot().RGBAAt(int(x0+r.cw-2), int(y0+r.lineH/2)); cursorColored(c) {
		t.Fatalf("a line cursor does not reach the next column: %v", c)
	}
	opts := r.vm.Options()
	opts.CursorStyle = "block"
	r.vm.SetOptions(opts)
	r.ts.Pump(time.Millisecond)
	if c := r.ts.Screenshot().RGBAAt(int(x0+r.cw-2), int(y0+2)); !cursorColored(c) {
		t.Errorf("after the change to block: %v", c)
	}
}

func TestExpandCursorShrinksTowardsItsMiddle(t *testing.T) {
	r := cursorRig(t, "block", func(o *Options) { o.CursorBlinking = "expand" })
	r.ts.Pump()
	x0, y0 := r.cursorCell(0, 4)
	if c := r.ts.Screenshot().RGBAAt(int(x0+3), int(y0+1)); !cursorColored(c) {
		t.Fatalf("the cursor starts whole: %v", c)
	}
	r.ts.Pump(BlinkInterval + BlinkInterval*5/10) // halfway to nothing
	r.ts.Pump(time.Millisecond)                   // the frame draws what the last one computed
	shot := r.ts.Screenshot()
	if c := shot.RGBAAt(int(x0+3), int(y0+1)); cursorColored(c) {
		t.Errorf("the top of the cursor is still there: %v", c)
	}
	if c := shot.RGBAAt(int(x0+3), int(y0+r.lineH/2)); !cursorColored(c) {
		t.Errorf("the middle of the cursor is gone: %v", c)
	}
}

func TestSmoothCursorFadesOut(t *testing.T) {
	r := cursorRig(t, "block", func(o *Options) { o.CursorBlinking = "smooth" })
	r.ts.Pump()
	x0, y0 := r.cursorCell(0, 4)
	whole := r.ts.Screenshot().RGBAAt(int(x0+3), int(y0+2))
	r.ts.Pump(BlinkInterval + BlinkInterval*4/10)
	r.ts.Pump(time.Millisecond)
	half := r.ts.Screenshot().RGBAAt(int(x0+3), int(y0+2))
	if half.R >= whole.R || half.R <= 0x1F {
		t.Errorf("at half opacity the cursor blends with the background: whole %v, half %v", whole, half)
	}
	r.ts.Pump(BlinkInterval / 2)
	r.ts.Pump(time.Millisecond)
	if gone := r.ts.Screenshot().RGBAAt(int(x0+3), int(y0+2)); gone.R != 0x1F {
		t.Errorf("when it is gone the editor's background shows: %v", gone)
	}
}

func TestFontWeightAndLetterSpacingMakeOtherParagraphs(t *testing.T) {
	ts := testutilNew(t)
	base := goOptions()
	bold := base
	bold.FontWeight = 700
	spaced := base
	spaced.LetterSpacing = 2
	white := ui.NewColor(0xFFFFFFFF)
	factory := ts.Dispatcher().TextEngine()
	a, b, c := newTextStyle(factory, base, white), newTextStyle(factory, bold, white), newTextStyle(factory, spaced, white)
	if a.fingerprint == b.fingerprint || a.fingerprint == c.fingerprint || b.fingerprint == c.fingerprint {
		t.Errorf("the cache keys do not tell the weight and the spacing apart: %q %q %q", a.fingerprint, b.fingerprint, c.fingerprint)
	}
	zero := base
	zero.FontWeight = 0
	if newTextStyle(factory, zero, white).fingerprint == a.fingerprint {
		t.Log("weight 0 is its own key, and means normal")
	}
	if w1, w2 := a.build("hello", nil).LongestLine(), c.build("hello", nil).LongestLine(); w2 <= w1 {
		t.Errorf("letter spacing makes the text wider: %v then %v", w1, w2)
	}
	if got := zero; newTextStyle(factory, got, white).build("x", nil).LongestLine() != a.build("x", nil).LongestLine() {
		t.Error("weight 0 draws as normal")
	}
}

func TestLetterSpacingMovesTheColumns(t *testing.T) {
	r := newMouseRig(t, "hello world", func(o *Options) { o.LetterSpacing = 3 })
	g := newGeometry(r.vm, r.ts.Dispatcher().TextEngine())
	plain := newMouseRig(t, "hello world", nil)
	pg := newGeometry(plain.vm, plain.ts.Dispatcher().TextEngine())
	if d := g.charWidth - pg.charWidth; d < 2.9 || d > 3.1 {
		t.Fatalf("a character is %v wider with a spacing of 3, want 3", d)
	}
	// The pointer finds the column by the wider characters.
	r.click(ui.Offset{Dx: r.gutter + 4*r.cw + 1, Dy: 9})
	if got := r.vm.Cursors().Primary().Position(); got.Col != 4 {
		t.Errorf("clicked column %d, want 4", got.Col)
	}
	r.click(ui.Offset{Dx: r.gutter + 4*r.cw + 1, Dy: 9})
}

func TestGoldenBoldAndLetterSpacing(t *testing.T) {
	r := newMouseRig(t, "plain text\nfunc main() {\n}", func(o *Options) {
		o.FontWeight = 700
		o.LetterSpacing = 1.5
	})
	r.ts.MatchesGolden("bold_spaced")
}

func TestZoomCommands(t *testing.T) {
	r := newMouseRig(t, "hello", nil)
	base := r.vm.Options()
	cases := []struct {
		command string
		level   float64
	}{
		{"editor.action.fontZoomIn", 1}, {"editor.action.fontZoomIn", 2}, {"editor.action.fontZoomOut", 1},
		{"editor.action.fontZoomOut", 0}, {"editor.action.fontZoomOut", -1},
		{"editor.action.fontZoomReset", 0},
	}
	for _, tc := range cases {
		if err := r.vm.RunCommand(tc.command, nil); err != nil {
			t.Fatal(err)
		}
		r.ts.Pump(time.Millisecond)
		want := 1 + tc.level*0.1
		got := r.vm.Options()
		if !near(got.FontSize, base.FontSize*want) || !near(got.LineHeight, base.LineHeight*want) {
			t.Errorf("%s: font %v line %v, want %v and %v", tc.command, got.FontSize, got.LineHeight, base.FontSize*want, base.LineHeight*want)
		}
	}
	noErrors(t, r.ts)
}

func TestZoomIsBounded(t *testing.T) {
	r := newMouseRig(t, "hello", nil)
	for range 30 {
		if err := r.vm.RunCommand("editor.action.fontZoomIn", nil); err != nil {
			t.Fatal(err)
		}
	}
	if r.vm.Zoom().Level() != MaxZoomLevel {
		t.Errorf("level %v, want %v", r.vm.Zoom().Level(), MaxZoomLevel)
	}
	if got := r.vm.Options().FontSize; !near(got, 14*3) {
		t.Errorf("font size at the top level: %v, want 42", got)
	}
	for range 40 {
		if err := r.vm.RunCommand("editor.action.fontZoomOut", nil); err != nil {
			t.Fatal(err)
		}
	}
	if r.vm.Zoom().Level() != MinZoomLevel {
		t.Errorf("level %v, want %v", r.vm.Zoom().Level(), MinZoomLevel)
	}
	if got := r.vm.Options().FontSize; !near(got, 7) {
		t.Errorf("font size at the bottom level: %v, want 7", got)
	}
}

func TestZoomRelaysOutTheText(t *testing.T) {
	r := newMouseRig(t, "a\nb\nc\nd", nil)
	before := r.vm.Projection().Height()
	r.vm.zoom.Set(10)
	r.ts.Pump(time.Millisecond)
	if after := r.vm.Projection().Height(); !near(after, before*2) {
		t.Errorf("content height %v then %v: at level 10 the lines are twice as high", before, after)
	}
	// The gutter is wider with the larger digits.
	r.gutter = r.vm.GutterWidths(r.ts.Dispatcher().TextEngine()).Total()
	r.click(ui.Offset{Dx: r.gutter + 1, Dy: 38*2 + 3}) // the third line, of 38 pixels
	if got := r.vm.Cursors().Primary().Position(); got.Line != 2 {
		t.Errorf("a click on the third zoomed line is line %d", got.Line)
	}
}

func TestSettingsChangeKeepsTheZoom(t *testing.T) {
	r := newMouseRig(t, "hello", nil)
	r.vm.zoom.Set(5)
	opts := r.vm.base
	opts.FontSize = 20
	opts.LineHeight = 27
	r.vm.SetOptions(opts)
	if got := r.vm.Options(); !near(got.FontSize, 30) || !near(got.LineHeight, 40.5) {
		t.Errorf("font %v line %v, want 30 and 40.5", got.FontSize, got.LineHeight)
	}
}

func TestEditorsOfAWindowShareTheZoom(t *testing.T) {
	zoom := NewZoom()
	a := newMouseRigWith(t, "one", nil, EditorOpts{Autofocus: true, Zoom: zoom})
	b := newMouseRigWith(t, "two", nil, EditorOpts{Autofocus: true, Zoom: zoom})
	if err := a.vm.RunCommand("editor.action.fontZoomIn", nil); err != nil {
		t.Fatal(err)
	}
	if zoom.Level() != 1 || !near(b.vm.Options().FontSize, 14*1.1) {
		t.Errorf("zoom %v, the other editor's font %v", zoom.Level(), b.vm.Options().FontSize)
	}
	// An editor that is shown with a zoom already at a level starts at it.
	c := newMouseRigWith(t, "three", nil, EditorOpts{Autofocus: true, Zoom: zoom})
	if !near(c.vm.Options().FontSize, 14*1.1) {
		t.Errorf("a new editor's font %v", c.vm.Options().FontSize)
	}
}

func TestMouseWheelZoom(t *testing.T) {
	r := newMouseRig(t, "a long line of text", func(o *Options) { o.MouseWheelZoom = true })
	hold := r.holdCtrl()
	r.ts.ScrollAt(r.at(0, 3), ui.Offset{Dy: -wheelNotch}) // a notch up zooms in
	if got := r.vm.Zoom().Level(); got != 1 {
		t.Errorf("a notch up: level %v", got)
	}
	r.ts.ScrollAt(r.at(0, 3), ui.Offset{Dy: 3 * wheelNotch})
	if got := r.vm.Zoom().Level(); got != -2 {
		t.Errorf("three notches down: level %v", got)
	}
	r.ts.ScrollAt(r.at(0, 3), ui.Offset{Dy: -wheelNotch / 4}) // a high resolution wheel
	if got := r.vm.Zoom().Level(); got != -1.75 {
		t.Errorf("a quarter notch up: level %v", got)
	}
	hold()
	if r.vm.ScrollTop() != 0 {
		t.Errorf("zooming does not scroll: %v", r.vm.ScrollTop())
	}
}

func TestMouseWheelZoomNeedsTheSettingAndOnlyControl(t *testing.T) {
	r := newMouseRig(t, "a long line of text", nil)
	hold := r.holdCtrl()
	r.ts.ScrollAt(r.at(0, 3), ui.Offset{Dy: -wheelNotch})
	hold()
	if r.vm.Zoom().Level() != 0 {
		t.Errorf("zoomed with the setting off: %v", r.vm.Zoom().Level())
	}
	r = newMouseRig(t, "a long line of text", func(o *Options) { o.MouseWheelZoom = true })
	release := r.holdShift()
	hold = r.holdCtrl()
	r.ts.ScrollAt(r.at(0, 3), ui.Offset{Dy: wheelNotch})
	hold()
	release()
	if r.vm.Zoom().Level() != 0 {
		t.Errorf("zoomed with Control and Shift: %v", r.vm.Zoom().Level())
	}
	r.ts.ScrollAt(r.at(0, 3), ui.Offset{Dy: wheelNotch})
	if r.vm.Zoom().Level() != 0 {
		t.Errorf("zoomed without Control: %v", r.vm.Zoom().Level())
	}
}

func TestTouchpadPanZoomsWithControl(t *testing.T) {
	r := newMouseRig(t, "a long line of text", func(o *Options) { o.MouseWheelZoom = true })
	hold := r.holdCtrl()
	g := r.ts.StartTrackpadGesture(r.at(0, 3))
	g.PanBy(ui.Offset{Dy: 200})
	hold()
	g.End()
	if got := r.vm.Zoom().Level(); got != 1 {
		t.Errorf("a pan of 200 pixels: level %v", got)
	}
}
