package editor

import (
	"image/color"
	"strings"
	"testing"

	"github.com/rm4n0s/glutter/gluttertest"
	"github.com/rm4n0s/glutter/ui"

	"github.com/rm4n0s/emetos/internal/core/scm/quickdiff"
	"github.com/rm4n0s/emetos/internal/core/theme"
	"github.com/rm4n0s/emetos/internal/gui/kit"
	"github.com/rm4n0s/emetos/internal/gui/testutil"
)

const quickDiffSample = `package sample

import "fmt"

func main() {
	fmt.Println("one")
	fmt.Println("two")
	fmt.Println("three")
	fmt.Println("four")
	fmt.Println("five")
	fmt.Println("six")
	fmt.Println("seven")
	fmt.Println("eight")
	fmt.Println("nine")
	fmt.Println("ten")
	fmt.Println("eleven")
	fmt.Println("twelve")
	fmt.Println("thirteen")
	fmt.Println("fourteen")
	fmt.Println("fifteen")
	fmt.Println("sixteen")
	fmt.Println("seventeen")
}
`

// quickDiffMarksOfTheSample are an added block (lines 6 and 7), a modified block (10 to 12), lines deleted after line 14, a secondary
// addition (17) and a secondary modification (19 and 20).
var quickDiffMarksOfTheSample = []QuickDiffMark{
	{Type: quickdiff.Add, Start: 6, End: 7},
	{Type: quickdiff.Modify, Start: 10, End: 12},
	{Type: quickdiff.Delete, Start: 14, End: 0},
	{Type: quickdiff.Add, Start: 17, End: 17, Secondary: true},
	{Type: quickdiff.Modify, Start: 19, End: 20, Secondary: true},
}

// quickDiffRig is an editor with a minimap and the marks of the sample on screen.
func quickDiffRig(t *testing.T, themeName string, tweak func(*Options), colorize func(*kit.Colors)) *minimapRig {
	t.Helper()
	vm, tok := highlighted(t, quickDiffSample)
	opts := vm.Options()
	opts.Minimap = MinimapOptions{Enabled: true, Side: "right", Size: "proportional", ShowSlider: "mouseover", Scale: 1, MaxColumn: 120}
	opts.ScrollBeyondLastLine = false
	opts.RenderLineHighlight = "none"
	opts.OccurrencesHighlight, opts.SelectionHighlight = "off", false
	opts.CursorBlinking = "solid"
	opts.QuickDiff = QuickDiffOptions{Decorations: "all", GutterWidth: 3, Visibility: "always", PatternModified: true}
	if tweak != nil {
		tweak(&opts)
	}
	vm.SetOptions(opts)
	vm.SetQuickDiffMarks(quickDiffMarksOfTheSample)
	t.Cleanup(vm.DisposeHighlights)
	t.Cleanup(vm.DisposeQuickDiff)
	th, err := theme.LoadBundled(themeName)
	if err != nil {
		t.Fatal(err)
	}
	colors := bundledColors(t, themeName)
	if colorize != nil {
		colorize(&colors)
	}
	ts := testutil.New(t, ui.Size{Width: 640, Height: 440})
	pumpHighlighted(t, ts, vm, colors, th.TokenTheme(nil))
	tok.Settle()
	r := &minimapRig{t: t, ts: ts, vm: vm}
	r.settle()
	return r
}

// stripeX is the x of the first pixel of the stripes in the gutter, and rowY the y of the middle of a line (from 0).
func stripeX(vm *ViewModel, ts *gluttertest.Tester) int {
	w := vm.GutterWidths(ts.Dispatcher().TextEngine())
	return int(w.GlyphMargin + w.LineNumbers + foldingControlsWidth + quickDiffStripeLeft)
}

func rowY(line int) int { return line*19 + 9 }

func samePixel(a color.RGBA, b ui.Color) bool {
	return int(a.R) == b.Red() && int(a.G) == b.Green() && int(a.B) == b.Blue()
}

func TestGoldenQuickDiffMarks(t *testing.T) {
	for _, tc := range []struct{ golden, theme string }{
		{"quickdiff_dark", "Dark Modern"},
		{"quickdiff_light", "Light Modern"},
	} {
		t.Run(tc.theme, func(t *testing.T) {
			r := quickDiffRig(t, tc.theme, nil, nil)
			noErrors(t, r.ts)
			testutil.MatchesGolden(r.ts, tc.golden)
		})
	}
}

func TestGoldenQuickDiffPatternAndWidth(t *testing.T) {
	r := quickDiffRig(t, "Dark Modern", func(o *Options) {
		o.QuickDiff.GutterWidth = 5
		o.QuickDiff.PatternAdded, o.QuickDiff.PatternModified = true, true
	}, nil)
	noErrors(t, r.ts)
	testutil.MatchesGolden(r.ts, "quickdiff_pattern_width5_dark")
}

func TestGoldenQuickDiffThinStripes(t *testing.T) {
	r := quickDiffRig(t, "Dark Modern", func(o *Options) {
		o.QuickDiff.GutterWidth = 1
		o.QuickDiff.PatternModified = false
	}, nil)
	noErrors(t, r.ts)
	testutil.MatchesGolden(r.ts, "quickdiff_width1_dark")
}

func TestGoldenQuickDiffOtherColors(t *testing.T) {
	// a theme that colors the marks in its own way: the gutter, the ruler and the minimap each have their own colors
	r := quickDiffRig(t, "Dark Modern", func(o *Options) { o.QuickDiff.PatternModified = false }, func(c *kit.Colors) {
		c.EditorGutterAdded, c.EditorGutterModified, c.EditorGutterDeleted = hexColor(t, "#ff00ff"), hexColor(t, "#ffff00"), hexColor(t, "#00ffff")
		c.EditorGutterAddedSecondary, c.EditorGutterModifiedSecondary = hexColor(t, "#aa00aa"), hexColor(t, "#aaaa00")
		c.EditorOverviewRulerAdded, c.EditorOverviewRulerModified, c.EditorOverviewRulerDeleted = hexColor(t, "#ff8800"), hexColor(t, "#88ff00"), hexColor(t, "#0088ff")
		c.MinimapGutterAdded, c.MinimapGutterModified, c.MinimapGutterDeleted = hexColor(t, "#ff0088"), hexColor(t, "#00ff88"), hexColor(t, "#8800ff")
	})
	noErrors(t, r.ts)
	testutil.MatchesGolden(r.ts, "quickdiff_other_colors_dark")
}

func TestGoldenQuickDiffVisibleOnHover(t *testing.T) {
	r := quickDiffRig(t, "Dark Modern", func(o *Options) { o.QuickDiff.Visibility = "hover" }, nil)
	noErrors(t, r.ts)
	testutil.MatchesGolden(r.ts, "quickdiff_hover_away_dark")
	r.ts.Hover(ui.Offset{Dx: 10, Dy: 100})
	r.settle()
	testutil.MatchesGolden(r.ts, "quickdiff_hover_over_dark")
}

// TestQuickDiffPlaces reads the pixels: the stripe of the gutter, the lane of the ruler and the mark of the minimap are each there or not
// as scm.diffDecorations says.
func TestQuickDiffPlaces(t *testing.T) {
	tests := []struct {
		decorations                string
		gutter, ruler, minimapMark bool
	}{
		{"all", true, true, true},
		{"gutter", true, false, false},
		{"overview", false, true, false},
		{"minimap", false, false, true},
		{"none", false, false, false},
	}
	for _, tt := range tests {
		t.Run(tt.decorations, func(t *testing.T) {
			var colors kit.Colors
			r := quickDiffRig(t, "Dark Modern", func(o *Options) { o.QuickDiff.Decorations = tt.decorations }, func(c *kit.Colors) { colors = *c })
			shot := r.ts.Screenshot()
			x := stripeX(r.vm, r.ts)
			// the stripe along the added block (line 6, index 5), the modified block is a hatch
			if got := samePixel(shot.RGBAAt(x+1, rowY(5)), colors.EditorGutterAdded); got != tt.gutter {
				t.Errorf("the added stripe is in the gutter: %v, want %v", got, tt.gutter)
			}
			// the ruler: the left third of the scrollbar column at the height of the added block
			size := r.ts.Screenshot().Bounds().Size()
			width := size.X
			rulerX := width - int(kit.DefaultVerticalScrollbarSize) + 1
			rulerY := 0
			found := false
			for y := 0; y < size.Y; y++ {
				if c := shot.RGBAAt(rulerX, y); c != shot.RGBAAt(rulerX-4, y) && c != (color.RGBA{}) && !samePixel(c, colors.EditorBackground) {
					found, rulerY = true, y
					break
				}
			}
			if found != tt.ruler {
				t.Errorf("a mark in the left lane of the ruler (first at y=%d): %v, want %v", rulerY, found, tt.ruler)
			}
			// the minimap: the gutter of the minimap, at the added block
			info := r.vm.mm.info
			mx := int(info.Left) + 2 + 1
			mfound := false
			for y := 0; y < size.Y; y++ {
				c := shot.RGBAAt(mx, y)
				if samePixel(c, colors.MinimapGutterAdded) || samePixel(c, colors.MinimapGutterModified) || samePixel(c, colors.MinimapGutterDeleted) {
					mfound = true
					break
				}
			}
			if mfound != tt.minimapMark {
				t.Errorf("a mark in the gutter of the minimap: %v, want %v", mfound, tt.minimapMark)
			}
		})
	}
}

func TestQuickDiffStripeWidthAndPattern(t *testing.T) {
	for _, width := range []int{1, 2, 3, 4, 5} {
		t.Run(strings.Repeat("w", width), func(t *testing.T) {
			var colors kit.Colors
			r := quickDiffRig(t, "Dark Modern", func(o *Options) { o.QuickDiff.GutterWidth = width }, func(c *kit.Colors) { colors = *c })
			shot := r.ts.Screenshot()
			x := stripeX(r.vm, r.ts)
			for dx := -1; dx <= width; dx++ {
				got := samePixel(shot.RGBAAt(x+dx, rowY(5)), colors.EditorGutterAdded)
				want := dx >= 0 && dx < width
				if got != want {
					t.Errorf("pixel %d of the added stripe is the color: %v, want %v", dx, got, want)
				}
			}
			// the modified block is a hatch: some pixels of its first tile are the color and some are not
			colored, plain := 0, 0
			for dx := 0; dx < width; dx++ {
				for dy := 0; dy < width; dy++ {
					if samePixel(shot.RGBAAt(x+dx, rowY(9)-9+dy), colors.EditorGutterModified) {
						colored++
					} else {
						plain++
					}
				}
			}
			if width == 1 {
				// a tile of one pixel is half of the gradient: the band that starts at the corner
				if colored+plain != 1 {
					t.Errorf("tile of one pixel has %d pixels", colored+plain)
				}
			} else if colored == 0 || plain == 0 {
				t.Errorf("the hatch of the modified block has %d colored and %d plain pixels in its first tile", colored, plain)
			}
		})
	}
}

func TestQuickDiffTriangleOfDeletedLines(t *testing.T) {
	var colors kit.Colors
	r := quickDiffRig(t, "Dark Modern", nil, func(c *kit.Colors) { colors = *c })
	shot := r.ts.Screenshot()
	x := stripeX(r.vm, r.ts)
	boundary := 14 * 19 // the bottom of line 14
	// a pixel of the triangle is not the color of the gutter, whole or blended at an edge
	gutter := shot.RGBAAt(x-2, boundary)
	has := func(dx, dy int) bool { return shot.RGBAAt(x+dx, boundary+dy) != gutter }
	if !has(0, 0) || !has(0, -3) || !has(0, 3) || !samePixel(shot.RGBAAt(x, boundary), colors.EditorGutterDeleted) {
		t.Error("the base of the triangle, 8 pixels tall, is not at the bottom of line 14")
	}
	if has(0, -6) || has(0, 6) {
		t.Error("the triangle is taller than 8 pixels")
	}
	if !has(2, 0) || has(2, 3) || has(5, 0) {
		t.Error("the triangle does not narrow to its tip, 4 pixels to the right")
	}
	// it is not a stripe: a line above it has nothing
	if samePixel(shot.RGBAAt(x+1, rowY(13)), colors.EditorGutterDeleted) {
		t.Error("the deletion is drawn along line 14")
	}
}

func TestQuickDiffHoverOnlyShowsWhileThePointerIsOverTheGutter(t *testing.T) {
	var colors kit.Colors
	r := quickDiffRig(t, "Dark Modern", func(o *Options) { o.QuickDiff.Visibility = "hover" }, func(c *kit.Colors) { colors = *c })
	x := stripeX(r.vm, r.ts)
	shown := func() bool { return samePixel(r.ts.Screenshot().RGBAAt(x+1, rowY(5)), colors.EditorGutterAdded) }
	if shown() {
		t.Error("the stripe shows with the pointer away")
	}
	r.ts.Hover(ui.Offset{Dx: 10, Dy: 100})
	r.settle()
	if !shown() {
		t.Error("the stripe does not show with the pointer over the gutter")
	}
	r.ts.Hover(ui.Offset{Dx: 300, Dy: 100})
	r.settle()
	if shown() {
		t.Error("the stripe stays after the pointer left the gutter")
	}
}
