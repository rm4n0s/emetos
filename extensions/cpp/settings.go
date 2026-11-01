package cpp

import "github.com/rm4n0s/emetos/extension"

// The settings of clangd (SPEC §10.34 "C and C++"): the names and meanings of those of vscode-clangd that change what the server does.
// The descriptions are written for Emetos. clangd.path is the finder's (langserver.Config.SettingSpecs) and clangd.trace.server the
// host's (Config.TraceKey). A change of any of them restarts the server (SPEC §10.34 "As built (step 24b.1)").

// settingsCategory is the group of the settings editor.
const settingsCategory = "clangd"

// settingSpecs are the settings the manifest declares.
func settingSpecs() []extension.SettingSpec {
	return []extension.SettingSpec{
		{
			Key: ArgumentsSetting, Type: "array", Default: []any{}, Category: settingsCategory,
			// the arguments decide what clangd may run (--query-driver) and where it writes (--log, --pch-storage): only the user's
			// settings count, not a folder's
			Scope: "machine",
			Description: "Arguments clangd is started with, one in each entry (clangd --help lists them), such as --background-index, " +
				"--clang-tidy, --header-insertion=never or --compile-commands-dir=build. Only the user's settings are read.",
		},
		{
			Key: FallbackFlagsSetting, Type: "array", Default: []any{}, Category: settingsCategory,
			Description: "Extra compiler flags clangd parses a file with when the project has no compilation database " +
				"(compile_commands.json) and no compile_flags.txt, such as -std=c++20 or -I include.",
		},
		{
			Key: TraceFileSetting, Type: "string", Default: "", Category: settingsCategory, Scope: "machine",
			Description: "A file clangd writes a performance trace to, in the format of Chrome's trace viewer (CLANGD_TRACE). " +
				"Empty for no trace. Only the user's settings are read.",
		},
	}
}
