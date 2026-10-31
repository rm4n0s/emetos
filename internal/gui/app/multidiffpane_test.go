package app

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/rm4n0s/errors"
	"github.com/rm4n0s/glutter/gluttertest"
	"github.com/rm4n0s/glutter/ui"
	"github.com/rm4n0s/glutter/widgets"

	"github.com/rm4n0s/emetos/extension"
	"github.com/rm4n0s/emetos/internal/core/command"
	"github.com/rm4n0s/emetos/internal/core/document"
	"github.com/rm4n0s/emetos/internal/core/editorgroup"
	"github.com/rm4n0s/emetos/internal/gui/kit"
	"github.com/rm4n0s/emetos/internal/gui/multidiff"
	"github.com/rm4n0s/emetos/internal/gui/testutil"
)

// The multi-file diff editor in the window (step 24.17).

// multiRig is an extension that provides the documents of the scheme "tst" from a map and the sets of the kind "tst.changes" from
// another, and the app it runs in.
type multiRig struct {
	*groupApp
	api extension.API
	reg extension.MultiDiffRegistration

	mu sync.Mutex
	// texts are the documents of the provider "tst"; sets are the files of each key of "tst.changes".
	texts map[string]string
	sets  map[string][]extension.MultiDiffResource
	// err is what Resources fails with, by key.
	err map[string]error
	// asked counts the calls of Resources by key.
	asked map[string]int
	// ran are the arguments of the commands of the per-file actions that ran.
	ran []any
}

func (r *multiRig) text(_ context.Context, uri string) (string, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	switch t := r.texts[uri]; t {
	case "BINARY":
		return "", errors.New(extension.TagContentBinary, "the file looks binary", "uri", uri)
	default:
		return t, nil
	}
}

func (r *multiRig) resources(_ context.Context, key string) ([]extension.MultiDiffResource, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.asked[key]++
	if err := r.err[key]; err != nil {
		return nil, err
	}
	return slices.Clone(r.sets[key]), nil
}

func (r *multiRig) setFiles(key string, files ...extension.MultiDiffResource) {
	r.mu.Lock()
	r.sets[key] = files
	r.mu.Unlock()
}

func (r *multiRig) askedOf(key string) int {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.asked[key]
}

func (r *multiRig) factory() extension.Factory {
	return func() extension.Extension {
		return &testExtension{
			manifest: extension.Manifest{
				ID: "emetos.multidifftest", Name: "Multi diff test", ActivationEvents: []string{extension.OnStartup},
				Contributes: extension.Contributes{
					Commands: []extension.CommandSpec{{ID: "tst.stageFile", Title: "Stage Changes", Icon: "add"}, {ID: "tst.hidden", Title: "Hidden", Icon: "remove"}},
					Menus: []extension.MenuItem{
						{Menu: extension.MenuMultiDiffFileToolbar, Command: "tst.stageFile", Group: "navigation", When: "scmProvider == tst && scmResourceGroup == workingTree"},
						{Menu: extension.MenuMultiDiffFileToolbar, Command: "tst.hidden", Group: "navigation", When: "scmProvider == tst && scmResourceGroup == index"},
					},
				},
			},
			activate: func(_ context.Context, api extension.API) error {
				r.api = api
				for _, id := range []string{"tst.stageFile", "tst.hidden"} {
					if err := api.Commands().Handle(id, func(_ context.Context, args any) error { r.ran = append(r.ran, args); return nil }); err != nil {
						return err
					}
				}
				if _, err := api.Documents().ProvideContent(extension.ContentProvider{Scheme: "tst", Text: r.text}); err != nil {
					return err
				}
				reg, err := api.Documents().ProvideMultiDiff(extension.MultiDiffProvider{Kind: "tst.changes", Resources: r.resources})
				r.reg = reg
				return err
			},
		}
	}
}

func newMultiRig(t *testing.T, names ...string) *multiRig {
	t.Helper()
	r := &multiRig{texts: map[string]string{}, sets: map[string][]extension.MultiDiffResource{}, err: map[string]error{}, asked: map[string]int{}}
	p := extensionProcess(t, r.factory())
	w, err := NewWorkspace(context.Background(), p, "")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(w.release)
	r.groupApp = newGroupAppWith(t, w, testWindowSize, names, nil)
	if r.api == nil || r.reg == nil {
		t.Fatal("the extension did not register its providers")
	}
	return r
}

// modified is a file of the work tree changed against its version "tst:<name>": the file of the group's folder.
func (r *multiRig) modified(name string) extension.MultiDiffResource {
	return extension.MultiDiffResource{
		Original: tst("id/~/" + name), Modified: document.FileURI(r.files[name]).String(), Path: "src/" + name, File: document.FileURI(r.files[name]).String(),
		Letter: "M", Color: "gitDecoration.modifiedResourceForeground", Tooltip: "Modified",
		Context: map[string]any{"scmProvider": "tst", "scmResourceGroup": "workingTree"},
		Args:    []any{map[string]any{"group": "workingTree", "uri": document.FileURI(r.files[name]).String()}},
	}
}

func (r *multiRig) multiPanes() []*multiDiffPane { return r.shell().multiDiffPanes() }

// pane is the only multi-file diff editor.
func (r *multiRig) pane() *multiDiffPane {
	r.t.Helper()
	ps := r.multiPanes()
	if len(ps) != 1 {
		r.t.Fatalf("%d multi-file diff editors", len(ps))
	}
	return ps[0]
}

// settle runs frames until the bodies that exist have their diffs.
func (r *multiRig) settleBodies() {
	r.t.Helper()
	waitFor(r.t, r.ts, "the bodies", func() bool {
		any := false
		for _, p := range r.multiPanes() {
			for _, b := range p.bodies {
				any = true
				if !b.ready() && b.notice == "" {
					return false
				}
			}
		}
		return any
	})
	for range 4 {
		r.ts.Pump(10 * time.Millisecond)
	}
}

func TestAMultiFileDiffEditorOpensWithItsFiles(t *testing.T) {
	r := newMultiRig(t, "a.go", "b.go", "c.go")
	for _, n := range []string{"a.go", "b.go", "c.go"} {
		r.set(tst("id/~/"+n), "package x\n// old "+n+"\n")
	}
	r.setFiles("k1", r.modified("a.go"), r.modified("b.go"), r.modified("c.go"))
	if err := r.api.Documents().OpenMultiDiff(extension.MultiDiffSpec{Title: "Tst: Changes", Kind: "tst.changes", Key: "k1"}); err != nil {
		t.Fatal(err)
	}
	waitFor(t, r.ts, "the tab", func() bool { return len(r.tabs()) == 1 && len(r.multiPanes()) == 1 })
	waitFor(t, r.ts, "the files", func() bool { return r.pane().input.Loaded() })
	if got := r.tabNames(); !slices.Equal(got, []string{"Tst: Changes (3 files)"}) {
		t.Errorf("tabs %v", got)
	}
	r.settleBodies()
	p := r.pane()
	if p.model.Len() != 3 || len(p.bodies) != 3 {
		t.Fatalf("%d sections, %d bodies", p.model.Len(), len(p.bodies))
	}
	for i := range 3 {
		if !p.model.Measured(i) {
			t.Errorf("section %d was not measured", i)
		}
	}
	// two lines, a change in the second: the body is as tall as its text and no taller
	b := p.bodies[p.model.Section(0).Key]
	if b == nil || b.dp == nil {
		t.Fatal("no diff editor in the first body")
	}
	if got, want := p.model.BodyHeight(0), contentHeight(b.dp); got != want {
		t.Errorf("body height %v, content %v", got, want)
	}
	if ch := b.dp.model.Changes(); len(ch) != 1 {
		t.Errorf("changes %+v", ch)
	}
	// every editor of the bodies is read-only
	for _, ed := range b.dp.sides {
		if !ed.view.ReadOnly() {
			t.Error("an editor of a section can be edited")
		}
	}
	// the kept tab is the multi-file editor
	if !r.shell().activeGroup().Model.IsPinned(r.activeInput()) {
		t.Error("the editor is a preview, want kept")
	}
	r.noErrors()
}

func (r *multiRig) set(uri, text string) {
	r.mu.Lock()
	r.texts[uri] = text
	r.mu.Unlock()
}

func (r *multiRig) activeInput() editorgroup.EditorInput {
	return r.shell().activeGroup().Model.ActiveEditor()
}

// run runs a command of the window.
func (r *multiRig) run(id string) {
	r.t.Helper()
	if err := r.win.Commands.Run(command.Context{Ctx: context.Background(), Keys: r.win.Keys, Source: command.SourceAPI}, id, nil); err != nil {
		r.t.Fatalf("%s: %v", id, err)
	}
	r.ts.Pump(10 * time.Millisecond)
}

// open opens the set of a key and waits for its files and its bodies.
func (r *multiRig) open(title, key string) {
	r.t.Helper()
	if err := r.api.Documents().OpenMultiDiff(extension.MultiDiffSpec{Title: title, Kind: "tst.changes", Key: key}); err != nil {
		r.t.Fatal(err)
	}
	waitFor(r.t, r.ts, "the files", func() bool { p := r.paneOf(key); return p != nil && p.input.Loaded() })
	r.settleBodies()
}

// paneOf is the multi-file diff editor of a key, nil when there is none.
func (r *multiRig) paneOf(key string) *multiDiffPane {
	for _, p := range r.multiPanes() {
		if p.input.Key() == key {
			return p
		}
	}
	return nil
}

// buttons are the tooltips of the icon buttons of the headers on screen.
func (r *multiRig) buttons() []string {
	var out []string
	for _, w := range r.ts.Widgets(r.headerButtons()) {
		out = append(out, w.(*kit.IconButton).Tooltip())
	}
	return out
}

// headerButtons finds the icon buttons of the headers of the sections.
func (r *multiRig) headerButtons() gluttertest.Finder {
	return gluttertest.Descendant(gluttertest.ByType[*multidiff.Widget](), gluttertest.ByType[*kit.IconButton]())
}

// headerButton finds the icon button of a header by its tooltip, the first when there are several.
func (r *multiRig) headerButton(tooltip string) gluttertest.Finder {
	return gluttertest.First(gluttertest.ByPredicate("header button "+tooltip, func(w widgets.Widget) bool {
		b, ok := w.(*kit.IconButton)
		return ok && b.Tooltip() == tooltip
	}))
}

// files makes n modified files named f0.go, f1.go... of the group's folder, with a version each.
func (r *multiRig) manyFiles(n int) []extension.MultiDiffResource {
	var out []extension.MultiDiffResource
	for i := range n {
		name := fmt.Sprintf("f%d.go", i)
		r.set(tst("id/~/"+name), "package x\n")
		out = append(out, extension.MultiDiffResource{
			Original: tst("id/~/" + name), Modified: tst("id/HEAD/" + name), Path: "pkg/" + name, Letter: "M",
		})
		r.set(tst("id/HEAD/"+name), "package x\n// new "+name+"\n")
	}
	return out
}

func TestTheSetIsAskedAgainWhenTheProviderSaysItChanged(t *testing.T) {
	r := newMultiRig(t, "a.go", "b.go", "c.go")
	for _, n := range []string{"a.go", "b.go", "c.go"} {
		r.set(tst("id/~/"+n), "old\n")
	}
	r.setFiles("k", r.modified("a.go"), r.modified("b.go"), r.modified("c.go"))
	r.open("Tst: Changes", "k")
	p := r.pane()
	keyB := p.model.Section(1).Key
	p.model.SetCollapsed(1, true)
	before := r.askedOf("k")
	// b.go is staged: it leaves the set, and a.go and c.go keep what they had
	r.setFiles("k", r.modified("a.go"), r.modified("c.go"))
	r.reg.Changed("k", "other")
	waitFor(t, r.ts, "the second answer", func() bool { return p.model.Len() == 2 })
	if got := r.askedOf("k"); got != before+1 {
		t.Errorf("asked %d times, want %d", got, before+1)
	}
	waitFor(t, r.ts, "the new count in the tab", func() bool { return slices.Equal(r.tabNames(), []string{"Tst: Changes (2 files)"}) })
	if _, still := p.index[keyB]; still {
		t.Error("the staged file is still a section")
	}
	// the measured heights stay: the two sections are not asked to measure again to have a height
	if !p.model.Measured(0) || !p.model.Measured(1) {
		t.Error("the sections that stayed lost their heights")
	}
	// a file that comes back is a new section
	r.setFiles("k", r.modified("a.go"), r.modified("b.go"), r.modified("c.go"))
	r.reg.Changed("k")
	waitFor(t, r.ts, "the third answer", func() bool { return p.model.Len() == 3 })
	if p.model.Collapsed(1) {
		t.Error("the collapsed mark of a file that left came back with it")
	}
	// a key that is not open costs nothing
	n := r.askedOf("k")
	r.reg.Changed("not-open")
	r.ts.Pump(10 * time.Millisecond)
	if r.askedOf("k") != n || r.askedOf("not-open") != 0 {
		t.Error("a set that is not open was asked for")
	}
	r.noErrors()
}

func TestASetThatIsGoneClosesItsTabWithoutAWord(t *testing.T) {
	r := newMultiRig(t, "a.go")
	r.set(tst("id/~/a.go"), "old\n")
	r.setFiles("k", r.modified("a.go"))
	r.open("Tst: Changes", "k")
	if len(r.tabs()) != 1 {
		t.Fatalf("tabs %v", r.tabNames())
	}
	notes := len(r.win.Notify.Items())
	r.mu.Lock()
	r.err["k"] = errors.New(extension.TagMultiDiffGone, "the repository is closed")
	r.mu.Unlock()
	r.reg.Changed("k")
	waitFor(t, r.ts, "the tab to close", func() bool { return len(r.tabs()) == 0 })
	if len(r.win.Notify.Items()) != notes {
		t.Errorf("notifications %v", r.win.Notify.Items())
	}
	r.noErrors()
}

func TestAProviderThatFailsShowsItsMessageInTheTab(t *testing.T) {
	r := newMultiRig(t)
	r.mu.Lock()
	r.err["k"] = errors.New("RigFailure", "the rig failed on purpose")
	r.mu.Unlock()
	if err := r.api.Documents().OpenMultiDiff(extension.MultiDiffSpec{Title: "Tst: Changes", Kind: "tst.changes", Key: "k"}); err != nil {
		t.Fatal(err)
	}
	waitFor(t, r.ts, "the message", func() bool {
		return r.ts.Exists(gluttertest.ByText("Cannot show the changes: the rig failed on purpose"))
	})
	if got := r.tabNames(); !slices.Equal(got, []string{"Tst: Changes"}) {
		t.Errorf("tabs %v: a tab with no files has no count", got)
	}
	// the next answer replaces the message
	r.mu.Lock()
	delete(r.err, "k")
	r.mu.Unlock()
	r.reg.Changed("k")
	waitFor(t, r.ts, "the empty message", func() bool { return r.ts.Exists(gluttertest.ByText("There are no changes to show.")) })
	if got := r.tabNames(); !slices.Equal(got, []string{"Tst: Changes (0 files)"}) {
		t.Errorf("tabs %v", got)
	}
	r.noErrors()
}

func TestTheSameSetIsOneTab(t *testing.T) {
	r := newMultiRig(t, "a.go")
	r.set(tst("id/~/a.go"), "old\n")
	r.setFiles("k", r.modified("a.go"))
	r.setFiles("j", r.modified("a.go"))
	r.open("Tst: Changes", "k")
	n := r.askedOf("k")
	if err := r.api.Documents().OpenMultiDiff(extension.MultiDiffSpec{Title: "Another title", Kind: "tst.changes", Key: "k"}); err != nil {
		t.Fatal(err)
	}
	waitFor(t, r.ts, "the second answer", func() bool { return r.askedOf("k") > n })
	if got := len(r.tabs()); got != 1 {
		t.Errorf("%d tabs for the same set", got)
	}
	// another key is another tab
	r.open("Other", "j")
	waitFor(t, r.ts, "two tabs", func() bool { return len(r.tabs()) == 2 })
	r.noErrors()
}

func TestAnUnknownKindIsRefusedAndAFixedSetOpensAndIsNotSaved(t *testing.T) {
	r := newMultiRig(t, "a.go")
	r.set(tst("id/~/a.go"), "old\n")
	err := r.api.Documents().OpenMultiDiff(extension.MultiDiffSpec{Title: "x", Kind: "no.such", Key: "k"})
	if appErr, ok := errors.FromError(err); !ok || appErr.Tag != "ExtMultiDiffKindUnknown" {
		t.Errorf("an unknown kind: %v", err)
	}
	res := r.modified("a.go")
	if err := r.api.Documents().OpenMultiDiff(extension.MultiDiffSpec{Title: "Fixed", Resources: []extension.MultiDiffResource{res}}); err != nil {
		t.Fatal(err)
	}
	waitFor(t, r.ts, "the tab", func() bool { return len(r.multiPanes()) == 1 })
	r.settleBodies()
	if got := r.tabNames(); !slices.Equal(got, []string{"Fixed (1 file)"}) {
		t.Errorf("tabs %v", got)
	}
	if _, ok := r.activeInput().Serialize(); ok {
		t.Error("a fixed set is saved with the window")
	}
	// a second fixed set is another tab
	if err := r.api.Documents().OpenMultiDiff(extension.MultiDiffSpec{Title: "Fixed", Resources: []extension.MultiDiffResource{res}}); err != nil {
		t.Fatal(err)
	}
	waitFor(t, r.ts, "two tabs", func() bool { return len(r.tabs()) == 2 })
	r.noErrors()
}

func TestCollapseAllAndExpandAll(t *testing.T) {
	r := newMultiRig(t, "a.go", "b.go")
	for _, n := range []string{"a.go", "b.go"} {
		r.set(tst("id/~/"+n), "old\n")
	}
	r.setFiles("k", r.modified("a.go"), r.modified("b.go"))
	r.open("Tst: Changes", "k")
	p := r.pane()
	if r.win.Keys.Value("multiDiffEditorAllCollapsed") != false {
		t.Errorf("multiDiffEditorAllCollapsed = %v", r.win.Keys.Value("multiDiffEditorAllCollapsed"))
	}
	r.run("multiDiffEditor.collapseAll")
	if !p.model.AllCollapsed() || r.win.Keys.Value("multiDiffEditorAllCollapsed") != true {
		t.Errorf("all collapsed %v key %v", p.model.AllCollapsed(), r.win.Keys.Value("multiDiffEditorAllCollapsed"))
	}
	if len(p.bodies) != 0 {
		t.Errorf("%d bodies of collapsed sections", len(p.bodies))
	}
	r.run("multiDiffEditor.expandAll")
	if p.model.AllCollapsed() || r.win.Keys.Value("multiDiffEditorAllCollapsed") != false {
		t.Error("expand all")
	}
	r.settleBodies()
	r.noErrors()
}

func TestTheTitleOfTheEditorHasTheMultiFileActions(t *testing.T) {
	r := newMultiRig(t, "a.go")
	r.set(tst("id/~/a.go"), "old\n")
	r.setFiles("k", r.modified("a.go"))
	r.open("Tst: Changes", "k")
	items := r.win.Menus.Items(command.EditorTitle, r.shell().groupContext(r.win, r.shell().activeGroup()))
	var got []string
	for _, e := range items {
		got = append(got, e.Command)
	}
	want := []string{"multiDiffEditor.goToPreviousChange", "multiDiffEditor.goToNextChange", "multiDiffEditor.collapseAll"}
	if !slices.Equal(got, want) {
		t.Errorf("title items %v, want %v", got, want)
	}
	r.run("multiDiffEditor.collapseAll")
	items = r.win.Menus.Items(command.EditorTitle, r.shell().groupContext(r.win, r.shell().activeGroup()))
	got = nil
	for _, e := range items {
		got = append(got, e.Command)
	}
	if want := []string{"multiDiffEditor.goToPreviousChange", "multiDiffEditor.goToNextChange", "multiDiffEditor.expandAll"}; !slices.Equal(got, want) {
		t.Errorf("title items with all collapsed %v, want %v", got, want)
	}
	// they are not in the title of another editor
	r.doc("a.go")
	r.noErrors()
}

func TestTheActionsOfAFileComeFromItsMenu(t *testing.T) {
	r := newMultiRig(t, "a.go", "b.go")
	for _, n := range []string{"a.go", "b.go"} {
		r.set(tst("id/~/"+n), "old\n")
	}
	staged := r.modified("b.go")
	staged.Context = map[string]any{"scmProvider": "tst", "scmResourceGroup": "index"}
	r.setFiles("k", r.modified("a.go"), staged)
	r.open("Tst: Changes", "k")
	// a.go: Open File and Stage; b.go: Open File and the action of the index group
	want := []string{"Open File", "Stage Changes", "Open File", "Hidden"}
	if got := r.buttons(); !slices.Equal(got, want) {
		t.Fatalf("buttons %v, want %v", got, want)
	}
	r.ts.Tap(r.headerButton("Stage Changes"))
	r.ts.Pump(10 * time.Millisecond)
	if len(r.ran) != 1 {
		t.Fatalf("ran %v", r.ran)
	}
	args, ok := r.ran[0].([]any)
	if !ok || len(args) != 1 || args[0].(map[string]any)["group"] != "workingTree" {
		t.Errorf("the command got %#v, want the arguments of the file", r.ran[0])
	}
	if r.pane().model.Collapsed(0) {
		t.Error("a click on an action collapsed the section")
	}
	r.noErrors()
}

func TestOpenFileShowsTheFileAtItsFirstChange(t *testing.T) {
	r := newMultiRig(t, "a.go")
	r.set(tst("id/~/a.go"), "package x\n// a.go\n")
	if err := os.WriteFile(r.files["a.go"], []byte("package x\n// a.go\nline\nmore\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	r.setFiles("k", r.modified("a.go"))
	r.open("Tst: Changes", "k")
	r.ts.Tap(r.headerButton("Open File"))
	waitFor(t, r.ts, "the file", func() bool { return len(r.tabs()) == 2 })
	if got := r.tabNames(); !slices.Equal(got, []string{"Tst: Changes (1 file)", "a.go"}) {
		t.Errorf("tabs %v", got)
	}
	e := r.shell().activeEditor()
	if e == nil || e.view.Cursors().Primary().Position().Line != 2 {
		t.Errorf("the cursor of the file is not at its first change")
	}
	if !r.shell().activeGroup().Model.IsPinned(r.activeInput()) {
		t.Error("the file is a preview, want kept")
	}
	r.noErrors()
}

func TestTheEditorRevealsTheFileItWasAskedFor(t *testing.T) {
	r := newMultiRig(t)
	r.setFiles("k", r.manyFiles(30)...)
	if err := r.api.Documents().OpenMultiDiff(extension.MultiDiffSpec{Title: "Many", Kind: "tst.changes", Key: "k", Reveal: tst("id/HEAD/f20.go")}); err != nil {
		t.Fatal(err)
	}
	waitFor(t, r.ts, "the files", func() bool { return len(r.multiPanes()) == 1 && r.pane().input.Loaded() })
	p := r.pane()
	if got := p.model.Current(); got != 20 {
		t.Errorf("the editor is at section %d, want 20", got)
	}
	r.settleBodies()
	if p.model.Current() != 20 {
		t.Errorf("after the heights were measured the editor is at section %d, want 20", p.model.Current())
	}
	r.noErrors()
}

func TestGoToNextChangeCrossesTheFiles(t *testing.T) {
	r := newMultiRig(t)
	files := r.manyFiles(6)
	r.setFiles("k", files...)
	r.open("Many", "k")
	p := r.pane()
	step := func(cmd string) {
		r.run(cmd)
		for range 4 {
			r.ts.Pump(10 * time.Millisecond)
		}
		waitFor(t, r.ts, "the diff", func() bool { return !p.nav.pending })
	}
	step("multiDiffEditor.goToNextChange")
	if p.nav.section != 0 || p.nav.change != 0 {
		t.Fatalf("first: %+v", p.nav)
	}
	step("multiDiffEditor.goToNextChange")
	if p.nav.section != 1 {
		t.Fatalf("the second change is in the next file: %+v", p.nav)
	}
	if top := p.model.Top(1) + multidiff.HeaderHeight; top < p.model.ScrollTop() || top > p.model.ScrollTop()+p.model.Viewport() {
		t.Errorf("the second file is at %v, outside the viewport at %v", top, p.model.ScrollTop())
	}
	step("multiDiffEditor.goToPreviousChange")
	if p.nav.section != 0 {
		t.Errorf("back to the first file: %+v", p.nav)
	}
	step("multiDiffEditor.goToPreviousChange") // wraps around to the last file
	if p.nav.section != 5 {
		t.Errorf("wrapped to %+v", p.nav)
	}
	r.noErrors()
}

func TestOnlyTheNearSectionsHaveABodyWith2000Files(t *testing.T) {
	r := newMultiRig(t)
	files := make([]extension.MultiDiffResource, 2000)
	for i := range files {
		files[i] = extension.MultiDiffResource{
			Original: tst(fmt.Sprintf("id/~/f%d.go", i)), Modified: tst(fmt.Sprintf("id/HEAD/f%d.go", i)), Path: fmt.Sprintf("pkg/%d/f%d.go", i%50, i), Letter: "M",
		}
	}
	// one text for every file: the provider is not asked for the others until a section needs them
	r.setFiles("k", files...)
	r.open("Many", "k")
	p := r.pane()
	if p.model.Len() != 2000 {
		t.Fatalf("%d sections", p.model.Len())
	}
	// the bodies are those of the sections within the viewport and the overscan, whatever the size of the set
	near := func(at string) {
		t.Helper()
		if n := len(p.bodies); n == 0 || n > 40 {
			t.Errorf("%d bodies %s", n, at)
		}
		lo, hi := p.model.ScrollTop()-multidiff.DefaultOverscan-1, p.model.ScrollTop()+p.model.Viewport()+multidiff.DefaultOverscan+1
		for key := range p.bodies {
			i := p.index[key]
			if p.model.Top(i)+p.model.Height(i) < lo || p.model.Top(i) > hi {
				t.Errorf("a body of section %d, at %v, while the editor shows %v to %v %s", i, p.model.Top(i), lo, hi, at)
			}
		}
	}
	near("at the top")
	docs := len(r.win.Documents.All())
	for _, at := range []int{1000, 1999} {
		p.model.ScrollToSection(at)
		r.ts.Pump(10 * time.Millisecond)
		r.settleBodies()
		near(fmt.Sprintf("at section %d", at))
	}
	// the documents of the bodies that left were let go
	if n := len(r.win.Documents.All()); n > docs+100 {
		t.Errorf("%d documents open after scrolling through 2,000 files (%d at the top)", n, docs)
	}
	r.noErrors()
}

func TestAClosedEditorLetsGoOfItsDocuments(t *testing.T) {
	r := newMultiRig(t)
	r.setFiles("k", r.manyFiles(5)...)
	open := len(r.win.Documents.All())
	r.open("Many", "k")
	if len(r.win.Documents.All()) <= open {
		t.Fatal("the bodies opened no documents")
	}
	r.shell().closeEditors(r.shell().activeGroup(), []editorgroup.EditorInput{r.activeInput()})
	waitFor(t, r.ts, "the tab to close", func() bool { return len(r.tabs()) == 0 })
	for range 5 {
		r.ts.Pump(10 * time.Millisecond)
	}
	if n := len(r.win.Documents.All()); n != open {
		t.Errorf("%d documents open, want %d: a closed editor holds documents", n, open)
	}
	r.noErrors()
}

// the second run of the program: the saved editor comes back, and asks the provider when the extension has started.
func TestTheSavedEditorAsksTheProviderAfterARestart(t *testing.T) {
	env := newStateEnv(t, nil)
	rig := &multiRig{texts: map[string]string{}, sets: map[string][]extension.MultiDiffResource{}, err: map[string]error{}, asked: map[string]int{}}
	rig.setFiles("k", rig.manyFiles(3)...)
	opts := ProcessOptions{Extensions: []extension.Factory{rig.factory()}}
	first := env.startWith(env.root, opts)
	waitFor(t, first.ts, "the extension", func() bool { return rig.api != nil })
	if err := rig.api.Documents().OpenMultiDiff(extension.MultiDiffSpec{Title: "Tst: Changes", Kind: "tst.changes", Key: "k"}); err != nil {
		t.Fatal(err)
	}
	waitFor(t, first.ts, "the files", func() bool {
		ps := first.shell().multiDiffPanes()
		return len(ps) == 1 && ps[0].input.Loaded()
	})
	if got := first.layout(); len(got) != 1 || !strings.Contains(got[0], "Tst: Changes (3 files)") {
		t.Fatalf("layout %v", got)
	}
	first.close()
	st, ok := env.savedWindow(env.root)
	if !ok {
		t.Fatal("no saved window")
	}
	var saved []editorgroup.SerializedInput
	for _, n := range st.Grid.Root {
		if n.Model != nil {
			saved = append(saved, n.Model.Editors...)
		}
	}
	if len(saved) != 1 || saved[0].TypeID != editorgroup.MultiDiffInputTypeID || strings.Contains(saved[0].Value, "f0.go") {
		t.Errorf("saved editors %+v: only the kind, the key and the title are saved", saved)
	}

	// a run without the extension: the tab is there, with its title, and says what it waits for
	second := env.start(env.root)
	if got := second.layout(); len(got) != 1 || !strings.Contains(got[0], "Tst: Changes") || strings.Contains(got[0], "files)") {
		t.Fatalf("layout without the extension %v", got)
	}
	if !second.ts.Exists(gluttertest.ByText(multiDiffWaiting)) {
		t.Error("the editor does not say that it waits for the extension")
	}
	second.close()

	// a run with it: the provider is asked once it is registered
	before := rig.askedOf("k")
	third := env.startWith(env.root, opts)
	waitFor(t, third.ts, "the files", func() bool {
		ps := third.shell().multiDiffPanes()
		return len(ps) == 1 && ps[0].input.Loaded() && len(ps[0].bodies) == 3
	})
	if got := third.layout(); len(got) != 1 || !strings.Contains(got[0], "Tst: Changes (3 files)") {
		t.Errorf("layout after the restart %v", got)
	}
	if rig.askedOf("k") <= before {
		t.Error("the provider was not asked for the files of the saved editor")
	}
	if errs := third.ts.TakeErrors(); len(errs) != 0 {
		t.Errorf("errors: %v", errs)
	}
	third.close()
}

// A wheel over the diff of a file scrolls the multi-file editor: the editors of the sections have nothing to scroll.
func TestTheWheelOverABodyScrollsTheWholeEditor(t *testing.T) {
	r := newMultiRig(t)
	long := "package x\n" + strings.Repeat("// line\n", 60)
	for i := range 6 {
		r.set(tst(fmt.Sprintf("id/~/f%d.go", i)), long)
		r.set(tst(fmt.Sprintf("id/HEAD/f%d.go", i)), long+"// added\n")
	}
	var files []extension.MultiDiffResource
	for i := range 6 {
		files = append(files, extension.MultiDiffResource{Original: tst(fmt.Sprintf("id/~/f%d.go", i)), Modified: tst(fmt.Sprintf("id/HEAD/f%d.go", i)), Path: fmt.Sprintf("f%d.go", i)})
	}
	r.setFiles("k", files...)
	r.open("Many", "k")
	p := r.pane()
	// the editor of a body is as tall as its text
	b := p.bodies[p.model.Section(0).Key]
	if h := p.model.BodyHeight(0); h < 60*19 {
		t.Fatalf("the first body is %v tall", h)
	}
	if got, want := b.dp.sides[1].view.ContentHeight(), p.model.BodyHeight(0); got > want+1 {
		t.Errorf("the editor of the body has %v of content in a section %v tall: it could scroll inside the section", got, want)
	}
	r.ts.ScrollAt(ui.Offset{Dx: 800, Dy: 300}, ui.Offset{Dx: 0, Dy: 106})
	for range 20 {
		r.ts.Pump(20 * time.Millisecond)
	}
	if got := p.model.ScrollTop(); got < 90 {
		t.Errorf("the wheel over a body scrolled the editor to %v", got)
	}
	r.noErrors()
}

// The inline view of the diff editor makes the sections taller: the heights follow the setting.
func TestTheHeightsFollowTheInlineViewSetting(t *testing.T) {
	r := newMultiRig(t, "a.go")
	r.set(tst("id/~/a.go"), "one\ntwo\nthree\n")
	if err := os.WriteFile(r.files["a.go"], []byte("one\n2\nthree\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	r.setFiles("k", r.modified("a.go"))
	r.open("Tst", "k")
	p := r.pane()
	side := p.model.BodyHeight(0)
	r.run("toggle.diff.renderSideBySide")
	waitFor(t, r.ts, "the inline view", func() bool { return p.model.BodyHeight(0) > side })
	r.noErrors()
}

var _ = filepath.Join
var _ = testutil.MonoFamily

func TestAKindOfMultiDiffProviderIsTakenOnce(t *testing.T) {
	r := newMultiRig(t, "a.go")
	_, err := r.api.Documents().ProvideMultiDiff(extension.MultiDiffProvider{Kind: "tst.changes", Resources: r.resources})
	appErr, ok := errors.FromError(err)
	if !ok || appErr.Tag != "ExtMultiDiffKindTaken" {
		t.Fatalf("got %v, want the tag ExtMultiDiffKindTaken", err)
	}
	if !appErr.HasRoute("extensionDocuments.ProvideMultiDiff.ExtMultiDiffKindTaken") {
		t.Errorf("route %s", appErr.Route())
	}
	// after the registration is disposed the kind is free again
	r.reg.Dispose()
	reg, err := r.api.Documents().ProvideMultiDiff(extension.MultiDiffProvider{Kind: "tst.changes", Resources: r.resources})
	if err != nil {
		t.Fatal(err)
	}
	reg.Dispose()
}

func TestTheHeaderIconsComeFromTheIconThemeLikeTheExplorersDo(t *testing.T) {
	r := newMultiRig(t, "loader.go", "notes.md", "plain.xyz")
	var files []extension.MultiDiffResource
	for _, n := range []string{"loader.go", "notes.md", "plain.xyz"} {
		r.set(tst("id/~/"+n), "old\n")
		files = append(files, r.modified(n))
	}
	r.setFiles("k", files...)
	r.open("Tst: Changes", "k")
	// the icon of a file is found by its name, its folder and its language: a Go file has the Go icon and a Markdown file the Markdown
	// one, as in the Explorer (the glyphs of the icon theme); a file the theme knows nothing of has the icon of a plain file
	inHeaders := func(glyph string) int {
		return r.ts.Count(gluttertest.Descendant(gluttertest.ByType[*multidiff.Widget](), gluttertest.ByText(glyph)))
	}
	if inHeaders(goGlyph) != 1 {
		t.Errorf("%d Go icons in the headers, want 1", inHeaders(goGlyph))
	}
	const markdownGlyph = "\ue060" // guide.md: the icon of the markdown language (README.md has the info icon, by its name)
	if inHeaders(markdownGlyph) != 1 {
		t.Errorf("%d Markdown icons in the headers, want 1", inHeaders(markdownGlyph))
	}
	r.noErrors()
}
