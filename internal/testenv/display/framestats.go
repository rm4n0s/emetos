package display

import (
	"strconv"
	"strings"
	"time"
)

// A FrameWindow is one "frames" line of Glutter's statistics (GLUTTER_FRAME_STATS=1): what it measured in one second.
type FrameWindow struct {
	// Frames is the number of frames drawn in the second.
	Frames int
	// BuildAvg and BuildMax are the average and the worst time of the UI thread (build, layout and paint), RasterAvg and
	// RasterMax those of the renderer.
	BuildAvg, BuildMax, RasterAvg, RasterMax time.Duration
	// Over16 is the number of frames whose build and raster took more than 16 ms together.
	Over16 int
}

// ParseFrameStats reads the "frames" lines of the log of an editor that ran with GLUTTER_FRAME_STATS=1. The log lines are
// slog's text format ("... msg=frames fps=60 build_avg=1.2ms ...") or its console format ("INFO frames fps=60 ...").
func ParseFrameStats(log string) []FrameWindow {
	var out []FrameWindow
	for _, line := range strings.Split(log, "\n") {
		if !strings.Contains(line, " msg=frames ") && !strings.Contains(line, "INFO frames ") {
			continue
		}
		var w FrameWindow
		for _, field := range strings.Fields(line) {
			key, value, ok := strings.Cut(field, "=")
			if !ok {
				continue
			}
			switch key {
			case "fps":
				w.Frames, _ = strconv.Atoi(value)
			case "over_16ms":
				w.Over16, _ = strconv.Atoi(value)
			case "build_avg":
				w.BuildAvg, _ = time.ParseDuration(value)
			case "build_max":
				w.BuildMax, _ = time.ParseDuration(value)
			case "raster_avg":
				w.RasterAvg, _ = time.ParseDuration(value)
			case "raster_max":
				w.RasterMax, _ = time.ParseDuration(value)
			}
		}
		out = append(out, w)
	}
	return out
}

// FrameSummary is a run of FrameWindows put together.
type FrameSummary struct {
	// Frames is the number of frames drawn.
	Frames int
	// BuildAvg and RasterAvg are the averages per frame, BuildMax and RasterMax the worst frames.
	BuildAvg, BuildMax, RasterAvg, RasterMax time.Duration
	// Over16 is the number of frames over 16 ms.
	Over16 int
}

// Summarize puts the windows together: the averages are weighted by the frames of each window.
func Summarize(windows []FrameWindow) FrameSummary {
	var s FrameSummary
	var build, raster time.Duration
	for _, w := range windows {
		s.Frames += w.Frames
		s.Over16 += w.Over16
		build += w.BuildAvg * time.Duration(w.Frames)
		raster += w.RasterAvg * time.Duration(w.Frames)
		s.BuildMax, s.RasterMax = max(s.BuildMax, w.BuildMax), max(s.RasterMax, w.RasterMax)
	}
	if s.Frames > 0 {
		s.BuildAvg, s.RasterAvg = build/time.Duration(s.Frames), raster/time.Duration(s.Frames)
	}
	return s
}

// FirstFrame is the "first frame presented" line of Glutter's log.
type FirstFrame struct {
	// SinceRun is the time from the start of Run to the frame, SinceProcessStart the time from the start of the process.
	SinceRun, SinceProcessStart time.Duration
}

// ParseFirstFrame reads the line of the first frame presented; false when the log has none. Glutter writes it without
// GLUTTER_FRAME_STATS too.
func ParseFirstFrame(log string) (FirstFrame, bool) {
	for _, line := range strings.Split(log, "\n") {
		if !strings.Contains(line, "first frame presented") {
			continue
		}
		var f FirstFrame
		for _, field := range strings.Fields(line) {
			key, value, ok := strings.Cut(field, "=")
			if !ok {
				continue
			}
			switch key {
			case "since_run":
				f.SinceRun, _ = time.ParseDuration(value)
			case "since_process_start":
				f.SinceProcessStart, _ = time.ParseDuration(value)
			}
		}
		return f, true
	}
	return FirstFrame{}, false
}

// A SlowFrame is one "slow frame" line of Glutter's log (GLUTTER_FRAME_STATS=1): a frame whose build and raster took over 16 ms
// together.
type SlowFrame struct {
	// Time is when the frame ended (the time of the log line), on the clock of the test's machine.
	Time time.Time
	// Build is the time of the UI thread (build, layout and paint), Raster the time of the renderer.
	Build, Raster time.Duration
}

// ParseSlowFrames reads the "slow frame" lines of the log of an editor that ran with GLUTTER_FRAME_STATS=1. A line with no
// readable time (the console format) has the zero Time.
func ParseSlowFrames(log string) []SlowFrame {
	var out []SlowFrame
	for _, line := range strings.Split(log, "\n") {
		if !strings.Contains(line, "slow frame") {
			continue
		}
		var f SlowFrame
		for _, field := range strings.Fields(line) {
			key, value, ok := strings.Cut(field, "=")
			if !ok {
				continue
			}
			switch key {
			case "time":
				f.Time, _ = time.Parse(time.RFC3339Nano, value)
			case "build":
				f.Build, _ = time.ParseDuration(value)
			case "raster":
				f.Raster, _ = time.ParseDuration(value)
			}
		}
		out = append(out, f)
	}
	return out
}
