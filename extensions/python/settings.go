package python

import (
	"strings"

	"github.com/rm4n0s/emetos/extension"
	"github.com/rm4n0s/emetos/extensions/internal/langserver"
)

// The settings of the Python pack (SPEC §10.34 "As built (step 24b.4)"): python.languageServer and python.defaultInterpreterPath, and the
// settings of the three servers under the names their VS Code extensions use: basedpyright.analysis.* (basedpyright), python.analysis.*
// (pyright, as Pylance's and Pyright's own extension name them) and pylsp.plugins.* (python-lsp-server). The descriptions are written for
// Emetos. The settings that differ from their default are sent to the server; the defaults of the server's own version apply to the
// others. Every setting is read at the start, so a change restarts the server (SPEC §10.34 "As built (step 24b.1)").

// The categories of the settings editor.
const (
	categoryPython       = "Python"
	categoryBasedpyright = "basedpyright"
	categoryPyright      = "Pyright"
	categoryPylsp        = "pylsp"
)

// serverSetting is a setting of one of the servers.
type serverSetting struct {
	// key is the whole name (basedpyright.analysis.typeCheckingMode).
	key  string
	typ  string
	def  any
	enum []any
	desc string
}

// spec is the SettingSpec of the setting.
func (s serverSetting) spec(category string) extension.SettingSpec {
	return extension.SettingSpec{Key: s.key, Type: s.typ, Default: s.def, Enum: s.enum, Description: s.desc, Category: category}
}

// list makes the default of an array setting.
func list(items ...any) []any {
	if items == nil {
		return []any{}
	}
	return items
}

// strs makes the values of an enum.
func strs(values ...string) []any {
	out := make([]any, len(values))
	for i, v := range values {
		out[i] = v
	}
	return out
}

// analysisSettings are the settings both Pyright and basedpyright have, under prefix (basedpyright.analysis. or python.analysis.). The type
// checking modes differ, so the caller gives them.
func analysisSettings(prefix string, modes []any, defaultMode string) []serverSetting {
	return []serverSetting{
		{key: prefix + "autoImportCompletions", typ: "boolean", def: true, desc: "Offer completions that add an import for a symbol of another module."},
		{key: prefix + "autoSearchPaths", typ: "boolean", def: true, desc: "Add the src folder of the project to the places imports are looked for, when it exists and there is no pyrightconfig.json."},
		{key: prefix + "diagnosticMode", typ: "string", def: "openFilesOnly", enum: strs("openFilesOnly", "workspace"),
			desc: "Which files are checked: the files that are open, or every file of the project."},
		{key: prefix + "diagnosticSeverityOverrides", typ: "object", def: map[string]any{},
			desc: "The severity of rules of the type checker, by rule name (\"reportMissingImports\": \"warning\"). The value is \"error\", \"warning\", \"information\", \"none\", true or false."},
		{key: prefix + "extraPaths", typ: "array", def: list(), desc: "More folders imports are looked for in, besides the project and the interpreter's packages."},
		{key: prefix + "logLevel", typ: "string", def: "Information", enum: strs("Error", "Warning", "Information", "Trace"),
			desc: "How much the server writes to its Output channel."},
		{key: prefix + "stubPath", typ: "string", def: "typings", desc: "The folder of type stubs of libraries that have none, relative to the project."},
		{key: prefix + "typeCheckingMode", typ: "string", def: defaultMode, enum: modes, desc: "How strict the type checker is. A pyrightconfig.json or the pyright table of pyproject.toml wins over this setting."},
		{key: prefix + "typeshedPaths", typ: "array", def: list(), desc: "Folders with typeshed stubs that replace the ones the server brings."},
		{key: prefix + "useLibraryCodeForTypes", typ: "boolean", def: true, desc: "Read the source of libraries that have no stubs, to learn their types."},
	}
}

// basedpyrightSettings are the settings of basedpyright.
func basedpyrightSettings() []serverSetting {
	s := analysisSettings("basedpyright.analysis.", strs("off", "basic", "standard", "strict", "recommended", "all"), "recommended")
	return append(s,
		serverSetting{key: "basedpyright.analysis.inlayHints.variableTypes", typ: "boolean", def: true, desc: "Show the inferred type after a variable that is assigned."},
		serverSetting{key: "basedpyright.analysis.inlayHints.callArgumentNames", typ: "boolean", def: true, desc: "Show the name of the parameter before an argument of a call."},
		serverSetting{key: "basedpyright.analysis.inlayHints.functionReturnTypes", typ: "boolean", def: true, desc: "Show the inferred return type after the parameters of a function."},
		serverSetting{key: "basedpyright.analysis.inlayHints.genericTypes", typ: "boolean", def: false, desc: "Show the type arguments that were inferred for a generic."},
		serverSetting{key: "basedpyright.disableOrganizeImports", typ: "boolean", def: false, desc: "Do not offer the code action that sorts the imports (use another tool for it)."},
		serverSetting{key: "basedpyright.disableLanguageServices", typ: "boolean", def: false, desc: "Turn off completion, hover, go to definition and the other features, and keep the diagnostics."},
	)
}

// pyrightSettings are the settings of Pyright, under the names Pyright's and Pylance's extensions give them.
func pyrightSettings() []serverSetting {
	s := analysisSettings("python.analysis.", strs("off", "basic", "standard", "strict"), "standard")
	return append(s,
		serverSetting{key: "pyright.disableOrganizeImports", typ: "boolean", def: false, desc: "Do not offer the code action that sorts the imports (use another tool for it)."},
		serverSetting{key: "pyright.disableLanguageServices", typ: "boolean", def: false, desc: "Turn off completion, hover, go to definition and the other features, and keep the diagnostics."},
	)
}

// pylspSettings are the settings of python-lsp-server (its README's configuration), the plugins that change what the server tells.
func pylspSettings() []serverSetting {
	return []serverSetting{
		{key: "pylsp.configurationSources", typ: "array", def: list("pycodestyle"), desc: "Where pycodestyle and flake8 read their own settings from: pycodestyle (setup.cfg, tox.ini) and/or flake8."},
		{key: "pylsp.signature.formatter", typ: "string", def: "black", enum: strs("black", "ruff"), desc: "The tool that formats the signatures shown in hovers and completions."},
		{key: "pylsp.signature.line_length", typ: "integer", def: float64(88), desc: "The line length the signatures are formatted to."},
		{key: "pylsp.plugins.jedi.extra_paths", typ: "array", def: list(), desc: "More folders Jedi looks for modules in."},
		{key: "pylsp.plugins.jedi.env_vars", typ: "object", def: map[string]any{}, desc: "Environment variables Jedi runs with."},
		{key: "pylsp.plugins.jedi.environment", typ: "string", def: "", desc: "The interpreter Jedi uses for the packages. When empty, the interpreter chosen with Python: Select Interpreter."},
		{key: "pylsp.plugins.jedi_completion.enabled", typ: "boolean", def: true, desc: "Enable completion with Jedi."},
		{key: "pylsp.plugins.jedi_completion.include_params", typ: "boolean", def: true, desc: "Complete the parameters of a function as snippets."},
		{key: "pylsp.plugins.jedi_completion.fuzzy", typ: "boolean", def: false, desc: "Match the typed text anywhere in a name, not only at its start."},
		{key: "pylsp.plugins.jedi_completion.eager", typ: "boolean", def: false, desc: "Resolve the details of every completion at once instead of when it is selected."},
		{key: "pylsp.plugins.jedi_completion.resolve_at_most", typ: "integer", def: float64(25), desc: "How many completions get their details at once."},
		{key: "pylsp.plugins.jedi_definition.enabled", typ: "boolean", def: true, desc: "Enable go to definition with Jedi."},
		{key: "pylsp.plugins.jedi_definition.follow_imports", typ: "boolean", def: true, desc: "Go through an import to the definition in the module it names."},
		{key: "pylsp.plugins.jedi_hover.enabled", typ: "boolean", def: true, desc: "Enable hover with Jedi."},
		{key: "pylsp.plugins.jedi_references.enabled", typ: "boolean", def: true, desc: "Enable find references with Jedi."},
		{key: "pylsp.plugins.jedi_signature_help.enabled", typ: "boolean", def: true, desc: "Enable signature help with Jedi."},
		{key: "pylsp.plugins.jedi_symbols.enabled", typ: "boolean", def: true, desc: "Enable the outline and the symbol search with Jedi."},
		{key: "pylsp.plugins.pycodestyle.enabled", typ: "boolean", def: true, desc: "Check the style of the code with pycodestyle."},
		{key: "pylsp.plugins.pycodestyle.maxLineLength", typ: "integer", def: float64(0), desc: "The longest line pycodestyle accepts; 0 leaves it to pycodestyle (79)."},
		{key: "pylsp.plugins.pycodestyle.ignore", typ: "array", def: list(), desc: "pycodestyle codes to leave out (E501)."},
		{key: "pylsp.plugins.pycodestyle.select", typ: "array", def: list(), desc: "The only pycodestyle codes to report; none listed reports them all."},
		{key: "pylsp.plugins.pyflakes.enabled", typ: "boolean", def: true, desc: "Check for errors with pyflakes (undefined names, unused imports)."},
		{key: "pylsp.plugins.mccabe.enabled", typ: "boolean", def: true, desc: "Warn about functions that are too complex."},
		{key: "pylsp.plugins.mccabe.threshold", typ: "integer", def: float64(15), desc: "The complexity above which mccabe warns."},
		{key: "pylsp.plugins.flake8.enabled", typ: "boolean", def: false, desc: "Check the code with flake8 (needs flake8 installed in the interpreter's environment)."},
		{key: "pylsp.plugins.flake8.maxLineLength", typ: "integer", def: float64(0), desc: "The longest line flake8 accepts; 0 leaves it to flake8 (79)."},
		{key: "pylsp.plugins.pylint.enabled", typ: "boolean", def: false, desc: "Check the code with pylint (needs pylint installed; it is slow)."},
		{key: "pylsp.plugins.pylint.args", typ: "array", def: list(), desc: "Arguments for pylint."},
		{key: "pylsp.plugins.pydocstyle.enabled", typ: "boolean", def: false, desc: "Check the docstrings with pydocstyle (needs pydocstyle installed)."},
		{key: "pylsp.plugins.autopep8.enabled", typ: "boolean", def: true, desc: "Format with autopep8 (needs autopep8 installed)."},
		{key: "pylsp.plugins.yapf.enabled", typ: "boolean", def: true, desc: "Format with yapf (needs yapf installed)."},
		{key: "pylsp.plugins.rope_autoimport.enabled", typ: "boolean", def: false, desc: "Offer completions that add an import, with rope (needs pylsp-rope installed)."},
		{key: "pylsp.plugins.rope_completion.enabled", typ: "boolean", def: false, desc: "Complete with rope (needs pylsp-rope installed)."},
	}
}

// settingSpecs are the settings the manifest declares besides the path settings of the programs (langserver.Config.SettingSpecs).
func settingSpecs() []extension.SettingSpec {
	specs := []extension.SettingSpec{
		{
			Key: LanguageServerSetting, Type: "string", Default: ServerBasedpyright, Category: categoryPython,
			Enum: strs(ServerBasedpyright, ServerPyright, ServerPylsp, ServerNone),
			EnumDescriptions: []string{
				"basedpyright: a type checker and language server, a fork of Pyright with more checks and inlay hints",
				"Pyright: Microsoft's type checker and language server",
				"python-lsp-server: Jedi-based completion with pyflakes and pycodestyle diagnostics",
				"No language server",
			},
			Description: "Which language server the Python files get. Emetos does not install one: it finds the program (basedpyright.path, pyright.path, " +
				"pylsp.path, then PATH, then the folder of the interpreter). Pylance is not available.",
		},
		{
			Key: DefaultInterpreterSetting, Type: "string", Default: "", Category: categoryPython, Scope: "machine",
			Description: "The Python interpreter used when you have not chosen one for the folder with Python: Select Interpreter: the path of a python program " +
				"or of an environment's folder. When empty, the active environment (VIRTUAL_ENV, CONDA_PREFIX) or the first python3 on PATH. " +
				"Only the user's settings are read.",
		},
	}
	for _, group := range []struct {
		category string
		list     []serverSetting
	}{{categoryBasedpyright, basedpyrightSettings()}, {categoryPyright, pyrightSettings()}, {categoryPylsp, pylspSettings()}} {
		for _, s := range group.list {
			specs = append(specs, s.spec(group.category))
		}
	}
	return specs
}

// nonDefault builds the nested options of the settings in list that are not at their default, below the prefix that the server's section
// is named by (basedpyright.analysis. under "basedpyright": {"analysis": {...}}). Every setting is read, so that a change of any restarts
// the server.
func nonDefault(s *langserver.Settings, list []serverSetting, section string) map[string]any {
	out := map[string]any{}
	for _, set := range list {
		v, ok := s.Value(set.key)
		if !ok || langserver.Fingerprint(v, true) == langserver.Fingerprint(set.def, true) {
			continue
		}
		rest, found := strings.CutPrefix(set.key, section+".")
		if !found {
			continue
		}
		langserver.SetNested(out, strings.Split(rest, "."), v)
	}
	return out
}
