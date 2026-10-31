package exthost

import (
	"context"
	"path/filepath"
	"sync"

	"github.com/rm4n0s/errors"

	"github.com/rm4n0s/emetos/extension"
)

// Watch implements extension.Workspace (SPEC §10.30 "Refresh"): it asks the window to watch the folder and hands the changes to
// fn on the UI thread, a batch for each turn of that thread, however many the watcher reported in between.
func (w workspaceAPI) Watch(ctx context.Context, spec extension.WatchSpec, fn func([]extension.FileChange)) (func(), error) {
	in := w.inst
	if in.isDead() {
		return nil, errors.New("ExtensionDeactivated", "the extension was deactivated", "extension", in.id)
	}
	if fn == nil || !filepath.IsAbs(spec.Path) {
		return nil, errors.New("ExtensionWatchPathInvalid", "a watch needs an absolute path and a function that gets the changes", "extension", in.id, "path", spec.Path)
	}
	watch := in.scope.opts.Watch
	if watch == nil {
		return nil, errors.New("ExtensionWatchUnavailable", "the scope has no watcher", "extension", in.id)
	}
	spec.Path = filepath.Clean(spec.Path)
	b := &watchBatch{inst: in, fn: fn}
	stop, err := watch(ctx, spec, b.add)
	if err != nil {
		return nil, err
	}
	var once sync.Once
	cancel := func() {
		once.Do(func() {
			b.stop()
			stop()
		})
	}
	if !in.track(cancel) {
		return nil, errors.New("ExtensionDeactivated", "the extension was deactivated", "extension", in.id)
	}
	return cancel, nil
}

// watchBatch gathers the changes of one watch until the UI thread's next turn.
type watchBatch struct {
	inst *instance
	fn   func([]extension.FileChange)

	mu      sync.Mutex
	pending []extension.FileChange
	posted  bool
	stopped bool
}

// add is the sink the window calls from the goroutine of its watcher.
func (b *watchBatch) add(changes []extension.FileChange) {
	b.mu.Lock()
	if b.stopped {
		b.mu.Unlock()
		return
	}
	b.pending = append(b.pending, changes...)
	post := !b.posted
	b.posted = true
	b.mu.Unlock()
	if post {
		b.inst.scope.post(b.deliver)
	}
}

// deliver runs on the UI thread: it gives the extension what has gathered. A panic fails the extension with
// ExtensionWatchCallbackPanicked.
func (b *watchBatch) deliver() {
	b.mu.Lock()
	changes, stopped := b.pending, b.stopped
	b.pending, b.posted = nil, false
	b.mu.Unlock()
	in := b.inst
	if stopped || len(changes) == 0 || in.isDead() {
		return
	}
	defer func() {
		if r := recover(); r != nil {
			failure := errors.NewErrf("ExtensionWatchCallbackPanicked", "it panicked in the function that gets the changes of a watch: %v", r).SetMetadata("extension", in.id, "scope", in.scope.opts.Info.Name)
			in.scope.fail(in.slot, failure, "watch")
		}
	}()
	b.fn(changes)
}

func (b *watchBatch) stop() {
	b.mu.Lock()
	b.stopped, b.pending = true, nil
	b.mu.Unlock()
}
