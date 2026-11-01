package python

import (
	"context"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"

	"github.com/rm4n0s/errors"

	"github.com/rm4n0s/emetos/extensions/internal/langserver"
)

// The Python interpreters (SPEC §10.34 "As built (step 24b.4)"): where they are found, how one is described and which one a folder uses.
// This file is the discovery and the description, free of the extension API, so that the debugger and the Testing provider of the pack
// (M25, M26) read the same answers as the language server does: Interpreters (interpreters.go) is the choice of one scope.

// Kind says where an interpreter was found.
type Kind string

// The kinds of interpreter, in the order the picker lists them.
const (
	// KindConfigured is the one python.defaultInterpreterPath names.
	KindConfigured Kind = "configured"
	// KindWorkspace is an environment in the folder of the tab: .venv, venv or env.
	KindWorkspace Kind = "workspace"
	// KindActive is the environment Emetos itself was started from (VIRTUAL_ENV).
	KindActive Kind = "active"
	// KindConda is the active conda environment (CONDA_PREFIX).
	KindConda Kind = "conda"
	// KindPyenv is a version pyenv installed.
	KindPyenv Kind = "pyenv"
	// KindGlobal is a python or python3 program on PATH.
	KindGlobal Kind = "global"
	// KindCustom is a path the user typed that none of the above lists.
	KindCustom Kind = "custom"
)

// title is the heading of the picker that says where the interpreters of the kind are from.
func (k Kind) title() string {
	switch k {
	case KindConfigured:
		return "python.defaultInterpreterPath"
	case KindWorkspace:
		return "Environment of this folder"
	case KindActive:
		return "Active environment (VIRTUAL_ENV)"
	case KindConda:
		return "Active conda environment (CONDA_PREFIX)"
	case KindPyenv:
		return "pyenv"
	case KindGlobal:
		return "Global (PATH)"
	}
	return "Chosen path"
}

// Interpreter is a Python interpreter: the program that runs Python code, with the environment it belongs to.
type Interpreter struct {
	// Path is the absolute path of the python program.
	Path string
	// Version is "3.12.1", empty when it could not be told.
	Version string
	// Name is the name of the environment (".venv", "base", "3.12.1" of pyenv); empty for a global interpreter.
	Name string
	// Kind says where it was found.
	Kind Kind
}

// BinDir is the folder of the program (the bin folder of an environment): the place the tools installed with it are found.
func (i Interpreter) BinDir() string { return filepath.Dir(i.Path) }

// Prefix is the folder of the environment: the parent of the bin folder.
func (i Interpreter) Prefix() string { return filepath.Dir(filepath.Dir(i.Path)) }

// Label is how the status item names the interpreter, as VS Code's does: "Python 3.12.1 ('.venv')", "Python 3.11.4 ('base': conda)", or
// "Python 3.12.1" for a global one. A version that is not known is left out.
func (i Interpreter) Label() string {
	text := "Python"
	if i.Version != "" {
		text += " " + i.Version
	}
	switch {
	case i.Kind == KindConda && i.Name != "":
		text += " ('" + i.Name + "': conda)"
	case i.Kind == KindPyenv:
		text += " (pyenv)"
	case i.Name != "" && i.Kind != KindGlobal:
		text += " ('" + i.Name + "')"
	}
	return text
}

// workspaceEnvironments are the folders of the project that hold an environment, in the order they are tried.
var workspaceEnvironments = []string{".venv", "venv", "env"}

// pythonNames matches the programs on PATH the picker lists: python, python3 and python3.12.
var pythonNames = regexp.MustCompile(`^python(3(\.[0-9]+)?)?$`)

// interpreterIn is the program of the environment at prefix, or "" when it has none.
func interpreterIn(prefix string) string {
	for _, name := range []string{"python", "python3"} {
		if p := filepath.Join(prefix, "bin", name); langserver.IsExecutable(p) {
			return p
		}
	}
	return ""
}

// Discover lists the interpreters of this machine for the tab: python.defaultInterpreterPath, the environments .venv, venv and env in
// the folder, the VIRTUAL_ENV and CONDA_PREFIX Emetos was started with, the versions of pyenv ($PYENV_ROOT or ~/.pyenv) and the python
// programs of PATH, each once. It reads folders and runs nothing; Describe adds the versions.
func Discover(env langserver.Env) []Interpreter {
	var out []Interpreter
	seen := map[string]bool{}
	add := func(path string, kind Kind, name string) {
		if path == "" {
			return
		}
		path = filepath.Clean(path)
		if seen[path] {
			return
		}
		seen[path] = true
		out = append(out, Interpreter{Path: path, Kind: kind, Name: name})
	}
	if env.Settings != nil {
		if p, err := Normalize(env, env.Settings.String(DefaultInterpreterSetting)); err == nil && p != "" {
			add(p, KindConfigured, environmentName(p))
		}
	}
	if env.Folder != "" {
		for _, dir := range workspaceEnvironments {
			add(interpreterIn(filepath.Join(env.Folder, dir)), KindWorkspace, dir)
		}
	}
	if v := env.Getenv("VIRTUAL_ENV"); v != "" {
		add(interpreterIn(v), KindActive, filepath.Base(filepath.Clean(v)))
	}
	if c := env.Getenv("CONDA_PREFIX"); c != "" {
		name := env.Getenv("CONDA_DEFAULT_ENV")
		if name == "" {
			name = filepath.Base(filepath.Clean(c))
		}
		add(interpreterIn(c), KindConda, name)
	}
	pyenvRoot := pyenvRoot(env)
	if pyenvRoot != "" {
		entries, _ := os.ReadDir(filepath.Join(pyenvRoot, "versions"))
		var names []string
		for _, e := range entries {
			names = append(names, e.Name())
		}
		sort.SliceStable(names, func(a, b int) bool { return langserver.CompareVersions(names[a], names[b]) > 0 })
		for _, n := range names {
			add(interpreterIn(filepath.Join(pyenvRoot, "versions", n)), KindPyenv, n)
		}
	}
	realPaths := map[string]bool{}
	for _, i := range out {
		if r, err := filepath.EvalSymlinks(i.Path); err == nil && i.Kind != KindWorkspace {
			realPaths[r] = true
		}
	}
	for _, dir := range filepath.SplitList(env.Path) {
		if dir == "" || (pyenvRoot != "" && filepath.Clean(dir) == filepath.Join(pyenvRoot, "shims")) {
			continue // the shims of pyenv need pyenv: its versions are listed above
		}
		entries, err := os.ReadDir(dir)
		if err != nil {
			continue
		}
		var names []string
		for _, e := range entries {
			if pythonNames.MatchString(e.Name()) {
				names = append(names, e.Name())
			}
		}
		sort.SliceStable(names, func(a, b int) bool { return pathRank(names[a]) < pathRank(names[b]) })
		for _, name := range names {
			p := filepath.Join(dir, name)
			if !langserver.IsExecutable(p) {
				continue
			}
			r, err := filepath.EvalSymlinks(p)
			if err != nil || realPaths[r] || seen[filepath.Clean(p)] {
				continue // python3 and python3.12 are one program
			}
			realPaths[r] = true
			add(p, KindGlobal, "")
		}
	}
	return out
}

// pathRank orders the names of the programs of one folder of PATH: python3 first (the one a shell runs), then python, then python3.N, so
// that when they are one program it is listed under the first.
func pathRank(name string) int {
	switch name {
	case "python3":
		return 0
	case "python":
		return 1
	}
	return 2
}

// pyenvRoot is the folder pyenv keeps its versions in: PYENV_ROOT, else ~/.pyenv.
func pyenvRoot(env langserver.Env) string {
	if r := env.Getenv("PYENV_ROOT"); r != "" {
		return r
	}
	if env.Home != "" {
		return filepath.Join(env.Home, ".pyenv")
	}
	return ""
}

// environmentName is the name of the environment a program belongs to: the name of the folder that holds its bin folder when that has a
// pyvenv.cfg or conda-meta (the name VS Code shows, not the prompt of the shell), "" for a global program.
func environmentName(path string) string {
	prefix := filepath.Dir(filepath.Dir(path))
	if readPyvenvCfg(prefix) != nil {
		return filepath.Base(prefix)
	}
	if info, err := os.Stat(filepath.Join(prefix, "conda-meta")); err == nil && info.IsDir() {
		return filepath.Base(prefix)
	}
	return ""
}

// readPyvenvCfg reads the pyvenv.cfg of the environment at prefix (key = value lines), nil when there is none.
func readPyvenvCfg(prefix string) map[string]string {
	data, err := os.ReadFile(filepath.Join(prefix, "pyvenv.cfg"))
	if err != nil {
		return nil
	}
	out := map[string]string{}
	for _, line := range strings.Split(string(data), "\n") {
		if k, v, ok := strings.Cut(line, "="); ok {
			out[strings.TrimSpace(k)] = strings.Trim(strings.TrimSpace(v), `"'`)
		}
	}
	return out
}

// within tells whether path is inside dir (or is dir).
func within(path, dir string) bool {
	if dir == "" {
		return false
	}
	rel, err := filepath.Rel(filepath.Clean(dir), filepath.Clean(path))
	return err == nil && rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator))
}

// Describe fills in the version and the name of the environment of the interpreter. The version comes from the pyvenv.cfg of an
// environment, else from running the program with --version; a program inside the folder of the tab is never run to describe it (a
// project must not decide what Emetos runs when it opens), so its version is empty when it has no pyvenv.cfg. ctx ends the run.
func Describe(ctx context.Context, env langserver.Env, i Interpreter) Interpreter {
	if i.Name == "" && i.Kind != KindGlobal {
		i.Name = environmentName(i.Path)
	}
	if i.Version == "" {
		if cfg := readPyvenvCfg(i.Prefix()); cfg != nil {
			v := cfg["version"]
			if v == "" {
				v = cfg["version_info"]
			}
			if m := versionDigits.FindString(v); m != "" {
				i.Version = m
			}
		}
	}
	if i.Version == "" && !within(i.Path, env.Folder) {
		if v, err := langserver.QueryVersion(ctx, i.Path, langserver.VersionCheck{Args: []string{"--version"}}, "", nil); err == nil {
			i.Version = v
		}
	}
	return i
}

// versionDigits is "3.12.1" at the start of a version text.
var versionDigits = regexp.MustCompile(`^[0-9]+(\.[0-9]+){1,2}`)

// Normalize turns what a person typed or a setting holds into the path of a program: ~, ${userHome}, ${workspaceFolder} and
// ${env:NAME} are expanded, a relative path is relative to the folder, a folder is an environment (its bin/python), and a name without a
// slash (python3) is looked for on PATH. An empty text gives "". It fails with PythonInterpreterInvalid when no program results.
func Normalize(env langserver.Env, text string) (string, error) {
	text = strings.TrimSpace(text)
	if text == "" {
		return "", nil
	}
	expanded := env.Expand(text)
	if !strings.Contains(expanded, "/") {
		if p, ok := langserver.FindExecutable(expanded, filepath.SplitList(env.Path)); ok {
			return p, nil
		}
		return "", errors.New("PythonInterpreterInvalid", expanded+" is not a program on PATH", "path", text)
	}
	path := filepath.Clean(expanded)
	if !filepath.IsAbs(path) {
		path = filepath.Join(env.Folder, path)
	}
	info, err := os.Stat(path)
	switch {
	case err != nil:
		return "", errors.New("PythonInterpreterInvalid", path+" does not exist", "path", path)
	case info.IsDir():
		if p := interpreterIn(path); p != "" {
			return p, nil
		}
		return "", errors.New("PythonInterpreterInvalid", path+" is a folder with no bin/python", "path", path)
	case !langserver.IsExecutable(path):
		return "", errors.New("PythonInterpreterInvalid", path+" is not a program that can be run", "path", path)
	}
	return path, nil
}

// CheckInterpreter tells why the text is not an interpreter a person may choose, "" when it is (the input box shows it as it is typed).
func CheckInterpreter(env langserver.Env, text string) string {
	if strings.TrimSpace(text) == "" {
		return "Enter the path of a python program, or of an environment's folder"
	}
	if _, err := Normalize(env, text); err != nil {
		if appErr, ok := errors.FromError(err); ok {
			return appErr.Message
		}
		return err.Error()
	}
	return ""
}

// ResolveInterpreter chooses the interpreter a folder uses now, without running anything: the one the user chose (when its program is
// still there), else python.defaultInterpreterPath, else the active environment (VIRTUAL_ENV, then CONDA_PREFIX), else the first
// python3 or python on PATH. A folder's own .venv is not chosen for the user (Discover lists it; the user accepts it once, which keeps
// it as the chosen one). why says in words which rule decided; ok is false when there is no interpreter at all.
func ResolveInterpreter(env langserver.Env, chosen string) (i Interpreter, why string, ok bool) {
	if chosen != "" {
		if langserver.IsExecutable(chosen) {
			return classify(env, chosen), "chosen by you", true
		}
	}
	if env.Settings != nil {
		if p, err := Normalize(env, env.Settings.String(DefaultInterpreterSetting)); err == nil && p != "" {
			return classify(env, p), DefaultInterpreterSetting, true
		}
	}
	if v := env.Getenv("VIRTUAL_ENV"); v != "" {
		if p := interpreterIn(v); p != "" {
			return Interpreter{Path: p, Kind: KindActive, Name: filepath.Base(filepath.Clean(v))}, "VIRTUAL_ENV", true
		}
	}
	if c := env.Getenv("CONDA_PREFIX"); c != "" {
		if p := interpreterIn(c); p != "" {
			name := env.Getenv("CONDA_DEFAULT_ENV")
			if name == "" {
				name = filepath.Base(filepath.Clean(c))
			}
			return Interpreter{Path: p, Kind: KindConda, Name: name}, "CONDA_PREFIX", true
		}
	}
	for _, name := range []string{"python3", "python"} {
		if p, found := langserver.FindExecutable(name, filepath.SplitList(env.Path)); found {
			return Interpreter{Path: p, Kind: KindGlobal}, "the first " + name + " on PATH", true
		}
	}
	return Interpreter{}, "", false
}

// classify tells the kind of the program at path from where it is: the environments of the folder, pyenv, or a path nobody listed.
func classify(env langserver.Env, path string) Interpreter {
	path = filepath.Clean(path)
	i := Interpreter{Path: path, Kind: KindCustom, Name: environmentName(path)}
	for _, known := range Discover(env) {
		if known.Path == path {
			return known
		}
	}
	if root := pyenvRoot(env); root != "" && within(path, filepath.Join(root, "versions")) {
		i.Kind = KindPyenv
	}
	return i
}
