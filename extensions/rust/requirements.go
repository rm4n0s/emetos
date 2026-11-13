package rust

import (
	"context"

	"github.com/rm4n0s/emetos/extension"
	"github.com/rm4n0s/emetos/extensions/internal/langserver"
	"github.com/rm4n0s/emetos/extensions/internal/nativedebug"
)

// What the Rust extension needs from the system besides rust-analyzer (SPEC §10.36, step 34.2): cargo, which builds, runs and tests the
// crate (findCargo), and the native debugger.

// RequirementCargo is the id of the requirement of cargo.
const RequirementCargo = "cargo"

// requirements are the requirements of the manifest besides the server's.
func requirements() []extension.Requirement {
	return []extension.Requirement{
		{
			ID: RequirementCargo, Name: "cargo", Kind: extension.RequirementProgram,
			Purpose: "builds, runs and tests Rust code, and builds the programs the debugger runs",
			Install: extension.Install{Docs: "https://rustup.rs/", Commands: []extension.InstallCommand{
				{Family: extension.FamilyFedora, Command: "sudo dnf install cargo"},
				{Family: extension.FamilyDebian, Command: "sudo apt install cargo"},
				{Family: extension.FamilyArch, Command: "sudo pacman -S rust"},
				{Family: extension.FamilyAny, Command: "curl --proto '=https' --tlsv1.2 -sSf https://sh.rustup.rs | sh"},
				{Family: extension.FamilyMacOS, Command: "brew install rust"},
			}},
			Check: func(ctx context.Context, env extension.RequirementEnv) extension.RequirementResult {
				return langserver.CheckProgram(ctx, langserver.ProgramCheck{
					Name: "cargo", Find: findCargo, Missing: "cargo was not found in PATH or in ~/.cargo/bin",
					Version: langserver.VersionCheck{Args: []string{"--version"}}, Folder: env.Folder,
				})
			},
		},
		nativedebug.Requirement("Rust"),
	}
}
