package exthost

import (
	"bytes"
	"context"
	"log/slog"
	"path/filepath"
	"sync"
	"testing"

	"github.com/rm4n0s/emetos/extension"
	"github.com/rm4n0s/emetos/internal/core/command"
	"github.com/rm4n0s/emetos/internal/core/contextkey"
	"github.com/rm4n0s/emetos/internal/core/keybinding"
	"github.com/rm4n0s/emetos/internal/core/notify"
	"github.com/rm4n0s/emetos/internal/core/settings"
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
}

// rig is a host with the registry of the process and the tabs made over it.
type rig struct {
	t        *testing.T
	host     *Host
	registry *settings.Registry
	tabs     []*tab
}

// newRig makes a host over the factories.
func newRig(t *testing.T, factories ...extension.Factory) *rig {
	t.Helper()
	reg := settings.DefaultRegistry()
	return &rig{t: t, registry: reg, host: NewHost(HostOptions{Factories: factories, Settings: reg, Logger: slog.New(slog.NewTextHandler(&bytes.Buffer{}, nil))})}
}

// newTab makes a scope named name over its own registries, as a workspace has them; it does not start it.
func (r *rig) newTab(name string, disabled ...string) *tab {
	r.t.Helper()
	commands := command.DefaultRegistry()
	tb := &tab{
		t: r.t, commands: commands, menus: command.DefaultMenus(commands), bar: workbench.NewStatusBar(),
		keys: &keySink{}, docs: &docSource{texts: map[string]string{}}, disabled: disabled, log: &bytes.Buffer{},
	}
	tb.store = settings.NewStore(r.registry, settings.StoreOptions{UserPath: filepath.Join(r.t.TempDir(), "settings.json")})
	tb.notify = notify.NewService(slog.New(slog.NewTextHandler(&bytes.Buffer{}, nil)), nil)
	tb.scope = r.host.NewScope(ScopeOptions{
		Info: extension.ScopeInfo{Kind: extension.ScopeWorkspace, Name: name, Folder: "/work/" + name},
		Ctx:  context.Background(), Commands: commands, Menus: tb.menus, Keybindings: tb.keys, Settings: tb.store,
		Notify: tb.notify, StatusBar: tb.bar, Documents: tb.docs,
		Disabled: func() []string { return tb.disabled }, Files: func() []string { return tb.files },
		Post:   func(f func()) { tb.posted = append(tb.posted, f); f() },
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
