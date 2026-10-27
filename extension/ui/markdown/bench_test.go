package markdown

import (
	"path/filepath"
	"strings"
	"testing"
	"time"

	gui "github.com/rm4n0s/glutter/ui"

	"github.com/rm4n0s/emetos/extension/ui/uitest"
	"github.com/rm4n0s/emetos/extension/ui/uitest/mdfixture"
)

// The benchmarks of the preview of a big document (SPEC §14 "Markdown preview", step 17b.2): the document of extension/ui/uitest/
// mdfixture, 700 KB with 150 images. They measure the UI thread alone, with the fake platform (no rasterization):
//
//	go test -tags glutter_release ./extension/ui/markdown -run xxx -bench Preview -benchmem

// bigPreview is the fixture document and what a preview of it needs.
type bigPreview struct {
	text string
	opts Options
}

func newBigPreview(b *testing.B) bigPreview {
	b.Helper()
	dir := b.TempDir()
	doc := mdfixture.Generate(b, dir)
	cache := NewImageCache(0)
	b.Cleanup(cache.Close)
	return bigPreview{text: doc.Text, opts: Options{
		Images: cache,
		ResolveImage: func(dest string) string {
			if strings.HasPrefix(dest, mdfixture.ImageDir+"/") {
				return filepath.Join(dir, filepath.FromSlash(dest))
			}
			return ""
		},
	}}
}

// BenchmarkPreviewParse is the parse of the document and the keys of its blocks, which run off the UI thread.
func BenchmarkPreviewParse(b *testing.B) {
	doc := mdfixture.Generate(b, b.TempDir())
	b.SetBytes(int64(len(doc.Text)))
	b.ReportAllocs()
	for range b.N {
		d := newDocument(doc.Text)
		if len(d.blocks) < 1000 {
			b.Fatalf("%d blocks", len(d.blocks))
		}
	}
}

// BenchmarkPreviewFirstFrame is the first frame of the preview of the big document, which does not wait for the parse, and
// the frame that shows the blocks when the parse is done.
func BenchmarkPreviewFirstFrame(b *testing.B) {
	big := newBigPreview(b)
	var first, shown time.Duration
	b.ReportAllocs()
	for range b.N {
		b.StopTimer()
		ctl := NewController()
		opts := big.opts
		opts.Controller = ctl
		v := NewView(big.text, opts)
		b.StartTimer()
		start := time.Now()
		ts := uitest.Pump(b, uitest.Themes()[0], gui.Size{Width: 700, Height: 800}, v)
		first += time.Since(start)
		start = time.Now()
		waitParsed(b, ts, ctl)
		shown += time.Since(start)
		b.StopTimer()
		ts.Pump()
	}
	b.ReportMetric(float64(first.Microseconds())/float64(b.N)/1000, "first-frame-ms")
	b.ReportMetric(float64(shown.Microseconds())/float64(b.N)/1000, "parse-and-blocks-ms")
}

// BenchmarkPreviewJump is a jump to a place of the document that was never on the screen: the window is chosen from the estimates,
// built and laid out.
func BenchmarkPreviewJump(b *testing.B) {
	big := newBigPreview(b)
	ctl := NewController()
	big.opts.Controller = ctl
	ts := uitest.Pump(b, uitest.Themes()[0], gui.Size{Width: 700, Height: 800}, NewView(big.text, big.opts))
	waitParsed(b, ts, ctl)
	lines := strings.Count(big.text, "\n")
	b.ReportAllocs()
	b.ResetTimer()
	for i := range b.N {
		ctl.ScrollToLine(3 + (i*7919)%lines)
		ts.PumpFrame(0)
		ts.PumpFrame(0)
	}
}

// BenchmarkPreviewWheelStep is a scroll of a few pixels, the frame of every step of a wheel or a trackpad.
func BenchmarkPreviewWheelStep(b *testing.B) {
	big := newBigPreview(b)
	ctl := NewController()
	big.opts.Controller = ctl
	ts := uitest.Pump(b, uitest.Themes()[0], gui.Size{Width: 700, Height: 800}, NewView(big.text, big.opts))
	waitParsed(b, ts, ctl)
	at := gui.Offset{Dx: 350, Dy: 400}
	b.ReportAllocs()
	b.ResetTimer()
	for i := range b.N {
		delta := 12.0
		if i%2000 >= 1000 {
			delta = -12
		}
		ts.ScrollAt(at, gui.Offset{Dx: 0, Dy: delta})
		ts.PumpFrame(16 * time.Millisecond)
	}
}
