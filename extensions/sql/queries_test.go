package sql

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/rm4n0s/emetos/extension"
	"github.com/rm4n0s/emetos/extensions/internal/dbconn"
	"github.com/rm4n0s/emetos/extensions/internal/resultgrid"
)

// editorRig is a rig with a SQLite connection that is open and an SQL editor that is in front.
type editorRig struct {
	*rig
	conn dbconn.Connection
	uri  string
}

// newEditorRig opens the fixture, makes an untitled SQL editor with the script in front, and binds it when bound is set.
func newEditorRig(t *testing.T, script string, bound bool) *editorRig {
	t.Helper()
	r := newRig(t)
	c := r.save(sqliteConn(makeFixture(t)))
	r.connect(c)
	uri, err := r.api.Documents().OpenUntitled("sql", script)
	if err != nil {
		t.Fatal(err)
	}
	e := &editorRig{rig: r, conn: c, uri: uri}
	if bound {
		e.ext.q.bindEditor(uri, binding{Connection: c.ID})
	}
	e.activate(nil)
	return e
}

// activate makes the editor the active one with the selections.
func (e *editorRig) activate(sel []extension.Selection) {
	e.t.Helper()
	e.api.SetActive(&extension.ActiveDocument{URI: e.uri, LanguageID: "sql", Selections: sel})
	e.api.Emit(extension.TopicEditorActiveChanged, extension.ActiveEditor{URI: e.uri, LanguageID: "sql"})
}

// cursorAt puts the cursor in the text at the first occurrence of the marker.
func (e *editorRig) cursorAt(marker string) {
	e.t.Helper()
	text, _ := e.api.DocumentText(e.uri)
	i := strings.Index(text, marker)
	if i < 0 {
		e.t.Fatalf("no %q in %q", marker, text)
	}
	line := strings.Count(text[:i], "\n")
	col := i - (strings.LastIndexByte(text[:i], '\n') + 1)
	p := extension.Position{Line: line, Col: col}
	e.activate([]extension.Selection{{Anchor: p, Active: p}})
}

// selectText selects the first occurrence of the text.
func (e *editorRig) selectText(sel string) {
	e.t.Helper()
	text, _ := e.api.DocumentText(e.uri)
	i := strings.Index(text, sel)
	if i < 0 {
		e.t.Fatalf("no %q in %q", sel, text)
	}
	pos := func(off int) extension.Position {
		return extension.Position{Line: strings.Count(text[:off], "\n"), Col: off - (strings.LastIndexByte(text[:off], '\n') + 1)}
	}
	e.activate([]extension.Selection{{Anchor: pos(i), Active: pos(i + len(sel))}})
}

// run runs a command of the extension and waits until the editor's run is over.
func (e *editorRig) run(command string) *runRecord {
	e.t.Helper()
	if err := e.api.Run(command, nil); err != nil {
		e.t.Fatal(err)
	}
	return e.wait()
}

// wait waits for the run of the editor to end and returns it.
func (e *editorRig) wait() *runRecord {
	e.t.Helper()
	ed := e.ext.q.eds[e.uri]
	if ed == nil {
		e.t.Fatal("the run made no editor")
	}
	e.api.WaitFor("the run to end", func() bool { return ed.running == nil && ed.shown != nil && !ed.shown.running })
	return ed.shown
}

// cellsOf reads the text of every cell of a set.
func cellsOf(set *ResultSet) [][]string {
	out := make([][]string, set.Table.Len())
	dst := make([]resultgrid.Value, len(set.Columns))
	for i := range out {
		set.Table.Row(i, dst)
		out[i] = make([]string, len(dst))
		for c, v := range dst {
			out[i][c] = v.Full("NULL")
		}
	}
	return out
}

func TestRunWithNoConnectionAsksWhichOneAndThenRuns(t *testing.T) {
	e := newEditorRig(t, "SELECT name FROM authors ORDER BY id", false)
	if err := e.api.Run(RunQuery, nil); err != nil {
		t.Fatal(err)
	}
	pick := e.api.LastPick()
	if pick == nil || len(pick.Labels()) != 1 || pick.Labels()[0] != "$(database) fixture" {
		t.Fatalf("pick = %+v", pick)
	}
	pick.Choose(0) // SQLite has no database to ask for
	rec := e.wait()
	if len(rec.sets) != 1 || len(rec.steps) != 1 {
		t.Fatalf("run = %+v", rec)
	}
	if got := cellsOf(rec.sets[0].set); len(got) != 2 || got[0][0] != "Ursula" {
		t.Errorf("rows = %v", got)
	}
	if b := e.ext.q.bindingOf(e.uri); b.Connection != e.conn.ID {
		t.Errorf("binding = %+v", b)
	}
	// the status bar says what the editor is bound to
	item := e.api.StatusItem("sql.connection")
	if item == nil || item.Hidden || item.Text != "$(database) fixture" || item.Command != ChooseConnection {
		t.Errorf("status item = %+v", item)
	}
	// the panel was shown, without taking the keyboard
	if view := e.api.View(ViewID); view == nil || len(view.Reveals) == 0 || view.Reveals[0] {
		t.Errorf("view = %+v", view)
	}
}

func TestRunRunsTheSelectionTheStatementUnderTheCursorOrAll(t *testing.T) {
	script := "SELECT 1 AS one;\nSELECT 2 AS two;\n\nSELECT 3 AS three, 4 AS four"
	tests := []struct {
		name   string
		prep   func(e *editorRig)
		cmd    string
		want   [][]string
		titles []string
	}{
		{"the statement under the cursor", func(e *editorRig) { e.cursorAt("2 AS") }, RunQuery, [][]string{{"2"}}, []string{"two"}},
		{"the last statement, which has no semicolon", func(e *editorRig) { e.cursorAt("4 AS") }, RunQuery, [][]string{{"3", "4"}}, []string{"three", "four"}},
		{"the first statement from the end of its line", func(e *editorRig) { e.cursorAt("\nSELECT 2") }, RunQuery, [][]string{{"1"}}, []string{"one"}},
		{"a selection of two statements", func(e *editorRig) { e.selectText("SELECT 2 AS two;\n\nSELECT 3") }, RunQuery, [][]string{{"2"}, {"3"}}, nil},
		{"all", func(e *editorRig) { e.cursorAt("1 AS") }, RunAllQueries, [][]string{{"1"}, {"2"}, {"3", "4"}}, nil},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			e := newEditorRig(t, script, true)
			tc.prep(e)
			rec := e.run(tc.cmd)
			var got [][]string
			for _, s := range rec.sets {
				got = append(got, cellsOf(s.set)[0])
			}
			if !slices.EqualFunc(got, tc.want, slices.Equal[[]string]) {
				t.Errorf("rows = %v, want %v", got, tc.want)
			}
			if tc.titles != nil {
				var names []string
				for _, c := range rec.sets[0].set.Columns {
					names = append(names, c.Name)
				}
				if !slices.Equal(names, tc.titles) {
					t.Errorf("columns = %v, want %v", names, tc.titles)
				}
			}
		})
	}
}

func TestResultSetsAreTabsAndTheMessagesTellRowsAffected(t *testing.T) {
	e := newEditorRig(t, "UPDATE authors SET born = born + 1; SELECT id FROM authors; SELECT name FROM authors; DELETE FROM log", true)
	e.api.SetSetting(SettingConfirmDestructive, false)
	rec := e.run(RunAllQueries)
	if len(rec.sets) != 2 || len(rec.steps) != 4 {
		t.Fatalf("sets %d, steps %d", len(rec.sets), len(rec.steps))
	}
	if rec.tab != 0 {
		t.Errorf("the tab shown is %d, want the first result", rec.tab)
	}
	if got := rec.sets[0].title(2); got != "Result 1 (2 rows)" {
		t.Errorf("title = %q", got)
	}
	if !rec.steps[0].res.HasAffected || rec.steps[0].res.Affected != 2 || rec.steps[3].res.Affected != 1 {
		t.Errorf("affected: %+v %+v", rec.steps[0].res, rec.steps[3].res)
	}
	if got := rowsAffected(1); got != "1 row affected" {
		t.Errorf("text = %q", got)
	}
	// a run with no result set shows the messages
	e.cursorAt("UPDATE")
	rec = e.run(RunQuery)
	if len(rec.sets) != 0 || rec.tab != 0 {
		t.Errorf("sets %d, tab %d", len(rec.sets), rec.tab)
	}
}

func TestAnErrorIsMarkedInAFileAndInAnUntitledEditor(t *testing.T) {
	t.Run("a file gets a marker", func(t *testing.T) {
		e := newEditorRig(t, "", true)
		path := filepath.Join(e.folder, "q.sql")
		uri := "file://" + path
		e.api.SetDocument(uri, "SELECT 1;\nSELECT id FORM authors;\nSELECT 3")
		e.uri = uri
		e.ext.q.bindEditor(uri, binding{Connection: e.conn.ID})
		e.activate(nil)
		rec := e.run(RunAllQueries)
		if !rec.failed() || len(rec.steps) != 2 || rec.skipped != 1 {
			t.Fatalf("steps %d, skipped %d", len(rec.steps), rec.skipped)
		}
		if rec.tab != len(rec.sets) {
			t.Errorf("the Messages are not the tab shown: %d", rec.tab)
		}
		marks := e.api.Markers(ProblemsOwner)[uri]
		if len(marks) != 1 || marks[0].StartLine != 2 || marks[0].StartColumn != len("SELECT id FORM ")+1 || marks[0].EndColumn != marks[0].StartColumn+len("authors") {
			t.Fatalf("markers = %+v", marks)
		}
		if !strings.Contains(marks[0].Message, `near "authors": syntax error`) {
			t.Errorf("message = %q", marks[0].Message)
		}
		// the next run clears them
		e.api.SetDocument(uri, "SELECT 1")
		e.cursorAt("SELECT")
		e.run(RunQuery)
		if n := len(e.api.Markers(ProblemsOwner)); n != 0 {
			t.Errorf("markers after a good run: %v", e.api.Markers(ProblemsOwner))
		}
	})
	t.Run("an untitled editor has the cursor moved to the error", func(t *testing.T) {
		e := newEditorRig(t, "SELECT 1;\n  SELECT nope FROM authors", true)
		rec := e.run(RunAllQueries)
		if !rec.failed() {
			t.Fatal("no failure")
		}
		shown := e.api.ShownDocuments()
		if len(shown) != 1 || shown[0].URI != e.uri || shown[0].Line != 2 || shown[0].Column != len("  SELECT ")+1 {
			t.Errorf("shown = %+v", shown)
		}
		if len(e.api.Markers(ProblemsOwner)) != 0 {
			t.Error("an untitled document got a marker")
		}
	})
}

func TestARunThatIsRefusedRunsNothing(t *testing.T) {
	t.Run("a read-only connection refuses a write", func(t *testing.T) {
		e := newEditorRig(t, "SELECT 1;\nDELETE FROM log WHERE n = 1", true)
		c := e.conn
		c.ReadOnly = true
		if _, _, err := e.db.Save(context.Background(), c, dbconn.PasswordChange{}); err != nil {
			t.Fatal(err)
		}
		if err := e.api.Run(RunAllQueries, nil); err != nil {
			t.Fatal(err)
		}
		notes := e.api.Notes()
		if len(notes) == 0 || !strings.Contains(notes[len(notes)-1].Message, "read-only: statement 2 (DELETE)") {
			t.Fatalf("notes = %+v", notes)
		}
		if ed := e.ext.q.eds[e.uri]; len(ed.runs) != 0 {
			t.Error("a run started")
		}
		// a read goes through
		e.cursorAt("SELECT 1")
		if rec := e.run(RunQuery); rec.failed() {
			t.Errorf("a read failed: %+v", rec.steps[0].res.Err)
		}
	})
	t.Run("not connected", func(t *testing.T) {
		e := newEditorRig(t, "SELECT 1", true)
		if err := e.hub.Service().Close(e.conn.ID); err != nil {
			t.Fatal(err)
		}
		if err := e.api.Run(RunQuery, nil); err != nil {
			t.Fatal(err)
		}
		if notes := e.api.Notes(); len(notes) == 0 || !strings.Contains(notes[len(notes)-1].Message, "is not connected") {
			t.Errorf("notes = %+v", notes)
		}
	})
	t.Run("the connection was deleted", func(t *testing.T) {
		e := newEditorRig(t, "SELECT 1", true)
		e.ext.q.setBinding(e.uri, binding{Connection: "gone"})
		if err := e.api.Run(RunQuery, nil); err != nil {
			t.Fatal(err)
		}
		if notes := e.api.Notes(); len(notes) == 0 || !strings.Contains(notes[len(notes)-1].Message, "was deleted") {
			t.Errorf("notes = %+v", notes)
		}
	})
	t.Run("not an SQL editor", func(t *testing.T) {
		e := newEditorRig(t, "SELECT 1", true)
		e.api.SetActive(&extension.ActiveDocument{URI: "file:///x.txt", LanguageID: "plaintext"})
		if err := e.api.Run(RunQuery, nil); err != nil {
			t.Fatal(err)
		}
		if notes := e.api.Notes(); len(notes) == 0 || !strings.Contains(notes[len(notes)-1].Message, "Open a SQL editor") {
			t.Errorf("notes = %+v", notes)
		}
	})
	t.Run("nothing to run", func(t *testing.T) {
		e := newEditorRig(t, "-- only a comment\n", true)
		if err := e.api.Run(RunQuery, nil); err != nil {
			t.Fatal(err)
		}
		if notes := e.api.Notes(); len(notes) == 0 || !strings.Contains(notes[len(notes)-1].Message, "nothing to run") {
			t.Errorf("notes = %+v", notes)
		}
	})
}

func TestDestructiveStatementsAskFirst(t *testing.T) {
	script := "DELETE FROM log;\nSELECT count(*) FROM log"
	t.Run("the answer is Run", func(t *testing.T) {
		e := newEditorRig(t, script, true)
		if err := e.api.Run(RunAllQueries, nil); err != nil {
			t.Fatal(err)
		}
		pick := e.api.LastPick()
		if pick == nil || !strings.Contains(pick.Options().Placeholder, "DELETE without WHERE can lose data") || !strings.Contains(pick.Options().Placeholder, "2 statements") {
			t.Fatalf("pick = %+v", pick)
		}
		pick.Choose(0)
		rec := e.wait()
		if cellsOf(rec.sets[0].set)[0][0] != "0" {
			t.Errorf("the delete did not run")
		}
	})
	t.Run("the answer is Cancel", func(t *testing.T) {
		e := newEditorRig(t, script, true)
		if err := e.api.Run(RunAllQueries, nil); err != nil {
			t.Fatal(err)
		}
		e.api.LastPick().Choose(1)
		time.Sleep(50 * time.Millisecond)
		e.api.Pump()
		if ed := e.ext.q.eds[e.uri]; len(ed.runs) != 0 {
			t.Error("a run started after Cancel")
		}
		rows, _, err := e.hub.Service().Rows(context.Background(), e.conn.ID, "", dialectQuery("SELECT count(*) FROM log"), 0)
		if err != nil || rows[0][0].String != "1" {
			t.Errorf("rows = %v, %v", rows, err)
		}
	})
	t.Run("the setting turns the question off", func(t *testing.T) {
		e := newEditorRig(t, script, true)
		e.api.SetSetting(SettingConfirmDestructive, false)
		rec := e.run(RunAllQueries)
		if len(e.api.Picks()) != 0 || cellsOf(rec.sets[0].set)[0][0] != "0" {
			t.Errorf("picks %d", len(e.api.Picks()))
		}
	})
	t.Run("a statement with a WHERE does not ask", func(t *testing.T) {
		e := newEditorRig(t, "DELETE FROM log WHERE n = 1", true)
		e.run(RunAllQueries)
		if len(e.api.Picks()) != 0 {
			t.Error("asked about a DELETE with a WHERE")
		}
	})
}

func TestTransactionsAreShownAndControlled(t *testing.T) {
	e := newEditorRig(t, "INSERT INTO log (line) VALUES ('in the transaction')", true)
	count := func() string {
		rows, _, err := e.hub.Service().Rows(context.Background(), e.conn.ID, "", dialectQuery("SELECT count(*) FROM log"), 0)
		if err != nil {
			t.Fatal(err)
		}
		return rows[0][0].String
	}
	// do runs a command and waits until the session is in the state the command leaves it in
	do := func(cmd string, wantTx bool) {
		t.Helper()
		if err := e.api.Run(cmd, nil); err != nil {
			t.Fatal(err)
		}
		e.api.WaitFor(cmd, func() bool {
			r := e.ext.q.eds[e.uri].runner
			return r != nil && !r.Running() && r.InTransaction() == wantTx
		})
		e.api.Pump()
	}
	do(BeginTransaction, true)
	if it := e.api.StatusItem("sql.transaction"); it == nil || it.Hidden || !strings.Contains(it.Text, "Transaction open") {
		t.Fatalf("transaction item = %+v", it)
	}
	if e.api.ContextKey(InTransactionKey) != true {
		t.Error("the context key is not set")
	}
	e.run(RunAllQueries)
	do(RollbackTransaction, false)
	if it := e.api.StatusItem("sql.transaction"); it == nil || !it.Hidden {
		t.Errorf("transaction item after the rollback = %+v", it)
	}
	if count() != "1" {
		t.Errorf("rows after a rollback: %s", count())
	}
	do(BeginTransaction, true)
	e.run(RunAllQueries)
	do(CommitTransaction, false)
	if count() != "2" || e.api.ContextKey(InTransactionKey) != false {
		t.Errorf("rows after a commit: %s", count())
	}
	// a commit with nothing open says so
	if err := e.api.Run(CommitTransaction, nil); err != nil {
		t.Fatal(err)
	}
	if notes := e.api.Notes(); !strings.Contains(notes[len(notes)-1].Message, "no open transaction") {
		t.Errorf("notes = %+v", notes)
	}
}

func TestDisconnectingAsksAboutAnOpenTransaction(t *testing.T) {
	e := newEditorRig(t, "INSERT INTO log (line) VALUES ('x')", true)
	if err := e.api.Run(BeginTransaction, nil); err != nil {
		t.Fatal(err)
	}
	e.api.WaitFor("the transaction", func() bool { return e.ext.q.eds[e.uri].runner != nil && e.ext.q.eds[e.uri].runner.InTransaction() })
	e.run(RunAllQueries)
	if err := e.dbAPI.Run(dbconn.Disconnect, e.conn.ID); err != nil {
		t.Fatal(err)
	}
	pick := e.dbAPI.LastPick()
	if pick == nil || !strings.Contains(pick.Options().Placeholder, "transaction open") || !e.hub.Service().IsOpen(e.conn.ID) {
		t.Fatalf("pick = %+v, open %v", pick, e.hub.Service().IsOpen(e.conn.ID))
	}
	pick.Choose(1) // Cancel
	if !e.hub.Service().IsOpen(e.conn.ID) {
		t.Fatal("Cancel disconnected")
	}
	if err := e.dbAPI.Run(dbconn.Disconnect, e.conn.ID); err != nil {
		t.Fatal(err)
	}
	e.dbAPI.LastPick().Choose(0)
	waitUntil(t, "the connection to close", func() bool { return !e.hub.Service().IsOpen(e.conn.ID) })
	e.api.WaitFor("the session to go", func() bool { return e.ext.q.eds[e.uri].runner == nil })
	if it := e.api.StatusItem("sql.connection"); it == nil || !strings.HasSuffix(it.Text, "(not connected)") {
		t.Errorf("connection item = %+v", it)
	}
	// a connection with nothing open is closed at once
	if err := e.dbAPI.Run(dbconn.Connect, e.conn.ID); err != nil {
		t.Fatal(err)
	}
	e.dbAPI.WaitFor("the connection", func() bool { return e.db.State(e.conn.ID).Status == dbconn.Connected })
	picks := len(e.dbAPI.Picks())
	if err := e.dbAPI.Run(dbconn.Disconnect, e.conn.ID); err != nil {
		t.Fatal(err)
	}
	if len(e.dbAPI.Picks()) != picks || e.hub.Service().IsOpen(e.conn.ID) {
		t.Error("an idle connection asked, or stayed open")
	}
}

func TestClosingAnEditorRollsBackItsTransaction(t *testing.T) {
	e := newEditorRig(t, "INSERT INTO log (line) VALUES ('lost')", true)
	if err := e.api.Run(BeginTransaction, nil); err != nil {
		t.Fatal(err)
	}
	e.api.WaitFor("the transaction", func() bool { return e.ext.q.eds[e.uri].runner != nil && e.ext.q.eds[e.uri].runner.InTransaction() })
	e.run(RunAllQueries)
	e.api.Emit(extension.TopicDocumentClosed, extension.DocumentClosed{URI: e.uri})
	e.api.WaitFor("the rollback note", func() bool {
		for _, n := range e.api.Notes() {
			if strings.Contains(n.Message, "rolled back") {
				return true
			}
		}
		return false
	})
	rows, _, err := e.hub.Service().Rows(context.Background(), e.conn.ID, "", dialectQuery("SELECT count(*) FROM log"), 0)
	if err != nil || rows[0][0].String != "1" {
		t.Errorf("rows = %v, %v", rows, err)
	}
	if _, ok := e.ext.q.eds[e.uri]; ok {
		t.Error("the editor is still known")
	}
	if it := e.api.StatusItem("sql.connection"); it == nil || !it.Hidden {
		// the active editor was the one closed; the next active event hides the item
		e.api.Emit(extension.TopicEditorActiveChanged, extension.ActiveEditor{})
		if it := e.api.StatusItem("sql.connection"); !it.Hidden {
			t.Errorf("item = %+v", it)
		}
	}
}

func TestTheEventOfAFinishedStatementCarriesNoSQLAndNoValues(t *testing.T) {
	e := newEditorRig(t, "SELECT name FROM authors; SELECT nope", true)
	e.run(RunAllQueries)
	var got []map[string]any
	for _, p := range e.api.PublishedEvents() {
		if p.Topic != TopicQueryFinished {
			continue
		}
		var v map[string]any
		if err := json.Unmarshal(p.Data, &v); err != nil {
			t.Fatal(err)
		}
		got = append(got, v)
		for k := range v {
			if k != "connection" && k != "ok" && k != "rows" && k != "millis" {
				t.Errorf("the event has %q", k)
			}
		}
		if strings.Contains(string(p.Data), "Ursula") || strings.Contains(string(p.Data), "SELECT") {
			t.Errorf("the event holds a value or SQL: %s", p.Data)
		}
	}
	if len(got) != 2 || got[0]["ok"] != true || got[0]["rows"] != float64(2) || got[0]["connection"] != e.conn.ID || got[1]["ok"] != false {
		t.Errorf("events = %v", got)
	}
}

func TestTheLastTenRunsAreKept(t *testing.T) {
	e := newEditorRig(t, "SELECT 1", true)
	var first *runRecord
	for i := 0; i < 13; i++ {
		rec := e.run(RunAllQueries)
		if i == 0 {
			first = rec
		}
	}
	ed := e.ext.q.eds[e.uri]
	if len(ed.runs) != historyRuns || ed.runs[0].n != 4 || ed.runs[len(ed.runs)-1].n != 13 {
		t.Fatalf("runs kept: %d, from %d to %d", len(ed.runs), ed.runs[0].n, ed.runs[len(ed.runs)-1].n)
	}
	if !first.discarded {
		t.Error("an old run kept its rows")
	}
}

func TestOldRunsLoseTheirRowsWhenTheEditorHoldsTooMuch(t *testing.T) {
	e := newEditorRig(t, "SELECT 1", true)
	q := e.ext.q
	ed := q.editor(e.uri)
	big := func() *runRecord {
		set := &ResultSet{Columns: []resultgrid.Column{{Name: "a"}}, Table: resultgrid.NewTable([]resultgrid.Column{{Name: "a"}}), bytes: editorBytes/2 + 1}
		return &runRecord{sets: []*setRecord{{set: set}}}
	}
	a, b, c := big(), big(), big()
	ed.runs = []*runRecord{a, b, c}
	q.trim(ed)
	if !a.discarded || !b.discarded || c.discarded {
		t.Errorf("discarded: %v %v %v, want the older two", a.discarded, b.discarded, c.discarded)
	}
}

func TestFetchMoreAppendsToTheGrid(t *testing.T) {
	e := newEditorRig(t, "WITH RECURSIVE n(i) AS (SELECT 1 UNION ALL SELECT i + 1 FROM n WHERE i < 25) SELECT i FROM n", true)
	e.api.SetSetting(SettingMaxRows, 10)
	rec := e.run(RunAllQueries)
	sr := rec.sets[0]
	if !sr.set.More() || sr.set.Table.Len() != 10 {
		t.Fatalf("rows %d, more %v", sr.set.Table.Len(), sr.set.More())
	}
	ed := e.ext.q.eds[e.uri]
	grid := e.ext.q.gridOf(ed, rec, sr)
	if grid.Model().Len() != 10 {
		t.Errorf("the grid has %d rows", grid.Model().Len())
	}
	e.ext.q.fetchMore(ed, rec, sr)
	e.api.WaitFor("the fetch", func() bool { return !ed.fetching })
	if sr.set.Table.Len() != 20 || grid.Model().Len() != 20 {
		t.Errorf("after one fetch: %d rows, grid %d", sr.set.Table.Len(), grid.Model().Len())
	}
	e.ext.q.fetchMore(ed, rec, sr)
	e.api.WaitFor("the second fetch", func() bool { return !ed.fetching })
	if sr.set.Table.Len() != 25 || sr.set.More() {
		t.Errorf("after two fetches: %d rows, more %v", sr.set.Table.Len(), sr.set.More())
	}
	// running again closes what was open
	e.run(RunAllQueries)
}

func TestCancelStopsTheStatementThatRuns(t *testing.T) {
	e := newEditorRig(t, "WITH RECURSIVE n(i) AS (SELECT 1 UNION ALL SELECT i + 1 FROM n) SELECT count(*) FROM n", true)
	if err := e.api.Run(RunAllQueries, nil); err != nil {
		t.Fatal(err)
	}
	ed := e.ext.q.eds[e.uri]
	e.api.WaitFor("the statement to run", func() bool { return ed.runner != nil && ed.runner.Running() && e.api.ContextKey(RunningKey) == true })
	time.Sleep(100 * time.Millisecond)
	if err := e.api.Run(CancelQuery, nil); err != nil {
		t.Fatal(err)
	}
	rec := e.wait()
	if !rec.cancelled || len(rec.steps) != 1 || !rec.steps[0].res.Cancelled {
		t.Fatalf("run = %+v", rec)
	}
	if e.api.ContextKey(RunningKey) != false {
		t.Error("the running key stays set")
	}
	// nothing running: a note
	if err := e.api.Run(CancelQuery, nil); err != nil {
		t.Fatal(err)
	}
	if notes := e.api.Notes(); !strings.Contains(notes[len(notes)-1].Message, "No statement is running") {
		t.Errorf("notes = %+v", notes)
	}
	// Escape in the results cancels too
	if err := e.api.Run(RunAllQueries, nil); err != nil {
		t.Fatal(err)
	}
	e.api.WaitFor("the second run", func() bool { return ed.runner.Running() })
	time.Sleep(100 * time.Millisecond)
	if err := e.api.Run(CancelQuery, nil); err != nil {
		t.Fatal(err)
	}
	if rec := e.wait(); !rec.cancelled {
		t.Errorf("the second run was not cancelled")
	}
}

func TestBindingsOfFilesAreKeptInTheWorkspaceState(t *testing.T) {
	e := newEditorRig(t, "SELECT 1", true)
	fileURI := "file://" + filepath.Join(e.folder, "q.sql")
	e.ext.q.bindEditor(fileURI, binding{Connection: e.conn.ID, Database: "d"})
	content := e.api.View(ViewID).Content
	raw, err := content.SaveState()
	if err != nil {
		t.Fatal(err)
	}
	data, err := json.Marshal(raw)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(data), "untitled") || !strings.Contains(string(data), fileURI) {
		t.Errorf("state = %s", data)
	}
	// a second window of the workspace gets them back
	r2 := newRig(t)
	if err := r2.api.View(ViewID).Content.RestoreState(data); err != nil {
		t.Fatal(err)
	}
	if b := r2.ext.q.bindingOf(fileURI); b.Connection != e.conn.ID || b.Database != "d" {
		t.Errorf("restored binding = %+v", b)
	}
	if err := r2.api.View(ViewID).Content.RestoreState([]byte("not json")); err == nil {
		t.Error("a state that is not JSON was accepted")
	}
	// nothing to keep: no state
	r3 := newRig(t)
	if st, err := r3.api.View(ViewID).Content.SaveState(); err != nil || st != nil {
		t.Errorf("empty state = %v, %v", st, err)
	}
}

func TestSavingAnUntitledEditorKeepsItsBinding(t *testing.T) {
	e := newEditorRig(t, "SELECT 1", true)
	newURI := "file://" + filepath.Join(e.folder, "saved.sql")
	e.api.SetDocument(newURI, "SELECT 1")
	e.api.Emit(extension.TopicDocumentSaved, extension.DocumentSaved{URI: newURI, As: true, PreviousURI: e.uri})
	if b := e.ext.q.bindingOf(newURI); b.Connection != e.conn.ID {
		t.Errorf("binding of the saved file = %+v", b)
	}
	if b := e.ext.q.bindingOf(e.uri); b.Connection != "" {
		t.Errorf("the old URI still has a binding: %+v", b)
	}
}

func TestChoosingAnotherConnectionForAnEditor(t *testing.T) {
	e := newEditorRig(t, "SELECT 1", true)
	other := sqliteConn(makeFixture(t))
	other.ID, other.Name = "", "second"
	other = e.save(other)
	e.connect(other)
	if err := e.api.Run(ChooseConnection, nil); err != nil {
		t.Fatal(err)
	}
	pick := e.api.LastPick()
	if pick == nil || len(pick.Labels()) != 2 {
		t.Fatalf("pick = %+v", pick)
	}
	idx := slices.Index(pick.Labels(), "$(database) second")
	pick.Choose(idx)
	if b := e.ext.q.bindingOf(e.uri); b.Connection != other.ID {
		t.Errorf("binding = %+v", b)
	}
	if it := e.api.StatusItem("sql.connection"); it == nil || it.Text != "$(database) second" {
		t.Errorf("item = %+v", it)
	}
}

func TestTheStatusItemFollowsTheActiveEditorAndTheColorOfTheConnection(t *testing.T) {
	e := newEditorRig(t, "SELECT 1", true)
	c := e.conn
	c.Color = "#d9534f"
	if _, _, err := e.db.Save(context.Background(), c, dbconn.PasswordChange{}); err != nil {
		t.Fatal(err)
	}
	e.activate(nil)
	it := e.api.StatusItem("sql.connection")
	if it == nil || it.Hidden || it.Color != "#d9534f" {
		t.Fatalf("item = %+v", it)
	}
	// an editor of another language hides it
	e.api.Emit(extension.TopicEditorActiveChanged, extension.ActiveEditor{URI: "file:///x.go", LanguageID: "go"})
	if it := e.api.StatusItem("sql.connection"); !it.Hidden {
		t.Error("the item shows over a Go file")
	}
	// an editor with no binding asks to choose
	other, err := e.api.Documents().OpenUntitled("sql", "")
	if err != nil {
		t.Fatal(err)
	}
	e.api.Emit(extension.TopicEditorActiveChanged, extension.ActiveEditor{URI: other, LanguageID: "sql"})
	if it := e.api.StatusItem("sql.connection"); it.Hidden || it.Text != "$(database) Select Connection" || it.Color != "" {
		t.Errorf("item = %+v", it)
	}
}

func TestNewQueryBindsTheEditorAndSelectTopRunsIt(t *testing.T) {
	r := newRig(t)
	c := r.save(sqliteConn(makeFixture(t)))
	r.connect(c)
	actions := r.db.ActionsIn(r.api, c.ID, r.node(c, "main", "Tables", "books"))
	run(t, actions, "New Query")
	docs := r.api.UntitledDocuments()
	if len(docs) != 1 {
		t.Fatalf("documents = %d", len(docs))
	}
	if b := r.ext.q.bindingOf(docs[0].URI); b.Connection != c.ID {
		t.Errorf("binding = %+v", b)
	}
	run(t, actions, "Select Top 1000")
	docs = r.api.UntitledDocuments()
	if len(docs) != 2 {
		t.Fatalf("documents = %d", len(docs))
	}
	ed := r.ext.q.eds[docs[1].URI]
	r.api.WaitFor("the query to run", func() bool { return ed != nil && ed.shown != nil && !ed.shown.running && len(ed.shown.steps) == 1 })
	if got := cellsOf(ed.shown.sets[0].set); len(got) != 2 || got[0][2] != "The Dispossessed" {
		t.Errorf("rows = %v", got)
	}
	if r.ext.q.shown != ed {
		t.Error("the panel does not show the results")
	}
}

func TestExportingTheResult(t *testing.T) {
	e := newEditorRig(t, "SELECT id, name, born FROM authors ORDER BY id DESC", true)
	rec := e.run(RunAllQueries)
	// the grid sorts the rows, and the export follows what the grid shows
	grid := e.ext.q.gridOf(e.ext.q.eds[e.uri], rec, rec.sets[0])
	grid.Model().SortBy(0, resultgrid.SortAsc)
	dir := t.TempDir()
	tests := []struct {
		format string
		file   string
		check  func(t *testing.T, got string)
	}{
		{formatCSV, "r.csv", func(t *testing.T, got string) {
			if got != "id,name,born\n1,Ursula,1929\n2,Iain,1954\n" {
				t.Errorf("csv = %q", got)
			}
		}},
		{formatJSON, "r.json", func(t *testing.T, got string) {
			var rows []map[string]any
			if err := json.Unmarshal([]byte(got), &rows); err != nil || len(rows) != 2 || rows[0]["name"] != "Ursula" || rows[1]["id"] != float64(2) {
				t.Errorf("json = %q (%v)", got, err)
			}
		}},
		{formatSQL, "r.sql", func(t *testing.T, got string) {
			want := "INSERT INTO \"authors\" (\"id\", \"name\", \"born\") VALUES (1, 'Ursula', 1929);\nINSERT INTO \"authors\" (\"id\", \"name\", \"born\") VALUES (2, 'Iain', 1954);\n"
			if got != want {
				t.Errorf("sql = %q", got)
			}
		}},
	}
	for _, tc := range tests {
		t.Run(tc.format, func(t *testing.T) {
			path := filepath.Join(dir, tc.file)
			e.api.AnswerFileChooser(path, nil)
			if err := e.api.Run(ExportResult, tc.format); err != nil {
				t.Fatal(err)
			}
			if tc.format == formatSQL {
				// the table is asked: the guess is the table of the SELECT
				in := e.api.Inputs()
				if len(in) == 0 || in[len(in)-1].Options().Value != "authors" {
					t.Fatalf("inputs = %+v", in)
				}
				in[len(in)-1].Accept("authors")
			}
			e.api.WaitFor("the file", func() bool {
				_, err := os.Stat(path)
				return err == nil && len(e.api.FileChooserCalls()) > 0
			})
			e.api.WaitFor("the note", func() bool {
				notes := e.api.Notes()
				return len(notes) > 0 && strings.HasPrefix(notes[len(notes)-1].Message, "Exported 2 rows to ")
			})
			data, err := os.ReadFile(path)
			if err != nil {
				t.Fatal(err)
			}
			tc.check(t, string(data))
			if info, _ := os.Stat(path); info.Mode().Perm() != 0o600 {
				t.Errorf("mode = %v", info.Mode())
			}
		})
	}
	calls := e.api.FileChooserCalls()
	if len(calls) != 3 || !calls[0].Options.Save || calls[0].Options.Name != "result.csv" || calls[2].Options.Filters[0].Patterns[0] != "*.sql" {
		t.Errorf("chooser calls = %+v", calls)
	}
}

func TestExportWithNothingToExportOrAClosedDialog(t *testing.T) {
	e := newEditorRig(t, "SELECT id FROM authors WHERE id > 100", true)
	if err := e.api.Run(ExportResult, formatCSV); err != nil {
		t.Fatal(err)
	}
	if notes := e.api.Notes(); len(notes) == 0 || !strings.Contains(notes[len(notes)-1].Message, "no result with rows") {
		t.Errorf("notes = %+v", notes)
	}
	e.run(RunAllQueries) // no rows
	if err := e.api.Run(ExportResult, formatCSV); err != nil {
		t.Fatal(err)
	}
	if len(e.api.FileChooserCalls()) != 0 {
		t.Error("a result with no rows asked for a file")
	}
	e2 := newEditorRig(t, "SELECT id FROM authors", true)
	e2.run(RunAllQueries)
	e2.api.AnswerFileChooser("", nil) // closed
	if err := e2.api.Run(ExportResult, formatJSON); err != nil {
		t.Fatal(err)
	}
	e2.api.WaitFor("the dialog", func() bool { return len(e2.api.FileChooserCalls()) == 1 })
	e2.api.Pump()
	for _, n := range e2.api.Notes() {
		if strings.Contains(n.Message, "Exported") || n.Severity == "error" {
			t.Errorf("a closed dialog said %+v", n)
		}
	}
	// no format: the user is asked
	if err := e2.api.Run(ExportResult, nil); err != nil {
		t.Fatal(err)
	}
	if pick := e2.api.LastPick(); pick == nil || len(pick.Labels()) != 3 {
		t.Errorf("pick = %+v", pick)
	}
}
