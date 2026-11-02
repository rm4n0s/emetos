package exthost

import (
	"context"
	"encoding/json"
	"log/slog"
	"net"
	"regexp"
	"slices"
	"sort"
	"strconv"
	"sync"
	"sync/atomic"

	"github.com/rm4n0s/errors"

	"github.com/rm4n0s/emetos/extension"
	"github.com/rm4n0s/emetos/internal/core/dap"
	"github.com/rm4n0s/emetos/internal/core/debug"
	"github.com/rm4n0s/emetos/internal/core/document"
)

// The debug host (SPEC §10.31 "The extension contract", step 25.3; after extHostDebugService.ts and mainThreadDebugService.ts). The
// window makes one DebugHost for each window tab, puts it in ScopeOptions.Debug, and gets the debug service from it. The host fills the
// part of the service's options that belongs to the extensions: which debuggers there are (from the manifests), how their adapters are
// reached (the descriptor factories), the configuration providers in VS Code's order, the trackers that see the adapters' messages. The
// rest of the options (the prompts, the tasks, the terminal, the active editor) are the window's, in DebugHostOptions.Base.
//
// The extensions of a debugger are activated by the event extension.OnDebug(type) when a configuration of the type is resolved or
// started, its initial or dynamic configurations are listed, or a tracker is wanted; the host asks the scope on the UI thread and waits,
// so a session may be started from any goroutine.

// DebugHostOptions are what the window gives a DebugHost.
type DebugHostOptions struct {
	// Base are the options of the debug service that are the window's: Variables, RunTask, SaveBeforeStart, ConfirmConcurrent,
	// RunInTerminal, OpenLaunchFile, InputMemory, Guess, the delays and the logger. The members the host fills (Debugger,
	// ResolveConfiguration, ResolveSubstituted, InitialConfigurations, DynamicConfigurations, DynamicTypes) are replaced, and its Trace
	// is called before the trackers.
	Base debug.Options
	// Logger receives the problems that do not stop a session; nil is the logger of the host.
	Logger *slog.Logger
}

// DebugHost is what the extensions of one window tab contribute to debugging, and the debug service made over it.
type DebugHost struct {
	log *slog.Logger
	svc *debug.Service

	mu        sync.Mutex
	scope     *Scope
	types     map[string]*debugType
	bpLangs   map[string]int
	factories []*factoryEntry
	providers []*providerEntry
	trackers  []*trackerFactoryEntry
	sessions  map[*debug.Session]*debugSession
	live      map[string]*trackerSet
	active    string
	seq       int
	unsub     func()

	startFns, endFns hub[func(extension.DebugSession)]
	activeFns        hub[func(extension.DebugSession)]
	customFns        hub[func(extension.DebugSessionCustomEvent)]
	bpFns            hub[func(extension.DebugBreakpointsChange)]

	// launch.json files the window told the host about (launch.go), by path.
	launchTexts map[string]string

	// servers are the addresses (host:port) of the server programs that serve the children of their session (DebugAdapter.ServesChildren),
	// by session. A child gets its parent's; the entry goes when the session ends.
	servers map[*debug.Session]string
}

// debugType is a debug type an enabled extension declares.
type debugType struct {
	spec   extension.DebuggerSpec
	extID  string
	schema debug.DebuggerSchema
}

type factoryEntry struct {
	inst *instance
	typ  string
	f    extension.DebugAdapterFactory
}

type providerEntry struct {
	seq     int
	inst    *instance
	typ     string
	p       extension.DebugConfigurationProvider
	trigger extension.DebugProviderTrigger
}

type trackerFactoryEntry struct {
	seq  int
	inst *instance
	typ  string
	f    extension.DebugAdapterTrackerFactory
}

// NewDebugHost makes the host and the debug service over it. ctx is the life of the window tab: the adapters' programs end with it.
func NewDebugHost(ctx context.Context, opts DebugHostOptions) *DebugHost {
	h := &DebugHost{
		log: opts.Logger, types: map[string]*debugType{}, bpLangs: map[string]int{}, sessions: map[*debug.Session]*debugSession{},
		live: map[string]*trackerSet{}, launchTexts: map[string]string{}, servers: map[*debug.Session]string{},
	}
	if h.log == nil {
		h.log = slog.Default()
	}
	o := opts.Base
	if o.Logger == nil {
		o.Logger = h.log
	}
	o.Debugger = h.debugger
	o.ResolveConfiguration = h.resolveConfiguration
	o.ResolveSubstituted = h.resolveSubstituted
	o.InitialConfigurations = h.initialConfigurations
	o.DynamicConfigurations = h.dynamicConfigurations
	o.DynamicTypes = h.dynamicTypes
	baseTrace := o.Trace
	o.Trace = func(s *debug.Session, m dap.TraceMessage) {
		if baseTrace != nil {
			baseTrace(s, m)
		}
		h.trace(s, m)
	}
	h.svc = debug.NewService(ctx, o)
	h.unsub = h.svc.Subscribe(h.onEvent)
	return h
}

// Service is the debug service the host fills the options of.
func (h *DebugHost) Service() *debug.Service { return h.svc }

// Close stops following the service. The sessions are the service's to stop (the window cancels its context).
func (h *DebugHost) Close() { h.unsub() }

// bind makes the scope the one the host asks to activate extensions. A host serves one scope.
func (h *DebugHost) bind(s *Scope) {
	h.mu.Lock()
	if h.scope != nil {
		h.mu.Unlock()
		h.log.Warn("a debug host serves one scope: the second is left without debugging", "scope", s.opts.Info.Name)
		return
	}
	h.scope = s
	h.mu.Unlock()
	h.registerLaunchCompletion(s)
}

func (h *DebugHost) scopeOf() *Scope {
	h.mu.Lock()
	defer h.mu.Unlock()
	return h.scope
}

// DebuggerInfo describes a debug type that an enabled extension declares.
type DebuggerInfo struct {
	// Type, Label and Languages are those of the declaration; Extension is the id of the extension that declares it.
	Type, Label string
	Languages   []string
	Extension   string
	// Dynamic says that the type has dynamic configurations (declared, or a provider is registered).
	Dynamic bool
}

// Debuggers lists the debug types of the enabled extensions, by type.
func (h *DebugHost) Debuggers() []DebuggerInfo {
	h.mu.Lock()
	defer h.mu.Unlock()
	out := make([]DebuggerInfo, 0, len(h.types))
	for _, t := range h.types {
		out = append(out, DebuggerInfo{Type: t.spec.Type, Label: labelOf(t.spec), Languages: slices.Clone(t.spec.Languages), Extension: t.extID, Dynamic: h.dynamicLocked(t)})
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Type < out[j].Type })
	return out
}

// DebuggersForLanguage lists the debug types that serve the language (F5 with no launch.json asks them).
func (h *DebugHost) DebuggersForLanguage(languageID string) []DebuggerInfo {
	var out []DebuggerInfo
	for _, d := range h.Debuggers() {
		if slices.Contains(d.Languages, languageID) {
			out = append(out, d)
		}
	}
	return out
}

// BreakpointLanguages lists the languages where the enabled extensions allow breakpoints (Contributes.Breakpoints), sorted.
func (h *DebugHost) BreakpointLanguages() []string {
	h.mu.Lock()
	defer h.mu.Unlock()
	out := make([]string, 0, len(h.bpLangs))
	for l := range h.bpLangs {
		out = append(out, l)
	}
	sort.Strings(out)
	return out
}

// AllowsBreakpoints tells whether breakpoints may be set in a document of the language (debug.allowBreakpointsEverywhere lifts the
// limit; the window passes it).
func (h *DebugHost) AllowsBreakpoints(languageID string, everywhere bool) bool {
	if everywhere {
		return true
	}
	h.mu.Lock()
	defer h.mu.Unlock()
	return h.bpLangs[languageID] > 0
}

func labelOf(spec extension.DebuggerSpec) string {
	if spec.Label != "" {
		return spec.Label
	}
	return spec.Type
}

// ---- the manifest

// addTypes records the debuggers and breakpoint languages the manifest of an installed extension declares.
func (h *DebugHost) addTypes(m extension.Manifest) {
	h.mu.Lock()
	for _, spec := range m.Contributes.Debuggers {
		if prev, taken := h.types[spec.Type]; taken {
			h.log.Warn("a debug type is declared by two extensions: the first keeps it", "type", spec.Type, "extension", m.ID, "other", prev.extID)
			continue
		}
		t := &debugType{spec: spec, extID: m.ID}
		t.schema = schemaOf(spec, h.log)
		h.types[spec.Type] = t
	}
	for _, b := range m.Contributes.Breakpoints {
		h.bpLangs[b.Language]++
	}
	h.mu.Unlock()
	h.launchChecksChanged()
}

// removeTypes takes the declarations of an extension away and ends the sessions of its debug types: their adapters came from it.
func (h *DebugHost) removeTypes(m extension.Manifest) {
	var gone []string
	h.mu.Lock()
	for _, spec := range m.Contributes.Debuggers {
		if t := h.types[spec.Type]; t != nil && t.extID == m.ID {
			delete(h.types, spec.Type)
			gone = append(gone, spec.Type)
		}
	}
	for _, b := range m.Contributes.Breakpoints {
		if h.bpLangs[b.Language]--; h.bpLangs[b.Language] <= 0 {
			delete(h.bpLangs, b.Language)
		}
	}
	h.mu.Unlock()
	h.launchChecksChanged()
	if len(gone) > 0 {
		go h.stopSessionsOf(gone)
	}
}

// stopSessionsOf stops the sessions whose debug type is one of those; it waits for each to end.
func (h *DebugHost) stopSessionsOf(types []string) {
	ctx, cancel := context.WithTimeout(context.Background(), debug.DisconnectTimeout*3)
	defer cancel()
	for _, s := range h.svc.Model().Sessions(false) {
		if !slices.Contains(types, s.Type()) {
			continue
		}
		if err := h.svc.Stop(ctx, s, s.IsAttach(), false); err != nil {
			h.log.Warn("stopping a session of a debugger that went away failed", "session", s.ID(), "type", s.Type(), "err", err)
		}
	}
}

func (h *DebugHost) dynamicLocked(t *debugType) bool {
	if t.spec.DynamicConfigurations {
		return true
	}
	for _, p := range h.providers {
		if p.typ == t.spec.Type && p.trigger == extension.DebugTriggerDynamic && p.p.Provide != nil {
			return true
		}
	}
	return false
}

// ---- running calls into extensions

// guard calls fn, which belongs to the extension of inst. A panic fails the extension (ExtensionDebugPanicked) and is the error of
// the call; so is a call of an extension that was deactivated (ExtensionDeactivated).
func (h *DebugHost) guard(inst *instance, where string, fn func() error) (err error) {
	defer func() {
		if r := recover(); r != nil {
			failure := errors.NewErrf("ExtensionDebugPanicked", "it panicked in %s: %v", where, r).SetMetadata("extension", inst.id, "scope", inst.scope.opts.Info.Name)
			inst.scope.post(func() { inst.scope.fail(inst.slot, failure, where) })
			err = failure
		}
	}()
	if inst.isDead() {
		return errors.New("ExtensionDeactivated", "the extension was deactivated", "extension", inst.id)
	}
	return fn()
}

// safe is guard for a call that has no result: a failure is logged.
func (h *DebugHost) safe(inst *instance, where string, fn func()) {
	if err := h.guard(inst, where, func() error { fn(); return nil }); err != nil {
		h.log.Debug("a call into an extension did not happen", "where", where, "err", err)
	}
}

// onUI runs f on the UI thread and waits for it; it gives up when ctx ends.
func (h *DebugHost) onUI(ctx context.Context, f func()) {
	sc := h.scopeOf()
	if sc == nil {
		return
	}
	if sc.onUI() {
		f()
		return
	}
	done := make(chan struct{})
	sc.post(func() {
		defer close(done)
		f()
	})
	select {
	case <-done:
	case <-ctx.Done():
	}
}

// activate activates the extension that declares the debug type, if it is not active.
func (h *DebugHost) activate(ctx context.Context, typ string) {
	if typ == "" || typ == "*" {
		return
	}
	h.mu.Lock()
	_, declared := h.types[typ]
	h.mu.Unlock()
	if !declared {
		return
	}
	h.onUI(ctx, func() {
		if sc := h.scopeOf(); sc != nil {
			sc.Fire(extension.OnDebug(typ))
		}
	})
}

// ---- Options.Debugger and the adapters

// debugger is debug.Options.Debugger: the debug type of an enabled extension.
func (h *DebugHost) debugger(typ string) (debug.Debugger, bool) {
	h.mu.Lock()
	t := h.types[typ]
	h.mu.Unlock()
	if t == nil {
		return debug.Debugger{}, false
	}
	vars := make(map[string]string, len(t.spec.Variables))
	for k, v := range t.spec.Variables {
		vars[k] = v
	}
	return debug.Debugger{
		Type: typ, Label: labelOf(t.spec), Languages: slices.Clone(t.spec.Languages), Variables: vars,
		Connect: func(ctx context.Context, cfg debug.Config, opts dap.ClientOptions) (*dap.Client, error) {
			return h.connect(ctx, typ, cfg, opts)
		},
	}, true
}

// connect reaches the adapter of a session (reach); the extension's view of a session that could not be reached is forgotten, for the
// service ends no session that never started.
func (h *DebugHost) connect(ctx context.Context, typ string, cfg debug.Config, opts dap.ClientOptions) (*dap.Client, error) {
	client, err := h.reach(ctx, typ, cfg, opts)
	if err != nil {
		if sess, ok := debug.SessionFromContext(ctx); ok {
			h.mu.Lock()
			delete(h.sessions, sess)
			h.mu.Unlock()
		}
	}
	return client, err
}

// reach: the extension that has the debug type is activated, the trackers are made, the descriptor comes from the factory, and the
// client is started over it. It fails with DebugConnectNoSession when the context carries no session.
func (h *DebugHost) reach(ctx context.Context, typ string, cfg debug.Config, opts dap.ClientOptions) (*dap.Client, error) {
	sess, ok := debug.SessionFromContext(ctx)
	if !ok {
		return nil, errors.New("DebugConnectNoSession", "the adapter of a debug type is reached for a session", "type", typ, "name", cfg.Name())
	}
	h.activate(ctx, typ)
	es := h.wrap(sess)
	set := h.startTrackers(ctx, es, typ)
	var adapter extension.DebugAdapter
	if host, port, ok := h.parentServer(sess); ok {
		// the server of the parent serves this session too (DebugAdapter.ServesChildren); the factory is not asked
		adapter = extension.DebugAdapter{Kind: extension.DebugAdapterServer, Host: host, Port: port, ServesChildren: true}
	} else {
		var err error
		if adapter, err = h.descriptor(ctx, es, typ); err != nil {
			set.failed(err)
			return nil, err
		}
	}
	spec, err := h.adapterSpec(typ, adapter, func(category, text string) { sess.AddOutput(category, text) })
	if err != nil {
		set.failed(err)
		return nil, err
	}
	client, err := dap.Connect(ctx, spec, opts)
	if err != nil {
		set.failed(err)
		return nil, err
	}
	if adapter.ServesChildren {
		h.keepServer(sess, adapter, client)
	}
	return client, nil
}

// parentServer is the address of the server of the session's parent when that server serves the children of its session.
func (h *DebugHost) parentServer(sess *debug.Session) (host string, port int, ok bool) {
	parent := sess.Parent()
	if parent == nil {
		return "", 0, false
	}
	h.mu.Lock()
	addr := h.servers[parent]
	h.mu.Unlock()
	host, p, err := net.SplitHostPort(addr)
	if err != nil {
		return "", 0, false
	}
	port, err = strconv.Atoi(p)
	return host, port, err == nil
}

// keepServer remembers the address a session's server was reached at, so that its children connect to it: the server program the host
// started, or the server a child was given.
func (h *DebugHost) keepServer(sess *debug.Session, adapter extension.DebugAdapter, client *dap.Client) {
	addr := ""
	switch sp := client.Transport().(type) {
	case *dap.ServerProcess:
		addr = sp.Addr()
	default:
		if adapter.Kind == extension.DebugAdapterServer {
			addr = net.JoinHostPort(adapter.Host, strconv.Itoa(adapter.Port))
		}
	}
	if addr == "" {
		return
	}
	h.mu.Lock()
	h.servers[sess] = addr
	h.mu.Unlock()
}

// descriptor asks the factory of the debug type for the adapter of the session; without an answer it is the adapter the manifest names.
func (h *DebugHost) descriptor(ctx context.Context, es extension.DebugSession, typ string) (extension.DebugAdapter, error) {
	h.mu.Lock()
	t := h.types[typ]
	var factory *factoryEntry
	for _, f := range h.factories {
		if f.typ == typ {
			factory = f
		}
	}
	h.mu.Unlock()
	var def *extension.DebugAdapter
	if t != nil && t.spec.Program != "" {
		def = defaultAdapter(t.spec, es.Folder())
	}
	if factory != nil {
		var got *extension.DebugAdapter
		err := h.guard(factory.inst, "the adapter factory of "+typ, func() error {
			var e error
			got, e = factory.f(ctx, es, cloneAdapter(def))
			return e
		})
		if err != nil {
			return extension.DebugAdapter{}, err
		}
		if got != nil {
			return *got, nil
		}
	}
	if def != nil {
		return *def, nil
	}
	return extension.DebugAdapter{}, errors.New("DebugAdapterMissing", "The debugger '"+typ+"' has no debug adapter: its extension registered no factory and names no program.", "type", typ)
}

func defaultAdapter(spec extension.DebuggerSpec, folder string) *extension.DebugAdapter {
	a := &extension.DebugAdapter{Kind: extension.DebugAdapterExecutable, Cwd: folder}
	if spec.Runtime != "" {
		a.Command = spec.Runtime
		a.Args = append(append(append([]string{}, spec.RuntimeArgs...), spec.Program), spec.Args...)
	} else {
		a.Command = spec.Program
		a.Args = append([]string{}, spec.Args...)
	}
	return a
}

func cloneAdapter(a *extension.DebugAdapter) *extension.DebugAdapter {
	if a == nil {
		return nil
	}
	c := *a
	c.Args = slices.Clone(a.Args)
	if a.Env != nil {
		c.Env = make(map[string]string, len(a.Env))
		for k, v := range a.Env {
			c.Env[k] = v
		}
	}
	return &c
}

// adapterSpec turns the descriptor of an extension into the core's. It fails with ExtensionDebugAdapterInvalid for a kind that is none
// of the four and for a descriptor that lacks what its kind needs.
func (h *DebugHost) adapterSpec(typ string, a extension.DebugAdapter, console func(category, text string)) (dap.Adapter, error) {
	process := func() dap.ProcessSpec {
		env := make([]string, 0, len(a.Env))
		for k, v := range a.Env {
			env = append(env, k+"="+v)
		}
		sort.Strings(env)
		spec := dap.ProcessSpec{
			Program: a.Command, Args: slices.Clone(a.Args), Dir: a.Cwd, Env: env,
			Stderr: func(line string) {
				h.log.Debug("debug adapter", "type", typ, "stderr", line)
				if a.ConsoleOutput && console != nil {
					console("stderr", line+"\n")
				}
			},
		}
		// the standard output of a program that is only a server is free: the program it runs writes there. The line with the port is
		// not output of the program.
		if a.ConsoleOutput && console != nil && a.Kind == extension.DebugAdapterExecutableServer {
			pattern := a.PortPattern
			if pattern == "" {
				pattern = dap.DefaultPortPattern
			}
			portLine, perr := regexp.Compile(pattern)
			var skipped atomic.Bool
			spec.Stdout = func(line string) {
				h.log.Debug("debug adapter", "type", typ, "stdout", line)
				if perr == nil && portLine.MatchString(line) && skipped.CompareAndSwap(false, true) {
					return
				}
				console("stdout", line+"\n")
			}
		}
		return spec
	}
	switch {
	case (a.Kind == extension.DebugAdapterExecutable || a.Kind == extension.DebugAdapterExecutableServer) && a.Command == "":
		return dap.Adapter{}, errors.New("ExtensionDebugAdapterInvalid", "the adapter of a debugger that is a program needs its command", "type", typ, "kind", int(a.Kind))
	case a.Kind == extension.DebugAdapterServer && (a.Port <= 0 || a.Port > 65535):
		return dap.Adapter{}, errors.New("ExtensionDebugAdapterInvalid", "the adapter of a server needs its port", "type", typ, "port", a.Port)
	case a.Kind == extension.DebugAdapterPipeServer && a.Path == "":
		return dap.Adapter{}, errors.New("ExtensionDebugAdapterInvalid", "the adapter of a pipe needs the path of its socket", "type", typ)
	}
	switch a.Kind {
	case extension.DebugAdapterExecutable:
		return dap.Adapter{Kind: dap.AdapterExecutable, Process: process()}, nil
	case extension.DebugAdapterExecutableServer:
		return dap.Adapter{Kind: dap.AdapterExecutableServer, Process: process(), Server: dap.ServerSpec{PortPattern: a.PortPattern}}, nil
	case extension.DebugAdapterServer:
		return dap.Adapter{Kind: dap.AdapterServer, Host: a.Host, Port: a.Port}, nil
	case extension.DebugAdapterPipeServer:
		return dap.Adapter{Kind: dap.AdapterPipe, Path: a.Path}, nil
	}
	return dap.Adapter{}, errors.New("ExtensionDebugAdapterInvalid", "the adapter has no known kind", "type", typ, "kind", int(a.Kind))
}

// ---- configuration providers

// providersFor lists the providers of a type that have the function, in the order they were registered.
func (h *DebugHost) providersFor(typ string, trigger *extension.DebugProviderTrigger, has func(extension.DebugConfigurationProvider) bool) []*providerEntry {
	h.mu.Lock()
	defer h.mu.Unlock()
	var out []*providerEntry
	for _, p := range h.providers {
		if p.typ == typ && has(p.p) && (trigger == nil || p.trigger == *trigger) {
			out = append(out, p)
		}
	}
	return out
}

// resolveConfiguration is debug.Options.ResolveConfiguration: resolveDebugConfiguration of the providers of the type, each with the
// result of the one before. A provider that gives nil cancels the launch.
//
// Ported from VS Code: src/vs/workbench/contrib/debug/browser/debugConfigurationManager.ts (resolveConfigurationByProviders)
func (h *DebugHost) resolveConfiguration(ctx context.Context, folder, typ string, cfg debug.Config) (debug.Config, error) {
	h.activate(ctx, typ)
	for _, p := range h.providersFor(typ, nil, func(p extension.DebugConfigurationProvider) bool { return p.Resolve != nil }) {
		if cfg == nil {
			break
		}
		next, err := h.callResolve(ctx, p, folder, cfg, p.p.Resolve)
		if err != nil {
			return nil, err
		}
		cfg = next
	}
	return cfg, nil
}

// resolveSubstituted is debug.Options.ResolveSubstituted: the providers of the type, then those of "*".
//
// Ported from VS Code: src/vs/workbench/contrib/debug/browser/debugConfigurationManager.ts (resolveDebugConfigurationWithSubstitutedVariables)
func (h *DebugHost) resolveSubstituted(ctx context.Context, folder, typ string, cfg debug.Config) (debug.Config, error) {
	has := func(p extension.DebugConfigurationProvider) bool { return p.ResolveSubstituted != nil }
	list := h.providersFor(typ, nil, has)
	if typ != "*" {
		list = append(list, h.providersFor("*", nil, has)...)
	}
	for _, p := range list {
		if cfg == nil {
			break
		}
		next, err := h.callResolve(ctx, p, folder, cfg, p.p.ResolveSubstituted)
		if err != nil {
			return nil, err
		}
		cfg = next
	}
	return cfg, nil
}

// providerContext is the context a configuration provider's function gets: the life of its extension, with the origin of the start it
// serves (extension.OriginFromContext), because the function runs on a goroutine where Origins().Current is the host's; a picker the
// provider shows names that origin and so appears on the screen of whoever pressed F5 (decision 1500).
func providerContext(inst *instance, start context.Context) context.Context {
	if o := extension.OriginFromContext(start); o != extension.HostOrigin {
		return extension.WithOrigin(inst.ctx, o)
	}
	return inst.ctx
}

func (h *DebugHost) callResolve(start context.Context, p *providerEntry, folder string, cfg debug.Config, fn func(context.Context, string, extension.DebugConfiguration) (extension.DebugConfiguration, error)) (debug.Config, error) {
	var out extension.DebugConfiguration
	err := h.guard(p.inst, "the configuration provider of "+p.typ, func() error {
		var e error
		out, e = fn(providerContext(p.inst, start), folder, extension.DebugConfiguration(cfg.Clone()))
		return e
	})
	if err != nil {
		return nil, err
	}
	if out == nil {
		return nil, nil
	}
	return debug.Config(out), nil
}

// initialConfigurations is debug.Options.InitialConfigurations: the configurations the manifest names, then those of the providers of
// trigger Initial.
//
// Ported from VS Code: src/vs/workbench/contrib/debug/common/debugger.ts (getInitialConfigurationContent)
func (h *DebugHost) initialConfigurations(ctx context.Context, folder, typ string) ([]debug.Config, error) {
	h.activate(ctx, typ)
	var out []debug.Config
	h.mu.Lock()
	if t := h.types[typ]; t != nil {
		for _, c := range t.spec.InitialConfigurations {
			cfg := debug.Config(c).Clone()
			if cfg["type"] == nil {
				cfg["type"] = typ
			}
			out = append(out, cfg)
		}
	}
	h.mu.Unlock()
	more, err := h.provided(ctx, folder, typ, extension.DebugTriggerInitial)
	if err != nil {
		return nil, err
	}
	return append(out, more...), nil
}

// dynamicConfigurations is debug.Options.DynamicConfigurations: the configurations the providers of trigger Dynamic give.
func (h *DebugHost) dynamicConfigurations(ctx context.Context, folder, typ string) ([]debug.Config, error) {
	h.activate(ctx, typ)
	return h.provided(ctx, folder, typ, extension.DebugTriggerDynamic)
}

func (h *DebugHost) provided(start context.Context, folder, typ string, trigger extension.DebugProviderTrigger) ([]debug.Config, error) {
	var out []debug.Config
	for _, p := range h.providersFor(typ, &trigger, func(p extension.DebugConfigurationProvider) bool { return p.Provide != nil }) {
		var list []extension.DebugConfiguration
		err := h.guard(p.inst, "the configuration provider of "+typ, func() error {
			var e error
			list, e = p.p.Provide(providerContext(p.inst, start), folder)
			return e
		})
		if err != nil {
			return nil, err
		}
		for _, c := range list {
			cfg := debug.Config(c).Clone()
			if cfg["type"] == nil {
				cfg["type"] = typ
			}
			out = append(out, cfg)
		}
	}
	return out, nil
}

// dynamicTypes is debug.Options.DynamicTypes.
func (h *DebugHost) dynamicTypes() []debug.DynamicType {
	h.mu.Lock()
	defer h.mu.Unlock()
	var out []debug.DynamicType
	for _, t := range h.types {
		if h.dynamicLocked(t) {
			out = append(out, debug.DynamicType{Type: t.spec.Type, Label: labelOf(t.spec)})
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Type < out[j].Type })
	return out
}

// ---- the sessions of the extensions' view

// debugSession is the extension.DebugSession of a session of the service.
type debugSession struct {
	h *DebugHost
	s *debug.Session
}

var _ extension.DebugSession = (*debugSession)(nil)

// wrap gives the extension's view of a session; the same session gives the same value.
func (h *DebugHost) wrap(s *debug.Session) *debugSession {
	if s == nil {
		return nil
	}
	h.mu.Lock()
	defer h.mu.Unlock()
	if d := h.sessions[s]; d != nil {
		return d
	}
	d := &debugSession{h: h, s: s}
	h.sessions[s] = d
	return d
}

// wrapAll wraps sessions for the extension; a nil session is a nil interface.
func (h *DebugHost) wrapOrNil(s *debug.Session) extension.DebugSession {
	if s == nil {
		return nil
	}
	return h.wrap(s)
}

// ID implements extension.DebugSession.
func (d *debugSession) ID() string { return d.s.ID() }

// Type implements extension.DebugSession.
func (d *debugSession) Type() string { return d.s.Type() }

// Name implements extension.DebugSession.
func (d *debugSession) Name() string { return d.s.Name() }

// Folder implements extension.DebugSession.
func (d *debugSession) Folder() string { return d.s.Root() }

// Configuration implements extension.DebugSession.
func (d *debugSession) Configuration() extension.DebugConfiguration {
	return extension.DebugConfiguration(d.s.Configuration())
}

// Parent implements extension.DebugSession.
func (d *debugSession) Parent() extension.DebugSession { return d.h.wrapOrNil(d.s.Parent()) }

// CustomRequest implements extension.DebugSession.
func (d *debugSession) CustomRequest(ctx context.Context, command string, args any) (json.RawMessage, error) {
	return d.s.CustomRequest(ctx, command, args)
}

// coreSession is the session of the service behind a session the extension holds. It fails with ExtensionDebugSessionForeign for a
// value this host did not make.
func (h *DebugHost) coreSession(es extension.DebugSession) (*debug.Session, error) {
	d, ok := es.(*debugSession)
	if !ok || d == nil || d.h != h {
		return nil, errors.New("ExtensionDebugSessionForeign", "the session is not one of this window tab's debug sessions")
	}
	return d.s, nil
}

// ---- hub

// hub is a list of listeners that extensions added.
type hub[F any] struct {
	mu   sync.Mutex
	next int
	list map[int]hubEntry[F]
}

type hubEntry[F any] struct {
	inst *instance
	fn   F
}

func (b *hub[F]) add(inst *instance, fn F) (remove func()) {
	b.mu.Lock()
	if b.list == nil {
		b.list = map[int]hubEntry[F]{}
	}
	b.next++
	id := b.next
	b.list[id] = hubEntry[F]{inst, fn}
	b.mu.Unlock()
	return func() {
		b.mu.Lock()
		delete(b.list, id)
		b.mu.Unlock()
	}
}

// entries lists the listeners in the order they were added.
func (b *hub[F]) entries() []hubEntry[F] {
	b.mu.Lock()
	defer b.mu.Unlock()
	ids := make([]int, 0, len(b.list))
	for id := range b.list {
		ids = append(ids, id)
	}
	sort.Ints(ids)
	out := make([]hubEntry[F], len(ids))
	for i, id := range ids {
		out[i] = b.list[id]
	}
	return out
}

// ---- events of the service

// onEvent follows the service: the listeners of the extensions hear about it on the UI thread, and the trackers about the end.
func (h *DebugHost) onEvent(ev debug.Event) {
	switch ev.Kind {
	case debug.EventSessionStarted:
		es := h.wrap(ev.Session)
		h.fire(func() {
			for _, e := range h.startFns.entries() {
				h.safe(e.inst, "OnDidStartSession", func() { e.fn(es) })
			}
		})
	case debug.EventSessionEnded:
		h.endTrackers(ev.Session, ev.Err)
		if ev.Restart {
			return
		}
		h.mu.Lock()
		delete(h.servers, ev.Session)
		h.mu.Unlock()
		es := h.wrap(ev.Session)
		h.fire(func() {
			for _, e := range h.endFns.entries() {
				h.safe(e.inst, "OnDidTerminateSession", func() { e.fn(es) })
			}
			h.mu.Lock()
			delete(h.sessions, ev.Session)
			h.mu.Unlock()
		})
	case debug.EventFocus:
		cur := h.svc.Model().Focus().Session
		id := ""
		if cur != nil {
			id = cur.ID()
		}
		h.mu.Lock()
		changed := id != h.active
		h.active = id
		h.mu.Unlock()
		if changed {
			es := h.wrapOrNil(cur)
			h.fire(func() {
				for _, e := range h.activeFns.entries() {
					h.safe(e.inst, "OnDidChangeActiveSession", func() { e.fn(es) })
				}
			})
		}
	case debug.EventCustom:
		if ev.DAP == nil || ev.Session == nil {
			return
		}
		out := extension.DebugSessionCustomEvent{Session: h.wrap(ev.Session), Event: ev.DAP.Event, Body: ev.DAP.Body}
		h.fire(func() {
			for _, e := range h.customFns.entries() {
				h.safe(e.inst, "OnDidReceiveCustomEvent", func() { e.fn(out) })
			}
		})
	case debug.EventBreakpoints:
		if ev.Breakpoints.SessionOnly {
			return
		}
		change := h.breakpointsChange(ev.Breakpoints)
		h.fire(func() {
			for _, e := range h.bpFns.entries() {
				h.safe(e.inst, "OnDidChangeBreakpoints", func() { e.fn(change) })
			}
		})
	}
}

// fire runs f on the UI thread, later.
func (h *DebugHost) fire(f func()) {
	if sc := h.scopeOf(); sc != nil {
		sc.post(f)
		return
	}
	f()
}

// ---- breakpoints

func (h *DebugHost) breakpointsChange(c debug.BreakpointsChange) extension.DebugBreakpointsChange {
	var out extension.DebugBreakpointsChange
	for _, id := range c.Added {
		if b, ok := h.breakpoint(id); ok {
			out.Added = append(out.Added, b)
		}
	}
	for _, id := range c.Changed {
		if b, ok := h.breakpoint(id); ok {
			out.Changed = append(out.Changed, b)
		}
	}
	for _, id := range c.Removed {
		out.Removed = append(out.Removed, extension.DebugBreakpoint{ID: id})
	}
	return out
}

func (h *DebugHost) breakpoint(id string) (extension.DebugBreakpoint, bool) {
	if b, ok := h.svc.Model().Breakpoint(id); ok {
		return fromLineBreakpoint(b), true
	}
	for _, f := range h.svc.Model().FunctionBreakpoints() {
		if f.ID == id {
			return fromFunctionBreakpoint(f), true
		}
	}
	return extension.DebugBreakpoint{}, false
}

func fromLineBreakpoint(b debug.Breakpoint) extension.DebugBreakpoint {
	line, col := b.RequestedLine, b.RequestedColumn
	if line == 0 {
		line, col = b.Line, b.Column
	}
	pos := extension.Position{Line: max(line-1, 0)}
	if col > 1 {
		pos.Col = col - 1
	}
	return extension.DebugBreakpoint{
		ID: b.ID, URI: document.FileURI(b.Path).String(), Position: pos, Disabled: !b.Enabled,
		Condition: b.Condition, HitCondition: b.HitCondition, LogMessage: b.LogMessage,
	}
}

func fromFunctionBreakpoint(f debug.FunctionBreakpoint) extension.DebugBreakpoint {
	return extension.DebugBreakpoint{ID: f.ID, Function: f.Name, Disabled: !f.Enabled, Condition: f.Condition, HitCondition: f.HitCondition}
}

// breakpoints lists the breakpoints of the service for the extension.
func (h *DebugHost) breakpoints() []extension.DebugBreakpoint {
	var out []extension.DebugBreakpoint
	for _, b := range h.svc.Model().Breakpoints(debug.BreakpointFilter{}) {
		out = append(out, fromLineBreakpoint(b))
	}
	for _, f := range h.svc.Model().FunctionBreakpoints() {
		out = append(out, fromFunctionBreakpoint(f))
	}
	return out
}

// installDebuggers adds the debug types and the breakpoint languages the manifest of an extension declares to the debug host, so that
// they are there before the extension is activated. It returns what takes them away; that also ends the sessions of the extension's
// debug types.
func (s *Scope) installDebuggers(sl *slot) []func() {
	h := s.opts.Debug
	m := sl.info.Manifest
	if h == nil || (len(m.Contributes.Debuggers) == 0 && len(m.Contributes.Breakpoints) == 0) {
		return nil
	}
	h.addTypes(m)
	return []func(){func() { h.removeTypes(m) }}
}

// InitialConfigurations lists the configurations a debug type gives a new launch.json: the ones its manifest names, then those of the
// providers that offer them for a new file (the window's Add Configuration and Open 'launch.json' use them).
func (h *DebugHost) InitialConfigurations(ctx context.Context, folder, typ string) ([]debug.Config, error) {
	return h.initialConfigurations(ctx, folder, typ)
}
