//go:build emetos_docker

package redis

// Tests against a real server in a throwaway container of the user's Docker (CLAUDE.md "Testing notes"). Run on their own:
//
//	scripts/heavy.sh --alone -- go test -tags emetos_docker ./extensions/redis -run TestDocker -count=1 -v
//
// The containers are made through the harness of step 18.1 (containerapitest, by way of dbtest.Runner): named emetos-test-<random>,
// labelled dev.emetos.test=1, their port published on 127.0.0.1 only, a random password, removed when the test ends, also when it
// fails. The image is one of containerapitest.PinnedImages. The test touches no other container, image, network or volume.
// `docker ps -a --filter label=dev.emetos.test=1` shows nothing after a run.

import (
	"context"
	"net"
	"slices"
	"strconv"
	"strings"
	"testing"
	"time"

	goredis "github.com/redis/go-redis/v9"

	"github.com/rm4n0s/emetos/extensions/internal/containerapi/containerapitest"
	"github.com/rm4n0s/emetos/extensions/internal/dbconn"
	"github.com/rm4n0s/emetos/extensions/internal/dbtest"
)

// dockerRedis starts a Redis with a password and returns its address, the password, and a client of the test that seeds it. The
// runner skips the test when no engine answers.
func dockerRedis(t *testing.T, runner dbtest.Runner) (addr, password string, seed *goredis.Client) {
	t.Helper()
	password = dbtest.RandomPassword(t)
	srv := runner.Start(t, dbtest.Spec{Image: containerapitest.ImageRedis, Port: 6379, Args: []string{"redis-server", "--requirepass", password}})
	addr = net.JoinHostPort(srv.Host, strconv.Itoa(srv.Port))
	seed = goredis.NewClient(&goredis.Options{Addr: addr, Password: password, DisableIdentity: true})
	t.Cleanup(func() {
		if err := seed.Close(); err != nil {
			t.Logf("closing the seed client: %v", err)
		}
	})
	deadline := time.Now().Add(60 * time.Second)
	for {
		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		err := seed.Ping(ctx).Err()
		cancel()
		if err == nil {
			return addr, password, seed
		}
		if time.Now().After(deadline) {
			t.Fatalf("the server did not start: %v", err)
		}
		time.Sleep(200 * time.Millisecond)
	}
}

// seedEveryType puts a key of every type in database 0 and a few in database 3, and a thousand small strings.
func seedEveryType(t *testing.T, c *goredis.Client) {
	t.Helper()
	ctx := context.Background()
	must := func(err error) {
		t.Helper()
		if err != nil {
			t.Fatal(err)
		}
	}
	must(c.Set(ctx, "greeting", "hello world", 0).Err())
	must(c.Set(ctx, "config:app", `{"debug":true,"level":3}`, 90*time.Second).Err())
	must(c.HSet(ctx, "user:42:profile", "name", "Ada", "mail", "ada@example.com", "role", "admin").Err())
	must(c.HSet(ctx, "user:7:profile", "name", "Bob").Err())
	must(c.RPush(ctx, "queue:jobs", "build", "test", "deploy").Err())
	must(c.SAdd(ctx, "user:42:tags", "go", "redis", "ui").Err())
	must(c.ZAdd(ctx, "scores", goredis.Z{Score: 7.5, Member: "carol"}, goredis.Z{Score: 9.25, Member: "alice"}, goredis.Z{Score: 12, Member: "bob"}).Err())
	must(c.XAdd(ctx, &goredis.XAddArgs{Stream: "events", ID: "1700000000000-0", Values: []interface{}{"type", "login", "user", "ada"}}).Err())
	must(c.XAdd(ctx, &goredis.XAddArgs{Stream: "events", ID: "1700000000001-0", Values: []interface{}{"type", "logout", "user", "ada"}}).Err())
	must(c.Do(ctx, "JSON.SET", "doc:1", "$", `{"title":"Hello","tags":["a","b"]}`).Err())
	must(c.Set(ctx, "blob", "\x00\x01binary\xff", 0).Err())
	must(c.Set(ctx, "big", strings.Repeat("x", 2<<20), 0).Err())
	pipe := c.Pipeline()
	for i := 0; i < 1000; i++ {
		pipe.Set(ctx, "bulk:"+strconv.Itoa(i/100)+":"+strconv.Itoa(i), "v", 0)
	}
	_, err := pipe.Exec(ctx)
	must(err)
	other := goredis.NewClient(&goredis.Options{Addr: c.Options().Addr, Password: c.Options().Password, DB: 3, DisableIdentity: true})
	defer func() {
		if err := other.Close(); err != nil {
			t.Logf("closing: %v", err)
		}
	}()
	must(other.Set(ctx, "session:abc", "x", time.Hour).Err())
	must(other.Set(ctx, "session:def", "y", 0).Err())
}

// dockerRig is the extension over the real dialer, with a connection to the container.
func dockerRig(t *testing.T, addr, password string, mods ...func(*dbconn.Connection)) *rig {
	t.Helper()
	host, port, _ := strings.Cut(addr, ":")
	portN, _ := strconv.Atoi(port)
	r := newRig(t, nil, append([]func(*dbconn.Connection){func(c *dbconn.Connection) { c.Host, c.Port = host, portN }}, mods...)...)
	r.ext.dialer = GoDialer{}
	r.ext.drv.dialer = GoDialer{}
	r.password = password
	return r
}

func TestDockerRedisBrowsedAndEdited(t *testing.T) {
	addr, password, seed := dockerRedis(t, dbtest.Docker())
	seedEveryType(t, seed)
	ctx := context.Background()
	r := dockerRig(t, addr, password)
	r.api.SetSetting(SettingScanCount, 100)
	r.connect()

	// the databases and the tree
	dbs := r.children(nil)
	if got := labels(dbs); !slices.Equal(got, []string{"db0", "db3"}) {
		t.Fatalf("databases = %q", got)
	}
	if dbs[0].Description != "1,011 keys" || dbs[1].Description != "2 keys" {
		t.Errorf("counts: %q, %q", dbs[0].Description, dbs[1].Description)
	}
	// the first page is only part of the keys, in the server's order, and ends with the row that loads more
	top := r.children(&dbs[0])
	if !slices.Contains(labels(top), "Load more keys...") || r.ext.state(r.conn.ID, 0).tree.Len() < 100 {
		t.Fatalf("top = %q", labels(top))
	}
	// load every page: the tree ends with all the keys, and the server never saw KEYS
	for i := 0; i < 30 && slices.Contains(labels(r.children(&dbs[0])), "Load more keys..."); i++ {
		before := r.ext.state(r.conn.ID, 0).tree.Len()
		r.ext.loadMore(r.conn.ID, 0)
		r.api.WaitFor("a page of keys", func() bool {
			return r.ext.state(r.conn.ID, 0).tree.Len() > before || !slices.Contains(labels(r.children(&dbs[0])), "Load more keys...")
		})
	}
	if n := r.ext.state(r.conn.ID, 0).tree.Len(); n != 1011 {
		t.Errorf("keys in the tree = %d, want 1011", n)
	}
	top = r.children(&dbs[0])
	if !slices.Contains(labels(top), "user") || !slices.Contains(labels(top), "greeting") || slices.Contains(labels(top), "Load more keys...") {
		t.Errorf("top = %q", labels(top))
	}
	stats, err := seed.Info(ctx, "commandstats").Result()
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(stats, "cmdstat_keys:") {
		t.Error("the explorer used KEYS")
	}
	if !strings.Contains(stats, "cmdstat_scan:") {
		t.Error("the explorer did not use SCAN")
	}
	bulk := find(t, r.children(&dbs[0]), "bulk")
	if bulk.Description != "1000" {
		t.Errorf("bulk = %+v", bulk)
	}
	filtered := r.ext.state(r.conn.ID, 0)
	filtered.setFilter("user:*")
	if got := labels(r.children(&dbs[0])); !slices.Equal(got, []string{"user"}) {
		t.Errorf("filtered = %q", got)
	}
	filtered.setFilter("")

	// every type opens in the editor
	for key, want := range map[string]string{
		"greeting": TypeString, "user:42:profile": TypeHash, "queue:jobs": TypeList, "user:42:tags": TypeSet,
		"scores": TypeZSet, "events": TypeStream, "doc:1": TypeJSON,
	} {
		kt := r.openKeyTab(0, key)
		if kt.info.Type != want || kt.info.Memory <= 0 || kt.info.Encoding == "" {
			t.Errorf("%s: info = %+v", key, kt.info)
		}
		if kt.coll != nil && kt.coll.Len() == 0 {
			t.Errorf("%s: no rows", key)
		}
	}

	// strings keep their TTL, JSON edits, binary and big values are cut or shown as hex
	str := r.openKeyTab(0, "config:app")
	if str.mode != modeJSON || str.info.TTLMillis <= 0 || str.info.TTLMillis > 90_000 {
		t.Fatalf("config: mode %d, TTL %d", str.mode, str.info.TTLMillis)
	}
	str.edit("{\n  \"debug\": false\n}")
	str.saveString()
	r.idle(str)
	if got, _ := seed.Get(ctx, "config:app").Result(); got != `{"debug":false}` {
		t.Errorf("saved %q", got)
	}
	if ttl, _ := seed.PTTL(ctx, "config:app").Result(); ttl <= 0 {
		t.Errorf("the TTL was lost: %v", ttl)
	}
	if blob := r.openKeyTab(0, "blob"); blob.mode != modeHex || !blob.locked() {
		t.Errorf("blob: mode %d locked %v", blob.mode, blob.locked())
	}
	if big := r.openKeyTab(0, "big"); !big.str.Truncated || big.str.Size != 2<<20 || len(big.editor.Value()) != MaxStringBytes {
		t.Errorf("big: truncated %v size %d", big.str.Truncated, big.str.Size)
	}
	doc := r.openKeyTab(0, "doc:1")
	doc.edit("{\n \"title\": \"Changed\"\n}")
	doc.saveString()
	r.idle(doc)
	if got, _ := seed.Do(ctx, "JSON.GET", "doc:1").Text(); got != `{"title":"Changed"}` {
		t.Errorf("JSON saved %q", got)
	}

	// the rows of each collection change on the server
	h := r.openKeyTab(0, "user:42:profile")
	h.openAddForm()
	h.form.ctl[0].SetValue("city")
	h.form.ctl[1].SetValue("Athens")
	h.submitForm()
	r.idle(h)
	selectRow(h, 0)
	h.removeRows()
	r.idle(h)
	if got, _ := seed.HGetAll(ctx, "user:42:profile").Result(); len(got) != 3 || got["city"] != "Athens" || got["name"] != "" {
		t.Errorf("hash = %v", got)
	}
	l := r.openKeyTab(0, "queue:jobs")
	l.openAddForm()
	l.form.ctl[0].SetValue("plan")
	l.form.choice[1] = 1
	l.submitForm()
	r.idle(l)
	selectRow(l, 2)
	l.removeRows()
	r.idle(l)
	if got, _ := seed.LRange(ctx, "queue:jobs", 0, -1).Result(); !slices.Equal(got, []string{"plan", "build", "deploy"}) {
		t.Errorf("list = %q", got)
	}
	s := r.openKeyTab(0, "user:42:tags")
	s.openAddForm()
	s.form.ctl[0].SetValue("tui")
	s.submitForm()
	r.idle(s)
	if n, _ := seed.SCard(ctx, "user:42:tags").Result(); n != 4 {
		t.Errorf("set has %d members", n)
	}
	z := r.openKeyTab(0, "scores")
	z.openAddForm()
	z.form.ctl[0].SetValue("dave")
	z.form.ctl[1].SetValue("1.5")
	z.submitForm()
	r.idle(z)
	if sc, _ := seed.ZScore(ctx, "scores", "dave").Result(); sc != 1.5 {
		t.Errorf("score = %v", sc)
	}
	x := r.openKeyTab(0, "events")
	x.openAddForm()
	x.form.ctl[1].SetValue("type tick\nn 5")
	x.submitForm()
	r.idle(x)
	if n, _ := seed.XLen(ctx, "events").Result(); n != 3 {
		t.Errorf("stream has %d entries", n)
	}

	// rename, TTL, persist, delete
	r.ext.doRename(KeyRef{Conn: r.conn.ID, DB: 0, Key: "greeting"}, "hello")
	r.api.WaitFor("the rename", func() bool { n, _ := seed.Exists(ctx, "hello").Result(); return n == 1 })
	r.ext.setTTL(KeyRef{Conn: r.conn.ID, DB: 0, Key: "hello"})
	r.api.LastInput().Accept("100")
	r.api.WaitFor("the TTL", func() bool { ttl, _ := seed.TTL(ctx, "hello").Result(); return ttl > 0 })
	r.ext.persistKey(KeyRef{Conn: r.conn.ID, DB: 0, Key: "hello"})
	r.api.WaitFor("the persist", func() bool { ttl, _ := seed.TTL(ctx, "hello").Result(); return ttl == -1 })
	r.ext.doDelete(KeyRef{Conn: r.conn.ID, DB: 0, Key: "hello"})
	r.api.WaitFor("the delete", func() bool { n, _ := seed.Exists(ctx, "hello").Result(); return n == 0 })

	// a new key of each type
	db := mustReal(t, r)
	for i, k := range KeyTypes {
		key := "new:" + strconv.Itoa(i)
		values := map[string][]string{TypeString: {"v"}, TypeHash: {"f", "v"}, TypeList: {"i"}, TypeSet: {"m"}, TypeZSet: {"m", "1"}, TypeStream: {"", "a 1"}, TypeJSON: {`{"a":1}`}}[k.Type]
		if err := CreateKey(ctx, db, key, k.Type, values, time.Minute); err != nil {
			t.Errorf("CreateKey %s: %v", k.Type, err)
			continue
		}
		if typ, _ := seed.Type(ctx, key).Result(); typ != k.Type {
			t.Errorf("%s: the server says %q", key, typ)
		}
	}
}

// mustReal returns database 0 of the connection of the rig.
func mustReal(t *testing.T, r *rig) DB {
	t.Helper()
	db, _, err := r.ext.drv.database(r.conn.ID, 0)
	if err != nil {
		t.Fatal(err)
	}
	return db
}

func TestDockerRedisPasswordAndReadOnly(t *testing.T) {
	addr, password, seed := dockerRedis(t, dbtest.Docker())
	seedEveryType(t, seed)
	ctx := context.Background()
	host, port, _ := strings.Cut(addr, ":")
	portN, _ := strconv.Atoi(port)
	spec := Spec{Mode: ModeStandalone, Addrs: []string{addr}, DialTimeout: 5 * time.Second}
	_, err := GoDialer{}.Dial(ctx, spec)
	wantRoute(t, err, "GoDialer.Dial.RedisPingFailed")
	spec.Password = password + "x"
	_, err = GoDialer{}.Dial(ctx, spec)
	wantRoute(t, err, "GoDialer.Dial.RedisPingFailed")
	if err != nil && strings.Contains(err.Error(), password) {
		t.Error("the error holds the password")
	}
	spec.Password, spec.ReadOnly = password, true
	srv, err := GoDialer{}.Dial(ctx, spec)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := srv.Close(); err != nil {
			t.Log(err)
		}
	})
	ro := ReadOnly(srv)
	db, err := ro.DB(0)
	if err != nil {
		t.Fatal(err)
	}
	wantRoute(t, db.Set(ctx, "greeting", "changed"), "readOnlyDB.Set.RedisReadOnly")
	if got, _ := seed.Get(ctx, "greeting").Result(); got != "hello world" {
		t.Errorf("a read-only connection changed a key: %q", got)
	}
	if keys, _, err := db.Scan(ctx, Cursor{}, "greeting", 1000); err != nil || len(keys) != 1 {
		t.Errorf("a read-only connection cannot read: %v, %v", keys, err)
	}
	// the connection form's own Test Connection path: the driver connects and disconnects
	r := dockerRig(t, addr, password, func(c *dbconn.Connection) { c.Host, c.Port = host, portN })
	if err := r.hub.Test(ctx, r.conn, password); err != nil {
		t.Errorf("Test Connection: %v", err)
	}
}
