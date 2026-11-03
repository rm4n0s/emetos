package python

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/rm4n0s/errors"

	"github.com/rm4n0s/emetos/extension"
	"github.com/rm4n0s/emetos/extensions/internal/fakeapi"
	"github.com/rm4n0s/emetos/extensions/internal/langserver"
)

// The Python pack over the fake API with fake servers and interpreters: scripts in folders the tests make. Nothing here reads the home
// folder, the PATH or the environment variables of the machine: every test sets its own (t.Setenv).

// rig is the pack activated in a tab with a folder.
type rig struct {
	t      *testing.T
	api    *fakeapi.API
	ext    *python
	base   string
	folder string
	bin    string
	home   string
}

// rigOptions say what the machine of a test has.
type rigOptions struct {
	// servers are the programs in the bin folder that is the PATH: "basedpyright", "pyright", "pylsp".
	servers []string
	// noPython leaves the bin folder with no python3.
	noPython bool
	// files are written to the folder of the tab (name to text; "" for a folder when the name ends with a slash).
	files map[string]string
	// state is what was kept before the pack was activated: the interpreter chosen in an earlier session.
	state string
	// settings are set before the pack is activated.
	settings map[string]any
	// environ are the environment variables of Emetos (VIRTUAL_ENV, CONDA_PREFIX, PYENV_ROOT).
	environ map[string]string
	// noFolder makes the tab a tab without a folder.
	noFolder bool
	// runner replaces the program runner of the Testing provider (a fake that prints recorded lines).
	runner testRunner
}

// serverScripts are the fake programs of the three servers: the language server ones print nothing useful, the siblings and pylsp print
// their versions as the real ones do.
var serverScripts = map[string]map[string]string{
	"basedpyright": {"basedpyright-langserver": "", "basedpyright": "basedpyright 1.40.2\nbased on pyright 1.1.414"},
	"pyright":      {"pyright-langserver": "", "pyright": "pyright 1.1.414"},
	"pylsp":        {"pylsp": "pylsp v1.15.0"},
}

// newRig activates the pack.
func newRig(t *testing.T, o rigOptions) *rig { t.Helper(); return newRigWith(t, o, nil) }

// newRigWith activates the pack; before makes the fake API ready (its logger) before the activation.
func newRigWith(t *testing.T, o rigOptions, before func(*fakeapi.API)) *rig {
	t.Helper()
	base := t.TempDir()
	r := &rig{t: t, base: base, folder: filepath.Join(base, "work"), bin: filepath.Join(base, "bin"), home: filepath.Join(base, "home")}
	for _, d := range []string{r.folder, r.bin, r.home} {
		if err := os.MkdirAll(d, 0o755); err != nil {
			t.Fatal(err)
		}
	}
	for name, text := range o.files {
		if strings.HasSuffix(name, "/") {
			if err := os.MkdirAll(filepath.Join(r.folder, name), 0o755); err != nil {
				t.Fatal(err)
			}
			continue
		}
		writeFile(t, r.folder, name, text, 0o644)
	}
	for _, s := range o.servers {
		for exe, out := range serverScripts[s] {
			writeFile(t, r.bin, exe, "#!/bin/sh\necho '"+out+"'\n", 0o755)
		}
	}
	if !o.noPython {
		fakePython(t, r.bin, "python3", "3.11.2")
	}
	t.Setenv("PATH", r.bin)
	t.Setenv("HOME", r.home)
	for _, k := range []string{"VIRTUAL_ENV", "CONDA_PREFIX", "CONDA_DEFAULT_ENV", "PYENV_ROOT"} {
		t.Setenv(k, o.environ[k])
	}
	ext := New()
	if o.runner != nil {
		ext.(*python).runner = o.runner
		ext.(*python).testDebounce = 5 * time.Millisecond
	}
	if err := extension.CheckManifest(ext.Manifest()); err != nil {
		t.Fatal(err)
	}
	info := extension.ScopeInfo{Name: "tab", Folder: r.folder}
	if o.noFolder {
		info.Folder = ""
	}
	r.api = fakeapi.New(t, ext.Manifest(), info)
	if before != nil {
		before(r.api)
	}
	for k, v := range o.settings {
		r.api.SetSetting(k, v)
	}
	if o.state != "" {
		r.api.SetState(StateKey, o.state)
	}
	if err := ext.Activate(context.Background(), r.api); err != nil {
		t.Fatal(err)
	}
	r.ext = ext.(*python)
	t.Cleanup(r.settle)
	r.settle()
	return r
}

// writeFile writes a file (and its folders) under dir and returns its path.
func writeFile(t *testing.T, dir, rel, content string, mode os.FileMode) string {
	t.Helper()
	p := filepath.Join(dir, filepath.FromSlash(rel))
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(p, []byte(content), mode); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(p, mode); err != nil {
		t.Fatal(err)
	}
	return p
}

// fakePython writes a python program that prints "Python <version>" and returns its path.
func fakePython(t *testing.T, dir, name, version string) string {
	t.Helper()
	return writeFile(t, dir, name, "#!/bin/sh\necho 'Python "+version+"'\n", 0o755)
}

// fakeEnv makes an environment at prefix: bin/python (a script that leaves a file named ran in the prefix when it is run, so that a
// test can tell that it was) and a pyvenv.cfg with the version, and returns the path of the program.
func fakeEnv(t *testing.T, prefix, version, prompt string) string {
	t.Helper()
	cfg := "home = /usr/bin\nversion = " + version + "\n"
	if prompt != "" {
		cfg += "prompt = " + prompt + "\n"
	}
	writeFile(t, prefix, "pyvenv.cfg", cfg, 0o644)
	return writeFile(t, prefix, "bin/python", "#!/bin/sh\ntouch '"+prefix+"/ran'\necho 'Python "+version+"'\n", 0o755)
}

// settle pumps the tab until the interpreter has been looked for.
func (r *rig) settle() {
	r.t.Helper()
	for i := 0; i < 5; i++ {
		r.api.Pump()
	}
	r.ext.interps.wait()
	r.api.Pump()
}

// run starts the server as the host does (the launcher, then the state running) and waits for the pack to see it.
func (r *rig) run() (extension.ServerLaunch, error) {
	r.t.Helper()
	r.api.SetServerStatus(ServerID, extension.ServerStatus{State: extension.ServerStarting})
	l, err := r.api.Launch(context.Background(), ServerID)
	if err != nil {
		r.api.SetServerStatus(ServerID, extension.ServerStatus{State: extension.ServerMissing})
	} else {
		r.api.SetServerStatus(ServerID, extension.ServerStatus{State: extension.ServerRunning, Starts: 1})
	}
	r.settle()
	return l, err
}

// editor puts a file of the language in front.
func (r *rig) editor(name, language, text string) string {
	r.t.Helper()
	uri := "file://" + filepath.Join(r.folder, name)
	r.api.SetDocument(uri, text)
	r.api.SetActive(&extension.ActiveDocument{URI: uri, LanguageID: language, Selections: []extension.Selection{{}}})
	r.api.Emit(extension.TopicEditorActiveChanged, extension.ActiveEditor{URI: uri, LanguageID: language})
	r.settle()
	return uri
}

// item is the status item of the server.
func (r *rig) item() *fakeapi.StatusItem { return r.api.StatusItem(langserver.StatusID(ServerID)) }

// notes are the messages of the notifications so far.
func (r *rig) notes() []string {
	var out []string
	for _, n := range r.api.Notes() {
		out = append(out, n.Message)
	}
	return out
}

// wantRoute checks the route and the tag of a failure.
func wantRoute(t *testing.T, err error, route string) {
	t.Helper()
	appErr, ok := errors.FromError(err)
	if !ok {
		t.Fatalf("expected *errors.Error, got %T (%v)", err, err)
	}
	if !appErr.HasRoute(route) {
		t.Errorf("unexpected failure path: %s (want %s)", appErr.Route(), route)
	}
}

// configuration is the answer of the launch to workspace/configuration for the section, as JSON.
func configuration(t *testing.T, l extension.ServerLaunch, section string) string {
	t.Helper()
	if l.Configuration == nil {
		t.Fatalf("the launch answers no workspace/configuration")
	}
	raw, ok := l.Configuration(section)
	if !ok {
		return "<not answered>"
	}
	return string(raw)
}

// options is the initialization options of the launch as JSON.
func options(t *testing.T, l extension.ServerLaunch) string {
	t.Helper()
	raw, err := json.Marshal(l.InitializationOptions)
	if err != nil {
		t.Fatal(err)
	}
	return string(raw)
}

func TestTheManifestDeclaresTheServerTheCommandsAndTheSettings(t *testing.T) {
	m := New().Manifest()
	if err := extension.CheckManifest(m); err != nil {
		t.Fatal(err)
	}
	if len(m.Contributes.LanguageServers) != 1 {
		t.Fatalf("servers = %+v", m.Contributes.LanguageServers)
	}
	sv := m.Contributes.LanguageServers[0]
	if sv.ID != ServerID || !sv.OwnUI || sv.TraceKey != TraceSetting || !slices.Equal(sv.Languages, []string{"python"}) || sv.Category != "Python" {
		t.Errorf("server = %+v", sv)
	}
	if sv.Program != "basedpyright-langserver" {
		t.Errorf("the program the manifest check needs is %q", sv.Program)
	}
	settings := map[string]extension.SettingSpec{}
	for _, s := range m.Contributes.Settings {
		settings[s.Key] = s
	}
	for _, key := range []string{BasedpyrightPathSetting, PyrightPathSetting, PylspPathSetting, DefaultInterpreterSetting} {
		s, ok := settings[key]
		if !ok {
			t.Errorf("the manifest lacks the setting %s", key)
		} else if s.Scope != "machine" {
			t.Errorf("%s has the scope %q: a folder's settings must not decide what program Emetos runs", key, s.Scope)
		}
	}
	ls := settings[LanguageServerSetting]
	if ls.Default != ServerBasedpyright || !slices.Equal(ls.Enum, []any{"basedpyright", "pyright", "pylsp", "none"}) || len(ls.EnumDescriptions) != 4 {
		t.Errorf("python.languageServer = %+v", ls)
	}
	for key, want := range map[string]any{
		"basedpyright.analysis.typeCheckingMode": "recommended", "python.analysis.typeCheckingMode": "standard",
		"basedpyright.analysis.diagnosticMode": "openFilesOnly", "pylsp.plugins.pycodestyle.enabled": true, "pylsp.plugins.pylint.enabled": false,
	} {
		if s, ok := settings[key]; !ok || s.Default != want {
			t.Errorf("%s = %+v, want the default %v", key, s, want)
		}
	}
	if len(settings) < 60 {
		t.Errorf("only %d settings are declared", len(settings))
	}
	if _, ok := settings[TraceSetting]; ok {
		t.Errorf("%s is the host's, not a setting the pack declares", TraceSetting)
	}
	if !slices.Contains(m.Events.Subscribes, extension.TopicEditorActiveChanged) {
		t.Errorf("events = %+v", m.Events)
	}
	if m.Name != "Python" {
		t.Errorf("name = %q", m.Name)
	}
}
