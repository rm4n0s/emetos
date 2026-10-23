package app

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/rm4n0s/errors"
	"github.com/rm4n0s/glutter/foundation"
	"github.com/rm4n0s/glutter/gestures"
	"github.com/rm4n0s/glutter/gluttertest"
	"github.com/rm4n0s/glutter/ui"
	"github.com/rm4n0s/glutter/widgets"

	"github.com/rm4n0s/emetos/internal/core/buffer"
	"github.com/rm4n0s/emetos/internal/core/command"
	"github.com/rm4n0s/emetos/internal/core/document"
	"github.com/rm4n0s/emetos/internal/core/editorgroup"
	"github.com/rm4n0s/emetos/internal/core/keybinding"
	"github.com/rm4n0s/emetos/internal/core/settings"
	"github.com/rm4n0s/emetos/internal/gui/editor"
	"github.com/rm4n0s/emetos/internal/gui/kit"
	"github.com/rm4n0s/emetos/internal/gui/kit/fonts"
	"github.com/rm4n0s/emetos/internal/gui/testutil"
)

// groupApp is a window that shows files in its editor group.
type groupApp struct {
	t     *testing.T
	win   *Window
	ts    *gluttertest.Tester
	files map[string]string // name to path
	docs  map[string]*document.Document
}

// longText is a file taller than the window, so that it can be scrolled.
func longText(n int) string {
	var b strings.Builder
	b.WriteString("package long\n")
	for i := range n {
		fmt.Fprintf(&b, "// line %d\n", i)
	}
	return b.String()
}

// newGroupApp makes the files (one of them, "long.go", is tall) and opens those named in open, in
// that order, as the command line does.
func newGroupApp(t *testing.T, names []string, open ...string) *groupApp {
	t.Helper()
	return newGroupAppWith(t, newTestWindow(t, ""), testWindowSize, names, nil, open...)
}

// newGroupAppWith is newGroupApp over a window, with the text of the files named in texts (a file with
// none gets a short one).
func newGroupAppWith(t *testing.T, win *Window, size ui.Size, names []string, texts map[string]string, open ...string) *groupApp {
	t.Helper()
	dir := t.TempDir()
	a := &groupApp{t: t, win: win, files: map[string]string{}, docs: map[string]*document.Document{}}
	for _, n := range names {
		path := filepath.Join(dir, n)
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatal(err)
		}
		text := "package x\n// " + n + "\n"
		if n == "long.go" {
			text = longText(300)
		}
		if custom, ok := texts[n]; ok {
			text = custom
		}
		if err := os.WriteFile(path, []byte(text), 0o644); err != nil {
			t.Fatal(err)
		}
		a.files[n] = path
	}
	var docs []*document.Document
	for _, n := range open {
		docs = append(docs, a.doc(n))
	}
	a.ts = testutil.New(t, size)
	a.ts.PumpWidget(newAppDocs(context.Background(), win, docs, nil, ""))
	a.pump()
	return a
}

// doc opens a file of the app (not counting it as used by an editor).
func (a *groupApp) doc(name string) *document.Document {
	a.t.Helper()
	if d, ok := a.docs[name]; ok {
		return d
	}
	d, err := a.win.Documents.OpenFile(context.Background(), a.files[name], document.NewOptions())
	if err != nil {
		a.t.Fatal(err)
	}
	a.docs[name] = d
	return d
}

func (a *groupApp) pump() {
	for range 4 {
		a.ts.Pump(20 * time.Millisecond)
	}
}

func (a *groupApp) shell() *shellState {
	return a.ts.Element(gluttertest.ByType[*shell]()).(*widgets.StatefulElement).State().(*shellState)
}

// tabs are the tabs of the strip.
func (a *groupApp) tabs() []kit.TabItem {
	if !a.ts.Exists(gluttertest.ByType[*kit.Tabs]()) {
		return nil
	}
	return a.ts.Widget(gluttertest.ByType[*kit.Tabs]()).(*kit.Tabs).Items()
}

func (a *groupApp) tabNames() []string {
	var out []string
	for _, it := range a.tabs() {
		out = append(out, it.Name)
	}
	return out
}

// active is the name of the active tab, or "".
func (a *groupApp) active() string {
	tabs := a.ts.Widget(gluttertest.ByType[*kit.Tabs]()).(*kit.Tabs)
	if i := tabs.Active(); i >= 0 && i < len(tabs.Items()) {
		return tabs.Items()[i].Name
	}
	return ""
}

func (a *groupApp) item(name string) kit.TabItem {
	a.t.Helper()
	for _, it := range a.tabs() {
		if it.Name == name {
			return it
		}
	}
	a.t.Fatalf("no tab %s in %v", name, a.tabNames())
	return kit.TabItem{}
}

// vm is the view model of the editor that is shown.
func (a *groupApp) vm() *editor.ViewModel {
	return a.ts.Widget(gluttertest.ByType[*editor.Editor]()).(*editor.Editor).ViewModel()
}

func (a *groupApp) tabFinder(name string) gluttertest.Finder {
	return gluttertest.Descendant(gluttertest.ByType[*kit.Tabs](), gluttertest.ByText(name))
}

var mouseButton = gluttertest.GestureOpts{Kind: ui.PointerDeviceKindMouse}

func (a *groupApp) clickTab(name string) {
	a.t.Helper()
	a.ts.TapAt(a.ts.Center(a.tabFinder(name)), mouseButton)
	a.pump()
}

func (a *groupApp) press(keys string) {
	a.t.Helper()
	kb, err := keybinding.ParseKeybinding(keys)
	if err != nil {
		a.t.Fatal(err)
	}
	for i, chord := range kb {
		if !sendChord(a.ts, chord) && i == len(kb)-1 {
			a.t.Errorf("the keys %s were not used", keys)
		}
	}
	a.pump()
}

func (a *groupApp) run(id string) {
	a.t.Helper()
	err := a.win.Process.Commands.Run(command.Context{Ctx: context.Background(), Keys: a.win.Keys, Source: command.SourceAPI}, id, nil)
	if err != nil {
		a.t.Fatalf("%s: %v", id, err)
	}
	a.pump()
}

// show opens a document in the group the way a navigation does (a preview) or the way a command does.
func (a *groupApp) show(name string, preview bool) {
	a.t.Helper()
	doc, err := a.win.Documents.Acquire(context.Background(), a.files[name], document.NewOptions())
	if err != nil {
		a.t.Fatal(err)
	}
	a.docs[name] = doc
	s := a.shell()
	s.show(widgets.WidgetsBindingOf(s.Context()), doc, openOptions{Preview: preview})
	a.pump()
}

func (a *groupApp) setting(key string, value any) {
	a.t.Helper()
	if err := a.win.Settings.Update(context.Background(), key, value, settings.TargetUser); err != nil {
		a.t.Fatal(err)
	}
	a.pump()
}

func (a *groupApp) edit(name string) {
	a.t.Helper()
	if _, err := a.doc(name).Edit([]document.Edit{{Range: buffer.Range{}, Text: "x"}}, document.EditOptions{}); err != nil {
		a.t.Fatal(err)
	}
	a.pump()
}

func wantNames(t *testing.T, got []string, want ...string) {
	t.Helper()
	if !slices.Equal(got, want) {
		t.Errorf("tabs %v, want %v", got, want)
	}
}

func (a *groupApp) noErrors() {
	a.t.Helper()
	if errs := a.ts.TakeErrors(); len(errs) != 0 {
		a.t.Errorf("the framework reported errors: %v", errs)
	}
}

func TestTheFilesOfTheCommandLineAreTabs(t *testing.T) {
	a := newGroupApp(t, []string{"a.go", "b.go", "c.txt"}, "a.go", "b.go", "c.txt")
	wantNames(t, a.tabNames(), "a.go", "b.go", "c.txt")
	if got := a.active(); got != "a.go" {
		t.Errorf("the first file is shown, got %s", got)
	}
	for _, it := range a.tabs() {
		if it.Preview || it.Dirty || it.Sticky {
			t.Errorf("tab %+v: the files of the command line are kept", it)
		}
	}
	if a.vm().Document() != a.doc("a.go") {
		t.Error("the editor shows the first file")
	}
	for name, doc := range a.docs {
		if got := a.win.Documents.Refs(doc); got != 1 {
			t.Errorf("%s has %d users, want its editor", name, got)
		}
	}
	// the tabs and the breadcrumbs are above the text, in that order
	if tabs, bar := a.ts.TopLeft(gluttertest.ByType[*kit.Tabs]()).Dy, a.ts.TopLeft(gluttertest.ByType[*editor.Breadcrumbs]()).Dy; tabs >= bar {
		t.Errorf("tabs at %v, breadcrumbs at %v", tabs, bar)
	}
	a.noErrors()
}

func TestSwitchingTabsKeepsTheViewOfEachEditor(t *testing.T) {
	a := newGroupApp(t, []string{"long.go", "b.go"}, "long.go", "b.go")
	// the long file is scrolled and has a cursor
	a.clickTab("long.go")
	first := a.vm()
	first.Cursors().SetSelections([]buffer.Selection{buffer.NewCursor(buffer.Position{Line: 120, Col: 3})})
	first.SetScrollTop(1500)
	a.pump()
	scrolled := first.ScrollTop()
	if scrolled < 1000 {
		t.Fatalf("the editor did not scroll: %v", scrolled)
	}

	a.clickTab("b.go")
	if a.vm().Document() != a.doc("b.go") || a.active() != "b.go" {
		t.Fatalf("the second file is not shown")
	}
	if a.vm() == first {
		t.Error("the files share a view model")
	}
	a.vm().Cursors().SetSelections([]buffer.Selection{buffer.NewCursor(buffer.Position{Line: 1, Col: 2})})

	a.clickTab("long.go")
	if a.vm() != first {
		t.Fatal("the first editor lost its view model")
	}
	if got := first.Cursors().Primary().Position(); got != (buffer.Position{Line: 120, Col: 3}) {
		t.Errorf("the cursor is at %v", got)
	}
	if got := first.ScrollTop(); got != scrolled {
		t.Errorf("the scroll position is %v, want %v", got, scrolled)
	}
	a.clickTab("b.go")
	if got := a.vm().Cursors().Primary().Position(); got != (buffer.Position{Line: 1, Col: 2}) {
		t.Errorf("the cursor of the second file is at %v", got)
	}
	a.noErrors()
}

func TestPreviewEditorsAreReplacedAndKeptByEditing(t *testing.T) {
	a := newGroupApp(t, []string{"a.go", "b.go", "c.go"}, "a.go")
	a.show("b.go", true)
	wantNames(t, a.tabNames(), "a.go", "b.go")
	if !a.item("b.go").Preview || a.item("a.go").Preview {
		t.Errorf("tabs %+v", a.tabs())
	}
	b := a.doc("b.go")
	a.show("c.go", true)
	wantNames(t, a.tabNames(), "a.go", "c.go")
	if _, open := a.win.Documents.Get(b.URI()); open {
		t.Error("the document of the replaced preview is still open")
	}
	if a.active() != "c.go" || !a.item("c.go").Preview {
		t.Errorf("tabs %+v", a.tabs())
	}
	// typing keeps the preview
	a.edit("c.go")
	if it := a.item("c.go"); it.Preview || !it.Dirty {
		t.Errorf("after typing: %+v", it)
	}
	a.show("b.go", true)
	wantNames(t, a.tabNames(), "a.go", "c.go", "b.go")
	a.noErrors()
}

func TestAPreviewIsKeptByADoubleClickAndByTheKeepCommand(t *testing.T) {
	a := newGroupApp(t, []string{"a.go", "b.go", "c.go"}, "a.go")
	a.show("b.go", true)
	a.ts.TapAt(a.ts.Center(a.tabFinder("b.go")), mouseButton)
	a.ts.Pump(100 * time.Millisecond)
	a.ts.TapAt(a.ts.Center(a.tabFinder("b.go")), mouseButton)
	a.pump()
	if a.item("b.go").Preview {
		t.Error("a double click on a preview tab keeps it")
	}
	a.show("c.go", true)
	if !a.item("c.go").Preview {
		t.Fatal("c.go is not a preview")
	}
	a.press("ctrl+k enter")
	if a.item("c.go").Preview {
		t.Error("the keep command (Ctrl+K Enter) keeps the preview")
	}
	wantNames(t, a.tabNames(), "a.go", "b.go", "c.go")
	a.noErrors()
}

func TestWithPreviewOffEveryEditorIsKept(t *testing.T) {
	a := newGroupApp(t, []string{"a.go", "b.go", "c.go"}, "a.go")
	a.setting("workbench.editor.enablePreview", false)
	a.show("b.go", true)
	a.show("c.go", true)
	wantNames(t, a.tabNames(), "a.go", "b.go", "c.go")
	for _, it := range a.tabs() {
		if it.Preview {
			t.Errorf("%s is a preview", it.Name)
		}
	}
}

func TestClosingTheActiveEditorActivatesTheMostRecentlyUsed(t *testing.T) {
	a := newGroupApp(t, []string{"a.go", "b.go", "c.go", "d.go"}, "a.go", "b.go", "c.go", "d.go")
	a.clickTab("c.go")
	a.clickTab("a.go")
	a.clickTab("d.go")
	a.press("ctrl+w")
	if got := a.active(); got != "a.go" {
		t.Errorf("after closing d.go, active %s, want a.go (the one used before it)", got)
	}
	wantNames(t, a.tabNames(), "a.go", "b.go", "c.go")
	if _, open := a.win.Documents.Get(document.FileURI(a.files["d.go"])); open {
		t.Error("the document of the closed editor is still open")
	}
	// with the setting off the neighbour is activated
	a.setting("workbench.editor.focusRecentEditorAfterClose", false)
	a.clickTab("c.go")
	a.clickTab("a.go")
	a.press("ctrl+w")
	if got := a.active(); got != "b.go" {
		t.Errorf("active %s, want b.go (right of the closed a.go)", got)
	}
	a.noErrors()
}

func TestTheLastClosingEditorShowsTheEmptyState(t *testing.T) {
	a := newGroupApp(t, []string{"a.go"}, "a.go")
	a.press("ctrl+w")
	if a.ts.Exists(gluttertest.ByType[*editor.Editor]()) || a.ts.Exists(gluttertest.ByType[*kit.Tabs]()) {
		t.Error("the editor or the tabs are still there")
	}
	if err := gluttertest.FindsOneWidget(a.ts, gluttertest.ByText("Emetos")); err != nil {
		t.Error("the logo and the name are shown:", err)
	}
	if got := a.win.Title(); got != "Emetos" {
		t.Errorf("the title is %q", got)
	}
	if n := len(a.win.Documents.All()); n != 0 {
		t.Errorf("%d documents are open", n)
	}
	if a.win.Keys.Get("activeEditor") != nil {
		t.Errorf("activeEditor is %v", a.win.Keys.Get("activeEditor"))
	}
	// a file can be opened again
	a.show("a.go", false)
	wantNames(t, a.tabNames(), "a.go")
	a.noErrors()
}

func TestTheWindowTitleFollowsTheActiveEditor(t *testing.T) {
	a := newGroupApp(t, []string{"a.go", "b.go"}, "a.go", "b.go")
	if !strings.HasPrefix(a.win.Title(), "a.go") {
		t.Errorf("title %q", a.win.Title())
	}
	a.clickTab("b.go")
	if !strings.HasPrefix(a.win.Title(), "b.go") {
		t.Errorf("title %q", a.win.Title())
	}
	a.edit("b.go")
	if !strings.HasPrefix(a.win.Title(), "● b.go") {
		t.Errorf("a modified file has the dot: %q", a.win.Title())
	}
	a.clickTab("a.go")
	if !strings.HasPrefix(a.win.Title(), "a.go") {
		t.Errorf("the other file is not modified: %q", a.win.Title())
	}
}

func TestTheTabShowsTheDirtyMarkOfItsDocument(t *testing.T) {
	a := newGroupApp(t, []string{"a.go", "b.go"}, "a.go", "b.go")
	a.edit("b.go")
	if !a.item("b.go").Dirty || a.item("a.go").Dirty {
		t.Errorf("tabs %+v", a.tabs())
	}
	doc := a.doc("b.go")
	for doc.CanUndo() {
		if _, err := doc.Undo(); err != nil {
			t.Fatal(err)
		}
	}
	a.pump()
	if a.item("b.go").Dirty {
		t.Error("undoing the edit leaves the tab modified")
	}
}

func TestDocumentsAreSharedAndCountedByEditors(t *testing.T) {
	a := newGroupApp(t, []string{"a.go", "b.go"}, "a.go")
	doc := a.doc("a.go")
	// opening the file that is shown again activates its tab and keeps one use
	a.show("a.go", false)
	wantNames(t, a.tabNames(), "a.go")
	if got := a.win.Documents.Refs(doc); got != 1 {
		t.Errorf("a.go has %d users, want 1", got)
	}
	// another user (a second group, a diff) keeps the document open when the editor closes
	a.win.Documents.Retain(doc)
	a.press("ctrl+w")
	if _, open := a.win.Documents.Get(doc.URI()); !open {
		t.Error("the document closed while another user had it")
	}
	if closed := a.win.Documents.Release(doc); !closed {
		t.Error("the last release closes the document")
	}
	if _, open := a.win.Documents.Get(doc.URI()); open {
		t.Error("the document is still open")
	}
	// OpenFile counts nobody, and a release does not close what nobody retained
	other := a.doc("b.go")
	if a.win.Documents.Release(other) {
		t.Error("a document nobody retained was closed by a release")
	}
}

func TestTheGroupCommandsAndTheirKeys(t *testing.T) {
	for _, tc := range []struct {
		name string
		keys string // pressed, or the id of a command to run
		cmd  string
		// before sets the tabs up; the files a.go, b.go, c.go and d.go are shown, a.go active, unless it changes that
		before func(a *groupApp)
		want   []string
		active string
	}{
		{name: "Ctrl+PageDown opens the next editor", keys: "ctrl+pagedown", want: []string{"a.go", "b.go", "c.go", "d.go"}, active: "b.go"},
		{name: "Ctrl+PageUp goes round to the last", keys: "ctrl+pageup", want: []string{"a.go", "b.go", "c.go", "d.go"}, active: "d.go"},
		{name: "Ctrl+PageDown goes round to the first", keys: "ctrl+pagedown", before: func(a *groupApp) { a.clickTab("d.go") },
			want: []string{"a.go", "b.go", "c.go", "d.go"}, active: "a.go"},
		{name: "Ctrl+Tab opens the editor used before", keys: "ctrl+tab", before: func(a *groupApp) { a.clickTab("c.go") },
			want: []string{"a.go", "b.go", "c.go", "d.go"}, active: "a.go"},
		{name: "Ctrl+Shift+Tab opens the one used longest ago", keys: "ctrl+shift+tab", before: func(a *groupApp) { a.clickTab("c.go") },
			want: []string{"a.go", "b.go", "c.go", "d.go"}, active: "b.go"},
		{name: "Ctrl+W closes the editor", keys: "ctrl+w", want: []string{"b.go", "c.go", "d.go"}, active: "d.go"},
		{name: "Ctrl+K Ctrl+W closes all editors but the pinned ones", keys: "ctrl+k ctrl+w",
			before: func(a *groupApp) { a.press("ctrl+k shift+enter") }, want: []string{"a.go"}, active: "a.go"},
		{name: "Ctrl+K W closes the editors of the group but the pinned ones", keys: "ctrl+k w",
			before: func(a *groupApp) { a.clickTab("b.go"); a.press("ctrl+k shift+enter") }, want: []string{"b.go"}, active: "b.go"},
		{name: "Close All Editors with nothing pinned", keys: "ctrl+k ctrl+w", want: nil},
		{name: "close other editors", cmd: "workbench.action.closeOtherEditors", before: func(a *groupApp) { a.clickTab("c.go") },
			want: []string{"c.go"}, active: "c.go"},
		{name: "close editors to the right", cmd: "workbench.action.closeEditorsToTheRight", before: func(a *groupApp) { a.clickTab("b.go") },
			want: []string{"a.go", "b.go"}, active: "b.go"},
		{name: "close saved editors", cmd: "workbench.action.closeUnmodifiedEditors", before: func(a *groupApp) { a.edit("b.go"); a.edit("d.go") },
			want: []string{"b.go", "d.go"}, active: "d.go"},
		{name: "Ctrl+K Shift+Enter pins the tab, which goes first", keys: "ctrl+k shift+enter", before: func(a *groupApp) { a.clickTab("c.go") },
			want: []string{"c.go", "a.go", "b.go", "d.go"}, active: "c.go"},
		{name: "Ctrl+K Shift+Enter again unpins it", keys: "ctrl+k shift+enter",
			before: func(a *groupApp) { a.clickTab("c.go"); a.press("ctrl+k shift+enter") },
			want:   []string{"c.go", "a.go", "b.go", "d.go"}, active: "c.go"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			a := newGroupApp(t, []string{"a.go", "b.go", "c.go", "d.go"}, "a.go", "b.go", "c.go", "d.go")
			if tc.before != nil {
				tc.before(a)
			}
			if tc.keys != "" {
				a.press(tc.keys)
			} else {
				a.run(tc.cmd)
			}
			wantNames(t, a.tabNames(), tc.want...)
			if tc.active != "" && a.active() != tc.active {
				t.Errorf("active %s, want %s", a.active(), tc.active)
			}
			a.noErrors()
		})
	}
}

func TestPinnedTabsKeepTheirPlace(t *testing.T) {
	a := newGroupApp(t, []string{"a.go", "b.go", "c.go"}, "a.go", "b.go", "c.go")
	a.clickTab("c.go")
	a.press("ctrl+k shift+enter")
	if !a.item("c.go").Sticky {
		t.Fatal("the tab is not pinned")
	}
	if a.win.Keys.Get("activeEditorIsPinned") != true {
		t.Error("activeEditorIsPinned is not set")
	}
	// a file opened later goes after the pinned tabs, even when the first tab is the active one
	a.setting("workbench.editor.openPositioning", "first")
	a.show("a.go", false)
	wantNames(t, a.tabNames(), "c.go", "a.go", "b.go")
	// the pin on the tab unpins it
	a.ts.TapAt(a.ts.Center(gluttertest.ByIcon(fonts.CodiconIcon(fonts.CodiconPinned))), mouseButton)
	a.pump()
	if a.item("c.go").Sticky {
		t.Error("clicking the pin leaves the tab pinned")
	}
}

func TestTheTabsReactToTheMouse(t *testing.T) {
	a := newGroupApp(t, []string{"a.go", "b.go", "c.go"}, "a.go", "b.go", "c.go")
	// the middle button closes
	a.ts.TapAt(a.ts.Center(a.tabFinder("b.go")), gluttertest.GestureOpts{Kind: ui.PointerDeviceKindMouse, Buttons: foundation.Some(gestures.TertiaryButton)})
	a.pump()
	wantNames(t, a.tabNames(), "a.go", "c.go")
	// the close button closes: it shows under the pointer
	a.ts.Hover(a.ts.Center(a.tabFinder("c.go")))
	a.ts.Pump(time.Millisecond)
	a.ts.TapAt(a.ts.Center(gluttertest.At(gluttertest.ByIcon(fonts.CodiconIcon(fonts.CodiconClose)), 1)), mouseButton)
	a.pump()
	wantNames(t, a.tabNames(), "a.go")
	if a.active() != "a.go" {
		t.Errorf("active %s", a.active())
	}
	a.noErrors()
}

func TestShowTabs(t *testing.T) {
	a := newGroupApp(t, []string{"a.go", "b.go"}, "a.go", "b.go")
	a.setting("workbench.editor.showTabs", "single")
	if a.ts.Exists(a.tabFinder("b.go")) || !a.ts.Exists(a.tabFinder("a.go")) {
		t.Errorf("single mode shows the title of the active editor only")
	}
	if got := a.ts.Size(gluttertest.ByType[*kit.Tabs]()).Height; got != kit.TabHeight {
		t.Errorf("the header is %v high", got)
	}
	a.setting("workbench.editor.showTabs", "none")
	if a.ts.Exists(gluttertest.ByType[*kit.Tabs]()) {
		t.Error("none shows no title area")
	}
	if !a.ts.Exists(gluttertest.ByType[*editor.Editor]()) {
		t.Error("the editor is still shown")
	}
	a.setting("workbench.editor.showTabs", "multiple")
	wantNames(t, a.tabNames(), "a.go", "b.go")
	a.noErrors()
}

func TestTabSettings(t *testing.T) {
	for _, tc := range []struct {
		name     string
		settings map[string]any
		check    func(o kit.TabsOpts) bool
	}{
		{"defaults", nil, func(o kit.TabsOpts) bool {
			return o.CloseButton == kit.TabCloseRight && o.PinnedSizing == kit.PinnedNormal && !o.HighlightModified && o.Mode == kit.TabsMultiple
		}},
		{"close button off", map[string]any{"workbench.editor.tabCloseButton": "off"}, func(o kit.TabsOpts) bool { return o.CloseButton == kit.TabCloseOff }},
		{"close button left", map[string]any{"workbench.editor.tabCloseButton": "left"}, func(o kit.TabsOpts) bool { return o.CloseButton == kit.TabCloseLeft }},
		{"action location left", map[string]any{"workbench.editor.tabActionLocation": "left"}, func(o kit.TabsOpts) bool { return o.CloseButton == kit.TabCloseLeft }},
		{"close action hidden", map[string]any{"workbench.editor.tabActionCloseVisibility": false}, func(o kit.TabsOpts) bool { return o.CloseButton == kit.TabCloseOff }},
		{"compact pinned tabs", map[string]any{"workbench.editor.pinnedTabSizing": "compact"}, func(o kit.TabsOpts) bool { return o.PinnedSizing == kit.PinnedCompact }},
		{"shrunk pinned tabs", map[string]any{"workbench.editor.pinnedTabSizing": "shrink"}, func(o kit.TabsOpts) bool { return o.PinnedSizing == kit.PinnedShrink }},
		{"highlighted modified tabs", map[string]any{"workbench.editor.highlightModifiedTabs": true}, func(o kit.TabsOpts) bool { return o.HighlightModified }},
		{"a single title", map[string]any{"workbench.editor.showTabs": "single"}, func(o kit.TabsOpts) bool { return o.Mode == kit.TabsSingle }},
	} {
		t.Run(tc.name, func(t *testing.T) {
			a := newGroupApp(t, []string{"a.go"}, "a.go")
			for k, v := range tc.settings {
				a.setting(k, v)
			}
			if o := a.shell().tabsOptions(a.win, a.shell().activeGroup()); !tc.check(o) {
				t.Errorf("options %+v", o)
			}
		})
	}
}

func TestClosingADirtyEditorDoesNotAskYet(t *testing.T) {
	a := newGroupApp(t, []string{"a.go", "b.go"}, "a.go", "b.go")
	a.edit("a.go")
	a.press("ctrl+w")
	wantNames(t, a.tabNames(), "b.go")
	if !a.shell().confirmClose(nil) {
		t.Error("confirmClose says no: the prompt of step 7.2 does not exist yet")
	}
}

func TestTheContextKeysOfTheGroup(t *testing.T) {
	a := newGroupApp(t, []string{"a.go", "b.go"}, "a.go")
	get := func(k string) any { return a.win.Keys.Get(k) }
	if get("activeEditor") != editorgroup.TextInputTypeID || get("activeEditorIsPinned") != false || get("activeEditorIsNotPreview") != true {
		t.Errorf("keys %v %v %v", get("activeEditor"), get("activeEditorIsPinned"), get("activeEditorIsNotPreview"))
	}
	a.show("b.go", true)
	if get("activeEditorIsNotPreview") != false {
		t.Error("a preview is not pinned")
	}
	a.press("ctrl+k enter")
	if get("activeEditorIsNotPreview") != true {
		t.Error("the keep command pins it")
	}
	a.press("ctrl+k shift+enter")
	if get("activeEditorIsPinned") != true {
		t.Error("the pin command sticks it")
	}
}

func TestCloseEditorInTheFileMenu(t *testing.T) {
	a := newGroupApp(t, []string{"a.go"}, "a.go")
	s := a.shell()
	items := s.menus.Items(command.MenubarFileMenu, s.menuContext())
	i := slices.IndexFunc(items, func(it kit.MenuItem) bool { return it.ID == "workbench.action.closeActiveEditor" })
	if i < 0 || items[i].Disabled {
		t.Fatalf("the File menu has no enabled Close Editor: %+v", items)
	}
	s.menus.Run(items[i])
	a.pump()
	a.pump()
	if len(a.tabs()) != 0 {
		t.Errorf("the editor is still open: %v", a.tabNames())
	}
	if items = s.menus.Items(command.MenubarFileMenu, s.menuContext()); slices.ContainsFunc(items, func(it kit.MenuItem) bool { return !it.Disabled && it.ID == "workbench.action.closeActiveEditor" }) {
		t.Error("Close Editor is enabled with no editor open")
	}
}

func TestABinaryFileWithEditorsOpenIsReported(t *testing.T) {
	a := newGroupApp(t, []string{"a.go"}, "a.go")
	a.shell().blockFile("/some/data.bin")
	a.pump()
	if !a.ts.Exists(gluttertest.ByType[*editor.Editor]()) {
		t.Error("the editor was replaced by the notice")
	}
	items := a.win.Process.Notify.Items()
	if len(items) != 1 || !strings.Contains(items[0].Message, "data.bin") {
		t.Errorf("notifications %+v", items)
	}
}

func TestEditingStopsAtTheLastEditorOfTheFile(t *testing.T) {
	// the title and the tabs follow a document that is edited in the editor the user types in
	a := newGroupApp(t, []string{"a.go"}, "a.go")
	vm := a.vm()
	if err := vm.RunCommand("type", map[string]any{"text": "z"}); err != nil {
		t.Fatal(err)
	}
	a.pump()
	if !a.item("a.go").Dirty {
		t.Error("typing in the editor leaves the tab clean")
	}
}

func TestGroupHandlersOfACommandTheRegistryDoesNotKnow(t *testing.T) {
	reg := command.NewRegistry()
	_, err := setGroupHandlers(reg, map[string]command.Handler{
		"workbench.action.nextEditor": func(command.Context, any) error { return nil },
	})
	appErr, ok := errors.FromError(err)
	if !ok {
		t.Fatalf("expected *errors.Error, got %T (%v)", err, err)
	}
	if !appErr.HasRoute("setGroupHandlers.EditorGroupCommandMissing") {
		t.Errorf("unexpected failure path: %s", appErr.Route())
	}
}
