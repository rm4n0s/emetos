package app

import (
	"path/filepath"
	"testing"
	"time"

	"github.com/rm4n0s/emetos/internal/core/logs"
	"github.com/rm4n0s/emetos/internal/core/settings"
	"github.com/rm4n0s/emetos/internal/gui/editor"
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
