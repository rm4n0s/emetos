package typescript

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

// The TypeScript pack over the fake API with a fake typescript-language-server (a script in a node_modules folder that a link in the bin
// folder points to, as npm lays it out), fake TypeScript packages and a fake node: folders the tests make. Nothing here reads the home
// folder, the PATH or the environment of the machine: every test sets its own (t.Setenv).

// rig is the pack activated in a tab with a folder.
type rig struct {
	t      *testing.T
	api    *fakeapi.API
	ext    *typescript
	base   string
	folder string
	bin    string
	home   string
	// lib is the folder npm -g installs into (the packages are in lib/node_modules).
	lib string
}

// rigOptions say what the machine of a test has.
type rigOptions struct {
	// noServer leaves the bin folder with no typescript-language-server, noNode with no node.
	noServer, noNode bool
	// globalTS is the version of the TypeScript installed beside the server ("" for none); noTsserver makes it a package with no tsserver.js
	// (TypeScript 7).
	globalTS    string
	noTsserver  bool
	workspaceTS string
	// files are written to the folder of the tab (name to text).
	files map[string]string
	// state is what was kept before the pack was activated: the TypeScript the user chose for the folder.
	state string
	// settings are set before the pack is activated.
	settings map[string]any
	// noFolder makes the tab a tab without a folder.
	noFolder bool
}

// newRig activates the pack.
func newRig(t *testing.T, o rigOptions) *rig { t.Helper(); return newRigWith(t, o, nil) }

// newRigWith activates the pack; before makes the fake API ready (its logger) before the activation.
func newRigWith(t *testing.T, o rigOptions, before func(*fakeapi.API)) *rig {
	t.Helper()
	base := t.TempDir()
	r := &rig{t: t, base: base, folder: filepath.Join(base, "work"), bin: filepath.Join(base, "bin"), home: filepath.Join(base, "home"), lib: filepath.Join(base, "npm", "lib")}
	for _, d := range []string{r.folder, r.bin, r.home} {
		if err := os.MkdirAll(d, 0o755); err != nil {
			t.Fatal(err)
		}
	}
	for name, text := range o.files {
		writeFile(t, r.folder, name, text, 0o644)
	}
	if !o.noServer {
		script := writeFile(t, r.lib, "node_modules/typescript-language-server/lib/cli.mjs", "#!/bin/sh\necho 6.0.2\n", 0o755)
		if err := os.Symlink(script, filepath.Join(r.bin, "typescript-language-server")); err != nil {
			t.Fatal(err)
		}
	}
	if !o.noNode {
		writeFile(t, r.bin, "node", "#!/bin/sh\necho v22.23.1\n", 0o755)
	}
	if o.globalTS != "" {
		fakeTypeScript(t, filepath.Join(r.lib, "node_modules", "typescript"), o.globalTS, !o.noTsserver)
	}
	if o.workspaceTS != "" {
		fakeTypeScript(t, filepath.Join(r.folder, "node_modules", "typescript"), o.workspaceTS, true)
	}
	t.Setenv("PATH", r.bin)
	t.Setenv("HOME", r.home)
	ext := New()
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
	r.ext = ext.(*typescript)
	t.Cleanup(r.settle)
	r.settle()
	return r
}

// fakeTypeScript makes a TypeScript package at dir: the package.json with the version and lib/tsserver.js (a TypeScript 7 has none).
func fakeTypeScript(t *testing.T, dir, version string, tsserver bool) {
	t.Helper()
	writeFile(t, dir, "package.json", `{"name":"typescript","version":"`+version+`"}`, 0o644)
	writeFile(t, dir, "lib/tsc.js", "", 0o644)
	if tsserver {
		writeFile(t, dir, "lib/tsserver.js", "", 0o644)
	}
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

// editor puts a file of the language in front, with the selections (a cursor at the start when none is given).
func (r *rig) editor(name, language, text string, sels ...extension.Selection) string {
	r.t.Helper()
	if len(sels) == 0 {
		sels = []extension.Selection{{}}
	}
	uri := "file://" + filepath.Join(r.folder, name)
	r.api.SetDocument(uri, text)
	r.api.SetActive(&extension.ActiveDocument{URI: uri, LanguageID: language, Selections: sels})
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

// note is the message of the last notification, "" when there is none.
func (r *rig) note() string {
	if n := r.notes(); len(n) > 0 {
		return n[len(n)-1]
	}
	return ""
}

// requests are the params of the requests of a method the pack sent.
func (r *rig) requests(method string) []string {
	var out []string
	for _, req := range r.api.ServerRequests(ServerID) {
		if req.Method == method {
			out = append(out, string(req.Params))
		}
	}
	return out
}

// answer sets the answers of the server to the requests of the pack, by method (a JSON text); a method with no answer is answered with null.
func (r *rig) answer(answers map[string]string) {
	r.api.OnServerRequest(ServerID, func(req fakeapi.ServerRequest) (json.RawMessage, error) {
		if a, ok := answers[req.Method]; ok {
			return json.RawMessage(a), nil
		}
		return json.RawMessage("null"), nil
	})
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

// launchOptions is the initialization options of the launch as JSON.
func launchOptions(t *testing.T, l extension.ServerLaunch) string {
	t.Helper()
	raw, err := json.Marshal(l.InitializationOptions)
	if err != nil {
		t.Fatal(err)
	}
	return string(raw)
}

// launchSettings is the settings the launch sends with workspace/didChangeConfiguration, as JSON.
func launchSettings(t *testing.T, l extension.ServerLaunch) string {
	t.Helper()
	raw, err := json.Marshal(l.Settings)
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
	if sv.ID != ServerID || !sv.OwnUI || sv.TraceKey != TraceSetting || sv.Category != "TypeScript" {
		t.Errorf("server = %+v", sv)
	}
	// the server serves the languages of the JavaScript pack too
	if !slices.Equal(sv.Languages, []string{"typescript", "typescriptreact", "javascript", "javascriptreact"}) {
		t.Errorf("languages = %v", sv.Languages)
	}
	if sv.Program != "typescript-language-server" || !slices.Equal(sv.Args, []string{"--stdio"}) {
		t.Errorf("program = %q %v", sv.Program, sv.Args)
	}
	settings := map[string]extension.SettingSpec{}
	for _, s := range m.Contributes.Settings {
		settings[s.Key] = s
	}
	// what decides which program or code Emetos runs is read from the user's settings only
	for _, key := range []string{PathSetting, NodePathSetting, TsdkSetting, NpmPathSetting} {
		s, ok := settings[key]
		if !ok {
			t.Errorf("the manifest lacks the setting %s", key)
		} else if s.Scope != "machine" {
			t.Errorf("%s has the scope %q: a folder's settings must not decide what Emetos runs", key, s.Scope)
		}
	}
	for key, want := range map[string]any{
		PromptWorkspaceVersionSetting: true, MaxMemorySetting: float64(3072), LocaleSetting: "auto", DisableTypeAcquisitionSetting: false,
		"typescript.preferences.quoteStyle": "auto", "javascript.preferences.quoteStyle": "auto", "typescript.format.semicolons": "ignore",
		"javascript.format.insertSpaceAfterCommaDelimiter": true, "typescript.inlayHints.parameterNames.enabled": "none",
		"typescript.inlayHints.parameterNames.suppressWhenArgumentMatchesName": true, "javascript.referencesCodeLens.enabled": false,
		"typescript.suggest.autoImports": true, "typescript.preferences.importModuleSpecifier": "shortest",
	} {
		if s, ok := settings[key]; !ok || s.Default != want {
			t.Errorf("%s = %+v, want the default %v", key, s, want)
		}
	}
	// what VS Code has for TypeScript only is not there for JavaScript
	for _, key := range []string{"typescript.preferences.preferTypeOnlyAutoImports", "typescript.inlayHints.enumMemberValues.enabled", "typescript.format.insertSpaceAfterTypeAssertion"} {
		if _, ok := settings[key]; !ok {
			t.Errorf("the manifest lacks %s", key)
		}
		if _, ok := settings[strings.Replace(key, "typescript.", "javascript.", 1)]; ok {
			t.Errorf("%s has a JavaScript twin", key)
		}
	}
	if len(settings) < 90 {
		t.Errorf("only %d settings are declared", len(settings))
	}
	if _, ok := settings[TraceSetting]; ok {
		t.Errorf("%s is the host's, not a setting the pack declares", TraceSetting)
	}
	if !slices.Contains(m.Events.Subscribes, extension.TopicEditorActiveChanged) {
		t.Errorf("events = %+v", m.Events)
	}
	if m.Name != "TypeScript" {
		t.Errorf("name = %q", m.Name)
	}
}

func TestTheLauncherStartsTheServerWithTheTypeScriptBesideIt(t *testing.T) {
	r := newRig(t, rigOptions{globalTS: "6.0.3", workspaceTS: "5.9.3", files: map[string]string{"tsconfig.json": "{}"}})
	l, err := r.run()
	if err != nil {
		t.Fatal(err)
	}
	if l.Program != filepath.Join(r.bin, "typescript-language-server") || !slices.Equal(l.Args, []string{"--stdio"}) || l.Root != r.folder {
		t.Errorf("launch = %s %v in %s", l.Program, l.Args, l.Root)
	}
	// the folder has a TypeScript of its own, which is not the one named: Emetos does not run what the folder brought
	want := `{"hostInfo":"emetos","maxTsServerMemory":3072,"tsserver":{"path":"` + filepath.Join(r.lib, "node_modules", "typescript", "lib", "tsserver.js") + `"}}`
	if got := launchOptions(t, l); got != want {
		t.Errorf("initialization options = %s\nwant %s", got, want)
	}
	if l.Settings != nil {
		t.Errorf("settings = %v", l.Settings)
	}
	if it := r.item(); it.Text != "$(check) TypeScript 6.0.3" || !strings.Contains(it.Tooltip, "(global)") || !strings.Contains(it.Tooltip, "typescript-language-server 6.0.2") {
		t.Errorf("item = %q\n%s", it.Text, it.Tooltip)
	}
	// tsserver asks the client for the indentation of the edits it makes
	raw, ok := l.Configuration("formattingOptions")
	if !ok || string(raw) != `{"insertSpaces":true,"tabSize":4}` {
		t.Errorf("formattingOptions = %s %v", raw, ok)
	}
}

func TestTheServerRunsInTheNearestTypeScriptProject(t *testing.T) {
	tests := []struct {
		name string
		// files are written to the folder of the tab, above is a file written to the folder that holds it
		files map[string]string
		above string
		root  func(r *rig) string
	}{
		{"the folder holds tsconfig.json", map[string]string{"tsconfig.json": "{}"}, "", func(r *rig) string { return r.folder }},
		{"the folder holds jsconfig.json", map[string]string{"jsconfig.json": "{}"}, "", func(r *rig) string { return r.folder }},
		{"the folder holds package.json", map[string]string{"package.json": "{}"}, "", func(r *rig) string { return r.folder }},
		{"the project is above the folder", nil, "tsconfig.json", func(r *rig) string { return r.base }},
		{"no marker: the folder", nil, "", func(r *rig) string { return r.folder }},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			r := newRig(t, rigOptions{globalTS: "6.0.3", files: tt.files})
			if tt.above != "" {
				writeFile(t, r.base, tt.above, "{}", 0o644)
			}
			l, err := r.run()
			if err != nil {
				t.Fatal(err)
			}
			if want := tt.root(r); l.Root != want {
				t.Errorf("root = %s, want %s", l.Root, want)
			}
		})
	}
}

func TestNodeRunsTheServerWhenTheSettingNamesIt(t *testing.T) {
	r := newRig(t, rigOptions{globalTS: "6.0.3"})
	node := writeFile(t, r.base, "other/node", "#!/bin/sh\n", 0o755)
	r.api.SetSetting(NodePathSetting, node)
	l, err := r.run()
	if err != nil {
		t.Fatal(err)
	}
	// the script is run by the node of the setting; the item still names the script as the server
	script := filepath.Join(r.lib, "node_modules", "typescript-language-server", "lib", "cli.mjs")
	if l.Program != node || !slices.Equal(l.Args, []string{script, "--stdio"}) {
		t.Errorf("launch = %s %v", l.Program, l.Args)
	}
	if tip := r.item().Tooltip; !strings.Contains(tip, filepath.Join(r.bin, "typescript-language-server")) {
		t.Errorf("tooltip = %s", tip)
	}
}

func TestTheNodeThatCannotBeUsedIsReported(t *testing.T) {
	tests := []struct {
		name  string
		opts  rigOptions
		node  string
		route string
		note  string
	}{
		{"no node on PATH", rigOptions{globalTS: "6.0.3", noNode: true}, "", "resolveNode.TypeScriptNodeNotFound", "node was not found in PATH"},
		{"the setting names a file that is not there", rigOptions{globalTS: "6.0.3"}, "/no/such/node", "resolveNode.TypeScriptNodeInvalid", "/no/such/node does not exist"},
		{"the setting names a name that is not on PATH", rigOptions{globalTS: "6.0.3"}, "nodejs", "resolveNode.TypeScriptNodeInvalid", "not a program in PATH"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			r := newRig(t, tt.opts)
			if tt.node != "" {
				r.api.SetSetting(NodePathSetting, tt.node)
			}
			_, err := r.run()
			if err == nil {
				t.Fatal("the server started")
			}
			wantRoute(t, err, tt.route)
			if !strings.Contains(err.Error(), tt.note) {
				t.Errorf("error = %v, want %q", err, tt.note)
			}
			if it := r.item(); !strings.Contains(it.Text, "cannot start") {
				t.Errorf("item = %q", it.Text)
			}
		})
	}
}

func TestAMissingServerGivesTheNotificationWithTheInstallCommand(t *testing.T) {
	r := newRig(t, rigOptions{noServer: true})
	r.editor("a.ts", "typescript", "")
	_, err := r.run()
	if err == nil {
		t.Fatal("the server started")
	}
	r.api.Emit(extension.TopicEditorActiveChanged, extension.ActiveEditor{URI: "file://" + filepath.Join(r.folder, "a.ts"), LanguageID: "typescript"})
	r.settle()
	if got := r.notes(); len(got) == 0 || got[0] != "typescript-language-server was not found." {
		t.Fatalf("notes = %v", got)
	}
	r.api.RunNoteAction(0, "Copy Install Command")
	r.settle()
	if texts := r.api.ClipboardTexts(); len(texts) != 1 || texts[0] != "npm install -g typescript-language-server typescript@6" {
		t.Errorf("clipboard = %q", texts)
	}
	if it := r.item(); it.Text != "$(warning) TypeScript: typescript-language-server not found" {
		t.Errorf("item = %q", it.Text)
	}
}

func TestAChangeOfASettingTheServerReadsRestartsIt(t *testing.T) {
	r := newRig(t, rigOptions{globalTS: "6.0.3"})
	if _, err := r.run(); err != nil {
		t.Fatal(err)
	}
	before := r.api.ServerRestarts(ServerID)
	r.api.SetSetting("typescript.format.semicolons", "remove")
	r.settle()
	if got := r.api.ServerRestarts(ServerID); got != before+1 {
		t.Errorf("restarts = %d, want %d", got, before+1)
	}
}
