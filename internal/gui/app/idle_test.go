package app

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/rm4n0s/emetos/internal/core/document"
	"github.com/rm4n0s/emetos/internal/core/logs"
	"github.com/rm4n0s/emetos/internal/core/settings"
	"github.com/rm4n0s/emetos/internal/gui/editor"
	"github.com/rm4n0s/emetos/internal/gui/testutil"
)

// TestIdleWorkbenchDrawsOnlyTheBlink pins the idle cost of the whole window (SPEC §14, under 1% of
// a core): with a file open and the editor focused, the only frames are those of the blinking cursor,
// one each time it is shown or hidden.
func TestIdleWorkbenchDrawsOnlyTheBlink(t *testing.T) {
	a := newKeyApp(t, "alpha\nbeta\n")
	a.focusEditor()
	a.ts.Pump(3 * time.Second) // settles what the opening started
	sched := a.ts.Binding.Scheduler()
	const seconds = 20
	frames := 0
	sched.AddPersistentFrameCallback(func(time.Duration) { frames++ })
	for i := range seconds * 100 { // every 10 ms of the fake clock
		if i%100 == 0 {
			// a line of the log every second (the frame statistics of a measured run are one): the Output
			// panel is closed, so it must not draw frames to follow the tail
			a.win.Process.Channels.Append(logs.MainChannel, "a line of the log")
		}
		a.ts.Pump(10 * time.Millisecond)
	}
	t.Logf("%d frames in %d s", frames, seconds)
	if want := seconds * int(time.Second/editor.BlinkInterval); frames > want+1 {
		t.Errorf("%d frames in %d s, want at most %d (one each time the cursor is shown or hidden)", frames, seconds, want+1)
	}
}

// TestStartWarmsTheGrammarOfTheFilesOfTheCommandLine pins that the rules of the grammar of a file that opens
// with the window are made on a goroutine before the first frame needs them (step 10.1: Markdown takes 60 ms).
func TestStartWarmsTheGrammarOfTheFilesOfTheCommandLine(t *testing.T) {
	env := newMultiEnv(t, nil)
	dir := env.folder("one", "a.go", "notes.md")
	// no window is shown: nothing tokenizes, so only the warming can make the rules
	p := newTestProcess(ProcessOptions{Paths: env.paths, StateAfter: env.clock.AfterFunc})
	p.Workspaces.ctx = t.Context()
	shown, err := p.Workspaces.start(t.Context(), Options{Folder: dir, Files: []string{filepath.Join(dir, "a.go")}})
	if err != nil {
		t.Fatal(err)
	}
	maxLine := func(lang string) int {
		return int(settings.Get[float64](shown.ws.Settings, "editor.maxTokenizationLineLength", lang))
	}
	g, err := p.Grammar("go", maxLine("go"))
	if err != nil || g == nil {
		t.Fatalf("Grammar: %v, %v", g, err)
	}
	deadline := time.Now().Add(10 * time.Second)
	for !g.Ready() {
		if time.Now().After(deadline) {
			t.Fatal("the rules of the Go grammar were not made")
		}
		time.Sleep(time.Millisecond)
	}
	if md, _ := p.Grammar("markdown", maxLine("markdown")); md != nil && md.Ready() {
		t.Error("the grammar of a file nobody opened was warmed")
	}
}

// Budget of one blink frame of an idle workbench (SPEC §14 "Idle allocation"): what the UI thread allocates between the
// start of the frame and the end of its paint, with a 10,000-line Go file open, highlighted, with the editor focused.
// Measured in step 17b.4 after the fixes: 4.6 KB and 92 allocations a frame (4.8 KB and 94 under the race detector); before,
// 8.0 KB and 147 allocations, of which 3.4 KB and 57 allocations were the layer of the cursors reading the text of every
// visible row to look for control characters. The budgets are 1.3 times the measurement: a regression of that size (a page
// of rows read again, or a widget tree built, on every blink) does not pass. The rest is Glutter's scene builder, which adds
// every retained layer to the scene again.
//
// What the renderer allocates for the same frame is Glutter's (about 2 MB a frame headless with Glutter 1.9.1, 160 KB with
// 1.9.2); no widget test sees it, because the tester has no renderer: it is measured by scripts/perf.sh --profile and
// recorded in docs/PERFORMANCE.md.
const (
	idleBlinkFrameBytes  = 6 << 10
	idleBlinkFrameAllocs = 120
)

// TestIdleBlinkFrameAllocationStaysInBudget pins what a blink frame of the cursor allocates in the whole workbench.
func TestIdleBlinkFrameAllocationStaysInBudget(t *testing.T) {
	var src strings.Builder
	src.WriteString("package main\n\n")
	for i := range 10000 {
		switch i % 4 {
		case 0:
			fmt.Fprintf(&src, "func handler%d(ctx context.Context, request *Request) error {\n", i)
		case 1:
			fmt.Fprintf(&src, "\tresult, err := service.Process(ctx, request.Items[%d])\n", i%64)
		case 2:
			src.WriteString("\tif err != nil { return fmt.Errorf(\"processing: %w\", err) }\n")
		default:
			src.WriteString("}\n")
		}
	}
	win := newTestWorkspace(t, "")
	path := filepath.Join(t.TempDir(), "big.go")
	if err := os.WriteFile(path, []byte(src.String()), 0o644); err != nil {
		t.Fatal(err)
	}
	doc, err := win.Documents.OpenFile(context.Background(), path, document.NewOptions())
	if err != nil {
		t.Fatal(err)
	}
	ts := testutil.New(t, testWindowSize)
	ts.PumpWidget(newApp(context.Background(), win, doc, nil))
	ts.Pump(10 * time.Millisecond)
	ed := win.shell.activeEditor()
	if ed == nil || !ed.focus.Request() {
		t.Fatal("the editor cannot take the focus")
	}
	ts.Pump(time.Millisecond)
	if ed.tokenizer == nil {
		t.Fatal("the editor has no tokenizer: the file would not be highlighted")
	}
	// the lines on the screen are tokenized by the first frames; the background tokenizer of the rest of the file allocates
	// on its own goroutine until it is done (tens of seconds under the race detector), which is not the idle window: stop it
	for range 5 {
		ts.Pump(time.Second)
	}
	close(ed.tokDone)                                    // ends the loop of the background tokenizer (as a change of language does)
	for slices := -1; ed.tokenizer.Slices() != slices; { // a slice that is running finishes
		slices = ed.tokenizer.Slices()
		time.Sleep(50 * time.Millisecond)
	}
	ts.Pump(time.Second)
	if len(ed.tokenizer.Line(5)) == 0 {
		t.Fatal("the lines on the screen have no tokens: the measurement would be of a plain text file")
	}

	const frames = 100
	var bytesPerFrame, allocsPerFrame uint64
	for round := range 3 { // the smallest of three: other goroutines that allocate meanwhile only add to a round
		runtime.GC()
		var before, after runtime.MemStats
		scenes := len(ts.Scenes())
		runtime.ReadMemStats(&before)
		for range frames {
			ts.Pump(editor.BlinkInterval)
		}
		runtime.ReadMemStats(&after)
		if got := len(ts.Scenes()) - scenes; got != frames {
			t.Fatalf("%d frames in %d blinks, want one each", got, frames)
		}
		b, a := (after.TotalAlloc-before.TotalAlloc)/frames, (after.Mallocs-before.Mallocs)/frames
		if round == 0 || b < bytesPerFrame {
			bytesPerFrame = b
		}
		if round == 0 || a < allocsPerFrame {
			allocsPerFrame = a
		}
	}
	t.Logf("one blink frame: %d bytes, %d allocations (budget %d and %d)", bytesPerFrame, allocsPerFrame, idleBlinkFrameBytes, idleBlinkFrameAllocs)
	if bytesPerFrame > idleBlinkFrameBytes {
		t.Errorf("a blink frame allocates %d bytes, budget %d", bytesPerFrame, idleBlinkFrameBytes)
	}
	if allocsPerFrame > idleBlinkFrameAllocs {
		t.Errorf("a blink frame makes %d allocations, budget %d", allocsPerFrame, idleBlinkFrameAllocs)
	}
	if errs := ts.TakeErrors(); len(errs) != 0 {
		t.Errorf("the framework reported errors: %v", errs)
	}
}
