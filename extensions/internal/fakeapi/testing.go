package fakeapi

import (
	"context"
	"slices"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/rm4n0s/errors"

	"github.com/rm4n0s/emetos/extension"
)

// The Testing API of the fake (SPEC §10.32 "The contract", step 26.1): the controllers an extension made, their items and run
// profiles, and the runs it reported, in memory, with the rules of the host (internal/gui/exthost/testingapi.go): the same errors, the
// same disposal with the activation, a panic in a handler taking the extension off. A test plays the part of Emetos: it resolves and
// refreshes, starts a run of a profile with the items to include and exclude, asks for the details of the coverage, and reads what the
// extension reported.

// testingState is the testing state of the fake; the API's mutex guards it.
type testingState struct {
	controllers map[string]*FakeTestController
	order       []string
	unavailable bool
	dead        bool
}

// Tests implements extension.API.
func (a *API) Tests() extension.Tests { return testsAPI{a} }

type testsAPI struct{ a *API }

// SetTestingUnavailable makes CreateTestController fail with ExtensionTestingUnavailable, as it does in a host with no testing service.
func (a *API) SetTestingUnavailable(unavailable bool) {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.testing.unavailable = unavailable
}

// TestController returns the controller the extension made with the id, or nil.
func (a *API) TestController(id string) *FakeTestController {
	a.mu.Lock()
	defer a.mu.Unlock()
	return a.testing.controllers[id]
}

// TestControllers lists the controllers, in the order they were made.
func (a *API) TestControllers() []*FakeTestController {
	a.mu.Lock()
	defer a.mu.Unlock()
	out := make([]*FakeTestController, 0, len(a.testing.order))
	for _, id := range a.testing.order {
		out = append(out, a.testing.controllers[id])
	}
	return out
}

// CreateTestController implements extension.Tests with the errors of the host.
func (t testsAPI) CreateTestController(id, label string) (extension.TestController, error) {
	a := t.a
	a.mu.Lock()
	defer a.mu.Unlock()
	switch {
	case a.testing.dead:
		return nil, errors.New("ExtensionDeactivated", "the extension was deactivated")
	case a.testing.unavailable:
		return nil, errors.New("ExtensionTestingUnavailable", "the scope has no testing service")
	case id == "" || label == "" || strings.ContainsRune(id, 0):
		return nil, errors.New("ExtensionTestingInvalid", "a test controller needs an id without NUL bytes and a label", "id", id)
	}
	if _, taken := a.testing.controllers[id]; taken {
		return nil, errors.New("ExtensionTestingControllerTaken", "a test controller has this id already", "id", id)
	}
	c := &FakeTestController{a: a, id: id, label: label}
	c.root = extension.NewTestTree(func(ch extension.TestTreeChange) {
		c.mu.Lock()
		c.changes = append(c.changes, ch)
		c.mu.Unlock()
	})
	if a.testing.controllers == nil {
		a.testing.controllers = map[string]*FakeTestController{}
	}
	a.testing.controllers[id] = c
	a.testing.order = append(a.testing.order, id)
	return c, nil
}

// FakeTestController is a test controller of the extension, with the means for a test to play Emetos.
type FakeTestController struct {
	a        *API
	id       string
	root     *extension.TestItemCollection
	disposed atomic.Bool

	mu          sync.Mutex
	label       string
	resolve     func(ctx context.Context, item *extension.TestItem) error
	refresh     func(ctx context.Context) error
	changes     []extension.TestTreeChange
	profiles    []*FakeTestProfile
	runs        []*FakeTestRun
	invalidated [][]*extension.TestItem
}

var _ extension.TestController = (*FakeTestController)(nil)

// ID implements extension.TestController.
func (c *FakeTestController) ID() string { return c.id }

// Label implements extension.TestController.
func (c *FakeTestController) Label() string {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.label
}

// SetLabel implements extension.TestController.
func (c *FakeTestController) SetLabel(label string) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.label = label
}

// Items implements extension.TestController.
func (c *FakeTestController) Items() *extension.TestItemCollection { return c.root }

// CreateTestItem implements extension.TestController.
func (c *FakeTestController) CreateTestItem(id, label, uri string) *extension.TestItem {
	return extension.NewTestItem(id, label, uri)
}

// SetResolveHandler implements extension.TestController.
func (c *FakeTestController) SetResolveHandler(fn func(ctx context.Context, item *extension.TestItem) error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.resolve = fn
}

// SetRefreshHandler implements extension.TestController.
func (c *FakeTestController) SetRefreshHandler(fn func(ctx context.Context) error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.refresh = fn
}

// Dispose implements extension.TestController.
func (c *FakeTestController) Dispose() {
	if c.disposed.Swap(true) {
		return
	}
	c.a.mu.Lock()
	if c.a.testing.controllers[c.id] == c {
		delete(c.a.testing.controllers, c.id)
		c.a.testing.order = slices.DeleteFunc(c.a.testing.order, func(x string) bool { return x == c.id })
	}
	c.a.mu.Unlock()
	c.mu.Lock()
	runs := slices.Clone(c.runs)
	c.mu.Unlock()
	for _, r := range runs {
		r.End()
	}
}

// Disposed reports whether the controller was disposed, by the extension or by the end of the activation.
func (c *FakeTestController) Disposed() bool { return c.disposed.Load() }

// InvalidateTestResults implements extension.TestController.
func (c *FakeTestController) InvalidateTestResults(items ...*extension.TestItem) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.invalidated = append(c.invalidated, slices.Clone(items))
}

// Invalidated lists the calls of InvalidateTestResults with the items they named.
func (c *FakeTestController) Invalidated() [][]*extension.TestItem {
	c.mu.Lock()
	defer c.mu.Unlock()
	return slices.Clone(c.invalidated)
}

// Changes lists the changes of the controller's tree, in order.
func (c *FakeTestController) Changes() []extension.TestTreeChange {
	c.mu.Lock()
	defer c.mu.Unlock()
	return slices.Clone(c.changes)
}

// guard runs a function of the extension, as the host does: a panic takes the extension off.
func (c *FakeTestController) guard(what string, fn func() error) (err error) {
	defer func() {
		if r := recover(); r != nil {
			c.a.scmFail(what, r)
			err = errors.NewErrf("ExtensionTestingPanicked", "it panicked in %s: %v", what, r)
		}
	}()
	return fn()
}

// Resolve plays the resolve of an item (nil for the whole tree): it calls the resolve handler, and does nothing without one.
func (c *FakeTestController) Resolve(ctx context.Context, item *extension.TestItem) error {
	c.mu.Lock()
	fn := c.resolve
	c.mu.Unlock()
	if fn == nil {
		return nil
	}
	return c.guard("the resolve handler", func() error { return fn(ctx, item) })
}

// Refresh plays the Refresh Tests command: it calls the refresh handler, and does nothing without one.
func (c *FakeTestController) Refresh(ctx context.Context) error {
	c.mu.Lock()
	fn := c.refresh
	c.mu.Unlock()
	if fn == nil {
		return nil
	}
	return c.guard("the refresh handler", func() error { return fn(ctx) })
}

// FakeTestProfile is a run profile of the extension.
type FakeTestProfile struct {
	c       *FakeTestController
	kind    extension.TestRunKind
	handler extension.TestRunHandler
	opts    extension.TestRunProfileOptions

	mu        sync.Mutex
	label     string
	isDefault bool
	disposed  bool
}

var _ extension.TestRunProfile = (*FakeTestProfile)(nil)

// CreateRunProfile implements extension.TestController with the errors of the host.
func (c *FakeTestController) CreateRunProfile(label string, kind extension.TestRunKind, handler extension.TestRunHandler, opts extension.TestRunProfileOptions) (extension.TestRunProfile, error) {
	if c.disposed.Load() {
		return nil, errors.New("ExtensionDeactivated", "the extension was deactivated")
	}
	if label == "" || handler == nil || kind < extension.TestKindRun || kind > extension.TestKindCoverage {
		return nil, errors.New("ExtensionTestingInvalid", "a run profile needs a label, a kind and a handler", "controller", c.id, "label", label)
	}
	p := &FakeTestProfile{c: c, kind: kind, handler: handler, opts: opts, label: label, isDefault: opts.IsDefault}
	c.mu.Lock()
	c.profiles = append(c.profiles, p)
	c.mu.Unlock()
	return p, nil
}

// Profiles lists the profiles that were made and not disposed.
func (c *FakeTestController) Profiles() []*FakeTestProfile {
	c.mu.Lock()
	defer c.mu.Unlock()
	return slices.DeleteFunc(slices.Clone(c.profiles), func(p *FakeTestProfile) bool {
		p.mu.Lock()
		defer p.mu.Unlock()
		return p.disposed
	})
}

// Label implements extension.TestRunProfile.
func (p *FakeTestProfile) Label() string {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.label
}

// Kind implements extension.TestRunProfile.
func (p *FakeTestProfile) Kind() extension.TestRunKind { return p.kind }

// IsDefault implements extension.TestRunProfile.
func (p *FakeTestProfile) IsDefault() bool {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.isDefault
}

// SetIsDefault implements extension.TestRunProfile.
func (p *FakeTestProfile) SetIsDefault(v bool) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.isDefault = v
}

// SetLabel implements extension.TestRunProfile.
func (p *FakeTestProfile) SetLabel(label string) {
	p.mu.Lock()
	defer p.mu.Unlock()
	if label != "" {
		p.label = label
	}
}

// Tag implements extension.TestRunProfile.
func (p *FakeTestProfile) Tag() string { return p.opts.Tag }

// SupportsContinuousRun implements extension.TestRunProfile.
func (p *FakeTestProfile) SupportsContinuousRun() bool { return p.opts.SupportsContinuousRun }

// Dispose implements extension.TestRunProfile.
func (p *FakeTestProfile) Dispose() {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.disposed = true
}

// Configure plays the user asking to configure the profile; it reports whether the profile has a configure handler.
func (p *FakeTestProfile) Configure() (bool, error) {
	if p.opts.ConfigureHandler == nil {
		return false, nil
	}
	return true, p.c.guard("the configure handler", func() error { p.opts.ConfigureHandler(); return nil })
}

// LoadCoverage plays the loading of the details of a file's coverage.
func (p *FakeTestProfile) LoadCoverage(ctx context.Context, file extension.FileCoverage) (extension.CoverageDetails, error) {
	if p.opts.LoadDetailedCoverage == nil {
		return extension.CoverageDetails{}, errors.New("ExtensionTestingInvalid", "the profile has no coverage loader", "controller", p.c.id)
	}
	var out extension.CoverageDetails
	err := p.c.guard("the coverage loader", func() error {
		var err error
		out, err = p.opts.LoadDetailedCoverage(ctx, file)
		return err
	})
	return out, err
}

// Triggers plays the question whether the change of the file starts the continuous run of the item again.
func (p *FakeTestProfile) Triggers(item *extension.TestItem, uri string) (bool, error) {
	if p.opts.ContinuousTrigger == nil {
		return item.Data().URI == uri, nil
	}
	hit := false
	err := p.c.guard("the continuous trigger", func() error { hit = p.opts.ContinuousTrigger(item, uri); return nil })
	return hit, err
}

// Run plays a run asked for by the user: it makes the request and calls the profile's handler with it. The runs the handler makes with
// CreateTestRun for the request are in Runs. It returns the request and the handler's error.
func (p *FakeTestProfile) Run(ctx context.Context, include, exclude []*extension.TestItem, continuous bool) (*extension.TestRunRequest, error) {
	req := &extension.TestRunRequest{Include: include, Exclude: exclude, Profile: p, Continuous: continuous}
	err := p.c.guard("the run handler", func() error { return p.handler(ctx, req) })
	return req, err
}

// ---- runs

// TestRunEventKind says what a TestRunEvent is.
type TestRunEventKind string

// The kinds of event of a run.
const (
	RunEnqueued TestRunEventKind = "enqueued"
	RunStarted  TestRunEventKind = "started"
	RunSkipped  TestRunEventKind = "skipped"
	RunPassed   TestRunEventKind = "passed"
	RunFailed   TestRunEventKind = "failed"
	RunErrored  TestRunEventKind = "errored"
	RunOutput   TestRunEventKind = "output"
	RunCoverage TestRunEventKind = "coverage"
	RunEnded    TestRunEventKind = "ended"
)

// TestRunEvent is something a run reported.
type TestRunEvent struct {
	Kind TestRunEventKind
	// Item is the item of a state or of an output; Duration and Messages come with the results.
	Item     *extension.TestItem
	Duration time.Duration
	Messages []extension.TestMessage
	// Text and Location are those of an output; Coverage is the file of a coverage report.
	Text     string
	Location *extension.Location
	Coverage extension.FileCoverage
}

// FakeTestRun is a run of the extension that reports into a list.
type FakeTestRun struct {
	c       *FakeTestController
	name    string
	persist bool
	request *extension.TestRunRequest

	mu     sync.Mutex
	events []TestRunEvent
	ended  bool
}

var _ extension.TestRun = (*FakeTestRun)(nil)

// CreateTestRun implements extension.TestController with the errors of the host.
func (c *FakeTestController) CreateTestRun(request *extension.TestRunRequest, name string, persist bool) (extension.TestRun, error) {
	if c.disposed.Load() {
		return nil, errors.New("ExtensionDeactivated", "the extension was deactivated")
	}
	if request == nil {
		return nil, errors.New("ExtensionTestingInvalid", "a test run needs the request it runs", "controller", c.id)
	}
	if name == "" {
		name = c.Label()
	}
	r := &FakeTestRun{c: c, name: name, persist: persist, request: request}
	c.mu.Lock()
	c.runs = append(c.runs, r)
	c.mu.Unlock()
	return r, nil
}

// Runs lists the runs the extension made, in order.
func (c *FakeTestController) Runs() []*FakeTestRun {
	c.mu.Lock()
	defer c.mu.Unlock()
	return slices.Clone(c.runs)
}

// Name implements extension.TestRun.
func (r *FakeTestRun) Name() string { return r.name }

// Persist is the persist argument the run was made with, and Request the request.
func (r *FakeTestRun) Persist() bool { return r.persist }

// Request is the request the run was made for.
func (r *FakeTestRun) Request() *extension.TestRunRequest { return r.request }

// Events lists what the run reported, in order.
func (r *FakeTestRun) Events() []TestRunEvent {
	r.mu.Lock()
	defer r.mu.Unlock()
	return slices.Clone(r.events)
}

// Ended reports whether End was called.
func (r *FakeTestRun) Ended() bool {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.ended
}

func (r *FakeTestRun) add(e TestRunEvent) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if !r.ended {
		r.events = append(r.events, e)
	}
}

// Enqueued implements extension.TestRun.
func (r *FakeTestRun) Enqueued(item *extension.TestItem) {
	r.add(TestRunEvent{Kind: RunEnqueued, Item: item, Duration: extension.TestNoDuration})
}

// Started implements extension.TestRun.
func (r *FakeTestRun) Started(item *extension.TestItem) {
	r.add(TestRunEvent{Kind: RunStarted, Item: item, Duration: extension.TestNoDuration})
}

// Skipped implements extension.TestRun.
func (r *FakeTestRun) Skipped(item *extension.TestItem) {
	r.add(TestRunEvent{Kind: RunSkipped, Item: item, Duration: extension.TestNoDuration})
}

// Passed implements extension.TestRun.
func (r *FakeTestRun) Passed(item *extension.TestItem, d time.Duration) {
	r.add(TestRunEvent{Kind: RunPassed, Item: item, Duration: d})
}

// Failed implements extension.TestRun.
func (r *FakeTestRun) Failed(item *extension.TestItem, msgs []extension.TestMessage, d time.Duration) {
	r.add(TestRunEvent{Kind: RunFailed, Item: item, Messages: slices.Clone(msgs), Duration: d})
}

// Errored implements extension.TestRun.
func (r *FakeTestRun) Errored(item *extension.TestItem, msgs []extension.TestMessage, d time.Duration) {
	r.add(TestRunEvent{Kind: RunErrored, Item: item, Messages: slices.Clone(msgs), Duration: d})
}

// AppendOutput implements extension.TestRun.
func (r *FakeTestRun) AppendOutput(text string, loc *extension.Location, item *extension.TestItem) {
	r.add(TestRunEvent{Kind: RunOutput, Text: text, Location: loc, Item: item, Duration: extension.TestNoDuration})
}

// AddCoverage implements extension.TestRun.
func (r *FakeTestRun) AddCoverage(f extension.FileCoverage) {
	r.add(TestRunEvent{Kind: RunCoverage, Coverage: f, Duration: extension.TestNoDuration})
}

// End implements extension.TestRun.
func (r *FakeTestRun) End() {
	r.mu.Lock()
	defer r.mu.Unlock()
	if !r.ended {
		r.ended = true
		r.events = append(r.events, TestRunEvent{Kind: RunEnded, Duration: extension.TestNoDuration})
	}
}
