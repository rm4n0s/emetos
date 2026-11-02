package extension

import (
	"context"
	"encoding/json"
	"regexp"
	"strings"

	"github.com/rm4n0s/errors"
)

// The Debug API (SPEC §10.31 "The extension contract", step 25.3; after the debug namespace of vscode.d.ts, extHostDebugService.ts and the
// contributes.debuggers of debugger.ts). A language extension contributes a debugger: the manifest says which debug types it has and
// the attributes of their configurations (Contributes.Debuggers), and Activate registers the code that finds the debug adapter of a
// session (RegisterAdapterDescriptorFactory) and, if it wants, completes the configurations (RegisterConfigurationProvider) and
// watches the adapter's messages (RegisterAdapterTrackerFactory). An extension may also start and stop sessions and follow them.
//
// Where a comment does not say otherwise a method may be called from any goroutine. The functions the extension gives (factories,
// providers, trackers) are called by the host on a goroutine of its own, never on the UI thread, with a context that ends when the
// work is no longer wanted; they must not touch the widgets (use Post). A panic in one deactivates the extension
// (ExtensionDebugPanicked). The listeners (OnDid...) run on the UI thread.

// ErrDebugOpenLaunchJSON is what a Resolve function returns to cancel the launch and have launch.json shown (VS Code's null result). It
// has the tag of the host's own sentinel, so that errors.Is finds it.
var ErrDebugOpenLaunchJSON = &errors.Error{Tag: "DebugOpenLaunchFile"}

// DebugConfiguration is a launch configuration: the JSON object of launch.json (name, type, request and the attributes of the
// debugger). Its values are those of encoding/json: string, float64, bool, nil, []any and map[string]any.
type DebugConfiguration map[string]any

// DebuggerSpec declares a debugger (VS Code's contributes.debuggers). A configuration with Type is started by this debugger.
type DebuggerSpec struct {
	// Type is the debug type of the configurations ("go", "debugpy"): letters, digits, dots, hyphens and underscores.
	Type string
	// Label is the name shown to the user ("Go"); the type when empty.
	Label string
	// Languages are the ids of the languages the debugger serves: F5 with no launch.json asks the debuggers of the language of the
	// active editor for a configuration.
	Languages []string
	// Program, Args, Runtime and RuntimeArgs are the default adapter: a program that speaks DAP on its standard input and output
	// (VS Code's program and args; with Runtime the program is the first argument of the runtime). The descriptor factory gets it as
	// its default and may return another; without a factory it is the adapter.
	Program     string
	Args        []string
	Runtime     string
	RuntimeArgs []string
	// ConfigurationAttributes are the JSON schemas of the attributes of a configuration, by request ("launch" and "attach"): an
	// object schema with properties, required, additionalProperties and the rest of the subset Emetos checks (internal/core/debug
	// CheckAttributes). launch.json is checked against them and completes from them.
	ConfigurationAttributes map[string]json.RawMessage
	// InitialConfigurations are the configurations a new launch.json starts with, before those the configuration providers of
	// trigger DebugTriggerInitial give.
	InitialConfigurations []DebugConfiguration
	// ConfigurationSnippets are the configurations "Add Configuration" and the completion of launch.json offer; their strings may use
	// the snippet syntax.
	ConfigurationSnippets []DebugSnippet
	// Variables maps the names of interactive variables to the ids of commands: ${command:pickProcess} runs the command.
	Variables map[string]string
	// DynamicConfigurations says that the debugger has configuration providers of trigger DebugTriggerDynamic: the dropdown of the Run
	// and Debug view lists the type, and activates the extension (OnDebug) to ask for the configurations (VS Code's
	// onDebugDynamicConfigurations activation event).
	DynamicConfigurations bool
}

// DebugSnippet is a configuration the editor offers to add.
type DebugSnippet struct {
	// Label is the name of the snippet and Description what it is for.
	Label, Description string
	// Body is the configuration; it needs a name, a type and a request.
	Body DebugConfiguration
}

// BreakpointSpec declares a language where breakpoints can be set (VS Code's contributes.breakpoints). debug.allowBreakpointsEverywhere
// lifts the limit.
type BreakpointSpec struct {
	// Language is the id of the language.
	Language string
}

// debugTypePattern is the form of a debug type.
var debugTypePattern = regexp.MustCompile(`^[A-Za-z0-9_.-]+$`)

// checkDebuggers checks the debuggers and breakpoint languages a manifest declares: every type is valid and new, the attribute schemas
// are JSON objects of the requests launch and attach, the initial configurations and the snippets have what they need to start, and a
// variable names a command.
func checkDebuggers(m Manifest) error {
	seen := map[string]bool{}
	for _, d := range m.Contributes.Debuggers {
		switch {
		case !debugTypePattern.MatchString(d.Type):
			return errors.New("ExtensionDebuggerInvalid", "the type of a debugger is letters, digits, dots, hyphens and underscores", "id", m.ID, "type", d.Type)
		case seen[d.Type]:
			return errors.New("ExtensionDebuggerDuplicate", "two debuggers of the extension have the same type", "id", m.ID, "type", d.Type)
		case d.Runtime != "" && d.Program == "":
			return errors.New("ExtensionDebuggerInvalid", "a debugger with a runtime needs the program the runtime runs", "id", m.ID, "type", d.Type)
		}
		seen[d.Type] = true
		for req, raw := range d.ConfigurationAttributes {
			var schema map[string]any
			switch {
			case req != "launch" && req != "attach":
				return errors.New("ExtensionDebuggerInvalid", "the attributes of a debugger are given for the requests launch and attach", "id", m.ID, "type", d.Type, "request", req)
			case json.Unmarshal(raw, &schema) != nil || schema == nil:
				return errors.New("ExtensionDebuggerInvalid", "the attributes of a request are a JSON schema, an object", "id", m.ID, "type", d.Type, "request", req)
			}
		}
		for i, c := range d.InitialConfigurations {
			if err := checkDebugConfiguration(m.ID, d.Type, "initial configuration", i, c); err != nil {
				return err
			}
		}
		for i, s := range d.ConfigurationSnippets {
			if strings.TrimSpace(s.Label) == "" {
				return errors.New("ExtensionDebuggerInvalid", "a configuration snippet needs a label", "id", m.ID, "type", d.Type, "snippet", i)
			}
			if err := checkDebugConfiguration(m.ID, d.Type, "configuration snippet", i, s.Body); err != nil {
				return err
			}
		}
		for name, cmd := range d.Variables {
			if name == "" || cmd == "" {
				return errors.New("ExtensionDebuggerInvalid", "a variable of a debugger names a command", "id", m.ID, "type", d.Type, "variable", name)
			}
		}
	}
	langs := map[string]bool{}
	for _, b := range m.Contributes.Breakpoints {
		switch {
		case strings.TrimSpace(b.Language) == "":
			return errors.New("ExtensionBreakpointInvalid", "a breakpoints entry names a language", "id", m.ID)
		case langs[b.Language]:
			return errors.New("ExtensionBreakpointInvalid", "a language is listed twice in the breakpoints of the extension", "id", m.ID, "language", b.Language)
		}
		langs[b.Language] = true
	}
	return nil
}

// checkDebugConfiguration checks a configuration of a debugger that starts one: a name, a request of launch or attach, and a type that
// is the debugger's or none.
func checkDebugConfiguration(extID, debugType, what string, i int, c DebugConfiguration) error {
	name, _ := c["name"].(string)
	req, _ := c["request"].(string)
	typ, hasType := c["type"].(string)
	switch {
	case strings.TrimSpace(name) == "":
		return errors.New("ExtensionDebuggerInvalid", "an "+what+" of a debugger needs a name", "id", extID, "type", debugType, "index", i)
	case req != "launch" && req != "attach":
		return errors.New("ExtensionDebuggerInvalid", "the request of an "+what+" is launch or attach", "id", extID, "type", debugType, "index", i, "request", req)
	case c["type"] != nil && (!hasType || typ != debugType):
		return errors.New("ExtensionDebuggerInvalid", "the type of an "+what+" is that of its debugger", "id", extID, "type", debugType, "index", i)
	}
	return nil
}

// DebugSession is a debug session (VS Code's DebugSession). The value stays usable after the session ended, but its requests fail.
type DebugSession interface {
	// ID is the id of the session, unique in the window tab.
	ID() string
	// Type is the debug type and Name the name of the session (the configuration's name, made unique among the sessions).
	Type() string
	Name() string
	// Folder is the folder of the launch.json the configuration comes from; empty for a configuration given whole.
	Folder() string
	// Configuration is the configuration the session runs: the resolved one, with its variables substituted.
	Configuration() DebugConfiguration
	// Parent is the session that started this one (the adapter's startDebugging), or nil.
	Parent() DebugSession
	// CustomRequest sends a request the protocol does not define to the adapter and returns the body of its response (VS Code's
	// customRequest). It fails with the host's DebugNoAdapter when the session has no adapter any more.
	CustomRequest(ctx context.Context, command string, args any) (json.RawMessage, error)
}

// DebugAdapterKind says how an adapter is reached.
type DebugAdapterKind int

// The kinds of adapter.
const (
	// DebugAdapterExecutable is a program that speaks DAP on its standard input and output (VS Code's DebugAdapterExecutable).
	DebugAdapterExecutable DebugAdapterKind = iota + 1
	// DebugAdapterServer is a server that listens on a TCP port already (VS Code's DebugAdapterServer).
	DebugAdapterServer
	// DebugAdapterPipeServer is a server that listens on a Unix socket already (VS Code's DebugAdapterNamedPipeServer).
	DebugAdapterPipeServer
	// DebugAdapterExecutableServer is a program that listens on a port and prints it: Emetos starts it, reads the port and connects
	// (delve's `dlv dap`, js-debug).
	DebugAdapterExecutableServer
)

// DebugAdapter says where a debug adapter is and how to reach it (VS Code's DebugAdapterDescriptor); the members of its Kind are used.
type DebugAdapter struct {
	// Kind is how the adapter is reached.
	Kind DebugAdapterKind
	// Command, Args, Env and Cwd start the program of DebugAdapterExecutable and DebugAdapterExecutableServer; Command is found in PATH
	// unless it has a slash, Env are added to the environment of Emetos and an empty Cwd is the folder of Emetos.
	Command string
	Args    []string
	Env     map[string]string
	Cwd     string
	// PortPattern finds the port in a line the program prints (DebugAdapterExecutableServer): a regular expression with the named
	// group "port" and, optionally, "host". Empty matches "listening at: 127.0.0.1:4711" and "listening on port 4711".
	PortPattern string
	// Host and Port are the address of DebugAdapterServer; an empty Host is 127.0.0.1.
	Host string
	Port int
	// Path is the Unix socket of DebugAdapterPipeServer.
	Path string
	// ConsoleOutput makes the lines the program writes to its standard error, and for DebugAdapterExecutableServer to its standard output
	// too, lines of the Debug Console of the session (categories stderr and stdout; the line that gives the port is left out). It is for an
	// adapter that runs the debuggee as its child and leaves the output of the debuggee to its own streams (Delve); the lines go to the log
	// of Emetos as well.
	ConsoleOutput bool
	// ServesChildren (DebugAdapterExecutableServer) says that the server program serves the sessions its adapter starts with
	// startDebugging as well: a child session connects to the address of the parent's server, and the factory is not asked for it
	// (js-debug's children are targets of the parent's process, which finds them by the __pendingTargetId of their configuration; VS
	// Code's extension gives them the parent's server the same way). The server ends with the session that started it.
	ServesChildren bool
}

// DebugAdapterFactory finds the adapter of a session (VS Code's DebugAdapterDescriptorFactory.createDebugAdapterDescriptor). executable
// is the adapter the manifest names (Program, Args, Runtime), or nil. Returning nil, nil falls back to executable; with none the session
// fails. It runs once for each session, when its adapter is needed, with the session's context.
type DebugAdapterFactory func(ctx context.Context, session DebugSession, executable *DebugAdapter) (*DebugAdapter, error)

// DebugProviderTrigger says when the configurations a provider gives are asked for.
type DebugProviderTrigger int

const (
	// DebugTriggerInitial is asked for when launch.json is created (VS Code's DebugConfigurationProviderTriggerKind.Initial).
	DebugTriggerInitial DebugProviderTrigger = iota
	// DebugTriggerDynamic is asked for when the configurations of the dropdown are listed and F5 is pressed with no launch.json (Dynamic).
	DebugTriggerDynamic
)

// DebugConfigurationProvider completes the configurations of a debug type (VS Code's DebugConfigurationProvider). The providers of a
// type are called in the order they were registered, each with the result of the one before; those of the type "*" come after the
// ones of the type. A nil function is not called. The folder is the folder of the launch.json, empty for none.
type DebugConfigurationProvider struct {
	// Provide gives the configurations to put into a new launch.json (trigger DebugTriggerInitial) or to list in the dropdown
	// (DebugTriggerDynamic).
	Provide func(ctx context.Context, folder string) ([]DebugConfiguration, error)
	// Resolve is called before the variables of a configuration are substituted, and may fill in what is missing (the program of a
	// "Debug current file"). Returning nil, nil cancels the launch silently, and nil, ErrDebugOpenLaunchJSON cancels it and shows
	// launch.json.
	Resolve func(ctx context.Context, folder string, cfg DebugConfiguration) (DebugConfiguration, error)
	// ResolveSubstituted is called after the variables are substituted, and after the task before the session ran; nil, nil cancels.
	ResolveSubstituted func(ctx context.Context, folder string, cfg DebugConfiguration) (DebugConfiguration, error)
}

// DebugAdapterTracker watches the messages between Emetos and an adapter (VS Code's DebugAdapterTracker). A nil function is not called.
// The functions run on the goroutine that reads or writes the adapter's stream, one at a time and in the order of the wire: keep them
// short and do not make a request of the session from them.
type DebugAdapterTracker struct {
	// OnWillStartSession is called before the adapter is reached.
	OnWillStartSession func()
	// OnWillReceiveMessage gets each message Emetos sends to the adapter, as the JSON of the protocol message.
	OnWillReceiveMessage func(message json.RawMessage)
	// OnDidSendMessage gets each message the adapter sends to Emetos.
	OnDidSendMessage func(message json.RawMessage)
	// OnWillStopSession is called when the session ends.
	OnWillStopSession func()
	// OnError is called with the error that ended the session: the adapter could not be reached or died.
	OnError func(err error)
	// OnExit is called when the adapter's program exited, with its exit status (nil when it is not known).
	OnExit func(code *int)
}

// DebugAdapterTrackerFactory makes the tracker of a session (VS Code's DebugAdapterTrackerFactory). A nil tracker watches nothing.
type DebugAdapterTrackerFactory func(ctx context.Context, session DebugSession) (*DebugAdapterTracker, error)

// DebugTarget says what to start: the configuration or compound called Name in the launch.json of the folder, or Config given whole.
type DebugTarget struct {
	Name   string
	Config DebugConfiguration
}

// DebugConsoleMode says whether a child session has a Debug Console of its own.
type DebugConsoleMode int

const (
	// DebugConsoleSeparate sends the session's output to its own console.
	DebugConsoleSeparate DebugConsoleMode = iota
	// DebugConsoleMergeWithParent sends it to the console of the parent session.
	DebugConsoleMergeWithParent
)

// DebugStartOptions are the options of StartDebugging (VS Code's DebugSessionOptions).
type DebugStartOptions struct {
	// Parent makes the session a child of another, shown under it in the Call Stack.
	Parent DebugSession
	// LifecycleManagedByParent makes the child's stop, disconnect and restart act on the parent.
	LifecycleManagedByParent bool
	// Compact shows the child's threads under the parent's session, without a row of its own.
	Compact bool
	// ConsoleMode says which Debug Console the output goes to.
	ConsoleMode DebugConsoleMode
	// NoDebug runs the program without debugging.
	NoDebug bool
	// SuppressSaveBeforeStart does not save the files before the session starts.
	SuppressSaveBeforeStart bool
	// SuppressDebugToolbar, SuppressDebugStatusbar and SuppressDebugView hide those parts of the workbench for the session.
	SuppressDebugToolbar, SuppressDebugStatusbar, SuppressDebugView bool
}

// DebugSessionCustomEvent is an event the adapter sent that the protocol does not define (VS Code's DebugSessionCustomEvent).
type DebugSessionCustomEvent struct {
	// Session is the session whose adapter sent it, Event the name of the event and Body its body as JSON.
	Session DebugSession
	Event   string
	Body    json.RawMessage
}

// DebugBreakpoint is a breakpoint (VS Code's SourceBreakpoint and FunctionBreakpoint). A breakpoint with a Function is a function
// breakpoint; the others are in a line of a file.
type DebugBreakpoint struct {
	// ID identifies the breakpoint; Breakpoints() fills it and RemoveBreakpoints looks the breakpoint up by it, or by its place.
	ID string
	// URI is the file (file:///path) and Position the place in it, zero-based: with a column the breakpoint is inline.
	URI      string
	Position Position
	// Function is the name of the function of a function breakpoint.
	Function string
	// Disabled says that the breakpoint does not stop the program.
	Disabled bool
	// Condition, HitCondition and LogMessage are the expressions of a conditional breakpoint, a hit count and a logpoint.
	Condition, HitCondition, LogMessage string
}

// DebugBreakpointsChange is what OnDidChangeBreakpoints reports (VS Code's BreakpointsChangeEvent).
type DebugBreakpointsChange struct {
	Added, Removed, Changed []DebugBreakpoint
}

// DebugConsole writes to the Debug Console (VS Code's debug.activeDebugConsole).
type DebugConsole interface {
	// Append writes the text as it is, AppendLine adds a line feed.
	Append(text string)
	AppendLine(text string)
}

// Debug is the Debug API of an extension in one scope.
type Debug interface {
	// RegisterAdapterDescriptorFactory registers the code that finds the adapter of the sessions of the debug type. A type has one
	// factory: it fails with ExtensionDebugFactoryTaken when another extension has one, ExtensionDebugInvalid for an empty type or
	// no function, ExtensionDebugUnavailable in a scope with no debug service (a host without a workbench) and ExtensionDeactivated
	// after the activation ended. The function it returns removes the factory; the activation's end does it too.
	RegisterAdapterDescriptorFactory(debugType string, factory DebugAdapterFactory) (dispose func(), err error)
	// RegisterConfigurationProvider registers a provider for the debug type, or for every type with "*". It fails like
	// RegisterAdapterDescriptorFactory, except that a type may have several providers.
	RegisterConfigurationProvider(debugType string, provider DebugConfigurationProvider, trigger DebugProviderTrigger) (dispose func(), err error)
	// RegisterAdapterTrackerFactory registers a tracker factory for the debug type, or for every type with "*": every session of the
	// type gets a tracker. It fails like RegisterConfigurationProvider.
	RegisterAdapterTrackerFactory(debugType string, factory DebugAdapterTrackerFactory) (dispose func(), err error)
	// StartDebugging starts the session of the configuration or compound in the target, in the launch.json of the folder (empty: the
	// first folder that has the name), and returns when its adapter has been started and has answered launch or attach. It returns
	// false with no error when the launch was cancelled (a provider, an input, a failed task). The host calls the providers of the
	// extensions on other goroutines, so call it from a goroutine of the extension and not from a command handler, which holds the UI
	// thread.
	StartDebugging(ctx context.Context, folder string, target DebugTarget, opts DebugStartOptions) (bool, error)
	// StopDebugging stops the session; nil stops all of them.
	StopDebugging(ctx context.Context, session DebugSession) error
	// ActiveSession is the session the Call Stack has focused, or nil. Sessions lists the sessions that run, the oldest first.
	ActiveSession() DebugSession
	Sessions() []DebugSession
	// OnDidStartSession, OnDidTerminateSession and OnDidChangeActiveSession (nil when none is active) and OnDidReceiveCustomEvent call
	// fn on the UI thread; the function they return removes the listener.
	OnDidStartSession(fn func(DebugSession)) (remove func())
	OnDidTerminateSession(fn func(DebugSession)) (remove func())
	OnDidChangeActiveSession(fn func(DebugSession)) (remove func())
	OnDidReceiveCustomEvent(fn func(DebugSessionCustomEvent)) (remove func())
	// Breakpoints lists the breakpoints. AddBreakpoints adds the ones that are not there and RemoveBreakpoints removes those with the
	// ID, or the place, of the given ones. OnDidChangeBreakpoints reports every change.
	Breakpoints() []DebugBreakpoint
	AddBreakpoints(ctx context.Context, breakpoints []DebugBreakpoint) error
	RemoveBreakpoints(ctx context.Context, breakpoints []DebugBreakpoint) error
	OnDidChangeBreakpoints(fn func(DebugBreakpointsChange)) (remove func())
	// Console is the Debug Console.
	Console() DebugConsole
}
