package editor

import "math"

// Ported from VS Code: src/vs/editor/common/config/editorOptions.ts (EditorLayoutInfoComputer) and
// src/vs/editor/browser/viewParts/minimap/minimap.ts (MinimapLayout, MinimapSamplingState)

const (
	// minimapGutterWidth is the space at the left of the lines of the minimap, in canvas pixels.
	minimapGutterWidth = 8
	// minimapBaseCharWidth and minimapBaseCharHeight are the size of a character block at scale 1,
	// in canvas pixels; a line of blocks is one pixel (times the scale) taller than a block.
	minimapBaseCharWidth  = 1
	minimapBaseCharHeight = 2
)

// MinimapOptions are the settings of the minimap (editor.minimap.*).
type MinimapOptions struct {
	// Enabled is editor.minimap.enabled.
	Enabled bool
	// Side is editor.minimap.side: "right" or "left".
	Side string
	// Size is editor.minimap.size: "proportional", "fill" or "fit".
	Size string
	// ShowSlider is editor.minimap.showSlider: "always" or "mouseover".
	ShowSlider string
	// Scale is editor.minimap.scale, from 1 to 3.
	Scale int
	// MaxColumn is editor.minimap.maxColumn.
	MaxColumn int
}

// minimapInput is what the layout of the minimap is computed from.
type minimapInput struct {
	MinimapOptions
	// OuterWidth and OuterHeight are the size of the editor.
	OuterWidth, OuterHeight float64
	// RemainingWidth is the width of the editor without its gutter.
	RemainingWidth float64
	// LineHeight and CharWidth are the height of a row and the width of a character of the text.
	LineHeight, CharWidth float64
	// PixelRatio is the number of device pixels of a pixel of the window.
	PixelRatio float64
	// ScrollBeyondLastLine is editor.scrollBeyondLastLine.
	ScrollBeyondLastLine bool
	// VerticalScrollbarWidth is the width of the scrollbar, which the minimap is next to.
	VerticalScrollbarWidth float64
	// ViewLineCount is the number of rows.
	ViewLineCount int
}

// minimapLayoutInfo is where the minimap is and how it draws.
type minimapLayoutInfo struct {
	// Show says whether there is a minimap.
	Show bool
	// Left and Width are the position and the width of the minimap in the editor.
	Left, Width float64
	// HeightIsEditorHeight says that the minimap is as high as the editor (fill, fit and sampling).
	HeightIsEditorHeight bool
	// IsSampling says that the minimap shows some of the rows (fill and fit with more rows than
	// fit).
	IsSampling bool
	// Scale is the size of a block in canvas pixels, and LineHeight the height of a line.
	Scale      int
	LineHeight int
	// CanvasInnerWidth and CanvasInnerHeight are the size of the canvas in device pixels;
	// CanvasOuterWidth and CanvasOuterHeight in pixels of the window.
	CanvasInnerWidth, CanvasInnerHeight int
	CanvasOuterWidth, CanvasOuterHeight float64
}

// containedMinimapLines is how many minimap lines a viewport of rows needs: the number of rows
// with the room around them, scaled so that all of them fit in the height of the editor.
//
// Ported from VS Code: editorOptions.ts (EditorLayoutInfoComputer.computeContainedMinimapLineCount)
type containedLines struct {
	typicalViewportLineCount, extraLinesBeforeFirstLine, extraLinesBeyondLastLine float64
	desiredRatio                                                                  float64
	minimapLineCount                                                              int
}

func containedMinimapLineCount(viewLineCount int, scrollBeyondLastLine bool, height, lineHeight, pixelRatio float64) containedLines {
	typical := height / lineHeight
	var c containedLines
	c.typicalViewportLineCount = typical
	c.extraLinesBeforeFirstLine = 0
	c.extraLinesBeyondLastLine = 0
	if scrollBeyondLastLine {
		c.extraLinesBeyondLastLine = math.Max(0, typical-1)
	}
	c.desiredRatio = (c.extraLinesBeforeFirstLine + float64(viewLineCount) + c.extraLinesBeyondLastLine) / (pixelRatio * height)
	c.minimapLineCount = int(math.Floor(float64(viewLineCount) / c.desiredRatio))
	return c
}

// computeMinimapLayout finds the size and position of the minimap, and the scale of its blocks.
// Emetos always draws blocks, so a line is three canvas pixels tall at scale 1 (VS Code draws
// characters at two when editor.minimap.renderCharacters is on), and the layout is made again
// from nothing each time (VS Code remembers the last one to avoid a loop between the width of
// the minimap and the wrapping of the text).
//
// Ported from VS Code: editorOptions.ts (EditorLayoutInfoComputer._computeMinimapLayout)
func computeMinimapLayout(in minimapInput) minimapLayoutInfo {
	pixelRatio := in.PixelRatio
	if pixelRatio <= 0 {
		pixelRatio = 1
	}
	if !in.Enabled || in.LineHeight <= 0 || in.OuterHeight <= 0 {
		return minimapLayoutInfo{
			Scale: 1, LineHeight: 1,
			CanvasInnerHeight: int(math.Floor(pixelRatio * in.OuterHeight)), CanvasOuterHeight: in.OuterHeight,
		}
	}
	scale := float64(max(in.Scale, 1))
	if pixelRatio >= 2 {
		scale = math.Round(scale * 2)
	}
	const baseCharHeight = 3
	innerHeight := math.Floor(pixelRatio * in.OuterHeight)
	outerHeight := innerHeight / pixelRatio
	heightIsEditorHeight, sampling := false, false
	lineHeight := baseCharHeight * scale
	charWidth := scale / pixelRatio
	widthMultiplier := 1.0

	if in.Size == "fill" || in.Size == "fit" {
		c := containedMinimapLineCount(in.ViewLineCount, in.ScrollBeyondLastLine, in.OuterHeight, in.LineHeight, pixelRatio)
		ratio := float64(in.ViewLineCount) / float64(c.minimapLineCount)
		if ratio > 1 {
			heightIsEditorHeight, sampling = true, true
			scale, lineHeight = 1, 1
			charWidth = scale / pixelRatio
		} else {
			fitBecomesFill := false
			maxScale := scale + 1
			if in.Size == "fit" {
				effective := math.Ceil((c.extraLinesBeforeFirstLine + float64(in.ViewLineCount) + c.extraLinesBeyondLastLine) * lineHeight)
				fitBecomesFill = effective > innerHeight
			}
			if in.Size == "fill" || fitBecomesFill {
				heightIsEditorHeight = true
				configured := scale
				lineHeight = math.Min(in.LineHeight*pixelRatio, math.Max(1, math.Floor(1/c.desiredRatio)))
				scale = math.Min(maxScale, math.Max(1, math.Floor(lineHeight/baseCharHeight)))
				if scale > configured {
					widthMultiplier = math.Min(2, scale/configured)
				}
				charWidth = scale / pixelRatio / widthMultiplier
				innerHeight = math.Ceil(math.Max(c.typicalViewportLineCount, c.extraLinesBeforeFirstLine+float64(in.ViewLineCount)+c.extraLinesBeyondLastLine) * lineHeight)
			}
		}
	}

	maxWidth := math.Floor(float64(in.MaxColumn) * charWidth)
	width := math.Min(maxWidth, math.Max(0, math.Floor(((in.RemainingWidth-in.VerticalScrollbarWidth-2)*charWidth)/(in.CharWidth+charWidth)))+minimapGutterWidth)

	innerWidth := math.Floor(pixelRatio * width)
	outerWidth := innerWidth / pixelRatio
	innerWidth = math.Floor(innerWidth * widthMultiplier)

	left := in.OuterWidth - width - in.VerticalScrollbarWidth
	if in.Side == "left" {
		left = 0
	}
	return minimapLayoutInfo{
		Show: true, Left: left, Width: width,
		HeightIsEditorHeight: heightIsEditorHeight, IsSampling: sampling,
		Scale: int(scale), LineHeight: int(lineHeight),
		CanvasInnerWidth: int(innerWidth), CanvasInnerHeight: int(innerHeight),
		CanvasOuterWidth: outerWidth, CanvasOuterHeight: outerHeight,
	}
}

// minimapSampling says which rows the minimap shows when it has fewer lines than the editor has
// rows: line i of the minimap (0-based) shows the 1-based row Lines[i].
//
// Ported from VS Code: minimap.ts (MinimapSamplingState.compute, from nothing)
type minimapSampling struct {
	Ratio float64
	Lines []int
}

// computeMinimapSampling samples rows for the minimap, or returns nil when it shows them all.
func computeMinimapSampling(l minimapLayoutInfo, in minimapInput) *minimapSampling {
	if !l.Show || !l.IsSampling {
		return nil
	}
	c := containedMinimapLineCount(in.ViewLineCount, in.ScrollBeyondLastLine, in.OuterHeight, in.LineHeight, in.PixelRatio)
	n := c.minimapLineCount
	if n <= 0 {
		return nil
	}
	ratio := float64(in.ViewLineCount) / float64(n)
	half := ratio / 2
	lines := make([]int, n)
	lines[0] = 1
	if n > 1 {
		for i := 0; i < n-1; i++ {
			lines[i] = int(math.Round(float64(i)*ratio + half))
		}
		lines[n-1] = in.ViewLineCount
	}
	return &minimapSampling{Ratio: ratio, Lines: lines}
}

// rowToMinimapLine is the 1-based minimap line of a 1-based row.
func (s *minimapSampling) rowToMinimapLine(row int) int {
	return min(len(s.Lines), max(1, int(math.Round(float64(row)/s.Ratio))))
}

// minimapView is what the slider and the lines to draw are made from: where the editor is
// scrolled and which rows it shows.
type minimapView struct {
	// ViewportStartLine and ViewportEndLine are the 1-based first and last rows on the screen, and
	// ViewportStartOffset the distance of the first one from the top of the text, in pixels.
	ViewportStartLine, ViewportEndLine int
	ViewportStartOffset                float64
	ViewportHeight                     float64
	// LineCount is the number of lines of the minimap (rows, or sampled rows) and RealLineCount
	// the number of rows.
	LineCount, RealLineCount int
	ScrollTop, ScrollHeight  float64
}

// minimapRenderLayout is where the slider is and which lines the minimap draws.
type minimapRenderLayout struct {
	ScrollTop, ScrollHeight float64
	// SliderNeeded says whether the slider is shown: the text is longer than the viewport.
	SliderNeeded bool
	// SliderTop and SliderHeight are in pixels of the window.
	SliderTop, SliderHeight float64
	// TopPaddingLineCount are empty lines at the top of the minimap, and StartLine and EndLine the
	// 1-based first and last lines it draws.
	TopPaddingLineCount, StartLine, EndLine int
	sliderRatio                             float64
}

// desiredScrollTopFromDelta is the scroll position that moves the slider by delta pixels.
func (l minimapRenderLayout) desiredScrollTopFromDelta(delta float64) float64 {
	if l.sliderRatio == 0 {
		return l.ScrollTop
	}
	return math.Round(l.ScrollTop + delta/l.sliderRatio)
}

// intersect is the lines between two 1-based lines that the minimap draws.
func (l minimapRenderLayout) intersect(first, last int) (int, int, bool) {
	first, last = max(l.StartLine, first), min(l.EndLine, last)
	return first, last, first <= last
}

// yForLine is the y of a 1-based line in the canvas.
func (l minimapRenderLayout) yForLine(line, lineHeight int) int {
	return (line - l.StartLine + l.TopPaddingLineCount) * lineHeight
}

// createMinimapRenderLayout finds the slider and the lines of the minimap. prev is the layout of
// the last frame, which keeps the first line from flickering while the editor scrolls.
//
// Ported from VS Code: minimap.ts (MinimapLayout.create), with no padding option and with
// editor.scrollBeyondLastLine as the only extra room
func createMinimapRenderLayout(l minimapLayoutInfo, in minimapInput, v minimapView, prev *minimapRenderLayout) minimapRenderLayout {
	pixelRatio := math.Max(in.PixelRatio, 1)
	lineHeight := float64(l.LineHeight)
	linesFitting := int(math.Floor(float64(l.CanvasInnerHeight) / lineHeight))
	editorLineHeight := in.LineHeight

	if l.HeightIsEditorHeight {
		logical := float64(v.RealLineCount) * editorLineHeight
		if in.ScrollBeyondLastLine {
			logical += math.Max(0, v.ViewportHeight-editorLineHeight)
		}
		sliderHeight := math.Max(1, math.Floor(v.ViewportHeight*v.ViewportHeight/logical))
		maxSliderTop := math.Max(0, in.OuterHeight-sliderHeight)
		ratio := maxSliderTop / (v.ScrollHeight - v.ViewportHeight)
		if math.IsInf(ratio, 0) || math.IsNaN(ratio) {
			ratio = 0
		}
		return minimapRenderLayout{
			ScrollTop: v.ScrollTop, ScrollHeight: v.ScrollHeight, SliderNeeded: maxSliderTop > 0, sliderRatio: ratio,
			SliderTop: v.ScrollTop * ratio, SliderHeight: sliderHeight,
			StartLine: 1, EndLine: min(v.LineCount, linesFitting),
		}
	}

	expected := v.ViewportHeight / editorLineHeight
	sliderHeight := math.Floor(expected * lineHeight / pixelRatio)

	extraAtTop := 0
	extraAtBottom := 0.0
	if in.ScrollBeyondLastLine {
		extraAtBottom = math.Max(extraAtBottom, expected-1)
	}

	var maxSliderTop float64
	if extraAtBottom > 0 {
		maxSliderTop = (float64(extraAtTop) + float64(v.LineCount) + extraAtBottom - expected - 1) * lineHeight / pixelRatio
	} else {
		maxSliderTop = math.Max(0, (float64(extraAtTop)+float64(v.LineCount))*lineHeight/pixelRatio-sliderHeight)
	}
	maxSliderTop = math.Min(in.OuterHeight-sliderHeight, maxSliderTop)

	ratio := maxSliderTop / (v.ScrollHeight - v.ViewportHeight)
	if math.IsInf(ratio, 0) || math.IsNaN(ratio) {
		ratio = 0
	}
	sliderTop := v.ScrollTop * ratio

	if float64(linesFitting) >= float64(extraAtTop)+float64(v.LineCount)+extraAtBottom {
		// all lines fit
		return minimapRenderLayout{
			ScrollTop: v.ScrollTop, ScrollHeight: v.ScrollHeight, SliderNeeded: maxSliderTop > 0, sliderRatio: ratio,
			SliderTop: sliderTop, SliderHeight: sliderHeight, TopPaddingLineCount: extraAtTop, StartLine: 1, EndLine: v.LineCount,
		}
	}

	var considering float64
	if v.ViewportStartLine > 1 {
		considering = float64(v.ViewportStartLine + extraAtTop)
	} else {
		considering = math.Max(1, v.ScrollTop/editorLineHeight)
	}
	topPadding := 0
	startLine := max(1, int(math.Floor(considering-sliderTop*pixelRatio/lineHeight)))
	if startLine < extraAtTop {
		topPadding = extraAtTop - startLine + 1
		startLine = 1
	} else {
		startLine = max(1, startLine-extraAtTop)
	}

	// Avoid flickering caused by a partial first line, by being consistent with the last layout.
	if prev != nil && prev.ScrollHeight == v.ScrollHeight {
		if prev.ScrollTop > v.ScrollTop {
			startLine = min(startLine, prev.StartLine)
			topPadding = max(topPadding, prev.TopPaddingLineCount)
		}
		if prev.ScrollTop < v.ScrollTop {
			startLine = max(startLine, prev.StartLine)
			topPadding = min(topPadding, prev.TopPaddingLineCount)
		}
	}

	endLine := min(v.LineCount, startLine-topPadding+linesFitting-1)
	partial := (v.ScrollTop - v.ViewportStartOffset) / editorLineHeight
	sliderTopAligned := (float64(v.ViewportStartLine-startLine+topPadding) + partial) * lineHeight / pixelRatio
	return minimapRenderLayout{
		ScrollTop: v.ScrollTop, ScrollHeight: v.ScrollHeight, SliderNeeded: true, sliderRatio: ratio,
		SliderTop: sliderTopAligned, SliderHeight: sliderHeight, TopPaddingLineCount: topPadding, StartLine: startLine, EndLine: endLine,
	}
}
