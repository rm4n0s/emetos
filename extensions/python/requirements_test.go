package python

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/rm4n0s/emetos/extension"
)

// fakePython writes a python3 that prints its version and imports the packages listed (any other import fails).
func fakeReqPython(t *testing.T, path string, version string, packages map[string]string) string {
	t.Helper()
	script := "#!/bin/sh\nif [ \"$1\" = \"--version\" ]; then echo 'Python " + version + "'; exit 0; fi\n"
	for pkg, v := range packages {
		script += "case \"$2\" in *'import " + pkg + ";'*) echo '" + v + "'; exit 0;; esac\n"
	}
	script += "echo \"ModuleNotFoundError: No module named\" >&2; exit 1\n"
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	return path
}

func pyRequirement(t *testing.T, id string) extension.Requirement {
	t.Helper()
	for _, r := range New().Manifest().Requirements {
		if r.ID == id {
			return r
		}
	}
	t.Fatalf("no requirement %s", id)
	return extension.Requirement{}
}

func TestTheManifestDeclaresTheRequirementsOfPython(t *testing.T) {
	m := New().Manifest()
	if err := extension.CheckManifest(m); err != nil {
		t.Fatal(err)
	}
	var ids []string
	for _, r := range m.Requirements {
		ids = append(ids, r.ID)
	}
	want := []string{ServerID, RequirementInterpreter, RequirementDebugpy, RequirementPytest}
	if len(ids) != len(want) {
		t.Fatalf("ids %v", ids)
	}
	for i := range want {
		if ids[i] != want[i] {
			t.Fatalf("ids %v, want %v", ids, want)
		}
	}
}

func TestTheRequirementsOfPython(t *testing.T) {
	t.Setenv("VIRTUAL_ENV", "")
	t.Setenv("CONDA_PREFIX", "")
	t.Setenv("HOME", t.TempDir())
	tests := []struct {
		name    string
		id      string
		setup   func(t *testing.T, bin, folder string) (settings map[string]any, chosen string)
		state   extension.RequirementState
		version string
		detail  string
		install string
	}{
		{"the python3 of PATH", RequirementInterpreter, func(t *testing.T, bin, _ string) (map[string]any, string) {
			fakeReqPython(t, filepath.Join(bin, "python3"), "3.12.1", nil)
			return nil, ""
		}, extension.RequirementFound, "3.12.1", "the first python3 on PATH", ""},
		{"no interpreter", RequirementInterpreter, func(*testing.T, string, string) (map[string]any, string) { return nil, "" },
			extension.RequirementMissing, "", "no python3 or python was found in PATH, and no environment or python.defaultInterpreterPath names one", ""},
		{"the one the user chose", RequirementInterpreter, func(t *testing.T, bin, _ string) (map[string]any, string) {
			fakeReqPython(t, filepath.Join(bin, "python3"), "3.12.1", nil)
			return nil, fakeReqPython(t, filepath.Join(t.TempDir(), "py311", "bin", "python3"), "3.11.4", nil)
		}, extension.RequirementFound, "3.11.4", "chosen by you", ""},
		{"debugpy in the interpreter", RequirementDebugpy, func(t *testing.T, bin, _ string) (map[string]any, string) {
			fakeReqPython(t, filepath.Join(bin, "python3"), "3.12.1", map[string]string{"debugpy": "1.8.22"})
			return nil, ""
		}, extension.RequirementFound, "1.8.22", "", ""},
		{"debugpy missing, with the pip command of the interpreter", RequirementDebugpy, func(t *testing.T, bin, _ string) (map[string]any, string) {
			fakeReqPython(t, filepath.Join(bin, "python3"), "3.12.1", nil)
			return nil, ""
		}, extension.RequirementMissing, "", "", "/python3 -m pip install --user debugpy"},
		{"pytest of an environment inside the folder is looked for, not run", RequirementPytest, func(t *testing.T, _, folder string) (map[string]any, string) {
			py := fakeReqPython(t, filepath.Join(folder, ".venv", "bin", "python3"), "3.13.0", nil)
			if err := os.WriteFile(filepath.Join(folder, ".venv", "pyvenv.cfg"), []byte("version = 3.13.0\n"), 0o644); err != nil {
				t.Fatal(err)
			}
			if err := os.MkdirAll(filepath.Join(folder, ".venv", "lib", "python3.13", "site-packages", "pytest"), 0o755); err != nil {
				t.Fatal(err)
			}
			t.Setenv("VIRTUAL_ENV", filepath.Join(folder, ".venv")) // an activated environment
			// a program that would leave a mark if it were run
			if err := os.WriteFile(py, []byte("#!/bin/sh\ntouch "+filepath.Join(folder, "RAN")+"\n"), 0o755); err != nil {
				t.Fatal(err)
			}
			return nil, ""
		}, extension.RequirementFound, "", "", ""},
		{"pytest is not used with unittest alone", RequirementPytest, func(*testing.T, string, string) (map[string]any, string) {
			return map[string]any{UnittestEnabledSetting: true}, ""
		}, extension.RequirementUnknown, "", "python.testing.unittestEnabled is on, so the tests run with unittest and pytest is not used", ""},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			bin := filepath.Join(t.TempDir(), "bin")
			if err := os.MkdirAll(bin, 0o755); err != nil {
				t.Fatal(err)
			}
			t.Setenv("PATH", bin)
			folder := t.TempDir()
			settings, chosen := tt.setup(t, bin, folder)
			env := extension.RequirementEnv{Folder: folder,
				Setting: func(k string) (any, bool) { v, ok := settings[k]; return v, ok },
				State: func(k string) (json.RawMessage, bool) {
					if k == StateKey && chosen != "" {
						b, _ := json.Marshal(chosen)
						return b, true
					}
					return nil, false
				}}
			got := pyRequirement(t, tt.id).Check(context.Background(), env)
			if got.State != tt.state || got.Version != tt.version || (tt.detail != "" && got.Detail != tt.detail) {
				t.Errorf("got %+v, want %s %q %q", got, tt.state, tt.version, tt.detail)
			}
			if tt.install != "" && (len(got.Install.Commands) != 1 || filepath.Base(filepath.Dir(got.Install.Commands[0].Command)) == "" ||
				!hasSuffix(got.Install.Commands[0].Command, tt.install)) {
				t.Errorf("install %+v", got.Install)
			}
			if _, err := os.Stat(filepath.Join(folder, "RAN")); err == nil {
				t.Error("an interpreter inside the folder was run")
			}
		})
	}
}

func hasSuffix(s, suffix string) bool {
	return len(s) >= len(suffix) && s[len(s)-len(suffix):] == suffix
}

// The server's requirement finds basedpyright in the bin folder of the interpreter the user chose, with no activation: the hook of the
// program reads the choice from the state the check gives.
func TestTheServerIsFoundInTheChosenInterpreterWithoutAnActivation(t *testing.T) {
	t.Setenv("VIRTUAL_ENV", "")
	t.Setenv("CONDA_PREFIX", "")
	t.Setenv("HOME", t.TempDir())
	t.Setenv("PATH", t.TempDir())
	env := filepath.Join(t.TempDir(), "py312")
	py := fakeReqPython(t, filepath.Join(env, "bin", "python3"), "3.12.1", nil)
	server := filepath.Join(env, "bin", "basedpyright-langserver")
	if err := os.WriteFile(server, []byte("#!/bin/sh\necho 1.31.0\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	got := pyRequirement(t, ServerID).Check(context.Background(), extension.RequirementEnv{Folder: t.TempDir(),
		State: func(k string) (json.RawMessage, bool) {
			if k == StateKey {
				b, _ := json.Marshal(py)
				return b, true
			}
			return nil, false
		}})
	if got.State != extension.RequirementFound || got.Path != server {
		t.Errorf("got %+v", got)
	}
}
