package app

import (
	"slices"
	"testing"
	"time"

	"github.com/rm4n0s/emetos/extension"
	"github.com/rm4n0s/emetos/internal/core/buffer"
	"github.com/rm4n0s/emetos/internal/core/command"
	"github.com/rm4n0s/emetos/internal/core/document"
	"github.com/rm4n0s/emetos/internal/core/scm/quickdiff"
	"github.com/rm4n0s/emetos/internal/gui/diffeditor"
	"github.com/rm4n0s/emetos/internal/gui/editor"
	"github.com/rm4n0s/emetos/internal/gui/kit"
	"github.com/rm4n0s/glutter/gluttertest"
	"github.com/rm4n0s/glutter/ui"
	"github.com/rm4n0s/glutter/widgets"
)

// The peek of a change in the window (step 24.12): the keys, the stripe, the follow, the actions.

const (
	peekText     = "one\nTWO\nthree\nfour\nFIVE\nsix\nseven\neight\nnine\nTEN\n"
	peekOriginal = "one\ntwo\nthree\nfour\nfive\nsix\nseven\neight\nnine\nten\n"
)

// peekApp is a window with an open file that differs from its original on lines 2, 5 and 10.
func peekApp(t *testing.T) *saveApp {
	t.Helper()
	a := quickDiffApp(t, peekText, peekOriginal)
	a.waitMarks("the marks", func(m []editor.QuickDiffMark) bool { return len(m) == 3 })
	if e := a.shell().activeEditor(); e == nil || !e.focus.Request() {
		t.Fatal("the editor cannot take the focus")
	}
	a.pump()
	return a
}

// outer is the view model of the editor of the file; a.vm() cannot tell it from the editors of the peek.
func (a *saveApp) outer() *editor.ViewModel { return a.shell().activeEditor().view }

// peekZone is the line the zone of the peek comes after, and whether it is there.
func (a *saveApp) peekZone() (int, bool) {
	zones := a.outer().Zones()
	if len(zones) != 1 {
		return 0, false
	}
	return zones[0].AfterLine, true
}

func (a *saveApp) wantPeekAt(what string, line int) {
	a.t.Helper()
	got, ok := a.peekZone()
	if !ok || got != line || !a.outer().QuickDiffPeekVisible() {
		a.t.Errorf("%s: the peek is after line %d (open %v), want line %d", what, got, ok, line)
	}
}

func (a *saveApp) wantNoPeek(what string) {
	a.t.Helper()
	if a.outer().QuickDiffPeekVisible() || len(a.outer().Zones()) != 0 {
		a.t.Errorf("%s: the peek is open", what)
	}
}

func TestTheKeysOpenTheMoveAndCloseThePeek(t *testing.T) {
	a := peekApp(t)
	a.press("alt+f3")
	a.wantPeekAt("Alt+F3 at the top", 2)
	a.press("alt+f3")
	a.wantPeekAt("Alt+F3 again", 5)
	a.press("alt+f3")
	a.wantPeekAt("Alt+F3 on the last", 10)
	a.press("alt+f3")
	a.wantPeekAt("Alt+F3 goes round", 2)
	a.press("shift+alt+f3")
	a.wantPeekAt("Shift+Alt+F3 goes round back", 10)
	a.press("shift+alt+f3")
	a.wantPeekAt("Shift+Alt+F3", 5)
	if got := a.outer().Cursors().Primary().Position().Line + 1; got != 5 {
		t.Errorf("the cursor is at line %d, want the line of the change", got)
	}
	a.press("escape")
	a.wantNoPeek("Escape")
	// the peek opens again, and Shift+Escape closes it too
	a.press("alt+f3")
	a.wantPeekAt("Alt+F3 after the cursor", 5)
	a.press("shift+escape")
	a.wantNoPeek("Shift+Escape")
	a.noErrors()
}

func TestThePeekHasASmallDiffEditorAndTheToggleKeyMovesTheKeyboardInAndOut(t *testing.T) {
	a := peekApp(t)
	a.press("alt+f3")
	a.waitFor("the small diff editor", func() bool {
		return len(a.ts.Widgets(gluttertest.ByType[*editor.Editor]())) == 2
	})
	outer := a.shell().activeEditor()
	if !outer.focus.Node().HasPrimaryFocus() {
		t.Fatal("the editor does not have the keyboard after the peek opened")
	}
	a.press("ctrl+k f2")
	if outer.focus.Node().HasPrimaryFocus() {
		t.Error("Ctrl+K F2 left the keyboard in the editor")
	}
	// Escape in the small editor closes the peek, and the keyboard is the editor's again
	a.press("escape")
	a.wantNoPeek("Escape in the small editor")
	if !outer.focus.Node().HasPrimaryFocus() {
		t.Error("the editor did not get the keyboard back")
	}
	a.press("alt+f3")
	a.press("ctrl+k f2")
	a.press("ctrl+k f2")
	if !outer.focus.Node().HasPrimaryFocus() {
		t.Error("the second Ctrl+K F2 did not give the keyboard back to the editor")
	}
	a.press("escape")
	a.noErrors()
}

// stripePoint is the point of the stripe of the marks at a line of the open editor.
func (a *saveApp) stripePoint(line int) ui.Offset {
	a.t.Helper()
	f := gluttertest.ByPredicate("the editor of the file", func(w widgets.Widget) bool { e, ok := w.(*editor.Editor); return ok && e.ViewModel() == a.outer() })
	vm := a.outer()
	w := vm.GutterWidths(a.ts.Dispatcher().TextEngine())
	top := a.ts.TopLeft(f)
	proj := vm.Projection()
	row := vm.RowOfPosition(buffer.Position{Line: line - 1})
	// the stripe is 5 pixels into the line decorations, after the folding controls (16)
	x := w.GlyphMargin + w.LineNumbers + 16 + 5 + 1
	return ui.Offset{Dx: top.Dx + x, Dy: top.Dy + proj.RowTop(row) - vm.ScrollTop() + proj.RowHeight(row)/2}
}

func TestAClickOnAStripeOpensThePeekAndAnotherClosesIt(t *testing.T) {
	a := peekApp(t)
	cursor := a.outer().Cursors().Primary().Position()
	a.ts.TapAt(a.stripePoint(5), mouseButton)
	a.pump()
	a.wantPeekAt("a click on the stripe of line 5", 5)
	if got := a.outer().Cursors().Primary().Position(); got == cursor && cursor.Line != 4 {
		// the peek moves the cursor to its change
		t.Errorf("the cursor stayed at %v", got)
	}
	a.ts.TapAt(a.stripePoint(10), mouseButton)
	a.pump()
	a.wantPeekAt("a click on the stripe of line 10", 10)
	a.ts.TapAt(a.stripePoint(10), mouseButton)
	a.pump()
	a.wantNoPeek("a second click on the same stripe")
	a.ts.TapAt(a.stripePoint(7), mouseButton)
	a.pump()
	a.wantNoPeek("a click on a line with no mark")
	a.noErrors()
}

func TestThePeekFollowsTheEditsAndGoesWhenTheChangesGo(t *testing.T) {
	a := peekApp(t)
	a.press("alt+f3")
	a.press("alt+f3")
	a.wantPeekAt("the second change", 5)
	// two lines above push the change down; the zone moves with the text and the model compares again
	if _, err := a.doc("a.go").Edit([]document.Edit{{Range: buffer.Range{}, Text: "a\nb\n"}}, document.EditOptions{}); err != nil {
		t.Fatal(err)
	}
	a.pump()
	a.wantPeekAt("two lines were added above", 7)
	a.waitFor("the new marks", func() bool {
		m := a.outer().QuickDiffMarks()
		return len(m) == 4 && m[0].Start == 1
	})
	a.pump()
	// one more change is undone: the peek shows the one now at the place
	a.run("undo")
	a.waitFor("the marks after the undo", func() bool { return len(a.outer().QuickDiffMarks()) == 3 })
	if !a.outer().QuickDiffPeekVisible() {
		t.Error("the peek went with an edit that left the change")
	}
	// the text is made equal to the original: no change, no peek
	doc := a.doc("a.go")
	if _, err := doc.Edit([]document.Edit{{Range: buffer.Range{End: buffer.Position{Line: doc.Snapshot().LineCount() - 1}}, Text: peekOriginal}}, document.EditOptions{}); err != nil {
		t.Fatal(err)
	}
	a.waitFor("no marks", func() bool { return len(a.outer().QuickDiffMarks()) == 0 })
	a.wantNoPeek("every change was undone")
	a.noErrors()
}

func TestTheButtonsOfTheTitleRunTheCommandsOfTheMenuWithTheChanges(t *testing.T) {
	a := peekApp(t)
	var got []any
	if err := a.win.Commands.Register(command.Meta{ID: "test.stageChange", Title: "Stage Change", Icon: "add"}); err != nil {
		t.Fatal(err)
	}
	if _, err := a.win.Commands.SetHandler("test.stageChange", func(_ command.Context, args any) error { got = append(got, args); return nil }); err != nil {
		t.Fatal(err)
	}
	if err := a.win.Menus.Add(command.MenuItem{Menu: command.SCMChangeContext, Command: "test.stageChange", Group: "inline"}); err != nil {
		t.Fatal(err)
	}
	a.press("alt+f3")
	a.press("alt+f3")
	a.waitFor("the button", func() bool { return a.ts.Widgets(titleButton("Stage Change")) != nil })
	a.ts.Tap(titleButton("Stage Change"), mouseButton)
	a.pump()
	if len(got) != 1 {
		t.Fatalf("the command ran %d times", len(got))
	}
	list, ok := got[0].([]any)
	if !ok || len(list) != 3 {
		t.Fatalf("the arguments are %#v", got[0])
	}
	if list[0] != quickdiff.URIOf(a.doc("a.go").URI()) || list[2] != 1 {
		t.Errorf("the file is %v and the index %v", list[0], list[2])
	}
	if changes, ok := list[1].([]any); !ok || len(changes) != 3 {
		t.Errorf("the changes are %#v", list[1])
	}
	a.wantNoPeek("after an action")
	a.noErrors()
}

func TestThePeekIsDroppedWithItsEditor(t *testing.T) {
	a := peekApp(t)
	a.press("alt+f3")
	a.press("ctrl+w")
	a.pump()
	a.ts.Pump(600 * time.Millisecond)
	a.noErrors()
	if slices.Contains(a.tabNames(), "a.go") {
		t.Error("the editor is still open")
	}
}

// titleButton finds the button of the title bar of the peek with the tooltip.
func titleButton(tooltip string) gluttertest.Finder {
	return gluttertest.ByPredicate("the button "+tooltip, func(w widgets.Widget) bool {
		b, ok := w.(*kit.IconButton)
		return ok && b.Tooltip() == tooltip
	})
}

func TestTheToolbarInTheGutterOfAWorkTreeDiffRunsTheCommandOfTheMenu(t *testing.T) {
	a := newSaveApp(t, map[string]string{"a.go": "one\ntwo\nthree\n"}, "a.go")
	var got []any
	if err := a.win.Commands.Register(command.Meta{ID: "test.stageBlock", Title: "Stage Block", Icon: "add"}); err != nil {
		t.Fatal(err)
	}
	if _, err := a.win.Commands.SetHandler("test.stageBlock", func(_ command.Context, args any) error { got = append(got, args); return nil }); err != nil {
		t.Fatal(err)
	}
	if err := a.win.Menus.Add(command.MenuItem{Menu: command.DiffEditorHunkToolbar, Command: "test.stageBlock", Group: "primary"}); err != nil {
		t.Fatal(err)
	}
	a.typeText("zero\n")
	a.press("ctrl+k d")
	pane := a.waitDiffPane()
	a.waitFor("the changes", func() bool { return len(pane.Model().Changes()) == 1 })
	vm := pane.Model().View(diffeditor.Modified)
	f := gluttertest.ByPredicate("the modified editor", func(w widgets.Widget) bool { e, ok := w.(*editor.Editor); return ok && e.ViewModel() == vm })
	top, size := a.ts.TopLeft(f), a.ts.Size(f)
	row := vm.RowOfPosition(buffer.Position{})
	proj := vm.Projection()
	a.ts.Hover(ui.Offset{Dx: top.Dx + size.Width - 250, Dy: top.Dy + proj.RowTop(row) + proj.RowHeight(row)/2})
	a.pump()
	a.waitFor("the toolbar", func() bool { return len(a.ts.Widgets(titleButton("Stage Block"))) == 1 })
	a.ts.Tap(titleButton("Stage Block"), mouseButton)
	a.pump()
	if len(got) != 1 {
		t.Fatalf("the command ran %d times", len(got))
	}
	ctx, ok := extension.ReadDiffEditorArgs(got[0])
	if !ok {
		t.Fatalf("the arguments are %#v", got[0])
	}
	if ctx.ModifiedURI != a.doc("a.go").URI().String() || ctx.OriginalURI != pane.input.Original().URI().String() {
		t.Errorf("the documents are %q and %q", ctx.OriginalURI, ctx.ModifiedURI)
	}
	if ctx.OriginalWithModifiedChanges != "zero\none\ntwo\nthree\n" {
		t.Errorf("the text is %q", ctx.OriginalWithModifiedChanges)
	}
	// the command reads the side of the diff the cursor is in, as the selection commands need
	vm.Cursors().SetSelections([]buffer.Selection{{Anchor: buffer.Position{Line: 1}, Active: buffer.Position{Line: 1, Col: 2}}})
	vm.SelectionsChanged()
	a.pump()
	active, ok := (extensionDocuments{w: a.win}).Active()
	if !ok || active.URI != a.doc("a.go").URI().String() || len(active.Selections) != 1 || active.Selections[0].Anchor.Line != 1 {
		t.Errorf("the active document of a diff editor is %+v, %v", active, ok)
	}
	a.noErrors()
}
