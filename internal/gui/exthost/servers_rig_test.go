package exthost

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"syscall"
	"testing"
	"time"

	"github.com/rm4n0s/errors"

	"github.com/rm4n0s/emetos/extension"
	"github.com/rm4n0s/emetos/internal/core/buffer"
	"github.com/rm4n0s/emetos/internal/core/document"
	"github.com/rm4n0s/emetos/internal/core/langfeatures"
	"github.com/rm4n0s/emetos/internal/core/lsp"
	"github.com/rm4n0s/emetos/internal/core/lsp/lsptest"
	"github.com/rm4n0s/emetos/internal/core/workspace"
)

// The window of the language server tests: documents that come and go, the events of the file watcher and the edits, in
// memory, and helpers to run the fake server program of core/lsp/lsptest as the server of an extension.

// fakeLSP is the LanguageWorkspace of a test tab.
type fakeLSP struct {
	mu      sync.Mutex
	docs    []*document.Document
	onOpen  []*func(*document.Document)
	onClose []*func(*document.Document)
	onFiles []*func([]workspace.Event)
	// disk holds the closed files by path.
	disk  map[string]string
	edits *memEdits
}

func newFakeLSP() *fakeLSP {
	f := &fakeLSP{disk: map[string]string{}}
	f.edits = &memEdits{lsp: f}
	return f
}

func (f *fakeLSP) Documents() []*document.Document {
	f.mu.Lock()
	defer f.mu.Unlock()
	return slices.Clone(f.docs)
}

func watch[T any](f *fakeLSP, list *[]*func(T), fn func(T)) func() {
	f.mu.Lock()
	defer f.mu.Unlock()
	p := &fn
	*list = append(*list, p)
	return func() {
		f.mu.Lock()
		defer f.mu.Unlock()
		*list = slices.DeleteFunc(*list, func(x *func(T)) bool { return x == p })
	}
}

func (f *fakeLSP) WatchOpen(fn func(*document.Document)) func()  { return watch(f, &f.onOpen, fn) }
func (f *fakeLSP) WatchClose(fn func(*document.Document)) func() { return watch(f, &f.onClose, fn) }
func (f *fakeLSP) WatchFiles(fn func([]workspace.Event)) func()  { return watch(f, &f.onFiles, fn) }

func (f *fakeLSP) OpenText(uri document.URI) (lsp.Lines, bool) {
	f.mu.Lock()
	defer f.mu.Unlock()
	for _, d := range f.docs {
		if d.URI() == uri {
			return d.Snapshot(), true
		}
	}
	return nil, false
}

func (f *fakeLSP) ReadText(_ context.Context, uri document.URI) (lsp.Lines, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	text, ok := f.disk[uri.Path]
	if !ok {
		return nil, errors.New("FakeFileMissing", "no such file", "path", uri.Path)
	}
	return buffer.New(text).Snapshot(), nil
}

func (f *fakeLSP) Edits() langfeatures.EditHost { return f.edits }

// open adds a document to the window and tells the watchers, as the window does.
func (f *fakeLSP) open(d *document.Document) {
	f.mu.Lock()
	f.docs = append(f.docs, d)
	fns := slices.Clone(f.onOpen)
	f.mu.Unlock()
	for _, fn := range fns {
		(*fn)(d)
	}
}

// closeDoc takes a document out of the window and tells the watchers.
func (f *fakeLSP) closeDoc(d *document.Document) {
	f.mu.Lock()
	f.docs = slices.DeleteFunc(f.docs, func(x *document.Document) bool { return x == d })
	fns := slices.Clone(f.onClose)
	f.mu.Unlock()
	for _, fn := range fns {
		(*fn)(d)
	}
}

// files sends a batch of watcher events, from the calling goroutine.
func (f *fakeLSP) files(events ...workspace.Event) {
	f.mu.Lock()
	fns := slices.Clone(f.onFiles)
	f.mu.Unlock()
	for _, fn := range fns {
		(*fn)(events)
	}
}

// memEdits is the EditHost of a test tab: the open documents of the fake window and a fake disk.
type memEdits struct {
	lsp *fakeLSP
	log []string
}

func (m *memEdits) Document(uri document.URI) (*document.Document, bool) {
	for _, d := range m.lsp.Documents() {
		if d.URI() == uri {
			return d, true
		}
	}
	return nil, false
}

func (m *memEdits) OpenForEdit(_ context.Context, path string) (*document.Document, error) {
	m.lsp.mu.Lock()
	text, ok := m.lsp.disk[path]
	m.lsp.mu.Unlock()
	if !ok {
		return nil, errors.New("FakeFileMissing", "no such file", "path", path)
	}
	d := document.New(document.FileURI(path), text, document.NewOptions())
	m.lsp.mu.Lock()
	m.lsp.docs = append(m.lsp.docs, d)
	m.lsp.mu.Unlock()
	m.log = append(m.log, "open "+path)
	return d, nil
}

func (m *memEdits) CreateFile(_ context.Context, path string, _ langfeatures.FileOptions) error {
	m.lsp.mu.Lock()
	m.lsp.disk[path] = ""
	m.lsp.mu.Unlock()
	m.log = append(m.log, "create "+path)
	return nil
}

func (m *memEdits) RenameFile(_ context.Context, from, to string, _ langfeatures.FileOptions) error {
	m.log = append(m.log, "rename "+from+" "+to)
	return nil
}

func (m *memEdits) DeleteFile(_ context.Context, path string, _ langfeatures.FileOptions) error {
	m.log = append(m.log, "delete "+path)
	return nil
}

// ---- the extension with a server ----

// serverTest is an extension with the fake language server, and the tab it runs in.
type serverTest struct {
	t      *testing.T
	rig    *rig
	tab    *tab
	spec   extension.LanguageServerSpec
	prog   string
	script string
	folder string
	fake   *made
	// api is the API of the last activation, and onActivate what a test's extension does in Activate.
	api        extension.API
	onActivate func(ctx context.Context, a extension.API) error
}

const (
	testExt    = "fakels"
	testServer = "fake"
)

// writeScript writes the script the fake server follows; {{folder}} in it is the folder of the tab.
func (st *serverTest) writeScript(s lsptest.Script) {
	t, path := st.t, st.script
	t.Helper()
	data, err := json.Marshal(s)
	if err != nil {
		t.Fatal(err)
	}
	data = []byte(strings.ReplaceAll(string(data), "{{folder}}", st.folder))
	if err := os.WriteFile(path, data, 0o600); err != nil {
		t.Fatal(err)
	}
}

// newServerTest makes a rig whose extension fakels declares the server fake over the fake server program, started with
// script. tweak may change the spec and the manifest before the host reads it. The tab is made but not started.
func newServerTest(t *testing.T, script lsptest.Script, tweak func(*extension.Manifest, *extension.LanguageServerSpec)) *serverTest {
	t.Helper()
	st := &serverTest{t: t, prog: lsptest.BuildProgram(t), folder: t.TempDir()}
	st.script = filepath.Join(t.TempDir(), "script.json")
	st.writeScript(script)
	st.spec = extension.LanguageServerSpec{
		ID: testServer, Name: "Fake LS", Languages: []string{"go"}, Program: st.prog, Args: []string{"-script", st.script},
		PathSetting: testExt + ".path", ConfigurationSection: testExt + ".config",
	}
	m := manifest(testExt)
	m.Name = "Fake Language Support"
	if tweak != nil {
		tweak(&m, &st.spec)
	}
	m.Contributes.LanguageServers = []extension.LanguageServerSpec{st.spec}
	f, rec := factory(m, func(f *fake) {
		f.activate = func(ctx context.Context, a extension.API) error {
			st.api = a
			if st.onActivate != nil {
				return st.onActivate(ctx, a)
			}
			return nil
		}
	})
	st.fake = rec
	st.rig = newRig(t, f)
	st.rig.folders["one"] = st.folder
	t.Cleanup(func() {
		st.rig.host.Shutdown()
		st.rig.host.WaitServers()
	})
	return st
}

// startTab makes the tab, defers its Post as the UI thread does, and starts it.
func (st *serverTest) startTab(name string) *tab {
	st.t.Helper()
	tb := st.rig.newTab(name)
	tb.deferred = true
	tb.scope.Start()
	tb.pump()
	if st.tab == nil {
		st.tab = tb
	}
	return tb
}

// goDoc makes a Go document in the folder of the tab and opens it in the window.
func (st *serverTest) goDoc(tb *tab, name, text string) *document.Document {
	st.t.Helper()
	opts := document.NewOptions()
	opts.LanguageID = "go"
	d := document.New(document.FileURI(filepath.Join(st.rig.folderOf(tb.scope.Info().Name), name)), text, opts)
	tb.lsp.open(d)
	tb.pump()
	return d
}

// status is the status of the server in the tab.
func (st *serverTest) status(tb *tab) ServerStatus {
	s, _ := tb.scope.ServerStatus(testExt, testServer)
	return s
}

// waitFor pumps the tab until cond holds.
func waitFor(t *testing.T, tb *tab, what string, cond func() bool) {
	t.Helper()
	deadline := time.Now().Add(30 * time.Second)
	for {
		tb.pump()
		if cond() {
			return
		}
		if time.Now().After(deadline) {
			t.Fatalf("timed out waiting for %s", what)
		}
		time.Sleep(5 * time.Millisecond)
	}
}

// waitRunning waits for the server of the tab to be initialized.
func (st *serverTest) waitRunning(tb *tab) ServerStatus {
	st.t.Helper()
	waitFor(st.t, tb, "the server to run", func() bool { return st.status(tb).State == ServerRunning })
	return st.status(tb)
}

// controller is the controller of the server in the tab.
func (st *serverTest) controller(tb *tab) *serverCtl {
	st.t.Helper()
	tb.scope.mu.Lock()
	defer tb.scope.mu.Unlock()
	for _, sl := range tb.scope.slots {
		if sl.inst != nil && len(sl.inst.servers) > 0 {
			return sl.inst.servers[0]
		}
	}
	st.t.Fatal("the tab has no server")
	return nil
}

// ask sends a request of the fake server's own to the running server and decodes the answer.
func ask[R any](t *testing.T, c *serverCtl, method string, params any) R {
	t.Helper()
	client := c.currentClient()
	if client == nil {
		t.Fatalf("the server is not running (%s)", c.Status().State)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	res, err := lsp.Request[R](ctx, client, method, params)
	if err != nil {
		t.Fatalf("%s: %v", method, err)
	}
	return res
}

// serverDoc is the server's copy of a document.
func serverDoc(t *testing.T, c *serverCtl, uri string) lsptest.Doc {
	t.Helper()
	return ask[lsptest.Doc](t, c, lsptest.MethodDocument, map[string]string{"uri": uri})
}

// traffic is the messages the server got and sent, without its own test methods.
func traffic(t *testing.T, c *serverCtl) []lsptest.Message {
	t.Helper()
	var out []lsptest.Message
	for _, m := range ask[[]lsptest.Message](t, c, lsptest.MethodTraffic, nil) {
		if m.Method != "" && len(m.Method) > 10 && m.Method[:10] == "$/lsptest/" {
			continue
		}
		out = append(out, m)
	}
	return out
}

// processGone tells whether no process of the group exists.
func processGone(pid int) bool {
	return syscall.Kill(-pid, 0) == syscall.ESRCH && syscall.Kill(pid, 0) == syscall.ESRCH
}

// requireGone waits for the process to end.
func requireGone(t *testing.T, what string, pid int) {
	t.Helper()
	deadline := time.Now().Add(15 * time.Second)
	for !processGone(pid) {
		if time.Now().After(deadline) {
			t.Fatalf("%s (pid %d) is still running", what, pid)
		}
		time.Sleep(10 * time.Millisecond)
	}
}

// uriOf is the URI the server knows a file by.
func uriOf(path string) string { return lsp.DocumentURI(document.FileURI(path)) }

// raw is a JSON value.
func raw(s string) json.RawMessage { return json.RawMessage(s) }
