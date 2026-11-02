package app

import (
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"sync/atomic"
	"time"

	"github.com/rm4n0s/glutter/ui"
	"github.com/rm4n0s/glutter/widgets"

	"github.com/rm4n0s/emetos/internal/core/document"
	"github.com/rm4n0s/emetos/internal/core/editorgroup"
	"github.com/rm4n0s/emetos/internal/core/settings"
	coretesting "github.com/rm4n0s/emetos/internal/core/testing"
	"github.com/rm4n0s/emetos/internal/gui/diffeditor"
	"github.com/rm4n0s/emetos/internal/gui/editor"
	"github.com/rm4n0s/emetos/internal/gui/kit"
	"github.com/rm4n0s/emetos/internal/gui/kit/fonts"
	"github.com/rm4n0s/emetos/internal/gui/testpeek"
	testview "github.com/rm4n0s/emetos/internal/gui/views/testing"
)

// The tests in the editors of the window (SPEC §10.32 "The editor", step 26.3). Every text editor of a file follows the test service of the
// workspace: the tests whose range starts on a line become the glyphs of its glyph margin, the messages of the failures written after their
// lines, and the peek (internal/gui/testpeek) shows a failure under its line. The editors of the window of the host and those of the
// headless views of its clients are all made here, from the one service of the host, so a test run from the glyph in the editor of a client
// is the host's and shows on both screens (D46).
//
// Ported from VS Code: src/vs/workbench/contrib/testing/browser/testingDecorations.ts and testingOutputPeek.ts

// editorTesting is the tests of one text editor.
type editorTesting struct {
	s       *shellState
	binding *widgets.WidgetsBinding
	win     *Workspace
	e       *textEditor
	svc     *coretesting.Service

	// path is the file of the document, empty for a document that is not a file on disk, and uri the same as a file:// URI.
	path, uri string
	pending   atomic.Bool
	peek      *testpeek.Controller
	stops     []func()
	disposed  bool
}

// followTesting makes the editor show the tests of the service. It returns the function that stops it.
func (s *shellState) followTesting(binding *widgets.WidgetsBinding, e *textEditor) func() {
	win := PeekWorkspace(s.Context())
	if win == nil || win.TestingService() == nil {
		return func() {}
	}
	d := &editorTesting{s: s, binding: binding, win: win, e: e, svc: win.TestingService()}
	e.tst = d
	d.attach()
	d.stops = append(d.stops,
		d.svc.Subscribe(func(ev coretesting.Event) {
			if ev.Kind != coretesting.EventCoverage && ev.Kind != coretesting.EventContinuous && ev.Kind != coretesting.EventProfiles {
				d.schedule()
			}
		}),
		win.Settings.OnChange(func(ev settings.ChangeEvent) {
			if ev.Affects("testing", "") {
				d.schedule()
			}
		}),
		e.doc.OnURIChange(func(_, _ document.URI) {
			binding.Post(func() {
				if !d.disposed {
					d.attach()
					d.refresh()
				}
			})
		}),
	)
	e.view.SetTestHost(&editor.TestHost{
		Enabled:    func() bool { return settings.Get[bool](win.Settings, "testing.gutterEnabled", "") },
		GlyphPress: d.glyphPress,
	})
	d.refresh()
	return d.dispose
}

// schedule refreshes the editor on the UI thread, once for any number of events.
func (d *editorTesting) schedule() {
	if d.pending.CompareAndSwap(false, true) {
		d.binding.Post(func() {
			d.pending.Store(false)
			if !d.disposed {
				d.refresh()
			}
		})
	}
}

// attach follows the file of the document.
func (d *editorTesting) attach() {
	d.path, d.uri = "", ""
	u := d.e.doc.URI()
	if u.Scheme == document.SchemeFile && u.Path != "" {
		d.path, d.uri = u.Path, explorerURI(u.Path)
	}
}

// dispose stops following the service and takes the marks and the peek away.
func (d *editorTesting) dispose() {
	if d.disposed {
		return
	}
	d.disposed = true
	for _, stop := range d.stops {
		stop()
	}
	d.stops = nil
	if d.peek != nil {
		d.peek.Dispose()
		d.peek = nil
	}
	d.e.view.DisposeTesting()
	d.e.tst = nil
	d.s.syncPeekKey()
}

// refresh puts the marks of the service on the editor: the glyphs, the messages, and the peek follows its message.
func (d *editorTesting) refresh() {
	view := d.e.view
	if d.path == "" {
		view.SetTestGlyphs(nil)
		view.SetTestMessages(nil)
		return
	}
	view.SetTestGlyphs(d.glyphs())
	view.SetTestMessages(d.messages())
	if d.peek != nil {
		d.peek.Refresh()
	}
}

// ---- the glyphs

// testsByLine are the tests of the file by the line their range starts on, in the order of the tree.
func (d *editorTesting) testsByLine() map[int][]coretesting.Item {
	by := map[int][]coretesting.Item{}
	for _, c := range d.svc.Controllers() {
		d.svc.Walk(c.ID, func(it coretesting.Item) bool {
			if it.URI == d.uri && it.Range != nil {
				by[it.Range.Start.Line] = append(by[it.Range.Start.Line], it)
			}
			return true
		})
	}
	return by
}

// gutterHint is the hover of a glyph: what a click does.
func gutterHint(action string) string {
	switch action {
	case "contextMenu":
		return "Click for test options"
	case "debug":
		return "Click to debug tests, right click for more options"
	case "runWithCoverage":
		return "Click to run tests with coverage, right click for more options"
	}
	return "Click to run tests, right click for more options"
}

// glyphs are the glyphs of the file: the run icon of a line whose tests have no result, the icon of their state otherwise.
//
// Ported from VS Code: testingDecorations.ts (createRunTestDecoration)
func (d *editorTesting) glyphs() []editor.TestGlyphMark {
	by := d.testsByLine()
	if len(by) == 0 {
		return nil
	}
	hint := gutterHint(settings.Get[string](d.win.Settings, "testing.defaultGutterClickAction", ""))
	lines := make([]int, 0, len(by))
	for line := range by {
		lines = append(lines, line)
	}
	sort.Ints(lines)
	out := make([]editor.TestGlyphMark, 0, len(lines))
	for _, line := range lines {
		items := by[line]
		state, retired := coretesting.StateUnset, false
		var ids []string
		for _, it := range items {
			ids = append(ids, it.ID)
			if _, tr, ok := d.svc.StateOf(it.ID); ok {
				state = coretesting.MaxPriority(state, tr.State)
				retired = retired || tr.Retired
			}
		}
		many := len(items) > 1 || len(d.svc.Children(items[0].ID)) > 0
		g := editor.TestGlyphMark{Line: line, IDs: ids, Hover: hint, Failed: state.IsFailed()}
		if state == coretesting.StateUnset {
			g.Icon, g.ColorID = fonts.CodiconPlay, "testing.runAction"
			if many {
				g.Icon = fonts.CodiconRunAll
			}
		} else {
			g.Icon, g.ColorID = testview.StateIcon(state), testview.StateColorID(state, retired)
		}
		out = append(out, g)
	}
	return out
}

// glyphPress is a click on a glyph: what testing.defaultGutterClickAction says, with Alt for the alternate action.
//
// Ported from VS Code: testingDecorations.ts (RunTestDecoration.click)
func (d *editorTesting) glyphPress(p editor.TestPress) {
	action := settings.Get[string](d.win.Settings, "testing.defaultGutterClickAction", "")
	group := coretesting.GroupRun
	switch action {
	case "contextMenu":
		d.showMenu(p.IDs, p.At)
		return
	case "debug":
		group = coretesting.GroupDebug
		if p.Mods.Alt {
			group = coretesting.GroupRun
		}
	case "runWithCoverage":
		group = coretesting.GroupCoverage
		if p.Mods.Alt {
			group = coretesting.GroupDebug
		}
	default:
		if p.Mods.Alt {
			group = coretesting.GroupDebug
		}
	}
	d.s.runTestRequest(group, p.IDs, nil, true)
}

// showMenu opens the menu of the tests of a glyph at a place of the window.
func (d *editorTesting) showMenu(ids []string, at ui.Offset) {
	if items := d.glyphMenu(ids); len(items) > 0 {
		d.s.editorMenu.Show(at, items)
	}
}

// glyphMenu is the menu of a glyph: for one test Run, Debug, Run with Coverage, Peek Error, Cancel and Reveal in Test Explorer; for
// several the same for all of them and a submenu for each test.
//
// Ported from VS Code: testingDecorations.ts (SingleRunTestDecoration, MultiRunTestDecoration getContextMenuActions)
func (d *editorTesting) glyphMenu(ids []string) []kit.MenuItem {
	var tests []coretesting.Item
	for _, id := range ids {
		if it, ok := d.svc.Item(id); ok {
			tests = append(tests, it)
		}
	}
	switch len(tests) {
	case 0:
		return nil
	case 1:
		return d.testMenu(tests[0])
	}
	var items []kit.MenuItem
	for _, g := range []struct {
		group coretesting.Group
		label string
	}{{coretesting.GroupRun, "Run All Tests"}, {coretesting.GroupDebug, "Debug All Tests"}, {coretesting.GroupCoverage, "Run All Tests with Coverage"}} {
		can := canRunIn(d.svc, g.group)
		any := false
		for _, t := range tests {
			any = any || can(t.ID)
		}
		if any {
			group := g.group
			items = append(items, d.item(g.label, func() { d.s.runTestRequest(group, ids, nil, true) }))
		}
	}
	items = append(items, kit.SeparatorItem())
	for _, t := range tests {
		items = append(items, kit.MenuItem{Label: t.Label, Submenu: true, Items: d.testMenu(t)})
	}
	return items
}

// item is a menu item that runs a function once the menu has given the focus back to the editor.
func (d *editorTesting) item(label string, run func()) kit.MenuItem {
	return kit.MenuItem{Label: label, OnRun: func() { d.s.afterFrame(d.binding, func() { d.s.afterFrame(d.binding, run) }) }}
}

// testMenu is the menu of one test.
func (d *editorTesting) testMenu(t coretesting.Item) []kit.MenuItem {
	caps := d.svc.Capabilities(t.ID)
	id := t.ID
	var items []kit.MenuItem
	for _, g := range []struct {
		ok    bool
		group coretesting.Group
		label string
	}{{caps.Run, coretesting.GroupRun, "Run Test"}, {caps.Debug, coretesting.GroupDebug, "Debug Test"}, {caps.Coverage, coretesting.GroupCoverage, "Run with Coverage"}} {
		if g.ok {
			group := g.group
			items = append(items, d.item(g.label, func() { d.s.runTestRequest(group, []string{id}, nil, true) }))
		}
	}
	if _, tr, ok := d.svc.StateOf(id); ok {
		if tr.State.IsFailed() {
			items = append(items, d.item("Peek Error", func() { d.s.peekTest(id) }))
		}
		if tr.State == coretesting.StateRunning {
			items = append(items, d.item("Cancel Test Run", d.svc.CancelAll))
		}
	}
	return append(items, d.item("Reveal in Test Explorer", func() { d.s.revealTest(id) }))
}

// ---- the messages

// failureAt is a message of a failed test that has a place in a file.
type failureAt struct {
	res *coretesting.Result
	t   coretesting.TestResult
	// index is the place of the message among those of the test, over its tasks.
	index int
	msg   coretesting.Message
	// line is where the message is (counted from 0) and uri its file.
	line int
	uri  string
}

// failures are the messages of failed tests whose place is in the file: those of the newest result, or of every result with
// testing.showAllMessages. A message with no place is put at the test's own range. A test that was invalidated shows none.
//
// Ported from VS Code: testingDecorations.ts (TestingDecorations.applyMessagesFromResult, isMessageInvalidated)
func (d *editorTesting) failures() []failureAt {
	return failuresIn(d.svc, d.uri, settings.Get[bool](d.win.Settings, "testing.showAllMessages", ""))
}

// failuresIn lists the failures of the results of a service that are in a file (a file:// URI), by line and then by the order of the
// results and tests.
func failuresIn(svc *coretesting.Service, uri string, all bool) []failureAt {
	results := svc.Results()
	if !all && len(results) > 1 {
		results = results[:1]
	}
	var out []failureAt
	for _, res := range results {
		for _, t := range res.Tests() {
			if t.Retired || !t.OwnState.IsFailed() && !t.State.IsFailed() {
				continue
			}
			for i, m := range messagesOf(t) {
				if m.Type != coretesting.MessageError {
					continue
				}
				f := failureAt{res: res, t: t, index: i, msg: m}
				switch {
				case m.Location != nil:
					f.uri, f.line = m.Location.URI, m.Location.Range.Start.Line
				case t.Item.URI != "" && t.Item.Range != nil:
					f.uri, f.line = t.Item.URI, t.Item.Range.Start.Line
				default:
					continue
				}
				if f.uri == uri {
					out = append(out, f)
				}
			}
		}
	}
	sort.SliceStable(out, func(a, b int) bool { return out[a].line < out[b].line })
	return out
}

// messagesOf are the messages of a test over all its tasks.
func messagesOf(t coretesting.TestResult) []coretesting.Message {
	var out []coretesting.Message
	for _, task := range t.Tasks {
		out = append(out, task.Messages...)
	}
	return out
}

// messages are the failure messages written after their lines: one for each line, the first.
func (d *editorTesting) messages() []editor.TestMessageMark {
	var out []editor.TestMessageMark
	seen := map[int]bool{}
	for _, f := range d.failures() {
		if seen[f.line] || f.msg.Location == nil {
			continue
		}
		seen[f.line] = true
		text := strings.TrimSpace(f.msg.Text)
		if text == "" {
			continue
		}
		hover := f.msg.Text
		if !f.msg.Markdown {
			hover = escapeMarkdown(hover)
		}
		out = append(out, editor.TestMessageMark{Line: f.line, Text: firstTextLine(text), Hover: hover})
	}
	return out
}

// firstTextLine is the first line of a text.
func firstTextLine(s string) string {
	if i := strings.IndexAny(s, "\r\n"); i >= 0 {
		return s[:i]
	}
	return s
}

// ---- the peek

// peekKey identifies a message among those of the results: the result, the test and the place among the messages of the test.
func peekKey(f failureAt) string { return f.res.ID() + "|" + f.t.Item.ID + "|" + strconv.Itoa(f.index) }

// subjects are the messages the peek goes through in this file.
func (d *editorTesting) subjects() []testpeek.Subject {
	var out []testpeek.Subject
	for _, f := range d.failures() {
		out = append(out, d.subject(f))
	}
	return out
}

// subject describes a failure for the peek.
func (d *editorTesting) subject(f failureAt) testpeek.Subject {
	s := testpeek.Subject{
		Key: peekKey(f), TestID: f.t.Item.ID, Title: f.t.Item.Label, Line: f.line,
		Place: filepath.Base(d.path) + ":" + strconv.Itoa(f.line+1),
		Text:  f.msg.Text, Markdown: f.msg.Markdown,
		Expected: f.msg.Expected, Actual: f.msg.Actual, HasDiff: f.msg.HasDiff,
		Output: testview.TestOutput(f.res, f.t), Failure: true, Language: d.e.doc.LanguageID(),
	}
	for _, fr := range f.msg.StackTrace {
		sf := testpeek.StackFrame{Label: fr.Label}
		if fr.URI != "" {
			sf.Path = testingPath(fr.URI)
			sf.Display = d.displayPath(sf.Path)
		}
		if fr.Pos != nil {
			sf.Line, sf.Col = fr.Pos.Line+1, fr.Pos.Col+1
		}
		s.Stack = append(s.Stack, sf)
	}
	return s
}

// displayPath is a file as the peek writes it: relative to the folder of the workspace when it is inside, else its name.
func (d *editorTesting) displayPath(path string) string {
	if d.win.Folder != "" {
		if rel, err := filepath.Rel(d.win.Folder, path); err == nil && !strings.HasPrefix(rel, "..") {
			return filepath.ToSlash(rel)
		}
	}
	return filepath.Base(path)
}

// peekController is the peek of the editor, made when it is first needed.
func (d *editorTesting) peekController() *testpeek.Controller {
	if d.peek != nil {
		return d.peek
	}
	d.peek = testpeek.New(testpeek.Config{
		Editor: d.e.view,
		List:   d.subjects,
		NewBody: func(spec testpeek.BodySpec) (testpeek.Body, error) {
			return d.s.newTestPeekBody(d.binding, spec)
		},
		OpenFrame: func(f testpeek.StackFrame) {
			if f.Path != "" {
				d.s.openPathAt(d.binding, f.Path, f.Line, f.Col, openOptions{Preview: true, TakeFocus: true})
			}
		},
		Actions: d.peekActions,
		FocusEditor: func() {
			if !d.disposed {
				d.e.focus.Request()
			}
		},
		OnChange: d.s.syncPeekKey,
		OnError:  func(err error) { d.win.Process.Logger.Warn("the peek of a test failed", "err", err) },
	})
	return d.peek
}

// peekActions are the buttons of the title bar of the peek: run the test again, and go to it.
func (d *editorTesting) peekActions(sub testpeek.Subject) []testpeek.Action {
	id := sub.TestID
	return []testpeek.Action{
		{Icon: fonts.CodiconPlay, Tooltip: "Run Test", Run: func() { d.s.runTestRequest(coretesting.GroupRun, []string{id}, nil, true) }},
		{Icon: fonts.CodiconGoToFile, Tooltip: "Go to Test", Run: func() {
			if it, ok := d.svc.Item(id); ok {
				d.s.testingGoTo(it)
			}
		}},
	}
}

// syncPeekKey sets testing.isPeekVisible: whether the active editor shows the peek.
func (s *shellState) syncPeekKey() {
	win := s.win
	if win == nil || win.TestingService() == nil {
		return
	}
	visible := false
	if e := s.activeEditor(); e != nil && e.tst != nil && e.tst.peek != nil {
		visible = e.tst.peek.Visible()
	}
	win.Keys.Set("testing.isPeekVisible", visible)
}

// editorsShowing are the editors that are the active tab of a group.
func (s *shellState) visibleTextEditors() []*textEditor {
	if s.group.grid == nil {
		return nil
	}
	var out []*textEditor
	for _, g := range s.group.grid.Groups(editorgroup.CreationTime) {
		if e := s.group.editors[g.Model.ActiveEditor()]; e != nil {
			out = append(out, e)
		}
	}
	return out
}

// anyPeekOpen tells whether some editor shows the peek of a test.
func (s *shellState) anyPeekOpen() bool {
	for _, e := range s.group.editors {
		if e.tst != nil && e.tst.peek != nil && e.tst.peek.Visible() {
			return true
		}
	}
	return false
}

// showFailure opens the file of a failure and shows the peek on its message.
func (s *shellState) showFailure(f failureAt) {
	path := testingPath(f.uri)
	key := peekKey(f)
	s.openPathAtThen(s.binding, path, f.line+1, 1, openOptions{Preview: true, TakeFocus: true}, func(e *textEditor) {
		if e.tst != nil {
			e.tst.peekController().Show(key)
		}
	})
}

// firstFailureOf is the first failure with a place among the messages of the test, or of the tests below it.
//
// Ported from VS Code: testingOutputPeek.ts (getFailedCandidateMessage) and testing.contribution.ts (vscode.peekTestError)
func (s *shellState) firstFailureOf(id string) (failureAt, bool) {
	svc := s.win.TestingService()
	res, _, ok := svc.StateOf(id)
	if !ok {
		return failureAt{}, false
	}
	for _, t := range res.Tests() {
		if t.Item.ID != id && !coretesting.IsAncestorID(id, t.Item.ID) {
			continue
		}
		for i, m := range messagesOf(t) {
			if m.Type != coretesting.MessageError {
				continue
			}
			f := failureAt{res: res, t: t, index: i, msg: m}
			switch {
			case m.Location != nil:
				f.uri, f.line = m.Location.URI, m.Location.Range.Start.Line
			case t.Item.URI != "" && t.Item.Range != nil:
				f.uri, f.line = t.Item.URI, t.Item.Range.Start.Line
			default:
				continue
			}
			return f, true
		}
	}
	return failureAt{}, false
}

// peekTest shows the peek on the first failure of a test (Peek Error).
func (s *shellState) peekTest(id string) bool {
	f, ok := s.firstFailureOf(id)
	if !ok {
		return false
	}
	s.showFailure(f)
	return true
}

// openOutputPeek is Peek Output: the peek on the message of the file nearest to the cursor, or else on any message that has a place.
//
// Ported from VS Code: testingOutputPeek.ts (TestingPeekOpener.open, getFileCandidateMessage, getAnyCandidateMessage)
func (s *shellState) openOutputPeek() {
	svc := s.win.TestingService()
	if e := s.activeEditor(); e != nil && e.tst != nil && e.tst.path != "" {
		line := e.view.Cursors().Primary().Position().Line
		best, dist := failureAt{}, -1
		for _, f := range e.tst.failures() {
			if dd := abs(f.line - line); dist < 0 || dd <= dist {
				best, dist = f, dd
			}
		}
		if dist >= 0 {
			s.showFailure(best)
			return
		}
	}
	for _, res := range svc.Results() {
		for _, t := range res.Tests() {
			if f, ok := s.firstFailureOf(t.Item.ID); ok && f.res == res {
				s.showFailure(f)
				return
			}
		}
	}
	s.win.Notify.Info("Emetos", "There are no test messages to peek at.")
}

func abs(n int) int {
	if n < 0 {
		return -n
	}
	return n
}

// stepPeek goes to the next or the previous message in the active editor (Alt+F8).
func (s *shellState) stepPeek(next bool) {
	if e := s.activeEditor(); e != nil && e.tst != nil && e.tst.path != "" {
		e.tst.peekController().Step(next)
	}
}

// closePeek closes the peek of the active editor.
func (s *shellState) closeTestPeek() {
	for _, e := range []*textEditor{s.activeEditor()} {
		if e != nil && e.tst != nil && e.tst.peek != nil {
			e.tst.peek.Close()
		}
	}
}

// revealTest shows the test in the Test Explorer.
func (s *shellState) revealTest(id string) {
	s.showTesting(s.binding, false, false)
	if ex := s.testing.explorer; ex != nil {
		ex.Reveal(id, true)
	}
}

// ---- the failure opens the peek by itself

// openPeekOnFailure opens the peek for a test that just failed, as testing.automaticallyOpenPeekView says.
//
// Ported from VS Code: testingOutputPeek.ts (TestingPeekOpener.openPeekOnFailure)
func (s *shellState) openPeekOnFailure(res *coretesting.Result, testID string) {
	win := s.win
	mode := settings.Get[string](win.Settings, "testing.automaticallyOpenPeekView", "")
	if mode != "failureAnywhere" && mode != "failureInVisibleDocument" {
		return
	}
	if res.Request().Continuous || s.anyPeekOpen() {
		return
	}
	f, ok := s.firstFailureOf(testID)
	if !ok || f.res != res {
		return
	}
	if mode == "failureInVisibleDocument" {
		visible := false
		for _, e := range s.visibleTextEditors() {
			visible = visible || e.tst != nil && e.tst.uri == f.uri
		}
		if !visible {
			return
		}
	}
	s.showFailure(f)
}

// ---- the small diff editor of the peek

// testPeekBody is the small diff editor of the peek: the expected text and the actual, in the inline view, both read-only.
type testPeekBody struct {
	s        *shellState
	sides    [2]*textEditor
	inputs   [2]*editorgroup.TextInput
	model    *diffeditor.Model
	focused  bool
	disposed bool
}

// testPeekCounter numbers the virtual documents of the peeks.
var testPeekCounter atomic.Int64

// newTestPeekBody makes the small diff editor for a message with an expected and an actual text. The two documents are the peek's alone,
// virtual and read-only.
//
// Ported from VS Code: testingOutputPeek.ts (DiffContentProvider)
func (s *shellState) newTestPeekBody(binding *widgets.WidgetsBinding, spec testpeek.BodySpec) (testpeek.Body, error) {
	win := PeekWorkspace(s.Context())
	n := testPeekCounter.Add(1)
	b := &testPeekBody{s: s}
	var docs [2]*document.Document
	for i, name := range []string{"Expected", "Actual"} {
		text := strings.ReplaceAll(spec.Expected, "\r\n", "\n")
		if i == 1 {
			text = strings.ReplaceAll(spec.Actual, "\r\n", "\n")
		}
		u := document.VirtualURI("testing/" + strconv.FormatInt(n, 10) + "/" + strings.ToLower(name))
		docs[i] = document.NewVirtual(u, document.VirtualContent{Text: text, Label: name, Language: spec.Language})
	}
	for i, side := range []diffeditor.Side{diffeditor.Original, diffeditor.Modified} {
		b.inputs[i] = editorgroup.NewTextInput(docs[i], s.inputOptions())
		b.sides[i] = s.newTextEditorWith(binding, b.inputs[i], func(o editor.Options) editor.Options {
			o = diffeditor.AdjustOptions(o, side)
			o.ReadOnly = true
			o.GlyphMargin = false
			return o
		})
		b.sides[i].view.SetEmbedded(true)
	}
	b.model = diffeditor.NewModel(diffeditor.ModelOpts{
		Original: b.sides[0].view, Modified: b.sides[1].view, Settings: win.Settings, Embedded: true,
		Post: binding.Post, Context: win.ctx,
		AfterTimer: func(d time.Duration, fn func()) func() { return binding.Gestures().NewTimer(d, fn).Cancel },
		OnError: func(err error) {
			win.Process.Logger.Warn("the diff of the peek of a test cannot be computed", "err", err)
		},
		OriginalFocus: b.sides[0].focus, ModifiedFocus: b.sides[1].focus,
	})
	b.model.Start()
	opts := func(ed *textEditor, side diffeditor.Side) editor.EditorOpts {
		return editor.EditorOpts{
			Commands: win.Commands, Keys: win.Keys, Scopes: win.Scopes,
			ClipboardMemory: win.Clipboard, FindMemory: win.Find, Zoom: win.Zoom, InputMode: win.InputMode, Focus: ed.focus, Context: win.ctx,
			OnFocus: func(has bool) {
				if side == diffeditor.Modified {
					b.focused = has
				}
			},
		}
	}
	return testpeek.Body{
		Widget:   diffeditor.NewWidget(b.model, diffeditor.WidgetOpts{Original: opts(b.sides[0], diffeditor.Original), Modified: opts(b.sides[1], diffeditor.Modified)}),
		Focus:    func() { b.sides[1].focus.Request() },
		HasFocus: func() bool { return b.focused },
		Ready:    b.model.Computed,
		Dispose:  b.dispose,
	}, nil
}

// dispose lets go of the editors and the documents.
func (b *testPeekBody) dispose() {
	if b.disposed {
		return
	}
	b.disposed = true
	b.model.Dispose()
	for i, ed := range b.sides {
		delete(b.s.group.focused, ed)
		ed.dispose()
		b.inputs[i].Dispose()
	}
}
