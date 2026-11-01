//go:build emetos_tsls

package exthost

import (
	"context"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/rm4n0s/emetos/extension"
	"github.com/rm4n0s/emetos/extensions/typescript"
	"github.com/rm4n0s/emetos/internal/core/document"
	"github.com/rm4n0s/emetos/internal/core/langfeatures"
	"github.com/rm4n0s/emetos/internal/gui/testutil"
	"github.com/rm4n0s/emetos/internal/gui/views/problems"
	"github.com/rm4n0s/emetos/internal/gui/workbench"
)

// The tests against the real typescript-language-server (SPEC §10.34 "Tests", step 24b.5): the TypeScript pack runs in a window tab over a
// copy of the project of testdata/typescript/project, with the programs of /var/tmp/emetos-tools (scripts/fetch-tools.sh
// typescript-language-server typescript typescript5; the pins of scripts/tool-versions.txt) or the folder EMETOS_TOOLS, run by the
// system's node. They are skipped when a program is missing. HOME is a temporary folder and PATH holds the system's node only. The server
// is started with typescript 6.0.3 beside it; the project has its own node_modules/typescript, 5.9.3 (a link to the typescript5 package),
// which must not run unless the test chooses it.

const (
	globalTS    = "6.0.3"
	workspaceTS = "5.9.3"
)

// tsFiles are the files of the project that a document is opened for, with their languages.
var tsFiles = map[string]string{"src/main.ts": "typescript", "src/helpers.ts": "typescript", "src/unsorted.ts": "typescript", "src/script.js": "javascript"}

// tsToolsDir is the folder of the tools.
func tsToolsDir() string {
	if tools := os.Getenv("EMETOS_TOOLS"); tools != "" {
		return tools
	}
	return "/var/tmp/emetos-tools"
}

// tsCapture is the pack that keeps the API of its activation.
type tsCapture struct {
	extension.Extension
	api *extension.API
}

// Activate implements extension.Extension.
func (c tsCapture) Activate(ctx context.Context, api extension.API) error {
	*c.api = api
	return c.Extension.Activate(ctx, api)
}

// tsRig runs the pack and the server in a copy of the project.
type tsRig struct {
	t      *testing.T
	r      *rig
	tb     *tab
	api    extension.API
	folder string
	docs   map[string]*document.Document
}

// writeFile writes a file below the folder.
func writeFile(t *testing.T, folder, name, text string) {
	t.Helper()
	path := filepath.Join(folder, filepath.FromSlash(name))
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(text), 0o600); err != nil {
		t.Fatal(err)
	}
}

// newTSRig copies the project, adds node_modules (emetoslib, and typescript when workspace is set), and starts the pack in a tab over it.
// chosen is what the user chose for the folder in the state ("" for nothing).
func newTSRig(t *testing.T, chosen string, extraSettings map[string]any) *tsRig {
	t.Helper()
	tools := tsToolsDir()
	program := filepath.Join(tools, "node", "node_modules", ".bin", "typescript-language-server")
	workspace := filepath.Join(tools, "node", "node_modules", "typescript5")
	node, err := exec.LookPath("node")
	if err != nil {
		t.Skip("node is not installed")
	}
	for _, p := range []string{program, workspace, filepath.Join(tools, "node", "node_modules", "typescript")} {
		if _, err := os.Stat(p); err != nil {
			t.Skip("typescript-language-server is not installed (scripts/fetch-tools.sh typescript-language-server typescript typescript5)")
		}
	}
	t.Setenv("HOME", t.TempDir())
	t.Setenv("PATH", strings.Join([]string{filepath.Dir(node), "/usr/bin", "/bin"}, string(os.PathListSeparator)))
	g := &tsRig{t: t, folder: t.TempDir(), docs: map[string]*document.Document{}}
	fixture := filepath.Join(testutil.RepoRoot(t), "testdata", "typescript", "project")
	for _, name := range []string{"tsconfig.json", "package.json", "src/main.ts", "src/helpers.ts", "src/unsorted.ts", "src/script.js"} {
		text, err := os.ReadFile(filepath.Join(fixture, filepath.FromSlash(name)))
		if err != nil {
			t.Fatal(err)
		}
		writeFile(t, g.folder, name, string(text))
	}
	// a library with a declaration file and a source, as the packages of npm have
	writeFile(t, g.folder, "node_modules/emetoslib/package.json", `{"name":"emetoslib","version":"1.0.0","type":"module","main":"index.js","types":"index.d.ts"}`)
	writeFile(t, g.folder, "node_modules/emetoslib/index.d.ts", "/** Say hello to name. */\nexport declare function greet(name: string): string;\n")
	writeFile(t, g.folder, "node_modules/emetoslib/index.js", "export function greet(name) {\n  return \"hello \" + name;\n}\n")
	if err := os.Symlink(workspace, filepath.Join(g.folder, "node_modules", "typescript")); err != nil {
		t.Fatal(err)
	}

	f := func() extension.Extension { return tsCapture{Extension: typescript.New(), api: &g.api} }
	g.r = newRig(t, f)
	g.r.folders["one"] = g.folder
	t.Cleanup(func() { g.r.host.Shutdown(); g.r.host.WaitServers() })
	g.tb = g.r.newTab("one")
	g.tb.deferred = true
	g.tb.changeSetting(typescript.PathSetting, program)
	g.tb.changeSetting(typescript.DisableTypeAcquisitionSetting, true)
	for k, v := range extraSettings {
		g.tb.changeSetting(k, v)
	}
	if chosen != "" {
		g.tb.keptMu.Lock()
		g.tb.kept = map[string]json.RawMessage{typescript.ID + "\x00" + g.folder + "\x00" + typescript.StateKey: json.RawMessage(strconv.Quote(chosen))}
		g.tb.keptMu.Unlock()
	}
	g.tb.scope.Start()
	g.tb.pump()
	for name, lang := range tsFiles {
		path := filepath.Join(g.folder, filepath.FromSlash(name))
		text, err := os.ReadFile(path)
		if err != nil {
			t.Fatal(err)
		}
		opts := document.NewOptions()
		opts.LanguageID = lang
		doc := document.New(document.FileURI(path), string(text), opts)
		g.docs[name] = doc
		g.tb.lsp.open(doc)
	}
	g.tb.pump()
	if err := g.tb.scope.PublishCore(extension.TopicEditorActiveChanged, extension.ActiveEditor{URI: g.docs["src/main.ts"].URI().String(), LanguageID: "typescript"}); err != nil {
		t.Fatal(err)
	}
	g.tb.frame()
	waitFor(t, g.tb, "typescript-language-server to run", func() bool { return g.status().State == ServerRunning })
	return g
}

// status is the state of the server.
func (g *tsRig) status() ServerStatus {
	s, _ := g.tb.scope.ServerStatus(typescript.ID, typescript.ServerID)
	return s
}

// output is what the Output channel of the server holds.
func (g *tsRig) output() string { return strings.Join(g.tb.channels.Lines("TypeScript"), "\n") }

// langDoc is the document of the file as the registry of the language features wants it.
func (g *tsRig) langDoc(name string) langfeatures.Document {
	d := g.docs[name]
	return langfeatures.Document{URI: d.URI(), LanguageID: tsFiles[name], Version: d.Version(), Text: d.Snapshot()}
}

// pumped runs f on a goroutine while the tab's UI thread is driven, as the servers read other files through it.
func (g *tsRig) pumped(f func()) {
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

// tsWait is how long a test waits for the server to analyze the project and answer.
var tsWait = 90 * time.Second

// eventually runs check until it holds, for as long as the server needs.
func (g *tsRig) eventually(what string, check func() bool) {
	g.t.Helper()
	deadline := time.Now().Add(tsWait)
	for time.Now().Before(deadline) {
		if check() {
			return
		}
		g.tb.pump()
		time.Sleep(200 * time.Millisecond)
	}
	g.t.Fatalf("typescript-language-server did not give %s; Output:\n%s", what, g.output())
}

// stop closes the tab and waits for the server to end.
func (g *tsRig) stop() {
	pid := g.status().PID
	g.tb.scope.Close()
	g.r.host.WaitServers()
	requireGone(g.t, "typescript-language-server", pid)
}

// request sends a request of the server's own protocol while the tab is driven.
func (g *tsRig) request(method string, params any) (result json.RawMessage, err error) {
	g.pumped(func() {
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()
		result, err = g.api.LanguageServers().Request(ctx, typescript.ServerID, method, params)
	})
	return result, err
}

// markers are the problems the server reported for a file.
func (g *tsRig) markers(name string) []problems.Marker {
	return g.tb.markers.Read(problems.ReadFilter{Owner: typescript.ID + "." + typescript.ServerID, Resource: filepath.Join(g.folder, filepath.FromSlash(name))})
}

// hasMarker tells whether a problem on the line (0-based) has the text in its message.
func hasMarker(list []problems.Marker, line int, text string) bool {
	for _, m := range list {
		if m.StartLine == line+1 && strings.Contains(m.Message, text) {
			return true
		}
	}
	return false
}

// itemText is the text of the status item of the server.
func (g *tsRig) itemText() string {
	for _, a := range []workbench.StatusAlignment{workbench.StatusLeft, workbench.StatusRight} {
		for _, it := range g.tb.bar.Items(a) {
			if strings.HasSuffix(it.ID, "status."+typescript.ServerID) {
				return it.Entry.Text
			}
		}
	}
	return ""
}

// itemTooltip is the tooltip of the status item of the server.
func (g *tsRig) itemTooltip() string {
	for _, a := range []workbench.StatusAlignment{workbench.StatusLeft, workbench.StatusRight} {
		for _, it := range g.tb.bar.Items(a) {
			if strings.HasSuffix(it.ID, "status."+typescript.ServerID) {
				return it.Entry.Tooltip
			}
		}
	}
	return ""
}

func TestTypeScriptServerAnswersDiagnosticsHoverCompletionDefinitionAndRename(t *testing.T) {
	g := newTSRig(t, "", nil)
	reg := g.tb.scope.Features()
	main := g.langDoc("src/main.ts")
	lines := strings.Split(g.docs["src/main.ts"].Text(), "\n")

	// the diagnostics: the type error and the unknown name, from tsserver, and no unresolved import of the library
	var got []problems.Marker
	g.eventually("the diagnostics", func() bool {
		got = g.markers("src/main.ts")
		return hasMarker(got, 4, "not assignable") && hasMarker(got, 5, "missing_name")
	})
	for _, m := range got {
		if m.Source != "typescript" {
			t.Errorf("the source of %q is %q", m.Message, m.Source)
		}
		if strings.Contains(m.Message, "emetoslib") {
			t.Errorf("the library is not resolved: %q", m.Message)
		}
	}

	// hover on greet: the signature and the documentation of the declaration file of the library
	greetUse := langfeatures.Position{Line: 5, Col: strings.Index(lines[5], "greet") + 2}
	g.eventually("a hover with the signature and the documentation", func() bool {
		ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
		defer cancel()
		var hs []langfeatures.Hover
		g.pumped(func() { hs, _ = reg.Hover(ctx, main, greetUse) })
		for _, h := range hs {
			for _, c := range h.Contents {
				if strings.Contains(c.Value, "greet(name: string): string") && strings.Contains(c.Value, "Say hello") {
					return true
				}
			}
		}
		return false
	})

	// completion of add in "const total: number = ad"
	at := langfeatures.Position{Line: 3, Col: strings.Index(lines[3], "add") + 2}
	g.eventually("completion with add", func() bool {
		ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
		defer cancel()
		var list langfeatures.CompletionList
		g.pumped(func() {
			list, _ = reg.Completion(ctx, main, at, langfeatures.CompletionTrigger{Kind: langfeatures.TriggerInvoked})
		})
		for _, it := range list.Items {
			if strings.TrimSpace(it.Label) == "add" {
				return true
			}
		}
		return false
	})

	// definition: add is defined in helpers.ts
	addUse := langfeatures.Position{Line: 3, Col: strings.Index(lines[3], "add") + 1}
	var links []langfeatures.LocationLink
	g.eventually("the definition of add", func() bool {
		ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
		defer cancel()
		g.pumped(func() { links, _ = reg.Locations(ctx, main, addUse, langfeatures.NavDefinition) })
		return len(links) >= 1
	})
	if p := links[0].Target.URI.Path; !strings.HasSuffix(p, "/src/helpers.ts") {
		t.Errorf("definition = %+v", links)
	}

	// the status item shows the TypeScript beside the server, not the folder's own
	if text := g.itemText(); text != "$(check) TypeScript "+globalTS {
		t.Errorf("the status item = %q", text)
	}
	if tip := g.itemTooltip(); !strings.Contains(tip, "(global)") || strings.Contains(tip, "node_modules/typescript/lib") && strings.Contains(tip, g.folder) {
		t.Errorf("the tooltip = %q", tip)
	}

	// rename at the definition: add and its uses change in the three files (at the use in main.ts the server, asked for aliases by
	// typescript.preferences.useAliasesForRenames, would write "add as plus")
	helpers := g.langDoc("src/helpers.ts")
	declaration := langfeatures.Position{Line: 0, Col: len("export function a")}
	g.eventually("the rename of add", func() bool {
		var we *langfeatures.WorkspaceEdit
		var err error
		g.pumped(func() { we, err = reg.Rename(context.Background(), helpers, declaration, "plus") })
		if err != nil || we == nil {
			return false
		}
		if _, err := langfeatures.Apply(context.Background(), g.tb.lsp.edits, we); err != nil {
			t.Fatalf("apply: %v", err)
		}
		g.tb.pump()
		return true
	})
	if text := g.docs["src/helpers.ts"].Text(); !strings.Contains(text, "function plus(") || strings.Contains(text, "add") {
		t.Errorf("helpers.ts after the rename:\n%s", text)
	}
	if text := g.docs["src/main.ts"].Text(); strings.Contains(text, "add") || strings.Count(text, "plus") != 2 {
		t.Errorf("main.ts after the rename:\n%s", text)
	}
	if text := g.docs["src/unsorted.ts"].Text(); strings.Contains(text, "add") {
		t.Errorf("unsorted.ts after the rename:\n%s", text)
	}
	g.stop()
}

// executeCommand sends workspace/executeCommand, as the commands of the pack do.
func (g *tsRig) executeCommand(command string, args ...any) (json.RawMessage, error) {
	return g.request("workspace/executeCommand", map[string]any{"command": command, "arguments": args})
}

func TestTypeScriptServerOrganizesImportsWithTheCodeActionOfTheCommand(t *testing.T) {
	g := newTSRig(t, "", nil)
	reg := g.tb.scope.Features()
	doc := g.docs["src/unsorted.ts"]
	// Shift+Alt+O is enabled by the kinds the server announces
	g.eventually("the kind source.organizeImports", func() bool {
		return reg.SupportsCodeActionKind(g.langDoc("src/unsorted.ts"), langfeatures.KindSourceOrganize)
	})
	var title string
	var told error
	done := false
	// Organize Imports of the pack asks for exactly this
	err := g.api.LanguageServers().ApplyCodeAction(context.Background(), typescript.ServerID, doc.URI().String(),
		extension.CodeActionRequest{Only: []string{"source.organizeImports"}}, func(t string, err error) { title, told, done = t, err, true })
	if err != nil {
		t.Fatal(err)
	}
	waitFor(t, g.tb, "the code action", func() bool { return done })
	if told != nil || title == "" {
		t.Fatalf("title %q, error %v", title, told)
	}
	g.eventually("the imports to be combined", func() bool {
		return strings.HasPrefix(doc.Text(), "import { add, sub } from \"./helpers\";\n")
	})
	if !doc.CanUndo() {
		t.Error("nothing to undo")
	}
	g.stop()
}

func TestTypeScriptServerSourceDefinitionProjectConfigurationAndFileReferences(t *testing.T) {
	g := newTSRig(t, "", nil)
	lines := strings.Split(g.docs["src/main.ts"].Text(), "\n")
	main := uriOf(filepath.Join(g.folder, "src", "main.ts"))

	// Go to Source Definition: from the call of greet, whose declaration is in a .d.ts, to the source of the library
	var locations []struct {
		URI string `json:"uri"`
	}
	g.eventually("the source definition of greet", func() bool {
		result, err := g.executeCommand("_typescript.goToSourceDefinition", main, map[string]any{"line": 5, "character": strings.Index(lines[5], "greet") + 1})
		return err == nil && json.Unmarshal(result, &locations) == nil && len(locations) > 0
	})
	if !strings.HasSuffix(locations[0].URI, "/node_modules/emetoslib/index.js") {
		t.Errorf("source definition = %+v", locations)
	}

	// Go to Project Configuration: tsserver tells the project of the file; a file that is in no project has an implicit one
	projectOf := func(name string) string {
		var configFile string
		g.eventually("the project of "+name, func() bool {
			result, err := g.executeCommand("typescript.tsserverRequest", "projectInfo", map[string]any{"file": filepath.Join(g.folder, filepath.FromSlash(name)), "needFileNameList": false}, map[string]any{"isAsync": false, "lowPriority": false})
			var response struct {
				Body struct {
					ConfigFileName string `json:"configFileName"`
				} `json:"body"`
			}
			if err != nil || json.Unmarshal(result, &response) != nil {
				return false
			}
			configFile = response.Body.ConfigFileName
			return configFile != ""
		})
		return configFile
	}
	if got := projectOf("src/main.ts"); got != filepath.Join(g.folder, "tsconfig.json") {
		t.Errorf("the project of main.ts = %q", got)
	}
	t.Logf("the project of script.js = %q", projectOf("src/script.js"))

	// Find File References: the files that import helpers.ts
	var files []string
	g.eventually("the references to helpers.ts", func() bool {
		result, err := g.executeCommand("typescript.tsserverRequest", "fileReferences", map[string]any{"file": filepath.Join(g.folder, "src", "helpers.ts")}, map[string]any{"isAsync": false, "lowPriority": false})
		var response struct {
			Body struct {
				Refs []struct {
					File string `json:"file"`
				} `json:"refs"`
			} `json:"body"`
		}
		if err != nil || json.Unmarshal(result, &response) != nil {
			return false
		}
		files = nil
		for _, r := range response.Body.Refs {
			files = append(files, filepath.Base(r.File))
		}
		return len(files) >= 3
	})
	t.Logf("the files that import helpers.ts: %v", files)
	g.stop()
}

func TestTypeScriptServerRunsTheWorkspaceVersionOnlyWhenChosen(t *testing.T) {
	// the version of the folder is there from the start and is not used until the choice is made: the first test of this file shows the
	// version beside the server; here the choice is kept in the state and the server starts with the folder's own
	g := newTSRig(t, typescript.ChoiceWorkspace, nil)
	g.eventually("the diagnostics of TypeScript "+workspaceTS, func() bool { return hasMarker(g.markers("src/main.ts"), 4, "not assignable") })
	if text := g.itemText(); text != "$(check) TypeScript "+workspaceTS+" (workspace)" {
		t.Errorf("the status item = %q", text)
	}
	if out := g.output(); strings.Contains(out, "Using Typescript version (bundled)") {
		t.Errorf("the server used its own TypeScript:\n%s", out)
	}
	// the choice made again with the command restarts the server with the other one
	if err := g.tb.commands.Run(commandContext(), typescript.SelectVersionCommand, typescript.ChoiceGlobal); err != nil {
		t.Fatal(err)
	}
	g.tb.pump()
	waitFor(t, g.tb, "the server to start again", func() bool { return g.status().Starts >= 2 && g.status().State == ServerRunning })
	g.eventually("TypeScript "+globalTS, func() bool { return g.itemText() == "$(check) TypeScript "+globalTS })
	g.eventually("the diagnostics again", func() bool { return hasMarker(g.markers("src/main.ts"), 4, "not assignable") })
	g.stop()
}

func TestTypeScriptServerGetsTheFormatAndInlayHintSettingsOfTheLanguage(t *testing.T) {
	g := newTSRig(t, "", map[string]any{
		"typescript.format.semicolons":                          "remove",
		"typescript.inlayHints.parameterNames.enabled":          "all",
		"javascript.inlayHints.parameterNames.enabled":          "none",
		"typescript.preferences.importModuleSpecifier":          "relative",
		"typescript.preferences.quoteStyle":                     "single",
		"typescript.suggest.completeFunctionCalls":              true,
		"typescript.referencesCodeLens.enabled":                 true,
		"typescript.inlayHints.variableTypes.enabled":           true,
		"typescript.inlayHints.parameterTypes.enabled":          true,
		"typescript.inlayHints.functionLikeReturnTypes.enabled": true,
	})
	main := uriOf(filepath.Join(g.folder, "src", "main.ts"))

	// the parameter names of add(1, 2) are hinted: the setting reached the server with workspace/didChangeConfiguration
	var hints []struct {
		Label []struct {
			Value string `json:"value"`
		} `json:"label"`
	}
	g.eventually("the inlay hints", func() bool {
		result, err := g.request("textDocument/inlayHint", map[string]any{
			"textDocument": map[string]any{"uri": main},
			"range":        map[string]any{"start": map[string]any{"line": 0, "character": 0}, "end": map[string]any{"line": 6, "character": 0}},
		})
		return err == nil && json.Unmarshal(result, &hints) == nil && len(hints) > 0
	})
	var labels []string
	for _, h := range hints {
		var text string
		for _, part := range h.Label {
			text += part.Value
		}
		labels = append(labels, text)
	}
	if got := strings.Join(labels, " "); !strings.Contains(got, "a:") || !strings.Contains(got, "b:") {
		t.Errorf("hints = %q", got)
	}

	// formatting removes the semicolons: format.semicolons reached the server too
	var edits []struct {
		NewText string `json:"newText"`
	}
	g.eventually("the formatting edits", func() bool {
		result, err := g.request("textDocument/formatting", map[string]any{
			"textDocument": map[string]any{"uri": main}, "options": map[string]any{"tabSize": 2, "insertSpaces": true},
		})
		return err == nil && json.Unmarshal(result, &edits) == nil && len(edits) > 0
	})
	removed := 0
	for _, e := range edits {
		if e.NewText == "" {
			removed++
		}
	}
	if removed < 4 {
		t.Errorf("%d semicolons were removed by %d edits, want the 4 of main.ts", removed, len(edits))
	}
	g.stop()
}

func TestTypeScriptServerServesJavaScriptFiles(t *testing.T) {
	g := newTSRig(t, "", nil)
	reg := g.tb.scope.Features()
	script := g.langDoc("src/script.js")
	use := langfeatures.Position{Line: 2, Col: strings.Index("export const three = add(1, 2);", "add") + 1}
	g.eventually("a hover in a JavaScript file", func() bool {
		ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
		defer cancel()
		var hs []langfeatures.Hover
		g.pumped(func() { hs, _ = reg.Hover(ctx, script, use) })
		for _, h := range hs {
			for _, c := range h.Contents {
				if strings.Contains(c.Value, "add(a: number, b: number): number") {
					return true
				}
			}
		}
		return false
	})
	g.stop()
}

func TestTypeScriptServerRunsUnderTheNodeOfTheSetting(t *testing.T) {
	node, err := exec.LookPath("node")
	if err != nil {
		t.Skip("node is not installed")
	}
	// the script is run by the node the setting names (not by the first one on PATH), and the TypeScript beside it is still found
	g := newTSRig(t, "", map[string]any{typescript.NodePathSetting: node})
	g.eventually("the diagnostics", func() bool { return hasMarker(g.markers("src/main.ts"), 4, "not assignable") })
	if text := g.itemText(); text != "$(check) TypeScript "+globalTS {
		t.Errorf("the status item = %q", text)
	}
	g.stop()
}
