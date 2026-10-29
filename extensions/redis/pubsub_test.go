package redis

import (
	"strconv"
	"strings"
	"testing"

	"github.com/rm4n0s/emetos/extensions/internal/dbconn"
	"github.com/rm4n0s/emetos/extensions/internal/resultgrid"
)

type resultgridValue = resultgrid.Value

// openPubSub connects the rig's connection and returns its Pub/Sub tab.
func (r *rig) openPubSub() *pubsubTab {
	r.t.Helper()
	r.connect()
	r.api.Pump()
	r.ext.setTarget(r.conn.ID)
	return r.ext.pubsubFor(r.conn.ID)
}

// subscribe subscribes and waits until the subscription is listed.
func (r *rig) subscribe(t *pubsubTab, name string, pattern bool) {
	r.t.Helper()
	before := len(t.items)
	t.subscribe(name, pattern)
	r.api.WaitFor("the subscription to "+name, func() bool { return len(t.items) > before })
}

// waitMessages waits until the tab holds n messages.
func (r *rig) waitMessages(t *pubsubTab, n int) {
	r.t.Helper()
	r.api.WaitFor(strconv.Itoa(n)+" messages", func() bool { return t.msgs.len() >= n })
}

func TestSubscribingShowsTheMessagesOfAChannel(t *testing.T) {
	r := newRig(t, nil)
	tab := r.openPubSub()
	r.subscribe(tab, "news", false)
	if len(tab.items) != 1 || tab.items[0] != (subItem{Name: "news"}) {
		t.Fatalf("items = %+v", tab.items)
	}
	if n := r.store.publish("news", "hello"); n != 1 {
		t.Fatalf("receivers = %d", n)
	}
	r.store.publish("other", "ignored")
	r.store.publish("news", "second")
	r.waitMessages(tab, 2)
	first, second := tab.msgs.at(0), tab.msgs.at(1)
	if first.Channel != "news" || first.Payload != "hello" || first.Pattern != "" || second.Payload != "second" {
		t.Errorf("messages = %+v, %+v", first, second)
	}
	// the grid shows the newest first
	dst := make([]string, 4)
	vals := make([]resultgridValue, 4)
	tab.table.sync()
	tab.table.Row(0, vals)
	for i, v := range vals {
		dst[i] = v.S
	}
	if dst[1] != "news" || dst[3] != "second" {
		t.Errorf("row 0 = %q", dst)
	}
}

func TestAPatternSubscriptionKeepsThePatternOfEachMessage(t *testing.T) {
	r := newRig(t, nil)
	tab := r.openPubSub()
	r.subscribe(tab, "news.*", true)
	r.store.publish("news.sport", "goal")
	r.waitMessages(tab, 1)
	if m := tab.msgs.at(0); m.Channel != "news.sport" || m.Pattern != "news.*" || m.Payload != "goal" {
		t.Errorf("message = %+v", m)
	}
}

func TestPublishingThroughTheTabReachesTheSubscribers(t *testing.T) {
	r := newRig(t, nil)
	tab := r.openPubSub()
	r.subscribe(tab, "news", false)
	tab.publish("news", "from the tab")
	r.api.WaitFor("the answer", func() bool { return strings.Contains(tab.note.Text, "Published") })
	if tab.note.Text != "Published to 1 receiver." {
		t.Errorf("note = %+v", tab.note)
	}
	r.waitMessages(tab, 1)
	if m := tab.msgs.at(0); m.Payload != "from the tab" {
		t.Errorf("message = %+v", m)
	}
	tab.publish("  ", "x")
	if !tab.note.Bad {
		t.Errorf("note = %+v", tab.note)
	}
	// the PUBLISH is a command of the console: an event too
	if got := published[CommandFinished](t, r, TopicCommandFinished); len(got) != 1 || !got[0].OK {
		t.Errorf("commandFinished = %+v", got)
	}
}

func TestAReadOnlyConnectionCannotPublish(t *testing.T) {
	r := newRig(t, nil, func(c *dbconn.Connection) { c.ReadOnly = true })
	tab := r.openPubSub()
	r.subscribe(tab, "news", false)
	tab.publish("news", "x")
	r.api.WaitFor("the answer", func() bool { return tab.note.Bad })
	if !strings.Contains(tab.note.Text, "read-only") || r.store.seen("PUBLISH") {
		t.Errorf("note = %+v, commands %q", tab.note, r.store.commands())
	}
}

func TestUnsubscribeStopsTheMessages(t *testing.T) {
	r := newRig(t, nil)
	tab := r.openPubSub()
	r.subscribe(tab, "a", false)
	r.subscribe(tab, "b", false)
	tab.subscribe("a", false) // twice
	if len(tab.items) != 2 || tab.note.Text != "Already subscribed to a." {
		t.Fatalf("items = %+v, note %+v", tab.items, tab.note)
	}
	tab.unsubscribe(subItem{Name: "a"})
	r.api.WaitFor("the unsubscribe", func() bool { return len(tab.items) == 1 })
	if n := r.store.publish("a", "x"); n != 0 {
		t.Errorf("a was heard by %d", n)
	}
	if n := r.store.publish("b", "x"); n != 1 {
		t.Errorf("b was heard by %d", n)
	}
	tab.subscribe("", false)
	if !tab.note.Bad {
		t.Errorf("note = %+v", tab.note)
	}
}

func TestTheMessagesAreBounded(t *testing.T) {
	r := newRig(t, nil)
	tab := r.openPubSub()
	r.subscribe(tab, "flood", false)
	const total = maxMessages + 300
	for i := 0; i < total; i++ {
		r.store.publish("flood", strconv.Itoa(i))
	}
	r.api.WaitFor("the flood", func() bool { return tab.msgs.len()+int(tab.msgs.dropped) >= total })
	if tab.msgs.len() != maxMessages || tab.msgs.dropped != 300 {
		t.Errorf("%d held, %d dropped", tab.msgs.len(), tab.msgs.dropped)
	}
	if last := tab.msgs.at(maxMessages - 1); last.Payload != strconv.Itoa(total-1) {
		t.Errorf("last = %+v", last)
	}
}

func TestPauseFreezesTheListAndResumeCatchesUp(t *testing.T) {
	r := newRig(t, nil)
	tab := r.openPubSub()
	r.subscribe(tab, "c", false)
	r.store.publish("c", "1")
	r.waitMessages(tab, 1)
	tab.setPaused(true)
	r.store.publish("c", "2")
	r.store.publish("c", "3")
	r.waitMessages(tab, 3)
	if tab.table.Len() != 1 {
		t.Errorf("a paused list shows %d rows", tab.table.Len())
	}
	vals := make([]resultgridValue, 4)
	tab.table.Row(0, vals)
	if vals[3].S != "1" {
		t.Errorf("paused row = %q", vals[3].S)
	}
	tab.setPaused(false)
	if tab.table.Len() != 3 {
		t.Errorf("a resumed list shows %d rows", tab.table.Len())
	}
	tab.table.Row(0, vals)
	if vals[3].S != "3" {
		t.Errorf("newest row = %q", vals[3].S)
	}
}

func TestClearForgetsTheMessages(t *testing.T) {
	r := newRig(t, nil)
	tab := r.openPubSub()
	r.subscribe(tab, "c", false)
	r.store.publish("c", "1")
	r.waitMessages(tab, 1)
	tab.clear()
	if tab.msgs.len() != 0 || tab.table.Len() != 0 {
		t.Errorf("%d messages, %d rows", tab.msgs.len(), tab.table.Len())
	}
}

func TestACloseConnectionEndsPubSub(t *testing.T) {
	r := newRig(t, nil)
	tab := r.openPubSub()
	r.subscribe(tab, "c", false)
	sub := r.store.subs[0]
	if err := r.hub.Disconnect(r.conn.ID); err != nil {
		t.Fatal(err)
	}
	r.api.Pump()
	if !sub.closed() || tab.sub != nil || len(tab.items) != 0 {
		t.Errorf("closed %v, sub %v, items %+v", sub.closed(), tab.sub, tab.items)
	}
}

func TestAPubSubConnectionThatBreaksSaysSo(t *testing.T) {
	r := newRig(t, nil)
	tab := r.openPubSub()
	r.subscribe(tab, "c", false)
	r.store.subs[0].fail(errBoom{})
	r.api.WaitFor("the end", func() bool { return tab.sub == nil })
	if !tab.note.Bad || !strings.Contains(tab.note.Text, "Subscribe again") || len(tab.items) != 0 {
		t.Errorf("note = %+v", tab.note)
	}
	// subscribing again opens a new connection
	r.subscribe(tab, "c", false)
	if len(r.store.subs) != 2 {
		t.Errorf("%d connections", len(r.store.subs))
	}
}

func TestDeactivatingTheExtensionClosesPubSubAndMonitor(t *testing.T) {
	r := newRig(t, nil)
	tab := r.openPubSub()
	r.subscribe(tab, "c", false)
	mon := r.ext.monitorFor(r.conn.ID)
	mon.start()
	r.api.WaitFor("the monitor", func() bool { return mon.running() })
	r.cancel()
	r.api.WaitFor("both to close", func() bool { return r.store.subs[0].closed() && r.store.mons[0].closed() })
}

func TestPubSubCannotBeOpenedOnAClosedServer(t *testing.T) {
	r := newRig(t, nil)
	tab := r.openPubSub()
	r.store.mu.Lock()
	r.store.closed = true
	r.store.mu.Unlock()
	tab.subscribe("c", false)
	r.api.WaitFor("the failure", func() bool { return tab.note.Bad })
	if !strings.Contains(tab.note.Text, "Cannot open Pub/Sub") {
		t.Errorf("note = %+v", tab.note)
	}
}

func TestTheSubscribeCommandsAskForTheChannel(t *testing.T) {
	r := newRig(t, nil)
	tab := r.openPubSub()
	if err := r.api.Run(CmdSubscribe, nil); err != nil {
		t.Fatal(err)
	}
	in := r.api.LastInput()
	if in == nil || !strings.Contains(in.Options().Prompt, "Channel") {
		t.Fatalf("input = %+v", in)
	}
	in.Accept("alerts")
	r.api.WaitFor("the subscription", func() bool { return len(tab.items) == 1 })
	if err := r.api.Run(CmdPSubscribe, nil); err != nil {
		t.Fatal(err)
	}
	r.api.LastInput().Accept("alerts.*")
	r.api.WaitFor("the pattern", func() bool { return len(tab.items) == 2 && tab.items[1].Pattern })
	if err := r.api.Run(CmdPublish, nil); err != nil {
		t.Fatal(err)
	}
	r.api.LastInput().Accept("alerts")
	r.api.LastInput().Accept("fire")
	r.api.WaitFor("the message", func() bool { return tab.msgs.len() >= 1 })
	if m := tab.msgs.at(0); m.Payload != "fire" || m.Channel != "alerts" {
		t.Errorf("message = %+v", m)
	}
}
