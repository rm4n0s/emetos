package exthost

import (
	"bytes"
	"context"
	"log/slog"
	"path/filepath"
	"sort"
	"sync"
	"testing"
	"time"

	"github.com/rm4n0s/emetos/extension"
	"github.com/rm4n0s/emetos/internal/core/command"
	"github.com/rm4n0s/emetos/internal/core/contextkey"
	"github.com/rm4n0s/emetos/internal/core/keybinding"
	"github.com/rm4n0s/emetos/internal/core/language"
	"github.com/rm4n0s/emetos/internal/core/logs"
	"github.com/rm4n0s/emetos/internal/core/notify"
	"github.com/rm4n0s/emetos/internal/core/settings"
	"github.com/rm4n0s/emetos/internal/core/storage"
	"github.com/rm4n0s/emetos/internal/gui/views/problems"
	"github.com/rm4n0s/emetos/internal/gui/workbench"
)

// fake is an extension whose behavior a test sets.
type fake struct {
	manifest extension.Manifest
	activate func(ctx context.Context, api extension.API) error
	// ctx is the context Activate received.
	ctx context.Context
}

func (f *fake) Manifest() extension.Manifest { return f.manifest }

func (f *fake) Activate(ctx context.Context, api extension.API) error {
	f.ctx = ctx
	if f.activate == nil {
		return nil
	}
	return f.activate(ctx, api)
}

// made records the extensions a factory made for activations (the first call of a factory is the host reading the
// manifest, and is not recorded).
type made struct {
	mu    sync.Mutex
	calls int
	list  []*fake
}

func (m *made) all() []*fake {
	m.mu.Lock()
	defer m.mu.Unlock()
	return append([]*fake(nil), m.list...)
}

// factory makes a factory of fakes with the manifest; setup fills in the behavior of each one.
func factory(m extension.Manifest, setup func(*fake)) (extension.Factory, *made) {
	rec := &made{}
	return func() extension.Extension {
		f := &fake{manifest: m}
		rec.mu.Lock()
		defer rec.mu.Unlock()
		rec.calls++
		if rec.calls == 1 {
			return f // the first call is the host reading the manifest: it is not an activation
		}
		if setup != nil {
			setup(f)
		}
		rec.list = append(rec.list, f)
		return f
	}, rec
}

// manifest is a valid manifest with the id, started by onStartup unless events are given.
func manifest(id string, events ...string) extension.Manifest {
	if len(events) == 0 {
		events = []string{extension.OnStartup}
	}
	return extension.Manifest{ID: id, Name: "Ext " + id, Version: "1.0.0", ActivationEvents: events}
}

// keySink records the keybindings of a scope.
type keySink struct {
	mu    sync.Mutex
	items map[int]keybinding.Item
	next  int
}

func (k *keySink) Add(items ...keybinding.Item) func() {
	k.mu.Lock()
	defer k.mu.Unlock()
	if k.items == nil {
		k.items = map[int]keybinding.Item{}
	}
	var ids []int
	for _, it := range items {
		k.next++
		k.items[k.next] = it
		ids = append(ids, k.next)
	}
	return func() {
		k.mu.Lock()
		defer k.mu.Unlock()
		for _, id := range ids {
			delete(k.items, id)
		}
	}
}

func (k *keySink) commands() []string {
	k.mu.Lock()
	defer k.mu.Unlock()
	var out []string
	for _, it := range k.items {
		out = append(out, it.Command)
	}
	return out
}

// docSource is a window with documents.
type docSource struct {
	texts map[string]string
	order []string
}

func (d *docSource) Read(_ context.Context, uri string) (string, error) {
	if t, ok := d.texts[uri]; ok {
		return t, nil
	}
	return "", context.DeadlineExceeded
}
func (d *docSource) Open() []string { return d.order }

// tab is the services of one scope, as a window tab has them.
type tab struct {
	t        *testing.T
	scope    *Scope
	commands *command.Registry
	menus    *command.MenuRegistry
	store    *settings.Store
	bar      *workbench.StatusBar
	notify   *notify.Service
	keys     *keySink
	docs     *docSource
	disabled []string
	files    []string
	changes  int
	posted   []func()
	log      *bytes.Buffer

	// evMu guards what the event tests share between goroutines: the queue of a deferred tab, the frames and the clock.
	evMu sync.Mutex
	// deferred makes Post queue its functions until pump runs them, as the UI thread does; by default Post runs them at once.
	deferred bool
	queued   []func()
	frames   []func()
	clock    *fakeClock

	// views, ctxKeys, markers and channels are the views model, the context keys, the Problems and the Output store of the
	// tab; shownChannels records the calls of ShowChannel and folder is the real folder of the tab when a test gives it one.
	views         *workbench.Views
	layout        *workbench.Layout
	ctxKeys       *contextkey.Scope
	markers       *problems.Service
	channels      *logs.Store
	shownChannels []string
	complete      bool

	// editors is the editor area of the tab, languages its grammars, uris the URIs it opened and changed how many times the
	// editor contributions changed (editors_test.go).
	editors   *fakeEditors
	languages *fakeLanguages
	uris      []string
	changed   int

	// lsp is the window's side of the language servers (servers_rig_test.go).
	lsp *fakeLSP

	// clips are the texts put on the clipboard of the tab, the first first, and clipErr is the error the clipboard answers.
	clips   []string
	clipErr error

	// picks are the lists the tab was asked to show (QuickPick().Show), the first first.
	picks []QuickPickRequest
	// inputs are the input boxes the tab was asked to show (InputBox().Show), the first first.
	inputs []InputBoxRequest
	// banners is the model of the banners of the tab, and chooser the file dialogs the tab was asked to show, which answer chooserPath
	// and chooserErr.
	banners     *workbench.Banners
	chooser     []extension.FileChooserOptions
	chooserPath string
	chooserErr  error

	// coreDocs is the CoreDocuments of the tab over its documents (core_test.go).
	coreDocs *fakeCoreDocs

	// asked are the questions the tab was asked about links, and answers the functions that answer them (askSharing).
	asked   []SharingAsk
	answers []func(SharingAnswer)
}

// rig is a host with the registry of the process and the tabs made over it.
type rig struct {
	t        *testing.T
	host     *Host
	registry *settings.Registry
	tabs     []*tab
	// sharing is the store of the links the host reads (newRigStore); noID and noAsk name tabs that cannot be named in a
	// link and tabs that cannot ask.
	sharing *storage.SharingStore
	noID    map[string]bool
	noAsk   map[string]bool
	// noPick names tabs that cannot show a list to choose from.
	noPick map[string]bool
	// noInput and noLSP name tabs that cannot show an input box and tabs that have no language workspace (no way to edit documents).
	noInput, noLSP map[string]bool
	// noBanners and noChooser name tabs that have no banners and no file dialog.
	noBanners, noChooser map[string]bool
	// folders gives a tab a real folder instead of /work/<name>.
	folders map[string]string
	// noCommands and noCoreDocs name tabs that have no registry of commands and tabs whose documents cannot be edited (the
	// core topics, core_test.go).
	noCommands, noCoreDocs map[string]bool
	// noClip names tabs that have no clipboard (references_test.go).
	noClip map[string]bool
}

// folderOf is the folder of the tab named name.
func (r *rig) folderOf(name string) string {
	if f, ok := r.folders[name]; ok {
		return f
	}
	return "/work/" + name
}

// newRig makes a host over the factories.
func newRig(t *testing.T, factories ...extension.Factory) *rig {
	t.Helper()
	return newRigStore(t, nil, factories...)
}

// newRigStore makes a host over the store of links; nil is a store in memory.
func newRigStore(t *testing.T, store *storage.SharingStore, factories ...extension.Factory) *rig {
	t.Helper()
	return newRigLanguages(t, store, nil, factories...)
}

// newRigLanguages is newRigStore over a language registry, which the language packs among the factories fill.
func newRigLanguages(t *testing.T, store *storage.SharingStore, languages *language.Registry, factories ...extension.Factory) *rig {
	t.Helper()
	reg := settings.DefaultRegistry()
	host := NewHost(HostOptions{Factories: factories, Settings: reg, Sharing: store, Languages: languages, Logger: slog.New(slog.NewTextHandler(&bytes.Buffer{}, nil))})
	return &rig{t: t, registry: reg, host: host, sharing: host.sharing, noID: map[string]bool{}, noAsk: map[string]bool{}, noPick: map[string]bool{}, noInput: map[string]bool{}, noLSP: map[string]bool{}, noBanners: map[string]bool{}, noChooser: map[string]bool{}, folders: map[string]string{}, noCommands: map[string]bool{}, noCoreDocs: map[string]bool{}}
}

// newTab makes a scope named name over its own registries, as a workspace has them; it does not start it.
func (r *rig) newTab(name string, disabled ...string) *tab {
	r.t.Helper()
	commands := command.DefaultRegistry()
	tb := &tab{
		t: r.t, commands: commands, menus: command.DefaultMenus(commands), bar: workbench.NewStatusBar(),
		keys: &keySink{}, docs: &docSource{texts: map[string]string{}}, disabled: disabled, log: &bytes.Buffer{},
	}
	tb.clock = &fakeClock{t: time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)}
	tb.store = settings.NewStore(r.registry, settings.StoreOptions{UserPath: filepath.Join(r.t.TempDir(), "settings.json")})
	tb.notify = notify.NewService(slog.New(slog.NewTextHandler(&bytes.Buffer{}, nil)), nil)
	tb.layout = workbench.NewLayout(workbench.LayoutStateFromSettings(tb.store))
	tb.views = workbench.NewViews(command.DefaultViews(), tb.layout)
	tb.ctxKeys = contextkey.NewRoot()
	tb.markers = problems.NewService()
	tb.channels = logs.NewStore(0)
	tb.complete = true
	tb.editors = &fakeEditors{}
	tb.languages = &fakeLanguages{}
	tb.lsp = newFakeLSP()
	var pick func(QuickPickRequest)
	if !r.noPick[name] {
		pick = func(req QuickPickRequest) { tb.picks = append(tb.picks, req) }
	}
	var input func(InputBoxRequest)
	if !r.noInput[name] {
		input = func(req InputBoxRequest) { tb.inputs = append(tb.inputs, req) }
	}
	var banners *workbench.Banners
	if !r.noBanners[name] {
		banners = workbench.NewBanners()
		tb.banners = banners
	}
	var chooser func(context.Context, extension.FileChooserOptions) (string, error)
	if !r.noChooser[name] {
		chooser = func(_ context.Context, o extension.FileChooserOptions) (string, error) {
			tb.chooser = append(tb.chooser, o)
			return tb.chooserPath, tb.chooserErr
		}
	}
	var lspw LanguageWorkspace = tb.lsp
	if r.noLSP[name] {
		lspw = nil
	}
	tb.coreDocs = newFakeCoreDocs(tb.docs)
	var docs DocumentSource = tb.coreDocs
	if r.noCoreDocs[name] {
		docs = tb.docs
	}
	registry := commands
	if r.noCommands[name] {
		registry = nil
	}
	var clip func(string) error
	if !r.noClip[name] {
		clip = func(text string) error {
			if tb.clipErr != nil {
				return tb.clipErr
			}
			tb.clips = append(tb.clips, text)
			return nil
		}
	}
	tb.scope = r.host.NewScope(ScopeOptions{
		Clipboard: clip, LSP: lspw, QuickPick: pick, InputBox: input, Banners: banners, FileChooser: chooser,
		Editors: tb.editors, Languages: tb.languages, EditorsChanged: func() { tb.changed++ },
		OpenURI: func(_ context.Context, uri string) error { tb.uris = append(tb.uris, uri); return nil },
		Info:    extension.ScopeInfo{Kind: extension.ScopeWorkspace, Name: name, Folder: r.folderOf(name), ID: r.scopeID(name)},
		Ctx:     context.Background(), Commands: registry, Menus: tb.menus, Keybindings: tb.keys, Settings: tb.store,
		Notify: tb.notify, StatusBar: tb.bar, Documents: docs,
		Disabled: func() []string { return tb.disabled }, Files: func() []string { return tb.files },
		Post: tb.post, AskSharing: r.askFunc(tb, name),
		Views: tb.views, Keys: tb.ctxKeys, Markers: tb.markers, Channels: tb.channels,
		ShowChannel:   func(name string, _ bool) { tb.shownChannels = append(tb.shownChannels, name) },
		FilesComplete: func() bool { return tb.complete },
		Frame:         func(f func()) { tb.evMu.Lock(); tb.frames = append(tb.frames, f); tb.evMu.Unlock() },
		After:         tb.clock.after, Now: tb.clock.nowFunc,
		Logger: slog.New(slog.NewTextHandler(tb.log, &slog.HandlerOptions{Level: slog.LevelDebug})),
	})
	r.t.Cleanup(tb.scope.Close)
	r.tabs = append(r.tabs, tb)
	return tb
}

// run runs a command of the tab as the palette does.
func (tb *tab) run(id string) error {
	return tb.commands.Run(command.Context{Ctx: context.Background(), Source: command.SourcePalette}, id, nil)
}

// menuCommands lists the commands of the entries of a menu.
func (tb *tab) menuCommands(menu string) []string {
	var out []string
	for _, e := range tb.menus.Items(menu, contextkey.NewRoot()) {
		if !e.Separator {
			out = append(out, e.Command)
		}
	}
	return out
}

// statusIDs lists the ids of the items of the status bar that extensions added.
func (tb *tab) statusIDs() []string {
	var out []string
	for _, a := range []workbench.StatusAlignment{workbench.StatusLeft, workbench.StatusRight} {
		for _, it := range tb.bar.Items(a) {
			out = append(out, it.ID)
		}
	}
	return out
}

// notifications lists the messages of the notifications of the tab.
func (tb *tab) notifications() []string {
	var out []string
	for _, it := range tb.notify.Items() {
		out = append(out, it.Message)
	}
	return out
}

// commandContext is the context a palette run has.
func commandContext() command.Context {
	return command.Context{Ctx: context.Background(), Source: command.SourcePalette}
}

// changeSetting sets a user setting of the tab to a value, which tells the listeners of its store.
func (tb *tab) changeSetting(key string, value any) {
	tb.t.Helper()
	if err := tb.store.Update(context.Background(), key, value, settings.TargetUser); err != nil {
		tb.t.Fatal(err)
	}
}

// post is the Post function of a tab: at once, or queued for pump when the tab is deferred.
func (tb *tab) post(f func()) {
	tb.evMu.Lock()
	tb.posted = append(tb.posted, f)
	if tb.deferred {
		tb.queued = append(tb.queued, f)
		tb.evMu.Unlock()
		return
	}
	tb.evMu.Unlock()
	f()
}

// pump runs what was posted, and what that posts, as the UI thread does between two frames.
func (tb *tab) pump() {
	for {
		tb.evMu.Lock()
		if len(tb.queued) == 0 {
			tb.evMu.Unlock()
			return
		}
		f := tb.queued[0]
		tb.queued = tb.queued[1:]
		tb.evMu.Unlock()
		f()
	}
}

// frame ends a frame: the functions that asked for it run, then what they posted.
func (tb *tab) frame() {
	tb.evMu.Lock()
	frames := tb.frames
	tb.frames = nil
	tb.evMu.Unlock()
	for _, f := range frames {
		f()
	}
	tb.pump()
}

// fakeClock is the clock and the timers of a tab: time passes only when a test says so.
type fakeClock struct {
	mu     sync.Mutex
	t      time.Time
	timers []*fakeTimer
}

type fakeTimer struct {
	at      time.Time
	f       func()
	stopped bool
}

func (c *fakeClock) nowFunc() time.Time {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.t
}

func (c *fakeClock) after(d time.Duration, f func()) func() {
	c.mu.Lock()
	defer c.mu.Unlock()
	tm := &fakeTimer{at: c.t.Add(d), f: f}
	c.timers = append(c.timers, tm)
	return func() { c.mu.Lock(); tm.stopped = true; c.mu.Unlock() }
}

// advance moves the clock on and calls the timers that fall due, earliest first.
func (c *fakeClock) advance(d time.Duration) {
	c.mu.Lock()
	c.t = c.t.Add(d)
	var due []*fakeTimer
	rest := c.timers[:0]
	for _, tm := range c.timers {
		switch {
		case tm.stopped:
		case !tm.at.After(c.t):
			due = append(due, tm)
		default:
			rest = append(rest, tm)
		}
	}
	c.timers = rest
	c.mu.Unlock()
	sort.SliceStable(due, func(i, j int) bool { return due[i].at.Before(due[j].at) })
	for _, tm := range due {
		tm.f()
	}
}

// pending is how many timers wait.
func (c *fakeClock) pending() int {
	c.mu.Lock()
	defer c.mu.Unlock()
	n := 0
	for _, tm := range c.timers {
		if !tm.stopped {
			n++
		}
	}
	return n
}

// scopeID is the ID a tab named name has in the links: its folder, or none for a tab in noID.
func (r *rig) scopeID(name string) string {
	if r.noID[name] {
		return ""
	}
	return "/work/" + name
}

// askFunc is the AskSharing of a tab: it records the question and keeps the function that answers it, for the test to
// call; nil for a tab in noAsk.
func (r *rig) askFunc(tb *tab, name string) func(SharingAsk, func(SharingAnswer)) {
	if r.noAsk[name] {
		return nil
	}
	return func(q SharingAsk, answer func(SharingAnswer)) {
		tb.evMu.Lock()
		defer tb.evMu.Unlock()
		tb.asked = append(tb.asked, q)
		tb.answers = append(tb.answers, answer)
	}
}
