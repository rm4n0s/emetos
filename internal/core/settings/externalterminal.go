package settings

// categoryExternalTerminal is the group of the settings of the external terminal.
const categoryExternalTerminal = "External Terminal"

// registerExternalTerminal registers the settings that decide which terminal "Open in Terminal" of a repository of the Source Control view
// opens (SPEC §10.30, step 24.23), with VS Code's keys, descriptions and defaults: terminal.sourceControlRepositoriesKind and the program
// of the Linux terminal. VS Code has the same for the Explorer (terminal.explorerKind), which Emetos does not build, and a program each for
// Windows and macOS, which Emetos has no use for.
//
// Ported from VS Code: src/vs/workbench/contrib/externalTerminal/electron-browser/externalTerminal.contribution.ts
func registerExternalTerminal(r *Registry) {
	r.MustRegister(
		Setting{
			Key: "terminal.sourceControlRepositoriesKind", Types: []Type{String}, Default: "integrated", Enum: []any{"integrated", "external", "both"},
			EnumDescriptions: []string{
				"Show the integrated terminal action.",
				"Show the external terminal action.",
				"Show both integrated and external terminal actions.",
			},
			Description: "When opening a repository from the Source Control Repositories view in a terminal, determines what kind of terminal will be launched",
			Scope:       ScopeWindow, Category: categoryExternalTerminal,
		},
		Setting{
			Key: "terminal.external.linuxExec", Types: []Type{String}, Default: "",
			Description: "Customizes which terminal to run on Linux. Empty chooses one from the desktop session (x-terminal-emulator on Debian, gnome-terminal under GNOME, konsole under KDE Plasma, else the program named by $COLORTERM or $TERM, else xterm).",
			Scope:       ScopeApplication, Category: categoryExternalTerminal,
		},
	)
}
