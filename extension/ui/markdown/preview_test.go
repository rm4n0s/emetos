package markdown

import (
	"fmt"
	"image"
	"image/color"
	"image/png"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/rm4n0s/glutter/foundation"
	"github.com/rm4n0s/glutter/gluttertest"
	gui "github.com/rm4n0s/glutter/ui"
	"github.com/rm4n0s/glutter/widgets"

	"github.com/rm4n0s/emetos/extension/ui/uitest"
)

// The preview of a document is a list that builds the blocks near the viewport (step 17b.2).

var previewSize = gui.Size{Width: 500, Height: 400}

// paragraphs is a document of n numbered paragraphs, each in four lines of the source (a heading, a blank, the text, a blank).
func paragraphs(n int) string {
	var b strings.Builder
	for i := range n {
		fmt.Fprintf(&b, "## Section %d\n\nParagraph %d of the document, with a few words to fill the line and take a second one.\n\n", i, i)
	}
	return b.String()
}

// pumpPreview shows a preview and waits for its text to be parsed (a big text is parsed on a goroutine).
func pumpPreview(t testing.TB, src string, opts Options) (*gluttertest.Tester, *Controller) {
	t.Helper()
	ctl := opts.Controller
	if ctl == nil {
		ctl = NewController()
		opts.Controller = ctl
	}
	ts := uitest.Pump(t, uitest.Themes()[0], previewSize, NewView(src, opts))
	waitParsed(t, ts, ctl)
	return ts, ctl
}

// waitParsed pumps until the preview has its blocks.
func waitParsed(t testing.TB, ts *gluttertest.Tester, ctl *Controller) {
	t.Helper()
	deadline := time.Now().Add(20 * time.Second)
	for {
		ts.Pump()
		if st, ok := ctl.Stats(); ok && st.Blocks > 0 {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("the preview has no blocks")
		}
		time.Sleep(time.Millisecond)
	}
	for range 3 {
		ts.Pump()
	}
}

// stats are the Stats of the preview of a controller.
func stats(t testing.TB, ctl *Controller) Stats {
	t.Helper()
	st, ok := ctl.Stats()
	if !ok {
		t.Fatal("the controller moves no view")
	}
	return st
}

func TestOnlyTheBlocksNearTheViewportAreBuilt(t *testing.T) {
	const sections = 1500
	ts, ctl := pumpPreview(t, paragraphs(sections), Options{})
	st := stats(t, ctl)
	if st.Blocks != 2*sections {
		t.Fatalf("the preview has %d blocks, want %d", st.Blocks, 2*sections)
	}
	// the viewport and 250 px around it: 400 + 250 pixels of headings (49 px) and paragraphs (45 px)
	if st.Built < 4 || st.Built > 30 {
		t.Errorf("%d blocks are built, want a few of the %d", st.Built, st.Blocks)
	}
	if n := ts.Count(gluttertest.ByType[*widgets.RichText]()); n != st.Built {
		t.Errorf("%d texts are mounted for %d built blocks", n, st.Built)
	}
	if !ts.Exists(textOf("Section 0")) || ts.Exists(textOf("Section 1000")) {
		t.Error("the first block is not on screen, or a far one is")
	}
}

func TestTheWheelScrollsAndKeepsTheBuiltBlocksFew(t *testing.T) {
	ts, ctl := pumpPreview(t, paragraphs(1500), Options{})
	at := gui.Offset{Dx: 200, Dy: 200}
	var tops []int
	for range 40 {
		ts.ScrollAt(at, gui.Offset{Dx: 0, Dy: 530})
		for range 12 { // a notch scrolls smoothly over 125 ms
			ts.Pump(16 * time.Millisecond)
		}
		tops = append(tops, ctl.TopLine())
	}
	if tops[len(tops)-1] <= tops[0] || tops[len(tops)-1] < 100 {
		t.Errorf("the top line went %v", tops)
	}
	st := stats(t, ctl)
	if st.Built > 30 || st.ScrollTop <= 0 {
		t.Errorf("after the wheel: %+v", st)
	}
	// the blocks that were passed were measured, the others are estimates
	if st.Measured < 20 || st.Measured >= st.Blocks/2 {
		t.Errorf("%d blocks are measured", st.Measured)
	}
}

// textOf finds the text of blocks (their rich text) that has a substring.
func textOf(substring string) gluttertest.Finder {
	return gluttertest.ByTextContaining(substring, gluttertest.FindOpts{FindRichText: true})
}

// plainText finds a plain text (the alt text of an image).
func plainText(text string) gluttertest.Finder { return gluttertest.ByText(text) }

// topOf is the distance of the text of a block from the top of the preview, in logical pixels.
func topOf(ts *gluttertest.Tester, text string) (float64, bool) {
	f := textOf(text)
	if !ts.Exists(f) {
		return 0, false
	}
	return ts.TopLeft(f).Dy, true
}

func TestScrollToLineAcrossEstimatedAndMeasuredHeights(t *testing.T) {
	const sections = 1500
	ts, ctl := pumpPreview(t, paragraphs(sections), Options{})
	first, _ := topOf(ts, "Section 0")
	for _, tc := range []struct {
		name string
		line int
		want int // the line of the block at the top
	}{
		{"far away, never measured", 4 * 1100, 4 * 1100},
		{"inside a block", 4*700 + 3, 4*700 + 2},
		{"back to the start", 0, 0},
		{"far away again, measured now", 4 * 1100, 4 * 1100},
		{"before the first block", -1, 0},
		{"the last block, which the end of the document keeps from the top", 4*sections - 2, 4*sections - 14},
	} {
		ctl.ScrollToLine(tc.line)
		for range 3 {
			ts.Pump()
		}
		if got := ctl.TopLine(); got != tc.want && (tc.want <= 4*(sections-4) || got < tc.want) {
			t.Errorf("%s: after ScrollToLine(%d) the top line is %d, want %d", tc.name, tc.line, got, tc.want)
		}
		if st := stats(t, ctl); st.Built > 30 {
			t.Errorf("%s: %d blocks are built", tc.name, st.Built)
		}
	}
	// the block of a line is at the top of the viewport, as far as the end of the document allows
	ctl.ScrollToLine(4 * 1100)
	for range 3 {
		ts.Pump()
	}
	if y, ok := topOf(ts, fmt.Sprintf("Section %d", 1100)); !ok || y > first+4 {
		t.Errorf("the heading of the line is %v px from the top (the first heading is at %v), found %v", y, first, ok)
	}
	// the end of the document is at the bottom of the viewport, with no empty space under it
	ctl.ScrollToLine(4*sections - 2)
	for range 3 {
		ts.Pump()
	}
	last := ts.BottomRight(textOf(fmt.Sprintf("Paragraph %d ", sections-1)))
	if gap := previewSize.Height - last.Dy; gap < 0 || gap > bottomPad+4 {
		t.Errorf("the last paragraph ends %v px above the bottom of the viewport, want the %v of the page", gap, bottomPad)
	}
}

func TestScrollToLineBeforeTheTextIsParsedWaits(t *testing.T) {
	src := paragraphs(1500) // more than syncParseLimit: parsed on a goroutine
	if len(src) <= syncParseLimit {
		t.Fatalf("the text has %d bytes, a sync parse takes up to %d", len(src), syncParseLimit)
	}
	ctl := NewController()
	ctl.ScrollToLine(4 * 900)
	if !ctl.hasPending || ctl.pending != 4*900 {
		t.Fatalf("the scroll did not wait: %+v", ctl)
	}
	ts := uitest.Pump(t, uitest.Themes()[0], previewSize, NewView(src, Options{Controller: ctl}))
	// the first frame is drawn with no blocks: it does not wait for the parse
	if st := stats(t, ctl); st.Blocks != 0 || !st.Parsing {
		t.Errorf("the first frame has %+v", st)
	}
	waitParsed(t, ts, ctl)
	if ctl.hasPending || ctl.TopLine() != 4*900 {
		t.Errorf("after the parse the top line is %d (pending %v), want %d", ctl.TopLine(), ctl.hasPending, 4*900)
	}
}

func TestASmallTextIsShownInTheFirstFrame(t *testing.T) {
	ctl := NewController()
	ts := uitest.Pump(t, uitest.Themes()[0], previewSize, NewView("# Title\n\ntext\n", Options{Controller: ctl}))
	if st := stats(t, ctl); st.Blocks != 2 || st.Built != 2 {
		t.Errorf("%+v", st)
	}
	if !ts.Exists(textOf("Title")) {
		t.Error("the title is not on screen")
	}
}

// changing is a preview whose text can be changed from the test.
func changing(t testing.TB, src string, opts Options) (*gluttertest.Tester, *Controller, *foundation.ValueNotifier[string]) {
	t.Helper()
	ctl := NewController()
	opts.Controller = ctl
	source := foundation.NewValueNotifier(src)
	v := widgets.NewValueListenableBuilder(source, func(_ widgets.BuildContext, s string, _ widgets.Widget) widgets.Widget {
		return NewView(s, opts)
	}, nil)
	ts := uitest.Pump(t, uitest.Themes()[0], previewSize, v)
	waitParsed(t, ts, ctl)
	return ts, ctl, source
}

func TestAnEditRebuildsOnlyItsBlockAndWaitsForTheDelay(t *testing.T) {
	src := paragraphs(10)
	ts, ctl, source := changing(t, src, Options{})
	built := stats(t, ctl).Builds
	if built == 0 {
		t.Fatal("nothing was built")
	}
	// the paragraph of the third section
	edited := strings.Replace(src, "Paragraph 2 of", "Paragraph 2, edited, of", 1)
	source.SetValue(edited)
	for range 3 {
		ts.Pump()
	}
	if ts.Exists(textOf("edited")) || stats(t, ctl).Builds != built {
		t.Error("the change was shown before the delay is over")
	}
	// a second change in the delay does not start another delay: the text at the end of the first one is shown
	edited2 := strings.Replace(edited, "Paragraph 2, edited,", "Paragraph 2, edited twice,", 1)
	ts.Pump(previewDelay / 2)
	source.SetValue(edited2)
	ts.Pump(time.Millisecond) // the view has the new text
	ts.Pump(previewDelay / 2)
	ts.Pump()
	if !ts.Exists(textOf("edited twice")) {
		t.Fatal("the change is not shown after the delay")
	}
	if got := stats(t, ctl).Builds; got != built+1 {
		t.Errorf("an edit of one block made %d new block widgets, want 1", got-built)
	}
	// an edit that makes the text the one that is shown needs no parse
	source.SetValue(edited2)
	ts.Pump(previewDelay * 2)
	if got := stats(t, ctl).Builds; got != built+1 {
		t.Errorf("the same text built %d more blocks", got-built-1)
	}
	// a block added above the viewport changes no block below it
	source.SetValue("A new first paragraph.\n\n" + edited2)
	ts.Pump(time.Millisecond)
	ts.Pump(previewDelay + time.Millisecond)
	ts.Pump()
	if got := stats(t, ctl).Builds; got != built+2 {
		t.Errorf("a new block built %d widgets in all, want %d", got-built, 2)
	}
}

// waitIdle pumps until the preview shows the latest text (a big one is parsed on a goroutine).
func waitIdle(t testing.TB, ts *gluttertest.Tester, ctl *Controller) {
	t.Helper()
	deadline := time.Now().Add(20 * time.Second)
	for stats(t, ctl).Parsing {
		if time.Now().After(deadline) {
			t.Fatal("the preview never showed the latest text")
		}
		ts.Pump()
		time.Sleep(time.Millisecond)
	}
	for range 3 {
		ts.Pump()
	}
}

func TestAnEditKeepsTheViewportAtItsBlock(t *testing.T) {
	src := paragraphs(300)
	ts, ctl, source := changing(t, src, Options{})
	ctl.ScrollToLine(4 * 150)
	for range 3 {
		ts.Pump()
	}
	before, ok := topOf(ts, "Section 150")
	if !ok || ctl.TopLine() != 4*150 {
		t.Fatalf("the preview is at line %d", ctl.TopLine())
	}
	// two lines are added at the start of the document: every block below moves down by two lines
	source.SetValue("one\n\ntwo\n\n" + src)
	ts.Pump(time.Millisecond)
	ts.Pump(previewDelay + time.Millisecond)
	waitIdle(t, ts, ctl)
	if got := ctl.TopLine(); got != 4*150+4 {
		t.Errorf("after the edit the top line is %d, want %d", got, 4*150+4)
	}
	if after, ok := topOf(ts, "Section 150"); !ok || after != before {
		t.Errorf("the block moved from %v to %v px (%v)", before, after, ok)
	}
}

func TestABigChangeIsParsedOffTheUIThreadAfterTheDelay(t *testing.T) {
	src := paragraphs(1200)
	ts, ctl, source := changing(t, src, Options{})
	source.SetValue(strings.Replace(src, "Paragraph 1 ", "Paragraph one ", 1))
	ts.Pump(time.Millisecond)
	ts.Pump(previewDelay + time.Millisecond)
	// the parse runs on a goroutine: the old text stays until it posts its result
	if st := stats(t, ctl); !st.Parsing {
		t.Errorf("the parse did not start: %+v", st)
	}
	deadline := time.Now().Add(20 * time.Second)
	for stats(t, ctl).Parsing || !ts.Exists(textOf("Paragraph one")) {
		if time.Now().After(deadline) {
			t.Fatal("the change was never shown")
		}
		ts.Pump()
		time.Sleep(time.Millisecond)
	}
}

func TestTheScrollBarTrackMovesThePreview(t *testing.T) {
	ts, ctl := pumpPreview(t, paragraphs(1500), Options{})
	// the scroll bar shows when the content moves
	ts.ScrollAt(gui.Offset{Dx: 250, Dy: 200}, gui.Offset{Dx: 0, Dy: 53})
	for range 10 {
		ts.Pump(16 * time.Millisecond)
	}
	before := stats(t, ctl).ScrollTop
	g := ts.StartGesture(gui.Offset{Dx: previewSize.Width - 4, Dy: previewSize.Height / 2})
	ts.Pump(time.Millisecond)
	g.Up()
	for range 3 {
		ts.Pump()
	}
	st := stats(t, ctl)
	if st.ScrollTop < before+st.ContentHeight/4 || st.TopLine < 1000 {
		t.Errorf("a click half way down the track left the preview at %+v (it was at %v)", st, before)
	}
	if st.Built > 30 {
		t.Errorf("%d blocks are built after the jump", st.Built)
	}
}

// pictures writes n PNG files of w by h pixels in a folder and returns the resolver of images for them (pic0.png, pic1.png...).
func pictures(t testing.TB, n, w, h int) func(string) string {
	t.Helper()
	dir := t.TempDir()
	img := image.NewRGBA(image.Rect(0, 0, w, h))
	for y := range h {
		for x := range w {
			img.Set(x, y, color.RGBA{uint8(x), uint8(y), 120, 255})
		}
	}
	for i := range n {
		f, err := os.Create(filepath.Join(dir, fmt.Sprintf("pic%d.png", i)))
		if err != nil {
			t.Fatal(err)
		}
		if err := png.Encode(f, img); err != nil {
			t.Fatal(err)
		}
		if err := f.Close(); err != nil {
			t.Fatal(err)
		}
	}
	return func(dest string) string {
		if strings.HasPrefix(dest, "pic") {
			return filepath.Join(dir, dest)
		}
		return ""
	}
}

// withImages is a document of n sections, each with a paragraph and an image.
func withImages(n int) string {
	var b strings.Builder
	for i := range n {
		fmt.Fprintf(&b, "Paragraph %d.\n\n![Picture %d](pic%d.png)\n\n", i, i, i)
	}
	return b.String()
}

// waitImages pumps until the images that were asked for are decoded.
func waitImages(t testing.TB, ts *gluttertest.Tester, cache *ImageCache) {
	t.Helper()
	deadline := time.Now().Add(20 * time.Second)
	for {
		ts.Pump()
		if cache.pending() == 0 {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("the images were not decoded")
		}
		time.Sleep(time.Millisecond)
	}
	for range 3 {
		ts.Pump()
	}
}

func TestImagesLoadOnlyNearTheViewportAndShowTheAltTextFirst(t *testing.T) {
	const n = 60
	cache := NewImageCache(0)
	t.Cleanup(cache.Close)
	opts := Options{Images: cache, ResolveImage: pictures(t, n, 300, 200)}
	ctl := NewController()
	opts.Controller = ctl
	ts := uitest.Pump(t, uitest.Themes()[0], previewSize, NewView(withImages(n), opts))
	waitParsed(t, ts, ctl)
	// the alt text shows while the pixels load
	if !ts.Exists(plainText("[Picture 0]")) && cache.Len() == 0 {
		t.Error("the alt text is not shown while the image loads")
	}
	waitImages(t, ts, cache)
	if ts.Exists(plainText("[Picture 0]")) {
		t.Error("the alt text is still shown after the image loaded")
	}
	// the alt text is a line, so the first windows are longer than the viewport needs: a few more images than it shows
	if cache.Len() < 1 || cache.Len() > 10 {
		t.Errorf("%d images were decoded for a viewport of 400 px with images of 200, want a few of the %d", cache.Len(), n)
	}
	// a jump far away decodes the images there, not the ones on the way
	ctl.ScrollToLine(4 * 40)
	waitImages(t, ts, cache)
	if cache.Len() < 2 || cache.Len() > 20 {
		t.Errorf("%d images are in the cache after a jump to the middle", cache.Len())
	}
	if !ts.Exists(gluttertest.ByType[*widgets.RawImage]()) {
		t.Error("no image is drawn at the end of the jump")
	}
}

func TestImagesAreDecodedAtTheSizeTheyAreShown(t *testing.T) {
	cache := NewImageCache(0)
	t.Cleanup(cache.Close)
	// 1200 x 800 pixels in a page 500 wide (448 for the content): shown 448 logical pixels wide, 448 x 2 = 896 device pixels
	opts := Options{Images: cache, ResolveImage: pictures(t, 1, 1200, 800)}
	ctl := NewController()
	opts.Controller = ctl
	ts := uitest.Pump(t, uitest.Themes()[0], previewSize, NewView("![x](pic0.png)", opts))
	ts.SetDevicePixelRatio(2)
	ts.Pump()
	waitParsed(t, ts, ctl)
	waitImages(t, ts, cache)
	var got *imageEntry
	for k, e := range cache.entries {
		if k.ratio100 == 200 {
			got = e
		}
	}
	if got == nil || got.res.pic == nil {
		t.Fatalf("no image in the cache: %+v", cache.entries)
	}
	if w, h := got.res.width, got.res.height; w != 448 || h != 299 {
		t.Errorf("the image is shown %v x %v logical pixels, want 448 x 299", w, h)
	}
	if w := got.res.pic.Width(); w != 896 {
		t.Errorf("the image holds %d pixels across, want 896 (not the 1200 of the file)", w)
	}
	if got.bytes != got.res.pic.Width()*got.res.pic.Height()*4 {
		t.Errorf("the cache counts %d bytes for %d x %d pixels", got.bytes, got.res.pic.Width(), got.res.pic.Height())
	}
}

func TestAnImageThatCannotBeLoadedKeepsItsAltText(t *testing.T) {
	cache := NewImageCache(0)
	t.Cleanup(cache.Close)
	dir := t.TempDir()
	broken := filepath.Join(dir, "broken.png")
	if err := os.WriteFile(broken, []byte("not an image"), 0o600); err != nil {
		t.Fatal(err)
	}
	opts := Options{Images: cache, ResolveImage: func(dest string) string {
		if dest == "broken.png" {
			return broken
		}
		return filepath.Join(dir, "missing.png")
	}}
	ctl := NewController()
	opts.Controller = ctl
	ts := uitest.Pump(t, uitest.Themes()[0], previewSize, NewView("![The broken one](broken.png)\n\n![The missing one](missing.png)", opts))
	waitParsed(t, ts, ctl)
	waitImages(t, ts, cache)
	for _, alt := range []string{"[The broken one]", "[The missing one]"} {
		if !ts.Exists(gluttertest.ByText(alt)) {
			t.Errorf("%s is not shown", alt)
		}
	}
	if ts.Exists(gluttertest.ByType[*widgets.RawImage]()) {
		t.Error("an image is drawn")
	}
	// the broken file is an entry with its error, so the next build does not read it again; the missing file has no stamp
	if cache.Len() != 1 || cache.Used() != 0 {
		t.Errorf("the cache has %d entries and %d bytes, want the failure of the broken file", cache.Len(), cache.Used())
	}
}

func TestTheContentStaysStillWhenAnImageAboveLoads(t *testing.T) {
	cache := NewImageCache(0)
	t.Cleanup(cache.Close)
	// a paragraph, an image 300 pixels tall, a long run of paragraphs: the preview is at the paragraphs, the image is above them
	src := "Paragraph before.\n\n![Tall](pic0.png)\n\n" + paragraphs(200)
	opts := Options{Images: cache, ResolveImage: pictures(t, 1, 400, 300)}
	ctl := NewController()
	opts.Controller = ctl
	ts := uitest.Pump(t, uitest.Themes()[0], previewSize, NewView(src, opts))
	waitParsed(t, ts, ctl)
	waitImages(t, ts, cache)
	// scroll to the first section: the image (300 px) is just above it, inside the margin, so it has not been loaded yet
	// when the jump is made from a document whose image never was; scroll so that the image is not loaded
	cache.entries = map[imageKey]*imageEntry{}
	cache.used = 0
	cache.lru.Init()
	ctl.ScrollToLine(4)
	for range 3 {
		ts.Pump()
	}
	before, ok := topOf(ts, "Section 0")
	if !ok {
		t.Fatal("the first section is not on screen")
	}
	waitImages(t, ts, cache)
	if after, ok := topOf(ts, "Section 0"); !ok || after != before {
		t.Errorf("the section moved from %v to %v px when the image above it loaded", before, after)
	}
}

func TestTheBudgetOfTheImageCacheHoldsWhileScrolling(t *testing.T) {
	const n = 50
	// each image is shown 300 x 200 logical pixels at a ratio of 1: 240000 bytes; the budget holds three of them
	cache := NewImageCache(3 * 300 * 200 * 4)
	t.Cleanup(cache.Close)
	opts := Options{Images: cache, ResolveImage: pictures(t, n, 300, 200)}
	ctl := NewController()
	opts.Controller = ctl
	ts := uitest.Pump(t, uitest.Themes()[0], previewSize, NewView(withImages(n), opts))
	waitParsed(t, ts, ctl)
	waitImages(t, ts, cache)
	for line := 0; line < 4*n; line += 20 {
		ctl.ScrollToLine(line)
		waitImages(t, ts, cache)
		if cache.Used() > cache.Budget() {
			t.Fatalf("at line %d the cache holds %d bytes over its budget of %d", line, cache.Used(), cache.Budget())
		}
	}
	if cache.Len() > 3 {
		t.Errorf("%d images are kept for a budget of three", cache.Len())
	}
}
