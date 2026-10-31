package fakeapi

import (
	"context"
	"path/filepath"
	"slices"
	"strings"

	"github.com/rm4n0s/errors"

	"github.com/rm4n0s/emetos/extension"
)

// FakeWatch is a call of Workspace().Watch that is still on: a test says what changed in it with Fire.
type FakeWatch struct {
	a       *API
	spec    extension.WatchSpec
	fn      func([]extension.FileChange)
	stopped bool
}

// Watch implements extension.Workspace with the errors of the host: ExtensionDeactivated and ExtensionWatchPathInvalid. The
// fake watches nothing: a test calls Fire.
func (w workspace) Watch(_ context.Context, spec extension.WatchSpec, fn func([]extension.FileChange)) (func(), error) {
	a := w.a
	a.mu.Lock()
	defer a.mu.Unlock()
	if a.scm.dead {
		return nil, errors.New("ExtensionDeactivated", "the extension was deactivated")
	}
	if fn == nil || !filepath.IsAbs(spec.Path) {
		return nil, errors.New("ExtensionWatchPathInvalid", "a watch needs an absolute path and a function that gets the changes", "path", spec.Path)
	}
	if a.watchErr != nil {
		return nil, a.watchErr
	}
	spec.Path = filepath.Clean(spec.Path)
	fw := &FakeWatch{a: a, spec: spec, fn: fn}
	a.watches = append(a.watches, fw)
	return func() {
		a.mu.Lock()
		defer a.mu.Unlock()
		fw.stopped = true
	}, nil
}

// FailWatches makes the next calls of Workspace().Watch fail with err (nil makes them work again).
func (a *API) FailWatches(err error) {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.watchErr = err
}

// Watches are the watches that are on, in the order they were made.
func (a *API) Watches() []*FakeWatch {
	a.mu.Lock()
	defer a.mu.Unlock()
	var out []*FakeWatch
	for _, w := range a.watches {
		if !w.stopped && !a.scm.dead {
			out = append(out, w)
		}
	}
	return out
}

// Path is the folder the watch is on, Recursive whether it is recursive and Excludes what it leaves out.
func (w *FakeWatch) Path() string { return w.spec.Path }

// Recursive says whether the watch goes below the folder.
func (w *FakeWatch) Recursive() bool { return w.spec.Recursive }

// Excludes are the exclude globs of the watch.
func (w *FakeWatch) Excludes() []string { return slices.Clone(w.spec.Excludes) }

// Stopped says whether the extension stopped the watch.
func (w *FakeWatch) Stopped() bool {
	w.a.mu.Lock()
	defer w.a.mu.Unlock()
	return w.stopped
}

// Fire reports changes to the extension, on its UI thread (when the test pumps), as the host does with the changes of one turn.
// Paths are absolute; a change that the watch's excludes cover is dropped, as the real watcher drops it.
func (w *FakeWatch) Fire(changes ...extension.FileChange) {
	a := w.a
	a.mu.Lock()
	if w.stopped || a.scm.dead {
		a.mu.Unlock()
		return
	}
	var keep []extension.FileChange
	for _, c := range changes {
		if !w.excluded(c.Path) {
			keep = append(keep, c)
		}
	}
	fn := w.fn
	a.mu.Unlock()
	if len(keep) == 0 {
		return
	}
	a.Post(func() {
		a.mu.Lock()
		dead := w.stopped || a.scm.dead
		a.mu.Unlock()
		if !dead {
			fn(keep)
		}
	})
}

// excluded reports whether an exclude glob of the watch covers the path: the pattern (parts joined by "/") matches the first parts
// of the path below the folder, or, when it starts with "**/", any run of parts. A path below a match is covered too.
func (w *FakeWatch) excluded(path string) bool {
	rel, err := filepath.Rel(w.spec.Path, path)
	if err != nil || rel == "." || !filepath.IsLocal(rel) {
		return false
	}
	parts := strings.Split(filepath.ToSlash(rel), "/")
	for _, ex := range w.spec.Excludes {
		pattern, anywhere := strings.CutPrefix(ex, "**/")
		want := strings.Split(pattern, "/")
		for start := 0; start+len(want) <= len(parts); start++ {
			if start > 0 && !anywhere {
				break
			}
			hit := true
			for i, wp := range want {
				if ok, _ := filepath.Match(wp, parts[start+i]); !ok {
					hit = false
					break
				}
			}
			if hit {
				return true
			}
		}
	}
	return false
}
