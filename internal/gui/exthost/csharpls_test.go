//go:build emetos_csharpls

package exthost

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/rm4n0s/emetos/extension"
	csharppack "github.com/rm4n0s/emetos/extensions/csharp"
	"github.com/rm4n0s/emetos/internal/core/document"
	"github.com/rm4n0s/emetos/internal/core/langfeatures"
	"github.com/rm4n0s/emetos/internal/core/lsp"
	"github.com/rm4n0s/emetos/internal/gui/testutil"
	"github.com/rm4n0s/emetos/internal/gui/views/problems"
	"github.com/rm4n0s/emetos/internal/gui/workbench"
)

// The tests against the real csharp-ls (SPEC §10.34 "Tests", step 24b.7): the C# pack runs in a window tab over a copy of the solution of
// testdata/csharp/solution, with the csharp-ls of /var/tmp/emetos-tools/dotnet-tools (scripts/fetch-tools.sh csharp-ls; the pin of
// scripts/tool-versions.txt) or the folder EMETOS_TOOLS, run by the system's .NET SDK. They are skipped when csharp-ls or dotnet is missing.
// Nothing reaches the user's home: HOME, XDG_*, DOTNET_CLI_HOME and NUGET_PACKAGES are temporary folders, the SDK's telemetry and
// first-run experience are off, and the solution has no package references and no package source, so nothing uses the network.

// csharpTools is the folder of the tools.
func csharpTools() string {
	if tools := os.Getenv("EMETOS_TOOLS"); tools != "" {
		return tools
	}
	return "/var/tmp/emetos-tools"
}

// csharpCapture is the pack that keeps the API of its activation.
type csharpCapture struct {
	extension.Extension
	api *extension.API
}

// Activate implements extension.Extension.
func (c csharpCapture) Activate(ctx context.Context, api extension.API) error {
	*c.api = api
	return c.Extension.Activate(ctx, api)
}

// csharpRig runs the pack and the server in a copy of the solution.
type csharpRig struct {
	t      *testing.T
	r      *rig
	tb     *tab
	api    extension.API
	folder string
	docs   map[string]*document.Document
}

const (
	csharpProgram = "App/Program.cs"
	csharpGreeter = "App/Greeter.cs"
)

// csharpFiles are the files of the solution that a document is opened for.
var csharpFiles = []string{csharpProgram, csharpGreeter}

// copyCSharpSolution copies the solution of testdata/csharp into the folder.
func copyCSharpSolution(t *testing.T, folder string) {
	t.Helper()
	root := filepath.Join(testutil.RepoRoot(t), "testdata", "csharp", "solution")
	err := filepath.WalkDir(root, func(path string, d os.DirEntry, err error) error {
		if err != nil || d.IsDir() {
			return err
		}
		rel, err := filepath.Rel(root, path)
		if err != nil {
			return err
		}
		text, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		dst := filepath.Join(folder, rel)
		if err := os.MkdirAll(filepath.Dir(dst), 0o755); err != nil {
			return err
		}
		return os.WriteFile(dst, text, 0o600)
	})
	if err != nil {
		t.Fatal(err)
	}
}

// isolateDotnet gives the SDK and the server folders of their own, and turns off what would reach out.
func isolateDotnet(t *testing.T, dotnet string) {
	t.Helper()
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(home, ".config"))
	t.Setenv("XDG_DATA_HOME", filepath.Join(home, ".local", "share"))
	t.Setenv("XDG_CACHE_HOME", filepath.Join(home, ".cache"))
	t.Setenv("DOTNET_CLI_HOME", filepath.Join(home, "dotnet-home"))
	t.Setenv("NUGET_PACKAGES", filepath.Join(home, "nuget-packages"))
	t.Setenv("NUGET_HTTP_CACHE_PATH", filepath.Join(home, "nuget-http"))
	t.Setenv("DOTNET_CLI_TELEMETRY_OPTOUT", "1")
	t.Setenv("DOTNET_NOLOGO", "1")
	t.Setenv("DOTNET_SKIP_FIRST_TIME_EXPERIENCE", "1")
	t.Setenv("DOTNET_ROOT", "")
	t.Setenv("PATH", strings.Join([]string{filepath.Dir(dotnet), "/usr/bin", "/bin"}, string(os.PathListSeparator)))
}

// newCSharpRig copies the solution and starts the pack in a tab over it.
func newCSharpRig(t *testing.T, extraSettings map[string]any) *csharpRig {
	t.Helper()
	program := filepath.Join(csharpTools(), "dotnet-tools", "csharp-ls")
	if _, err := os.Stat(program); err != nil {
		t.Skip("csharp-ls is not installed (scripts/fetch-tools.sh csharp-ls)")
	}
	dotnet, err := exec.LookPath("dotnet")
	if err != nil {
		t.Skip("dotnet is not installed")
	}
	isolateDotnet(t, dotnet)
	g := &csharpRig{t: t, folder: t.TempDir(), docs: map[string]*document.Document{}}
	copyCSharpSolution(t, g.folder)

	f := func() extension.Extension { return csharpCapture{Extension: csharppack.New(), api: &g.api} }
	g.r = newRig(t, f)
	g.r.folders["one"] = g.folder
	t.Cleanup(func() { g.r.host.Shutdown(); g.r.host.WaitServers() })
	g.tb = g.r.newTab("one")
	g.tb.deferred = true
	g.tb.changeSetting(csharppack.PathSetting, program)
	if trace := os.Getenv("EMETOS_CSHARPLS_TRACE"); trace != "" {
		g.tb.changeSetting(csharppack.TraceSetting, trace)
	}
	for k, v := range extraSettings {
		g.tb.changeSetting(k, v)
	}
	g.tb.scope.Start()
	g.tb.pump()
	for _, name := range csharpFiles {
		path := filepath.Join(g.folder, filepath.FromSlash(name))
		text, err := os.ReadFile(path)
		if err != nil {
			t.Fatal(err)
		}
		opts := document.NewOptions()
		opts.LanguageID = "csharp"
		doc := document.New(document.FileURI(path), string(text), opts)
		g.docs[name] = doc
		g.tb.lsp.open(doc)
	}
	// an editor of the language is in front: the status item shows
	if err := g.tb.scope.PublishCore(extension.TopicEditorActiveChanged, extension.ActiveEditor{URI: document.FileURI(filepath.Join(g.folder, filepath.FromSlash(csharpProgram))).String(), LanguageID: "csharp"}); err != nil {
		t.Fatal(err)
	}
	g.tb.pump()
	g.tb.frame()
	waitFor(t, g.tb, "csharp-ls to run", func() bool { return g.status().State == ServerRunning })
	return g
}

// status is the state of the server.
func (g *csharpRig) status() ServerStatus {
	s, _ := g.tb.scope.ServerStatus(csharppack.ID, csharppack.ServerID)
	return s
}

// output is what the Output channel of the server holds.
func (g *csharpRig) output() string { return strings.Join(g.tb.channels.Lines("C#"), "\n") }

// langDoc is the document of the file as the registry of the language features wants it.
func (g *csharpRig) langDoc(name string) langfeatures.Document {
	d := g.docs[name]
	return langfeatures.Document{URI: d.URI(), LanguageID: "csharp", Version: d.Version(), Text: d.Snapshot()}
}

// pumped runs f on a goroutine while the tab's UI thread is driven, as the servers read other files through it.
func (g *csharpRig) pumped(f func()) {
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

// csharpWait is how long a test waits for the server to load the solution and answer.
var csharpWait = 180 * time.Second

func init() {
	if v := os.Getenv("EMETOS_CSHARPLS_WAIT"); v != "" {
		if d, err := time.ParseDuration(v); err == nil {
			csharpWait = d
		}
	}
}

// eventually runs check until it holds, for as long as the server needs.
func (g *csharpRig) eventually(what string, check func() bool) {
	g.t.Helper()
	deadline := time.Now().Add(csharpWait)
	for time.Now().Before(deadline) {
		if check() {
			return
		}
		g.tb.pump()
		time.Sleep(250 * time.Millisecond)
	}
	g.t.Fatalf("csharp-ls did not give %s; Output:\n%s", what, g.output())
}

// stop closes the tab and waits for the server to end.
func (g *csharpRig) stop() {
	pid := g.status().PID
	g.tb.scope.Close()
	g.r.host.WaitServers()
	requireGone(g.t, "csharp-ls", pid)
}

// request sends a request of the server's own protocol while the tab is driven.
func (g *csharpRig) request(method string, params any) (result json.RawMessage, err error) {
	g.pumped(func() {
		ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
		defer cancel()
		result, err = g.api.LanguageServers().Request(ctx, csharppack.ServerID, method, params)
	})
	return result, err
}

// markers are the problems the server reported for a file.
func (g *csharpRig) markers(name string) []problems.Marker {
	return g.tb.markers.Read(problems.ReadFilter{Owner: csharppack.ID + "." + csharppack.ServerID, Resource: filepath.Join(g.folder, filepath.FromSlash(name))})
}

// item is the status item of the server.
func (g *csharpRig) item() workbench.StatusEntry {
	for _, a := range []workbench.StatusAlignment{workbench.StatusLeft, workbench.StatusRight} {
		for _, it := range g.tb.bar.Items(a) {
			if strings.HasSuffix(it.ID, "status."+csharppack.ServerID) {
				return it.Entry
			}
		}
	}
	return workbench.StatusEntry{}
}

// hasCSharpMarker tells whether a problem on the line (1-based) has the text in its message.
func hasCSharpMarker(list []problems.Marker, line int, text string) bool {
	for _, m := range list {
		if m.StartLine == line && strings.Contains(m.Message, text) {
			return true
		}
	}
	return false
}

func TestCsharpLsAnswersDiagnosticsHoverCompletionDefinitionAndMetadata(t *testing.T) {
	g := newCSharpRig(t, nil)
	reg := g.tb.scope.Features()
	program := g.langDoc(csharpProgram)
	lines := strings.Split(g.docs[csharpProgram].Text(), "\n")
	line := func(text string) int {
		for i, l := range lines {
			if strings.Contains(l, text) {
				return i
			}
		}
		t.Fatalf("no line with %q in\n%s", text, strings.Join(lines, "\n"))
		return 0
	}

	// the problems: the type error and the unknown name from Roslyn
	g.eventually("the diagnostics", func() bool {
		got := g.markers(csharpProgram)
		return hasCSharpMarker(got, 12, "Cannot implicitly convert type 'string' to 'int'") && hasCSharpMarker(got, 13, "The name 'missing' does not exist in the current context")
	})
	if tip := g.item().Tooltip; !strings.Contains(tip, "Loads App.sln (the only one in the folder)") || !strings.Contains(tip, "csharp-ls 0.28.0") {
		t.Errorf("the tooltip = %q", tip)
	}

	// hover on Greeter: the type and its documentation
	var lastHover string
	use := line(`Greeter.Greet("world")`)
	g.eventually("a hover with the documentation", func() bool {
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()
		var hs []langfeatures.Hover
		g.pumped(func() {
			hs, _ = reg.Hover(ctx, program, langfeatures.Position{Line: use, Col: strings.Index(lines[use], "Greeter") + 2})
		})
		var seen []string
		for _, h := range hs {
			for _, c := range h.Contents {
				seen = append(seen, c.Value)
				if strings.Contains(c.Value, "Greeter") && strings.Contains(c.Value, "Says hello.") {
					return true
				}
			}
		}
		if text := strings.Join(seen, "|"); len(seen) > 0 && text != lastHover {
			lastHover = text
			t.Logf("hover: %q", text)
		}
		return false
	})

	// completion after "Greeter." offers Greet
	at := langfeatures.Position{Line: use, Col: strings.Index(lines[use], "Greet(") + 2}
	var lastLabels string
	g.eventually("completion with Greet", func() bool {
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()
		var list langfeatures.CompletionList
		var perr []langfeatures.ProviderError
		g.pumped(func() {
			list, perr = reg.Completion(ctx, program, at, langfeatures.CompletionTrigger{Kind: langfeatures.TriggerInvoked})
		})
		var labels []string
		for _, it := range list.Items {
			labels = append(labels, strings.TrimSpace(it.Label))
			if strings.TrimSpace(it.Label) == "Greet" {
				return true
			}
		}
		if text := fmt.Sprint(perr, labels); text != lastLabels {
			lastLabels = text
			t.Logf("completion: %d items, error %v", len(list.Items), perr)
		}
		return false
	})

	// definition: Greeter is a class of the solution, Console one of the framework
	var links []langfeatures.LocationLink
	g.eventually("the definition of Greeter", func() bool {
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()
		g.pumped(func() {
			links, _ = reg.Locations(ctx, program, langfeatures.Position{Line: use, Col: strings.Index(lines[use], "Greeter") + 1}, langfeatures.NavDefinition)
		})
		return len(links) >= 1
	})
	if p := links[0].Target.URI.Path; !strings.HasSuffix(p, "/App/Greeter.cs") {
		t.Errorf("definition of Greeter = %+v", links)
	}
	console := line("Console.WriteLine(names")
	g.eventually("the definition of Console in the framework", func() bool {
		ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
		defer cancel()
		g.pumped(func() {
			links, _ = reg.Locations(ctx, program, langfeatures.Position{Line: console, Col: strings.Index(lines[console], "WriteLine") + 2}, langfeatures.NavDefinition)
		})
		return len(links) >= 1
	})
	target := links[0].Target.URI
	// the server gives a csharp:/ address, which the editor shows as a read-only document of the provider of the scheme csharp
	if target.Scheme != document.SchemeVirtual || !strings.HasPrefix(target.Path, "/csharp/") || !strings.HasSuffix(target.Path, "/App/App.csproj/decompiled/System.Console.cs") {
		t.Fatalf("definition of WriteLine = %+v", links)
	}
	serverURI := lsp.DocumentURI(target)
	if !strings.HasPrefix(serverURI, "csharp:/") || strings.HasPrefix(serverURI, "csharp://") {
		t.Errorf("the server's address = %q", serverURI)
	}
	// the provider asks the server for the text with the address as the server gave it
	var reply struct {
		Source string `json:"source"`
	}
	g.eventually("the decompiled text of Console", func() bool {
		result, err := g.request("csharp/metadata", map[string]any{"textDocument": map[string]any{"uri": serverURI}})
		return err == nil && json.Unmarshal(result, &reply) == nil && strings.Contains(reply.Source, "class Console")
	})

	// rename Greet at its declaration: the declaration and its two uses change in both files
	greeter := g.langDoc(csharpGreeter)
	gl := strings.Split(g.docs[csharpGreeter].Text(), "\n")
	declLine := -1
	for i, l := range gl {
		if strings.Contains(l, "string Greet(") {
			declLine = i
		}
	}
	if declLine < 0 {
		t.Fatalf("no declaration of Greet in\n%s", g.docs[csharpGreeter].Text())
	}
	declaration := langfeatures.Position{Line: declLine, Col: strings.Index(gl[declLine], "Greet") + 1}
	g.eventually("the rename of Greet", func() bool {
		var we *langfeatures.WorkspaceEdit
		var err error
		g.pumped(func() { we, err = reg.Rename(context.Background(), greeter, declaration, "Salute") })
		if err != nil || we == nil {
			return false
		}
		if _, err := langfeatures.Apply(context.Background(), g.tb.lsp.edits, we); err != nil {
			t.Fatalf("apply: %v", err)
		}
		g.tb.pump()
		return true
	})
	if text := g.docs[csharpGreeter].Text(); !strings.Contains(text, "string Salute(string name)") || strings.Contains(text, "Greet(") {
		t.Errorf("Greeter.cs after the rename:\n%s", text)
	}
	if text := g.docs[csharpProgram].Text(); strings.Count(text, "Greeter.Salute(") != 2 || strings.Contains(text, "Greet(") {
		t.Errorf("Program.cs after the rename:\n%s", text)
	}
	g.stop()
}

// A solution the user chose is the one csharp-ls loads: the choice is a path in the state of the folder, so this one is the setting.
func TestCsharpLsLoadsTheSolutionOfTheSetting(t *testing.T) {
	g := newCSharpRig(t, map[string]any{csharppack.SolutionSetting: "App/App.csproj"})
	g.eventually("the diagnostics", func() bool {
		return hasCSharpMarker(g.markers(csharpProgram), 12, "Cannot implicitly convert type 'string' to 'int'")
	})
	if tip := g.item().Tooltip; !strings.Contains(tip, "Loads App/App.csproj (csharp.solution)") {
		t.Errorf("the tooltip = %q", tip)
	}
	g.stop()
}
