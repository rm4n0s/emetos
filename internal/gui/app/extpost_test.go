package app

import (
	"context"
	"slices"
	"sync/atomic"
	"testing"
	"time"

	"github.com/rm4n0s/glutter/ui"

	"github.com/rm4n0s/emetos/extension"
	"github.com/rm4n0s/emetos/internal/gui/testutil"
)

// The program makes its workspaces, and starts their extensions, before the binding of the first frame exists. What an extension
// hands to the UI thread from a goroutine of its own in between (Git, once it has found its program) waits for that thread: it used
// to run on the goroutine that posted it, beside the others and beside the start.

func TestWhatAnExtensionPostsWhileTheProgramStartsWaitsForTheUIThread(t *testing.T) {
	env := newMultiEnv(t, nil)
	root := env.folder("project", "main.go")
	var ran atomic.Int32
	var order []int // written by the posted functions only
	posted := make(chan struct{})
	factory := func() extension.Extension {
		return &testExtension{
			manifest: extension.Manifest{ID: "emetos.posttest", Name: "Post test", ActivationEvents: []string{extension.OnStartup}},
			activate: func(_ context.Context, api extension.API) error {
				go func() {
					for i := range 3 {
						api.Post(func() {
							order = append(order, i)
							ran.Add(1)
						})
					}
					close(posted)
				}()
				return nil
			},
		}
	}
	p := newTestProcess(ProcessOptions{Paths: env.paths, StateAfter: env.clock.AfterFunc, Extensions: []extension.Factory{factory}})
	m := p.Workspaces
	m.ctx = t.Context()
	m.Quit = func() {}
	shown, err := m.start(t.Context(), Options{Folder: root})
	if err != nil {
		t.Fatal(err)
	}
	shown.steps = nil
	select {
	case <-posted:
	case <-time.After(20 * time.Second):
		t.Fatal("the extension did not post")
	}
	if n := ran.Load(); n != 0 {
		t.Fatalf("%d of the functions ran before there was a UI thread", n)
	}
	ts := testutil.New(t, ui.Size{Width: 1000, Height: 700})
	ts.PumpWidget(m.hostWidget())
	for range 8 {
		ts.Pump(20 * time.Millisecond)
	}
	if !slices.Equal(order, []int{0, 1, 2}) {
		t.Errorf("the functions ran in the order %v once the window was shown, want 0 1 2", order)
	}
	// from now on a post goes to the binding at once
	done := make(chan struct{})
	go func() {
		shown.ws.postFromExtension(func() { order = append(order, 3) })
		close(done)
	}()
	<-done
	ts.Pump(20 * time.Millisecond)
	if !slices.Equal(order, []int{0, 1, 2, 3}) {
		t.Errorf("a function posted after the start: the order is %v", order)
	}
}
