package csharp

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"testing"

	"github.com/rm4n0s/errors"

	"github.com/rm4n0s/emetos/extension"
	"github.com/rm4n0s/emetos/extensions/internal/fakeapi"
	"github.com/rm4n0s/emetos/extensions/internal/langserver"
)

// The C# pack over the fake API with a fake csharp-ls and a fake dotnet (scripts that print what the real ones print) in folders the tests
// make. Nothing here reads the home folder, the PATH or the .NET installation of the machine: every test sets its own (t.Setenv) and the pack is
// given no system folders to look in.

// rig is the pack activated in a tab with a folder.
type rig struct {
	t      *testing.T
	api    *fakeapi.API
	ext    *csharp
	base   string
	folder string
	bin    string
	home   string
}

// rigOptions say what the machine of a test has.
type rigOptions struct {
	// noServer leaves csharp-ls out of PATH, noDotnet leaves dotnet out.
	noServer, noDotnet bool
	// version is what the fake csharp-ls prints; 0.28.0 when empty.
	version string
	// settings are set before the pack is activated.
	settings map[string]any
	// files are written to the folder of the tab (name to text).
	files map[string]string
	// noFolder makes the tab a tab without a folder.
	noFolder bool
	// state is kept before the pack is activated (the choice of an earlier session).
	state map[string]any
	// env are variables set for the test.
	env map[string]string
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
		writeFile(t, r.folder, name, text, 0o644)
	}
	version := o.version
	if version == "" {
		version = "0.28.0"
	}
	if !o.noServer {
		fakeServer(t, r.bin, version)
	}
	if !o.noDotnet {
		writeFile(t, r.bin, "dotnet", "#!/bin/sh\n", 0o755)
	}
	t.Setenv("PATH", r.bin)
	t.Setenv("HOME", r.home)
	t.Setenv("DOTNET_ROOT", "")
	t.Setenv("DOTNET_CLI_HOME", "")
	for k, v := range o.env {
		t.Setenv(k, v)
	}
	ext := New()
	if err := extension.CheckManifest(ext.Manifest()); err != nil {
		t.Fatal(err)
	}
	c := ext.(*csharp)
	c.systemDotnet, c.installLocations = nil, nil
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
	for k, v := range o.state {
		r.api.SetState(k, v)
	}
	if err := ext.Activate(context.Background(), r.api); err != nil {
		t.Fatal(err)
	}
	r.ext = c
	t.Cleanup(r.settle)
	r.settle()
	return r
}

// fakeServer makes a csharp-ls in the folder that prints a version as the real one does.
func fakeServer(t *testing.T, dir, version string) string {
	t.Helper()
	return writeFile(t, dir, "csharp-ls", "#!/bin/sh\necho 'csharp-ls, "+version+" (Klebiškis)+72671fc7fa810c158864e72485db68e8b9b25c3d'\n", 0o755)
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

// settle pumps the tab.
func (r *rig) settle() {
	r.t.Helper()
	for i := 0; i < 5; i++ {
		r.api.Pump()
	}
	r.ext.wait()
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

// logged is a writer whose lines a test can read: the log of an activation.
type logged struct {
	mu    sync.Mutex
	lines []string
}

func (l *logged) Write(p []byte) (int, error) {
	l.mu.Lock()
	l.lines = append(l.lines, string(p))
	l.mu.Unlock()
	return len(p), nil
}

func (l *logged) text() string {
	l.mu.Lock()
	defer l.mu.Unlock()
	return strings.Join(l.lines, "")
}

// env makes the Env of a test: the settings are the map's, the environment the other's.
func env(folder, home, path string, set map[string]any, vars map[string]string) langserver.Env {
	return langserver.Env{Folder: folder, Home: home, Path: path, Environ: func(k string) string { return vars[k] },
		Settings: langserver.NewSettings(func(key string) (any, bool) { v, ok := set[key]; return v, ok })}
}

// launchConfiguration is the answer to workspace/configuration for the section csharp.
func launchConfiguration(t *testing.T, l extension.ServerLaunch) map[string]any {
	t.Helper()
	raw, ok := l.Configuration("csharp")
	if !ok {
		t.Fatal("the launch answers nothing for the section csharp")
	}
	var out map[string]any
	if err := json.Unmarshal(raw, &out); err != nil {
		t.Fatal(err)
	}
	return out
}

func TestTheManifestDeclaresTheServerTheCommandsAndTheSettings(t *testing.T) {
	m := New().Manifest()
	if err := extension.CheckManifest(m); err != nil {
		t.Fatal(err)
	}
	if m.ID != ID || m.Name != "C#" {
		t.Errorf("manifest = %s %q", m.ID, m.Name)
	}
	if len(m.Contributes.LanguageServers) != 1 {
		t.Fatalf("servers = %+v", m.Contributes.LanguageServers)
	}
	sv := m.Contributes.LanguageServers[0]
	if sv.ID != ServerID || !sv.OwnUI || sv.TraceKey != TraceSetting || sv.Category != "C#" || !slices.Equal(sv.Languages, []string{"csharp"}) {
		t.Errorf("server = %+v", sv)
	}
	settings := map[string]extension.SettingSpec{}
	for _, s := range m.Contributes.Settings {
		if _, dup := settings[s.Key]; dup {
			t.Errorf("%s is declared twice", s.Key)
		}
		settings[s.Key] = s
		if !strings.HasPrefix(s.Key, "csharp.") || s.Description == "" {
			t.Errorf("setting %+v lacks its description or is not a csharp setting", s)
		}
	}
	if settings[PathSetting].Scope != "machine" {
		t.Errorf("%s is read from %q settings, want the user's only: it decides which program runs", PathSetting, settings[PathSetting].Scope)
	}
	for _, key := range []string{SolutionSetting, LogLevelSetting, AnalyzersSetting, ApplyFormattingSetting, LocaleSetting} {
		if s, ok := settings[key]; !ok || s.Scope == "machine" {
			t.Errorf("%s = %+v: a setting of the folder", key, s)
		}
	}
	if settings[AnalyzersSetting].Default != false || settings[LogLevelSetting].Default != "info" {
		t.Errorf("defaults = %v %v", settings[AnalyzersSetting].Default, settings[LogLevelSetting].Default)
	}
	if len(m.Contributes.Commands) != 1 || m.Contributes.Commands[0].ID != SelectSolutionCommand || m.Contributes.Commands[0].Category != "C#" || m.Contributes.Commands[0].Title != "Select Solution" {
		t.Errorf("commands = %+v", m.Contributes.Commands)
	}
	if !slices.Contains(m.Events.Subscribes, extension.TopicEditorActiveChanged) {
		t.Errorf("subscribes = %v", m.Events.Subscribes)
	}
	if got := extension.RestartServerCommand(ID); got != "emetos.csharp.restartServer" {
		t.Errorf("restart command = %s", got)
	}
}

func TestFindingCsharpLsLooksInTheSettingThenPathThenTheFolderOfTheDotnetTools(t *testing.T) {
	tests := []struct {
		name string
		// layout makes the folders under base and returns the settings, the PATH and the variables of the test.
		layout func(t *testing.T, base string) (set map[string]any, path string, vars map[string]string)
		want   func(base string) langserver.Found
		route  string
	}{
		{"the setting wins over PATH and the tools folder", func(t *testing.T, base string) (map[string]any, string, map[string]string) {
			fakeServer(t, filepath.Join(base, "bin"), "0.28.0")
			fakeServer(t, filepath.Join(base, "home", ".dotnet", "tools"), "0.28.0")
			fakeServer(t, filepath.Join(base, "mine"), "0.28.0")
			return map[string]any{PathSetting: filepath.Join(base, "mine", "csharp-ls")}, filepath.Join(base, "bin"), nil
		}, func(base string) langserver.Found {
			return langserver.Found{Path: filepath.Join(base, "mine", "csharp-ls"), Source: langserver.FromSetting}
		}, ""},
		{"PATH wins over the tools folder", func(t *testing.T, base string) (map[string]any, string, map[string]string) {
			fakeServer(t, filepath.Join(base, "bin"), "0.28.0")
			fakeServer(t, filepath.Join(base, "home", ".dotnet", "tools"), "0.28.0")
			return nil, filepath.Join(base, "bin"), nil
		}, func(base string) langserver.Found {
			return langserver.Found{Path: filepath.Join(base, "bin", "csharp-ls"), Source: langserver.FromPath}
		}, ""},
		{"the folder of the .NET global tools", func(t *testing.T, base string) (map[string]any, string, map[string]string) {
			fakeServer(t, filepath.Join(base, "home", ".dotnet", "tools"), "0.28.0")
			return nil, filepath.Join(base, "bin"), nil
		}, func(base string) langserver.Found {
			return langserver.Found{Path: filepath.Join(base, "home", ".dotnet", "tools", "csharp-ls"), Source: langserver.FromAfter}
		}, ""},
		{"the tools folder under DOTNET_CLI_HOME", func(t *testing.T, base string) (map[string]any, string, map[string]string) {
			fakeServer(t, filepath.Join(base, "cli", ".dotnet", "tools"), "0.28.0")
			return nil, "", map[string]string{"DOTNET_CLI_HOME": filepath.Join(base, "cli")}
		}, func(base string) langserver.Found {
			return langserver.Found{Path: filepath.Join(base, "cli", ".dotnet", "tools", "csharp-ls"), Source: langserver.FromAfter}
		}, ""},
		{"a name in the setting is looked for in PATH", func(t *testing.T, base string) (map[string]any, string, map[string]string) {
			fakeServer(t, filepath.Join(base, "bin"), "0.28.0")
			return map[string]any{PathSetting: "csharp-ls"}, filepath.Join(base, "bin"), nil
		}, func(base string) langserver.Found {
			return langserver.Found{Path: filepath.Join(base, "bin", "csharp-ls"), Source: langserver.FromSetting}
		}, ""},
		{"a wrong setting is not replaced by another program", func(t *testing.T, base string) (map[string]any, string, map[string]string) {
			fakeServer(t, filepath.Join(base, "bin"), "0.28.0")
			return map[string]any{PathSetting: filepath.Join(base, "nowhere", "csharp-ls")}, filepath.Join(base, "bin"), nil
		}, nil, "findSet.ServerPathInvalid"},
		{"nowhere", func(t *testing.T, base string) (map[string]any, string, map[string]string) {
			return nil, filepath.Join(base, "bin"), nil
		}, nil, "Find.ServerNotFound"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			base := t.TempDir()
			set, path, vars := tt.layout(t, base)
			prog := New().(*csharp).serverConfig().Programs[0]
			got, err := langserver.Find(prog, env(filepath.Join(base, "work"), filepath.Join(base, "home"), path, set, vars))
			if tt.route != "" {
				wantRoute(t, err, tt.route)
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			if want := tt.want(base); got != want {
				t.Errorf("found %+v, want %+v", got, want)
			}
		})
	}
}

func TestAStartRunsTheServerWithItsArgumentsEnvironmentAndSettings(t *testing.T) {
	r := newRig(t, rigOptions{files: map[string]string{"App.sln": "", "App/App.csproj": ""}})
	l, err := r.run()
	if err != nil {
		t.Fatal(err)
	}
	if l.Program != filepath.Join(r.bin, "csharp-ls") {
		t.Errorf("program = %s", l.Program)
	}
	if !slices.Equal(l.Args, []string{"--solution", "App.sln"}) {
		t.Errorf("args = %v", l.Args)
	}
	if l.Root != r.folder {
		t.Errorf("root = %s", l.Root)
	}
	for _, want := range dotnetEnv {
		if !slices.Contains(l.Env, want) {
			t.Errorf("the environment lacks %s: %v", want, l.Env)
		}
	}
	if !reflectEqual(l.ClientExperimental, map[string]any{"csharp": map[string]any{"metadataUris": true}}) {
		t.Errorf("client capabilities = %v", l.ClientExperimental)
	}
	cfg := launchConfiguration(t, l)
	want := map[string]any{"logLevel": "info", "analyzersEnabled": false, "applyFormattingOptions": false, "useMetadataUris": true}
	if !reflectEqual(cfg, want) {
		t.Errorf("configuration = %v, want %v", cfg, want)
	}
	if text := r.item().Text; !strings.Contains(text, "C#") {
		t.Errorf("item = %q", text)
	}
	if tip := r.item().Tooltip; !strings.Contains(tip, "Loads App.sln") {
		t.Errorf("tooltip = %q: it says what is loaded", tip)
	}
}

func TestTheSettingsReachTheServer(t *testing.T) {
	r := newRig(t, rigOptions{settings: map[string]any{LogLevelSetting: "debug", AnalyzersSetting: true, ApplyFormattingSetting: true, LocaleSetting: "de"}})
	l, err := r.run()
	if err != nil {
		t.Fatal(err)
	}
	if !slices.Equal(l.Args, []string{"--loglevel", "debug", "--locale", "de"}) {
		t.Errorf("args = %v", l.Args)
	}
	want := map[string]any{"logLevel": "debug", "analyzersEnabled": true, "applyFormattingOptions": true, "useMetadataUris": true}
	if cfg := launchConfiguration(t, l); !reflectEqual(cfg, want) {
		t.Errorf("configuration = %v, want %v", cfg, want)
	}
}

func TestAChangeOfASettingRestartsTheServer(t *testing.T) {
	for _, key := range []string{SolutionSetting, LogLevelSetting, AnalyzersSetting, ApplyFormattingSetting, LocaleSetting, PathSetting} {
		t.Run(key, func(t *testing.T) {
			r := newRig(t, rigOptions{files: map[string]string{"App.sln": ""}})
			if _, err := r.run(); err != nil {
				t.Fatal(err)
			}
			before := r.api.ServerRestarts(ServerID)
			var value any = "x"
			switch key {
			case AnalyzersSetting, ApplyFormattingSetting:
				value = true
			case LogLevelSetting:
				value = "debug"
			case PathSetting:
				value = filepath.Join(r.bin, "csharp-ls")
			}
			r.api.SetSetting(key, value)
			r.settle()
			if got := r.api.ServerRestarts(ServerID); got != before+1 {
				t.Errorf("restarts = %d, want %d", got, before+1)
			}
		})
	}
}

func TestEveryFailureOfAStartIsSaidAndLeavesTheServerMissing(t *testing.T) {
	tests := []struct {
		name    string
		o       rigOptions
		route   string
		item    string
		message string
	}{
		{"no csharp-ls", rigOptions{noServer: true}, "Find.ServerNotFound", "csharp-ls not found", "csharp-ls was not found."},
		{"a wrong path", rigOptions{settings: map[string]any{PathSetting: "/nowhere/csharp-ls"}}, "findSet.ServerPathInvalid", "wrong path", "csharp.languageServer.path is set to /nowhere/csharp-ls"},
		{"no .NET SDK", rigOptions{noDotnet: true}, "requireDotnet.CSharpDotnetNotFound", "csharp-ls cannot start", "the .NET SDK was not found"},
		{"a solution that is not there", rigOptions{settings: map[string]any{SolutionSetting: "Nope.sln"}}, "resolveTarget.CSharpSolutionInvalid", "csharp-ls cannot start", "csharp.solution is set to Nope.sln, which does not exist"},
		{"a file that is no solution", rigOptions{settings: map[string]any{SolutionSetting: "a.txt"}, files: map[string]string{"a.txt": ""}}, "resolveTarget.CSharpSolutionInvalid", "csharp-ls cannot start", "which is not a .sln, .slnx or .csproj file"},
		{"a folder as the solution", rigOptions{settings: map[string]any{SolutionSetting: "src"}, files: map[string]string{"src/a.cs": ""}}, "resolveTarget.CSharpSolutionInvalid", "csharp-ls cannot start", "which is a folder"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			r := newRig(t, tt.o)
			_, err := r.run()
			wantRoute(t, err, tt.route)
			if text := r.item().Text; !strings.Contains(text, tt.item) {
				t.Errorf("item = %q, want it to say %q", text, tt.item)
			}
			if !strings.Contains(strings.Join(r.notes(), "\n"), tt.message) {
				t.Errorf("notifications = %v, want one with %q", r.notes(), tt.message)
			}
		})
	}
}

func TestTheNotificationForAMissingServerOffersTheInstallCommand(t *testing.T) {
	r := newRig(t, rigOptions{noServer: true})
	if _, err := r.run(); err == nil {
		t.Fatal("the server started")
	}
	prog := r.ext.serverConfig().Programs[0]
	for _, family := range []string{langserver.FamilyFedora, langserver.FamilyDebian, langserver.FamilyArch, ""} {
		c, ok := prog.Install.CommandFor(family)
		if !ok || c.Command != "dotnet tool install --global csharp-ls" {
			t.Errorf("%q = %+v", family, c)
		}
	}
	var labels []string
	for _, n := range r.api.Notes() {
		labels = append(labels, n.Actions...)
	}
	for _, want := range []string{"How to Install", "Copy Install Command", "Set Path..."} {
		if !slices.Contains(labels, want) {
			t.Errorf("the notification lacks %q: %v", want, labels)
		}
	}
	if !strings.HasPrefix(prog.Install.Docs, "https://github.com/razzmatazz/csharp-language-server") {
		t.Errorf("docs = %s", prog.Install.Docs)
	}
}

func TestTheVersionOfTheServerIsRead(t *testing.T) {
	r := newRig(t, rigOptions{version: "0.21.3"})
	if _, err := r.run(); err != nil {
		t.Fatal(err)
	}
	if got := r.ext.ctl.Version(); got != "0.21.3" {
		t.Errorf("version = %q", got)
	}
}

func TestTheDotnetInstallationIsFoundTheWayAnApplicationLauncherDoes(t *testing.T) {
	tests := []struct {
		name   string
		layout func(t *testing.T, base string, c *csharp) (path string, vars map[string]string)
		want   func(base string) string
		from   func(base string) string
	}{
		{"DOTNET_ROOT", func(t *testing.T, base string, c *csharp) (string, map[string]string) {
			writeFile(t, base, "root/dotnet", "x", 0o755)
			writeFile(t, base, "bin/dotnet", "x", 0o755)
			return filepath.Join(base, "bin"), map[string]string{"DOTNET_ROOT": filepath.Join(base, "root")}
		}, func(base string) string { return filepath.Join(base, "root", "dotnet") }, func(string) string { return "DOTNET_ROOT" }},
		{"PATH", func(t *testing.T, base string, c *csharp) (string, map[string]string) {
			writeFile(t, base, "bin/dotnet", "x", 0o755)
			return filepath.Join(base, "bin"), nil
		}, func(base string) string { return filepath.Join(base, "bin", "dotnet") }, func(string) string { return "PATH" }},
		{"the install_location file", func(t *testing.T, base string, c *csharp) (string, map[string]string) {
			writeFile(t, base, "sdk/dotnet", "x", 0o755)
			loc := writeFile(t, base, "etc/install_location", filepath.Join(base, "sdk")+"\n", 0o644)
			c.installLocations = []string{filepath.Join(base, "etc", "install_location_x64"), loc}
			return "", nil
		}, func(base string) string { return filepath.Join(base, "sdk", "dotnet") }, func(base string) string { return filepath.Join(base, "etc", "install_location") }},
		{"a usual folder", func(t *testing.T, base string, c *csharp) (string, map[string]string) {
			writeFile(t, base, "usr/lib/dotnet/dotnet", "x", 0o755)
			c.systemDotnet = []string{filepath.Join(base, "usr", "share", "dotnet"), filepath.Join(base, "usr", "lib", "dotnet")}
			return "", nil
		}, func(base string) string { return filepath.Join(base, "usr", "lib", "dotnet", "dotnet") }, func(base string) string { return filepath.Join(base, "usr", "lib", "dotnet") }},
		{"the folder of the installer script", func(t *testing.T, base string, c *csharp) (string, map[string]string) {
			writeFile(t, base, "home/.dotnet/dotnet", "x", 0o755)
			return "", nil
		}, func(base string) string { return filepath.Join(base, "home", ".dotnet", "dotnet") }, func(base string) string { return filepath.Join(base, "home", ".dotnet") }},
		{"an install_location that names nothing", func(t *testing.T, base string, c *csharp) (string, map[string]string) {
			loc := writeFile(t, base, "etc/install_location", "\n", 0o644)
			c.installLocations = []string{loc}
			return "", nil
		}, nil, nil},
		{"nothing", func(t *testing.T, base string, c *csharp) (string, map[string]string) { return "", nil }, nil, nil},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			base := t.TempDir()
			c := &csharp{}
			path, vars := tt.layout(t, base, c)
			got, from, ok := c.findDotnet(env(base, filepath.Join(base, "home"), path, nil, vars))
			if tt.want == nil {
				if ok {
					t.Fatalf("found %s in %s", got, from)
				}
				wantRoute(t, c.requireDotnet(env(base, filepath.Join(base, "home"), path, nil, vars)), "requireDotnet.CSharpDotnetNotFound")
				return
			}
			if !ok || got != tt.want(base) || from != tt.from(base) {
				t.Errorf("found %s from %s (%v), want %s from %s", got, from, ok, tt.want(base), tt.from(base))
			}
			if err := c.requireDotnet(env(base, filepath.Join(base, "home"), path, nil, vars)); err != nil {
				t.Error(err)
			}
		})
	}
}

func TestTheInstallLocationFilesAreTheOnesOfTheArchitectureThenOfEveryOne(t *testing.T) {
	files := dotnetInstallLocations()
	if len(files) != 2 || !strings.HasPrefix(files[0], "/etc/dotnet/install_location_") || files[1] != "/etc/dotnet/install_location" {
		t.Errorf("files = %v", files)
	}
}

func TestACsharpFolderWithoutAFolderStartsTheServerWhereItIs(t *testing.T) {
	r := newRig(t, rigOptions{noFolder: true})
	l, err := r.run()
	if err != nil {
		t.Fatal(err)
	}
	if l.Root != "" || len(l.Args) != 0 {
		t.Errorf("root %q args %v", l.Root, l.Args)
	}
}

func TestNoWarningIsLoggedInNormalUse(t *testing.T) {
	var buf logged
	r := newRigWith(t, rigOptions{files: map[string]string{"App.sln": ""}}, func(a *fakeapi.API) { a.SetLogger(newLogger(&buf)) })
	if _, err := r.run(); err != nil {
		t.Fatal(err)
	}
	if strings.Contains(buf.text(), "level=WARN") || strings.Contains(buf.text(), "level=ERROR") {
		t.Errorf("log:\n%s", buf.text())
	}
}

// reflectEqual compares two values through their JSON, so that a map[string]any made in Go equals one read back from JSON.
func reflectEqual(a, b any) bool {
	x, err1 := json.Marshal(a)
	y, err2 := json.Marshal(b)
	return err1 == nil && err2 == nil && string(x) == string(y)
}
