package httpclient

import (
	"context"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/rm4n0s/errors"
	"github.com/rm4n0s/glutter/gluttertest"
	"github.com/rm4n0s/glutter/widgets"

	gui "github.com/rm4n0s/glutter/ui"

	"github.com/rm4n0s/emetos/extension"
	"github.com/rm4n0s/emetos/extension/ui/uitest"
	"github.com/rm4n0s/emetos/extensions/httpclient/format"
	"github.com/rm4n0s/emetos/extensions/httpclient/store"
	"github.com/rm4n0s/emetos/extensions/internal/dbconn"
	"github.com/rm4n0s/emetos/extensions/internal/fakeapi"
)

// fixtureDir is the project the proof of step 27.3 uses; the tests start from a copy of it.
const fixtureDir = "../../testdata/proof/27.3/project"

// testNow is the clock of the tests: the history shows "Today" and "Yesterday" from it.
var testNow = time.Date(2026, 11, 3, 14, 0, 0, 0, time.UTC)

// rig is the extension activated over the fake API in a copy of the fixture folder.
type rig struct {
	t      *testing.T
	api    *fakeapi.API
	ext    *Extension
	folder string
	kr     *dbconn.MemoryKeyring
	// cancel ends the context of the extension, as closing the window does.
	cancel context.CancelFunc
}

// isolate points every XDG folder of the test at a folder of its own.
func isolate(t *testing.T) {
	t.Helper()
	root := t.TempDir()
	t.Setenv("XDG_STATE_HOME", filepath.Join(root, "state"))
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(root, "config"))
	t.Setenv("XDG_CACHE_HOME", filepath.Join(root, "cache"))
	t.Setenv("XDG_DATA_HOME", filepath.Join(root, "data"))
	t.Setenv("HOME", root)
}

// copyDir copies a folder.
func copyDir(t *testing.T, from, to string) {
	t.Helper()
	err := filepath.WalkDir(from, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		rel, _ := filepath.Rel(from, path)
		if d.IsDir() {
			return os.MkdirAll(filepath.Join(to, rel), 0o755)
		}
		data, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		return os.WriteFile(filepath.Join(to, rel), data, 0o644)
	})
	if err != nil {
		t.Fatal(err)
	}
}

// newRig activates the extension in a copy of the fixture; before changes the folder before the extension reads it.
func newRig(t *testing.T, before ...func(folder string)) *rig { return newRigWith(t, nil, before...) }

// newRigWith is newRig with the options of the extension changed by mod before it is activated.
func newRigWith(t *testing.T, mod func(*Options), before ...func(folder string)) *rig {
	t.Helper()
	isolate(t)
	folder := t.TempDir()
	copyDir(t, fixtureDir, folder)
	for _, b := range before {
		b(folder)
	}
	return activateWith(t, folder, mod)
}

// activate activates the extension in a folder and waits until it has read the files.
func activate(t *testing.T, folder string) *rig { return activateWith(t, folder, nil) }

// activateWith is activate with the options of the extension changed by mod (nil: as they are).
func activateWith(t *testing.T, folder string, mod func(*Options)) *rig {
	t.Helper()
	r := &rig{t: t, folder: folder, kr: &dbconn.MemoryKeyring{}}
	opts := Options{Keyring: r.kr, Now: func() time.Time { return testNow }}
	if mod != nil {
		mod(&opts)
	}
	e := FactoryOf(opts)().(*Extension)
	r.api = fakeapi.New(t, e.Manifest(), extension.ScopeInfo{Kind: extension.ScopeWorkspace, Name: "project", Folder: folder, ID: folder})
	ctx, cancel := context.WithCancel(context.Background())
	r.cancel = cancel
	t.Cleanup(func() {
		cancel()
		time.Sleep(5 * time.Millisecond)
	})
	if err := e.Activate(ctx, r.api); err != nil {
		t.Fatal(err)
	}
	r.ext = e
	if folder != "" {
		r.api.WaitFor("the files to be read", func() bool { return e.store != nil || e.loadErr != nil })
	}
	return r
}

// settle waits until the jobs the extension started off the UI thread have handed their results back.
func (r *rig) settle() {
	r.t.Helper()
	r.api.WaitFor("the jobs to end", func() bool { return r.ext.pending.Load() == 0 })
	r.api.Pump()
}

// run runs a command and waits for what it started.
func (r *rig) run(id string, args any) {
	r.t.Helper()
	if err := r.api.Run(id, args); err != nil {
		r.t.Fatalf("%s: %v", id, err)
	}
	r.settle()
}

// path is the absolute path of a file of the project.
func (r *rig) path(rel ...string) string {
	return filepath.Join(append([]string{r.folder, filepath.FromSlash(format.Dir)}, rel...)...)
}

// col is the path of a file in the collections folder.
func (r *rig) col(rel string) string { return r.path("collections", filepath.FromSlash(rel)) }

// env is the path of the file of an environment.
func (r *rig) env(key string) string { return r.path("environments", key+".jsonc") }

// node is the node with the path in the tree.
func (r *rig) node(path string) *store.Node {
	r.t.Helper()
	n := r.ext.tree.Find(path)
	if n == nil {
		r.t.Fatalf("no node %s", path)
	}
	return n
}

// has says whether the file exists.
func (r *rig) has(path string) bool {
	_, err := os.Stat(path)
	return err == nil
}

// read returns the text of a file.
func (r *rig) read(path string) string {
	r.t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		r.t.Fatal(err)
	}
	return string(data)
}

// answer types the answer of the input box that is open and accepts it, then waits for the work it starts.
func (r *rig) answer(text string) {
	r.t.Helper()
	in := r.api.LastInput()
	if in == nil {
		r.t.Fatal("no input box is open")
	}
	in.Accept(text)
	r.settle()
}

// pressDialog presses a button of the dialog that is open and waits for the work it starts.
func (r *rig) pressDialog(label string) {
	r.t.Helper()
	open := r.api.OpenDialogs()
	if len(open) == 0 {
		r.t.Fatal("no dialog is open")
	}
	open[0].Press(label)
	r.settle()
}

// choose chooses the item of the last quick pick with the label (a prefix after the icon) and waits for the work it starts.
func (r *rig) choose(label string) {
	r.t.Helper()
	p := r.api.LastPick()
	if p == nil {
		r.t.Fatal("no list is open")
	}
	for i, l := range p.Labels() {
		if strings.Contains(l, label) {
			p.Choose(i)
			r.settle()
			return
		}
	}
	r.t.Fatalf("no item %q in %q", label, p.Labels())
}

// show draws a view of the extension in a window of the theme.
func (r *rig) show(th uitest.Theme, view string, w, h float64) *gluttertest.Tester {
	r.t.Helper()
	v := r.api.View(view)
	if v == nil {
		r.t.Fatalf("the view %s was not provided", view)
	}
	return uitest.Pump(r.t, th, gui.Size{Width: w, Height: h}, widgets.NewBuilder(func(bc widgets.BuildContext) widgets.Widget { return v.Content.Build(bc) }))
}

// notes are the messages of the notifications so far.
func (r *rig) notes() []string {
	var out []string
	for _, n := range r.api.Notes() {
		out = append(out, n.Severity+": "+n.Message)
	}
	return out
}

// writeFile writes a file, making its folder.
func writeFile(t *testing.T, path, text string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(text), 0o644); err != nil {
		t.Fatal(err)
	}
}

// errFake is an error of a fake that fails.
var errFake = &errors.Error{Tag: "FakeFailure", Message: "the fake failed"}

// removeFile removes a file.
func removeFile(path string) error { return os.Remove(path) }

// removeAll removes a folder.
func removeAll(path string) error { return os.RemoveAll(path) }
