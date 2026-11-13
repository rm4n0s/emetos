package golang

import (
	"context"
	"path/filepath"

	"github.com/rm4n0s/emetos/extension"
	"github.com/rm4n0s/emetos/extensions/internal/langserver"
)

// What the Go extension needs from the system (SPEC §10.36, step 34.2): go, gopls, dlv and the lint tool of go.lintTool, each found
// by the finder the extension uses when it runs it (find.go).

// The ids of the requirements of the extension.
const (
	RequirementGo       = "go"
	RequirementGopls    = "gopls"
	RequirementDlv      = "dlv"
	RequirementLintTool = "lint-tool"
)

// goInstall says how to get Go.
var goInstall = extension.Install{
	Docs: "https://go.dev/doc/install",
	Commands: []extension.InstallCommand{
		{Family: extension.FamilyFedora, Command: "sudo dnf install golang"},
		{Family: extension.FamilyDebian, Command: "sudo apt install golang-go"},
		{Family: extension.FamilyArch, Command: "sudo pacman -S go"},
		{Family: extension.FamilySuse, Command: "sudo zypper install go"},
		{Family: extension.FamilyMacOS, Command: "brew install go"},
	},
}

// toolInstall says how to get a tool of allTools: its go install command.
func toolInstall(name, docs string) extension.Install {
	t, _ := toolByName(name)
	return extension.Install{Docs: docs, Commands: []extension.InstallCommand{{Family: extension.FamilyAny, Command: "go install " + t.Package + "@" + t.Version}}}
}

// lintInstalls say how to get each lint tool of go.lintTool.
var lintInstalls = map[string]extension.Install{
	"staticcheck":      toolInstall("staticcheck", "https://staticcheck.dev/docs/getting-started/"),
	"golangci-lint":    {Docs: "https://golangci-lint.run/welcome/install/", Commands: []extension.InstallCommand{{Family: extension.FamilyAny, Command: "go install github.com/golangci/golangci-lint/cmd/golangci-lint@v1.64.8"}}},
	"golangci-lint-v2": {Docs: "https://golangci-lint.run/welcome/install/", Commands: []extension.InstallCommand{{Family: extension.FamilyAny, Command: "go install github.com/golangci/golangci-lint/v2/cmd/golangci-lint@latest"}}},
	"revive":           {Docs: "https://revive.run/", Commands: []extension.InstallCommand{{Family: extension.FamilyAny, Command: "go install github.com/mgechev/revive@latest"}}},
}

// requirements are the requirements of the manifest.
func requirements() []extension.Requirement {
	return []extension.Requirement{
		{
			ID: RequirementGo, Name: "Go", Kind: extension.RequirementProgram,
			Purpose: "builds, runs, tests and vets Go code, and installs the Go tools",
			Install: goInstall, Check: checkGo,
		},
		{
			ID: RequirementGopls, Name: "gopls", Kind: extension.RequirementProgram,
			Purpose: "completion, hover, diagnostics, formatting, rename and the other language features of Go",
			Install: toolInstall(gopls, "https://go.dev/gopls/"), InstallAction: InstallToolsCommand, PathSetting: GoplsPathSetting,
			Check: checkGopls,
		},
		{
			ID: RequirementDlv, Name: "dlv", Kind: extension.RequirementProgram, Optional: true,
			Purpose: "debugs Go programs and tests",
			Install: toolInstall("dlv", "https://github.com/go-delve/delve/tree/master/Documentation/installation"), InstallAction: InstallToolsCommand,
			Check: checkDlv,
		},
		{
			ID: RequirementLintTool, Name: "Go lint tool", Kind: extension.RequirementProgram, Optional: true,
			Purpose: "lints Go code (go.lintTool: staticcheck by default)",
			Install: lintInstalls[defaultLintTool],
			Check:   checkLintTool,
		},
	}
}

// checkGo is the check of go.
func checkGo(ctx context.Context, env extension.RequirementEnv) extension.RequirementResult {
	s := goSettings(env.Setting)
	missing := "go was not found in PATH"
	if alt := s.alternateTool("go"); alt != "" {
		missing = "go.alternateTools names " + alt + " for go, which cannot be run"
	}
	return langserver.CheckProgram(ctx, langserver.ProgramCheck{
		Name: "go", Find: func() (string, error) { return findGo(s, env.Folder) }, Missing: missing,
		Version: langserver.VersionCheck{Args: []string{"version"}}, Folder: env.Folder,
	})
}

// checkGopls is the check of gopls, which the host starts from go.goplsPath or PATH only: a gopls that "Go: Install/Update Tools" put
// in a folder that is not in PATH cannot be used until the folder is in PATH or go.goplsPath names it.
func checkGopls(ctx context.Context, env extension.RequirementEnv) extension.RequirementResult {
	s := goSettings(env.Setting)
	set := s.str(GoplsPathSetting)
	missing := "gopls was not found in PATH"
	if set != "" {
		missing = "go.goplsPath is set to " + set + ", which cannot be run"
	}
	res := langserver.CheckProgram(ctx, langserver.ProgramCheck{
		Name: gopls, Find: func() (string, error) { return findGopls(s, env.Folder) }, Missing: missing,
		Version: langserver.VersionCheck{Args: []string{"version"}}, Folder: env.Folder,
	})
	if res.State == extension.RequirementMissing && set == "" {
		if path := inToolDirs(s, gopls); path != "" {
			res = extension.RequirementResult{State: extension.RequirementUnusable, Path: path,
				Detail: "gopls is in " + filepath.Dir(path) + ", which is not in PATH: add the folder to PATH or set go.goplsPath"}
		}
	}
	if res.State == extension.RequirementFound && set != "" && res.Detail == "" {
		res.Detail = "found through go.goplsPath"
	}
	return res
}

// checkDlv is the check of dlv.
func checkDlv(ctx context.Context, env extension.RequirementEnv) extension.RequirementResult {
	s := goSettings(env.Setting)
	return langserver.CheckProgram(ctx, langserver.ProgramCheck{
		Name: "dlv", Find: func() (string, error) { return findDlv(s, env.Folder) },
		Version: langserver.VersionCheck{Args: []string{"version"}}, Folder: env.Folder,
	})
}

// checkLintTool is the check of the lint tool go.lintTool names; its result names the tool and gives its install command.
func checkLintTool(ctx context.Context, env extension.RequirementEnv) extension.RequirementResult {
	s := goSettings(env.Setting)
	tool := s.lintTool()
	name := lintToolName(s, tool)
	versionArgs := []string{"-version"}
	if lintProgram(tool) == "golangci-lint" {
		versionArgs = []string{"--version"}
	}
	res := langserver.CheckProgram(ctx, langserver.ProgramCheck{
		Name: tool, Find: func() (string, error) { return lookProgram(name, env.Folder) },
		Missing: name + " (go.lintTool " + tool + ") was not found in PATH or go.alternateTools",
		Version: langserver.VersionCheck{Args: versionArgs}, Folder: env.Folder,
	})
	if res.State == extension.RequirementFound && res.Detail == "" {
		res.Detail = tool
	}
	if in, ok := lintInstalls[tool]; ok {
		res.Install = in
	}
	return res
}
