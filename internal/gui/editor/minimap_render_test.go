package editor

import (
	"strings"
	"testing"

	"github.com/rm4n0s/glutter/ui"
)

func testMinimapStyle() minimapStyle {
	return minimapStyle{
		Width: 40, Height: 30, LineHeight: 3, Scale: 1, TabSize: 4,
		Background: minimapRGBA{30, 30, 30, 255}, DefaultBackground: minimapRGBA{30, 30, 30, 255},
		Foreground: minimapRGBA{255, 255, 255, 255}, ForegroundAlpha: 255,
	}
}

// lines is a source of minimap lines: line n is lines[n-1], all in the color the spans say.
func rowsOf2(texts []string, spans map[int][]span) func(int) minimapRowData {
	return func(line int) minimapRowData {
		text := texts[line-1]
		sp := spans[line]
		return minimapRowData{key: minimapRowKey{line: line, end: len(text), text: text, spans: spansKey(sp)}, text: text, spans: sp}
	}
}

func (p *minimapPainter) at(x, y int) minimapRGBA {
	i := (y*p.style.Width + x) * 4
	return minimapRGBA{p.pix[i], p.pix[i+1], p.pix[i+2], p.pix[i+3]}
}

func layoutOf(start, end int) minimapRenderLayout {
	return minimapRenderLayout{StartLine: start, EndLine: end}
}

var (
	minimapBG   = minimapRGBA{30, 30, 30, 255}
	minimapText = minimapRGBA{142, 142, 142, 255} // white moved halfway to the background: 30 + 225 / 2, rounded to even
)

func TestMinimapPainterDrawsBlocks(t *testing.T) {
	var p minimapPainter
	p.reset(testMinimapStyle())
	texts := []string{"ab c", "\tx", "", "日a"}
	if !p.frame(layoutOf(1, 4), rowsOf2(texts, nil)) {
		t.Fatal("the first frame did not change")
	}
	cases := []struct {
		name string
		x, y int
		want minimapRGBA
	}{
		{"the gutter", 3, 0, minimapBG},
		{"a at the first column, upper row", 8, 0, minimapText},
		{"a, second row", 8, 1, minimapText},
		{"a, the gap row below it", 8, 2, minimapBG},
		{"b", 9, 0, minimapText},
		{"the space", 10, 0, minimapBG},
		{"c", 11, 0, minimapText},
		{"after c", 12, 0, minimapBG},
		{"a tab takes four columns", 8, 3, minimapBG},
		{"the tab ends", 11, 3, minimapBG},
		{"x after the tab", 12, 3, minimapText},
		{"an empty line", 8, 6, minimapBG},
		{"a full-width character takes two blocks", 8, 9, minimapText},
		{"second block", 9, 9, minimapText},
		{"a after it", 10, 9, minimapText},
		{"below the last line", 8, 12, minimapBG},
	}
	for _, tc := range cases {
		if got := p.at(tc.x, tc.y); got != tc.want {
			t.Errorf("%s: pixel (%d, %d) = %v, want %v", tc.name, tc.x, tc.y, got, tc.want)
		}
	}
	if p.drawn != 4 || p.copied != 0 {
		t.Errorf("drawn %d copied %d, want 4 and 0", p.drawn, p.copied)
	}
	if img := p.image(); img == nil || img.Width() != 40 || img.Height() != 30 {
		t.Errorf("image %v", img)
	}
}

func TestMinimapPainterColorsAndOpacity(t *testing.T) {
	red := span{start: 0, end: 2, color: ui.NewColor(0xFFFF0000)}
	cases := []struct {
		name  string
		alpha uint8
		bg    minimapRGBA
		want  minimapRGBA
	}{
		{"opaque red", 255, minimapBG, minimapRGBA{142, 15, 15, 255}},                                   // red 30 + 225 / 2 = 142.5 (to even), green 30 - 30 / 2
		{"half opacity", 128, minimapBG, minimapRGBA{86, 22, 22, 255}},                                  // red 30 + 225 * 0.251 = 86.5, green 30 - 30 * 0.251 = 22.5 (to even)
		{"no opacity", 0, minimapBG, minimapBG},                                                         // c = 0
		{"a lighter background", 255, minimapRGBA{200, 200, 200, 255}, minimapRGBA{228, 100, 100, 255}}, // 200 + 55 / 2 = 227.5, 200 - 100 = 100
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			st := testMinimapStyle()
			st.ForegroundAlpha, st.Background, st.DefaultBackground = tc.alpha, tc.bg, tc.bg
			var p minimapPainter
			p.reset(st)
			p.frame(layoutOf(1, 1), rowsOf2([]string{"xy z"}, map[int][]span{1: {red}}))
			if got := p.at(8, 0); got != tc.want {
				t.Errorf("a red block = %v, want %v", got, tc.want)
			}
			// the third character is in no span: the foreground color
			if tc.alpha == 255 && tc.bg == minimapBG {
				if got := p.at(11, 0); got != minimapText {
					t.Errorf("a character with no color = %v, want %v", got, minimapText)
				}
			}
		})
	}
}

func TestMinimapPainterStopsAtTheEdge(t *testing.T) {
	st := testMinimapStyle()
	st.Width = 16 // the gutter and eight blocks
	var p minimapPainter
	p.reset(st)
	p.frame(layoutOf(1, 1), rowsOf2([]string{strings.Repeat("x", 30)}, nil))
	for x := 8; x < 16; x++ {
		if got := p.at(x, 0); got != minimapText {
			t.Errorf("block %d = %v", x, got)
		}
	}
	// a full-width character that does not fit is left out whole or in part, never over the edge
	p.reset(st)
	p.frame(layoutOf(1, 1), rowsOf2([]string{"xxxxxxx日"}, nil))
	if got := p.at(15, 0); got != minimapText {
		t.Errorf("the first block of the wide character = %v", got)
	}
}

func TestMinimapPainterScaleAndSingleRowLines(t *testing.T) {
	st := testMinimapStyle()
	st.Scale, st.LineHeight = 2, 6
	var p minimapPainter
	p.reset(st)
	p.frame(layoutOf(1, 2), rowsOf2([]string{"a", "b"}, nil))
	// at scale 2 a block is 2 wide and 4 high, and the line 6 high
	for _, c := range []struct {
		x, y int
		want minimapRGBA
	}{{8, 0, minimapText}, {9, 3, minimapText}, {10, 0, minimapBG}, {8, 4, minimapBG}, {8, 5, minimapBG}, {9, 6, minimapText}, {9, 9, minimapText}, {9, 10, minimapBG}} {
		if got := p.at(c.x, c.y); got != c.want {
			t.Errorf("scale 2: pixel (%d, %d) = %v, want %v", c.x, c.y, got, c.want)
		}
	}
	// a minimap of lines of one pixel draws blocks of one pixel
	st = testMinimapStyle()
	st.LineHeight = 1
	p.reset(st)
	p.frame(layoutOf(1, 2), rowsOf2([]string{"a", "b"}, nil))
	if p.at(8, 0) != minimapText || p.at(8, 1) != minimapText || p.at(8, 2) != minimapBG {
		t.Errorf("lines of a pixel: %v %v %v", p.at(8, 0), p.at(8, 1), p.at(8, 2))
	}
}

func TestMinimapPainterTranslucentBackground(t *testing.T) {
	st := testMinimapStyle()
	st.Background = minimapRGBA{100, 0, 0, 128}
	var p minimapPainter
	p.reset(st)
	p.frame(layoutOf(1, 1), rowsOf2([]string{"x"}, nil))
	// premultiplied: 100 * 128 / 255 = 50
	if got := p.at(0, 0); got != (minimapRGBA{50, 0, 0, 128}) {
		t.Errorf("the background = %v", got)
	}
	// the blocks blend with the background over the editor's: 128/255 of 100 over 30 = 65
	if got := p.at(8, 0); got.A != 255 {
		t.Errorf("a block is opaque: %v", got)
	}
}

func TestMinimapPainterOnlyDrawsTheLinesThatChanged(t *testing.T) {
	texts := make([]string, 10)
	for i := range texts {
		texts[i] = strings.Repeat("x", i+1)
	}
	var p minimapPainter
	p.reset(testMinimapStyle())
	p.frame(layoutOf(1, 10), rowsOf2(texts, nil))
	first := append([]byte(nil), p.pix...)

	// the same frame again: nothing changes, nothing is drawn (the counts stay those of the frame
	// that drew)
	if p.frame(layoutOf(1, 10), rowsOf2(texts, nil)) || p.drawn != 10 || p.copied != 0 {
		t.Errorf("an unchanged frame: drawn %d copied %d", p.drawn, p.copied)
	}

	// one line changes
	edited := append([]string(nil), texts...)
	edited[4] = "changed"
	if !p.frame(layoutOf(1, 10), rowsOf2(edited, nil)) || p.drawn != 1 || p.copied != 9 {
		t.Errorf("one line edited: drawn %d copied %d, want 1 and 9", p.drawn, p.copied)
	}
	// the other lines are what they were
	for y := 0; y < 30; y++ {
		if y/3 == 4 {
			continue
		}
		for x := range 40 {
			i := (y*40 + x) * 4
			if string(p.pix[i:i+4]) != string(first[i:i+4]) {
				t.Fatalf("pixel (%d, %d) of an untouched line changed", x, y)
			}
		}
	}

	// the colors of a line change (the tokenizer came by)
	colored := map[int][]span{3: {{start: 0, end: 3, color: ui.NewColor(0xFF00FF00)}}}
	if !p.frame(layoutOf(1, 10), rowsOf2(edited, colored)) || p.drawn != 1 || p.copied != 9 {
		t.Errorf("one line colored: drawn %d copied %d", p.drawn, p.copied)
	}
}

func TestMinimapPainterScrolling(t *testing.T) {
	texts := make([]string, 30)
	for i := range texts {
		texts[i] = strings.Repeat("y", i%7+1)
	}
	var p minimapPainter
	p.reset(testMinimapStyle())
	p.frame(layoutOf(1, 10), rowsOf2(texts, nil))
	// one line further down: nine lines move up, one is new
	if !p.frame(layoutOf(2, 11), rowsOf2(texts, nil)) || p.drawn != 1 || p.copied != 9 {
		t.Errorf("scrolled by a line: drawn %d copied %d, want 1 and 9", p.drawn, p.copied)
	}
	var fresh minimapPainter
	fresh.reset(testMinimapStyle())
	fresh.frame(layoutOf(2, 11), rowsOf2(texts, nil))
	if string(p.pix) != string(fresh.pix) {
		t.Error("a scrolled frame is not what a frame drawn from scratch is")
	}
	// padding at the top moves the lines down
	pad := layoutOf(1, 9)
	pad.TopPaddingLineCount = 1
	p.frame(pad, rowsOf2(texts, nil))
	fresh.reset(testMinimapStyle())
	fresh.frame(pad, rowsOf2(texts, nil))
	if string(p.pix) != string(fresh.pix) {
		t.Error("a frame with padding is not what a frame drawn from scratch is")
	}
	if p.at(8, 0) != minimapBG || p.at(8, 3) != minimapText {
		t.Errorf("padding: %v %v", p.at(8, 0), p.at(8, 3))
	}
	// a jump far away copies nothing
	p.frame(layoutOf(20, 29), rowsOf2(texts, nil))
	if p.copied != 0 || p.drawn != 10 {
		t.Errorf("a jump: drawn %d copied %d", p.drawn, p.copied)
	}
}

func TestMinimapPainterWithNothingToDraw(t *testing.T) {
	var p minimapPainter
	p.reset(minimapStyle{})
	if p.frame(layoutOf(1, 1), rowsOf2([]string{"x"}, nil)) || p.image() != nil {
		t.Error("a canvas with no size drew something")
	}
	p.reset(testMinimapStyle())
	if !p.frame(layoutOf(1, 0), rowsOf2(nil, nil)) {
		t.Error("an empty first frame did not change")
	}
	if got := p.at(8, 0); got != minimapBG {
		t.Errorf("empty: %v", got)
	}
}
