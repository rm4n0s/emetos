package redis

import (
	"testing"

	"github.com/rm4n0s/emetos/extension/ui/uitest"
	"github.com/rm4n0s/emetos/extensions/internal/dbconn"
)

// The goldens are the dark and the light theme of each page; they are drawn by Glutter's software renderer, so they need no display.

func TestGoldenTheHashEditor(t *testing.T) {
	for _, th := range uitest.Themes() {
		t.Run(th.Name, func(t *testing.T) {
			r := newRig(t, nil)
			r.connect()
			kt := r.openKeyTab(0, "user:42:profile")
			ts := showKeyTab(t, th, kt, 760, 360)
			uitest.MatchesGolden(ts, "key_hash_"+th.Name)
		})
	}
}

func TestGoldenTheHashEditorWithItsForm(t *testing.T) {
	for _, th := range uitest.Themes() {
		t.Run(th.Name, func(t *testing.T) {
			r := newRig(t, nil)
			r.connect()
			kt := r.openKeyTab(0, "user:42:profile")
			selectRow(kt, 1)
			kt.openEditForm()
			ts := showKeyTab(t, th, kt, 760, 520)
			uitest.MatchesGolden(ts, "key_hash_form_"+th.Name)
		})
	}
}

func TestGoldenTheStringEditor(t *testing.T) {
	for _, th := range uitest.Themes() {
		t.Run(th.Name, func(t *testing.T) {
			r := newRig(t, nil)
			r.connect()
			kt := r.openKeyTab(0, "config:app")
			ts := showKeyTab(t, th, kt, 760, 300)
			uitest.MatchesGolden(ts, "key_string_json_"+th.Name)
		})
	}
}

func TestGoldenTheHexView(t *testing.T) {
	for _, th := range uitest.Themes() {
		t.Run(th.Name, func(t *testing.T) {
			r := newRig(t, nil)
			r.connect()
			r.store.put(0, "blob", &fakeKey{typ: TypeString, str: "\x00\x01Redis binary\xff\xfe value", ttl: 3_600_000})
			kt := r.openKeyTab(0, "blob")
			ts := showKeyTab(t, th, kt, 760, 240)
			uitest.MatchesGolden(ts, "key_string_hex_"+th.Name)
		})
	}
}

func TestGoldenTheListSortedSetAndStream(t *testing.T) {
	for _, th := range uitest.Themes() {
		for key, name := range map[string]string{"queue:jobs": "list", "scores": "zset", "events": "stream", "user:42:tags": "set"} {
			t.Run(th.Name+"/"+name, func(t *testing.T) {
				r := newRig(t, nil)
				r.connect()
				kt := r.openKeyTab(0, key)
				ts := showKeyTab(t, th, kt, 760, 300)
				uitest.MatchesGolden(ts, "key_"+name+"_"+th.Name)
			})
		}
	}
}

func TestGoldenAReadOnlyKey(t *testing.T) {
	for _, th := range uitest.Themes() {
		t.Run(th.Name, func(t *testing.T) {
			r := newRig(t, nil, func(c *dbconn.Connection) { c.ReadOnly = true })
			r.connect()
			kt := r.openKeyTab(0, "user:42:profile")
			ts := showKeyTab(t, th, kt, 760, 300)
			uitest.MatchesGolden(ts, "key_readonly_"+th.Name)
		})
	}
}

func TestGoldenTheNewKeyForm(t *testing.T) {
	for _, th := range uitest.Themes() {
		t.Run(th.Name, func(t *testing.T) {
			r := newRig(t, nil)
			r.connect()
			nt := r.openNewKey("user:42:")
			nt.name.SetValue("user:42:settings")
			nt.chooseType(1)
			nt.form.ctl[0].SetValue("theme")
			nt.form.ctl[1].SetValue("dark")
			ts := showNewKey(t, th, nt, 760, 640)
			uitest.MatchesGolden(ts, "newkey_hash_"+th.Name)
		})
	}
}
