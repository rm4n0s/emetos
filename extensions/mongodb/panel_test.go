package mongodb

import (
	"regexp"
	"strings"
	"testing"
	"time"

	"github.com/rm4n0s/errors"
	"github.com/rm4n0s/glutter/gluttertest"
	gui "github.com/rm4n0s/glutter/ui"
	"github.com/rm4n0s/glutter/widgets"
	"go.mongodb.org/mongo-driver/v2/bson"

	"github.com/rm4n0s/emetos/extension"
	"github.com/rm4n0s/emetos/extension/ui/uitest"
	"github.com/rm4n0s/emetos/extensions/internal/resultgrid"
)

// rich makes the text finders look into rich text, which the lines of the JSON view are.
var rich = gluttertest.FindOpts{FindRichText: true}

// steady fixes the times of a run, which the picture shows.
func steady(rec *runRecord) {
	rec.started = time.Date(2026, 10, 29, 9, 30, 0, 0, time.UTC)
	rec.elapsed = 12 * time.Millisecond
	for _, st := range rec.steps {
		st.res.Elapsed = 3 * time.Millisecond
	}
}

// panelView is the panel shown in a window of a test. The panel builds again when the extension asks (Refresh); here settle does it.
type panelView struct {
	*playRig
	ts      *gluttertest.Tester
	rebuild widgets.StateSetter
}

// showPanel shows the panel of the rig in a window.
func showPanel(t *testing.T, p *playRig, th uitest.Theme, size gui.Size) *panelView {
	t.Helper()
	pv := &panelView{playRig: p}
	pv.ts = uitest.Pump(t, th, size, widgets.NewStatefulBuilder(func(bc widgets.BuildContext, setState widgets.StateSetter) widgets.Widget {
		pv.rebuild = setState
		return p.mgAPI.View(ViewID).Content.Build(bc)
	}))
	return pv
}

// settle runs what the panel posted and builds it again.
func (v *panelView) settle() {
	for i := 0; i < 5; i++ {
		v.mgAPI.Pump()
		if v.rebuild != nil {
			v.rebuild(func() {})
		}
		v.ts.Pump(time.Millisecond)
	}
}

// jsonTokens is a small grammar of JSON for the tests: the scopes that give the colors of keys, strings, numbers and constants.
func jsonTokens(_, text string) ([][]extension.Token, error) {
	re := regexp.MustCompile(`"(?:[^"\\]|\\.)*"\s*:|"(?:[^"\\]|\\.)*"|-?\d+(?:\.\d+)?(?:[eE][+-]?\d+)?|true|false|null`)
	var out [][]extension.Token
	for _, line := range strings.Split(text, "\n") {
		var toks []extension.Token
		for _, m := range re.FindAllStringIndex(line, -1) {
			s := line[m[0]:m[1]]
			scope := "constant.numeric.json"
			end := m[1]
			switch {
			case strings.HasSuffix(s, ":"):
				scope, end = "support.type.property-name.json", m[0]+strings.LastIndex(s, `"`)+1
			case strings.HasPrefix(s, `"`):
				scope = "string.quoted.double.json"
			case s == "true" || s == "false" || s == "null":
				scope = "constant.language.json"
			}
			toks = append(toks, extension.Token{Start: m[0], End: end, Scopes: []string{"source.json", scope}})
		}
		out = append(out, toks)
	}
	return out, nil
}

// shopDocs are the documents of the panel tests: orders with nested documents, arrays and the BSON types.
func shopDocs() []bson.D {
	return []bson.D{
		{{Key: "_id", Value: bson.ObjectID{0x65, 0x3d, 0x2a, 0x10, 0, 0, 0, 0, 0, 0, 0, 1}}, {Key: "sku", Value: "A1"}, {Key: "qty", Value: int32(2)},
			{Key: "price", Value: 9.5}, {Key: "paid", Value: true},
			{Key: "created", Value: bson.NewDateTimeFromTime(time.Date(2026, 10, 29, 9, 30, 0, 0, time.UTC))},
			{Key: "customer", Value: bson.D{{Key: "name", Value: "Ann"}, {Key: "address", Value: bson.D{{Key: "city", Value: "Athens"}, {Key: "zip", Value: "10431"}}}}},
			{Key: "tags", Value: bson.A{"new", "gift"}}, {Key: "note", Value: nil}},
		{{Key: "_id", Value: bson.ObjectID{0x65, 0x3d, 0x2a, 0x11, 0, 0, 0, 0, 0, 0, 0, 2}}, {Key: "sku", Value: "B2"}, {Key: "qty", Value: int64(5)},
			{Key: "price", Value: 20.0}, {Key: "paid", Value: false},
			{Key: "created", Value: bson.NewDateTimeFromTime(time.Date(2026, 10, 28, 17, 5, 0, 0, time.UTC))},
			{Key: "customer", Value: bson.D{{Key: "name", Value: "Bob"}}},
			{Key: "lines", Value: bson.A{bson.D{{Key: "sku", Value: "B2"}, {Key: "n", Value: 1.5}}}}, {Key: "note", Value: "rush"}},
		{{Key: "_id", Value: bson.ObjectID{0x65, 0x3d, 0x2a, 0x12, 0, 0, 0, 0, 0, 0, 0, 3}}, {Key: "sku", Value: "C3"}, {Key: "qty", Value: int32(1)}},
	}
}

// panelRig runs `db.orders.find({})` with the shop documents and a cursor that has more, and shows the panel.
func panelRig(t *testing.T, th uitest.Theme, size gui.Size, mode viewMode) *panelView {
	t.Helper()
	p := newPlayRig(t, "db.orders.find({})", true)
	p.mgAPI.SetTokenizer(jsonTokens)
	cur := &fakeCursor{docs: docsOf(30)}
	p.answer(func(c Call, size int) (Reply, error) { return Reply{Docs: shopDocs(), Cursor: cur}, nil })
	steady(p.run(RunCommand))
	p.ext.pg.mode = mode
	return showPanel(t, p, th, size)
}

// openRows taps the rows with the labels one after the other, waiting for what is below each to show.
func openRows(t *testing.T, v *panelView, rows ...[2]string) {
	t.Helper()
	for _, row := range rows {
		v.ts.Tap(gluttertest.ByTextContaining(row[0]))
		deadline := time.Now().Add(2 * time.Second)
		for !v.ts.Exists(gluttertest.ByTextContaining(row[1])) {
			if time.Now().After(deadline) {
				t.Fatalf("%q did not show after opening %q", row[1], row[0])
			}
			v.settle()
			time.Sleep(time.Millisecond)
		}
	}
}

func TestGoldenTheTreeOfDocuments(t *testing.T) {
	for _, th := range uitest.Themes() {
		t.Run(th.Name, func(t *testing.T) {
			pv := panelRig(t, th, gui.Size{Width: 900, Height: 400}, ModeTree)
			ts := pv.ts
			pv.settle()
			openRows(t, pv, [2]string{"#1", "customer"}, [2]string{"customer", "address"}, [2]string{"address", "city"})
			uitest.MatchesGolden(ts, "results_tree_"+th.Name)
		})
	}
}

func TestGoldenTheJSONOfDocuments(t *testing.T) {
	for _, th := range uitest.Themes() {
		t.Run(th.Name, func(t *testing.T) {
			pv := panelRig(t, th, gui.Size{Width: 900, Height: 330}, ModeJSON)
			p, ts := pv.playRig, pv.ts
			p.ext.pg.shown.rec.sets[0].selected = 1
			pv.settle()
			uitest.MatchesGolden(ts, "results_json_"+th.Name)
		})
	}
}

func TestGoldenTheTableOfDocuments(t *testing.T) {
	for _, th := range uitest.Themes() {
		t.Run(th.Name, func(t *testing.T) {
			pv := panelRig(t, th, gui.Size{Width: 900, Height: 330}, ModeTable)
			ts := pv.ts
			pv.settle()
			uitest.MatchesGolden(ts, "results_table_"+th.Name)
		})
	}
}

func TestGoldenTheMessagesOfARun(t *testing.T) {
	script := "db.orders.insertOne({sku: \"D4\"})\ndb.orders.updateMany({qty: {$gt: 1}}, {$set: {paid: true}})\ndb.orders.find({sku: \"D4\"})\ndb.orders.insertOne({_id: 1})\ndb.customers.find({})\n"
	for _, th := range uitest.Themes() {
		t.Run(th.Name, func(t *testing.T) {
			p := newPlayRig(t, script, true)
			p.answer(func(c Call, size int) (Reply, error) {
				switch c.Kind {
				case CallInsertOne:
					if len(c.Docs[0]) == 1 && c.Docs[0][0].Key == "_id" {
						return Reply{}, errors.New("MongoWriteFailed", "E11000 duplicate key error collection: shop.orders index: _id_ dup key: { _id: 1 }", "code", 11000, "codeName", "DuplicateKey")
					}
					return Reply{Docs: []bson.D{{{Key: "acknowledged", Value: true}}}, Write: &WriteSummary{Inserted: 1}}, nil
				case CallUpdateMany:
					return Reply{Docs: []bson.D{{{Key: "acknowledged", Value: true}}}, Write: &WriteSummary{Matched: 3, Modified: 2}}, nil
				}
				return Reply{Docs: shopDocs()[:1]}, nil
			})
			rec := p.run(RunAllCommand)
			if len(rec.steps) != 4 || !rec.failed() || rec.tab != len(rec.sets) {
				t.Fatalf("record = %+v", rec)
			}
			steady(rec)
			pv := showPanel(t, p, th, gui.Size{Width: 900, Height: 275})
			ts := pv.ts
			pv.settle()
			for _, want := range []string{"[1] db.orders.insertOne({sku: \"D4\"})", "1 inserted in", "3 matched, 2 modified in", "1 document returned in",
				"Error 11000 (DuplicateKey): E11000 duplicate key error", "Line 4, column 1", "1 statement not run."} {
				if !ts.Exists(gluttertest.ByTextContaining(want)) {
					t.Errorf("the messages have no %q", want)
				}
			}
			uitest.MatchesGolden(ts, "results_messages_"+th.Name)
		})
	}
}

func TestThePanelBeforeAnythingRan(t *testing.T) {
	r := newRig(t)
	ts := uitest.Pump(t, uitest.Themes()[0], gui.Size{Width: 900, Height: 100}, widgets.NewBuilder(func(bc widgets.BuildContext) widgets.Widget { return r.mgAPI.View(ViewID).Content.Build(bc) }))
	if !ts.Exists(gluttertest.ByTextContaining("Run a playground with Ctrl+Enter")) {
		t.Error("the empty panel does not say how to run")
	}
}

func TestThePanelShowsTheDocumentsInTheThreeViews(t *testing.T) {
	th := uitest.Themes()[0]
	pv := panelRig(t, th, gui.Size{Width: 900, Height: 330}, ModeTree)
	p, ts := pv.playRig, pv.ts
	pv.settle()
	// the toolbar, the tab and the footer
	for _, want := range []string{"Tree", "JSON", "Table", "Edit", "Delete", "Copy", "orders.find (3+ documents)", "Messages", "3+ documents loaded", "More", "Shop · shop", "1 statement"} {
		if !ts.Exists(gluttertest.ByTextContaining(want)) {
			t.Errorf("the panel has no %q", want)
		}
	}
	// the tree: the documents with a preview, the fields when a row is opened
	if !ts.Exists(gluttertest.ByTextContaining("#2")) || !ts.Exists(gluttertest.ByTextContaining(`sku: "A1"`)) {
		t.Error("the tree does not show the documents")
	}
	openRows(t, pv, [2]string{"#1", "customer"})
	for _, want := range []string{"price", "9.5", "double", "ObjectId(", "objectId", "{2 fields}", "[2 items]", "date", "null"} {
		if !ts.Exists(gluttertest.ByTextContaining(want)) {
			t.Errorf("the tree has no %q", want)
		}
	}
	// the JSON view
	ts.Tap(gluttertest.ByText("JSON"))
	pv.settle()
	if p.ext.pg.mode != ModeJSON || !ts.Exists(gluttertest.ByTextContaining(`"sku"`, rich)) {
		t.Errorf("mode %q", p.ext.pg.mode)
	}
	// the table: the fields are the columns
	ts.Tap(gluttertest.ByText("Table"))
	pv.settle()
	if p.ext.pg.mode != ModeTable {
		t.Fatalf("mode %q", p.ext.pg.mode)
	}
	for _, want := range []string{"sku", "qty", "customer", "A1", "B2", "Athens"} {
		if want == "Athens" {
			continue // a document is a cell of JSON text
		}
		if !ts.Exists(gluttertest.ByTextContaining(want)) {
			t.Errorf("the table has no %q", want)
		}
	}
	sr := p.ext.pg.shown.rec.sets[0]
	cols := sr.table.src.Columns()
	if len(cols) != 10 || cols[0].Name != "_id" || cols[len(cols)-1].Name != "lines" {
		t.Errorf("columns = %+v", cols)
	}
	// More in the table's own footer reads the next page and the columns stay when no new field came
	p.ext.pg.fetchMore(p.ext.pg.shown, p.ext.pg.shown.rec, sr)
	p.mgAPI.WaitFor("the page", func() bool { return len(sr.set.Docs) > 3 })
	pv.settle()
	// the documents of the next page have the field n, which makes a column
	if cols := sr.table.src.Columns(); len(cols) != 11 || cols[10].Name != "n" {
		t.Errorf("columns after More = %+v", cols)
	}
	// Messages
	ts.Tap(gluttertest.ByText("Messages"))
	pv.settle()
	if !ts.Exists(gluttertest.ByTextContaining("[1] db.orders.find({})")) {
		t.Error("the messages do not list the statement")
	}
}

func TestMoreInTheTreeAndTheSelectionOfADocument(t *testing.T) {
	th := uitest.Themes()[0]
	pv := panelRig(t, th, gui.Size{Width: 900, Height: 330}, ModeTree)
	p, ts := pv.playRig, pv.ts
	pv.settle()
	sr := p.ext.pg.shown.rec.sets[0]
	ts.Tap(gluttertest.ByTextContaining("#2"))
	if sr.selected != 1 {
		t.Errorf("selected = %d", sr.selected)
	}
	ts.Tap(gluttertest.ByText("More"))
	p.mgAPI.WaitFor("the page", func() bool { return len(sr.set.Docs) == 23 })
	pv.settle()
	if !ts.Exists(gluttertest.ByTextContaining("23+ documents loaded")) && !ts.Exists(gluttertest.ByTextContaining("23 documents loaded")) {
		t.Error("the footer does not count the documents")
	}
	if sr.selected != 1 {
		t.Errorf("More lost the selection: %d", sr.selected)
	}
	// the JSON view: a line of a document selects it
	p.ext.pg.mode = ModeJSON
	p.ext.pg.refresh()
	pv.settle()
	ts.Tap(gluttertest.ByTextContaining(`"sku": "A1"`, rich))
	if sr.selected != 0 {
		t.Errorf("selected = %d", sr.selected)
	}
	// the table: the selected row is the document that Edit would open
	p.ext.pg.mode = ModeTable
	p.ext.pg.refresh()
	pv.settle()
	tv := p.ext.pg.tableOf(p.ext.pg.shown, p.ext.pg.shown.rec, sr)
	tv.grid.Model().PressRow(1, resultgridMods())
	if got := p.ext.pg.selectedIndex(sr); got != 1 {
		t.Errorf("selected index of the table = %d", got)
	}
	var items []string
	for _, it := range p.ext.pg.documentMenuAt(sr, tv.selectedDoc) {
		items = append(items, it.Label)
	}
	if strings.Join(items, ",") != "Edit Document,Delete Document,Copy Document" {
		t.Errorf("menu = %v", items)
	}
}

func TestThePanelForARunThatIsRunningOrWasCancelled(t *testing.T) {
	th := uitest.Themes()[0]
	p := newPlayRig(t, "db.orders.find({})", true)
	rec := &runRecord{n: 1, connection: "Shop", database: "shop", total: 3, current: 2, running: true}
	ed := p.ext.pg.editor(p.uri, "Untitled-1")
	ed.rec, ed.running = rec, rec
	p.ext.pg.shown = ed
	pv := showPanel(t, p, th, gui.Size{Width: 900, Height: 120})
	ts := pv.ts
	pv.settle()
	for _, want := range []string{"Running statement 2 of 3...", "Cancel"} {
		if !ts.Exists(gluttertest.ByTextContaining(want)) {
			t.Errorf("the running panel has no %q", want)
		}
	}
	if ts.Exists(gluttertest.ByText("Tree")) {
		t.Error("the views are offered while a statement runs")
	}
}

func TestTheViewsFollowTheDocumentsThatChange(t *testing.T) {
	p := newPlayRig(t, "db.orders.find({})", true)
	p.answer(func(c Call, size int) (Reply, error) { return Reply{Docs: shopDocs()}, nil })
	rec := p.run(RunCommand)
	sr := rec.sets[0]
	pg := p.ext.pg
	pg.treeOf(sr)
	pg.jsonOf(sr)
	pg.tableOf(pg.shown, rec, sr)
	jsonBefore := sr.json
	sr.set.Docs = append(sr.set.Docs, bson.D{{Key: "_id", Value: int32(9)}, {Key: "brandNew", Value: "x"}})
	pg.syncViews(sr)
	if sr.tree.count != 4 {
		t.Errorf("tree count = %d", sr.tree.count)
	}
	if v := pg.jsonOf(sr); v.count != 4 || v.model != jsonBefore.model || v.ctl != jsonBefore.ctl {
		t.Errorf("json view = %+v (the list must keep its model and its place)", v.count)
	}
	tv := pg.tableOf(pg.shown, rec, sr)
	if tv.shown != 4 || tv.src.Columns()[len(tv.src.Columns())-1].Name != "brandNew" {
		t.Errorf("table = %+v", tv.src.Columns())
	}
	// documents that go
	sr.selected = 3
	sr.set.Docs = sr.set.Docs[:2]
	pg.syncViews(sr)
	if sr.selected != -1 || sr.tree.count != 2 {
		t.Errorf("selected %d tree %d", sr.selected, sr.tree.count)
	}
	sr.invalidate()
	if sr.tree != nil || sr.json != nil || sr.table != nil {
		t.Error("invalidate left views")
	}
}

func TestTheColumnsOfTheTableAreBounded(t *testing.T) {
	doc := bson.D{}
	for i := 0; i < maxColumns+50; i++ {
		doc = append(doc, bson.E{Key: "f" + groupDigits(i), Value: int32(i)})
	}
	src := newDocSource([]bson.D{doc})
	if len(src.Columns()) != maxColumns {
		t.Errorf("%d columns", len(src.Columns()))
	}
}

func TestTheValuesOfTheGrid(t *testing.T) {
	cases := []struct {
		v    any
		want string
	}{
		{nil, "NULL"}, {"text", "text"}, {int32(5), "5"}, {int64(7), "7"}, {int(8), "8"}, {1.5, "1.5"}, {true, "true"},
		{bson.NewDateTimeFromTime(time.Date(2026, 1, 2, 3, 4, 5, 0, time.UTC)), "2026-01-02T03:04:05Z"},
		{bson.ObjectID{0x65, 0x3d, 0x2a, 0x10, 0, 0, 0, 0, 0, 0, 0, 1}, `ObjectId("653d2a100000000000000001")`},
		{bson.Binary{Subtype: 0, Data: []byte{1, 2}}, "0x0102"},
		{bson.D{{Key: "a", Value: int32(1)}}, `{"a":1}`},
		{bson.A{"x", int32(2)}, `["x",2]`},
		{bson.Regex{Pattern: "^a", Options: "i"}, `/^a/i`},
		{bson.MinKey{}, `MinKey()`},
	}
	for _, tc := range cases {
		if got := gridValue(tc.v).Full("NULL"); got != tc.want {
			t.Errorf("gridValue(%#v) = %q, want %q", tc.v, got, tc.want)
		}
	}
}

func TestThePreviewsOfTheTree(t *testing.T) {
	d := shopDocs()[0]
	if got := previewOf(d); got != `_id: ObjectId("653d2a100000000000000001"), sku: "A1", qty: 2, ...` {
		t.Errorf("preview = %q", got)
	}
	cases := []struct {
		v    any
		want string
	}{
		{bson.D{{Key: "a", Value: int32(1)}}, "{1 fields}"}, {bson.A{int32(1), int32(2)}, "[2 items]"}, {"x", `"x"`}, {nil, "null"},
		{strings.Repeat("y", 100), `"` + strings.Repeat("y", 29) + "..."},
	}
	for _, tc := range cases {
		if got := shortValue(tc.v, 30); got != tc.want {
			t.Errorf("shortValue(%v) = %q, want %q", tc.v, got, tc.want)
		}
	}
	for _, d := range []any{nil, int32(1)} {
		if hasChildren(d) {
			t.Errorf("hasChildren(%v)", d)
		}
	}
	if !hasChildren(bson.A{int32(1)}) || hasChildren(bson.D{}) {
		t.Error("hasChildren of arrays and documents")
	}
}

// resultgridMods are no modifier keys.
func resultgridMods() resultgrid.Mods { return resultgrid.Mods{} }
