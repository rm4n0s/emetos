package app

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"slices"
	"strings"
	"sync"
	"testing"

	"github.com/rm4n0s/errors"
	"github.com/rm4n0s/glutter/gluttertest"
	"github.com/rm4n0s/glutter/widgets"

	"github.com/rm4n0s/emetos/extension"
	extui "github.com/rm4n0s/emetos/extension/ui"
	"github.com/rm4n0s/emetos/internal/core/buffer"
	"github.com/rm4n0s/emetos/internal/core/document"
	"github.com/rm4n0s/emetos/internal/core/editorgroup"
	"github.com/rm4n0s/emetos/internal/gui/editor"
	"github.com/rm4n0s/emetos/internal/gui/kit"
)

// The contributions of extensions to the editor area in the real workbench (step 11.6): editor tabs, editor title actions
// and file views.

// exState is what the test extension "ex" keeps; a test reads it.
type exState struct {
	mu                   sync.Mutex
	api                  extension.API
	cards                []*exCard
	runs                 []any
	views                map[string]*exView
	opens                int
	failOpen, panicBuild bool
}

// exCard is the content of an editor tab of the kind ex.card.
type exCard struct {
	tab   extension.EditorTab
	state string
}

// exView is a file view of ex in one tab.
type exView struct {
	doc extension.DocumentHandle
	tab extension.FileViewTab
}

func exFactory(st *exState) extension.Factory {
	return func() extension.Extension { return &exExt{st: st} }
}

type exExt struct{ st *exState }

func (e *exExt) Manifest() extension.Manifest {
	m := extension.Manifest{ID: "ex", Name: "Ex", Version: "1.0.0"}
	m.Contributes.Commands = []extension.CommandSpec{{ID: "ex.newCard", Title: "New Card"}, {ID: "ex.markdown", Title: "Markdown Only"}}
	m.Contributes.EditorKinds = []extension.EditorKindSpec{{ID: "ex.card", Name: "Card", Icon: "note"}}
	m.Contributes.EditorActions = []extension.EditorActionSpec{
		{Command: "ex.newCard", Icon: "new-file", Tooltip: "New Card", Order: 1},
		{Command: "ex.markdown", Icon: "open-preview", Tooltip: "Markdown Only", When: "resourceLangId == markdown", Order: 2},
	}
	for i := range 12 {
		id := fmt.Sprintf("ex.fill%d", i)
		m.Contributes.Commands = append(m.Contributes.Commands, extension.CommandSpec{ID: id, Title: id})
		m.Contributes.EditorActions = append(m.Contributes.EditorActions, extension.EditorActionSpec{Command: id, Icon: "add", Tooltip: id, When: "ex.fill", Order: float64(10 + i)})
	}
	m.Contributes.FileViews = []extension.FileViewSpec{
		{ID: "ex.upper", Name: "Upper", Icon: "preview", Languages: []string{"markdown"}, Order: 1},
		{ID: "ex.plain", Name: "Plain", Globs: []string{"*.txt"}},
	}
	return m
}

func (e *exExt) Activate(_ context.Context, api extension.API) error {
	st := e.st
	st.mu.Lock()
	st.api = api
	st.views = map[string]*exView{}
	st.mu.Unlock()
	err := api.Editors().Provide("ex.card", extension.EditorKind{Create: func(tab extension.EditorTab, state json.RawMessage) (extension.EditorTabContent, error) {
		c := &exCard{tab: tab}
		if len(state) > 0 {
			if err := json.Unmarshal(state, &c.state); err != nil {
				return extension.EditorTabContent{}, err
			}
		}
		st.mu.Lock()
		st.cards = append(st.cards, c)
		st.mu.Unlock()
		tab.SetTitle("Card " + c.state)
		return extension.EditorTabContent{
			Build: func(bc widgets.BuildContext) widgets.Widget {
				return widgets.NewText("card:"+c.state, widgets.TextOpts{Style: extui.TextStyle(bc, 13, extui.Theme(bc).Foreground)})
			},
			SaveState: func() (any, error) { return c.state, nil },
		}, nil
	}})
	if err != nil {
		return err
	}
	for _, id := range []string{"ex.markdown"} {
		if err := api.Commands().Handle(id, func(_ context.Context, args any) error {
			st.mu.Lock()
			st.runs = append(st.runs, args)
			st.mu.Unlock()
			return nil
		}); err != nil {
			return err
		}
	}
	for i := range 12 {
		if err := api.Commands().Handle(fmt.Sprintf("ex.fill%d", i), func(_ context.Context, args any) error {
			st.mu.Lock()
			st.runs = append(st.runs, args)
			st.mu.Unlock()
			return nil
		}); err != nil {
			return err
		}
	}
	if err := api.Commands().Handle("ex.newCard", func(_ context.Context, args any) error {
		st.mu.Lock()
		st.runs = append(st.runs, args)
		n := len(st.cards) + 1
		st.mu.Unlock()
		_, err := api.Editors().Open("ex.card", fmt.Sprintf("c%d", n), extension.EditorOpenOptions{Group: extension.GroupOfArgs(args)})
		return err
	}); err != nil {
		return err
	}
	open := func(id string, render func(extension.DocumentHandle) string) extension.FileViewProvider {
		return extension.FileViewProvider{Open: func(doc extension.DocumentHandle, tab extension.FileViewTab) (extension.FileViewContent, error) {
			st.mu.Lock()
			st.opens++
			fail, boom := st.failOpen, st.panicBuild
			st.views[id] = &exView{doc: doc, tab: tab}
			st.mu.Unlock()
			if fail {
				return extension.FileViewContent{}, errors.New("OpenFailed", "cannot open")
			}
			stop := doc.OnChange(func(extension.ViewDocumentChange) { tab.Refresh() })
			return extension.FileViewContent{
				Build: func(bc widgets.BuildContext) widgets.Widget {
					if boom {
						panic("build")
					}
					return widgets.NewText(render(doc), widgets.TextOpts{Style: extui.TextStyle(bc, 13, extui.Theme(bc).Foreground)})
				},
				Close: stop,
			}, nil
		}}
	}
	if err := api.FileViews().Provide("ex.upper", open("ex.upper", func(d extension.DocumentHandle) string {
		return "UPPER:" + strings.ToUpper(strings.SplitN(d.Text(), "\n", 2)[0])
	})); err != nil {
		return err
	}
	return api.FileViews().Provide("ex.plain", open("ex.plain", func(d extension.DocumentHandle) string { return "PLAIN:" + strings.SplitN(d.Text(), "\n", 2)[0] }))
}

// exEnv starts windows over a project with the extension ex.
func exEnv(t *testing.T, settingsJSON map[string]any) (*stateEnv, *exState) {
	t.Helper()
	st := &exState{}
	return newStateEnv(t, settingsJSON), st
}

func (e *stateEnv) startEx(st *exState) *stateRun {
	e.t.Helper()
	return e.startWith(e.root, ProcessOptions{Extensions: []extension.Factory{exFactory(st)}})
}

func (r *stateRun) has(text string) bool { return r.ts.Exists(gluttertest.ByText(text)) }

// activeInput is the active editor of the active group.
func (r *stateRun) activeInput() editorgroup.EditorInput {
	return r.shell().group.grid.Active().Model.ActiveEditor()
}

// stripButtons are the tooltips of the title actions of the strip of the group at an index of the grid.
func (r *stateRun) stripButtons(index int) []string {
	var out []string
	tabs := gluttertest.At(editorTabs(), index)
	for _, e := range r.ts.Elements(gluttertest.Descendant(tabs, gluttertest.ByType[*kit.IconButton]())) {
		if b, ok := e.Widget().(*kit.IconButton); ok && b.Tooltip() != "" {
			out = append(out, b.Tooltip())
		}
	}
	return out
}

// iconButton finds the icon button with a tooltip.
func iconButton(tooltip string) gluttertest.Finder {
	return gluttertest.ByPredicate("the icon button "+tooltip, func(w widgets.Widget) bool {
		b, ok := w.(*kit.IconButton)
		return ok && b != nil && b.Tooltip() == tooltip
	})
}

// microTabs are the labels of the micro-tabs above the editor, or nil when the file has none.
func (r *stateRun) microTabs(labels ...string) []string {
	var out []string
	for _, l := range labels {
		if r.ts.Exists(gluttertest.Descendant(gluttertest.ByType[*kit.Hover](), gluttertest.ByText(l))) || r.has(l) {
			out = append(out, l)
		}
	}
	return out
}

func TestEditorTabsOpenSplitMoveCloseAndComeBack(t *testing.T) {
	env, st := exEnv(t, nil)
	first := env.startEx(st)
	first.command("ex.newCard", nil)
	first.command("ex.newCard", nil)
	if got := first.layout(); !slices.Equal(got, []string{"1!: Card c1, Card c2*"}) {
		t.Fatalf("after two cards: %v", got)
	}
	if !first.has("card:c2") || len(st.cards) != 2 {
		t.Fatalf("the active card is not shown (%d cards)", len(st.cards))
	}
	// the state of a card is its own
	st.cards[1].state = "c2 changed"
	// a split makes a card in the new group with the state the first one has
	first.command("workbench.action.splitEditorRight", nil)
	if got := first.layout(); len(got) != 2 || !strings.HasPrefix(got[1], "2!: ") {
		t.Fatalf("after the split: %v", got)
	}
	if !first.has("card:c2 changed") || len(st.cards) != 3 || st.cards[2].state != "c2 changed" {
		t.Errorf("the copy did not get the state: %d cards, last %q", len(st.cards), st.cards[len(st.cards)-1].state)
	}
	// move the copy back to the first group, then close it
	first.command("workbench.action.moveEditorToPreviousGroup", nil)
	if got := first.layout(); len(got) != 1 || !strings.Contains(got[0], "Card c2 changed") {
		t.Fatalf("after the move: %v", got)
	}
	first.command("workbench.action.closeActiveEditor", nil)
	if got := first.layout(); !slices.Equal(got, []string{"1!: Card c1, Card c2*"}) {
		t.Fatalf("after the close: %v", got)
	}
	first.close()

	// a new run: the tabs come back with what they saved, as the extension is activated by showing them
	st2 := &exState{}
	second := env.startEx(st2)
	// the title the tab had is its own, and the extension names it again from the state it gets
	if got := second.layout(); !slices.Equal(got, []string{"1!: Card c1, Card c2 changed*"}) {
		t.Fatalf("restored: %v", got)
	}
	if !second.has("card:c2 changed") && !second.has("card:c2") {
		t.Errorf("the active card is not shown after the restart")
	}
	if len(st2.cards) == 0 || st2.cards[0].state == "" {
		t.Errorf("the cards of the new run got no state: %+v", st2.cards)
	}
}

func TestTheMarksOfAnEditorTabShowInItsTab(t *testing.T) {
	env, st := exEnv(t, nil)
	r := env.startEx(st)
	r.command("ex.newCard", nil)
	st.cards[0].tab.SetDirty(true)
	st.cards[0].tab.SetBusy(true)
	r.pump()
	items := r.shell().tabItems(kit.ThemeOf(r.ts.Element(gluttertest.ByType[*shell]())), r.shell().group.grid.Active())
	_ = items
	in := r.activeInput().(*editorgroup.ExtensionTabInput)
	if !in.Dirty() || !in.Busy() {
		t.Errorf("the input has dirty %v busy %v", in.Dirty(), in.Busy())
	}
	// closing a dirty tab asks nothing
	r.command("workbench.action.closeActiveEditor", nil)
	if got := r.layout(); !slices.Equal(got, []string{"1!: "}) {
		t.Errorf("a dirty extension tab did not close: %v", got)
	}
}

func TestTitleActionsAppearByTheirConditionAndRunInTheirGroup(t *testing.T) {
	env, st := exEnv(t, nil)
	r := env.startEx(st)
	r.open(env.path("docs/c.md"), 1)
	r.command("workbench.action.splitEditorRight", nil)
	r.open(env.path("main.go"), 2)
	if got, want := r.stripButtons(0), []string{"New Card", "Markdown Only"}; !slices.Equal(got, want) {
		t.Errorf("the strip of the Markdown group has %v, want %v", got, want)
	}
	if got, want := r.stripButtons(1), []string{"New Card"}; !slices.Equal(got, want) {
		t.Errorf("the strip of the Go group has %v, want %v", got, want)
	}
	// the icon of the second group runs its command for the second group, which is not the active one after a click
	r.command("workbench.action.focusFirstEditorGroup", nil)
	tap := gluttertest.Descendant(gluttertest.At(editorTabs(), 1), iconButton("New Card"))
	r.ts.Tap(tap)
	r.pump()
	if len(st.runs) == 0 {
		t.Fatal("the icon ran no command")
	}
	if group := extension.GroupOfArgs(st.runs[len(st.runs)-1]); group != 2 {
		t.Errorf("the command ran for the group %d, want 2", group)
	}
	if got := r.layout(); len(got) != 2 || !strings.Contains(got[1], "Card c1") {
		t.Errorf("the card did not open in the group of the icon: %v", got)
	}
}

func TestTitleActionsThatDoNotFitGoIntoAMenu(t *testing.T) {
	env, st := exEnv(t, nil)
	r := env.startEx(st)
	r.open(env.path("main.go"), 1)
	r.command("ex.markdown", nil) // activates the extension
	// the narrow group fits few icons: split until it is narrow
	for range 3 {
		r.command("workbench.action.splitEditorRight", nil)
	}
	if err := st.api.Context().Set("ex.fill", true); err != nil {
		t.Fatal(err)
	}
	r.pump()
	buttons := r.stripButtons(3)
	if !slices.Contains(buttons, "More Actions...") {
		t.Fatalf("the narrow strip has no overflow button: %v", buttons)
	}
	if len(buttons) >= 14 {
		t.Errorf("the strip shows all %d icons", len(buttons))
	}
	r.ts.Tap(gluttertest.Descendant(gluttertest.At(editorTabs(), 3), iconButton("More Actions...")))
	r.pump()
	if !r.has("ex.fill11") {
		t.Error("the menu does not list the last action")
	}
	r.ts.Tap(gluttertest.ByText("ex.fill11"))
	r.pump()
	if len(st.runs) == 0 || extension.GroupOfArgs(st.runs[len(st.runs)-1]) != 4 {
		t.Errorf("the menu item ran with %v, want the group 4", st.runs)
	}
}

func TestMicroTabsShowOnlyForTheFilesAViewSelectsAndGoWithTheExtension(t *testing.T) {
	env, st := exEnv(t, nil)
	r := env.startEx(st)
	r.open(env.path("docs/c.md"), 1)
	if got := r.microTabs("Text", "Upper"); !slices.Equal(got, []string{"Text", "Upper"}) {
		t.Errorf("a Markdown file shows the micro-tabs %v", got)
	}
	r.open(env.path("main.go"), 1)
	if got := r.microTabs("Upper", "Plain"); len(got) != 0 {
		t.Errorf("a Go file shows the micro-tabs %v", got)
	}
	r.open(env.path("e.txt"), 1)
	if got := r.microTabs("Plain"); len(got) != 1 {
		t.Errorf("a text file shows the micro-tabs %v", got)
	}
	// the extension is turned off: no micro-tab
	if err := r.win.setExtensionEnabled("ex", false); err != nil {
		t.Fatal(err)
	}
	r.pump()
	r.pump()
	if got := r.microTabs("Plain", "Text"); len(got) != 0 {
		t.Errorf("a disabled extension still has micro-tabs %v", got)
	}
	if err := r.win.setExtensionEnabled("ex", true); err != nil {
		t.Fatal(err)
	}
	r.pump()
	r.pump()
	if got := r.microTabs("Plain"); len(got) != 1 {
		t.Errorf("the micro-tab did not come back with the extension: %v", got)
	}
}

func TestSwitchingViewsKeepsTheDocumentAndItsUndoAndEditsGoThroughTheDocument(t *testing.T) {
	env, st := exEnv(t, nil)
	r := env.startEx(st)
	r.open(env.path("docs/c.md"), 1)
	ed := r.editorOf(1, "c.md")
	r.command("workbench.action.cycleFileView", nil)
	if !r.has("UPPER:PACKAGE LONG") {
		t.Fatal("the Upper view is not shown")
	}
	// an edit from the view: through the document, so it is dirty, undoable and shown by the view
	view := st.views["ex.upper"]
	if err := view.doc.Edit([]extension.TextEdit{{StartLine: 0, StartColumn: 0, EndLine: 0, EndColumn: 7, Text: "module"}}); err != nil {
		t.Fatal(err)
	}
	r.pump()
	if !ed.doc.Dirty() || !strings.HasPrefix(ed.doc.Text(), "module long") || !r.has("UPPER:MODULE LONG") {
		t.Errorf("the edit did not reach the document (dirty %v, %q) or the view", ed.doc.Dirty(), ed.doc.Text()[:20])
	}
	// back to Text: the same document, the undo undoes the edit
	r.command("workbench.action.cycleFileView", nil)
	if r.has("UPPER:MODULE LONG") || !ed.doc.Dirty() {
		t.Error("the view is still shown, or the document lost its change")
	}
	if _, err := ed.doc.Undo(); err != nil {
		t.Fatal(err)
	}
	if ed.doc.Dirty() || !strings.HasPrefix(ed.doc.Text(), "package long") {
		t.Errorf("the undo did not restore the document: %q", ed.doc.Text()[:20])
	}
	_ = buffer.Position{}
}

func TestTheDefaultFileViewSettingChoosesTheViewOfANewEditor(t *testing.T) {
	env, st := exEnv(t, map[string]any{"workbench.editor.defaultFileViews": map[string]any{"*.md": "ex.upper", "*.txt": "ex.missing"}})
	r := env.startEx(st)
	r.open(env.path("docs/c.md"), 1)
	if !r.has("UPPER:PACKAGE LONG") {
		t.Error("the Markdown file did not open in the view of the setting")
	}
	// a view the file does not have is ignored: Text
	r.open(env.path("e.txt"), 1)
	if r.has("PLAIN:PACKAGE LONG") || r.shell().activeEditor().fileView != "" {
		t.Error("a view that is not there was chosen")
	}
}

func TestTheChosenFileViewComesBackAfterARestart(t *testing.T) {
	env, st := exEnv(t, nil)
	first := env.startEx(st)
	first.open(env.path("docs/c.md"), 1)
	first.open(env.path("e.txt"), 1)
	first.command("workbench.action.cycleFileView", nil) // e.txt: Plain
	first.command("workbench.action.previousEditor", nil)
	first.command("workbench.action.cycleFileView", nil) // c.md: Upper
	first.close()
	second := env.startEx(&exState{})
	if second.editorOf(1, "c.md").fileView != "ex.upper" || second.editorOf(1, "e.txt").fileView != "ex.plain" {
		t.Errorf("the views after the restart are %q and %q", second.editorOf(1, "c.md").fileView, second.editorOf(1, "e.txt").fileView)
	}
	if got := second.shell().activeEditor().doc.URI().Path; !strings.HasSuffix(got, "c.md") || !second.has("UPPER:PACKAGE LONG") {
		t.Errorf("the active file %s does not show its view", got)
	}
}

func TestAFileViewThatFailsFallsBackToText(t *testing.T) {
	for _, tc := range []struct {
		name string
		set  func(*exState)
	}{
		{"an error from Open", func(st *exState) { st.failOpen = true }},
		{"a panic in Build", func(st *exState) { st.panicBuild = true }},
	} {
		t.Run(tc.name, func(t *testing.T) {
			env, st := exEnv(t, nil)
			tc.set(st)
			r := env.startEx(st)
			r.open(env.path("docs/c.md"), 1)
			r.command("workbench.action.cycleFileView", nil)
			for range 3 {
				r.pump()
			}
			if r.has("UPPER:PACKAGE LONG") {
				t.Fatal("the failed view is shown")
			}
			if len(r.ts.Elements(gluttertest.ByType[*editor.Editor]())) == 0 {
				t.Error("the text is not shown after the failure")
			}
			if tc.name == "a panic in Build" {
				if state := r.shell().win.Extensions().State("ex"); state.String() != "failed" {
					t.Errorf("the extension is %v", state)
				}
				if got := r.microTabs("Upper"); len(got) != 0 {
					t.Errorf("the micro-tab of a failed extension is still there: %v", got)
				}
			}
		})
	}
}

func TestEditorStateCodingKeepsOldStatesReadable(t *testing.T) {
	view := json.RawMessage(`{"version":1,"selections":[]}`)
	tests := []struct {
		name     string
		viewS    json.RawMessage
		fileView string
	}{
		{"text only", view, ""},
		{"a view", view, "ex.upper"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			raw := encodeEditorState(tt.viewS, tt.fileView)
			gotView, gotFile := decodeEditorState(raw)
			if gotFile != tt.fileView || string(gotView) != string(tt.viewS) {
				t.Errorf("decode(encode) = %s, %q", gotView, gotFile)
			}
		})
	}
	if tt := encodeEditorState(view, ""); string(tt) != string(view) {
		t.Errorf("a state with no view changed: %s", tt)
	}
}

// appRoute asserts the route of an error.
func appRoute(t *testing.T, err error, want string) {
	t.Helper()
	appErr, ok := errors.FromError(err)
	if !ok {
		t.Fatalf("expected *errors.Error, got %T (%v)", err, err)
	}
	if !appErr.HasRoute(want) {
		t.Errorf("unexpected failure path: %s (want %s)", appErr.Route(), want)
	}
}

func TestTheWindowRefusesWhatItCannotShow(t *testing.T) {
	win := newTestWorkspace(t, "")
	host := extEditorHost{w: win}
	in := editorgroup.NewExtensionTabInput("ex", "ex.card", "t1", "Card", "")
	appRoute(t, host.OpenTab(in, 0, false), "extEditorHost.OpenTab->extEditorHost.shellOrErr.ExtEditorsUnavailable")
	appRoute(t, win.openURI(context.Background(), "https://example.com"), "Workspace.openURI.ExtOpenURIUnavailable")
	if host.GroupOf(in) != 0 {
		t.Error("a window that is not on screen holds a tab")
	}
	host.CloseTab(in)
	host.RevealTab(in, true)
}

func TestOpeningAFileViewNeedsAFile(t *testing.T) {
	env, st := exEnv(t, nil)
	r := env.startEx(st)
	host := extEditorHost{w: r.win}
	appRoute(t, host.OpenFileView(document.URI{Scheme: document.SchemeUntitled, Path: "1"}.String(), "ex.upper", false, 0), "extEditorHost.OpenFileView.ExtEditorsFileOnly")
	// a file of the folder opens in the view, in the group on the side
	if err := host.OpenFileView(extURI(env.path("docs/c.md")), "ex.upper", true, 0); err != nil {
		t.Fatal(err)
	}
	for range 5 {
		r.pump()
	}
	if got := r.layout(); len(got) != 2 || !strings.Contains(got[1], "c.md") {
		t.Fatalf("the file did not open on the side: %v", got)
	}
	if e := r.shell().activeEditor(); e == nil || e.fileView != "ex.upper" || !r.has("UPPER:PACKAGE LONG") {
		t.Errorf("the file is not shown in the view")
	}
	// an empty URI is the file of the active editor of the group; an empty group has none
	r.command("workbench.action.focusFirstEditorGroup", nil)
	appRoute(t, host.OpenFileView("", "ex.upper", false, 0), "extEditorHost.OpenFileView.ExtEditorsNoDocument")
	r.open(env.path("e.txt"), 1)
	if err := host.OpenFileView("", "ex.plain", false, 0); err != nil {
		t.Fatal(err)
	}
	for range 5 {
		r.pump()
	}
	if e := r.shell().activeEditor(); e == nil || e.fileView != "ex.plain" {
		t.Errorf("the file of the group is not in the view")
	}
}

// extURI is the URI of a path.
func extURI(path string) string { return "file://" + path }

func TestADocumentHandleOpensFilesOfTheFolderAndReadsTheScroll(t *testing.T) {
	a := newGroupApp(t, []string{"long.go", "b.go"}, "long.go")
	e := a.shell().activeEditor()
	h := e.handle(a.shell())
	if h.LanguageID() != "go" || !strings.HasPrefix(h.Text(), "package long") || h.Version() == 0 || h.ReadOnly() || h.Folder() != "" {
		t.Errorf("the handle reads %q %q %d", h.LanguageID(), h.Text()[:12], h.Version())
	}
	appRoute(t, h.OpenFile("relative.go"), "docHandle.OpenFile.ExtFileViewNoFolder")
	if h.TopLine() != 0 {
		t.Errorf("the top line of an editor that did not scroll is %d", h.TopLine())
	}
	var lines []int
	stop := h.OnScroll(func(line int) { lines = append(lines, line) })
	a.vm().SetScrollTop(a.vm().Projection().RowTop(40))
	a.pump()
	if h.TopLine() != 40 || len(lines) == 0 || lines[len(lines)-1] != 40 {
		t.Errorf("after the scroll the top line is %d and the callbacks got %v", h.TopLine(), lines)
	}
	stop()
	n := len(lines)
	a.vm().SetScrollTop(0)
	a.pump()
	if len(lines) != n {
		t.Error("the callback ran after it was stopped")
	}
	// an edit of the handle is one undo step and tells the listeners with its cause
	var causes []string
	h.OnChange(func(c extension.ViewDocumentChange) { causes = append(causes, c.Cause) })
	if err := h.Edit([]extension.TextEdit{{StartLine: 0, StartColumn: 0, EndLine: 0, EndColumn: 7, Text: "module"}}); err != nil {
		t.Fatal(err)
	}
	if _, err := e.doc.Undo(); err != nil {
		t.Fatal(err)
	}
	if !slices.Equal(causes, []string{"edit", "undo"}) || e.doc.Dirty() {
		t.Errorf("causes %v, dirty %v", causes, e.doc.Dirty())
	}
	_ = buffer.Position{}
}

func TestLanguagesGiveTokensWithTheirScopes(t *testing.T) {
	win := newTestWorkspace(t, "")
	l := extLanguages{w: win}
	if id, ok := l.Resolve("go"); !ok || id != "go" {
		t.Errorf("Resolve(go) = %q, %v", id, ok)
	}
	if _, ok := l.Resolve("klingon"); ok {
		t.Error("Resolve found a language that is not there")
	}
	lines, err := l.Tokenize("go", "package main\nfunc f() {}\n")
	if err != nil || len(lines) != 3 {
		t.Fatalf("Tokenize = %d lines, %v", len(lines), err)
	}
	var keyword bool
	for _, tok := range lines[0] {
		if tok.Start == 0 && tok.End == 7 {
			keyword = slices.ContainsFunc(tok.Scopes, func(s string) bool { return strings.HasPrefix(s, "keyword") })
		}
	}
	if !keyword {
		t.Errorf("the first word of the line is not a keyword: %+v", lines[0])
	}
	if _, err := l.Tokenize("plaintext", "x"); err == nil {
		t.Error("a language with no grammar was tokenized")
	} else {
		appRoute(t, err, "extLanguages.Tokenize.ExtensionLanguageUnknown")
	}
	_, err = l.Tokenize("go", strings.Repeat("// a\n", maxTokenizeBytes/4))
	appRoute(t, err, "extLanguages.Tokenize.ExtensionTokenizeTooLong")
}

// syncBuffer is a bytes.Buffer that the log of a window may write from any goroutine.
type syncBuffer struct {
	mu  sync.Mutex
	buf bytes.Buffer
}

func (b *syncBuffer) Write(p []byte) (int, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buf.Write(p)
}

// infoLines are the log lines of level info that mention a text.
func (b *syncBuffer) infoLines(text string) []string {
	b.mu.Lock()
	defer b.mu.Unlock()
	var out []string
	for _, line := range strings.Split(b.buf.String(), "\n") {
		if strings.Contains(line, `"level":"INFO"`) && strings.Contains(line, text) {
			out = append(out, line)
		}
	}
	return out
}

// TestTabsOfAnExtensionThatEmetosNoLongerHasAreDropped is the old state of step 17c.1: the last session of a window had tabs
// of the Agents extension (agents.session), which was removed, and the state of its views (the agents.container view and the
// agents.login panel). The window starts without an error dialog, the tabs of the extensions that remain come back, one line
// of the log at info level says what was dropped, and the saved state of the views is left as it was.
func TestTabsOfAnExtensionThatEmetosNoLongerHasAreDropped(t *testing.T) {
	for _, tt := range []struct {
		name     string
		dropped  []*editorgroup.ExtensionTabInput
		wantTabs []string
		wantLog  []string
	}{
		{
			name: "two sessions of the removed extension",
			dropped: []*editorgroup.ExtensionTabInput{
				editorgroup.NewExtensionTabInput("agents", "agents.session", "s1", "Session", "hubot"),
				editorgroup.NewExtensionTabInput("agents", "agents.session", "s2", "Fix the bug", "hubot"),
			},
			wantTabs: []string{"1!: Card c1*"},
			wantLog:  []string{"agents/agents.session"},
		},
		{
			name:     "a kind that the extension no longer declares",
			dropped:  []*editorgroup.ExtensionTabInput{editorgroup.NewExtensionTabInput("ex", "ex.gone", "g1", "Gone", "note")},
			wantTabs: []string{"1!: Card c1*"},
			wantLog:  []string{"ex/ex.gone"},
		},
		{
			name:     "nothing to drop",
			wantTabs: []string{"1!: Card c1*"},
		},
	} {
		t.Run(tt.name, func(t *testing.T) {
			env, st := exEnv(t, nil)
			first := env.startEx(st)
			first.command("ex.newCard", nil)
			g := first.shell().group.grid.Active()
			for _, in := range tt.dropped {
				g.Model.OpenEditor(in, editorgroup.OpenOptions{Pinned: true, Active: false})
			}
			agentsView := json.RawMessage(`{"sessions":[{"id":"s1","title":"hello"}]}`)
			first.win.Extensions().SetViewStates(map[string]json.RawMessage{"agents.sessions": agentsView, "agents.login": json.RawMessage(`null`)})
			first.pump()
			if got := len(g.Model.Editors(editorgroup.Sequential, false)); got != 1+len(tt.dropped) {
				t.Fatalf("%d tabs in the first window, want %d: %v", got, 1+len(tt.dropped), first.layout())
			}
			first.close()

			// the next run has the extension ex only: Emetos has no extension "agents"
			logs := &syncBuffer{}
			logger := slog.New(slog.NewJSONHandler(logs, &slog.HandlerOptions{Level: slog.LevelInfo}))
			second := env.startWith(env.root, ProcessOptions{Extensions: []extension.Factory{exFactory(&exState{})}, Logger: logger})
			if got := second.layout(); !slices.Equal(got, tt.wantTabs) {
				t.Errorf("tabs after the restore = %v, want %v", got, tt.wantTabs)
			}
			if errs := second.ts.TakeErrors(); len(errs) != 0 {
				t.Errorf("errors: %v", errs)
			}
			if n := len(second.win.Notify.Items()); n != 0 {
				t.Errorf("%d notifications (no dialog and no message for tabs that are gone)", n)
			}
			lines := logs.infoLines("saved tabs of extensions")
			if len(tt.wantLog) == 0 {
				if len(lines) != 0 {
					t.Errorf("a log line without a dropped tab: %v", lines)
				}
			} else {
				if len(lines) != 1 {
					t.Fatalf("%d log lines about dropped tabs, want 1: %v", len(lines), lines)
				}
				for _, want := range tt.wantLog {
					if !strings.Contains(lines[0], want) {
						t.Errorf("the log line %s does not name %s", lines[0], want)
					}
				}
			}
			// the state of the views of the removed extension is not deleted: it stays in the state of the window
			second.close()
			saved, ok := env.savedWindow(env.root)
			if !ok {
				t.Fatal("no saved state after the second run")
			}
			if got := saved.Views["agents.sessions"]; string(got) != string(agentsView) {
				t.Errorf("the saved state of agents.sessions = %s, want it kept as %s", got, agentsView)
			}
		})
	}
}
