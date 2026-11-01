package python

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/rm4n0s/emetos/extensions/internal/langserver"
)

// The discovery and the description of interpreters, over fake folders, environment variables and a fake pyenv. Programs are scripts;
// nothing reads the machine's folders.

// machine is a fake machine for the discovery: a folder, a home folder, PATH folders and environment variables.
type machine struct {
	t       *testing.T
	base    string
	folder  string
	home    string
	path    []string
	environ map[string]string
}

func newMachine(t *testing.T) *machine {
	t.Helper()
	base := t.TempDir()
	m := &machine{t: t, base: base, folder: filepath.Join(base, "work"), home: filepath.Join(base, "home"), environ: map[string]string{}}
	for _, d := range []string{m.folder, m.home} {
		if err := os.MkdirAll(d, 0o755); err != nil {
			t.Fatal(err)
		}
	}
	return m
}

// env is the Env of the machine; settings may be nil.
func (m *machine) env(settings map[string]any) langserver.Env {
	return langserver.Env{
		Folder: m.folder, Home: m.home, Path: strings.Join(m.path, string(os.PathListSeparator)),
		Environ:  func(name string) string { return m.environ[name] },
		Settings: langserver.NewSettings(func(key string) (any, bool) { v, ok := settings[key]; return v, ok }),
	}
}

// rel writes a path relative to the machine's base folder, so that the expected lists do not hold temporary folder names.
func (m *machine) rel(p string) string {
	r, err := filepath.Rel(m.base, p)
	if err != nil {
		return p
	}
	return r
}

// list is what Discover found as "kind name path" lines.
func (m *machine) list(found []Interpreter) []string {
	var out []string
	for _, i := range found {
		out = append(out, string(i.Kind)+" '"+i.Name+"' "+m.rel(i.Path))
	}
	return out
}

func TestDiscoverListsTheInterpretersOfTheMachineOnceEach(t *testing.T) {
	tests := []struct {
		name  string
		setup func(m *machine) map[string]any
		want  []string
	}{
		{"nothing", func(m *machine) map[string]any { return nil }, nil},
		{"environments of the folder, in the order .venv, venv, env", func(m *machine) map[string]any {
			fakeEnv(t, filepath.Join(m.folder, "env"), "3.9.1", "")
			fakeEnv(t, filepath.Join(m.folder, ".venv"), "3.12.1", "")
			fakeEnv(t, filepath.Join(m.folder, "venv"), "3.11.2", "")
			return nil
		}, []string{"workspace '.venv' work/.venv/bin/python", "workspace 'venv' work/venv/bin/python", "workspace 'env' work/env/bin/python"}},
		{"a folder of that name without a program is not an environment", func(m *machine) map[string]any {
			if err := os.MkdirAll(filepath.Join(m.folder, ".venv", "bin"), 0o755); err != nil {
				t.Fatal(err)
			}
			return nil
		}, nil},
		{"VIRTUAL_ENV and CONDA_PREFIX of Emetos's environment", func(m *machine) map[string]any {
			fakeEnv(t, filepath.Join(m.base, "envs", "tools"), "3.10.4", "")
			fakePython(t, filepath.Join(m.base, "conda", "bin"), "python", "3.11.4")
			m.environ["VIRTUAL_ENV"] = filepath.Join(m.base, "envs", "tools")
			m.environ["CONDA_PREFIX"] = filepath.Join(m.base, "conda")
			m.environ["CONDA_DEFAULT_ENV"] = "base"
			return nil
		}, []string{"active 'tools' envs/tools/bin/python", "conda 'base' conda/bin/python"}},
		{"conda without CONDA_DEFAULT_ENV is named after its folder", func(m *machine) map[string]any {
			fakePython(t, filepath.Join(m.base, "conda", "envs", "ml", "bin"), "python", "3.11.4")
			m.environ["CONDA_PREFIX"] = filepath.Join(m.base, "conda", "envs", "ml")
			return nil
		}, []string{"conda 'ml' conda/envs/ml/bin/python"}},
		{"pyenv under the home folder, the newest version first", func(m *machine) map[string]any {
			for _, v := range []string{"3.9.18", "3.12.1", "3.10.13", "miniconda3-4.7"} {
				fakePython(t, filepath.Join(m.home, ".pyenv", "versions", v, "bin"), "python", v)
			}
			if err := os.MkdirAll(filepath.Join(m.home, ".pyenv", "versions", "empty"), 0o755); err != nil {
				t.Fatal(err)
			}
			return nil
		}, []string{
			"pyenv '3.12.1' home/.pyenv/versions/3.12.1/bin/python", "pyenv '3.10.13' home/.pyenv/versions/3.10.13/bin/python",
			"pyenv '3.9.18' home/.pyenv/versions/3.9.18/bin/python", "pyenv 'miniconda3-4.7' home/.pyenv/versions/miniconda3-4.7/bin/python",
		}},
		{"PYENV_ROOT replaces ~/.pyenv, and the shims are no interpreters", func(m *machine) map[string]any {
			fakePython(t, filepath.Join(m.home, ".pyenv", "versions", "3.8.0", "bin"), "python", "3.8.0")
			fakePython(t, filepath.Join(m.base, "pyroot", "versions", "3.12.1", "bin"), "python", "3.12.1")
			fakePython(t, filepath.Join(m.base, "pyroot", "shims"), "python", "shim")
			m.environ["PYENV_ROOT"] = filepath.Join(m.base, "pyroot")
			m.path = []string{filepath.Join(m.base, "pyroot", "shims")}
			return nil
		}, []string{"pyenv '3.12.1' pyroot/versions/3.12.1/bin/python"}},
		{"python programs on PATH: python3 and python3.12 of one program are listed once", func(m *machine) map[string]any {
			usr := filepath.Join(m.base, "usr", "bin")
			real := fakePython(t, usr, "python3.12", "3.12.3")
			for _, link := range []string{"python3", "python"} {
				if err := os.Symlink(real, filepath.Join(usr, link)); err != nil {
					t.Fatal(err)
				}
			}
			fakePython(t, filepath.Join(m.base, "local", "bin"), "python3.9", "3.9.2")
			writeFile(t, usr, "python3-config", "#!/bin/sh\n", 0o755) // not an interpreter
			writeFile(t, usr, "pythonw", "#!/bin/sh\n", 0o755)
			writeFile(t, filepath.Join(m.base, "local", "bin"), "python3.8", "#!/bin/sh\n", 0o644) // not executable
			m.path = []string{usr, "", filepath.Join(m.base, "local", "bin")}
			return nil
		}, []string{"global '' usr/bin/python3", "global '' local/bin/python3.9"}},
		{"the same program in two places is listed once, the first kind wins", func(m *machine) map[string]any {
			py := fakeEnv(t, filepath.Join(m.base, "envs", "tools"), "3.10.4", "")
			m.environ["VIRTUAL_ENV"] = filepath.Join(m.base, "envs", "tools")
			m.path = []string{filepath.Dir(py)}
			return nil
		}, []string{"active 'tools' envs/tools/bin/python"}},
		{"python.defaultInterpreterPath comes first", func(m *machine) map[string]any {
			fakeEnv(t, filepath.Join(m.folder, ".venv"), "3.12.1", "")
			fakePython(t, filepath.Join(m.base, "opt", "bin"), "python3", "3.7.0")
			return map[string]any{DefaultInterpreterSetting: filepath.Join(m.base, "opt", "bin", "python3")}
		}, []string{"configured '' opt/bin/python3", "workspace '.venv' work/.venv/bin/python"}},
		{"a setting that names no program adds nothing", func(m *machine) map[string]any {
			return map[string]any{DefaultInterpreterSetting: "/nowhere/bin/python"}
		}, nil},
		{"everything at once, in the order of the picker", func(m *machine) map[string]any {
			fakeEnv(t, filepath.Join(m.folder, ".venv"), "3.12.1", "")
			fakeEnv(t, filepath.Join(m.base, "envs", "tools"), "3.10.4", "")
			fakePython(t, filepath.Join(m.home, ".pyenv", "versions", "3.9.18", "bin"), "python", "3.9.18")
			fakePython(t, filepath.Join(m.base, "usr", "bin"), "python3", "3.13.0")
			m.environ["VIRTUAL_ENV"] = filepath.Join(m.base, "envs", "tools")
			m.path = []string{filepath.Join(m.base, "usr", "bin")}
			return nil
		}, []string{
			"workspace '.venv' work/.venv/bin/python", "active 'tools' envs/tools/bin/python",
			"pyenv '3.9.18' home/.pyenv/versions/3.9.18/bin/python", "global '' usr/bin/python3",
		}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			m := newMachine(t)
			settings := tt.setup(m)
			got := m.list(Discover(m.env(settings)))
			if strings.Join(got, "\n") != strings.Join(tt.want, "\n") {
				t.Errorf("found:\n  %s\nwant:\n  %s", strings.Join(got, "\n  "), strings.Join(tt.want, "\n  "))
			}
		})
	}
}

func TestResolveInterpreterFollowsTheRulesInOrder(t *testing.T) {
	tests := []struct {
		name   string
		setup  func(m *machine) (chosen string, settings map[string]any)
		want   string
		why    string
		wantOK bool
	}{
		{"nothing", func(m *machine) (string, map[string]any) { return "", nil }, "", "", false},
		{"the first python3 on PATH", func(m *machine) (string, map[string]any) {
			fakePython(t, filepath.Join(m.base, "usr", "bin"), "python", "3.8.0")
			fakePython(t, filepath.Join(m.base, "usr", "bin"), "python3", "3.12.0")
			m.path = []string{filepath.Join(m.base, "usr", "bin")}
			return "", nil
		}, "usr/bin/python3", "the first python3 on PATH", true},
		{"python when there is no python3", func(m *machine) (string, map[string]any) {
			fakePython(t, filepath.Join(m.base, "usr", "bin"), "python", "3.8.0")
			m.path = []string{filepath.Join(m.base, "usr", "bin")}
			return "", nil
		}, "usr/bin/python", "the first python on PATH", true},
		{"CONDA_PREFIX before PATH", func(m *machine) (string, map[string]any) {
			fakePython(t, filepath.Join(m.base, "usr", "bin"), "python3", "3.12.0")
			fakePython(t, filepath.Join(m.base, "conda", "bin"), "python", "3.11.4")
			m.path = []string{filepath.Join(m.base, "usr", "bin")}
			m.environ["CONDA_PREFIX"] = filepath.Join(m.base, "conda")
			return "", nil
		}, "conda/bin/python", "CONDA_PREFIX", true},
		{"VIRTUAL_ENV before CONDA_PREFIX", func(m *machine) (string, map[string]any) {
			fakePython(t, filepath.Join(m.base, "conda", "bin"), "python", "3.11.4")
			fakeEnv(t, filepath.Join(m.base, "envs", "tools"), "3.10.4", "")
			m.environ["CONDA_PREFIX"] = filepath.Join(m.base, "conda")
			m.environ["VIRTUAL_ENV"] = filepath.Join(m.base, "envs", "tools")
			return "", nil
		}, "envs/tools/bin/python", "VIRTUAL_ENV", true},
		{"the setting before the active environment", func(m *machine) (string, map[string]any) {
			fakeEnv(t, filepath.Join(m.base, "envs", "tools"), "3.10.4", "")
			fakePython(t, filepath.Join(m.base, "opt", "bin"), "python3", "3.7.0")
			m.environ["VIRTUAL_ENV"] = filepath.Join(m.base, "envs", "tools")
			return "", map[string]any{DefaultInterpreterSetting: filepath.Join(m.base, "opt")}
		}, "opt/bin/python3", DefaultInterpreterSetting, true},
		{"the folder's own environment is not taken without the user's say", func(m *machine) (string, map[string]any) {
			fakeEnv(t, filepath.Join(m.folder, ".venv"), "3.12.1", "")
			return "", nil
		}, "", "", false},
		{"the folder's own environment is taken when the setting names it", func(m *machine) (string, map[string]any) {
			fakeEnv(t, filepath.Join(m.folder, ".venv"), "3.12.1", "")
			return "", map[string]any{DefaultInterpreterSetting: "./.venv"}
		}, "work/.venv/bin/python", DefaultInterpreterSetting, true},
		{"the choice before everything", func(m *machine) (string, map[string]any) {
			fakeEnv(t, filepath.Join(m.base, "envs", "tools"), "3.10.4", "")
			m.environ["VIRTUAL_ENV"] = filepath.Join(m.base, "envs", "tools")
			return fakeEnv(t, filepath.Join(m.folder, ".venv"), "3.12.1", ""), nil
		}, "work/.venv/bin/python", "chosen by you", true},
		{"a choice whose program is gone is passed over", func(m *machine) (string, map[string]any) {
			fakePython(t, filepath.Join(m.base, "usr", "bin"), "python3", "3.12.0")
			m.path = []string{filepath.Join(m.base, "usr", "bin")}
			return filepath.Join(m.base, "deleted", "bin", "python"), nil
		}, "usr/bin/python3", "the first python3 on PATH", true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			m := newMachine(t)
			chosen, settings := tt.setup(m)
			i, why, ok := ResolveInterpreter(m.env(settings), chosen)
			if ok != tt.wantOK || why != tt.why {
				t.Fatalf("ok %v, why %q; want %v, %q (%+v)", ok, why, tt.wantOK, tt.why, i)
			}
			if ok && m.rel(i.Path) != tt.want {
				t.Errorf("interpreter = %s, want %s", m.rel(i.Path), tt.want)
			}
		})
	}
}

func TestDescribeReadsTheEnvironmentAndNeverRunsAProgramOfTheFolder(t *testing.T) {
	m := newMachine(t)
	own := fakeEnv(t, filepath.Join(m.folder, ".venv"), "3.12.1", "a prompt the shell shows")
	noCfg := writeFile(t, filepath.Join(m.folder, "venv"), "bin/python", "#!/bin/sh\ntouch \""+m.folder+"/venv/ran\"\necho 'Python 3.5.0'\n", 0o755)
	outside := fakeEnv(t, filepath.Join(m.base, "envs", "tools"), "3.10.4", "")
	noVersion := writeFile(t, filepath.Join(m.base, "envs", "odd"), "bin/python", "#!/bin/sh\necho 'not a version'\n", 0o755)
	global := fakePython(t, filepath.Join(m.base, "usr", "bin"), "python3", "3.13.1")
	cfgOnly := filepath.Join(m.base, "envs", "uv")
	writeFile(t, cfgOnly, "pyvenv.cfg", "version_info = 3.13.0.final.0\n", 0o644)
	fakePython(t, cfgOnly, "bin/python", "3.99.0")
	conda := fakePython(t, filepath.Join(m.base, "conda", "envs", "ml", "bin"), "python", "3.11.4")
	if err := os.MkdirAll(filepath.Join(m.base, "conda", "envs", "ml", "conda-meta"), 0o755); err != nil {
		t.Fatal(err)
	}
	tests := []struct {
		name string
		in   Interpreter
		want Interpreter
	}{
		{"the environment of the folder: the pyvenv.cfg, the name of its folder as the name", Interpreter{Path: own, Kind: KindWorkspace}, Interpreter{Path: own, Kind: KindWorkspace, Version: "3.12.1", Name: ".venv"}},
		{"a program of the folder with no pyvenv.cfg is not run: no version", Interpreter{Path: noCfg, Kind: KindWorkspace, Name: "venv"}, Interpreter{Path: noCfg, Kind: KindWorkspace, Name: "venv"}},
		{"an environment outside the folder: the pyvenv.cfg", Interpreter{Path: outside, Kind: KindActive, Name: "tools"}, Interpreter{Path: outside, Kind: KindActive, Name: "tools", Version: "3.10.4"}},
		{"an environment outside the folder with no pyvenv.cfg: --version, and no version when it prints none", Interpreter{Path: noVersion, Kind: KindCustom}, Interpreter{Path: noVersion, Kind: KindCustom, Name: ""}},
		{"a global program: --version, no name", Interpreter{Path: global, Kind: KindGlobal}, Interpreter{Path: global, Kind: KindGlobal, Version: "3.13.1"}},
		{"version_info is cut to its numbers", Interpreter{Path: filepath.Join(cfgOnly, "bin", "python"), Kind: KindActive}, Interpreter{Path: filepath.Join(cfgOnly, "bin", "python"), Kind: KindActive, Version: "3.13.0", Name: "uv"}},
		{"a conda environment is named after its folder", Interpreter{Path: conda, Kind: KindConda, Name: "ml"}, Interpreter{Path: conda, Kind: KindConda, Name: "ml", Version: "3.11.4"}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := Describe(context.Background(), m.env(nil), tt.in)
			if got != tt.want {
				t.Errorf("Describe = %+v\nwant       %+v", got, tt.want)
			}
		})
	}
	for _, ran := range []string{filepath.Join(m.folder, ".venv", "ran"), filepath.Join(m.folder, "venv", "ran")} {
		if _, err := os.Stat(ran); err == nil {
			t.Errorf("a program of the folder was run: %s exists", ran)
		}
	}
}

func TestInterpreterLabels(t *testing.T) {
	tests := []struct {
		in   Interpreter
		want string
	}{
		{Interpreter{Version: "3.12.1", Name: ".venv", Kind: KindWorkspace}, "Python 3.12.1 ('.venv')"},
		{Interpreter{Version: "3.11.4", Name: "base", Kind: KindConda}, "Python 3.11.4 ('base': conda)"},
		{Interpreter{Version: "3.9.18", Name: "3.9.18", Kind: KindPyenv}, "Python 3.9.18 (pyenv)"},
		{Interpreter{Version: "3.13.1", Kind: KindGlobal}, "Python 3.13.1"},
		{Interpreter{Name: "tools", Kind: KindActive}, "Python ('tools')"},
		{Interpreter{Kind: KindCustom}, "Python"},
	}
	for _, tt := range tests {
		if got := tt.in.Label(); got != tt.want {
			t.Errorf("Label(%+v) = %q, want %q", tt.in, got, tt.want)
		}
	}
	i := Interpreter{Path: "/opt/envs/tools/bin/python"}
	if i.BinDir() != "/opt/envs/tools/bin" || i.Prefix() != "/opt/envs/tools" {
		t.Errorf("BinDir %s, Prefix %s", i.BinDir(), i.Prefix())
	}
}

func TestNormalizeTurnsWhatWasTypedIntoAProgram(t *testing.T) {
	m := newMachine(t)
	py := fakeEnv(t, filepath.Join(m.folder, ".venv"), "3.12.1", "")
	global := fakePython(t, filepath.Join(m.base, "usr", "bin"), "python3", "3.13.1")
	m.path = []string{filepath.Join(m.base, "usr", "bin")}
	m.environ["MYPY_HOME"] = filepath.Join(m.base, "usr")
	if err := os.MkdirAll(filepath.Join(m.folder, "emptydir"), 0o755); err != nil {
		t.Fatal(err)
	}
	writeFile(t, m.folder, "notexec", "", 0o644)
	tests := []struct {
		name  string
		text  string
		want  string
		route string
	}{
		{"empty is nothing", "  ", "", ""},
		{"an absolute path", py, py, ""},
		{"surrounding spaces", " " + py + " ", py, ""},
		{"relative to the folder", ".venv/bin/python", py, ""},
		{"the folder of an environment", filepath.Join(m.folder, ".venv"), py, ""},
		{"a folder relative to the tab's", "./.venv", py, ""},
		{"a bare name is a name on PATH, never a file of the folder", ".venv", "", "Normalize.PythonInterpreterInvalid"},
		{"${workspaceFolder}", "${workspaceFolder}/.venv/bin/python", py, ""},
		{"${env:NAME}", "${env:MYPY_HOME}/bin/python3", global, ""},
		{"a name on PATH", "python3", global, ""},
		{"a name that is not on PATH", "python9", "", "Normalize.PythonInterpreterInvalid"},
		{"a path that does not exist", "/nowhere/python", "", "Normalize.PythonInterpreterInvalid"},
		{"a folder with no bin/python", "emptydir", "", "Normalize.PythonInterpreterInvalid"},
		{"a file that cannot be run", "notexec", "", "Normalize.PythonInterpreterInvalid"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := Normalize(m.env(nil), tt.text)
			if tt.route != "" {
				wantRoute(t, err, tt.route)
				if msg := CheckInterpreter(m.env(nil), tt.text); msg == "" {
					t.Error("the input box accepts it")
				}
				return
			}
			if err != nil || got != tt.want {
				t.Errorf("Normalize = %q, %v; want %q", got, err, tt.want)
			}
			if msg := CheckInterpreter(m.env(nil), tt.text); (msg != "") != (tt.text == "  ") {
				t.Errorf("CheckInterpreter = %q", msg)
			}
		})
	}
	// ~ is the home folder of the machine
	fakePython(t, filepath.Join(m.home, "py", "bin"), "python", "3.8.0")
	if got, err := Normalize(m.env(nil), "~/py"); err != nil || got != filepath.Join(m.home, "py", "bin", "python") {
		t.Errorf("Normalize(~/py) = %q, %v", got, err)
	}
}
