package csharp

import "github.com/rm4n0s/emetos/extension"

// The settings of the C# pack (SPEC §10.34 "As built (step 24b.7)"): the ones csharp-ls reads (its README names them csharp.*), the path of
// the program, and the solution to load. Every one is read when the server starts, so a change of any restarts it.

// The keys of the settings.
const (
	// PathSetting holds the path of the csharp-ls program.
	PathSetting = "csharp.languageServer.path"
	// TraceSetting traces the traffic with the server (the host's).
	TraceSetting = "csharp.trace.server"
	// LogLevelSetting is the log level of csharp-ls (--loglevel and csharp.logLevel).
	LogLevelSetting = "csharp.logLevel"
	// AnalyzersSetting runs the Roslyn analyzers as part of the diagnostics.
	AnalyzersSetting = "csharp.analyzersEnabled"
	// ApplyFormattingSetting uses the formatting options of the editor, which may override the .editorconfig of the project.
	ApplyFormattingSetting = "csharp.applyFormattingOptions"
	// LocaleSetting forces the language of the messages of the server (--locale).
	LocaleSetting = "csharp.locale"
)

// defaultLogLevel is the level csharp-ls logs at when not told.
const defaultLogLevel = "info"

// settingSpecs are the settings the manifest declares besides the path of the server (langserver.Config.SettingSpecs).
func settingSpecs() []extension.SettingSpec {
	return []extension.SettingSpec{
		{
			Key: SolutionSetting, Type: "string", Default: "", Category: category,
			Description: "The solution (.sln, .slnx) or project (.csproj) csharp-ls loads, a path relative to the folder. When empty, the solution the folder holds is loaded, or the " +
				"project; \"C#: Select Solution\" chooses for a folder and takes precedence over this setting.",
		},
		{
			Key: LogLevelSetting, Type: "string", Default: defaultLogLevel, Enum: []any{"trace", "debug", "info", "warning", "error"}, Category: category,
			Description: "How much csharp-ls writes to its Output channel.",
		},
		{
			Key: AnalyzersSetting, Type: "boolean", Default: false, Category: category,
			Description: "Run the Roslyn analyzers (the style rules of the IDE, the analyzers of NuGet packages) as part of the diagnostics. It makes the diagnostics slower and uses more of the processor.",
		},
		{
			Key: ApplyFormattingSetting, Type: "boolean", Default: false, Category: category,
			Description: "Format with the tab size and indentation of the editor, which can override the .editorconfig of the project.",
		},
		{
			Key: LocaleSetting, Type: "string", Default: "", Category: category,
			Description: "The language of the messages and diagnostics of csharp-ls, such as en-US or de. When empty, the language of the system.",
		},
	}
}

// category is the heading of the settings in the Settings editor.
const category = "C#"
