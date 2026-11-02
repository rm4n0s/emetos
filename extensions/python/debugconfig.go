package python

import (
	"context"
	"maps"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"

	"github.com/rm4n0s/errors"

	"github.com/rm4n0s/emetos/extension"
)

// The configuration provider of the Python debugger: the configurations a new launch.json starts with and the dynamic ones, the
// defaults of a configuration before its variables are substituted, and its environment, paths and options after (SPEC §10.31 "The
// debuggers", Python).

// Ported from vscode-python-debugger: src/extension/debugger/configuration/debugConfigurationService.ts,
// dynamicdebugConfigurationService.ts, providers/*.ts and resolvers/{base,launch,attach,helper}.ts.

// The names of the configurations (the snippets of vscode-python-debugger's DebugConfigStrings).
const (
	nameFile         = "Python Debugger: Current File"
	nameFileWithArgs = "Python Debugger: Current File with Arguments"
	nameModule       = "Python Debugger: Module"
	nameAttach       = "Python Debugger: Remote Attach"
	nameAttachPID    = "Python Debugger: Attach using Process Id"
	nameDjango       = "Python Debugger: Django"
	nameFlask        = "Python Debugger: Flask"
	nameFastAPI      = "Python Debugger: FastAPI"
	namePyramid      = "Python Debugger: Pyramid"
)

// The interactive variables the provider answers itself (a command of an extension returns no value), and the one it replaces by the
// interpreter.
const (
	pickArgsVariable       = "${command:pickArgs}"
	interpreterPathCommand = "${command:python.interpreterPath}"
	defaultAttachPort      = 5678
	defaultAttachHost      = "localhost"
)

// The debug options the adapter is told about (vscode-python-debugger's DebugOptions).
const (
	optRedirectOutput  = "RedirectOutput"
	optDjango          = "Django"
	optJinja           = "Jinja"
	optSudo            = "Sudo"
	optStopOnEntry     = "StopOnEntry"
	optShowReturnValue = "ShowReturnValue"
	optSubProcess      = "Multiprocess"
)

// configChoice is a configuration of the list a new launch.json starts from.
type configChoice struct {
	label, description string
	config             func() extension.DebugConfiguration
	// ask names the questions the choice has: "module" or "attach".
	ask string
}

// launchBase is a launch configuration of the debugger with a name.
func launchBase(name string, extra map[string]any) extension.DebugConfiguration {
	c := extension.DebugConfiguration{"name": name, "type": DebugType, "request": "launch"}
	for k, v := range extra {
		c[k] = v
	}
	return c
}

// configChoices are the configurations of the list, in the order of vscode-python-debugger's quick pick.
func configChoices() []configChoice {
	return []configChoice{
		{label: "Python File", description: "Debug the currently active Python file", config: func() extension.DebugConfiguration {
			return launchBase(nameFile, map[string]any{"program": "${file}", "console": "integratedTerminal"})
		}},
		{label: "Python File with Arguments", description: "Debug the currently active Python file with arguments", config: func() extension.DebugConfiguration {
			return launchBase(nameFileWithArgs, map[string]any{"program": "${file}", "console": "integratedTerminal", "args": pickArgsVariable})
		}},
		{label: "Module", description: "Debug a Python module by invoking it with '-m'", ask: "module", config: func() extension.DebugConfiguration {
			return launchBase(nameModule, map[string]any{"module": "enter-your-module-name"})
		}},
		{label: "Remote Attach", description: "Attach to a remote debug server", ask: "attach", config: func() extension.DebugConfiguration {
			return extension.DebugConfiguration{"name": nameAttach, "type": DebugType, "request": "attach",
				"connect":      map[string]any{"host": defaultAttachHost, "port": float64(defaultAttachPort)},
				"pathMappings": []any{map[string]any{"localRoot": "${workspaceFolder}", "remoteRoot": "."}}}
		}},
		{label: "Attach using Process ID", description: "Attach to a local process", config: func() extension.DebugConfiguration {
			return extension.DebugConfiguration{"name": nameAttachPID, "type": DebugType, "request": "attach", "processId": pickProcessVariable}
		}},
		{label: "Django", description: "Launch and debug a Django web application", config: func() extension.DebugConfiguration {
			return launchBase(nameDjango, map[string]any{"program": "${workspaceFolder}/manage.py", "args": []any{"runserver"}, "django": true, "autoStartBrowser": false})
		}},
		{label: "FastAPI", description: "Launch and debug a FastAPI web application", config: func() extension.DebugConfiguration {
			return launchBase(nameFastAPI, map[string]any{"module": "uvicorn", "args": []any{"main:app", "--reload"}, "jinja": true})
		}},
		{label: "Flask", description: "Launch and debug a Flask web application", config: func() extension.DebugConfiguration {
			return launchBase(nameFlask, map[string]any{"module": "flask", "env": map[string]any{"FLASK_APP": "app.py", "FLASK_DEBUG": "1"},
				"args": []any{"run", "--no-debugger", "--no-reload"}, "jinja": true, "autoStartBrowser": false})
		}},
		{label: "Pyramid", description: "Launch and debug a Pyramid web application", config: func() extension.DebugConfiguration {
			return launchBase(namePyramid, map[string]any{"module": "pyramid.scripts.pserve", "args": []any{"${workspaceFolder}/development.ini"}, "pyramid": true, "jinja": true})
		}},
	}
}

// provideDebugConfigurations asks which configuration a new launch.json starts with (the quick pick of vscode-python-debugger's
// PythonDebugConfigurationService). It blocks until the user chose, so it runs off the UI thread. A closed list gives no configuration.
func (p *python) provideDebugConfigurations(ctx context.Context, _ string) ([]extension.DebugConfiguration, error) {
	choices := configChoices()
	items := make([]extension.QuickPickItem, len(choices))
	for i, c := range choices {
		items[i] = extension.QuickPickItem{Label: c.label, Description: c.description}
	}
	index, err := p.pickItem(ctx, extension.QuickPickOptions{Placeholder: "Debug Configuration", Items: items, IgnoreFocusOut: true})
	if err != nil {
		return nil, err
	}
	if index < 0 {
		return nil, nil
	}
	cfg := choices[index].config()
	switch choices[index].ask {
	case "module":
		module, ok, err := p.askText(ctx, "Enter a Python module/package name", "enter-your-module-name", func(s string) string {
			if strings.TrimSpace(s) == "" {
				return "Enter a valid module name"
			}
			return ""
		})
		if err != nil {
			return nil, err
		}
		if !ok || strings.TrimSpace(module) == "" {
			return nil, nil
		}
		cfg["module"] = strings.TrimSpace(module)
	case "attach":
		host, ok, err := p.askText(ctx, "Enter a valid host name or IP address", defaultAttachHost, func(s string) string {
			if strings.TrimSpace(s) == "" {
				return "Enter a valid host name or IP address"
			}
			return ""
		})
		if err != nil {
			return nil, err
		}
		if !ok || strings.TrimSpace(host) == "" {
			return nil, nil
		}
		connect := cfg["connect"].(map[string]any)
		connect["host"] = strings.TrimSpace(host)
		portText, ok, err := p.askText(ctx, "Enter the port number that the debug server is listening on", strconv.Itoa(defaultAttachPort), func(s string) string {
			if _, isNumber := portNumber(s); !isNumber {
				return "Enter a valid port number"
			}
			return ""
		})
		if err != nil {
			return nil, err
		}
		if port, isNumber := portNumber(portText); ok && isNumber {
			connect["port"] = float64(port)
		}
	}
	return []extension.DebugConfiguration{cfg}, nil
}

// portNumber reads a port: digits only.
func portNumber(s string) (int, bool) {
	s = strings.TrimSpace(s)
	if s == "" || strings.Trim(s, "0123456789") != "" {
		return 0, false
	}
	n, err := strconv.Atoi(s)
	return n, err == nil && n > 0 && n < 65536
}

// provideDynamicConfigurations gives the configurations the dropdown of Run and Debug lists without a launch.json: the file in front,
// and a Django application when the folder has a manage.py (vscode-python-debugger's DynamicPythonDebugConfigurationService; the
// detection of Flask and FastAPI applications in the files of the folder is not built).
func (p *python) provideDynamicConfigurations(_ context.Context, folder string) ([]extension.DebugConfiguration, error) {
	list := []extension.DebugConfiguration{launchBase(nameFile, map[string]any{"program": "${file}"})}
	if folder == "" {
		folder = p.api.Scope().Folder
	}
	if folder != "" {
		if info, err := os.Stat(filepath.Join(folder, "manage.py")); err == nil && !info.IsDir() {
			list = append(list, launchBase(nameDjango, map[string]any{"program": "${workspaceFolder}/manage.py", "args": []any{"runserver"}, "django": true}))
		}
	}
	return list, nil
}

// pickItem shows a quick pick from a goroutine and waits for the answer: the index of the item, or -1 when the list was closed. It
// stops waiting when ctx ends. It fails with PythonDebugPickFailed when the host has no quick pick.
func (p *python) pickItem(ctx context.Context, opts extension.QuickPickOptions) (int, error) {
	ch := make(chan int, 1)
	if err := p.api.QuickPick().Show(opts, func(i int) { ch <- i }); err != nil {
		return -1, errors.NewErr("PythonDebugPickFailed", err)
	}
	select {
	case i := <-ch:
		return i, nil
	case <-ctx.Done():
		return -1, ctx.Err()
	}
}

// askText asks for a line of text from a goroutine; ok is false when the box was closed. validate returns what is wrong with the text,
// or "". It fails with PythonDebugPickFailed when the host has no input box.
func (p *python) askText(ctx context.Context, prompt, value string, validate func(string) string) (text string, ok bool, err error) {
	type answer struct {
		text string
		ok   bool
	}
	ch := make(chan answer, 1)
	opts := extension.InputBoxOptions{Prompt: prompt, Value: value, Validate: validate}
	if err := p.api.InputBox().Show(opts, func(s string, accepted bool) { ch <- answer{s, accepted} }); err != nil {
		return "", false, errors.NewErr("PythonDebugPickFailed", err)
	}
	select {
	case a := <-ch:
		return a.text, a.ok, nil
	case <-ctx.Done():
		return "", false, ctx.Err()
	}
}

// snapshot of the editor in front, for the providers (they run off the UI thread).
type editorSnapshot struct{ uri, language string }

// activeEditor is the editor in front for a goroutine.
func (p *python) activeEditor() editorSnapshot {
	if s := p.editorSnap.Load(); s != nil {
		return *s
	}
	return editorSnapshot{}
}

// isSet tells whether an attribute has a value: it is there and not null.
func isSet(cfg extension.DebugConfiguration, name string) bool { return cfg[name] != nil }

// resolveDebug completes a configuration before its variables are substituted (resolveDebugConfiguration). A configuration with no
// request is the "Debug" of F5 with no launch.json: it launches the Python file in front, and there is none to launch when the editor
// in front is not a Python file (the launch is cancelled). It returns nil, nil when the launch is cancelled. It fails with
// PythonDebugRequestInvalid for the deprecated request "test" and with PythonDebugNoInterpreter when a launch has no interpreter.
func (p *python) resolveDebug(ctx context.Context, _ string, cfg extension.DebugConfiguration) (extension.DebugConfiguration, error) {
	cfg = maps.Clone(cfg)
	if cfg == nil {
		cfg = extension.DebugConfiguration{}
	}
	request, _ := cfg["request"].(string)
	if request == "test" {
		return nil, errors.New("PythonDebugRequestInvalid", "This configuration can only be used by the test debugging commands. \"request\": \"test\" is deprecated, "+
			"please keep as \"request\": \"launch\" and add \"purpose\": [\"debug-test\"] instead.")
	}
	if request == "" {
		if ed := p.activeEditor(); ed.language != "python" || ed.uri == "" {
			p.api.Notifications().Info("Debug needs a Python file: open one, or add a configuration to launch.json.")
			return nil, nil
		}
		cfg["name"], cfg["request"], cfg["program"], cfg["console"] = nameFile, "launch", "${file}", "integratedTerminal"
		request = "launch"
	}
	if cfg["type"] == nil || cfg["type"] == "" {
		cfg["type"] = DebugType
	}
	if !isSet(cfg, "clientOS") {
		cfg["clientOS"] = "unix"
	}
	if name, ok := cfg["consoleName"].(string); ok && name != "" {
		cfg["consoleTitle"] = name
	}
	delete(cfg, "consoleName")
	if request == "attach" {
		return cfg, p.resolveProcessID(ctx, cfg)
	}
	if request == "launch" {
		if err := p.resolveInterpreterAttributes(cfg); err != nil {
			return nil, err
		}
		if cfg["args"] == pickArgsVariable {
			args, ok, err := p.askText(ctx, "Enter program arguments", "", nil)
			if err != nil {
				return nil, err
			}
			if !ok {
				return nil, nil
			}
			if strings.TrimSpace(args) == "" {
				cfg["args"] = []any{}
			} else {
				cfg["args"] = args
			}
		}
	}
	return cfg, nil
}

// resolveInterpreterAttributes sets python, debugAdapterPython and debugLauncherPython of a launch (resolveAndUpdatePythonPath): the
// value of the attribute, else the legacy pythonPath, else the interpreter in use. The variable ${command:python.interpreterPath} is the
// interpreter in use. Unlike vscode-python-debugger, whose adapter is bundled, the adapter runs in the interpreter of the program
// (python) unless debugAdapterPython says another, because debugpy is installed there.
func (p *python) resolveInterpreterAttributes(cfg extension.DebugConfiguration) error {
	text := func(name string) string {
		s, _ := cfg[name].(string)
		if strings.TrimSpace(s) == interpreterPathCommand {
			return ""
		}
		return strings.TrimSpace(s)
	}
	legacy := text("pythonPath")
	delete(cfg, "pythonPath")
	python := text("python")
	if python == "" {
		python = legacy
	}
	if python == "" {
		i, err := p.interpreterInUse()
		if err != nil {
			return err
		}
		python = i
	}
	cfg["python"] = python
	for _, name := range []string{"debugAdapterPython", "debugLauncherPython"} {
		if v := text(name); v != "" {
			cfg[name] = v
		} else {
			cfg[name] = python
		}
	}
	return nil
}

// interpreterInUse is the Python program the pack uses now (Interpreters.Resolve: the choice of the user, python.defaultInterpreterPath,
// the active environment, python3 on PATH). It runs nothing. It fails with PythonDebugNoInterpreter, and shows the notification that
// says how to get one, when there is none. It may be called from any goroutine.
func (p *python) interpreterInUse() (string, error) {
	env := p.interps.environ(p.interps.snapshot())
	i, _, ok := p.interps.Resolve(env)
	if !ok {
		p.api.Notifications().Warn("No Python interpreter was found. Choose one with Python: Select Interpreter, or set python.defaultInterpreterPath.")
		return "", errors.New("PythonDebugNoInterpreter", "no Python interpreter was found: choose one with Python: Select Interpreter or set python.defaultInterpreterPath")
	}
	return i.Path, nil
}

// folderOf is the folder of a configuration: the folder of its launch.json, else of the workspace.
func (p *python) folderOf(folder string) string {
	if folder != "" {
		return folder
	}
	return p.api.Scope().Folder
}

// resolveDebugSubstituted completes a configuration after its variables are substituted (resolveDebugConfigurationWithSubstitutedVariables):
// for a launch the working folder, the environment (the file, then env), the options of the adapter and the path mappings; for an attach
// the host, the options and the path mappings. A process id given as a string becomes a number.
func (p *python) resolveDebugSubstituted(_ context.Context, folder string, cfg extension.DebugConfiguration) (extension.DebugConfiguration, error) {
	cfg = maps.Clone(cfg)
	folder = p.folderOf(folder)
	if s, ok := cfg["processId"].(string); ok {
		if n, isNumber := portNumber(s); isNumber {
			cfg["processId"] = float64(n)
		}
	}
	justMyCode := p.debugJustMyCode()
	if cfg["request"] == "attach" {
		return p.attachDefaults(cfg, folder, justMyCode), nil
	}
	return p.launchDefaults(cfg, folder, justMyCode)
}

// debugJustMyCode is the setting debugpy.debugJustMyCode: the default of justMyCode.
func (p *python) debugJustMyCode() bool {
	v, err := extension.SettingValue[bool](p.api.Settings(), DebugJustMyCodeSetting)
	return err != nil || v
}

// envFileSetting is python.envFile with ${workspaceFolder} replaced.
func (p *python) envFileSetting(folder string) string {
	v, _ := extension.SettingValue[string](p.api.Settings(), EnvFileSetting)
	if v == "" {
		return ""
	}
	return strings.NewReplacer("${workspaceFolder}", folder, "${workspaceRoot}", folder).Replace(v)
}

// stringMap reads the members of an object that are strings.
func stringMap(v any) map[string]string {
	m, ok := v.(map[string]any)
	if !ok {
		return nil
	}
	out := make(map[string]string, len(m))
	for k, val := range m {
		if s, ok := val.(string); ok {
			out[k] = s
		}
	}
	return out
}

// addOption adds a debug option to the list once.
func addOption(options []any, option string) []any {
	if slices.Contains(options, any(option)) {
		return options
	}
	return append(options, option)
}

// launchDefaults is provideLaunchDefaults of vscode-python-debugger's LaunchConfigurationResolver.
func (p *python) launchDefaults(cfg extension.DebugConfiguration, folder string, justMyCode bool) (extension.DebugConfiguration, error) {
	if pythonPath, ok := cfg["pythonPath"].(string); ok && pythonPath != "" {
		for _, name := range []string{"python", "debugAdapterPython", "debugLauncherPython"} {
			if !isSet(cfg, name) {
				cfg[name] = pythonPath
			}
		}
	}
	delete(cfg, "pythonPath")
	if cwd, ok := cfg["cwd"].(string); (!ok || cwd == "") && folder != "" {
		cfg["cwd"] = folder
	}
	envFile, hasEnvFile := cfg["envFile"].(string)
	if !hasEnvFile && folder != "" {
		envFile = p.envFileSetting(folder)
	}
	// the file of environment variables: its variables, then those of env win
	own := stringMap(cfg["env"])
	fileVars, err := readEnvFile(envFile, own)
	if err != nil {
		return nil, err
	}
	gevent, _ := cfg["gevent"].(bool)
	env := debugEnvironment(own, fileVars, gevent, os.Getenv)
	envAny := make(map[string]any, len(env))
	for k, v := range env {
		envAny[k] = v
	}
	cfg["env"] = envAny
	delete(cfg, "envFile")

	if _, ok := cfg["stopOnEntry"].(bool); !ok {
		cfg["stopOnEntry"] = false
	}
	cfg["showReturnValue"] = cfg["showReturnValue"] != false
	console, _ := cfg["console"].(string)
	if console == "" {
		console = "integratedTerminal"
		cfg["console"] = console
	}
	if console != "internalConsole" && !isSet(cfg, "internalConsoleOptions") {
		cfg["internalConsoleOptions"] = "neverOpen"
	}
	if q, _ := cfg["terminalQuoteCharacter"].(string); len(q) != 1 {
		cfg["terminalQuoteCharacter"] = "\""
	}
	options, _ := cfg["debugOptions"].([]any)
	if options == nil {
		options = []any{}
	}
	if !isSet(cfg, "justMyCode") {
		cfg["justMyCode"] = justMyCode
	}
	if folder != "" {
		cfg["workspaceFolder"] = folder
	}
	on := func(name string) bool { v, _ := cfg[name].(bool); return v }
	if on("stopOnEntry") {
		options = addOption(options, optStopOnEntry)
	}
	if on("showReturnValue") {
		options = addOption(options, optShowReturnValue)
	}
	if on("django") {
		options = addOption(options, optDjango)
	}
	if on("jinja") {
		options = addOption(options, optJinja)
	}
	if !isSet(cfg, "redirectOutput") && console == "internalConsole" {
		cfg["redirectOutput"] = true
	}
	if on("redirectOutput") {
		options = addOption(options, optRedirectOutput)
	}
	if on("sudo") {
		options = addOption(options, optSudo)
	}
	if on("subProcess") {
		options = addOption(options, optSubProcess)
	}
	module, _ := cfg["module"].(string)
	module = strings.ToUpper(module)
	if (on("pyramid") || module == "FLASK" || module == "FASTAPI") && !slices.Contains(options, any(optJinja)) && cfg["jinja"] != false {
		options = addOption(options, optJinja)
	}
	cfg["debugOptions"] = options
	// unlike an attach, a launch has no default path mapping
	if mappings, ok := cfg["pathMappings"].([]any); ok {
		var fixed []any
		if len(mappings) > 0 {
			fixed = fixUpPathMappings(mappings, folder, "")
		}
		if len(fixed) == 0 {
			delete(cfg, "pathMappings")
		} else {
			cfg["pathMappings"] = fixed
		}
	}
	return cfg, nil
}

// attachDefaults is provideAttachDefaults of vscode-python-debugger's AttachConfigurationResolver.
func (p *python) attachDefaults(cfg extension.DebugConfiguration, folder string, justMyCode bool) extension.DebugConfiguration {
	options, _ := cfg["debugOptions"].([]any)
	if options == nil {
		options = []any{}
	}
	if !isSet(cfg, "connect") && !isSet(cfg, "listen") && !isSet(cfg, "host") {
		cfg["host"] = defaultAttachHost
	}
	if !isSet(cfg, "justMyCode") {
		cfg["justMyCode"] = justMyCode
	}
	cfg["showReturnValue"] = cfg["showReturnValue"] != false
	if folder != "" {
		cfg["workspaceFolder"] = folder
	}
	on := func(name string) bool { v, _ := cfg[name].(bool); return v }
	if on("django") {
		options = addOption(options, optDjango)
	}
	if on("jinja") {
		options = addOption(options, optJinja)
	}
	if on("subProcess") {
		options = addOption(options, optSubProcess)
	}
	if on("pyramid") && !slices.Contains(options, any(optJinja)) && cfg["jinja"] != false {
		options = addOption(options, optJinja)
	}
	if on("redirectOutput") || !isSet(cfg, "redirectOutput") {
		options = addOption(options, optRedirectOutput)
	}
	if on("showReturnValue") {
		options = addOption(options, optShowReturnValue)
	}
	cfg["debugOptions"] = options

	// the roots of the two sides are mapped when the program is on this machine
	mappings, _ := cfg["pathMappings"].([]any)
	local, _ := cfg["localRoot"].(string)
	remote, _ := cfg["remoteRoot"].(string)
	if local != "" && remote != "" {
		mappings = append(slices.Clone(mappings), map[string]any{"localRoot": local, "remoteRoot": remote})
	}
	host, _ := cfg["host"].(string)
	if isLocalHost(host) {
		mappings = fixUpPathMappings(mappings, folder, "")
	}
	if len(mappings) == 0 {
		delete(cfg, "pathMappings")
	} else {
		cfg["pathMappings"] = mappings
	}
	return cfg
}

// isLocalHost tells whether the name is this machine.
func isLocalHost(host string) bool {
	switch strings.ToLower(host) {
	case "localhost", "127.0.0.1", "::1":
		return true
	}
	return false
}

// fixUpPathMappings makes the mappings usable (BaseConfigurationResolver.fixUpPathMappings): none gives the one of the local root to
// the same path; ${workspaceFolder} in a local root is the local root. With no local root there are no mappings.
func fixUpPathMappings(mappings []any, defaultLocal, defaultRemote string) []any {
	if defaultLocal == "" {
		return nil
	}
	if defaultRemote == "" {
		defaultRemote = defaultLocal
	}
	if len(mappings) == 0 {
		return []any{map[string]any{"localRoot": defaultLocal, "remoteRoot": defaultRemote}}
	}
	out := make([]any, 0, len(mappings))
	for _, m := range mappings {
		mm, ok := m.(map[string]any)
		if !ok {
			continue
		}
		local, _ := mm["localRoot"].(string)
		local = strings.NewReplacer("${workspaceFolder}", defaultLocal, "${workspaceRoot}", defaultLocal).Replace(local)
		out = append(out, map[string]any{"localRoot": local, "remoteRoot": mm["remoteRoot"]})
	}
	return out
}
