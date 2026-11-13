package javascript

import (
	"context"
	"os"
	"path/filepath"

	"github.com/rm4n0s/emetos/extension"
	"github.com/rm4n0s/emetos/extensions/internal/langserver"
)

// What the JavaScript extension needs from the system (SPEC §10.36, step 34.2): node, which runs js-debug and the tests of Node, Jest
// and Vitest, and js-debug itself, found where the debugger looks for them (nodeFind, locate).

// The ids of the requirements of the extension.
const (
	RequirementNode    = "node"
	RequirementJsDebug = "js-debug"
)

// requirements are the requirements of the manifest.
func requirements() []extension.Requirement {
	return []extension.Requirement{
		{
			ID: RequirementNode, Name: "Node.js", Kind: extension.RequirementProgram,
			Purpose: "runs the JavaScript debugger and the tests of Node, Jest and Vitest (" + minNode + " or newer)",
			Install: nodeInstall, PathSetting: NodePathSetting, Check: checkNode,
		},
		{
			ID: RequirementJsDebug, Name: "js-debug", Kind: extension.RequirementProgram, Optional: true,
			Purpose:       "debugs JavaScript and TypeScript programs",
			Install:       extension.Install{Docs: "https://github.com/microsoft/vscode-js-debug/releases/tag/v" + JsDebugVersion},
			InstallAction: DownloadCommand, PathSetting: AdapterPathSetting, Check: checkJsDebug,
		},
	}
}

// envOf is the langserver.Env of a check: this machine and the folder.
func envOf(env extension.RequirementEnv) langserver.Env {
	home, err := os.UserHomeDir()
	if err != nil {
		home = ""
	}
	return langserver.Env{Folder: env.Folder, Home: home, Path: os.Getenv("PATH")}
}

// checkNode is the check of node.
func checkNode(ctx context.Context, env extension.RequirementEnv) extension.RequirementResult {
	e := envOf(env)
	setting := env.StringSetting(NodePathSetting)
	res := langserver.CheckProgram(ctx, langserver.ProgramCheck{
		Name: "node", Find: func() (string, error) { return nodeFind(e, setting) },
		Version: langserver.VersionCheck{Args: []string{"--version"}, Min: minNode}, Folder: env.Folder,
	})
	if res.State == extension.RequirementFound && setting != "" && res.Detail == "" {
		res.Detail = "found through " + NodePathSetting
	}
	return res
}

// checkJsDebug is the check of js-debug: debug.javascript.adapterPath, else the newest release downloaded into Emetos's data folder.
func checkJsDebug(_ context.Context, env extension.RequirementEnv) extension.RequirementResult {
	setting := env.StringSetting(AdapterPathSetting)
	if setting != "" {
		setting = envOf(env).Abs(setting)
	}
	dir, err := dataDirFromEnv()
	if err != nil {
		dir = ""
	}
	program, found, err := locate(setting, dir)
	switch {
	case err != nil:
		return extension.RequirementResult{State: extension.RequirementUnusable, Path: setting, Detail: AdapterPathSetting + " is " + setting + ", which is not js-debug"}
	case !found:
		return extension.RequirementResult{State: extension.RequirementMissing, Detail: "js-debug was not downloaded: run \"JavaScript Debugger: Download\" or set " + AdapterPathSetting}
	}
	res := extension.RequirementResult{State: extension.RequirementFound, Path: program}
	if setting != "" {
		res.Detail = "found through " + AdapterPathSetting
	} else if rel, err := filepath.Rel(dir, program); err == nil {
		res.Version = firstElement(filepath.ToSlash(rel)) // the folder of the release is its version
	}
	return res
}

// firstElement is the first element of a slashed path.
func firstElement(p string) string {
	for i := range len(p) {
		if p[i] == '/' {
			return p[:i]
		}
	}
	return p
}
