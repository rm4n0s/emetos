package mongodb

import (
	"context"
	"strings"
	"testing"

	"go.mongodb.org/mongo-driver/v2/bson"

	"github.com/rm4n0s/emetos/extensions/mongodb/playground"
)

// execText parses one statement, translates it and runs it on the real backend.
func execText(t *testing.T, b Backend, src string, page int) (Reply, error) {
	t.Helper()
	step, err := translateText(t, src, "shop")
	if err != nil {
		t.Fatal(err)
	}
	return b.Exec(context.Background(), *step.Call, page)
}

func wireDocs(n int) []bson.D {
	out := make([]bson.D, n)
	for i := range out {
		out[i] = bson.D{{Key: "_id", Value: int32(i)}, {Key: "v", Value: "x"}}
	}
	return out
}

func lastCall(t *testing.T, s *wireServer, name string) bson.D {
	t.Helper()
	calls := s.calls(name)
	if len(calls) == 0 {
		t.Fatalf("no %s command was sent", name)
	}
	return calls[len(calls)-1]
}

func TestTheRealBackendReadsWithACursorInPages(t *testing.T) {
	s := fixtureWire(t)
	s.finds = map[string][]bson.D{"shop.orders": wireDocs(5)}
	b := dialWire(t, s)
	reply, err := execText(t, b, `db.orders.find({qty: {$gt: 1}}, {v: 1}).sort({v: -1}).skip(1)`, 2)
	if err != nil {
		t.Fatal(err)
	}
	if len(reply.Docs) != 2 || reply.Cursor == nil {
		t.Fatalf("first page = %d docs, cursor %v", len(reply.Docs), reply.Cursor)
	}
	cmd := lastCall(t, s, "find")
	for key, want := range map[string]string{"filter": `{ qty: { $gt: 1 } }`, "projection": `{ v: 1 }`, "sort": `{ v: -1 }`} {
		v, _ := get(cmd, key)
		if got := playground.Format(v); got != want {
			t.Errorf("%s = %s, want %s", key, got, want)
		}
	}
	if v, _ := get(cmd, "batchSize"); v != int32(2) && v != int64(2) {
		t.Errorf("batchSize = %v (the page is what the server sends)", v)
	}
	if v, _ := get(cmd, "skip"); v != int64(1) && v != int32(1) {
		t.Errorf("skip = %v", v)
	}
	docs, done, err := reply.Cursor.Next(context.Background(), 2)
	if err != nil || len(docs) != 2 || done {
		t.Fatalf("second page = %d docs, done %v, %v", len(docs), done, err)
	}
	docs, done, err = reply.Cursor.Next(context.Background(), 2)
	if err != nil || len(docs) != 1 || !done {
		t.Fatalf("third page = %d docs, done %v, %v", len(docs), done, err)
	}
	if len(s.calls("getMore")) != 2 {
		t.Errorf("%d getMore", len(s.calls("getMore")))
	}
	// a cursor that is done stays done, and closes quietly
	if docs, done, err := reply.Cursor.Next(context.Background(), 2); len(docs) != 0 || !done || err != nil {
		t.Errorf("after the end: %d %v %v", len(docs), done, err)
	}
	if err := reply.Cursor.Close(context.Background()); err != nil {
		t.Error(err)
	}
	// a limit that the first page holds leaves no cursor; Close of an open one kills it
	reply, err = execText(t, b, `db.orders.find().limit(3)`, 5)
	if err != nil || len(reply.Docs) != 3 || reply.Cursor != nil {
		t.Fatalf("limit = %d docs cursor %v %v", len(reply.Docs), reply.Cursor, err)
	}
	reply, err = execText(t, b, `db.orders.findOne({})`, 5)
	if err != nil || len(reply.Docs) != 1 {
		t.Fatalf("findOne = %d docs %v", len(reply.Docs), err)
	}
	reply, err = execText(t, b, `db.orders.find()`, 2)
	if err != nil || reply.Cursor == nil {
		t.Fatal(err)
	}
	if err := reply.Cursor.Close(context.Background()); err != nil {
		t.Fatal(err)
	}
	if len(s.calls("killCursors")) == 0 {
		t.Error("an open cursor was closed without killCursors")
	}
	// the aggregation, the index list and the page size zero
	s.finds["shop.orders"] = wireDocs(3)
	reply, err = execText(t, b, `db.orders.aggregate([{$match: {}}], {allowDiskUse: true})`, 0)
	if err != nil || len(reply.Docs) != 3 {
		t.Fatalf("aggregate = %d docs %v", len(reply.Docs), err)
	}
	if v, _ := get(lastCall(t, s, "aggregate"), "allowDiskUse"); v != true {
		t.Errorf("allowDiskUse = %v", v)
	}
	reply, err = execText(t, b, `db.orders.getIndexes()`, 20)
	if err != nil || len(reply.Docs) != 3 {
		t.Fatalf("getIndexes = %d docs %v", len(reply.Docs), err)
	}
}

func TestTheRealBackendWritesAndAnswersLikeTheShell(t *testing.T) {
	s := fixtureWire(t)
	b := dialWire(t, s)
	cases := []struct {
		name, src string
		write     WriteSummary
		fields    string // the document of the reply, keys only
	}{
		{"insertOne", `db.orders.insertOne({sku: "A"})`, WriteSummary{Inserted: 1}, "acknowledged,insertedId"},
		{"insertMany", `db.orders.insertMany([{a: 1}, {a: 2}])`, WriteSummary{Inserted: 2}, "acknowledged,insertedIds"},
		{"updateOne", `db.orders.updateOne({a: 1}, {$set: {b: 2}})`, WriteSummary{Matched: 1, Modified: 1}, "acknowledged,matchedCount,modifiedCount,upsertedCount,upsertedId"},
		{"updateMany with upsert", `db.orders.updateMany({a: 1}, {$set: {b: 2}}, {upsert: true})`, WriteSummary{Matched: 0, Modified: 0, Upserted: 1}, "acknowledged,matchedCount,modifiedCount,upsertedCount,upsertedId"},
		{"replaceOne", `db.orders.replaceOne({_id: 1}, {a: 3})`, WriteSummary{Matched: 1, Modified: 1}, "acknowledged,matchedCount,modifiedCount,upsertedCount,upsertedId"},
		{"deleteOne", `db.orders.deleteOne({_id: 1})`, WriteSummary{Deleted: 3}, "acknowledged,deletedCount"},
		{"deleteMany", `db.orders.deleteMany({a: 1})`, WriteSummary{Deleted: 3}, "acknowledged,deletedCount"},
		{"bulkWrite", `db.orders.bulkWrite([{insertOne: {document: {a: 1}}}, {deleteOne: {filter: {a: 1}}}, {updateMany: {filter: {}, update: {$inc: {n: 1}}}}, {replaceOne: {filter: {a: 1}, replacement: {a: 2}}}, {updateOne: {filter: {a: 1}, update: {$set: {a: 1}}}}, {deleteMany: {filter: {a: 2}}}], {ordered: false})`,
			WriteSummary{}, "acknowledged,insertedCount,matchedCount,modifiedCount,deletedCount,upsertedCount,upsertedIds"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			reply, err := execText(t, b, tc.src, 20)
			if err != nil {
				t.Fatal(err)
			}
			if len(reply.Docs) != 1 || reply.Write == nil {
				t.Fatalf("reply = %+v", reply)
			}
			var keys []string
			for _, e := range reply.Docs[0] {
				keys = append(keys, e.Key)
			}
			if strings.Join(keys, ",") != tc.fields {
				t.Errorf("fields = %v", keys)
			}
			if tc.name != "bulkWrite" && *reply.Write != tc.write {
				t.Errorf("write = %+v, want %+v", *reply.Write, tc.write)
			}
		})
	}
	// what was sent
	ins := s.calls("insert")[1] // insertMany
	if v, _ := get(ins, "ordered"); v != true {
		t.Errorf("ordered = %v", v)
	}
	if _, err := execText(t, b, `db.orders.insertMany([{a: 1}], {ordered: false})`, 20); err != nil {
		t.Fatal(err)
	}
	if v, _ := get(lastCall(t, s, "insert"), "ordered"); v != false {
		t.Errorf("ordered = %v, want false", v)
	}
	docs, _ := get(lastCall(t, s, "insert"), "documents")
	if a, _ := docs.(bson.A); len(a) != 1 || a[0].(bson.D)[0].Key != "_id" {
		t.Errorf("the driver did not give the document an _id: %v", docs)
	}
	if _, err := execText(t, b, `db.orders.updateMany({a: 1}, {$set: {b: 2}}, {upsert: true, hint: "a_1", collation: {locale: "en"}, arrayFilters: [{"x.a": 1}], comment: "c", bypassDocumentValidation: true, let: {v: 1}})`, 20); err != nil {
		t.Fatal(err)
	}
	up := lastCall(t, s, "update")
	updates, _ := get(up, "updates")
	u := updates.(bson.A)[0].(bson.D)
	for _, key := range []string{"q", "u", "multi", "upsert", "hint", "collation", "arrayFilters"} {
		if _, found := get(u, key); !found {
			t.Errorf("the update has no %s: %v", key, u)
		}
	}
	if v, _ := get(u, "multi"); v != true {
		t.Errorf("multi = %v", v)
	}
	dels, _ := get(lastCall(t, s, "delete"), "deletes")
	if l, _ := get(dels.(bson.A)[0].(bson.D), "limit"); l != int32(0) && l != int64(0) {
		t.Errorf("deleteMany limit = %v", l)
	}
}

func TestTheRealBackendRunsTheReadsAndTheCommands(t *testing.T) {
	s := fixtureWire(t)
	s.distinct = bson.A{"a", "b"}
	b := dialWire(t, s)
	reply, err := execText(t, b, `db.orders.countDocuments({a: 1}, {limit: 5, skip: 1})`, 20)
	if err != nil || len(reply.Docs) != 1 || reply.Docs[0][0].Key != "count" {
		t.Fatalf("countDocuments = %+v %v", reply, err)
	}
	if pl, _ := get(lastCall(t, s, "aggregate"), "pipeline"); pl == nil {
		t.Error("countDocuments did not aggregate")
	}
	reply, err = execText(t, b, `db.orders.estimatedDocumentCount()`, 20)
	if err != nil || reply.Docs[0][0].Value != int64(123456) {
		t.Fatalf("estimated = %+v %v", reply, err)
	}
	reply, err = execText(t, b, `db.orders.distinct("sku", {a: 1})`, 20)
	if err != nil || len(reply.Docs) != 2 || reply.Docs[1][0].Key != "value" || reply.Docs[1][0].Value != "b" {
		t.Fatalf("distinct = %+v %v", reply, err)
	}
	reply, err = execText(t, b, `db.runCommand({ping: 1})`, 20)
	if err != nil || len(reply.Docs) != 1 {
		t.Fatalf("ping = %+v %v", reply, err)
	}
	for _, src := range []string{`db.orders.createIndex({a: 1}, {unique: true})`, `db.orders.dropIndex("a_1")`, `db.orders.drop()`} {
		if _, err := execText(t, b, src, 20); err != nil {
			t.Fatalf("%s: %v", src, err)
		}
	}
	ci, _ := get(lastCall(t, s, "createIndexes"), "indexes")
	spec := ci.(bson.A)[0].(bson.D)
	if name, _ := get(spec, "name"); name != "a_1" {
		t.Errorf("the index is named %v", name)
	}
	if u, _ := get(spec, "unique"); u != true {
		t.Errorf("unique = %v", u)
	}
	reply, err = execText(t, b, `show dbs`, 20)
	if err != nil || len(reply.Docs) != 2 || reply.Docs[0][0].Value != "old" || reply.Docs[1][0].Value != "shop" {
		t.Fatalf("show dbs = %+v %v", reply, err)
	}
	reply, err = execText(t, b, `show collections`, 20)
	if err != nil || len(reply.Docs) != 3 || reply.Docs[0][0].Value != "events" || reply.Docs[1][1].Value != "view" {
		t.Fatalf("show collections = %+v %v", reply, err)
	}
	// a Call the backend does not know
	_, err = b.Exec(context.Background(), Call{Kind: "mystery", Database: "shop", Collection: "c"}, 20)
	wantRoute(t, err, "driverBackend.write.MongoCallUnsupported")
	_, err = b.Exec(context.Background(), Call{Kind: CallBulkWrite, Database: "shop", Collection: "c", Ops: []BulkOp{{Kind: "mystery"}}}, 20)
	wantRoute(t, err, "bulkModel.MongoCallUnsupported")
	_, err = b.Exec(context.Background(), Call{Kind: CallFind, Database: "shop", Collection: "c", Options: CallOptions{Collation: bson.D{{Key: "strength", Value: "x"}}}}, 20)
	_ = err
	_, err = b.Exec(context.Background(), Call{Kind: CallInsertOne, Database: "shop", Collection: "c", Docs: []bson.D{{}}, Options: CallOptions{Collation: bson.D{{Key: "strength", Value: "x"}}}}, 20)
	wantRoute(t, err, "collationOf.MongoCollationInvalid")
}

func TestTheRealBackendSaysWhyTheServerRefused(t *testing.T) {
	s := fixtureWire(t)
	b := dialWire(t, s)
	cases := []struct {
		name, src, command, route string
	}{
		{"find", `db.orders.find()`, "find", "driverBackend.find.MongoReadFailed"},
		{"aggregate", `db.orders.aggregate([{$match: {}}])`, "aggregate", "driverBackend.Exec.MongoReadFailed"},
		{"getIndexes", `db.orders.getIndexes()`, "listIndexes", "driverBackend.Exec.MongoReadFailed"},
		{"countDocuments", `db.orders.countDocuments({})`, "aggregate", "driverBackend.Exec.MongoReadFailed"},
		{"estimatedDocumentCount", `db.orders.estimatedDocumentCount()`, "count", "driverBackend.Exec.MongoReadFailed"},
		{"distinct", `db.orders.distinct("a")`, "distinct", "driverBackend.Exec.MongoReadFailed"},
		{"a command", `db.runCommand({ping: 1})`, "ping", "driverBackend.Exec.MongoCommandFailed"},
		{"insertOne", `db.orders.insertOne({a: 1})`, "insert", "driverBackend.write.MongoWriteFailed"},
		{"insertMany", `db.orders.insertMany([{a: 1}])`, "insert", "driverBackend.write.MongoWriteFailed"},
		{"updateOne", `db.orders.updateOne({}, {$set: {a: 1}})`, "update", "driverBackend.write.MongoWriteFailed"},
		{"updateMany", `db.orders.updateMany({}, {$set: {a: 1}})`, "update", "driverBackend.write.MongoWriteFailed"},
		{"replaceOne", `db.orders.replaceOne({}, {a: 1})`, "update", "driverBackend.write.MongoWriteFailed"},
		{"deleteOne", `db.orders.deleteOne({})`, "delete", "driverBackend.write.MongoWriteFailed"},
		{"deleteMany", `db.orders.deleteMany({})`, "delete", "driverBackend.write.MongoWriteFailed"},
		{"bulkWrite", `db.orders.bulkWrite([{deleteOne: {filter: {}}}])`, "delete", "driverBackend.write.MongoWriteFailed"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			s.mu.Lock()
			s.failList = tc.command
			s.mu.Unlock()
			_, err := execText(t, b, tc.src, 20)
			if err == nil {
				t.Fatal("no error")
			}
			wantRoute(t, err, tc.route)
			if code, name := ServerError(err); code != 13 || name != "Unauthorized" {
				t.Errorf("code %d name %q: %v", code, name, err)
			}
			if !strings.Contains(err.Error(), "not authorized") {
				t.Errorf("message = %q", err)
			}
		})
	}
	s.mu.Lock()
	s.failList = ""
	s.mu.Unlock()
	// the code of an error that is not the server's is zero
	if code, name := ServerError(context.Canceled); code != 0 || name != "" {
		t.Errorf("code %d %q", code, name)
	}
	// a failure of getMore ends the cursor
	s.finds = map[string][]bson.D{"shop.orders": wireDocs(5)}
	reply, err := execText(t, b, `db.orders.find()`, 2)
	if err != nil || reply.Cursor == nil {
		t.Fatal(err)
	}
	s.mu.Lock()
	s.failList = "getMore"
	s.mu.Unlock()
	_, done, err := reply.Cursor.Next(context.Background(), 2)
	if err == nil || !done {
		t.Fatalf("done %v err %v", done, err)
	}
	wantRoute(t, err, "driverCursor.Next.MongoCursorFailed")
	if code, _ := ServerError(err); code != 13 {
		t.Errorf("code = %d", code)
	}
}

func TestTheWholeOfARunOverTheWire(t *testing.T) {
	s := fixtureWire(t)
	s.finds = map[string][]bson.D{"shop.orders": wireDocs(30)}
	b := dialWire(t, s)
	res := Run(context.Background(), b, itemsOf(t, "use shop\ndb.orders.find()\ndb.orders.insertOne({a: 1})\ndb.orders.estimatedDocumentCount()"), RunOptions{PageSize: 20})
	if len(res) != 4 || res[1].Err != nil || len(res[1].Sets) != 1 || len(res[1].Sets[0].Docs) != 20 || !res[1].Sets[0].More() {
		t.Fatalf("results = %+v", res)
	}
	if res[2].Message != "1 inserted" || res[3].Err != nil {
		t.Errorf("results = %+v %+v", res[2], res[3])
	}
	set := res[1].Sets[0]
	docs, done, err := set.Cursor.Next(context.Background(), 20)
	if err != nil || len(docs) != 10 || !done {
		t.Fatalf("second page = %d %v %v", len(docs), done, err)
	}
}

func TestTheNamesOfTheCodesOfWriteErrors(t *testing.T) {
	raw, err := bson.Marshal(bson.D{{Key: "codeName", Value: "FromServer"}})
	if err != nil {
		t.Fatal(err)
	}
	cases := []struct {
		code int
		raw  bson.Raw
		want string
	}{{11000, nil, "DuplicateKey"}, {121, nil, "DocumentValidationFailure"}, {99999, nil, ""}, {11000, raw, "FromServer"}}
	for _, tc := range cases {
		if got := nameOfWrite(tc.code, tc.raw); got != tc.want {
			t.Errorf("nameOfWrite(%d) = %q, want %q", tc.code, got, tc.want)
		}
	}
}

func TestAnOpenCursorThatCannotBeClosed(t *testing.T) {
	s := fixtureWire(t)
	s.finds = map[string][]bson.D{"shop.orders": wireDocs(5)}
	b := dialWire(t, s)
	reply, err := execText(t, b, `db.orders.find()`, 2)
	if err != nil || reply.Cursor == nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	err = reply.Cursor.Close(ctx)
	wantRoute(t, err, "driverCursor.Close.MongoCursorCloseFailed")
	// a second close does nothing
	if err := reply.Cursor.Close(context.Background()); err != nil {
		t.Error(err)
	}
}
