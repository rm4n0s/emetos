package app

import (
	"context"
	"fmt"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/rm4n0s/errors"
	"github.com/rm4n0s/glutter/gluttertest"
	"github.com/rm4n0s/glutter/services"
	"github.com/rm4n0s/glutter/ui"

	"github.com/rm4n0s/emetos/internal/core/command"
	"github.com/rm4n0s/emetos/internal/core/logs"
	"github.com/rm4n0s/emetos/internal/gui/editor"
	"github.com/rm4n0s/emetos/internal/gui/kit"
	"github.com/rm4n0s/emetos/internal/gui/testutil"
	"github.com/rm4n0s/emetos/internal/gui/views/output"
	"github.com/rm4n0s/emetos/internal/gui/views/problems"
	"github.com/rm4n0s/emetos/internal/gui/workbench"
)

// utilSource has a line with characters that take more than one byte and more than one UTF-16 unit.
const utilSource = "package util\n\n// héllo\U0001F600 x\nfunc Helper() {}\n"

// panelApp is a window with two files (main.go is open) whose panel is the test's to open.
type panelApp struct {
	*groupApp
}

func newPanelApp(t *testing.T, theme string) *panelApp {
	t.Helper()
	if !DevCommandsEnabled() {
		t.Skip("this build does not run dev commands")
	}
	extra := map[string]any{"editor.minimap.enabled": false, "breadcrumbs.enabled": false}
	if theme != "" {
		extra["workbench.colorTheme"] = theme
	}
	win, _ := themedWindow(t, extra)
	a := newGroupAppWith(t, win, ui.Size{Width: 960, Height: 560}, []string{"main.go", "util.go"},
		map[string]string{"main.go": goldenSource, "util.go": utilSource}, "main.go")
	// the text of the window is in the test font, not in a font of the machine
	a.ts.SetPlatformConfiguration(func(c *ui.PlatformConfiguration) { c.UIFontFamily, c.UIFontSize = testutil.MonoFamily, 12 })
	a.pump()
	return &panelApp{a}
}

// do runs a command, as the dev commands do, and returns its error.
func (a *panelApp) do(id string, args any) error {
	a.t.Helper()
	err := a.win.Process.Commands.Run(command.Context{Ctx: context.Background(), Keys: a.win.Keys, Source: command.SourceAPI}, id, args)
	a.pump()
	return err
}

// mustDo runs a command that must succeed.
func (a *panelApp) mustDo(id string, args any) {
	a.t.Helper()
	if err := a.do(id, args); err != nil {
		a.t.Fatalf("%s: %v", id, err)
	}
}

// marker is the argument of one marker of dev.setMarkers.
func marker(severity, message string, line, col int, rest ...string) map[string]any {
	m := map[string]any{"severity": severity, "message": message, "startLine": float64(line), "startColumn": float64(col)}
	if len(rest) > 0 {
		m["source"] = rest[0]
	}
	if len(rest) > 1 {
		m["code"] = rest[1]
	}
	return m
}

// setMarkers sets the markers of an owner for a file of the app.
func (a *panelApp) setMarkers(file string, markers ...any) {
	a.t.Helper()
	a.mustDo("dev.setMarkers", map[string]any{"owner": "test", "resource": a.files[file], "markers": markers})
}

// fillProblems gives the two files markers: three in main.go and two in util.go.
func (a *panelApp) fillProblems() {
	a.t.Helper()
	a.setMarkers("main.go",
		marker("warning", "unused variable x", 9, 2, "compiler"),
		marker("error", "undefined: foo", 13, 6, "compiler", "E200"),
		marker("error", "missing return", 3, 1, "vet"))
	a.setMarkers("util.go",
		marker("info", "could be simplified", 4, 1, "lint"),
		marker("error", "bad accent", 3, 12))
}

// outputText is the text of the Output panel. The editor paints its text itself, so a finder does not
// see it.
func (a *panelApp) outputText() string { return a.win.Output.Document().Text() }

func (a *panelApp) has(text string) bool { return a.ts.Exists(gluttertest.ByText(text)) }

// tap presses the first widget with the text.
func (a *panelApp) tap(text string) {
	a.t.Helper()
	a.ts.TapAt(a.ts.Center(gluttertest.First(gluttertest.ByText(text))), mouseButton)
	a.pump()
}

// editorView is the view model of the editor of the group, not the one of the Output panel.
func (a *panelApp) editorView() *editor.ViewModel { return a.shell().activeEditor().view }

func TestPanelTabsAndTheCommandsThatOpenThem(t *testing.T) {
	a := newPanelApp(t, "")
	l := a.win.Layout
	if l.State().PanelVisible || a.win.Views.ActivePanel() != command.ViewContainerMarkers {
		t.Fatalf("the panel starts visible=%v with %q", l.State().PanelVisible, a.win.Views.ActivePanel())
	}
	steps := []struct {
		name      string
		command   string
		visible   bool
		active    string
		wantTexts []string
	}{
		{"the Problems command opens the panel on the Problems", "workbench.actions.view.problems", true, command.ViewContainerMarkers, []string{"PROBLEMS", "OUTPUT"}},
		{"the Output command changes the tab", "workbench.action.output.toggleOutput", true, command.ViewContainerOutput, nil},
		{"the Output command with the Output focused hides the panel", "workbench.action.output.toggleOutput", false, command.ViewContainerOutput, nil},
		{"the Output command opens it again", "workbench.action.output.toggleOutput", true, command.ViewContainerOutput, nil},
		{"the Problems focus command changes the tab", "workbench.panel.markers.view.focus", true, command.ViewContainerMarkers, nil},
		{"the Output focus command", "workbench.panel.output.focus", true, command.ViewContainerOutput, nil},
		{"Focus Problems", "workbench.action.problems.focus", true, command.ViewContainerMarkers, nil},
		{"toggleProblems hides the Problems that are shown", "workbench.actions.view.toggleProblems", false, command.ViewContainerMarkers, nil},
		{"toggleProblems shows them", "workbench.actions.view.toggleProblems", true, command.ViewContainerMarkers, nil},
		{"showErrorsWarnings shows the Problems without a toggle", "workbench.action.showErrorsWarnings", true, command.ViewContainerMarkers, nil},
	}
	for _, s := range steps {
		a.mustDo(s.command, nil)
		if got := l.State().PanelVisible; got != s.visible {
			t.Errorf("%s: panel visible = %v, want %v", s.name, got, s.visible)
		}
		if got := a.win.Views.ActivePanel(); got != s.active {
			t.Errorf("%s: active panel %q, want %q", s.name, got, s.active)
		}
		if got := a.win.Keys.Get("activePanel"); got != s.active {
			t.Errorf("%s: activePanel = %v, want %q", s.name, got, s.active)
		}
		want := s.visible && s.active == command.ViewContainerMarkers
		if got := a.win.Keys.Get("problemsVisibility"); got != want {
			t.Errorf("%s: problemsVisibility = %v, want %v", s.name, got, want)
		}
		for _, text := range s.wantTexts {
			if !a.has(text) {
				t.Errorf("%s: no tab %q", s.name, text)
			}
		}
	}
	a.noErrors()
}

func TestPanelTabsSwitchByClick(t *testing.T) {
	a := newPanelApp(t, "")
	a.mustDo("workbench.action.togglePanel", nil)
	a.tap("OUTPUT")
	if a.win.Views.ActivePanel() != command.ViewContainerOutput || !a.ts.Exists(gluttertest.ByKey(output.EditorKey)) {
		t.Errorf("clicking OUTPUT: active %q", a.win.Views.ActivePanel())
	}
	a.tap("PROBLEMS")
	if a.win.Views.ActivePanel() != command.ViewContainerMarkers || a.ts.Exists(gluttertest.ByKey(output.EditorKey)) {
		t.Errorf("clicking PROBLEMS: active %q", a.win.Views.ActivePanel())
	}
	a.noErrors()
}

func TestPanelKeysRunThroughTheDispatcher(t *testing.T) {
	a := newPanelApp(t, "")
	a.press("ctrl+shift+m")
	if !a.win.Layout.State().PanelVisible || a.win.Views.ActivePanel() != command.ViewContainerMarkers {
		t.Errorf("ctrl+shift+m: visible %v, active %q", a.win.Layout.State().PanelVisible, a.win.Views.ActivePanel())
	}
	a.press("ctrl+k ctrl+h")
	if a.win.Views.ActivePanel() != command.ViewContainerOutput {
		t.Errorf("ctrl+k ctrl+h: active %q", a.win.Views.ActivePanel())
	}
	// the focus is in the Output now, so the same keys hide the panel
	a.press("ctrl+k ctrl+h")
	if a.win.Layout.State().PanelVisible {
		t.Error("ctrl+k ctrl+h with the Output focused did not hide the panel")
	}
	a.noErrors()
}

func TestPanelFocusKeysAndContextKeys(t *testing.T) {
	a := newPanelApp(t, "")
	a.fillProblems()
	a.mustDo("workbench.actions.view.problems", nil)
	if a.win.Keys.Get("problemFocus") != true || a.win.Keys.Get("focusedView") != command.ViewMarkers {
		t.Errorf("after opening: problemFocus %v, focusedView %v", a.win.Keys.Get("problemFocus"), a.win.Keys.Get("focusedView"))
	}
	// ctrl+f in the Problems goes to the filter, and Escape there clears it
	a.press("ctrl+f")
	if a.win.Keys.Get("problemsFilterFocus") != true || !a.win.Problems.Filter().HasFocus() {
		t.Fatalf("ctrl+f: problemsFilterFocus %v", a.win.Keys.Get("problemsFilterFocus"))
	}
	a.ts.EnterText(gluttertest.ByKey(problems.FilterKey), "undefined")
	a.pump()
	if got := a.win.Problems.Model().Options().Text; got != "undefined" {
		t.Fatalf("the filter text is %q", got)
	}
	a.press("escape")
	if got := a.win.Problems.Model().Options().Text; got != "" {
		t.Errorf("escape in the filter left %q", got)
	}
	// ctrl+down goes back to the problems
	a.press("ctrl+down")
	if a.win.Keys.Get("problemsFilterFocus") == true || a.win.Keys.Get("problemFocus") != true {
		t.Errorf("ctrl+down: problemsFilterFocus %v, problemFocus %v", a.win.Keys.Get("problemsFilterFocus"), a.win.Keys.Get("problemFocus"))
	}
	a.noErrors()
}

func TestProblemsTreeGroupsByFileAndCountsOnTheTab(t *testing.T) {
	a := newPanelApp(t, "")
	a.mustDo("workbench.actions.view.problems", nil)
	if !a.has(problems.MessageNoProblems) {
		t.Fatal("no message about there being no problems")
	}
	if a.ts.Exists(gluttertest.ByKey(workbench.PanelBadgeKey(command.ViewContainerMarkers))) {
		t.Error("the tab has a badge with no problems")
	}
	a.fillProblems()
	if a.has(problems.MessageNoProblems) {
		t.Error("the message stays while there are problems")
	}
	// two files, each with its markers; the file with an error that comes first
	for _, text := range []string{"main.go", "util.go", "undefined: foo", "missing return", "unused variable x", "could be simplified",
		"compiler(E200)", "vet", "[Ln 13, Col 6]", "[Ln 3, Col 1]", "[Ln 9, Col 2]"} {
		if !a.has(text) {
			t.Errorf("the panel has no %q", text)
		}
	}
	// within main.go: the errors by position, then the warning
	for _, order := range [][2]string{{"main.go", "missing return"}, {"missing return", "undefined: foo"}, {"undefined: foo", "unused variable x"}, {"unused variable x", "util.go"}} {
		y := func(s string) float64 { return a.ts.Center(gluttertest.First(gluttertest.ByText(s))).Dy }
		if y(order[0]) >= y(order[1]) {
			t.Errorf("%q is not above %q", order[0], order[1])
		}
	}
	badge := a.ts.Widget(gluttertest.ByKey(workbench.PanelBadgeKey(command.ViewContainerMarkers)))
	if badge == nil || !a.has("5") {
		t.Errorf("the tab shows no count of 5")
	}
	// the counts of the files
	counts := a.ts.Elements(gluttertest.ByType[*kit.Badge]())
	var got []int
	for _, e := range counts {
		got = append(got, e.Widget().(*kit.Badge).Count())
	}
	if fmt.Sprint(got) != "[3 2]" {
		t.Errorf("the counts of the files are %v, want [3 2]", got)
	}
	// removing the markers of a file takes it away and lowers the count
	a.mustDo("dev.setMarkers", map[string]any{"owner": "test", "resource": a.files["util.go"]})
	if a.has("util.go") || a.has("could be simplified") {
		t.Error("util.go is still shown after its markers were removed")
	}
	if got := a.win.Markers.Stats().Total(); got != 3 {
		t.Errorf("total = %d, want 3", got)
	}
	a.noErrors()
}

func TestProblemsCollapse(t *testing.T) {
	a := newPanelApp(t, "")
	a.fillProblems()
	a.mustDo("workbench.actions.view.problems", nil)
	// a click on a file folds it
	a.tap("util.go")
	if a.has("could be simplified") || a.has("bad accent") || !a.has("undefined: foo") {
		t.Errorf("after folding util.go: simplified=%v accent=%v foo=%v", a.has("could be simplified"), a.has("bad accent"), a.has("undefined: foo"))
	}
	a.ts.Pump(time.Second) // not a double click
	a.tap("util.go")
	if !a.has("could be simplified") {
		t.Error("a second click did not unfold util.go")
	}
	// collapse all, by the button and by the command
	a.ts.TapAt(a.ts.Center(gluttertest.ByKey(problems.CollapseAllKey)), mouseButton)
	a.pump()
	if a.has("undefined: foo") || a.has("could be simplified") || !a.has("main.go") || !a.has("util.go") {
		t.Error("collapse all left markers or lost files")
	}
	a.ts.Pump(time.Second)
	a.tap("main.go")
	a.mustDo("workbench.actions.treeView.workbench.panel.markers.view.collapseAll", nil)
	if a.has("undefined: foo") {
		t.Error("the command did not collapse the tree")
	}
	// a file that gets new markers stays folded; a file that comes back is unfolded
	a.setMarkers("main.go", marker("error", "another", 1, 1))
	if a.has("another") {
		t.Error("a folded file unfolded when its markers changed")
	}
	a.mustDo("dev.setMarkers", map[string]any{"owner": "test", "resource": a.files["main.go"]})
	a.setMarkers("main.go", marker("error", "back again", 1, 1))
	if !a.has("back again") {
		t.Error("a file that came back is folded")
	}
	a.noErrors()
}

func TestProblemsClickOpensTheFileAtTheMarker(t *testing.T) {
	a := newPanelApp(t, "")
	a.fillProblems()
	a.mustDo("workbench.actions.view.problems", nil)

	// a marker of the open file: the cursor goes to the start of its range
	a.tap("undefined: foo")
	if got := a.active(); got != "main.go" {
		t.Fatalf("the active tab is %s", got)
	}
	p := a.editorView().Cursors().Primary()
	if p.Position().Line != 12 || p.Position().Col != 5 {
		t.Errorf("cursor at %+v, want line 12 col 5 (Ln 13, Col 6)", p.Position())
	}

	// a marker of a file that is not open: it opens as a preview, and its column counts UTF-16 units
	a.tap("bad accent")
	if a.active() != "util.go" {
		t.Fatalf("the active tab is %s, tabs %v", a.active(), a.tabNames())
	}
	if !a.item("util.go").Preview {
		t.Error("the file opened by a click is not a preview")
	}
	got := a.editorView().Cursors().Primary().Position()
	// "// héllo😀 x": the x is the 12th UTF-16 unit and the 14th byte
	if got.Line != 2 || got.Col != 14 {
		t.Errorf("cursor at %+v, want line 2 col 14", got)
	}

	// a double click keeps it
	a.ts.TapAt(a.ts.Center(gluttertest.ByText("could be simplified")), mouseButton)
	a.ts.TapAt(a.ts.Center(gluttertest.ByText("could be simplified")), mouseButton)
	a.pump()
	if a.item("util.go").Preview {
		t.Error("a double click left the file as a preview")
	}
	a.noErrors()
}

func TestProblemsEnterOpensTheFocusedMarker(t *testing.T) {
	a := newPanelApp(t, "")
	a.setMarkers("util.go", marker("error", "only one", 4, 3))
	a.mustDo("workbench.actions.view.problems", nil)
	if a.win.Keys.Get("problemFocus") != true {
		t.Fatal("the tree has no focus")
	}
	// the file, then its marker
	a.ts.SendKeyEvent(services.LogicalKeyboardKeyArrowDown, gluttertest.KeyOpts{Physical: services.PhysicalKeyboardKeyArrowDown})
	a.ts.SendKeyEvent(services.LogicalKeyboardKeyArrowDown, gluttertest.KeyOpts{Physical: services.PhysicalKeyboardKeyArrowDown})
	a.ts.SendKeyEvent(services.LogicalKeyboardKeyEnter, gluttertest.KeyOpts{Physical: services.PhysicalKeyboardKeyEnter})
	a.pump()
	if a.active() != "util.go" {
		t.Fatalf("the active tab is %s", a.active())
	}
	if got := a.editorView().Cursors().Primary().Position(); got.Line != 3 || got.Col != 2 {
		t.Errorf("cursor at %+v, want line 3 col 2", got)
	}
	a.noErrors()
}

func TestProblemsFilter(t *testing.T) {
	a := newPanelApp(t, "")
	a.fillProblems()
	a.mustDo("workbench.actions.view.problems", nil)

	a.ts.EnterText(gluttertest.ByKey(problems.FilterKey), "undefined")
	a.pump()
	if !a.has("undefined: foo") || a.has("missing return") || a.has("util.go") {
		t.Errorf("text filter: foo=%v missing=%v util=%v", a.has("undefined: foo"), a.has("missing return"), a.has("util.go"))
	}
	// the badge on the tab counts all the problems, not the filtered ones
	if !a.has("5") {
		t.Error("the badge of the tab follows the filter")
	}
	// nothing matches: the message with the link that clears the filters
	a.ts.EnterText(gluttertest.ByKey(problems.FilterKey), "zzz")
	a.pump()
	if !a.has(problems.MessageNoResults) || !a.ts.Exists(gluttertest.ByKey(problems.ClearFiltersKey)) {
		t.Fatal("no message about the filter")
	}
	a.ts.TapAt(a.ts.Center(gluttertest.ByKey(problems.ClearFiltersKey)), mouseButton)
	a.pump()
	if !a.has("undefined: foo") || a.win.Problems.Filter().Value() != "" || a.win.Problems.Model().Filtering() {
		t.Error("Clear Filters did not clear the filter")
	}

	// negation
	a.ts.EnterText(gluttertest.ByKey(problems.FilterKey), "!undefined")
	a.pump()
	if a.has("undefined: foo") || !a.has("missing return") {
		t.Error("a negated text did not hide what matches")
	}
	a.win.Problems.ClearFilterText()
	a.pump()

	// the severity toggles
	a.ts.TapAt(a.ts.Center(gluttertest.ByKey(problems.ToggleKey(problems.SeverityError))), mouseButton)
	a.pump()
	if a.has("undefined: foo") || !a.has("unused variable x") || a.win.Keys.Get("problems.filter.errors") != false {
		t.Errorf("without errors: foo=%v unused=%v key=%v", a.has("undefined: foo"), a.has("unused variable x"), a.win.Keys.Get("problems.filter.errors"))
	}
	a.ts.TapAt(a.ts.Center(gluttertest.ByKey(problems.ToggleKey(problems.SeverityWarning))), mouseButton)
	a.ts.TapAt(a.ts.Center(gluttertest.ByKey(problems.ToggleKey(problems.SeverityInfo))), mouseButton)
	a.pump()
	if !a.has(problems.MessageNoResults) {
		t.Error("no message with every severity off")
	}
	// the commands do the same
	for _, id := range []string{"toggleErrors", "toggleWarnings", "toggleInfos"} {
		a.mustDo("workbench.actions.workbench.panel.markers.view."+id, nil)
	}
	if !a.has("undefined: foo") || !a.has("unused variable x") || !a.has("could be simplified") {
		t.Error("the toggle commands did not bring the severities back")
	}
	a.noErrors()
}

func TestProblemsActiveFileOnly(t *testing.T) {
	a := newPanelApp(t, "")
	a.fillProblems()
	a.mustDo("workbench.actions.view.problems", nil)
	a.mustDo("workbench.actions.workbench.panel.markers.view.toggleActiveFile", nil)
	if a.has("util.go") || !a.has("main.go") || a.win.Keys.Get("problems.filter.activeFile") != true {
		t.Errorf("active file only: util=%v main=%v key=%v", a.has("util.go"), a.has("main.go"), a.win.Keys.Get("problems.filter.activeFile"))
	}
	// another file becomes the active editor
	a.show("util.go", false)
	a.pump()
	if !a.has("util.go") || a.has("undefined: foo") {
		t.Errorf("after switching to util.go: util=%v foo=%v", a.has("util.go"), a.has("undefined: foo"))
	}
	// a file with no problems says so
	a.mustDo("dev.setMarkers", map[string]any{"owner": "test", "resource": a.files["util.go"]})
	if !a.has(problems.MessageNoProblemsActiveFile) {
		t.Error("no message about the current file")
	}
	a.noErrors()
}

func TestOutputShowsTheChannelsAndSwitchesThroughThePicker(t *testing.T) {
	a := newPanelApp(t, "")
	a.mustDo("dev.log", map[string]any{"message": "first line"})
	a.mustDo("dev.log", map[string]any{"channel": "Tasks", "message": "task output"})
	a.mustDo("workbench.action.output.toggleOutput", nil)
	if got := a.outputText(); got != "first line" {
		t.Fatalf("the Emetos channel is not shown alone: %q", got)
	}
	dd := a.ts.Widget(gluttertest.ByKey(output.ChannelsKey)).(*kit.Dropdown)
	var labels []string
	for _, o := range dd.Options() {
		labels = append(labels, o.Label)
	}
	if fmt.Sprint(labels) != "[Emetos Tasks]" || dd.Selected() != 0 {
		t.Errorf("the picker has %v with %d selected", labels, dd.Selected())
	}
	// open the list and pick the other channel
	a.ts.TapAt(a.ts.Center(gluttertest.ByKey(output.ChannelsKey)), mouseButton)
	a.pump()
	a.tap("Tasks")
	if a.win.Output.Active() != "Tasks" || a.outputText() != "task output" {
		t.Errorf("after the pick: active %q, text %q", a.win.Output.Active(), a.outputText())
	}
	if a.win.Keys.Get("activeOutputChannel") != "Tasks" {
		t.Errorf("activeOutputChannel = %v", a.win.Keys.Get("activeOutputChannel"))
	}
	// a channel that comes while the panel is open is in the picker
	a.mustDo("dev.log", map[string]any{"channel": "Git", "message": "git output"})
	dd = a.ts.Widget(gluttertest.ByKey(output.ChannelsKey)).(*kit.Dropdown)
	if len(dd.Options()) != 3 || dd.Selected() != 1 {
		t.Errorf("the picker has %d channels with %d selected", len(dd.Options()), dd.Selected())
	}
	a.noErrors()
}

func TestOutputTailsWhileTheWindowShowsIt(t *testing.T) {
	a := newPanelApp(t, "")
	a.mustDo("workbench.action.output.toggleOutput", nil)
	vm := a.win.Output.ViewModel()
	for i := range 60 {
		a.mustDo("dev.log", fmt.Sprintf("line %d", i))
	}
	if got := strings.Count(a.win.Output.Document().Text(), "\n") + 1; got != 60 {
		t.Fatalf("the document has %d lines, want 60", got)
	}
	a.pump()
	if vm.ScrollTop() == 0 || !vm.AtBottom() {
		t.Errorf("the end is not in view: scrollTop=%v atBottom=%v", vm.ScrollTop(), vm.AtBottom())
	}
	// more lines are followed
	end := vm.ScrollTop()
	a.mustDo("dev.log", "line 60")
	a.pump()
	if vm.ScrollTop() <= end || !vm.AtBottom() {
		t.Errorf("a new line is not followed: %v -> %v, atBottom=%v", end, vm.ScrollTop(), vm.AtBottom())
	}

	// the user scrolls up: the lines that come do not take the view away
	a.ts.ScrollAt(a.ts.Center(gluttertest.ByKey(output.EditorKey)), ui.Offset{Dy: -400})
	a.pump()
	if vm.AtBottom() {
		t.Fatal("the editor did not scroll up")
	}
	top := vm.ScrollTop()
	a.mustDo("dev.log", "line 61")
	a.pump()
	if vm.ScrollTop() != top || vm.AtBottom() {
		t.Errorf("the view moved while scrolled up: %v -> %v", top, vm.ScrollTop())
	}
	// back at the end it follows again
	a.ts.ScrollAt(a.ts.Center(gluttertest.ByKey(output.EditorKey)), ui.Offset{Dy: 4000})
	a.pump()
	if !vm.AtBottom() {
		t.Fatal("the editor did not scroll to the end")
	}
	end = vm.ScrollTop()
	a.mustDo("dev.log", "line 62")
	a.pump()
	if vm.ScrollTop() <= end || !vm.AtBottom() {
		t.Errorf("it does not follow again once at the end: %v -> %v", end, vm.ScrollTop())
	}
	a.noErrors()
}

func TestOutputScrollLockAndClear(t *testing.T) {
	a := newPanelApp(t, "")
	a.mustDo("workbench.action.output.toggleOutput", nil)
	for i := range 60 {
		a.mustDo("dev.log", fmt.Sprintf("line %d", i))
	}
	a.pump()
	vm := a.win.Output.ViewModel()
	// the button locks, and the lines that come stay out of view
	a.ts.TapAt(a.ts.Center(gluttertest.ByKey(output.ScrollLockKey)), mouseButton)
	a.pump()
	if !a.win.Output.ScrollLock() || a.win.Keys.Get("outputView.scrollLock") != true {
		t.Fatal("the button did not lock the scrolling")
	}
	top := vm.ScrollTop()
	a.mustDo("dev.log", "line 60")
	a.pump()
	if vm.ScrollTop() != top || vm.AtBottom() {
		t.Errorf("a locked view followed a line: %v -> %v", top, vm.ScrollTop())
	}
	// the command unlocks and goes to the end
	a.mustDo("workbench.output.action.toggleAutoScroll", nil)
	a.pump()
	if a.win.Output.ScrollLock() || !vm.AtBottom() || vm.ScrollTop() <= top {
		t.Errorf("after unlocking: lock %v, atBottom %v, %v -> %v", a.win.Output.ScrollLock(), vm.AtBottom(), top, vm.ScrollTop())
	}
	// clear, by the button and by the command
	a.ts.TapAt(a.ts.Center(gluttertest.ByKey(output.ClearKey)), mouseButton)
	a.pump()
	if got := a.win.Output.Document().Text(); got != "" {
		t.Errorf("after the button the text is %q", got)
	}
	a.mustDo("dev.log", "again")
	a.mustDo("workbench.output.action.clearOutput", nil)
	if got := a.win.Output.Document().Text(); got != "" {
		t.Errorf("after the command the text is %q", got)
	}
	if got := a.win.Process.Channels.Lines(logs.MainChannel); len(got) != 0 {
		t.Errorf("the channel still has %d lines", len(got))
	}
	a.noErrors()
}

func TestOutputIsReadOnlyAndTheSmartScrollFollowsTheCursor(t *testing.T) {
	a := newPanelApp(t, "")
	for i := range 40 {
		a.mustDo("dev.log", fmt.Sprintf("line %d", i))
	}
	a.mustDo("workbench.action.output.toggleOutput", nil)
	if !a.win.Output.Focus().HasFocus() {
		t.Fatal("the editor of the Output has no focus")
	}
	if a.win.Keys.Get("inOutput") != true {
		t.Error("inOutput is not set while the editor has the focus")
	}
	before := a.win.Output.Document().Text()
	for _, ch := range "xyz" {
		a.ts.SendKeyEvent(services.LogicalKeyboardKey(ch), gluttertest.KeyOpts{Character: string(ch)})
	}
	a.ts.SendKeyEvent(services.LogicalKeyboardKeyEnter, gluttertest.KeyOpts{Physical: services.PhysicalKeyboardKeyEnter, Character: "\n"})
	a.ts.SendKeyEvent(services.LogicalKeyboardKeyBackspace, gluttertest.KeyOpts{Physical: services.PhysicalKeyboardKeyBackspace})
	a.pump()
	if a.win.Output.Document().Text() != before {
		t.Error("typing changed the text of the Output")
	}
	if a.win.Output.Document().Dirty() {
		t.Error("the Output is dirty")
	}
	// a click on a line above the last locks the scrolling, a click on the last line unlocks it
	// the rows at the bottom of the editor: the last line, and the one above it
	bottom := a.ts.BottomRight(gluttertest.ByKey(output.EditorKey))
	x := a.ts.Center(gluttertest.ByKey(output.EditorKey)).Dx
	rowHeight := a.win.Output.ViewModel().Options().LineHeight
	a.ts.TapAt(ui.Offset{Dx: x, Dy: bottom.Dy - rowHeight/2}, mouseButton)
	a.pump()
	last := a.win.Output.Document().Snapshot().LineCount() - 1
	if got := a.win.Output.ViewModel().Cursors().Primary().Position().Line; got != last {
		t.Fatalf("the cursor is on line %d, want the last line %d", got, last)
	}
	if a.win.Output.ScrollLock() {
		t.Error("a click on the last line locked the scrolling")
	}
	a.ts.TapAt(ui.Offset{Dx: x, Dy: bottom.Dy - rowHeight*3/2}, mouseButton)
	a.pump()
	if !a.win.Output.ScrollLock() {
		t.Error("a click above the last line did not lock the scrolling")
	}
	a.noErrors()
}

func TestOutputOpensTheLogFile(t *testing.T) {
	a := newPanelApp(t, "")
	a.mustDo("workbench.action.output.toggleOutput", nil)
	// the window of a test has no log file: a notification says so
	a.mustDo("workbench.action.openActiveLogOutputFile", nil)
	if got := a.win.Process.Notify.Items(); len(got) == 0 || !strings.Contains(got[len(got)-1].Message, "has no file") {
		t.Errorf("notifications: %+v", got)
	}

	paths := testutil.Isolate(t)
	testutil.Settings(t, paths, nil)
	l, err := logs.Open(logs.Options{Dir: paths.LogsDir()})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := l.Close(); err != nil {
			t.Error(err)
		}
	})
	l.Logger().Info("hello from the log")
	win, err := NewWindow(context.Background(), NewProcess(ProcessOptions{Paths: paths, Logs: l}), "")
	if err != nil {
		t.Fatal(err)
	}
	b := newGroupAppWith(t, win, ui.Size{Width: 960, Height: 560}, nil, nil)
	pa := &panelApp{b}
	pa.mustDo("workbench.action.output.toggleOutput", nil)
	if !strings.Contains(pa.outputText(), "hello from the log") {
		t.Error("the log line of the process is not in the Emetos channel")
	}
	pa.mustDo("workbench.action.openActiveLogOutputFile", nil)
	if got := filepath.Base(pa.shell().activeEditor().doc.URI().Path); got != "main.log" {
		t.Errorf("the active editor shows %q, want main.log", got)
	}
	pa.noErrors()
}

func TestDevCommandsFillThePanels(t *testing.T) {
	a := newPanelApp(t, "")
	// a relative resource is relative to the folder Emetos runs in
	a.mustDo("dev.setMarkers", map[string]any{"resource": "docs/SPEC.md", "markers": []any{map[string]any{"message": "m"}}})
	got := a.win.Markers.Read(problems.ReadFilter{})
	if len(got) != 1 || !filepath.IsAbs(got[0].Resource) || !strings.HasSuffix(got[0].Resource, filepath.Join("docs", "SPEC.md")) {
		t.Fatalf("markers: %+v", got)
	}
	m := got[0]
	if m.Owner != "dev" || m.Severity != problems.SeverityError || m.StartLine != 1 || m.StartColumn != 1 || m.EndLine != 1 || m.EndColumn != 1 {
		t.Errorf("defaults: %+v", m)
	}
	a.mustDo("dev.setMarkers", map[string]any{"resource": "docs/SPEC.md"})
	if n := len(a.win.Markers.Read(problems.ReadFilter{})); n != 0 {
		t.Errorf("a missing list left %d markers", n)
	}
	a.mustDo("dev.log", map[string]any{"channel": "Build", "message": "compiling"})
	a.mustDo("dev.log", "to the main channel")
	if got := a.win.Process.Channels.Names(); fmt.Sprint(got) != "[Build Emetos]" {
		t.Errorf("channels: %v", got)
	}
	// without a window the command says so
	bare := NewProcess(ProcessOptions{Paths: testutil.Isolate(t)})
	err := bare.Commands.Run(command.Context{Ctx: context.Background(), Source: command.SourceAPI}, "dev.setMarkers", map[string]any{})
	if appErr, ok := errors.FromError(err); !ok || !appErr.HasRoute("DevNoWindow") {
		t.Errorf("dev.setMarkers with no window: %v", err)
	}
	a.noErrors()
}

func TestDevCommandErrors(t *testing.T) {
	a := newPanelApp(t, "")
	cases := []struct {
		name  string
		id    string
		args  any
		route string
	}{
		{"setMarkers without arguments", "dev.setMarkers", nil, "parseSetMarkers.DevSetMarkersArgumentsInvalid"},
		{"setMarkers with a bad severity", "dev.setMarkers", map[string]any{"resource": "x", "markers": []any{map[string]any{"message": "m", "severity": "fatal"}}}, "parseMarker.DevSetMarkersSeverityInvalid"},
		{"log without arguments", "dev.log", nil, "parseLog.DevLogArgumentsInvalid"},
		{"log with no message", "dev.log", map[string]any{"channel": "c"}, "parseLog.DevLogMessageInvalid"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			err := a.do(tc.id, tc.args)
			appErr, ok := errors.FromError(err)
			if !ok {
				t.Fatalf("expected *errors.Error, got %T (%v)", err, err)
			}
			if !appErr.HasRoute(tc.route) {
				t.Errorf("unexpected failure path: %s", appErr.Route())
			}
		})
	}
	a.noErrors()
}

func TestParseSetMarkers(t *testing.T) {
	getwd := func() (string, error) { return "/work", nil }
	cases := []struct {
		name string
		args any
		want markersArguments
	}{
		{"an absolute path", map[string]any{"resource": "/a/b.go"}, markersArguments{Owner: "dev", Resource: "/a/b.go"}},
		{"a relative path", map[string]any{"resource": "a/../b.go"}, markersArguments{Owner: "dev", Resource: "/work/b.go"}},
		{"an owner", map[string]any{"owner": "lint", "resource": "/a"}, markersArguments{Owner: "lint", Resource: "/a"}},
		{"a null list", map[string]any{"resource": "/a", "markers": nil}, markersArguments{Owner: "dev", Resource: "/a"}},
		{"a marker with everything", map[string]any{"resource": "/a", "markers": []any{map[string]any{
			"severity": "warning", "message": "m", "startLine": 3.0, "startColumn": 4.0, "endLine": 5.0, "endColumn": 6.0, "source": "s", "code": "c"}}},
			markersArguments{Owner: "dev", Resource: "/a", Markers: []problems.Marker{{Severity: problems.SeverityWarning, Message: "m", StartLine: 3, StartColumn: 4, EndLine: 5, EndColumn: 6, Source: "s", Code: "c"}}}},
		{"the defaults of a marker", map[string]any{"resource": "/a", "markers": []any{map[string]any{"message": "m", "startLine": 7.0}}},
			markersArguments{Owner: "dev", Resource: "/a", Markers: []problems.Marker{{Severity: problems.SeverityError, Message: "m", StartLine: 7, StartColumn: 1, EndLine: 7, EndColumn: 1}}}},
		{"an end that follows the start", map[string]any{"resource": "/a", "markers": []any{map[string]any{"message": "m", "startLine": 2.0, "startColumn": 9.0}}},
			markersArguments{Owner: "dev", Resource: "/a", Markers: []problems.Marker{{Severity: problems.SeverityError, Message: "m", StartLine: 2, StartColumn: 9, EndLine: 2, EndColumn: 9}}}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, err := parseSetMarkers(tc.args, getwd)
			if err != nil {
				t.Fatal(err)
			}
			if got.Owner != tc.want.Owner || got.Resource != tc.want.Resource || fmt.Sprint(got.Markers) != fmt.Sprint(tc.want.Markers) {
				t.Errorf("got %+v, want %+v", got, tc.want)
			}
		})
	}
}

func TestParseSetMarkersErrors(t *testing.T) {
	getwd := func() (string, error) { return "/work", nil }
	marker := func(m map[string]any) any { return map[string]any{"resource": "/a", "markers": []any{m}} }
	cases := []struct {
		name  string
		args  any
		getwd func() (string, error)
		route string
	}{
		{"no arguments", nil, getwd, "parseSetMarkers.DevSetMarkersArgumentsInvalid"},
		{"a string", "x", getwd, "parseSetMarkers.DevSetMarkersArgumentsInvalid"},
		{"an owner that is not a text", map[string]any{"owner": 1.0, "resource": "/a"}, getwd, "parseSetMarkers.DevSetMarkersOwnerInvalid"},
		{"an empty owner", map[string]any{"owner": "", "resource": "/a"}, getwd, "parseSetMarkers.DevSetMarkersOwnerInvalid"},
		{"no resource", map[string]any{}, getwd, "parseSetMarkers.DevSetMarkersResourceInvalid"},
		{"a resource that is not a text", map[string]any{"resource": 3.0}, getwd, "parseSetMarkers.DevSetMarkersResourceInvalid"},
		{"no folder to resolve a path in", map[string]any{"resource": "rel"}, func() (string, error) { return "", errors.New("Fake", "no folder") }, "parseSetMarkers.DevSetMarkersCwdFailed"},
		{"markers that are not a list", map[string]any{"resource": "/a", "markers": "x"}, getwd, "parseSetMarkers.DevSetMarkersMarkersInvalid"},
		{"a marker that is not an object", map[string]any{"resource": "/a", "markers": []any{"x"}}, getwd, "parseSetMarkers->parseMarker.DevSetMarkersMarkerInvalid"},
		{"an unknown severity", marker(map[string]any{"message": "m", "severity": "fatal"}), getwd, "parseMarker.DevSetMarkersSeverityInvalid"},
		{"a severity that is not a text", marker(map[string]any{"message": "m", "severity": 2.0}), getwd, "parseMarker.DevSetMarkersSeverityInvalid"},
		{"no message", marker(map[string]any{"severity": "error"}), getwd, "parseMarker.DevSetMarkersMessageInvalid"},
		{"an empty message", marker(map[string]any{"message": ""}), getwd, "parseMarker.DevSetMarkersMessageInvalid"},
		{"a line of 0", marker(map[string]any{"message": "m", "startLine": 0.0}), getwd, "parseMarker.DevSetMarkersPositionInvalid"},
		{"a negative column", marker(map[string]any{"message": "m", "startColumn": -1.0}), getwd, "parseMarker.DevSetMarkersPositionInvalid"},
		{"a fraction", marker(map[string]any{"message": "m", "endLine": 1.5}), getwd, "parseMarker.DevSetMarkersPositionInvalid"},
		{"a position that is a text", marker(map[string]any{"message": "m", "endColumn": "3"}), getwd, "parseMarker.DevSetMarkersPositionInvalid"},
		{"a source that is a number", marker(map[string]any{"message": "m", "source": 1.0}), getwd, "parseMarker.DevSetMarkersTextInvalid"},
		{"a code that is a number", marker(map[string]any{"message": "m", "code": 1.0}), getwd, "parseMarker.DevSetMarkersTextInvalid"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, err := parseSetMarkers(tc.args, tc.getwd)
			appErr, ok := errors.FromError(err)
			if !ok {
				t.Fatalf("expected *errors.Error, got %T (%v)", err, err)
			}
			if !appErr.HasRoute(tc.route) {
				t.Errorf("unexpected failure path: %s", appErr.Route())
			}
		})
	}
}

func TestParseLog(t *testing.T) {
	cases := []struct {
		name string
		args any
		want logArguments
	}{
		{"a message", "hello", logArguments{Channel: logs.MainChannel, Message: "hello"}},
		{"a channel and a message", map[string]any{"channel": "Build", "message": "m"}, logArguments{Channel: "Build", Message: "m"}},
		{"no channel is the main one", map[string]any{"message": "m"}, logArguments{Channel: logs.MainChannel, Message: "m"}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, err := parseLog(tc.args)
			if err != nil || got != tc.want {
				t.Errorf("got %+v, %v, want %+v", got, err, tc.want)
			}
		})
	}
}

func TestParseLogErrors(t *testing.T) {
	cases := []struct {
		name  string
		args  any
		route string
	}{
		{"no arguments", nil, "parseLog.DevLogArgumentsInvalid"},
		{"a number", 3.0, "parseLog.DevLogArgumentsInvalid"},
		{"a channel that is not a text", map[string]any{"channel": 1.0, "message": "m"}, "parseLog.DevLogChannelInvalid"},
		{"an empty channel", map[string]any{"channel": "", "message": "m"}, "parseLog.DevLogChannelInvalid"},
		{"no message", map[string]any{"channel": "c"}, "parseLog.DevLogMessageInvalid"},
		{"an empty message", map[string]any{"message": ""}, "parseLog.DevLogMessageInvalid"},
		{"a message that is not a text", map[string]any{"message": 4.0}, "parseLog.DevLogMessageInvalid"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, err := parseLog(tc.args)
			appErr, ok := errors.FromError(err)
			if !ok {
				t.Fatalf("expected *errors.Error, got %T (%v)", err, err)
			}
			if !appErr.HasRoute(tc.route) {
				t.Errorf("unexpected failure path: %s", appErr.Route())
			}
		})
	}
}

func TestByteColumn(t *testing.T) {
	cases := []struct {
		name   string
		line   string
		column int
		want   int
	}{
		{"the first column", "abc", 1, 0},
		{"ascii", "abc", 3, 2},
		{"just past the end", "abc", 4, 3},
		{"far past the end", "abc", 40, 3},
		{"column 0 counts as the first", "abc", 0, 0},
		{"after a two byte character", "héllo", 3, 3},
		{"after a character of two UTF-16 units", "a\U0001F600b", 4, 5},
		{"in the middle of a pair is after the pair", "a\U0001F600b", 3, 5},
		{"an empty line", "", 5, 0},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := byteColumn(tc.line, tc.column); got != tc.want {
				t.Errorf("byteColumn(%q, %d) = %d, want %d", tc.line, tc.column, got, tc.want)
			}
		})
	}
}
