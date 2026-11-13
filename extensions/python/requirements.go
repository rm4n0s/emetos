package python

import (
	"context"
	"os"
	"path/filepath"
	"strings"

	"github.com/rm4n0s/emetos/extension"
	"github.com/rm4n0s/emetos/extensions/internal/langserver"
)

// What the Python extension needs from the system besides its language server (SPEC §10.36, step 34.2): the interpreter the tab uses
// (ResolveInterpreter, with the one the user chose), and debugpy and pytest in it. An interpreter inside the folder of the tab is never
// run by a check (Describe's rule: a project must not decide what Emetos runs when it opens); its packages are looked for in its
// site-packages instead.

// The ids of the requirements of the extension besides the server's.
const (
	RequirementInterpreter = "interpreter"
	RequirementDebugpy     = "debugpy"
	RequirementPytest      = "pytest"
)

// pythonInstall says how to get Python.
var pythonInstall = extension.Install{
	Docs: "https://www.python.org/downloads/",
	Commands: []extension.InstallCommand{
		{Family: extension.FamilyFedora, Command: "sudo dnf install python3"},
		{Family: extension.FamilyDebian, Command: "sudo apt install python3 python3-venv"},
		{Family: extension.FamilyArch, Command: "sudo pacman -S python"},
		{Family: extension.FamilySuse, Command: "sudo zypper install python3"},
		{Family: extension.FamilyMacOS, Command: "brew install python"},
	},
}

// requirements are the requirements of the manifest besides the server's.
func requirements() []extension.Requirement {
	return []extension.Requirement{
		{
			ID: RequirementInterpreter, Name: "Python interpreter", Kind: extension.RequirementProgram,
			Purpose: "runs, debugs and tests Python code, and is the environment the language server reads",
			Install: pythonInstall, PathSetting: DefaultInterpreterSetting, Check: checkInterpreter,
		},
		{
			ID: RequirementDebugpy, Name: "debugpy", Kind: extension.RequirementProgram, Optional: true,
			Purpose: "debugs Python programs (in the interpreter of the tab)",
			Install: packageInstall("", "debugpy", "https://github.com/microsoft/debugpy"), Check: checkPackage("debugpy"),
		},
		{
			ID: RequirementPytest, Name: "pytest", Kind: extension.RequirementProgram, Optional: true,
			Purpose: "runs the tests of the Testing view with pytest (unittest needs nothing)",
			Install: packageInstall("", "pytest", "https://docs.pytest.org/en/stable/getting-started.html"), Check: checkPackage("pytest"),
		},
	}
}

// packageInstall is the pip command that installs a package for an interpreter (the one the debugger offers too, pipInstallArgs); an
// empty python gives the command for python3.
func packageInstall(python, pkg, docs string) extension.Install {
	if python == "" {
		python = "python3"
	}
	args := pipInstallArgs(python)
	args[len(args)-1] = pkg
	return extension.Install{Docs: docs, Commands: []extension.InstallCommand{{Family: extension.FamilyAny, Command: strings.Join(args, " ")}}}
}

// envOf is the langserver.Env of a check: this machine, the folder and the settings of the env.
func envOf(env extension.RequirementEnv) langserver.Env {
	get := env.Setting
	if get == nil {
		get = func(string) (any, bool) { return nil, false }
	}
	return langserver.DefaultEnv(env.Folder, langserver.NewSettings(get))
}

// interpreterOf is the interpreter the tab uses and why, as the extension resolves it.
func interpreterOf(env extension.RequirementEnv) (Interpreter, string, bool) {
	var chosen string
	env.StateValue(StateKey, &chosen)
	return ResolveInterpreter(envOf(env), chosen)
}

// insideFolder tells whether an interpreter is inside the folder of the tab, where a check runs nothing.
func insideFolder(env extension.RequirementEnv, i Interpreter) bool {
	return env.Folder != "" && within(i.Path, env.Folder)
}

// checkInterpreter is the check of the interpreter.
func checkInterpreter(ctx context.Context, env extension.RequirementEnv) extension.RequirementResult {
	i, why, ok := interpreterOf(env)
	if !ok {
		return extension.RequirementResult{State: extension.RequirementMissing, Detail: "no python3 or python was found in PATH, and no environment or " + DefaultInterpreterSetting + " names one"}
	}
	d := Describe(ctx, envOf(env), i)
	if ctx.Err() != nil {
		return extension.RequirementResult{State: extension.RequirementUnknown, Path: i.Path, Detail: "the check was stopped"}
	}
	res := extension.RequirementResult{State: extension.RequirementFound, Path: i.Path, Version: d.Version, Detail: why}
	if d.Name != "" {
		res.Detail = d.Name + ", " + why
	}
	return res
}

// checkPackage is the check of a package that must be importable in the interpreter of the tab.
func checkPackage(pkg string) func(ctx context.Context, env extension.RequirementEnv) extension.RequirementResult {
	return func(ctx context.Context, env extension.RequirementEnv) extension.RequirementResult {
		if pkg == "pytest" {
			if pytestOn, unittestOn := boolSetting(env, PytestEnabledSetting), boolSetting(env, UnittestEnabledSetting); unittestOn && !pytestOn {
				return extension.RequirementResult{State: extension.RequirementUnknown, Detail: UnittestEnabledSetting + " is on, so the tests run with unittest and pytest is not used"}
			}
		}
		i, _, ok := interpreterOf(env)
		if !ok {
			return extension.RequirementResult{State: extension.RequirementUnknown, Detail: "there is no Python interpreter to look in"}
		}
		install := packageInstall(i.Path, pkg, "")
		if insideFolder(env, i) {
			if dir := sitePackage(i, pkg); dir != "" {
				return extension.RequirementResult{State: extension.RequirementFound, Path: dir, Detail: "in " + i.Path + " (not run: it is inside the folder)"}
			}
			return extension.RequirementResult{State: extension.RequirementMissing, Install: install, Detail: pkg + " is not in the site-packages of " + i.Path}
		}
		version, err := langserver.QueryVersion(ctx, i.Path, langserver.VersionCheck{Args: []string{"-c", "import " + pkg + "; print(" + pkg + ".__version__)"}}, filepath.Dir(i.Path), nil)
		switch {
		case ctx.Err() != nil:
			return extension.RequirementResult{State: extension.RequirementUnknown, Detail: "the check was stopped"}
		case err != nil:
			return extension.RequirementResult{State: extension.RequirementMissing, Install: install, Detail: pkg + " cannot be imported by " + i.Path}
		}
		return extension.RequirementResult{State: extension.RequirementFound, Path: i.Path, Version: version, Detail: "in " + i.Path}
	}
}

// boolSetting is a boolean setting of the env, false when it is not one.
func boolSetting(env extension.RequirementEnv, key string) bool {
	if env.Setting == nil {
		return false
	}
	v, _ := env.Setting(key)
	b, _ := v.(bool)
	return b
}

// sitePackage finds a package in the site-packages of an environment without running it: <prefix>/lib*/python3*/site-packages/<pkg>.
func sitePackage(i Interpreter, pkg string) string {
	for _, lib := range []string{"lib", "lib64"} {
		matches, _ := filepath.Glob(filepath.Join(i.Prefix(), lib, "python3*", "site-packages", pkg))
		for _, m := range matches {
			if st, err := os.Stat(m); err == nil && st.IsDir() {
				return m
			}
		}
	}
	return ""
}
