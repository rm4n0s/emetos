//go:build emetos_docker

package redis

// Step 21.2 against a real server in a throwaway container of the user's Docker, with the rules of docker_test.go (CLAUDE.md "Testing
// notes"): named emetos-test-<random>, labelled dev.emetos.test=1, published on 127.0.0.1 only, a random password, removed when the test
// ends. The commands that change the server (FLUSHDB, CONFIG SET) are sent only to that container. Run on their own:
//
//	scripts/heavy.sh --alone -- go test -tags emetos_docker ./extensions/redis -run TestDockerRedisConsole -count=1 -v

import (
	"context"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/rm4n0s/emetos/extensions/internal/dbconn"
	"github.com/rm4n0s/emetos/extensions/internal/dbtest"
)

func TestDockerRedisConsoleCommandsAndReplies(t *testing.T) {
	addr, password, seed := dockerRedis(t, dbtest.Docker())
	seedEveryType(t, seed)
	ctx := context.Background()
	r := dockerRig(t, addr, password)
	c := r.openConsole()

	tests := []struct {
		line string
		want string // a line that the reply holds
	}{
		{"PING", "PONG"},
		{"GET greeting", `"hello world"`},
		{"GET nothing:here", "(nil)"},
		{`HGETALL user:42:profile`, `2# "name" => "Ada"`}, // RESP3: a map, shown by field name
		{"SMEMBERS user:42:tags", `1~ "go"`},              // RESP3: a set
		{"ZRANGE scores 0 -1 WITHSCORES", `(double) 7.5`},
		{"XRANGE events - +", `"type"`},
		{"JSON.GET doc:1", `\"title\"`}, // a string without a line break is quoted, its quotes escaped
		{"TYPE queue:jobs", "list"},
		{"LRANGE queue:jobs 0 -1", `3) "deploy"`},
		{"INCR hits", "(integer) 1"},
		{"INCR hits", "(integer) 2"},
		{"SET note \"two words\\n\"", "OK"},
		{"GET note", `two words`}, // a string with a line break is shown as it is, like INFO
		{"GET queue:jobs", "(error) WRONGTYPE"},
		{"NOSUCHCOMMAND x", "(error) ERR unknown command"},
		{"INFO server", "redis_version:8."},
		{"CLIENT LIST", "cmd=client|list"},
	}
	for _, tc := range tests {
		r.run(c, tc.line)
		last := c.entries[len(c.entries)-1]
		if !strings.Contains(last.text, tc.want) {
			t.Errorf("%s: reply %q does not contain %q", tc.line, last.text, tc.want)
		}
	}
	if v, _ := seed.Get(ctx, "note").Result(); v != "two words\n" {
		t.Errorf("the quoting sent %q", v)
	}
	// SELECT changes the database of the console
	r.run(c, "SELECT 3")
	r.run(c, "GET session:abc")
	if c.db != 3 || c.entries[len(c.entries)-1].text != `"x"` {
		t.Errorf("db %d, last %+v", c.db, c.entries[len(c.entries)-1])
	}
	r.run(c, "SELECT 99")
	if last := c.entries[len(c.entries)-1]; last.kind != entryError || c.db != 3 {
		t.Errorf("SELECT 99: %+v, db %d", last, c.db)
	}
	// a blocking command waits longer than the read timeout of the client (5 s), and a cancelled one stops
	start := time.Now()
	r.run(c, "BLPOP queue:none 7")
	if last := c.entries[len(c.entries)-1]; last.text != "(nil)" || time.Since(start) < 6500*time.Millisecond {
		t.Errorf("BLPOP: %+v after %v", last, time.Since(start))
	}
	c.submit("BLPOP queue:none 0")
	r.api.WaitFor("the command to be sent", func() bool { return c.inFlight() })
	time.Sleep(300 * time.Millisecond)
	c.cancel()
	r.api.WaitFor("the cancel", func() bool { return !c.busy() })
	r.run(c, "PING")
	if last := c.entries[len(c.entries)-1]; last.text != "PONG" {
		t.Errorf("after the cancel: %+v", last)
	}
	// the events
	if got := published[CommandFinished](t, r, TopicCommandFinished); len(got) < 20 {
		t.Errorf("%d commandFinished events", len(got))
	}
}

func TestDockerRedisConsoleAsksBeforeADangerousCommand(t *testing.T) {
	addr, password, seed := dockerRedis(t, dbtest.Docker())
	seedEveryType(t, seed)
	ctx := context.Background()
	r := dockerRig(t, addr, password)
	c := r.openConsole()
	before, _ := seed.DBSize(ctx).Result()
	c.submit("FLUSHDB")
	r.api.Pump()
	time.Sleep(200 * time.Millisecond)
	if n, _ := seed.DBSize(ctx).Result(); n != before || !c.run.waiting {
		t.Fatalf("FLUSHDB ran before the answer: %d keys of %d", n, before)
	}
	// an answer to another command: it is replaced, never run
	r.run(c, "PING")
	r.api.RunNoteAction(0, "Run")
	r.api.Pump()
	time.Sleep(200 * time.Millisecond)
	if n, _ := seed.DBSize(ctx).Result(); n != before {
		t.Fatalf("a stale confirmation ran FLUSHDB: %d keys of %d", n, before)
	}
	// KEYS asks too
	c.submit("KEYS user:*")
	r.api.Pump()
	if got := r.api.Notes()[len(r.api.Notes())-1]; !strings.Contains(got.Message, "KEYS") {
		t.Errorf("notes = %+v", got)
	}
	c.cancel()
	// confirmed: in database 7 of the container only
	r.run(c, "SELECT 7")
	r.run(c, "SET scratch 1")
	c.submit("FLUSHDB")
	r.api.Pump()
	notes := r.api.Notes()
	r.api.RunNoteAction(len(notes)-1, "Run")
	r.api.WaitFor("FLUSHDB", func() bool { return !c.busy() })
	if n, _ := seed.DBSize(ctx).Result(); n != before {
		t.Errorf("database 0 changed: %d keys of %d", n, before)
	}
	r.run(c, "DBSIZE")
	if last := c.entries[len(c.entries)-1]; last.text != "(integer) 0" {
		t.Errorf("database 7 after FLUSHDB: %+v", last)
	}
}

func TestDockerRedisConsoleOfAReadOnlyConnection(t *testing.T) {
	addr, password, seed := dockerRedis(t, dbtest.Docker())
	seedEveryType(t, seed)
	ctx := context.Background()
	r := dockerRig(t, addr, password, func(c *dbconn.Connection) { c.ReadOnly = true })
	c := r.openConsole()
	for _, line := range []string{"SET k v", "DEL greeting", "HSET h f v", "PUBLISH news x", "FT.SUGADD sug hello 1"} {
		r.run(c, line)
		last := c.entries[len(c.entries)-1]
		if last.kind != entryError {
			t.Errorf("%s: %+v", line, last)
		}
	}
	if n, _ := seed.Exists(ctx, "k", "h").Result(); n != 0 {
		t.Error("a read-only connection wrote")
	}
	if v, _ := seed.Get(ctx, "greeting").Result(); v != "hello world" {
		t.Errorf("greeting = %q", v)
	}
	r.run(c, "GET greeting")
	if last := c.entries[len(c.entries)-1]; last.text != `"hello world"` {
		t.Errorf("a read: %+v", last)
	}
	if n, _ := seed.Exists(ctx, "sug").Result(); n != 0 {
		t.Error("a command the server flags as a write went through")
	}
}

func TestDockerRedisPubSubAndMonitor(t *testing.T) {
	addr, password, seed := dockerRedis(t, dbtest.Docker())
	seedEveryType(t, seed)
	ctx := context.Background()
	r := dockerRig(t, addr, password)
	r.api.SetSetting(SettingConfirmDangerous, false)
	tab := r.openPubSub()
	r.subscribe(tab, "news", false)
	r.subscribe(tab, "sport.*", true)
	if err := seed.Publish(ctx, "news", "from another client").Err(); err != nil {
		t.Fatal(err)
	}
	if err := seed.Publish(ctx, "sport.golf", "birdie").Err(); err != nil {
		t.Fatal(err)
	}
	r.waitMessages(tab, 2)
	first, second := tab.msgs.at(0), tab.msgs.at(1)
	if first.Channel != "news" || first.Payload != "from another client" || second.Channel != "sport.golf" || second.Pattern != "sport.*" {
		t.Errorf("messages = %+v, %+v", first, second)
	}
	tab.publish("news", "from the tab")
	r.api.WaitFor("the publish", func() bool { return strings.HasPrefix(tab.note.Text, "Published to 1 receiver") })
	r.waitMessages(tab, 3)
	tab.unsubscribe(subItem{Name: "news"})
	r.api.WaitFor("the unsubscribe", func() bool { return len(tab.items) == 1 })
	if n, _ := seed.Publish(ctx, "news", "nobody").Result(); n != 0 {
		t.Errorf("news still has %d listeners", n)
	}

	mon := r.openMonitor()
	mon.askStart()
	r.api.WaitFor("the monitor", func() bool { return mon.running() })
	if err := seed.Set(ctx, "watched", "yes", 0).Err(); err != nil {
		t.Fatal(err)
	}
	var line MonitorLine
	r.api.WaitFor("the SET of the other client", func() bool {
		for i := 0; i < mon.lines.len(); i++ {
			if l := mon.lines.at(i); strings.Contains(l.Command, `"watched"`) {
				line = l
				return true
			}
		}
		return false
	})
	if line.DB != "0" || !strings.HasPrefix(line.Command, `"set"`) || line.Time.IsZero() || !strings.Contains(line.Client, ":") {
		t.Errorf("line = %+v", line)
	}
	for i := 0; i < mon.lines.len(); i++ {
		if strings.Contains(mon.lines.at(i).Command, password) {
			t.Error("the password is in the monitor")
		}
	}
	mon.stop()
	if mon.running() {
		t.Error("still running")
	}
}

func TestDockerRedisServerInformation(t *testing.T) {
	addr, password, seed := dockerRedis(t, dbtest.Docker())
	seedEveryType(t, seed)
	ctx := context.Background()
	if err := seed.Do(ctx, "CONFIG", "SET", "slowlog-log-slower-than", "0").Err(); err != nil {
		t.Fatal(err)
	}
	if err := seed.Get(ctx, "greeting").Err(); err != nil {
		t.Fatal(err)
	}
	r := dockerRig(t, addr, password)
	tab := r.openInfo()
	rows := func() []string {
		var out []string
		for i := 0; i < tab.table.Len(); i++ {
			vals := make([]resultgridValue, len(tab.table.Columns()))
			tab.table.Row(i, vals)
			var cells []string
			for _, v := range vals {
				cells = append(cells, v.S)
			}
			out = append(out, strings.Join(cells, "|"))
		}
		return out
	}
	has := func(substr string) bool {
		return slices.ContainsFunc(rows(), func(s string) bool { return strings.Contains(s, substr) })
	}
	if !has("redis_version|8.") || !has("Keyspace|db0|keys=1011") {
		t.Errorf("INFO rows = %q", rows()[:min(5, len(rows()))])
	}
	for view, want := range map[int]string{infoViewSlow: "greeting", infoViewClients: "|default|", infoViewMemory: "total.allocated"} {
		tab.choose(view)
		r.api.WaitFor("the view", func() bool { return tab.view == view && !tab.loading })
		if tab.problem != "" {
			t.Fatalf("view %d: %s", view, tab.problem)
		}
		if !has(want) {
			t.Errorf("view %d: no %q in %q", view, want, rows()[:min(5, len(rows()))])
		}
	}
}

func TestDockerRedisCompletionCommands(t *testing.T) {
	addr, password, _ := dockerRedis(t, dbtest.Docker())
	r := dockerRig(t, addr, password)
	r.connect()
	r.api.Pump()
	r.ext.ensureCommandDocs(r.conn.ID)
	// until the server has answered, the names are the built-in ones, without summaries
	var get CommandDoc
	r.api.WaitFor("the documentation of the server", func() bool {
		for _, d := range r.ext.commandDocs(r.conn.ID) {
			if d.Name == "GET" {
				get = d
			}
		}
		return get.Summary != ""
	})
	if len(r.ext.commandDocs(r.conn.ID)) < 200 {
		t.Errorf("%d commands", len(r.ext.commandDocs(r.conn.ID)))
	}
}
