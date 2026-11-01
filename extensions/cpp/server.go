package cpp

import (
	"os"
	"strings"

	"github.com/rm4n0s/emetos/extensions/internal/langserver"
)

// rootMarkers are the files that mark the root of a C or C++ project for clangd: the compilation database, the flags file and the
// project configuration. The server is started in the nearest folder that holds one (langserver.ResolveRoot).
var rootMarkers = []string{"compile_commands.json", "compile_flags.txt", ".clangd"}

// serverConfig describes clangd for extensions/internal/langserver (SPEC §10.34 "C and C++").
func (c *cpp) serverConfig() *langserver.Config {
	return &langserver.Config{
		ID: ServerID, Name: "clangd", Languages: cppLanguages, Category: "clangd",
		Purpose: "completion, diagnostics with fix-its, hover, go to definition, rename, inlay hints, semantic highlighting and code actions for C, C++ and CUDA",
		Programs: []langserver.Program{{
			Name: "clangd", PathSetting: PathSetting,
			// clangd prints "clangd version 22.1.8 (...)"; no feature of the pack needs a version
			Version: langserver.VersionCheck{Args: []string{"--version"}},
			Install: langserver.Install{
				Docs: "https://clangd.llvm.org/installation",
				Commands: []langserver.InstallCommand{
					{Family: langserver.FamilyFedora, Command: "sudo dnf install clang-tools-extra"},
					{Family: langserver.FamilyDebian, Command: "sudo apt install clangd"},
					{Family: langserver.FamilyArch, Command: "sudo pacman -S clang"},
					{Family: langserver.FamilySuse, Command: "sudo zypper install clang-tools"},
				},
			},
		}},
		RootMarkers: rootMarkers,
		Customize:   customize,
		TraceKey:    TraceSetting,
		// the files whose change clangd needs to know about (it reads them again on the next parse)
		WatchedFiles:  []string{"**/compile_commands.json", "**/compile_flags.txt", "**/.clangd", "**/.clang-tidy", "**/.clang-format"},
		SettingsQuery: "clangd",
		Menu: []langserver.MenuEntry{
			{Label: "$(go-to-file) Switch Between Source/Header", Description: "of the file in front", Run: c.switchHeaderSource},
			{Label: "$(gear) Open Project Configuration (.clangd)", Description: "offers to create it when there is none", Run: c.projectConfig},
			{Label: "$(eye) Toggle Inlay Hints", Description: InlayHintsSetting, Run: c.toggleInlayHints},
		},
	}
}

// customize sets what clangd starts with from the settings, as vscode-clangd does: clangd.arguments are its arguments, the file status
// is asked for and clangd.fallbackFlags are its initialization options, and clangd.trace is the file it traces to (CLANGD_TRACE).
func customize(l *langserver.Launch, env langserver.Env) error {
	l.Args = append(l.Args, env.Settings.Strings(ArgumentsSetting)...)
	opts := map[string]any{"clangdFileStatus": true}
	if flags := env.Settings.Strings(FallbackFlagsSetting); len(flags) > 0 {
		opts["fallbackFlags"] = flags
	}
	l.InitializationOptions = opts
	if trace := env.Settings.String(TraceFileSetting); trace != "" {
		l.Env = append(l.Env, "CLANGD_TRACE="+env.Expand(trace))
	}
	return nil
}

// compileCommandsDirArg is the argument that names the folder of the compilation database.
const compileCommandsDirArg = "--compile-commands-dir"

// databaseDir is the folder clangd.arguments name for the compilation database (--compile-commands-dir=DIR or --compile-commands-dir
// DIR), "" for none.
func databaseDir(args []string) string {
	for i, a := range args {
		switch {
		case strings.HasPrefix(a, compileCommandsDirArg+"="):
			return strings.TrimPrefix(a, compileCommandsDirArg+"=")
		case a == compileCommandsDirArg && i+1 < len(args):
			return args[i+1]
		}
	}
	return ""
}

// fileExists tells whether path is a file.
func fileExists(path string) bool {
	info, err := os.Stat(path)
	return err == nil && !info.IsDir()
}
