package exthost

import (
	"context"
	"log/slog"
	"slices"
	"sync"

	"github.com/rm4n0s/errors"

	"github.com/rm4n0s/emetos/extension"
	coretesting "github.com/rm4n0s/emetos/internal/core/testing"
)

// The testing host (SPEC §10.32 "The contract", step 26.1; after extHostTesting.ts and mainThreadTesting.ts). The window makes one
// TestingHost for each window tab, puts it in ScopeOptions.Testing, and reads the test model from it. The host turns the calls of an
// extension's controllers into calls of the model (internal/core/testing): the items of a controller's tree become the model's items,
// a profile becomes a profile whose Run function calls the extension's handler with a TestRunRequest made of the extension's items,
// and a TestRun becomes a task of a result.
//
// The extension's functions (the handlers) run on the goroutine that asked: the one that calls Service.RunTests, ResolveItem or
// RefreshTests, which is never the UI thread (the window starts a run on a goroutine of its own). A panic in one fails the extension
// (ExtensionTestingPanicked) and is the error of the call.

// TestingHostOptions are what the window gives a TestingHost.
type TestingHostOptions struct {
	// Logger receives the problems that do not stop a run; nil is slog.Default.
	Logger *slog.Logger
	// Model are the options of the model; the logger is the host's when it has none.
	Model coretesting.Options
}

// TestingHost is what the extensions of one window tab contribute to testing, and the test model made over it.
type TestingHost struct {
	log *slog.Logger
	svc *coretesting.Service

	mu          sync.Mutex
	scope       *Scope
	controllers map[string]*hostController
	trackers    map[*extension.TestRunRequest]string // the result a request belongs to
}

// NewTestingHost makes the host and the test model over it.
func NewTestingHost(opts TestingHostOptions) *TestingHost {
	h := &TestingHost{log: opts.Logger, controllers: map[string]*hostController{}, trackers: map[*extension.TestRunRequest]string{}}
	if h.log == nil {
		h.log = slog.Default()
	}
	mo := opts.Model
	if mo.Logger == nil {
		mo.Logger = h.log
	}
	h.svc = coretesting.NewService(mo)
	return h
}

// Service is the test model the extensions fill.
func (h *TestingHost) Service() *coretesting.Service { return h.svc }

// Close stops the continuous runs and cancels the runs of the model.
func (h *TestingHost) Close() { h.svc.Close() }

// bind makes the scope the one that fails an extension that panics. A host serves one scope.
func (h *TestingHost) bind(s *Scope) {
	h.mu.Lock()
	defer h.mu.Unlock()
	if h.scope != nil {
		h.log.Warn("a testing host serves one scope: the second is left without testing", "scope", s.opts.Info.Name)
		return
	}
	h.scope = s
}

// guard calls fn, which belongs to the extension of inst. A panic fails the extension (ExtensionTestingPanicked) and is the error of
// the call; so is a call of an extension that was deactivated (ExtensionDeactivated).
func (h *TestingHost) guard(inst *instance, where string, fn func() error) (err error) {
	defer func() {
		if r := recover(); r != nil {
			failure := errors.NewErrf("ExtensionTestingPanicked", "it panicked in %s: %v", where, r).SetMetadata("extension", inst.id, "scope", inst.scope.opts.Info.Name)
			inst.scope.post(func() { inst.scope.fail(inst.slot, failure, where) })
			err = failure
		}
	}()
	if inst.isDead() {
		return errors.New("ExtensionDeactivated", "the extension was deactivated", "extension", inst.id)
	}
	return fn()
}

// hostController is a controller an extension made.
type hostController struct {
	h    *TestingHost
	inst *instance
	id   string
	core *coretesting.Controller
	root *extension.TestItemCollection

	mu       sync.Mutex
	label    string
	resolve  func(ctx context.Context, item *extension.TestItem) error
	refresh  func(ctx context.Context) error
	profiles map[int]*hostProfile
	runs     []*hostRun
	disposed bool
}

var _ extension.TestController = (*hostController)(nil)

func toCoreRange(r *extension.Range) *coretesting.Range {
	if r == nil {
		return nil
	}
	return &coretesting.Range{Start: coretesting.Pos{Line: r.Start.Line, Col: r.Start.Col}, End: coretesting.Pos{Line: r.End.Line, Col: r.End.Col}}
}

func toCoreLocation(l *extension.Location) *coretesting.Location {
	if l == nil {
		return nil
	}
	return &coretesting.Location{URI: l.URI, Range: *toCoreRange(&l.Range)}
}

// observe follows the tree of the controller: the changes of its items become the model's.
func (c *hostController) observe(ch extension.TestTreeChange) {
	c.mu.Lock()
	gone := c.disposed
	c.mu.Unlock()
	if gone {
		return
	}
	id := coretesting.JoinID(c.id, ch.Path...)
	switch ch.Kind {
	case extension.TestItemRemoved:
		c.core.Remove(id)
	default:
		d := ch.Data
		err := c.core.Upsert(coretesting.Item{
			ID: id, URI: d.URI, Range: toCoreRange(d.Range), Label: d.Label, Description: d.Description, SortText: d.SortText,
			Tags: slices.Clone(d.Tags), Error: d.Error, Busy: d.Busy, CanResolveChildren: d.CanResolveChildren,
		})
		if err != nil {
			c.h.log.Warn("a test item could not be added to the model", "controller", c.id, "item", ch.Path, "err", err)
		}
	}
}

// lookup returns the extension's item for a model id, or nil for the controller's own id and for an id that is not in the tree.
func (c *hostController) lookup(id string) *extension.TestItem {
	parts := coretesting.SplitID(id)
	if len(parts) < 2 {
		return nil
	}
	var item *extension.TestItem
	coll := c.root
	for _, p := range parts[1:] {
		if item = coll.Get(p); item == nil {
			return nil
		}
		coll = item.Children()
	}
	return item
}

// idOf is the model id of an item of the controller's tree, and false for an item that is in no tree of this controller.
func (c *hostController) idOf(item *extension.TestItem) (string, bool) {
	if item == nil {
		return "", false
	}
	var parts []string
	top := item
	for x := item; x != nil; x = x.Parent() {
		parts = append(parts, x.ID())
		top = x
	}
	if c.root.Get(top.ID()) != top {
		return "", false
	}
	slices.Reverse(parts)
	return coretesting.JoinID(c.id, parts...), true
}

// items turns model ids into the extension's items; the controller's own id gives none, and says all.
func (c *hostController) items(ids []string) (items []*extension.TestItem, all bool) {
	for _, id := range ids {
		if id == c.id {
			all = true
			continue
		}
		if it := c.lookup(id); it != nil {
			items = append(items, it)
		}
	}
	return items, all
}

// ---- extension.TestController

// ID implements extension.TestController.
func (c *hostController) ID() string { return c.id }

// Label implements extension.TestController.
func (c *hostController) Label() string {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.label
}

// SetLabel implements extension.TestController.
func (c *hostController) SetLabel(label string) {
	c.mu.Lock()
	c.label = label
	c.mu.Unlock()
	c.core.SetLabel(label)
}

// Items implements extension.TestController.
func (c *hostController) Items() *extension.TestItemCollection { return c.root }

// CreateTestItem implements extension.TestController.
func (c *hostController) CreateTestItem(id, label, uri string) *extension.TestItem {
	return extension.NewTestItem(id, label, uri)
}

// SetResolveHandler implements extension.TestController.
func (c *hostController) SetResolveHandler(fn func(ctx context.Context, item *extension.TestItem) error) {
	c.mu.Lock()
	c.resolve = fn
	c.mu.Unlock()
}

// SetRefreshHandler implements extension.TestController.
func (c *hostController) SetRefreshHandler(fn func(ctx context.Context) error) {
	c.mu.Lock()
	c.refresh = fn
	c.mu.Unlock()
}

// callResolve is the model's Resolve function of the controller.
func (c *hostController) callResolve(ctx context.Context, id string) error {
	c.mu.Lock()
	fn := c.resolve
	c.mu.Unlock()
	if fn == nil {
		return nil
	}
	item := c.lookup(id)
	if item == nil && id != c.id {
		return nil // the item went away
	}
	return c.h.guard(c.inst, "the resolve handler of the controller "+c.id, func() error { return fn(ctx, item) })
}

// callRefresh is the model's Refresh function of the controller.
func (c *hostController) callRefresh(ctx context.Context) error {
	c.mu.Lock()
	fn := c.refresh
	c.mu.Unlock()
	if fn == nil {
		return nil
	}
	return c.h.guard(c.inst, "the refresh handler of the controller "+c.id, func() error { return fn(ctx) })
}

// InvalidateTestResults implements extension.TestController.
func (c *hostController) InvalidateTestResults(items ...*extension.TestItem) {
	if len(items) == 0 {
		c.h.svc.InvalidateResults(c.id)
		return
	}
	var ids []string
	for _, it := range items {
		if id, ok := c.idOf(it); ok {
			ids = append(ids, id)
		}
	}
	if len(ids) > 0 {
		c.h.svc.InvalidateResults(ids...)
	}
}

// Dispose implements extension.TestController.
func (c *hostController) Dispose() {
	c.mu.Lock()
	if c.disposed {
		c.mu.Unlock()
		return
	}
	c.disposed = true
	runs := slices.Clone(c.runs)
	c.mu.Unlock()
	for _, r := range runs {
		r.End()
	}
	c.h.mu.Lock()
	delete(c.h.controllers, c.id)
	c.h.mu.Unlock()
	c.core.Dispose()
}

// logGuard is guard for a call that has no result: a failure is logged.
func (h *TestingHost) logGuard(inst *instance, where string, fn func()) {
	if err := h.guard(inst, where, func() error { fn(); return nil }); err != nil {
		h.log.Debug("a call into an extension did not happen", "where", where, "err", err)
	}
}
