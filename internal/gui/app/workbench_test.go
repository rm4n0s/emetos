package app

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/rm4n0s/glutter/gluttertest"
	"github.com/rm4n0s/glutter/services"

	"github.com/rm4n0s/emetos/internal/core/command"
	"github.com/rm4n0s/emetos/internal/core/document"
	"github.com/rm4n0s/emetos/internal/core/keybinding"
	"github.com/rm4n0s/emetos/internal/core/settings"
	"github.com/rm4n0s/emetos/internal/gui/editor"
	"github.com/rm4n0s/emetos/internal/gui/kit"
	"github.com/rm4n0s/emetos/internal/gui/testutil"
	"github.com/rm4n0s/emetos/internal/gui/workbench"
)

// ctrl is the chord of Ctrl and a key.
func ctrl(k keybinding.Key) keybinding.Chord { return keybinding.Chord{Ctrl: true, Key: k} }

func TestLayoutKeysRunThroughTheDispatcher(t *testing.T) {
	a := newKeyApp(t, "alpha\n")
	l := a.win.Layout
	if !l.State().SideBarVisible || l.State().PanelVisible {
		t.Fatalf("the layout does not start as VS Code's: %+v", l.State())
	}
	for i, want := range []bool{false, true} {
		if !sendChord(a.ts, ctrl(keybinding.KeyB)) {
			t.Error("ctrl+b was not used")
		}
		a.ts.Pump(10 * time.Millisecond)
		if got := l.State().SideBarVisible; got != want {
			t.Errorf("after ctrl+b number %d the side bar is visible = %v, want %v", i+1, got, want)
		}
		if got := a.win.Keys.Get("sideBarVisible"); got != want {
			t.Errorf("sideBarVisible = %v, want %v", got, want)
		}
	}
	for i, want := range []bool{true, false} {
		if !sendChord(a.ts, ctrl(keybinding.KeyJ)) {
			t.Error("ctrl+j was not used")
		}
		a.ts.Pump(10 * time.Millisecond)
		if got := l.State().PanelVisible; got != want {
			t.Errorf("after ctrl+j number %d the panel is visible = %v, want %v", i+1, got, want)
		}
		if got := a.win.Keys.Get("panelVisible"); got != want {
			t.Errorf("panelVisible = %v, want %v", got, want)
		}
	}
	if errs := a.ts.TakeErrors(); len(errs) != 0 {
		t.Errorf("errors: %v", errs)
	}
}

func TestCtrlZeroFocusesTheSideBarAndTypingThenGoesNowhere(t *testing.T) {
	a := newKeyApp(t, "")
	a.ts.SendKeyEvent('x', gluttertest.KeyOpts{Physical: services.PhysicalKeyboardKeyKeyX, Character: "x"})
	if a.text() != "x" {
		t.Fatalf("the editor does not take typing: %q", a.text())
	}
	if !sendChord(a.ts, ctrl(keybinding.Digit0)) {
		t.Error("ctrl+0 was not used")
	}
	a.ts.Pump(10 * time.Millisecond)
	if a.win.Keys.Get("sideBarFocus") != true || a.win.Keys.Get("editorAreaFocus") == true {
		t.Errorf("sideBarFocus %v, editorAreaFocus %v", a.win.Keys.Get("sideBarFocus"), a.win.Keys.Get("editorAreaFocus"))
	}
	a.ts.SendKeyEvent('y', gluttertest.KeyOpts{Physical: services.PhysicalKeyboardKeyKeyY, Character: "y"})
	if a.text() != "x" {
		t.Errorf("the editor took typing while the side bar has the focus: %q", a.text())
	}
	// and the editor has it back with its command
	if err := a.win.Commands.Run(command.Context{Ctx: t.Context(), Keys: a.win.Keys}, "workbench.action.focusActiveEditorGroup", nil); err != nil {
		t.Fatal(err)
	}
	a.ts.Pump(10 * time.Millisecond)
	a.ts.SendKeyEvent('z', gluttertest.KeyOpts{Physical: services.PhysicalKeyboardKeyKeyZ, Character: "z"})
	if a.text() != "xz" {
		t.Errorf("the editor does not take typing again: %q", a.text())
	}
}

func TestNoCommandRunsWhileADialogShows(t *testing.T) {
	a := newKeyApp(t, "alpha\n")
	ran, restore := a.spyOnCommands(t)
	defer restore()
	a.win.Dialogs.Show(kit.DialogSpec{Type: kit.DialogInfo, Message: "Modal", Buttons: []string{"&&OK"}})
	a.ts.Pump(10 * time.Millisecond)
	if err := gluttertest.FindsOneWidget(a.ts, gluttertest.ByText("Modal")); err != nil {
		t.Fatal(err)
	}
	for _, c := range []keybinding.Chord{ctrl(keybinding.KeyB), ctrl(keybinding.KeyJ), ctrl(keybinding.KeyA), ctrl(keybinding.Digit0)} {
		if sendChord(a.ts, c) {
			t.Errorf("%s was used while a dialog shows", c.Label())
		}
	}
	if len(*ran) != 0 {
		t.Errorf("commands ran under the dialog: %v", *ran)
	}
	if !a.win.Layout.State().SideBarVisible {
		t.Error("ctrl+b hid the side bar under the dialog")
	}

	// the keys work again once the dialog is gone
	a.win.Dialogs.Dismiss()
	a.ts.Pump(10 * time.Millisecond)
	a.ts.Pump(10 * time.Millisecond)
	if !sendChord(a.ts, ctrl(keybinding.KeyB)) {
		t.Error("ctrl+b was not used after the dialog closed")
	}
	if !slicesContains(*ran, "workbench.action.toggleSidebarVisibility") {
		t.Errorf("ran %v", *ran)
	}
}

func slicesContains(list []string, s string) bool {
	for _, x := range list {
		if x == s {
			return true
		}
	}
	return false
}

func TestDispatchKeyIgnoresKeysWhileADialogShows(t *testing.T) {
	win := newTestWorkspace(t, "")
	ran := 0
	if _, err := win.Commands.SetHandler("workbench.action.toggleSidebarVisibility", func(command.Context, any) error { ran++; return nil }); err != nil {
		t.Fatal(err)
	}
	chord := ctrl(keybinding.KeyB)
	if !win.DispatchKey(chord, nil) || ran != 1 {
		t.Fatalf("without a dialog: ran %d", ran)
	}
	win.Dialogs.Show(kit.DialogSpec{Message: "Modal", Buttons: []string{"&&OK"}})
	if win.DispatchKey(chord, nil) || ran != 1 {
		t.Errorf("with a dialog: ran %d", ran)
	}
	win.Dialogs.Dismiss()
	if !win.DispatchKey(chord, nil) || ran != 2 {
		t.Errorf("after the dialog: ran %d", ran)
	}
}

func TestTheLayoutFollowsTheSettings(t *testing.T) {
	a := newKeyApp(t, "alpha\n")
	update := func(key string, value any) {
		t.Helper()
		if err := a.win.Settings.Update(context.Background(), key, value, settings.TargetUser); err != nil {
			t.Fatal(err)
		}
		a.ts.Pump(10 * time.Millisecond)
	}
	update("workbench.statusBar.visible", false)
	update("workbench.sideBar.location", "right")
	update("workbench.activityBar.location", "hidden")
	st := a.win.Layout.State()
	if st.StatusBarVisible || st.SideBarPosition != workbench.SideBarRight || st.ActivityBarVisible {
		t.Errorf("the layout of the window does not follow its settings: %+v", st)
	}
	for _, p := range []workbench.Part{workbench.PartStatusBar, workbench.PartActivityBar} {
		if n := len(a.ts.Elements(gluttertest.ByKey(workbench.PartKey(p)))); n != 0 {
			t.Errorf("%s is still shown", p)
		}
	}
}

func TestTheEditorAreaKeepsTheBreadcrumbsAndTheEditor(t *testing.T) {
	win := newTestWorkspace(t, "")
	doc, err := win.Documents.OpenFile(context.Background(), sampleFile(t), document.NewOptions())
	if err != nil {
		t.Fatal(err)
	}
	ts := testutil.New(t, testWindowSize)
	ts.PumpWidget(newApp(context.Background(), win, doc, nil))
	ts.Pump(10 * time.Millisecond)
	if err := gluttertest.FindsOneWidget(ts, gluttertest.ByType[*editor.Breadcrumbs]()); err != nil {
		t.Error(err)
	}
	// the editor is in the editor area, below the title bar and beside the side bar and the activity bar
	area := gluttertest.ByKey(workbench.PartKey(workbench.PartEditor))
	if err := gluttertest.FindsOneWidget(ts, gluttertest.Descendant(area, gluttertest.ByType[*editor.Editor]())); err != nil {
		t.Error(err)
	}
	if got := ts.TopLeft(area); got.Dx != 348 || got.Dy != 35 {
		t.Errorf("the editor area starts at %v, want (348, 35)", got)
	}
	// the editor keeps the focus it asked for when it was shown
	if got := win.Keys.Get("editorAreaFocus"); got != true {
		t.Errorf("editorAreaFocus = %v", got)
	}
	if errs := ts.TakeErrors(); len(errs) != 0 {
		t.Errorf("errors: %v", errs)
	}
}

// titleApp is an app on a file in a folder, with the title it makes.
func titleApp(t *testing.T, folder string, files ...string) (*Workspace, *gluttertest.Tester, []*document.Document) {
	t.Helper()
	win := newTestWorkspace(t, folder)
	var docs []*document.Document
	for _, f := range files {
		doc, err := win.Documents.OpenFile(context.Background(), f, document.NewOptions())
		if err != nil {
			t.Fatal(err)
		}
		docs = append(docs, doc)
	}
	var shown *document.Document
	if len(docs) > 0 {
		shown = docs[0]
	}
	ts := testutil.New(t, testWindowSize)
	ts.PumpWidget(newApp(context.Background(), win, shown, nil))
	ts.Pump(10 * time.Millisecond)
	return win, ts, docs
}

func TestTheTitleOfTheWindow(t *testing.T) {
	dir := t.TempDir()
	proj := filepath.Join(dir, "proj")
	if err := os.MkdirAll(filepath.Join(proj, "src"), 0o755); err != nil {
		t.Fatal(err)
	}
	file := filepath.Join(proj, "src", "main.go")
	other := filepath.Join(dir, "notes.txt")
	for _, f := range []string{file, other} {
		if err := os.WriteFile(f, []byte("text\n"), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	cases := []struct {
		name   string
		folder string
		files  []string
		want   string
	}{
		{"nothing open", "", nil, "Emetos"},
		{"only a folder", proj, nil, "proj - Emetos"},
		{"a file", "", []string{file}, "main.go - Emetos"},
		{"a file in the folder", proj, []string{file}, "main.go - proj - Emetos"},
		{"a file outside of the folder", proj, []string{other}, "notes.txt - proj - Emetos"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			win, ts, _ := titleApp(t, tc.folder, tc.files...)
			if got := win.Title(); got != tc.want {
				t.Errorf("title %q, want %q", got, tc.want)
			}
			if errs := ts.TakeErrors(); len(errs) != 0 {
				t.Errorf("errors: %v", errs)
			}
		})
	}
}

func TestTheTitleFollowsTheDirtyMarkAndTheSettings(t *testing.T) {
	file := filepath.Join(t.TempDir(), "main.go")
	if err := os.WriteFile(file, []byte("package main\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	win, ts, docs := titleApp(t, "", file)
	if got := win.Title(); got != "main.go - Emetos" {
		t.Fatalf("title %q", got)
	}

	vm := ts.Widget(gluttertest.ByType[*editor.Editor]()).(*editor.Editor).ViewModel()
	if err := vm.RunCommand("type", map[string]any{"text": "x"}); err != nil {
		t.Fatal(err)
	}
	if !docs[0].Dirty() {
		t.Fatal("the document is not dirty")
	}
	ts.Pump(10 * time.Millisecond)
	ts.Pump(10 * time.Millisecond)
	if got := win.Title(); got != "● main.go - Emetos" {
		t.Errorf("title of a dirty file %q", got)
	}
	if err := vm.RunCommand("undo", nil); err != nil {
		t.Fatal(err)
	}
	ts.Pump(10 * time.Millisecond)
	ts.Pump(10 * time.Millisecond)
	if got := win.Title(); got != "main.go - Emetos" {
		t.Errorf("title after undo %q", got)
	}

	set := func(key string, value any) {
		t.Helper()
		if err := win.Settings.Update(context.Background(), key, value, settings.TargetUser); err != nil {
			t.Fatal(err)
		}
		ts.Pump(10 * time.Millisecond)
		ts.Pump(10 * time.Millisecond)
	}
	set("window.title", "${activeEditorLanguageId}${separator}${activeEditorShort}")
	if got := win.Title(); got != "go - main.go" {
		t.Errorf("title with another template %q", got)
	}
	set("window.titleSeparator", " | ")
	if got := win.Title(); got != "go | main.go" {
		t.Errorf("title with another separator %q", got)
	}
	if errs := ts.TakeErrors(); len(errs) != 0 {
		t.Errorf("errors: %v", errs)
	}
}

func TestTheTitleFollowsTheFileThatIsShown(t *testing.T) {
	dir := t.TempDir()
	a, b := filepath.Join(dir, "a.txt"), filepath.Join(dir, "b.txt")
	for _, f := range []string{a, b} {
		if err := os.WriteFile(f, []byte("x\n"), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	win, ts, _ := titleApp(t, "", a)
	if win.Title() != "a.txt - Emetos" {
		t.Fatalf("title %q", win.Title())
	}
	if err := win.Commands.Run(command.Context{Ctx: t.Context()}, "dev.openFile", map[string]any{"path": b}); err != nil {
		t.Fatal(err)
	}
	for range 20 {
		ts.Pump(10 * time.Millisecond)
		time.Sleep(time.Millisecond)
	}
	if got := win.Title(); got != "b.txt - Emetos" {
		t.Errorf("title %q", got)
	}
}
