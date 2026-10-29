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
