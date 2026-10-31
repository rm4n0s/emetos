package app

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/rm4n0s/errors"

	"github.com/rm4n0s/emetos/extension"
	"github.com/rm4n0s/emetos/internal/core/buffer"
	"github.com/rm4n0s/emetos/internal/core/document"
	"github.com/rm4n0s/emetos/internal/core/scm/quickdiff"
	"github.com/rm4n0s/emetos/internal/gui/editor"
)

// The quick diff in the window (step 24.11): the editor of a file marks what differs from the original a provider names, the marks follow
// the edits, and the format on save of modifications takes its baseline from the primary provider.

// quickDiffApp is an app that shows a.go, with a provider that compares every file of its folder with the file name + ".orig".
func quickDiffApp(t *testing.T, text, original string) *saveApp {
	t.Helper()
	a := newSaveApp(t, map[string]string{"a.go": text}, "a.go")
	if err := os.WriteFile(a.files["a.go"]+".orig", []byte(original), 0o644); err != nil {
		t.Fatal(err)
	}
	dir := filepath.Dir(a.files["a.go"])
	a.win.QuickDiff.Add(quickdiff.Provider{Label: "Test", Kind: quickdiff.KindPrimary, RootURI: document.FileURI(dir).String(),
		Original: func(_ context.Context, uri string) (string, error) { return uri + ".orig", nil }})
	return a
}

// waitMarks pumps the app until the editor has marks that cond accepts.
func (a *saveApp) waitMarks(what string, cond func([]editor.QuickDiffMark) bool) []editor.QuickDiffMark {
	a.t.Helper()
	var marks []editor.QuickDiffMark
	a.waitFor(what, func() bool {
		marks = a.vm().QuickDiffMarks()
		return cond(marks)
	})
	return marks
}

func TestTheEditorMarksWhatDiffersFromTheOriginalAndFollowsTheEdits(t *testing.T) {
	a := quickDiffApp(t, "one\nTWO\nthree\nfour\nfive\n", "one\ntwo\nthree\nfour\nfive\nsix\n")
	marks := a.waitMarks("the first marks", func(m []editor.QuickDiffMark) bool { return len(m) == 2 })
	if marks[0].Type != quickdiff.Modify || marks[0].Start != 2 || marks[0].End != 2 {
		t.Errorf("first mark = %+v, want line 2 modified", marks[0])
	}
	if marks[1].Type != quickdiff.Delete || marks[1].Start != 5 || marks[1].End != 0 {
		t.Errorf("second mark = %+v, want lines deleted after line 5", marks[1])
	}
	if got := a.vm().QuickDiffCount(); got != 2 {
		t.Errorf("QuickDiffCount = %d, want 2", got)
	}
	// an edit moves the marks at once, and the model compares again after the pause
	if _, err := a.doc("a.go").Edit([]document.Edit{{Range: buffer.Range{}, Text: "zero\n"}}, document.EditOptions{}); err != nil {
		t.Fatal(err)
	}
	a.pump()
	if m := a.vm().QuickDiffMarks(); len(m) != 2 || m[0].Start != 3 {
		t.Errorf("right after the edit the marks are %+v, want them moved down by a line", m)
	}
	marks = a.waitMarks("the marks after the edit", func(m []editor.QuickDiffMark) bool {
		return len(m) == 3 && m[0].Type == quickdiff.Add
	})
	if marks[0].Start != 1 || marks[1].Start != 3 {
		t.Errorf("marks = %+v, want the added line 1 and the modified line 3", marks)
	}
	// putting the text back leaves the one deletion
	if _, err := a.doc("a.go").Edit([]document.Edit{{Range: buffer.Range{End: buffer.Position{Line: 1}}}, {Range: buffer.Range{Start: buffer.Position{Line: 2}, End: buffer.Position{Line: 2, Col: 3}}, Text: "two"}}, document.EditOptions{}); err != nil {
		t.Fatal(err)
	}
	a.waitMarks("the marks after the undo", func(m []editor.QuickDiffMark) bool { return len(m) == 1 && m[0].Type == quickdiff.Delete })
	a.noErrors()
}

func TestTheNextChangeCommandsMoveTheCursorWithWrap(t *testing.T) {
	a := quickDiffApp(t, "one\nTWO\nthree\nfour\nFIVE\nsix\n", "one\ntwo\nthree\nfour\nfive\nsix\n")
	a.waitMarks("the marks", func(m []editor.QuickDiffMark) bool { return len(m) == 2 })
	if e := a.shell().activeEditor(); e == nil || !e.focus.Request() { // the keys are those of the editor that has the focus
		t.Fatal("the editor cannot take the focus")
	}
	a.pump()
	line := func() int { return a.vm().Cursors().Primary().Position().Line }
	a.press("alt+f5")
	if line() != 1 {
		t.Errorf("the first Alt+F5 went to line %d, want the first change", line()+1)
	}
	a.press("alt+f5")
	if line() != 4 {
		t.Errorf("the second Alt+F5 went to line %d, want the second change", line()+1)
	}
	a.press("alt+f5")
	if line() != 1 {
		t.Errorf("Alt+F5 after the last change went to line %d, want it to wrap to the first", line()+1)
	}
	a.press("shift+alt+f5")
	if line() != 4 {
		t.Errorf("Shift+Alt+F5 from the first change went to line %d, want it to wrap to the last", line()+1)
	}
	a.run("workbench.action.editor.previousChange")
	if line() != 1 {
		t.Errorf("the previous change command went to line %d", line()+1)
	}
	keys := a.win.Scopes.ScopeOf(a.ts.Binding.FocusManager().PrimaryFocus(), a.win.Keys)
	if got := keys.Get("quickDiffDecorationCount"); fmt.Sprint(got) != "2" {
		t.Errorf("the context key quickDiffDecorationCount is %v, want 2", got)
	}
}

func TestAFileWithNoOriginalHasNoMarks(t *testing.T) {
	a := quickDiffApp(t, "one\ntwo\n", "one\ntwo\n")
	time.Sleep(3 * quickdiff.DefaultDelay)
	a.pump()
	if a.vm().QuickDiffCount() != 0 {
		t.Errorf("%d marks for a file that equals its original", a.vm().QuickDiffCount())
	}
	// an original that cannot be read leaves the file with no marks, and the window goes on
	if err := os.Remove(a.files["a.go"] + ".orig"); err != nil {
		t.Fatal(err)
	}
	a.win.QuickDiff.Refresh("")
	time.Sleep(3 * quickdiff.DefaultDelay)
	a.pump()
	if a.vm().QuickDiffCount() != 0 {
		t.Errorf("%d marks for a file whose original cannot be read", a.vm().QuickDiffCount())
	}
	a.noErrors()
}

func TestTheQuickDiffSettingsReachTheEditor(t *testing.T) {
	a := quickDiffApp(t, "one\nTWO\n", "one\ntwo\n")
	a.waitMarks("the marks", func(m []editor.QuickDiffMark) bool { return len(m) == 1 })
	a.setting("scm.diffDecorations", "overview")
	a.setting("scm.diffDecorationsGutterWidth", 5)
	a.setting("scm.diffDecorationsGutterVisibility", "hover")
	a.setting("scm.diffDecorationsGutterPattern", map[string]any{"added": true, "modified": false})
	want := editor.QuickDiffOptions{Decorations: "overview", GutterWidth: 5, Visibility: "hover", PatternAdded: true}
	if got := a.vm().Options().QuickDiff; got != want {
		t.Errorf("options = %+v, want %+v", got, want)
	}
}

func TestIgnoringWhiteSpaceAtTheEndsOfLinesFollowsTheSetting(t *testing.T) {
	a := quickDiffApp(t, "one\ntwo  \n", "one\ntwo\n")
	a.waitMarks("the marks", func(m []editor.QuickDiffMark) bool { return len(m) == 1 })
	a.setting("scm.diffDecorationsIgnoreTrimWhitespace", "true")
	a.waitMarks("no marks while white space is ignored", func(m []editor.QuickDiffMark) bool { return len(m) == 0 })
	a.setting("scm.diffDecorationsIgnoreTrimWhitespace", "inherit") // diffEditor.ignoreTrimWhitespace is true by default
	time.Sleep(3 * quickdiff.DefaultDelay)
	a.pump()
	if n := a.vm().QuickDiffCount(); n != 0 {
		t.Errorf("inherit: %d marks, want none (the diff editor's setting ignores the white space)", n)
	}
	a.setting("diffEditor.ignoreTrimWhitespace", false)
	a.waitMarks("the marks again", func(m []editor.QuickDiffMark) bool { return len(m) == 1 })
	a.setting("scm.diffDecorationsIgnoreTrimWhitespace", "false")
	a.waitMarks("the marks with false", func(m []editor.QuickDiffMark) bool { return len(m) == 1 })
}

func TestClosingTheEditorTakesTheMarksAway(t *testing.T) {
	a := quickDiffApp(t, "one\nTWO\n", "one\ntwo\n")
	a.waitMarks("the marks", func(m []editor.QuickDiffMark) bool { return len(m) == 1 })
	vm := a.vm()
	a.run("workbench.action.closeActiveEditor")
	if vm.QuickDiffCount() != 0 {
		t.Errorf("%d marks left in the closed editor", vm.QuickDiffCount())
	}
	// showing the file again makes the marks again
	a.show("a.go", false)
	a.waitMarks("the marks of the new editor", func(m []editor.QuickDiffMark) bool { return len(m) == 1 })
}

func TestTheEditorsOfADiffHaveNoQuickDiff(t *testing.T) {
	a := &diffApp{newSaveApp(t, map[string]string{"old.go": "one\ntwo\n", "new.go": "one\nTWO\n"})}
	dir := filepath.Dir(a.files["old.go"])
	for _, name := range []string{"old.go", "new.go"} {
		if err := os.WriteFile(a.files[name]+".orig", []byte("one\ntwo\nthree\n"), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	a.win.QuickDiff.Add(quickdiff.Provider{Label: "Test", RootURI: document.FileURI(dir).String(),
		Original: func(_ context.Context, uri string) (string, error) { return uri + ".orig", nil }})
	a.openDiff("old.go", "new.go")
	time.Sleep(3 * quickdiff.DefaultDelay)
	a.pump()
	for i, side := range a.pane().sides {
		if side.stopQuickDiff != nil || side.view.QuickDiffCount() != 0 {
			t.Errorf("side %d of the diff follows the quick diff (%d marks)", i, side.view.QuickDiffCount())
		}
	}
	a.noErrors()
}

func TestTheOriginalOfAQuickDiffIsAProvidedDocumentOrAFile(t *testing.T) {
	a := quickDiffApp(t, "x\n", "x\n")
	if err := a.win.virtual.RegisterPrefix("/qdtest/", document.VirtualPrefixProvider{
		Label:    func(rest string) string { return rest },
		Language: func(string) string { return "plaintext" },
		Text:     func(_ context.Context, rest string) (string, error) { return "provided " + rest, nil },
	}); err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	if got, err := a.win.quickDiffText(ctx, extension.ContentURI("qdtest", "a/b.txt")); err != nil || got != "provided a/b.txt" {
		t.Errorf("a provided document: %q, %v", got, err)
	}
	if got, err := a.win.quickDiffText(ctx, document.FileURI(a.files["a.go"]+".orig").String()); err != nil || got != "x\n" {
		t.Errorf("a file: %q, %v", got, err)
	}
	// the path of a file URI of an extension is escaped
	spaced := filepath.Join(filepath.Dir(a.files["a.go"]), "a b%.orig")
	if err := os.WriteFile(spaced, []byte("spaced\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if got, err := a.win.quickDiffText(ctx, quickdiff.URIOf(document.FileURI(spaced))); err != nil || got != "spaced\n" {
		t.Errorf("a file with a space and a percent sign: %q, %v", got, err)
	}
	_, err := a.win.quickDiffText(ctx, "untitled:1")
	appErr, ok := errors.FromError(err)
	if !ok || !appErr.HasRoute("Workspace.quickDiffText.QuickDiffOriginalUnsupported") {
		t.Errorf("an untitled original: %v", err)
	}
	if _, err := a.win.quickDiffText(ctx, "://bad"); err == nil {
		t.Error("a URI that does not parse was read")
	}
}

func TestQuickDiffBaselineOfTheWindow(t *testing.T) {
	a := quickDiffApp(t, "one\n", "baseline\n")
	ctx := context.Background()
	if got, ok := a.win.quickDiffBaseline(ctx, a.files["a.go"]); !ok || got != "baseline\n" {
		t.Errorf("baseline = %q, %v", got, ok)
	}
	if _, ok := a.win.quickDiffBaseline(ctx, filepath.Join(t.TempDir(), "other.go")); ok {
		t.Error("a file of no provider has a baseline")
	}
	none := &Workspace{}
	if _, ok := none.quickDiffBaseline(ctx, "/x"); ok {
		t.Error("a window without a registry has a baseline")
	}
	cancelled, cancel := context.WithCancel(ctx)
	cancel()
	if _, ok := a.win.quickDiffBaseline(cancelled, a.files["a.go"]); ok {
		t.Error("a cancelled context gave a baseline")
	}
}

func TestFormatOnSaveOfTheModificationsComparesWithTheQuickDiffOriginal(t *testing.T) {
	for _, tc := range []struct {
		name     string
		original string // "" for no provider
		want     []int  // the lines that are formatted
	}{
		// the file on disk is "one two three four", and the text has "one!" for its first line
		{"the file on disk without a provider", "", []int{0}},
		{"the index version, which differs in the third line too", "one\ntwo\nTHREE\nfour\n", []int{0, 2}},
		{"an original equal to the text formats nothing", "one!\ntwo\nthree\nfour\n", nil},
	} {
		t.Run(tc.name, func(t *testing.T) {
			a, f := saveFeaturesApp(t, map[string]any{"editor.formatOnSaveMode": "modifications"}, "one\ntwo\nthree\nfour\n")
			if tc.original != "" {
				base := a.files["a.go"] + ".base"
				if err := writeTestFile(base, tc.original); err != nil {
					t.Fatal(err)
				}
				a.win.QuickDiff.Add(quickdiff.Provider{Label: "Test", RootURI: document.FileURI(filepath.Dir(base)).String(),
					Original: func(_ context.Context, uri string) (string, error) { return uri + ".base", nil }})
			}
			if _, err := a.doc("a.go").Edit([]document.Edit{{Range: buffer.Range{End: buffer.Position{Line: 0, Col: 3}}, Text: "one!"}}, document.EditOptions{}); err != nil {
				t.Fatal(err)
			}
			a.press("ctrl+s")
			a.waitFor("the save", func() bool { return !a.doc("a.go").Dirty() })
			f.mu.Lock()
			ranges := f.ranges
			f.mu.Unlock()
			var got []int
			for _, r := range ranges {
				got = append(got, r.Start.Line)
			}
			if len(got) != len(tc.want) {
				t.Fatalf("formatted lines %v, want %v", got, tc.want)
			}
			for i := range got {
				if got[i] != tc.want[i] {
					t.Errorf("formatted lines %v, want %v", got, tc.want)
				}
			}
		})
	}
}
