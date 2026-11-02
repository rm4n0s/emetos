package python

import (
	"context"
	_ "embed"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/rm4n0s/errors"

	"github.com/rm4n0s/emetos/extension"
	"github.com/rm4n0s/emetos/extensions/internal/langserver"
)

// Debugging Python programs with debugpy (SPEC §10.31 "The debuggers", Python; step 25.8). The extension contributes the debugger of
// type "debugpy" (and "python", its old name): its attributes (those of the Python Debugger of VS Code), the configuration snippets, the
// configuration providers (debugconfig.go) and the adapter descriptor, `<interpreter> -m debugpy.adapter` started as an executable
// that speaks DAP on its standard input and output. A missing debugpy is told to the user with a button that installs it in a terminal.

// Ported from vscode-python-debugger: src/extension/debugger/adapter/factory.ts (createDebugAdapterDescriptor) and the debugger entry of
// package.json (the attributes).

// The ids of the debugger and its settings.
const (
	// DebugType is the debug type of the debugger, and DebugTypeOld the name it had in the Python extension of VS Code: a launch.json
	// that says "python" is served by the same debugger.
	DebugType    = "debugpy"
	DebugTypeOld = "python"
	// EnvFileSetting is the file of environment variables a launch reads when its configuration names none (VS Code's python.envFile).
	EnvFileSetting = "python.envFile"
	// DebugJustMyCodeSetting is the default of justMyCode (VS Code's debugpy.debugJustMyCode).
	DebugJustMyCodeSetting = "debugpy.debugJustMyCode"
)

// debugAttributes are the attributes of the launch and attach requests: the debugger entry of the package.json of the Python Debugger,
// written to debug-attributes.json by scripts/update-assets.sh.
//
//go:embed debug-attributes.json
var debugAttributes []byte

// attributeSchemas reads the embedded attributes: the JSON schema of each request.
func attributeSchemas() map[string]json.RawMessage {
	var out map[string]json.RawMessage
	if err := json.Unmarshal(debugAttributes, &out); err != nil {
		return nil
	}
	return out
}

// debuggerSpecs declare the two types. Only debugpy serves the language and has dynamic configurations, so that F5 with no launch.json
// has one debugger to choose; the old name is for the launch.json files that use it.
func debuggerSpecs() []extension.DebuggerSpec {
	snippets := func() []extension.DebugSnippet {
		var list []extension.DebugSnippet
		for _, c := range configChoices() {
			body := c.config()
			body["name"] = "${1:" + body["name"].(string) + "}"
			list = append(list, extension.DebugSnippet{Label: "Python Debugger: " + c.label, Description: c.description, Body: body})
		}
		return list
	}
	return []extension.DebuggerSpec{
		{
			Type: DebugType, Label: "Python Debugger", Languages: []string{"python"}, ConfigurationAttributes: attributeSchemas(),
			ConfigurationSnippets: snippets(), DynamicConfigurations: true,
		},
		{
			Type: DebugTypeOld, Label: "Python (the old name of Python Debugger)", ConfigurationAttributes: attributeSchemas(),
			ConfigurationSnippets: nil,
		},
	}
}

// debugSettingSpecs are the settings the debugger reads.
func debugSettingSpecs() []extension.SettingSpec {
	return []extension.SettingSpec{
		{
			Key: EnvFileSetting, Type: "string", Default: "${workspaceFolder}/.env", Category: categoryPython,
			Description: "The file of environment variables (`NAME=value` lines, `#` comments) that a debug session reads when its configuration has no `envFile`. A file that is not there is ignored.",
		},
		{
			Key: DebugJustMyCodeSetting, Type: "boolean", Default: true, Category: categoryPython,
			Description: "When debugging, only the code you wrote is debugged: library code is stepped over and does not stop the program. This is the default of `justMyCode` of the debug configurations that do not set it.",
		},
	}
}

// registerDebug registers the configuration providers and the descriptor factory of the two types. The registrations end with the
// activation. A host with no debug service has none: the extension then has no debugger.
func (p *python) registerDebug() {
	d := p.api.Debug()
	for _, typ := range []string{DebugType, DebugTypeOld} {
		initial := extension.DebugConfigurationProvider{Provide: p.provideDebugConfigurations, Resolve: p.resolveDebug, ResolveSubstituted: p.resolveDebugSubstituted}
		if _, err := d.RegisterConfigurationProvider(typ, initial, extension.DebugTriggerInitial); err != nil {
			p.api.Log().Debug("the Python debugger has no configuration provider", "err", err)
			return
		}
		if typ == DebugType {
			dynamic := extension.DebugConfigurationProvider{Provide: p.provideDynamicConfigurations}
			if _, err := d.RegisterConfigurationProvider(typ, dynamic, extension.DebugTriggerDynamic); err != nil {
				p.api.Log().Debug("the Python debugger has no dynamic configurations", "err", err)
			}
		}
		if _, err := d.RegisterAdapterDescriptorFactory(typ, p.debugAdapter); err != nil {
			p.api.Log().Debug("the Python debugger has no adapter", "err", err)
		}
	}
}

// debugAdapter is the descriptor factory (createDebugAdapterDescriptor). An attach with connect (or the legacy host and port) goes to
// the server that is running already: a debuggee started with `python -m debugpy --listen`. Every other session starts the adapter,
// `<interpreter> -m debugpy.adapter` (or `<interpreter> <debugAdapterPath>` for another adapter), after it was checked that the
// interpreter has debugpy. It may run on any goroutine.
func (p *python) debugAdapter(ctx context.Context, session extension.DebugSession, _ *extension.DebugAdapter) (*extension.DebugAdapter, error) {
	cfg := session.Configuration()
	if cfg["request"] == "attach" {
		if connect, ok := cfg["connect"].(map[string]any); ok {
			port, _ := portOf(connect["port"])
			if port == 0 {
				return nil, errors.New("PythonDebugAttachInvalid", "\"connect\" needs a \"port\"", "session", session.ID())
			}
			host, _ := connect["host"].(string)
			return &extension.DebugAdapter{Kind: extension.DebugAdapterServer, Host: hostOrLoopback(host), Port: port}, nil
		}
		if port, ok := portOf(cfg["port"]); ok && port > 0 {
			host, _ := cfg["host"].(string)
			return &extension.DebugAdapter{Kind: extension.DebugAdapterServer, Host: hostOrLoopback(host), Port: port}, nil
		}
		if !isSet(cfg, "listen") && !isSet(cfg, "processId") {
			return nil, errors.New("PythonDebugAttachInvalid", "\"request\": \"attach\" requires either \"connect\", \"listen\", or \"processId\"", "session", session.ID())
		}
	}
	python, _ := cfg["debugAdapterPython"].(string)
	if python == "" {
		python, _ = cfg["python"].(string)
	}
	if python == "" {
		var err error
		if python, err = p.interpreterInUse(); err != nil {
			return nil, err
		}
	}
	var args []string
	if path, _ := cfg["debugAdapterPath"].(string); path != "" {
		args = []string{path}
	} else {
		if err := p.checkDebugpy(ctx, python); err != nil {
			return nil, err
		}
		args = []string{"-m", "debugpy.adapter"}
	}
	if logToFile, _ := cfg["logToFile"].(bool); logToFile {
		if dir := debugpyLogDir(); dir != "" {
			args = append(args, "--log-dir", dir)
		}
	}
	// the adapter does not run in the folder of the project: its modules would come before debugpy's (python -m puts the working
	// folder first on the path). The program has its own cwd, which the configuration carries.
	return &extension.DebugAdapter{Kind: extension.DebugAdapterExecutable, Command: python, Args: args, Cwd: filepath.Dir(python)}, nil
}

// hostOrLoopback is the host of an attach: the name given, else this machine.
func hostOrLoopback(host string) string {
	if strings.TrimSpace(host) == "" {
		return "127.0.0.1"
	}
	return strings.TrimSpace(host)
}

// portOf reads a port given as a number or as a string of digits.
func portOf(v any) (int, bool) {
	switch n := v.(type) {
	case float64:
		return int(n), n == float64(int(n))
	case int:
		return n, true
	case string:
		return portNumber(n)
	}
	return 0, false
}

// debugpyLogDir is the folder the adapter writes its log to when a configuration says logToFile: below the cache folder of Emetos. It
// is "" when the folder cannot be made.
func debugpyLogDir() string {
	cache, err := os.UserCacheDir()
	if err != nil {
		return ""
	}
	dir := filepath.Join(cache, "emetos", "debugpy")
	if os.MkdirAll(dir, 0o700) != nil {
		return ""
	}
	return dir
}

// checkTimeout ends the interpreter when it does not answer the question whether it has debugpy.
const checkTimeout = 20 * time.Second

// checkDebugpy asks the interpreter whether it can import debugpy. When it cannot, the user is told, with a button that installs it in a
// terminal, and the session is cancelled with PythonDebugpyMissing. An interpreter that cannot be run fails with
// PythonDebugInterpreterFailed.
func (p *python) checkDebugpy(ctx context.Context, python string) error {
	ctx, cancel := context.WithTimeout(ctx, checkTimeout)
	defer cancel()
	_, err := langserver.QueryVersion(ctx, python, langserver.VersionCheck{Args: []string{"-c", "import debugpy; print(debugpy.__version__)"}}, filepath.Dir(python), nil)
	if err == nil {
		return nil
	}
	reason := err.Error()
	if appErr, ok := errors.FromError(err); ok {
		reason = appErr.Message
		if out, _ := appErr.Metadata["output"].(string); strings.Contains(out, "debugpy") {
			p.promptMissingDebugpy(python)
			return errors.New("PythonDebugpyMissing", "debugpy is not installed in "+python+": "+out, "python", python)
		}
	}
	return errors.New("PythonDebugInterpreterFailed", "the interpreter cannot be run: "+reason, "python", python)
}

// promptMissingDebugpy tells the user that debugpy is missing from the interpreter, with a button that installs it. It tells again each
// time a session is started without it. It may be called from any goroutine.
func (p *python) promptMissingDebugpy(python string) {
	p.api.Notifications().Warn("debugpy was not found in "+python+". It debugs Python programs.",
		extension.NotificationAction{Label: "Install", Run: func() { p.installDebugpy(python) }})
}

// pipInstallArgs is the command that installs debugpy for the interpreter: into the user's packages, unless the interpreter belongs to an
// environment, where --user is refused and the environment is the place.
func pipInstallArgs(python string) []string {
	args := []string{python, "-m", "pip", "install"}
	if !inEnvironment(python) {
		args = append(args, "--user")
	}
	return append(args, "debugpy")
}

// inEnvironment tells whether the interpreter is that of a virtual environment or a conda environment.
func inEnvironment(python string) bool {
	prefix := filepath.Dir(filepath.Dir(python))
	if readPyvenvCfg(prefix) != nil {
		return true
	}
	info, err := os.Stat(filepath.Join(prefix, "conda-meta"))
	return err == nil && info.IsDir()
}

// The requests of the Terminal extension (SPEC §10.25 "Events and requests"). The topics are written here, not imported, so that the
// extensions stay apart.
const (
	terminalCreate  = "emetos.terminal.create"
	terminalRun     = "emetos.terminal.run"
	terminalTimeout = 10 * time.Second
)

// installDebugpy opens a terminal and types the pip command in it, so that the user sees it run (D40). When the Terminal extension is
// off or fails, the user is told and the command line is in the message. UI thread.
func (p *python) installDebugpy(python string) {
	line := shellJoin(pipInstallArgs(python))
	fail := func(why string) {
		p.api.Notifications().Error("Cannot open a terminal: " + why + ". Run this command yourself: " + line)
	}
	ctx, cancel := context.WithTimeout(p.ctx, terminalTimeout)
	err := p.api.Events().Request(ctx, terminalCreate, map[string]any{"name": "Install debugpy", "show": true}, func(ev extension.Event, err error) {
		cancel()
		if err != nil {
			fail(err.Error())
			return
		}
		reply, err := extension.EventValue[struct {
			ID int `json:"id"`
		}](ev)
		if err != nil {
			fail(err.Error())
			return
		}
		ctx2, cancel2 := context.WithTimeout(p.ctx, terminalTimeout)
		err = p.api.Events().Request(ctx2, terminalRun, map[string]any{"id": reply.ID, "text": line}, func(_ extension.Event, err error) {
			cancel2()
			if err != nil {
				fail(err.Error())
			}
		})
		if err != nil {
			cancel2()
			fail(err.Error())
		}
	})
	if err != nil {
		cancel()
		fail(err.Error())
	}
}

// shellJoin quotes the words for a POSIX shell and joins them.
func shellJoin(words []string) string {
	quoted := make([]string, len(words))
	for i, w := range words {
		quoted[i] = shellQuote(w)
	}
	return strings.Join(quoted, " ")
}

// shellQuote quotes a word when it needs it.
func shellQuote(s string) string {
	if s != "" && strings.Trim(s, "abcdefghijklmnopqrstuvwxyzABCDEFGHIJKLMNOPQRSTUVWXYZ0123456789_@%+=:,./-") == "" {
		return s
	}
	return "'" + strings.ReplaceAll(s, "'", `'\''`) + "'"
}
