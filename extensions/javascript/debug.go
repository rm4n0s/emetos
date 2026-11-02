package javascript

import (
	"context"
	_ "embed"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"

	"github.com/rm4n0s/errors"

	"github.com/rm4n0s/emetos/extension"
	"github.com/rm4n0s/emetos/extensions/internal/langserver"
)

// Debugging JavaScript and TypeScript programs with js-debug (SPEC §10.31 "The debuggers", JavaScript and TypeScript; step 25.10). The
// extension contributes the debug types node and pwa-node (its old name): their attributes and snippets (those of js-debug's package.json),
// the configuration providers (debugconfig.go) and the adapter descriptor, `node <js-debug>/src/dapDebugServer.js 0 127.0.0.1` started as
// an executable server. js-debug serves a session and the sessions it starts for the child processes and workers of the program
// (startDebugging) from the one server, so the descriptor says ServesChildren. A missing node or js-debug is told to the user.

// Ported from vscode-js-debug: src/ui/debugAdapterFactory.ts (the server and its address), src/build/generate-contributions.ts (the
// debugger entries, through the package.json of the release) and src/ui/configuration/*.ts (debugconfig.go).

// The ids of the debugger, its setting and its command.
const (
	// DebugType is the debug type of the debugger, and DebugTypeOld the name it had before 2022 (pwa-node): a launch.json that says so is
	// served by the same debugger.
	DebugType    = "node"
	DebugTypeOld = "pwa-node"
	// AdapterPathSetting is where js-debug is: its dapDebugServer.js, the folder with src/dapDebugServer.js, or the folder that holds the
	// folder js-debug of the release.
	AdapterPathSetting = "debug.javascript.adapterPath"
	// NodePathSetting is the node program the TypeScript pack runs its server with (typescript.nodePath); the debugger runs js-debug with
	// the same node. It is read, not registered, here.
	NodePathSetting = "typescript.nodePath"
	// DownloadCommand fetches the release of js-debug that is pinned in this package.
	DownloadCommand = "emetos.javascript.downloadJsDebug"
)

// breakpointLanguages are the languages where breakpoints can be set (js-debug's breakpointLanguages).
var breakpointLanguages = []string{"javascript", "javascriptreact", "typescript", "typescriptreact"}

// minNode is the oldest node that runs js-debug's server.
const minNode = "18"

// debugAttributes are the attributes of the launch and attach requests: the debugger entry of the package.json of js-debug, written to
// debug-attributes.json by scripts/update-assets.sh.
//
//go:embed debug-attributes.json
var debugAttributes []byte

// debugSnippets are the configuration snippets of the debugger entry, written to debug-snippets.json by scripts/update-assets.sh.
//
//go:embed debug-snippets.json
var debugSnippets []byte

// attributeSchemas reads the embedded attributes: the JSON schema of each request.
func attributeSchemas() map[string]json.RawMessage {
	var out map[string]json.RawMessage
	if err := json.Unmarshal(debugAttributes, &out); err != nil {
		return nil
	}
	return out
}

// snippets reads the embedded snippets of js-debug.
func snippets() []extension.DebugSnippet {
	var list []struct {
		Label       string                       `json:"label"`
		Description string                       `json:"description"`
		Body        extension.DebugConfiguration `json:"body"`
	}
	if err := json.Unmarshal(debugSnippets, &list); err != nil {
		return nil
	}
	out := make([]extension.DebugSnippet, len(list))
	for i, s := range list {
		out[i] = extension.DebugSnippet{Label: s.Label, Description: s.Description, Body: s.Body}
	}
	return out
}

// debuggerSpecs declare the two types. Only node serves the languages and has dynamic configurations, so that F5 with no launch.json has
// one debugger to choose; the old name is for the launch.json files that use it.
func debuggerSpecs() []extension.DebuggerSpec {
	return []extension.DebuggerSpec{
		{
			Type: DebugType, Label: "Node.js", Languages: breakpointLanguages, ConfigurationAttributes: attributeSchemas(),
			ConfigurationSnippets: snippets(), DynamicConfigurations: true,
		},
		{Type: DebugTypeOld, Label: "Node.js (the old name pwa-node)", ConfigurationAttributes: attributeSchemas()},
	}
}

// debugSettingSpecs are the settings the debugger reads.
func debugSettingSpecs() []extension.SettingSpec {
	return []extension.SettingSpec{{
		Key: AdapterPathSetting, Type: "string", Default: "", Category: "JavaScript Debugger",
		Description: "The js-debug that debugs JavaScript and TypeScript: its `dapDebugServer.js`, or the folder that holds `src/dapDebugServer.js` " +
			"(or a folder `js-debug` with it). When it is empty, the release that \"JavaScript Debugger: Download\" put in Emetos's data folder is used.",
	}}
}

// commandSpecs are the commands of the extension.
func commandSpecs() []extension.CommandSpec {
	return []extension.CommandSpec{{
		ID: DownloadCommand, Title: "Download", Category: "JavaScript Debugger",
		Description: "Downloads js-debug " + JsDebugVersion + " from github.com/microsoft/vscode-js-debug into Emetos's data folder, after its SHA-256 was checked. Nothing is downloaded unless you run this.",
	}}
}

// registerDebug registers the configuration providers and the descriptor factory of the two types. The registrations end with the
// activation. A host with no debug service has none: the extension then has no debugger.
func (j *javascript) registerDebug() {
	d := j.api.Debug()
	for _, typ := range []string{DebugType, DebugTypeOld} {
		provider := extension.DebugConfigurationProvider{Resolve: j.resolveDebug, ResolveSubstituted: j.resolveDebugSubstituted}
		if typ == DebugType {
			provider.Provide = j.provideInitial
		}
		if _, err := d.RegisterConfigurationProvider(typ, provider, extension.DebugTriggerInitial); err != nil {
			j.api.Log().Debug("the JavaScript debugger has no configuration provider", "err", err)
			return
		}
		if typ == DebugType {
			dynamic := extension.DebugConfigurationProvider{Provide: j.provideDynamic}
			if _, err := d.RegisterConfigurationProvider(typ, dynamic, extension.DebugTriggerDynamic); err != nil {
				j.api.Log().Debug("the JavaScript debugger has no dynamic configurations", "err", err)
			}
		}
		if _, err := d.RegisterAdapterDescriptorFactory(typ, j.debugAdapter); err != nil {
			j.api.Log().Debug("the JavaScript debugger has no adapter", "err", err)
		}
	}
}

// settingString reads a setting of the debugger, "" when it is not set or not registered (typescript.nodePath belongs to another
// extension, which may be off).
func (j *javascript) settingString(key string) string {
	s, err := extension.SettingValue[string](j.api.Settings(), key)
	if err != nil {
		return ""
	}
	return strings.TrimSpace(s)
}

// folderOf is the folder a session works in: its own, else the folder of the tab.
func (j *javascript) folderOf(folder string) string {
	if folder != "" {
		return folder
	}
	return j.api.Scope().Folder
}

// env is what the search for a program knows of the machine.
func (j *javascript) env(folder string) langserver.Env {
	home, err := os.UserHomeDir()
	if err != nil {
		home = "" // a path with ~ in it then stays as it is
	}
	return langserver.Env{Folder: folder, Home: home, Path: os.Getenv("PATH")}
}

// nodeInstall says how to get node.
var nodeInstall = langserver.Install{
	Docs: "https://nodejs.org/en/download",
	Commands: []langserver.InstallCommand{
		{Family: langserver.FamilyFedora, Command: "sudo dnf install nodejs"},
		{Family: langserver.FamilyDebian, Command: "sudo apt install nodejs"},
		{Family: langserver.FamilyArch, Command: "sudo pacman -S nodejs"},
		{Family: langserver.FamilySuse, Command: "sudo zypper install nodejs"},
	},
}

// findNode finds the node program that runs js-debug: typescript.nodePath, else the first node on PATH. It fails with JsDebugNodeInvalid
// for a setting that is wrong and JsDebugNodeNotFound when there is none; the user is told either way.
func (j *javascript) findNode(env langserver.Env) (string, error) {
	if setting := j.settingString(NodePathSetting); setting != "" {
		if why := langserver.CheckPath(env, setting); why != "" {
			j.tellNode(NodePathSetting + " is " + setting + ": " + why + ". The debugger of JavaScript runs js-debug with node.")
			return "", errors.New("JsDebugNodeInvalid", NodePathSetting+" is set to "+setting+": "+why, "setting", NodePathSetting, "value", setting)
		}
		expanded := env.Expand(setting)
		if strings.Contains(expanded, "/") {
			return env.Abs(setting), nil
		}
		if p, ok := langserver.FindExecutable(expanded, filepath.SplitList(env.Path)); ok {
			return p, nil
		}
		j.tellNode(NodePathSetting + " is " + setting + ", which is not a program in PATH. The debugger of JavaScript runs js-debug with node.")
		return "", errors.New("JsDebugNodeInvalid", NodePathSetting+" is set to "+setting+", which is not a program in PATH", "setting", NodePathSetting, "value", setting)
	}
	if p, ok := langserver.FindExecutable("node", filepath.SplitList(env.Path)); ok {
		return p, nil
	}
	j.tellNode("node was not found. The debugger of JavaScript runs js-debug with it: install Node.js " + minNode + " or newer, or set " + NodePathSetting + ".")
	return "", errors.New("JsDebugNodeNotFound", "node was not found in PATH: js-debug is a Node.js program", "path", env.Path)
}

// checkNode runs the node and refuses one older than js-debug needs (JsDebugNodeTooOld) or one that cannot be run (JsDebugNodeFailed).
func (j *javascript) checkNode(ctx context.Context, node, folder string) error {
	version, err := langserver.QueryVersion(ctx, node, langserver.VersionCheck{Args: []string{"--version"}}, folder, nil)
	if err != nil {
		j.tellNode(node + " cannot be run: " + err.Error())
		return errors.New("JsDebugNodeFailed", "node cannot be run: "+err.Error(), "node", node)
	}
	if langserver.CompareVersions(version, minNode) < 0 {
		j.tellNode("node " + version + " is too old for js-debug: install Node.js " + minNode + " or newer, or set " + NodePathSetting + ".")
		return errors.New("JsDebugNodeTooOld", "node "+version+" is older than "+minNode+", which js-debug needs", "node", node, "version", version)
	}
	return nil
}

// tellNode tells the user that node cannot be used, with a button that copies the command that installs it for this system. It may be
// called from any goroutine.
func (j *javascript) tellNode(message string) {
	var actions []extension.NotificationAction
	if cmd, ok := nodeInstall.CommandFor(langserver.Family(j.osRelease())); ok {
		actions = append(actions, extension.NotificationAction{Label: "Copy Install Command", Run: func() { j.copyInstall(cmd.Command) }})
	}
	j.api.Notifications().Warn(message, actions...)
}

// copyInstall puts the install command on the clipboard. UI thread.
func (j *javascript) copyInstall(command string) {
	if err := j.api.Clipboard().WriteText(command); err != nil {
		j.api.Notifications().Error("The command cannot be copied: " + err.Error() + ". Run it yourself: " + command)
		return
	}
	j.api.Notifications().Info("Copied: " + command)
}

// tellJsDebug tells the user that js-debug is missing, with a button that downloads it (the click is the user's consent, D4). It may be
// called from any goroutine.
func (j *javascript) tellJsDebug(message string) {
	j.api.Notifications().Warn(message,
		extension.NotificationAction{Label: "Download", Run: func() { j.startDownload() }})
}

// debugAdapter is the descriptor factory (createDebugAdapterDescriptor): node runs js-debug's server, which listens on a port of this
// machine and prints it; Emetos connects to it, and so do the child sessions of the session. It may run on any goroutine. It fails with
// JsDebugPathInvalid, JsDebugNotFound and the errors of findNode and checkNode.
func (j *javascript) debugAdapter(ctx context.Context, session extension.DebugSession, _ *extension.DebugAdapter) (*extension.DebugAdapter, error) {
	folder := j.folderOf(session.Folder())
	env := j.env(folder)
	setting := j.settingString(AdapterPathSetting)
	if setting != "" {
		setting = env.Abs(setting)
	}
	dir, derr := j.dataDir()
	if derr != nil {
		// the folder of the downloads is unknown: only the setting can say where js-debug is
		j.api.Log().Debug("the data folder of the downloads is unknown", "err", derr)
		dir = ""
	}
	program, found, err := locate(setting, dir)
	if err != nil {
		j.tellJsDebug("debug.javascript.adapterPath is " + setting + ", which is not js-debug. Point it at a js-debug release, or clear it and download one.")
		return nil, err
	}
	if !found {
		j.tellJsDebug("js-debug was not found. It debugs JavaScript and TypeScript programs. Download js-debug " + j.release.Version + " (from github.com/microsoft/vscode-js-debug), or set debug.javascript.adapterPath.")
		return nil, errors.New("JsDebugNotFound", "js-debug was not found: set debug.javascript.adapterPath or run \"JavaScript Debugger: Download\"", "dataDir", dir)
	}
	node, err := j.findNode(env)
	if err != nil {
		return nil, err
	}
	if err := j.checkNode(ctx, node, filepath.Dir(program)); err != nil {
		return nil, err
	}
	// port 0: a free one; the server prints "Debug server listening at 127.0.0.1:PORT"
	return &extension.DebugAdapter{
		Kind: extension.DebugAdapterExecutableServer, Command: node, Args: []string{program, "0", "127.0.0.1"}, Cwd: filepath.Dir(program),
		ServesChildren: true,
	}, nil
}
