package sql

import (
	"strings"
	"testing"
	"time"

	"github.com/rm4n0s/glutter/gluttertest"
	"github.com/rm4n0s/glutter/services"
	gui "github.com/rm4n0s/glutter/ui"

	"github.com/rm4n0s/emetos/extension"
	"github.com/rm4n0s/emetos/extension/ui/uitest"
)

// mustFind fails the test when nothing on screen matches.
func mustFind(t *testing.T, ts *gluttertest.Tester, f gluttertest.Finder) {
	t.Helper()
	if !ts.Exists(f) {
		t.Errorf("nothing on screen matches %v", f)
	}
}

func darkPump(t *testing.T, e *editorRig, height float64) *gluttertest.Tester {
	t.Helper()
	ts := uitest.Pump(t, uitest.Themes()[0], gui.Size{Width: 760, Height: height}, panelWidget(e))
	ts.Pump(time.Millisecond)
	return ts
}

func TestTheTabsOfThePanelSwitchBetweenResultsAndMessages(t *testing.T) {
	e := newEditorRig(t, "SELECT 1 AS a; SELECT 2 AS b", true)
	rec := e.run(RunAllQueries)
	ts := darkPump(t, e, 190)
	ts.Tap(gluttertest.ByTextContaining("Messages"))
	ts.Pump(time.Millisecond)
	if rec.tab != len(rec.sets) {
		t.Fatalf("tab = %d after a tap on Messages", rec.tab)
	}
	// the host builds the content again after Refresh
	ts = darkPump(t, e, 190)
	mustFind(t, ts, gluttertest.ByTextContaining("[1] SELECT 1 AS a"))
	ts.Tap(gluttertest.ByTextContaining("Result 2"))
	ts.Pump(time.Millisecond)
	if rec.tab != 1 {
		t.Errorf("tab = %d after a tap on Result 2", rec.tab)
	}
}

func TestTheErrorLinkMovesTheCursorInTheEditor(t *testing.T) {
	e := newEditorRig(t, "SELECT 1;\nSELECT nope FROM authors", true)
	e.run(RunAllQueries)
	before := len(e.api.ShownDocuments())
	ts := darkPump(t, e, 190)
	ts.Tap(gluttertest.ByTextContaining("Line 2, column 8"))
	ts.Pump(time.Millisecond)
	shown := e.api.ShownDocuments()
	if len(shown) != before+1 || shown[len(shown)-1].Line != 2 || shown[len(shown)-1].Column != 8 || shown[len(shown)-1].URI != e.uri {
		t.Errorf("shown = %+v", shown)
	}
}

func TestTheExportButtonsAskWhereToSave(t *testing.T) {
	e := newEditorRig(t, "SELECT id FROM authors", true)
	e.run(RunAllQueries)
	ts := darkPump(t, e, 190)
	ts.Tap(gluttertest.ByText("JSON"))
	ts.Pump(time.Millisecond)
	e.api.WaitFor("the dialog", func() bool { return len(e.api.FileChooserCalls()) == 1 })
	if c := e.api.FileChooserCalls()[0]; !c.Options.Save || c.Options.Name != "result.json" {
		t.Errorf("chooser = %+v", c)
	}
}

func TestTheFetchMoreButtonReadsMoreRows(t *testing.T) {
	e := newEditorRig(t, "WITH RECURSIVE n(i) AS (SELECT 1 UNION ALL SELECT i + 1 FROM n WHERE i < 12) SELECT i FROM n", true)
	e.api.SetSetting(SettingMaxRows, 5)
	rec := e.run(RunAllQueries)
	ts := darkPump(t, e, 190)
	ts.Tap(gluttertest.ByText("Fetch More"))
	ts.Pump(time.Millisecond)
	ed := e.ext.q.eds[e.uri]
	e.api.WaitFor("the fetch", func() bool { return !ed.fetching })
	if got := rec.sets[0].set.Table.Len(); got != 10 {
		t.Errorf("rows = %d", got)
	}
	if rec.sets[0].grid.Model().Len() != 10 {
		t.Errorf("the grid has %d rows", rec.sets[0].grid.Model().Len())
	}
}

func TestCancelAndEscapeInThePanelStopTheRun(t *testing.T) {
	long := "WITH RECURSIVE n(i) AS (SELECT 1 UNION ALL SELECT i + 1 FROM n) SELECT count(*) FROM n"
	for _, how := range []string{"the button", "Escape"} {
		t.Run(how, func(t *testing.T) {
			e := newEditorRig(t, long, true)
			if err := e.api.Run(RunAllQueries, nil); err != nil {
				t.Fatal(err)
			}
			ed := e.ext.q.eds[e.uri]
			e.api.WaitFor("the statement to run", func() bool { return ed.runner != nil && ed.runner.Running() })
			time.Sleep(100 * time.Millisecond)
			ts := darkPump(t, e, 120)
			mustFind(t, ts, gluttertest.ByText("Cancel"))
			if how == "the button" {
				ts.Tap(gluttertest.ByText("Cancel"))
			} else {
				// a press in the panel gives it the keyboard
				ts.Tap(gluttertest.ByText("Messages"))
				ts.SendKeyDownEvent(services.LogicalKeyboardKeyEscape, gluttertest.KeyOpts{})
				ts.SendKeyUpEvent(services.LogicalKeyboardKeyEscape)
			}
			ts.Pump(time.Millisecond)
			rec := e.wait()
			if !rec.cancelled {
				t.Errorf("the run was not cancelled: %+v", rec.summary())
			}
		})
	}
}

func TestThePanelListsTheEarlierRunsAndTheEmptyState(t *testing.T) {
	e := newEditorRig(t, "SELECT 1", true)
	ts := darkPump(t, e, 120)
	mustFind(t, ts, gluttertest.ByTextContaining("Run a query with Ctrl+Enter"))
	e.run(RunAllQueries)
	e.run(RunAllQueries)
	ts = darkPump(t, e, 120)
	mustFind(t, ts, gluttertest.ByTextContaining("Run 2"))
	if got := e.ext.q.shown.shown.summary(); !strings.Contains(got, "1 statement") || !strings.Contains(got, "1 row") {
		t.Errorf("summary = %q", got)
	}
}

func TestThePanelFollowsTheActiveEditor(t *testing.T) {
	e := newEditorRig(t, "SELECT 1", true)
	e.run(RunAllQueries)
	other, err := e.api.Documents().OpenUntitled("sql", "SELECT 2")
	if err != nil {
		t.Fatal(err)
	}
	e.ext.q.bindEditor(other, binding{Connection: e.conn.ID})
	e.uri = other
	e.activate(nil)
	e.cursorAt("SELECT")
	e.run(RunQuery)
	if e.ext.q.shown != e.ext.q.eds[other] {
		t.Fatal("the panel does not show the second editor")
	}
	// back to the first editor: its results come back
	first := "untitled:Untitled-1"
	e.api.Emit(extension.TopicEditorActiveChanged, extension.ActiveEditor{URI: first, LanguageID: "sql"})
	if e.ext.q.shown != e.ext.q.eds[first] {
		t.Errorf("the panel shows %v", e.ext.q.shown)
	}
}
