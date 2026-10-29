package redis

import (
	"context"
	"encoding/json"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/rm4n0s/errors"

	"github.com/rm4n0s/emetos/extension"
	"github.com/rm4n0s/emetos/extensions/internal/dbconn"
	"github.com/rm4n0s/emetos/extensions/internal/fakeapi"
	"github.com/rm4n0s/emetos/extensions/internal/resultgrid"
)

// idle waits until the tab has nothing running.
func (r *rig) idle(kt *keyTab) {
	r.t.Helper()
	r.api.WaitFor("the key editor to be idle", func() bool { return !kt.busy && !kt.loading })
}

// fakeTab is the fake API's tab of a key editor.
func (r *rig) fakeTab(kt *keyTab) *fakeapi.Tab {
	r.t.Helper()
	for _, ft := range r.api.EditorTabs() {
		if ft.Kind() == KeyKindID && ft.Title() == kt.titleWanted() {
			return ft
		}
	}
	r.t.Fatalf("no fake tab titled %q", kt.titleWanted())
	return nil
}

func (t *keyTab) titleWanted() string {
	name := t.ref.Key
	if i := strings.LastIndexAny(name, ":/"); i >= 0 && i < len(name)-1 {
		name = name[i+1:]
	}
	return name
}

// edit types text in the editor of a string key, as the user does.
func (t *keyTab) edit(text string) {
	t.editor.SetValue(text)
	t.editorChanged(text)
}

// rowValues reads the grid of a tab.
func rowValues(kt *keyTab) [][]string { return rowsOf(kt.coll) }

// selectRow selects the row r of the grid, as a click does.
func selectRow(kt *keyTab, r int) { kt.grid.Model().PressRow(r, resultgrid.Mods{}) }

func TestTheKeyEditorShowsEveryTypeOfKey(t *testing.T) {
	tests := []struct {
		key  string
		info string
		rows int // rows of the grid, 0 for an editor
		text string
		mode int
	}{
		{"greeting", "String  ·  11 bytes  ·  TTL no expiry  ·  144 B  ·  listpack", 0, "hello world", modeText},
		{"config:app", "String  ·  24 bytes  ·  TTL 1m 30s  ·  248 B  ·  listpack", 0, "{\n  \"debug\": true,\n  \"level\": 3\n}", modeJSON},
		{"user:42:profile", "Hash  ·  3 fields  ·  TTL no expiry  ·  80 B  ·  listpack", 3, "", 0},
		{"queue:jobs", "List  ·  3 items  ·  TTL no expiry  ·  80 B  ·  listpack", 3, "", 0},
		{"user:42:tags", "Set  ·  3 members  ·  TTL no expiry  ·  80 B  ·  listpack", 3, "", 0},
		{"scores", "Sorted set  ·  3 members  ·  TTL no expiry  ·  80 B  ·  listpack", 3, "", 0},
		{"events", "Stream  ·  2 entries  ·  TTL no expiry  ·  72 B  ·  listpack", 2, "", 0},
		{"doc:1", "JSON (ReJSON)  ·  TTL no expiry  ·  48 B  ·  listpack", 0, "{\n  \"title\": \"Hello\",\n  \"tags\": [\n    \"a\",\n    \"b\"\n  ]\n}", modeJSON},
	}
	r := newRig(t, nil)
	r.connect()
	for _, tc := range tests {
		t.Run(tc.key, func(t *testing.T) {
			kt := r.openKeyTab(0, tc.key)
			if got := InfoLine(kt.info); got != tc.info {
				t.Errorf("info = %q\nwant   %q", got, tc.info)
			}
			if tc.rows > 0 {
				if kt.coll == nil || kt.coll.Len() != tc.rows || kt.grid.Model().Len() != tc.rows {
					t.Errorf("rows = %v", kt.coll)
				}
				return
			}
			if kt.coll != nil || kt.editor.Value() != tc.text || kt.mode != tc.mode {
				t.Errorf("editor %q mode %d, want %q mode %d", kt.editor.Value(), kt.mode, tc.text, tc.mode)
			}
			if kt.dirty {
				t.Error("a value that was just loaded is not an edit")
			}
		})
	}
	tab := r.fakeTab(r.keyTabs()[0])
	if tab.Title() != "greeting" || !strings.Contains(tab.Hint(), "cache db0 greeting") {
		t.Errorf("title %q hint %q", tab.Title(), tab.Hint())
	}
	if tab.Busy() {
		t.Error("the tab is still busy after the load")
	}
	// a key opens one tab: opening it again shows that one
	n := len(r.keyTabs())
	r.ext.openKey(KeyRef{Conn: r.conn.ID, DB: 0, Key: "greeting"})
	if len(r.keyTabs()) != n || !tab.Revealed {
		t.Errorf("tabs %d (was %d), revealed %v", len(r.keyTabs()), n, tab.Revealed)
	}
}

func TestOpeningAKeyFromTheExplorer(t *testing.T) {
	r := newRig(t, nil)
	r.connect()
	db0 := r.children(nil)[0]
	events := find(t, r.children(&db0), "events")
	r.hub.Open(r.conn.ID, events)
	r.api.WaitFor("the tab", func() bool { return len(r.keyTabs()) == 1 && r.keyTabs()[0].loaded })
	if kt := r.keyTabs()[0]; kt.ref != (KeyRef{r.conn.ID, 0, "events"}) || kt.coll.Type() != TypeStream {
		t.Errorf("tab = %+v", kt.ref)
	}
	// Open from the menu
	r.action(&events, "Open")
	if len(r.keyTabs()) != 1 {
		t.Errorf("tabs = %d", len(r.keyTabs()))
	}
	// the other rows open nothing
	r.hub.Open(r.conn.ID, db0)
	r.hub.Open(r.conn.ID, dbconn.Node{Data: "x"})
	if len(r.keyTabs()) != 1 {
		t.Errorf("tabs = %d", len(r.keyTabs()))
	}
}

func TestTheStateOfATabComesBack(t *testing.T) {
	r := newRig(t, nil)
	r.connect()
	r.store.put(0, "bin\xff:key", &fakeKey{typ: TypeString, str: "x"})
	for _, key := range []string{"greeting", "bin\xff:key"} {
		kt := r.openKeyTab(0, key)
		ft := r.fakeTab(kt)
		raw := ft.State()
		var st keyState
		if err := json.Unmarshal(raw, &st); err != nil {
			t.Fatal(err)
		}
		if st.ref() != kt.ref {
			t.Errorf("state %s gives %+v, want %+v", raw, st.ref(), kt.ref)
		}
		if key != "greeting" && st.Key != "" {
			t.Errorf("a binary key in a text field of the state: %q", st.Key)
		}
		back := r.api.RestoreTab(KeyKindID, "restored-"+key, raw)
		r.api.WaitFor("the restored tab", func() bool { return len(r.keyTabs()) > 0 && r.keyTabs()[len(r.keyTabs())-1].loaded })
		if got := r.keyTabs()[len(r.keyTabs())-1].ref; got != kt.ref {
			t.Errorf("restored %+v, want %+v", got, kt.ref)
		}
		_ = back
	}
}

func TestATabWithoutItsStateFails(t *testing.T) {
	r := newRig(t, nil)
	_, err := r.ext.createKeyTab(nil, nil)
	wantRoute(t, err, "Extension.createKeyTab.RedisKeyStateMissing")
	_, err = r.ext.createKeyTab(nil, json.RawMessage(`{"conn": 5}`))
	wantRoute(t, err, "Extension.createKeyTab.RedisKeyStateInvalid")
	_, err = r.ext.createNewKeyTab(nil, nil)
	wantRoute(t, err, "Extension.createNewKeyTab.RedisNewKeyStateMissing")
	_, err = r.ext.createNewKeyTab(nil, json.RawMessage(`[1]`))
	wantRoute(t, err, "Extension.createNewKeyTab.RedisNewKeyStateInvalid")
}

func TestEditingAStringKeepsItsTTL(t *testing.T) {
	r := newRig(t, nil)
	r.connect()
	kt := r.openKeyTab(0, "greeting")
	if kt.dirty || kt.locked() {
		t.Fatal("a fresh string is not dirty and not locked")
	}
	kt.edit("hello there")
	if !kt.dirty || !r.fakeTab(kt).Dirty() {
		t.Fatal("an edit marks the tab")
	}
	kt.edit("hello world") // back to what it was
	if kt.dirty || r.fakeTab(kt).Dirty() {
		t.Error("an edit that is undone clears the mark")
	}
	kt.edit("hello there")
	kt.saveString()
	r.idle(kt)
	db := mustDB(t, r.store, 0)
	if got, _, _ := db.GetRange(context.Background(), "greeting", 100); got != "hello there" {
		t.Errorf("saved %q", got)
	}
	if kt.dirty || r.fakeTab(kt).Dirty() || kt.editor.Value() != "hello there" {
		t.Errorf("after the save: dirty %v, editor %q", kt.dirty, kt.editor.Value())
	}
	// revert
	kt.edit("something else")
	kt.revert()
	if kt.editor.Value() != "hello there" || kt.dirty {
		t.Errorf("after Revert: %q dirty %v", kt.editor.Value(), kt.dirty)
	}
	// nothing changed: nothing is sent
	before := len(r.store.commands())
	kt.saveString()
	if len(r.store.commands()) != before {
		t.Error("Save sent a command although nothing changed")
	}
}

func TestEditingJSONInTheJSONView(t *testing.T) {
	r := newRig(t, nil)
	r.connect()
	kt := r.openKeyTab(0, "config:app")
	if kt.mode != modeJSON {
		t.Fatalf("mode = %d", kt.mode)
	}
	kt.edit("{\n  \"debug\": false,\n  \"level\": 4\n}")
	kt.saveString()
	r.idle(kt)
	db := mustDB(t, r.store, 0)
	got, _, _ := db.GetRange(context.Background(), "config:app", 100)
	if got != `{"debug":false,"level":4}` {
		t.Errorf("saved %q: a value that was stored compact stays compact", got)
	}
	if info, _ := db.Info(context.Background(), "config:app"); info.TTLMillis != 90_000 {
		t.Errorf("TTL = %d", info.TTLMillis)
	}
	// broken JSON is refused and nothing is written
	kt.edit(`{"debug": `)
	kt.saveString()
	r.idle(kt)
	if got2, _, _ := db.GetRange(context.Background(), "config:app", 100); got2 != got {
		t.Errorf("a refused save wrote %q", got2)
	}
	if !kt.note.Bad || !strings.Contains(kt.note.Text, "not valid JSON") || !kt.dirty {
		t.Errorf("note %+v, dirty %v", kt.note, kt.dirty)
	}
}

func TestTheViewsOfAString(t *testing.T) {
	r := newRig(t, nil)
	r.connect()
	kt := r.openKeyTab(0, "config:app")
	kt.setMode(modeText)
	if kt.editor.Value() != `{"debug":true,"level":3}` {
		t.Errorf("text = %q", kt.editor.Value())
	}
	kt.setMode(modeHex)
	if !strings.HasPrefix(kt.editor.Value(), "00000000  7b 22 64 65 62 75 67 22") {
		t.Errorf("hex = %q", kt.editor.Value())
	}
	// the hex view takes back every edit
	kt.editorChanged("typed")
	if kt.editor.Value() != kt.shown || kt.dirty {
		t.Errorf("the hex view took an edit: %q dirty %v", kt.editor.Value(), kt.dirty)
	}
	if !kt.locked() {
		t.Error("hex is locked")
	}
	kt.saveString()
	if kt.busy {
		t.Error("Save ran in the hex view")
	}
	kt.setMode(modeJSON)
	if !strings.HasPrefix(kt.editor.Value(), "{\n") {
		t.Errorf("json = %q", kt.editor.Value())
	}
}

func TestABinaryStringIsShownAsHex(t *testing.T) {
	r := newRig(t, nil)
	r.connect()
	r.store.put(0, "blob", &fakeKey{typ: TypeString, str: "\x00\x01binary\xff"})
	kt := r.openKeyTab(0, "blob")
	if kt.mode != modeHex || !kt.str.Binary() || !kt.locked() {
		t.Errorf("mode %d binary %v locked %v", kt.mode, kt.str.Binary(), kt.locked())
	}
	if !strings.Contains(kt.editor.Value(), "|..binary.|") {
		t.Errorf("hex = %q", kt.editor.Value())
	}
}

func TestABigStringIsCutAndCannotBeSaved(t *testing.T) {
	r := newRig(t, nil)
	r.connect()
	big := strings.Repeat("x", MaxStringBytes+1000)
	r.store.put(0, "big", &fakeKey{typ: TypeString, str: big})
	kt := r.openKeyTab(0, "big")
	if !kt.str.Truncated || kt.str.Size != int64(len(big)) || len(kt.editor.Value()) != MaxStringBytes || !kt.locked() {
		t.Errorf("truncated %v size %d editor %d", kt.str.Truncated, kt.str.Size, len(kt.editor.Value()))
	}
	kt.editorChanged("typed")
	if kt.dirty {
		t.Error("a cut value took an edit")
	}
	kt.saveString()
	if got, _, _ := mustDB(t, r.store, 0).GetRange(context.Background(), "big", 10); got != "xxxxxxxxxx" {
		t.Error("the big string was changed")
	}
}

func TestEditingAReJSONKey(t *testing.T) {
	r := newRig(t, nil)
	r.connect()
	kt := r.openKeyTab(0, "doc:1")
	if kt.mode != modeJSON || kt.info.Type != TypeJSON {
		t.Fatalf("mode %d type %q", kt.mode, kt.info.Type)
	}
	kt.edit("{\n \"title\": \"Changed\"\n}")
	kt.saveString()
	r.idle(kt)
	if got, _ := mustDB(t, r.store, 0).JSONGet(context.Background(), "doc:1"); got != `{"title":"Changed"}` {
		t.Errorf("saved %q", got)
	}
	kt.edit("not json")
	kt.saveString()
	r.idle(kt)
	if !kt.note.Bad {
		t.Errorf("note = %+v", kt.note)
	}
}

func TestEditingTheRowsOfAHash(t *testing.T) {
	r := newRig(t, nil)
	r.connect()
	kt := r.openKeyTab(0, "user:42:profile")
	// add
	kt.openAddForm()
	if kt.form == nil || kt.form.Title != "Add field" {
		t.Fatalf("form = %+v", kt.form)
	}
	kt.submitForm() // the field is required
	if !kt.note.Bad || !strings.Contains(kt.note.Text, "Field is required") || kt.busy {
		t.Errorf("note = %+v", kt.note)
	}
	kt.form.ctl[0].SetValue("city")
	kt.form.ctl[1].SetValue("Athens")
	kt.submitForm()
	r.idle(kt)
	if kt.form != nil || kt.coll.Len() != 4 || kt.grid.Model().Len() != 4 {
		t.Fatalf("after Add: form %v, rows %d, grid %d", kt.form, kt.coll.Len(), kt.grid.Model().Len())
	}
	// edit the row that was added
	selectRow(kt, 3)
	kt.openEditForm()
	if kt.form == nil || !kt.form.Edit || kt.form.fields[0].Value != "city" || !kt.form.fields[0].Disabled {
		t.Fatalf("edit form = %+v", kt.form)
	}
	kt.form.ctl[1].SetValue("Patras")
	kt.submitForm()
	r.idle(kt)
	if got := rowValues(kt)[3]; got[1] != "Patras" {
		t.Errorf("row = %q", got)
	}
	// remove two rows: the notification asks first
	selectRow(kt, 0)
	kt.grid.Model().Press(1, 0, resultgrid.Mods{Shift: true})
	kt.removeRows()
	if kt.busy {
		t.Fatal("two rows were removed without asking")
	}
	note := r.waitNote("Remove 2 rows")
	if len(note.Actions) != 1 || note.Actions[0] != "Remove" {
		t.Fatalf("note = %+v", note)
	}
	r.api.RunNoteAction(len(r.api.Notes())-1, "Remove")
	r.idle(kt)
	if got := rowValues(kt); len(got) != 2 || got[0][0] != "role" || got[1][0] != "city" {
		t.Errorf("rows = %q", got)
	}
	// one row is removed at once
	selectRow(kt, 0)
	kt.removeRows()
	r.idle(kt)
	if kt.coll.Len() != 1 {
		t.Errorf("rows = %d", kt.coll.Len())
	}
	// nothing selected
	kt.grid.Model().ClearSelection()
	kt.removeRows()
	if !kt.note.Bad || !strings.Contains(kt.note.Text, "Select the rows") {
		t.Errorf("note = %+v", kt.note)
	}
	kt.openEditForm()
	if !kt.note.Bad || !strings.Contains(kt.note.Text, "one row") {
		t.Errorf("note = %+v", kt.note)
	}
	// cancel closes the form
	kt.openAddForm()
	kt.closeForm()
	if kt.form != nil {
		t.Error("the form is still there")
	}
}

func TestEditingAListReloadsItsRows(t *testing.T) {
	r := newRig(t, nil)
	r.connect()
	kt := r.openKeyTab(0, "queue:jobs")
	kt.openAddForm()
	kt.form.ctl[0].SetValue("plan")
	kt.form.choice[1] = 1 // head
	kt.submitForm()
	r.idle(kt)
	if got := rowValues(kt); len(got) != 4 || got[0][1] != "plan" || got[3][1] != "deploy" {
		t.Fatalf("rows = %q", got)
	}
	selectRow(kt, 1)
	kt.openEditForm()
	if kt.form.fields[0].Label != "Value of item 1" {
		t.Errorf("label = %q", kt.form.fields[0].Label)
	}
	kt.form.ctl[0].SetValue("BUILD")
	kt.submitForm()
	r.idle(kt)
	selectRow(kt, 0)
	kt.removeRows()
	r.idle(kt)
	if got := rowValues(kt); len(got) != 3 || got[0][1] != "BUILD" {
		t.Errorf("rows = %q", got)
	}
}

func TestEditingASetASortedSetAndAStream(t *testing.T) {
	r := newRig(t, nil)
	r.connect()

	set := r.openKeyTab(0, "user:42:tags")
	set.openAddForm()
	set.form.ctl[0].SetValue("tui")
	set.submitForm()
	r.idle(set)
	selectRow(set, 0)
	set.removeRows()
	r.idle(set)
	if got := rowValues(set); len(got) != 3 || got[0][0] != "redis" {
		t.Errorf("set = %q", got)
	}
	set.openEditForm() // a set has no edit
	if set.form != nil && set.form.Edit {
		t.Error("a member of a set was opened for editing")
	}

	z := r.openKeyTab(0, "scores")
	selectRow(z, 0)
	z.openEditForm()
	z.form.ctl[1].SetValue("100")
	z.submitForm()
	r.idle(z)
	if got := rowValues(z)[0]; got[0] != "carol" || got[1] != "100" {
		t.Errorf("zset row = %q", got)
	}
	z.openAddForm()
	z.form.ctl[0].SetValue("dave")
	z.form.ctl[1].SetValue("abc")
	z.submitForm()
	r.idle(z)
	if !z.note.Bad || !strings.Contains(z.note.Text, "a score is a number") || z.form == nil {
		t.Errorf("a bad score: note %+v, form kept %v", z.note, z.form != nil)
	}

	s := r.openKeyTab(0, "events")
	s.openAddForm()
	s.form.ctl[1].SetValue("type tick\nn 5")
	s.submitForm()
	r.idle(s)
	if got := rowValues(s); len(got) != 3 || !strings.Contains(got[0][2], "type=tick") {
		t.Errorf("stream = %q", got)
	}
	selectRow(s, 0)
	s.removeRows()
	r.idle(s)
	if s.coll.Len() != 2 {
		t.Errorf("stream rows = %d", s.coll.Len())
	}
}

func TestMoreRowsAreFetchedOnDemand(t *testing.T) {
	r := newRig(t, bigFixture(450))
	r.connect()
	kt := r.openKeyTab(0, "hash")
	if kt.coll.Len() != ValuePageSize || !kt.coll.More() {
		t.Fatalf("first page: %d rows, more %v", kt.coll.Len(), kt.coll.More())
	}
	kt.fetchMore()
	r.idle(kt)
	kt.fetchMore()
	r.idle(kt)
	if kt.coll.Len() != 450 || kt.coll.More() || kt.grid.Model().Len() != 450 {
		t.Errorf("rows %d, more %v, grid %d", kt.coll.Len(), kt.coll.More(), kt.grid.Model().Len())
	}
	kt.fetchMore() // nothing left
	if kt.busy {
		t.Error("fetchMore ran with nothing to fetch")
	}
}

func TestAReadOnlyConnectionCannotChangeAKey(t *testing.T) {
	r := newRig(t, nil, func(c *dbconn.Connection) { c.ReadOnly = true })
	r.connect()
	str := r.openKeyTab(0, "greeting")
	if !str.readOnly || !str.locked() {
		t.Fatal("a string of a read-only connection is locked")
	}
	str.editorChanged("typed")
	if str.dirty || str.editor.Value() != "hello world" {
		t.Errorf("a read-only string took an edit: %q", str.editor.Value())
	}
	h := r.openKeyTab(0, "user:42:profile")
	h.openAddForm()
	h.openEditForm()
	h.removeRows()
	if h.form != nil || h.busy || h.coll.Len() != 3 {
		t.Errorf("a read-only hash changed: form %v, busy %v, %d rows", h.form != nil, h.busy, h.coll.Len())
	}
	// the server would refuse anyway
	err := h.coll.Add(context.Background(), []string{"x", "y"})
	wantRoute(t, err, "readOnlyDB.HSet.RedisReadOnly")
}

func TestAKeyThatIsGone(t *testing.T) {
	r := newRig(t, nil)
	r.connect()
	kt := r.openKeyTab(0, "greeting")
	r.store.mu.Lock()
	delete(r.store.dbs[0].keys, "greeting")
	r.store.mu.Unlock()
	kt.reload()
	r.idle(kt)
	if !kt.missing || kt.coll != nil {
		t.Errorf("missing %v", kt.missing)
	}
}

func TestATabFollowsTheConnection(t *testing.T) {
	r := newRig(t, nil)
	r.connect()
	kt := r.openKeyTab(0, "greeting")
	if kt.disconnected {
		t.Fatal("connected")
	}
	if err := r.hub.Disconnect(r.conn.ID); err != nil {
		t.Fatal(err)
	}
	r.api.Pump()
	if !kt.disconnected {
		t.Error("the tab does not know that the connection closed")
	}
	// a tab opened on a closed connection says so
	r.ext.openKey(KeyRef{Conn: r.conn.ID, DB: 0, Key: "scores"})
	var other *keyTab
	r.api.WaitFor("the second tab", func() bool {
		for _, t := range r.keyTabs() {
			if t.ref.Key == "scores" && t.loaded {
				other = t
			}
		}
		return other != nil
	})
	if !other.disconnected || other.problem == "" {
		t.Errorf("disconnected %v, problem %q", other.disconnected, other.problem)
	}
	// connecting again loads both
	r.connect()
	r.api.WaitFor("the tabs to load again", func() bool {
		return !kt.disconnected && !other.disconnected && other.loaded && !other.loading && !kt.loading
	})
	if other.coll == nil || other.coll.Len() != 3 || kt.editor.Value() != "hello world" {
		t.Errorf("after reconnecting: %v", other.coll)
	}
}

func TestAServerThatFailsShowsTheProblem(t *testing.T) {
	r := newRig(t, nil)
	r.connect()
	r.store.fail["TYPE"] = errors.New("TestBoom", "LOADING Redis is loading the dataset in memory")
	r.ext.openKey(KeyRef{Conn: r.conn.ID, DB: 0, Key: "greeting"})
	var kt *keyTab
	r.api.WaitFor("the tab", func() bool {
		if len(r.keyTabs()) == 1 && r.keyTabs()[0].loaded {
			kt = r.keyTabs()[0]
		}
		return kt != nil
	})
	if !strings.Contains(kt.problem, "LOADING") || kt.disconnected {
		t.Errorf("problem = %q", kt.problem)
	}
	kt.reload()
	r.idle(kt)
	if kt.problem != "" || kt.editor.Value() != "hello world" {
		t.Errorf("after Refresh: problem %q, editor %q", kt.problem, kt.editor.Value())
	}
}

func TestRenamingAKeyFromTheTab(t *testing.T) {
	r := newRig(t, nil)
	r.connect()
	db0 := r.children(nil)[0]
	r.children(&db0)
	kt := r.openKeyTab(0, "greeting")
	var reloads int
	stop := r.hub.SubscribeReload(func(string) { reloads++ })
	defer stop()
	r.ext.renameKey(kt.ref)
	in := r.api.LastInput()
	if in.Options().Value != "greeting" || in.Validate("") == "" || in.Validate("greeting") == "" || in.Validate("hello") != "" {
		t.Fatalf("the box: %+v", in.Options())
	}
	in.Accept("hello")
	r.api.WaitFor("the rename", func() bool { return kt.ref.Key == "hello" && !kt.loading })
	if got := r.fakeTab(kt).Title(); got != "hello" {
		t.Errorf("title = %q", got)
	}
	if reloads == 0 {
		t.Error("the explorer was not told")
	}
	if ok, _ := mustDB(t, r.store, 0).Exists(context.Background(), "greeting"); ok {
		t.Error("the old key is still there")
	}
	names := labels(r.children(&db0))
	if !slices.Contains(names, "hello") || slices.Contains(names, "greeting") {
		t.Errorf("explorer = %q", names)
	}
	if kt.editor.Value() != "hello world" {
		t.Errorf("editor = %q", kt.editor.Value())
	}
	// a name that is taken is refused
	r.ext.renameKey(kt.ref)
	r.api.LastInput().Accept("scores")
	r.waitNote("a key with this name exists")
	if kt.ref.Key != "hello" {
		t.Errorf("the tab followed a rename that failed: %q", kt.ref.Key)
	}
}

func TestTTLFromTheTab(t *testing.T) {
	r := newRig(t, nil)
	r.connect()
	kt := r.openKeyTab(0, "greeting")
	r.ext.setTTL(kt.ref)
	in := r.api.LastInput()
	for _, bad := range []string{"", "0", "-5", "abc", "1.5"} {
		if in.Validate(bad) == "" {
			t.Errorf("the box accepts %q", bad)
		}
	}
	in.Accept("120")
	r.api.WaitFor("the TTL", func() bool { return kt.info.TTLMillis == 120_000 && !kt.loading })
	if !strings.Contains(InfoLine(kt.info), "TTL 2m 0s") {
		t.Errorf("info = %q", InfoLine(kt.info))
	}
	r.ext.persistKey(kt.ref)
	r.api.WaitFor("persist", func() bool { return kt.info.TTLMillis == -1 && !kt.loading })
	// setting a TTL on a key that went away is an error for the user
	r.ext.setTTL(KeyRef{Conn: r.conn.ID, DB: 0, Key: "nothing"})
	r.api.LastInput().Accept("5")
	r.waitNote("Setting the TTL failed")
}

func TestDeletingAKeyAsksFirstAndClosesItsTab(t *testing.T) {
	r := newRig(t, nil)
	r.connect()
	db0 := r.children(nil)[0]
	r.children(&db0)
	kt := r.openKeyTab(0, "greeting")
	r.ext.deleteKey(kt.ref)
	note := r.waitNote("Delete the key \"greeting\"")
	if len(note.Actions) != 1 || note.Actions[0] != "Delete" {
		t.Fatalf("note = %+v", note)
	}
	if ok, _ := mustDB(t, r.store, 0).Exists(context.Background(), "greeting"); !ok {
		t.Fatal("the key was deleted without waiting for the answer")
	}
	r.api.RunNoteAction(len(r.api.Notes())-1, "Delete")
	r.api.WaitFor("the delete", func() bool { return r.fakeTab(kt).Closed })
	if ok, _ := mustDB(t, r.store, 0).Exists(context.Background(), "greeting"); ok {
		t.Error("the key is still there")
	}
	if slices.Contains(labels(r.children(&db0)), "greeting") {
		t.Error("the explorer still lists the key")
	}
	r.store.fail["DEL"] = errors.New("TestBoom", "boom")
	r.ext.doDelete(KeyRef{Conn: r.conn.ID, DB: 0, Key: "scores"})
	r.waitNote("Deleting the key failed")
}

func TestTheTabsRemoveThemselvesWhenClosed(t *testing.T) {
	r := newRig(t, nil)
	r.connect()
	kt := r.openKeyTab(0, "greeting")
	ft := r.fakeTab(kt)
	ft.Close()
	if len(r.keyTabs()) != 0 {
		t.Errorf("tabs = %d after closing", len(r.keyTabs()))
	}
}

func TestInfoLine(t *testing.T) {
	tests := []struct {
		info KeyInfo
		want string
	}{
		{KeyInfo{Type: TypeString, TTLMillis: -1, Memory: -1, Size: 1}, "String  ·  1 byte  ·  TTL no expiry"},
		{KeyInfo{Type: TypeHash, TTLMillis: 5000, Memory: 2048, Size: 0, Encoding: "hashtable"}, "Hash  ·  0 fields  ·  TTL 5s  ·  2.0 KB  ·  hashtable"},
		{KeyInfo{Type: TypeJSON, TTLMillis: -1, Memory: -1, Size: -1}, "JSON (ReJSON)  ·  TTL no expiry"},
		{KeyInfo{Type: "TSDB-TYPE", TTLMillis: -1, Memory: -1, Size: -1}, "TSDB-TYPE  ·  TTL no expiry"},
		{KeyInfo{Type: TypeList, TTLMillis: -1, Memory: -1, Size: 1}, "List  ·  1 item  ·  TTL no expiry"},
		{KeyInfo{Type: TypeStream, TTLMillis: -1, Memory: -1, Size: 1}, "Stream  ·  1 entry  ·  TTL no expiry"},
	}
	for _, tc := range tests {
		if got := InfoLine(tc.info); got != tc.want {
			t.Errorf("InfoLine = %q, want %q", got, tc.want)
		}
	}
}

var _ extension.EditorTab = (*fakeapi.Tab)(nil)

func TestAnAnswerThatComesAfterTheTabClosedIsDropped(t *testing.T) {
	r := newRig(t, nil)
	r.connect()
	kt := r.openKeyTab(0, "greeting")
	kt.reload() // the answer is posted, and the tab closes before the UI thread takes it
	r.fakeTab(kt).Close()
	time.Sleep(50 * time.Millisecond)
	r.api.Pump() // would set the text of a disposed controller
	kt.reload()
	kt.connectionChanged()
	// an operation that was running
	kt2 := r.openKeyTab(0, "scores")
	kt2.run("Waiting", func(context.Context, DB) error { return nil }, func() { t.Error("done ran after the tab closed") })
	r.fakeTab(kt2).Close()
	time.Sleep(50 * time.Millisecond)
	r.api.Pump()
}
