package csharp

import (
	"context"
	"strings"

	"github.com/rm4n0s/emetos/extension"
	"github.com/rm4n0s/emetos/extensions/internal/langserver"
)

// csharp-ls for the C# pack (SPEC §10.34 "C#"): the program is `csharp-ls`, a .NET global tool (a small launcher in ~/.dotnet/tools that
// runs the tool's dll with the .NET SDK); it speaks LSP on the standard input and output.

// serverConfig describes csharp-ls for extensions/internal/langserver.
func (c *csharp) serverConfig() *langserver.Config {
	return &langserver.Config{
		ID: ServerID, Name: "C#", Languages: []string{"csharp"}, Category: "C#",
		Purpose: "diagnostics, completion, hover, go to definition (into .NET libraries, decompiled), rename, code actions and formatting for C#, with solutions and projects of the .NET SDK",
		Programs: []langserver.Program{{
			Name: "csharp-ls", PathSetting: PathSetting, Dirs: toolDirs,
			After: []string{"~/.dotnet/tools"},
			// csharp-ls --version prints "csharp-ls, 0.28.0 (Klebiškis)+72671fc7"
			Version: langserver.VersionCheck{Args: []string{"--version"}},
			Install: langserver.Install{
				Docs: "https://github.com/razzmatazz/csharp-language-server#quick-start",
				Commands: []langserver.InstallCommand{
					{Family: langserver.FamilyAny, Command: "dotnet tool install --global csharp-ls"},
				},
			},
		}},
		RootMarkers:      allMarkers,
		CustomizeContext: c.customize,
		TraceKey:         TraceSetting,
		// the files csharp-ls loads its projects from: it reloads the solution when one of them changes
		WatchedFiles:  []string{"**/*.sln", "**/*.slnx", "**/*.csproj", "**/Directory.Build.props", "**/Directory.Build.targets", "**/Directory.Packages.props", "**/global.json", "**/.editorconfig"},
		SettingsQuery: "csharp",
		Menu: []langserver.MenuEntry{
			{Label: "$(root-folder) Select Solution...", Description: "the solution or project csharp-ls loads", Run: c.selectSolution},
		},
	}
}

// The environment variables the server runs with. csharp-ls runs the .NET SDK (MSBuild, restore), which sends usage data to Microsoft and
// prints a welcome banner and writes a sentinel to the home folder on its first run, unless told not to; Emetos sends no telemetry
// (SPEC §12), so every start says so.
var dotnetEnv = []string{"DOTNET_CLI_TELEMETRY_OPTOUT=1", "DOTNET_NOLOGO=1", "DOTNET_SKIP_FIRST_TIME_EXPERIENCE=1"}

// customize is Config.CustomizeContext: it checks the .NET SDK, decides the solution the server loads, and gives the server its settings.
// It runs on the launcher's goroutine.
func (c *csharp) customize(_ context.Context, l *langserver.Launch, env langserver.Env) error {
	if err := c.requireDotnet(env); err != nil {
		return err
	}
	t, err := resolveTarget(env, c.chosen())
	if err != nil {
		return err
	}
	l.Root = t.Root
	if l.Root == "" {
		l.Root = env.Folder
	}
	l.Args = append(l.Args, t.args()...)
	l.Env = append(l.Env, dotnetEnv...)
	s := env.Settings
	level := s.String(LogLevelSetting)
	if level != "" && level != defaultLogLevel {
		l.Args = append(l.Args, "--loglevel", level)
	}
	locale := s.String(LocaleSetting)
	if locale != "" {
		l.Args = append(l.Args, "--locale", locale)
	}
	l.Configuration = map[string]any{"csharp": map[string]any{
		"logLevel":               orDefault(level, defaultLogLevel),
		"analyzersEnabled":       s.Bool(AnalyzersSetting),
		"applyFormattingOptions": s.Bool(ApplyFormattingSetting),
		"useMetadataUris":        true,
	}}
	// csharp-ls answers go to definition on a symbol of a library with a csharp:/ address instead of a path of a temporary file when the
	// client says it can show those; csharp/metadata gives the text (content.go)
	l.ClientExperimental = map[string]any{"csharp": map[string]any{"metadataUris": true}}
	c.mu.Lock()
	c.current, c.ambiguous = t, t.Several
	c.mu.Unlock()
	c.api.Post(c.targetChanged)
	return nil
}

// orDefault is the text, or def when it is empty.
func orDefault(text, def string) string {
	if strings.TrimSpace(text) == "" {
		return def
	}
	return text
}

// chosen is the file the user chose for the folder, kept in the state; "" when none was. It may be called from any goroutine.
func (c *csharp) chosen() string {
	v, _ := extension.StateValue[string](c.api.State(), StateKey)
	return v
}

// targetChanged shows the target the last start used in the tooltip of the item, and tells the user once that there is a choice to make
// when the folder holds several solutions. UI thread.
func (c *csharp) targetChanged() {
	c.mu.Lock()
	t, several := c.current, c.ambiguous
	c.mu.Unlock()
	if c.ctl != nil {
		c.ctl.SetDetail(t.describe(c.api.Scope().Folder))
	}
	if len(several) == 0 || c.offered || c.chosen() != "" {
		return
	}
	c.offered = true
	names := make([]string, len(several))
	for i, f := range several {
		names[i] = relative(c.api.Scope().Folder, f)
	}
	c.api.Notifications().Info("This folder has several solutions ("+strings.Join(names, ", ")+"). csharp-ls loads one of them; choose which.",
		extension.NotificationAction{Label: "Select Solution...", Run: c.selectSolution})
}
