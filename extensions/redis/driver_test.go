package redis

import (
	"context"
	"fmt"
	"slices"
	"strconv"
	"strings"
	"testing"

	"github.com/rm4n0s/errors"

	"github.com/rm4n0s/emetos/extensions/internal/dbconn"
)

func TestTheManifestIsValid(t *testing.T) {
	m := (&Extension{}).Manifest()
	if err := extension_CheckManifest(m); err != nil {
		t.Fatal(err)
	}
	if m.ID != "emetos.redis" {
		t.Errorf("id = %q", m.ID)
	}
	if len(m.Contributes.EditorKinds) != 3 || len(m.Contributes.Settings) != 2 || m.Contributes.Settings[0].Key != "redis.scanCount" ||
		m.Contributes.Settings[1].Key != "redis.confirmDangerous" || len(m.Contributes.PanelViews) != 1 {
		t.Errorf("contributes = %+v", m.Contributes)
	}
}

func TestTheDriverIsRegisteredAndItsEngineFits(t *testing.T) {
	r := newRig(t, nil)
	eng, drv, ok := r.hub.Engine(EngineID)
	if !ok || drv == nil || eng.Name != "Redis" || eng.DefaultPort != 6379 || !eng.NoDatabase || len(eng.URLSchemes) != 2 {
		t.Fatalf("engine = %+v, %v", eng, ok)
	}
	// deactivating the extension removes the driver
	r.cancel()
	r.api.WaitFor("the driver to go", func() bool { _, _, ok := r.hub.Engine(EngineID); return !ok })
}

func TestConnectingOpensTheServer(t *testing.T) {
	r := newRig(t, nil, func(c *dbconn.Connection) { c.User = "app"; c.Options[OptDB] = "3" })
	r.connect()
	if r.hub.State(r.conn.ID).Status != dbconn.Connected {
		t.Fatalf("state = %+v", r.hub.State(r.conn.ID))
	}
	if len(r.dial.specs) != 1 || r.dial.specs[0].Username != "app" || r.dial.specs[0].DB != 3 || r.dial.specs[0].Addrs[0] != "redis.local:6379" {
		t.Errorf("specs = %+v", r.dial.specs)
	}
	if !r.ext.Driver().Connected(r.conn.ID) {
		t.Error("the driver has no session")
	}
	if err := r.hub.Disconnect(r.conn.ID); err != nil {
		t.Fatal(err)
	}
	if r.ext.Driver().Connected(r.conn.ID) || !r.store.closed {
		t.Error("Disconnect did not close the server")
	}
}

func TestConnectErrors(t *testing.T) {
	r := newRig(t, nil)
	drv := r.ext.Driver()
	ctx := context.Background()
	// a connection whose settings cannot make a Spec
	bad := r.conn
	bad.Options = map[string]string{OptMode: "primary"}
	wantRoute(t, drv.Connect(ctx, bad, ""), "Driver.Connect->BuildSpec.RedisModeInvalid")
	// a server that does not answer: the error of the dialer reaches the caller as it is
	r.dial.err = errors.New("TestRefused", "connection refused")
	err := drv.Connect(ctx, r.conn, "")
	wantRoute(t, err, "TestRefused")
	r.dial.err = nil
	// a connection that is replaced closes the old server
	if err := drv.Connect(ctx, r.conn, ""); err != nil {
		t.Fatal(err)
	}
	first, _ := drv.session(r.conn.ID)
	if err := drv.Connect(ctx, r.conn, ""); err != nil {
		t.Fatal(err)
	}
	if second, _ := drv.session(r.conn.ID); second == first {
		t.Error("the session was not replaced")
	}
	// Disconnect of a connection that is not open is nothing
	if err := drv.Disconnect(dbconn.Connection{ID: "nope"}); err != nil {
		t.Error(err)
	}
	_, err = drv.session("nope")
	wantRoute(t, err, "Driver.session.RedisNotConnected")
	_, err = drv.Children(ctx, dbconn.Connection{ID: "nope"}, nil)
	wantRoute(t, err, "Driver.Children->Driver.session.RedisNotConnected")
	if _, _, err := drv.database("nope", 0); err == nil {
		t.Error("a database of a connection that is not open")
	}
}

func TestADatabaseOfAClosedServerFails(t *testing.T) {
	r := newRig(t, nil)
	r.connect()
	s, _ := r.ext.Driver().session(r.conn.ID)
	if err := s.server.Close(); err != nil {
		t.Fatal(err)
	}
	if _, err := s.state(5); err == nil {
		t.Error("state of a database of a closed server")
	}
	_, err := r.hub.Children(context.Background(), r.conn.ID, &dbconn.Node{Kind: kindDB, Data: nodeData{Kind: kindDB, DB: 5}})
	if err == nil {
		t.Error("Children of a database of a closed server")
	}
}

func TestTheDatabasesAreListedWithTheirKeyCounts(t *testing.T) {
	r := newRig(t, nil)
	r.connect()
	nodes := r.children(nil)
	if got, want := labels(nodes), []string{"db0", "db3"}; !slices.Equal(got, want) {
		t.Fatalf("databases = %q", got)
	}
	db0, db3 := nodes[0], nodes[1]
	if db0.Description != "9 keys" || db3.Description != "2 keys" || !db0.Expandable || db0.Icon != "database" || db0.Kind != kindDB {
		t.Errorf("db0 %+v db3 %+v", db0, db3)
	}
	if !strings.Contains(db3.Tooltip, "1 with a TTL") || !strings.Contains(db0.Tooltip, "9 keys") {
		t.Errorf("tooltips %q, %q", db0.Tooltip, db3.Tooltip)
	}
	if !r.store.seen("INFO keyspace") {
		t.Error("the databases did not come from INFO keyspace")
	}
}

func TestTheDatabaseTheConnectionSelectsIsAlwaysListed(t *testing.T) {
	store := newFakeServer()
	store.put(5, "a", &fakeKey{typ: TypeString, str: "x"})
	r := newRig(t, store, func(c *dbconn.Connection) { c.Options[OptDB] = "2" })
	r.connect()
	if got, want := labels(r.children(nil)), []string{"db2", "db5"}; !slices.Equal(got, want) {
		t.Errorf("databases = %q, want %q", got, want)
	}
	keys := r.children(nil)[0]
	if keys.Description != "0 keys" {
		t.Errorf("description = %q", keys.Description)
	}
	got := r.children(&keys)
	if len(got) != 1 || got[0].Kind != kindEmpty || got[0].Label != "No keys" {
		t.Errorf("an empty database lists %+v", got)
	}
}

func TestTheKeysFormATreeBySeparator(t *testing.T) {
	r := newRig(t, nil)
	r.connect()
	db0 := r.children(nil)[0]
	top := r.children(&db0)
	if got, want := labels(top), []string{"config", "doc", "queue", "user", "events", "greeting", "scores"}; !slices.Equal(got, want) {
		t.Fatalf("top = %q, want %q", got, want)
	}
	user := find(t, top, "user")
	if !user.Expandable || user.Kind != kindFolder || user.Description != "3" || user.Icon != "folder" || user.ID != "d:user:" {
		t.Errorf("user = %+v", user)
	}
	events := find(t, top, "events")
	if events.Expandable || events.Kind != kindKey || events.Icon != "pulse" || events.ID != "k:events" {
		t.Errorf("events = %+v", events)
	}
	if !strings.Contains(events.Tooltip, "Stream") || !strings.Contains(events.Tooltip, "no expiry") {
		t.Errorf("events tooltip = %q", events.Tooltip)
	}
	below := r.children(&user)
	if got, want := labels(below), []string{"7", "42"}; !slices.Equal(got, want) {
		t.Fatalf("user = %q, want %q", got, want)
	}
	profile := r.children(&below[1])
	if got, want := labels(profile), []string{"profile", "tags"}; !slices.Equal(got, want) {
		t.Fatalf("user:42 = %q", got)
	}
	if profile[0].Icon != "symbol-namespace" || profile[1].Icon != "symbol-enum" {
		t.Errorf("icons = %q, %q", profile[0].Icon, profile[1].Icon)
	}
	cfg := r.children(&top[0])
	if len(cfg) != 1 || !strings.Contains(cfg[0].Tooltip, "TTL 1m 30s") || cfg[0].Icon != "symbol-string" {
		t.Errorf("config = %+v", cfg)
	}
	doc := r.children(&top[1])
	if len(doc) != 1 || doc[0].Icon != "bracket-dot" {
		t.Errorf("doc = %+v", doc)
	}
	// a key and a node that has nothing below it list nothing
	if got := r.children(&events); got != nil {
		t.Errorf("below a key: %+v", got)
	}
	// the explorer never uses KEYS
	for _, c := range r.store.commands() {
		if strings.HasPrefix(strings.ToUpper(c), "KEYS") {
			t.Errorf("command %q", c)
		}
	}
}

func TestAFlatSeparatorListsEveryKey(t *testing.T) {
	r := newRig(t, nil, func(c *dbconn.Connection) { c.Options[OptSeparator] = "none" })
	r.connect()
	db0 := r.children(nil)[0]
	top := r.children(&db0)
	if len(top) != 9 || top[0].Kind != kindKey {
		t.Errorf("top = %q", labels(top))
	}
}

func TestKeysLoadInPagesAndLoadMoreScansOn(t *testing.T) {
	store := newFakeServer()
	for i := 0; i < 1200; i++ {
		store.put(0, fmt.Sprintf("key:%04d", i), &fakeKey{typ: TypeString, str: "v"})
	}
	r := newRig(t, store)
	r.connect()
	db0 := r.children(nil)[0]
	top := r.children(&db0)
	// one folder "key" and the row that loads more
	if got := labels(top); !slices.Equal(got, []string{"key", "Load more keys..."}) {
		t.Fatalf("top = %q", got)
	}
	if top[0].Description != "500" || top[1].Description != "500 of 1,200" || top[1].Kind != kindMore {
		t.Errorf("folder %+v, more %+v", top[0], top[1])
	}
	scans := func() int {
		n := 0
		for _, c := range store.commands() {
			if strings.HasPrefix(c, "SCAN ") {
				n++
			}
		}
		return n
	}
	if scans() != 1 {
		t.Errorf("SCAN calls = %d, want 1", scans())
	}
	// listing again does not scan again
	r.children(&db0)
	r.children(&top[0])
	if scans() != 1 {
		t.Errorf("SCAN calls after listing again = %d", scans())
	}
	var reloads int
	stop := r.hub.SubscribeReload(func(string) { reloads++ })
	defer stop()
	// Open on the "more" row loads the next page, then the rows are listed again
	r.hub.Open(r.conn.ID, top[1])
	r.api.WaitFor("the next page", func() bool { return reloads == 1 })
	top = r.children(&db0)
	if top[0].Description != "1000" || top[1].Description != "1,000 of 1,200" {
		t.Errorf("after one more page: folder %q, more %q", top[0].Description, top[1].Description)
	}
	r.action(&top[1], "Load More")
	r.api.WaitFor("the last page", func() bool { return reloads == 2 })
	top = r.children(&db0)
	if len(top) != 1 || top[0].Description != "1200" {
		t.Errorf("after the last page: %q", labels(top))
	}
	// the keys of a folder are listed from the tree, sorted
	keys := r.children(&top[0])
	if len(keys) != 1200 || keys[0].Label != "0000" || keys[1199].Label != "1199" {
		t.Errorf("keys = %d, %q..%q", len(keys), keys[0].Label, keys[len(keys)-1].Label)
	}
}

func TestAPageStopsAtTheScanBudget(t *testing.T) {
	// no key matches: every SCAN call returns nothing, and the page must still end
	store := newFakeServer()
	for i := 0; i < 400; i++ {
		store.put(0, "other:"+strconv.Itoa(i), &fakeKey{typ: TypeString, str: "v"})
	}
	store.batch = 2 // 200 calls to cover the keyspace
	r := newRig(t, store)
	r.connect()
	if s, err := r.ext.Driver().session(r.conn.ID); err == nil {
		st, _ := s.state(0)
		st.setFilter("zzz")
	}
	db0 := r.children(nil)[0]
	got := r.children(&db0)
	if len(got) != 1 || got[0].Kind != kindMore {
		t.Fatalf("after the budget: %q", labels(got))
	}
	scans := 0
	for _, c := range store.commands() {
		if strings.HasPrefix(c, "SCAN ") {
			scans++
		}
	}
	if scans != maxScanCalls {
		t.Errorf("SCAN calls = %d, want the budget %d", scans, maxScanCalls)
	}
}

func TestTheFilterShowsTheKeysThatMatch(t *testing.T) {
	r := newRig(t, nil)
	r.connect()
	db0 := r.children(nil)[0]
	r.children(&db0)
	var reloads int
	stop := r.hub.SubscribeReload(func(string) { reloads++ })
	defer stop()

	r.action(&db0, "Filter Keys...")
	in := r.api.LastInput()
	if in == nil {
		t.Fatal("no input box")
	}
	in.Accept("user")
	if reloads != 1 {
		t.Fatalf("reloads = %d", reloads)
	}
	db0 = r.children(nil)[0]
	if !strings.Contains(db0.Description, "filter user") {
		t.Errorf("description = %q", db0.Description)
	}
	top := r.children(&db0)
	if got := labels(top); !slices.Equal(got, []string{"user"}) {
		t.Errorf("filtered top = %q", got)
	}
	if top[0].Description != "3" {
		t.Errorf("user count = %q", top[0].Description)
	}
	var sawPattern bool
	for _, c := range r.store.commands() {
		sawPattern = sawPattern || strings.Contains(c, "MATCH *user*")
	}
	if !sawPattern {
		t.Errorf("the filter was not sent as MATCH *user*: %q", r.store.commands())
	}
	// the menu now offers Clear Filter
	if titles := r.actionTitles(&db0); !slices.Contains(titles, "Clear Filter") {
		t.Errorf("menu = %q", titles)
	}
	// a filter that matches nothing says so
	r.ext.setFilter(r.conn.ID, 0, "zzz")
	db0 = r.children(nil)[0]
	got := r.children(&db0)
	if len(got) != 1 || got[0].Kind != kindEmpty || got[0].Label != "No key matches zzz" {
		t.Errorf("no match lists %+v", got)
	}
	r.action(&db0, "Clear Filter")
	db0 = r.children(nil)[0]
	if strings.Contains(db0.Description, "filter") || len(r.children(&db0)) != 7 {
		t.Errorf("after clearing: %q, %d rows", db0.Description, len(r.children(&db0)))
	}
	// an explicit pattern is used as it is
	r.ext.setFilter(r.conn.ID, 0, "user:4*")
	if got := labels(r.children(&db0)); !slices.Equal(got, []string{"user"}) {
		t.Errorf("pattern top = %q", got)
	}
	// the filter dialog starts with the current text; Cancel changes nothing
	r.action(&db0, "Filter Keys...")
	in = r.api.LastInput()
	if in.Options().Value != "user:4*" {
		t.Errorf("the box starts with %q", in.Options().Value)
	}
	in.Cancel()
	if !r.ext.hasFilter(r.conn.ID, 0) {
		t.Error("Cancel cleared the filter")
	}
}

func TestRefreshScansAgain(t *testing.T) {
	r := newRig(t, nil)
	r.connect()
	db0 := r.children(nil)[0]
	if len(r.children(&db0)) != 7 {
		t.Fatal("fixture")
	}
	r.store.put(0, "fresh", &fakeKey{typ: TypeString, str: "new"})
	if len(r.children(&db0)) != 7 {
		t.Error("the keys changed without a refresh: the explorer lists what it scanned")
	}
	r.hub.RefreshDrivers() // what the Refresh command of the view does
	got := labels(r.children(&db0))
	if !slices.Contains(got, "fresh") {
		t.Errorf("after the refresh: %q", got)
	}
	r.store.put(0, "fresher", &fakeKey{typ: TypeString, str: "new"})
	r.action(&db0, "Refresh Keys")
	if !slices.Contains(labels(r.children(&db0)), "fresher") {
		t.Error("Refresh Keys did not scan again")
	}
}

func TestErrorsOfTheServerReachTheView(t *testing.T) {
	r := newRig(t, nil)
	r.connect()
	db0 := r.children(nil)[0]
	r.store.fail["DBSIZE"] = errors.New("TestBoom", "LOADING the dataset")
	_, err := r.hub.Children(context.Background(), r.conn.ID, &db0)
	wantRoute(t, err, "TestBoom")
	// the failed load left nothing half done: the next one works
	if len(r.children(&db0)) != 7 {
		t.Error("the next load did not work")
	}
	r.hub.RefreshDrivers()
	r.store.fail["PIPELINE TYPE PTTL"] = errors.New("TestBoom", "boom")
	_, err = r.hub.Children(context.Background(), r.conn.ID, &db0)
	wantRoute(t, err, "TestBoom")
	st := r.ext.state(r.conn.ID, 0)
	if st.started {
		t.Error("a page whose types could not be read was committed")
	}
	r.store.fail["SCAN"] = errors.New("TestBoom", "boom")
	_, err = r.hub.Children(context.Background(), r.conn.ID, &db0)
	wantRoute(t, err, "TestBoom")
	if len(r.children(&db0)) != 7 {
		t.Error("the load after a failed SCAN did not work")
	}
}

func TestAKeyDeletedDuringTheScanIsLeftOut(t *testing.T) {
	store := fixtureServer()
	r := newRig(t, store)
	r.connect()
	s, _ := r.ext.Driver().session(r.conn.ID)
	st, _ := s.state(0)
	// the scan returns "ghost", which has no type by the time TYPE asks
	inner := st.db
	st.db = ghostDB{inner}
	db0 := r.children(nil)[0]
	got := labels(r.children(&db0))
	if slices.Contains(got, "ghost") || len(got) != 7 {
		t.Errorf("top = %q", got)
	}
}

// ghostDB adds a key to every scan that Metas cannot find.
type ghostDB struct{ DB }

func (g ghostDB) Scan(ctx context.Context, cur Cursor, match string, count int64) ([]string, Cursor, error) {
	keys, next, err := g.DB.Scan(ctx, cur, match, count)
	return append(keys, "ghost"), next, err
}

func TestTheMenuOfEachKindOfNode(t *testing.T) {
	r := newRig(t, nil)
	r.connect()
	db0 := r.children(nil)[0]
	top := r.children(&db0)
	user, events, cfg := find(t, top, "user"), find(t, top, "events"), r.children(&top[0])[0]
	tests := []struct {
		name string
		node *dbconn.Node
		want []string
	}{
		{"a connection", nil, []string{"Open Console", "Open Pub/Sub", "Open Monitor", "Server Information"}},
		{"a database", &db0, []string{"Open Console", "New Key...", "Filter Keys...", "Refresh Keys"}},
		{"a folder", &user, []string{"New Key Here...", "Copy Prefix"}},
		{"a key with no TTL", &events, []string{"Open", "Copy Key", "Rename...", "Set TTL...", "Delete..."}},
		{"a key with a TTL", &cfg, []string{"Open", "Copy Key", "Rename...", "Set TTL...", "Persist", "Delete..."}},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if got := r.actionTitles(tc.node); !slices.Equal(got, tc.want) {
				t.Errorf("menu = %q, want %q", got, tc.want)
			}
		})
	}
	if got := r.actionTitles(&dbconn.Node{Kind: "x"}); got != nil {
		t.Errorf("a node the driver did not make has a menu: %q", got)
	}
	if got := r.actionTitles(&dbconn.Node{Kind: kindEmpty, Data: nodeData{Kind: kindEmpty}}); got != nil {
		t.Errorf("an empty row has a menu: %q", got)
	}
}

func TestAReadOnlyConnectionHasNoMenuEntryThatWrites(t *testing.T) {
	r := newRig(t, nil, func(c *dbconn.Connection) { c.ReadOnly = true })
	r.connect()
	db0 := r.children(nil)[0]
	top := r.children(&db0)
	user, cfg := find(t, top, "user"), r.children(&top[0])[0]
	if got := r.actionTitles(&db0); !slices.Equal(got, []string{"Open Console", "Filter Keys...", "Refresh Keys"}) {
		t.Errorf("database menu = %q", got)
	}
	if got := r.actionTitles(&user); !slices.Equal(got, []string{"Copy Prefix"}) {
		t.Errorf("folder menu = %q", got)
	}
	if got := r.actionTitles(&cfg); !slices.Equal(got, []string{"Open", "Copy Key"}) {
		t.Errorf("key menu = %q", got)
	}
	// and the server is wrapped, so a write that gets past the menu is refused
	db, _, err := r.ext.Driver().database(r.conn.ID, 0)
	if err != nil {
		t.Fatal(err)
	}
	wantRoute(t, db.Set(context.Background(), "x", "y"), "readOnlyDB.Set.RedisReadOnly")
}

func TestCopyKeyAndCopyPrefixUseTheClipboard(t *testing.T) {
	r := newRig(t, nil)
	r.connect()
	db0 := r.children(nil)[0]
	top := r.children(&db0)
	user, events := find(t, top, "user"), find(t, top, "events")
	r.action(&user, "Copy Prefix")
	r.action(&events, "Copy Key")
	if got := r.api.ClipboardTexts(); !slices.Equal(got, []string{"user:", "events"}) {
		t.Errorf("clipboard = %q", got)
	}
	r.api.FailClipboard(errors.New("TestBoom", "no clipboard"))
	r.action(&events, "Copy Key")
	r.waitNote("Cannot copy")
}
