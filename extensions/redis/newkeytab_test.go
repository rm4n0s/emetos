package redis

import (
	"context"
	"slices"
	"strings"
	"testing"

	"github.com/rm4n0s/errors"

	"github.com/rm4n0s/emetos/extensions/internal/dbconn"
)

// openNewKey opens the New Key form of a database and returns its tab.
func (r *rig) openNewKey(prefix string) *newKeyTab {
	r.t.Helper()
	n := len(r.ext.newKeyTabs)
	r.ext.newKey(r.conn.ID, 0, prefix)
	if len(r.ext.newKeyTabs) != n+1 {
		r.t.Fatalf("no New Key form was opened (%d tabs)", len(r.ext.newKeyTabs))
	}
	return r.ext.newKeyTabs[n]
}

func (r *rig) waitCreated(nt *newKeyTab) {
	r.t.Helper()
	r.api.WaitFor("the form to finish", func() bool { return !nt.busy })
}

func TestTheNewKeyFormStartsWithTheFolder(t *testing.T) {
	r := newRig(t, nil)
	r.connect()
	nt := r.openNewKey("user:42:")
	if nt.name.Value() != "user:42:" || nt.typ != 0 || len(nt.form.fields) != 1 || nt.form.fields[0].Label != "Value" {
		t.Errorf("name %q type %d fields %+v", nt.name.Value(), nt.typ, nt.form.fields)
	}
	ft := r.api.EditorTabs()[0]
	if ft.Title() != "New Key" || !strings.Contains(ft.Hint(), "cache db0") || !ft.Revealed {
		t.Errorf("title %q hint %q", ft.Title(), ft.Hint())
	}
	// the menu of a database and of a folder open it
	db0 := r.children(nil)[0]
	r.action(&db0, "New Key...")
	user := find(t, r.children(&db0), "user")
	r.action(&user, "New Key Here...")
	if len(r.ext.newKeyTabs) != 3 || r.ext.newKeyTabs[2].name.Value() != "user:" {
		t.Errorf("forms = %d", len(r.ext.newKeyTabs))
	}
}

func TestCreatingKeysOfEveryType(t *testing.T) {
	tests := []struct {
		typ    string
		fill   func(*rowForm)
		check  func(t *testing.T, db DB)
		fields int
	}{
		{TypeString, func(f *rowForm) { f.ctl[0].SetValue("hello") }, func(t *testing.T, db DB) {
			if got, _, _ := db.GetRange(context.Background(), "new", 10); got != "hello" {
				t.Errorf("value = %q", got)
			}
		}, 1},
		{TypeHash, func(f *rowForm) { f.ctl[0].SetValue("f"); f.ctl[1].SetValue("v") }, func(t *testing.T, db DB) {
			if f, _, _ := db.HScan(context.Background(), "new", 0, 10); len(f) != 1 {
				t.Errorf("fields = %+v", f)
			}
		}, 2},
		{TypeList, func(f *rowForm) { f.ctl[0].SetValue("first") }, func(t *testing.T, db DB) {
			if l, _ := db.LRange(context.Background(), "new", 0, -1); !slices.Equal(l, []string{"first"}) {
				t.Errorf("items = %q", l)
			}
		}, 1},
		{TypeSet, func(f *rowForm) { f.ctl[0].SetValue("m") }, func(t *testing.T, db DB) {
			if m, _, _ := db.SScan(context.Background(), "new", 0, 10); !slices.Equal(m, []string{"m"}) {
				t.Errorf("members = %q", m)
			}
		}, 1},
		{TypeZSet, func(f *rowForm) { f.ctl[0].SetValue("m"); f.ctl[1].SetValue("1.5") }, func(t *testing.T, db DB) {
			if m, _, _ := db.ZScan(context.Background(), "new", 0, 10); len(m) != 1 || m[0].Score != 1.5 {
				t.Errorf("members = %+v", m)
			}
		}, 2},
		{TypeStream, func(f *rowForm) { f.ctl[1].SetValue("a 1") }, func(t *testing.T, db DB) {
			if e, _ := db.XRevRange(context.Background(), "new", "+", 10); len(e) != 1 {
				t.Errorf("entries = %+v", e)
			}
		}, 2},
		{TypeJSON, func(f *rowForm) { f.ctl[0].SetValue(`{"a":1}`) }, func(t *testing.T, db DB) {
			if d, _ := db.JSONGet(context.Background(), "new"); d != `{"a":1}` {
				t.Errorf("doc = %q", d)
			}
		}, 1},
	}
	for i, tc := range tests {
		t.Run(tc.typ, func(t *testing.T) {
			r := newRig(t, nil)
			r.connect()
			db0 := r.children(nil)[0]
			r.children(&db0)
			nt := r.openNewKey("")
			nt.name.SetValue("new")
			nt.chooseType(i)
			if len(nt.form.fields) != tc.fields {
				t.Fatalf("fields = %d, want %d", len(nt.form.fields), tc.fields)
			}
			tc.fill(nt.form)
			nt.ttl.SetValue("300")
			nt.create()
			r.waitCreated(nt)
			r.api.Pump()
			db := mustDB(t, r.store, 0)
			tc.check(t, db)
			if info, _ := db.Info(context.Background(), "new"); info.Type != tc.typ || info.TTLMillis != 300_000 {
				t.Errorf("info = %+v", info)
			}
			// the form closes, the key opens in the editor and is in the explorer
			if !r.api.EditorTabs()[0].Closed {
				t.Error("the form is still open")
			}
			r.api.WaitFor("the key editor", func() bool { return len(r.keyTabs()) == 1 && r.keyTabs()[0].loaded })
			if r.keyTabs()[0].ref.Key != "new" {
				t.Errorf("opened %q", r.keyTabs()[0].ref.Key)
			}
			if !slices.Contains(labels(r.children(&db0)), "new") {
				t.Errorf("explorer = %q", labels(r.children(&db0)))
			}
		})
	}
}

func TestNewKeyMistakesStayInTheForm(t *testing.T) {
	r := newRig(t, nil)
	r.connect()
	nt := r.openNewKey("")
	nt.create()
	if !nt.note.Bad || nt.note.Text != "A key needs a name." {
		t.Errorf("note = %+v", nt.note)
	}
	nt.name.SetValue("k")
	nt.chooseType(1) // hash: Field is required
	nt.create()
	if !nt.note.Bad || nt.note.Text != "Field is required." {
		t.Errorf("note = %+v", nt.note)
	}
	nt.form.ctl[0].SetValue("f")
	nt.ttl.SetValue("soon")
	nt.create()
	if !nt.note.Bad || !strings.Contains(nt.note.Text, "time to live") {
		t.Errorf("note = %+v", nt.note)
	}
	nt.ttl.SetValue("")
	nt.name.SetValue("greeting") // taken
	nt.create()
	r.waitCreated(nt)
	if !nt.note.Bad || !strings.Contains(nt.note.Text, "a key with this name exists") {
		t.Errorf("note = %+v", nt.note)
	}
	if r.api.EditorTabs()[0].Closed {
		t.Error("the form closed after a failure")
	}
	r.store.fail["EXISTS"] = errors.New("TestBoom", "boom")
	nt.name.SetValue("other")
	nt.create()
	r.waitCreated(nt)
	if !nt.note.Bad || nt.note.Text != "boom" {
		t.Errorf("note = %+v", nt.note)
	}
	// a connection that closed
	if err := r.hub.Disconnect(r.conn.ID); err != nil {
		t.Fatal(err)
	}
	nt.create()
	r.waitCreated(nt)
	if !nt.note.Bad || !strings.Contains(nt.note.Text, "not open") {
		t.Errorf("note = %+v", nt.note)
	}
}

func TestAReadOnlyConnectionCannotMakeKeys(t *testing.T) {
	r := newRig(t, nil, func(c *dbconn.Connection) { c.ReadOnly = true })
	r.connect()
	db0 := r.children(nil)[0]
	if titles := r.actionTitles(&db0); slices.Contains(titles, "New Key...") {
		t.Errorf("menu = %q", titles)
	}
	nt := r.openNewKey("")
	nt.name.SetValue("k")
	nt.form.ctl[0].SetValue("v")
	nt.create()
	r.waitCreated(nt)
	if !nt.note.Bad || !strings.Contains(nt.note.Text, "read-only") {
		t.Errorf("note = %+v", nt.note)
	}
	if ok, _ := mustDB(t, r.store, 0).Exists(context.Background(), "k"); ok {
		t.Error("a read-only connection made a key")
	}
}

func TestTheNewKeyTabRemovesItselfWhenClosed(t *testing.T) {
	r := newRig(t, nil)
	r.connect()
	r.openNewKey("")
	r.api.EditorTabs()[0].Close()
	if len(r.ext.newKeyTabs) != 0 {
		t.Errorf("forms = %d", len(r.ext.newKeyTabs))
	}
}
