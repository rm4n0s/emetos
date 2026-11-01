package rust

import (
	"context"
	"encoding/json"
	"log/slog"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"github.com/rm4n0s/errors"

	"github.com/rm4n0s/emetos/extension"
	"github.com/rm4n0s/emetos/extensions/internal/fakeapi"
)

// The commands over the fake API: what they ask the server, in which units, and what they show of the answer.

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

func TestExpandMacroOpensTheExpansionInAReadOnlyTab(t *testing.T) {
	r := newRig(t, true)
	r.run()
	uri := r.editor("src/main.rs", "fn main() {\n    let x = double!(1);\n}\n", extension.Selection{Anchor: extension.Position{Line: 1, Col: 12}, Active: extension.Position{Line: 1, Col: 12}})
	r.answer(map[string]string{"rust-analyzer/expandMacro": `{"name":"double!","expansion":"1 * 2"}`})
	if err := r.api.Run(ExpandMacroCommand, nil); err != nil {
		t.Fatal(err)
	}
	r.settle()
	reqs := r.requests("rust-analyzer/expandMacro")
	want := `{"textDocument":{"uri":"` + uri + `"},"position":{"line":1,"character":12}}`
	if len(reqs) != 1 || reqs[0] != want {
		t.Fatalf("requests = %v, want %s", reqs, want)
	}
	opened := r.api.OpenedDocuments()
	if len(opened) != 1 || opened[0].Title != "Expansion of double!" || !opened[0].Preview || !strings.HasPrefix(opened[0].URI, extension.ContentScheme+"/"+ContentScheme+"/") {
		t.Fatalf("opened = %+v", opened)
	}
	text, err := r.api.ContentText(context.Background(), opened[0].URI)
	if err != nil || text != "1 * 2" {
		t.Errorf("the text of the tab = %q, %v", text, err)
	}
	if got := r.api.ContentLanguage(opened[0].URI); got != "rust" {
		t.Errorf("language = %q", got)
	}
	// the next answer replaces the text of the tab that is open
	r.answer(map[string]string{"rust-analyzer/expandMacro": `{"name":"vec!","expansion":"Vec::new()"}`})
	if err := r.api.Run(ExpandMacroCommand, nil); err != nil {
		t.Fatal(err)
	}
	r.settle()
	opened = r.api.OpenedDocuments()
	if len(opened) != 2 || opened[1].URI != opened[0].URI || opened[1].Title != "Expansion of vec!" {
		t.Fatalf("opened = %+v", opened)
	}
	if text, _ := r.api.ContentText(context.Background(), opened[1].URI); text != "Vec::new()" {
		t.Errorf("the text of the tab = %q", text)
	}
	if changes := r.api.ContentProviderFor(ContentScheme).ContentChanges(); len(changes) != 2 {
		t.Errorf("the tab was told %d times that its text changed", len(changes))
	}
}

func TestACommandAsksInTheUnitsOfTheServer(t *testing.T) {
	// "é" is two bytes and one UTF-16 unit; "😀" is four bytes and two UTF-16 units
	text := "let é😀x = m!();\n"
	byteCol := strings.Index(text, "x") // after é and 😀
	tests := []struct {
		name, encoding string
		character      int
	}{
		{"UTF-8 counts bytes", "utf-8", byteCol},
		{"UTF-16 counts code units", "utf-16", 4 + 1 + 1}, // "let " + é + 😀 (two units) is 4 + 1 + 2
		{"UTF-32 counts code points", "utf-32", 4 + 1 + 1},
	}
	tests[1].character = 4 + 1 + 2
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			r := newRig(t, true)
			r.run()
			r.api.SetServerEncoding(ServerID, tt.encoding)
			uri := r.editor("src/main.rs", text, extension.Selection{Anchor: extension.Position{Col: byteCol}, Active: extension.Position{Col: byteCol}})
			if err := r.api.Run(ExpandMacroCommand, nil); err != nil {
				t.Fatal(err)
			}
			r.settle()
			reqs := r.requests("rust-analyzer/expandMacro")
			want := `{"textDocument":{"uri":"` + uri + `"},"position":{"line":0,"character":` + itoaT(tt.character) + `}}`
			if len(reqs) != 1 || reqs[0] != want {
				t.Errorf("requests = %v, want %s", reqs, want)
			}
		})
	}
}

func itoaT(n int) string { b, _ := json.Marshal(n); return string(b) }

func TestACommandThatCannotWorkSaysWhy(t *testing.T) {
	tests := []struct {
		name  string
		setup func(r *rig)
		run   string
		want  string
	}{
		{"no editor", func(r *rig) { r.run() }, ExpandMacroCommand, "Expand macro needs a Rust file"},
		{"not a Rust file", func(r *rig) {
			r.run()
			r.api.SetActive(&extension.ActiveDocument{URI: "file:///work/a.go", LanguageID: "go", Selections: []extension.Selection{{}}})
		}, ParentModuleCommand, "Locate parent module needs a Rust file"},
		{"the server does not run", func(r *rig) {
			r.editor("src/main.rs", "fn main() {}\n")
			r.api.SetServerStatus(ServerID, extension.ServerStatus{State: extension.ServerStarting})
		}, JoinLinesCommand, "Join lines needs rust-analyzer, which is starting"},
		{"the server is missing", func(r *rig) {
			r.editor("src/main.rs", "fn main() {}\n")
			r.api.SetServerStatus(ServerID, extension.ServerStatus{State: extension.ServerMissing})
		}, MatchingBraceCommand, "Find matching brace needs rust-analyzer, which is not available"},
		{"the server failed", func(r *rig) {
			r.editor("src/main.rs", "fn main() {}\n")
			r.api.SetServerStatus(ServerID, extension.ServerStatus{State: extension.ServerFailed})
		}, ViewSyntaxTreeCommand, "View Syntax Tree needs rust-analyzer, which is stopped after crashing"},
		{"the server has not started", func(r *rig) { r.editor("src/main.rs", "fn main() {}\n") }, OpenCargoTomlCommand, "Open Cargo.toml needs rust-analyzer, which is not running yet"},
		{"reload without a server", func(r *rig) {}, ReloadWorkspaceCommand, "Reload workspace needs rust-analyzer, which is not running yet"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			r := newRig(t, true)
			tt.setup(r)
			if err := r.api.Run(tt.run, nil); err != nil {
				t.Fatal(err)
			}
			r.settle()
			if got := r.note(); !strings.Contains(got, tt.want) {
				t.Errorf("notification %q, want %q", got, tt.want)
			}
			if got := r.api.ServerRequests(ServerID); len(got) != 0 {
				t.Errorf("requests = %v", got)
			}
		})
	}
}

func TestACommandWhoseRequestFailsShowsTheError(t *testing.T) {
	r := newRig(t, true)
	r.run()
	r.editor("src/main.rs", "fn main() {}\n")
	r.api.OnServerRequest(ServerID, func(fakeapi.ServerRequest) (json.RawMessage, error) {
		return nil, errors.New("TestRequestCancelled", "request cancelled")
	})
	if err := r.api.Run(ExpandMacroCommand, nil); err != nil {
		t.Fatal(err)
	}
	r.settle()
	notes := r.api.Notes()
	if len(notes) != 1 || notes[0].Severity != "error" || !strings.Contains(notes[0].Message, "Expand macro: rust-analyzer failed: request cancelled") {
		t.Errorf("notes = %+v", notes)
	}
	if len(r.api.OpenedDocuments()) != 0 {
		t.Error("a tab was opened")
	}
}

func TestExpandMacroWithNoMacroSaysSo(t *testing.T) {
	r := newRig(t, true)
	r.run()
	r.editor("src/main.rs", "fn main() {}\n")
	r.answer(nil)
	if err := r.api.Run(ExpandMacroCommand, nil); err != nil {
		t.Fatal(err)
	}
	r.settle()
	if got := r.note(); !strings.Contains(got, "no macro call at the cursor") {
		t.Errorf("notification %q", got)
	}
}

func TestViewSyntaxTreeSendsTheSelection(t *testing.T) {
	r := newRig(t, true)
	r.run()
	r.answer(map[string]string{"rust-analyzer/viewSyntaxTree": `"SOURCE_FILE@0..13\n"`})
	// a cursor: the whole file; a selection made backwards: its range, in order
	uri := r.editor("src/main.rs", "fn main() {}\n")
	for _, sel := range []extension.Selection{
		{},
		{Anchor: extension.Position{Col: 7}, Active: extension.Position{Col: 3}},
	} {
		r.editor("src/main.rs", "fn main() {}\n", sel)
		if err := r.api.Run(ViewSyntaxTreeCommand, nil); err != nil {
			t.Fatal(err)
		}
		r.settle()
	}
	reqs := r.requests("rust-analyzer/viewSyntaxTree")
	wants := []string{
		`{"textDocument":{"uri":"` + uri + `"},"range":null}`,
		`{"textDocument":{"uri":"` + uri + `"},"range":{"start":{"line":0,"character":3},"end":{"line":0,"character":7}}}`,
	}
	if len(reqs) != 2 || reqs[0] != wants[0] || reqs[1] != wants[1] {
		t.Fatalf("requests = %v, want %v", reqs, wants)
	}
	opened := r.api.OpenedDocuments()
	if len(opened) != 2 || opened[0].Title != "Syntax Tree of main.rs" || !strings.HasSuffix(opened[0].URI, "syntax.rast") {
		t.Fatalf("opened = %+v", opened)
	}
	if text, _ := r.api.ContentText(context.Background(), opened[0].URI); text != "SOURCE_FILE@0..13\n" {
		t.Errorf("the text = %q", text)
	}
	if got := r.api.ContentLanguage(opened[0].URI); got != "plaintext" {
		t.Errorf("language = %q", got)
	}
}

func TestLocatingTheParentModule(t *testing.T) {
	file := func(r *rig, name string) string { return "file://" + filepath.Join(r.folder, name) }
	t.Run("one parent: the file opens at the line, in UTF-16 columns, counted from 1", func(t *testing.T) {
		r := newRig(t, true)
		r.run()
		r.api.SetServerEncoding(ServerID, "utf-8")
		r.api.SetDocument(file(r, "src/main.rs"), "é mod geometry;\n")
		r.editor("src/geometry.rs", "pub fn f() {}\n")
		// utf-8: the server says byte 7, the "g" of "geometry" after "é mod " (seven bytes), which is UTF-16 column 6
		r.answer(map[string]string{"experimental/parentModule": `[{"targetUri":"` + file(r, "src/main.rs") + `","targetRange":{"start":{"line":0,"character":0},"end":{"line":0,"character":16}},"targetSelectionRange":{"start":{"line":0,"character":7},"end":{"line":0,"character":15}}}]`})
		if err := r.api.Run(ParentModuleCommand, nil); err != nil {
			t.Fatal(err)
		}
		r.settle()
		shown := r.api.ShownDocuments()
		if len(shown) != 1 || shown[0].URI != file(r, "src/main.rs") || shown[0].Line != 1 || shown[0].Column != 7 || shown[0].Preview {
			t.Errorf("shown = %+v", shown)
		}
	})
	t.Run("a Location", func(t *testing.T) {
		r := newRig(t, true)
		r.run()
		r.editor("src/geometry.rs", "pub fn f() {}\n")
		r.answer(map[string]string{"experimental/parentModule": `{"uri":"` + file(r, "src/lib.rs") + `","range":{"start":{"line":4,"character":2},"end":{"line":4,"character":9}}}`})
		if err := r.api.Run(ParentModuleCommand, nil); err != nil {
			t.Fatal(err)
		}
		r.settle()
		if shown := r.api.ShownDocuments(); len(shown) != 1 || shown[0].Line != 5 || shown[0].Column != 3 {
			t.Errorf("shown = %+v", shown)
		}
	})
	t.Run("several parents: the user chooses", func(t *testing.T) {
		r := newRig(t, true)
		r.run()
		r.editor("src/geometry.rs", "pub fn f() {}\n")
		r.answer(map[string]string{"experimental/parentModule": `[{"uri":"` + file(r, "src/main.rs") + `","range":{"start":{"line":0,"character":0},"end":{"line":0,"character":0}}},{"uri":"` + file(r, "src/lib.rs") + `","range":{"start":{"line":2,"character":0},"end":{"line":2,"character":0}}}]`})
		if err := r.api.Run(ParentModuleCommand, nil); err != nil {
			t.Fatal(err)
		}
		r.settle()
		pick := r.api.LastPick()
		if pick == nil {
			t.Fatal("no list was shown")
		}
		items := pick.Options().Items
		if len(items) != 2 || items[0].Label != filepath.Join("src", "main.rs") || items[1].Label != filepath.Join("src", "lib.rs") || items[1].Description != "line 3" {
			t.Fatalf("items = %+v", items)
		}
		pick.Choose(1)
		r.settle()
		if shown := r.api.ShownDocuments(); len(shown) != 1 || shown[0].URI != file(r, "src/lib.rs") || shown[0].Line != 3 {
			t.Errorf("shown = %+v", shown)
		}
	})
	t.Run("none", func(t *testing.T) {
		r := newRig(t, true)
		r.run()
		r.editor("src/main.rs", "fn main() {}\n")
		r.answer(nil)
		if err := r.api.Run(ParentModuleCommand, nil); err != nil {
			t.Fatal(err)
		}
		r.settle()
		if got := r.note(); !strings.Contains(got, "found no parent module") {
			t.Errorf("notification %q", got)
		}
	})
}

func TestOpeningCargoToml(t *testing.T) {
	r := newRig(t, true)
	r.run()
	uri := r.editor("src/main.rs", "fn main() {}\n")
	toml := "file://" + filepath.Join(r.folder, "Cargo.toml")
	r.answer(map[string]string{"experimental/openCargoToml": `{"uri":"` + toml + `","range":{"start":{"line":0,"character":0},"end":{"line":0,"character":0}}}`})
	if err := r.api.Run(OpenCargoTomlCommand, nil); err != nil {
		t.Fatal(err)
	}
	r.settle()
	if reqs := r.requests("experimental/openCargoToml"); len(reqs) != 1 || reqs[0] != `{"textDocument":{"uri":"`+uri+`"}}` {
		t.Errorf("requests = %v", reqs)
	}
	if shown := r.api.ShownDocuments(); len(shown) != 1 || shown[0].URI != toml || shown[0].Line != 1 || shown[0].Column != 1 {
		t.Errorf("shown = %+v", shown)
	}
	r.answer(nil)
	if err := r.api.Run(OpenCargoTomlCommand, nil); err != nil {
		t.Fatal(err)
	}
	r.settle()
	if got := r.note(); !strings.Contains(got, "found no Cargo.toml") {
		t.Errorf("notification %q", got)
	}
}

func TestJoinLinesAppliesTheEditsOfTheServer(t *testing.T) {
	text := "fn main() {\n    let x = 1;\n    let y = 2;\n}\n"
	r := newRig(t, true)
	r.run()
	r.api.SetServerEncoding(ServerID, "utf-8")
	uri := r.editor("src/main.rs", text,
		extension.Selection{Anchor: extension.Position{Line: 1, Col: 4}, Active: extension.Position{Line: 1, Col: 4}},
		extension.Selection{Anchor: extension.Position{Line: 2, Col: 8}, Active: extension.Position{Line: 2, Col: 2}},
	)
	r.answer(map[string]string{"experimental/joinLines": `[{"range":{"start":{"line":1,"character":14},"end":{"line":2,"character":4}},"newText":" "}]`})
	if err := r.api.Run(JoinLinesCommand, nil); err != nil {
		t.Fatal(err)
	}
	r.settle()
	reqs := r.requests("experimental/joinLines")
	want := `{"textDocument":{"uri":"` + uri + `"},"ranges":[{"start":{"line":1,"character":4},"end":{"line":1,"character":4}},{"start":{"line":2,"character":2},"end":{"line":2,"character":8}}]}`
	if len(reqs) != 1 || reqs[0] != want {
		t.Fatalf("requests = %v, want %s", reqs, want)
	}
	if got, _ := r.api.DocumentText(uri); got != "fn main() {\n    let x = 1; let y = 2;\n}\n" {
		t.Errorf("the text after the join = %q", got)
	}
	// an answer of nothing changes nothing; a file that changed meanwhile is not edited
	r.answer(nil)
	if err := r.api.Run(JoinLinesCommand, nil); err != nil {
		t.Fatal(err)
	}
	r.settle()
	if len(r.api.Applied()) != 1 {
		t.Errorf("%d edits were applied", len(r.api.Applied()))
	}
	r.api.OnServerRequest(ServerID, func(fakeapi.ServerRequest) (json.RawMessage, error) {
		r.api.SetDocument(uri, "changed\n")
		return json.RawMessage(`[{"range":{"start":{"line":0,"character":0},"end":{"line":0,"character":1}},"newText":"x"}]`), nil
	})
	if err := r.api.Run(JoinLinesCommand, nil); err != nil {
		t.Fatal(err)
	}
	r.settle()
	if got := r.note(); !strings.Contains(got, "the file changed while rust-analyzer worked") {
		t.Errorf("notification %q", got)
	}
	if len(r.api.Applied()) != 1 {
		t.Errorf("%d edits were applied", len(r.api.Applied()))
	}
}

func TestFindMatchingBraceMovesTheCursor(t *testing.T) {
	r := newRig(t, true)
	r.run()
	uri := r.editor("src/main.rs", "fn main() {\n}\n", extension.Selection{Anchor: extension.Position{Col: 10}, Active: extension.Position{Col: 10}})
	r.answer(map[string]string{"experimental/matchingBrace": `[{"line":1,"character":0}]`})
	if err := r.api.Run(MatchingBraceCommand, nil); err != nil {
		t.Fatal(err)
	}
	r.settle()
	if reqs := r.requests("experimental/matchingBrace"); len(reqs) != 1 || reqs[0] != `{"textDocument":{"uri":"`+uri+`"},"positions":[{"line":0,"character":10}]}` {
		t.Errorf("requests = %v", reqs)
	}
	if shown := r.api.ShownDocuments(); len(shown) != 1 || shown[0].URI != uri || shown[0].Line != 2 || shown[0].Column != 1 {
		t.Errorf("shown = %+v", shown)
	}
	r.answer(map[string]string{"experimental/matchingBrace": `[]`})
	if err := r.api.Run(MatchingBraceCommand, nil); err != nil {
		t.Fatal(err)
	}
	r.settle()
	if len(r.api.ShownDocuments()) != 1 {
		t.Error("the cursor moved with no answer")
	}
}

func TestReloadWorkspaceAndRestartServer(t *testing.T) {
	r := newRig(t, true)
	r.run()
	if err := r.api.Run(ReloadWorkspaceCommand, nil); err != nil {
		t.Fatal(err)
	}
	r.settle()
	reqs := r.api.ServerRequests(ServerID)
	if len(reqs) != 1 || reqs[0].Method != "rust-analyzer/reloadWorkspace" || string(reqs[0].Params) != "null" {
		t.Errorf("requests = %+v", reqs)
	}
	if err := r.api.Run(RestartServerCommand, nil); err != nil {
		t.Fatal(err)
	}
	if got := r.api.ServerRestarts(ServerID); got != 1 {
		t.Errorf("restarts = %d", got)
	}
}

func TestTheMenuOfTheStatusItemHasTheRustEntries(t *testing.T) {
	r := newRig(t, true)
	r.run()
	r.editor("src/main.rs", "fn main() {}\n")
	r.api.Emit(extension.TopicEditorActiveChanged, extension.ActiveEditor{URI: "file:///work/main.rs", LanguageID: "rust"})
	r.api.Pump()
	r.api.StatusItem("status." + ServerID).OnClick()
	pick := r.api.LastPick()
	if pick == nil {
		t.Fatal("no menu")
	}
	var labels []string
	for _, it := range pick.Options().Items {
		labels = append(labels, it.Label)
	}
	joined := strings.Join(labels, "|")
	if !strings.Contains(joined, "Reload Workspace") || !strings.Contains(joined, "Open Cargo.toml") || !strings.Contains(joined, "Restart") {
		t.Errorf("menu = %v", labels)
	}
	for i, l := range labels {
		if strings.Contains(l, "Reload Workspace") {
			pick.Choose(i)
		}
	}
	r.settle()
	if reqs := r.requests("rust-analyzer/reloadWorkspace"); len(reqs) != 1 {
		t.Errorf("the menu entry sent %d requests", len(reqs))
	}
}

func TestTheReadOnlyTabsFallBackToAnUntitledDocument(t *testing.T) {
	r := newRig(t, true)
	r.run()
	r.ext.reg = nil // a scope that cannot show provided documents
	r.editor("src/main.rs", "fn main() {}\n")
	r.answer(map[string]string{"rust-analyzer/expandMacro": `{"name":"m!","expansion":"1"}`})
	if err := r.api.Run(ExpandMacroCommand, nil); err != nil {
		t.Fatal(err)
	}
	r.settle()
	got := r.api.UntitledDocuments()
	if len(got) != 1 || got[0].LanguageID != "rust" || got[0].Text != "1" {
		t.Errorf("untitled = %+v", got)
	}
}

func TestTheContentProviderRefusesWhatItDoesNotHave(t *testing.T) {
	r := newRig(t, true)
	_, err := r.ext.contentText(context.Background(), extension.ContentURI(ContentScheme, "nothing/here.rs"))
	wantRoute(t, err, "contentText.RustContentUnknown")
	if got := r.api.ContentLabel(extension.ContentURI(ContentScheme, expansionPath)); got != "expansion.rs" {
		t.Errorf("label = %q", got)
	}
}

// logged is a logger whose lines the test can read.
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
			ext := New().(*rust)
			api := fakeapi.New(t, ext.Manifest(), extension.ScopeInfo{Name: "tab", Folder: t.TempDir()})
			var buf logged
			api.SetLogger(slog.New(slog.NewTextHandler(&buf, &slog.HandlerOptions{Level: slog.LevelDebug})))
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
			// the commands still work: the text opens in an untitled document
			api.SetServerStatus(ServerID, extension.ServerStatus{State: extension.ServerRunning})
			api.OnServerRequest(ServerID, func(fakeapi.ServerRequest) (json.RawMessage, error) {
				return json.RawMessage(`{"name":"m!","expansion":"1"}`), nil
			})
			api.SetDocument("file:///work/a.rs", "m!()")
			api.SetActive(&extension.ActiveDocument{URI: "file:///work/a.rs", LanguageID: "rust", Selections: []extension.Selection{{}}})
			if err := api.Run(ExpandMacroCommand, nil); err != nil {
				t.Fatal(err)
			}
			ext.wait()
			api.Pump()
			if got := api.UntitledDocuments(); len(got) != 1 || got[0].Text != "1" {
				t.Errorf("untitled = %+v", got)
			}
		})
	}
}
