package mongodb

import (
	"context"
	"encoding/json"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/rm4n0s/errors"
	"go.mongodb.org/mongo-driver/v2/bson"

	"github.com/rm4n0s/emetos/extension"
	"github.com/rm4n0s/emetos/extensions/internal/dbconn"
)

// playRig is a rig with a connection that is open and a MongoDB playground that is in front.
type playRig struct {
	*rig
	conn dbconn.Connection
	uri  string
}

// newPlayRig makes the playground with the script in front, and binds it to the connection and the database "shop" when bound is set.
func newPlayRig(t *testing.T, script string, bound bool) *playRig {
	t.Helper()
	r := newRig(t)
	c := r.shop()
	r.dbAPI.Pump()
	uri, err := r.mgAPI.Documents().OpenUntitled(LanguageID, script)
	if err != nil {
		t.Fatal(err)
	}
	p := &playRig{rig: r, conn: c, uri: uri}
	if bound {
		r.ext.pg.bindEditor(uri, binding{Connection: c.ID, Database: "shop"})
	}
	p.activate(nil)
	return p
}

// activate makes the playground the active editor with the selections.
func (p *playRig) activate(sel []extension.Selection) {
	p.t.Helper()
	p.mgAPI.SetActive(&extension.ActiveDocument{URI: p.uri, LanguageID: LanguageID, Selections: sel})
	p.mgAPI.Emit(extension.TopicEditorActiveChanged, extension.ActiveEditor{URI: p.uri, LanguageID: LanguageID})
	p.mgAPI.Pump()
}

// cursorAt puts the cursor in the text at the first occurrence of the marker.
func (p *playRig) cursorAt(marker string) {
	p.t.Helper()
	text, _ := p.mgAPI.DocumentText(p.uri)
	i := strings.Index(text, marker)
	if i < 0 {
		p.t.Fatalf("no %q in %q", marker, text)
	}
	pos := positionOf(text, i)
	p.activate([]extension.Selection{{Anchor: pos, Active: pos}})
}

// selectText selects the first occurrence of the text.
func (p *playRig) selectText(sel string) {
	p.t.Helper()
	text, _ := p.mgAPI.DocumentText(p.uri)
	i := strings.Index(text, sel)
	if i < 0 {
		p.t.Fatalf("no %q in %q", sel, text)
	}
	p.activate([]extension.Selection{{Anchor: positionOf(text, i), Active: positionOf(text, i+len(sel))}})
}

func positionOf(text string, off int) extension.Position {
	return extension.Position{Line: strings.Count(text[:off], "\n"), Col: off - (strings.LastIndexByte(text[:off], '\n') + 1)}
}

// editor is the state of the playground's editor.
func (p *playRig) editor() *editor { return p.ext.pg.eds[p.uri] }

// run runs a command and waits until the run of the editor ends.
func (p *playRig) run(command string) *runRecord {
	p.t.Helper()
	if err := p.mgAPI.Run(command, nil); err != nil {
		p.t.Fatal(err)
	}
	return p.wait()
}

// wait waits for the run of the editor to end and returns its record.
func (p *playRig) wait() *runRecord {
	p.t.Helper()
	p.mgAPI.WaitFor("a run to start", func() bool { return p.editor() != nil && p.editor().rec != nil })
	ed := p.editor()
	p.mgAPI.WaitFor("the run to end", func() bool { return ed.running == nil && ed.rec != nil && !ed.rec.running })
	return ed.rec
}

// notes lists the messages of the notifications so far.
func (p *playRig) notes() []string {
	var out []string
	for _, n := range p.mgAPI.Notes() {
		out = append(out, n.Severity+": "+n.Message)
	}
	return out
}

// lastNote is the last notification, or "".
func (p *playRig) lastNote() string {
	n := p.notes()
	if len(n) == 0 {
		return ""
	}
	return n[len(n)-1]
}

// answer makes the fake server answer every call from the function.
func (p *playRig) answer(fn func(c Call, size int) (Reply, error)) {
	p.fb.mu.Lock()
	p.fb.exec = fn
	p.fb.mu.Unlock()
}

// ordersReply answers a find on orders with documents and every other call with nothing.
func ordersReply(docs []bson.D) func(c Call, size int) (Reply, error) {
	return func(c Call, size int) (Reply, error) {
		if c.Kind == CallFind || c.Kind == CallFindOne {
			return Reply{Docs: docs}, nil
		}
		return Reply{}, nil
	}
}

const twoStatements = "use shop\ndb.orders.find({sku: \"A1\"})\ndb.customers.find({})\n"

func TestRunRunsTheStatementUnderTheCursor(t *testing.T) {
	p := newPlayRig(t, twoStatements, true)
	p.answer(ordersReply(docsOf(3)))
	p.cursorAt(`{sku: "A1"}`)
	rec := p.run(RunCommand)
	execs := p.fb.execList()
	if len(execs) != 1 || execs[0].Call.Collection != "orders" || execs[0].Call.Database != "shop" || execs[0].Call.Kind != CallFind {
		t.Fatalf("calls = %+v", execs)
	}
	if execs[0].PageSize != 20 {
		t.Errorf("page size = %d, want the setting's default", execs[0].PageSize)
	}
	if len(rec.steps) != 1 || len(rec.sets) != 1 || len(rec.sets[0].set.Docs) != 3 || rec.total != 1 {
		t.Fatalf("record = %+v", rec)
	}
	if rec.connection != "Shop" || rec.database != "shop" || rec.tab != 0 {
		t.Errorf("record = %+v", rec)
	}
	// the panel is shown without taking the keyboard
	if view := p.mgAPI.View(ViewID); view == nil || len(view.Reveals) == 0 || view.Reveals[0] {
		t.Errorf("view = %+v", view)
	}
	// the statement is a find on a collection, so its documents can be edited
	if !rec.sets[0].set.Editable {
		t.Error("the documents of a find cannot be edited")
	}
	// the cursor on the other line runs the other statement
	p.cursorAt(`db.customers`)
	rec = p.run(RunCommand)
	if execs = p.fb.execList(); len(execs) != 2 || execs[1].Call.Collection != "customers" {
		t.Errorf("calls = %+v", execs)
	}
	if rec.n != 2 {
		t.Errorf("run number = %d", rec.n)
	}
}

func TestRunAllRunsEveryStatementAndASelectionRunsWhatItCovers(t *testing.T) {
	p := newPlayRig(t, "db.a.find({})\ndb.b.find({})\ndb.c.find({})\n", true)
	p.answer(ordersReply(docsOf(1)))
	rec := p.run(RunAllCommand)
	if rec.total != 3 || len(rec.steps) != 3 || len(rec.sets) != 3 {
		t.Fatalf("record = %+v", rec)
	}
	var names []string
	for _, e := range p.fb.execList() {
		names = append(names, e.Call.Collection)
	}
	if strings.Join(names, ",") != "a,b,c" {
		t.Errorf("order = %v", names)
	}
	if rec.sets[1].title() != "b.find (1 document)" {
		t.Errorf("title = %q", rec.sets[1].title())
	}
	// a selection over the last two lines
	p.selectText("db.b.find({})\ndb.c.find({})")
	rec = p.run(RunCommand)
	if rec.total != 2 {
		t.Fatalf("selection ran %d statements", rec.total)
	}
	if execs := p.fb.execList(); len(execs) != 5 || execs[3].Call.Collection != "b" || execs[4].Call.Collection != "c" {
		t.Errorf("calls = %+v", execs)
	}
}

func TestTheDatabaseOfAStatementIsTheLastUseBeforeItElseTheBindingElseTheConnection(t *testing.T) {
	script := "db.x.find()\nuse one\ndb.x.find()\nuse(\"two\")\ndb.x.find()\n"
	p := newPlayRig(t, script, true)
	run := func() []string {
		before := len(p.fb.execList())
		p.run(RunAllCommand)
		var dbs []string
		for _, e := range p.fb.execList()[before:] {
			dbs = append(dbs, e.Call.Database)
		}
		return dbs
	}
	if got := run(); strings.Join(got, ",") != "shop,one,two" {
		t.Errorf("run all = %v", got)
	}
	// the statement under the cursor runs on the database the use before it selected
	p.cursorAt("db.x.find()\nuse(\"two\")")
	p.cursorAt("db.x.find()\n")
	text, _ := p.mgAPI.DocumentText(p.uri)
	last := strings.LastIndex(text, "db.x.find()")
	pos := positionOf(text, last)
	p.activate([]extension.Selection{{Anchor: pos, Active: pos}})
	before := len(p.fb.execList())
	p.run(RunCommand)
	if e := p.fb.execList()[before:]; len(e) != 1 || e[0].Call.Database != "two" {
		t.Errorf("last statement ran %+v", e)
	}
	// without a binding database the connection's is used, and without that the shell's default
	p.ext.pg.setBinding(p.uri, binding{Connection: p.conn.ID})
	c := p.conn
	c.Database = "fromconn"
	if _, _, err := p.hub.Save(context.Background(), c, dbconn.PasswordChange{}); err != nil {
		t.Fatal(err)
	}
	p.cursorAt("db.x.find()")
	before = len(p.fb.execList())
	p.run(RunCommand)
	if e := p.fb.execList()[before:]; len(e) != 1 || e[0].Call.Database != "fromconn" {
		t.Errorf("first statement ran %+v", e)
	}
	c.Database = ""
	if _, _, err := p.hub.Save(context.Background(), c, dbconn.PasswordChange{}); err != nil {
		t.Fatal(err)
	}
	before = len(p.fb.execList())
	p.run(RunCommand)
	if e := p.fb.execList()[before:]; len(e) != 1 || e[0].Call.Database != DefaultDatabase {
		t.Errorf("with no database ran %+v", e)
	}
}

func TestRunOfAnUnboundPlaygroundAsksWhichConnectionAndThenRuns(t *testing.T) {
	p := newPlayRig(t, "db.orders.find({})", false)
	p.answer(ordersReply(docsOf(2)))
	if err := p.mgAPI.Run(RunCommand, nil); err != nil {
		t.Fatal(err)
	}
	pick := p.mgAPI.LastPick()
	if pick == nil || len(pick.Labels()) != 1 || pick.Labels()[0] != "$(database) Shop" {
		t.Fatalf("pick = %+v", pick)
	}
	pick.Choose(0)
	// then the databases of the server: the default first, then each
	p.mgAPI.WaitFor("the list of databases", func() bool { return len(p.mgAPI.Picks()) == 2 })
	db := p.mgAPI.LastPick()
	if labels := db.Labels(); len(labels) != 6 || labels[0] != "$(star) Default database" || labels[1] != "$(database) local" {
		t.Fatalf("databases = %v", labels)
	}
	var choice int
	for i, l := range db.Labels() {
		if l == "$(database) shop" {
			choice = i
		}
	}
	db.Choose(choice)
	rec := p.wait()
	if len(rec.sets) != 1 {
		t.Fatalf("record = %+v", rec)
	}
	if b := p.ext.pg.bindingOf(p.uri); b.Connection != p.conn.ID || b.Database != "shop" {
		t.Errorf("binding = %+v", b)
	}
	item := p.mgAPI.StatusItem("mongodb.connection")
	if item == nil || item.Hidden || item.Text != "$(database) Shop · shop" || item.Command != SelectConnectionCommand {
		t.Errorf("status item = %+v", item)
	}
}

func TestTheStatusBarItem(t *testing.T) {
	p := newPlayRig(t, "db.orders.find({})", false)
	item := func() *struct {
		text, tip, color string
		hidden           bool
	} {
		it := p.mgAPI.StatusItem("mongodb.connection")
		if it == nil {
			t.Fatal("no status item")
		}
		return &struct {
			text, tip, color string
			hidden           bool
		}{it.Text, it.Tooltip, it.Color, it.Hidden}
	}
	if it := item(); it.hidden || it.text != "$(database) Select Connection" {
		t.Errorf("unbound = %+v", it)
	}
	// a color, a database and read-only
	c := p.conn
	c.Color, c.ReadOnly, c.Database = "#336699", true, "fromconn"
	if _, _, err := p.hub.Save(context.Background(), c, dbconn.PasswordChange{}); err != nil {
		t.Fatal(err)
	}
	p.ext.pg.bindEditor(p.uri, binding{Connection: p.conn.ID})
	it := item()
	if it.text != "$(database) Shop · fromconn" || it.color != "#336699" || !strings.Contains(it.tip, "read-only") || !strings.Contains(it.tip, "db.example.com") {
		t.Errorf("bound = %+v", it)
	}
	p.ext.pg.bindEditor(p.uri, binding{Connection: p.conn.ID, Database: "shop"})
	if it := item(); it.text != "$(database) Shop · shop" {
		t.Errorf("bound to a database = %+v", it)
	}
	// the connection is closed
	if err := p.dbAPI.Run(dbconn.Disconnect, p.conn.ID); err != nil {
		t.Fatal(err)
	}
	p.mgAPI.WaitFor("the item to follow the disconnect", func() bool { return strings.HasSuffix(item().text, "(not connected)") })
	// the connection is deleted
	p.ext.pg.setBinding(p.uri, binding{Connection: "gone"})
	p.ext.pg.updateStatus()
	if it := item(); it.text != "$(warning) Connection deleted" {
		t.Errorf("deleted = %+v", it)
	}
	// another language hides it
	p.mgAPI.Emit(extension.TopicEditorActiveChanged, extension.ActiveEditor{URI: "untitled:Untitled-9", LanguageID: "json"})
	p.mgAPI.Pump()
	if !item().hidden {
		t.Error("the item shows for a JSON editor")
	}
}

func TestRunWhenTheConnectionIsNotOpenOrWasDeleted(t *testing.T) {
	p := newPlayRig(t, "db.orders.find({})", true)
	if err := p.dbAPI.Run(dbconn.Disconnect, p.conn.ID); err != nil {
		t.Fatal(err)
	}
	p.mgAPI.WaitFor("the disconnect", func() bool { return p.hub.State(p.conn.ID).Status != dbconn.Connected })
	if err := p.mgAPI.Run(RunCommand, nil); err != nil {
		t.Fatal(err)
	}
	if got := p.lastNote(); !strings.Contains(got, `"Shop" is not connected`) {
		t.Errorf("note = %q", got)
	}
	if len(p.fb.execList()) != 0 {
		t.Error("something ran")
	}
	p.ext.pg.setBinding(p.uri, binding{Connection: "gone"})
	if err := p.mgAPI.Run(RunCommand, nil); err != nil {
		t.Fatal(err)
	}
	if got := p.lastNote(); !strings.Contains(got, "was deleted") {
		t.Errorf("note = %q", got)
	}
	if b := p.ext.pg.bindingOf(p.uri); b.Connection != "" {
		t.Errorf("binding = %+v", b)
	}
	// not a playground, and nothing to run
	p.mgAPI.SetActive(&extension.ActiveDocument{URI: "untitled:Untitled-7", LanguageID: "json"})
	if err := p.mgAPI.Run(RunCommand, nil); err != nil {
		t.Fatal(err)
	}
	if got := p.lastNote(); !strings.Contains(got, "Open a MongoDB playground") {
		t.Errorf("note = %q", got)
	}
	empty := newPlayRig(t, "// only a comment\n", true)
	if err := empty.mgAPI.Run(RunCommand, nil); err != nil {
		t.Fatal(err)
	}
	if got := empty.lastNote(); !strings.Contains(got, "nothing to run") {
		t.Errorf("note = %q", got)
	}
}

func TestAMistakeInThePlaygroundRefusesTheRunAndSaysWhere(t *testing.T) {
	p := newPlayRig(t, "db.a.find({})\nvar x = 1\ndb.b.find(\n", true)
	if err := p.mgAPI.Run(RunAllCommand, nil); err != nil {
		t.Fatal(err)
	}
	rec := p.editor().rec
	if len(p.fb.execList()) != 0 {
		t.Fatalf("a playground with mistakes ran: %+v", p.fb.execList())
	}
	if rec == nil || rec.refused == "" || len(rec.steps) != 2 || rec.skipped != 3 || !rec.failed() {
		t.Fatalf("record = %+v", rec)
	}
	st := rec.steps[0]
	if st.line != 2 || st.column != 1 || st.res.Err.Tag != "MongoParseUnsupported" {
		t.Errorf("first mistake = %+v %+v", st, st.res.Err)
	}
	if rec.steps[1].line != 3 {
		t.Errorf("second mistake = %+v", rec.steps[1])
	}
	if got := p.lastNote(); !strings.HasPrefix(got, "error: Nothing was run: the playground has 2 mistakes") {
		t.Errorf("note = %q", got)
	}
	// an untitled document cannot have markers: the cursor goes to the first mistake
	shown := p.mgAPI.ShownDocuments()
	if len(shown) != 1 || shown[0].URI != p.uri || shown[0].Line != 2 || shown[0].Column != 1 {
		t.Errorf("shown = %+v", shown)
	}
	// the cursor on a good statement runs it, whatever the rest of the playground has
	p.cursorAt("db.a.find")
	p.answer(ordersReply(docsOf(1)))
	rec = p.run(RunCommand)
	if rec.failed() || len(rec.sets) != 1 {
		t.Errorf("run at the cursor = %+v", rec)
	}
}

func TestTheMistakesOfAFileAreMarkedInIt(t *testing.T) {
	r := newRig(t)
	c := r.shop()
	uri := "file://" + r.folder + "/shop.mongodb"
	r.mgAPI.SetDocument(uri, "db.a.find({})\ndb.b.nope()\n")
	r.ext.pg.bindEditor(uri, binding{Connection: c.ID, Database: "shop"})
	r.mgAPI.SetActive(&extension.ActiveDocument{URI: uri, LanguageID: LanguageID})
	r.mgAPI.Emit(extension.TopicEditorActiveChanged, extension.ActiveEditor{URI: uri, LanguageID: LanguageID})
	r.mgAPI.Pump()
	if err := r.mgAPI.Run(RunAllCommand, nil); err != nil {
		t.Fatal(err)
	}
	ms := r.mgAPI.Markers(ProblemsOwner)[uri]
	if len(ms) != 1 || ms[0].StartLine != 2 || ms[0].Source != "MongoDB" || !strings.Contains(ms[0].Message, "nope") {
		t.Fatalf("markers = %+v", ms)
	}
	if len(r.mgAPI.ShownDocuments()) != 0 {
		t.Error("the editor was moved although the file has a marker")
	}
	// the next run clears them
	r.fb.exec = ordersReply(docsOf(1))
	r.mgAPI.SetDocument(uri, "db.a.find({})\n")
	if err := r.mgAPI.Run(RunAllCommand, nil); err != nil {
		t.Fatal(err)
	}
	r.mgAPI.WaitFor("the run", func() bool {
		ed := r.ext.pg.eds[uri]
		return ed != nil && ed.rec != nil && !ed.rec.running && len(ed.rec.sets) == 1
	})
	if len(r.mgAPI.Markers(ProblemsOwner)[uri]) != 0 {
		t.Errorf("markers = %+v", r.mgAPI.Markers(ProblemsOwner))
	}
	// an error of the server marks the statement
	r.fb.exec = func(c Call, size int) (Reply, error) {
		return Reply{}, errors.New("MongoReadFailed", "unknown operator: $nope", "code", 2, "codeName", "BadValue")
	}
	if err := r.mgAPI.Run(RunAllCommand, nil); err != nil {
		t.Fatal(err)
	}
	r.mgAPI.WaitFor("the marker", func() bool { return len(r.mgAPI.Markers(ProblemsOwner)[uri]) == 1 })
	if m := r.mgAPI.Markers(ProblemsOwner)[uri][0]; m.Message != "Error 2 (BadValue): unknown operator: $nope" || m.StartLine != 1 || m.StartColumn != 1 {
		t.Errorf("marker = %+v", m)
	}
	// closing the editor takes them away
	r.mgAPI.Emit(extension.TopicDocumentClosed, extension.DocumentClosed{URI: uri})
	r.mgAPI.Pump()
	if len(r.mgAPI.Markers(ProblemsOwner)[uri]) != 0 || r.ext.pg.eds[uri] != nil {
		t.Error("a closed editor kept its markers or its state")
	}
}

func TestAServerErrorIsShownWithItsCodeAndStopsTheRun(t *testing.T) {
	p := newPlayRig(t, "db.a.insertOne({_id: 1})\ndb.b.find({})\n", true)
	p.answer(func(c Call, size int) (Reply, error) {
		return Reply{}, errors.New("MongoWriteFailed", "E11000 duplicate key error", "code", 11000, "codeName", "DuplicateKey")
	})
	rec := p.run(RunAllCommand)
	if !rec.failed() || len(rec.steps) != 1 || rec.skipped != 1 || rec.tab != len(rec.sets) {
		t.Fatalf("record = %+v", rec)
	}
	if got := rec.steps[0].res.Err.Text(); got != "Error 11000 (DuplicateKey): E11000 duplicate key error" {
		t.Errorf("error = %q", got)
	}
	if got := rec.summary(); !strings.HasPrefix(got, "Failed · 1 statement") {
		t.Errorf("summary = %q", got)
	}
	// the event says it failed and carries nothing of the statement
	var ev map[string]any
	for _, pub := range p.mgAPI.PublishedEvents() {
		if pub.Topic == CommandFinishedTopic {
			if err := json.Unmarshal(pub.Data, &ev); err != nil {
				t.Fatal(err)
			}
		}
	}
	if ev == nil || ev["ok"] != false || ev["connection"] != p.conn.ID || len(ev) != 3 {
		t.Errorf("event = %v", ev)
	}
	if _, ok := ev["millis"].(float64); !ok {
		t.Errorf("millis = %v", ev["millis"])
	}
}

func TestTheEventOfAStatementThatWorked(t *testing.T) {
	p := newPlayRig(t, "db.a.find({})\ndb.b.find({})\n", true)
	p.answer(ordersReply(docsOf(1)))
	p.run(RunAllCommand)
	var n int
	for _, pub := range p.mgAPI.PublishedEvents() {
		if pub.Topic != CommandFinishedTopic {
			continue
		}
		n++
		var ev map[string]any
		if err := json.Unmarshal(pub.Data, &ev); err != nil {
			t.Fatal(err)
		}
		if ev["ok"] != true || ev["connection"] != p.conn.ID {
			t.Errorf("event = %v", ev)
		}
		if strings.Contains(string(pub.Data), "find") || strings.Contains(string(pub.Data), "orders") {
			t.Errorf("the event carries the statement: %s", pub.Data)
		}
	}
	if n != 2 {
		t.Errorf("%d events, want one for each statement", n)
	}
}

func TestAReadOnlyConnectionRefusesWhatWritesBeforeAnythingIsSent(t *testing.T) {
	p := newPlayRig(t, "db.a.find({})\ndb.a.insertOne({x: 1})\n", true)
	c := p.conn
	c.ReadOnly = true
	if _, _, err := p.hub.Save(context.Background(), c, dbconn.PasswordChange{}); err != nil {
		t.Fatal(err)
	}
	if err := p.mgAPI.Run(RunAllCommand, nil); err != nil {
		t.Fatal(err)
	}
	if got := p.lastNote(); got != `error: "Shop" is read-only: statement 2 (insertOne) does not only read. Nothing was run.` {
		t.Errorf("note = %q", got)
	}
	if len(p.fb.execList()) != 0 {
		t.Errorf("the statement that reads ran before the refusal: %+v", p.fb.execList())
	}
	// what reads runs: find, show, a command that reads, an aggregation that does not write
	p.mgAPI.SetDocument(p.uri, "show dbs\ndb.a.find({})\ndb.runCommand({ping: 1})\ndb.a.aggregate([{$match: {}}])\ndb.a.countDocuments({})\n")
	p.answer(ordersReply(docsOf(1)))
	rec := p.run(RunAllCommand)
	if rec.failed() || len(rec.steps) != 5 {
		t.Errorf("record = %+v", rec)
	}
	// what writes through a command or an aggregation is refused
	for _, text := range []string{"db.runCommand({create: \"x\"})", "db.a.aggregate([{$out: \"b\"}])", "db.a.drop()", "db.a.createIndex({a: 1})", "db.a.bulkWrite([{deleteOne: {filter: {}}}])"} {
		p.mgAPI.SetDocument(p.uri, text)
		before := len(p.notes())
		if err := p.mgAPI.Run(RunAllCommand, nil); err != nil {
			t.Fatal(err)
		}
		if n := p.notes(); len(n) != before+1 || !strings.Contains(n[len(n)-1], "is read-only: statement 1") {
			t.Errorf("%s: notes = %v", text, n[before:])
		}
	}
}

func TestDestructiveStatementsAskFirst(t *testing.T) {
	p := newPlayRig(t, "db.a.deleteMany({})\ndb.b.drop()\ndb.c.find({})\n", true)
	p.answer(ordersReply(docsOf(1)))
	if err := p.mgAPI.Run(RunAllCommand, nil); err != nil {
		t.Fatal(err)
	}
	pick := p.mgAPI.LastPick()
	if pick == nil {
		t.Fatal("nothing was asked")
	}
	if got := pick.Options().Placeholder; got != `deleteMany({}), drop can lose data. Run 3 statements on "Shop"?` {
		t.Errorf("placeholder = %q", got)
	}
	if labels := pick.Labels(); len(labels) != 2 || labels[0] != "$(play) Run" || labels[1] != "$(close) Cancel" {
		t.Errorf("items = %v", labels)
	}
	if len(p.fb.execList()) != 0 {
		t.Error("something ran before the answer")
	}
	pick.Choose(1)
	if len(p.fb.execList()) != 0 || p.editor() != nil && p.editor().rec != nil {
		t.Errorf("Cancel ran something: %+v", p.fb.execList())
	}
	if err := p.mgAPI.Run(RunAllCommand, nil); err != nil {
		t.Fatal(err)
	}
	p.mgAPI.LastPick().Choose(0)
	rec := p.wait()
	if len(rec.steps) != 3 || len(p.fb.execList()) != 3 {
		t.Errorf("record = %+v", rec)
	}
	// a delete with a filter, and a drop that the setting does not guard
	p.mgAPI.SetDocument(p.uri, "db.a.deleteMany({x: 1})\n")
	picks := len(p.mgAPI.Picks())
	p.run(RunAllCommand)
	if len(p.mgAPI.Picks()) != picks {
		t.Error("a deleteMany with a filter asked")
	}
	p.mgAPI.SetDocument(p.uri, "db.runCommand({dropDatabase: 1})\n")
	p.mgAPI.SetSetting(ConfirmDestructiveSetting, false)
	p.run(RunAllCommand)
	if len(p.mgAPI.Picks()) != picks {
		t.Error("it asked although the setting is off")
	}
	p.mgAPI.SetSetting(ConfirmDestructiveSetting, true)
	if err := p.mgAPI.Run(RunAllCommand, nil); err != nil {
		t.Fatal(err)
	}
	if got := p.mgAPI.LastPick().Options().Placeholder; got != `dropDatabase can lose data. Run 1 statement on "Shop"?` {
		t.Errorf("placeholder = %q", got)
	}
	p.mgAPI.LastPick().Choose(1)
}

func TestMorePagesReadTheCursor(t *testing.T) {
	p := newPlayRig(t, "db.orders.find({})", true)
	cur := &fakeCursor{docs: docsOf(45)}
	p.answer(func(c Call, size int) (Reply, error) { return Reply{Docs: docsOf(20), Cursor: cur}, nil })
	p.mgAPI.SetSetting(PageSizeSetting, 20)
	rec := p.run(RunCommand)
	sr := rec.sets[0]
	if !sr.set.More() || len(sr.set.Docs) != 20 || sr.title() != "orders.find (20+ documents)" || sr.set.PageSize != 20 {
		t.Fatalf("set = %+v title %q", sr.set, sr.title())
	}
	more := func() {
		before := len(sr.set.Docs)
		p.ext.pg.fetchMore(p.editor(), rec, sr)
		p.mgAPI.WaitFor("the page", func() bool { return !rec.fetching && len(sr.set.Docs) > before })
	}
	more()
	if len(sr.set.Docs) != 40 || !sr.set.More() {
		t.Fatalf("after one More: %d docs, more %v", len(sr.set.Docs), sr.set.More())
	}
	more()
	if len(sr.set.Docs) != 60 || !sr.set.More() {
		t.Fatalf("after two: %d docs, more %v", len(sr.set.Docs), sr.set.More())
	}
	more()
	if len(sr.set.Docs) != 65 || sr.set.More() {
		t.Fatalf("after three: %d docs, more %v", len(sr.set.Docs), sr.set.More())
	}
	if reads := cur.reads; len(reads) != 3 || reads[0] != 20 {
		t.Errorf("reads = %v", reads)
	}
	if sr.title() != "orders.find (65 documents)" {
		t.Errorf("title = %q", sr.title())
	}
	// More on a set that has no more does nothing
	p.ext.pg.fetchMore(p.editor(), rec, sr)
	if rec.fetching {
		t.Error("it fetched with nothing to fetch")
	}
	// the page size is the setting's
	p.mgAPI.SetSetting(PageSizeSetting, 5)
	cur2 := &fakeCursor{docs: docsOf(5)}
	p.answer(func(c Call, size int) (Reply, error) { return Reply{Docs: docsOf(size), Cursor: cur2}, nil })
	rec = p.run(RunCommand)
	if execs := p.fb.execList(); execs[len(execs)-1].PageSize != 5 || rec.sets[0].set.PageSize != 5 || len(rec.sets[0].set.Docs) != 5 {
		t.Errorf("page size: calls %+v set %d", execs[len(execs)-1], rec.sets[0].set.PageSize)
	}
	// a new run lets go of the cursor of the old one
	p.run(RunCommand)
	for i := 0; i < 200 && !cur2.isClosed(); i++ {
		time.Sleep(5 * time.Millisecond)
	}
	if !cur2.isClosed() {
		t.Error("the cursor of the earlier run was left open")
	}
}

func TestAFailingPageEndsTheCursorAndSaysSo(t *testing.T) {
	p := newPlayRig(t, "db.orders.find({})", true)
	cur := &fakeCursor{docs: docsOf(10), err: errors.New("MongoCursorFailed", "cursor id 7 not found", "code", 43)}
	p.answer(func(c Call, size int) (Reply, error) { return Reply{Docs: docsOf(2), Cursor: cur}, nil })
	rec := p.run(RunCommand)
	sr := rec.sets[0]
	p.ext.pg.fetchMore(p.editor(), rec, sr)
	p.mgAPI.WaitFor("the failure", func() bool { return !rec.fetching && !sr.set.More() })
	if got := p.lastNote(); got != "error: Cannot fetch more documents: cursor id 7 not found" {
		t.Errorf("note = %q", got)
	}
	if len(sr.set.Docs) != 2 {
		t.Errorf("docs = %d", len(sr.set.Docs))
	}
}

func TestCancelStopsTheStatementThatRuns(t *testing.T) {
	p := newPlayRig(t, "db.orders.find({})\ndb.orders.find({})", true)
	entered := make(chan struct{})
	var once sync.Once
	// a server whose Exec waits for the context of the call
	p.ext.mgr.mu.Lock()
	p.ext.mgr.servers[p.conn.ID] = &waitingBackend{fakeBackend: p.fb, entered: entered, once: &once}
	p.ext.mgr.mu.Unlock()
	if err := p.mgAPI.Run(RunAllCommand, nil); err != nil {
		t.Fatal(err)
	}
	<-entered
	p.mgAPI.Pump()
	ed := p.editor()
	if ed == nil || !ed.busy() || p.mgAPI.ContextKey(RunningKey) != true {
		t.Fatalf("the run does not show: editor %+v key %v", ed, p.mgAPI.ContextKey(RunningKey))
	}
	// a second run waits for the first
	if err := p.mgAPI.Run(RunCommand, nil); err != nil {
		t.Fatal(err)
	}
	if got := p.lastNote(); !strings.HasPrefix(got, "warn: A statement is running") {
		t.Errorf("note = %q", got)
	}
	if err := p.mgAPI.Run(CancelCommand, nil); err != nil {
		t.Fatal(err)
	}
	rec := p.wait()
	if !rec.cancelled || len(rec.steps) != 1 || rec.skipped != 1 || !rec.steps[0].res.Cancelled || rec.summary() != "Cancelled · 1 statement · "+formatElapsed(rec.elapsed) {
		t.Fatalf("record = %+v summary %q", rec, rec.summary())
	}
	if p.mgAPI.ContextKey(RunningKey) != false {
		t.Errorf("running key = %v", p.mgAPI.ContextKey(RunningKey))
	}
	// with nothing running, cancel says so
	if err := p.mgAPI.Run(CancelCommand, nil); err != nil {
		t.Fatal(err)
	}
	if got := p.lastNote(); got != "info: No statement is running." {
		t.Errorf("note = %q", got)
	}
}

// waitingBackend is a fake server whose Exec waits for its context to end.
type waitingBackend struct {
	*fakeBackend
	entered chan struct{}
	once    *sync.Once
}

func (w *waitingBackend) Exec(ctx context.Context, c Call, pageSize int) (Reply, error) {
	w.once.Do(func() { close(w.entered) })
	<-ctx.Done()
	return Reply{}, errors.NewErr("MongoReadFailed", ctx.Err())
}

func TestOpenCollectionShowsTheFirstDocuments(t *testing.T) {
	r := newRig(t)
	c := r.shop()
	r.fb.exec = ordersReply(docsOf(4))
	nodes := nodesOf(t, r, c)
	var run func()
	for _, a := range r.hub.ActionsIn(r.dbAPI, c.ID, ptrTo(nodes["collection"])) {
		if a.Title == "Open Collection" {
			run = a.Run
		}
	}
	if run == nil {
		t.Fatal("no Open Collection in the menu")
	}
	run()
	var rec *runRecord
	r.mgAPI.WaitFor("the documents", func() bool {
		ed := r.ext.pg.eds["collection:"+c.ID+"/shop/orders"]
		if ed != nil && ed.rec != nil && !ed.rec.running {
			rec = ed.rec
			return true
		}
		return false
	})
	execs := r.fb.execList()
	if len(execs) != 1 || execs[0].Call.Kind != CallFind || execs[0].Call.Database != "shop" || execs[0].Call.Collection != "orders" || len(execs[0].Call.Filter) != 0 || execs[0].PageSize != 20 {
		t.Fatalf("calls = %+v", execs)
	}
	if len(rec.sets) != 1 || len(rec.sets[0].set.Docs) != 4 || !rec.sets[0].set.Editable || r.ext.pg.shown.name != "shop.orders" {
		t.Fatalf("record = %+v", rec)
	}
	// through the command, and for a collection of a connection that is closed
	if err := r.mgAPI.Run(OpenCollectionCommand, map[string]any{"connection": c.ID, "database": "shop", "collection": "customers"}); err != nil {
		t.Fatal(err)
	}
	r.mgAPI.WaitFor("the second collection", func() bool { return len(r.fb.execList()) == 2 })
	for _, bad := range []any{nil, map[string]any{"connection": "gone", "database": "shop", "collection": "x"}, map[string]any{"connection": c.ID}} {
		if err := r.mgAPI.Run(OpenCollectionCommand, bad); err != nil {
			t.Fatal(err)
		}
	}
	if len(r.fb.execList()) != 2 {
		t.Error("a bad argument ran something")
	}
	if err := r.dbAPI.Run(dbconn.Disconnect, c.ID); err != nil {
		t.Fatal(err)
	}
	r.mgAPI.WaitFor("the disconnect", func() bool { return r.hub.State(c.ID).Status != dbconn.Connected })
	r.ext.pg.openCollection(c, "shop", "orders")
	if got := r.notes(); got[len(got)-1] != `error: "Shop" is not connected.` {
		t.Errorf("notes = %v", got)
	}
}

// notes lists the messages of the notifications of the extension's API.
func (r *rig) notes() []string {
	var out []string
	for _, n := range r.mgAPI.Notes() {
		out = append(out, n.Severity+": "+n.Message)
	}
	return out
}

func TestTheBindingsOfFilesAreKeptAndRestored(t *testing.T) {
	r := newRig(t)
	c := r.shop()
	file := "file://" + r.folder + "/a.mongodb"
	r.ext.pg.bindEditor(file, binding{Connection: c.ID, Database: "shop"})
	r.ext.pg.bindEditor("untitled:Untitled-1", binding{Connection: c.ID})
	r.ext.pg.mode = ModeJSON
	view := r.mgAPI.View(ViewID)
	state, err := view.Content.SaveState()
	if err != nil {
		t.Fatal(err)
	}
	st, ok := state.(savedState)
	if !ok || len(st.Files) != 1 || st.Files[file].Database != "shop" || st.Mode != ModeJSON {
		t.Fatalf("state = %+v", state)
	}
	raw, err := json.Marshal(state)
	if err != nil {
		t.Fatal(err)
	}
	r2 := newRig(t)
	r2.ext.pg.setBinding(file, binding{Connection: "taken"})
	if err := r2.mgAPI.View(ViewID).Content.RestoreState(raw); err != nil {
		t.Fatal(err)
	}
	if b := r2.ext.pg.bindingOf(file); b.Connection != "taken" {
		t.Errorf("a binding that was there was replaced: %+v", b)
	}
	r2.ext.pg.setBinding(file, binding{})
	if err := r2.mgAPI.View(ViewID).Content.RestoreState(raw); err != nil {
		t.Fatal(err)
	}
	if b := r2.ext.pg.bindingOf(file); b.Connection != c.ID || b.Database != "shop" || r2.ext.pg.mode != ModeJSON {
		t.Errorf("binding = %+v mode %q", b, r2.ext.pg.mode)
	}
	err = r2.mgAPI.View(ViewID).Content.RestoreState(json.RawMessage(`{"files": 5}`))
	wantRoute(t, err, "restoreState.MongoStateInvalid")
}

func TestSaveAsMovesTheBindingAndTheResults(t *testing.T) {
	p := newPlayRig(t, "db.orders.find({})", true)
	p.answer(ordersReply(docsOf(1)))
	p.run(RunCommand)
	newURI := "file://" + p.folder + "/orders.mongodb"
	p.mgAPI.Emit(extension.TopicDocumentSaved, extension.DocumentSaved{URI: newURI, As: true, PreviousURI: p.uri})
	p.mgAPI.Pump()
	if b := p.ext.pg.bindingOf(newURI); b.Connection != p.conn.ID || p.ext.pg.bindingOf(p.uri).Connection != "" {
		t.Errorf("bindings: new %+v old %+v", b, p.ext.pg.bindingOf(newURI))
	}
	ed := p.ext.pg.eds[newURI]
	if ed == nil || ed.name != "orders.mongodb" || ed.rec == nil || p.ext.pg.eds[p.uri] != nil {
		t.Errorf("editor = %+v", ed)
	}
	// a save that is not a Save As changes nothing
	p.mgAPI.Emit(extension.TopicDocumentSaved, extension.DocumentSaved{URI: newURI})
	p.mgAPI.Pump()
	if p.ext.pg.eds[newURI] != ed {
		t.Error("a plain save moved the editor")
	}
}

func TestClosingAnEditorLetsGoOfItsCursorsAndDeactivationStopsEverything(t *testing.T) {
	p := newPlayRig(t, "db.orders.find({})", true)
	cur := &fakeCursor{docs: docsOf(9)}
	p.answer(func(c Call, size int) (Reply, error) { return Reply{Docs: docsOf(2), Cursor: cur}, nil })
	p.run(RunCommand)
	p.mgAPI.Emit(extension.TopicDocumentClosed, extension.DocumentClosed{URI: p.uri})
	p.mgAPI.Pump()
	for i := 0; i < 200 && !cur.isClosed(); i++ {
		time.Sleep(5 * time.Millisecond)
	}
	if !cur.isClosed() || p.editor() != nil || p.ext.pg.shown != nil {
		t.Errorf("closed %v editor %v shown %v", cur.isClosed(), p.editor(), p.ext.pg.shown)
	}
	if b := p.ext.pg.bindingOf(p.uri); b.Connection != "" {
		t.Errorf("an untitled document kept its binding: %+v", b)
	}
	// deactivation closes the cursors that are left
	p2 := newPlayRig(t, "db.orders.find({})", true)
	cur2 := &fakeCursor{docs: docsOf(9)}
	p2.answer(func(c Call, size int) (Reply, error) { return Reply{Docs: docsOf(2), Cursor: cur2}, nil })
	p2.run(RunCommand)
	p2.cancel()
	for i := 0; i < 400 && !cur2.isClosed(); i++ {
		time.Sleep(5 * time.Millisecond)
	}
	if !cur2.isClosed() {
		t.Error("the cursor was left open when the extension was deactivated")
	}
	p2.ext.mgr.mu.Lock()
	_, still := p2.ext.mgr.tabs[p2.mgAPI.Scope()]
	p2.ext.mgr.mu.Unlock()
	if still {
		t.Error("the tab is still registered")
	}
}

func TestANewPlaygroundFromTheCommandIsBoundToTheConnection(t *testing.T) {
	r := newRig(t)
	c := r.shop()
	if err := r.mgAPI.Run(NewPlaygroundCommand, map[string]any{"connection": c.ID, "database": "shop", "collection": "orders"}); err != nil {
		t.Fatal(err)
	}
	docs := r.mgAPI.UntitledDocuments()
	if len(docs) != 1 || !strings.Contains(docs[0].Text, "use shop") {
		t.Fatalf("documents = %+v", docs)
	}
	if b := r.ext.pg.bindingOf(docs[0].URI); b.Connection != c.ID || b.Database != "shop" {
		t.Errorf("binding = %+v", b)
	}
	// the menu of the explorer binds too
	nodes := nodesOf(t, r, c)
	for _, a := range r.hub.Actions(c.ID, ptrTo(nodes["database"])) {
		if a.Title == "New Playground" {
			a.Run()
		}
	}
	docs = r.mgAPI.UntitledDocuments()
	if len(docs) != 2 {
		t.Fatalf("documents = %+v", docs)
	}
	if b := r.ext.pg.bindingOf(docs[1].URI); b.Connection != c.ID || b.Database != "shop" {
		t.Errorf("binding = %+v", b)
	}
	// and the status bar says so for the playground that is in front
	r.mgAPI.SetActive(&extension.ActiveDocument{URI: docs[1].URI, LanguageID: LanguageID})
	r.mgAPI.Emit(extension.TopicEditorActiveChanged, extension.ActiveEditor{URI: docs[1].URI, LanguageID: LanguageID})
	r.mgAPI.Pump()
	if it := r.mgAPI.StatusItem("mongodb.connection"); it == nil || it.Text != "$(database) Shop · shop" {
		t.Errorf("status = %+v", it)
	}
}

func TestACursorOnACommentRunsTheNextStatementAndAfterTheLastTheLastOne(t *testing.T) {
	p := newPlayRig(t, "// first\n\ndb.a.find({})\n// between\ndb.b.find({})\n// the end\n", true)
	p.answer(ordersReply(docsOf(1)))
	for _, tc := range []struct{ marker, want string }{{"// first", "a"}, {"// between", "b"}, {"// the end", "b"}} {
		p.cursorAt(tc.marker)
		before := len(p.fb.execList())
		p.run(RunCommand)
		if e := p.fb.execList()[before:]; len(e) != 1 || e[0].Call.Collection != tc.want {
			t.Errorf("cursor on %q ran %+v, want %s", tc.marker, e, tc.want)
		}
	}
}
