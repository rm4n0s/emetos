//go:build emetos_display

package main

import (
	"path/filepath"
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

	// scroll with the wheel over the preview
	x, y := p.X+p.Width/2, p.Y+p.Height/2
	before := len(a.FrameWindows())
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
// restored session does, and returns the time of its first frame.
func startWithPreview(t *testing.T, big bool) (*display.App, display.FirstFrame) {
	t.Helper()
	opts := display.Options{
		Args:   []string{".", mdfixture.DocName},
		Env:    []string{"GLUTTER_FRAME_STATS=1"},
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

// TestRealInputBigPreviewStartDoesNotWaitForThePreview: an editor that starts with the preview of the big document (what a
// restored session does) draws its first frame as fast as one that starts with the preview of a small one.
func TestRealInputBigPreviewStartDoesNotWaitForThePreview(t *testing.T) {
	_, small := startWithPreview(t, false)
	a, big := startWithPreview(t, true)
	t.Logf("first frame: %v since Run (%v since the process started) with the small preview, %v (%v) with the big one", small.SinceRun, small.SinceProcessStart, big.SinceRun, big.SinceProcessStart)
	if extra := big.SinceRun - small.SinceRun; extra > 150*time.Millisecond {
		t.Errorf("the first frame with the big preview took %v more than with the small one", extra)
	}
	a.Eventually("the preview to build its first blocks", func() (bool, string) {
		list := a.Previews()
		return len(list) == 1 && list[0].Built > 0, "no blocks yet"
	})
	if p := preview(t, a); p.Blocks < 1000 || p.Built > builtBudget {
		t.Errorf("the preview: %+v", p)
	}
}
