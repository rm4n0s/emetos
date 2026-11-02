package debug

import (
	"context"
	"log/slog"
	"slices"
	"sync/atomic"

	"github.com/rm4n0s/glutter/ui"
	"github.com/rm4n0s/glutter/widgets"

	"github.com/rm4n0s/emetos/internal/core/debug"
	"github.com/rm4n0s/emetos/internal/gui/kit"
)

// Opts are what the views need of their window. Everything is called on the UI thread, except Post, which any goroutine may call.
type Opts struct {
	// Ctx ends the requests the views make when it is done.
	Ctx context.Context
	// Service is the debug service of the workspace; the view of a client of a hosted workspace gets the host's.
	Service *debug.Service
	// Post runs a function on the UI thread, later.
	Post func(func())
	// Logger logs a request of a view that failed; slog's default when nil.
	Logger *slog.Logger
	// ShowTypes is debug.showVariableTypes; false when nil.
	ShowTypes func() bool
	// Presentation is debug.breakpointsView.presentation, "list" or "tree"; "list" when nil.
	Presentation func() string
	// Visible says whether a view is on screen: a view that is folded or hidden asks the adapter for nothing. True when nil.
	Visible func(viewID string) bool
}

// Panes are the five views of the container, over one service.
type Panes struct {
	Variables   *VariablesPane
	Watch       *WatchPane
	CallStack   *CallStackPane
	Breakpoints *BreakpointsPane
	Loaded      *LoadedPane

	opts  Opts
	stop  func()
	dirty atomic.Bool
}

// NewPanes makes the views and follows the service.
func NewPanes(opts Opts) *Panes {
	if opts.Logger == nil {
		opts.Logger = slog.Default()
	}
	if opts.Ctx == nil {
		opts.Ctx = context.Background()
	}
	p := &Panes{opts: opts}
	p.Variables = newVariablesPane(opts)
	p.Watch = newWatchPane(opts)
	p.CallStack = newCallStackPane(opts)
	p.Breakpoints = newBreakpointsPane(opts)
	p.Loaded = newLoadedPane(opts)
	p.stop = opts.Service.Subscribe(p.onEvent)
	p.Refresh()
	return p
}

// Dispose stops following the service and cancels what the views asked for.
func (p *Panes) Dispose() {
	p.stop()
	for _, x := range p.all() {
		x.dispose()
	}
}

func (p *Panes) all() []pane {
	return []pane{p.Variables, p.Watch, p.CallStack, p.Breakpoints, p.Loaded}
}

// onEvent hands the events of the service to the UI thread, one burst at a time.
func (p *Panes) onEvent(ev debug.Event) {
	// what a session prints is the Debug Console's (console.go): a flood of output does not make these views read the model again
	if ev.Kind == debug.EventRepl || ev.Kind == debug.EventOutput {
		return
	}
	if p.dirty.Swap(true) {
		// a burst already waits: the views read the whole model when it runs
		if ev.Kind != debug.EventStopped && ev.Kind != debug.EventSessionEnded {
			return
		}
	}
	p.opts.Post(func() {
		p.dirty.Store(false)
		p.handle(ev)
	})
}

func (p *Panes) handle(ev debug.Event) {
	for _, x := range p.all() {
		x.refresh(ev)
	}
}

// Refresh reads the model again, now.
func (p *Panes) Refresh() {
	for _, x := range p.all() {
		x.refresh(debug.Event{})
	}
}

// VisibilityChanged tells the views that one of them came on screen or left it.
func (p *Panes) VisibilityChanged() {
	for _, x := range p.all() {
		x.refresh(debug.Event{})
	}
}

// pane is what the views have in common.
type pane interface {
	refresh(ev debug.Event)
	dispose()
}

// base is the part of a pane that follows a tree: its model, the listeners of its changes and the rows by key.
type base struct {
	opts      Opts
	id        string
	model     *kit.TreeModel
	ctl       *kit.TreeController
	rows      map[string]Row
	listeners []*func()
	cancel    context.CancelFunc
	ctx       context.Context
	message   string
	edit      *editState
}

func newBase(opts Opts, id string, treeOpts kit.TreeModelOpts) *base {
	b := &base{opts: opts, id: id, rows: map[string]Row{}, ctl: kit.NewTreeController()}
	b.ctx, b.cancel = context.WithCancel(opts.Ctx)
	b.model = kit.NewTreeModel(treeOpts)
	return b
}

// ID is the id of the view.
func (b *base) ID() string { return b.id }

// Model is the tree the view shows.
func (b *base) Model() *kit.TreeModel { return b.model }

// Controller gives access to the tree widget from outside (focus, reveal).
func (b *base) Controller() *kit.TreeController { return b.ctl }

// Message is the text the view shows in place of an empty tree.
func (b *base) Message() string { return b.message }

// Row is the row of a key of the tree.
func (b *base) Row(key string) (Row, bool) {
	r, ok := b.rows[key]
	return r, ok
}

// Focused is the row that has the focus in the tree, if any.
func (b *base) Focused() (Row, bool) {
	key, ok := b.model.Focused()
	if !ok {
		return Row{}, false
	}
	return b.Row(key)
}

// Selected are the rows that are selected, in the order of the tree.
func (b *base) Selected() []Row {
	var out []Row
	for _, k := range b.model.SelectedKeys() {
		if r, ok := b.Row(k); ok {
			out = append(out, r)
		}
	}
	return out
}

// OnChange calls fn after the rows or a value of the view changed, and returns the function that stops it.
func (b *base) OnChange(fn func()) func() {
	p := &fn
	b.listeners = append(b.listeners, p)
	return func() {
		if i := slices.Index(b.listeners, p); i >= 0 {
			b.listeners = slices.Delete(b.listeners, i, i+1)
		}
	}
}

func (b *base) notify() {
	for _, f := range slices.Clone(b.listeners) {
		(*f)()
	}
}

func (b *base) dispose() {
	b.cancel()
	if b.edit != nil {
		b.edit = nil
	}
}

func (b *base) visible() bool { return b.opts.Visible == nil || b.opts.Visible(b.id) }

func (b *base) showTypes() bool { return b.opts.ShowTypes != nil && b.opts.ShowTypes() }

// Fonts and sizes shared by the rows.
const rowFontSize = 13.0

var _ = ui.Color{}
var _ widgets.Widget
