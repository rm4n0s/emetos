package exthost

import (
	"context"
	"slices"
	"strings"
	"time"

	"github.com/rm4n0s/errors"

	"github.com/rm4n0s/emetos/extension"
	coretesting "github.com/rm4n0s/emetos/internal/core/testing"
)

// The binding of extension.Tests to the testing host of a scope (SPEC §10.32 "The contract", step 26.1; after extHostTesting.ts).

// testsAPI is the extension.Tests of one activation.
type testsAPI struct{ inst *instance }

var _ extension.Tests = testsAPI{}

// Tests implements extension.API.
func (a *api) Tests() extension.Tests { return testsAPI{a.inst} }

// CreateTestController implements extension.Tests.
func (t testsAPI) CreateTestController(id, label string) (extension.TestController, error) {
	in := t.inst
	if in.isDead() {
		return nil, errors.New("ExtensionDeactivated", "the extension was deactivated", "extension", in.id)
	}
	h := in.scope.opts.Testing
	if h == nil {
		return nil, errors.New("ExtensionTestingUnavailable", "the scope has no testing service", "extension", in.id)
	}
	if id == "" || label == "" || strings.ContainsRune(id, 0) {
		return nil, errors.New("ExtensionTestingInvalid", "a test controller needs an id without NUL bytes and a label", "extension", in.id, "id", id)
	}
	c := &hostController{h: h, inst: in, id: id, label: label, profiles: map[int]*hostProfile{}}
	h.mu.Lock()
	if other, taken := h.controllers[id]; taken {
		h.mu.Unlock()
		return nil, errors.New("ExtensionTestingControllerTaken", "a test controller has this id already", "extension", in.id, "id", id, "other", other.inst.id)
	}
	h.controllers[id] = c
	h.mu.Unlock()
	core, err := h.svc.RegisterController(coretesting.ControllerOptions{ID: id, Label: label, Resolve: c.callResolve, Refresh: c.callRefresh})
	if err != nil {
		h.mu.Lock()
		delete(h.controllers, id)
		h.mu.Unlock()
		return nil, err
	}
	c.core = core
	c.root = extension.NewTestTree(c.observe)
	in.track(c.Dispose)
	return c, nil
}

// ---- profiles

// hostProfile is a run profile an extension made.
type hostProfile struct {
	c       *hostController
	id      int
	kind    extension.TestRunKind
	tag     string
	cont    bool
	handler extension.TestRunHandler
	opts    extension.TestRunProfileOptions

	label string
}

var _ extension.TestRunProfile = (*hostProfile)(nil)

func groupOf(k extension.TestRunKind) coretesting.Group {
	switch k {
	case extension.TestKindDebug:
		return coretesting.GroupDebug
	case extension.TestKindCoverage:
		return coretesting.GroupCoverage
	}
	return coretesting.GroupRun
}

// CreateRunProfile implements extension.TestController.
func (c *hostController) CreateRunProfile(label string, kind extension.TestRunKind, handler extension.TestRunHandler, opts extension.TestRunProfileOptions) (extension.TestRunProfile, error) {
	if c.inst.isDead() {
		return nil, errors.New("ExtensionDeactivated", "the extension was deactivated", "extension", c.inst.id)
	}
	if label == "" || handler == nil || kind < extension.TestKindRun || kind > extension.TestKindCoverage {
		return nil, errors.New("ExtensionTestingInvalid", "a run profile needs a label, a kind and a handler", "extension", c.inst.id, "controller", c.id, "label", label)
	}
	p := &hostProfile{c: c, kind: kind, tag: opts.Tag, cont: opts.SupportsContinuousRun, handler: handler, opts: opts, label: label}
	spec := coretesting.ProfileSpec{
		Label: label, Group: groupOf(kind), IsDefault: opts.IsDefault, Tag: opts.Tag, SupportsContinuous: opts.SupportsContinuousRun, Run: p.run,
	}
	if opts.ConfigureHandler != nil {
		spec.Configure = func() {
			c.h.logGuard(c.inst, "the configure handler of the profile "+label, func() { opts.ConfigureHandler() })
		}
	}
	if opts.ContinuousTrigger != nil {
		spec.Trigger = func(item coretesting.Item, uri string) bool {
			it := c.lookup(item.ID)
			if it == nil {
				return false
			}
			hit := false
			c.h.logGuard(c.inst, "the continuous trigger of the profile "+label, func() { hit = opts.ContinuousTrigger(it, uri) })
			return hit
		}
	}
	if opts.LoadDetailedCoverage != nil {
		spec.LoadCoverage = p.loadCoverage
	}
	id, err := c.core.AddProfile(spec)
	if err != nil {
		return nil, err
	}
	p.id = id
	c.mu.Lock()
	c.profiles[id] = p
	c.mu.Unlock()
	return p, nil
}

// run is the Run function of the profile in the model: it calls the extension's handler with a request made of the extension's items.
func (p *hostProfile) run(ctx context.Context, req coretesting.ProfileRequest) error {
	c := p.c
	include, all := c.items(req.Include)
	exclude, _ := c.items(req.Exclude)
	ereq := &extension.TestRunRequest{Exclude: exclude, Profile: p, Continuous: req.Continuous, PreserveFocus: req.PreserveFocus}
	if !all {
		ereq.Include = include
	}
	if req.ResultID != "" {
		c.h.mu.Lock()
		c.h.trackers[ereq] = req.ResultID
		c.h.mu.Unlock()
		defer func() {
			c.h.mu.Lock()
			delete(c.h.trackers, ereq)
			c.h.mu.Unlock()
		}()
	}
	return c.h.guard(c.inst, "the run handler of the profile "+p.Label(), func() error { return p.handler(ctx, ereq) })
}

// loadCoverage is the loader of the details of the coverage this profile measures.
func (p *hostProfile) loadCoverage(ctx context.Context, f coretesting.FileCoverage) (coretesting.Details, error) {
	var d extension.CoverageDetails
	err := p.c.h.guard(p.c.inst, "the coverage loader of the profile "+p.Label(), func() error {
		var err error
		d, err = p.opts.LoadDetailedCoverage(ctx, fromCoreCoverage(f))
		return err
	})
	if err != nil {
		return coretesting.Details{}, err
	}
	return toCoreDetails(d), nil
}

func fromCoreCount(c *coretesting.Count) *extension.CoverageCount {
	if c == nil {
		return nil
	}
	return &extension.CoverageCount{Covered: c.Covered, Total: c.Total}
}

func toCoreCount(c *extension.CoverageCount) *coretesting.Count {
	if c == nil {
		return nil
	}
	return &coretesting.Count{Covered: c.Covered, Total: c.Total}
}

func fromCoreCoverage(f coretesting.FileCoverage) extension.FileCoverage {
	return extension.FileCoverage{
		URI: f.URI, Statement: extension.CoverageCount{Covered: f.Statement.Covered, Total: f.Statement.Total},
		Branch: fromCoreCount(f.Branch), Declaration: fromCoreCount(f.Declaration), Data: f.Data,
	}
}

func toCoreCoverage(f extension.FileCoverage) coretesting.FileCoverage {
	return coretesting.FileCoverage{
		URI: f.URI, Statement: coretesting.Count{Covered: f.Statement.Covered, Total: f.Statement.Total},
		Branch: toCoreCount(f.Branch), Declaration: toCoreCount(f.Declaration), Data: f.Data,
	}
}

func toCoreDetails(d extension.CoverageDetails) coretesting.Details {
	var out coretesting.Details
	for _, s := range d.Statements {
		st := coretesting.StatementDetail{Executed: s.Executed, Location: *toCoreRange(&s.Location)}
		for _, b := range s.Branches {
			st.Branches = append(st.Branches, coretesting.BranchDetail{Executed: b.Executed, Location: toCoreRange(b.Location), Label: b.Label})
		}
		out.Statements = append(out.Statements, st)
	}
	for _, de := range d.Declarations {
		out.Declarations = append(out.Declarations, coretesting.DeclarationDetail{Name: de.Name, Executed: de.Executed, Location: *toCoreRange(&de.Location)})
	}
	return out
}

// Label implements extension.TestRunProfile.
func (p *hostProfile) Label() string {
	p.c.mu.Lock()
	defer p.c.mu.Unlock()
	return p.label
}

// Kind implements extension.TestRunProfile.
func (p *hostProfile) Kind() extension.TestRunKind { return p.kind }

// IsDefault implements extension.TestRunProfile. It reads the model, where the user's choice of the default counts.
func (p *hostProfile) IsDefault() bool {
	for _, info := range p.c.h.svc.Profiles(p.c.id) {
		if info.ID == p.id {
			return info.IsDefault
		}
	}
	return false
}

// SetIsDefault implements extension.TestRunProfile.
func (p *hostProfile) SetIsDefault(v bool) { p.c.core.UpdateProfile(p.id, nil, &v) }

// SetLabel implements extension.TestRunProfile.
func (p *hostProfile) SetLabel(label string) {
	if label == "" {
		return
	}
	p.c.mu.Lock()
	p.label = label
	p.c.mu.Unlock()
	p.c.core.UpdateProfile(p.id, &label, nil)
}

// Tag implements extension.TestRunProfile.
func (p *hostProfile) Tag() string { return p.tag }

// SupportsContinuousRun implements extension.TestRunProfile.
func (p *hostProfile) SupportsContinuousRun() bool { return p.cont }

// Dispose implements extension.TestRunProfile.
func (p *hostProfile) Dispose() {
	p.c.mu.Lock()
	delete(p.c.profiles, p.id)
	p.c.mu.Unlock()
	p.c.core.RemoveProfile(p.id)
}

// ---- runs

// hostRun is a TestRun of an extension: a task of a result.
type hostRun struct {
	c    *hostController
	run  *coretesting.Run
	name string
}

var _ extension.TestRun = (*hostRun)(nil)

// CreateTestRun implements extension.TestController.
func (c *hostController) CreateTestRun(request *extension.TestRunRequest, name string, persist bool) (extension.TestRun, error) {
	if c.inst.isDead() {
		return nil, errors.New("ExtensionDeactivated", "the extension was deactivated", "extension", c.inst.id)
	}
	if request == nil {
		return nil, errors.New("ExtensionTestingInvalid", "a test run needs the request it runs", "extension", c.inst.id, "controller", c.id)
	}
	if name == "" {
		name = c.Label()
	}
	var loader coretesting.CoverageLoader
	var prof *hostProfile
	if hp, ok := request.Profile.(*hostProfile); ok && hp.c == c {
		prof = hp
		if hp.opts.LoadDetailedCoverage != nil {
			loader = hp.loadCoverage
		}
	}
	c.h.mu.Lock()
	resultID, bound := c.h.trackers[request]
	c.h.mu.Unlock()
	var res *coretesting.Result
	if bound {
		res = c.h.svc.Result(resultID)
	}
	if res == nil || res.IsComplete() {
		res = c.h.svc.StartExternalRun(coretesting.ExternalRun{Request: c.externalRequest(request, prof), Persist: persist})
	}
	run, err := res.AddTask(coretesting.TaskSpec{Name: name, ControllerID: c.id, LoadCoverage: loader})
	if err != nil {
		return nil, err
	}
	r := &hostRun{c: c, run: run, name: name}
	c.mu.Lock()
	c.runs = append(c.runs, r)
	c.mu.Unlock()
	return r, nil
}

// externalRequest is the model's request for a run the extension starts on its own.
func (c *hostController) externalRequest(req *extension.TestRunRequest, prof *hostProfile) coretesting.RunRequest {
	out := coretesting.RunRequest{Group: coretesting.GroupRun, Continuous: req.Continuous, PreserveFocus: req.PreserveFocus}
	if prof != nil {
		out.Group = groupOf(prof.kind)
		ids := []string{c.id}
		if req.Include != nil {
			ids = nil
			for _, it := range req.Include {
				if id, ok := c.idOf(it); ok {
					ids = append(ids, id)
				}
			}
		}
		out.Targets = []coretesting.RunTarget{{ControllerID: c.id, ProfileID: prof.id, TestIDs: ids}}
	}
	for _, it := range req.Exclude {
		if id, ok := c.idOf(it); ok {
			out.Exclude = append(out.Exclude, id)
		}
	}
	return out
}

func coreDuration(d time.Duration) time.Duration {
	if d < 0 {
		return coretesting.NoDuration
	}
	return d
}

func (r *hostRun) id(item *extension.TestItem) (string, bool) {
	id, ok := r.c.idOf(item)
	if !ok {
		r.c.h.log.Warn("a test run was told about an item that is not in the controller's tree", "controller", r.c.id)
	}
	return id, ok
}

// Name implements extension.TestRun.
func (r *hostRun) Name() string { return r.name }

func (r *hostRun) set(item *extension.TestItem, st coretesting.State, d time.Duration, msgs []extension.TestMessage) {
	id, ok := r.id(item)
	if !ok {
		return
	}
	for _, m := range msgs {
		r.run.AddMessage(id, toCoreMessage(m))
	}
	r.run.SetState(id, st, coreDuration(d))
}

// Enqueued implements extension.TestRun.
func (r *hostRun) Enqueued(item *extension.TestItem) {
	r.set(item, coretesting.StateQueued, coretesting.NoDuration, nil)
}

// Started implements extension.TestRun.
func (r *hostRun) Started(item *extension.TestItem) {
	r.set(item, coretesting.StateRunning, coretesting.NoDuration, nil)
}

// Skipped implements extension.TestRun.
func (r *hostRun) Skipped(item *extension.TestItem) {
	r.set(item, coretesting.StateSkipped, coretesting.NoDuration, nil)
}

// Passed implements extension.TestRun.
func (r *hostRun) Passed(item *extension.TestItem, d time.Duration) {
	r.set(item, coretesting.StatePassed, d, nil)
}

// Failed implements extension.TestRun.
func (r *hostRun) Failed(item *extension.TestItem, msgs []extension.TestMessage, d time.Duration) {
	r.set(item, coretesting.StateFailed, d, msgs)
}

// Errored implements extension.TestRun.
func (r *hostRun) Errored(item *extension.TestItem, msgs []extension.TestMessage, d time.Duration) {
	r.set(item, coretesting.StateErrored, d, msgs)
}

func toCoreMessage(m extension.TestMessage) coretesting.Message {
	out := coretesting.Message{
		Type: coretesting.MessageError, Text: m.Message, Markdown: m.Markdown, Expected: m.ExpectedOutput, Actual: m.ActualOutput,
		HasDiff: m.ExpectedOutput != "" && m.ActualOutput != "", Location: toCoreLocation(m.Location), ContextValue: m.ContextValue,
	}
	for _, f := range m.StackTrace {
		sf := coretesting.StackFrame{Label: f.Label, URI: f.URI}
		if f.Position != nil {
			sf.Pos = &coretesting.Pos{Line: f.Position.Line, Col: f.Position.Col}
		}
		out.StackTrace = append(out.StackTrace, sf)
	}
	return out
}

// AppendOutput implements extension.TestRun.
func (r *hostRun) AppendOutput(text string, loc *extension.Location, item *extension.TestItem) {
	id := ""
	if item != nil {
		var ok bool
		if id, ok = r.id(item); !ok {
			id = ""
		}
	}
	r.run.AppendOutput([]byte(text), toCoreLocation(loc), id)
}

// AddCoverage implements extension.TestRun.
func (r *hostRun) AddCoverage(f extension.FileCoverage) { r.run.AddCoverage(toCoreCoverage(f)) }

// End implements extension.TestRun.
func (r *hostRun) End() {
	r.run.End()
	c := r.c
	c.mu.Lock()
	c.runs = slices.DeleteFunc(c.runs, func(x *hostRun) bool { return x == r })
	c.mu.Unlock()
}
