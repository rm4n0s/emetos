//go:build emetos_pyright || emetos_pylsp

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
	"github.com/rm4n0s/emetos/extensions/python"
	"github.com/rm4n0s/emetos/internal/core/document"
	"github.com/rm4n0s/emetos/internal/core/langfeatures"
	"github.com/rm4n0s/emetos/internal/gui/testutil"
	"github.com/rm4n0s/emetos/internal/gui/views/problems"
	"github.com/rm4n0s/emetos/internal/gui/workbench"
)

// The tests against the real Python servers (SPEC §10.34 "Tests", step 24b.4): the Python pack runs in a window tab over a copy of the
// project of testdata/python/project and a .venv the test makes, with the programs of /var/tmp/emetos-tools (scripts/fetch-tools.sh
// python-tools pyright; the pins of scripts/tool-versions.txt) or the folder EMETOS_TOOLS. They are skipped when a program is missing.
// HOME is a temporary folder and PATH holds the system's node and python only, so that nothing is read from or written to the user's.

// pyServer describes one of the servers.
type pyServer struct {
	// name is the value of python.languageServer, program the file under the tools folder and pathKey the setting that names it.
	name, program, pathKey string
	// source is the source the diagnostics of the server carry (a part of it, in lower case).
	source string
	// typeChecker says that the server reports the type error of main.py and offers Organize Imports.
	typeChecker bool
	// organize is the command that sorts the imports.
	organize string
}

var (
	basedpyrightServer = pyServer{name: python.ServerBasedpyright, program: "venv/bin/basedpyright-langserver", pathKey: python.BasedpyrightPathSetting, source: "basedpyright", typeChecker: true, organize: "basedpyright.organizeimports"}
	pyrightServer      = pyServer{name: python.ServerPyright, program: "node/node_modules/.bin/pyright-langserver", pathKey: python.PyrightPathSetting, source: "pyright", typeChecker: true, organize: "pyright.organizeimports"}
	pylspServer        = pyServer{name: python.ServerPylsp, program: "venv/bin/pylsp", pathKey: python.PylspPathSetting, source: "pyflakes"}
)

// toolsPath is the program of the server, or skips the test.
func (s pyServer) toolsPath(t *testing.T) string {
	t.Helper()
	tools := os.Getenv("EMETOS_TOOLS")
	if tools == "" {
		tools = "/var/tmp/emetos-tools"
	}
	path := filepath.Join(tools, s.program)
	if _, err := os.Stat(path); err != nil {
		t.Skipf("%s is not installed (scripts/fetch-tools.sh python-tools pyright)", s.name)
	}
	return path
}

// pyCapture is the Python pack that keeps the API of its activation.
type pyCapture struct {
	extension.Extension
	api *extension.API
}

// Activate implements extension.Extension.
func (c pyCapture) Activate(ctx context.Context, api extension.API) error {
	*c.api = api
	return c.Extension.Activate(ctx, api)
}

// pyRig runs the Python pack and a server in a copy of the project.
type pyRig struct {
	t      *testing.T
	server pyServer
	r      *rig
	tb     *tab
	api    extension.API
	folder string
	// venvPython is the program of the .venv of the folder.
	venvPython string
	docs       map[string]*document.Document
}

// pyFiles are the files of the project that a document is opened for.
var pyFiles = []string{"main.py", "helpers.py", "unsorted.py"}

// makeVenv makes folder/.venv with the system's python3 (no pip) and a package, emetoslib, in its site-packages that exists nowhere else.
func makeVenv(t *testing.T, folder string) string {
	t.Helper()
	system, err := exec.LookPath("python3")
	if err != nil {
		t.Skip("python3 is not installed")
	}
	venv := filepath.Join(folder, ".venv")
	if out, err := exec.Command(system, "-m", "venv", "--without-pip", venv).CombinedOutput(); err != nil {
		t.Fatalf("python3 -m venv: %v\n%s", err, out)
	}
	py := filepath.Join(venv, "bin", "python")
	site, err := exec.Command(py, "-c", "import sysconfig; print(sysconfig.get_path('purelib'))").Output()
	if err != nil {
		t.Fatal(err)
	}
	pkg := filepath.Join(strings.TrimSpace(string(site)), "emetoslib")
	if err := os.MkdirAll(pkg, 0o755); err != nil {
		t.Fatal(err)
	}
	src := "def greet(name: str) -> str:\n    \"\"\"Say hello to name.\"\"\"\n    return \"hello \" + name\n"
	for name, text := range map[string]string{"__init__.py": src, "py.typed": ""} {
		if err := os.WriteFile(filepath.Join(pkg, name), []byte(text), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	return py
}

// newPyRig copies the project, makes its .venv and starts the pack in a tab over it. chosen says that the .venv is the interpreter the
// user chose (the state of the tab); the server starts as soon as a document opens.
func newPyRig(t *testing.T, s pyServer, chosen bool) *pyRig {
	t.Helper()
	program := s.toolsPath(t)
	node := ""
	if p, err := exec.LookPath("node"); err == nil {
		node = filepath.Dir(p)
	}
	t.Setenv("HOME", t.TempDir())
	t.Setenv("PATH", strings.Join([]string{node, "/usr/bin", "/bin"}, string(os.PathListSeparator)))
	for _, k := range []string{"VIRTUAL_ENV", "CONDA_PREFIX", "PYENV_ROOT"} {
		t.Setenv(k, "")
	}
	g := &pyRig{t: t, server: s, folder: t.TempDir(), docs: map[string]*document.Document{}}
	fixture := filepath.Join(testutil.RepoRoot(t), "testdata", "python", "project")
	for _, name := range append([]string{"pyproject.toml"}, pyFiles...) {
		text, err := os.ReadFile(filepath.Join(fixture, name))
		if err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(g.folder, name), text, 0o600); err != nil {
			t.Fatal(err)
		}
	}
	g.venvPython = makeVenv(t, g.folder)
	f := func() extension.Extension { return pyCapture{Extension: python.New(), api: &g.api} }
	g.r = newRig(t, f)
	g.r.folders["one"] = g.folder
	t.Cleanup(func() { g.r.host.Shutdown(); g.r.host.WaitServers() })
	g.tb = g.r.newTab("one")
	g.tb.deferred = true
	g.tb.changeSetting(python.LanguageServerSetting, s.name)
	g.tb.changeSetting(s.pathKey, program)
	if chosen {
		g.tb.keptMu.Lock()
		g.tb.kept = map[string]json.RawMessage{python.ID + "\x00" + g.folder + "\x00" + python.StateKey: json.RawMessage(strconv.Quote(g.venvPython))}
		g.tb.keptMu.Unlock()
	}
	g.tb.scope.Start()
	g.tb.pump()
	for _, name := range pyFiles {
		path := filepath.Join(g.folder, name)
		text, err := os.ReadFile(path)
		if err != nil {
			t.Fatal(err)
		}
		opts := document.NewOptions()
		opts.LanguageID = "python"
		doc := document.New(document.FileURI(path), string(text), opts)
		g.docs[name] = doc
		g.tb.lsp.open(doc)
	}
	g.tb.pump()
	// a Python file comes to the front: the status item shows, and the pack may offer the environment of the folder
	if err := g.tb.scope.PublishCore(extension.TopicEditorActiveChanged, extension.ActiveEditor{URI: g.docs["main.py"].URI().String(), LanguageID: "python"}); err != nil {
		t.Fatal(err)
	}
	g.tb.frame()
	waitFor(t, g.tb, s.name+" to run", func() bool { return g.status().State == ServerRunning })
	return g
}

// status is the state of the server.
func (g *pyRig) status() ServerStatus {
	s, _ := g.tb.scope.ServerStatus(python.ID, python.ServerID)
	return s
}

// output is what the Output channel of the server holds.
func (g *pyRig) output() string { return strings.Join(g.tb.channels.Lines("Python"), "\n") }

// langDoc is the document of the file as the registry of the language features wants it.
func (g *pyRig) langDoc(name string) langfeatures.Document {
	d := g.docs[name]
	return langfeatures.Document{URI: d.URI(), LanguageID: "python", Version: d.Version(), Text: d.Snapshot()}
}

// pumped runs f on a goroutine while the tab's UI thread is driven, as the servers read other files through it.
func (g *pyRig) pumped(f func()) {
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

// pyWait is how long a test waits for a server to analyze the project and answer.
var pyWait = 90 * time.Second

// eventually runs check until it holds, for as long as the server needs.
func (g *pyRig) eventually(what string, check func() bool) {
	g.t.Helper()
	deadline := time.Now().Add(pyWait)
	for time.Now().Before(deadline) {
		if check() {
			return
		}
		g.tb.pump()
		time.Sleep(200 * time.Millisecond)
	}
	g.t.Fatalf("%s did not give %s; Output:\n%s", g.server.name, what, g.output())
}

// stop closes the tab and waits for the server to end.
func (g *pyRig) stop() {
	pid := g.status().PID
	g.tb.scope.Close()
	g.r.host.WaitServers()
	requireGone(g.t, g.server.name, pid)
}

// markers are the problems the server reported for a file.
func (g *pyRig) markers(name string) []problems.Marker {
	return g.tb.markers.Read(problems.ReadFilter{Owner: python.ID + "." + python.ServerID, Resource: filepath.Join(g.folder, name)})
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
func (g *pyRig) itemText() string {
	for _, a := range []workbench.StatusAlignment{workbench.StatusLeft, workbench.StatusRight} {
		for _, it := range g.tb.bar.Items(a) {
			if strings.HasSuffix(it.ID, "status."+python.ServerID) {
				return it.Entry.Text
			}
		}
	}
	return ""
}

// checkTheFeatures runs what every server answers in the project with the .venv chosen: the diagnostics, the hover and the completion
// from the installed package, the definition, the rename across two files and the item of the status bar.
func checkTheFeatures(t *testing.T, s pyServer) {
	g := newPyRig(t, s, true)
	reg := g.tb.scope.Features()
	main := g.langDoc("main.py")
	lines := strings.Split(g.docs["main.py"].Text(), "\n")
	greetUse := langfeatures.Position{Line: 5, Col: strings.Index(lines[5], "greet") + 2}
	addUse := langfeatures.Position{Line: 3, Col: strings.Index(lines[3], "add") + 1}

	// diagnostics: the undefined name from every server, the type error from the type checkers, and no unresolved import, because the
	// server runs the interpreter of the .venv, where emetoslib is installed
	var got []problems.Marker
	g.eventually("the diagnostic of the undefined name", func() bool {
		got = g.markers("main.py")
		return hasMarker(got, 5, "missing_name")
	})
	for _, m := range got {
		t.Logf("%v %d:%d %q source=%q code=%q", m.Severity, m.StartLine, m.StartColumn, m.Message, m.Source, m.Code)
		if !strings.Contains(strings.ToLower(m.Source), s.source) {
			t.Errorf("the source of %q is %q, want one with %q", m.Message, m.Source, s.source)
		}
		if strings.Contains(m.Message, "emetoslib") {
			t.Errorf("the package of the .venv is not resolved: %q", m.Message)
		}
	}
	if s.typeChecker && !hasMarker(got, 4, "str") {
		t.Errorf("no type error on line 5: %+v", got)
	}

	// hover on greet: the signature and the docstring of the function of the package
	g.eventually("a hover with the signature and the docstring", func() bool {
		ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
		defer cancel()
		var hs []langfeatures.Hover
		g.pumped(func() { hs, _ = reg.Hover(ctx, main, greetUse) })
		for _, h := range hs {
			for _, c := range h.Contents {
				if strings.Contains(c.Value, "greet(") && strings.Contains(c.Value, "str") && strings.Contains(c.Value, "Say hello") {
					return true
				}
			}
		}
		return false
	})

	// completion in the import of the package: the function, which only the interpreter's packages have
	at := langfeatures.Position{Line: 0, Col: len("from emetoslib import gre")}
	g.eventually("completion with greet", func() bool {
		ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
		defer cancel()
		var list langfeatures.CompletionList
		g.pumped(func() {
			list, _ = reg.Completion(ctx, main, at, langfeatures.CompletionTrigger{Kind: langfeatures.TriggerInvoked})
		})
		for _, it := range list.Items {
			if strings.HasPrefix(strings.TrimSpace(it.Label), "greet") { // python-lsp-server writes greet(name)
				return true
			}
		}
		return false
	})

	// definition: add is defined in helpers.py
	var links []langfeatures.LocationLink
	g.eventually("the definition of add", func() bool {
		ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
		defer cancel()
		g.pumped(func() { links, _ = reg.Locations(ctx, main, addUse, langfeatures.NavDefinition) })
		return len(links) >= 1
	})
	if p := links[0].Target.URI.Path; !strings.HasSuffix(p, "/helpers.py") {
		t.Errorf("definition = %+v", links)
	}

	// the status item shows the interpreter of the .venv
	if text := g.itemText(); !strings.Contains(text, "('.venv')") || !strings.HasPrefix(text, "$(check) Python 3.") {
		t.Errorf("the status item = %q", text)
	}

	// rename: add and its use in main.py change in the two files
	g.eventually("the rename of add", func() bool {
		var we *langfeatures.WorkspaceEdit
		var err error
		g.pumped(func() { we, err = reg.Rename(context.Background(), main, addUse, "plus") })
		if err != nil || we == nil {
			return false
		}
		if _, err := langfeatures.Apply(context.Background(), g.tb.lsp.edits, we); err != nil {
			t.Fatalf("apply: %v", err)
		}
		g.tb.pump()
		return true
	})
	if text := g.docs["helpers.py"].Text(); !strings.Contains(text, "def plus(") || strings.Contains(text, "add") && !strings.Contains(text, "Add two") {
		t.Errorf("helpers.py after the rename:\n%s", text)
	}
	if text := g.docs["main.py"].Text(); strings.Contains(text, "add") || strings.Count(text, "plus") != 3 {
		t.Errorf("main.py after the rename:\n%s", text)
	}
	g.stop()
}

// checkOrganizeImports asks the server to sort the imports of unsorted.py, as the command Organize Imports of the pack does (the server's
// own command, which answers with workspace/applyEdit), and checks the text.
func checkOrganizeImports(t *testing.T, s pyServer) {
	g := newPyRig(t, s, true)
	doc := g.docs["unsorted.py"]
	var done bool
	var told error
	if err := g.api.LanguageServers().ExecuteCommand(context.Background(), python.ServerID, s.organize, []any{doc.URI().String()}, func(err error) { done, told = true, err }); err != nil {
		t.Fatal(err)
	}
	waitFor(t, g.tb, "the command", func() bool { return done })
	if told != nil {
		t.Fatalf("%s: %v", s.name, told)
	}
	g.eventually("the imports to be sorted", func() bool { return strings.HasPrefix(doc.Text(), "import os\nimport sys\n") })
	if !doc.CanUndo() {
		t.Error("nothing to undo")
	}
	g.stop()
}

// checkTheInterpreterDecidesTheImports starts the server with no interpreter chosen, where emetoslib cannot be found, then chooses the
// .venv with the command of the pack: the server is restarted with it and the import is resolved.
func checkTheInterpreterDecidesTheImports(t *testing.T, s pyServer) {
	g := newPyRig(t, s, false)
	g.eventually("the unresolved import", func() bool { return hasMarker(g.markers("main.py"), 0, "emetoslib") })
	if err := g.tb.commands.Run(commandContext(), python.SetInterpreterCommand, g.venvPython); err != nil {
		t.Fatal(err)
	}
	g.tb.pump()
	waitFor(t, g.tb, "the server to start again", func() bool { return g.status().Starts >= 2 && g.status().State == ServerRunning })
	g.eventually("the import to be resolved", func() bool {
		list := g.markers("main.py")
		return hasMarker(list, 5, "missing_name") && !hasMarker(list, 0, "emetoslib")
	})
	if text := g.itemText(); !strings.Contains(text, "('.venv')") {
		t.Errorf("the status item = %q", text)
	}
	g.stop()
}
