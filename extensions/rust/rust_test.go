package rust

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/rm4n0s/errors"

	"github.com/rm4n0s/emetos/extension"
	"github.com/rm4n0s/emetos/extensions/internal/fakeapi"
	"github.com/rm4n0s/emetos/extensions/internal/langserver"
)

// The Rust pack over the fake API with a fake rust-analyzer: a script in a bin folder that prints a version. The tests make their own
// folders, programs and environment; nothing here reads the home folder or the PATH of the machine.

// rig is the pack activated in a tab with a folder.
type rig struct {
	t      *testing.T
	api    *fakeapi.API
	ext    *rust
	folder string
	bin    string
}

// newRig activates the pack. installed puts a rust-analyzer in the PATH; tweak may change the manifest before the fake reads it.
func newRig(t *testing.T, installed bool) *rig {
	t.Helper()
	base := t.TempDir()
	r := &rig{t: t, folder: filepath.Join(base, "work"), bin: filepath.Join(base, "bin")}
	for dir, files := range map[string]map[string]string{r.folder: {"Cargo.toml": "[package]\nname = \"x\"\n"}} {
		for name, text := range files {
			writeFile(t, dir, name, text, 0o644)
		}
	}
	if installed {
		writeFile(t, r.bin, "rust-analyzer", "#!/bin/sh\necho 'rust-analyzer 0.3.3097-standalone'\n", 0o755)
	} else if err := os.MkdirAll(r.bin, 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", r.bin)
	t.Setenv("HOME", filepath.Join(base, "home"))
	ext := New()
	if err := extension.CheckManifest(ext.Manifest()); err != nil {
		t.Fatal(err)
	}
	r.api = fakeapi.New(t, ext.Manifest(), extension.ScopeInfo{Name: "tab", Folder: r.folder})
	if err := ext.Activate(context.Background(), r.api); err != nil {
		t.Fatal(err)
	}
	r.ext = ext.(*rust)
	t.Cleanup(r.ext.wait)
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

// run starts the server as the host does (the launcher, then the state running) and waits for the pack to see it.
func (r *rig) run() {
	r.t.Helper()
	r.api.SetServerStatus(ServerID, extension.ServerStatus{State: extension.ServerStarting})
	if _, err := r.api.Launch(context.Background(), ServerID); err != nil {
		r.t.Fatal(err)
	}
	r.api.SetServerStatus(ServerID, extension.ServerStatus{State: extension.ServerRunning, Starts: 1})
	r.api.Pump()
}

// answer sets what the server answers to the requests of a method; the others get null.
func (r *rig) answer(answers map[string]string) {
	r.api.OnServerRequest(ServerID, func(req fakeapi.ServerRequest) (json.RawMessage, error) {
		if a, ok := answers[req.Method]; ok {
			return json.RawMessage(a), nil
		}
		return json.RawMessage("null"), nil
	})
}

// editor opens a Rust file with the text in front, with the cursor (an empty selection) or the selection given.
func (r *rig) editor(name, text string, sels ...extension.Selection) string {
	r.t.Helper()
	uri := "file://" + filepath.Join(r.folder, name)
	r.api.SetDocument(uri, text)
	if len(sels) == 0 {
		sels = []extension.Selection{{}}
	}
	r.api.SetActive(&extension.ActiveDocument{URI: uri, LanguageID: "rust", Selections: sels})
	return uri
}

// settle pumps the tab until the requests in flight have been answered and shown.
func (r *rig) settle() {
	r.t.Helper()
	r.ext.wait()
	r.api.Pump()
}

// note is the message of the last notification.
func (r *rig) note() string {
	r.t.Helper()
	notes := r.api.Notes()
	if len(notes) == 0 {
		return ""
	}
	return notes[len(notes)-1].Message
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

func TestTheManifestDeclaresTheServerTheCommandsAndTheSettings(t *testing.T) {
	m := New().Manifest()
	if err := extension.CheckManifest(m); err != nil {
		t.Fatal(err)
	}
	if len(m.Contributes.LanguageServers) != 1 {
		t.Fatalf("servers = %+v", m.Contributes.LanguageServers)
	}
	sv := m.Contributes.LanguageServers[0]
	if sv.ID != ServerID || !sv.OwnUI || sv.TraceKey != "rust-analyzer.trace.server" || !slices.Equal(sv.Languages, []string{"rust"}) || sv.Category != "Rust" {
		t.Errorf("server = %+v", sv)
	}
	ids := map[string]bool{}
	for _, c := range m.Contributes.Commands {
		ids[c.ID] = true
		if c.Category != commandCategory || c.Title == "" {
			t.Errorf("command %+v", c)
		}
	}
	for _, id := range []string{ExpandMacroCommand, ViewSyntaxTreeCommand, ParentModuleCommand, OpenCargoTomlCommand, JoinLinesCommand, MatchingBraceCommand, ReloadWorkspaceCommand, RestartServerCommand} {
		if !ids[id] {
			t.Errorf("the manifest lacks the command %s", id)
		}
	}
	keys := map[string]string{}
	for _, k := range m.Contributes.Keybindings {
		keys[k.Command] = k.Key
	}
	if keys[ParentModuleCommand] != "ctrl+shift+u" || keys[JoinLinesCommand] != "ctrl+shift+j" || len(keys) != 2 {
		t.Errorf("keybindings = %v", keys)
	}
	settingKeys := map[string]bool{}
	for _, s := range m.Contributes.Settings {
		settingKeys[s.Key] = true
	}
	for _, key := range []string{PathSetting, "rust-analyzer.cargo.features", "rust-analyzer.check.command", "rust-analyzer.procMacro.enable", "rust-analyzer.inlayHints.typeHints.enable", lensEnableSetting, lensRunEnableSetting} {
		if !settingKeys[key] {
			t.Errorf("the manifest lacks the setting %s", key)
		}
	}
	for _, key := range []string{"rust-analyzer.debug.engine", "rust-analyzer.statusBar.clickAction", "rust-analyzer.server.extraEnv", TraceSetting} {
		if settingKeys[key] {
			t.Errorf("the manifest has the setting %s, which is a setting of the client of VS Code or the host's", key)
		}
	}
	if !slices.Contains(m.Events.Subscribes, extension.TopicEditorActiveChanged) {
		t.Errorf("events = %+v", m.Events)
	}
}

func TestTheSettingsListIsWhole(t *testing.T) {
	list, err := loadSettings()
	if err != nil {
		t.Fatal(err)
	}
	if len(list) < 150 {
		t.Fatalf("%d settings; rust-analyzer has about two hundred", len(list))
	}
	types := map[string]bool{"string": true, "number": true, "integer": true, "boolean": true, "array": true, "object": true}
	seen := map[string]bool{}
	for _, s := range list {
		switch {
		case !strings.HasPrefix(s.Key, SettingPrefix):
			t.Errorf("%s is not a setting of rust-analyzer", s.Key)
		case seen[s.Key]:
			t.Errorf("%s twice", s.Key)
		case !types[s.Type]:
			t.Errorf("%s has the type %q", s.Key, s.Type)
		case s.Description == "":
			t.Errorf("%s has no description", s.Key)
		}
		for _, also := range s.AlsoTypes {
			if !types[also] && also != "null" {
				t.Errorf("%s also has the type %q", s.Key, also)
			}
		}
		seen[s.Key] = true
	}
	// a few with their VS Code defaults
	byKey := map[string]raSetting{}
	for _, s := range list {
		byKey[s.Key] = s
	}
	if s := byKey["rust-analyzer.check.command"]; s.Default != "check" || s.Type != "string" {
		t.Errorf("check.command = %+v", s)
	}
	if s := byKey["rust-analyzer.cargo.features"]; s.Type != "string" || !slices.Contains(s.AlsoTypes, "array") {
		t.Errorf("cargo.features = %+v", s)
	}
	if s := byKey["rust-analyzer.inlayHints.discriminantHints.enable"]; len(s.Enum) != 3 || len(s.EnumDescriptions) != 3 {
		t.Errorf("discriminantHints = %+v", s)
	}
	if s := byKey["rust-analyzer.typing.triggerChars"]; s.Default != "=." || !slices.Contains(s.AlsoTypes, "null") {
		t.Errorf("typing.triggerChars = %+v", s)
	}
}

func TestOptionsAreTheSettingsThatAreNotTheirDefaults(t *testing.T) {
	tests := []struct {
		name   string
		values map[string]any
		want   string
	}{
		{"nothing is set", nil, `{}`},
		{"a default is left out", map[string]any{"rust-analyzer.check.command": "check", "rust-analyzer.procMacro.enable": true}, `{}`},
		{"one level", map[string]any{"rust-analyzer.check.command": "clippy"}, `{"check":{"command":"clippy"}}`},
		{"nested", map[string]any{"rust-analyzer.inlayHints.typeHints.enable": false, "rust-analyzer.inlayHints.parameterHints.enable": false},
			`{"inlayHints":{"parameterHints":{"enable":false},"typeHints":{"enable":false}}}`},
		{"a list", map[string]any{"rust-analyzer.cargo.features": []any{"serde", "full"}}, `{"cargo":{"features":["serde","full"]}}`},
		{"all", map[string]any{"rust-analyzer.cargo.features": "all"}, `{"cargo":{"features":"all"}}`},
		{"a key that is no setting", map[string]any{"rust-analyzer.nothing.here": 1}, `{}`},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			s := langserver.NewSettings(func(key string) (any, bool) {
				if v, ok := tt.values[key]; ok {
					return v, true
				}
				list, _ := loadSettings()
				for _, set := range list {
					if set.Key == key {
						return set.Default, true
					}
				}
				return nil, false
			})
			got, err := json.Marshal(serverOptions(s))
			if err != nil {
				t.Fatal(err)
			}
			if string(got) != tt.want {
				t.Errorf("options = %s, want %s", got, tt.want)
			}
			if !slices.Contains(s.Keys(), "rust-analyzer.check.command") {
				t.Error("the settings were not all read, so a change of one would restart nothing")
			}
		})
	}
}

func TestANameThatStartsAnotherIsLeftOut(t *testing.T) {
	tests := []struct {
		name  string
		paths [][]string
		want  string
	}{
		{"siblings", [][]string{{"a", "b"}, {"a", "c"}}, `{"a":{"b":1,"c":2}}`},
		{"a leaf, then a longer name", [][]string{{"a", "b"}, {"a", "b", "c"}}, `{"a":{"b":1}}`},
		{"a longer name, then a leaf", [][]string{{"a", "b", "c"}, {"a", "b"}}, `{"a":{"b":{"c":1}}}`},
		{"a name twice", [][]string{{"a"}, {"a"}}, `{"a":1}`},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			m := map[string]any{}
			for i, p := range tt.paths {
				setNested(m, p, i+1)
			}
			got, _ := json.Marshal(m)
			if string(got) != tt.want {
				t.Errorf("got %s, want %s", got, tt.want)
			}
		})
	}
}

func TestTheLauncherStartsRustAnalyzerWithTheSettingsAsOptions(t *testing.T) {
	r := newRig(t, true)
	r.api.SetSetting("rust-analyzer.check.command", "clippy")
	r.api.SetSetting("rust-analyzer.cargo.features", "all")
	l, err := r.api.Launch(context.Background(), ServerID)
	if err != nil {
		t.Fatal(err)
	}
	if filepath.Base(l.Program) != "rust-analyzer" || l.Root != r.folder {
		t.Errorf("launch = %+v", l)
	}
	opts, _ := json.Marshal(l.InitializationOptions)
	if string(opts) != `{"cargo":{"features":"all"},"check":{"command":"clippy"}}` {
		t.Errorf("initialization options = %s", opts)
	}
	section, ok := l.Configuration("rust-analyzer")
	if !ok || string(section) != string(opts) {
		t.Errorf("the answer for the section rust-analyzer = %s, %v", section, ok)
	}
	if sub, ok := l.Configuration("rust-analyzer.check"); !ok || string(sub) != `{"command":"clippy"}` {
		t.Errorf("the answer for rust-analyzer.check = %s, %v", sub, ok)
	}
	if _, ok := l.Configuration("gopls"); ok {
		t.Error("a section of another server was answered")
	}
	exp, _ := json.Marshal(l.ClientExperimental)
	if string(exp) != `{"commands":{"commands":[]},"serverStatusNotification":true}` {
		t.Errorf("experimental capabilities = %s", exp)
	}
}

func TestChangingASettingRestartsRustAnalyzer(t *testing.T) {
	r := newRig(t, true)
	r.run()
	r.api.SetSetting("rust-analyzer.check.command", "clippy")
	if got := r.api.ServerRestarts(ServerID); got != 1 {
		t.Errorf("restarts = %d after a setting rust-analyzer reads changed", got)
	}
	r.run() // the server started again, and read the settings again
	r.api.SetSetting("editor.fontSize", 12)
	if got := r.api.ServerRestarts(ServerID); got != 1 {
		t.Errorf("restarts = %d after a setting it does not read changed", got)
	}
}

func TestARustAnalyzerThatPrintsNoVersionIsRefused(t *testing.T) {
	r := newRig(t, false)
	// rustup's proxy for a component that is not installed: it prints an error and no version
	writeFile(t, r.bin, "rust-analyzer", "#!/bin/sh\necho \"error: Unknown binary 'rust-analyzer' in official toolchain\" >&2\nexit 1\n", 0o755)
	_, err := r.api.Launch(context.Background(), ServerID)
	if err == nil {
		t.Fatal("the proxy was started")
	}
	wantRoute(t, err, "customize.RustAnalyzerVersionFailed")
	if !strings.Contains(err.Error(), "rustup component add rust-analyzer") {
		t.Errorf("the message does not say what to do: %v", err)
	}
}

func TestAMissingRustAnalyzerGivesTheInstallHelp(t *testing.T) {
	r := newRig(t, false)
	if _, err := r.api.Launch(context.Background(), ServerID); err == nil {
		t.Fatal("the launcher found a rust-analyzer")
	}
	r.api.SetServerStatus(ServerID, extension.ServerStatus{State: extension.ServerMissing})
	r.api.Pump()
	notes := r.api.Notes()
	if len(notes) != 1 || notes[0].Message != "rust-analyzer was not found." || !slices.Contains(notes[0].Actions, "Copy Install Command") {
		t.Fatalf("notes = %+v", notes)
	}
	r.api.RunNoteAction(0, "Copy Install Command")
	got := r.api.ClipboardTexts()
	if len(got) != 1 || !strings.Contains(got[0], "rust-analyzer") {
		t.Errorf("clipboard = %v", got)
	}
}

func TestTheStatusItemShowsWhatRustAnalyzerSaysOfItself(t *testing.T) {
	r := newRig(t, true)
	r.run()
	r.api.Emit(extension.TopicEditorActiveChanged, extension.ActiveEditor{URI: "file:///work/main.rs", LanguageID: "rust"})
	r.api.Pump()
	item := func() string { return r.api.StatusItem(langserver.StatusID(ServerID)).Text }
	if !strings.Contains(item(), "$(check)") {
		t.Fatalf("item = %q", item())
	}
	before := r.api.CodeLensRefreshes()
	r.api.ServerNotify(ServerID, statusMethod, map[string]any{"health": "ok", "quiescent": false})
	if !strings.Contains(item(), "$(sync~spin)") {
		t.Errorf("item = %q while the server works", item())
	}
	r.api.ServerNotify(ServerID, statusMethod, map[string]any{"health": "warning", "quiescent": true, "message": "Failed to load the build script"})
	if !strings.Contains(item(), "$(warning)") || !strings.Contains(r.api.StatusItem(langserver.StatusID(ServerID)).Tooltip, "Failed to load the build script") {
		t.Errorf("item = %q, tooltip %q", item(), r.api.StatusItem(langserver.StatusID(ServerID)).Tooltip)
	}
	r.api.ServerNotify(ServerID, statusMethod, map[string]any{"health": "error", "quiescent": true, "message": "no workspace"})
	if !strings.Contains(item(), "$(error)") {
		t.Errorf("item = %q", item())
	}
	r.api.ServerNotify(ServerID, statusMethod, map[string]any{"health": "ok", "quiescent": true})
	if !strings.Contains(item(), "$(check)") {
		t.Errorf("item = %q once the server is idle", item())
	}
	// the lenses are asked for again once, when the workspace is loaded
	if got := r.api.CodeLensRefreshes() - before; got != 1 {
		t.Errorf("the lenses were refreshed %d times", got)
	}
	// a notification that cannot be read changes nothing
	r.api.ServerNotify(ServerID, statusMethod, "nonsense")
	if !strings.Contains(item(), "$(check)") {
		t.Errorf("item = %q after a bad notification", item())
	}
	// a restart forgets the health
	r.api.ServerNotify(ServerID, statusMethod, map[string]any{"health": "error", "quiescent": true})
	r.api.SetServerStatus(ServerID, extension.ServerStatus{State: extension.ServerStarting})
	r.api.SetServerStatus(ServerID, extension.ServerStatus{State: extension.ServerRunning})
	if !strings.Contains(item(), "$(check)") {
		t.Errorf("item = %q after the server started again", item())
	}
}
