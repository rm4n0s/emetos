package app

import (
	"testing"
	"time"

	"github.com/rm4n0s/glutter/services"
	"github.com/rm4n0s/glutter/ui"

	"github.com/rm4n0s/emetos/extension"
)

// BenchmarkHeadlessFrame measures the frames of a 1280x800 headless view over a Go file: what the UI thread of the host
// pays for a frame after a key typed in the view (build, layout and paint, then the software renderer), reported as
// build-ns/frame and raster-ns/frame (SPEC §10.18 "As built (step 15.1)"). The window itself is pumped outside the timer.
func BenchmarkHeadlessFrame(b *testing.B) {
	r := newProjectRig(b, extension.Size{Width: 1280, Height: 800}, "Dark Modern")
	v := r.view
	var raster time.Duration
	draw := v.eng.onScene
	v.eng.onScene = func(s *ui.Scene, w, h int) {
		t0 := time.Now()
		draw(s, w, h)
		raster += time.Since(t0)
	}
	r.frame(b)
	raster = 0
	var total time.Duration
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		v.SendKey(extension.KeyEvent{Kind: extension.KeyDown, Physical: services.PhysicalKeyboardKeyKeyX.USBHidUsage(), Logical: 'x', Text: "x"})
		v.SendKey(extension.KeyEvent{Kind: extension.KeyUp, Physical: services.PhysicalKeyboardKeyKeyX.USBHidUsage(), Logical: 'x'})
		t0 := time.Now()
		v.eng.pump()
		total += time.Since(t0)
	}
	b.StopTimer()
	b.ReportMetric(float64((total-raster).Nanoseconds())/float64(b.N), "build-ns/frame")
	b.ReportMetric(float64(raster.Nanoseconds())/float64(b.N), "raster-ns/frame")
}
