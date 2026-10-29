package redis

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/rm4n0s/emetos/extension"
	"github.com/rm4n0s/emetos/extensions/internal/dbconn"
	"github.com/rm4n0s/emetos/extensions/internal/fakeapi"
)

// openConsole connects the rig's connection and returns its console, as the Open Console command makes it.
func (r *rig) openConsole() *console {
	r.t.Helper()
	r.connect()
	r.api.Pump()
	r.ext.setTarget(r.conn.ID)
	return r.ext.consoleFor(r.conn.ID)
}

// run submits a command line and waits until the console is idle again.
func (r *rig) run(c *console, line string) {
	r.t.Helper()
	c.submit(line)
	r.api.WaitFor("the command "+line, func() bool { return !c.busy() })
}

// texts are the entries of the console as "kind text".
func (c *console) texts() []string {
	out := make([]string, len(c.entries))
	for i, e := range c.entries {
		out[i] = [...]string{"cmd", "reply", "error", "note"}[e.kind] + " " + e.text
	}
	return out
}

func joined(c *console) string { return strings.Join(c.texts(), "\n") }

func TestTheConsoleShowsTheReplyOfACommand(t *testing.T) {
	tests := []struct {
		name  string
		resp3 bool
		line  string
		want  string
	}{
		{"a status", false, "PING", "cmd db0> PING\nreply PONG"},
		{"a string", false, "GET greeting", "cmd db0> GET greeting\nreply \"hello world\""},
		{"a missing key", false, "GET nothing", "cmd db0> GET nothing\nreply (nil)"},
		{"an integer", false, "DBSIZE", "cmd db0> DBSIZE\nreply (integer) 9"},
		{"quotes make one argument", false, `ECHO "a  b"`, "cmd db0> ECHO \"a  b\"\nreply \"a  b\""},
		{"a hash in RESP2 is a list", false, "HGETALL user:7:profile", "cmd db0> HGETALL user:7:profile\nreply 1) \"name\"\n2) \"Bob\""},
		{"a hash in RESP3 is a map", true, "HGETALL user:42:profile", "cmd db0> HGETALL user:42:profile\nreply 1# \"name\" => \"Ada\"\n2# \"mail\" => \"ada@example.com\"\n3# \"role\" => \"admin\""},
		{"a set in RESP3 is a set", true, "SMEMBERS user:42:tags", "cmd db0> SMEMBERS user:42:tags\nreply 1~ \"go\"\n2~ \"redis\"\n3~ \"ui\""},
		{"a set in RESP2 is a list", false, "SMEMBERS user:42:tags", "cmd db0> SMEMBERS user:42:tags\nreply 1) \"go\"\n2) \"redis\"\n3) \"ui\""},
		{"an empty array", false, "LRANGE nolist 0 -1", "cmd db0> LRANGE nolist 0 -1\nreply (empty array)"},
		{"an error of the server is red", false, "GET user:7:profile", "cmd db0> GET user:7:profile\nerror (error) WRONGTYPE Operation against a key holding the wrong kind of value"},
		{"an unknown command", false, "NOPE", "cmd db0> NOPE\nerror (error) ERR unknown command 'NOPE'"},
		{"INFO is text", false, "INFO server", "cmd db0> INFO server\nreply # Server\nredis_version:8.2.2\nredis_mode:standalone\n\n# Clients\nconnected_clients:3\n\n# Keyspace\ndb0:keys=9,expires=1,avg_ttl=0"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			store := fixtureServer()
			store.resp3 = tc.resp3
			r := newRig(t, store)
			c := r.openConsole()
			r.run(c, tc.line)
			if got := joined(c); got != tc.want {
				t.Errorf("console =\n%s\nwant\n%s", got, tc.want)
			}
		})
	}
}

func TestTheConsoleSendsWhatTheQuotingSays(t *testing.T) {
	r := newRig(t, nil)
	c := r.openConsole()
	r.run(c, `SET k "a b\tc"`)
	if !r.store.seen("SET k a b\tc") {
		t.Errorf("commands = %q", r.store.commands())
	}
	r.run(c, `SET k "open`)
	if got := c.entries[len(c.entries)-1]; got.kind != entryError || !strings.Contains(got.text, "double quote is not closed") {
		t.Errorf("last entry = %+v", got)
	}
	r.run(c, `GET "a"b`)
	if got := c.entries[len(c.entries)-1]; got.kind != entryError || !strings.Contains(got.text, "closing quote") {
		t.Errorf("last entry = %+v", got)
	}
	if r.store.seen("GET") {
		t.Errorf("a line that cannot be read was sent: %q", r.store.commands())
	}
}

func TestSelectChangesTheDatabaseOfTheConsole(t *testing.T) {
	r := newRig(t, nil)
	c := r.openConsole()
	r.run(c, "SELECT 3")
	if c.db != 3 || c.prompt() != "db3> " {
		t.Fatalf("db = %d", c.db)
	}
	r.run(c, "GET session:abc")
	if got := c.entries[len(c.entries)-1]; got.text != `"x"` {
		t.Errorf("reply = %+v", got)
	}
	for _, line := range []string{"SELECT", "SELECT x", "SELECT -1"} {
		before := c.db
		r.run(c, line)
		if got := c.entries[len(c.entries)-1]; got.kind != entryError || c.db != before {
			t.Errorf("%q: %+v, db %d", line, got, c.db)
		}
	}
}

func TestTheConsoleDoesNotSendWhatNeedsAConnectionOfItsOwn(t *testing.T) {
	r := newRig(t, nil)
	c := r.openConsole()
	for _, line := range []string{"SUBSCRIBE news", "MONITOR", "MULTI", "HELLO 3", "AUTH x"} {
		r.run(c, line)
		if got := c.entries[len(c.entries)-1]; got.kind != entryError {
			t.Errorf("%q: %+v", line, got)
		}
	}
	for _, n := range []string{"SUBSCRIBE", "MONITOR", "MULTI", "HELLO", "AUTH"} {
		if r.store.seen(n) {
			t.Errorf("%s was sent", n)
		}
	}
}

func TestADangerousCommandAsksFirstAndRunsOnlyWhenTheUserSaysRun(t *testing.T) {
	r := newRig(t, nil)
	c := r.openConsole()
	c.submit("FLUSHALL")
	r.api.Pump()
	if !c.busy() || !c.run.waiting {
		t.Fatalf("the console does not wait: run = %+v", c.run)
	}
	notes := r.api.Notes()
	if len(notes) != 1 || notes[0].Severity != "warn" || !strings.Contains(notes[0].Message, "FLUSHALL") || len(notes[0].Actions) != 1 || notes[0].Actions[0] != "Run" {
		t.Fatalf("notes = %+v", notes)
	}
	if r.store.seen("FLUSHALL") {
		t.Fatal("FLUSHALL was sent before the user answered")
	}
	r.api.RunNoteAction(0, "Run")
	r.api.WaitFor("FLUSHALL", func() bool { return !c.busy() })
	if !r.store.seen("FLUSHALL") || len(r.store.dbs[0].keys) != 0 {
		t.Errorf("commands = %q", r.store.commands())
	}
	if got := c.entries[len(c.entries)-1]; got.text != "OK" {
		t.Errorf("last entry = %+v", got)
	}
}

func TestADangerousCommandThatIsNotConfirmedIsNeverRun(t *testing.T) {
	t.Run("another command replaces it", func(t *testing.T) {
		r := newRig(t, nil)
		c := r.openConsole()
		c.submit("FLUSHDB")
		r.api.Pump()
		r.run(c, "PING")
		if !strings.Contains(joined(c), "note (not run: FLUSHDB)") {
			t.Errorf("console =\n%s", joined(c))
		}
		// the old notification is stale: pressing Run now runs nothing
		r.api.RunNoteAction(0, "Run")
		r.api.Pump()
		if r.store.seen("FLUSHDB") {
			t.Error("FLUSHDB was sent after it had been replaced")
		}
		if last := r.api.Notes(); last[len(last)-1].Message != "That command is no longer waiting." {
			t.Errorf("notes = %+v", last)
		}
	})
	t.Run("Cancel drops it", func(t *testing.T) {
		r := newRig(t, nil)
		c := r.openConsole()
		c.submit("SHUTDOWN")
		r.api.Pump()
		c.cancel()
		if c.busy() {
			t.Fatal("still busy")
		}
		r.api.RunNoteAction(0, "Run")
		r.api.Pump()
		if r.store.seen("SHUTDOWN") {
			t.Error("SHUTDOWN was sent after Cancel")
		}
	})
	t.Run("pressing Run twice runs it once", func(t *testing.T) {
		r := newRig(t, nil)
		c := r.openConsole()
		c.submit("CONFIG SET maxmemory 1")
		r.api.Pump()
		r.api.RunNoteAction(0, "Run")
		r.api.WaitFor("the command", func() bool { return !c.busy() })
		r.api.RunNoteAction(0, "Run")
		r.api.Pump()
		n := 0
		for _, cmd := range r.store.commands() {
			if strings.HasPrefix(cmd, "CONFIG SET") {
				n++
			}
		}
		if n != 1 {
			t.Errorf("CONFIG SET was sent %d times", n)
		}
	})
}

func TestTheConfirmationCanBeTurnedOff(t *testing.T) {
	r := newRig(t, nil)
	r.api.SetSetting(SettingConfirmDangerous, false)
	c := r.openConsole()
	r.run(c, "FLUSHDB")
	if len(r.api.Notes()) != 0 || !r.store.seen("FLUSHDB") {
		t.Errorf("notes = %+v, commands = %q", r.api.Notes(), r.store.commands())
	}
}

func TestOnlyADeleteOfSeveralKeysAsks(t *testing.T) {
	r := newRig(t, nil)
	c := r.openConsole()
	r.run(c, "DEL greeting")
	if len(r.api.Notes()) != 0 || !r.store.seen("DEL greeting") {
		t.Fatalf("DEL of one key asked: %+v", r.api.Notes())
	}
	c.submit("DEL scores events")
	r.api.Pump()
	if len(r.api.Notes()) != 1 || r.store.seen("DEL scores") {
		t.Fatalf("DEL of two keys: notes %+v, commands %q", r.api.Notes(), r.store.commands())
	}
}

func TestAFileRunsLineByLineAndStopsAtADangerousCommand(t *testing.T) {
	r := newRig(t, nil)
	c := r.openConsole()
	c.runLines([]string{"# set things up", "", "SET a 1", "FLUSHDB", "SET b 2", "GET b"})
	r.api.Pump()
	// the first SET runs, FLUSHDB waits
	r.api.WaitFor("the first command", func() bool { return c.run != nil && c.run.waiting })
	if !r.store.seen("SET a 1") || r.store.seen("FLUSHDB") || r.store.seen("SET b 2") {
		t.Fatalf("commands = %q", r.store.commands())
	}
	r.api.RunNoteAction(0, "Run")
	r.api.WaitFor("the rest", func() bool { return !c.busy() })
	want := []string{"SET a 1", "FLUSHDB", "SET b 2", "GET b"}
	var got []string
	for _, cmd := range r.store.commands() {
		if !strings.HasPrefix(cmd, "COMMAND") && !strings.HasPrefix(cmd, "INFO") {
			got = append(got, cmd)
		}
	}
	if strings.Join(got, "|") != strings.Join(want, "|") {
		t.Errorf("commands = %q, want %q", got, want)
	}
	if last := c.entries[len(c.entries)-1]; last.text != `"2"` {
		t.Errorf("last entry = %+v", last)
	}
}

func TestAFileStopsAtALineItCannotRead(t *testing.T) {
	r := newRig(t, nil)
	c := r.openConsole()
	c.runLines([]string{"SET a 1", `SET b "open`, "SET c 3"})
	r.api.WaitFor("the run", func() bool { return !c.busy() })
	if r.store.seen("SET c 3") || !r.store.seen("SET a 1") {
		t.Errorf("commands = %q", r.store.commands())
	}
}

func TestACommandCanBeCancelled(t *testing.T) {
	r := newRig(t, nil)
	c := r.openConsole()
	c.submit("BLPOP queue:none 0")
	r.api.WaitFor("the command to be sent", func() bool { return r.store.seen("BLPOP") })
	if !c.inFlight() {
		t.Fatal("not in flight")
	}
	c.cancel()
	r.api.WaitFor("the cancel", func() bool { return !c.busy() })
	if !strings.Contains(joined(c), "note (cancelled)") {
		t.Errorf("console =\n%s", joined(c))
	}
	// a second command goes on
	r.run(c, "PING")
	if last := c.entries[len(c.entries)-1]; last.text != "PONG" {
		t.Errorf("last entry = %+v", last)
	}
}

func TestACommandWhileAnotherRunsIsRefused(t *testing.T) {
	r := newRig(t, nil)
	c := r.openConsole()
	c.submit("BLPOP queue:none 0")
	r.api.WaitFor("the command to be sent", func() bool { return r.store.seen("BLPOP") })
	c.submit("PING")
	if last := c.entries[len(c.entries)-1]; !strings.Contains(last.text, "still running") {
		t.Errorf("last entry = %+v", last)
	}
	c.cancel()
	r.api.WaitFor("the cancel", func() bool { return !c.busy() })
}

func TestTheConsoleKeepsABoundedNumberOfEntries(t *testing.T) {
	r := newRig(t, nil)
	c := r.openConsole()
	for i := 0; i < maxConsoleEntries+200; i++ {
		c.add(entryReply, "x")
	}
	if len(c.entries) != maxConsoleEntries {
		t.Errorf("%d entries", len(c.entries))
	}
	c.clear()
	big := strings.Repeat("y", 1<<20)
	for i := 0; i < 5; i++ {
		c.add(entryReply, big)
	}
	if c.size > maxConsoleBytes || len(c.entries) != 2 {
		t.Errorf("%d entries of %d bytes", len(c.entries), c.size)
	}
	c.clear()
	if len(c.entries) != 0 || c.size != 0 {
		t.Error("Clear leaves entries")
	}
}

func TestAClosedConnectionStopsTheCommandAndTheConsoleSaysSo(t *testing.T) {
	r := newRig(t, nil)
	c := r.openConsole()
	if err := r.hub.Disconnect(r.conn.ID); err != nil {
		t.Fatal(err)
	}
	r.api.Pump()
	r.run(c, "PING")
	if last := c.entries[len(c.entries)-1]; last.kind != entryError || !strings.Contains(last.text, "connection is not open") {
		t.Errorf("last entry = %+v", last)
	}
}

// ---- the read-only connection ----

func TestAReadOnlyConnectionRefusesWritesBeforeTheyAreSent(t *testing.T) {
	r := newRig(t, nil, func(c *dbconn.Connection) { c.ReadOnly = true })
	c := r.openConsole()
	for _, line := range []string{"SET k v", "DEL greeting", "FLUSHALL", "PUBLISH news hi", "EVAL x 0", "CONFIG SET maxmemory 1", "MYMODULE.WRITE x"} {
		r.run(c, line)
		last := c.entries[len(c.entries)-1]
		if last.kind != entryError || !strings.Contains(last.text, "read-only") {
			t.Errorf("%q: last entry = %+v", line, last)
		}
	}
	for _, n := range []string{"SET", "DEL", "FLUSHALL", "PUBLISH", "EVAL", "CONFIG SET", "MYMODULE.WRITE"} {
		if r.store.seen(n) {
			t.Errorf("%s was sent to a read-only connection: %q", n, r.store.commands())
		}
	}
	// a read goes through
	r.run(c, "GET greeting")
	if last := c.entries[len(c.entries)-1]; last.text != `"hello world"` {
		t.Errorf("last entry = %+v", last)
	}
	// a batch goes on after a refusal
	c.runLines([]string{"SET a 1", "GET greeting"})
	r.api.WaitFor("the batch", func() bool { return !c.busy() })
	if last := c.entries[len(c.entries)-1]; last.text != `"hello world"` {
		t.Errorf("a refusal stopped the batch: %+v", last)
	}
}

func TestTheServersFlagsDecideWhatAReadOnlyConnectionRefuses(t *testing.T) {
	r := newRig(t, nil, func(c *dbconn.Connection) { c.ReadOnly = true })
	r.connect()
	db, _, err := r.ext.Driver().database(r.conn.ID, 0)
	if err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	// a command only the server knows to be a write
	_, err = db.Do(ctx, []string{"mymodule.write", "x"})
	wantRoute(t, err, "readOnlyDB.Do.RedisReadOnly")
	// the flags are asked once for a command
	for i := 0; i < 3; i++ {
		if _, err := db.Do(ctx, []string{"ttl", "greeting"}); err != nil {
			t.Fatal(err)
		}
	}
	asked := 0
	for _, c := range r.store.commands() {
		if c == "COMMAND INFO ttl" {
			asked++
		}
	}
	if asked != 1 {
		t.Errorf("COMMAND INFO ttl was sent %d times", asked)
	}
	// a subcommand is looked up with its container
	if _, err := db.Do(ctx, []string{"config", "get", "x"}); err != nil {
		t.Fatal(err)
	}
	if !r.store.seen("COMMAND INFO config|get") {
		t.Errorf("commands = %q", r.store.commands())
	}
}

func TestAServerThatCannotDescribeItsCommandsFallsBackOnTheBuiltInList(t *testing.T) {
	store := fixtureServer()
	store.noCommandInfo = true
	r := newRig(t, store, func(c *dbconn.Connection) { c.ReadOnly = true })
	c := r.openConsole()
	r.run(c, "SET k v")
	if last := c.entries[len(c.entries)-1]; last.kind != entryError || !strings.Contains(last.text, "read-only") || store.seen("SET") {
		t.Errorf("last entry = %+v, commands %q", last, store.commands())
	}
	r.run(c, "GET greeting")
	if last := c.entries[len(c.entries)-1]; last.text != `"hello world"` {
		t.Errorf("last entry = %+v", last)
	}
}

func TestAConnectionThatFailsWhileTheGuardAsksIsAnError(t *testing.T) {
	r := newRig(t, nil, func(c *dbconn.Connection) { c.ReadOnly = true })
	r.connect()
	db, _, err := r.ext.Driver().database(r.conn.ID, 0)
	if err != nil {
		t.Fatal(err)
	}
	r.store.fail["COMMAND INFO fresh"] = context.DeadlineExceeded
	_, err = db.Do(context.Background(), []string{"fresh", "x"})
	if err == nil || r.store.seen("fresh x") {
		t.Errorf("err = %v, commands %q", err, r.store.commands())
	}
}

// ---- the events ----

// published decodes the events of a topic.
func published[T any](t *testing.T, r *rig, topic string) []T {
	t.Helper()
	r.api.Pump()
	var out []T
	for _, p := range r.api.PublishedEvents() {
		if p.Topic != topic {
			continue
		}
		var v T
		if err := json.Unmarshal(p.Data, &v); err != nil {
			t.Fatal(err)
		}
		out = append(out, v)
	}
	return out
}

func TestTheEventsOfAConnectionAndOfCommands(t *testing.T) {
	r := newRig(t, nil)
	c := r.openConsole()
	if got := published[ConnectionEvent](t, r, TopicConnected); len(got) != 1 || got[0].Connection != r.conn.ID || got[0].Name != "cache" {
		t.Errorf("connected = %+v", got)
	}
	r.run(c, "PING")
	r.run(c, "GET user:7:profile")     // an error reply
	r.run(c, "SET k \"secret value\"") // nothing of it may reach the event
	fin := published[CommandFinished](t, r, TopicCommandFinished)
	if len(fin) != 3 || !fin[0].OK || fin[1].OK || !fin[2].OK || fin[0].Connection != r.conn.ID || fin[0].Millis < 0 {
		t.Errorf("commandFinished = %+v", fin)
	}
	for _, p := range r.api.PublishedEvents() {
		if strings.Contains(string(p.Data), "secret") || strings.Contains(string(p.Data), "user:7") || strings.Contains(string(p.Data), "PING") {
			t.Errorf("an event carries a command, a key or a value: %s %s", p.Topic, p.Data)
		}
	}
	if err := r.hub.Disconnect(r.conn.ID); err != nil {
		t.Fatal(err)
	}
	if got := published[ConnectionEvent](t, r, TopicDisconnected); len(got) != 1 || got[0].Connection != r.conn.ID {
		t.Errorf("disconnected = %+v", got)
	}
}

func TestTheFileRunOfACommandIsAnEventToo(t *testing.T) {
	r := newRig(t, nil)
	c := r.openConsole()
	c.runLines([]string{"PING", "PING"})
	r.api.WaitFor("the run", func() bool { return !c.busy() })
	if got := published[CommandFinished](t, r, TopicCommandFinished); len(got) != 2 {
		t.Errorf("commandFinished = %+v", got)
	}
}

// ---- completion and the commands of the server ----

func TestTheConsoleCompletesTheNamesOfTheServersCommands(t *testing.T) {
	r := newRig(t, nil)
	c := r.openConsole()
	r.ext.ensureCommandDocs(r.conn.ID) // the console does it when it is built
	r.api.WaitFor("the commands", func() bool { return len(r.ext.commandDocs(r.conn.ID)) == len(fakeDocs) })
	names := r.ext.commandNames(r.conn.ID)
	if strings.Join(names, " ") != "DEL GET GETDEL GETEX HGETALL PING SET" {
		t.Errorf("names = %q", names)
	}
	c.input.SetValue("get")
	if got := c.suggestions(); strings.Join(got, " ") != "GETDEL GETEX" {
		t.Errorf("suggestions = %q", got)
	}
	c.input.SetValue("GET ")
	if got := c.suggestions(); got != nil {
		t.Errorf("suggestions after the first word = %q", got)
	}
	c.input.SetValue("")
	if got := c.suggestions(); got != nil {
		t.Errorf("suggestions of nothing = %q", got)
	}
	// a server that cannot list its commands: the built-in names
	store := fixtureServer()
	store.noCommandInfo = true
	r2 := newRig(t, store)
	r2.openConsole()
	r2.ext.ensureCommandDocs(r2.conn.ID)
	r2.api.Pump()
	if names := r2.ext.commandNames(r2.conn.ID); len(names) != len(builtinCommandNames) {
		t.Errorf("%d names", len(names))
	}
}

func TestCommandDocsAreReadFromBothProtocols(t *testing.T) {
	for _, resp3 := range []bool{false, true} {
		store := fixtureServer()
		store.resp3 = resp3
		db, _ := store.DB(0)
		rep, err := db.Do(context.Background(), []string{"COMMAND", "DOCS"})
		if err != nil {
			t.Fatal(err)
		}
		docs := ParseCommandDocs(rep)
		if len(docs) != len(fakeDocs) || docs[1].Name != "GET" || docs[1].Summary != "Returns the string value of a key." {
			t.Errorf("resp3=%v: docs = %+v", resp3, docs)
		}
	}
	rep, _ := (&fakeDB{s: newFakeServer()}).Do(context.Background(), []string{"COMMAND"})
	if docs := ParseCommandList(rep); len(docs) != len(fakeDocs) || docs[0].Name != "DEL" {
		t.Errorf("docs = %+v", docs)
	}
	if ParseCommandDocs(Reply{Kind: ReplyInt}) != nil || ParseCommandList(Reply{Kind: ReplyNil}) != nil {
		t.Error("a reply that is not a list gives commands")
	}
}

// ---- the Databases view and the commands ----

func TestTheOpenConsoleCommandShowsThePanelOfAnOpenConnection(t *testing.T) {
	r := newRig(t, nil)
	r.connect()
	r.api.Pump()
	if err := r.api.Run(CmdOpenConsole, r.conn.ID); err != nil {
		t.Fatal(err)
	}
	v := r.api.View(PanelViewID)
	if v == nil || len(v.Reveals) == 0 || !v.Reveals[len(v.Reveals)-1] || r.ext.target != r.conn.ID {
		t.Fatalf("view = %+v, target %q", v, r.ext.target)
	}
	// the menu of the connection, and of a database
	db0 := r.children(nil)[0]
	r.action(nil, "Open Console")
	r.action(&db0, "Open Console")
	r.api.Pump()
	if got := r.ext.consoleFor(r.conn.ID).db; got != 0 {
		t.Errorf("db = %d", got)
	}
	db3 := find(t, r.children(nil), "db3")
	r.action(&db3, "Open Console")
	if c := r.ext.consoleFor(r.conn.ID); c.db != 3 || !strings.Contains(joined(c), "now go to database 3") {
		t.Errorf("db = %d\n%s", c.db, joined(c))
	}
}

func TestWithNoOpenConnectionTheCommandsSayWhatToDo(t *testing.T) {
	r := newRig(t, nil)
	for _, id := range []string{CmdOpenConsole, CmdOpenPubSub, CmdOpenMonitor, CmdServerInfo, CmdSubscribe, CmdPublish} {
		before := len(r.api.Notes())
		if err := r.api.Run(id, nil); err != nil {
			t.Fatal(err)
		}
		r.api.Pump()
		notes := r.api.Notes()
		if len(notes) == before || !strings.Contains(notes[len(notes)-1].Message, "Connect") && !strings.Contains(notes[len(notes)-1].Message, "No Redis connection is open") {
			t.Errorf("%s: notes = %+v", id, notes)
		}
	}
	if r.ext.chooseTarget() != "" {
		t.Error("a target without a connection")
	}
}

func TestTheTargetFollowsTheOpenConnections(t *testing.T) {
	r := newRig(t, nil)
	r.connect()
	r.api.Pump()
	if got := r.ext.chooseTarget(); got != r.conn.ID {
		t.Fatalf("target = %q", got)
	}
	if err := r.hub.Disconnect(r.conn.ID); err != nil {
		t.Fatal(err)
	}
	if got := r.ext.chooseTarget(); got != "" {
		t.Errorf("target after Disconnect = %q", got)
	}
}

var _ = time.Second
var _ = fakeapi.Note{}
var _ extension.Position

func TestTheRunCommandTakesLinesFromItsArguments(t *testing.T) {
	r := newRig(t, nil)
	r.connect()
	r.api.Pump()
	args := map[string]any{"connection": r.conn.ID, "lines": []any{"PING", "DBSIZE"}}
	if err := r.api.Run(CmdRun, args); err != nil {
		t.Fatal(err)
	}
	c := r.ext.consoleFor(r.conn.ID)
	r.api.WaitFor("the run", func() bool { return !c.busy() && len(c.entries) >= 4 })
	if got := joined(c); got != "cmd db0> PING\nreply PONG\ncmd db0> DBSIZE\nreply (integer) 9" {
		t.Errorf("console =\n%s", got)
	}
	// with no connection named, the one the tabs show
	if err := r.api.Run(CmdRun, map[string]any{"lines": []any{"ECHO x"}}); err != nil {
		t.Fatal(err)
	}
	r.api.WaitFor("the run", func() bool { return !c.busy() && len(c.entries) >= 6 })
	// lines that are not text are not lines: the command then looks at the active file, which there is none
	if err := r.api.Run(CmdRun, map[string]any{"lines": []any{1}}); err != nil {
		t.Fatal(err)
	}
	if notes := r.api.Notes(); len(notes) != 1 || !strings.Contains(notes[0].Message, "Open a .redis file") {
		t.Errorf("notes = %+v", notes)
	}
	if err := r.hub.Disconnect(r.conn.ID); err != nil {
		t.Fatal(err)
	}
	r.api.Pump()
	if err := r.api.Run(CmdRun, map[string]any{"lines": []any{"PING"}}); err != nil {
		t.Fatal(err)
	}
	if notes := r.api.Notes(); !strings.Contains(notes[len(notes)-1].Message, "No Redis connection is open") {
		t.Errorf("notes = %+v", notes)
	}
}

func TestTheOpenKeyCommandOpensTheKeyEditor(t *testing.T) {
	r := newRig(t, nil)
	if err := r.api.Run(CmdOpenKey, nil); err != nil {
		t.Fatal(err)
	}
	if notes := r.api.Notes(); len(notes) != 1 || !strings.Contains(notes[0].Message, "No Redis connection is open") {
		t.Fatalf("notes = %+v", notes)
	}
	r.connect()
	r.api.Pump()
	if err := r.api.Run(CmdOpenKey, map[string]any{"connection": r.conn.ID, "db": float64(3), "key": "session:abc"}); err != nil {
		t.Fatal(err)
	}
	var tab *keyTab
	r.api.WaitFor("the key editor", func() bool {
		for _, kt := range r.ext.keyTabs {
			if kt.ref.Key == "session:abc" && kt.loaded {
				tab = kt
			}
		}
		return tab != nil
	})
	if tab.ref.DB != 3 {
		t.Errorf("ref = %+v", tab.ref)
	}
	// with no arguments the key is asked, in the database of the console
	r.ext.consoleFor(r.conn.ID).db = 0
	if err := r.api.Run(CmdOpenKey, nil); err != nil {
		t.Fatal(err)
	}
	in := r.api.LastInput()
	if in == nil || !strings.Contains(in.Options().Prompt, "database 0") {
		t.Fatalf("input = %+v", in)
	}
	in.Accept("greeting")
	r.api.WaitFor("the key editor", func() bool {
		for _, kt := range r.ext.keyTabs {
			if kt.ref.Key == "greeting" && kt.loaded {
				return true
			}
		}
		return false
	})
}
