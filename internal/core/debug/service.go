package debug

import (
	"context"
	"log/slog"
	"sort"
	"sync"
	"time"

	"github.com/rm4n0s/emetos/internal/core/dap"
	"github.com/rm4n0s/errors"
)

// ConnectFunc reaches the debug adapter of a configuration and starts a client over the connection with the given options (which
// carry the session's handler of the adapter's requests). The context is the life of the adapter's program.
type ConnectFunc func(ctx context.Context, cfg Config, opts dap.ClientOptions) (*dap.Client, error)

// AdapterConnector makes a ConnectFunc of a function that says where the adapter is (the descriptor of a debugger, step 25.3).
func AdapterConnector(adapter func(ctx context.Context, cfg Config) (dap.Adapter, error)) ConnectFunc {
	return func(ctx context.Context, cfg Config, opts dap.ClientOptions) (*dap.Client, error) {
		a, err := adapter(ctx, cfg)
		if err != nil {
			return nil, err
		}
		return dap.Connect(ctx, a, opts)
	}
}

// Debugger is a debug type that can be started: the contribution of a language extension.
type Debugger struct {
	// Type is the debug type of the configurations it runs.
	Type string
	// Label is the name shown to the user.
	Label string
	// Connect reaches the adapter.
	Connect ConnectFunc
	// Variables maps the names of interactive variables (${command:pickProcess}) to the ids of commands.
	Variables map[string]string
	// Languages are the languages the debugger is the default one for.
	Languages []string
}

func (d Debugger) connect(ctx context.Context, cfg Config, opts dap.ClientOptions) (*dap.Client, error) {
	if d.Connect == nil {
		return nil, errors.New("DebugConnectMissing", "the debugger has no way to reach its adapter", "type", d.Type)
	}
	return d.Connect(ctx, cfg, opts)
}

// TaskResult is how a task before or after a session ended.
type TaskResult int

const (
	// TaskSuccess is a task that ran and passed, or no task.
	TaskSuccess TaskResult = iota
	// TaskFailure is a task that failed and whose failure stops the launch (after the question of debug.onTaskErrors).
	TaskFailure
)

// GuessResult is the debugger chosen for the active editor when there is no configuration (F5 with no launch.json).
type GuessResult struct {
	// Type is the debug type.
	Type string
	// Launch and Config are a dynamic configuration the debugger gave, if any.
	Launch *Launch
	Config Config
	// Label is its label.
	Label string
}

// DynamicType is a debug type that provides dynamic configurations.
type DynamicType struct {
	Type  string
	Label string
}

// Options are the callbacks and settings the window gives the service. Every member is optional.
type Options struct {
	// Logger receives the problems that do not stop a session; nil discards them.
	Logger *slog.Logger
	// Debugger returns the debugger of a debug type (the contributions of the extensions).
	Debugger func(typ string) (Debugger, bool)
	// ResolveConfiguration is resolveDebugConfiguration of the providers of a type (the type "*" asks the generic ones): it may
	// complete the configuration. It returns nil to cancel the launch, or ErrOpenLaunchFile to cancel it and show launch.json.
	ResolveConfiguration func(ctx context.Context, folder, typ string, cfg Config) (Config, error)
	// ResolveSubstituted is resolveDebugConfigurationWithSubstitutedVariables: after the variables, and after the task before.
	ResolveSubstituted func(ctx context.Context, folder, typ string, cfg Config) (Config, error)
	// InitialConfigurations are the configurations a debugger gives for a new launch.json.
	InitialConfigurations func(ctx context.Context, folder, typ string) ([]Config, error)
	// DynamicConfigurations are the configurations a debugger gives for the dropdown without a launch file.
	DynamicConfigurations func(ctx context.Context, folder, typ string) ([]Config, error)
	// DynamicTypes lists the debug types that provide dynamic configurations.
	DynamicTypes func() []DynamicType
	// Guess chooses the debugger for the active editor when a configuration has no type.
	Guess func(ctx context.Context) (GuessResult, bool)
	// Variables returns the values the variables read (the active editor, the folders, the settings); nil resolves with none. ctx
	// is the context the start was asked with, so that the window can answer for the screen that asked (decision 1180).
	Variables func(ctx context.Context) *VariableContext
	// InputMemory remembers the last value of each input.
	InputMemory InputMemory
	// RunTask runs a task of the Tasks extension before or after a session and tells how it ended; the question about a failed
	// task (debug.onTaskErrors) is the callback's.
	RunTask func(ctx context.Context, folder string, task TaskRef) (TaskResult, error)
	// SaveBeforeStart saves the files as debug.saveBeforeStart says.
	SaveBeforeStart func(ctx context.Context) error
	// ConfirmConcurrent asks whether to start a second instance of a configuration that runs; nil means yes.
	ConfirmConcurrent func(ctx context.Context, label string) bool
	// RunInTerminal runs the program of a runInTerminal request in a terminal and returns the process id of its shell, or 0.
	RunInTerminal func(ctx context.Context, args dap.RunInTerminalRequestArguments, sessionID string) (shellPID int, err error)
	// OpenLaunchFile shows launch.json of a launch (after a provider cancelled with ErrOpenLaunchFile); typ is the debug type.
	OpenLaunchFile func(ctx context.Context, l *Launch, typ string)
	// Trace sees every message of every session in the order of the wire (the debug adapter trackers of step 25.3).
	Trace func(s *Session, m dap.TraceMessage)
	// AutoExpandLazyVariables says whether lazy variables are evaluated when they are listed.
	AutoExpandLazyVariables func() bool
	// Console gives the settings of the Debug Console that the model reads when a line comes (debug.console.collapseIdenticalLines and
	// debug.console.maximumLines); DefaultReplConfig when nil.
	Console func() ReplConfig
	// StackDelay is how long after the first frame of a stop the rest of the stack is loaded (VS Code: 420 ms).
	StackDelay time.Duration
	// RestartDelay is how long a restart without a restart request waits between the end and the new start (VS Code: 300 ms).
	RestartDelay time.Duration
	// FocusDelay is how long after a thread continues the focus passes to another stopped one (VS Code: 800 ms); RunningDelay is
	// how long after a session runs the frame is defocused (VS Code: 200 ms).
	FocusDelay, RunningDelay time.Duration
}

// Service starts, restarts and stops sessions, keeps the model and the launches, and the breakpoints sent to the adapters. It is the
// entry point of the window.
//
// Ported from VS Code: src/vs/workbench/contrib/debug/browser/debugService.ts
type Service struct {
	base     context.Context
	opts     Options
	model    *Model
	launches *Launches

	mu                sync.Mutex
	initializing      bool
	cancels           map[string]context.CancelFunc
	restarting        map[*Session]bool
	pendingSend       map[string]bool // files whose breakpoints are sent when they are saved
	firstSessionStart bool
	passTimer         map[*Session]*time.Timer
	inputs            *LRUInputMemory
	runningTimer      map[*Session]*time.Timer
	hasDebugged       bool
}

// NewService makes a service. ctx is the life of the window: the adapters' programs end with it.
func NewService(ctx context.Context, opts Options) *Service {
	s := &Service{
		base: ctx, opts: opts, model: NewModel(), cancels: map[string]context.CancelFunc{},
		inputs: &LRUInputMemory{}, restarting: map[*Session]bool{}, pendingSend: map[string]bool{}, firstSessionStart: true,
		passTimer: map[*Session]*time.Timer{}, runningTimer: map[*Session]*time.Timer{},
	}
	s.launches = newLaunches(s)
	return s
}

// Model returns the model.
func (s *Service) Model() *Model { return s.model }

// Launches returns the launches and the selected configuration.
func (s *Service) Launches() *Launches { return s.launches }

// Subscribe registers a listener of the changes of the model (Model.Subscribe).
func (s *Service) Subscribe(fn func(Event)) (cancel func()) { return s.model.Subscribe(fn) }

// FirstSessionStart tells whether no session has been started since the window opened (debug.openDebug and
// debug.internalConsoleOptions have a case for the first one).
func (s *Service) FirstSessionStart() bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.firstSessionStart
}

// HasDebugged tells whether a session was started in this window (the context key debuggersAvailable / hasDebugged).
func (s *Service) HasDebugged() bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.hasDebugged
}

// State is the state of the focused session; without one, Initializing while a session is being made, else Inactive.
func (s *Service) State() State {
	f := s.model.Focus()
	if f.Session != nil {
		return f.Session.State()
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.initializing {
		return Initializing
	}
	return Inactive
}

func (s *Service) setInitializing(on bool) {
	s.mu.Lock()
	changed := s.initializing != on
	s.initializing = on
	s.mu.Unlock()
	if changed {
		s.model.emit(Event{Kind: EventSessions})
	}
}

// ---- starting

// Target says what to start: the configuration or compound with a name in the launch, or a configuration given whole.
type Target struct {
	Name   string
	Config Config
}

// StartDebugging starts a configuration or a compound. It saves the files first (unless the options say not to or this is a child),
// asks the providers, substitutes the variables, runs the task before, and starts the session. It returns false with no error when
// the launch was cancelled (by the user at an input, by a provider, by a failed task); an error says what is wrong with the
// configuration or the adapter.
//
// Ported from VS Code: src/vs/workbench/contrib/debug/browser/debugService.ts (startDebugging, createSession, doCreateSession)
func (s *Service) StartDebugging(ctx context.Context, launch *Launch, target Target, opts SessionOptions) (bool, error) {
	s.setInitializing(true)
	s.mu.Lock()
	s.hasDebugged = true
	s.mu.Unlock()
	ok, err := s.startDebugging(ctx, launch, target, opts)
	s.endInitializing()
	return ok, err
}

func (s *Service) endInitializing() {
	s.setInitializing(false)
}

func (s *Service) startDebugging(ctx context.Context, launch *Launch, target Target, opts SessionOptions) (bool, error) {
	if !opts.NoSaveBeforeStart && opts.Parent == nil && s.opts.SaveBeforeStart != nil {
		if err := s.opts.SaveBeforeStart(ctx); err != nil {
			return false, err
		}
	}
	name := target.Name
	if name == "" && target.Config == nil {
		sel := s.launches.Selected()
		name = sel.Name
		if launch == nil {
			launch = sel.Launch
		}
	}
	var cfg Config
	var compound Compound
	isCompound := false
	switch {
	case target.Config != nil:
		cfg = target.Config
	case name != "" && launch != nil:
		if c, ok := launch.Configuration(name); ok {
			cfg = c
		}
		if comp, ok := launch.Compound(name); ok && cfg == nil {
			compound, isCompound = comp, true
		}
	}
	if cfg == nil && !isCompound && name == "" {
		// The selection may be a dynamic configuration.
		c, err := s.launches.SelectedConfig(ctx)
		if err != nil {
			return false, err
		}
		cfg = c
	}
	if isCompound {
		return s.startCompound(ctx, launch, compound, opts)
	}
	if cfg == nil && name != "" {
		if sel := s.launches.Selected(); sel.Dynamic && sel.Name == name {
			c, err := s.launches.SelectedConfig(ctx)
			if err != nil {
				return false, err
			}
			cfg = c
		}
	}
	if cfg == nil && name != "" {
		if launch != nil {
			return false, errors.New("DebugConfigurationMissing", "Configuration '"+name+"' is missing in 'launch.json'.", "name", name)
		}
		return false, errors.New("DebugLaunchMissing", "'launch.json' does not exist for passed workspace folder.", "name", name)
	}
	return s.createSession(ctx, launch, cfg, opts)
}

// startCompound starts the configurations of a compound together; it succeeds when each did.
func (s *Service) startCompound(ctx context.Context, launch *Launch, comp Compound, opts SessionOptions) (bool, error) {
	if len(comp.Configurations) == 0 {
		return false, errors.New("DebugCompoundEmpty", "Compound must have \"configurations\" attribute set in order to start multiple configurations.", "compound", comp.Name)
	}
	if !comp.PreLaunchTask.IsZero() {
		res, err := s.runTask(ctx, launch, comp.PreLaunchTask)
		if err != nil {
			return false, err
		}
		if res == TaskFailure {
			return false, nil
		}
	}
	if comp.StopAll {
		opts.CompoundRoot = &CompoundRoot{}
	}
	type job struct {
		launch *Launch
		cfg    Config
	}
	var jobs []job
	for _, ref := range comp.Configurations {
		if ref.Name == comp.Name {
			continue
		}
		var from *Launch
		switch ref.Folder {
		case "":
			var having []*Launch
			for _, l := range s.launches.Launches() {
				if _, ok := l.Configuration(ref.Name); ok {
					having = append(having, l)
				}
			}
			switch {
			case len(having) == 1:
				from = having[0]
			case len(having) > 1 && launch != nil && containsLaunch(having, launch):
				from = launch
			case len(having) == 0:
				return false, errors.New("DebugCompoundConfigurationMissing", "Could not find launch configuration '"+ref.Name+"' in the workspace.", "compound", comp.Name, "name", ref.Name)
			default:
				return false, errors.New("DebugCompoundConfigurationAmbiguous", "There are multiple launch configurations '"+ref.Name+"' in the workspace. Use folder name to qualify the configuration.", "compound", comp.Name, "name", ref.Name)
			}
		default:
			var match []*Launch
			for _, l := range s.launches.Launches() {
				if l.Kind == LaunchFolder && l.Name == ref.Folder {
					if _, ok := l.Configuration(ref.Name); ok {
						match = append(match, l)
					}
				}
			}
			if len(match) != 1 {
				return false, errors.New("DebugCompoundFolderMissing", "Can not find folder with name '"+ref.Folder+"' for configuration '"+ref.Name+"' in compound '"+comp.Name+"'.", "compound", comp.Name, "folder", ref.Folder, "name", ref.Name)
			}
			from = match[0]
		}
		cfg, _ := from.Configuration(ref.Name)
		jobs = append(jobs, job{from, cfg})
	}
	type result struct {
		ok  bool
		err error
	}
	results := make([]result, len(jobs))
	var wg sync.WaitGroup
	for i, j := range jobs {
		wg.Add(1)
		go func() {
			defer wg.Done()
			ok, err := s.createSession(ctx, j.launch, j.cfg, opts)
			results[i] = result{ok, err}
		}()
	}
	wg.Wait()
	all := true
	var firstErr error
	for _, r := range results {
		all = all && r.ok
		if firstErr == nil && r.err != nil {
			firstErr = r.err
		}
	}
	return all, firstErr
}

func containsLaunch(list []*Launch, l *Launch) bool {
	for _, x := range list {
		if x == l {
			return true
		}
	}
	return false
}

func (s *Service) runTask(ctx context.Context, launch *Launch, task TaskRef) (TaskResult, error) {
	if task.IsZero() || s.opts.RunTask == nil {
		return TaskSuccess, nil
	}
	folder := ""
	if launch != nil {
		folder = launch.Folder
	}
	return s.opts.RunTask(ctx, folder, task)
}

func (s *Service) debuggerFor(typ string) (Debugger, bool) {
	if s.opts.Debugger == nil {
		return Debugger{}, false
	}
	return s.opts.Debugger(typ)
}

// inputsFor are the inputs ${input:id} may name: those of the launch, then those of the others.
func (s *Service) inputsFor(launch *Launch) []Input {
	var out []Input
	if launch != nil {
		out = append(out, launch.Inputs()...)
	}
	for _, l := range s.launches.Launches() {
		if l != launch {
			out = append(out, l.Inputs()...)
		}
	}
	return out
}

// substitute replaces the variables of a configuration with the debugger's interactive variables and the launch's inputs. ok is
// false when the user cancelled.
func (s *Service) substitute(ctx context.Context, launch *Launch, folder string, cfg Config) (Config, bool, error) {
	dbg, found := s.debuggerFor(cfg.Type())
	if !found {
		return cfg, true, nil
	}
	var vc *VariableContext
	if s.opts.Variables != nil {
		vc = s.opts.Variables(ctx)
	}
	return ResolveConfig(ctx, vc, cfg, ResolveOptions{
		Folder: folder, Interactive: true, Inputs: s.inputsFor(launch), Commands: dbg.Variables, Section: "launch", Memory: s.inputMemory(),
	})
}

// createSession gets the debugger for the type, resolves the configuration by the providers, substitutes the variables, runs the
// task before and starts the session.
func (s *Service) createSession(ctx context.Context, launch *Launch, cfg Config, opts SessionOptions) (bool, error) {
	if cfg == nil {
		cfg = Config{}
	} else {
		cfg = cfg.Clone()
	}
	typ := cfg.Type()
	if opts.NoDebug != nil && *opts.NoDebug {
		cfg["noDebug"] = true
	} else if opts.NoDebug == nil && opts.Parent != nil && opts.Parent.Configuration().NoDebug() {
		cfg["noDebug"] = true
	}
	unresolved := cfg.Clone()
	folder := ""
	if launch != nil {
		folder = launch.Folder
	} else if opts.Parent != nil {
		folder = opts.Parent.root
	}
	if typ == "" && s.opts.Guess != nil {
		if g, ok := s.opts.Guess(ctx); ok {
			typ = g.Type
			if cfg.Type() == "" {
				cfg["type"] = g.Type
			}
			if g.Config != nil {
				if g.Launch != nil {
					launch = g.Launch
					folder = g.Launch.Folder
				}
				for k, v := range g.Config {
					cfg[k] = v
				}
			}
		}
	}

	sessionID := newID()
	cctx, cancel := context.WithCancel(ctx)
	s.mu.Lock()
	s.cancels[sessionID] = cancel
	s.mu.Unlock()
	defer func() {
		cancel()
		s.mu.Lock()
		delete(s.cancels, sessionID)
		s.mu.Unlock()
	}()

	byProviders, err := s.launches.ResolveByProviders(cctx, folder, typ, cfg)
	if err != nil {
		if errors.Is(err, ErrOpenLaunchFile) {
			if launch != nil && s.opts.OpenLaunchFile != nil && cctx.Err() == nil {
				s.opts.OpenLaunchFile(cctx, launch, typ)
			}
			return false, nil
		}
		return false, err
	}
	if byProviders == nil {
		return false, nil
	}
	if byProviders.Type() == "" {
		return false, errors.New("DebugTypeMissing", "Missing property 'type' for the chosen launch configuration.", "name", byProviders.Name())
	}
	resolved, ok, err := s.substitute(cctx, launch, folder, byProviders)
	if err != nil {
		return false, err
	}
	if !ok || cctx.Err() != nil {
		return false, nil
	}

	if opts.StartedByUser && !resolved.suppressMultiple() {
		for _, ex := range s.model.Sessions(false) {
			c := ex.Configuration()
			if c.Name() == resolved.Name() && c.Type() == resolved.Type() && c.Request() == resolved.Request() && ex.root == folder {
				if s.opts.ConfirmConcurrent != nil && !s.opts.ConfirmConcurrent(cctx, ex.Label()) {
					return false, nil
				}
				break
			}
		}
	}

	if task := resolved.Task("preLaunchTask"); !task.IsZero() {
		res, err := s.runTask(cctx, launch, task)
		if err != nil {
			return false, err
		}
		if res == TaskFailure {
			return false, nil
		}
	}
	if s.opts.ResolveSubstituted != nil {
		out, err := s.opts.ResolveSubstituted(cctx, folder, resolved.Type(), resolved)
		if err != nil {
			if errors.Is(err, ErrOpenLaunchFile) {
				if launch != nil && s.opts.OpenLaunchFile != nil && cctx.Err() == nil {
					s.opts.OpenLaunchFile(cctx, launch, typ)
				}
				return false, nil
			}
			return false, err
		}
		if out == nil {
			return false, nil
		}
		resolved = out
	}
	if r := resolved.Request(); r != "launch" && r != "attach" {
		if r == "" {
			return false, errors.New("DebugRequestMissing", "Attribute 'request' is missing from the chosen debug configuration.", "name", resolved.Name())
		}
		return false, errors.New("DebugRequestUnsupported", "Attribute 'request' has an unsupported value '"+r+"' in the chosen debug configuration.", "name", resolved.Name(), "request", r)
	}
	dbg, found := s.debuggerFor(resolved.Type())
	if !found {
		return false, errors.New("DebugTypeUnsupported", "Configured debug type '"+resolved.Type()+"' is not supported.", "type", resolved.Type(), "name", resolved.Name())
	}
	return s.doCreateSession(cctx, sessionID, folder, dbg, resolved, unresolved, opts)
}

// suppressMultiple tells whether the configuration turns off the question about a second instance.
func (c Config) suppressMultiple() bool {
	b, _ := c["suppressMultipleSessionWarning"].(bool)
	return b
}

// doCreateSession makes the session, adds it to the model and runs initialize and launch or attach.
func (s *Service) doCreateSession(ctx context.Context, id, root string, dbg Debugger, resolved, unresolved Config, opts SessionOptions) (bool, error) {
	sess := newSession(s, id, resolved, unresolved, root, dbg, opts)
	if opts.CompoundRoot != nil {
		opts.CompoundRoot.OnSessionStop(func() {
			if err := sess.Terminate(s.base, false); err != nil {
				s.logger().Warn("stopping the sessions of a compound failed", "session", sess.id, "err", err)
			}
		})
	}
	s.model.addSession(sess)
	if err := s.launchOrAttachToSession(ctx, sess, false); err != nil {
		if errors.Is(err, context.Canceled) || sess.stoppedByUser() {
			return false, nil
		}
		return false, err
	}
	s.mu.Lock()
	first := s.firstSessionStart
	s.firstSessionStart = false
	s.mu.Unlock()
	s.model.emit(Event{Kind: EventSessionStarted, Session: sess, First: first})
	return true, nil
}

func (s *Service) launchOrAttachToSession(ctx context.Context, sess *Session, forceFocus bool) error {
	if err := sess.initialize(ctx); err != nil {
		s.defocusIfFocused(sess)
		return err
	}
	if err := sess.launchOrAttach(ctx, sess.Configuration()); err != nil {
		s.defocusIfFocused(sess)
		return err
	}
	f := s.model.Focus()
	if forceFocus || f.Session == nil || (sess.parent != nil && sess.parent == f.Session && sess.opts.Compact) {
		s.FocusStackFrame(nil, nil, sess)
	}
	return nil
}

func (s *Service) defocusIfFocused(sess *Session) {
	if s.model.Focus().Session == sess {
		s.FocusStackFrame(nil, nil, nil)
	}
}

// startChild starts the session an adapter asked for with startDebugging; it is shown under its parent.
func (s *Service) startChild(ctx context.Context, parent *Session, cfg Config) (bool, error) {
	return s.startDebugging(ctx, nil, Target{Config: cfg}, SessionOptions{Parent: parent})
}

// ---- ending

// sessionEnded runs when a session's adapter has ended: the task after the session, the focus, the exception filters. cause is
// the failure of an adapter that died, or nil.
func (s *Service) sessionEnded(sess *Session, cause error) {
	s.mu.Lock()
	restart := s.restarting[sess]
	s.mu.Unlock()
	if task := sess.Configuration().Task("postDebugTask"); !restart && !task.IsZero() && s.opts.RunTask != nil {
		var launch *Launch
		if sess.root != "" {
			launch = s.launches.Launch(sess.root)
		}
		if _, err := s.runTask(s.base, launch, task); err != nil {
			s.logger().Warn("the task after the session failed", "session", sess.id, "err", err)
		}
	}
	s.cancelTokens(sess.id)
	if !restart {
		sess.cancelLife()
	}
	s.cancelTimers(sess)
	s.model.removeExceptionBreakpointsForSession(sess.id)
	s.model.clearTriggered(sess.id)
	s.model.emit(Event{Kind: EventSessionEnded, Session: sess, Restart: restart, Err: cause})
	if f := s.model.Focus(); f.Session == sess {
		fs, ft, ff := s.toFocus(nil, nil, nil, sess)
		s.model.setFocus(Focus{Session: fs, Thread: ft, Frame: ff})
	}
	if len(s.model.Sessions(false)) == 0 {
		s.setInitializing(false)
	}
}

func (s *Service) cancelTokens(id string) {
	s.mu.Lock()
	cancel := s.cancels[id]
	delete(s.cancels, id)
	s.mu.Unlock()
	if cancel != nil {
		cancel()
	}
}

// Stop ends a session: Terminate, or Disconnect for an attach (the toolbar's Stop becomes Disconnect then). Without a session it
// stops all of them; with none it cancels the sessions being started.
//
// Ported from VS Code: src/vs/workbench/contrib/debug/browser/debugService.ts (stopSession)
func (s *Service) Stop(ctx context.Context, sess *Session, disconnect, suspend bool) error {
	if sess != nil {
		if disconnect {
			return sess.Disconnect(ctx, false, suspend)
		}
		return sess.Terminate(ctx, false)
	}
	sessions := s.model.Sessions(false)
	if len(sessions) == 0 {
		s.mu.Lock()
		cancels := s.cancels
		s.cancels = map[string]context.CancelFunc{}
		s.mu.Unlock()
		for _, c := range cancels {
			c()
		}
		s.endInitializing()
		return nil
	}
	var wg sync.WaitGroup
	errs := make([]error, len(sessions))
	for i, x := range sessions {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if disconnect {
				errs[i] = x.Disconnect(ctx, false, suspend)
			} else {
				errs[i] = x.Terminate(ctx, false)
			}
		}()
	}
	wg.Wait()
	for _, err := range errs {
		if err != nil {
			return err
		}
	}
	return nil
}

// RestartSession restarts a session: the adapter's restart when it has one, else it ends the session and starts it again with the
// configuration read again from launch.json. restartData, when not nil, is what a terminated event asked the restart with (an
// automatic restart, which runs no task).
//
// Ported from VS Code: src/vs/workbench/contrib/debug/browser/debugService.ts (restartSession)
func (s *Service) RestartSession(ctx context.Context, sess *Session, restartData any) error {
	if !sess.opts.NoSaveBeforeRestart && s.opts.SaveBeforeStart != nil {
		if err := s.opts.SaveBeforeStart(ctx); err != nil {
			return err
		}
	}
	auto := restartData != nil
	var launch *Launch
	if sess.root != "" {
		launch = s.launches.Launch(sess.root)
	}
	runTasks := func() (TaskResult, error) {
		if auto {
			return TaskSuccess, nil
		}
		cfg := sess.Configuration()
		for _, key := range []string{"preRestartTask", "postDebugTask"} {
			if _, err := s.runTask(ctx, launch, cfg.Task(key)); err != nil {
				return TaskFailure, err
			}
		}
		res, err := s.runTask(ctx, launch, cfg.Task("preLaunchTask"))
		if err != nil || res != TaskSuccess {
			return res, err
		}
		return s.runTask(ctx, launch, cfg.Task("postRestartTask"))
	}

	// Read the configuration again when launch.json changed; else use the one in memory.
	if launch != nil {
		if fresh, ok := launch.Configuration(sess.unresolvedName()); ok && !jsonEqual(fresh, sess.UnresolvedConfiguration()) {
			if sess.Configuration().NoDebug() {
				fresh["noDebug"] = true
			}
			byProviders, err := s.launches.ResolveByProviders(ctx, sess.root, fresh.Type(), fresh)
			if err != nil {
				return err
			}
			if byProviders == nil {
				return nil
			}
			subst, ok, err := s.substitute(ctx, launch, sess.root, byProviders)
			if err != nil || !ok {
				return err
			}
			if s.opts.ResolveSubstituted != nil {
				subst, err = s.opts.ResolveSubstituted(ctx, sess.root, subst.Type(), subst)
				if err != nil || subst == nil {
					return err
				}
			}
			resolved := subst
			sess.SetConfiguration(resolved, fresh)
		}
	}
	if restartData != nil {
		cfg := sess.Configuration()
		cfg["__restart"] = restartData
		sess.SetConfiguration(cfg, sess.UnresolvedConfiguration())
	}
	s.model.clearTriggered(sess.id)

	doRestart := func(fn func() (bool, error)) error {
		s.mu.Lock()
		s.restarting[sess] = true
		s.mu.Unlock()
		didRestart := false
		var err error
		func() {
			defer func() {
				s.mu.Lock()
				delete(s.restarting, sess)
				s.mu.Unlock()
			}()
			didRestart, err = fn()
		}()
		if !didRestart || err != nil {
			// The end of the session was announced as one that restarts; the restart failed, so announce the final one.
			s.model.emit(Event{Kind: EventSessionEnded, Session: sess, Restart: false, Err: err})
		}
		return err
	}

	if sess.Capabilities().SupportsRestartRequest {
		res, err := runTasks()
		if err != nil {
			return err
		}
		if res != TaskSuccess {
			return nil
		}
		return doRestart(func() (bool, error) {
			if err := sess.Restart(ctx); err != nil {
				return false, err
			}
			return true, nil
		})
	}

	shouldFocus := s.model.Focus().Session == sess
	return doRestart(func() (bool, error) {
		var err error
		if auto {
			err = sess.Disconnect(ctx, true, false)
		} else {
			err = sess.Terminate(ctx, true)
		}
		if err != nil {
			return false, err
		}
		// Wait for the end of the session: its adapter says terminated or exits.
		if err := waitEnded(ctx, sess); err != nil {
			return false, err
		}
		if d := s.opts.RestartDelay; d > 0 {
			select {
			case <-time.After(d):
			case <-ctx.Done():
				return false, nil
			}
		}
		res, err := runTasks()
		if err != nil || res != TaskSuccess {
			return false, err
		}
		if err := s.launchOrAttachToSession(ctx, sess, shouldFocus); err != nil {
			return false, err
		}
		s.model.emit(Event{Kind: EventSessionStarted, Session: sess})
		return true, nil
	})
}

// waitEnded waits until the session has no adapter.
func waitEnded(ctx context.Context, sess *Session) error {
	t := time.NewTicker(5 * time.Millisecond)
	defer t.Stop()
	for {
		sess.m.mu.Lock()
		gone := sess.client == nil
		sess.m.mu.Unlock()
		if gone {
			return nil
		}
		select {
		case <-ctx.Done():
			return errors.NewErr("DebugRestartCancelled", ctx.Err()).SetMetadata("session", sess.id)
		case <-t.C:
		}
	}
}

func (sess *Session) unresolvedName() string { return sess.UnresolvedConfiguration().Name() }

// ---- focus

// toFocus is VS Code's getStackFrameThreadAndSessionToFocus: what to focus given what was asked.
func (s *Service) toFocus(frame *StackFrame, thread *Thread, session, avoid *Session) (*Session, *Thread, *StackFrame) {
	if session == nil {
		switch {
		case frame != nil:
			session = frame.Thread.sess
		case thread != nil:
			session = thread.sess
		default:
			sessions := s.model.Sessions(false)
			for _, x := range sessions {
				if x.State() == Stopped {
					session = x
					break
				}
			}
			if session == nil {
				for _, x := range sessions {
					if x != avoid && (avoid == nil || x != avoid.parent) {
						session = x
						break
					}
				}
			}
			if session == nil && len(sessions) > 0 {
				session = sessions[0]
			}
		}
	}
	if thread == nil {
		if frame != nil {
			thread = frame.Thread
		} else if session != nil {
			threads := session.Threads()
			for _, t := range threads {
				if t.Stopped() {
					thread = t
					break
				}
			}
			if thread == nil && len(threads) > 0 {
				thread = threads[0]
			}
		}
	}
	if frame == nil && thread != nil {
		frame = thread.TopStackFrame()
	}
	return session, thread, frame
}

// FocusStackFrame focuses a stack frame, a thread (its top frame) or a session (its stopped thread); nil for all three picks what
// VS Code does: a stopped session, else the first.
func (s *Service) FocusStackFrame(frame *StackFrame, thread *Thread, session *Session) {
	sess, th, fr := s.toFocus(frame, thread, session, nil)
	s.model.setFocus(Focus{Session: sess, Thread: th, Frame: fr})
}

// focusAfterStop focuses the thread that stopped when the user has not focused something else.
func (s *Service) focusAfterStop(sess *Session, t *Thread, d StoppedDetails) {
	f := s.model.Focus()
	gone := f.Thread != nil && f.Thread.sess == sess && sess.Thread(f.Thread.ID) == nil
	if gone || (!d.PreserveFocusHint && len(t.CallStack()) > 0) {
		if f.Frame == nil || f.Frame.Thread.sess == sess || gone {
			s.FocusStackFrame(nil, t, nil)
		}
	}
}

func (s *Service) schedulePassFocus(sess *Session) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if t := s.passTimer[sess]; t != nil {
		t.Stop()
	}
	delay := s.opts.FocusDelay
	s.passTimer[sess] = time.AfterFunc(delay, func() { s.passFocus(sess) })
	if t := s.runningTimer[sess]; t != nil {
		t.Stop()
	}
	rd := s.opts.RunningDelay
	s.runningTimer[sess] = time.AfterFunc(rd, func() { s.defocusFrame(sess) })
}

// passFocus gives the focus to a stopped thread of another session when the focused one continued.
func (s *Service) passFocus(sess *Session) {
	any := false
	for _, x := range s.model.Sessions(false) {
		if x.State() == Stopped {
			any = true
		}
	}
	if !any {
		for _, t := range sess.Threads() {
			any = any || t.Stopped()
		}
	}
	if !any {
		return
	}
	sess.m.mu.Lock()
	last, has := sess.lastContinuedThread, sess.hasContinuedThread
	sess.m.mu.Unlock()
	f := s.model.Focus()
	if has {
		if f.Thread != nil && f.Thread.ID == last && f.Thread.sess == sess && !f.Thread.Stopped() {
			var target *Thread
			if d := sess.StoppedDetails(); d != nil && d.ThreadID != 0 {
				target = sess.Thread(d.ThreadID)
			}
			s.FocusStackFrame(nil, target, nil)
		}
		return
	}
	if f.Session == sess && sess.State() != Stopped {
		s.FocusStackFrame(nil, nil, nil)
	}
}

// defocusFrame drops the frame from the focus of a session that is running again.
func (s *Service) defocusFrame(sess *Session) {
	f := s.model.Focus()
	if sess.State() == Running && f.Session == sess && f.Frame != nil {
		s.model.setFocus(Focus{Session: sess, Thread: f.Thread})
	}
}

func (sess *Session) cancelPassFocus() {
	sess.svc.mu.Lock()
	if t := sess.svc.passTimer[sess]; t != nil {
		t.Stop()
		delete(sess.svc.passTimer, sess)
	}
	sess.svc.mu.Unlock()
}

func (s *Service) cancelTimers(sess *Session) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if t := s.passTimer[sess]; t != nil {
		t.Stop()
		delete(s.passTimer, sess)
	}
	if t := s.runningTimer[sess]; t != nil {
		t.Stop()
		delete(s.runningTimer, sess)
	}
}

// ---- watch expressions

// EvaluateWatchExpressions evaluates every watch expression in the focused stack frame (the Watch view does it when the focus
// changes).
func (s *Service) EvaluateWatchExpressions(ctx context.Context) {
	f := s.model.Focus()
	for _, w := range s.model.WatchExpressions() {
		if w.Name == "" {
			continue
		}
		_ = w.Evaluate(ctx, f.Session, f.Frame, dap.ContextWatch, true) // a failure is the value of the expression
	}
}

// ---- breakpoints

// AddBreakpoints adds breakpoints to a file and sends them to the sessions. The breakpoints are added even when sending fails.
func (s *Service) AddBreakpoints(ctx context.Context, path string, data []BreakpointData) ([]Breakpoint, error) {
	added := s.model.addBreakpoints(path, data)
	return added, s.sendBreakpoints(ctx, path, false, nil)
}

// RemoveBreakpoints removes breakpoints by id (all when none is given) and updates the sessions. A triggered breakpoint whose
// trigger was removed becomes an ordinary one.
func (s *Service) RemoveBreakpoints(ctx context.Context, ids ...string) error {
	all := s.model.Breakpoints(BreakpointFilter{})
	var targets []string
	if len(ids) == 0 {
		for _, b := range all {
			targets = append(targets, b.ID)
		}
	} else {
		targets = ids
	}
	clear := map[string]bool{}
	for _, b := range all {
		for _, id := range targets {
			if b.ID == id {
				clear[b.Path] = true
			}
		}
	}
	removed := s.model.removeBreakpoints(targets)
	// Breakpoints that waited for a removed one wait no more.
	drop := map[string]bool{}
	for _, r := range removed {
		drop[r.ID] = true
	}
	unlink := map[string]BreakpointUpdate{}
	for _, b := range s.model.Breakpoints(BreakpointFilter{TriggeredOnly: true}) {
		if drop[b.TriggeredBy] {
			unlink[b.ID] = BreakpointUpdate{TriggeredBy: sp("")}
			clear[b.Path] = true
		}
	}
	if len(unlink) > 0 {
		s.model.updateBreakpoints(unlink)
	}
	var first error
	for _, p := range sortedKeys(clear) {
		if err := s.sendBreakpoints(ctx, p, false, nil); err != nil && first == nil {
			first = err
		}
	}
	return first
}

func sortedKeys(m map[string]bool) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}

// UpdateBreakpoints changes breakpoints of a file. With sendOnSave the new positions go to the adapters when the file is saved
// (DocumentSaved), as the breakpoints that moved with edits do; else at once.
func (s *Service) UpdateBreakpoints(ctx context.Context, path string, updates map[string]BreakpointUpdate, sendOnSave bool) error {
	s.model.updateBreakpoints(updates)
	if sendOnSave {
		s.mu.Lock()
		s.pendingSend[path] = true
		s.mu.Unlock()
		return nil
	}
	return s.sendBreakpoints(ctx, path, false, nil)
}

// DocumentSaved sends, for a file that was saved, the breakpoints that moved with its edits (with sourceModified).
func (s *Service) DocumentSaved(ctx context.Context, path string) error {
	s.mu.Lock()
	pending := s.pendingSend[path]
	delete(s.pendingSend, path)
	s.mu.Unlock()
	if !pending {
		return nil
	}
	return s.sendBreakpoints(ctx, path, true, nil)
}

// DocumentDeleted removes the breakpoints of a file that was deleted.
func (s *Service) DocumentDeleted(path string) {
	var ids []string
	for _, b := range s.model.Breakpoints(BreakpointFilter{OriginalPath: path}) {
		ids = append(ids, b.ID)
	}
	if len(ids) > 0 {
		s.model.removeBreakpoints(ids)
	}
}

// EnableOrDisableBreakpoints enables or disables one breakpoint of any kind by id, or all of them when id is empty. A triggered
// breakpoint and the one that triggers it keep the same state.
func (s *Service) EnableOrDisableBreakpoints(ctx context.Context, enable bool, id string) error {
	if id == "" {
		s.model.enableAll(enable)
		return s.sendAllToSessions(ctx)
	}
	if !s.model.setEnablement(id, enable) {
		return nil
	}
	if bp, ok := s.model.Breakpoint(id); ok {
		if enable && bp.TriggeredBy != "" {
			if trig, ok := s.model.Breakpoint(bp.TriggeredBy); ok && !trig.Enabled {
				if err := s.EnableOrDisableBreakpoints(ctx, true, trig.ID); err != nil {
					return err
				}
			}
		}
		for _, other := range s.model.Breakpoints(BreakpointFilter{TriggeredOnly: true}) {
			if other.TriggeredBy == bp.ID && other.Enabled != enable {
				if err := s.EnableOrDisableBreakpoints(ctx, enable, other.ID); err != nil {
					return err
				}
			}
		}
		return s.sendBreakpoints(ctx, bp.Path, false, nil)
	}
	for _, fb := range s.model.FunctionBreakpoints() {
		if fb.ID == id {
			return s.sendFunctionBreakpoints(ctx, nil)
		}
	}
	return s.sendExceptionBreakpoints(ctx, nil)
}

// SetBreakpointsActivated turns all breakpoints on or off without changing them ("Toggle Activate Breakpoints").
func (s *Service) SetBreakpointsActivated(ctx context.Context, on bool) error {
	s.model.setActivated(on)
	return s.sendAllToSessions(ctx)
}

// AddFunctionBreakpoint adds a function breakpoint. With an empty name it only adds the row the user is about to name; the name
// is sent by UpdateFunctionBreakpoint.
func (s *Service) AddFunctionBreakpoint(ctx context.Context, opts FunctionBreakpointOptions) (FunctionBreakpoint, error) {
	fb := s.model.addFunctionBreakpoint(opts, "")
	if opts.Name == "" {
		return fb, nil
	}
	return fb, s.sendFunctionBreakpoints(ctx, nil)
}

// UpdateFunctionBreakpoint changes the name, condition or hit condition of a function breakpoint (nil leaves one as it is).
func (s *Service) UpdateFunctionBreakpoint(ctx context.Context, id string, name, condition, hitCondition *string) error {
	s.model.updateFunctionBreakpoint(id, name, condition, hitCondition)
	return s.sendFunctionBreakpoints(ctx, nil)
}

// RemoveFunctionBreakpoints removes the function breakpoint with an id, or all of them.
func (s *Service) RemoveFunctionBreakpoints(ctx context.Context, id string) error {
	s.model.removeFunctionBreakpoints(id)
	return s.sendFunctionBreakpoints(ctx, nil)
}

// SetExceptionBreakpointCondition sets the condition of an exception filter.
func (s *Service) SetExceptionBreakpointCondition(ctx context.Context, id, condition string) error {
	s.model.setExceptionCondition(id, condition)
	return s.sendExceptionBreakpoints(ctx, nil)
}

// sendToSessions runs send for one session, or for each session that has an adapter; the first failure is returned after all ran.
func (s *Service) sendToSessions(only *Session, send func(*Session) error) error {
	var targets []*Session
	if only != nil {
		targets = []*Session{only}
	} else {
		for _, x := range s.model.Sessions(false) {
			if x.rawClient() != nil {
				targets = append(targets, x)
			}
		}
	}
	var first error
	for _, x := range targets {
		if err := send(x); err != nil && first == nil {
			first = err
		}
	}
	return first
}

// sendBreakpoints sends the breakpoints of a file to a session, or to all.
func (s *Service) sendBreakpoints(ctx context.Context, path string, sourceModified bool, only *Session) error {
	bps := s.model.Breakpoints(BreakpointFilter{OriginalPath: path, EnabledOnly: true})
	return s.sendToSessions(only, func(x *Session) error {
		if x.Configuration().NoDebug() {
			return nil
		}
		send := bps[:0:0]
		for _, b := range bps {
			if b.TriggeredBy == "" || s.triggeredIn(b.ID, x.id) {
				send = append(send, b)
			}
		}
		return x.sendBreakpoints(ctx, path, send, sourceModified)
	})
}

func (s *Service) triggeredIn(id, sessionID string) bool {
	s.model.mu.Lock()
	defer s.model.mu.Unlock()
	for _, b := range s.model.lines {
		if b.id == id {
			return b.triggered[sessionID]
		}
	}
	return false
}

func (s *Service) sendFunctionBreakpoints(ctx context.Context, only *Session) error {
	var fbs []FunctionBreakpoint
	activated := s.model.BreakpointsActivated()
	for _, fb := range s.model.FunctionBreakpoints() {
		if fb.Enabled && activated {
			fbs = append(fbs, fb)
		}
	}
	return s.sendToSessions(only, func(x *Session) error {
		if !x.Capabilities().SupportsFunctionBreakpoints || x.Configuration().NoDebug() {
			return nil
		}
		return x.sendFunctionBreakpoints(ctx, fbs)
	})
}

func (s *Service) sendExceptionBreakpoints(ctx context.Context, only *Session) error {
	return s.sendToSessions(only, func(x *Session) error {
		var enabled []ExceptionBreakpoint
		for _, e := range s.model.ExceptionBreakpoints(x.id) {
			if e.Enabled {
				enabled = append(enabled, e)
			}
		}
		caps := x.Capabilities()
		if caps.SupportsConfigurationDoneRequest && len(caps.ExceptionBreakpointFilters) == 0 {
			return nil // only setExceptionBreakpoints for adapters that have filters (DAP issue 90001)
		}
		if x.Configuration().NoDebug() {
			return nil
		}
		return x.sendExceptionBreakpoints(ctx, enabled)
	})
}

// sendAllToSessions sends every breakpoint to every session.
func (s *Service) sendAllToSessions(ctx context.Context) error {
	return s.sendToSessions(nil, func(x *Session) error { return s.sendAllBreakpoints(ctx, x) })
}

// sendAllBreakpoints sends the breakpoints of every file, then the function breakpoints and the exception filters (last, since some
// adapters rely on the order), to a session. The files go one after the other in the order of the model, so the order on the wire is
// fixed (VS Code sends them together).
//
// Ported from VS Code: src/vs/workbench/contrib/debug/browser/debugService.ts (sendAllBreakpoints)
func (s *Service) sendAllBreakpoints(ctx context.Context, sess *Session) error {
	var first error
	note := func(err error) {
		if err != nil && first == nil {
			first = err
		}
	}
	seen := map[string]bool{}
	for _, b := range s.model.Breakpoints(BreakpointFilter{}) {
		if seen[b.Path] {
			continue
		}
		seen[b.Path] = true
		note(s.sendBreakpoints(ctx, b.Path, false, sess))
	}
	note(s.sendFunctionBreakpoints(ctx, sess))
	note(s.sendExceptionBreakpoints(ctx, sess))
	return first
}

// SetExceptionBreakpointFallbackSession remembers the filters of a session to show when none runs.
func (s *Service) SetExceptionBreakpointFallbackSession(sessionID string) {
	s.model.setExceptionFallbackSession(sessionID)
}

// inputMemory is the memory of the inputs' last values: the window's, else the service's own.
func (s *Service) inputMemory() InputMemory {
	if s.opts.InputMemory != nil {
		return s.opts.InputMemory
	}
	return s.inputs
}
