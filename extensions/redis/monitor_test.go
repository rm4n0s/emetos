package redis

import (
	"strconv"
	"strings"
	"testing"
	"time"
)

func TestParseMonitorLine(t *testing.T) {
	tests := []struct {
		name, line, db, client, command string
		secs                            int64
		nanos                           int64
	}{
		{"a command", `1339518083.107412 [0 127.0.0.1:60866] "keys" "*"`, "0", "127.0.0.1:60866", `"keys" "*"`, 1339518083, 107412000},
		{"another database", `1700000000.000001 [3 10.0.0.2:5000] "get" "a b"`, "3", "10.0.0.2:5000", `"get" "a b"`, 1700000000, 1000},
		{"a script", `1700000001.5 [0 lua] "set" "k" "v"`, "0", "lua", `"set" "k" "v"`, 1700000001, 500000000},
		{"a unix socket", `1700000002.25 [1 unix:/run/redis.sock] "ping"`, "1", "unix:/run/redis.sock", `"ping"`, 1700000002, 250000000},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			l := ParseMonitorLine(tc.line)
			if l.DB != tc.db || l.Client != tc.client || l.Command != tc.command || l.Time.Unix() != tc.secs {
				t.Errorf("got %+v", l)
			}
			if d := l.Time.Nanosecond() - int(tc.nanos); d < -2000 || d > 2000 {
				t.Errorf("nanoseconds = %d, want %d", l.Time.Nanosecond(), tc.nanos)
			}
		})
	}
	for _, line := range []string{"OK", "", "abc [0 x] y", "1.5 no brackets", "1.5 [0 unclosed"} {
		l := ParseMonitorLine(line)
		if l.Command != line || l.DB != "" || time.Since(l.Time) > time.Minute {
			t.Errorf("%q: %+v", line, l)
		}
	}
}

// openMonitor connects the rig's connection and returns its Monitor tab.
func (r *rig) openMonitor() *monitorTab {
	r.t.Helper()
	r.connect()
	r.api.Pump()
	r.ext.setTarget(r.conn.ID)
	return r.ext.monitorFor(r.conn.ID)
}

func TestMonitorWarnsThatItSlowsTheServerAndStartsOnlyWhenAsked(t *testing.T) {
	r := newRig(t, nil)
	tab := r.openMonitor()
	tab.askStart()
	r.api.Pump()
	notes := r.api.Notes()
	if len(notes) != 1 || notes[0].Severity != "warn" || !strings.Contains(notes[0].Message, "slows the server down") || len(notes[0].Actions) != 1 || notes[0].Actions[0] != "Start" {
		t.Fatalf("notes = %+v", notes)
	}
	if tab.running() || len(r.store.mons) != 0 {
		t.Fatal("MONITOR started before the user answered")
	}
	r.api.RunNoteAction(0, "Start")
	r.api.WaitFor("the monitor", func() bool { return tab.running() })
	if len(r.store.mons) != 1 {
		t.Errorf("%d monitors", len(r.store.mons))
	}
	// asking again while it runs does nothing
	tab.askStart()
	r.api.Pump()
	if len(r.api.Notes()) != 1 {
		t.Errorf("notes = %+v", r.api.Notes())
	}
}

func TestMonitorStartsAtOnceWhenTheConfirmationsAreOff(t *testing.T) {
	r := newRig(t, nil)
	r.api.SetSetting(SettingConfirmDangerous, false)
	tab := r.openMonitor()
	tab.askStart()
	r.api.WaitFor("the monitor", func() bool { return tab.running() })
	if len(r.api.Notes()) != 0 {
		t.Errorf("notes = %+v", r.api.Notes())
	}
}

func TestMonitorShowsTheCommandsOfTheServer(t *testing.T) {
	r := newRig(t, nil)
	tab := r.openMonitor()
	tab.start()
	r.api.WaitFor("the monitor", func() bool { return tab.running() })
	r.store.pushMonitor(monitorLine(1700000000.5, 0, "127.0.0.1:1", `"set" "k" "v"`))
	r.store.pushMonitor(monitorLine(1700000001.5, 3, "lua", `"get" "k"`))
	r.api.WaitFor("two lines", func() bool { return tab.lines.len() == 2 })
	vals := make([]resultgridValue, 4)
	tab.table.sync()
	tab.table.Row(0, vals) // the newest first
	if vals[1].S != "3" || vals[2].S != "lua" || vals[3].S != `"get" "k"` {
		t.Errorf("row 0 = %+v", vals)
	}
	tab.stop()
	if tab.running() || !r.store.mons[0].closed() {
		t.Error("Stop did not end the monitor")
	}
	// a line that arrives after Stop is dropped
	r.store.pushMonitor("1.0 [0 x] \"late\"")
	r.api.Pump()
	if tab.lines.len() != 2 {
		t.Errorf("%d lines", tab.lines.len())
	}
}

func TestMonitorLinesAreBoundedAndPauseAndClearWork(t *testing.T) {
	r := newRig(t, nil)
	tab := r.openMonitor()
	tab.start()
	r.api.WaitFor("the monitor", func() bool { return tab.running() })
	for i := 0; i < maxMessages+50; i++ {
		r.store.pushMonitor(monitorLine(1700000000+float64(i), 0, "c", strconv.Quote("cmd"+strconv.Itoa(i))))
	}
	r.api.WaitFor("the flood", func() bool { return tab.lines.len()+int(tab.lines.dropped) >= maxMessages+50 })
	if tab.lines.len() != maxMessages || tab.lines.dropped != 50 {
		t.Errorf("%d held, %d dropped", tab.lines.len(), tab.lines.dropped)
	}
	tab.setPaused(true)
	r.store.pushMonitor(monitorLine(1800000000, 0, "c", `"after pause"`))
	r.api.WaitFor("the line", func() bool { return tab.lines.at(tab.lines.len()-1).Command == `"after pause"` })
	vals := make([]resultgridValue, 4)
	tab.table.Row(0, vals)
	if vals[3].S == `"after pause"` {
		t.Error("a paused list moved")
	}
	tab.setPaused(false)
	tab.table.Row(0, vals)
	if vals[3].S != `"after pause"` {
		t.Errorf("a resumed list shows %q", vals[3].S)
	}
	tab.clear()
	if tab.lines.len() != 0 || tab.table.Len() != 0 {
		t.Error("Clear leaves lines")
	}
}

func TestMonitorFailuresAreShown(t *testing.T) {
	t.Run("a cluster has no monitor", func(t *testing.T) {
		r := newRig(t, nil)
		tab := r.openMonitor()
		r.store.mu.Lock()
		r.store.mode = ModeCluster
		r.store.mu.Unlock()
		tab.start()
		r.api.WaitFor("the failure", func() bool { return tab.note.Bad })
		if !strings.Contains(tab.note.Text, "standalone") || tab.running() {
			t.Errorf("note = %+v", tab.note)
		}
	})
	t.Run("the connection breaks", func(t *testing.T) {
		r := newRig(t, nil)
		tab := r.openMonitor()
		tab.start()
		r.api.WaitFor("the monitor", func() bool { return tab.running() })
		mon := r.store.mons[0]
		mon.mu.Lock()
		mon.err = errBoom{}
		mon.mu.Unlock()
		mon.Close()
		r.api.WaitFor("the end", func() bool { return !tab.running() })
		if !tab.note.Bad {
			t.Errorf("note = %+v", tab.note)
		}
	})
	t.Run("the connection of the tab closes", func(t *testing.T) {
		r := newRig(t, nil)
		tab := r.openMonitor()
		tab.start()
		r.api.WaitFor("the monitor", func() bool { return tab.running() })
		if err := r.hub.Disconnect(r.conn.ID); err != nil {
			t.Fatal(err)
		}
		r.api.Pump()
		if tab.running() || !r.store.mons[0].closed() {
			t.Error("the monitor is still on")
		}
	})
}

func TestTheMonitorCommandsDriveTheTab(t *testing.T) {
	r := newRig(t, nil)
	r.api.SetSetting(SettingConfirmDangerous, false)
	r.connect()
	r.api.Pump()
	if err := r.api.Run(CmdStartMonitor, r.conn.ID); err != nil {
		t.Fatal(err)
	}
	tab := r.ext.monitorFor(r.conn.ID)
	r.api.WaitFor("the monitor", func() bool { return tab.running() })
	if err := r.api.Run(CmdStopMonitor, nil); err != nil {
		t.Fatal(err)
	}
	if tab.running() {
		t.Error("still running")
	}
	if err := r.api.Run(CmdOpenMonitor, nil); err != nil {
		t.Fatal(err)
	}
	if v := r.api.View(PanelViewID); v == nil || len(v.Reveals) == 0 {
		t.Error("the tab was not shown")
	}
}
