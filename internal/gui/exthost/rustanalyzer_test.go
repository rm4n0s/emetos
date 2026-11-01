//go:build emetos_rustanalyzer

package exthost

import (
	"context"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/rm4n0s/emetos/extension"
	"github.com/rm4n0s/emetos/extensions/rust"
	"github.com/rm4n0s/emetos/internal/core/document"
	"github.com/rm4n0s/emetos/internal/core/langfeatures"
	"github.com/rm4n0s/emetos/internal/gui/testutil"
	"github.com/rm4n0s/emetos/internal/gui/views/problems"
)

// The tests against the real rust-analyzer (SPEC §10.34 "Tests", step 24b.2): the Rust pack runs in a window tab over the crate of
// testdata/rust/crate, with the rust-analyzer of /var/tmp/emetos-tools/bin (scripts/fetch-tools.sh rust-analyzer; the pinned release of
// scripts/tool-versions.txt) or the one at EMETOS_RUST_ANALYZER. They are skipped when the program is missing. cargo is the system's;
// its home folder is a temporary one, so that nothing is written to the user's.

// rustAnalyzerPath finds the program the tests run, or skips the test.
func rustAnalyzerPath(t *testing.T) string {
	t.Helper()
	path := os.Getenv("EMETOS_RUST_ANALYZER")
	if path == "" {
		path = "/var/tmp/emetos-tools/bin/rust-analyzer"
	}
	if _, err := os.Stat(path); err != nil {
		t.Skip("rust-analyzer is not installed (scripts/fetch-tools.sh rust-analyzer)")
	}
	if _, err := exec.LookPath("cargo"); err != nil {
		t.Skip("cargo is not installed")
	}
	return path
}

// capture is the Rust pack that keeps the API of its activation.
type capture struct {
	extension.Extension
	api *extension.API
}

// Activate implements extension.Extension.
func (c capture) Activate(ctx context.Context, api extension.API) error {
	*c.api = api
	return c.Extension.Activate(ctx, api)
}

// rustRig runs the Rust pack and rust-analyzer in a copy of the crate.
type rustRig struct {
	t      *testing.T
	r      *rig
	tb     *tab
	api    extension.API
	folder string
	docs   map[string]*document.Document
}

// newRustRig copies the crate (main.rs gets mainText when it is not empty) and starts the pack in a tab over it.
func newRustRig(t *testing.T, mainText string) *rustRig {
	t.Helper()
	program := rustAnalyzerPath(t)
	t.Setenv("CARGO_HOME", t.TempDir())
	t.Setenv("CARGO_TARGET_DIR", t.TempDir())
	g := &rustRig{t: t, folder: t.TempDir(), docs: map[string]*document.Document{}}
	fixture := filepath.Join(testutil.RepoRoot(t), "testdata", "rust", "crate")
	for _, name := range []string{"Cargo.toml", "src/main.rs", "src/geometry.rs"} {
		text, err := os.ReadFile(filepath.Join(fixture, filepath.FromSlash(name)))
		if err != nil {
			t.Fatal(err)
		}
		if name == "src/main.rs" && mainText != "" {
			text = []byte(mainText)
		}
		if err := os.MkdirAll(filepath.Join(g.folder, filepath.Dir(filepath.FromSlash(name))), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(g.folder, filepath.FromSlash(name)), text, 0o600); err != nil {
			t.Fatal(err)
		}
	}
	f := func() extension.Extension { return capture{Extension: rust.New(), api: &g.api} }
	g.r = newRig(t, f)
	g.r.folders["one"] = g.folder
	t.Cleanup(func() { g.r.host.Shutdown(); g.r.host.WaitServers() })
	g.tb = g.r.newTab("one")
	g.tb.deferred = true
	g.tb.changeSetting(rust.PathSetting, program)
	g.tb.scope.Start()
	g.tb.pump()
	for _, name := range []string{"src/main.rs", "src/geometry.rs"} {
		path := filepath.Join(g.folder, filepath.FromSlash(name))
		text, err := os.ReadFile(path)
		if err != nil {
			t.Fatal(err)
		}
		opts := document.NewOptions()
		opts.LanguageID = "rust"
		doc := document.New(document.FileURI(path), string(text), opts)
		g.docs[name] = doc
		g.tb.lsp.open(doc)
	}
	g.tb.pump()
	waitFor(t, g.tb, "rust-analyzer to run", func() bool { return g.status().State == ServerRunning })
	return g
}

// status is the state of rust-analyzer.
func (g *rustRig) status() ServerStatus {
	s, _ := g.tb.scope.ServerStatus(rust.ID, rust.ServerID)
	return s
}

// output is what the Output channel of rust-analyzer holds.
func (g *rustRig) output() string {
	return strings.Join(g.tb.channels.Lines("rust-analyzer"), "\n")
}

// langDoc is the document of the file as the registry of the language features wants it.
func (g *rustRig) langDoc(name string) langfeatures.Document {
	d := g.docs[name]
	return langfeatures.Document{URI: d.URI(), LanguageID: "rust", Version: d.Version(), Text: d.Snapshot()}
}

// pumped runs f on a goroutine while the tab's UI thread is driven, as the servers read other files through it.
func (g *rustRig) pumped(f func()) {
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

// rustWait is how long a test waits for rust-analyzer to load the crate and answer.
var rustWait = 120 * time.Second

// eventually runs check until it holds, for as long as rust-analyzer needs to load the crate.
func (g *rustRig) eventually(what string, check func() bool) {
	g.t.Helper()
	deadline := time.Now().Add(rustWait)
	for time.Now().Before(deadline) {
		if check() {
			return
		}
		g.tb.pump()
		time.Sleep(300 * time.Millisecond)
	}
	g.t.Fatalf("rust-analyzer did not give %s; Output:\n%s", what, g.output())
}

// stop closes the tab and waits for the server to end.
func (g *rustRig) stop() {
	pid := g.status().PID
	g.tb.scope.Close()
	g.r.host.WaitServers()
	requireGone(g.t, "rust-analyzer", pid)
}

func TestRustAnalyzerAnswersHoverCompletionDefinitionAndRename(t *testing.T) {
	g := newRustRig(t, "")
	reg := g.tb.scope.Features()
	main := g.langDoc("src/main.rs")
	lines := strings.Split(g.docs["src/main.rs"].Text(), "\n")
	use := 6 // let middle = midpoint(...)
	pos := langfeatures.Position{Line: use, Col: strings.Index(lines[use], "midpoint") + 2}

	// hover: the signature of the function, once the crate is loaded
	g.eventually("a hover with the signature", func() bool {
		ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
		defer cancel()
		var hs []langfeatures.Hover
		g.pumped(func() { hs, _ = reg.Hover(ctx, main, pos) })
		for _, h := range hs {
			for _, c := range h.Contents {
				if strings.Contains(c.Value, "fn midpoint(a: &Point, b: &Point) -> Point") {
					return true
				}
			}
		}
		return false
	})

	// completion at the empty line of main: the function with its type, and the local with its type
	at := langfeatures.Position{Line: 7, Col: 0}
	var items []langfeatures.CompletionItem
	g.eventually("completion with midpoint and a", func() bool {
		ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
		defer cancel()
		var list langfeatures.CompletionList
		g.pumped(func() {
			list, _ = reg.Completion(ctx, main, at, langfeatures.CompletionTrigger{Kind: langfeatures.TriggerInvoked})
		})
		items = list.Items
		var fn, local bool
		for _, it := range items {
			fn = fn || strings.HasPrefix(it.Label, "midpoint")
			local = local || it.Label == "a"
		}
		return fn && local
	})
	for _, it := range items {
		typed := it.Detail + it.LabelDetail + it.LabelDescription
		switch {
		case strings.HasPrefix(it.Label, "midpoint") && !strings.Contains(typed, "-> Point"):
			t.Errorf("the completion of midpoint shows no type: %+v", it)
		case it.Label == "a" && !strings.Contains(typed, "Point"):
			t.Errorf("the completion of the local a shows no type: %+v", it)
		}
	}

	// definition: geometry.rs, the line of the function
	var links []langfeatures.LocationLink
	g.eventually("the definition", func() bool {
		ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
		defer cancel()
		g.pumped(func() { links, _ = reg.Locations(ctx, main, pos, langfeatures.NavDefinition) })
		return len(links) == 1
	})
	if !strings.HasSuffix(links[0].Target.URI.Path, "geometry.rs") || links[0].TargetSelection.Start.Line != 7 {
		t.Errorf("definition = %+v", links)
	}

	// rename: the function and its use change, in two files
	var we *langfeatures.WorkspaceEdit
	var err error
	g.pumped(func() { we, err = reg.Rename(context.Background(), main, pos, "center") })
	if err != nil || we == nil {
		t.Fatalf("rename = %+v, %v\nOutput:\n%s", we, err, g.output())
	}
	g.pumped(func() {})
	if _, err := langfeatures.Apply(context.Background(), g.tb.lsp.edits, we); err != nil {
		t.Fatalf("apply: %v", err)
	}
	g.tb.pump()
	for name, doc := range g.docs {
		if got := doc.Text(); strings.Contains(got, "midpoint") || !strings.Contains(got, "center") {
			t.Errorf("%s after the rename:\n%s", name, got)
		}
	}
	g.stop()
}

func TestRustAnalyzerExpandsAMacroAndReportsItsStatus(t *testing.T) {
	g := newRustRig(t, "")
	for _, id := range []string{rust.ExpandMacroCommand, rust.ViewSyntaxTreeCommand, rust.ParentModuleCommand, rust.JoinLinesCommand, rust.RestartServerCommand} {
		if _, ok := g.tb.commands.Lookup(id); !ok {
			t.Errorf("the command %s is not registered", id)
		}
	}
	main := g.docs["src/main.rs"].Text()
	lines := strings.Split(main, "\n")
	line := 8 // let doubled = double!(middle.x);
	col := strings.Index(lines[line], "double!")
	enc, err := g.api.LanguageServers().PositionEncoding(rust.ServerID)
	if err != nil || enc != "utf-8" {
		t.Errorf("the position encoding = %q, %v; rust-analyzer takes the UTF-8 Emetos offers", enc, err)
	}
	params := map[string]any{
		"textDocument": map[string]any{"uri": uriOf(filepath.Join(g.folder, "src", "main.rs"))},
		"position":     map[string]any{"line": line, "character": col + 2},
	}
	var result json.RawMessage
	g.eventually("the expansion of double!", func() bool {
		ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
		defer cancel()
		g.pumped(func() {
			result, err = g.api.LanguageServers().Request(ctx, rust.ServerID, "rust-analyzer/expandMacro", params)
		})
		return err == nil && strings.Contains(string(result), "2.0")
	})
	var out struct{ Name, Expansion string }
	if err := json.Unmarshal(result, &out); err != nil || out.Name != "double!" || !strings.Contains(out.Expansion, "middle.x") {
		t.Errorf("the expansion = %s", result)
	}
	// the syntax tree of the file
	var tree json.RawMessage
	g.pumped(func() {
		tree, err = g.api.LanguageServers().Request(context.Background(), rust.ServerID, "rust-analyzer/viewSyntaxTree", map[string]any{
			"textDocument": params["textDocument"], "range": nil,
		})
	})
	if err != nil || !strings.Contains(string(tree), "SOURCE_FILE") {
		t.Errorf("the syntax tree = %s, %v", tree, err)
	}
	// the parent module of geometry.rs is main.rs
	var parent json.RawMessage
	g.pumped(func() {
		parent, err = g.api.LanguageServers().Request(context.Background(), rust.ServerID, "experimental/parentModule", map[string]any{
			"textDocument": map[string]any{"uri": uriOf(filepath.Join(g.folder, "src", "geometry.rs"))},
			"position":     map[string]any{"line": 0, "character": 0},
		})
	})
	if err != nil || !strings.Contains(string(parent), "main.rs") {
		t.Errorf("the parent module = %s, %v", parent, err)
	}
	// the server says how it does once the crate is loaded (without rust-src it warns that it cannot load the standard library)
	var item string
	g.eventually("the status of the server", func() bool {
		it, ok := g.tb.bar.Get("extension.emetos.rust.status.rust-analyzer")
		item = it.Entry.Text + " | " + it.Entry.Tooltip
		return ok && (strings.Contains(it.Entry.Text, "$(check)") || strings.Contains(it.Entry.Text, "$(warning)"))
	})
	t.Logf("the status item: %s", item)
	g.stop()
}

func TestRustAnalyzerPublishesTheDiagnosticsOfCargoCheck(t *testing.T) {
	src := "mod geometry;\n\nfn main() {\n    let unused = 5;\n    let wrong: i32 = \"text\";\n}\n"
	g := newRustRig(t, src)
	path := filepath.Join(g.folder, "src", "main.rs")
	var got []problems.Marker
	g.eventually("the diagnostics", func() bool {
		got = g.tb.markers.Read(problems.ReadFilter{Owner: "emetos.rust.rust-analyzer", Resource: path})
		var fromCargo bool
		for _, m := range got {
			fromCargo = fromCargo || m.Source == "rustc"
		}
		return fromCargo
	})
	var mismatch bool
	for _, m := range got {
		t.Logf("%v %d:%d %q source=%q code=%q", m.Severity, m.StartLine, m.StartColumn, m.Message, m.Source, m.Code)
		mismatch = mismatch || (m.Severity == problems.SeverityError && strings.Contains(m.Message, "mismatched types"))
	}
	if !mismatch {
		t.Errorf("no error about the mismatched types: %+v", got)
	}
	g.stop()
}
