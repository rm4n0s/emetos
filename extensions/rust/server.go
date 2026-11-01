package rust

import (
	"github.com/rm4n0s/errors"

	"github.com/rm4n0s/emetos/extensions/internal/langserver"
)

// serverConfig describes rust-analyzer for extensions/internal/langserver (SPEC §10.34 "Rust").
func (r *rust) serverConfig() *langserver.Config {
	return &langserver.Config{
		ID: ServerID, Name: "rust-analyzer", Languages: rustLanguages, Category: "Rust",
		Purpose: "completion, diagnostics from cargo check, hover, go to definition, rename, inlay hints and code actions for Rust",
		Programs: []langserver.Program{{
			Name: "rust-analyzer", PathSetting: PathSetting,
			// the folder rustup and cargo install into; the program there may be rustup's proxy (see customize)
			After:   []string{"~/.cargo/bin"},
			Version: langserver.VersionCheck{Args: []string{"--version"}},
			Install: langserver.Install{
				Docs: "https://rust-analyzer.github.io/book/installation.html",
				Commands: []langserver.InstallCommand{
					{Family: langserver.FamilyFedora, Command: "sudo dnf install rust-analyzer"},
					{Family: langserver.FamilyDebian, Command: "sudo apt install rust-analyzer"},
					{Family: langserver.FamilyArch, Command: "sudo pacman -S rust-analyzer"},
					{Family: langserver.FamilyAny, Command: "rustup component add rust-analyzer"},
				},
			},
		}},
		RootMarkers: []string{"Cargo.toml"},
		Customize:   customize,
		TraceKey:    TraceSetting,
		// the files whose change rust-analyzer needs to know about, besides those it registers itself
		WatchedFiles:  []string{"**/Cargo.toml", "**/Cargo.lock", "**/rust-project.json", "**/rust-toolchain", "**/rust-toolchain.toml", "**/.cargo/config", "**/.cargo/config.toml"},
		SettingsQuery: "rust-analyzer",
		Menu: []langserver.MenuEntry{
			{Label: "$(refresh) Reload Workspace", Description: "read Cargo.toml and the build scripts again", Run: r.reloadWorkspace},
			{Label: "$(file-code) Open Cargo.toml", Description: "of the package of the file in front", Run: r.openCargoToml},
		},
	}
}

// clientExperimental is what Emetos tells rust-analyzer about itself in the "experimental" member of its capabilities: it hears
// experimental/serverStatus, and it runs none of the commands rust-analyzer's own lenses would name (the Run lenses are the pack's,
// see lens.go, so that they run through the Tasks extension; the lenses that show references need commands Emetos does not have).
var clientExperimental = map[string]any{
	"serverStatusNotification": true,
	"commands":                 map[string]any{"commands": []string{}},
}

// customize sets what rust-analyzer starts with from the settings: they are its initialization options and the answer to
// workspace/configuration for the section rust-analyzer, as VS Code's extension sends them. A program that does not print its
// version is refused: rustup's proxy in ~/.cargo/bin is found first, and without the component it prints an error and nothing else.
func customize(l *langserver.Launch, env langserver.Env) error {
	if l.VersionErr != nil {
		return errors.New("RustAnalyzerVersionFailed",
			"rust-analyzer did not print its version ("+l.VersionErr.Error()+"); if "+l.Found.Path+" is rustup's, run: rustup component add rust-analyzer",
			"path", l.Found.Path)
	}
	opts := serverOptions(env.Settings)
	l.InitializationOptions = opts
	l.Configuration = map[string]any{"rust-analyzer": opts}
	l.ClientExperimental = clientExperimental
	return nil
}
