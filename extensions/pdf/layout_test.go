package pdf

import (
	"math"
	"testing"

	gui "github.com/rm4n0s/glutter/ui"

	"github.com/rm4n0s/emetos/extensions/pdf/poppler"
)

func TestTheLayoutOfThePages(t *testing.T) {
	pages := []poppler.Page{{Width: 100, Height: 200}, {Width: 200, Height: 100}, {Width: 100, Height: 200}}
	l := layoutPages(pages, 2, 300)
	// the widest page is 400 px: the content is 424 wide, each page centered
	want := []pageRect{{12, 12, 200, 400}, {12, 424, 400, 200}, {112, 636, 200, 400}}
	want[0].x = (424 - 200) / 2.0
	want[2].x = want[0].x
	for i, r := range l.rects {
		if r != want[i] {
			t.Errorf("page %d at %+v, want %+v", i+1, r, want[i])
		}
	}
	if l.width != 424 || l.height != 1048 {
		t.Errorf("the content is %v x %v, want 424 x 1048", l.width, l.height)
	}
	// a view wider than the pages
	if wide := layoutPages(pages, 1, 1000); wide.width != 1000 || wide.rects[1].x != 400 {
		t.Errorf("in a wide view: width %v, page 2 at %v", wide.width, wide.rects[1].x)
	}
	tests := []struct {
		name          string
		top, bottom   float64
		first, last   int
		current, page int
	}{
		{"the top", 0, 300, 0, 0, 0, 0},
		{"three pages, the second shown most", 300, 700, 0, 2, 1, 0},
		{"in the gap above page 2", 413, 420, -1, -1, 1, 1},
		{"all", 0, 2000, 0, 2, 0, 0},
		{"beyond the end", 5000, 5100, -1, -1, 2, 2},
	}
	for _, tt := range tests {
		first, last := l.visible(tt.top, tt.bottom)
		if first != tt.first || last != tt.last {
			t.Errorf("%s: visible %d..%d, want %d..%d", tt.name, first, last, tt.first, tt.last)
		}
		if got := l.current(tt.top, tt.bottom); got != tt.current {
			t.Errorf("%s: current %d, want %d", tt.name, got, tt.current)
		}
		if got := l.pageAt(tt.top); got != tt.page {
			t.Errorf("%s: pageAt %d, want %d", tt.name, got, tt.page)
		}
	}
	// a place survives a new scale
	p := l.placeAt(424 + 50)
	if p.page != 1 || p.offset != 0.25 {
		t.Errorf("place %+v, want page 2 at a quarter", p)
	}
	l2 := layoutPages(pages, 1, 300)
	if got := l2.heightOf(p); got != l2.rects[1].y+25 {
		t.Errorf("at scale 1 the place is at %v, want %v", got, l2.rects[1].y+25)
	}
	if got := l2.heightOf(place{page: 2}); got != l2.rects[2].y-pageGap {
		t.Errorf("the top of a page is at %v, want the gap above it", got)
	}
	if (layout{}).pageAt(10) != -1 {
		t.Error("a page in an empty layout")
	}
}

func TestTheScaleOfTheZooms(t *testing.T) {
	pages := []poppler.Page{{Width: 612, Height: 792}, {Width: 842, Height: 595}}
	tests := []struct {
		name string
		z    zoom
		cur  int
		want float64
	}{
		{"Fit Width fits the widest page", zoom{}, 0, (800 - 24) / 842.0},
		{"Fit Page fits the current page", zoom{mode: zoomFitPage}, 0, (600 - 24) / 792.0},
		{"Fit Page of the landscape page", zoom{mode: zoomFitPage}, 1, (800 - 24) / 842.0},
		{"150%", zoom{mode: zoomLevel, level: 1.5}, 0, 2},
	}
	for _, tt := range tests {
		if got := scaleFor(tt.z, pages, tt.cur, 800, 600); math.Abs(got-tt.want) > 1e-9 {
			t.Errorf("%s: %v, want %v", tt.name, got, tt.want)
		}
	}
	if got := scaleFor(zoom{}, pages, 0, 10, 10); got != minScale {
		t.Errorf("a tiny view gives %v, want the least scale", got)
	}
}

func TestTheZoomSteps(t *testing.T) {
	tests := []struct {
		scale     float64
		in, out   float64
		inOK, oOK bool
	}{
		{scaleAt100, 1.25, 0.75, true, true},
		{0.9 * scaleAt100, 1, 0.75, true, true},
		{0.5 * scaleAt100, 0.75, 0, true, false},
		{4 * scaleAt100, 0, 3, false, true},
		{5 * scaleAt100, 0, 4, false, true},
	}
	for _, tt := range tests {
		in, ok := zoomIn(tt.scale)
		if ok != tt.inOK || (ok && in.level != tt.in) {
			t.Errorf("zoomIn(%v) = %v, %v", tt.scale, in, ok)
		}
		out, ok := zoomOut(tt.scale)
		if ok != tt.oOK || (ok && out.level != tt.out) {
			t.Errorf("zoomOut(%v) = %v, %v", tt.scale, out, ok)
		}
	}
	for _, z := range zoomOptions() {
		if got := parseZoom(z.String()); got != z {
			t.Errorf("%v does not survive its state (%q gives %v)", z, z.String(), got)
		}
	}
	labels := map[string]zoom{"Fit Width": {}, "Fit Page": {mode: zoomFitPage}, "125%": {mode: zoomLevel, level: 1.25}}
	for want, z := range labels {
		if z.Label() != want {
			t.Errorf("label %q, want %q", z.Label(), want)
		}
	}
	for _, s := range []string{"x", "1.1", "fitHeight"} {
		if parseZoom(s) != (zoom{}) {
			t.Errorf("%q is not Fit Width", s)
		}
	}
}

func TestPageNumbers(t *testing.T) {
	tests := []struct {
		in   string
		want int
		ok   bool
	}{{"3", 3, true}, {" 10 ", 10, true}, {"0", 0, false}, {"11", 0, false}, {"x", 0, false}, {"", 0, false}}
	for _, tt := range tests {
		if got, ok := pageNumber(tt.in, 10); got != tt.want || ok != tt.ok {
			t.Errorf("pageNumber(%q) = %d, %v", tt.in, got, ok)
		}
	}
}

func TestTheCache(t *testing.T) {
	img := func(w, h int) *gui.Image { return gui.NewImageFromRGBA(make([]byte, w*h*4), w, h) }
	c := newPageCache(1000)
	c.put(0, 72, img(10, 10)) // 400 bytes
	c.put(1, 72, img(10, 10))
	c.put(0, 144, img(5, 5)) // 100
	if c.size != 900 || len(c.entries) != 3 {
		t.Fatalf("size %d, %d entries", c.size, len(c.entries))
	}
	if d := c.best(0); d == nil || d.dpi != 144 {
		t.Errorf("best of page 1: %+v", d)
	}
	if c.get(0, 72.2) == nil { // the same picture within half a percent; it is now the most recent
		t.Error("page 1 at 72 dpi not found")
	}
	if c.get(0, 80) != nil {
		t.Error("a resolution that was never drawn")
	}
	c.put(2, 72, img(10, 10)) // over the bound: page 2 (the least recently used) goes
	if c.get(1, 72) != nil || c.size != 900 {
		t.Errorf("the least recently used page stayed: size %d", c.size)
	}
	c.put(2, 72, img(10, 10)) // the same again replaces it
	if c.size != 900 {
		t.Errorf("a replaced picture counts twice: %d", c.size)
	}
	c.put(3, 72, img(20, 20)) // alone over the bound, it stays
	if len(c.entries) != 1 || c.entries[0].page != 3 {
		t.Errorf("entries %+v", c.entries)
	}
	c.clear()
	if c.size != 0 || len(c.entries) != 0 {
		t.Error("clear left pages")
	}
}
