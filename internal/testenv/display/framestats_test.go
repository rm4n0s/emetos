package display

import (
	"testing"
	"time"
)

func TestParseFrameStatsAndSummarize(t *testing.T) {
	log := `time=2026-10-27T10:00:00.000Z level=INFO msg="first frame presented" since_run=80ms
time=2026-10-27T10:00:01.000Z level=INFO msg=frames fps=10 build_avg=2ms build_max=5ms raster_avg=10ms raster_max=30ms over_16ms=2 go_heap_mb=40
2026/10/27 10:00:02 INFO frames fps=30 build_avg=4ms build_max=9ms raster_avg=20ms raster_max=25ms over_16ms=1
time=2026-10-27T10:00:03.000Z level=INFO msg="slow frame" build=1ms raster=40ms
`
	windows := ParseFrameStats(log)
	if len(windows) != 2 {
		t.Fatalf("%d windows, want 2: %+v", len(windows), windows)
	}
	if w := windows[0]; w.Frames != 10 || w.BuildMax != 5*time.Millisecond || w.RasterMax != 30*time.Millisecond || w.Over16 != 2 {
		t.Errorf("first window: %+v", w)
	}
	s := Summarize(windows)
	// (10*2 + 30*4) / 40 = 3.5 ms; (10*10 + 30*20) / 40 = 17.5 ms
	if s.Frames != 40 || s.BuildAvg != 3500*time.Microsecond || s.RasterAvg != 17500*time.Microsecond || s.BuildMax != 9*time.Millisecond || s.RasterMax != 30*time.Millisecond || s.Over16 != 3 {
		t.Errorf("summary: %+v", s)
	}
	if got := Summarize(nil); got != (FrameSummary{}) {
		t.Errorf("empty summary: %+v", got)
	}
}

func TestParseFirstFrame(t *testing.T) {
	log := `time=2026-10-27T10:00:00.000Z level=INFO msg="first frame presented" since_run=270ms renderer="gles: llvmpipe" since_process_start=320ms go_heap_mb=40
time=2026-10-27T10:00:01.000Z level=INFO msg=frames fps=10
`
	f, ok := ParseFirstFrame(log)
	if !ok || f.SinceRun != 270*time.Millisecond || f.SinceProcessStart != 320*time.Millisecond {
		t.Errorf("first frame: %+v %v", f, ok)
	}
	if _, ok := ParseFirstFrame("nothing\n"); ok {
		t.Error("a log with no first frame has one")
	}
}
