package exthost

import (
	"context"
	"sync"

	"github.com/rm4n0s/errors"

	"github.com/rm4n0s/emetos/extension"
	"github.com/rm4n0s/emetos/internal/core/debug"
	"github.com/rm4n0s/emetos/internal/core/document"
)

// The binding of extension.Debug to the debug host of a scope (SPEC §10.31 "The extension contract", step 25.3; after
// extHostDebugService.ts).

// debugAPI is the extension.Debug of one activation.
type debugAPI struct{ inst *instance }

var _ extension.Debug = debugAPI{}

// Debug implements extension.API.
func (a *api) Debug() extension.Debug { return debugAPI{a.inst} }

// host is the debug host of the scope, or the error a registration or a start fails with when there is none. It fails with
// ExtensionDeactivated after the activation ended and ExtensionDebugUnavailable in a scope with no debug service.
func (d debugAPI) host() (*DebugHost, error) {
	in := d.inst
	if in.isDead() {
		return nil, errors.New("ExtensionDeactivated", "the extension was deactivated", "extension", in.id)
	}
	h := in.scope.opts.Debug
	if h == nil {
		return nil, errors.New("ExtensionDebugUnavailable", "the scope has no debug service", "extension", in.id)
	}
	return h, nil
}

// register checks what the three registrations need alike: the extension is active, the scope has the service, and the debug type and
// the function are not empty. It fails with ExtensionDeactivated, ExtensionDebugUnavailable and ExtensionDebugInvalid.
func (d debugAPI) register(debugType string, noFunction bool) (*DebugHost, error) {
	h, err := d.host()
	if err != nil {
		return nil, err
	}
	if debugType == "" || noFunction {
		return nil, errors.New("ExtensionDebugInvalid", "a debug registration needs a debug type and a function", "extension", d.inst.id, "type", debugType)
	}
	return h, nil
}

// removal makes the function that removes a registration once, and has the activation's end do it.
func (d debugAPI) removal(remove func()) func() {
	var once sync.Once
	dispose := func() { once.Do(remove) }
	d.inst.track(dispose)
	return dispose
}

// RegisterAdapterDescriptorFactory implements extension.Debug. It fails with ExtensionDebugFactoryTaken when the debug type has a
// factory.
func (d debugAPI) RegisterAdapterDescriptorFactory(debugType string, factory extension.DebugAdapterFactory) (func(), error) {
	h, err := d.register(debugType, factory == nil)
	if err != nil {
		return nil, err
	}
	e := &factoryEntry{inst: d.inst, typ: debugType, f: factory}
	h.mu.Lock()
	for _, f := range h.factories {
		if f.typ == debugType {
			h.mu.Unlock()
			return nil, errors.New("ExtensionDebugFactoryTaken", "the debug type has an adapter factory already", "extension", d.inst.id, "type", debugType, "other", f.inst.id)
		}
	}
	h.factories = append(h.factories, e)
	h.mu.Unlock()
	return d.removal(func() {
		h.mu.Lock()
		h.factories = deleteEntry(h.factories, e)
		h.mu.Unlock()
	}), nil
}

// RegisterConfigurationProvider implements extension.Debug.
func (d debugAPI) RegisterConfigurationProvider(debugType string, p extension.DebugConfigurationProvider, trigger extension.DebugProviderTrigger) (func(), error) {
	h, err := d.register(debugType, p.Provide == nil && p.Resolve == nil && p.ResolveSubstituted == nil)
	if err != nil {
		return nil, err
	}
	h.mu.Lock()
	h.seq++
	e := &providerEntry{seq: h.seq, inst: d.inst, typ: debugType, p: p, trigger: trigger}
	h.providers = append(h.providers, e)
	h.mu.Unlock()
	h.launchChecksChanged()
	return d.removal(func() {
		h.mu.Lock()
		h.providers = deleteEntry(h.providers, e)
		h.mu.Unlock()
	}), nil
}

// RegisterAdapterTrackerFactory implements extension.Debug.
func (d debugAPI) RegisterAdapterTrackerFactory(debugType string, f extension.DebugAdapterTrackerFactory) (func(), error) {
	h, err := d.register(debugType, f == nil)
	if err != nil {
		return nil, err
	}
	h.mu.Lock()
	h.seq++
	e := &trackerFactoryEntry{seq: h.seq, inst: d.inst, typ: debugType, f: f}
	h.trackers = append(h.trackers, e)
	h.mu.Unlock()
	return d.removal(func() {
		h.mu.Lock()
		h.trackers = deleteEntry(h.trackers, e)
		h.mu.Unlock()
	}), nil
}

func deleteEntry[T comparable](list []T, e T) []T {
	out := list[:0:0]
	for _, x := range list {
		if x != e {
			out = append(out, x)
		}
	}
	return out
}

// StartDebugging implements extension.Debug. A configuration given whole needs no launch.json; a name is looked up in the launch.json of
// the folder, or, with no folder, in the first launch that has it. It fails with ExtensionDebugInvalid for a target with neither a
// name nor a configuration, and with ExtensionDebugSessionForeign for a parent session that is not one of the tab's.
func (d debugAPI) StartDebugging(ctx context.Context, folder string, target extension.DebugTarget, opts extension.DebugStartOptions) (bool, error) {
	h, err := d.host()
	if err != nil {
		return false, err
	}
	if target.Name == "" && target.Config == nil {
		return false, errors.New("ExtensionDebugInvalid", "StartDebugging needs the name of a configuration or a configuration", "extension", d.inst.id)
	}
	so := debug.SessionOptions{
		LifecycleManagedByParent: opts.LifecycleManagedByParent, Compact: opts.Compact, NoSaveBeforeStart: opts.SuppressSaveBeforeStart,
		SuppressDebugToolbar: opts.SuppressDebugToolbar, SuppressDebugStatusbar: opts.SuppressDebugStatusbar, SuppressDebugView: opts.SuppressDebugView,
	}
	if opts.NoDebug {
		yes := true
		so.NoDebug = &yes
	}
	if opts.ConsoleMode == extension.DebugConsoleMergeWithParent {
		so.Repl = "mergeWithParent"
	} else {
		so.Repl = "separate"
	}
	if opts.Parent != nil {
		parent, err := h.coreSession(opts.Parent)
		if err != nil {
			return false, err
		}
		so.Parent = parent
	}
	var launch *debug.Launch
	if folder != "" {
		launch = h.svc.Launches().Launch(folder)
	}
	if launch == nil && target.Config == nil && folder == "" {
		for _, l := range h.svc.Launches().Launches() {
			if _, ok := l.Configuration(target.Name); ok {
				launch = l
				break
			}
			if _, ok := l.Compound(target.Name); ok {
				launch = l
				break
			}
		}
	}
	t := debug.Target{Name: target.Name}
	if target.Config != nil {
		t.Config = debug.Config(target.Config).Clone()
	}
	return h.svc.StartDebugging(ctx, launch, t, so)
}

// StopDebugging implements extension.Debug. It fails with ExtensionDebugSessionForeign for a session that is not one of the tab's.
func (d debugAPI) StopDebugging(ctx context.Context, session extension.DebugSession) error {
	h, err := d.host()
	if err != nil {
		return err
	}
	if session == nil {
		return h.svc.Stop(ctx, nil, false, false)
	}
	s, err := h.coreSession(session)
	if err != nil {
		return err
	}
	return h.svc.Stop(ctx, s, s.IsAttach(), false)
}

// ActiveSession implements extension.Debug.
func (d debugAPI) ActiveSession() extension.DebugSession {
	h, err := d.host()
	if err != nil {
		return nil
	}
	return h.wrapOrNil(h.svc.Model().Focus().Session)
}

// Sessions implements extension.Debug.
func (d debugAPI) Sessions() []extension.DebugSession {
	h, err := d.host()
	if err != nil {
		return nil
	}
	var out []extension.DebugSession
	for _, s := range h.svc.Model().Sessions(false) {
		out = append(out, h.wrap(s))
	}
	return out
}

// listen adds a listener to one of the hubs of the host, removed with the activation. In a scope with no service, or after the
// activation ended, the listener is never called and the remove function does nothing.
func listen[F any](d debugAPI, pick func(*DebugHost) *hub[F], fn F) func() {
	h, err := d.host()
	if err != nil {
		return func() {}
	}
	return d.removal(pick(h).add(d.inst, fn))
}

// OnDidStartSession implements extension.Debug.
func (d debugAPI) OnDidStartSession(fn func(extension.DebugSession)) func() {
	return listen(d, func(h *DebugHost) *hub[func(extension.DebugSession)] { return &h.startFns }, fn)
}

// OnDidTerminateSession implements extension.Debug.
func (d debugAPI) OnDidTerminateSession(fn func(extension.DebugSession)) func() {
	return listen(d, func(h *DebugHost) *hub[func(extension.DebugSession)] { return &h.endFns }, fn)
}

// OnDidChangeActiveSession implements extension.Debug.
func (d debugAPI) OnDidChangeActiveSession(fn func(extension.DebugSession)) func() {
	return listen(d, func(h *DebugHost) *hub[func(extension.DebugSession)] { return &h.activeFns }, fn)
}

// OnDidReceiveCustomEvent implements extension.Debug.
func (d debugAPI) OnDidReceiveCustomEvent(fn func(extension.DebugSessionCustomEvent)) func() {
	return listen(d, func(h *DebugHost) *hub[func(extension.DebugSessionCustomEvent)] { return &h.customFns }, fn)
}

// OnDidChangeBreakpoints implements extension.Debug.
func (d debugAPI) OnDidChangeBreakpoints(fn func(extension.DebugBreakpointsChange)) func() {
	return listen(d, func(h *DebugHost) *hub[func(extension.DebugBreakpointsChange)] { return &h.bpFns }, fn)
}

// Breakpoints implements extension.Debug.
func (d debugAPI) Breakpoints() []extension.DebugBreakpoint {
	h, err := d.host()
	if err != nil {
		return nil
	}
	return h.breakpoints()
}

// AddBreakpoints implements extension.Debug. A breakpoint that is neither a function nor in a file (a URI of the file scheme) fails with
// ExtensionDebugBreakpointInvalid.
func (d debugAPI) AddBreakpoints(ctx context.Context, bps []extension.DebugBreakpoint) error {
	h, err := d.host()
	if err != nil {
		return err
	}
	byPath := map[string][]debug.BreakpointData{}
	var paths []string
	var functions []debug.FunctionBreakpointOptions
	for _, b := range bps {
		if b.Function != "" {
			functions = append(functions, debug.FunctionBreakpointOptions{Name: b.Function, Disabled: b.Disabled, Condition: b.Condition, HitCondition: b.HitCondition})
			continue
		}
		u, perr := document.ParseURI(b.URI)
		if perr != nil || u.Scheme != document.SchemeFile {
			return errors.New("ExtensionDebugBreakpointInvalid", "a breakpoint is a function or a line of a file of the file scheme", "extension", d.inst.id, "uri", b.URI)
		}
		path := u.Path
		data := debug.BreakpointData{Line: b.Position.Line + 1, Disabled: b.Disabled, Condition: b.Condition, HitCondition: b.HitCondition, LogMessage: b.LogMessage}
		if b.Position.Col > 0 {
			data.Column = b.Position.Col + 1
		}
		if _, seen := byPath[path]; !seen {
			paths = append(paths, path)
		}
		byPath[path] = append(byPath[path], data)
	}
	for _, path := range paths {
		if _, err := h.svc.AddBreakpoints(ctx, path, byPath[path]); err != nil {
			return err
		}
	}
	for _, f := range functions {
		if _, err := h.svc.AddFunctionBreakpoint(ctx, f); err != nil {
			return err
		}
	}
	return nil
}

// RemoveBreakpoints implements extension.Debug. A breakpoint is found by its ID, or, without one, by its place or its function.
func (d debugAPI) RemoveBreakpoints(ctx context.Context, bps []extension.DebugBreakpoint) error {
	h, err := d.host()
	if err != nil {
		return err
	}
	var lineIDs, funcIDs []string
	for _, want := range bps {
		for _, have := range h.svc.Model().Breakpoints(debug.BreakpointFilter{}) {
			if sameBreakpoint(want, fromLineBreakpoint(have)) {
				lineIDs = append(lineIDs, have.ID)
			}
		}
		for _, have := range h.svc.Model().FunctionBreakpoints() {
			if sameBreakpoint(want, fromFunctionBreakpoint(have)) {
				funcIDs = append(funcIDs, have.ID)
			}
		}
	}
	if len(lineIDs) > 0 {
		if err := h.svc.RemoveBreakpoints(ctx, lineIDs...); err != nil {
			return err
		}
	}
	for _, id := range funcIDs {
		if err := h.svc.RemoveFunctionBreakpoints(ctx, id); err != nil {
			return err
		}
	}
	return nil
}

// sameBreakpoint tells whether want names have: by ID, else by function name, else by file and position.
func sameBreakpoint(want, have extension.DebugBreakpoint) bool {
	switch {
	case want.ID != "":
		return want.ID == have.ID
	case want.Function != "":
		return have.Function == want.Function
	}
	return have.Function == "" && have.URI == want.URI && have.Position == want.Position
}

// Console implements extension.Debug.
func (d debugAPI) Console() extension.DebugConsole { return debugConsole{d} }

// debugConsole is the Debug Console of an activation.
type debugConsole struct{ d debugAPI }

// Append implements extension.DebugConsole. In a scope with no service, or after the activation ended, it does nothing.
func (c debugConsole) Append(text string) {
	if h, err := c.d.host(); err == nil {
		h.svc.AppendConsole(text)
	}
}

// AppendLine implements extension.DebugConsole.
func (c debugConsole) AppendLine(text string) { c.Append(text + "\n") }
