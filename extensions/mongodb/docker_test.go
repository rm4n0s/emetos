//go:build emetos_docker

package mongodb

import (
	"context"
	"strings"
	"testing"
	"time"

	"go.mongodb.org/mongo-driver/v2/bson"
	"go.mongodb.org/mongo-driver/v2/mongo"
	"go.mongodb.org/mongo-driver/v2/mongo/options"

	"github.com/rm4n0s/emetos/extension"

	"github.com/rm4n0s/emetos/extensions/internal/containerapi/containerapitest"
	"github.com/rm4n0s/emetos/extensions/internal/dbconn"
	"github.com/rm4n0s/emetos/extensions/internal/dbtest"
)

// Tests against a real server: MongoDB in a throwaway container of a pinned image (containerapitest.ImageMongoDB), made through the
// harness of the container engine client (extensions/internal/containerapi/containerapitest) with the Runner of extensions/sql/dbtest.
// The container is named emetos-test-<random>, carries the label dev.emetos.test=1, publishes its port on 127.0.0.1 only and is
// removed when the test ends. Run them on their own:
//
//	scripts/heavy.sh --alone -- go test -tags emetos_docker ./extensions/mongodb/... -v -count=1

// mongoServer starts a throwaway MongoDB (a labelled container of containerapitest.ImageMongoDB, on 127.0.0.1, with a root user whose
// password is random for this test) and waits until it answers with that password. The container is removed when the test ends. The
// test is skipped when the Docker engine does not answer.
func mongoServer(t *testing.T, runner dbtest.Runner) (conn dbconn.Connection, password string) {
	t.Helper()
	password = dbtest.RandomPassword(t)
	c := runner.Start(t, dbtest.Spec{Image: containerapitest.ImageMongoDB, Port: 27017, Env: map[string]string{
		"MONGO_INITDB_ROOT_USERNAME": "root", "MONGO_INITDB_ROOT_PASSWORD": password,
	}})
	conn = dbconn.Connection{ID: "docker-mongo", Name: "Docker MongoDB", Engine: EngineID, Scope: dbconn.ScopeUser, Host: c.Host, Port: c.Port, User: "root",
		Options: map[string]string{optAuthSource: "admin", optDirect: "true"}}
	// the image starts a server for its init scripts, stops it and starts the real one: the server is ready when it answers twice in a row
	deadline := time.Now().Add(120 * time.Second)
	answered := 0
	for answered < 2 {
		if time.Now().After(deadline) {
			t.Fatal("the MongoDB container did not become ready in two minutes")
		}
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		b, err := driverDialer{timeout: 2 * time.Second}.Dial(ctx, conn, password)
		cancel()
		if err != nil {
			answered = 0
			time.Sleep(time.Second)
			continue
		}
		if err := b.Close(context.Background()); err != nil {
			t.Fatal(err)
		}
		answered++
		time.Sleep(time.Second)
	}
	return conn, password
}

// seed fills the server with a fixture: a collection with documents and indexes, a view, a time series collection and an empty collection.
func seed(t *testing.T, conn dbconn.Connection, password string) {
	t.Helper()
	opts, err := clientOptions(conn, password, 10*time.Second)
	if err != nil {
		t.Fatal(err)
	}
	client, err := mongo.Connect(opts)
	if err != nil {
		t.Fatal(err)
	}
	defer func() {
		if err := client.Disconnect(context.Background()); err != nil {
			t.Error(err)
		}
	}()
	ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
	defer cancel()
	db := client.Database("shop")
	orders := db.Collection("orders")
	var docs []any
	for i := 0; i < 5; i++ {
		d := bson.D{{Key: "sku", Value: "S" + string(rune('A'+i))}, {Key: "qty", Value: int32(i + 1)}, {Key: "created", Value: bson.NewDateTimeFromTime(time.Unix(1700000000+int64(i), 0))},
			{Key: "customer", Value: bson.D{{Key: "name", Value: "Ann"}}}}
		if i%2 == 0 {
			d = append(d, bson.E{Key: "tags", Value: bson.A{"gift", "rush"}})
		}
		docs = append(docs, d)
	}
	if _, err := orders.InsertMany(ctx, docs); err != nil {
		t.Fatal(err)
	}
	if _, err := orders.Indexes().CreateOne(ctx, mongo.IndexModel{Keys: bson.D{{Key: "sku", Value: 1}, {Key: "created", Value: -1}}, Options: options.Index().SetUnique(true)}); err != nil {
		t.Fatal(err)
	}
	if _, err := orders.Indexes().CreateOne(ctx, mongo.IndexModel{Keys: bson.D{{Key: "created", Value: 1}}, Options: options.Index().SetName("created_ttl").SetExpireAfterSeconds(3600)}); err != nil {
		t.Fatal(err)
	}
	if err := db.CreateView(ctx, "big_orders", "orders", mongo.Pipeline{{{Key: "$match", Value: bson.D{{Key: "qty", Value: bson.D{{Key: "$gt", Value: 2}}}}}}}); err != nil {
		t.Fatal(err)
	}
	if err := db.CreateCollection(ctx, "events", options.CreateCollection().SetTimeSeriesOptions(options.TimeSeries().SetTimeField("t"))); err != nil {
		t.Fatal(err)
	}
	if err := db.CreateCollection(ctx, "empty"); err != nil {
		t.Fatal(err)
	}
}

// TestMongoDBInADockerContainer connects to a real server (a labelled container of a pinned image, with a root user) and browses it
// as the explorer does.
func TestMongoDBInADockerContainer(t *testing.T) {
	conn, password := mongoServer(t, dbtest.Docker())
	seed(t, conn, password)

	r := newRig(t)
	r.ext.mgr.dialer = driverDialer{timeout: 10 * time.Second}
	conn.ID = ""
	conn.PasswordEnv = "${env:MONGO_TEST_PASSWORD}"
	r.env["MONGO_TEST_PASSWORD"] = password
	saved := r.save(conn)
	r.connect(saved)

	dbs := r.children(saved, nil)
	shop := find(t, dbs, "shop")
	find(t, dbs, "admin")
	colls := r.children(saved, &shop)
	for _, want := range []struct{ label, desc string }{
		{"orders", "5 documents"}, {"big_orders", "view on orders"}, {"events", "time series, 0 documents"}, {"empty", "0 documents"},
	} {
		if n := find(t, colls, want.label); n.Description != want.desc {
			t.Errorf("%s = %q, want %q", want.label, n.Description, want.desc)
		}
	}
	orders := find(t, colls, "orders")
	groups := r.children(saved, &orders)
	idx := r.children(saved, ptr(find(t, groups, "Indexes")))
	byName := map[string]string{}
	for _, n := range idx {
		byName[n.Label] = n.Description
	}
	if byName["_id_"] != "{ _id: 1 }" || byName["sku_1_created_-1"] != "{ sku: 1, created: -1 }  unique" || byName["created_ttl"] != "{ created: 1 }  TTL 3600 s" {
		t.Errorf("indexes = %v", byName)
	}
	fields := r.children(saved, ptr(find(t, groups, "Schema")))
	want := map[string]string{"_id": "objectId", "sku": "string", "qty": "int", "created": "date", "customer": "object", "tags": "array<string>  60%"}
	for _, f := range fields {
		if w, ok := want[f.Label]; !ok || f.Description != w {
			t.Errorf("field %s = %q, want %q", f.Label, f.Description, w)
		}
		delete(want, f.Label)
	}
	if len(want) != 0 {
		t.Errorf("fields not found: %v", want)
	}
	// a view has its schema from the pipeline
	view := find(t, colls, "big_orders")
	vgroups := r.children(saved, &view)
	if got := labels(vgroups); len(got) != 1 || got[0] != "Schema" {
		t.Fatalf("a view has %v", got)
	}
	vfields := r.children(saved, &vgroups[0])
	if len(vfields) == 0 {
		t.Error("the view has no fields")
	}
	// the empty collection has nothing to sample
	empty := find(t, colls, "empty")
	egroups := r.children(saved, &empty)
	if n := r.children(saved, ptr(find(t, egroups, "Schema"))); len(n) != 1 || n[0].Kind != kindNote {
		t.Errorf("empty = %+v", n)
	}
}

// TestAWrongPasswordIsRefusedByARealServer checks that the server's answer to a wrong password comes to the user and never carries
// the password.
func TestAWrongPasswordIsRefusedByARealServer(t *testing.T) {
	conn, password := mongoServer(t, dbtest.Docker())
	wrong := dbtest.RandomPassword(t)
	_, err := driverDialer{timeout: 5 * time.Second}.Dial(context.Background(), conn, wrong)
	wantRoute(t, err, "driverDialer.Dial.MongoConnectFailed")
	for _, secret := range []string{wrong, password} {
		if strings.Contains(err.Error(), secret) {
			t.Errorf("the error shows a password: %v", err)
		}
	}
	if !strings.Contains(strings.ToLower(err.Error()), "auth") {
		t.Errorf("the message does not say what happened: %v", err)
	}
}

// realPlayRig connects the extension to the real server (seeded) and puts a playground with the script in front, bound to the
// database shop.
func realPlayRig(t *testing.T, script string) *playRig {
	t.Helper()
	conn, password := mongoServer(t, dbtest.Docker())
	seed(t, conn, password)
	r := newRig(t)
	r.ext.mgr.dialer = driverDialer{timeout: 10 * time.Second}
	conn.ID = ""
	conn.PasswordEnv = "${env:MONGO_TEST_PASSWORD}"
	r.env["MONGO_TEST_PASSWORD"] = password
	saved := r.save(conn)
	r.connect(saved)
	r.dbAPI.Pump()
	uri, err := r.mgAPI.Documents().OpenUntitled(LanguageID, script)
	if err != nil {
		t.Fatal(err)
	}
	p := &playRig{rig: r, conn: saved, uri: uri}
	r.ext.pg.bindEditor(uri, binding{Connection: saved.ID, Database: "shop"})
	p.activate(nil)
	return p
}

// TestPlaygroundStatementsAgainstARealServer runs every kind of statement on the real server: reads with chains and pages, writes with
// their answers, commands, show, the error of a duplicate key with its code, and the destructive confirmation.
func TestPlaygroundStatementsAgainstARealServer(t *testing.T) {
	script := `use shop
db.orders.find({qty: {$gte: 2}}, {sku: 1, qty: 1}).sort({qty: -1}).skip(1).limit(2)
db.orders.findOne({sku: "SA"})
db.orders.aggregate([{$group: {_id: null, total: {$sum: "$qty"}}}])
db.orders.countDocuments({qty: {$gt: 1}})
db.orders.estimatedDocumentCount()
db.orders.distinct("sku")
db.orders.insertOne({_id: "extra", sku: "ZZ", qty: 9})
db.orders.insertMany([{_id: "m1", sku: "M1", qty: 1}, {_id: "m2", sku: "M2", qty: 1}])
db.orders.updateOne({_id: "extra"}, {$set: {qty: 10}})
db.orders.updateMany({qty: 1}, {$inc: {qty: 100}})
db.orders.replaceOne({_id: "extra"}, {sku: "ZZ", qty: 11})
db.orders.bulkWrite([{insertOne: {document: {_id: "b1", sku: "B1"}}}, {updateOne: {filter: {_id: "b1"}, update: {$set: {x: 1}}}}, {deleteOne: {filter: {_id: "b1"}}}])
db.orders.createIndex({qty: 1}, {name: "qty_1"})
db.orders.getIndexes()
db.orders.dropIndex("qty_1")
db.orders.deleteOne({_id: "extra"})
db.orders.deleteMany({_id: {$in: ["m1", "m2"]}})
db.runCommand({ping: 1})
db.adminCommand({listDatabases: 1, nameOnly: true})
show dbs
show collections
`
	p := realPlayRig(t, script)
	rec := p.run(RunAllCommand)
	if rec.failed() || len(rec.steps) != 22 {
		for _, st := range rec.steps {
			if st.res.Err != nil {
				t.Errorf("%s: %s", st.res.Item.Text, st.res.Err.Text())
			}
		}
		t.Fatalf("record = %d steps, refused %q", len(rec.steps), rec.refused)
	}
	msg := func(i int) string { return rec.steps[i].res.Message }
	// find with the whole chain: qty >= 2 sorted descending, skip one, limit two: qty 4 and 3 of the 5 (5, 4, 3, 2)
	find := rec.steps[1].res.Sets[0]
	if len(find.Docs) != 2 || fieldOf(find.Docs[0], "qty") != int32(4) || fieldOf(find.Docs[1], "qty") != int32(3) || find.Editable || find.More() {
		t.Errorf("find = %v editable %v more %v", find.Docs, find.Editable, find.More())
	}
	if len(find.Docs[0]) != 3 { // _id, sku, qty
		t.Errorf("the projection gave %v", find.Docs[0])
	}
	if one := rec.steps[2].res.Sets[0]; len(one.Docs) != 1 || fieldOf(one.Docs[0], "sku") != "SA" || !one.Editable {
		t.Errorf("findOne = %v editable %v", one.Docs, one.Editable)
	}
	if agg := rec.steps[3].res.Sets[0]; fieldOf(agg.Docs[0], "total") != int32(15) {
		t.Errorf("aggregate = %v", agg.Docs)
	}
	if got := rec.steps[4].res.Sets[0].Docs[0]; fieldOf(got, "count") != int64(4) {
		t.Errorf("countDocuments = %v", got)
	}
	if got := rec.steps[5].res.Sets[0].Docs[0]; fieldOf(got, "count") != int64(5) {
		t.Errorf("estimatedDocumentCount = %v", got)
	}
	if got := rec.steps[6].res.Sets[0]; len(got.Docs) != 5 {
		t.Errorf("distinct = %v", got.Docs)
	}
	for i, want := range map[int]string{7: "1 inserted", 8: "2 inserted", 9: "1 matched, 1 modified", 10: "3 matched, 3 modified", 11: "1 matched, 1 modified"} {
		if msg(i) != want {
			t.Errorf("step %d = %q, want %q", i, msg(i), want)
		}
	}
	if w := rec.steps[12].res.Write; w == nil || w.Inserted != 1 || w.Matched != 1 || w.Deleted != 1 {
		t.Errorf("bulkWrite = %+v", w)
	}
	if got := rec.steps[13].res.Sets[0].Docs[0]; fieldOf(got, "ok") == nil {
		t.Errorf("createIndex = %v", got)
	}
	var names []string
	for _, d := range rec.steps[14].res.Sets[0].Docs {
		names = append(names, fieldOf(d, "name").(string))
	}
	if strings.Join(names, ",") != "_id_,sku_1_created_-1,created_ttl,qty_1" {
		t.Errorf("getIndexes = %v", names)
	}
	if msg(16) != "1 deleted" || msg(17) != "2 deleted" {
		t.Errorf("deletes = %q %q", msg(16), msg(17))
	}
	if got := rec.steps[19].res.Sets[0].Docs[0]; fieldOf(got, "databases") == nil {
		t.Errorf("adminCommand = %v", got)
	}
	var dbs, colls []string
	for _, d := range rec.steps[20].res.Sets[0].Docs {
		dbs = append(dbs, fieldOf(d, "name").(string))
	}
	for _, d := range rec.steps[21].res.Sets[0].Docs {
		colls = append(colls, fieldOf(d, "name").(string))
	}
	if !strings.Contains(strings.Join(dbs, ","), "shop") || !strings.HasPrefix(strings.Join(colls, ","), "big_orders,empty,events,orders") {
		t.Errorf("show: %v / %v", dbs, colls)
	}
	// the 5 documents of a find with a page of 2 come in pages, and nothing beyond the page is read
	p.mgAPI.SetSetting(PageSizeSetting, 2)
	p.mgAPI.SetDocument(p.uri, "db.orders.find({}).sort({qty: 1})")
	rec = p.run(RunAllCommand)
	set := rec.sets[0].set
	if len(set.Docs) != 2 || !set.More() || !set.Editable {
		t.Fatalf("first page = %d more %v", len(set.Docs), set.More())
	}
	for i := 0; i < 2; i++ {
		p.ext.pg.fetchMore(p.editor(), rec, rec.sets[0])
		want := 4 + (i * 1)
		p.mgAPI.WaitFor("a page", func() bool { return !rec.fetching && len(set.Docs) >= min(want, 5) })
	}
	if len(set.Docs) != 5 || set.More() {
		t.Errorf("after the pages: %d docs, more %v", len(set.Docs), set.More())
	}
	// a duplicate key: the code of the server
	p.mgAPI.SetDocument(p.uri, "db.orders.insertOne({_id: \"dup\"})\ndb.orders.insertOne({_id: \"dup\"})\ndb.orders.find()")
	rec = p.run(RunAllCommand)
	if !rec.failed() || len(rec.steps) != 2 || rec.skipped != 1 {
		t.Fatalf("record = %+v", rec)
	}
	if e := rec.steps[1].res.Err; e.Code != 11000 || e.CodeName != "DuplicateKey" || !strings.HasPrefix(e.Text(), "Error 11000 (DuplicateKey): ") {
		t.Errorf("err = %+v", e)
	}
	// a statement the server refuses by its own rules
	p.mgAPI.SetDocument(p.uri, "db.orders.find({$nope: 1})")
	rec = p.run(RunAllCommand)
	if e := rec.steps[0].res.Err; e == nil || e.Code == 0 || !strings.Contains(e.Message, "$nope") {
		t.Errorf("err = %+v", e)
	}
	// deleteMany({}) asks first, and on a real server nothing is lost when the answer is Cancel
	p.mgAPI.SetDocument(p.uri, "db.empty.insertOne({a: 1})\ndb.empty.deleteMany({})\ndb.empty.find()")
	if err := p.mgAPI.Run(RunAllCommand, nil); err != nil {
		t.Fatal(err)
	}
	pick := p.mgAPI.LastPick()
	if pick == nil || !strings.Contains(pick.Options().Placeholder, "deleteMany({}) can lose data") {
		t.Fatalf("pick = %+v", pick)
	}
	pick.Choose(1)
	// and a drop, then a view of the collections after it
	p.mgAPI.SetDocument(p.uri, "db.empty.drop()\nshow collections")
	if err := p.mgAPI.Run(RunAllCommand, nil); err != nil {
		t.Fatal(err)
	}
	p.mgAPI.LastPick().Choose(0)
	rec = p.wait()
	var left []string
	for _, d := range rec.steps[1].res.Sets[0].Docs {
		left = append(left, fieldOf(d, "name").(string))
	}
	if strings.Contains(strings.Join(left, ","), "empty") || rec.failed() {
		t.Errorf("collections after the drop = %v", left)
	}
}

// TestEditingAndDeletingADocumentOnARealServer opens a document from a find, changes it in the editor, saves it with the confirmation
// of the changed fields, and deletes another.
func TestEditingAndDeletingADocumentOnARealServer(t *testing.T) {
	p := realPlayRig(t, `db.orders.find({sku: "SB"})`)
	rec := p.run(RunCommand)
	if rec.failed() || len(rec.sets[0].set.Docs) != 1 {
		t.Fatalf("record = %+v", rec)
	}
	sr := rec.sets[0]
	sr.selected = 0
	before := sr.set.Docs[0]
	if err := p.mgAPI.Run(EditDocumentCommand, nil); err != nil {
		t.Fatal(err)
	}
	docs := p.mgAPI.UntitledDocuments()
	uri := docs[len(docs)-1].URI
	p.mgAPI.SetActive(&extension.ActiveDocument{URI: uri, LanguageID: "json"})
	p.mgAPI.Emit(extension.TopicEditorActiveChanged, extension.ActiveEditor{URI: uri, LanguageID: "json"})
	p.mgAPI.Pump()
	text, _ := p.mgAPI.DocumentText(uri)
	if !strings.Contains(text, `"sku": "SB"`) || !strings.Contains(text, `"$date"`) {
		t.Fatalf("text = %s", text)
	}
	p.mgAPI.SetDocument(uri, strings.Replace(strings.Replace(text, `"qty": 2`, `"qty": 20`, 1), `"name": "Ann"`, `"name": "Bea"`, 1))
	if err := p.mgAPI.Run(SaveDocumentCommand, nil); err != nil {
		t.Fatal(err)
	}
	pick := p.mgAPI.LastPick()
	if pick == nil || !strings.Contains(pick.Options().Placeholder, "2 fields changed: qty, customer.name") {
		t.Fatalf("pick = %+v", pick)
	}
	pick.Choose(0)
	p.mgAPI.WaitFor("the save", func() bool { return strings.HasPrefix(p.lastNote(), "info: Saved the document") })
	// read it back through the server, not through the results
	p.activate(nil)
	p.mgAPI.SetDocument(p.uri, `db.orders.find({sku: "SB"})`)
	rec = p.run(RunCommand)
	after := rec.sets[0].set.Docs[0]
	if fieldOf(after, "qty") != int32(20) || fieldOf(after, "created") != fieldOf(before, "created") {
		t.Errorf("after = %v", after)
	}
	if c, _ := fieldOf(after, "customer").(bson.D); fieldOf(c, "name") != "Bea" {
		t.Errorf("customer = %v", fieldOf(after, "customer"))
	}
	// delete another document: Cancel keeps it, Delete removes it
	p.mgAPI.SetDocument(p.uri, `db.orders.find({sku: "SC"})`)
	rec = p.run(RunCommand)
	rec.sets[0].selected = 0
	if err := p.mgAPI.Run(DeleteDocumentCommand, nil); err != nil {
		t.Fatal(err)
	}
	p.mgAPI.LastPick().Choose(1)
	p.mgAPI.SetDocument(p.uri, `db.orders.countDocuments({sku: "SC"})`)
	cnt := p.run(RunCommand)
	if cnt.failed() || len(cnt.sets) != 1 {
		t.Fatalf("count = %+v %+v", cnt, cnt.steps[0].res.Err)
	}
	if got := fieldOf(cnt.sets[0].set.Docs[0], "count"); got != int64(1) {
		t.Fatalf("Cancel deleted: count = %v (%v)", got, cnt.sets[0].set.Docs)
	}
	p.mgAPI.SetDocument(p.uri, `db.orders.find({sku: "SC"})`)
	rec = p.run(RunCommand)
	rec.sets[0].selected = 0
	if err := p.mgAPI.Run(DeleteDocumentCommand, nil); err != nil {
		t.Fatal(err)
	}
	p.mgAPI.LastPick().Choose(0)
	p.mgAPI.WaitFor("the delete", func() bool { return strings.HasPrefix(p.lastNote(), "info: Deleted the document") })
	p.mgAPI.SetDocument(p.uri, `db.orders.countDocuments({sku: "SC"})`)
	if got := fieldOf(p.run(RunCommand).sets[0].set.Docs[0], "count"); got != int64(0) {
		t.Errorf("after Delete: count = %v", got)
	}
}

// TestAReadOnlyConnectionOfARealServerWritesNothing refuses the statements that write before anything reaches the server.
func TestAReadOnlyConnectionOfARealServerWritesNothing(t *testing.T) {
	p := realPlayRig(t, "db.orders.insertOne({_id: \"nope\"})")
	c := p.conn
	c.ReadOnly = true
	if _, _, err := p.hub.Save(context.Background(), c, dbconn.PasswordChange{}); err != nil {
		t.Fatal(err)
	}
	if err := p.mgAPI.Run(RunCommand, nil); err != nil {
		t.Fatal(err)
	}
	if got := p.lastNote(); !strings.Contains(got, "is read-only") {
		t.Errorf("note = %q", got)
	}
	p.mgAPI.SetDocument(p.uri, `db.orders.countDocuments({_id: "nope"})`)
	if got := fieldOf(p.run(RunCommand).sets[0].set.Docs[0], "count"); got != int64(0) {
		t.Errorf("count = %v", got)
	}
}
