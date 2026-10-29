package redis

import (
	"context"
	"slices"
	"strings"
	"time"

	"github.com/rm4n0s/glutter/foundation"
	"github.com/rm4n0s/glutter/painting"
	"github.com/rm4n0s/glutter/rendering"
	"github.com/rm4n0s/glutter/widgets"

	"github.com/rm4n0s/emetos/extension"
	"github.com/rm4n0s/emetos/extension/ui"
	"github.com/rm4n0s/emetos/extensions/internal/pageui"
	"github.com/rm4n0s/emetos/extensions/internal/resultgrid"
)

// maxMessages is how many Pub/Sub messages and Monitor lines a tab keeps (SPEC §10.23: bounded).
const maxMessages = 1000

// subItem is a channel or a pattern a connection listens to.
type subItem struct {
	Pattern bool
	Name    string
}

// pubsubTab is the Pub/Sub tab of one connection: its subscriptions, the messages it received and the Publish form. It belongs to the
// UI thread.
type pubsubTab struct {
	x    *Extension
	conn string

	sub     Subscription
	opening bool
	items   []subItem
	// gen counts the connections opened, so that a message that arrives from one that was closed is dropped.
	gen int

	msgs   *ring[PubSubMessage]
	snap   []PubSubMessage
	paused bool
	feed   feeder[PubSubMessage]
	table  *msgSource
	grid   *resultgrid.Grid

	subInput, pubChannel, pubMessage *ui.InputBoxController
	note                             pageui.Note
}

// msgSource shows the messages to the grid, newest first. It reads the live ring, or the copy taken when the tab was paused.
type msgSource struct {
	t *pubsubTab
	n int
}

var msgColumns = []resultgrid.Column{{Name: "Time"}, {Name: "Channel"}, {Name: "Pattern"}, {Name: "Message"}}

func (s *msgSource) Columns() []resultgrid.Column { return msgColumns }
func (s *msgSource) Len() int                     { return s.n }

// sync takes the number of rows the tab has now: the grid reads only that many.
func (s *msgSource) sync() {
	if s.t.paused {
		s.n = len(s.t.snap)
	} else {
		s.n = s.t.msgs.len()
	}
}

func (s *msgSource) Row(i int, dst []resultgrid.Value) {
	var m PubSubMessage
	if s.t.paused {
		if i >= len(s.t.snap) {
			return
		}
		m = s.t.snap[len(s.t.snap)-1-i]
	} else {
		if i >= s.t.msgs.len() {
			return
		}
		m = s.t.msgs.at(s.t.msgs.len() - 1 - i)
	}
	dst[0] = resultgrid.Text(m.Time.In(s.t.x.loc()).Format("15:04:05.000"))
	dst[1] = resultgrid.Text(m.Channel)
	dst[2] = resultgrid.Text(m.Pattern) // the grid reuses dst: every column is written
	dst[3] = resultgrid.Text(m.Payload)
}

// pubsubFor returns the Pub/Sub tab state of a connection, made on first use.
func (x *Extension) pubsubFor(conn string) *pubsubTab {
	if t, ok := x.pubsubs[conn]; ok {
		return t
	}
	t := &pubsubTab{x: x, conn: conn, msgs: newRing[PubSubMessage](maxMessages),
		subInput: ui.NewInputBoxController(""), pubChannel: ui.NewInputBoxController(""), pubMessage: ui.NewInputBoxController("")}
	t.table = &msgSource{t: t}
	t.grid = resultgrid.New(resultgrid.Options{OnCopy: x.copyText})
	t.grid.SetSource(t.table)
	setColumnWidths(t.grid, 135, 170, 130, 520)
	x.pubsubs[conn] = t
	return t
}

// refreshPubSub builds the tab again.
func (x *Extension) refreshPubSub() {
	x.refreshPage(PubSubViewID)
}

// connect opens the Pub/Sub connection of the tab, then runs then on the UI thread. It does nothing while one is being opened.
func (t *pubsubTab) connect(then func()) {
	if t.sub != nil {
		then()
		return
	}
	if t.opening {
		return
	}
	t.opening = true
	t.gen++
	gen := t.gen
	x := t.x
	go func() {
		ctx, cancel := context.WithTimeout(x.ctx, opTimeout)
		defer cancel()
		var sub Subscription
		s, err := x.drv.session(t.conn)
		if err == nil {
			sub, err = s.server.PubSub(ctx)
		}
		x.api.Post(func() {
			t.opening = false
			if err != nil {
				x.api.Log().Warn("Pub/Sub cannot be opened", "err", err)
				t.note = pageui.Bad("Cannot open Pub/Sub: " + errorText(err))
				x.refreshPubSub()
				return
			}
			if gen != t.gen || x.ctx.Err() != nil {
				sub.Close() // the tab moved on: nobody wants this connection
				return
			}
			t.sub = sub
			x.track(sub)
			go t.pump(sub, gen)
			then()
		})
	}()
}

// pump reads the messages of a subscription and hands them to the UI thread in batches.
func (t *pubsubTab) pump(sub Subscription, gen int) {
	x := t.x
	for m := range sub.Messages() {
		if t.feed.add(m) {
			x.api.Post(func() { t.flush(gen) })
		}
	}
	x.api.Post(func() { t.ended(sub, gen) })
}

// flush moves the queued messages into the ring.
func (t *pubsubTab) flush(gen int) {
	batch, lost := t.feed.take()
	if gen != t.gen {
		return
	}
	for _, m := range batch {
		t.msgs.push(m)
	}
	t.msgs.dropped += lost
	if t.paused {
		return
	}
	t.table.sync()
	t.grid.Appended()
	t.x.refreshPubSub()
}

// ended follows a subscription whose messages stopped: a connection that failed shows why.
func (t *pubsubTab) ended(sub Subscription, gen int) {
	if gen != t.gen || t.sub != sub {
		return
	}
	t.sub = nil
	t.items = nil
	t.x.untrack(sub)
	if err := sub.Err(); err != nil {
		t.x.api.Log().Warn("a Pub/Sub connection ended", "err", err)
		t.note = pageui.Bad("The Pub/Sub connection ended: " + errorText(err) + " Subscribe again to reconnect.")
	}
	t.x.refreshPubSub()
}

// subscribe listens to a channel, or to a pattern.
func (t *pubsubTab) subscribe(name string, pattern bool) {
	name = strings.TrimSpace(name)
	if name == "" {
		t.note = pageui.Bad("Type a channel" + map[bool]string{true: " pattern", false: ""}[pattern] + " first.")
		t.x.refreshPubSub()
		return
	}
	item := subItem{Pattern: pattern, Name: name}
	if slices.Contains(t.items, item) {
		t.note = pageui.Fine("Already subscribed to " + name + ".")
		t.x.refreshPubSub()
		return
	}
	t.connect(func() {
		sub := t.sub
		x := t.x
		go func() {
			ctx, cancel := context.WithTimeout(x.ctx, opTimeout)
			defer cancel()
			var err error
			if pattern {
				err = sub.PSubscribe(ctx, name)
			} else {
				err = sub.Subscribe(ctx, name)
			}
			x.api.Post(func() {
				if err != nil {
					x.api.Log().Warn("a subscription failed", "err", err)
					t.note = pageui.Bad("Cannot subscribe: " + errorText(err))
				} else if t.sub == sub {
					t.items = append(t.items, item)
					t.note = pageui.Note{}
					t.subInput.SetValue("")
				}
				x.refreshPubSub()
			})
		}()
	})
}

// unsubscribe stops listening to a channel or a pattern.
func (t *pubsubTab) unsubscribe(item subItem) {
	sub := t.sub
	if sub == nil {
		return
	}
	x := t.x
	go func() {
		ctx, cancel := context.WithTimeout(x.ctx, opTimeout)
		defer cancel()
		var err error
		if item.Pattern {
			err = sub.PUnsubscribe(ctx, item.Name)
		} else {
			err = sub.Unsubscribe(ctx, item.Name)
		}
		x.api.Post(func() {
			if err != nil {
				x.api.Log().Warn("an unsubscribe failed", "err", err)
				t.note = pageui.Bad("Cannot unsubscribe: " + errorText(err))
			} else if i := slices.Index(t.items, item); i >= 0 && t.sub == sub {
				t.items = slices.Delete(t.items, i, i+1)
			}
			x.refreshPubSub()
		})
	}()
}

// publish sends a message on a channel with PUBLISH through the console's path, so that a read-only connection refuses it.
func (t *pubsubTab) publish(channel, message string) {
	channel = strings.TrimSpace(channel)
	if channel == "" {
		t.note = pageui.Bad("Type the channel to publish on.")
		t.x.refreshPubSub()
		return
	}
	x := t.x
	conn, db := t.conn, x.consoleDB(t.conn)
	go func() {
		ctx, cancel := context.WithTimeout(x.ctx, opTimeout)
		defer cancel()
		start := time.Now()
		d, _, err := x.drv.database(conn, db)
		var rep Reply
		if err == nil {
			rep, err = d.Do(ctx, []string{"PUBLISH", channel, message})
		}
		ms := time.Since(start).Milliseconds()
		x.api.Post(func() {
			ok := err == nil && !rep.IsError()
			switch {
			case err != nil:
				t.note = pageui.Bad("Cannot publish: " + errorText(err))
			case rep.IsError():
				t.note = pageui.Bad("Cannot publish: " + rep.Str)
			default:
				t.note = pageui.Fine("Published to " + plural(rep.Int, "receiver", "receivers") + ".")
				t.pubMessage.SetValue("")
			}
			x.publishCommandFinished(conn, ok, ms)
			x.refreshPubSub()
		})
	}()
}

// setPaused freezes the list of messages, or lets it follow the ring again. The ring keeps receiving while the list is frozen.
func (t *pubsubTab) setPaused(p bool) {
	if t.paused == p {
		return
	}
	t.paused = p
	if p {
		t.snap = t.msgs.snapshot()
	} else {
		t.snap = nil
	}
	t.table.sync()
	t.grid.Appended()
	t.x.refreshPubSub()
}

// clear forgets the messages.
func (t *pubsubTab) clear() {
	t.msgs.clear()
	t.snap = nil
	t.table.sync()
	t.grid.SetSource(t.table)
	setColumnWidths(t.grid, 135, 170, 130, 520)
	t.x.refreshPubSub()
}

// close ends the Pub/Sub connection.
func (t *pubsubTab) close() {
	t.gen++
	sub := t.sub
	t.sub, t.items, t.opening = nil, nil, false
	if sub != nil {
		t.x.untrack(sub)
		sub.Close()
	}
	t.feed.take()
}

// buildPubSub makes the widget of the Redis Pub/Sub tab.
func (x *Extension) buildPubSub(bc widgets.BuildContext) widgets.Widget {
	colors := ui.Theme(bc)
	conn := x.chooseTarget()
	if conn == "" {
		return widgets.NewColoredBox(colors.PanelBackground, x.noConnection(bc, colors))
	}
	t := x.pubsubFor(conn)
	c, _ := x.hub.Store().Get(conn)
	readOnly := c.ReadOnly
	count := plural(int64(t.msgs.len()), "message", "messages")
	if t.msgs.dropped > 0 {
		count += ", " + Commas(t.msgs.dropped) + " older dropped"
	}
	if t.paused {
		count += ", paused"
	}
	bar := tabBar(
		x.connPicker(conn, func(id string) { x.setTarget(id); x.refreshPubSub() }), pageui.HGap(10), muted(bc, colors, count), widgets.NewSpacer(),
		small(map[bool]string{true: "Resume", false: "Pause"}[t.paused], true, false, func() { t.setPaused(!t.paused) }), pageui.HGap(6),
		small("Clear", true, t.msgs.len() == 0, t.clear),
	)
	subscribe := widgets.NewPadding(painting.NewEdgeInsetsFromLTRB(12, 0, 12, 4), widgets.NewRow([]widgets.Widget{
		widgets.NewSizedBox(ui.NewInputBox(nil, nil, ui.InputBoxOpts{Controller: t.subInput, Placeholder: "Channel or pattern (news.*)",
			OnSubmit: func(s string) { t.subscribe(s, false) }}), widgets.SizedBoxOpts{Width: foundation.Some(260.0)}), pageui.HGap(8),
		small("Subscribe", false, false, func() { t.subscribe(t.subInput.Value(), false) }), pageui.HGap(6),
		small("PSubscribe", true, false, func() { t.subscribe(t.subInput.Value(), true) }), pageui.HGap(12),
		widgets.NewFlexible(t.chips(bc, colors)),
	}, widgets.RowOpts{CrossAxisAlignment: rendering.CrossAxisAlignmentCenter}))
	publish := widgets.NewPadding(painting.NewEdgeInsetsFromLTRB(12, 0, 12, 6), widgets.NewRow([]widgets.Widget{
		widgets.NewSizedBox(ui.NewInputBox(nil, nil, ui.InputBoxOpts{Controller: t.pubChannel, Placeholder: "Channel to publish on", Disabled: readOnly}),
			widgets.SizedBoxOpts{Width: foundation.Some(260.0)}), pageui.HGap(8),
		widgets.NewExpanded(ui.NewInputBox(nil, nil, ui.InputBoxOpts{Controller: t.pubMessage, Placeholder: "Message", Disabled: readOnly,
			OnSubmit: func(string) { t.publish(t.pubChannel.Value(), t.pubMessage.Value()) }})), pageui.HGap(8),
		small("Publish", false, readOnly, func() { t.publish(t.pubChannel.Value(), t.pubMessage.Value()) }),
	}, widgets.RowOpts{CrossAxisAlignment: rendering.CrossAxisAlignmentCenter}))
	rows := []widgets.Widget{bar, subscribe, publish}
	if readOnly {
		rows = append(rows, widgets.NewPadding(painting.NewEdgeInsetsFromLTRB(12, 0, 12, 4), muted(bc, colors, "Read-only connection: publishing is off.")))
	}
	if t.note.Text != "" {
		rows = append(rows, widgets.NewPadding(painting.NewEdgeInsetsFromLTRB(12, 0, 12, 4), pageui.NoteLine(bc, colors, t.note)))
	}
	rows = append(rows, hline(colors), widgets.NewExpanded(t.body(bc, colors)))
	return widgets.NewColoredBox(colors.PanelBackground, widgets.NewColumn(rows, widgets.ColumnOpts{CrossAxisAlignment: rendering.CrossAxisAlignmentStretch}))
}

// chips are the subscriptions, each with a button that stops it.
func (t *pubsubTab) chips(bc widgets.BuildContext, colors ui.Colors) widgets.Widget {
	if len(t.items) == 0 {
		if t.opening {
			return muted(bc, colors, "Connecting...")
		}
		return muted(bc, colors, "Not subscribed to anything.")
	}
	var parts []widgets.Widget
	for _, it := range t.items {
		label := it.Name
		if it.Pattern {
			label = "pattern " + it.Name
		}
		item := it
		parts = append(parts, widgets.NewText(label, widgets.TextOpts{Style: ui.TextStyle(bc, 12, colors.Foreground)}),
			ui.NewIconButton("close", ui.IconButtonOpts{Tooltip: "Unsubscribe from " + it.Name, OnPressed: func() { t.unsubscribe(item) }}), pageui.HGap(6))
	}
	return widgets.NewWrap(parts, widgets.WrapOpts{CrossAxisAlignment: rendering.WrapCrossAlignmentCenter})
}

// body is the grid of messages, or a line that says there are none.
func (t *pubsubTab) body(bc widgets.BuildContext, colors ui.Colors) widgets.Widget {
	if t.msgs.len() == 0 {
		return widgets.NewPadding(painting.NewEdgeInsetsFromLTRB(12, 10, 12, 0), widgets.NewAlign(
			widgets.NewText("No message yet. Subscribe to a channel, or press Publish to send one to yourself.",
				widgets.TextOpts{Style: ui.TextStyle(bc, 12, colors.DescriptionForeground)}), widgets.AlignOpts{Alignment: painting.AlignmentTopLeft}))
	}
	return t.grid.Widget()
}

// subscribePrompt asks for a channel or a pattern and subscribes, from the command palette.
func (x *Extension) subscribePrompt(pattern bool) {
	conn := x.chooseTarget()
	if conn == "" {
		x.api.Notifications().Warn("Connect a Redis connection in the Databases view first.")
		return
	}
	prompt := "Channel to subscribe to"
	if pattern {
		prompt = "Pattern of channels to subscribe to (news.*)"
	}
	x.showPanel(PubSubViewID)
	err := x.api.InputBox().Show(extension.InputBoxOptions{Prompt: prompt}, func(value string, ok bool) {
		if ok {
			x.pubsubFor(conn).subscribe(value, pattern)
		}
	})
	if err != nil {
		x.api.Log().Warn("the channel cannot be asked", "err", err)
	}
}

// publishPrompt asks for a channel and a message and publishes, from the command palette.
func (x *Extension) publishPrompt() {
	conn := x.chooseTarget()
	if conn == "" {
		x.api.Notifications().Warn("Connect a Redis connection in the Databases view first.")
		return
	}
	x.showPanel(PubSubViewID)
	err := x.api.InputBox().Show(extension.InputBoxOptions{Prompt: "Channel to publish on"}, func(channel string, ok bool) {
		if !ok {
			return
		}
		err := x.api.InputBox().Show(extension.InputBoxOptions{Prompt: "Message"}, func(message string, ok bool) {
			if ok {
				x.pubsubFor(conn).publish(channel, message)
			}
		})
		if err != nil {
			x.api.Log().Warn("the message cannot be asked", "err", err)
		}
	})
	if err != nil {
		x.api.Log().Warn("the channel cannot be asked", "err", err)
	}
}
