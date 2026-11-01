//go:build emetos_clangd

package exthost

import (
	"context"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/rm4n0s/emetos/extension"
	"github.com/rm4n0s/emetos/extensions/cpp"
	"github.com/rm4n0s/emetos/internal/core/document"
	"github.com/rm4n0s/emetos/internal/core/langfeatures"
	"github.com/rm4n0s/emetos/internal/gui/testutil"
	"github.com/rm4n0s/emetos/internal/gui/views/problems"
)

// The tests against the real clangd (SPEC §10.34 "Tests", step 24b.3): the C and C++ pack runs in a window tab over a copy of the CMake
// project of testdata/cpp/project, with the clangd of the system (PATH), or the one at EMETOS_CLANGD. They are skipped when the program
// is missing. The compile_commands.json is made from the template of the project for the folder of the copy, as CMake writes it.

// clangdPath finds the program the tests run, or skips the test.
func clangdPath(t *testing.T) string {
	t.Helper()
	path := os.Getenv("EMETOS_CLANGD")
	if path == "" {
		var err error
		if path, err = exec.LookPath("clangd"); err != nil {
			t.Skip("clangd is not installed")
		}
	}
	return path
}

// cppCapture is the C and C++ pack that keeps the API of its activation.
type cppCapture struct {
	extension.Extension
	api *extension.API
}

// Activate implements extension.Extension.
func (c cppCapture) Activate(ctx context.Context, api extension.API) error {
	*c.api = api
	return c.Extension.Activate(ctx, api)
}

// cppRig runs the pack and clangd in a copy of the project.
type cppRig struct {
	t      *testing.T
	r      *rig
	tb     *tab
	api    extension.API
	folder string
	docs   map[string]*document.Document
	// statuses are the states clangd told for files (textDocument/clangd.fileStatus).
	mu       sync.Mutex
	statuses []string
}

// cppFiles are the files of the project that a document is opened for.
var cppFiles = []string{"src/main.cpp", "src/shapes.cpp", "include/shapes.h"}

// newCppRig copies the project (main.cpp gets mainText when it is not empty) and starts the pack in a tab over it.
func newCppRig(t *testing.T, mainText string) *cppRig {
	t.Helper()
	program := clangdPath(t)
	g := &cppRig{t: t, folder: t.TempDir(), docs: map[string]*document.Document{}}
	fixture := filepath.Join(testutil.RepoRoot(t), "testdata", "cpp", "project")
	for _, name := range append([]string{"CMakeLists.txt", "compile_commands.json.in"}, cppFiles...) {
		text, err := os.ReadFile(filepath.Join(fixture, filepath.FromSlash(name)))
		if err != nil {
			t.Fatal(err)
		}
		switch {
		case name == "src/main.cpp" && mainText != "":
			text = []byte(mainText)
		case name == "compile_commands.json.in":
			name, text = "compile_commands.json", []byte(strings.ReplaceAll(string(text), "@DIR@", g.folder))
		}
		if err := os.MkdirAll(filepath.Join(g.folder, filepath.Dir(filepath.FromSlash(name))), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(g.folder, filepath.FromSlash(name)), text, 0o600); err != nil {
			t.Fatal(err)
		}
	}
	f := func() extension.Extension { return cppCapture{Extension: cpp.New(), api: &g.api} }
	g.r = newRig(t, f)
	g.r.folders["one"] = g.folder
	t.Cleanup(func() { g.r.host.Shutdown(); g.r.host.WaitServers() })
	g.tb = g.r.newTab("one")
	g.tb.deferred = true
	g.tb.changeSetting(cpp.PathSetting, program)
	g.tb.scope.Start()
	g.tb.pump()
	if _, err := g.api.LanguageServers().OnNotification(cpp.ServerID, "textDocument/clangd.fileStatus", func(p json.RawMessage) {
		g.mu.Lock()
		g.statuses = append(g.statuses, string(p))
		g.mu.Unlock()
	}); err != nil {
		t.Fatal(err)
	}
	for _, name := range cppFiles {
		path := filepath.Join(g.folder, filepath.FromSlash(name))
		text, err := os.ReadFile(path)
		if err != nil {
			t.Fatal(err)
		}
		opts := document.NewOptions()
		opts.LanguageID = "cpp"
		doc := document.New(document.FileURI(path), string(text), opts)
		g.docs[name] = doc
		g.tb.lsp.open(doc)
	}
	g.tb.pump()
	waitFor(t, g.tb, "clangd to run", func() bool { return g.status().State == ServerRunning })
	return g
}

// status is the state of clangd.
func (g *cppRig) status() ServerStatus {
	s, _ := g.tb.scope.ServerStatus(cpp.ID, cpp.ServerID)
	return s
}

// output is what the Output channel of clangd holds.
func (g *cppRig) output() string { return strings.Join(g.tb.channels.Lines("clangd"), "\n") }

// langDoc is the document of the file as the registry of the language features wants it.
func (g *cppRig) langDoc(name string) langfeatures.Document {
	d := g.docs[name]
	return langfeatures.Document{URI: d.URI(), LanguageID: "cpp", Version: d.Version(), Text: d.Snapshot()}
}

// pumped runs f on a goroutine while the tab's UI thread is driven, as the servers read other files through it.
func (g *cppRig) pumped(f func()) {
	done := make(chan struct{})
	go func() { defer close(done); f() }()
	for {
		select {
		case <-done:
			return
		default:
			g.tb.pump()
			time.Sleep(5 * time.Millisecond)
		}
	}
}

// clangdWait is how long a test waits for clangd to parse the project and answer.
var clangdWait = 60 * time.Second

// eventually runs check until it holds, for as long as clangd needs to parse the project.
func (g *cppRig) eventually(what string, check func() bool) {
	g.t.Helper()
	deadline := time.Now().Add(clangdWait)
	for time.Now().Before(deadline) {
		if check() {
			return
		}
		g.tb.pump()
		time.Sleep(200 * time.Millisecond)
	}
	g.t.Fatalf("clangd did not give %s; Output:\n%s", what, g.output())
}

// stop closes the tab and waits for the server to end.
func (g *cppRig) stop() {
	pid := g.status().PID
	g.tb.scope.Close()
	g.r.host.WaitServers()
	requireGone(g.t, "clangd", pid)
}

// request sends a request of clangd's own protocol while the tab is driven.
func (g *cppRig) request(method string, params any) (result json.RawMessage, err error) {
	g.pumped(func() {
		ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
		defer cancel()
		result, err = g.api.LanguageServers().Request(ctx, cpp.ServerID, method, params)
	})
	return result, err
}

func TestClangdPublishesDiagnosticsAndAnswersHoverCompletionDefinitionAndRename(t *testing.T) {
	g := newCppRig(t, "")
	reg := g.tb.scope.Features()
	main := g.langDoc("src/main.cpp")
	lines := strings.Split(g.docs["src/main.cpp"].Text(), "\n")
	call := 4 // int total = area(r);
	pos := langfeatures.Position{Line: call, Col: strings.Index(lines[call], "area") + 2}
	path := filepath.Join(g.folder, "src", "main.cpp")

	// diagnostics: the use of an undeclared function, from clangd itself
	var got []problems.Marker
	g.eventually("the diagnostic of the undeclared function", func() bool {
		got = g.tb.markers.Read(problems.ReadFilter{Owner: "emetos.cpp.clangd", Resource: path})
		for _, m := range got {
			if m.Severity == problems.SeverityError && strings.Contains(m.Message, "missing") {
				return true
			}
		}
		return false
	})
	for _, m := range got {
		t.Logf("%v %d:%d %q source=%q code=%q", m.Severity, m.StartLine, m.StartColumn, m.Message, m.Source, m.Code)
	}

	// hover: the signature of the function
	g.eventually("a hover with the signature", func() bool {
		ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
		defer cancel()
		var hs []langfeatures.Hover
		g.pumped(func() { hs, _ = reg.Hover(ctx, main, pos) })
		for _, h := range hs {
			for _, c := range h.Contents {
				if strings.Contains(c.Value, "int area(const Rectangle &r)") {
					return true
				}
			}
		}
		return false
	})

	// completion: the function and the local, after "int total = a"
	at := langfeatures.Position{Line: call, Col: strings.Index(lines[call], "area") + 1}
	g.eventually("completion with area", func() bool {
		ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
		defer cancel()
		var list langfeatures.CompletionList
		g.pumped(func() {
			list, _ = reg.Completion(ctx, main, at, langfeatures.CompletionTrigger{Kind: langfeatures.TriggerInvoked})
		})
		for _, it := range list.Items {
			if strings.TrimSpace(it.Label) == "area" || strings.HasPrefix(strings.TrimSpace(it.Label), "area") {
				return true
			}
		}
		return false
	})

	// definition: the declaration in the header (or the definition in shapes.cpp once the background index knows it)
	var links []langfeatures.LocationLink
	g.eventually("the definition", func() bool {
		ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
		defer cancel()
		g.pumped(func() { links, _ = reg.Locations(ctx, main, pos, langfeatures.NavDefinition) })
		return len(links) >= 1
	})
	if p := links[0].Target.URI.Path; !strings.HasSuffix(p, "shapes.h") && !strings.HasSuffix(p, "shapes.cpp") {
		t.Errorf("definition = %+v", links)
	}

	// rename: the function and its uses change in the three files (the header and the source are found through the index)
	g.eventually("the rename of area", func() bool {
		var we *langfeatures.WorkspaceEdit
		var err error
		g.pumped(func() { we, err = reg.Rename(context.Background(), main, pos, "surface") })
		if err != nil || we == nil {
			return false
		}
		if _, err := langfeatures.Apply(context.Background(), g.tb.lsp.edits, we); err != nil {
			t.Fatalf("apply: %v", err)
		}
		g.tb.pump()
		return true
	})
	for name, doc := range g.docs {
		if got := doc.Text(); strings.Contains(got, "area") || !strings.Contains(got, "surface") {
			t.Errorf("%s after the rename:\n%s", name, got)
		}
	}
	g.stop()
}

func TestClangdSwitchesBetweenSourceAndHeaderAndShowsItsAST(t *testing.T) {
	g := newCppRig(t, "")
	for _, id := range []string{cpp.SwitchHeaderSourceCommand, cpp.ASTCommand, cpp.ToggleInlayHintsCommand, cpp.ProjectConfigCommand, cpp.RestartCommand} {
		if _, ok := g.tb.commands.Lookup(id); !ok {
			t.Errorf("the command %s is not registered", id)
		}
	}
	enc, err := g.api.LanguageServers().PositionEncoding(cpp.ServerID)
	if err != nil || enc != "utf-8" {
		t.Errorf("the position encoding = %q, %v; clangd takes the UTF-8 Emetos offers", enc, err)
	}
	shapes := uriOf(filepath.Join(g.folder, "src", "shapes.cpp"))
	var other string
	g.eventually("the header of shapes.cpp", func() bool {
		result, err := g.request("textDocument/switchSourceHeader", map[string]any{"uri": shapes})
		if err != nil || json.Unmarshal(result, &other) != nil {
			return false
		}
		return strings.HasSuffix(other, "/include/shapes.h")
	})
	// and back
	header := uriOf(filepath.Join(g.folder, "include", "shapes.h"))
	result, err := g.request("textDocument/switchSourceHeader", map[string]any{"uri": header})
	var back string
	if err != nil || json.Unmarshal(result, &back) != nil || !strings.HasSuffix(back, "/src/shapes.cpp") {
		t.Errorf("the source of shapes.h = %s, %v", result, err)
	}

	// the AST of the call area(r) at line 4 of main.cpp
	lines := strings.Split(g.docs["src/main.cpp"].Text(), "\n")
	col := strings.Index(lines[4], "area")
	var tree json.RawMessage
	g.eventually("the AST of the call", func() bool {
		tree, err = g.request("textDocument/ast", map[string]any{
			"textDocument": map[string]any{"uri": uriOf(filepath.Join(g.folder, "src", "main.cpp"))},
			"range":        map[string]any{"start": map[string]any{"line": 4, "character": col}, "end": map[string]any{"line": 4, "character": col + 7}},
		})
		return err == nil && strings.Contains(string(tree), `"kind"`)
	})
	var root struct {
		Role, Kind string
		Children   []json.RawMessage
	}
	if json.Unmarshal(tree, &root) != nil || root.Kind == "" || !strings.Contains(string(tree), "area") {
		t.Errorf("the AST = %s", tree)
	}

	// clangd tells the state of the files it works on, because the pack asked for it (clangdFileStatus)
	g.eventually("the file status", func() bool {
		g.mu.Lock()
		defer g.mu.Unlock()
		for _, s := range g.statuses {
			if strings.Contains(s, "main.cpp") {
				return true
			}
		}
		return false
	})
	g.stop()
}
