package mongodb

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/rm4n0s/errors"
	"go.mongodb.org/mongo-driver/v2/bson"

	"github.com/rm4n0s/emetos/extensions/mongodb/playground"
)

// itemsOf parses a playground into the items a run takes.
func itemsOf(t *testing.T, src string) []RunItem {
	t.Helper()
	s := playground.Parse(src)
	var out []RunItem
	for _, it := range s.Items {
		if it.Err != nil {
			t.Fatalf("mistake in %q: %v", src, it.Err)
		}
		out = append(out, RunItem{Text: src[it.Span.Start.Offset:it.Span.End.Offset], Span: it.Span, Statement: it.Statement})
	}
	return out
}

func docsOf(n int) []bson.D {
	out := make([]bson.D, n)
	for i := range out {
		out[i] = bson.D{{Key: "_id", Value: int32(i)}, {Key: "n", Value: int32(i * 2)}}
	}
	return out
}

func TestRunDoesTheStatementsInOrderAndFollowsUse(t *testing.T) {
	fb := &fakeBackend{}
	fb.exec = func(c Call, size int) (Reply, error) {
		switch c.Kind {
		case CallFind:
			return Reply{Docs: docsOf(2)}, nil
		case CallInsertOne:
			return Reply{Docs: []bson.D{{{Key: "acknowledged", Value: true}}}, Write: &WriteSummary{Inserted: 1}}, nil
		}
		return Reply{}, nil
	}
	var started []int
	var finished []string
	res := Run(context.Background(), fb, itemsOf(t, "db.a.find()\nuse other\ndb.b.insertOne({x: 1})\nshow collections\nuse(\"third\")\ndb.runCommand({ping: 1})"), RunOptions{
		Database: "first", PageSize: 7,
		Started:  func(i int, _ RunItem) { started = append(started, i) },
		Finished: func(r *StepResult) { finished = append(finished, r.Message) },
	})
	if len(res) != 6 {
		t.Fatalf("%d results", len(res))
	}
	want := []string{"first.a.find", "", "other.b.insertOne", "other", "", "third"}
	execs := fb.execList()
	if len(execs) != 4 {
		t.Fatalf("%d calls: %+v", len(execs), execs)
	}
	gotDBs := []string{execs[0].Call.Database + "." + execs[0].Call.Collection + "." + string(execs[0].Call.Kind), execs[1].Call.Database + "." + execs[1].Call.Collection + "." + string(execs[1].Call.Kind), execs[2].Call.Database, execs[3].Call.Database}
	if gotDBs[0] != want[0] || gotDBs[1] != want[2] || gotDBs[2] != want[3] || gotDBs[3] != want[5] {
		t.Errorf("calls = %v", gotDBs)
	}
	for _, e := range execs {
		if e.PageSize != 7 {
			t.Errorf("page size = %d", e.PageSize)
		}
	}
	if res[1].Use != "other" || res[1].Message != "switched to database other" || res[1].Call != nil {
		t.Errorf("use = %+v", res[1])
	}
	if res[0].Message != "2 documents returned" || len(res[0].Sets) != 1 || res[0].Sets[0].Title != "a.find" || !res[0].Sets[0].Editable {
		t.Errorf("find = %+v %+v", res[0], res[0].Sets)
	}
	if res[2].Message != "1 inserted" || res[2].Write == nil || res[2].Write.Inserted != 1 {
		t.Errorf("insert = %+v", res[2])
	}
	if res[3].Message != "No documents returned" || len(res[3].Sets) != 0 {
		t.Errorf("show = %+v", res[3])
	}
	if res[0].Database != "first" || res[2].Database != "other" || res[5].Database != "third" {
		t.Errorf("databases = %q %q %q", res[0].Database, res[2].Database, res[5].Database)
	}
	if len(started) != 6 || started[5] != 5 || len(finished) != 6 {
		t.Errorf("started %v finished %v", started, finished)
	}
}

func TestRunStopsAtTheFirstErrorAndSaysItsCode(t *testing.T) {
	fb := &fakeBackend{}
	fb.exec = func(c Call, size int) (Reply, error) {
		if c.Kind == CallInsertOne {
			return Reply{}, errors.New("MongoWriteFailed", "E11000 duplicate key error collection: shop.orders index: _id_ dup key: { _id: 1 }", "code", 11000, "codeName", "DuplicateKey")
		}
		return Reply{}, nil
	}
	res := Run(context.Background(), fb, itemsOf(t, "db.a.find()\ndb.a.insertOne({_id: 1})\ndb.a.find()"), RunOptions{})
	if len(res) != 2 {
		t.Fatalf("%d results, want the run to stop at the second", len(res))
	}
	e := res[1].Err
	if e == nil || e.Code != 11000 || e.CodeName != "DuplicateKey" || e.Tag != "MongoWriteFailed" {
		t.Fatalf("err = %+v", e)
	}
	if got := e.Text(); !strings.HasPrefix(got, "Error 11000 (DuplicateKey): E11000 duplicate key") {
		t.Errorf("text = %q", got)
	}
	if res[0].Err != nil || res[1].Cancelled {
		t.Errorf("first = %+v second cancelled = %v", res[0], res[1].Cancelled)
	}
	if len(fb.execList()) != 2 {
		t.Errorf("%d calls", len(fb.execList()))
	}
}

func TestRunErrorText(t *testing.T) {
	cases := []struct {
		e    RunError
		want string
	}{
		{RunError{Message: "bad"}, "bad"},
		{RunError{Message: "no", Code: 13}, "Error 13: no"},
		{RunError{Message: "no", Code: 13, CodeName: "Unauthorized"}, "Error 13 (Unauthorized): no"},
	}
	for _, tc := range cases {
		if got := tc.e.Text(); got != tc.want {
			t.Errorf("Text = %q, want %q", got, tc.want)
		}
	}
}

func TestATranslationErrorIsTheErrorOfTheStatement(t *testing.T) {
	fb := &fakeBackend{}
	res := Run(context.Background(), fb, itemsOf(t, `db.a.updateOne({}, {x: 1})`), RunOptions{})
	if len(res) != 1 || res[0].Err == nil || res[0].Err.Tag != "MongoRunUpdateNeedsOperators" || res[0].Err.Code != 0 {
		t.Fatalf("res = %+v", res)
	}
	if len(fb.execList()) != 0 {
		t.Error("a statement that cannot be translated was sent")
	}
}

func TestACancelledRun(t *testing.T) {
	fb := &fakeBackend{}
	entered := make(chan struct{})
	ctx, cancel := context.WithCancel(context.Background())
	fb.exec = func(c Call, size int) (Reply, error) {
		close(entered)
		<-ctx.Done()
		return Reply{}, errors.NewErr("MongoReadFailed", ctx.Err())
	}
	go func() {
		<-entered
		cancel()
	}()
	res := Run(ctx, fb, itemsOf(t, "db.a.find()\ndb.a.find()"), RunOptions{})
	if len(res) != 1 || !res[0].Cancelled || res[0].Err == nil || res[0].Err.Text() != "The statement was cancelled." {
		t.Fatalf("res = %+v", res)
	}
	// a run whose context ended before it began does nothing
	if res := Run(ctx, fb, itemsOf(t, "db.a.find()"), RunOptions{}); len(res) != 0 {
		t.Errorf("%d results for a cancelled context", len(res))
	}
}

func TestACursorLeavesTheSetOpenUntilItRunsDry(t *testing.T) {
	cur := &fakeCursor{docs: docsOf(5)}
	fb := &fakeBackend{}
	fb.exec = func(c Call, size int) (Reply, error) { return Reply{Docs: docsOf(2), Cursor: cur}, nil }
	res := Run(context.Background(), fb, itemsOf(t, "db.a.find()"), RunOptions{PageSize: 2})
	set := res[0].Sets[0]
	if !set.More() || len(set.Docs) != 2 || res[0].Message != "2+ documents returned" {
		t.Fatalf("set = %+v message %q", set, res[0].Message)
	}
	var dropped []Cursor
	set.OnDrop = func(c Cursor) { dropped = append(dropped, c) }
	for i := 0; i < 3 && set.More(); i++ {
		docs, done, err := set.Cursor.Next(context.Background(), 2)
		if err != nil {
			t.Fatal(err)
		}
		set.Append(docs, done)
	}
	if set.More() || len(set.Docs) != 7 || len(dropped) != 1 {
		t.Errorf("after the reads: more %v docs %d dropped %d", set.More(), len(set.Docs), len(dropped))
	}
}

func TestASetStopsAtItsLimitAndLetsGoOfItsCursor(t *testing.T) {
	cur := &fakeCursor{docs: docsOf(10)}
	set := &DocSet{Cursor: cur, Docs: docsOf(maxSetDocs - 3), PageSize: 20}
	var dropped int
	set.OnDrop = func(Cursor) { dropped++ }
	docs, done, _ := cur.Next(context.Background(), 5)
	set.Append(docs, done)
	if len(set.Docs) != maxSetDocs || !set.Truncated || set.More() {
		t.Fatalf("docs %d truncated %v more %v", len(set.Docs), set.Truncated, set.More())
	}
	set.Release()
	if dropped != 1 || set.Cursor != nil {
		t.Errorf("dropped %d cursor %v", dropped, set.Cursor)
	}
	for i := 0; i < 200 && !cur.isClosed(); i++ {
		time.Sleep(5 * time.Millisecond)
	}
	if !cur.isClosed() {
		t.Error("the cursor was not closed")
	}
	set.Release() // twice does nothing
	if dropped != 1 {
		t.Errorf("dropped %d times", dropped)
	}
	// a set that ran dry drops its cursor once
	cur2 := &fakeCursor{docs: docsOf(1)}
	s2 := &DocSet{Cursor: cur2}
	n := 0
	s2.OnDrop = func(Cursor) { n++ }
	docs2, done2, _ := cur2.Next(context.Background(), 5)
	s2.Append(docs2, done2)
	if n != 1 || s2.More() {
		t.Errorf("n = %d more %v", n, s2.More())
	}
}

func TestTheTextsOfTheMessages(t *testing.T) {
	counts := []struct {
		n    int
		more bool
		want string
	}{{0, false, "0 documents"}, {1, false, "1 document"}, {1, true, "1+ documents"}, {20, true, "20+ documents"}, {1234567, false, "1,234,567 documents"}}
	for _, tc := range counts {
		if got := documentsText(tc.n, tc.more); got != tc.want {
			t.Errorf("documentsText(%d, %v) = %q, want %q", tc.n, tc.more, got, tc.want)
		}
	}
	writes := []struct {
		w    WriteSummary
		want string
	}{
		{WriteSummary{}, "0 documents changed"},
		{WriteSummary{Inserted: 1}, "1 inserted"},
		{WriteSummary{Matched: 2, Modified: 1}, "2 matched, 1 modified"},
		{WriteSummary{Matched: 1, Upserted: 1}, "1 matched, 1 upserted"},
		{WriteSummary{Deleted: 1500}, "1,500 deleted"},
		{WriteSummary{Inserted: 1, Matched: 2, Modified: 3, Upserted: 4, Deleted: 5}, "1 inserted, 2 matched, 3 modified, 4 upserted, 5 deleted"},
	}
	for _, tc := range writes {
		if got := writeText(tc.w); got != tc.want {
			t.Errorf("writeText(%+v) = %q, want %q", tc.w, got, tc.want)
		}
	}
	times := []struct {
		d    time.Duration
		want string
	}{
		{400 * time.Microsecond, "0.4 ms"}, {12 * time.Millisecond, "12 ms"}, {1250 * time.Millisecond, "1.25 s"}, {125 * time.Second, "2 min 5 s"},
	}
	for _, tc := range times {
		if got := formatElapsed(tc.d); got != tc.want {
			t.Errorf("formatElapsed(%v) = %q, want %q", tc.d, got, tc.want)
		}
	}
	if got := plural(1, "statement"); got != "1 statement" {
		t.Errorf("plural(1) = %q", got)
	}
	if got := plural(2500, "field"); got != "2,500 fields" {
		t.Errorf("plural(2500) = %q", got)
	}
	if got := firstLine("db.a.find({\n  x: 1\n})", 100); got != "db.a.find({ ..." {
		t.Errorf("firstLine = %q", got)
	}
	if got := firstLine(strings.Repeat("é", 150), 10); got != strings.Repeat("é", 10)+" ..." {
		t.Errorf("firstLine long = %q", got)
	}
}

func TestLinesAndColumnsOfAnOffset(t *testing.T) {
	text := "use a\nφ𝒳db.x.find()\nend"
	cases := []struct {
		offset     int
		line, col  int
		backOffset int
	}{
		{0, 1, 1, 0},
		{6, 2, 1, 6},
		{8, 2, 2, 8},              // after φ (2 bytes, 1 UTF-16 unit)
		{12, 2, 4, 12},            // after 𝒳 (4 bytes, 2 UTF-16 units)
		{len(text) - 3, 3, 1, 0},  // start of the last line
		{len(text) + 10, 3, 4, 0}, // past the end
		{-4, 1, 1, 0},             // before the start
	}
	for _, tc := range cases {
		l, c := lineColumn(text, tc.offset)
		if l != tc.line || c != tc.col {
			t.Errorf("lineColumn(%d) = %d:%d, want %d:%d", tc.offset, l, c, tc.line, tc.col)
		}
	}
	if got := offsetOf(text, 1, 2); got != 8 {
		t.Errorf("offsetOf(1, 2) = %d", got)
	}
	if got := offsetOf(text, 2, 99); got != len(text) {
		t.Errorf("offsetOf(2, 99) = %d", got)
	}
	if got := offsetOf(text, 9, 0); got != len(text) {
		t.Errorf("offsetOf(9, 0) = %d", got)
	}
}
