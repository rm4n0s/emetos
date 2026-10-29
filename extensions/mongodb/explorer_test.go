package mongodb

import (
	"context"
	"slices"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/rm4n0s/errors"
	"go.mongodb.org/mongo-driver/v2/bson"

	"github.com/rm4n0s/emetos/extensions/internal/dbconn"
)

func TestTheDatabasesAreListedWithTheirSizes(t *testing.T) {
	r := newRig(t)
	c := r.shop()
	nodes := r.children(c, nil)
	// the databases of the user in order, the server's own after them
	if got, want := labels(nodes), []string{"Archive", "shop", "admin", "config", "local"}; !slices.Equal(got, want) {
		t.Fatalf("databases = %v, want %v", got, want)
	}
	cases := []struct{ label, desc string }{
		{"shop", "12.0 MiB"},
		{"Archive", "empty"},
		{"admin", "40.0 KiB"},
		{"local", "72.0 KiB"},
	}
	for _, tt := range cases {
		n := find(t, nodes, tt.label)
		if n.Description != tt.desc || !n.Expandable || n.Kind != kindDatabase || n.Icon != "database" {
			t.Errorf("%s = %+v", tt.label, n)
		}
	}
}

func TestTheCollectionsOfADatabase(t *testing.T) {
	r := newRig(t)
	c := r.shop()
	shop := find(t, r.children(c, nil), "shop")
	nodes := r.children(c, &shop)
	if got, want := labels(nodes), []string{"customers", "events", "open_orders", "orders", "system.views"}; !slices.Equal(got, want) {
		t.Fatalf("collections = %v, want %v", got, want)
	}
	cases := []struct {
		label, desc, icon string
		data              nodeData
	}{
		{"orders", "123,456 documents", "table", nodeData{Database: "shop", Collection: "orders", CollKind: KindCollection}},
		{"customers", "1 document", "table", nodeData{Database: "shop", Collection: "customers", CollKind: KindCollection}},
		{"events", "time series, 0 documents", "graph", nodeData{Database: "shop", Collection: "events", CollKind: KindTimeSeries}},
		{"open_orders", "view on orders", "eye", nodeData{Database: "shop", Collection: "open_orders", CollKind: KindView}},
	}
	for _, tt := range cases {
		n := find(t, nodes, tt.label)
		if n.Description != tt.desc || n.Icon != tt.icon || !n.Expandable || n.Kind != kindCollection {
			t.Errorf("%s = %+v", tt.label, n)
		}
		if got, _ := n.Data.(nodeData); got != tt.data {
			t.Errorf("%s data = %+v, want %+v", tt.label, got, tt.data)
		}
	}
	// a view has no count: it is not asked
	for _, call := range r.fb.callList() {
		if call == "count shop.open_orders" {
			t.Error("the documents of a view were counted")
		}
	}
}

func TestACountThatFailsIsLeftOut(t *testing.T) {
	r := newRig(t)
	r.fb.countErr = map[string]error{"shop.orders": errors.New("FakeCountFailed", "not authorized")}
	c := r.shop()
	shop := find(t, r.children(c, nil), "shop")
	nodes := r.children(c, &shop)
	if n := find(t, nodes, "orders"); n.Description != "" {
		t.Errorf("orders = %+v", n)
	}
	if n := find(t, nodes, "customers"); n.Description != "1 document" {
		t.Errorf("customers = %+v", n)
	}
}

func TestThereIsNoCountForThousandsOfCollections(t *testing.T) {
	r := newRig(t)
	many := make([]CollectionInfo, countLimit+50)
	for i := range many {
		many[i] = CollectionInfo{Name: "c" + strings.Repeat("0", 4-len(strconv.Itoa(i))) + strconv.Itoa(i), Kind: KindCollection}
	}
	r.fb.colls["big"] = many
	r.fb.dbs = append(r.fb.dbs, DatabaseInfo{Name: "big", SizeOnDisk: 1})
	c := r.shop()
	big := find(t, r.children(c, nil), "big")
	nodes := r.children(c, &big)
	if len(nodes) != countLimit+50 {
		t.Fatalf("%d collections", len(nodes))
	}
	counted := 0
	for _, call := range r.fb.callList() {
		if strings.HasPrefix(call, "count big.") {
			counted++
		}
	}
	if counted != countLimit {
		t.Errorf("%d counts were asked for, want %d", counted, countLimit)
	}
}

func TestListingACancelledDatabaseStops(t *testing.T) {
	r := newRig(t)
	c := r.shop()
	shop := find(t, r.children(c, nil), "shop")
	r.fb.block = make(chan struct{})
	defer close(r.fb.block)
	ctx, cancel := context.WithCancel(context.Background())
	go func() {
		time.Sleep(20 * time.Millisecond)
		cancel()
	}()
	_, err := r.hub.Children(ctx, c.ID, &shop)
	wantRoute(t, err, "collectionNodes.MongoListingCancelled")
}

func TestACollectionHasItsIndexesAndItsSchema(t *testing.T) {
	r := newRig(t)
	c := r.shop()
	shop := find(t, r.children(c, nil), "shop")
	colls := r.children(c, &shop)
	orders := find(t, colls, "orders")
	groups := r.children(c, &orders)
	if got, want := labels(groups), []string{"Indexes", "Schema"}; !slices.Equal(got, want) {
		t.Fatalf("groups = %v", got)
	}
	view := find(t, colls, "open_orders")
	if got, want := labels(r.children(c, &view)), []string{"Schema"}; !slices.Equal(got, want) {
		t.Errorf("a view has %v, want only its schema", got)
	}
}

func TestTheIndexesOfACollection(t *testing.T) {
	r := newRig(t)
	c := r.shop()
	shop := find(t, r.children(c, nil), "shop")
	orders := find(t, r.children(c, &shop), "orders")
	indexes := find(t, r.children(c, &orders), "Indexes")
	nodes := r.children(c, &indexes)
	got := map[string]string{}
	for _, n := range nodes {
		got[n.Label] = n.Description
		if n.Icon != "key" || n.Expandable || n.Kind != kindIndex {
			t.Errorf("%s = %+v", n.Label, n)
		}
	}
	want := map[string]string{
		"_id_":             "{ _id: 1 }",
		"sku_1_created_-1": "{ sku: 1, created: -1 }  unique, sparse",
		"created_ttl":      "{ created: 1 }  TTL 3600 s",
		"text":             `{ _fts: "text", _ftsx: 1 }`,
	}
	for k, v := range want {
		if got[k] != v {
			t.Errorf("index %s = %q, want %q", k, got[k], v)
		}
	}
	// a collection with no index but _id_ lists it; one with none says so
	customers := find(t, r.children(c, &shop), "customers")
	idx := find(t, r.children(c, &customers), "Indexes")
	only := r.children(c, &idx)
	if len(only) != 1 || only[0].Kind != kindNote || only[0].Label != "No indexes" {
		t.Errorf("a collection with no indexes lists %+v", only)
	}
}

func TestTheSchemaIsSampled(t *testing.T) {
	r := newRig(t)
	c := r.shop()
	shop := find(t, r.children(c, nil), "shop")
	orders := find(t, r.children(c, &shop), "orders")
	schema := find(t, r.children(c, &orders), "Schema")
	fields := r.children(c, &schema)
	want := []struct{ label, desc, icon string }{
		{"_id", "objectId", "key"},
		{"sku", "string", "symbol-string"},
		{"qty", "int | long", "symbol-numeric"},
		{"customer", "object", "symbol-structure"},
		{"tags", "array<string>  50%", "symbol-array"},
		{"note", "null | string", "symbol-string"},
		{"lines", "array<object>  50%", "symbol-array"},
	}
	if got := labels(fields); len(got) != len(want) {
		t.Fatalf("fields = %v", got)
	}
	for i, w := range want {
		n := fields[i]
		if n.Label != w.label || n.Description != w.desc || n.Icon != w.icon {
			t.Errorf("field %d = %q %q %q, want %q %q %q", i, n.Label, n.Description, n.Icon, w.label, w.desc, w.icon)
		}
	}
	// the sample size is the setting's, 100 by default
	if got := r.fb.sampleSizes; len(got) != 1 || got[0] != 100 {
		t.Errorf("sample sizes = %v", got)
	}
	// documents inside documents, and inside arrays
	customer := fields[3]
	sub := r.children(c, &customer)
	if got, want := labels(sub), []string{"name", "address"}; !slices.Equal(got, want) {
		t.Fatalf("customer fields = %v", got)
	}
	addr := sub[1]
	if addr.Description != "object  50%" || !addr.Expandable {
		t.Errorf("customer.address = %+v", addr)
	}
	city := r.children(c, &addr)
	if len(city) != 1 || city[0].Label != "city" || city[0].Description != "string" || city[0].Tooltip != "customer.address.city: string (in 1 of 1 documents of the sample)" {
		t.Errorf("customer.address children = %+v", city)
	}
	lines := fields[6]
	items := r.children(c, &lines)
	if got, want := labels(items), []string{"sku", "n"}; !slices.Equal(got, want) {
		t.Errorf("lines fields = %v", got)
	}
}

func TestACollectionWithNoDocumentsHasNoSchema(t *testing.T) {
	r := newRig(t)
	c := r.shop()
	shop := find(t, r.children(c, nil), "shop")
	customers := find(t, r.children(c, &shop), "customers")
	schema := find(t, r.children(c, &customers), "Schema")
	nodes := r.children(c, &schema)
	if len(nodes) != 1 || nodes[0].Kind != kindNote || nodes[0].Label != "No documents to sample" {
		t.Errorf("nodes = %+v", nodes)
	}
}

func TestTheSampleSizeIsTheSetting(t *testing.T) {
	r := newRig(t)
	c := r.shop()
	r.mgAPI.SetSetting(SchemaSampleSizeSetting, float64(7))
	r.mgAPI.Pump()
	shop := find(t, r.children(c, nil), "shop")
	orders := find(t, r.children(c, &shop), "orders")
	schema := find(t, r.children(c, &orders), "Schema")
	r.children(c, &schema)
	if got := r.fb.sampleSizes; len(got) != 1 || got[0] != 7 {
		t.Errorf("sample sizes = %v, want [7]", got)
	}
	for _, bad := range []any{float64(0), float64(20000), "many", nil} {
		r.mgAPI.SetSetting(SchemaSampleSizeSetting, bad)
		r.mgAPI.Pump()
		if got := r.ext.sampleSize(); got != 100 {
			t.Errorf("the setting %v gives a sample of %d, want the default 100", bad, got)
		}
	}
}

func TestTooManyFieldsAreCutWithANote(t *testing.T) {
	r := newRig(t)
	wide := make([]bson.E, schemaMaxFields+20)
	for i := range wide {
		wide[i] = bson.E{Key: "f" + strconv.Itoa(i), Value: int32(i)}
	}
	r.fb.samples["shop.orders"] = []bson.D{wide}
	c := r.shop()
	shop := find(t, r.children(c, nil), "shop")
	orders := find(t, r.children(c, &shop), "orders")
	schema := find(t, r.children(c, &orders), "Schema")
	nodes := r.children(c, &schema)
	if len(nodes) != schemaMaxFields+1 || nodes[len(nodes)-1].Kind != kindNote {
		t.Fatalf("%d nodes, the last %+v", len(nodes), nodes[len(nodes)-1])
	}
	// inside a field, too
	r.fb.samples["shop.orders"] = []bson.D{{{Key: "doc", Value: bson.D(wide)}}}
	nodes = r.children(c, &schema)
	if len(nodes) != 1 || !nodes[0].Expandable {
		t.Fatalf("nodes = %+v", nodes)
	}
	inner := r.children(c, &nodes[0])
	if len(inner) != schemaMaxFields+1 || inner[len(inner)-1].Kind != kindNote {
		t.Errorf("%d inner nodes", len(inner))
	}
}

func TestAnErrorOfTheServerReachesTheView(t *testing.T) {
	r := newRig(t)
	c := r.shop()
	r.fb.err = errors.New("FakeServerDown", "the server went away")
	_, err := r.hub.Children(context.Background(), c.ID, nil)
	// the error of the server comes through as it is: the view shows its message
	if appErr, ok := errors.FromError(err); !ok || appErr.Tag != "FakeServerDown" || !strings.Contains(err.Error(), "the server went away") {
		t.Errorf("err = %v", err)
	}
}

func TestTheNodesOfAConnectionThatIsNotOpen(t *testing.T) {
	r := newRig(t)
	c := r.save(dbconn.Connection{Name: "Shop", Engine: EngineID, Host: "h", Port: 27017})
	d := &driver{e: r.ext}
	_, err := d.Children(context.Background(), c, nil)
	wantRoute(t, err, "driver.Children.MongoNotConnected")
}

func TestFormatBytes(t *testing.T) {
	cases := []struct {
		n    int64
		want string
	}{
		{0, "0 B"}, {1, "1 B"}, {1023, "1023 B"}, {1024, "1.0 KiB"}, {1536, "1.5 KiB"}, {1 << 20, "1.0 MiB"}, {5 << 30, "5.0 GiB"}, {3 << 40, "3.0 TiB"}, {1 << 62, "4096.0 PiB"},
	}
	for _, tt := range cases {
		if got := FormatBytes(tt.n); got != tt.want {
			t.Errorf("FormatBytes(%d) = %q, want %q", tt.n, got, tt.want)
		}
	}
}

func TestFormatCount(t *testing.T) {
	cases := []struct {
		n    int64
		want string
	}{
		{0, "0 documents"}, {1, "1 document"}, {2, "2 documents"}, {999, "999 documents"}, {1000, "1,000 documents"}, {1234567, "1,234,567 documents"},
	}
	for _, tt := range cases {
		if got := FormatCount(tt.n); got != tt.want {
			t.Errorf("FormatCount(%d) = %q, want %q", tt.n, got, tt.want)
		}
	}
}
