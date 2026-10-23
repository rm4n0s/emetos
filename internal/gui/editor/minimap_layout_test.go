package editor

import (
	"math"
	"testing"
)

// minimapIn is the input of a 800 by 600 editor with the default settings of the minimap, whose
// text starts 60 pixels from the left.
func minimapIn(tweak func(*minimapInput)) minimapInput {
	in := minimapInput{
		MinimapOptions: MinimapOptions{Enabled: true, Side: "right", Size: "proportional", ShowSlider: "mouseover", Scale: 1, MaxColumn: 120},
		OuterWidth:     800, OuterHeight: 600, RemainingWidth: 740, LineHeight: 19, CharWidth: 8.4, PixelRatio: 1,
		ScrollBeyondLastLine: true, VerticalScrollbarWidth: 14, ViewLineCount: 100,
	}
	if tweak != nil {
		tweak(&in)
	}
	return in
}

// The expected values of these tables come from working VS Code's formulas out again by hand (a
// second implementation in a script), since VS Code has no test of its layout.
func TestComputeMinimapLayout(t *testing.T) {
	cases := []struct {
		name  string
		tweak func(*minimapInput)
		want  minimapLayoutInfo
	}{
		{"default", nil, minimapLayoutInfo{Show: true, Left: 701, Width: 85, Scale: 1, LineHeight: 3,
			CanvasInnerWidth: 85, CanvasInnerHeight: 600, CanvasOuterWidth: 85, CanvasOuterHeight: 600}},
		{"capped by the maximum column", func(in *minimapInput) { in.OuterWidth, in.RemainingWidth = 2060, 2000 },
			minimapLayoutInfo{Show: true, Left: 1926, Width: 120, Scale: 1, LineHeight: 3,
				CanvasInnerWidth: 120, CanvasInnerHeight: 600, CanvasOuterWidth: 120, CanvasOuterHeight: 600}},
		{"on the left", func(in *minimapInput) { in.Side = "left" }, minimapLayoutInfo{Show: true, Left: 0, Width: 85, Scale: 1, LineHeight: 3,
			CanvasInnerWidth: 85, CanvasInnerHeight: 600, CanvasOuterWidth: 85, CanvasOuterHeight: 600}},
		{"scale 2", func(in *minimapInput) { in.Scale = 2 }, minimapLayoutInfo{Show: true, Left: 639, Width: 147, Scale: 2, LineHeight: 6,
			CanvasInnerWidth: 147, CanvasInnerHeight: 600, CanvasOuterWidth: 147, CanvasOuterHeight: 600}},
		{"a screen of two pixels per pixel", func(in *minimapInput) { in.PixelRatio = 2 }, minimapLayoutInfo{Show: true, Left: 701, Width: 85, Scale: 2, LineHeight: 6,
			CanvasInnerWidth: 170, CanvasInnerHeight: 1200, CanvasOuterWidth: 85, CanvasOuterHeight: 600}},
		{"fill stretches a short text", func(in *minimapInput) { in.Size, in.ViewLineCount = "fill", 10 },
			minimapLayoutInfo{Show: true, Left: 701, Width: 85, HeightIsEditorHeight: true, Scale: 2, LineHeight: 14,
				CanvasInnerWidth: 170, CanvasInnerHeight: 569, CanvasOuterWidth: 85, CanvasOuterHeight: 600}},
		{"fill, a text of 30 rows", func(in *minimapInput) { in.Size, in.ViewLineCount = "fill", 30 },
			minimapLayoutInfo{Show: true, Left: 701, Width: 85, HeightIsEditorHeight: true, Scale: 2, LineHeight: 9,
				CanvasInnerWidth: 170, CanvasInnerHeight: 546, CanvasOuterWidth: 85, CanvasOuterHeight: 600}},
		{"fit leaves a short text as it is", func(in *minimapInput) { in.Size, in.ViewLineCount = "fit", 10 },
			minimapLayoutInfo{Show: true, Left: 701, Width: 85, Scale: 1, LineHeight: 3,
				CanvasInnerWidth: 85, CanvasInnerHeight: 600, CanvasOuterWidth: 85, CanvasOuterHeight: 600}},
		{"fit shrinks a long text by sampling", func(in *minimapInput) { in.Size, in.ViewLineCount = "fit", 2000 },
			minimapLayoutInfo{Show: true, Left: 701, Width: 85, HeightIsEditorHeight: true, IsSampling: true, Scale: 1, LineHeight: 1,
				CanvasInnerWidth: 85, CanvasInnerHeight: 600, CanvasOuterWidth: 85, CanvasOuterHeight: 600}},
		{"fill samples a long text", func(in *minimapInput) { in.Size, in.ViewLineCount = "fill", 1000 },
			minimapLayoutInfo{Show: true, Left: 701, Width: 85, HeightIsEditorHeight: true, IsSampling: true, Scale: 1, LineHeight: 1,
				CanvasInnerWidth: 85, CanvasInnerHeight: 600, CanvasOuterWidth: 85, CanvasOuterHeight: 600}},
		{"disabled", func(in *minimapInput) { in.Enabled = false }, minimapLayoutInfo{Scale: 1, LineHeight: 1, CanvasInnerHeight: 600, CanvasOuterHeight: 600}},
		{"no room", func(in *minimapInput) { in.RemainingWidth = 10 }, minimapLayoutInfo{Show: true, Left: 778, Width: 8, Scale: 1, LineHeight: 3,
			CanvasInnerWidth: 8, CanvasInnerHeight: 600, CanvasOuterWidth: 8, CanvasOuterHeight: 600}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := computeMinimapLayout(minimapIn(tc.tweak)); got != tc.want {
				t.Errorf("layout\n got %+v\nwant %+v", got, tc.want)
			}
		})
	}
}

func TestMinimapSampling(t *testing.T) {
	in := minimapIn(func(in *minimapInput) { in.Size, in.ViewLineCount = "fill", 1000 })
	layout := computeMinimapLayout(in)
	s := computeMinimapSampling(layout, in)
	if s == nil {
		t.Fatal("no sampling")
	}
	if len(s.Lines) != 582 {
		t.Fatalf("%d minimap lines, want 582", len(s.Lines))
	}
	if math.Abs(s.Ratio-1000.0/582.0) > 1e-9 {
		t.Errorf("ratio %v", s.Ratio)
	}
	for i, want := range map[int]int{0: 1, 1: 3, 2: 4, 100: 173, 580: 997, 581: 1000} {
		if s.Lines[i] != want {
			t.Errorf("line %d samples row %d, want %d", i, s.Lines[i], want)
		}
	}
	for i, row := range []int{1, 1000, 500} {
		got := s.rowToMinimapLine(row)
		want := []int{1, 582, 291}[i]
		if got != want {
			t.Errorf("row %d is on the minimap line %d, want %d", row, got, want)
		}
	}
	// no sampling when the minimap shows every row
	if computeMinimapSampling(computeMinimapLayout(minimapIn(nil)), minimapIn(nil)) != nil {
		t.Error("sampling in proportional mode")
	}
	if computeMinimapSampling(minimapLayoutInfo{}, in) != nil {
		t.Error("sampling with no minimap")
	}
}

func TestMinimapRenderLayout(t *testing.T) {
	const lineHeight = 19.0
	cases := []struct {
		name  string
		tweak func(*minimapInput)
		view  minimapView
		prev  *minimapRenderLayout
		want  minimapRenderLayout
	}{
		{"a short text at the top", nil,
			minimapView{ViewportStartLine: 1, ViewportEndLine: 32, ViewportHeight: 600, LineCount: 50, RealLineCount: 50, ScrollHeight: 1531},
			nil, minimapRenderLayout{SliderNeeded: true, SliderHeight: 94, StartLine: 1, EndLine: 50, ScrollHeight: 1531}},
		{"a short text at the bottom", nil,
			minimapView{ViewportStartLine: 50, ViewportEndLine: 50, ViewportStartOffset: 931 - 931%19, ViewportHeight: 600, LineCount: 50, RealLineCount: 50, ScrollTop: 931, ScrollHeight: 1531},
			nil, minimapRenderLayout{SliderNeeded: true, SliderTop: 144, SliderHeight: 94, StartLine: 1, EndLine: 50, ScrollTop: 931, ScrollHeight: 1531}},
		{"a long text at the top", nil,
			minimapView{ViewportStartLine: 1, ViewportEndLine: 32, ViewportHeight: 600, LineCount: 2000, RealLineCount: 2000, ScrollHeight: 38581},
			nil, minimapRenderLayout{SliderNeeded: true, SliderHeight: 94, StartLine: 1, EndLine: 200, ScrollHeight: 38581}},
		{"a long text in the middle", nil,
			minimapView{ViewportStartLine: 501, ViewportEndLine: 532, ViewportStartOffset: 9500, ViewportHeight: 600, LineCount: 2000, RealLineCount: 2000, ScrollTop: 9500, ScrollHeight: 38581},
			nil, minimapRenderLayout{SliderNeeded: true, SliderTop: 129, SliderHeight: 94, StartLine: 458, EndLine: 657, ScrollTop: 9500, ScrollHeight: 38581}},
		{"a long text near the end", nil,
			minimapView{ViewportStartLine: 1900, ViewportEndLine: 1931, ViewportStartOffset: 35000, ViewportHeight: 600, LineCount: 2000, RealLineCount: 2000, ScrollTop: 35000, ScrollHeight: 38581},
			nil, minimapRenderLayout{SliderNeeded: true, SliderTop: 468, SliderHeight: 94, StartLine: 1744, EndLine: 1943, ScrollTop: 35000, ScrollHeight: 38581}},
		{"the slider follows a partial first line", nil,
			minimapView{ViewportStartLine: 501, ViewportEndLine: 532, ViewportStartOffset: 9500, ViewportHeight: 600, LineCount: 2000, RealLineCount: 2000, ScrollTop: 9510, ScrollHeight: 38581},
			nil, minimapRenderLayout{SliderNeeded: true, SliderTop: 130.57894736842104, SliderHeight: 94, StartLine: 458, EndLine: 657, ScrollTop: 9510, ScrollHeight: 38581}},
		{"scrolling up never moves the first line down", nil,
			minimapView{ViewportStartLine: 501, ViewportEndLine: 532, ViewportStartOffset: 9500, ViewportHeight: 600, LineCount: 2000, RealLineCount: 2000, ScrollTop: 9510, ScrollHeight: 38581},
			&minimapRenderLayout{ScrollTop: 9600, ScrollHeight: 38581, StartLine: 470},
			minimapRenderLayout{SliderNeeded: true, SliderTop: 130.57894736842104, SliderHeight: 94, StartLine: 458, EndLine: 657, ScrollTop: 9510, ScrollHeight: 38581}},
		{"scrolling down never moves the first line up", nil,
			minimapView{ViewportStartLine: 501, ViewportEndLine: 532, ViewportStartOffset: 9500, ViewportHeight: 600, LineCount: 2000, RealLineCount: 2000, ScrollTop: 9510, ScrollHeight: 38581},
			&minimapRenderLayout{ScrollTop: 9400, ScrollHeight: 38581, StartLine: 490},
			minimapRenderLayout{SliderNeeded: true, SliderTop: 34.578947368421055, SliderHeight: 94, StartLine: 490, EndLine: 689, ScrollTop: 9510, ScrollHeight: 38581}},
		{"a previous layout of another text is ignored", nil,
			minimapView{ViewportStartLine: 501, ViewportEndLine: 532, ViewportStartOffset: 9500, ViewportHeight: 600, LineCount: 2000, RealLineCount: 2000, ScrollTop: 9510, ScrollHeight: 38581},
			&minimapRenderLayout{ScrollTop: 9400, ScrollHeight: 100, StartLine: 490},
			minimapRenderLayout{SliderNeeded: true, SliderTop: 130.57894736842104, SliderHeight: 94, StartLine: 458, EndLine: 657, ScrollTop: 9510, ScrollHeight: 38581}},
		{"fill makes the slider as high as the share of the text on the screen",
			func(in *minimapInput) { in.Size, in.ViewLineCount = "fill", 10 },
			minimapView{ViewportStartLine: 1, ViewportEndLine: 10, ViewportHeight: 600, LineCount: 10, RealLineCount: 10, ScrollTop: 100, ScrollHeight: 10*19 + 600 - 19},
			nil, minimapRenderLayout{SliderNeeded: true, SliderTop: 78.3625730994152, SliderHeight: 466, StartLine: 1, EndLine: 10, ScrollTop: 100, ScrollHeight: 10*19 + 600 - 19}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			in := minimapIn(tc.tweak)
			in.LineHeight = lineHeight
			layout := computeMinimapLayout(in)
			got := createMinimapRenderLayout(layout, in, tc.view, tc.prev)
			got.sliderRatio = 0 // checked on its own
			if math.Abs(got.SliderTop-tc.want.SliderTop) > 1e-6 {
				t.Errorf("slider top %v, want %v", got.SliderTop, tc.want.SliderTop)
			}
			got.SliderTop, tc.want.SliderTop = 0, 0
			if got != tc.want {
				t.Errorf("layout\n got %+v\nwant %+v", got, tc.want)
			}
		})
	}
}

func TestMinimapSliderMath(t *testing.T) {
	in := minimapIn(nil)
	layout := computeMinimapLayout(in)
	l := createMinimapRenderLayout(layout, in, minimapView{ViewportStartLine: 501, ViewportEndLine: 532, ViewportStartOffset: 9500,
		ViewportHeight: 600, LineCount: 2000, RealLineCount: 2000, ScrollTop: 9500, ScrollHeight: 38581}, nil)
	// moving the slider by 100 pixels scrolls by 100 / ratio pixels
	if got, want := l.desiredScrollTopFromDelta(100), math.Round(9500+100/0.013322450699033728); got != want {
		t.Errorf("scroll for a delta of 100: %v, want %v", got, want)
	}
	if got := l.desiredScrollTopFromDelta(0); got != 9500 {
		t.Errorf("no delta: %v", got)
	}
	if first, last, ok := l.intersect(400, 460); !ok || first != 458 || last != 460 {
		t.Errorf("intersect(400, 460) = %d %d %v", first, last, ok)
	}
	if _, _, ok := l.intersect(1, 457); ok {
		t.Error("lines before the first intersect")
	}
	if _, _, ok := l.intersect(658, 900); ok {
		t.Error("lines after the last intersect")
	}
	if got := l.yForLine(460, 3); got != 6 {
		t.Errorf("y of line 460 = %d, want 6", got)
	}
	// no ratio (the text fits): the scroll does not move
	if got := (minimapRenderLayout{ScrollTop: 7}).desiredScrollTopFromDelta(50); got != 7 {
		t.Errorf("no ratio: %v", got)
	}
}
