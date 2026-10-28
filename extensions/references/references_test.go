package references

import (
	"context"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/rm4n0s/errors"

	"github.com/rm4n0s/emetos/extension"
	"github.com/rm4n0s/emetos/extensions/internal/fakeapi"
)

// project is the folder the tests ask about: Greet is declared in util.go and used in main.go and internal/cli/cli.go.
var project = map[string]string{
	"main.go":              "package main\n\nimport \"fmt\"\n\nfunc main() {\n\tfmt.Println(Greet(\"world\"))\n\tfmt.Println(Greet(\"again\"))\n}\n",
	"util.go":              "package main\n\n// Greet says hello.\nfunc Greet(name string) string {\n\treturn \"hello \" + name\n}\n",
	"internal/cli/cli.go":  "package cli\n\nfunc Run() { _ = Greet(\"cli\") }\n",
	"README.md":            "# project\n",
	"internal/cli/doc.txt": "Greet appears here too.\n",
}

// rig is the extension activated over the fake API, in a folder with the files of project.
type rig struct {
	t      *testing.T
	api    *fakeapi.API
	ext    *references
	folder string
}

// writeProject writes the files into a new folder.
func writeProject(t *testing.T, files map[string]string) string {
	t.Helper()
	dir := t.TempDir()
	for name, text := range files {
		full := filepath.Join(dir, filepath.FromSlash(name))
		if err := os.MkdirAll(filepath.Dir(full), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(full, []byte(text), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	return dir
}

// newRig activates the extension over a folder.
func newRig(t *testing.T) *rig {
	t.Helper()
	folder := writeProject(t, project)
	e := New()
	api := fakeapi.New(t, e.Manifest(), extension.ScopeInfo{Kind: extension.ScopeWorkspace, Name: "project", Folder: folder, ID: folder})
	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)
	if err := e.Activate(ctx, api); err != nil {
		t.Fatal(err)
	}
	return &rig{t: t, api: api, ext: e.(*references), folder: folder}
}

// uri is the URI of a file of the project.
func (r *rig) uri(name string) string {
	return "file://" + filepath.Join(r.folder, filepath.FromSlash(name))
}

// open makes a file of the project an open document, with its text, and the active editor with the cursor at the line and
// the column of the first occurrence of word after it.
func (r *rig) cursorAt(name string, line int, word string) extension.Position {
	r.t.Helper()
	text := project[name]
	lines := strings.Split(text, "\n")
	col := strings.Index(lines[line], word)
	if col < 0 {
		r.t.Fatalf("%q is not in line %d of %s", word, line, name)
	}
	pos := extension.Position{Line: line, Col: col}
	r.api.SetDocument(r.uri(name), text)
	r.api.SetActive(&extension.ActiveDocument{URI: r.uri(name), LanguageID: "go", Version: 1, Selections: []extension.Selection{{Anchor: pos, Active: pos}}})
	return pos
}

// place is the location of a word on a line of a file of the project.
func (r *rig) place(name string, line int, word string) extension.Location {
	r.t.Helper()
	lines := strings.Split(project[name], "\n")
	col := strings.Index(lines[line], word)
	if col < 0 {
		r.t.Fatalf("%q is not in line %d of %s", word, line, name)
	}
	return extension.Location{URI: r.uri(name), Range: extension.Range{
		Start: extension.Position{Line: line, Col: col}, End: extension.Position{Line: line, Col: col + len(word)},
	}}
}

// greetings are the places the language server finds for Greet, in no order.
func (r *rig) greetings() []extension.Location {
	return []extension.Location{
		r.place("main.go", 6, "Greet"), r.place("util.go", 3, "Greet"), r.place("main.go", 5, "Greet"), r.place("internal/cli/cli.go", 2, "Greet"),
	}
}

// answer makes the language server answer a feature with the locations; it also keeps the positions it was asked about.
func (r *rig) answer(feature string, locs []extension.Location) {
	r.api.AnswerLocations(feature, func(string, extension.Position) ([]extension.Location, error) { return locs, nil })
}

// run runs a command and waits until the search it started is over.
func (r *rig) run(id string, args any) {
	r.t.Helper()
	if err := r.api.Run(id, args); err != nil {
		r.t.Fatalf("%s: %v", id, err)
	}
	r.api.WaitFor("the search to end", func() bool { return !r.ext.view.loading })
}

// rows are the rows of the tree as "depth key" lines.
func (r *rig) rows() []string {
	var out []string
	for _, n := range r.ext.view.tree.Rows() {
		out = append(out, strings.Repeat("  ", n.Depth)+n.Key)
	}
	return out
}

// loaded waits until the lines of the files the tree shows are read.
func (r *rig) loaded() {
	r.t.Helper()
	r.api.WaitFor("the files to be read", func() bool {
		for _, f := range r.ext.view.mod.files {
			if lines, ok := r.ext.view.ensureText(f.uri); !ok || lines == nil {
				return false
			}
		}
		return true
	})
}

func TestTheManifestIsValid(t *testing.T) {
	m := New().Manifest()
	if err := extension.CheckManifest(m); err != nil {
		t.Fatal(err)
	}
	// every command a handler is made for is declared, and the other way round
	r := newRig(t)
	for _, c := range m.Contributes.Commands {
		if !r.api.HasHandler(c.ID) {
			t.Errorf("the command %s has no handler", c.ID)
		}
	}
	if !r.api.HasHandler(Find) {
		t.Error("the legacy command references-view.find has no handler")
	}
}

func TestTheContributionsAreVSCodes(t *testing.T) {
	m := New().Manifest()
	titles := map[string]string{}
	for _, c := range m.Contributes.Commands {
		titles[c.ID] = c.Category + "|" + c.Title
	}
	for id, want := range map[string]string{
		"references-view.findReferences":      "References|Find All References",
		"references-view.findImplementations": "References|Find All Implementations",
		"references-view.clearHistory":        "References|Clear History",
		"references-view.clear":               "References|Clear",
		"references-view.refresh":             "References|Refresh",
		"references-view.pickFromHistory":     "References|Show History",
		"references-view.removeReferenceItem": "|Dismiss",
		"references-view.copy":                "|Copy",
		"references-view.copyAll":             "|Copy All",
		"references-view.copyPath":            "|Copy Path",
		"references-view.refind":              "|Rerun",
		"references-view.next":                "|Go to Next Reference",
		"references-view.prev":                "|Go to Previous Reference",
	} {
		if titles[id] != want {
			t.Errorf("%s is %q, want %q", id, titles[id], want)
		}
	}
	type menu struct {
		command, when, group string
		order                float64
	}
	var editor []menu
	for _, it := range m.Contributes.Menus {
		if it.Menu == extension.MenuEditorContext {
			editor = append(editor, menu{it.Command, it.When, it.Group, it.Order})
		}
	}
	want := []menu{
		{"references-view.findReferences", "editorHasReferenceProvider", "0_navigation", 1},
		{"references-view.findImplementations", "editorHasImplementationProvider", "0_navigation", 2},
	}
	if !slices.Equal(editor, want) {
		t.Errorf("the editor context menu = %+v, want %+v", editor, want)
	}
	keys := map[string]string{}
	for _, k := range m.Contributes.Keybindings {
		keys[k.Command] = k.Key + " when " + k.When
	}
	for id, want := range map[string]string{
		"references-view.findReferences": "shift+alt+f12 when editorHasReferenceProvider",
		"references-view.next":           "f4 when emetos.references.hasResult",
		"references-view.prev":           "shift+f4 when emetos.references.hasResult",
	} {
		if keys[id] != want {
			t.Errorf("the key of %s is %q, want %q", id, keys[id], want)
		}
	}
	if len(m.Contributes.ViewContainers) != 1 || m.Contributes.ViewContainers[0].Icon != "references" || m.Contributes.ViewContainers[0].Title != "References" {
		t.Errorf("containers = %+v", m.Contributes.ViewContainers)
	}
}

func TestFindAllReferencesFillsTheView(t *testing.T) {
	r := newRig(t)
	r.cursorAt("util.go", 3, "Greet")
	r.answer("references", r.greetings())
	r.run(FindReferences, nil)

	reqs := r.api.LocationRequests()
	if len(reqs) != 1 || reqs[0].Feature != "references" || reqs[0].URI != r.uri("util.go") || reqs[0].Position != (extension.Position{Line: 3, Col: 5}) {
		t.Fatalf("requests = %+v", reqs)
	}
	v := r.api.View(ViewID)
	if v.Title != titleReferences {
		t.Errorf("the title of the view is %q", v.Title)
	}
	if len(v.Reveals) != 1 || !v.Reveals[0] {
		t.Errorf("the view was revealed with %v, want once with the focus", v.Reveals)
	}
	if got := r.ext.view.message; got != "4 results in 3 files" {
		t.Errorf("message = %q", got)
	}
	want := []string{
		"f:" + r.uri("internal/cli/cli.go"), "  r:" + r.uri("internal/cli/cli.go") + "@2:17-2:22",
		"f:" + r.uri("main.go"), "  r:" + r.uri("main.go") + "@5:13-5:18", "  r:" + r.uri("main.go") + "@6:13-6:18",
		"f:" + r.uri("util.go"), "  r:" + r.uri("util.go") + "@3:5-3:10",
	}
	// the file of the request is open and so is the one that is nearest; the others are folded
	r.ext.view.tree.ExpandAll()
	if got := r.rows(); !slices.Equal(got, want) {
		t.Errorf("rows = %q\nwant   %q", got, want)
	}
	for key, want := range map[string]any{keyActive: true, keyHasResult: true, keyHasHistory: true, keySource: "references", keyCanNavigate: true} {
		if got := r.api.ContextKey(key); got != want {
			t.Errorf("context key %s = %v, want %v", key, got, want)
		}
	}
	// the location of the declaration is selected, since it holds the cursor
	if key, ok := r.ext.view.tree.Selected(); !ok || key != "r:"+r.uri("util.go")+"@3:5-3:10" {
		t.Errorf("selected = %q %v", key, ok)
	}
}

func TestTheLabelsMarkTheSymbol(t *testing.T) {
	r := newRig(t)
	r.cursorAt("util.go", 3, "Greet")
	r.answer("references", r.greetings())
	r.run(FindReferences, nil)
	r.loaded()
	main := r.ext.view.mod.files[1]
	label, hl := r.ext.view.labelOf(main.refs[0])
	// the line is "\tfmt.Println(Greet(\"world\"))": the text goes back 8 characters, to the start of the word it lands in
	if label != "Println(Greet(\"world\"))" {
		t.Errorf("label = %q", label)
	}
	if len(hl) != 1 || label[hl[0].Start:hl[0].End] != "Greet" {
		t.Errorf("highlights = %+v of %q", hl, label)
	}
	// a file that was never read has no label yet
	if l, h := r.ext.view.labelOf(&refItem{loc: extension.Location{URI: "file:///no/such/file.go"}}); l != "" || h != nil {
		t.Errorf("label of an unread file = %q %v", l, h)
	}
}

func TestFindAllImplementations(t *testing.T) {
	r := newRig(t)
	r.cursorAt("util.go", 3, "Greet")
	r.answer("implementations", []extension.Location{r.place("util.go", 3, "Greet")})
	r.run(FindImplementations, nil)
	if reqs := r.api.LocationRequests(); len(reqs) != 1 || reqs[0].Feature != "implementations" {
		t.Fatalf("requests = %+v", reqs)
	}
	if v := r.api.View(ViewID); v.Title != titleImplementations {
		t.Errorf("the title of the view is %q", v.Title)
	}
	if got := r.ext.view.message; got != "1 result in 1 file" {
		t.Errorf("message = %q", got)
	}
	if got := r.api.ContextKey(keySource); got != "implementations" {
		t.Errorf("source = %v", got)
	}
}

func TestASearchThatFindsNothing(t *testing.T) {
	tests := []struct {
		name   string
		answer func(string, extension.Position) ([]extension.Location, error)
		kept   bool
	}{
		{"an empty answer", func(string, extension.Position) ([]extension.Location, error) { return nil, nil }, false},
		{"an error", func(string, extension.Position) ([]extension.Location, error) {
			return nil, errors.New("ServerBroken", "the server failed")
		}, false},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			r := newRig(t)
			r.cursorAt("util.go", 3, "Greet")
			r.api.AnswerLocations("references", tc.answer)
			r.run(FindReferences, nil)
			if got := r.ext.view.message; got != "No results." {
				t.Errorf("message = %q", got)
			}
			if got := r.api.View(ViewID).Title; got != titleReferences {
				t.Errorf("title = %q", got)
			}
			if r.api.ContextKey(keyHasResult) != false || r.api.ContextKey(keyCanNavigate) != false || r.api.ContextKey(keyHasHistory) != false {
				t.Errorf("keys = %v %v %v", r.api.ContextKey(keyHasResult), r.api.ContextKey(keyCanNavigate), r.api.ContextKey(keyHasHistory))
			}
			if got := r.rows(); len(got) != 0 {
				t.Errorf("rows = %q", got)
			}
			// nothing found is not kept in the history
			if r.ext.view.hist.size() != 0 {
				t.Error("a search with no result is in the history")
			}
		})
	}
}

func TestNoProviderIsNoResults(t *testing.T) {
	r := newRig(t)
	r.cursorAt("util.go", 3, "Greet") // the fake has no answer for the feature: ExtensionNoProvider
	r.run(FindImplementations, nil)
	if got := r.ext.view.message; got != "No results." {
		t.Errorf("message = %q", got)
	}
}

func TestNothingIsAskedWhereThereIsNoWord(t *testing.T) {
	r := newRig(t)
	r.api.SetDocument(r.uri("util.go"), project["util.go"])
	// line 1 of util.go is empty
	r.api.SetActive(&extension.ActiveDocument{URI: r.uri("util.go"), Selections: []extension.Selection{{}}})
	r.api.SetActive(&extension.ActiveDocument{URI: r.uri("util.go"), Selections: []extension.Selection{{Anchor: extension.Position{Line: 1}, Active: extension.Position{Line: 1}}}})
	r.answer("references", r.greetings())
	r.run(FindReferences, nil)
	if reqs := r.api.LocationRequests(); len(reqs) != 0 {
		t.Errorf("requests = %+v", reqs)
	}
	if got := r.ext.view.message; got != "No results." {
		t.Errorf("message = %q", got)
	}
	// no editor in front: the command does nothing
	r.api.SetActive(nil)
	r.run(FindReferences, nil)
	if reqs := r.api.LocationRequests(); len(reqs) != 0 {
		t.Errorf("requests = %+v", reqs)
	}
}

func TestANewSearchReplacesTheOneThatIsRunning(t *testing.T) {
	r := newRig(t)
	release := make(chan struct{})
	r.api.AnswerLocations("references", func(uri string, _ extension.Position) ([]extension.Location, error) {
		if uri == r.uri("util.go") { // the first search is held until the second one is over
			<-release
			return []extension.Location{r.place("main.go", 5, "Greet")}, nil
		}
		return r.greetings(), nil
	})
	r.cursorAt("util.go", 3, "Greet")
	if err := r.api.Run(FindReferences, nil); err != nil {
		t.Fatal(err)
	}
	if !r.ext.view.loading {
		t.Fatal("the search is not loading")
	}
	r.cursorAt("main.go", 5, "Greet")
	r.run(FindReferences, nil)
	close(release)
	// the late answer of the first search is dropped, whenever it comes
	time.Sleep(50 * time.Millisecond)
	r.api.Pump()
	if got := r.ext.view.message; got != "4 results in 3 files" {
		t.Errorf("message = %q: the answer of the replaced search was taken", got)
	}
}

func TestTheHistoryOfSearches(t *testing.T) {
	r := newRig(t)
	r.api.AnswerLocations("references", func(uri string, _ extension.Position) ([]extension.Location, error) {
		if uri == r.uri("util.go") {
			return r.greetings(), nil
		}
		return []extension.Location{r.place("main.go", 4, "main")}, nil
	})
	r.cursorAt("util.go", 3, "Greet")
	r.run(FindReferences, nil)
	r.cursorAt("main.go", 4, "main")
	r.run(FindReferences, nil)

	if err := r.api.Run(PickFromHistory, nil); err != nil {
		t.Fatal(err)
	}
	pick := r.api.LastPick()
	if pick == nil {
		t.Fatal("no list was shown")
	}
	if got := pick.Labels(); !slices.Equal(got, []string{"main", "Greet"}) {
		t.Errorf("labels = %v", got)
	}
	if got := pick.Options().Items[1].Description; got != "util.go • references" {
		t.Errorf("description = %q", got)
	}
	if pick.Options().Placeholder != "Select previous reference search" {
		t.Errorf("placeholder = %q", pick.Options().Placeholder)
	}
	// choosing runs the older search again, which becomes the newest
	pick.Choose(1)
	r.api.WaitFor("the search to end", func() bool { return !r.ext.view.loading })
	if got := r.ext.view.message; got != "4 results in 3 files" {
		t.Errorf("message = %q", got)
	}
	if got := r.ext.view.hist.last().word; got != "Greet" {
		t.Errorf("the newest search is %q", got)
	}

	// Clear shows the history, which is listed with the newest first
	r.run(Clear, nil)
	if got := r.ext.view.message; got != "No results. Try running a previous search again:" {
		t.Errorf("message = %q", got)
	}
	var rows []string
	for _, n := range r.ext.view.tree.Rows() {
		if d, ok := n.Data.(historyNode); ok {
			rows = append(rows, d.h.word)
		}
	}
	if !slices.Equal(rows, []string{"Greet", "main"}) {
		t.Errorf("the history = %v", rows)
	}
	if r.api.ContextKey(keyHasResult) != false || r.api.ContextKey(keyHasHistory) != true {
		t.Errorf("keys: hasResult %v, hasHistory %v", r.api.ContextKey(keyHasResult), r.api.ContextKey(keyHasHistory))
	}

	// Rerun on a row of the history runs it again; Refresh runs the newest again
	r.run(Refind, map[string]any{"key": "h:" + r.ext.view.hist.items[0].key})
	if r.ext.view.mod == nil {
		t.Error("Rerun did not run the search")
	}
	r.run(Clear, nil)
	r.run(Refresh, nil)
	if r.ext.view.mod == nil {
		t.Error("Refresh did not run the last search")
	}

	// Clear History forgets everything
	r.run(ClearHistory, nil)
	if r.ext.view.hist.size() != 0 || r.api.ContextKey(keyHasHistory) != false || r.ext.view.message != "No results." {
		t.Errorf("after Clear History: %d searches, key %v, message %q", r.ext.view.hist.size(), r.api.ContextKey(keyHasHistory), r.ext.view.message)
	}
	if err := r.api.Run(PickFromHistory, nil); err != nil {
		t.Fatal(err)
	}
}

func TestAHistoryRowOpensTheFileAtTheWord(t *testing.T) {
	r := newRig(t)
	r.answer("references", r.greetings())
	r.cursorAt("util.go", 3, "Greet")
	r.run(FindReferences, nil)
	r.run(Clear, nil)
	for _, n := range r.ext.view.tree.Rows() {
		if d, ok := n.Data.(historyNode); ok {
			r.ext.view.onSelect(n)
			_ = d
		}
	}
	shown := r.api.ShownDocuments()
	if len(shown) != 1 || shown[0].URI != r.uri("util.go") || shown[0].Line != 4 || shown[0].Column != 6 {
		t.Errorf("shown = %+v", shown)
	}
}

func TestNextAndPreviousLocation(t *testing.T) {
	r := newRig(t)
	r.cursorAt("util.go", 3, "Greet")
	r.answer("references", r.greetings())
	r.run(FindReferences, nil)
	// the declaration (util.go:4) is selected: next wraps to the first location (cli.go), which is shown
	r.run(Next, nil)
	r.run(Next, nil)
	r.run(Prev, nil)
	var lines []string
	for _, s := range r.api.ShownDocuments() {
		lines = append(lines, filepath.Base(strings.TrimPrefix(s.URI, "file://"))+":"+strconv.Itoa(s.Line)+":"+strconv.Itoa(s.Column))
	}
	want := []string{"cli.go:3:18", "main.go:6:14", "cli.go:3:18"}
	if !slices.Equal(lines, want) {
		t.Errorf("shown = %v, want %v", lines, want)
	}
	if key, _ := r.ext.view.tree.Selected(); !strings.HasPrefix(key, "r:"+r.uri("internal/cli/cli.go")) {
		t.Errorf("selected = %q", key)
	}
	for _, s := range r.api.ShownDocuments() {
		if !s.Preview {
			t.Errorf("%+v was not shown as a preview", s)
		}
	}
}

func TestNavigationStartsFromTheCursorWhenNothingIsSelected(t *testing.T) {
	r := newRig(t)
	r.cursorAt("util.go", 3, "Greet")
	r.answer("references", r.greetings())
	r.run(FindReferences, nil)
	r.ext.view.tree.ClearSelection()
	// the cursor is on the declaration, the last location: next goes to the first
	r.run(Next, nil)
	shown := r.api.ShownDocuments()
	if len(shown) != 1 || !strings.HasSuffix(shown[0].URI, "cli.go") {
		t.Errorf("shown = %+v", shown)
	}
	// without a result there is nowhere to go
	r.run(Clear, nil)
	r.run(Next, nil)
	if len(r.api.ShownDocuments()) != 1 {
		t.Error("navigation went on without a result")
	}
}

func TestDismissRemovesLocationsAndFiles(t *testing.T) {
	r := newRig(t)
	r.cursorAt("util.go", 3, "Greet")
	r.answer("references", r.greetings())
	r.run(FindReferences, nil)
	main := r.ext.view.mod.files[1]
	r.run(RemoveReferenceItem, map[string]any{"key": main.refs[0].key()})
	if got := r.ext.view.message; got != "3 results in 3 files" {
		t.Errorf("message = %q", got)
	}
	r.run(RemoveReferenceItem, map[string]any{"key": main.key()})
	if got := r.ext.view.message; got != "2 results in 2 files" {
		t.Errorf("message = %q", got)
	}
	// with no key the selected row goes: the declaration
	r.run(RemoveReferenceItem, nil)
	if got := r.ext.view.message; got != "1 result in 1 file" {
		t.Errorf("message = %q", got)
	}
}

func TestCopyCommands(t *testing.T) {
	r := newRig(t)
	r.cursorAt("util.go", 3, "Greet")
	r.answer("references", r.greetings())
	r.run(FindReferences, nil)
	main := r.ext.view.mod.files[1]
	if err := r.api.Run(Copy, map[string]any{"key": main.refs[1].key()}); err != nil {
		t.Fatal(err)
	}
	if err := r.api.Run(Copy, map[string]any{"key": main.key()}); err != nil {
		t.Fatal(err)
	}
	if err := r.api.Run(CopyPath, map[string]any{"key": main.key()}); err != nil {
		t.Fatal(err)
	}
	if err := r.api.Run(CopyAll, map[string]any{"key": main.refs[0].key()}); err != nil {
		t.Fatal(err)
	}
	got := r.api.ClipboardTexts()
	if len(got) != 4 {
		t.Fatalf("clipboard = %q", got)
	}
	// a location: line and column from 1, then the line around it (21 characters before, up to the end after)
	if want := "7, 14: \tfmt.Println(Greet(\"again\"))"; got[0] != want {
		t.Errorf("copy of a location = %q, want %q", got[0], want)
	}
	if want := "main.go\n  6, 14: \tfmt.Println(Greet(\"world\"))\n  7, 14: \tfmt.Println(Greet(\"again\"))\n"; got[1] != want {
		t.Errorf("copy of a file = %q, want %q", got[1], want)
	}
	if want := filepath.Join(r.folder, "main.go"); got[2] != want {
		t.Errorf("copy of a path = %q, want %q", got[2], want)
	}
	all := got[3]
	if !strings.HasPrefix(all, "internal/cli/cli.go\n  3, 18: ") || !strings.Contains(all, "\n\nmain.go\n") || !strings.HasSuffix(all, "util.go\n  4, 6: func Greet(name string) string {\n\n") {
		t.Errorf("copy of everything = %q", all)
	}
	// Copy Path on a location copies nothing
	if err := r.api.Run(CopyPath, map[string]any{"key": main.refs[0].key()}); err != nil || len(r.api.ClipboardTexts()) != 4 {
		t.Errorf("Copy Path on a location: %v, %q", err, r.api.ClipboardTexts())
	}
	// a clipboard that fails is the error of the command
	r.api.FailClipboard(errors.New("ClipboardBroken", "refused"))
	if err := r.api.Run(Copy, map[string]any{"key": main.key()}); err == nil {
		t.Error("a failed clipboard was no error")
	}
}

func TestCollapseAllFoldsTheFiles(t *testing.T) {
	r := newRig(t)
	r.cursorAt("util.go", 3, "Greet")
	r.answer("references", r.greetings())
	r.run(FindReferences, nil)
	before := len(r.ext.view.tree.Rows())
	r.run(CollapseAll, nil)
	if after := len(r.ext.view.tree.Rows()); after >= before || after != 3 {
		t.Errorf("rows %d before and %d after collapsing", before, after)
	}
}

func TestOpeningALocationShowsItAsAPreview(t *testing.T) {
	r := newRig(t)
	r.cursorAt("util.go", 3, "Greet")
	r.answer("references", r.greetings())
	r.run(FindReferences, nil)
	r.ext.view.tree.ExpandAll()
	node, ok := r.ext.view.tree.Node(r.ext.view.mod.files[1].refs[1].key())
	if !ok {
		t.Fatal("no row")
	}
	r.ext.view.onSelect(node)
	shown := r.api.ShownDocuments()
	if len(shown) != 1 || shown[0].URI != r.uri("main.go") || shown[0].Line != 7 || shown[0].Column != 14 || !shown[0].Preview {
		t.Errorf("shown = %+v", shown)
	}
	// a file in the tree is not opened by a click on it
	fileNode, _ := r.ext.view.tree.Node(r.ext.view.mod.files[1].key())
	r.ext.view.onSelect(fileNode)
	if len(r.api.ShownDocuments()) != 1 {
		t.Error("a file was opened")
	}
}

func TestAColumnInANonASCIILineIsCountedInUTF16Units(t *testing.T) {
	r := newRig(t)
	text := "größe := Greet()\n"
	uri := r.uri("util.go")
	r.api.SetDocument(uri, text)
	r.ext.view.texts[uri] = splitLines(text)
	r.ext.view.open(uri, extension.Position{Line: 0, Col: 11}) // byte 11 is the G, the 10th unit
	shown := r.api.ShownDocuments()
	if len(shown) != 1 || shown[0].Line != 1 || shown[0].Column != 10 {
		t.Errorf("shown = %+v", shown)
	}
}

func TestACursorInAClosedFileIsReadFromDisk(t *testing.T) {
	r := newRig(t)
	// the document is not open: the fake reads the file from disk, as the window does inside the folder
	pos := extension.Position{Line: 3, Col: 5}
	r.api.SetActive(&extension.ActiveDocument{URI: r.uri("util.go"), Selections: []extension.Selection{{Anchor: pos, Active: pos}}})
	r.answer("references", r.greetings())
	r.run(FindReferences, nil)
	if got := r.ext.view.message; got != "4 results in 3 files" {
		t.Errorf("message = %q", got)
	}
	r.loaded()
}

func TestAFileOutsideTheFolderKeepsItsFullPath(t *testing.T) {
	r := newRig(t)
	outside := t.TempDir()
	if got := r.ext.view.relPath("file://" + filepath.Join(outside, "x.go")); got != filepath.Join(outside, "x.go") {
		t.Errorf("relPath = %q", got)
	}
	if got := r.ext.view.relPath("untitled:1"); got != "untitled:1" {
		t.Errorf("relPath = %q", got)
	}
	if got := r.ext.view.relPath(r.uri("internal/cli/cli.go")); got != "internal/cli/cli.go" {
		t.Errorf("relPath = %q", got)
	}
	// a location in a file outside the folder is read from disk for its label
	if err := os.WriteFile(filepath.Join(outside, "x.go"), []byte("package x\nvar Greet = 1\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	loc := extension.Location{URI: "file://" + filepath.Join(outside, "x.go"), Range: extension.Range{Start: extension.Position{Line: 1, Col: 4}, End: extension.Position{Line: 1, Col: 9}}}
	ref := newModel([]extension.Location{loc}).files[0].refs[0]
	r.ext.view.ensureText(loc.URI)
	r.api.WaitFor("the file to be read", func() bool { l, ok := r.ext.view.ensureText(loc.URI); return ok && l != nil })
	if label, _ := r.ext.view.labelOf(ref); label != "var Greet = 1" {
		t.Errorf("label = %q", label)
	}
}

func TestAFileThatIsTooLargeIsNotRead(t *testing.T) {
	big := filepath.Join(t.TempDir(), "big.txt")
	f, err := os.Create(big)
	if err != nil {
		t.Fatal(err)
	}
	if err := f.Truncate(maxTextSize + 1); err != nil {
		t.Fatal(err)
	}
	if err := f.Close(); err != nil {
		t.Fatal(err)
	}
	_, err = readCapped(big)
	appErr, ok := errors.FromError(err)
	if !ok || !appErr.HasRoute("readCapped.TextTooLarge") {
		t.Errorf("error = %v", err)
	}
	if _, err := readCapped(filepath.Join(t.TempDir(), "missing")); err == nil {
		t.Error("a missing file was read")
	}
}
