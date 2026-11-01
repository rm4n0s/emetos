package python

import "github.com/rm4n0s/emetos/extensions/internal/langserver"

// The language servers of the Python pack (SPEC §10.34 "Python"): basedpyright by default, Pyright or python-lsp-server on the user's
// choice (python.languageServer), or none. The pack finds the program and never installs one (D47); Pylance is not offered (D49).

// The ids and the values of the settings the pack reads.
const (
	// ID is the id of the extension, and ServerID the id of its language server in the manifest: one server, whichever program runs it.
	ID       = "emetos.python"
	ServerID = "python"
	// LanguageServerSetting chooses the program: ServerBasedpyright, ServerPyright, ServerPylsp or ServerNone.
	LanguageServerSetting = "python.languageServer"
	// DefaultInterpreterSetting names the interpreter used when the user chose none for the folder.
	DefaultInterpreterSetting = "python.defaultInterpreterPath"
	// TraceSetting traces the traffic with the server (the host's).
	TraceSetting = "python.trace.server"
	// The path settings of the three programs.
	BasedpyrightPathSetting = "basedpyright.path"
	PyrightPathSetting      = "pyright.path"
	PylspPathSetting        = "pylsp.path"
	// StateKey is the key of the state under which the interpreter the user chose for the folder is kept (extension.State).
	StateKey = "python.interpreter"
)

// The values of python.languageServer.
const (
	ServerBasedpyright = "basedpyright"
	ServerPyright      = "pyright"
	ServerPylsp        = "pylsp"
	ServerNone         = "none"
)

// rootMarkers are the files that mark the root of a Python project: the server is started in the nearest folder that holds one.
var rootMarkers = []string{"pyproject.toml", "pyrightconfig.json", "setup.py", "setup.cfg", "Pipfile"}

// selectServer is Config.Select: the index of the program python.languageServer names in Config.Programs (basedpyright for a value that is
// none of them; none is Config.Disabled's).
func selectServer(s *langserver.Settings) int {
	switch s.String(LanguageServerSetting) {
	case ServerPyright:
		return 1
	case ServerPylsp:
		return 2
	}
	return 0
}

// disabled is Config.Disabled: python.languageServer is none.
func disabled(s *langserver.Settings) string {
	if s.String(LanguageServerSetting) == ServerNone {
		return LanguageServerSetting + " is none"
	}
	return ""
}

// serverConfig describes the three servers for extensions/internal/langserver.
func (p *python) serverConfig() *langserver.Config {
	return &langserver.Config{
		ID: ServerID, Name: "Python", Languages: []string{"python"}, Category: "Python",
		Purpose: "type checking, diagnostics, completion, hover, go to definition, rename and code actions for Python",
		Programs: []langserver.Program{
			{
				Name: "basedpyright", Exe: "basedpyright-langserver", Args: []string{"--stdio"}, PathSetting: BasedpyrightPathSetting, Dirs: p.interpreterDirs,
				// basedpyright-langserver has no --version: its sibling basedpyright prints "basedpyright 1.40.2"
				Version: langserver.VersionCheck{Args: []string{"--version"}, Exe: "basedpyright"},
				Install: langserver.Install{
					Docs: "https://docs.basedpyright.com/latest/installation/command-line-and-language-server/",
					Commands: []langserver.InstallCommand{
						{Family: langserver.FamilyAny, Command: "pipx install basedpyright"},
					},
				},
			},
			{
				Name: "pyright", Exe: "pyright-langserver", Args: []string{"--stdio"}, PathSetting: PyrightPathSetting, Dirs: p.interpreterDirs,
				Version: langserver.VersionCheck{Args: []string{"--version"}, Exe: "pyright"},
				Install: langserver.Install{
					Docs: "https://microsoft.github.io/pyright/#/installation",
					Commands: []langserver.InstallCommand{
						{Family: langserver.FamilyArch, Command: "sudo pacman -S pyright"},
						{Family: langserver.FamilyAny, Command: "npm install -g pyright"},
					},
				},
			},
			{
				Name: "pylsp", PathSetting: PylspPathSetting, Dirs: p.interpreterDirs,
				// pylsp --version prints "pylsp v1.15.0"
				Version: langserver.VersionCheck{Args: []string{"--version"}},
				Install: langserver.Install{
					Docs: "https://github.com/python-lsp/python-lsp-server#installation",
					Commands: []langserver.InstallCommand{
						{Family: langserver.FamilyFedora, Command: "sudo dnf install python3-lsp-server"},
						{Family: langserver.FamilyDebian, Command: "sudo apt install python3-pylsp"},
						{Family: langserver.FamilyArch, Command: "sudo pacman -S python-lsp-server"},
						{Family: langserver.FamilySuse, Command: "sudo zypper install python3-python-lsp-server"},
						{Family: langserver.FamilyAny, Command: "pipx install python-lsp-server"},
					},
				},
			},
		},
		Select:      selectServer,
		Disabled:    disabled,
		RootMarkers: rootMarkers,
		Customize:   p.customize,
		TraceKey:    TraceSetting,
		// the files whose change the servers need to know about (they read the project configuration again)
		WatchedFiles:  []string{"**/pyrightconfig.json", "**/pyproject.toml"},
		SettingsQuery: "python",
		Menu: []langserver.MenuEntry{
			{Label: "$(python) Select Interpreter...", Description: "the Python the server, the debugger and the tests use", Run: func() { p.interps.Select() }},
		},
	}
}

// interpreterDirs is Program.Dirs: the folder of the interpreter's programs is searched after PATH, so that a server installed in the
// environment the project uses is found (SPEC §10.34 "Python"). It runs on the launcher's goroutine.
func (p *python) interpreterDirs(env langserver.Env) (before, after []string) {
	if i, _, ok := p.interps.Resolve(env); ok {
		after = append(after, i.BinDir())
	}
	return nil, after
}

// customize is Config.Customize: it gives the server the interpreter and the settings that are not at their default. It runs on the
// launcher's goroutine.
func (p *python) customize(l *langserver.Launch, env langserver.Env) error {
	i, _, ok := p.interps.Resolve(env)
	if ok {
		i = Describe(p.ctx, env, i)
	}
	p.interps.api.Post(p.interps.Refresh) // the status item shows the interpreter this start used
	switch l.Program.Name {
	case ServerPylsp:
		l.InitializationOptions = map[string]any{"pylsp": pylspOptions(env.Settings, i, ok)}
	case ServerPyright:
		python := nonDefault(env.Settings, pyrightSettings(), "python")
		if ok {
			python["pythonPath"] = i.Path
		}
		l.Configuration = map[string]any{"python": python, "pyright": nonDefault(env.Settings, pyrightSettings(), "pyright")}
	default:
		python := map[string]any{}
		if ok {
			python["pythonPath"] = i.Path
		}
		l.Configuration = map[string]any{
			"python": python, "basedpyright": nonDefault(env.Settings, basedpyrightSettings(), "basedpyright"), "pyright": map[string]any{},
		}
	}
	return nil
}

// pylspOptions are the options python-lsp-server starts with: the settings that are not at their default, below "plugins" and
// "signature", with the interpreter as the environment of Jedi unless the user named one.
func pylspOptions(s *langserver.Settings, i Interpreter, haveInterpreter bool) map[string]any {
	opts := nonDefault(s, pylspSettings(), "pylsp")
	if haveInterpreter {
		if s.String("pylsp.plugins.jedi.environment") == "" {
			langserver.SetNested(opts, []string{"plugins", "jedi", "environment"}, i.Path)
		}
	}
	return opts
}
