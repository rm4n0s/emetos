//go:build emetos_jdtls

package exthost

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/rm4n0s/emetos/extension"
	javapack "github.com/rm4n0s/emetos/extensions/java"
	"github.com/rm4n0s/emetos/internal/core/document"
	"github.com/rm4n0s/emetos/internal/core/langfeatures"
	"github.com/rm4n0s/emetos/internal/core/lsp"
	"github.com/rm4n0s/emetos/internal/gui/testutil"
	"github.com/rm4n0s/emetos/internal/gui/views/problems"
	"github.com/rm4n0s/emetos/internal/gui/workbench"
)

// The tests against the real Eclipse JDT LS (SPEC §10.34 "Tests", step 24b.6): the Java pack runs in a window tab over a copy of the
// project of testdata/java/plain (sources and no build file: jdtls cannot import a Maven or Gradle project offline, see testdata/java/README.txt),
// with the jdtls of /var/tmp/emetos-tools/jdtls (scripts/fetch-tools.sh jdtls; the pin of
// scripts/tool-versions.txt) or the folder EMETOS_JDTLS, run by the system's Java (21 or newer). They are skipped when jdtls or Java is
// missing. HOME and XDG_CACHE_HOME are temporary folders, so that the workspace of the server, the Maven repository and everything else
// jdtls writes stay in them, and nothing uses the network. The JVM is capped at 1 GB (its default).

// jdtlsFolder finds the jdtls the tests run, or skips the test.
func jdtlsFolder(t *testing.T) string {
	t.Helper()
	dir := os.Getenv("EMETOS_JDTLS")
	if dir == "" {
		dir = "/var/tmp/emetos-tools/jdtls"
	}
	if _, err := os.Stat(filepath.Join(dir, "plugins")); err != nil {
		t.Skip("jdtls is not installed (scripts/fetch-tools.sh jdtls)")
	}
	if _, err := exec.LookPath("java"); err != nil {
		t.Skip("java is not installed")
	}
	return dir
}

// javaCapture is the Java pack that keeps the API of its activation.
type javaCapture struct {
	extension.Extension
	api *extension.API
}

// Activate implements extension.Extension.
func (c javaCapture) Activate(ctx context.Context, api extension.API) error {
	*c.api = api
	return c.Extension.Activate(ctx, api)
}

// javaRig runs the pack and the server in a copy of the project.
type javaRig struct {
	t      *testing.T
	r      *rig
	tb     *tab
	api    extension.API
	folder string
	cache  string
	docs   map[string]*document.Document
}

// javaFiles are the files of the project that a document is opened for.
var javaFiles = []string{"src/main/java/dev/emetos/app/App.java", "src/main/java/dev/emetos/app/Greeter.java"}

const javaApp = "src/main/java/dev/emetos/app/App.java"

// copyProject copies a project of testdata/java into the folder.
func copyProject(t *testing.T, name, folder string) {
	t.Helper()
	root := filepath.Join(testutil.RepoRoot(t), "testdata", "java", name)
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

// newJavaRig copies the Maven project and starts the pack in a tab over it.
func newJavaRig(t *testing.T, extraSettings map[string]any) *javaRig {
	t.Helper()
	jdtls := jdtlsFolder(t)
	java, _ := exec.LookPath("java")
	t.Setenv("HOME", t.TempDir())
	cache := t.TempDir()
	t.Setenv("XDG_CACHE_HOME", cache)
	t.Setenv("JAVA_HOME", "")
	t.Setenv("PATH", strings.Join([]string{filepath.Dir(java), "/usr/bin", "/bin"}, string(os.PathListSeparator)))
	g := &javaRig{t: t, folder: t.TempDir(), cache: cache, docs: map[string]*document.Document{}}
	copyProject(t, "plain", g.folder)

	f := func() extension.Extension { return javaCapture{Extension: javapack.New(), api: &g.api} }
	g.r = newRig(t, f)
	g.r.folders["one"] = g.folder
	t.Cleanup(func() { g.r.host.Shutdown(); g.r.host.WaitServers() })
	g.tb = g.r.newTab("one")
	g.tb.deferred = true
	g.tb.changeSetting(javapack.PathSetting, jdtls)
	if trace := os.Getenv("EMETOS_JDTLS_TRACE"); trace != "" {
		g.tb.changeSetting(javapack.TraceSetting, trace)
	}
	for k, v := range extraSettings {
		g.tb.changeSetting(k, v)
	}
	g.tb.scope.Start()
	g.tb.pump()
	// the file in front is known before the server starts: it tells jdtls where the sources of a folder with no build file start
	if err := g.tb.scope.PublishCore(extension.TopicEditorActiveChanged, extension.ActiveEditor{URI: document.FileURI(filepath.Join(g.folder, filepath.FromSlash(javaApp))).String(), LanguageID: "java"}); err != nil {
		t.Fatal(err)
	}
	g.tb.pump()
	for _, name := range javaFiles {
		path := filepath.Join(g.folder, filepath.FromSlash(name))
		text, err := os.ReadFile(path)
		if err != nil {
			t.Fatal(err)
		}
		opts := document.NewOptions()
		opts.LanguageID = "java"
		doc := document.New(document.FileURI(path), string(text), opts)
		g.docs[name] = doc
		g.tb.lsp.open(doc)
	}
	g.tb.pump()
	g.tb.frame()
	waitFor(t, g.tb, "jdtls to run", func() bool { return g.status().State == ServerRunning })
	return g
}

// status is the state of the server.
func (g *javaRig) status() ServerStatus {
	s, _ := g.tb.scope.ServerStatus(javapack.ID, javapack.ServerID)
	return s
}

// output is what the Output channel of the server holds.
func (g *javaRig) output() string { return strings.Join(g.tb.channels.Lines("Java"), "\n") }

// langDoc is the document of the file as the registry of the language features wants it.
func (g *javaRig) langDoc(name string) langfeatures.Document {
	d := g.docs[name]
	return langfeatures.Document{URI: d.URI(), LanguageID: "java", Version: d.Version(), Text: d.Snapshot()}
}

// pumped runs f on a goroutine while the tab's UI thread is driven, as the servers read other files through it.
func (g *javaRig) pumped(f func()) {
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

// jdtlsWait is how long a test waits for the server to import the project and answer.
var jdtlsWait = 180 * time.Second

func init() {
	if v := os.Getenv("EMETOS_JDTLS_WAIT"); v != "" {
		if d, err := time.ParseDuration(v); err == nil {
			jdtlsWait = d
		}
	}
}

// eventually runs check until it holds, for as long as the server needs.
func (g *javaRig) eventually(what string, check func() bool) {
	g.t.Helper()
	deadline := time.Now().Add(jdtlsWait)
	for time.Now().Before(deadline) {
		if check() {
			return
		}
		g.tb.pump()
		time.Sleep(250 * time.Millisecond)
	}
	g.t.Fatalf("jdtls did not give %s; Output:\n%s", what, g.output())
}

// stop closes the tab and waits for the server to end.
func (g *javaRig) stop() {
	pid := g.status().PID
	g.tb.scope.Close()
	g.r.host.WaitServers()
	requireGone(g.t, "jdtls", pid)
}

// request sends a request of the server's own protocol while the tab is driven.
func (g *javaRig) request(method string, params any) (result json.RawMessage, err error) {
	g.pumped(func() {
		ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
		defer cancel()
		result, err = g.api.LanguageServers().Request(ctx, javapack.ServerID, method, params)
	})
	return result, err
}

// markers are the problems the server reported for a file.
func (g *javaRig) markers(name string) []problems.Marker {
	return g.tb.markers.Read(problems.ReadFilter{Owner: javapack.ID + "." + javapack.ServerID, Resource: filepath.Join(g.folder, filepath.FromSlash(name))})
}

// itemText and itemTooltip are the text and the tooltip of the status item of the server.
func (g *javaRig) itemText() string    { return g.item().Text }
func (g *javaRig) itemTooltip() string { return g.item().Tooltip }

// item is the status item of the server.
func (g *javaRig) item() workbench.StatusEntry {
	for _, a := range []workbench.StatusAlignment{workbench.StatusLeft, workbench.StatusRight} {
		for _, it := range g.tb.bar.Items(a) {
			if strings.HasSuffix(it.ID, "status."+javapack.ServerID) {
				return it.Entry
			}
		}
	}
	return workbench.StatusEntry{}
}

// javaGreeter is the second file of the project.
const javaGreeter = "src/main/java/dev/emetos/app/Greeter.java"

// hasJavaMarker tells whether a problem on the line (1-based) has the text in its message.
func hasJavaMarker(list []problems.Marker, line int, text string) bool {
	for _, m := range list {
		if m.StartLine == line && strings.Contains(m.Message, text) {
			return true
		}
	}
	return false
}

// ready waits for the server to say ServiceReady and for the project to be built, which is when the status item is the check.
func (g *javaRig) ready() {
	g.eventually("the status item to show the check", func() bool { return g.itemText() == "$(check) Java" })
}

func TestJdtlsAnswersDiagnosticsHoverCompletionDefinitionAndRename(t *testing.T) {
	g := newJavaRig(t, nil)
	reg := g.tb.scope.Features()
	app := g.langDoc(javaApp)
	lines := strings.Split(g.docs[javaApp].Text(), "\n")

	// the problems: the type error and the unknown name from the compiler of Eclipse (the unused import is not one: its severity is ignore)
	var got []problems.Marker
	g.eventually("the diagnostics", func() bool {
		got = g.markers(javaApp)
		return hasJavaMarker(got, 11, "Type mismatch: cannot convert from String to int") && hasJavaMarker(got, 12, "missing cannot be resolved to a variable")
	})
	for _, m := range got {
		if m.Source != "Java" {
			t.Errorf("the source of %q is %q", m.Message, m.Source)
		}
	}
	g.ready()
	if text := g.itemText(); text != "$(check) Java" {
		t.Errorf("the status item = %q", text)
	}
	if tip := g.itemTooltip(); !strings.Contains(tip, "jdtls 1.63.0: "+jdtlsFolder(t)) {
		t.Errorf("the tooltip = %q", tip)
	}

	// hover on greet: the signature and the documentation of the method
	var lastHover string
	greetUse := langfeatures.Position{Line: 9, Col: strings.Index(lines[9], "greet") + 2}
	g.eventually("a hover with the signature and the documentation", func() bool {
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()
		var hs []langfeatures.Hover
		g.pumped(func() { hs, _ = reg.Hover(ctx, app, greetUse) })
		var signature, documentation bool
		var seen []string
		for _, h := range hs {
			for _, c := range h.Contents {
				seen = append(seen, c.Value)
				signature = signature || strings.Contains(c.Value, "String dev.emetos.app.Greeter.greet(String name)")
				documentation = documentation || strings.Contains(c.Value, "Says hello to name.")
			}
		}
		if signature && documentation {
			return true
		}
		if len(seen) > 0 && strings.Join(seen, "|") != lastHover {
			lastHover = strings.Join(seen, "|")
			t.Logf("hover: %q", lastHover)
		}
		return false
	})

	// completion after "Greeter." offers greet
	at := langfeatures.Position{Line: 9, Col: strings.Index(lines[9], "greet") + 2} // after "gr"
	var lastLabels string
	g.eventually("completion with greet", func() bool {
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()
		var list langfeatures.CompletionList
		var err []langfeatures.ProviderError
		g.pumped(func() {
			list, err = reg.Completion(ctx, app, at, langfeatures.CompletionTrigger{Kind: langfeatures.TriggerInvoked})
		})
		var labels []string
		for _, it := range list.Items {
			labels = append(labels, strings.TrimSpace(it.Label))
			if strings.HasPrefix(strings.TrimSpace(it.Label), "greet") {
				return true
			}
		}
		if text := fmt.Sprint(err, labels); text != lastLabels {
			lastLabels = text
			t.Logf("completion: %d items, error %v, labels %q", len(list.Items), err, labels)
		}
		return false
	})

	// definition: Greeter is a class of the project, ArrayList one of the JDK
	var links []langfeatures.LocationLink
	g.eventually("the definition of Greeter", func() bool {
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()
		g.pumped(func() {
			links, _ = reg.Locations(ctx, app, langfeatures.Position{Line: 9, Col: strings.Index(lines[9], "Greeter") + 1}, langfeatures.NavDefinition)
		})
		return len(links) >= 1
	})
	if p := links[0].Target.URI.Path; !strings.HasSuffix(p, "/dev/emetos/app/Greeter.java") {
		t.Errorf("definition of Greeter = %+v", links)
	}
	g.eventually("the definition of ArrayList in the JDK", func() bool {
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()
		g.pumped(func() {
			links, _ = reg.Locations(ctx, app, langfeatures.Position{Line: 8, Col: strings.Index(lines[8], "ArrayList") + 2}, langfeatures.NavDefinition)
		})
		return len(links) >= 1
	})
	target := links[0].Target.URI
	// the server gives the source of the class when it has it (src.zip of the JDK), and the class file otherwise
	if target.Scheme != document.SchemeVirtual || !strings.HasPrefix(target.Path, "/jdt/contents/java.base/java.util/ArrayList.") {
		t.Fatalf("definition of ArrayList = %+v", links)
	}
	// the provider of the scheme jdt asks the server for the text of that class, with the address as the server gave it
	serverURI := lsp.DocumentURI(target)
	if !strings.HasPrefix(serverURI, "jdt://contents/java.base/java.util/ArrayList.") {
		t.Errorf("the server's address = %q", serverURI)
	}
	var content string
	g.eventually("the text of ArrayList", func() bool {
		result, err := g.request("java/classFileContents", map[string]any{"uri": serverURI})
		return err == nil && json.Unmarshal(result, &content) == nil && strings.Contains(content, "class ArrayList")
	})

	// rename greet at its declaration: the declaration and its use change in both files
	greeter := g.langDoc(javaGreeter)
	gl := strings.Split(g.docs[javaGreeter].Text(), "\n")
	declLine := slices.IndexFunc(gl, func(l string) bool { return strings.Contains(l, "String greet(") })
	if declLine < 0 {
		t.Fatalf("no declaration of greet in\n%s", g.docs[javaGreeter].Text())
	}
	declaration := langfeatures.Position{Line: declLine, Col: strings.Index(gl[declLine], "greet") + 1}
	g.eventually("the rename of greet", func() bool {
		var we *langfeatures.WorkspaceEdit
		var err error
		g.pumped(func() { we, err = reg.Rename(context.Background(), greeter, declaration, "salute") })
		if err != nil || we == nil {
			return false
		}
		if _, err := langfeatures.Apply(context.Background(), g.tb.lsp.edits, we); err != nil {
			t.Fatalf("apply: %v", err)
		}
		g.tb.pump()
		return true
	})
	if text := g.docs[javaGreeter].Text(); !strings.Contains(text, "String salute(String name)") || strings.Contains(text, "greet(") {
		t.Errorf("Greeter.java after the rename:\n%s", text)
	}
	if text := g.docs[javaApp].Text(); !strings.Contains(text, "Greeter.salute(") || strings.Contains(text, "greet(") {
		t.Errorf("App.java after the rename:\n%s", text)
	}
	g.stop()
}

func TestJdtlsOrganizesImportsAndAnswersTheProtocolOfTheCommands(t *testing.T) {
	g := newJavaRig(t, nil)
	reg := g.tb.scope.Features()
	g.eventually("the diagnostics", func() bool { return hasJavaMarker(g.markers(javaApp), 11, "Type mismatch") })
	g.ready()
	doc := g.docs[javaApp]
	// Shift+Alt+O is enabled by the kinds the server announces
	g.eventually("the kind source.organizeImports", func() bool { return reg.SupportsCodeActionKind(g.langDoc(javaApp), langfeatures.KindSourceOrganize) })

	// Organize Imports of the pack asks for exactly this: the unused import goes and the others are sorted
	var title string
	var told error
	done := false
	err := g.api.LanguageServers().ApplyCodeAction(context.Background(), javapack.ServerID, doc.URI().String(),
		extension.CodeActionRequest{Only: []string{"source.organizeImports"}}, func(t string, err error) { title, told, done = t, err, true })
	if err != nil {
		t.Fatal(err)
	}
	waitFor(t, g.tb, "the code action", func() bool { return done })
	if told != nil || title == "" {
		t.Fatalf("title %q, error %v", title, told)
	}
	var lastText string
	g.eventually("the imports to be organized", func() bool {
		if text := doc.Text(); text != lastText {
			lastText = text
			t.Logf("App.java is now:\n%s", text)
		}
		return strings.HasPrefix(doc.Text(), "package dev.emetos.app;\n\nimport java.util.ArrayList;\nimport java.util.List;\n\npublic class App")
	})
	if !doc.CanUndo() {
		t.Error("nothing to undo")
	}

	// Update Project Configuration sends this for the file in front: jdtls declares it as a notification, so it is sent as one, and the
	// server goes on answering (the build below)
	if err := g.api.LanguageServers().Notify(javapack.ServerID, "java/projectConfigurationUpdate", map[string]any{"uri": uriOf(filepath.Join(g.folder, filepath.FromSlash(javaApp)))}); err != nil {
		t.Errorf("java/projectConfigurationUpdate: %v", err)
	}
	// Force Java Compilation sends this: the status of the build is a number (0 failed, 1 succeeded, 2 with errors, 3 cancelled)
	var status int
	g.eventually("the build of the workspace", func() bool {
		result, err := g.request("java/buildWorkspace", []any{false})
		return err == nil && json.Unmarshal(result, &status) == nil
	})
	if status < 0 || status > 3 {
		t.Errorf("build status = %d", status)
	}
	g.stop()
}
