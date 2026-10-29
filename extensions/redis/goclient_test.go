package redis

import (
	"context"
	"reflect"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/rm4n0s/errors"
)

func TestParseKeyspace(t *testing.T) {
	tests := []struct {
		name string
		text string
		want []DBInfo
	}{
		{"empty", "# Keyspace\r\n", nil},
		{"two databases", "# Keyspace\r\ndb0:keys=3,expires=1,avg_ttl=0\r\ndb5:keys=10,expires=0,avg_ttl=0\r\n", []DBInfo{{0, 3, 1}, {5, 10, 0}}},
		{"out of order", "db9:keys=1,expires=0\ndb2:keys=7,expires=7\n", []DBInfo{{2, 7, 7}, {9, 1, 0}}},
		{"a line that is not a database", "db_foo:keys=1\nnonsense\ndbx:keys=2\n", nil},
		{"extra fields and a bad number", "db1:keys=4,expires=oops,avg_ttl=12,subexpiry=0\n", []DBInfo{{1, 4, 0}}},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if got := ParseKeyspace(tc.text); !reflect.DeepEqual(got, tc.want) {
				t.Errorf("ParseKeyspace = %+v, want %+v", got, tc.want)
			}
		})
	}
}

// pagesNode is a nodeScanner that serves pages of keys.
type pagesNode struct {
	pages [][]string
	err   error
	size  int64
}

func (n *pagesNode) scan(_ context.Context, cursor uint64, _ string, _ int64) ([]string, uint64, error) {
	if n.err != nil {
		return nil, 0, n.err
	}
	i := int(cursor)
	next := uint64(i + 1)
	if i+1 >= len(n.pages) {
		next = 0
	}
	return n.pages[i], next, nil
}

func (n *pagesNode) dbsize(context.Context) (int64, error) { return n.size, n.err }

func TestScanNodesWalksEveryMasterOnce(t *testing.T) {
	tests := []struct {
		name  string
		nodes []nodeScanner
		want  []string
		calls int
	}{
		{"one node", []nodeScanner{&pagesNode{pages: [][]string{{"a", "b"}, {"c"}}}}, []string{"a", "b", "c"}, 2},
		{"three nodes", []nodeScanner{
			&pagesNode{pages: [][]string{{"a"}}}, &pagesNode{pages: [][]string{{"b"}, {}, {"c"}}}, &pagesNode{pages: [][]string{{"d"}}}},
			[]string{"a", "b", "c", "d"}, 5},
		{"an empty node in the middle", []nodeScanner{
			&pagesNode{pages: [][]string{{"a"}}}, &pagesNode{pages: [][]string{{}}}, &pagesNode{pages: [][]string{{"z"}}}}, []string{"a", "z"}, 3},
		{"no nodes", nil, nil, 1},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			var got []string
			cur := Cursor{}
			calls := 0
			for !cur.Done() {
				calls++
				if calls > 20 {
					t.Fatal("the scan does not end")
				}
				keys, next, err := scanNodes(context.Background(), tc.nodes, cur, "", 10)
				if err != nil {
					t.Fatal(err)
				}
				got = append(got, keys...)
				cur = next
			}
			if !slices.Equal(got, tc.want) {
				t.Errorf("keys = %q, want %q", got, tc.want)
			}
			if calls != tc.calls {
				t.Errorf("calls = %d, want %d", calls, tc.calls)
			}
		})
	}
}

func TestScanNodesKeepsTheCursorOnAnError(t *testing.T) {
	boom := errors.New("Boom", "boom")
	nodes := []nodeScanner{&pagesNode{pages: [][]string{{"a"}}}, &pagesNode{err: boom}}
	keys, cur, err := scanNodes(context.Background(), nodes, Cursor{Node: 1}, "", 10)
	if err == nil || keys != nil || cur.Node != 1 || cur.Done() {
		t.Errorf("keys %v, cursor %+v, err %v", keys, cur, err)
	}
	// a cursor past the end is over
	if _, cur, err := scanNodes(context.Background(), nodes, Cursor{Node: 7}, "", 10); err != nil || !cur.Done() {
		t.Errorf("past the end: %+v, %v", cur, err)
	}
}

// dial opens the Go client on a RESP server over a fake store.
func dial(t *testing.T, store *fakeServer, password, spwd string) (Server, *respServer) {
	t.Helper()
	srv := newRESPServer(t, store, spwd)
	s, err := GoDialer{}.Dial(context.Background(), Spec{Mode: ModeStandalone, Addrs: []string{srv.addr()}, Password: password, DialTimeout: 2 * time.Second})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := s.Close(); err != nil {
			t.Logf("close: %v", err)
		}
	})
	return s, srv
}

func TestGoClientBrowsesAServer(t *testing.T) {
	ctx := context.Background()
	s, srv := dial(t, fixtureServer(), "", "")
	dbs, err := s.Databases(ctx)
	if err != nil || len(dbs) != 2 || dbs[0].Index != 0 || dbs[0].Keys != 9 || dbs[1].Index != 3 || dbs[1].Expires != 1 {
		t.Fatalf("Databases = %+v, %v", dbs, err)
	}
	db, err := s.DB(0)
	if err != nil {
		t.Fatal(err)
	}
	if n, err := db.DBSize(ctx); err != nil || n != 9 {
		t.Errorf("DBSize = %d, %v", n, err)
	}
	var keys []string
	cur := Cursor{}
	for !cur.Done() {
		got, next, err := db.Scan(ctx, cur, "user:*", 2)
		if err != nil {
			t.Fatal(err)
		}
		keys = append(keys, got...)
		cur = next
	}
	if want := []string{"user:42:profile", "user:42:tags", "user:7:profile"}; !slices.Equal(keys, want) {
		t.Errorf("scanned %q, want %q", keys, want)
	}
	metas, err := db.Metas(ctx, []string{"greeting", "config:app", "missing", "scores"})
	if err != nil {
		t.Fatal(err)
	}
	want := []KeyMeta{{TypeString, -1}, {TypeString, 90_000}, {TypeNone, -2}, {TypeZSet, -1}}
	if !reflect.DeepEqual(metas, want) {
		t.Errorf("Metas = %+v, want %+v", metas, want)
	}
	if got, err := db.Metas(ctx, nil); err != nil || got != nil {
		t.Errorf("Metas(nil) = %v, %v", got, err)
	}
	info, err := db.Info(ctx, "user:42:profile")
	if err != nil || info.Type != TypeHash || info.Size != 3 || info.TTLMillis != -1 || info.Encoding != "listpack" || info.Memory <= 0 {
		t.Errorf("Info = %+v, %v", info, err)
	}
	if info, err := db.Info(ctx, "nope"); err != nil || info.Type != TypeNone {
		t.Errorf("Info(missing) = %+v, %v", info, err)
	}
	for _, c := range srv.commands() {
		if strings.HasPrefix(strings.ToUpper(c), "KEYS") {
			t.Errorf("the client sent %q: the explorer never uses KEYS", c)
		}
	}
	if !slices.ContainsFunc(srv.commands(), func(c string) bool { return strings.HasPrefix(strings.ToUpper(c), "SCAN ") }) {
		t.Error("the client did not use SCAN")
	}
}

func TestGoClientSelectsTheDatabase(t *testing.T) {
	ctx := context.Background()
	s, srv := dial(t, fixtureServer(), "", "")
	db3, err := s.DB(3)
	if err != nil {
		t.Fatal(err)
	}
	keys, _, err := db3.Scan(ctx, Cursor{}, "", 100)
	if err != nil || !slices.Equal(keys, []string{"session:abc", "session:def"}) {
		t.Errorf("db3 keys = %q, %v", keys, err)
	}
	if !slices.Contains(srv.commands(), "select 3") && !slices.Contains(srv.commands(), "SELECT 3") {
		t.Errorf("no SELECT 3 in %q", srv.commands())
	}
	if again, _ := s.DB(3); again != db3 {
		t.Error("the handle of a database is made once")
	}
}

func TestGoClientValues(t *testing.T) {
	ctx := context.Background()
	s, srv := dial(t, fixtureServer(), "", "")
	db, _ := s.DB(0)

	// strings keep the TTL
	if err := db.Set(ctx, "config:app", "changed"); err != nil {
		t.Fatal(err)
	}
	data, size, err := db.GetRange(ctx, "config:app", 4)
	if err != nil || data != "chan" || size != 7 {
		t.Errorf("GetRange = %q, %d, %v", data, size, err)
	}
	if info, _ := db.Info(ctx, "config:app"); info.TTLMillis != 90_000 {
		t.Errorf("TTL after SET = %d, want it kept", info.TTLMillis)
	}

	// hash
	fields, next, err := db.HScan(ctx, "user:42:profile", 0, 10)
	if err != nil || next != 0 || !reflect.DeepEqual(fields, []Pair{{"name", "Ada"}, {"mail", "ada@example.com"}, {"role", "admin"}}) {
		t.Errorf("HScan = %+v, %d, %v", fields, next, err)
	}
	if added, err := db.HSet(ctx, "user:42:profile", "city", "Athens"); err != nil || !added {
		t.Errorf("HSet new = %v, %v", added, err)
	}
	if added, err := db.HSet(ctx, "user:42:profile", "city", "Patras"); err != nil || added {
		t.Errorf("HSet old = %v, %v", added, err)
	}
	if err := db.HDel(ctx, "user:42:profile", "city", "role"); err != nil {
		t.Fatal(err)
	}

	// list
	items, err := db.LRange(ctx, "queue:jobs", 1, 5)
	if err != nil || !slices.Equal(items, []string{"test", "deploy"}) {
		t.Errorf("LRange = %q, %v", items, err)
	}
	if err := db.RPush(ctx, "queue:jobs", "ship"); err != nil {
		t.Fatal(err)
	}
	if err := db.LPush(ctx, "queue:jobs", "plan"); err != nil {
		t.Fatal(err)
	}
	if err := db.LSet(ctx, "queue:jobs", 1, "BUILD"); err != nil {
		t.Fatal(err)
	}
	if err := db.LRemoveAt(ctx, "queue:jobs", 2); err != nil {
		t.Fatal(err)
	}
	items, _ = db.LRange(ctx, "queue:jobs", 0, -1)
	if want := []string{"plan", "BUILD", "deploy", "ship"}; !slices.Equal(items, want) {
		t.Errorf("list = %q, want %q", items, want)
	}

	// set
	members, next, err := db.SScan(ctx, "user:42:tags", 0, 10)
	if err != nil || next != 0 || !slices.Equal(members, []string{"go", "redis", "ui"}) {
		t.Errorf("SScan = %q, %d, %v", members, next, err)
	}
	if added, err := db.SAdd(ctx, "user:42:tags", "tui"); err != nil || !added {
		t.Errorf("SAdd = %v, %v", added, err)
	}
	if err := db.SRem(ctx, "user:42:tags", "go", "ui"); err != nil {
		t.Fatal(err)
	}

	// sorted set
	zs, next, err := db.ZScan(ctx, "scores", 0, 10)
	if err != nil || next != 0 || !reflect.DeepEqual(zs, []ZMember{{"carol", 7.5}, {"alice", 9.25}, {"bob", 12}}) {
		t.Errorf("ZScan = %+v, %d, %v", zs, next, err)
	}
	if added, err := db.ZAdd(ctx, "scores", "dave", 1.5); err != nil || !added {
		t.Errorf("ZAdd = %v, %v", added, err)
	}
	if err := db.ZRem(ctx, "scores", "bob"); err != nil {
		t.Fatal(err)
	}

	// stream: fields keep their order, newest first
	entries, err := db.XRevRange(ctx, "events", "+", 10)
	if err != nil || len(entries) != 2 || entries[0].ID != "1700000000001-0" || entries[0].Fields[0].Field != "type" || entries[0].Fields[1].Field != "user" {
		t.Errorf("XRevRange = %+v, %v", entries, err)
	}
	id, err := db.XAdd(ctx, "events", "*", []Pair{{"type", "tick"}, {"n", "1"}})
	if err != nil || id == "" {
		t.Errorf("XAdd = %q, %v", id, err)
	}
	if err := db.XDel(ctx, "events", "1700000000000-0"); err != nil {
		t.Fatal(err)
	}

	// JSON
	doc, err := db.JSONGet(ctx, "doc:1")
	if err != nil || doc != `{"title":"Hello","tags":["a","b"]}` {
		t.Errorf("JSONGet = %q, %v", doc, err)
	}
	if err := db.JSONSet(ctx, "doc:1", `{"x":1}`); err != nil {
		t.Fatal(err)
	}

	// keys
	if ok, err := db.Exists(ctx, "greeting"); err != nil || !ok {
		t.Errorf("Exists = %v, %v", ok, err)
	}
	if ok, err := db.Rename(ctx, "greeting", "scores"); err != nil || ok {
		t.Errorf("Rename onto a key that exists = %v, %v (RENAMENX refuses)", ok, err)
	}
	if ok, err := db.Rename(ctx, "greeting", "hello"); err != nil || !ok {
		t.Errorf("Rename = %v, %v", ok, err)
	}
	if err := db.Expire(ctx, "hello", 5*time.Second); err != nil {
		t.Fatal(err)
	}
	if info, _ := db.Info(ctx, "hello"); info.TTLMillis != 5000 {
		t.Errorf("TTL = %d", info.TTLMillis)
	}
	err = db.Expire(ctx, "nothing", time.Second)
	wantRoute(t, err, "goDB.Expire.RedisKeyMissing")
	if ok, err := db.Persist(ctx, "hello"); err != nil || !ok {
		t.Errorf("Persist = %v, %v", ok, err)
	}
	if n, err := db.Del(ctx, "hello", "scores", "nothing"); err != nil || n != 2 {
		t.Errorf("Del = %d, %v", n, err)
	}
	for _, c := range srv.commands() {
		if strings.HasPrefix(strings.ToUpper(c), "KEYS") {
			t.Errorf("the client sent %q", c)
		}
	}
}

func TestGoClientAuthentication(t *testing.T) {
	store := fixtureServer()
	srv := newRESPServer(t, store, "s3cret")
	spec := Spec{Mode: ModeStandalone, Addrs: []string{srv.addr()}, DialTimeout: time.Second}
	_, err := GoDialer{}.Dial(context.Background(), spec)
	wantRoute(t, err, "GoDialer.Dial.RedisPingFailed")
	spec.Password = "wrong"
	_, err = GoDialer{}.Dial(context.Background(), spec)
	wantRoute(t, err, "GoDialer.Dial.RedisPingFailed")
	if err != nil && strings.Contains(err.Error(), "wrong") {
		t.Errorf("the error holds the password: %v", err)
	}
	spec.Password = "s3cret"
	s, err := GoDialer{}.Dial(context.Background(), spec)
	if err != nil {
		t.Fatal(err)
	}
	if err := s.Close(); err != nil {
		t.Error(err)
	}
}

func TestGoClientToAnAddressThatAnswersNothing(t *testing.T) {
	_, err := GoDialer{}.Dial(context.Background(), Spec{Mode: ModeStandalone, Addrs: []string{"127.0.0.1:1"}, DialTimeout: 200 * time.Millisecond})
	wantRoute(t, err, "GoDialer.Dial.RedisPingFailed")
}

func TestGoServerLifecycle(t *testing.T) {
	s, _ := dial(t, fixtureServer(), "", "")
	if s.Mode() != ModeStandalone {
		t.Errorf("Mode = %q", s.Mode())
	}
	if err := s.Close(); err != nil {
		t.Fatal(err)
	}
	_, err := s.DB(0)
	wantRoute(t, err, "goServer.DB.RedisServerClosed")
	if err := s.Close(); err != nil {
		t.Errorf("closing twice: %v", err)
	}
}

func TestGoServerCloseReportsAClientThatFails(t *testing.T) {
	s, _ := dial(t, fixtureServer(), "", "")
	db, _ := s.DB(0)
	if err := db.(*goDB).c.Close(); err != nil {
		t.Fatal(err)
	}
	wantRoute(t, s.Close(), "goServer.Close.RedisCloseFailed")
}

func TestAClusterHasOnlyDatabaseZero(t *testing.T) {
	s := &goServer{spec: Spec{Mode: ModeCluster, Addrs: []string{"127.0.0.1:1"}, DialTimeout: 100 * time.Millisecond}, dbs: map[int]*goDB{}}
	t.Cleanup(func() {
		if err := s.Close(); err != nil {
			t.Log(err)
		}
	})
	_, err := s.DB(2)
	wantRoute(t, err, "goServer.DB.RedisClusterDatabaseUnavailable")
	db, err := s.DB(0)
	if err != nil {
		t.Fatal(err)
	}
	// no node answers: the masters cannot be listed
	_, err = db.DBSize(context.Background())
	wantRoute(t, err, "clusterMasters.RedisClusterNodesFailed")
	_, err = s.Databases(context.Background())
	wantRoute(t, err, "RedisClusterNodesFailed")
	_, _, err = db.Scan(context.Background(), Cursor{}, "", 10)
	wantRoute(t, err, "clusterMasters.RedisClusterNodesFailed")
}

// TestEveryCommandOfTheClientNamesItsFailure sends each method to a server that answers every command with an error, and checks the
// route of the error it gets: one tag for each place that can fail.
func TestEveryCommandOfTheClientNamesItsFailure(t *testing.T) {
	ctx := context.Background()
	s, srv := dial(t, fixtureServer(), "", "")
	db, _ := s.DB(0)
	// the server is told to fail only now, after the handshake
	srv.failAll.Store(true)
	tests := []struct {
		name  string
		call  func() error
		route string
	}{
		{"DBSize", func() error { _, err := db.DBSize(ctx); return err }, "goDB.DBSize.RedisDBSizeFailed"},
		{"Scan", func() error { _, _, err := db.Scan(ctx, Cursor{}, "", 10); return err }, "goDB.Scan.RedisScanFailed"},
		{"Metas", func() error { _, err := db.Metas(ctx, []string{"a"}); return err }, "goDB.Metas.RedisMetasFailed"},
		{"Info", func() error { _, err := db.Info(ctx, "a"); return err }, "goDB.Info.RedisInfoFailed"},
		{"GetRange", func() error { _, _, err := db.GetRange(ctx, "a", 10); return err }, "goDB.GetRange.RedisGetFailed"},
		{"Set", func() error { return db.Set(ctx, "a", "b") }, "goDB.Set.RedisSetFailed"},
		{"HScan", func() error { _, _, err := db.HScan(ctx, "a", 0, 10); return err }, "goDB.HScan.RedisHScanFailed"},
		{"HSet", func() error { _, err := db.HSet(ctx, "a", "f", "v"); return err }, "goDB.HSet.RedisHSetFailed"},
		{"HDel", func() error { return db.HDel(ctx, "a", "f") }, "goDB.HDel.RedisHDelFailed"},
		{"LRange", func() error { _, err := db.LRange(ctx, "a", 0, 1); return err }, "goDB.LRange.RedisLRangeFailed"},
		{"LPush", func() error { return db.LPush(ctx, "a", "v") }, "goDB.LPush.RedisLPushFailed"},
		{"RPush", func() error { return db.RPush(ctx, "a", "v") }, "goDB.RPush.RedisRPushFailed"},
		{"LSet", func() error { return db.LSet(ctx, "a", 0, "v") }, "goDB.LSet.RedisLSetFailed"},
		{"LRemoveAt", func() error { return db.LRemoveAt(ctx, "a", 0) }, "goDB.LRemoveAt.RedisLRemoveMarkFailed"},
		{"SScan", func() error { _, _, err := db.SScan(ctx, "a", 0, 10); return err }, "goDB.SScan.RedisSScanFailed"},
		{"SAdd", func() error { _, err := db.SAdd(ctx, "a", "m"); return err }, "goDB.SAdd.RedisSAddFailed"},
		{"SRem", func() error { return db.SRem(ctx, "a", "m") }, "goDB.SRem.RedisSRemFailed"},
		{"ZScan", func() error { _, _, err := db.ZScan(ctx, "a", 0, 10); return err }, "goDB.ZScan.RedisZScanFailed"},
		{"ZAdd", func() error { _, err := db.ZAdd(ctx, "a", "m", 1); return err }, "goDB.ZAdd.RedisZAddFailed"},
		{"ZRem", func() error { return db.ZRem(ctx, "a", "m") }, "goDB.ZRem.RedisZRemFailed"},
		{"XRevRange", func() error { _, err := db.XRevRange(ctx, "a", "+", 10); return err }, "goDB.XRevRange.RedisXRevRangeFailed"},
		{"XAdd", func() error { _, err := db.XAdd(ctx, "a", "*", []Pair{{"f", "v"}}); return err }, "goDB.XAdd.RedisXAddFailed"},
		{"XDel", func() error { return db.XDel(ctx, "a", "1-0") }, "goDB.XDel.RedisXDelFailed"},
		{"JSONGet", func() error { _, err := db.JSONGet(ctx, "a"); return err }, "goDB.JSONGet.RedisJSONGetFailed"},
		{"JSONSet", func() error { return db.JSONSet(ctx, "a", "{}") }, "goDB.JSONSet.RedisJSONSetFailed"},
		{"Exists", func() error { _, err := db.Exists(ctx, "a"); return err }, "goDB.Exists.RedisExistsFailed"},
		{"Rename", func() error { _, err := db.Rename(ctx, "a", "b"); return err }, "goDB.Rename.RedisRenameFailed"},
		{"Expire", func() error { return db.Expire(ctx, "a", time.Second) }, "goDB.Expire.RedisExpireFailed"},
		{"Persist", func() error { _, err := db.Persist(ctx, "a"); return err }, "goDB.Persist.RedisPersistFailed"},
		{"Del", func() error { _, err := db.Del(ctx, "a"); return err }, "goDB.Del.RedisDelFailed"},
		{"Databases", func() error { _, err := s.Databases(ctx); return err }, "goServer.Databases.RedisKeyspaceFailed"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) { wantRoute(t, tc.call(), tc.route) })
	}
}

func TestLRemoveAtNamesTheStepThatFailed(t *testing.T) {
	ctx := context.Background()
	s, srv := dial(t, fixtureServer(), "", "")
	db, _ := s.DB(0)
	// the item is not there: the mark cannot be set
	wantRoute(t, db.LRemoveAt(ctx, "queue:jobs", 9), "goDB.LRemoveAt.RedisLRemoveMarkFailed")
	// the mark is set and cannot be removed
	srv.failCommand("LREM")
	wantRoute(t, db.LRemoveAt(ctx, "queue:jobs", 0), "goDB.LRemoveAt.RedisLRemoveFailed")
}

func TestZScanRefusesAScoreThatIsNotANumber(t *testing.T) {
	store := newFakeServer()
	store.put(0, "z", &fakeKey{typ: TypeZSet, zset: []ZMember{{"m", 1}}})
	s, srv := dial(t, store, "", "")
	db, _ := s.DB(0)
	srv.zscore = func(float64) string { return "not-a-number" }
	_, _, err := db.ZScan(context.Background(), "z", 0, 10)
	wantRoute(t, err, "goDB.ZScan.RedisZScanScoreInvalid")
}

func TestParseEntriesRefusesWhatIsNotAnEntry(t *testing.T) {
	tests := []struct {
		name  string
		reply []interface{}
	}{
		{"not an array", []interface{}{"x"}},
		{"too short", []interface{}{[]interface{}{"1-0"}}},
		{"an id that is not a string", []interface{}{[]interface{}{int64(1), []interface{}{}}}},
		{"fields that are not an array", []interface{}{[]interface{}{"1-0", "nope"}}},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			_, err := parseEntries(tc.reply)
			wantRoute(t, err, "parseEntries.RedisReplyInvalid")
		})
	}
	got, err := parseEntries([]interface{}{[]interface{}{"1-0", nil}, []interface{}{"2-0", []interface{}{"a", "1", "a", "2"}}})
	if err != nil || len(got) != 2 || len(got[0].Fields) != 0 || len(got[1].Fields) != 2 {
		t.Errorf("parseEntries = %+v, %v", got, err)
	}
}

func TestReadOnlyServerRefusesEveryWrite(t *testing.T) {
	ctx := context.Background()
	store := fixtureServer()
	ro := ReadOnly(store)
	db, err := ro.DB(0)
	if err != nil {
		t.Fatal(err)
	}
	tests := []struct {
		name string
		call func() error
	}{
		{"Set", func() error { return db.Set(ctx, "a", "b") }},
		{"HSet", func() error { _, err := db.HSet(ctx, "a", "f", "v"); return err }},
		{"HDel", func() error { return db.HDel(ctx, "a", "f") }},
		{"LPush", func() error { return db.LPush(ctx, "a", "v") }},
		{"RPush", func() error { return db.RPush(ctx, "a", "v") }},
		{"LSet", func() error { return db.LSet(ctx, "a", 0, "v") }},
		{"LRemoveAt", func() error { return db.LRemoveAt(ctx, "a", 0) }},
		{"SAdd", func() error { _, err := db.SAdd(ctx, "a", "m"); return err }},
		{"SRem", func() error { return db.SRem(ctx, "a", "m") }},
		{"ZAdd", func() error { _, err := db.ZAdd(ctx, "a", "m", 1); return err }},
		{"ZRem", func() error { return db.ZRem(ctx, "a", "m") }},
		{"XAdd", func() error { _, err := db.XAdd(ctx, "a", "*", nil); return err }},
		{"XDel", func() error { return db.XDel(ctx, "a", "1-0") }},
		{"JSONSet", func() error { return db.JSONSet(ctx, "a", "{}") }},
		{"Rename", func() error { _, err := db.Rename(ctx, "a", "b"); return err }},
		{"Expire", func() error { return db.Expire(ctx, "a", time.Second) }},
		{"Persist", func() error { _, err := db.Persist(ctx, "a"); return err }},
		{"Del", func() error { _, err := db.Del(ctx, "a"); return err }},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) { wantRoute(t, tc.call(), "readOnlyDB."+tc.name+".RedisReadOnly") })
	}
	for _, c := range store.commands() {
		if !strings.HasPrefix(c, "INFO") {
			t.Errorf("a refused write reached the server: %q", c)
		}
	}
	// reads still work
	if keys, _, err := db.Scan(ctx, Cursor{}, "", 100); err != nil || len(keys) == 0 {
		t.Errorf("Scan = %v, %v", keys, err)
	}
	if _, err := ro.Databases(ctx); err != nil {
		t.Error(err)
	}
	if ro.Mode() != ModeStandalone {
		t.Errorf("Mode = %q", ro.Mode())
	}
	closed := newFakeServer()
	_ = closed.Close()
	if _, err := ReadOnly(closed).DB(0); err == nil {
		t.Error("a closed server gave a database")
	}
}

func TestInfoNamesTheFailureOfTheTTL(t *testing.T) {
	// the type is answered and the TTL is not: the key's header cannot be made
	s, srv := dial(t, fixtureServer(), "", "")
	db, _ := s.DB(0)
	srv.failCommand("PTTL")
	_, err := db.Info(context.Background(), "greeting")
	wantRoute(t, err, "goDB.Info.RedisInfoTTLFailed")
	// MEMORY USAGE and OBJECT ENCODING are not the key's: a server that refuses them still gives the rest
	s2, srv2 := dial(t, fixtureServer(), "", "")
	db2, _ := s2.DB(0)
	srv2.failCommand("MEMORY")
	srv2.failCommand("OBJECT")
	info, err := db2.Info(context.Background(), "greeting")
	if err != nil || info.Type != TypeString || info.Memory != -1 || info.Encoding != "" || info.Size != 11 {
		t.Errorf("Info = %+v, %v", info, err)
	}
}
