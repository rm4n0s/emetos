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

func TestParseSlowFrames(t *testing.T) {
	log := `time=2026-10-28T05:15:04.746+02:00 level=INFO msg="slow frame" build=103.82ms raster=4.32915s
time=2026-10-28T05:15:05.515+02:00 level=INFO msg=frames fps=10 build_avg=2ms
2026/10/28 05:15:06 INFO slow frame build=9.07ms raster=699.89ms
time=2026-10-28T05:15:07.706+02:00 level=INFO msg="slow frame" build=20µs raster=19.03ms
`
	at := time.Date(2026, 10, 28, 5, 15, 4, 746_000_000, time.FixedZone("", 2*3600))
	tests := []struct {
		name  string
		index int
		want  SlowFrame
	}{
		{"the first frame of the window", 0, SlowFrame{Time: at, Build: 103820 * time.Microsecond, Raster: 4329150 * time.Microsecond}},
		{"the console format has no time", 1, SlowFrame{Build: 9070 * time.Microsecond, Raster: 699890 * time.Microsecond}},
		{"microseconds", 2, SlowFrame{Time: at.Add(2960 * time.Millisecond), Build: 20 * time.Microsecond, Raster: 19030 * time.Microsecond}},
	}
	got := ParseSlowFrames(log)
	if len(got) != len(tests) {
		t.Fatalf("%d slow frames, want %d: %+v", len(got), len(tests), got)
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			g := got[tt.index]
			if !g.Time.Equal(tt.want.Time) || g.Build != tt.want.Build || g.Raster != tt.want.Raster {
				t.Errorf("slow frame %d: %+v, want %+v", tt.index, g, tt.want)
			}
		})
	}
	if len(ParseSlowFrames("nothing\n")) != 0 {
		t.Error("a log with no slow frame has one")
	}
}
