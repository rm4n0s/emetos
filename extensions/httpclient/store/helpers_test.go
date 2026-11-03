package store

import (
	"context"
	"net/url"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/rm4n0s/errors"

	"github.com/rm4n0s/emetos/extension"
)

// wantRoute checks that err is an *errors.Error whose route contains route.
func wantRoute(t *testing.T, err error, route string) {
	t.Helper()
	appErr, ok := errors.FromError(err)
	if !ok {
		t.Fatalf("expected *errors.Error, got %T (%v)", err, err)
	}
	if !appErr.HasRoute(route) {
		t.Fatalf("unexpected failure path %s, want it to contain %s", appErr.Route(), route)
	}
}

// fakeWatch is a watch the fake host placed.
type fakeWatch struct {
	spec   extension.WatchSpec
	fn     func([]extension.FileChange)
	active bool
}

// fakeHost is a Host over a temporary folder: a watcher the test fires by hand and a trash that is a folder.
type fakeHost struct {
	t        *testing.T
	trashDir string

	mu      sync.Mutex
	watches []*fakeWatch
	placed  chan extension.WatchSpec
	failErr error
}

func newFakeHost(t *testing.T) *fakeHost {
	return &fakeHost{t: t, trashDir: t.TempDir(), placed: make(chan extension.WatchSpec, 32)}
}

func (h *fakeHost) Watch(_ context.Context, spec extension.WatchSpec, fn func([]extension.FileChange)) (func(), error) {
	h.mu.Lock()
	defer h.mu.Unlock()
	if h.failErr != nil {
		h.placed <- spec
		return nil, h.failErr
	}
	w := &fakeWatch{spec: spec, fn: fn, active: true}
	h.watches = append(h.watches, w)
	h.placed <- spec
	return func() {
		h.mu.Lock()
		defer h.mu.Unlock()
		w.active = false
	}, nil
}

// fire reports a change of the path (absolute) to the watches that are active.
func (h *fakeHost) fire(path string) {
	h.mu.Lock()
	var fns []func([]extension.FileChange)
	for _, w := range h.watches {
		if w.active {
			fns = append(fns, w.fn)
		}
	}
	h.mu.Unlock()
	for _, fn := range fns {
		fn([]extension.FileChange{{Kind: extension.FileChanged, Path: path}})
	}
}

// active returns the specs of the active watches.
func (h *fakeHost) active() []extension.WatchSpec {
	h.mu.Lock()
	defer h.mu.Unlock()
	var out []extension.WatchSpec
	for _, w := range h.watches {
		if w.active {
			out = append(out, w.spec)
		}
	}
	return out
}

// nextWatch waits for the next watch to be placed.
func (h *fakeHost) nextWatch() extension.WatchSpec {
	h.t.Helper()
	select {
	case spec := <-h.placed:
		return spec
	case <-time.After(5 * time.Second):
		h.t.Fatal("no watch was placed")
		return extension.WatchSpec{}
	}
}

// TrashFiles moves the files into the trash folder, and returns the function that moves them back.
func (h *fakeHost) TrashFiles(_ context.Context, uris []string) (func(context.Context) error, error) {
	type moved struct{ from, to string }
	var done []moved
	for _, raw := range uris {
		u, err := url.Parse(raw)
		if err != nil {
			return nil, err
		}
		to := filepath.Join(h.trashDir, filepath.Base(u.Path)+"-"+time.Now().Format("150405.000000000"))
		if err := os.Rename(u.Path, to); err != nil {
			return nil, err
		}
		done = append(done, moved{u.Path, to})
	}
	return func(context.Context) error {
		for _, m := range done {
			if err := os.Rename(m.to, m.from); err != nil {
				return err
			}
		}
		return nil
	}, nil
}

func write(t *testing.T, path, text string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(text), 0o644); err != nil {
		t.Fatal(err)
	}
}

func read(t *testing.T, path string) string {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return string(data)
}

// open opens a store over a new folder with a fake host.
func open(t *testing.T) (*Store, *fakeHost, string) {
	t.Helper()
	folder := t.TempDir()
	h := newFakeHost(t)
	s, err := Open(context.Background(), folder, h, Options{})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(s.Close)
	return s, h, folder
}

// keys lists the keys of the children of a node.
func keys(n *Node) []string {
	var out []string
	for _, c := range n.Children {
		out = append(out, c.Key)
	}
	return out
}

// changes returns a channel that gets a value for each call of a listener of the store.
func changes(s *Store) chan struct{} {
	ch := make(chan struct{}, 64)
	s.OnChange(func() { ch <- struct{}{} })
	return ch
}

func waitChange(t *testing.T, ch chan struct{}) {
	t.Helper()
	select {
	case <-ch:
	case <-time.After(5 * time.Second):
		t.Fatal("the store did not report a change")
	}
}

func cancelled() (context.Context, context.CancelFunc) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	return ctx, cancel
}
