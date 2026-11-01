package cpp

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

// The C and C++ pack over the fake API with a fake clangd: a script in a bin folder that prints a version. The tests make their own
// folders, programs and environment; nothing here reads the home folder or the PATH of the machine.

// rig is the pack activated in a tab with a folder.
type rig struct {
	t      *testing.T
	api    *fakeapi.API
	ext    *cpp
	folder string
	bin    string
}

// newRig activates the pack. installed puts a clangd in the PATH; files are written to the folder of the tab (name to text).
func newRig(t *testing.T, installed bool, files map[string]string) *rig {
	t.Helper()
	base := t.TempDir()
	r := &rig{t: t, folder: filepath.Join(base, "work"), bin: filepath.Join(base, "bin")}
	if err := os.MkdirAll(r.folder, 0o755); err != nil {
		t.Fatal(err)
	}
	for name, text := range files {
		writeFile(t, r.folder, name, text, 0o644)
	}
	if installed {
		writeFile(t, r.bin, "clangd", "#!/bin/sh\necho 'clangd version 22.1.8 (Fedora 22.1.8-4.fc44)'\n", 0o755)
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
	r.ext = ext.(*cpp)
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

// editor opens a file in front with the text, in the language, with the cursor (an empty selection) or the selection given.
func (r *rig) editor(name, language, text string, sels ...extension.Selection) string {
	r.t.Helper()
	uri := "file://" + filepath.Join(r.folder, name)
	r.api.SetDocument(uri, text)
	if len(sels) == 0 {
		sels = []extension.Selection{{}}
	}
	r.api.SetActive(&extension.ActiveDocument{URI: uri, LanguageID: language, Selections: sels})
	r.api.Emit(extension.TopicEditorActiveChanged, extension.ActiveEditor{URI: uri, LanguageID: language})
	r.api.Pump()
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

// item is the status item of the server.
func (r *rig) item() *fakeapi.StatusItem { return r.api.StatusItem(langserver.StatusID(ServerID)) }

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
	if sv.ID != ServerID || !sv.OwnUI || sv.TraceKey != TraceSetting || !slices.Equal(sv.Languages, []string{"c", "cpp", "cuda-cpp"}) || sv.Category != "clangd" {
		t.Errorf("server = %+v", sv)
	}
	if !slices.Equal(sv.WatchedFiles[:3], []string{"**/compile_commands.json", "**/compile_flags.txt", "**/.clangd"}) {
		t.Errorf("watched files = %v", sv.WatchedFiles)
	}
	settingKeys := map[string]extension.SettingSpec{}
	for _, s := range m.Contributes.Settings {
		settingKeys[s.Key] = s
	}
	for _, key := range []string{PathSetting, ArgumentsSetting, FallbackFlagsSetting, TraceFileSetting} {
		s, ok := settingKeys[key]
		if !ok {
			t.Errorf("the manifest lacks the setting %s", key)
		} else if key != FallbackFlagsSetting && s.Scope != "machine" {
			t.Errorf("%s has the scope %q: a folder's settings must not decide what clangd runs or writes", key, s.Scope)
		}
	}
	if settingKeys[FallbackFlagsSetting].Type != "array" || settingKeys[ArgumentsSetting].Type != "array" {
		t.Errorf("the lists are not arrays: %+v", settingKeys)
	}
	if _, ok := settingKeys[TraceSetting]; ok {
		t.Errorf("%s is the host's, not a setting the pack declares", TraceSetting)
	}
	if !slices.Equal(m.Contributes.WritesSettings, []string{InlayHintsSetting}) {
		t.Errorf("writes = %v", m.Contributes.WritesSettings)
	}
	if !slices.Contains(m.Events.Subscribes, extension.TopicEditorActiveChanged) {
		t.Errorf("events = %+v", m.Events)
	}
	if m.Name != "C and C++" {
		t.Errorf("name = %q", m.Name)
	}
}

func TestTheLauncherStartsClangdWithTheSettings(t *testing.T) {
	r := newRig(t, true, map[string]string{"compile_commands.json": "[]"})
	r.api.SetSetting(ArgumentsSetting, []any{"--background-index", "--header-insertion=never"})
	r.api.SetSetting(FallbackFlagsSetting, []any{"-std=c++20", "-Iinclude"})
	r.api.SetSetting(TraceFileSetting, "${workspaceFolder}/clangd-trace.json")
	l, err := r.api.Launch(context.Background(), ServerID)
	if err != nil {
		t.Fatal(err)
	}
	if filepath.Base(l.Program) != "clangd" || l.Root != r.folder {
		t.Errorf("launch = %+v", l)
	}
	if !slices.Equal(l.Args, []string{"--background-index", "--header-insertion=never"}) {
		t.Errorf("args = %v", l.Args)
	}
	opts, _ := json.Marshal(l.InitializationOptions)
	if string(opts) != `{"clangdFileStatus":true,"fallbackFlags":["-std=c++20","-Iinclude"]}` {
		t.Errorf("initialization options = %s", opts)
	}
	if !slices.Contains(l.Env, "CLANGD_TRACE="+r.folder+"/clangd-trace.json") {
		t.Errorf("env = %v", l.Env)
	}
}

func TestTheLauncherSendsOnlyTheFileStatusRequestWhenNothingIsSet(t *testing.T) {
	r := newRig(t, true, nil)
	l, err := r.api.Launch(context.Background(), ServerID)
	if err != nil {
		t.Fatal(err)
	}
	opts, _ := json.Marshal(l.InitializationOptions)
	if string(opts) != `{"clangdFileStatus":true}` || len(l.Args) != 0 || len(l.Env) != 0 {
		t.Errorf("options %s, args %v, env %v", opts, l.Args, l.Env)
	}
}

func TestTheServerRunsInTheNearestFolderWithAProjectFile(t *testing.T) {
	base := t.TempDir()
	tests := []struct {
		name  string
		files map[string]string
		want  string // the root, relative to the folder of the tab's parent; "." is the folder itself
	}{
		{"the database is in the folder", map[string]string{"work/compile_commands.json": "[]"}, "work"},
		{"a compile_flags.txt marks it too", map[string]string{"work/compile_flags.txt": "-std=c11"}, "work"},
		{"a .clangd marks it too", map[string]string{"work/.clangd": "Diagnostics: {}"}, "work"},
		{"the database is above", map[string]string{"compile_commands.json": "[]", "work/src/.keep": ""}, "."},
		{"no marker: the folder", map[string]string{"work/main.cpp": ""}, "work"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			root := filepath.Join(base, strings.NewReplacer(" ", "-", ":", "").Replace(tt.name))
			for name, text := range tt.files {
				writeFile(t, root, name, text, 0o644)
			}
			folder := filepath.Join(root, "work")
			if tt.name == "the database is above" {
				folder = filepath.Join(root, "work", "src")
			}
			writeFile(t, filepath.Join(root, "bin"), "clangd", "#!/bin/sh\necho 'clangd version 22.1.8'\n", 0o755)
			t.Setenv("PATH", filepath.Join(root, "bin"))
			t.Setenv("HOME", filepath.Join(root, "home"))
			ext := New()
			api := fakeapi.New(t, ext.Manifest(), extension.ScopeInfo{Name: "tab", Folder: folder})
			if err := ext.Activate(context.Background(), api); err != nil {
				t.Fatal(err)
			}
			l, err := api.Launch(context.Background(), ServerID)
			if err != nil {
				t.Fatal(err)
			}
			if want := filepath.Join(root, tt.want); l.Root != want {
				t.Errorf("root = %s, want %s", l.Root, want)
			}
		})
	}
}

func TestChangingASettingRestartsClangd(t *testing.T) {
	r := newRig(t, true, nil)
	r.run()
	r.api.SetSetting(ArgumentsSetting, []any{"--clang-tidy"})
	if got := r.api.ServerRestarts(ServerID); got != 1 {
		t.Errorf("restarts = %d after a setting clangd reads changed", got)
	}
	r.run() // the server started again, and read the settings again
	r.api.SetSetting("editor.fontSize", 12)
	if got := r.api.ServerRestarts(ServerID); got != 1 {
		t.Errorf("restarts = %d after a setting it does not read changed", got)
	}
}

func TestAMissingClangdGivesTheInstallHelpOfTheSystem(t *testing.T) {
	tests := []struct {
		osRelease, want string
	}{
		{"ID=fedora\n", "sudo dnf install clang-tools-extra"},
		{"ID=ubuntu\nID_LIKE=debian\n", "sudo apt install clangd"},
		{"ID=arch\n", "sudo pacman -S clang"},
		{"ID=opensuse-tumbleweed\n", "sudo zypper install clang-tools"},
	}
	for _, tt := range tests {
		t.Run(tt.want, func(t *testing.T) {
			base := t.TempDir()
			t.Setenv("PATH", base)
			t.Setenv("HOME", base)
			ext := New().(*cpp)
			cfg := ext.serverConfig()
			api := fakeapi.New(t, ext.Manifest(), extension.ScopeInfo{Name: "tab", Folder: base})
			if _, err := langserver.Attach(context.Background(), api, *cfg, langserver.WithOSRelease(tt.osRelease)); err != nil {
				t.Fatal(err)
			}
			if _, err := api.Launch(context.Background(), ServerID); err == nil {
				t.Fatal("the launcher found a clangd")
			}
			api.SetServerStatus(ServerID, extension.ServerStatus{State: extension.ServerMissing})
			api.Pump()
			notes := api.Notes()
			if len(notes) != 1 || notes[0].Message != "clangd was not found." || !slices.Contains(notes[0].Actions, "Copy Install Command") {
				t.Fatalf("notes = %+v", notes)
			}
			api.RunNoteAction(0, "Copy Install Command")
			if got := api.ClipboardTexts(); len(got) != 1 || got[0] != tt.want {
				t.Errorf("clipboard = %v, want %q", got, tt.want)
			}
		})
	}
}

func TestADatabaseIsFoundWhereClangdLooksForIt(t *testing.T) {
	tests := []struct {
		name  string
		files map[string]string
		args  []string
		want  string // relative to the folder; "" for none
	}{
		{"nothing", map[string]string{"main.cpp": ""}, nil, ""},
		{"in the folder", map[string]string{"compile_commands.json": "[]"}, nil, "compile_commands.json"},
		{"in build", map[string]string{"build/compile_commands.json": "[]"}, nil, "build/compile_commands.json"},
		{"in another folder, not named", map[string]string{"out/compile_commands.json": "[]"}, nil, ""},
		{"in the folder --compile-commands-dir names", map[string]string{"out/compile_commands.json": "[]"}, []string{"--compile-commands-dir=out"}, "out/compile_commands.json"},
		{"in the folder named by a separate argument", map[string]string{"out/compile_commands.json": "[]"}, []string{"--compile-commands-dir", "out"}, "out/compile_commands.json"},
		{"the argument has no value", map[string]string{"main.cpp": ""}, []string{"--compile-commands-dir"}, ""},
		{"compile_flags.txt", map[string]string{"compile_flags.txt": "-std=c11\n"}, nil, "compile_flags.txt"},
		{".clangd with flags", map[string]string{".clangd": "CompileFlags:\n  Add: [-Wall]\n"}, nil, ".clangd"},
		{".clangd without flags", map[string]string{".clangd": "Diagnostics:\n  UnusedIncludes: Strict\n"}, nil, ""},
		{".clangd with flags in a comment", map[string]string{".clangd": "# CompileFlags:\n"}, nil, ""},
		{"a folder with the name of the file", map[string]string{"compile_commands.json/x": ""}, nil, ""},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			dir := t.TempDir()
			for name, text := range tt.files {
				writeFile(t, dir, name, text, 0o644)
			}
			want := tt.want
			if want != "" {
				want = filepath.Join(dir, filepath.FromSlash(want))
			}
			if got := findDatabase(dir, tt.args); got != want {
				t.Errorf("database = %q, want %q", got, want)
			}
		})
	}
}

func TestTheStatusItemSaysWhenThereIsNoCompilationDatabase(t *testing.T) {
	r := newRig(t, true, map[string]string{"main.cpp": "int main() {}\n"})
	r.run()
	r.editor("main.cpp", "cpp", "int main() {}\n")
	item := r.item()
	if !strings.Contains(item.Text, "$(warning)") || !strings.Contains(item.Text, "no compile_commands.json") || !strings.Contains(item.Tooltip, "Create .clangd") {
		t.Fatalf("item = %q | %q", item.Text, item.Tooltip)
	}
	// the fallback flags are what the user chose to do without a database
	r.api.SetSetting(FallbackFlagsSetting, []any{"-std=c++20"})
	r.run()
	if item := r.item(); !strings.Contains(item.Text, "$(check)") || strings.Contains(item.Text, "no compile_commands.json") {
		t.Errorf("item = %q with fallback flags", item.Text)
	}
	r.api.SetSetting(FallbackFlagsSetting, []any{})
	r.run()
	if !strings.Contains(r.item().Text, "no compile_commands.json") {
		t.Fatalf("item = %q after the flags went", r.item().Text)
	}
	// the build writes the database: the next change of the editor shows it
	writeFile(t, r.folder, "build/compile_commands.json", "[]", 0o644)
	r.editor("main.cpp", "cpp", "int main() {}\n")
	item = r.item()
	if !strings.Contains(item.Text, "$(check)") || strings.Contains(item.Text, "no compile_commands.json") || !strings.Contains(item.Tooltip, "Compilation database: "+filepath.Join(r.folder, "build", "compile_commands.json")) {
		t.Errorf("item = %q | %q", item.Text, item.Tooltip)
	}
}

func TestTheStatusItemFollowsTheFileStatusOfTheFileInFront(t *testing.T) {
	r := newRig(t, true, map[string]string{"compile_commands.json": "[]"})
	r.run()
	uri := r.editor("main.cpp", "cpp", "int main() {}\n")
	other := "file://" + filepath.Join(r.folder, "other.cpp")
	if !strings.Contains(r.item().Text, "$(check) clangd") {
		t.Fatalf("item = %q", r.item().Text)
	}
	r.api.ServerNotify(ServerID, fileStatusMethod, map[string]any{"uri": uri, "state": "parsing includes"})
	if got := r.item().Text; !strings.Contains(got, "$(sync~spin)") || !strings.Contains(got, "clangd: parsing includes") {
		t.Errorf("item = %q while the file is parsed", got)
	}
	if tip := r.item().Tooltip; !strings.Contains(tip, "main.cpp: parsing includes") {
		t.Errorf("tooltip = %q", tip)
	}
	// the state of a file that is not in front changes nothing
	r.api.ServerNotify(ServerID, fileStatusMethod, map[string]any{"uri": other, "state": "running action"})
	if got := r.item().Text; !strings.Contains(got, "clangd: parsing includes") {
		t.Errorf("item = %q after the state of another file", got)
	}
	r.api.ServerNotify(ServerID, fileStatusMethod, map[string]any{"uri": uri, "state": "idle"})
	if got := r.item().Text; !strings.Contains(got, "$(check) clangd") || strings.Contains(got, "parsing") {
		t.Errorf("item = %q once the file is idle", got)
	}
	if tip := r.item().Tooltip; !strings.Contains(tip, "main.cpp: idle") {
		t.Errorf("tooltip = %q", tip)
	}
	// moving to the file that is busy shows its state
	r.api.SetDocument(other, "")
	r.api.SetActive(&extension.ActiveDocument{URI: other, LanguageID: "c", Selections: []extension.Selection{{}}})
	r.api.Emit(extension.TopicEditorActiveChanged, extension.ActiveEditor{URI: other, LanguageID: "c"})
	r.api.Pump()
	if got := r.item().Text; !strings.Contains(got, "clangd: running action") {
		t.Errorf("item = %q for the other file", got)
	}
	// a notification that cannot be read changes nothing
	r.api.ServerNotify(ServerID, fileStatusMethod, "nonsense")
	if got := r.item().Text; !strings.Contains(got, "clangd: running action") {
		t.Errorf("item = %q after a bad notification", got)
	}
	// a restart forgets the states
	r.api.SetServerStatus(ServerID, extension.ServerStatus{State: extension.ServerStarting})
	r.api.SetServerStatus(ServerID, extension.ServerStatus{State: extension.ServerRunning})
	if got := r.item().Text; strings.Contains(got, "running action") || !strings.Contains(got, "$(check)") {
		t.Errorf("item = %q after the server started again", got)
	}
}

func TestTheMenuOfTheStatusItemHasTheClangdEntries(t *testing.T) {
	r := newRig(t, true, map[string]string{"compile_commands.json": "[]"})
	r.run()
	r.editor("main.cpp", "cpp", "int main() {}\n")
	r.item().OnClick()
	pick := r.api.LastPick()
	if pick == nil {
		t.Fatal("no menu")
	}
	labels := pick.Labels()
	joined := strings.Join(labels, "|")
	for _, want := range []string{"Switch Between Source/Header", "Open Project Configuration", "Toggle Inlay Hints", "Restart", "Set Path"} {
		if !strings.Contains(joined, want) {
			t.Errorf("the menu lacks %q: %v", want, labels)
		}
	}
	for i, l := range labels {
		if strings.Contains(l, "Switch Between") {
			r.answer(map[string]string{"textDocument/switchSourceHeader": `"file:///work/a.h"`})
			pick.Choose(i)
		}
	}
	r.settle()
	if reqs := r.requests("textDocument/switchSourceHeader"); len(reqs) != 1 {
		t.Errorf("the menu entry sent %d requests", len(reqs))
	}
}

func TestTheReadOnlyTabFallsBackToAnUntitledDocument(t *testing.T) {
	r := newRig(t, true, nil)
	r.run()
	r.ext.reg = nil // a scope that cannot show provided documents
	r.editor("main.cpp", "cpp", "int main() {}\n")
	r.answer(map[string]string{"textDocument/ast": `{"role":"declaration","kind":"Function","detail":"main"}`})
	if err := r.api.Run(ASTCommand, nil); err != nil {
		t.Fatal(err)
	}
	r.settle()
	got := r.api.UntitledDocuments()
	if len(got) != 1 || got[0].LanguageID != "plaintext" || got[0].Text != "Function main\n" {
		t.Errorf("untitled = %+v", got)
	}
}

func TestTheContentProviderRefusesWhatItDoesNotHave(t *testing.T) {
	r := newRig(t, true, nil)
	_, err := r.ext.contentText(context.Background(), extension.ContentURI(ContentScheme, "nothing/here.txt"))
	wantRoute(t, err, "contentText.ClangdContentUnknown")
	if got := r.api.ContentLabel(extension.ContentURI(ContentScheme, astPath)); got != "ast.txt" {
		t.Errorf("label = %q", got)
	}
}

func TestAScopeThatCannotShowProvidedDocumentsIsNoFailureOfThePack(t *testing.T) {
	tests := []struct {
		name     string
		err      error
		wantWarn bool
	}{
		{"a scope with no documents", errors.New("ExtensionDocumentUnavailable", "the scope cannot show provided documents"), false},
		{"another refusal", errors.New("ExtContentSchemeTaken", "the window tab has the scheme"), true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ext := New().(*cpp)
			api := fakeapi.New(t, ext.Manifest(), extension.ScopeInfo{Name: "tab", Folder: t.TempDir()})
			var buf logged
			api.SetLogger(newLogger(&buf))
			api.FailProvideContent(tt.err)
			if err := ext.Activate(context.Background(), api); err != nil {
				t.Fatalf("Activate: %v", err)
			}
			if got := strings.Contains(buf.text(), "level=WARN"); got != tt.wantWarn {
				t.Errorf("warned = %v, want %v; log:\n%s", got, tt.wantWarn, buf.text())
			}
			if ext.reg != nil {
				t.Error("the pack has a registration")
			}
		})
	}
}
