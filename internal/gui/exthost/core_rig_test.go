package exthost

import (
	"context"
	"os"
	"path/filepath"
	"sync"
	"syscall"
	"testing"

	"github.com/rm4n0s/errors"

	"github.com/rm4n0s/emetos/extension"
	"github.com/rm4n0s/emetos/internal/core/command"
	"github.com/rm4n0s/emetos/internal/core/document"
)

// fakeCoreDocs is the window's side of the document topics over real documents: the files it opens are read from disk, and
// it records what the scope asked of it.
type fakeCoreDocs struct {
	*docSource
	mu   sync.Mutex
	docs map[document.URI]*document.Document

	opened, saved, closed, revealed []string
	revealAt                        []extension.Position
	openErr, saveErr                error
	// saveHangs makes Save never answer.
	saveHangs  bool
	began, end int
}

func newFakeCoreDocs(src *docSource) *fakeCoreDocs {
	return &fakeCoreDocs{docSource: src, docs: map[document.URI]*document.Document{}}
}

// add makes a document open.
func (f *fakeCoreDocs) add(uri document.URI, text string) *document.Document {
	f.mu.Lock()
	defer f.mu.Unlock()
	d := document.New(uri, text, document.NewOptions())
	f.docs[uri] = d
	return d
}

func (f *fakeCoreDocs) Document(uri document.URI) (*document.Document, bool) {
	f.mu.Lock()
	defer f.mu.Unlock()
	d, ok := f.docs[uri]
	return d, ok
}

func (f *fakeCoreDocs) OpenFile(_ context.Context, path string, pos extension.Position, done func(*document.Document, error)) {
	f.mu.Lock()
	f.opened = append(f.opened, path)
	f.revealAt = append(f.revealAt, pos)
	err := f.openErr
	f.mu.Unlock()
	if err != nil {
		done(nil, err)
		return
	}
	data, rerr := os.ReadFile(path)
	if rerr != nil {
		done(nil, errors.NewErr("FakeOpenFailed", rerr))
		return
	}
	done(f.add(document.FileURI(path), string(data)), nil)
}

func (f *fakeCoreDocs) Save(_ context.Context, doc *document.Document, done func(error)) {
	f.mu.Lock()
	f.saved = append(f.saved, doc.URI().String())
	hang, err := f.saveHangs, f.saveErr
	f.mu.Unlock()
	if hang {
		return
	}
	done(err)
}

func (f *fakeCoreDocs) Close(doc *document.Document) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.closed = append(f.closed, doc.URI().String())
	delete(f.docs, doc.URI())
	return nil
}

func (f *fakeCoreDocs) Reveal(doc *document.Document, pos extension.Position) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.revealed = append(f.revealed, doc.URI().String())
	f.revealAt = append(f.revealAt, pos)
	return nil
}

func (f *fakeCoreDocs) BeginEdit() func() {
	f.mu.Lock()
	f.began++
	f.mu.Unlock()
	return func() { f.mu.Lock(); f.end++; f.mu.Unlock() }
}

// coreHarness is a host with the extension emetos.relay (both privileges) and ext.plain (none) active in a tab "one" over
// a real folder.
type coreHarness struct {
	*harness
	dir      string
	outside  string
	api      map[string]extension.API
	ran      []string
	runArgs  []any
	counters map[string]int
}

const (
	privileged = extension.RelayID
	plain      = "ext.plain"
)

// newCoreHarness makes the harness; setup changes the rig (folders, tabs without commands) before the tab is made.
func newCoreHarness(t *testing.T, setup func(*rig)) *coreHarness {
	t.Helper()
	root := t.TempDir()
	c := &coreHarness{dir: filepath.Join(root, "work"), outside: filepath.Join(root, "outside"), counters: map[string]int{}}
	for _, d := range []string{c.dir, c.outside, filepath.Join(c.dir, "src")} {
		if err := os.MkdirAll(d, 0o755); err != nil {
			t.Fatal(err)
		}
	}
	files := map[string]string{
		filepath.Join(c.dir, "a.txt"):        "hello\nworld\n",
		filepath.Join(c.dir, "src", "m.go"):  "package main\n",
		filepath.Join(c.dir, "bin"):          "\xff\xfe\x00",
		filepath.Join(c.outside, "secret"):   "TOP SECRET",
		filepath.Join(c.outside, "other.go"): "package other\n",
	}
	for p, text := range files {
		if err := os.WriteFile(p, []byte(text), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	links := map[string]string{"out-dir": c.outside, "out-abs": filepath.Join(c.outside, "secret"), "loop1": "loop2", "loop2": "loop1", "in": "a.txt"}
	for name, target := range links {
		if err := os.Symlink(target, filepath.Join(c.dir, name)); err != nil {
			t.Fatal(err)
		}
	}
	if err := syscall.Mkfifo(filepath.Join(c.dir, "fifo"), 0o644); err != nil {
		t.Fatal(err)
	}
	var factories []extension.Factory
	c.api = map[string]extension.API{}
	for _, id := range []string{privileged, plain} {
		m := manifest(id)
		if id == privileged {
			m.Privileges = []string{extension.PrivilegeEventsPublishAny, extension.PrivilegeCommandsExecute}
		}
		f, _ := factory(m, func(f *fake) {
			f.activate = func(_ context.Context, api extension.API) error { c.api[id] = api; return nil }
		})
		factories = append(factories, f)
	}
	r := newRig(t, factories...)
	r.folders["one"] = c.dir
	if setup != nil {
		setup(r)
	}
	tb := r.newTab("one")
	tb.scope.Start()
	for _, id := range []string{privileged, plain} {
		if c.api[id] == nil {
			t.Fatalf("%s was not activated: %v", id, tb.scope.Failure(id))
		}
	}
	tb.pump()
	tb.deferred = true
	c.harness = &harness{t: t, r: r, tb: tb, apis: c.api}
	return c
}

// ask makes a request as the extension and waits for the answer.
func (c *coreHarness) ask(from, topic string, payload any) (extension.Event, error) {
	c.t.Helper()
	o := newOutcome()
	if err := c.events(from).Request(context.Background(), topic, payload, o.fn); err != nil {
		c.t.Fatal(err)
	}
	c.await(o)
	_, ev, err := o.get()
	return ev, err
}

// register makes a command of the tab that records its runs.
func (c *coreHarness) register(id string, h command.Handler) {
	c.t.Helper()
	reg := c.tb.commands
	if _, ok := reg.Lookup(id); !ok {
		if err := reg.Register(command.Meta{ID: id, Title: id}); err != nil {
			c.t.Fatal(err)
		}
	}
	if _, err := reg.SetHandler(id, h); err != nil {
		c.t.Fatal(err)
	}
}
