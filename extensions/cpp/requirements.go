package cpp

import (
	"context"

	"github.com/rm4n0s/emetos/extension"
	"github.com/rm4n0s/emetos/extensions/internal/langserver"
	"github.com/rm4n0s/emetos/extensions/internal/nativedebug"
)

// What the C and C++ extension needs from the system besides clangd (SPEC §10.36, step 34.2): the native debugger, and ctest (which comes
// with CMake) for the tests of the Testing view.

// RequirementCTest is the id of the requirement of ctest.
const RequirementCTest = "ctest"

// requirements are the requirements of the manifest besides the server's.
func requirements() []extension.Requirement {
	return []extension.Requirement{
		nativedebug.Requirement("C and C++"),
		{
			ID: RequirementCTest, Name: "CMake (ctest)", Kind: extension.RequirementProgram, Optional: true,
			Purpose: "configures CMake projects and runs their tests in the Testing view",
			Install: extension.Install{Docs: "https://cmake.org/download/", Commands: []extension.InstallCommand{
				{Family: extension.FamilyFedora, Command: "sudo dnf install cmake"},
				{Family: extension.FamilyDebian, Command: "sudo apt install cmake"},
				{Family: extension.FamilyArch, Command: "sudo pacman -S cmake"},
				{Family: extension.FamilySuse, Command: "sudo zypper install cmake"},
				{Family: extension.FamilyMacOS, Command: "brew install cmake"},
			}},
			Check: func(ctx context.Context, env extension.RequirementEnv) extension.RequirementResult {
				return langserver.CheckProgram(ctx, langserver.ProgramCheck{
					Name: "ctest", Find: findCTest, Missing: "ctest was not found in PATH",
					Version: langserver.VersionCheck{Args: []string{"--version"}}, Folder: env.Folder,
				})
			},
		},
	}
}
