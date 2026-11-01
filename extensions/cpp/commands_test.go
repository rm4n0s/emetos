package cpp

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"github.com/rm4n0s/emetos/extension"
	"github.com/rm4n0s/emetos/extensions/internal/fakeapi"
)

// The commands over the fake API: what they ask the server, in which units, and what they show of the answer.

// commandCoverage says which test runs each command of the manifest. TestEveryCommandHasAHandlerAndATest keeps it whole: a command
// added to the manifest without a line here, or a line without a command, fails.
var commandCoverage = map[string]string{
	SwitchHeaderSourceCommand: "TestSwitchBetweenSourceAndHeaderOpensTheOtherFile",
	ASTCommand:                "TestShowASTOpensTheTreeInAReadOnlyTab",
	ToggleInlayHintsCommand:   "TestToggleInlayHintsFlipsTheSettingOfTheEditor",
	ProjectConfigCommand:      "TestTheProjectConfigurationOpensOrIsCreated",
	RestartCommand:            "TestRestartLanguageServerRestartsClangd",
}

func TestEveryCommandHasAHandlerAndATest(t *testing.T) {
	r := newRig(t, true, nil)
	declared := map[string]bool{}
	for _, c := range r.ext.Manifest().Contributes.Commands {
		declared[c.ID] = true
		if c.Category != commandCategory || c.Title == "" || c.Description == "" {
			t.Errorf("command %+v lacks its category, title or description", c)
		}
		if _, ok := commandCoverage[c.ID]; !ok {
			t.Errorf("the command %s has no entry in commandCoverage", c.ID)
		}
		if !r.api.HasHandler(c.ID) {
			t.Errorf("the command %s has no handler", c.ID)
		}
	}
	for id := range commandCoverage {
		if !declared[id] {
			t.Errorf("commandCoverage names %s, which the manifest does not declare", id)
		}
	}
	for _, c := range r.ext.Manifest().Contributes.Commands {
		// the commands that work on the file in front are enabled for the languages of clangd only
		switch c.ID {
		case SwitchHeaderSourceCommand, ASTCommand:
			if c.When != cppWhen {
				t.Errorf("%s is enabled when %q", c.ID, c.When)
			}
		}
	}
}

func TestTheKeybindingOfSwitchBetweenSourceAndHeaderIsAltO(t *testing.T) {
	keys := New().Manifest().Contributes.Keybindings
	if len(keys) != 1 {
		t.Fatalf("keybindings = %+v", keys)
	}
	k := keys[0]
	if k.Command != SwitchHeaderSourceCommand || k.Key != "alt+o" {
		t.Errorf("keybinding = %+v", k)
	}
	// the clause is the one of vscode-clangd for the three languages of the pack, with the focus in the editor
	for _, want := range []string{"editorTextFocus", "editorLangId == c ", "editorLangId == cpp", "editorLangId == cuda-cpp"} {
		if !strings.Contains(k.When, want) {
			t.Errorf("when = %q lacks %q", k.When, want)
		}
	}
}

func TestSwitchBetweenSourceAndHeaderOpensTheOtherFile(t *testing.T) {
	r := newRig(t, true, map[string]string{"compile_commands.json": "[]"})
	r.run()
	uri := r.editor("src/main.cpp", "cpp", "#include \"a.h\"\n")
	other := "file://" + filepath.Join(r.folder, "include", "a.h")
	r.answer(map[string]string{"textDocument/switchSourceHeader": strconv.Quote(other)})
	if err := r.api.Run(SwitchHeaderSourceCommand, nil); err != nil {
		t.Fatal(err)
	}
	r.settle()
	if reqs := r.requests("textDocument/switchSourceHeader"); len(reqs) != 1 || reqs[0] != `{"uri":"`+uri+`"}` {
		t.Fatalf("requests = %v", reqs)
	}
	shown := r.api.ShownDocuments()
	if len(shown) != 1 || shown[0].URI != other || shown[0].Line != 1 || shown[0].Column != 1 || shown[0].Preview {
		t.Errorf("shown = %+v", shown)
	}

	// no other file: the user is told, nothing opens
	r.answer(nil)
	if err := r.api.Run(SwitchHeaderSourceCommand, nil); err != nil {
		t.Fatal(err)
	}
	r.settle()
	if len(r.api.ShownDocuments()) != 1 || !strings.Contains(r.note(), "found no header or source file for main.cpp") {
		t.Errorf("shown %d, note %q", len(r.api.ShownDocuments()), r.note())
	}

	// a file that cannot be opened
	r.answer(map[string]string{"textDocument/switchSourceHeader": strconv.Quote(other)})
	r.api.FailOpen(os.ErrNotExist)
	_ = r.api.Run(SwitchHeaderSourceCommand, nil)
	r.settle()
}

func TestShowASTOpensTheTreeInAReadOnlyTab(t *testing.T) {
	// "é" is two bytes and one UTF-16 unit; the range of the selection is asked in the units the server chose
	text := "int é = f(1);\nint main() {}\n"
	tree := `{"role":"declaration","kind":"Var","detail":"é","range":{"start":{"line":0,"character":@S},"end":{"line":0,"character":@E}},` +
		`"children":[{"role":"type","kind":"Builtin","detail":"int"},` +
		`{"role":"expression","kind":"Call","children":[{"role":"expression","kind":"DeclRef","detail":"f"}]},` +
		`{"role":"specifier","kind":"Static"}]}`
	tests := []struct {
		name, encoding string
		start, end     int // the columns of the selection "é = f(1)" in the server's units; the editor counts bytes: 4 to 13
		wantRange      string
	}{
		{"UTF-8 counts bytes", "utf-8", 4, 13, "<1:5-1:13>"},
		{"UTF-16 counts code units", "utf-16", 4, 12, "<1:5-1:13>"},
		{"UTF-32 counts code points", "utf-32", 4, 12, "<1:5-1:13>"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			r := newRig(t, true, nil)
			r.run()
			r.api.SetServerEncoding(ServerID, tt.encoding)
			uri := r.editor("main.cpp", "cpp", text, extension.Selection{Anchor: extension.Position{Col: 4}, Active: extension.Position{Col: 13}})
			answer := strings.NewReplacer("@S", strconv.Itoa(tt.start), "@E", strconv.Itoa(tt.end)).Replace(tree)
			r.answer(map[string]string{"textDocument/ast": answer})
			if err := r.api.Run(ASTCommand, nil); err != nil {
				t.Fatal(err)
			}
			r.settle()
			reqs := r.requests("textDocument/ast")
			want := `{"textDocument":{"uri":"` + uri + `"},"range":{"start":{"line":0,"character":` + strconv.Itoa(tt.start) +
				`},"end":{"line":0,"character":` + strconv.Itoa(tt.end) + `}}}`
			if len(reqs) != 1 || reqs[0] != want {
				t.Fatalf("requests = %v, want %s", reqs, want)
			}
			opened := r.api.OpenedDocuments()
			if len(opened) != 1 || opened[0].Title != "AST of main.cpp" || !opened[0].Preview {
				t.Fatalf("opened = %+v", opened)
			}
			got, err := r.api.ContentText(context.Background(), opened[0].URI)
			if err != nil {
				t.Fatal(err)
			}
			wantText := "Var é  " + tt.wantRange + "\n" +
				"  Builtin type int\n" +
				"  Call\n" +
				"    DeclRef f\n" +
				"  Static specifier\n"
			if got != wantText {
				t.Errorf("the tab shows\n%s\nwant\n%s", got, wantText)
			}
			if lang := r.api.ContentLanguage(opened[0].URI); lang != "plaintext" {
				t.Errorf("language = %q", lang)
			}
		})
	}
}

func TestShowASTAsksForTheCursorWhenNothingIsSelected(t *testing.T) {
	r := newRig(t, true, nil)
	r.run()
	uri := r.editor("main.cpp", "cpp", "int main() {}\n", extension.Selection{Anchor: extension.Position{Col: 4}, Active: extension.Position{Col: 4}})
	r.answer(map[string]string{"textDocument/ast": `{"role":"declaration","kind":"Function","detail":"main"}`})
	if err := r.api.Run(ASTCommand, nil); err != nil {
		t.Fatal(err)
	}
	r.settle()
	want := `{"textDocument":{"uri":"` + uri + `"},"range":{"start":{"line":0,"character":4},"end":{"line":0,"character":4}}}`
	if reqs := r.requests("textDocument/ast"); len(reqs) != 1 || reqs[0] != want {
		t.Errorf("requests = %v, want %s", reqs, want)
	}
	// a selection made backwards covers the same range
	r.editor("main.cpp", "cpp", "int main() {}\n", extension.Selection{Anchor: extension.Position{Col: 8}, Active: extension.Position{Col: 4}})
	if err := r.api.Run(ASTCommand, nil); err != nil {
		t.Fatal(err)
	}
	r.settle()
	want = `{"textDocument":{"uri":"` + uri + `"},"range":{"start":{"line":0,"character":4},"end":{"line":0,"character":8}}}`
	if reqs := r.requests("textDocument/ast"); len(reqs) != 2 || reqs[1] != want {
		t.Errorf("requests = %v, want %s", reqs, want)
	}
	// the next answer replaces the text of the tab that is open
	r.answer(map[string]string{"textDocument/ast": `{"role":"declaration","kind":"Var"}`})
	if err := r.api.Run(ASTCommand, nil); err != nil {
		t.Fatal(err)
	}
	r.settle()
	opened := r.api.OpenedDocuments()
	if len(opened) != 3 || opened[2].URI != opened[0].URI {
		t.Fatalf("opened = %+v", opened)
	}
	if text, _ := r.api.ContentText(context.Background(), opened[2].URI); text != "Var\n" {
		t.Errorf("the text of the tab = %q", text)
	}
	if changes := r.api.ContentProviderFor(ContentScheme).ContentChanges(); len(changes) != 3 {
		t.Errorf("the tab was told %d times that its text changed", len(changes))
	}
}

func TestShowASTWithNoTreeSaysSo(t *testing.T) {
	for _, answer := range []string{"null", `{}`, `"nonsense"`} {
		r := newRig(t, true, nil)
		r.run()
		r.editor("main.cpp", "cpp", "int main() {}\n")
		r.answer(map[string]string{"textDocument/ast": answer})
		if err := r.api.Run(ASTCommand, nil); err != nil {
			t.Fatal(err)
		}
		r.settle()
		if len(r.api.OpenedDocuments()) != 0 || !strings.Contains(r.note(), "no syntax tree for this place") {
			t.Errorf("answer %s: opened %d, note %q", answer, len(r.api.OpenedDocuments()), r.note())
		}
	}
}

func TestACommandThatCannotWorkSaysWhy(t *testing.T) {
	tests := []struct {
		name   string
		setup  func(r *rig)
		status extension.ServerState
		want   string
	}{
		{"no editor", func(r *rig) { r.api.SetActive(nil) }, extension.ServerRunning, "needs a C or C++ file"},
		{"a file of another language", func(r *rig) { r.editor("a.rs", "rust", "fn main() {}\n") }, extension.ServerRunning, "needs a C or C++ file"},
		{"the server is starting", func(r *rig) { r.editor("a.c", "c", "int x;\n") }, extension.ServerStarting, "which is starting"},
		{"the server is missing", func(r *rig) { r.editor("a.c", "c", "int x;\n") }, extension.ServerMissing, "which is not available"},
		{"the server crashed", func(r *rig) { r.editor("a.c", "c", "int x;\n") }, extension.ServerFailed, "which is stopped after crashing"},
		{"the server has not started", func(r *rig) { r.editor("a.c", "c", "int x;\n") }, extension.ServerStopped, "which is stopped"},
		{"the file cannot be read", func(r *rig) {
			r.editor("a.c", "c", "int x;\n")
			r.api.SetDocument("file://"+filepath.Join(r.folder, "a.c"), "")
			r.api.SetActive(&extension.ActiveDocument{URI: "file:///nowhere/gone.c", LanguageID: "c", Selections: []extension.Selection{{}}})
		}, extension.ServerRunning, "gone.c cannot be read"},
	}
	for _, tt := range tests {
		for _, id := range []string{SwitchHeaderSourceCommand, ASTCommand} {
			t.Run(tt.name+" "+id, func(t *testing.T) {
				r := newRig(t, true, nil)
				r.api.SetServerStatus(ServerID, extension.ServerStatus{State: tt.status})
				tt.setup(r)
				if err := r.api.Run(id, nil); err != nil {
					t.Fatal(err)
				}
				r.settle()
				if !strings.Contains(r.note(), tt.want) {
					t.Errorf("note = %q, want it to contain %q", r.note(), tt.want)
				}
				if len(r.api.ServerRequests(ServerID)) != 0 {
					t.Errorf("a request was sent: %+v", r.api.ServerRequests(ServerID))
				}
			})
		}
	}
}

func TestACommandWhoseRequestFailsShowsTheError(t *testing.T) {
	r := newRig(t, true, nil)
	r.run()
	r.editor("main.cpp", "cpp", "int main() {}\n")
	r.api.OnServerRequest(ServerID, func(fakeapi.ServerRequest) (json.RawMessage, error) {
		return nil, os.ErrDeadlineExceeded
	})
	if err := r.api.Run(ASTCommand, nil); err != nil {
		t.Fatal(err)
	}
	r.settle()
	if got := r.note(); !strings.Contains(got, "Show AST: clangd failed") {
		t.Errorf("note = %q", got)
	}
}

func TestToggleInlayHintsFlipsTheSettingOfTheEditor(t *testing.T) {
	tests := []struct {
		current string
		want    string // "" when nothing is written
	}{
		{"on", "off"},
		{"off", "on"},
		{"onUnlessPressed", "offUnlessPressed"},
		{"offUnlessPressed", "onUnlessPressed"},
		{"", "off"},
		{"sideways", ""},
	}
	for _, tt := range tests {
		t.Run(tt.current, func(t *testing.T) {
			r := newRig(t, true, nil)
			r.api.SetSetting(InlayHintsSetting, tt.current)
			if err := r.api.Run(ToggleInlayHintsCommand, nil); err != nil {
				t.Fatal(err)
			}
			updates := r.api.SettingUpdates()
			if tt.want == "" {
				if len(updates) != 0 || !strings.Contains(r.note(), "has no opposite") {
					t.Errorf("updates %+v, note %q", updates, r.note())
				}
				return
			}
			if len(updates) != 1 || updates[0].Key != InlayHintsSetting || updates[0].Value != tt.want {
				t.Errorf("updates = %+v, want %s", updates, tt.want)
			}
		})
	}
	t.Run("a client of a hosted workspace", func(t *testing.T) {
		r := newRig(t, true, nil)
		r.api.SetSetting(InlayHintsSetting, "on")
		r.api.RunAs(extension.Origin(1), func() {
			if err := r.api.Run(ToggleInlayHintsCommand, nil); err != nil {
				t.Error(err)
			}
		})
		if len(r.api.SettingUpdates()) != 0 || !strings.Contains(r.note(), "setting of the host") {
			t.Errorf("updates %+v, note %q", r.api.SettingUpdates(), r.note())
		}
	})
}

func TestTheProjectConfigurationOpensOrIsCreated(t *testing.T) {
	t.Run("it opens the one that is there", func(t *testing.T) {
		r := newRig(t, true, map[string]string{".clangd": "CompileFlags:\n  Add: [-Wall]\n"})
		if err := r.api.Run(ProjectConfigCommand, nil); err != nil {
			t.Fatal(err)
		}
		shown := r.api.ShownDocuments()
		if len(shown) != 1 || shown[0].URI != "file://"+filepath.Join(r.folder, ".clangd") {
			t.Errorf("shown = %+v", shown)
		}
		if r.api.LastPick() != nil {
			t.Error("the user was asked about a file that is there")
		}
	})
	t.Run("it offers to create one and the stub is a CompileFlags section", func(t *testing.T) {
		r := newRig(t, true, map[string]string{"main.cpp": ""})
		if err := r.api.Run(ProjectConfigCommand, nil); err != nil {
			t.Fatal(err)
		}
		pick := r.api.LastPick()
		if pick == nil || len(pick.Labels()) != 2 || !strings.Contains(pick.Labels()[0], "Create .clangd") {
			t.Fatalf("pick = %+v", pick)
		}
		if _, err := os.Stat(filepath.Join(r.folder, ".clangd")); err == nil {
			t.Fatal("the file was created before the user chose")
		}
		pick.Choose(0)
		data, err := os.ReadFile(filepath.Join(r.folder, ".clangd"))
		if err != nil {
			t.Fatal(err)
		}
		if string(data) != projectConfigStub || !hasCompileFlags(filepath.Join(r.folder, ".clangd")) {
			t.Errorf("the file holds\n%s", data)
		}
		if shown := r.api.ShownDocuments(); len(shown) != 1 || !strings.HasSuffix(shown[0].URI, "/.clangd") {
			t.Errorf("shown = %+v", shown)
		}
		// the project has flags now
		if findDatabase(r.folder, nil) == "" {
			t.Error("the stub does not count as flags for clangd")
		}
	})
	t.Run("cancel creates nothing", func(t *testing.T) {
		r := newRig(t, true, nil)
		if err := r.api.Run(ProjectConfigCommand, nil); err != nil {
			t.Fatal(err)
		}
		r.api.LastPick().Choose(1)
		if _, err := os.Stat(filepath.Join(r.folder, ".clangd")); err == nil || len(r.api.ShownDocuments()) != 0 {
			t.Errorf("a file was made or shown: %v", err)
		}
	})
	t.Run("the file goes to the root of the project", func(t *testing.T) {
		r := newRig(t, true, map[string]string{"compile_commands.json": "[]"})
		if err := r.api.Run(ProjectConfigCommand, nil); err != nil {
			t.Fatal(err)
		}
		r.api.LastPick().Choose(0)
		if _, err := os.Stat(filepath.Join(r.folder, ".clangd")); err != nil {
			t.Error(err)
		}
	})
	t.Run("a tab with no folder", func(t *testing.T) {
		ext := New()
		api := fakeapi.New(t, ext.Manifest(), extension.ScopeInfo{Name: "tab"})
		if err := ext.Activate(context.Background(), api); err != nil {
			t.Fatal(err)
		}
		if err := api.Run(ProjectConfigCommand, nil); err != nil {
			t.Fatal(err)
		}
		if notes := api.Notes(); len(notes) != 1 || !strings.Contains(notes[0].Message, "needs a folder") {
			t.Errorf("notes = %+v", notes)
		}
	})
	t.Run("the file cannot be created", func(t *testing.T) {
		r := newRig(t, true, nil)
		// a file where the folder should be
		blocker := filepath.Join(r.folder, "blocker")
		writeFile(t, r.folder, "blocker", "x", 0o644)
		err := createProjectConfig(filepath.Join(blocker, ".clangd"))
		wantRoute(t, err, "createProjectConfig.ClangdConfigCreateFailed")
	})
	t.Run("one that appeared meanwhile is left alone", func(t *testing.T) {
		dir := t.TempDir()
		path := writeFile(t, dir, ".clangd", "mine\n", 0o644)
		if err := createProjectConfig(path); err != nil {
			t.Fatal(err)
		}
		if data, _ := os.ReadFile(path); string(data) != "mine\n" {
			t.Errorf("the file was overwritten: %q", data)
		}
	})
}

func TestRestartLanguageServerRestartsClangd(t *testing.T) {
	r := newRig(t, true, nil)
	if err := r.api.Run(RestartCommand, nil); err != nil {
		t.Fatal(err)
	}
	if got := r.api.ServerRestarts(ServerID); got != 1 {
		t.Errorf("restarts = %d", got)
	}
}

func TestTheToggledValueIsTheOppositeOfTheTwoStatesOfTheSetting(t *testing.T) {
	// the toggle of vscode-clangd, twice, is where it started
	for _, v := range []string{"on", "off", "onUnlessPressed", "offUnlessPressed"} {
		once, ok := toggledInlayHints(v)
		twice, ok2 := toggledInlayHints(once)
		if !ok || !ok2 || twice != v {
			t.Errorf("%s -> %s -> %s", v, once, twice)
		}
	}
}
