//go:build emetos_display

package main

import (
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/rm4n0s/emetos/extension/ui/uitest/mdfixture"
	"github.com/rm4n0s/emetos/internal/gui/app"
	"github.com/rm4n0s/emetos/internal/testenv/display"
)

// The Markdown preview of a big document (SPEC §14, step 17b.2), with real input: the preview of a 700 KB document with 150
// images opens at once, scrolls with the wheel and stays light. The numbers are the headless ones (software rendering); the
// ones with a GPU come from the pass on the desktop.

const (
	// previewOpenBudget is SPEC §14's "the first frame of the preview within 300 ms", with the time the test takes to ask.
	previewOpenBudget = 300 * time.Millisecond
	// previewMemoryBudget is "resident memory at most 200 MB more than with the file open as text".
	previewMemoryBudget = 200 << 20
	// previewFirstFrameBudget is the most the UI thread spends on one frame of the open of the preview, in the debug build the
	// display tests run: 8.3 to 9.1 ms with Glutter 1.9.4 (47 to 55 ms before 1.9.3, when Glutter's checks of the UI thread
	// read the goroutine number from runtime.Stack; the release build takes 9 ms; docs/PERFORMANCE.md "After Glutter 1.9.3").
	previewFirstFrameBudget = 20 * time.Millisecond
	// builtBudget is the most blocks a window of the preview builds (the viewport and the margin around it, with the headings,
	// paragraphs and images of the fixture, are about 20).
	builtBudget = 60
)

// startBigPreview starts the editor with the big document open as text, and returns it with the memory it holds then.
func startBigPreview(t *testing.T) (*display.App, int64) {
	t.Helper()
	a := display.Start(t, display.Options{
		Args:    []string{".", mdfixture.DocName},
		Env:     []string{"GLUTTER_FRAME_STATS=1"},
		Prepare: func(dir string) { mdfixture.Generate(t, dir) },
	})
	a.Settle()
	return a, a.ResidentBytes()
}

// preview is the only preview on screen.
func preview(t *testing.T, a *display.App) app.DevMarkdown {
	t.Helper()
	list := a.Previews()
	if len(list) != 1 {
		t.Fatalf("%d previews on screen, want 1: %+v", len(list), list)
	}
	return list[0]
}

// openBigPreview opens the preview to the side and waits for its first blocks; it returns the time that took.
func openBigPreview(t *testing.T, a *display.App) time.Duration {
	t.Helper()
	start := time.Now()
	a.Dev("emetos.markdown.openPreviewToSide", nil)
	var opened time.Duration
	a.Eventually("the preview to build its first blocks", func() (bool, string) {
		list := a.Previews()
		if len(list) == 1 && list[0].Built > 0 {
			opened = time.Since(start)
			return true, ""
		}
		return false, "no blocks yet"
	})
	return opened
}

func TestRealInputBigPreviewOpensAndScrolls(t *testing.T) {
	a, asText := startBigPreview(t)
	commanded := time.Now()
	opened := openBigPreview(t, a)
	t.Logf("the first blocks of the preview were built %v after the command", opened)
	if opened > previewOpenBudget+100*time.Millisecond {
		t.Errorf("the preview took %v to show its first blocks, want at most %v (and the 100 ms of the test's own polling)", opened, previewOpenBudget)
	}
	p := preview(t, a)
	if p.Blocks < 1000 {
		t.Fatalf("the preview has %d blocks: %+v", p.Blocks, p)
	}
	t.Logf("the preview: %+v", p)

	// Glutter logs a window of frame statistics once a second, and the first window begins with the first frame of the editor: the
	// window that is logged next holds the start of the window (the first draw, which makes the pipelines and the shaders: 4 to 5 s
	// of the renderer on llvmpipe with no shader cache, 0.9 s with the GPU) and the open of the preview. The scrolling is measured
	// from the window after it (D13 of docs/DESKTOP-PASS.md), and the open by the frames that were logged between the command
	// and here.
	before := a.WaitFrameWindow()
	if first, ok := a.FirstFrame(); ok {
		t.Logf("the first frame of the editor came %v after the start of Run", first.SinceRun)
	}
	var openBuild time.Duration
	for _, f := range a.SlowFrames() {
		if !f.Time.Before(commanded) {
			openBuild = max(openBuild, f.Build)
		}
	}
	t.Logf("the UI thread took at most %v for a frame of the open of the preview (frames over 16 ms only)", openBuild)
	if openBuild > previewFirstFrameBudget {
		t.Errorf("a frame of the open of the preview took %v of the UI thread, want at most %v", openBuild, previewFirstFrameBudget)
	}

	// scroll with the wheel over the preview
	x, y := p.X+p.Width/2, p.Y+p.Height/2
	top0 := p.ScrollTop
	for range 30 {
		a.Scroll(x, y, 0, 3)
	}
	a.Settle()
	time.Sleep(1500 * time.Millisecond)
	p = preview(t, a)
	if p.ScrollTop <= top0 || p.TopLine == 0 {
		t.Errorf("the wheel did not scroll the preview: %+v", p)
	}
	if p.Built > builtBudget {
		t.Errorf("the preview has built %d blocks, want at most %d", p.Built, builtBudget)
	}
	// a click on the track of the scroll bar, half way down, jumps there
	// the last pixels of the window are the compositor's (a wheel there is not delivered): the track is clicked 8 px from them
	a.ClickScrollbar(p.X+p.Width-8, p.Y+p.Height/2)
	time.Sleep(1500 * time.Millisecond)
	jumped := preview(t, a)
	t.Logf("after the jump: %+v", jumped)
	if jumped.ScrollTop <= p.ScrollTop || jumped.Built > builtBudget {
		t.Errorf("the track did not move the preview: %+v", jumped)
	}
	sum := display.Summarize(a.FrameWindows()[before:])
	t.Logf("scrolling and jumping: %d frames, the UI thread %v on average and %v at worst, the renderer %v and %v, %d over 16 ms",
		sum.Frames, sum.BuildAvg, sum.BuildMax, sum.RasterAvg, sum.RasterMax, sum.Over16)
	for _, line := range strings.Split(a.MainLog(), "\n") {
		if strings.Contains(line, "slow frame") {
			t.Log(line)
		}
	}
	withPreview := a.ResidentBytes()
	t.Logf("resident: %d MB as text, %d MB with the preview (%+d MB)", asText>>20, withPreview>>20, (withPreview-asText)>>20)
	if withPreview-asText > previewMemoryBudget {
		t.Errorf("the preview made the resident memory grow by %d MB, want at most %d", (withPreview-asText)>>20, previewMemoryBudget>>20)
	}
	if sum.BuildMax > 100*time.Millisecond {
		t.Errorf("a frame took %v of the UI thread", sum.BuildMax)
	}
}

// TestRealInputBigPreviewProof makes the proof image of step 17b.2: the preview of the fixture scrolled to its middle.
func TestRealInputBigPreviewProof(t *testing.T) {
	a, _ := startBigPreview(t)
	openBigPreview(t, a)
	p := preview(t, a)
	a.Scroll(p.X+p.Width/2, p.Y+p.Height/2, 0, 1)
	p = preview(t, a)
	a.ClickScrollbar(p.X+p.Width-8, p.Y+p.Height/2)
	a.Eventually("the images near the middle to be loaded", func() (bool, string) {
		cur := preview(t, a)
		return cur.Images > 0 && cur.ScrollTop > cur.ContentHeight/3, "images " + strconv.Itoa(cur.Images)
	})
	// the middle has a picture a few notches down
	a.Scroll(p.X+p.Width/2, p.Y+p.Height/2, 0, 14)
	time.Sleep(1500 * time.Millisecond)
	a.Screenshot(filepath.Join(proofDir(t), "17b.2-preview.png"))
}

// startWithPreview starts the editor on one Markdown file that opens in its preview (workbench.editor.defaultFileViews), as a
// restored session does, and returns the time of its first frame. The driver keeps its compiled shaders in mesaCache.
func startWithPreview(t *testing.T, big bool, mesaCache string) (*display.App, display.FirstFrame) {
	t.Helper()
	opts := display.Options{
		Args:   []string{".", mdfixture.DocName},
		Env:    []string{"GLUTTER_FRAME_STATS=1", "MESA_SHADER_CACHE_DIR=" + mesaCache},
		Config: map[string]string{"settings.json": `{"workbench.editor.defaultFileViews": {"*.md": "emetos.markdown.preview"}}`},
	}
	if big {
		opts.Prepare = func(dir string) { mdfixture.Generate(t, dir) }
	} else {
		opts.Project = map[string]string{mdfixture.DocName: "# Small\n\nA short document.\n"}
	}
	a := display.Start(t, opts)
	a.Settle()
	first, ok := a.FirstFrame()
	if !ok {
		t.Fatalf("the editor logged no first frame:\n%s", a.MainLog())
	}
	return a, first
}

// startBudget is the most the first frame of an editor that starts with the preview of the big document may come after
// the first frame of one that starts with the preview of a small one (SPEC §14, the Markdown preview row).
const startBudget = 150 * time.Millisecond

// warmStart is the time under which the first frame of a start is one with no compile of shaders in it (0.45 s warm, 2 to 5 s
// not), and maxWarmStarts the starts that fill the shader cache before the test gives up.
const (
	warmStart     = time.Second
	maxWarmStarts = 4
)

// startRounds is the number of starts of each kind that are measured.
const startRounds = 3

// TestRealInputBigPreviewStartDoesNotWaitForThePreview: an editor that starts with the preview of the big document (what a
// restored session does) draws its first frame as fast as one that starts with the preview of a small one.
//
// Every start of the display harness has a new XDG_CACHE_HOME, so every start is a first start for the driver, and on llvmpipe
// the compile of the shaders takes 4 to 5 s of the first frame, with about 500 ms of noise between two starts: more than the
// whole budget. The starts of this test share one Mesa shader cache (MESA_SHADER_CACHE_DIR) in a folder of the test, which a
// first start that is not measured fills, so the driver's compile is out of every measured start (the first frame takes about
// 0.3 s then). The starts alternate between the two kinds, and the kinds are compared by their fastest start, so that a start
// that the machine slowed down does not count.
func TestRealInputBigPreviewStartDoesNotWaitForThePreview(t *testing.T) {
	mesaCache := t.TempDir()
	// The first start fills the cache only in part (the first start after it still takes about 2 s, the ones after it 0.45 s),
	// so the cache is filled until a start is fast.
	for attempt := 1; ; attempt++ {
		var warm bool
		t.Run("warm the shader cache "+strconv.Itoa(attempt), func(t *testing.T) {
			_, first := startWithPreview(t, false, mesaCache)
			t.Logf("first frame of a start that fills the cache: %v since Run", first.SinceRun)
			warm = first.SinceRun < warmStart
		})
		if warm {
			break
		}
		if attempt == maxWarmStarts {
			t.Fatalf("the first frame still took more than %v after %d starts with the same shader cache", warmStart, attempt)
		}
	}
	var small, bigTimes []time.Duration
	for round := range startRounds {
		// the order alternates, so that neither kind always runs second (on a machine that warms up or slows down)
		order := []bool{false, true}
		if round%2 == 1 {
			order = []bool{true, false}
		}
		for _, big := range order {
			name := "small preview"
			if big {
				name = "big preview"
			}
			t.Run(name+" "+strconv.Itoa(round+1), func(t *testing.T) {
				a, first := startWithPreview(t, big, mesaCache)
				t.Logf("first frame: %v since Run (%v since the process started)", first.SinceRun, first.SinceProcessStart)
				if !big {
					small = append(small, first.SinceRun)
					return
				}
				bigTimes = append(bigTimes, first.SinceRun)
				a.Eventually("the preview to build its first blocks", func() (bool, string) {
					list := a.Previews()
					return len(list) == 1 && list[0].Built > 0, "no blocks yet"
				})
				if p := preview(t, a); p.Blocks < 1000 || p.Built > builtBudget {
					t.Errorf("the preview: %+v", p)
				}
			})
		}
	}
	if len(small) != startRounds || len(bigTimes) != startRounds {
		t.Fatalf("%d starts with the small preview and %d with the big one were measured, want %d of each", len(small), len(bigTimes), startRounds)
	}
	fastestSmall, fastestBig := slices.Min(small), slices.Min(bigTimes)
	t.Logf("first frames with the small preview: %v, with the big one: %v", small, bigTimes)
	if extra := fastestBig - fastestSmall; extra > startBudget {
		t.Errorf("the fastest first frame with the big preview took %v (%v) more than the fastest with the small one (%v), want at most %v",
			extra, fastestBig, fastestSmall, startBudget)
	}
}
