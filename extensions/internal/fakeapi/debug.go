package fakeapi

import (
	"context"
	"encoding/json"
	"slices"
	"strconv"
	"strings"
	"sync"

	"github.com/rm4n0s/errors"

	"github.com/rm4n0s/emetos/extension"
)

// The Debug API of the fake (SPEC §10.31 "The extension contract", step 25.3): the descriptor factories, configuration providers and
// tracker factories an extension registered, in memory, with the rules of the host (internal/gui/exthost/debugapi.go): the same errors,
// the same disposal with the activation, the providers called in the order they were registered, a panic taking the extension off. A
// test plays the part of Emetos: it asks for the adapter of a session, resolves a configuration, makes trackers, and makes sessions
// start and end to see what the listeners of the extension do.

// debugState is the debug state of the fake; the API's mutex guards it.
type debugState struct {
	factories   map[string]extension.DebugAdapterFactory
	providers   []*debugProvider
	trackers    []*debugTrackerFactory
	seq         int
	starts      []DebugStart
	startFn     func(DebugStart) (bool, error)
	sessions    []*FakeDebugSession
	active      *FakeDebugSession
	breakpoints []extension.DebugBreakpoint
	nextBP      int
	console     strings.Builder
	unavailable bool

	startFns  []*debugListener[func(extension.DebugSession)]
	endFns    []*debugListener[func(extension.DebugSession)]
	activeFns []*debugListener[func(extension.DebugSession)]
	customFns []*debugListener[func(extension.DebugSessionCustomEvent)]
	bpFns     []*debugListener[func(extension.DebugBreakpointsChange)]
}

type debugProvider struct {
	seq     int
	typ     string
	p       extension.DebugConfigurationProvider
	trigger extension.DebugProviderTrigger
}

type debugTrackerFactory struct {
	seq int
	typ string
	f   extension.DebugAdapterTrackerFactory
}

type debugListener[F any] struct{ fn F }

// DebugStart is a call of StartDebugging.
type DebugStart struct {
	Folder  string
	Target  extension.DebugTarget
	Options extension.DebugStartOptions
}

// Debug implements extension.API.
func (a *API) Debug() extension.Debug { return debugAPI{a} }

type debugAPI struct{ a *API }

// SetDebugUnavailable makes the registrations and StartDebugging fail with ExtensionDebugUnavailable, as they do in a host with no
// debug service.
func (a *API) SetDebugUnavailable(unavailable bool) {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.debug.unavailable = unavailable
}

// register checks what the registrations need alike, with the errors of the host. The caller holds the lock.
func (d debugAPI) register(debugType string, noFunction bool) error {
	a := d.a
	switch {
	case a.scm.dead:
		return errors.New("ExtensionDeactivated", "the extension was deactivated")
	case a.debug.unavailable:
		return errors.New("ExtensionDebugUnavailable", "the scope has no debug service")
	case debugType == "" || noFunction:
		return errors.New("ExtensionDebugInvalid", "a debug registration needs a debug type and a function", "type", debugType)
	}
	return nil
}

// RegisterAdapterDescriptorFactory implements extension.Debug with the errors of the host.
func (d debugAPI) RegisterAdapterDescriptorFactory(debugType string, f extension.DebugAdapterFactory) (func(), error) {
	a := d.a
	a.mu.Lock()
	defer a.mu.Unlock()
	if err := d.register(debugType, f == nil); err != nil {
		return nil, err
	}
	if _, taken := a.debug.factories[debugType]; taken {
		return nil, errors.New("ExtensionDebugFactoryTaken", "the debug type has an adapter factory already", "type", debugType)
	}
	if a.debug.factories == nil {
		a.debug.factories = map[string]extension.DebugAdapterFactory{}
	}
	a.debug.factories[debugType] = f
	return func() {
		a.mu.Lock()
		defer a.mu.Unlock()
		delete(a.debug.factories, debugType)
	}, nil
}

// RegisterConfigurationProvider implements extension.Debug with the errors of the host.
func (d debugAPI) RegisterConfigurationProvider(debugType string, p extension.DebugConfigurationProvider, trigger extension.DebugProviderTrigger) (func(), error) {
	a := d.a
	a.mu.Lock()
	defer a.mu.Unlock()
	if err := d.register(debugType, p.Provide == nil && p.Resolve == nil && p.ResolveSubstituted == nil); err != nil {
		return nil, err
	}
	a.debug.seq++
	e := &debugProvider{seq: a.debug.seq, typ: debugType, p: p, trigger: trigger}
	a.debug.providers = append(a.debug.providers, e)
	return func() {
		a.mu.Lock()
		defer a.mu.Unlock()
		a.debug.providers = slices.DeleteFunc(a.debug.providers, func(x *debugProvider) bool { return x == e })
	}, nil
}

// RegisterAdapterTrackerFactory implements extension.Debug with the errors of the host.
func (d debugAPI) RegisterAdapterTrackerFactory(debugType string, f extension.DebugAdapterTrackerFactory) (func(), error) {
	a := d.a
	a.mu.Lock()
	defer a.mu.Unlock()
	if err := d.register(debugType, f == nil); err != nil {
		return nil, err
	}
	a.debug.seq++
	e := &debugTrackerFactory{seq: a.debug.seq, typ: debugType, f: f}
	a.debug.trackers = append(a.debug.trackers, e)
	return func() {
		a.mu.Lock()
		defer a.mu.Unlock()
		a.debug.trackers = slices.DeleteFunc(a.debug.trackers, func(x *debugTrackerFactory) bool { return x == e })
	}, nil
}

// OnDebugStart sets the function that answers StartDebugging; without one it answers true. It is called with the lock released.
func (a *API) OnDebugStart(fn func(DebugStart) (bool, error)) {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.debug.startFn = fn
}

// StartDebugging implements extension.Debug: the call is recorded (DebugStarts) and answered by OnDebugStart.
func (d debugAPI) StartDebugging(_ context.Context, folder string, target extension.DebugTarget, opts extension.DebugStartOptions) (bool, error) {
	a := d.a
	a.mu.Lock()
	switch {
	case a.scm.dead:
		a.mu.Unlock()
		return false, errors.New("ExtensionDeactivated", "the extension was deactivated")
	case a.debug.unavailable:
		a.mu.Unlock()
		return false, errors.New("ExtensionDebugUnavailable", "the scope has no debug service")
	case target.Name == "" && target.Config == nil:
		a.mu.Unlock()
		return false, errors.New("ExtensionDebugInvalid", "StartDebugging needs the name of a configuration or a configuration")
	}
	call := DebugStart{Folder: folder, Target: target, Options: opts}
	a.debug.starts = append(a.debug.starts, call)
	fn := a.debug.startFn
	a.mu.Unlock()
	if fn == nil {
		return true, nil
	}
	return fn(call)
}

// DebugStarts are the calls of StartDebugging, the first first.
func (a *API) DebugStarts() []DebugStart {
	a.mu.Lock()
	defer a.mu.Unlock()
	return slices.Clone(a.debug.starts)
}

// StopDebugging implements extension.Debug: the session ends (EndDebugSession); nil ends all of them.
func (d debugAPI) StopDebugging(_ context.Context, session extension.DebugSession) error {
	a := d.a
	a.mu.Lock()
	if a.scm.dead {
		a.mu.Unlock()
		return errors.New("ExtensionDeactivated", "the extension was deactivated")
	}
	var stop []*FakeDebugSession
	for _, s := range a.debug.sessions {
		if session == nil || extension.DebugSession(s) == session {
			stop = append(stop, s)
		}
	}
	a.mu.Unlock()
	for _, s := range stop {
		a.EndDebugSession(s)
	}
	return nil
}

// ActiveSession implements extension.Debug.
func (d debugAPI) ActiveSession() extension.DebugSession {
	d.a.mu.Lock()
	defer d.a.mu.Unlock()
	if d.a.debug.active == nil {
		return nil
	}
	return d.a.debug.active
}

// Sessions implements extension.Debug.
func (d debugAPI) Sessions() []extension.DebugSession {
	d.a.mu.Lock()
	defer d.a.mu.Unlock()
	out := make([]extension.DebugSession, len(d.a.debug.sessions))
	for i, s := range d.a.debug.sessions {
		out[i] = s
	}
	return out
}

// debugListen adds a listener; it is removed by the function returned and when the activation ends.
func debugListen[F any](a *API, list func(*debugState) *[]*debugListener[F], fn F) func() {
	a.mu.Lock()
	defer a.mu.Unlock()
	if a.scm.dead || a.debug.unavailable {
		return func() {}
	}
	l := &debugListener[F]{fn: fn}
	p := list(&a.debug)
	*p = append(*p, l)
	return func() {
		a.mu.Lock()
		defer a.mu.Unlock()
		q := list(&a.debug)
		*q = slices.DeleteFunc(*q, func(x *debugListener[F]) bool { return x == l })
	}
}

// OnDidStartSession implements extension.Debug.
func (d debugAPI) OnDidStartSession(fn func(extension.DebugSession)) func() {
	return debugListen(d.a, func(s *debugState) *[]*debugListener[func(extension.DebugSession)] { return &s.startFns }, fn)
}

// OnDidTerminateSession implements extension.Debug.
func (d debugAPI) OnDidTerminateSession(fn func(extension.DebugSession)) func() {
	return debugListen(d.a, func(s *debugState) *[]*debugListener[func(extension.DebugSession)] { return &s.endFns }, fn)
}

// OnDidChangeActiveSession implements extension.Debug.
func (d debugAPI) OnDidChangeActiveSession(fn func(extension.DebugSession)) func() {
	return debugListen(d.a, func(s *debugState) *[]*debugListener[func(extension.DebugSession)] { return &s.activeFns }, fn)
}

// OnDidReceiveCustomEvent implements extension.Debug.
func (d debugAPI) OnDidReceiveCustomEvent(fn func(extension.DebugSessionCustomEvent)) func() {
	return debugListen(d.a, func(s *debugState) *[]*debugListener[func(extension.DebugSessionCustomEvent)] { return &s.customFns }, fn)
}

// OnDidChangeBreakpoints implements extension.Debug.
func (d debugAPI) OnDidChangeBreakpoints(fn func(extension.DebugBreakpointsChange)) func() {
	return debugListen(d.a, func(s *debugState) *[]*debugListener[func(extension.DebugBreakpointsChange)] { return &s.bpFns }, fn)
}

// Breakpoints implements extension.Debug.
func (d debugAPI) Breakpoints() []extension.DebugBreakpoint {
	d.a.mu.Lock()
	defer d.a.mu.Unlock()
	return slices.Clone(d.a.debug.breakpoints)
}

// AddBreakpoints implements extension.Debug: the breakpoints get an ID and the listeners hear about them.
func (d debugAPI) AddBreakpoints(_ context.Context, bps []extension.DebugBreakpoint) error {
	a := d.a
	a.mu.Lock()
	if a.scm.dead {
		a.mu.Unlock()
		return errors.New("ExtensionDeactivated", "the extension was deactivated")
	}
	var added []extension.DebugBreakpoint
	for _, b := range bps {
		if b.Function == "" && !strings.HasPrefix(b.URI, "file:") {
			a.mu.Unlock()
			return errors.New("ExtensionDebugBreakpointInvalid", "a breakpoint is a function or a line of a file of the file scheme", "uri", b.URI)
		}
		a.debug.nextBP++
		b.ID = "bp" + strconv.Itoa(a.debug.nextBP)
		a.debug.breakpoints = append(a.debug.breakpoints, b)
		added = append(added, b)
	}
	fns := slices.Clone(a.debug.bpFns)
	a.mu.Unlock()
	for _, l := range fns {
		l.fn(extension.DebugBreakpointsChange{Added: added})
	}
	return nil
}

// RemoveBreakpoints implements extension.Debug: a breakpoint is found by its ID, its function or its place.
func (d debugAPI) RemoveBreakpoints(_ context.Context, bps []extension.DebugBreakpoint) error {
	a := d.a
	a.mu.Lock()
	if a.scm.dead {
		a.mu.Unlock()
		return errors.New("ExtensionDeactivated", "the extension was deactivated")
	}
	var removed []extension.DebugBreakpoint
	for _, want := range bps {
		a.debug.breakpoints = slices.DeleteFunc(a.debug.breakpoints, func(have extension.DebugBreakpoint) bool {
			same := false
			switch {
			case want.ID != "":
				same = want.ID == have.ID
			case want.Function != "":
				same = have.Function == want.Function
			default:
				same = have.Function == "" && have.URI == want.URI && have.Position == want.Position
			}
			if same {
				removed = append(removed, extension.DebugBreakpoint{ID: have.ID})
			}
			return same
		})
	}
	fns := slices.Clone(a.debug.bpFns)
	a.mu.Unlock()
	if len(removed) > 0 {
		for _, l := range fns {
			l.fn(extension.DebugBreakpointsChange{Removed: removed})
		}
	}
	return nil
}

// Console implements extension.Debug.
func (d debugAPI) Console() extension.DebugConsole { return fakeConsole(d) }

type fakeConsole struct{ a *API }

// Append implements extension.DebugConsole.
func (c fakeConsole) Append(text string) {
	c.a.mu.Lock()
	defer c.a.mu.Unlock()
	if !c.a.scm.dead {
		c.a.debug.console.WriteString(text)
	}
}

// AppendLine implements extension.DebugConsole.
func (c fakeConsole) AppendLine(text string) { c.Append(text + "\n") }

// DebugConsoleText is what the extension wrote to the Debug Console.
func (a *API) DebugConsoleText() string {
	a.mu.Lock()
	defer a.mu.Unlock()
	return a.debug.console.String()
}

// ---- the part of Emetos

// DebugFactoryTypes are the debug types that have a descriptor factory.
func (a *API) DebugFactoryTypes() []string {
	a.mu.Lock()
	defer a.mu.Unlock()
	var out []string
	for t := range a.debug.factories {
		out = append(out, t)
	}
	slices.Sort(out)
	return out
}

// DebugAdapter asks the factory of the debug type for the adapter of the session, as the host does when a session starts; def is the
// executable the manifest names, or nil. A panic takes the extension off. A debug type with no factory gives def.
func (a *API) DebugAdapter(ctx context.Context, debugType string, s extension.DebugSession, def *extension.DebugAdapter) (adapter *extension.DebugAdapter, err error) {
	a.mu.Lock()
	f := a.debug.factories[debugType]
	a.mu.Unlock()
	if f == nil {
		return def, nil
	}
	defer func() {
		if r := recover(); r != nil {
			a.scmFail("debug adapter factory", r)
			adapter, err = nil, errors.NewErrf("ExtensionDebugPanicked", "it panicked in the adapter factory of %s: %v", debugType, r)
		}
	}()
	got, err := f(ctx, s, def)
	if err != nil {
		return nil, err
	}
	if got == nil {
		return def, nil
	}
	return got, nil
}

// providersFor lists the providers of a type with a function, in the order they were registered.
func (a *API) providersFor(typ string, has func(extension.DebugConfigurationProvider) bool, trigger *extension.DebugProviderTrigger) []*debugProvider {
	a.mu.Lock()
	defer a.mu.Unlock()
	var out []*debugProvider
	for _, p := range a.debug.providers {
		if p.typ == typ && has(p.p) && (trigger == nil || p.trigger == *trigger) {
			out = append(out, p)
		}
	}
	return out
}

// ResolveDebugConfiguration plays resolveDebugConfiguration of the host: the providers of the type, each with the result of the one
// before; a provider that gives nil ends it with nil (the launch is cancelled).
func (a *API) ResolveDebugConfiguration(ctx context.Context, debugType, folder string, cfg extension.DebugConfiguration) (extension.DebugConfiguration, error) {
	return a.callResolvers(ctx, a.providersFor(debugType, func(p extension.DebugConfigurationProvider) bool { return p.Resolve != nil }, nil),
		func(p extension.DebugConfigurationProvider) func(context.Context, string, extension.DebugConfiguration) (extension.DebugConfiguration, error) {
			return p.Resolve
		}, folder, cfg)
}

// ResolveDebugConfigurationSubstituted plays resolveDebugConfigurationWithSubstitutedVariables: the providers of the type, then those
// of "*".
func (a *API) ResolveDebugConfigurationSubstituted(ctx context.Context, debugType, folder string, cfg extension.DebugConfiguration) (extension.DebugConfiguration, error) {
	has := func(p extension.DebugConfigurationProvider) bool { return p.ResolveSubstituted != nil }
	list := a.providersFor(debugType, has, nil)
	if debugType != "*" {
		list = append(list, a.providersFor("*", has, nil)...)
	}
	return a.callResolvers(ctx, list, func(p extension.DebugConfigurationProvider) func(context.Context, string, extension.DebugConfiguration) (extension.DebugConfiguration, error) {
		return p.ResolveSubstituted
	}, folder, cfg)
}

func (a *API) callResolvers(ctx context.Context, list []*debugProvider, pick func(extension.DebugConfigurationProvider) func(context.Context, string, extension.DebugConfiguration) (extension.DebugConfiguration, error), folder string, cfg extension.DebugConfiguration) (out extension.DebugConfiguration, err error) {
	defer func() {
		if r := recover(); r != nil {
			a.scmFail("debug configuration provider", r)
			out, err = nil, errors.NewErrf("ExtensionDebugPanicked", "it panicked in a debug configuration provider: %v", r)
		}
	}()
	out = cfg
	for _, p := range list {
		if out == nil {
			break
		}
		out, err = pick(p.p)(ctx, folder, out)
		if err != nil {
			return nil, err
		}
	}
	return out, nil
}

// ProvideDebugConfigurations plays the listing of the configurations of a type for a trigger.
func (a *API) ProvideDebugConfigurations(ctx context.Context, debugType, folder string, trigger extension.DebugProviderTrigger) ([]extension.DebugConfiguration, error) {
	var out []extension.DebugConfiguration
	for _, p := range a.providersFor(debugType, func(p extension.DebugConfigurationProvider) bool { return p.Provide != nil }, &trigger) {
		list, err := p.p.Provide(ctx, folder)
		if err != nil {
			return nil, err
		}
		out = append(out, list...)
	}
	return out, nil
}

// DebugTrackers makes the trackers of a session as the host does: the factories of the type and of "*", in the order they were
// registered; a factory that fails or gives nil leaves its tracker out.
func (a *API) DebugTrackers(ctx context.Context, debugType string, s extension.DebugSession) []*extension.DebugAdapterTracker {
	a.mu.Lock()
	var list []*debugTrackerFactory
	for _, f := range a.debug.trackers {
		if f.typ == debugType || f.typ == "*" {
			list = append(list, f)
		}
	}
	a.mu.Unlock()
	var out []*extension.DebugAdapterTracker
	for _, f := range list {
		if t, err := f.f(ctx, s); err == nil && t != nil {
			out = append(out, t)
		}
	}
	return out
}

// FakeDebugSession is a debug session that a test made start; it implements extension.DebugSession.
type FakeDebugSession struct {
	id, typ, name, folder string
	cfg                   extension.DebugConfiguration
	parent                extension.DebugSession

	mu     sync.Mutex
	custom func(command string, args any) (json.RawMessage, error)
	calls  []string
}

// NewDebugSession makes a session. It is not running until StartDebugSession.
func (a *API) NewDebugSession(id, debugType, name string, cfg extension.DebugConfiguration) *FakeDebugSession {
	return &FakeDebugSession{id: id, typ: debugType, name: name, cfg: cfg}
}

// SetFolder and SetParent set the folder and the parent session of the session.
func (s *FakeDebugSession) SetFolder(folder string) { s.folder = folder }

// SetParent sets the parent session.
func (s *FakeDebugSession) SetParent(p extension.DebugSession) { s.parent = p }

// OnCustomRequest sets the function that answers CustomRequest; without one the answer is null.
func (s *FakeDebugSession) OnCustomRequest(fn func(command string, args any) (json.RawMessage, error)) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.custom = fn
}

// CustomRequests are the commands of the custom requests the extension sent to the session.
func (s *FakeDebugSession) CustomRequests() []string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return slices.Clone(s.calls)
}

// ID implements extension.DebugSession.
func (s *FakeDebugSession) ID() string { return s.id }

// Type implements extension.DebugSession.
func (s *FakeDebugSession) Type() string { return s.typ }

// Name implements extension.DebugSession.
func (s *FakeDebugSession) Name() string { return s.name }

// Folder implements extension.DebugSession.
func (s *FakeDebugSession) Folder() string { return s.folder }

// Configuration implements extension.DebugSession.
func (s *FakeDebugSession) Configuration() extension.DebugConfiguration { return s.cfg }

// Parent implements extension.DebugSession.
func (s *FakeDebugSession) Parent() extension.DebugSession { return s.parent }

// CustomRequest implements extension.DebugSession.
func (s *FakeDebugSession) CustomRequest(_ context.Context, command string, args any) (json.RawMessage, error) {
	s.mu.Lock()
	fn := s.custom
	s.calls = append(s.calls, command)
	s.mu.Unlock()
	if fn == nil {
		return json.RawMessage("null"), nil
	}
	return fn(command, args)
}

// StartDebugSession makes the session run, and the listeners of OnDidStartSession hear about it.
func (a *API) StartDebugSession(s *FakeDebugSession) {
	a.mu.Lock()
	a.debug.sessions = append(a.debug.sessions, s)
	fns := slices.Clone(a.debug.startFns)
	a.mu.Unlock()
	for _, l := range fns {
		l.fn(s)
	}
}

// EndDebugSession ends a session, and the listeners of OnDidTerminateSession hear about it.
func (a *API) EndDebugSession(s *FakeDebugSession) {
	a.mu.Lock()
	a.debug.sessions = slices.DeleteFunc(a.debug.sessions, func(x *FakeDebugSession) bool { return x == s })
	wasActive := a.debug.active == s
	if wasActive {
		a.debug.active = nil
	}
	fns := slices.Clone(a.debug.endFns)
	active := slices.Clone(a.debug.activeFns)
	a.mu.Unlock()
	for _, l := range fns {
		l.fn(s)
	}
	if wasActive {
		for _, l := range active {
			l.fn(nil)
		}
	}
}

// SetActiveDebugSession focuses a session (nil: none), and the listeners of OnDidChangeActiveSession hear about it.
func (a *API) SetActiveDebugSession(s *FakeDebugSession) {
	a.mu.Lock()
	a.debug.active = s
	fns := slices.Clone(a.debug.activeFns)
	a.mu.Unlock()
	for _, l := range fns {
		if s == nil {
			l.fn(nil)
		} else {
			l.fn(s)
		}
	}
}

// DebugCustomEvent makes the adapter of a session send an event the protocol does not define.
func (a *API) DebugCustomEvent(s *FakeDebugSession, event string, body json.RawMessage) {
	a.mu.Lock()
	fns := slices.Clone(a.debug.customFns)
	a.mu.Unlock()
	for _, l := range fns {
		l.fn(extension.DebugSessionCustomEvent{Session: s, Event: event, Body: body})
	}
}
