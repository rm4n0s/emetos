package golang

import (
	"context"
	_ "embed"
	"encoding/json"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/rm4n0s/errors"

	"github.com/rm4n0s/emetos/extension"
)

// Debugging Go programs and tests with Delve (SPEC §10.31 "The debuggers", Go; step 25.7). The extension contributes the debugger of type
// "go": its attributes (VS Code Go's), the configuration snippets and the configuration provider (goDebugConfiguration.ts), and the adapter
// descriptor, `dlv dap` started as an executable that prints its port (goDebugFactory.ts). The "debug test" lens and the command
// "Go: Debug Test At Cursor" start a session of mode test (debugtest.go); the process picker of an attach is in debugprocess.go.

// Ported from VS Code Go: extension/src/goDebugConfiguration.ts (GoDebugConfigurationProvider: pickConfiguration,
// resolveDebugConfiguration, resolveDebugConfigurationWithSubstitutedVariables, parseDebugProgramArgSync, removeGcflags),
// extension/src/goDebugFactory.ts (the dlv dap arguments of getSpawnConfig) and the debugger entry of extension/package.json
// (the attributes and the snippets).

// The ids of the debugger.
const (
	// DebugType is the debug type of the debugger.
	DebugType = "go"
	// DebugTestAtCursorCommand debugs the test, benchmark, example or fuzz function at the cursor (VS Code Go's go.debug.cursor), and
	// DebugTestFunctionCommand debugs one by its name: {"uri": ..., "name": ...}. The lenses run the second; it has no title.
	DebugTestAtCursorCommand = ID + ".debugTestAtCursor"
	DebugTestFunctionCommand = ID + ".debugTestFunction"
	// DelveConfigSetting is the object of defaults of the debug configurations (VS Code Go's go.delveConfig).
	DelveConfigSetting = "go.delveConfig"
	// debugAdapterName is the only debug adapter there is: VS Code Go's "dlv-dap" (its "legacy" adapter is not built).
	debugAdapterName = "dlv-dap"
)

// debugAttributes is the attributes of the launch and attach requests: the debugger entry of VS Code Go's package.json, written to
// debug-attributes.json by scripts/update-assets.sh.
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

// debuggerSpec is the declaration of the debugger. There is no initial configuration: the provider of trigger Initial asks which
// kind the new launch.json should start with, as VS Code Go does.
func debuggerSpec() extension.DebuggerSpec {
	return extension.DebuggerSpec{
		Type: DebugType, Label: "Go", Languages: []string{"go"},
		ConfigurationAttributes: attributeSchemas(),
		ConfigurationSnippets: []extension.DebugSnippet{
			{Label: "Go: Launch package", Description: "Debug/test the package in the program attribute", Body: extension.DebugConfiguration{
				"name": "${2:Launch Package}", "type": DebugType, "request": "launch", "mode": "auto", "program": "${1:.}"}},
			{Label: "Go: Launch file", Description: "Debug the file in the program attribute", Body: extension.DebugConfiguration{
				"name": "${2:Launch file}", "type": DebugType, "request": "launch", "mode": "debug", "program": "${1:main.go}"}},
			{Label: "Go: Launch test function", Description: "Debug the test function in the args, ensure program attributes points to right package", Body: extension.DebugConfiguration{
				"name": "${3:Launch test function}", "type": DebugType, "request": "launch", "mode": "test", "program": "${1:.}",
				"args": []any{"-test.run", "${2:MyTestFunction}"}}},
			{Label: "Go: Attach to local process", Description: "Attach to an existing process by process ID", Body: extension.DebugConfiguration{
				"name": "${1:Attach to Process}", "type": DebugType, "request": "attach", "mode": "local", "processId": 0}},
			{Label: "Go: Connect to server", Description: "Connect to a remote headless debug server", Body: extension.DebugConfiguration{
				"name": "${1:Connect to server}", "type": DebugType, "request": "attach", "mode": "remote", "port": 2345, "host": "127.0.0.1"}},
		},
	}
}

// delveDefaults are the defaults of the attributes go.delveConfig sets (the defaults of the properties in VS Code Go's package.json).
var delveDefaults = map[string]any{
	"showRegisters": false, "showGlobalVariables": false, "substitutePath": []any{}, "showLog": false, "logOutput": "debugger",
	"dlvFlags": []any{}, "hideSystemGoroutines": false,
}

// delveSettingSpec is the setting go.delveConfig.
func delveSettingSpec() extension.SettingSpec {
	return extension.SettingSpec{
		Key: DelveConfigSetting, Type: "object", Category: category,
		Default: map[string]any{
			"debugAdapter": debugAdapterName, "showGlobalVariables": false, "showRegisters": false, "hideSystemGoroutines": false,
			"showLog": false, "logOutput": "debugger", "dlvFlags": []any{}, "substitutePath": []any{},
		},
		Description: "Delve configuration. The members are the defaults of the debug configurations of the type `go` that do not set them:\n\n" +
			"- `showGlobalVariables`: whether the package variables are shown in the Variables view\n" +
			"- `showRegisters`: whether the registers are shown in the Variables view\n" +
			"- `hideSystemGoroutines`: whether system goroutines are hidden from the Call Stack\n" +
			"- `showLog`: show the log output of Delve (its `--log` flag) and `logOutput`: the comma separated components that log (`--log-output`: `debugger`, `gdbwire`, `lldbout`, `debuglineerr`, `rpc`, `dap`)\n" +
			"- `dlvFlags`: extra flags for `dlv dap` (`--listen` is used internally)\n" +
			"- `substitutePath`: a list of `{from, to}` mappings from a local path to the path the debuggee uses\n" +
			"- `debugAdapter`: only `dlv-dap` is available; `legacy` is read as `dlv-dap`",
	}
}

// delveSetting is the value of an attribute from go.delveConfig, or its default.
func (g *golang) delveSetting(name string) any {
	if m, err := extension.SettingValue[map[string]any](g.api.Settings(), DelveConfigSetting); err == nil {
		if v, ok := m[name]; ok {
			return v
		}
	}
	return delveDefaults[name]
}

// editorSnapshot is what the editor in front was when the UI thread last told: the file and its language. It may be read from any
// goroutine (the providers run off the UI thread).
type editorSnapshot struct{ uri, language string }

// snapshotEditor stores the editor in front for the goroutines. It runs on the UI thread.
func (g *golang) snapshotEditor() {
	g.editorSnap.Store(&editorSnapshot{uri: g.editor.uri, language: g.editor.language})
}

// activeEditor is the editor in front for a goroutine.
func (g *golang) activeEditor() editorSnapshot {
	if s := g.editorSnap.Load(); s != nil {
		return *s
	}
	return editorSnapshot{}
}

// registerDebug registers the configuration provider (initial and for the resolution of the configurations) and the descriptor
// factory of the debugger. The registrations end with the activation. A host with no debug service has none: the extension then has
// no debugger, and the commands that debug say so when they run.
func (g *golang) registerDebug() {
	d := g.api.Debug()
	provider := extension.DebugConfigurationProvider{Provide: g.provideDebugConfigurations, Resolve: g.resolveDebug, ResolveSubstituted: g.resolveDebugSubstituted}
	if _, err := d.RegisterConfigurationProvider(DebugType, provider, extension.DebugTriggerInitial); err != nil {
		g.api.Log().Debug("the Go debugger has no configuration provider", "err", err)
		return
	}
	if _, err := d.RegisterAdapterDescriptorFactory(DebugType, g.debugAdapter); err != nil {
		g.api.Log().Debug("the Go debugger has no adapter", "err", err)
	}
}

// ---- dlv ----

// dlvProgram finds dlv: go.alternateTools.dlv (it must exist), else dlv in PATH, else the bin folder of go.toolsGopath, GOBIN or
// the GOPATH (go.toolsEnvVars first), the places "Go: Install/Update Tools" installs it to.
func (g *golang) dlvProgram() (string, error) {
	folder := g.api.Scope().Folder
	if name := g.alternateTool("dlv"); name != "" {
		path, err := lookProgram(name, folder)
		if err != nil {
			return "", errors.New("GoDlvNotFound", "go.alternateTools names a dlv that cannot be run: "+describe(err), "program", name)
		}
		return path, nil
	}
	if path, err := lookProgram("dlv", folder); err == nil {
		return path, nil
	}
	for _, dir := range g.toolBinDirs() {
		path := filepath.Join(dir, "dlv")
		if st, err := os.Stat(path); err == nil && !st.IsDir() && st.Mode()&0o111 != 0 {
			return path, nil
		}
	}
	return "", errors.New("GoDlvNotFound", "dlv was not found in go.alternateTools, in PATH or in the bin folder of GOBIN or GOPATH")
}

// toolBinDirs are the folders the Go tools are installed to: the bin folder of go.toolsGopath, GOBIN, and the bin folder of each
// GOPATH (the home folder's go when none is set). The variables are those of go.toolsEnvVars, else of Emetos.
func (g *golang) toolBinDirs() []string {
	vars := map[string]string{}
	if m, err := extension.SettingValue[map[string]any](g.api.Settings(), ToolsEnvVarsSetting); err == nil {
		for k, v := range m {
			if s, ok := v.(string); ok {
				vars[k] = s
			}
		}
	}
	get := func(name string) string {
		if v, ok := vars[name]; ok {
			return v
		}
		return os.Getenv(name)
	}
	var dirs []string
	if gp, _ := extension.SettingValue[string](g.api.Settings(), ToolsGopathSetting); strings.TrimSpace(gp) != "" {
		dirs = append(dirs, filepath.Join(strings.TrimSpace(gp), "bin"))
	}
	if bin := get("GOBIN"); bin != "" {
		dirs = append(dirs, bin)
	}
	if gopath := get("GOPATH"); gopath != "" {
		for _, p := range filepath.SplitList(gopath) {
			dirs = append(dirs, filepath.Join(p, "bin"))
		}
	} else if home, err := os.UserHomeDir(); err == nil {
		dirs = append(dirs, filepath.Join(home, "go", "bin"))
	}
	return dirs
}

// promptMissingDlv tells the user that dlv is missing, with a button that installs it. It tells again each time a session is started
// without it (VS Code Go asks as long as the user has not declined). It may be called from any goroutine.
func (g *golang) promptMissingDlv() {
	tl, _ := toolByName("dlv")
	g.api.Notifications().Warn(tl.Name+" was not found. It "+tl.Purpose+". Set go.alternateTools if it is somewhere else.",
		extension.NotificationAction{Label: "Install", Run: func() { g.startInstall([]string{tl.Name}) }})
}

// ---- the adapter ----

// debugAdapter is the descriptor factory of the debugger (createDebugAdapterDescriptor): a configuration with a port connects to a
// `dlv dap` server that runs already (VS Code Go's DebugAdapterServer), any other starts `dlv dap` and connects to the port it
// prints. dlv is started in the folder of the package it builds (__buildDir), with the environment of the configuration.
func (g *golang) debugAdapter(_ context.Context, session extension.DebugSession, _ *extension.DebugAdapter) (*extension.DebugAdapter, error) {
	cfg := session.Configuration()
	if port, ok := intAttribute(cfg["port"]); ok && port > 0 {
		host, _ := cfg["host"].(string)
		if host == "" {
			host = "127.0.0.1"
		}
		return &extension.DebugAdapter{Kind: extension.DebugAdapterServer, Host: host, Port: port}, nil
	}
	dlv, _ := cfg["dlvToolPath"].(string)
	if dlv == "" {
		var err error
		if dlv, err = g.dlvProgram(); err != nil {
			g.promptMissingDlv()
			return nil, err
		}
	}
	dir := session.Folder()
	if dir == "" {
		dir = g.api.Scope().Folder
	}
	if build, _ := cfg["__buildDir"].(string); build != "" && cfg["request"] == "launch" {
		dir = build
	}
	return &extension.DebugAdapter{
		Kind: extension.DebugAdapterExecutableServer, Command: dlv, Args: dlvArgs(cfg), Cwd: dir, Env: stringEnv(cfg["env"]), ConsoleOutput: true,
	}, nil
}

// dlvArgs are the arguments of `dlv dap`: the extra flags of dlvFlags, the log flags (the output flag only with the log flag, or dlv
// complains) and the address to listen on, a port dlv chooses.
func dlvArgs(cfg extension.DebugConfiguration) []string {
	args := []string{"dap"}
	if flags, ok := cfg["dlvFlags"].([]any); ok {
		for _, f := range flags {
			if s, ok := f.(string); ok && s != "" {
				args = append(args, s)
			}
		}
	}
	if log, ok := cfg["showLog"].(bool); ok && log {
		args = append(args, "--log=true")
		if out, _ := cfg["logOutput"].(string); out != "" {
			args = append(args, "--log-output="+out)
		}
	}
	return append(args, "--listen=127.0.0.1:0")
}

// stringEnv is the environment of dlv from the env attribute: the members that are strings.
func stringEnv(v any) map[string]string {
	m, ok := v.(map[string]any)
	if !ok {
		return nil
	}
	out := make(map[string]string, len(m))
	for k, val := range m {
		if s, ok := val.(string); ok && k != "" {
			out[k] = s
		}
	}
	return out
}

// intAttribute reads a number or a string of digits attribute.
func intAttribute(v any) (int, bool) {
	switch n := v.(type) {
	case float64:
		return int(n), n == float64(int(n))
	case int:
		return n, true
	case string:
		i, err := strconv.Atoi(strings.TrimSpace(n))
		return i, err == nil
	}
	return 0, false
}
