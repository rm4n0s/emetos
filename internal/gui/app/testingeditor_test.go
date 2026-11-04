package app

import (
	"context"
	"image"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/rm4n0s/glutter/foundation"
	"github.com/rm4n0s/glutter/gestures"
	"github.com/rm4n0s/glutter/gluttertest"
	"github.com/rm4n0s/glutter/ui"

	"github.com/rm4n0s/emetos/extension"
	"github.com/rm4n0s/emetos/extensions/golang"
	"github.com/rm4n0s/emetos/internal/core/buffer"
	"github.com/rm4n0s/emetos/internal/core/command"
	"github.com/rm4n0s/emetos/internal/core/document"
	"github.com/rm4n0s/emetos/internal/core/settings"
	coretesting "github.com/rm4n0s/emetos/internal/core/testing"
	"github.com/rm4n0s/emetos/internal/gui/editor"
	testview "github.com/rm4n0s/emetos/internal/gui/views/testing"
)

// The tests in the editor (step 26.3, SPEC §10.32 "The editor"): the glyphs and their clicks and menu, the failure messages after their
// lines, the peek with its diff and its stack, Alt+F8, Test Results and the settings that decide what a run opens, against the fake
// controller of extensions/devtesting through the real host and model.

// peekSource is a test file: the tests are at the lines peekScript says.
const peekSource = `package calc

import "testing"

func TestAdd(t *testing.T) {
	if got := Add(2, 3); got != 5 {
		t.Errorf("Add = %d", got)
	}
}

func TestDivide(t *testing.T) {
	got, _ := Divide(5, 2)
	if got != 2.5 {
		t.Errorf("Divide(5, 2) = %v", got)
	}
}

func TestReport(t *testing.T) {
	got := Report(12)
	if got != "Total: 12\nTax: 2.40" {
		t.Errorf("Report(12) = %q", got)
	}
}

func TestRoot(t *testing.T) {
	t.Skip("later")
}

func TestGroup(t *testing.T) {
	t.Run("one", func(t *testing.T) {})
	t.Run("two", func(t *testing.T) {})
}
`

// peekScript is the script of the fake controller of these tests: a passing test, two failures (one with a diff), a skipped test and a group
// with two subtests that start on the lines of the group.
const peekScript = `{
  "label": "Fake Tests (dev)",
  "delayMs": 1,
  "tests": [
    {"id": "TestAdd", "label": "TestAdd", "file": "calc_test.go", "line": 5, "endLine": 9, "result": "passed", "ms": 2},
    {"id": "TestDivide", "label": "TestDivide", "file": "calc_test.go", "line": 11, "endLine": 16, "result": "failed", "ms": 12,
      "message": "Divide(5, 2) = 2, want 2.5", "failLine": 14,
      "stack": [{"label": "calc.TestDivide", "file": "calc_test.go", "line": 14}, {"label": "testing.tRunner"}],
      "output": "=== RUN   TestDivide\n\u001b[31mFAIL\u001b[0m TestDivide\n"},
    {"id": "TestReport", "label": "TestReport", "file": "calc_test.go", "line": 18, "endLine": 23, "result": "failed", "ms": 3,
      "message": "Report(12) is wrong", "failLine": 21, "expected": "Total: 12\nTax: 2.40", "actual": "Total: 12\nTax: 2.04"},
    {"id": "TestRoot", "label": "TestRoot", "file": "calc_test.go", "line": 25, "endLine": 27, "result": "skipped"},
    {"id": "TestGroup", "label": "TestGroup", "file": "calc_test.go", "line": 29, "endLine": 32, "children": [
      {"id": "one", "label": "one", "file": "calc_test.go", "line": 30, "endLine": 30, "result": "passed", "ms": 1},
      {"id": "two", "label": "two", "file": "calc_test.go", "line": 31, "endLine": 31, "result": "passed", "ms": 1}
    ]}
  ]
}`

// peekRig is a window with the file open and the cursor in it.
func peekRig(t *testing.T, o testingOpts) (*testingRig, *textEditor) {
	t.Helper()
	if o.script == "" {
		o.script = peekScript
	}
	if o.files == nil {
		o.files = map[string]string{}
	}
	o.files["calc_test.go"] = peekSource
	if o.open == nil {
		o.open = []string{"calc_test.go"}
	}
	if o.settings == nil {
		o.settings = map[string]any{}
	}
	if _, ok := o.settings["testing.openTesting"]; !ok {
		o.settings["testing.openTesting"] = "neverOpen"
	}
	r := newTestingRig(t, o)
	e := r.win.shell.activeEditor()
	if e == nil || e.tst == nil {
		t.Fatalf("the editor has no tests: %+v", e)
	}
	r.waitFor("the glyphs", func() bool { return len(e.view.TestGlyphs()) > 0 })
	return r, e
}

// glyphLines are the lines (counted from 0) that have a glyph, with the icon of each.
func glyphLines(e *textEditor) map[int]int {
	out := map[int]int{}
	for _, g := range e.view.TestGlyphs() {
		out[g.Line] = g.Icon
	}
	return out
}

func TestTheGlyphsShowTheRunIconAndThenTheStateOfTheLastResult(t *testing.T) {
	r, e := peekRig(t, testingOpts{})
	before := glyphLines(e)
	// the tests are at lines 5, 11, 18, 25, 29, 30 and 31 (counted from 1)
	if got := len(before); got != 7 {
		t.Fatalf("%d glyphs before a run, want 7: %v", got, before)
	}
	if before[4] == before[28] {
		t.Errorf("a single test and a group with children have the same icon %#x", before[4])
	}
	for line, icon := range before {
		if line != 28 && icon != before[4] {
			t.Errorf("line %d has the icon %#x before a run, want the run icon %#x", line, icon, before[4])
		}
	}
	r.runCommand(command.TestingRunAll, nil)
	r.waitFor("the glyphs of the results", func() bool { return glyphLines(e)[4] != before[4] })
	after := glyphLines(e)
	if after[4] != testview.StateIcon(coretesting.StatePassed) || after[10] != testview.StateIcon(coretesting.StateFailed) ||
		after[24] != testview.StateIcon(coretesting.StateSkipped) {
		t.Errorf("icons after a run: passed %#x, failed %#x, skipped %#x: %v", after[4], after[10], after[24], after)
	}
	// the group shows the state of its children, and a failure marks its glyph for the overview ruler
	if after[28] != testview.StateIcon(coretesting.StatePassed) {
		t.Errorf("the group has the icon %#x, want passed", after[28])
	}
	failed := 0
	for _, g := range e.view.TestGlyphs() {
		if g.Failed {
			failed++
		}
	}
	if failed != 2 {
		t.Errorf("%d glyphs are failures, want 2", failed)
	}
	if errs := r.ts.TakeErrors(); len(errs) != 0 {
		t.Errorf("errors: %v", errs)
	}
}

func TestTheGlyphsFollowTheEditsAndTheGutterSetting(t *testing.T) {
	r, e := peekRig(t, testingOpts{})
	at := buffer.Position{Line: 3, Col: 0}
	if _, err := e.doc.Edit([]document.Edit{{Range: buffer.Range{Start: at, End: at}, Text: "// one\n// two\n"}}, document.EditOptions{}); err != nil {
		t.Fatal(err)
	}
	if got := glyphLines(e); got[6] == 0 || got[4] != 0 {
		t.Errorf("the glyph of the first test is at %v, want line 6 after two lines went in above it", got)
	}
	r.set("testing.gutterEnabled", false)
	r.waitFor("the gutter to go", func() bool { return !r.testGlyphsShown(e) })
}

// testGlyphsShown tells whether the editor's margin has the lane of the glyphs.
func (r *testingRig) testGlyphsShown(e *textEditor) bool {
	h := e.view.Options().LineHeight
	return e.view.GutterWidths(r.ts.Dispatcher().TextEngine()).GlyphMargin > h
}

func TestAGutterClickRunsAsTheSettingSays(t *testing.T) {
	tests := []struct {
		action string
		alt    bool
		group  coretesting.Group
		menu   bool
	}{
		{"run", false, coretesting.GroupRun, false},
		{"run", true, coretesting.GroupDebug, false},
		{"debug", false, coretesting.GroupDebug, false},
		{"debug", true, coretesting.GroupRun, false},
		{"runWithCoverage", false, coretesting.GroupCoverage, false},
		{"runWithCoverage", true, coretesting.GroupDebug, false},
		{"contextMenu", false, 0, true},
	}
	for _, tc := range tests {
		t.Run(tc.action+map[bool]string{true: " with Alt", false: ""}[tc.alt], func(t *testing.T) {
			r, e := peekRig(t, testingOpts{settings: map[string]any{"testing.defaultGutterClickAction": tc.action}})
			ids := []string{id("TestDivide")}
			e.tst.glyphPress(editor.TestPress{Line: 10, IDs: ids, Mods: editor.MouseModifiers{Alt: tc.alt}, At: ui.Offset{Dx: 100, Dy: 100}})
			if tc.menu {
				r.pump()
				if len(r.svc().Results()) != 0 {
					t.Error("a click that asks for the menu ran the test")
				}
				return
			}
			r.waitFor("the run", func() bool { rs := r.svc().Results(); return len(rs) > 0 && rs[0].IsComplete() })
			res := r.svc().Results()[0]
			if res.Request().Group != tc.group || !res.Request().PreserveFocus {
				t.Errorf("the request is %+v, want group %v with the focus kept", res.Request(), tc.group)
			}
			if got := leaves(res); !slices.Equal(got, []string{"TestDivide"}) {
				t.Errorf("ran %v, want only TestDivide", got)
			}
		})
	}
}

// set changes a setting of the user.
func (r *testingRig) set(key string, value any) {
	r.t.Helper()
	if err := r.win.Settings.Update(context.Background(), key, value, settings.TargetUser); err != nil {
		r.t.Fatal(err)
	}
	r.pump()
}

// glyphPoint is the middle of the left lane of the glyph margin on a line (from 0) of an editor, in the window.
func (r *testingRig) glyphPoint(e *textEditor, line int) ui.Offset {
	r.t.Helper()
	vm := e.view
	row := vm.RowOfPosition(buffer.Position{Line: line})
	top := r.ts.TopLeft(gluttertest.ByType[*editor.Editor]())
	h := vm.Options().LineHeight
	return ui.Offset{Dx: top.Dx + h/2, Dy: top.Dy + vm.Projection().RowTop(row) - vm.ScrollTop() + h/2}
}

func (r *testingRig) mouse(buttons ...int) gluttertest.GestureOpts {
	b := gestures.PrimaryButton
	if len(buttons) > 0 {
		b = buttons[0]
	}
	return gluttertest.GestureOpts{Kind: ui.PointerDeviceKindMouse, Buttons: foundation.Some(b)}
}

func TestClickingTheGlyphRunsItsTestAndTheSecondaryButtonOpensItsMenu(t *testing.T) {
	r, e := peekRig(t, testingOpts{})
	r.ts.TapAt(r.glyphPoint(e, 10), r.mouse())
	r.waitFor("the run", func() bool { rs := r.svc().Results(); return len(rs) > 0 && rs[0].IsComplete() })
	if got := leaves(r.svc().Results()[0]); !slices.Equal(got, []string{"TestDivide"}) {
		t.Errorf("a click on the glyph of TestDivide ran %v", got)
	}
	// the glyph of the group runs both its subtests
	r.ts.TapAt(r.glyphPoint(e, 28), r.mouse())
	r.waitFor("the second run", func() bool { rs := r.svc().Results(); return len(rs) > 1 && rs[0].IsComplete() && !r.svc().IsRunning() })
	if got := leaves(r.svc().Results()[0]); !slices.Equal(got, []string{"TestGroup/one", "TestGroup/two"}) {
		t.Errorf("a click on the glyph of the group ran %v", got)
	}
	// the secondary button remembers the glyph for the menu of the editor, which lists what the test can do
	r.ts.TapAt(r.glyphPoint(e, 10), r.mouse(gestures.SecondaryButton))
	r.pump()
	for _, want := range []string{"Run Test", "Debug Test", "Run with Coverage", "Peek Error", "Reveal in Test Explorer"} {
		if !r.ts.Exists(gluttertest.ByText(want, gluttertest.FindOpts{FindRichText: true})) {
			t.Errorf("the context menu of the glyph of a failed test has no %q", want)
		}
	}
	if r.ts.Exists(gluttertest.ByText("Cut", gluttertest.FindOpts{FindRichText: true})) {
		t.Error("the menu of the editor opened over the menu of the glyph")
	}
	// the items of the glyph of the group and of its test, as the menu builds them
	var labels []string
	for _, it := range e.tst.glyphMenu([]string{id("TestGroup")}) {
		labels = append(labels, it.Label)
	}
	if want := []string{"Run Test", "Debug Test", "Run with Coverage", "Reveal in Test Explorer"}; !slices.Equal(labels, want) {
		t.Errorf("the menu of the group is %v, want %v", labels, want)
	}
	labels = nil
	for _, it := range e.tst.glyphMenu([]string{id("TestAdd"), id("TestDivide")}) {
		if !it.Separator {
			labels = append(labels, it.Label)
		}
	}
	if want := []string{"Run All Tests", "Debug All Tests", "Run All Tests with Coverage", "TestAdd", "TestDivide"}; !slices.Equal(labels, want) {
		t.Errorf("the menu of two tests is %v, want %v", labels, want)
	}
}

func TestFailureMessagesShowAtTheirLinesOfTheLastResultOnly(t *testing.T) {
	r, e := peekRig(t, testingOpts{})
	r.runCommand(command.TestingRunAll, nil)
	r.waitFor("the messages", func() bool { return len(e.view.TestMessages()) == 2 })
	msgs := e.view.TestMessages()
	if msgs[13] != "Divide(5, 2) = 2, want 2.5" || msgs[20] != "Report(12) is wrong" {
		t.Errorf("the messages are %v", msgs)
	}
	// a newer run of the test that passes hides what the older one said, unless every result is shown
	r.runCommand(command.TestingRun, id("TestAdd"))
	r.waitFor("the messages of the old run to go", func() bool { return len(e.view.TestMessages()) == 0 })
	r.set("testing.showAllMessages", true)
	r.waitFor("every message", func() bool { return len(e.view.TestMessages()) == 2 })
}

func TestThePeekShowsTheMessageItsDiffAndItsStackAndGoesFromFailureToFailure(t *testing.T) {
	r, e := peekRig(t, testingOpts{})
	r.runCommand(command.TestingRunAll, nil)
	r.waitFor("the messages", func() bool { return len(e.view.TestMessages()) == 2 })
	e.view.Cursors().SetSelections([]buffer.Selection{buffer.NewCursor(buffer.Position{Line: 19, Col: 0})})
	r.run(command.TestingOpenOutputPeek, nil)
	peek := e.tst.peek
	if peek == nil || !peek.Visible() {
		t.Fatal("Peek Output did not open the peek")
	}
	cur, _ := peek.Current()
	if cur.TestID != id("TestReport") || !cur.HasDiff || cur.Expected != "Total: 12\nTax: 2.40" || cur.Line != 20 || cur.Title != "TestReport" {
		t.Errorf("the peek shows %+v, want the failure of TestReport nearest to the cursor", cur)
	}
	if b := peek.Body(); b == nil || b.Widget == nil {
		t.Error("the message has a diff and the peek has no small diff editor")
	}
	if r.win.Keys.Get("testing.isPeekVisible") != true {
		t.Error("testing.isPeekVisible is not set while the peek shows")
	}
	// Alt+F8 goes to the next failure going round, Shift+Alt+F8 back
	e.focus.Request()
	r.pump()
	r.press("alt+f8")
	cur, _ = peek.Current()
	if cur.TestID != id("TestDivide") || len(cur.Stack) != 2 || cur.Stack[0].Line != 14 || cur.Stack[0].Path == "" || !strings.Contains(cur.Output, "FAIL") {
		t.Errorf("after Alt+F8 the peek shows %+v, want TestDivide with its stack and output", cur)
	}
	if peek.Body() != nil {
		t.Error("a message with no diff has a small diff editor")
	}
	r.press("shift+alt+f8")
	if cur, _ = peek.Current(); cur.TestID != id("TestReport") {
		t.Errorf("after Shift+Alt+F8 the peek shows %q", cur.TestID)
	}
	if i, n := peek.Position(); i != 2 || n != 2 {
		t.Errorf("the place of the message is %d of %d", i, n)
	}
	// Escape closes it
	r.press("escape")
	if peek.Visible() || r.win.Keys.Get("testing.isPeekVisible") != false {
		t.Error("Escape did not close the peek")
	}
	// Alt+F8 does nothing while there is no peek, the command opens it
	r.press("alt+f8")
	if peek.Visible() {
		t.Error("Alt+F8 opened the peek when it was not showing")
	}
	r.run(command.TestingGoToNextMessage, nil)
	if !peek.Visible() {
		t.Error("Go to Next Test Failure did not open the peek")
	}
	// a newer run that has neither failure takes the peek away with its messages
	r.runCommand(command.TestingRun, id("TestAdd"))
	r.waitFor("the peek to follow the results", func() bool { return len(e.view.TestMessages()) == 0 && !peek.Visible() })
	if r.win.Keys.Get("testing.isPeekVisible") != false {
		t.Error("testing.isPeekVisible stays set after the peek went with its message")
	}
}

func TestPeekErrorOfATestOpensItsFileAndTheFailureOfTheTest(t *testing.T) {
	r, e := peekRig(t, testingOpts{})
	r.runCommand(command.TestingRunAll, nil)
	r.run(command.TestingPeekError, id("TestDivide"))
	r.waitFor("the peek", func() bool { return e.tst.peek != nil && e.tst.peek.Visible() })
	if cur, _ := e.tst.peek.Current(); cur.TestID != id("TestDivide") {
		t.Errorf("the peek shows %q", cur.TestID)
	}
	// the cursor went to the line of the message
	if line := e.view.Cursors().Primary().Position().Line; line != 13 {
		t.Errorf("the cursor is on line %d, want 13", line)
	}
}

func TestTheSettingsDecideWhetherAFailureOpensThePeekByItself(t *testing.T) {
	tests := []struct {
		name    string
		setting string
		open    bool
		want    bool
	}{
		{"never", "never", true, false},
		{"anywhere, the file open", "failureAnywhere", true, true},
		{"anywhere, the file not open", "failureAnywhere", false, true},
		{"in a visible document, the file open", "failureInVisibleDocument", true, true},
		{"in a visible document, the file not open", "failureInVisibleDocument", false, false},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			o := testingOpts{settings: map[string]any{"testing.automaticallyOpenPeekView": tc.setting}}
			o.open = []string{"calc_test.go"}
			if !tc.open {
				o.open = []string{"other.go"}
				o.files = map[string]string{"other.go": "package calc\n"}
			}
			r := newTestingRig(t, withPeekFiles(o))
			r.run(command.TestingRunAll, nil)
			r.finished()
			peeked := func() bool {
				for _, e := range r.win.shell.group.editors {
					if e.tst != nil && e.tst.peek != nil && e.tst.peek.Visible() {
						return true
					}
				}
				return false
			}
			if tc.want {
				r.waitFor("the peek", peeked)
				return
			}
			r.pump()
			time.Sleep(50 * time.Millisecond)
			r.pump()
			if peeked() {
				t.Error("a peek opened")
			}
		})
	}
}

// withPeekFiles adds the test file and the script to the options of a rig.
func withPeekFiles(o testingOpts) testingOpts {
	if o.script == "" {
		o.script = peekScript
	}
	if o.files == nil {
		o.files = map[string]string{}
	}
	o.files["calc_test.go"] = peekSource
	if _, ok := o.settings["testing.openTesting"]; !ok {
		o.settings["testing.openTesting"] = "neverOpen"
	}
	return o
}

func TestOpenTestingDecidesWhichViewAStartOpens(t *testing.T) {
	tests := []struct {
		setting string
		panel   string
		sidebar string
	}{
		{"neverOpen", "", ""},
		{"openOnTestStart", command.ViewContainerTestResults, ""},
		{"openOnTestFailure", command.ViewContainerTestResults, ""},
		{"openExplorerOnTestStart", "", command.ViewContainerTesting},
	}
	for _, tc := range tests {
		t.Run(tc.setting, func(t *testing.T) {
			r, _ := peekRig(t, testingOpts{settings: map[string]any{"testing.openTesting": tc.setting}})
			r.win.Views.Open(command.ViewContainerExplorer)
			r.win.Layout.SetPanelVisible(false)
			r.run(command.TestingRunAll, nil)
			r.finished()
			r.pump()
			gotPanel := ""
			if r.win.Layout.State().PanelVisible {
				gotPanel = r.win.Views.ActivePanel()
			}
			gotSide := ""
			if r.win.Views.Active() == command.ViewContainerTesting {
				gotSide = command.ViewContainerTesting
			}
			if gotPanel != tc.panel || gotSide != tc.sidebar {
				t.Errorf("a run opened the panel %q and the side bar %q, want %q and %q", gotPanel, gotSide, tc.panel, tc.sidebar)
			}
		})
	}
	t.Run("a run from the editor opens nothing", func(t *testing.T) {
		r, e := peekRig(t, testingOpts{settings: map[string]any{"testing.openTesting": "openOnTestStart"}})
		r.win.Layout.SetPanelVisible(false)
		e.tst.glyphPress(editor.TestPress{Line: 10, IDs: []string{id("TestDivide")}})
		r.waitFor("the run", func() bool { rs := r.svc().Results(); return len(rs) > 0 && rs[0].IsComplete() })
		if r.win.Layout.State().PanelVisible {
			t.Error("a run that keeps the focus opened Test Results")
		}
	})
}

func TestFollowingTheRunningTestRevealsItInTheExplorer(t *testing.T) {
	for _, always := range []bool{false, true} {
		t.Run(map[bool]string{false: "folded rows stay folded", true: "always reveal"}[always], func(t *testing.T) {
			script := `{"delayMs": 1, "tests": [{"id": "g", "label": "g", "children": [
				{"id": "A", "label": "A", "result": "passed", "ms": 1}, {"id": "B", "label": "B", "result": "passed", "ms": 1}]}]}`
			r := newTestingRig(t, testingOpts{script: script, settings: map[string]any{"testing.followRunningTest": true,
				"testing.alwaysRevealTestOnStateChange": always, "testing.openTesting": "neverOpen"}})
			r.run(command.ViewContainerTesting, nil)
			ex := r.explorer()
			ex.Model().Collapse(ex.Element(id("g")).Key)
			r.run(command.TestingRunAll, nil)
			r.finished()
			r.pump()
			b := ex.Element(id("g", "B"))
			selected, _ := ex.Model().Selected()
			if always && selected != b.Key {
				t.Errorf("the selected row is %q, want the last test that ran (%q)", selected, b.Key)
			}
			if !always && selected != "" {
				t.Errorf("a folded group was opened to reveal %q", selected)
			}
		})
	}
	t.Run("off: nothing follows", func(t *testing.T) {
		r := newTestingRig(t, testingOpts{settings: map[string]any{"testing.openTesting": "neverOpen"}})
		r.run(command.ViewContainerTesting, nil)
		r.run(command.TestingRunAll, nil)
		r.finished()
		if sel, ok := r.explorer().Model().Selected(); ok {
			t.Errorf("the row %q was selected with followRunningTest off", sel)
		}
	})
}

func TestSaveBeforeTestSavesTheFilesWithChanges(t *testing.T) {
	for _, save := range []bool{true, false} {
		t.Run(map[bool]string{true: "on", false: "off"}[save], func(t *testing.T) {
			r, e := peekRig(t, testingOpts{settings: map[string]any{"testing.saveBeforeTest": save}})
			at := buffer.Position{Line: 0, Col: 0}
			if _, err := e.doc.Edit([]document.Edit{{Range: buffer.Range{Start: at, End: at}, Text: "// edited\n"}}, document.EditOptions{}); err != nil {
				t.Fatal(err)
			}
			if !e.doc.Dirty() {
				t.Fatal("the document is not dirty")
			}
			r.runCommand(command.TestingRunAll, nil)
			r.waitFor("the save", func() bool { return e.doc.Dirty() != save })
			data, err := os.ReadFile(filepath.Join(r.folder, "calc_test.go"))
			if err != nil {
				t.Fatal(err)
			}
			if got := strings.HasPrefix(string(data), "// edited"); got != save {
				t.Errorf("the file has the change: %v, want %v", got, save)
			}
		})
	}
}

func TestTestResultsShowsTheRunsTheirTestsAndTheirMessages(t *testing.T) {
	r, _ := peekRig(t, testingOpts{settings: map[string]any{"testing.openTesting": "openOnTestStart"}})
	r.run(command.TestingRunAll, nil)
	r.finished()
	r.pump()
	if r.win.Views.ActivePanel() != command.ViewContainerTestResults || !r.win.Layout.State().PanelVisible {
		t.Fatalf("the panel shows %q", r.win.Views.ActivePanel())
	}
	res := r.win.shell.testing.results
	var labels []string
	for _, n := range res.Model().Rows() {
		labels = append(labels, n.Label)
	}
	joined := strings.Join(labels, "|")
	for _, want := range []string{"TestDivide", "Divide(5, 2) = 2, want 2.5", "TestReport", "Report(12) is wrong"} {
		if !strings.Contains(joined, want) {
			t.Errorf("Test Results has %q, want %q in it", joined, want)
		}
	}
	// a message shows its text, the diff and the stack
	var msgKey string
	for _, n := range res.Model().Rows() {
		if strings.HasPrefix(n.Label, "Divide(5, 2)") {
			msgKey = n.Key
		}
	}
	if !res.Select(msgKey) {
		t.Fatal("the message cannot be selected")
	}
	n, _ := res.Selected()
	text := testview.NodeText(n, nil)
	if !strings.Contains(text, "Divide(5, 2) = 2, want 2.5") || !strings.Contains(text, "Stack") || !strings.Contains(text, "calc.TestDivide") {
		t.Errorf("the text of the message is %q", text)
	}
	// a test shows its output with the colors
	for _, n := range res.Model().Rows() {
		if n.Label == "TestDivide" {
			res.Select(n.Key)
		}
	}
	n, _ = res.Selected()
	if got := testview.NodeText(n, nil); !strings.Contains(got, "\x1b[31mFAIL\x1b[0m TestDivide") {
		t.Errorf("the text of the test is %q", got)
	}
	if errs := r.ts.TakeErrors(); len(errs) != 0 {
		t.Errorf("errors: %v", errs)
	}
}

func TestAClientsEditorShowsTheGlyphsAndTheMessagesOfTheHostsResults(t *testing.T) {
	r, hostEditor := peekRig(t, testingOpts{})
	v, err := r.win.makeHeadlessView(extension.Size{Width: 900, Height: 560}, rigFrameRate)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(v.Close)
	var frames atomic.Int64
	v.OnFrame(func(*image.RGBA) { frames.Add(1) })
	r.waitFor("the view to draw", func() bool { return frames.Load() > 0 && v.ws.shell != nil && v.ws.shell.testing.explorer != nil })
	client := v.ws
	sh := client.shell
	sh.openPathAt(sh.binding, filepath.Join(r.folder, "calc_test.go"), 0, 0, openOptions{})
	var clientEditor *textEditor
	r.waitFor("the client's editor", func() bool {
		clientEditor = sh.activeEditor()
		return clientEditor != nil && clientEditor.tst != nil && len(clientEditor.view.TestGlyphs()) == 7
	})
	// a click on the glyph in the client's editor runs the test on the host, and both screens show the failure
	clientEditor.tst.glyphPress(editor.TestPress{Line: 10, IDs: []string{id("TestDivide")}})
	r.waitFor("the run on both screens", func() bool {
		return len(hostEditor.view.TestMessages()) == 1 && len(clientEditor.view.TestMessages()) == 1
	})
	if msgs := clientEditor.view.TestMessages(); msgs[13] != "Divide(5, 2) = 2, want 2.5" {
		t.Errorf("the client's messages are %v", msgs)
	}
	for name, e := range map[string]*textEditor{"host": hostEditor, "client": clientEditor} {
		if icon := glyphLines(e)[10]; icon != testview.StateIcon(coretesting.StateFailed) {
			t.Errorf("the %s's glyph of TestDivide has the icon %#x", name, icon)
		}
	}
	// the peek of the client opens on the client's screen only
	clientEditor.tst.peekController().Step(true)
	if !clientEditor.tst.peek.Visible() || hostEditor.tst.peek != nil {
		t.Errorf("the peek is on the client (%v) and not on the host (%v)", clientEditor.tst.peek.Visible(), hostEditor.tst.peek != nil)
	}
}

// TestEscapeClosesThePeekOfTwoControllersThatBothFailInTheFile mirrors the case a real-input test saw once in step 26.6: the Go
// controller and the fake one report failures in the same file, the peek shows "1 of N" and Escape must close it.
func TestEscapeClosesThePeekOfTwoControllersThatBothFailInTheFile(t *testing.T) {
	files := map[string]string{
		"go.mod": "module calc\n\ngo 1.21\n",
		"calc.go": `package calc

import "fmt"

func Add(a, b int) int { return a + b }

func Divide(a, b int) (float64, error) { return float64(a / b), nil }

func Report(n int) string { return fmt.Sprintf("Total: %d\nTax: %.2f", n, float64(n)*0.17) }
`,
	}
	r, e := peekRig(t, testingOpts{files: files, extensions: []extension.Factory{golang.New}})
	r.win.Extensions().Activate(golang.ID)
	// the go commands of the controller may still write to the build cache (inside the temporary folders) when the test ends
	t.Cleanup(func() { time.Sleep(500 * time.Millisecond) })
	r.waitFor("the Go controller's tests", func() bool { return r.svc().ItemCount("go") > 0 })
	r.runCommand(command.TestingRunAll, nil)
	r.waitFor("the messages of both controllers", func() bool { return len(e.view.TestMessages()) >= 2 })
	r.waitFor("the glyph of TestDivide", func() bool { _, ok := glyphLines(e)[10]; return ok })

	// the glyph of a failed test is clicked (Alt: nothing runs, the click only asks for the peek the way the setting says)
	e.view.Cursors().SetSelections([]buffer.Selection{buffer.NewCursor(buffer.Position{Line: 13, Col: 0})})
	r.run(command.TestingOpenOutputPeek, nil)
	peek := e.tst.peek
	if peek == nil || !peek.Visible() {
		t.Fatal("Peek Output did not open the peek")
	}
	if _, n := peek.Position(); n < 2 {
		t.Fatalf("the peek has %d messages, want the failures of both controllers", n)
	}
	r.run(command.TestingGoToNextMessage, nil)
	if i, n := peek.Position(); i < 2 || n < 2 {
		t.Errorf("the place of the message is %d of %d", i, n)
	}
	if r.win.Keys.Get("testing.isPeekVisible") != true {
		t.Fatal("testing.isPeekVisible is not set while the peek shows")
	}
	// Escape with the focus in the editor, then again after the peek's content changed with the focus in the peek
	for _, focusPeek := range []bool{false, true} {
		if !peek.Visible() {
			r.run(command.TestingGoToNextMessage, nil)
		}
		if focusPeek {
			if b := peek.Body(); b != nil && b.Focus != nil {
				b.Focus()
			}
		} else {
			e.focus.Request()
		}
		r.pump()
		r.press("escape")
		if peek.Visible() || r.win.Keys.Get("testing.isPeekVisible") != false {
			t.Errorf("Escape did not close the peek (focus in the peek: %v, %d of %d)", focusPeek, func() int { i, _ := peek.Position(); return i }(), func() int { _, n := peek.Position(); return n }())
		}
	}
}
