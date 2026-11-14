package mediapreview

import (
	"math"
	"strconv"
)

// The zoom of the image preview, as pure functions of the scale and the sizes (SPEC §10.37 "Image Preview").
//
// Ported from VS Code: extensions/media-preview/media/imagePreview.js

// ZoomLevels are the scales Zoom In and Zoom Out step through.
var ZoomLevels = []float64{0.1, 0.2, 0.3, 0.4, 0.5, 0.6, 0.7, 0.8, 0.9, 1, 1.5, 2, 3, 5, 7, 10, 15, 20}

// The bounds of the scale, the scale from which pixels are drawn as squares, the step of Ctrl+wheel, and the margin of a fitted image.
const (
	MinScale            = 0.1
	MaxScale            = 20
	PixelationThreshold = 3
	pinchFactor         = 0.075
	fitMargin           = 20
)

// ZoomPickScales are the scales of the zoom pick of the status bar, 0 standing for Whole Image (VS Code's zoomStatusBarEntry).
var ZoomPickScales = []float64{10, 5, 2, 1, 0.5, 0.2, 0}

// clampScale keeps a scale between MinScale and MaxScale.
func clampScale(s float64) float64 { return math.Min(math.Max(s, MinScale), MaxScale) }

// ZoomIn is the next level above the scale.
func ZoomIn(scale float64) float64 {
	for _, l := range ZoomLevels {
		if l > scale {
			return l
		}
	}
	return MaxScale
}

// ZoomOut is the next level below the scale.
func ZoomOut(scale float64) float64 {
	for i := len(ZoomLevels) - 1; i >= 0; i-- {
		if ZoomLevels[i] < scale {
			return ZoomLevels[i]
		}
	}
	return MinScale
}

// WheelZoom is the scale after one notch of Ctrl+wheel (or a pinch reported as one): down (dy > 0) zooms out.
func WheelZoom(scale, dy float64) float64 {
	delta := 1.0
	if dy < 0 {
		delta = -1
	}
	return clampScale(scale * (1 - delta*pinchFactor))
}

// FitScale is the scale of a fitted image ("Whole Image"): shrunk to the view less its margin when it is larger, else 1.
func FitScale(imgW, imgH, viewW, viewH float64) float64 {
	if imgW <= 0 || imgH <= 0 {
		return 1
	}
	s := math.Min((viewW-fitMargin)/imgW, (viewH-fitMargin)/imgH)
	if s <= 0 {
		return MinScale
	}
	return math.Min(1, s)
}

// ZoomLabel is the text of the zoom: Whole Image while fitted, else the percent.
func ZoomLabel(fit bool, scale float64) string {
	if fit {
		return "Whole Image"
	}
	return strconv.Itoa(int(math.Round(scale*100))) + "%"
}

// Place is where an image of the size, shown at the scale, is drawn in a view: centered on an axis where it is smaller than the view,
// else moved by the offset (how far the view is scrolled into it), which it clamps.
func Place(imgW, imgH, scale, viewW, viewH, offX, offY float64) (x, y, clampedX, clampedY float64) {
	place := func(size, view, off float64) (float64, float64) {
		if size <= view {
			return (view - size) / 2, 0
		}
		off = math.Min(math.Max(off, 0), size-view)
		return -off, off
	}
	x, clampedX = place(imgW*scale, viewW, offX)
	y, clampedY = place(imgH*scale, viewH, offY)
	return x, y, clampedX, clampedY
}

// Anchor is the offsets after a zoom from one scale to another that keeps the point (px, py) of the view over the same point of the
// image; on an axis where the image was centered, the point of the image is found from where it was drawn.
func Anchor(imgW, imgH, from, to, viewW, viewH, offX, offY, px, py float64) (float64, float64) {
	x, y, _, _ := Place(imgW, imgH, from, viewW, viewH, offX, offY)
	u, v := (px-x)/from, (py-y)/from // the point of the image under (px, py)
	_, _, nx, ny := Place(imgW, imgH, to, viewW, viewH, u*to-px, v*to-py)
	return nx, ny
}

// formatSize writes a size in bytes as VS Code's BinarySizeStatusBarEntry does: 512B, 245.30KB, 1.50MB.
//
// Ported from VS Code: extensions/media-preview/src/binarySizeStatusBarEntry.ts
func formatSize(size int64) string {
	const kb = 1024
	units := []string{"KB", "MB", "GB", "TB"}
	if size < kb {
		return strconv.FormatInt(size, 10) + "B"
	}
	v := float64(size) / kb
	i := 0
	for v >= kb && i < len(units)-1 {
		v /= kb
		i++
	}
	return strconv.FormatFloat(v, 'f', 2, 64) + units[i]
}
