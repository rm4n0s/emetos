package terminal

import (
	"strings"
	"testing"
	"time"

	gui "github.com/rm4n0s/glutter/ui"

	"github.com/rm4n0s/emetos/extension/ui/uitest"
)

// fullScreen is a screen of rows x cols cells that all change from one frame to the next: every row is a line of colored text
// that depends on the number of the frame (SPEC §14 "Terminal frame": htop, vim scrolling).
func fullScreen(frame, rows, cols int) string {
	var sb strings.Builder
	sb.WriteString("\x1b[H")
	for y := 0; y < rows; y++ {
		for x := 0; x < cols; x += 8 {
			sb.WriteString("\x1b[3")
			sb.WriteByte(byte('1' + (x/8+y+frame)%6))
			sb.WriteString("m")
			for i := 0; i < 8 && x+i < cols; i++ {
				sb.WriteByte(byte('a' + (x+i+y+frame)%26))
			}
		}
		if y < rows-1 {
			sb.WriteString("\r\n")
		}
	}
	return sb.String()
}

// BenchmarkFullScreenFrame measures the UI thread's part of a frame of 160 x 45 cells that all changed: the snapshot of the
// rows, the runs of glyphs and backgrounds and the two atlas draws, without the renderer (the fake platform does not
// rasterize), which Glutter owns. Budget of SPEC §14: 3 ms or less.
func BenchmarkFullScreenFrame(b *testing.B) {
	th := uitest.Themes()[0]
	r := newFakePaneT(b, th, gui.Size{Width: 1320, Height: 880}, nil)
	if r.pv.cols < 160 || r.pv.rows < 45 {
		b.Fatalf("the pane has %d x %d cells, want 160 x 45", r.pv.cols, r.pv.rows)
	}
	cols, rows := 160, 45
	r.writeB(b, fullScreen(0, rows, cols)) // warms the glyph atlas: a frame shows the glyphs it needs once
	b.ReportAllocs()
	b.ResetTimer()
	var total time.Duration
	for i := 0; i < b.N; i++ {
		b.StopTimer()
		r.feed(b, fullScreen(i+1, rows, cols))
		b.StartTimer()
		start := time.Now()
		r.ts.FlushPosted()
		r.ts.Pump(16 * time.Millisecond)
		total += time.Since(start)
	}
	b.ReportMetric(float64(total.Microseconds())/float64(b.N)/1000, "ms/frame")
}

// BenchmarkEchoFrame measures the UI thread's part of the frame that shows a typed character at a prompt: one row changed.
func BenchmarkEchoFrame(b *testing.B) {
	th := uitest.Themes()[0]
	r := newFakePaneT(b, th, gui.Size{Width: 1320, Height: 880}, nil)
	r.writeB(b, "user@host:~/project$ ")
	b.ReportAllocs()
	b.ResetTimer()
	var total time.Duration
	for i := 0; i < b.N; i++ {
		b.StopTimer()
		r.feed(b, string(rune('a'+i%26)))
		b.StartTimer()
		start := time.Now()
		r.ts.FlushPosted()
		r.ts.Pump(16 * time.Millisecond)
		total += time.Since(start)
	}
	b.ReportMetric(float64(total.Microseconds())/float64(b.N)/1000, "ms/frame")
}
