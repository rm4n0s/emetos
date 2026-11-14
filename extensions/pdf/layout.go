package pdf

import (
	"math"
	"sort"
	"strconv"
	"strings"

	"github.com/rm4n0s/emetos/extensions/pdf/poppler"
)

// The layout of the pages (SPEC §10.37 "PDF"): one column, centered, with a gap between the pages and around them, scrolled
// continuously. Sizes are in logical pixels; a page's size in points times the scale.

// pageGap is the space between the pages and around them.
const pageGap = 12.0

// scaleAt100 is the scale of 100%: a point is 96/72 logical pixels, a page as it is at 96 dpi.
const scaleAt100 = 96.0 / 72.0

// The bounds of the scale, in pixels per point: a fit to a tiny or a huge view stays within them.
const (
	minScale = 0.05
	maxScale = 10.0
)

// ZoomLevels are the levels of the zoom pick and of Zoom In and Zoom Out, as fractions of 100%.
var ZoomLevels = []float64{0.5, 0.75, 1, 1.25, 1.5, 2, 3, 4}

// zoomMode is how the scale is chosen.
type zoomMode int

const (
	// zoomFitWidth makes the widest page as wide as the view (the default).
	zoomFitWidth zoomMode = iota
	// zoomFitPage makes the current page fit the view.
	zoomFitPage
	// zoomLevel is one of ZoomLevels.
	zoomLevel
)

// zoom is the zoom of a tab.
type zoom struct {
	mode  zoomMode
	level float64
}

// String is the zoom as the state of the tab saves it: "fitWidth", "fitPage" or the level ("1.5").
func (z zoom) String() string {
	switch z.mode {
	case zoomFitPage:
		return "fitPage"
	case zoomLevel:
		return strconv.FormatFloat(z.level, 'f', -1, 64)
	}
	return "fitWidth"
}

// parseZoom reads a zoom that String wrote; anything else is Fit Width.
func parseZoom(s string) zoom {
	switch s {
	case "fitPage":
		return zoom{mode: zoomFitPage}
	case "", "fitWidth":
		return zoom{}
	}
	f, err := strconv.ParseFloat(s, 64)
	if err != nil {
		return zoom{}
	}
	for _, l := range ZoomLevels {
		if math.Abs(l-f) < 1e-9 {
			return zoom{mode: zoomLevel, level: l}
		}
	}
	return zoom{}
}

// Label is the zoom as the pick shows it: "Fit Width", "Fit Page", "150%".
func (z zoom) Label() string {
	switch z.mode {
	case zoomFitPage:
		return "Fit Page"
	case zoomLevel:
		return strconv.Itoa(int(math.Round(z.level*100))) + "%"
	}
	return "Fit Width"
}

// zoomOptions are the zooms of the pick, in its order.
func zoomOptions() []zoom {
	out := []zoom{{mode: zoomFitWidth}, {mode: zoomFitPage}}
	for _, l := range ZoomLevels {
		out = append(out, zoom{mode: zoomLevel, level: l})
	}
	return out
}

// zoomIn is the first level above a scale; false at the last one.
func zoomIn(scale float64) (zoom, bool) {
	level := scale / scaleAt100
	for _, l := range ZoomLevels {
		if l > level*1.001 {
			return zoom{mode: zoomLevel, level: l}, true
		}
	}
	return zoom{}, false
}

// zoomOut is the first level below a scale; false at the first one.
func zoomOut(scale float64) (zoom, bool) {
	level := scale / scaleAt100
	for i := len(ZoomLevels) - 1; i >= 0; i-- {
		if ZoomLevels[i] < level*0.999 {
			return zoom{mode: zoomLevel, level: ZoomLevels[i]}, true
		}
	}
	return zoom{}, false
}

// scaleFor is the scale of a zoom for pages in a view of a size; cur is the current page, which Fit Page fits.
func scaleFor(z zoom, pages []poppler.Page, cur int, w, h float64) float64 {
	var s float64
	switch z.mode {
	case zoomLevel:
		s = z.level * scaleAt100
	case zoomFitPage:
		if cur < 0 || cur >= len(pages) {
			return scaleAt100
		}
		p := pages[cur]
		s = min((w-2*pageGap)/p.Width, (h-2*pageGap)/p.Height)
	default:
		widest := 0.0
		for _, p := range pages {
			widest = max(widest, p.Width)
		}
		if widest == 0 {
			return scaleAt100
		}
		s = (w - 2*pageGap) / widest
	}
	return min(max(s, minScale), maxScale)
}

// pageRect is where a page is in the content.
type pageRect struct {
	x, y, w, h float64
}

// layout is where the pages are at a scale, and the size of the content.
type layout struct {
	rects         []pageRect
	width, height float64
}

// layoutPages lays out the pages at a scale in a view of a width: the content is at least as wide as the view, and each page is
// centered in it.
func layoutPages(pages []poppler.Page, scale, viewWidth float64) layout {
	widest := 0.0
	for _, p := range pages {
		widest = max(widest, p.Width*scale)
	}
	l := layout{rects: make([]pageRect, len(pages)), width: max(viewWidth, widest+2*pageGap)}
	y := pageGap
	for i, p := range pages {
		w, h := p.Width*scale, p.Height*scale
		l.rects[i] = pageRect{x: (l.width - w) / 2, y: y, w: w, h: h}
		y += h + pageGap
	}
	l.height = y
	return l
}

// pageAt is the page whose box, or the gap above it, holds the height y; the first or the last page beyond the content.
func (l layout) pageAt(y float64) int {
	if len(l.rects) == 0 {
		return -1
	}
	i := sort.Search(len(l.rects), func(i int) bool { return l.rects[i].y+l.rects[i].h > y })
	return min(i, len(l.rects)-1)
}

// visible is the first and the last page that the band from top to bottom shows; -1, -1 when it shows none.
func (l layout) visible(top, bottom float64) (int, int) {
	first := sort.Search(len(l.rects), func(i int) bool { return l.rects[i].y+l.rects[i].h > top })
	last := sort.Search(len(l.rects), func(i int) bool { return l.rects[i].y >= bottom }) - 1
	if first >= len(l.rects) || last < first {
		return -1, -1
	}
	return first, last
}

// current is the page that the band from top to bottom shows the most of (the first of two that it shows as much of); the page at top
// when it shows none.
func (l layout) current(top, bottom float64) int {
	first, last := l.visible(top, bottom)
	if first < 0 {
		return l.pageAt(top)
	}
	best, most := first, -1.0
	for i := first; i <= last; i++ {
		r := l.rects[i]
		if shown := min(r.y+r.h, bottom) - max(r.y, top); shown > most+0.5 {
			best, most = i, shown
		}
	}
	return best
}

// place is a place in the document that stays when the layout changes: a page, and how far down it (a fraction of its height) the
// top of the view is.
type place struct {
	page   int
	offset float64
}

// placeAt is the place at a height of the content.
func (l layout) placeAt(y float64) place {
	i := l.pageAt(y)
	if i < 0 {
		return place{}
	}
	r := l.rects[i]
	return place{page: i, offset: min(max((y-r.y)/r.h, 0), 1)}
}

// heightOf is the height of the content at a place: the gap above a page when it is at its top.
func (l layout) heightOf(p place) float64 {
	if len(l.rects) == 0 {
		return 0
	}
	i := min(max(p.page, 0), len(l.rects)-1)
	r := l.rects[i]
	if p.offset <= 0 {
		return r.y - pageGap
	}
	return r.y + p.offset*r.h
}

// pageNumber reads what was typed in the page box: a page number from 1 to n; false otherwise.
func pageNumber(s string, n int) (int, bool) {
	v, err := strconv.Atoi(strings.TrimSpace(s))
	if err != nil || v < 1 || v > n {
		return 0, false
	}
	return v, true
}
