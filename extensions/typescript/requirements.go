package typescript

import (
	"context"
	"os"

	"github.com/rm4n0s/errors"

	"github.com/rm4n0s/emetos/extension"
	"github.com/rm4n0s/emetos/extensions/internal/langserver"
)

// What the TypeScript extension needs from the system besides its language server (SPEC §10.36, step 34.2): node, which runs
// typescript-language-server (resolveNode), and the TypeScript package the server is told to use (Decide). The server's own requirement
// does not run Customize, which is where a start checks these two.

// The ids of the requirements of the extension besides the server's.
const (
	RequirementNode       = "node"
	RequirementTypeScript = "typescript-package"
)

// minServerNode is the oldest node typescript-language-server runs on.
const minServerNode = "22.22.2"

// requirements are the requirements of the manifest besides the server's.
func (t *typescript) requirements() []extension.Requirement {
	program := t.serverConfig().Programs[0]
	return []extension.Requirement{
		{
			ID: RequirementNode, Name: "Node.js", Kind: extension.RequirementProgram,
			Purpose: "runs typescript-language-server (" + minServerNode + " or newer)",
			Install: extension.Install{Docs: "https://nodejs.org/en/download", Commands: []extension.InstallCommand{
				{Family: extension.FamilyFedora, Command: "sudo dnf install nodejs"},
				{Family: extension.FamilyDebian, Command: "sudo apt install nodejs"},
				{Family: extension.FamilyArch, Command: "sudo pacman -S nodejs"},
				{Family: extension.FamilySuse, Command: "sudo zypper install nodejs"},
				{Family: extension.FamilyMacOS, Command: "brew install node"},
			}},
			PathSetting: NodePathSetting, Check: checkServerNode,
		},
		{
			ID: RequirementTypeScript, Name: "TypeScript", Kind: extension.RequirementProgram,
			Purpose: "the compiler typescript-language-server runs (tsserver.js)",
			Install: extension.Install{Docs: "https://www.typescriptlang.org/download/", Commands: []extension.InstallCommand{
				{Family: extension.FamilyAny, Command: "npm install -g typescript@6"},
			}},
			PathSetting: TsdkSetting,
			Check: func(_ context.Context, env extension.RequirementEnv) extension.RequirementResult {
				return checkTypeScript(env, program)
			},
		},
	}
}

// envOf is the langserver.Env of a check: this machine, the folder and the settings of the env.
func envOf(env extension.RequirementEnv) langserver.Env {
	get := env.Setting
	if get == nil {
		get = func(string) (any, bool) { return nil, false }
	}
	return langserver.DefaultEnv(env.Folder, langserver.NewSettings(get))
}

// checkServerNode is the check of the node that runs the server.
func checkServerNode(ctx context.Context, env extension.RequirementEnv) extension.RequirementResult {
	e := envOf(env)
	setting := env.StringSetting(NodePathSetting)
	return langserver.CheckProgram(ctx, langserver.ProgramCheck{
		Name: "node", Find: func() (string, error) { return resolveNode(e, setting) },
		Version: langserver.VersionCheck{Args: []string{"--version"}, Min: minServerNode}, Folder: env.Folder,
	})
}

// checkTypeScript is the check of the TypeScript package: the one Decide would give the server, from typescript.tsdk, the package beside
// the server, or the folder's own when the user chose it. It reads files and runs nothing.
func checkTypeScript(env extension.RequirementEnv, program langserver.Program) extension.RequirementResult {
	e := envOf(env)
	server := ""
	if found, err := langserver.Find(program, e); err == nil {
		server = found.Path
	}
	var chosen string
	env.StateValue(StateKey, &chosen)
	home, _ := os.UserHomeDir()
	plan, err := Decide(e, server, langserver.ResolveRoot(env.Folder, rootMarkers, home), env.StringSetting(TsdkSetting), chosen)
	if err != nil {
		appErr, _ := errors.FromError(err)
		switch {
		case appErr != nil && appErr.Tag == "TypeScriptTsdkInvalid":
			return extension.RequirementResult{State: extension.RequirementUnusable, Detail: appErr.Message}
		case server == "":
			return extension.RequirementResult{State: extension.RequirementUnknown, Detail: "typescript-language-server was not found, and the TypeScript it runs is looked for beside it; typescript.tsdk is not set"}
		case appErr != nil:
			return extension.RequirementResult{State: extension.RequirementMissing, Detail: appErr.Message}
		}
		return extension.RequirementResult{State: extension.RequirementMissing, Detail: err.Error()}
	}
	return extension.RequirementResult{State: extension.RequirementFound, Path: plan.Use.Lib, Version: plan.Use.Number, Detail: plan.Use.Label()}
}
