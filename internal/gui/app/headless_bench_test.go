package app

import (
	"testing"
	"time"

	"github.com/rm4n0s/glutter/gluttertest"
	"github.com/rm4n0s/glutter/services"

	"github.com/rm4n0s/emetos/extension"
)

// BenchmarkHeadlessFrame measures the frames of a 1280x800 headless view over a Go file (SPEC §10.18 "As built (Glutter
// 1.9.1)"). The UI thread of the host builds, lays out, paints and detaches the scene; the view draws it on a goroutine of its
// own. For each key typed in the view (or, for "scroll", each wheel step that moves the whole editor) it reports ui-ns/frame,
// the time the UI thread spent in the pump that ran the frame (the window rebuilds for the edit as well, which "window only"
// measures with no view: the difference is the view's), and picture-ns/frame, the time from the key to the picture in the
// hand of the function of OnFrame (the rest, picture minus ui, is the drawing on the goroutine of the view).
func BenchmarkHeadlessFrame(b *testing.B) {
	for _, name := range []string{"typed", "scroll", "window only"} {
		b.Run(name, func(b *testing.B) {
			r := newProjectRig(b, extension.Size{Width: 1280, Height: 800}, "Dark Modern")
			r.frame(b)
			if name == "window only" {
				r.view.Close()
				r.pump()
			}
			x := services.PhysicalKeyboardKeyKeyX.USBHidUsage()
			dy := -120.0
			step := func() {
				switch name {
				case "typed":
					r.view.SendKey(extension.KeyEvent{Kind: extension.KeyDown, Physical: x, Logical: 'x', Text: "x"})
					r.view.SendKey(extension.KeyEvent{Kind: extension.KeyUp, Physical: x, Logical: 'x'})
				case "scroll":
					dy = -dy // down and up, so that every step moves the picture
					r.view.SendScroll(extension.ScrollEvent{X: 700, Y: 400, DY: dy})
				default:
					r.ts.SendKeyEvent('x', gluttertest.KeyOpts{Physical: services.PhysicalKeyboardKeyKeyX, Character: "x"})
				}
			}
			var ui, picture time.Duration
			b.ResetTimer()
			for range b.N {
				t0 := time.Now()
				step()
				// the pump that runs the frame of the window and of the view; the picture is then drawn on the goroutine of
				// the view, and what comes after is waiting for it
				p0 := time.Now()
				r.ts.Pump(20 * time.Millisecond)
				ui += time.Since(p0)
				if name == "window only" {
					continue
				}
				select {
				case <-r.frames:
				case <-time.After(5 * time.Second):
					b.Fatal("the view drew nothing")
				}
				picture += time.Since(t0)
			}
			b.StopTimer()
			b.ReportMetric(float64(ui.Nanoseconds())/float64(b.N), "ui-ns/frame")
			b.ReportMetric(float64(picture.Nanoseconds())/float64(b.N), "picture-ns/frame")
		})
	}
}
