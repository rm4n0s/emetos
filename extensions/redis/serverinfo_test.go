package redis

import (
	"encoding/json"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/rm4n0s/emetos/extensions/internal/fakeapi"
)

func TestParseInfo(t *testing.T) {
	text := "# Server\r\nredis_version:8.2.2\r\nuptime_in_seconds:42\r\n\r\n# Clients\r\nconnected_clients:3\r\n\r\n# Keyspace\r\ndb0:keys=9,expires=1,avg_ttl=0\r\nnoise\r\n"
	got := ParseInfo(text)
	want := InfoTable{Columns: []string{"Section", "Key", "Value"}, Rows: [][]string{
		{"Server", "redis_version", "8.2.2"}, {"Server", "uptime_in_seconds", "42"}, {"Clients", "connected_clients", "3"},
		{"Keyspace", "db0", "keys=9,expires=1,avg_ttl=0"},
	}}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("got %+v", got)
	}
	if got := ParseInfo(""); len(got.Rows) != 0 || len(got.Columns) != 3 {
		t.Errorf("empty = %+v", got)
	}
}

func TestParseSlowLog(t *testing.T) {
	entry := arr(num(14), num(1700000000), num(15500), arr(str("KEYS"), str("user:*"), str("a b")), str("127.0.0.1:50000"), str("app"))
	got := ParseSlowLog(arr(entry, arr(num(1)), arr(num(2), num(1700000100), num(20), arr(str("GET"), str("k")))), time.UTC)
	if len(got.Rows) != 2 {
		t.Fatalf("rows = %+v", got.Rows)
	}
	row := got.Rows[0]
	if row[0] != "14" || row[2] != "15.50 ms" || row[3] != `KEYS user:* "a b"` || row[4] != "127.0.0.1:50000" || row[5] != "app" || row[1] != "2023-11-14 22:13:20" {
		t.Errorf("row = %q", row)
	}
	if got.Rows[1][2] != "20 µs" || got.Rows[1][4] != "" {
		t.Errorf("short entry = %q", got.Rows[1])
	}
	if got := ParseSlowLog(Reply{Kind: ReplyNil}, time.UTC); len(got.Rows) != 0 || len(got.Columns) != 6 {
		t.Errorf("nil = %+v", got)
	}
	for _, tc := range []struct {
		us   int64
		want string
	}{{5, "5 µs"}, {1500, "1.50 ms"}, {2_500_000, "2.50 s"}} {
		if got := formatMicros(tc.us); got != tc.want {
			t.Errorf("formatMicros(%d) = %q", tc.us, got)
		}
	}
}

func TestParseClientList(t *testing.T) {
	text := "id=3 addr=127.0.0.1:50000 laddr=127.0.0.1:6379 name=emetos age=12 idle=0 flags=N db=0 sub=0 cmd=client|list user=default\n" +
		"id=4 addr=127.0.0.1:50002 name= age=900 idle=45 flags=N db=3 sub=1 cmd=subscribe user=default\n\n"
	got := ParseClientList(text)
	wantCols := []string{"id", "addr", "name", "user", "db", "age", "idle", "flags", "cmd", "sub"}
	if !reflect.DeepEqual(got.Columns, wantCols) {
		t.Errorf("columns = %q", got.Columns)
	}
	if len(got.Rows) != 2 || got.Rows[0][0] != "3" || got.Rows[0][2] != "emetos" || got.Rows[1][2] != "" || got.Rows[1][4] != "3" || got.Rows[1][8] != "subscribe" {
		t.Errorf("rows = %q", got.Rows)
	}
	if got := ParseClientList(""); len(got.Rows) != 0 {
		t.Errorf("empty = %+v", got)
	}
}

func TestParseMemoryStatsInBothProtocols(t *testing.T) {
	for _, resp3 := range []bool{false, true} {
		store := fixtureServer()
		store.resp3 = resp3
		db, _ := store.DB(0)
		rep, err := db.Do(t.Context(), []string{"MEMORY", "STATS"})
		if err != nil {
			t.Fatal(err)
		}
		got := ParseMemoryStats(rep)
		want := [][]string{{"peak.allocated", "2097152"}, {"total.allocated", "1048576"}, {"db.0.overhead.hashtable.main", "72"}, {"fragmentation", "1.25"}}
		if !reflect.DeepEqual(got.Rows, want) {
			t.Errorf("resp3=%v: rows = %q, want %q", resp3, got.Rows, want)
		}
	}
}

// infoFakeTab is the fake editor tab of the server information.
func (r *rig) infoFakeTab() *fakeapi.Tab {
	r.t.Helper()
	for _, ft := range r.api.EditorTabs() {
		if ft.Kind() == InfoKindID {
			return ft
		}
	}
	r.t.Fatal("no server information tab")
	return nil
}

// openInfo opens the server information tab of the rig's connection.
func (r *rig) openInfo() *infoTab {
	r.t.Helper()
	r.connect()
	r.api.Pump()
	r.ext.openServerInfo(r.conn.ID)
	var tab *infoTab
	r.api.WaitFor("the information tab", func() bool {
		for _, t := range r.ext.infoTabs {
			if t.loaded && !t.loading {
				tab = t
				return true
			}
		}
		return false
	})
	return tab
}

func TestTheServerInformationTabShowsEachView(t *testing.T) {
	store := fixtureServer()
	store.slow = []Reply{arr(num(1), num(1700000000), num(2500), arr(str("KEYS"), str("*")), str("127.0.0.1:1"), str(""))}
	r := newRig(t, store)
	tab := r.openInfo()
	if tab.view != infoViewInfo || tab.table.Len() != 4 {
		t.Fatalf("view %d, %d rows", tab.view, tab.table.Len())
	}
	for _, tc := range []struct {
		view int
		rows int
		cols int
	}{{infoViewSlow, 1, 6}, {infoViewClients, 2, 12}, {infoViewMemory, 4, 2}, {infoViewInfo, 4, 3}} {
		tab.choose(tc.view)
		r.api.WaitFor("the view", func() bool { return tab.view == tc.view && !tab.loading })
		if tab.table.Len() != tc.rows || len(tab.table.Columns()) != tc.cols {
			t.Errorf("view %d: %d rows, %d columns", tc.view, tab.table.Len(), len(tab.table.Columns()))
		}
	}
	for _, want := range []string{"INFO all", "SLOWLOG GET 128", "CLIENT LIST", "MEMORY STATS"} {
		if !store.seen(want) {
			t.Errorf("%q was not sent: %q", want, store.commands())
		}
	}
}

func TestTheServerInformationTabIsOneForEachConnection(t *testing.T) {
	r := newRig(t, nil)
	r.openInfo()
	r.ext.openServerInfo(r.conn.ID)
	if len(r.ext.infoTabs) != 1 || !r.infoFakeTab().Revealed {
		t.Errorf("%d tabs", len(r.ext.infoTabs))
	}
	if got := r.infoFakeTab().Title(); got != "cache info" {
		t.Errorf("title = %q", got)
	}
	st := r.infoFakeTab().State()
	var s infoState
	if err := json.Unmarshal(st, &s); err != nil || s.Conn != r.conn.ID {
		t.Errorf("state = %s", st)
	}
	r.infoFakeTab().Close()
	if len(r.ext.infoTabs) != 0 {
		t.Error("the tab stays after Close")
	}
}

func TestTheServerInformationTabSaysWhatWentWrong(t *testing.T) {
	r := newRig(t, nil)
	tab := r.openInfo()
	r.store.fail["CLIENT LIST"] = errBoom{}
	tab.choose(infoViewClients)
	r.api.WaitFor("the failure", func() bool { return tab.view == infoViewClients && tab.problem != "" })
	if tab.problem != "boom" {
		t.Errorf("problem = %q", tab.problem)
	}
	// a closed connection is said in the tab and the tab loads again when it opens
	if err := r.hub.Disconnect(r.conn.ID); err != nil {
		t.Fatal(err)
	}
	r.api.Pump()
	tab.connectionChanged()
	if r.ext.drv.Connected(r.conn.ID) {
		t.Fatal("still connected")
	}
}

func TestServerInformationRouteTags(t *testing.T) {
	r := newRig(t, nil)
	tab := r.openInfo()
	// a server that answers a command with an error reply: the view fails with RedisInfoRefused
	r.store.refuse = map[string]string{"CLIENT": "NOPERM this user has no permissions to run the 'client' command"}
	_, err := tab.fetch(t.Context(), infoViewClients)
	wantRoute(t, err, "infoTab.fetch.RedisInfoRefused")
	tab.choose(infoViewClients)
	r.api.WaitFor("the failure", func() bool { return tab.problem != "" })
	if !strings.Contains(tab.problem, "NOPERM") {
		t.Errorf("problem = %q", tab.problem)
	}
	// a bad state
	_, err = r.ext.createInfoTab(nil, nil)
	wantRoute(t, err, "Extension.createInfoTab.RedisInfoStateMissing")
	_, err = r.ext.createInfoTab(nil, json.RawMessage("{"))
	wantRoute(t, err, "Extension.createInfoTab.RedisInfoStateInvalid")
}
