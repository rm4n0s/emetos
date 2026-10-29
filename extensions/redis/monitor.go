package redis

import (
	"context"
	"strconv"
	"strings"
	"time"

	"github.com/rm4n0s/glutter/painting"
	"github.com/rm4n0s/glutter/rendering"
	"github.com/rm4n0s/glutter/widgets"

	"github.com/rm4n0s/emetos/extension"
	"github.com/rm4n0s/emetos/extension/ui"
	"github.com/rm4n0s/emetos/extensions/internal/pageui"
	"github.com/rm4n0s/emetos/extensions/internal/resultgrid"
)

// MonitorLine is a line of MONITOR read into its parts.
type MonitorLine struct {
	// Time is the time the server gave.
	Time time.Time
	// DB is the database of the command, Client who sent it ("127.0.0.1:60866", "lua", "unix:/run/redis.sock") and Command the command
	// with its arguments as the server wrote them.
	DB, Client, Command string
}

// ParseMonitorLine reads `1339518083.107412 [0 127.0.0.1:60866] "keys" "*"`. A line that does not look like that is kept whole in
// Command, with the time of now.
func ParseMonitorLine(line string) MonitorLine {
	stamp, rest, ok := strings.Cut(line, " ")
	secs, err := strconv.ParseFloat(stamp, 64)
	if !ok || err != nil || !strings.HasPrefix(rest, "[") {
		return MonitorLine{Time: time.Now(), Command: line}
	}
	end := strings.Index(rest, "]")
	if end < 0 {
		return MonitorLine{Time: time.Now(), Command: line}
	}
	db, client, _ := strings.Cut(rest[1:end], " ")
	whole := int64(secs)
	return MonitorLine{
		Time: time.Unix(whole, int64((secs-float64(whole))*1e9)), DB: db, Client: client, Command: strings.TrimSpace(rest[end+1:]),
	}
}

// monitorTab is the Monitor tab of one connection. It belongs to the UI thread.
type monitorTab struct {
	x    *Extension
	conn string

	mon      Monitor
	starting bool
	gen      int

	lines  *ring[MonitorLine]
	snap   []MonitorLine
	paused bool
	feed   feeder[MonitorLine]
	table  *monitorSource
	grid   *resultgrid.Grid
	note   pageui.Note
}

// monitorSource shows the lines to the grid, newest first.
type monitorSource struct {
	t *monitorTab
	n int
}

var monitorColumns = []resultgrid.Column{{Name: "Time"}, {Name: "DB"}, {Name: "Client"}, {Name: "Command"}}

func (s *monitorSource) Columns() []resultgrid.Column { return monitorColumns }
func (s *monitorSource) Len() int                     { return s.n }

// sync takes the number of rows the tab has now.
func (s *monitorSource) sync() {
	if s.t.paused {
		s.n = len(s.t.snap)
	} else {
		s.n = s.t.lines.len()
	}
}

func (s *monitorSource) Row(i int, dst []resultgrid.Value) {
	var l MonitorLine
	if s.t.paused {
		if i >= len(s.t.snap) {
			return
		}
		l = s.t.snap[len(s.t.snap)-1-i]
	} else {
		if i >= s.t.lines.len() {
			return
		}
		l = s.t.lines.at(s.t.lines.len() - 1 - i)
	}
	dst[0] = resultgrid.Text(l.Time.In(s.t.x.loc()).Format("15:04:05.000000"))
	dst[1] = resultgrid.Text(l.DB)
	dst[2] = resultgrid.Text(l.Client)
	dst[3] = resultgrid.Text(l.Command)
}

// monitorFor returns the Monitor tab state of a connection, made on first use.
func (x *Extension) monitorFor(conn string) *monitorTab {
	if t, ok := x.monitors[conn]; ok {
		return t
	}
	t := &monitorTab{x: x, conn: conn, lines: newRing[MonitorLine](maxMessages)}
	t.table = &monitorSource{t: t}
	t.grid = resultgrid.New(resultgrid.Options{OnCopy: x.copyText})
	t.grid.SetSource(t.table)
	setColumnWidths(t.grid, 175, 50, 190, 520)
	x.monitors[conn] = t
	return t
}

// refreshMonitor builds the tab again.
func (x *Extension) refreshMonitor() {
	x.refreshPage(MonitorViewID)
}

// running tells whether MONITOR is on.
func (t *monitorTab) running() bool { return t.mon != nil }

// askStart starts MONITOR after a warning, unless the user turned the confirmations off (SPEC §10.23: MONITOR warns that it slows the
// server down).
func (t *monitorTab) askStart() {
	if t.running() || t.starting {
		return
	}
	if !t.x.confirmDangerous() {
		t.start()
		return
	}
	t.x.api.Notifications().Warn("MONITOR shows every command the server runs, and it slows the server down while it runs. Start it on \""+
		t.x.connectionName(t.conn)+"\"?", extension.NotificationAction{Label: "Start", Run: t.start})
}

// start opens the monitor connection.
func (t *monitorTab) start() {
	if t.running() || t.starting {
		return
	}
	t.starting = true
	t.gen++
	gen := t.gen
	x := t.x
	t.note = pageui.Fine("Starting...")
	x.refreshMonitor()
	go func() {
		ctx, cancel := context.WithTimeout(x.ctx, opTimeout)
		defer cancel()
		var mon Monitor
		s, err := x.drv.session(t.conn)
		if err == nil {
			mon, err = s.server.Monitor(ctx)
		}
		x.api.Post(func() {
			t.starting = false
			if err != nil {
				x.api.Log().Warn("MONITOR cannot be started", "err", err)
				t.note = pageui.Bad("Cannot start MONITOR: " + errorText(err))
				x.refreshMonitor()
				return
			}
			if gen != t.gen || x.ctx.Err() != nil {
				mon.Close() // the tab moved on
				return
			}
			t.mon = mon
			x.track(mon)
			t.note = pageui.Note{}
			go t.pump(mon, gen)
			x.refreshMonitor()
		})
	}()
}

// pump reads the lines of the monitor and hands them to the UI thread in batches.
func (t *monitorTab) pump(mon Monitor, gen int) {
	x := t.x
	for line := range mon.Lines() {
		if t.feed.add(ParseMonitorLine(line)) {
			x.api.Post(func() { t.flush(gen) })
		}
	}
	x.api.Post(func() { t.ended(mon, gen) })
}

// flush moves the queued lines into the ring.
func (t *monitorTab) flush(gen int) {
	batch, lost := t.feed.take()
	if gen != t.gen {
		return
	}
	for _, l := range batch {
		t.lines.push(l)
	}
	t.lines.dropped += lost
	if t.paused {
		return
	}
	t.table.sync()
	t.grid.Appended()
	t.x.refreshMonitor()
}

// ended follows a monitor whose lines stopped.
func (t *monitorTab) ended(mon Monitor, gen int) {
	if gen != t.gen || t.mon != mon {
		return
	}
	t.mon = nil
	t.x.untrack(mon)
	if err := mon.Err(); err != nil {
		t.x.api.Log().Warn("MONITOR ended", "err", err)
		t.note = pageui.Bad("MONITOR ended: " + errorText(err))
	}
	t.x.refreshMonitor()
}

// stop ends MONITOR.
func (t *monitorTab) stop() {
	t.gen++
	mon := t.mon
	t.mon, t.starting = nil, false
	if mon != nil {
		t.x.untrack(mon)
		mon.Close()
	}
	t.feed.take()
	t.note = pageui.Note{}
	t.x.refreshMonitor()
}

// setPaused freezes the list; the ring keeps receiving.
func (t *monitorTab) setPaused(p bool) {
	if t.paused == p {
		return
	}
	t.paused = p
	if p {
		t.snap = t.lines.snapshot()
	} else {
		t.snap = nil
	}
	t.table.sync()
	t.grid.Appended()
	t.x.refreshMonitor()
}

// clear forgets the lines.
func (t *monitorTab) clear() {
	t.lines.clear()
	t.snap = nil
	t.table.sync()
	t.grid.SetSource(t.table)
	setColumnWidths(t.grid, 175, 50, 190, 520)
	t.x.refreshMonitor()
}

// buildMonitor makes the widget of the Redis Monitor tab.
func (x *Extension) buildMonitor(bc widgets.BuildContext) widgets.Widget {
	colors := ui.Theme(bc)
	conn := x.chooseTarget()
	if conn == "" {
		return widgets.NewColoredBox(colors.PanelBackground, x.noConnection(bc, colors))
	}
	t := x.monitorFor(conn)
	status := "Stopped"
	switch {
	case t.starting:
		status = "Starting..."
	case t.running():
		status = "Running"
	}
	count := status + ", " + plural(int64(t.lines.len()), "command", "commands")
	if t.lines.dropped > 0 {
		count += ", " + Commas(t.lines.dropped) + " older dropped"
	}
	if t.paused {
		count += ", paused"
	}
	startStop := small("Start", false, t.starting, t.askStart)
	if t.running() {
		startStop = small("Stop", false, false, t.stop)
	}
	bar := tabBar(
		x.connPicker(conn, func(id string) { x.setTarget(id); x.refreshMonitor() }), pageui.HGap(10), startStop, pageui.HGap(10),
		muted(bc, colors, count), widgets.NewSpacer(),
		small(map[bool]string{true: "Resume", false: "Pause"}[t.paused], true, false, func() { t.setPaused(!t.paused) }), pageui.HGap(6),
		small("Clear", true, t.lines.len() == 0, t.clear),
	)
	rows := []widgets.Widget{bar,
		widgets.NewPadding(painting.NewEdgeInsetsFromLTRB(12, 0, 12, 4), muted(bc, colors,
			"MONITOR shows every command the server runs and slows it down while it runs: stop it when you are done."))}
	if t.note.Text != "" {
		rows = append(rows, widgets.NewPadding(painting.NewEdgeInsetsFromLTRB(12, 0, 12, 4), pageui.NoteLine(bc, colors, t.note)))
	}
	body := t.grid.Widget()
	if t.lines.len() == 0 {
		body = widgets.NewPadding(painting.NewEdgeInsetsFromLTRB(12, 10, 12, 0), widgets.NewAlign(
			widgets.NewText("No command yet. Press Start, then use the server from another client.",
				widgets.TextOpts{Style: ui.TextStyle(bc, 12, colors.DescriptionForeground)}), widgets.AlignOpts{Alignment: painting.AlignmentTopLeft}))
	}
	rows = append(rows, hline(colors), widgets.NewExpanded(body))
	return widgets.NewColoredBox(colors.PanelBackground, widgets.NewColumn(rows, widgets.ColumnOpts{CrossAxisAlignment: rendering.CrossAxisAlignmentStretch}))
}
