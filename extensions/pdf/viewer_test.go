package pdf

import (
	"context"
	"encoding/json"
	"image"
	"image/color"
	"math"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/rm4n0s/errors"
	"github.com/rm4n0s/glutter/gluttertest"
	"github.com/rm4n0s/glutter/services"
	gui "github.com/rm4n0s/glutter/ui"
	"github.com/rm4n0s/glutter/widgets"

	"github.com/rm4n0s/emetos/extension"
	"github.com/rm4n0s/emetos/extension/ui/uitest"
	"github.com/rm4n0s/emetos/extensions/internal/fakeapi"
	"github.com/rm4n0s/emetos/extensions/pdf/poppler"
)

// rig is the PDF Viewer activated over the fake API with a fake Poppler: pages of a size, an optional password, and pages drawn as
// pictures of their size (a light page with dark lines of "text", the lines' gray telling the page) that a test can hold back.
type rig struct {
	t   *testing.T
	api *fakeapi.API
	x   *Extension
	dir string

	mu       sync.Mutex
	pages    []poppler.Page
	password string
	findErr  error
	infoErr  error
	infos    int
	renders  []renderCall
	active   int
	most     int
	// gate holds the drawing back while it is not nil: a page is drawn when the test sends on it.
	gate chan struct{}
	// failPage is a page (from 1) that cannot be drawn.
	failPage int
}

// renderCall is a page asked of the fake pdftoppm.
type renderCall struct {
	page     int
	dpi      float64
	password string
}

// letter is a US Letter page in points.
var letter = poppler.Page{Width: 612, Height: 792}

func newRig(t *testing.T, pages int) *rig {
	t.Helper()
	dir := t.TempDir()
	r := &rig{t: t, dir: dir}
	for range pages {
		r.pages = append(r.pages, letter)
	}
	x := New().(*Extension)
	x.find = func(context.Context, string) (poppler.Programs, error) {
		r.mu.Lock()
		defer r.mu.Unlock()
		return poppler.Programs{Dir: "/fake", Version: "24.02.0"}, r.findErr
	}
	x.info = func(_ context.Context, _ poppler.Programs, _, password string) (poppler.Document, error) {
		r.mu.Lock()
		defer r.mu.Unlock()
		r.infos++
		switch {
		case r.infoErr != nil:
			return poppler.Document{}, r.infoErr
		case r.password != "" && password == "":
			return poppler.Document{}, errors.New("PDFPasswordNeeded", "This document is protected by a password.")
		case r.password != "" && password != r.password:
			return poppler.Document{}, errors.New("PDFPasswordWrong", "The password is not correct.")
		}
		return poppler.Document{Pages: slices.Clone(r.pages), Encrypted: r.password != ""}, nil
	}
	x.render = r.render
	r.x = x
	r.api = fakeapi.New(t, x.Manifest(), extension.ScopeInfo{Kind: extension.ScopeWorkspace, Name: "project", Folder: dir, ID: dir})
	if err := x.Activate(t.Context(), r.api); err != nil {
		t.Fatal(err)
	}
	return r
}

func (r *rig) render(ctx context.Context, _ poppler.Programs, _, password string, page int, dpi float64) (image.Image, error) {
	r.mu.Lock()
	r.renders = append(r.renders, renderCall{page: page, dpi: dpi, password: password})
	r.active++
	r.most = max(r.most, r.active)
	gate, size, fail := r.gate, r.pages[page-1], r.failPage == page
	r.mu.Unlock()
	defer func() {
		r.mu.Lock()
		r.active--
		r.mu.Unlock()
	}()
	if gate != nil {
		select {
		case <-gate:
		case <-ctx.Done():
			return nil, errors.NewErr("PopplerStopped", ctx.Err())
		}
	}
	if fail {
		return nil, errors.New("PDFRenderFailed", "pdftoppm failed", "message", "Illegal character in content stream")
	}
	return pageImage(size, dpi, page), nil
}

// pageImage is a page as the fake draws it: a light page with dark lines of "text", the lines' gray telling the page.
func pageImage(p poppler.Page, dpi float64, page int) image.Image {
	w, h := int(math.Ceil(p.Width*dpi/72)), int(math.Ceil(p.Height*dpi/72))
	img := image.NewRGBA(image.Rect(0, 0, w, h))
	line := color.RGBA{R: uint8(20 * page), G: uint8(20 * page), B: 90, A: 255}
	for y := range h {
		for x := range w {
			c := color.RGBA{R: 250, G: 250, B: 250, A: 255}
			row := y * 48 / h
			inText := x > w/8 && x < w*7/8-(row%4)*w/16 && row >= 4 && row < 44
			if inText && row%2 == 0 {
				c = line
			}
			img.SetRGBA(x, y, c)
		}
	}
	return img
}

// calls are the pages asked of pdftoppm so far.
func (r *rig) calls() []renderCall {
	r.mu.Lock()
	defer r.mu.Unlock()
	return slices.Clone(r.renders)
}

// release lets n held-back pages be drawn.
func (r *rig) release(n int) {
	for range n {
		r.gate <- struct{}{}
	}
}

// write writes a file of the folder and gives its path.
func (r *rig) write(name string, data []byte) string {
	r.t.Helper()
	p := filepath.Join(r.dir, name)
	if err := os.WriteFile(p, data, 0o644); err != nil {
		r.t.Fatal(err)
	}
	return p
}

// open opens a PDF with the state of a tab that comes back, shows it in a view of a size, and waits until its pages are laid out (or
// it shows something else).
func (r *rig) open(state json.RawMessage, size gui.Size) (*fakeapi.FakeFileEditor, *viewer, *gluttertest.Tester) {
	r.t.Helper()
	fe, err := r.api.OpenFileEditor(ViewerID, r.write("doc.pdf", []byte("%PDF-1.4 a document")), state)
	if err != nil {
		r.t.Fatal(err)
	}
	v := r.x.viewers[len(r.x.viewers)-1]
	fe.SetActive(true)
	ts := uitest.Pump(r.t, uitest.Themes()[0], size, widgets.NewBuilder(fe.Content.Build))
	r.settle(ts, "the document", func() bool { return !v.loading && (len(v.lay.rects) > 0 || v.problem != nil || v.locked) })
	return fe, v, ts
}

// settle pumps the fake API and the tester until cond holds.
func (r *rig) settle(ts *gluttertest.Tester, what string, cond func() bool) {
	r.t.Helper()
	deadline := time.Now().Add(10 * time.Second)
	for {
		r.api.Pump()
		ts.Pump(time.Millisecond)
		if cond() {
			return
		}
		if time.Now().After(deadline) {
			r.t.Fatalf("timed out waiting for %s", what)
		}
		time.Sleep(time.Millisecond)
	}
}

// drawnAll waits until no page is being drawn and the pages in view are drawn at their resolution.
func (r *rig) drawnAll(ts *gluttertest.Tester, v *viewer) {
	r.t.Helper()
	r.settle(ts, "the pages in view", func() bool {
		if len(v.jobs) > 0 || v.settling {
			return false
		}
		for _, p := range v.wanted() {
			if _, failed := v.failed[p]; !failed && v.cache.get(p, v.dpiFor(p)) == nil {
				return false
			}
		}
		return true
	})
}

// pagesOf are the pages (from 1) of the calls, sorted: two pages drawn at once may be asked in either order.
func pagesOf(calls []renderCall) []int {
	var out []int
	for _, c := range calls {
		out = append(out, c.page)
	}
	slices.Sort(out)
	return out
}

var viewSize = gui.Size{Width: 480, Height: 360}

func TestOnlyThePagesInViewAreDrawn(t *testing.T) {
	r := newRig(t, 20)
	_, v, ts := r.open(nil, viewSize)
	r.drawnAll(ts, v)
	// Fit Width: a page is 456 px wide and 590 tall, so the view shows page 1 and the next one is drawn too
	if got := pagesOf(r.calls()); !slices.Equal(got, []int{1, 2}) {
		t.Errorf("drawn %v, want [1 2]", got)
	}
	if want := (480.0 - 2*pageGap) / 612; math.Abs(v.scale-want) > 1e-9 {
		t.Errorf("the scale is %v, want %v (Fit Width)", v.scale, want)
	}
	// scrolling to page 10 draws pages 9 to 11, and none between
	v.goToPage(9)
	r.drawnAll(ts, v)
	if got := pagesOf(r.calls()[2:]); !slices.Equal(got, []int{9, 10, 11}) {
		t.Errorf("drawn %v, want [9 10 11]", got)
	}
	if got := v.pageBox.Value(); got != "10" {
		t.Errorf("the page box says %q", got)
	}
}

func TestAtMostTwoPagesAreDrawnAtOnceAndPagesThatLeaveAreStopped(t *testing.T) {
	r := newRig(t, 20)
	r.gate = make(chan struct{})
	_, v, ts := r.open(nil, gui.Size{Width: 480, Height: 1400}) // three pages in view
	r.settle(ts, "two pages being drawn", func() bool { return len(v.jobs) == 2 })
	time.Sleep(20 * time.Millisecond)
	r.api.Pump()
	if n := len(r.calls()); n != 2 {
		t.Fatalf("%d pages asked at once, want 2", n)
	}
	// the view moves away: the two are stopped, and the pages of the new place are drawn
	v.goToPage(15)
	r.settle(ts, "the new pages", func() bool { return len(v.jobs) == 2 && v.jobs[15] != nil })
	r.mu.Lock()
	most := r.most
	r.mu.Unlock()
	if most > 2+2 { // the stopped ones may still be ending
		t.Errorf("%d drawn at once", most)
	}
	go r.release(10)
	r.drawnAll(ts, v)
	if v.cache.get(0, v.dpiFor(0)) != nil {
		t.Error("a page that left the view before it was drawn is in the cache")
	}
}

func TestTheCacheKeepsItsBound(t *testing.T) {
	r := newRig(t, 30)
	_, v, ts := r.open(nil, viewSize)
	r.drawnAll(ts, v)
	one := v.cache.size / len(v.cache.entries)
	v.cache.limit = 3 * one // three pages
	for p := range 30 {
		v.goToPage(p)
		r.drawnAll(ts, v)
		if v.cache.size > v.cache.limit {
			t.Fatalf("at page %d the cache holds %d bytes, over %d", p+1, v.cache.size, v.cache.limit)
		}
	}
	if got := len(v.cache.entries); got != 3 {
		t.Errorf("%d pages kept, want 3", got)
	}
}

func TestZoomKeepsThePageInViewAndWaitsBeforeDrawing(t *testing.T) {
	r := newRig(t, 20)
	_, v, ts := r.open(nil, viewSize)
	v.goToPage(6)
	r.drawnAll(ts, v)
	before := len(r.calls())
	if err := r.api.Run(CommandZoomIn, nil); err != nil {
		t.Fatal(err)
	}
	// Fit Width is 56% here (a page 456 px wide)
	if v.zoom != (zoom{mode: zoomLevel, level: 0.75}) {
		t.Errorf("Zoom In from Fit Width (%.0f%%) gives %v, want 75%%", 100*(480.0-2*pageGap)/612/scaleAt100, v.zoom)
	}
	if got := v.currentPage(); got != 6 {
		t.Errorf("the current page is %d, want 6", got+1)
	}
	// nothing is drawn during the 200 ms after the step; the old pictures are scaled meanwhile
	ts.Pump(100 * time.Millisecond)
	r.api.Pump()
	if n := len(r.calls()); n != before {
		t.Errorf("%d pages drawn before the zoom settled", n-before)
	}
	if v.cache.get(6, v.dpiFor(6)) != nil || v.cache.best(6) == nil {
		t.Error("the page is not shown scaled from its old picture")
	}
	ts.Pump(150 * time.Millisecond)
	r.drawnAll(ts, v)
	if !slices.Contains(r.calls()[before:], renderCall{page: 7, dpi: 72}) {
		t.Errorf("drawn %+v, want page 7 at 72 dpi (75%%)", r.calls()[before:])
	}
	// the pick and Zoom Out
	if err := r.api.Run(CommandZoomOut, nil); err != nil {
		t.Fatal(err)
	}
	if v.zoom.level != 0.5 || v.currentPage() != 6 {
		t.Errorf("Zoom Out gives %v at page %d", v.zoom, v.currentPage()+1)
	}
	v.setZoom(zoom{mode: zoomFitPage})
	// the pages are 328 px tall, under the toolbar
	if want := (360.0 - toolbarHeight - 2*pageGap) / 792; math.Abs(v.scale-want) > 1e-9 || v.currentPage() != 6 {
		t.Errorf("Fit Page: scale %v (want %v) at page %d", v.scale, want, v.currentPage()+1)
	}
}

func TestTheLargestPicturesStayWithinTheSizeOfAPicture(t *testing.T) {
	r := newRig(t, 2)
	_, v, ts := r.open(nil, viewSize)
	v.setZoom(zoom{mode: zoomLevel, level: 4})
	ts.Pump(250 * time.Millisecond)
	r.drawnAll(ts, v)
	last := r.calls()[len(r.calls())-1]
	if side := 792 * last.dpi / 72; side > 4096.001 {
		t.Errorf("drawn at %v dpi: %v pixels tall", last.dpi, side)
	}
}

func TestThePageBoxAndTheButtons(t *testing.T) {
	r := newRig(t, 12)
	_, v, ts := r.open(nil, viewSize)
	tests := []struct {
		name string
		do   func()
		want int
	}{
		{"a number", func() { v.submitPage("8") }, 7},
		{"spaces around it", func() { v.submitPage(" 3 ") }, 2},
		{"not a number", func() { v.submitPage("x") }, 2},
		{"beyond the last", func() { v.submitPage("13") }, 2},
		{"Next Page", func() { mustRun(t, r, CommandNextPage) }, 3},
		{"Previous Page", func() { mustRun(t, r, CommandPreviousPage) }, 2},
		{"the last page", func() { v.submitPage("12") }, 11},
		{"Next Page at the last", func() { mustRun(t, r, CommandNextPage) }, 11},
	}
	for _, tt := range tests {
		tt.do()
		ts.Pump(time.Millisecond)
		if got := v.currentPage(); got != tt.want {
			t.Errorf("%s: page %d, want %d", tt.name, got+1, tt.want+1)
		}
		if got := v.pageBox.Value(); got != strconv.Itoa(tt.want+1) {
			t.Errorf("%s: the page box says %q", tt.name, got)
		}
	}
	// Go to Page gives the box the focus
	mustRun(t, r, CommandGoToPage)
	ts.Pump(time.Millisecond)
	if !v.pageBox.HasFocus() {
		t.Error("Go to Page did not focus the page box")
	}
	if !ts.Exists(gluttertest.ByText("/ 12")) {
		t.Error("the toolbar does not say / 12")
	}
}

func mustRun(t *testing.T, r *rig, id string) {
	t.Helper()
	if err := r.api.Run(id, nil); err != nil {
		t.Fatal(err)
	}
}

func TestTheKeysScroll(t *testing.T) {
	r := newRig(t, 10)
	_, v, ts := r.open(nil, viewSize)
	tests := []struct {
		key  services.LogicalKeyboardKey
		want func() float64
	}{
		{services.LogicalKeyboardKeyPageDown, func() float64 { return v.viewH - pageOverlap }},
		{services.LogicalKeyboardKeyArrowDown, func() float64 { return v.viewH - pageOverlap + lineStep }},
		{services.LogicalKeyboardKeyArrowUp, func() float64 { return v.viewH - pageOverlap }},
		{services.LogicalKeyboardKeyPageUp, func() float64 { return 0 }},
		{services.LogicalKeyboardKeyEnd, func() float64 { return v.lay.height - v.viewH }},
		{services.LogicalKeyboardKeyHome, func() float64 { return 0 }},
	}
	for _, tt := range tests {
		ts.SendKeyEvent(tt.key)
		ts.Pump(time.Millisecond)
		if math.Abs(v.top-tt.want()) > 0.01 {
			t.Errorf("%v: top %v, want %v", tt.key, v.top, tt.want())
		}
	}
}

func TestAPasswordIsAskedAndGivenToEveryProgram(t *testing.T) {
	r := newRig(t, 3)
	r.password = "secret"
	_, v, ts := r.open(nil, viewSize)
	if !v.locked || v.wrongPassword {
		t.Fatalf("locked %v, wrong %v", v.locked, v.wrongPassword)
	}
	if !ts.Exists(gluttertest.ByText("This document is protected by a password. Enter the password to open it.")) {
		t.Error("no password box")
	}
	v.submitPassword("guess")
	r.settle(ts, "the answer", func() bool { return !v.loading })
	if !v.locked || !v.wrongPassword {
		t.Errorf("a wrong password: locked %v, wrong %v", v.locked, v.wrongPassword)
	}
	if !ts.Exists(gluttertest.ByText("The password is not correct.")) {
		t.Error("the wrong password is not said")
	}
	v.submitPassword("secret")
	r.settle(ts, "the pages", func() bool { return !v.loading && len(v.lay.rects) == 3 })
	r.drawnAll(ts, v)
	for _, c := range r.calls() {
		if c.password != "secret" {
			t.Errorf("page %d drawn with the password %q", c.page, c.password)
		}
	}
	// the password is never saved
	if st, err := v.saveState(); err != nil || strings.Contains(mustJSON(t, st), "secret") {
		t.Errorf("the state %v (%v) holds the password", st, err)
	}
}

func mustJSON(t *testing.T, v any) string {
	data, err := json.Marshal(v)
	if err != nil {
		t.Fatal(err)
	}
	return string(data)
}

func TestTheMessagesOfADocumentThatCannotBeShown(t *testing.T) {
	tests := []struct {
		name     string
		find     error
		info     error
		text     string
		buttons  []string
		missings bool
	}{
		{"no Poppler", errors.New("PopplerNotFound", "pdfinfo was not found in PATH, /opt/homebrew/bin or /usr/local/bin"), nil,
			"Showing PDF files needs Poppler. pdfinfo was not found in PATH, /opt/homebrew/bin or /usr/local/bin.",
			[]string{"Set Path...", "Instructions", "Check Again"}, true},
		{"a damaged file", nil, errors.New("PDFDamaged", "exit status 1", "message", "Couldn't read xref table"),
			"This file cannot be opened as a PDF: Couldn't read xref table.", []string{"Check Again"}, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			r := newRig(t, 2)
			r.findErr, r.infoErr = tt.find, tt.info
			_, v, ts := r.open(nil, viewSize)
			if v.problem == nil {
				t.Fatal("no problem")
			}
			if !ts.Exists(gluttertest.ByText(tt.text)) {
				t.Errorf("the message %q is not shown", tt.text)
			}
			for _, b := range tt.buttons {
				if !ts.Exists(gluttertest.ByText(b)) {
					t.Errorf("no button %q", b)
				}
			}
			// Check Again reads the file again, and a fixed problem shows the pages
			r.mu.Lock()
			r.findErr, r.infoErr = nil, nil
			r.mu.Unlock()
			ts.Tap(gluttertest.ByText("Check Again"))
			r.settle(ts, "the pages", func() bool { return !v.loading && len(v.lay.rects) == 2 })
		})
	}
}

func TestAPageThatCannotBeDrawnSaysSo(t *testing.T) {
	r := newRig(t, 2)
	r.failPage = 1
	_, v, ts := r.open(nil, viewSize)
	r.settle(ts, "the failure", func() bool { _, ok := v.failed[0]; return ok })
	ts.Pump(time.Millisecond)
	if !ts.Exists(gluttertest.ByText("This page cannot be drawn: Illegal character in content stream")) {
		t.Error("the page does not say why it is not drawn")
	}
	before := len(r.calls())
	v.scrollTo(0, 1)
	r.drawnAll(ts, v)
	for _, c := range r.calls()[before:] {
		if c.page == 1 {
			t.Error("a page that failed is drawn again")
		}
	}
}

func TestAReloadKeepsThePage(t *testing.T) {
	r := newRig(t, 10)
	fe, v, ts := r.open(nil, viewSize)
	v.goToPage(5)
	r.drawnAll(ts, v)
	infos := r.infos
	// the watcher's first event, with the file as it was: nothing is read
	fe.Change(false)
	r.api.Pump()
	if r.infos != infos || v.loading {
		t.Fatalf("a change that changes nothing read the file (%d reads)", r.infos-infos)
	}
	r.write("doc.pdf", []byte("%PDF-1.4 a document, longer now"))
	r.mu.Lock()
	r.pages = append(r.pages, letter, letter)
	r.mu.Unlock()
	fe.Change(false)
	r.settle(ts, "the reload", func() bool { return !v.loading && len(v.lay.rects) == 12 })
	if got := v.currentPage(); got != 5 {
		t.Errorf("after the reload the page is %d, want 6", got+1)
	}
	if !ts.Exists(gluttertest.ByText("/ 12")) {
		t.Error("the toolbar does not count the new pages")
	}
	fe.Change(true)
	r.api.Pump()
	ts.Pump(time.Millisecond)
	if !ts.Exists(gluttertest.ByText("The file was deleted.")) {
		t.Error("a deleted file does not say so")
	}
}

func TestTheStateIsSavedAndRestored(t *testing.T) {
	r := newRig(t, 10)
	fe, v, ts := r.open(nil, viewSize)
	v.setZoom(zoom{mode: zoomLevel, level: 1.5})
	v.goToPage(4)
	ts.Pump(time.Millisecond)
	data, err := fe.SaveState()
	if err != nil {
		t.Fatal(err)
	}
	var st viewerState
	if err := json.Unmarshal(data, &st); err != nil {
		t.Fatal(err)
	}
	if st.Page != 5 || st.Zoom != "1.5" {
		t.Errorf("saved %+v", st)
	}
	fe.Close()
	_, v2, ts2 := r.open(data, viewSize)
	ts2.Pump(time.Millisecond)
	if v2.zoom != (zoom{mode: zoomLevel, level: 1.5}) || v2.currentPage() != 4 {
		t.Errorf("restored %v at page %d", v2.zoom, v2.currentPage()+1)
	}
	// a state that cannot be read is Fit Width at the first page
	_, v3, _ := r.open(json.RawMessage(`{"page": "x"}`), viewSize)
	if v3.zoom != (zoom{}) || v3.currentPage() != 0 {
		t.Errorf("a broken state gives %v at page %d", v3.zoom, v3.currentPage()+1)
	}
}

func TestClosingStopsTheDrawing(t *testing.T) {
	r := newRig(t, 5)
	r.gate = make(chan struct{})
	fe, v, ts := r.open(nil, viewSize)
	r.settle(ts, "the drawing", func() bool { return len(v.jobs) == 2 })
	fe.Close()
	deadline := time.Now().Add(5 * time.Second)
	for {
		r.mu.Lock()
		active := r.active
		r.mu.Unlock()
		if active == 0 {
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("%d pages still drawn after the tab closed", active)
		}
		time.Sleep(time.Millisecond)
	}
	if len(r.x.viewers) != 0 {
		t.Error("the tab is still known")
	}
}

func TestCtrlAndTheWheelZoom(t *testing.T) {
	r := newRig(t, 5)
	_, v, ts := r.open(nil, viewSize)
	ts.SendKeyDownEvent(services.LogicalKeyboardKeyControlLeft)
	ts.ScrollAt(gui.Offset{Dx: 200, Dy: 200}, gui.Offset{Dy: -20})
	ts.SendKeyUpEvent(services.LogicalKeyboardKeyControlLeft)
	ts.Pump(time.Millisecond)
	if v.zoom != (zoom{mode: zoomLevel, level: 0.75}) {
		t.Errorf("Ctrl and the wheel up gives %v, want 75%%", v.zoom)
	}
	before := v.top
	ts.ScrollAt(gui.Offset{Dx: 200, Dy: 200}, gui.Offset{Dy: 100})
	for range 30 {
		ts.Pump(20 * time.Millisecond)
	}
	if v.top <= before {
		t.Errorf("the wheel did not scroll: top %v", v.top)
	}
}

func TestGoldens(t *testing.T) {
	for _, th := range uitest.Themes() {
		t.Run("fit width "+th.Name, func(t *testing.T) {
			r := newRig(t, 3)
			fe, err := r.api.OpenFileEditor(ViewerID, r.write("doc.pdf", []byte("%PDF")), nil)
			if err != nil {
				t.Fatal(err)
			}
			v := r.x.viewers[0]
			ts := uitest.Pump(t, th, viewSize, widgets.NewBuilder(fe.Content.Build))
			r.settle(ts, "the pages", func() bool { return len(v.lay.rects) == 3 })
			r.drawnAll(ts, v)
			ts.Pump(time.Millisecond)
			uitest.MatchesGolden(ts, "pdf_fit_width_"+th.Name)
		})
		t.Run("password "+th.Name, func(t *testing.T) {
			r := newRig(t, 3)
			r.password = "secret"
			fe, err := r.api.OpenFileEditor(ViewerID, r.write("doc.pdf", []byte("%PDF")), nil)
			if err != nil {
				t.Fatal(err)
			}
			v := r.x.viewers[0]
			ts := uitest.Pump(t, th, viewSize, widgets.NewBuilder(fe.Content.Build))
			r.settle(ts, "the password box", func() bool { return v.locked })
			v.submitPassword("guess")
			r.settle(ts, "the answer", func() bool { return !v.loading })
			ts.Pump(time.Millisecond)
			uitest.MatchesGolden(ts, "pdf_password_"+th.Name)
		})
	}
}
