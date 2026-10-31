package exthost

import (
	"context"
	"testing"

	"github.com/rm4n0s/errors"

	"github.com/rm4n0s/emetos/extension"
)

func TestWatchBatchesTheChangesOfATurnOfTheUIThread(t *testing.T) {
	st := newServerTest(t, simple(), nil)
	tb := st.startTab("one")
	tb.deferred = true // Post queues until pump, as the UI thread does between two turns
	var got [][]extension.FileChange
	cancel, err := st.api.Workspace().Watch(context.Background(), extension.WatchSpec{Path: "/repo/.git", Recursive: true, Excludes: []string{"objects"}},
		func(c []extension.FileChange) { got = append(got, c) })
	if err != nil {
		t.Fatal(err)
	}
	if len(tb.watches) != 1 || tb.watches[0].spec.Path != "/repo/.git" || !tb.watches[0].spec.Recursive || tb.watches[0].spec.Excludes[0] != "objects" {
		t.Fatalf("watches = %+v", tb.watches)
	}
	w := tb.watches[0]
	for i := 0; i < 1000; i++ { // a burst of events from the goroutine of the watcher
		w.sink([]extension.FileChange{{Kind: extension.FileChanged, Path: "/repo/.git/index"}})
	}
	if len(got) != 0 {
		t.Fatal("the changes were delivered before the UI thread's turn")
	}
	tb.pump()
	if len(got) != 1 || len(got[0]) != 1000 {
		t.Fatalf("one batch of 1000 changes wanted, got %d batches", len(got))
	}
	w.sink([]extension.FileChange{{Kind: extension.FileCreated, Path: "/repo/.git/HEAD.lock"}})
	tb.pump()
	if len(got) != 2 || got[1][0].Path != "/repo/.git/HEAD.lock" {
		t.Errorf("the next turn's batch: %+v", got)
	}
	// cancel stops the delivery of what is pending too
	w.sink([]extension.FileChange{{Kind: extension.FileChanged, Path: "/repo/.git/index"}})
	cancel()
	cancel() // twice does nothing
	tb.pump()
	if len(got) != 2 || !w.stopped {
		t.Errorf("after cancel: %d batches, stopped %v", len(got), w.stopped)
	}
}

func TestWatchEndsWithTheActivation(t *testing.T) {
	st := newServerTest(t, simple(), nil)
	tb := st.startTab("one")
	delivered := 0
	if _, err := st.api.Workspace().Watch(context.Background(), extension.WatchSpec{Path: "/repo"}, func([]extension.FileChange) { delivered++ }); err != nil {
		t.Fatal(err)
	}
	st.rig.host.Shutdown()
	if !tb.watches[0].stopped {
		t.Error("the watch was not stopped with the activation")
	}
	tb.watches[0].sink([]extension.FileChange{{Kind: extension.FileChanged, Path: "/repo/a"}})
	tb.pump()
	if delivered != 0 {
		t.Errorf("%d deliveries after the activation ended", delivered)
	}
}

func TestWatchRefusals(t *testing.T) {
	tests := []struct {
		name  string
		setup func(st *serverTest, tb *tab)
		spec  extension.WatchSpec
		fn    func([]extension.FileChange)
		route string
	}{
		{"relative path", nil, extension.WatchSpec{Path: "repo"}, func([]extension.FileChange) {}, "workspaceAPI.Watch.ExtensionWatchPathInvalid"},
		{"no function", nil, extension.WatchSpec{Path: "/repo"}, nil, "workspaceAPI.Watch.ExtensionWatchPathInvalid"},
		{"no watcher", func(st *serverTest, _ *tab) { st.rig.noWatch["one"] = true }, extension.WatchSpec{Path: "/repo"}, func([]extension.FileChange) {}, "workspaceAPI.Watch.ExtensionWatchUnavailable"},
		{"the window fails", func(_ *serverTest, tb *tab) { tb.watchErr = errors.New("WatcherPathNotFound", "no folder") }, extension.WatchSpec{Path: "/repo"}, func([]extension.FileChange) {}, "WatcherPathNotFound"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			st := newServerTest(t, simple(), nil)
			if tc.setup != nil && tc.name == "no watcher" {
				tc.setup(st, nil)
			}
			tb := st.startTab("one")
			if tc.setup != nil && tc.name != "no watcher" {
				tc.setup(st, tb)
			}
			_, err := st.api.Workspace().Watch(context.Background(), tc.spec, tc.fn)
			wantRoute(t, err, tc.route)
		})
	}
	st := newServerTest(t, simple(), nil)
	st.startTab("one")
	st.rig.host.Shutdown()
	_, err := st.api.Workspace().Watch(context.Background(), extension.WatchSpec{Path: "/repo"}, func([]extension.FileChange) {})
	wantRoute(t, err, "workspaceAPI.Watch.ExtensionDeactivated")
}

func TestAWatchCallbackThatPanicsFailsTheExtension(t *testing.T) {
	st := newServerTest(t, simple(), nil)
	tb := st.startTab("one")
	if _, err := st.api.Workspace().Watch(context.Background(), extension.WatchSpec{Path: "/repo"}, func([]extension.FileChange) { panic("boom") }); err != nil {
		t.Fatal(err)
	}
	tb.watches[0].sink([]extension.FileChange{{Kind: extension.FileChanged, Path: "/repo/a"}})
	tb.pump()
	waitFor(t, tb, "the extension to fail", func() bool { return tb.scope.State(testExt) == StateFailed })
	appErr, ok := errors.FromError(tb.scope.Failure(testExt).Err)
	if !ok || appErr.Tag != "ExtensionWatchCallbackPanicked" || !appErr.HasRoute("watchBatch.deliver.func1.ExtensionWatchCallbackPanicked") {
		t.Errorf("failure = %v", tb.scope.Failure(testExt).Err)
	}
}
