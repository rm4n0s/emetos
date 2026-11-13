package csharp

import (
	"context"

	"github.com/rm4n0s/emetos/extension"
	"github.com/rm4n0s/emetos/extensions/internal/langserver"
)

// What the C# extension needs from the system besides csharp-ls (SPEC §10.36, step 34.2): the .NET SDK csharp-ls runs on, found where
// the launcher of a .NET application looks (findDotnet). Nothing is run: `dotnet --version` would make the SDK write to the home folder on
// its first run, so the result has no version.

// RequirementDotnet is the id of the requirement of the .NET SDK.
const RequirementDotnet = "dotnet"

// requirements are the requirements of the manifest besides the server's.
func (c *csharp) requirements() []extension.Requirement {
	return []extension.Requirement{{
		ID: RequirementDotnet, Name: ".NET SDK", Kind: extension.RequirementProgram,
		Purpose: "runs csharp-ls, the C# language server (the .NET 10 SDK)",
		Install: extension.Install{Docs: "https://dotnet.microsoft.com/download", Commands: []extension.InstallCommand{
			{Family: extension.FamilyFedora, Command: "sudo dnf install dotnet-sdk-10.0"},
			{Family: extension.FamilyDebian, Command: "sudo apt install dotnet-sdk-10.0"},
			{Family: extension.FamilyArch, Command: "sudo pacman -S dotnet-sdk"},
			{Family: extension.FamilyMacOS, Command: "brew install --cask dotnet-sdk"},
		}},
		Check: func(_ context.Context, env extension.RequirementEnv) extension.RequirementResult {
			path, from, ok := c.findDotnet(langserver.DefaultEnv(env.Folder, nil))
			if !ok {
				return extension.RequirementResult{State: extension.RequirementMissing, Detail: "dotnet was not found in DOTNET_ROOT, PATH or the folders .NET is installed in"}
			}
			return extension.RequirementResult{State: extension.RequirementFound, Path: path, Detail: "found through " + from}
		},
	}}
}
