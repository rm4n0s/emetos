package app

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/rm4n0s/errors"

	"github.com/rm4n0s/emetos/extension"
	"github.com/rm4n0s/emetos/internal/gui/testutil"
)

// An extension is activated before the shell starts the watcher of the window; the Git extension asked for its watch in between and
// was refused, so the files changed outside Emetos never refreshed the status (the user, 2026-11-13: the Explorer did not color the
// files another program changed, "unless the user change tabs").
func TestAnExtensionWatchAskedBeforeTheWatcherStartsWaitsForIt(t *testing.T) {
	folder := t.TempDir()
	paths := testutil.Isolate(t)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	win, err := NewWorkspace(ctx, newTestProcess(ProcessOptions{Paths: paths, WatchFiles: true}), folder)
	if err != nil {
		t.Fatal(err)
	}
	got := make(chan []extension.FileChange, 10)
	type result struct {
		stop func()
		err  error
	}
	asked := make(chan result, 1)
	go func() {
		stop, err := win.extensionWatch(ctx, extension.WatchSpec{Path: folder, Recursive: true}, func(c []extension.FileChange) { got <- c })
		asked <- result{stop, err}
	}()
	select {
	case r := <-asked:
		t.Fatalf("the watch was answered before the watcher started: %v", r.err)
	case <-time.After(100 * time.Millisecond):
	}
	ready := win.StartWatching(ctx)
	t.Cleanup(func() {
		cancel()
		if w := win.Watcher(); w != nil {
			<-w.Done()
		}
	})
	var r result
	select {
	case r = <-asked:
	case <-time.After(5 * time.Second):
		t.Fatal("the watch still waits after the watcher started")
	}
	if r.err != nil {
		t.Fatalf("the watch failed: %v", r.err)
	}
	defer r.stop()
	<-ready
	file := filepath.Join(folder, "late.txt")
	if err := os.WriteFile(file, []byte("late"), 0o644); err != nil {
		t.Fatal(err)
	}
	deadline := time.After(5 * time.Second)
	for {
		select {
		case changes := <-got:
			for _, c := range changes {
				if c.Path == file {
					return
				}
			}
		case <-deadline:
			t.Fatal("the change of a file made after the start was not told")
		}
	}
}

func TestAnExtensionWatchThatCannotHaveAWatcher(t *testing.T) {
	folder := t.TempDir()
	tests := []struct {
		name  string
		watch bool
		ctx   func() context.Context
		route string
	}{
		{"a window that never watches files answers at once", false, context.Background, "Workspace.extensionWatch.ExtensionWatchUnavailable"},
		{"a wait that is cancelled", true, func() context.Context {
			ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
			_ = cancel
			return ctx
		}, "Workspace.extensionWatch.ExtensionWatchCancelled"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			paths := testutil.Isolate(t)
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			win, err := NewWorkspace(ctx, newTestProcess(ProcessOptions{Paths: paths, WatchFiles: tt.watch}), folder)
			if err != nil {
				t.Fatal(err)
			}
			done := make(chan error, 1)
			go func() {
				_, err := win.extensionWatch(tt.ctx(), extension.WatchSpec{Path: folder}, func([]extension.FileChange) {})
				done <- err
			}()
			select {
			case err = <-done:
			case <-time.After(5 * time.Second):
				t.Fatal("the watch did not answer")
			}
			appErr, ok := errors.FromError(err)
			if !ok {
				t.Fatalf("expected *errors.Error, got %T (%v)", err, err)
			}
			if !appErr.HasRoute(tt.route) {
				t.Errorf("unexpected failure path: %s", appErr.Route())
			}
		})
	}
}
