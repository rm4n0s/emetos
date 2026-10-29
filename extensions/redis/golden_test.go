package redis

import (
	"testing"
	"time"

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

// ---- step 21.2: the console, Pub/Sub, Monitor and the server information ----

func TestGoldenTheConsole(t *testing.T) {
	for _, th := range uitest.Themes() {
		t.Run(th.Name, func(t *testing.T) {
			store := fixtureServer()
			store.resp3 = true
			r := newRig(t, store)
			c := r.consoleWithReplies()
			c.input.SetValue("hget")
			r.ext.ensureCommandDocs(r.conn.ID)
			r.api.WaitFor("the commands", func() bool { return len(r.ext.commandDocs(r.conn.ID)) == len(fakeDocs) })
			ts := r.showPanel(th, ConsoleViewID, 820, 440)
			uitest.MatchesGolden(ts, "console_"+th.Name)
		})
	}
}

func TestGoldenTheConsoleAskingBeforeADangerousCommand(t *testing.T) {
	for _, th := range uitest.Themes() {
		t.Run(th.Name, func(t *testing.T) {
			r := newRig(t, nil)
			c := r.openConsole()
			r.run(c, "PING")
			c.submit("FLUSHALL")
			r.api.Pump()
			ts := r.showPanel(th, ConsoleViewID, 820, 260)
			uitest.MatchesGolden(ts, "console_confirm_"+th.Name)
		})
	}
}

func TestGoldenThePubSubTab(t *testing.T) {
	for _, th := range uitest.Themes() {
		t.Run(th.Name, func(t *testing.T) {
			r := newRig(t, nil)
			r.ext.zone = time.UTC
			r.store.now = func() time.Time { return time.Date(2026, 10, 29, 9, 15, 30, 123e6, time.UTC) }
			tab := r.openPubSub()
			r.subscribe(tab, "news", false)
			r.subscribe(tab, "sport.*", true)
			r.store.publish("news", "Go 1.27 is out")
			r.store.publish("sport.golf", "birdie on the 9th")
			r.store.publish("news", `{"id":7,"ok":true}`)
			r.waitMessages(tab, 3)
			ts := r.showPanel(th, PubSubViewID, 900, 340)
			uitest.MatchesGolden(ts, "pubsub_"+th.Name)
		})
	}
}

func TestGoldenTheMonitorTab(t *testing.T) {
	for _, th := range uitest.Themes() {
		t.Run(th.Name, func(t *testing.T) {
			r := newRig(t, nil)
			r.ext.zone = time.UTC
			tab := r.openMonitor()
			tab.start()
			r.api.WaitFor("the monitor", func() bool { return tab.running() })
			r.store.pushMonitor(monitorLine(1700000000.125, 0, "127.0.0.1:50210", `"set" "session:42" "x" "ex" "60"`))
			r.store.pushMonitor(monitorLine(1700000000.250, 0, "127.0.0.1:50210", `"get" "session:42"`))
			r.store.pushMonitor(monitorLine(1700000001.5, 3, "lua", `"incr" "hits"`))
			r.api.WaitFor("three lines", func() bool { return tab.lines.len() == 3 })
			ts := r.showPanel(th, MonitorViewID, 900, 300)
			uitest.MatchesGolden(ts, "monitor_"+th.Name)
		})
	}
}

func TestGoldenTheServerInformationTab(t *testing.T) {
	for _, th := range uitest.Themes() {
		t.Run(th.Name, func(t *testing.T) {
			store := fixtureServer()
			store.slow = []Reply{
				arr(num(7), num(1700000000), num(15500), arr(str("KEYS"), str("user:*")), str("127.0.0.1:50000"), str("app")),
				arr(num(6), num(1699999990), num(2300), arr(str("SORT"), str("queue:jobs")), str("127.0.0.1:50002"), str("")),
			}
			r := newRig(t, store)
			r.ext.zone = time.UTC
			tab := r.openInfo()
			tab.choose(infoViewSlow)
			r.api.WaitFor("the slow log", func() bool { return tab.view == infoViewSlow && !tab.loading })
			tab.at = time.Date(2026, 10, 29, 9, 30, 0, 0, time.UTC)
			ts := r.showInfo(th, tab, 900, 300)
			uitest.MatchesGolden(ts, "serverinfo_"+th.Name)
		})
	}
}
