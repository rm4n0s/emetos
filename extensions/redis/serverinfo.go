package redis

import (
	"context"
	"encoding/json"
	"strconv"
	"strings"
	"time"

	"github.com/rm4n0s/errors"
	"github.com/rm4n0s/glutter/foundation"
	"github.com/rm4n0s/glutter/painting"
	"github.com/rm4n0s/glutter/rendering"
	"github.com/rm4n0s/glutter/widgets"

	"github.com/rm4n0s/emetos/extension"
	"github.com/rm4n0s/emetos/extension/ui"
	"github.com/rm4n0s/emetos/extensions/internal/pageui"
	"github.com/rm4n0s/emetos/extensions/internal/resultgrid"
)

// The views of the server information tab (SPEC §10.23: INFO, SLOWLOG GET, CLIENT LIST, MEMORY STATS).
const (
	infoViewInfo = iota
	infoViewSlow
	infoViewClients
	infoViewMemory
)

var infoViewNames = []string{"Info", "Slow Log", "Clients", "Memory"}

// slowLogCount is how many entries of the slow log the tab asks for.
const slowLogCount = 128

// InfoTable is what a view of the server information shows: the columns and the rows of text.
type InfoTable struct {
	Columns []string
	Rows    [][]string
}

// ParseInfo reads the text of INFO into rows of section, key and value: `# Server` starts a section and `key:value` lines are its
// entries. Blank lines and lines without a colon are skipped.
func ParseInfo(text string) InfoTable {
	t := InfoTable{Columns: []string{"Section", "Key", "Value"}}
	section := ""
	for _, line := range strings.Split(text, "\n") {
		line = strings.TrimSpace(line)
		switch {
		case line == "":
		case strings.HasPrefix(line, "#"):
			section = strings.TrimSpace(strings.TrimPrefix(line, "#"))
		default:
			if k, v, ok := strings.Cut(line, ":"); ok {
				t.Rows = append(t.Rows, []string{section, k, v})
			}
		}
	}
	return t
}

// ParseSlowLog reads the reply of SLOWLOG GET: entries of [id, unix time, microseconds, [command and arguments], client address, client
// name]. The times are written in the zone loc.
func ParseSlowLog(r Reply, loc *time.Location) InfoTable {
	t := InfoTable{Columns: []string{"ID", "Time", "Duration", "Command", "Client", "Name"}}
	if r.Kind != ReplyArray {
		return t
	}
	for _, e := range r.Items {
		if e.Kind != ReplyArray || len(e.Items) < 4 {
			continue
		}
		var args []string
		for _, a := range e.Items[3].Items {
			args = append(args, QuoteArg(a.Str))
		}
		row := []string{plainText(e.Items[0]), "", formatMicros(e.Items[2].Int), strings.Join(args, " "), "", ""}
		if e.Items[1].Kind == ReplyInt {
			row[1] = time.Unix(e.Items[1].Int, 0).In(loc).Format("2006-01-02 15:04:05")
		}
		if len(e.Items) > 4 {
			row[4] = e.Items[4].Str
		}
		if len(e.Items) > 5 {
			row[5] = e.Items[5].Str
		}
		t.Rows = append(t.Rows, row)
	}
	return t
}

// formatMicros writes a duration given in microseconds.
func formatMicros(us int64) string {
	switch {
	case us >= 1_000_000:
		return strconv.FormatFloat(float64(us)/1e6, 'f', 2, 64) + " s"
	case us >= 1000:
		return strconv.FormatFloat(float64(us)/1e3, 'f', 2, 64) + " ms"
	}
	return strconv.FormatInt(us, 10) + " µs"
}

// clientColumns are the fields of CLIENT LIST the tab shows, in this order.
var clientColumns = []string{"id", "addr", "name", "user", "db", "age", "idle", "flags", "cmd", "sub", "psub", "multi", "qbuf", "tot-mem"}

// ParseClientList reads the text of CLIENT LIST: a line for each client of `key=value` pairs separated by spaces. The columns are
// those of clientColumns that some client has.
func ParseClientList(text string) InfoTable {
	var clients []map[string]string
	for _, line := range strings.Split(text, "\n") {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		m := map[string]string{}
		for _, f := range strings.Fields(line) {
			if k, v, ok := strings.Cut(f, "="); ok {
				m[k] = v
			}
		}
		clients = append(clients, m)
	}
	var cols []string
	for _, c := range clientColumns {
		for _, m := range clients {
			if _, ok := m[c]; ok {
				cols = append(cols, c)
				break
			}
		}
	}
	t := InfoTable{Columns: cols}
	for _, m := range clients {
		row := make([]string, len(cols))
		for i, c := range cols {
			row[i] = m[c]
		}
		t.Rows = append(t.Rows, row)
	}
	return t
}

// ParseMemoryStats reads the reply of MEMORY STATS: a map in RESP3, a flat array in RESP2, whose values are numbers, and for the
// databases maps (db.0: overhead.hashtable.main, ...). A nested map is written as name.key rows.
func ParseMemoryStats(r Reply) InfoTable {
	t := InfoTable{Columns: []string{"Name", "Value"}}
	var add func(prefix string, r Reply)
	add = func(prefix string, r Reply) {
		switch r.Kind {
		case ReplyMap:
			for _, p := range r.Pairs {
				add(joinName(prefix, plainText(p.Key)), p.Value)
			}
		case ReplyArray:
			nested := len(r.Items) > 0 && len(r.Items)%2 == 0 && (r.Items[0].Kind == ReplyString || r.Items[0].Kind == ReplyStatus)
			if prefix != "" && nested {
				for i := 0; i+1 < len(r.Items); i += 2 {
					add(joinName(prefix, plainText(r.Items[i])), r.Items[i+1])
				}
				return
			}
			if prefix == "" {
				for i := 0; i+1 < len(r.Items); i += 2 {
					add(plainText(r.Items[i]), r.Items[i+1])
				}
			}
		default:
			t.Rows = append(t.Rows, []string{prefix, plainText(r)})
		}
	}
	add("", r)
	return t
}

func joinName(prefix, name string) string {
	if prefix == "" {
		return name
	}
	return prefix + "." + name
}

// infoState is the state of a server information tab.
type infoState struct {
	Conn string `json:"conn"`
}

// infoTab is the server information tab of one connection. Its fields belong to the UI thread.
type infoTab struct {
	x    *Extension
	tab  extension.EditorTab
	conn string

	view    int
	gen     int
	loaded  bool
	loading bool
	problem string
	at      time.Time
	table   *resultgrid.Table
	grid    *resultgrid.Grid
	closed  bool
	stop    func()
}

// createInfoTab makes the content of a server information tab.
func (x *Extension) createInfoTab(tab extension.EditorTab, state json.RawMessage) (extension.EditorTabContent, error) {
	var st infoState
	if len(state) == 0 {
		return extension.EditorTabContent{}, errors.New("RedisInfoStateMissing", "a server information tab is opened with its connection")
	}
	if err := json.Unmarshal(state, &st); err != nil {
		return extension.EditorTabContent{}, errors.NewErr("RedisInfoStateInvalid", err)
	}
	t := &infoTab{x: x, tab: tab, conn: st.Conn}
	t.grid = resultgrid.New(resultgrid.Options{OnCopy: x.copyText})
	x.infoTabs = append(x.infoTabs, t)
	tab.SetTitle(x.connectionName(st.Conn) + " info")
	tab.SetHint("Server information of " + x.connectionName(st.Conn))
	t.stop = x.hub.Subscribe(func() { x.api.Post(t.connectionChanged) })
	t.reload()
	return extension.EditorTabContent{
		Build:     t.build,
		SaveState: func() (any, error) { return infoState{Conn: t.conn}, nil },
		OnClose: func() {
			t.closed = true
			t.stop()
			t.grid.Dispose()
			for i, o := range x.infoTabs {
				if o == t {
					x.infoTabs = append(x.infoTabs[:i], x.infoTabs[i+1:]...)
					break
				}
			}
		},
	}, nil
}

// openServerInfo opens the tab of a connection, or shows the one that is open.
func (x *Extension) openServerInfo(conn string) {
	for _, t := range x.infoTabs {
		if t.conn == conn {
			t.tab.Reveal(true)
			return
		}
	}
	tab, err := x.api.Editors().Open(InfoKindID, infoState{Conn: conn}, extension.EditorOpenOptions{})
	if err != nil {
		x.api.Log().Warn("the server information tab cannot be opened", "err", err)
		x.api.Notifications().Error("Cannot open the server information: " + err.Error())
		return
	}
	tab.Reveal(true)
}

// connectionChanged loads the tab again when its connection opens.
func (t *infoTab) connectionChanged() {
	if t.closed {
		return
	}
	connected := t.x.drv.Connected(t.conn)
	if connected && !t.loaded && !t.loading {
		t.reload()
	}
	if !connected && t.loaded {
		t.tab.Refresh()
	}
}

// choose shows another view and reads it.
func (t *infoTab) choose(view int) {
	if t.view == view && t.loaded {
		return
	}
	t.view = view
	t.reload()
}

// reload reads the view again off the UI thread.
func (t *infoTab) reload() {
	if t.closed {
		return
	}
	t.gen++
	gen, view := t.gen, t.view
	t.loading, t.problem = true, ""
	t.tab.SetBusy(true)
	t.tab.Refresh()
	x := t.x
	go func() {
		ctx, cancel := context.WithTimeout(x.ctx, opTimeout)
		defer cancel()
		table, err := t.fetch(ctx, view)
		x.api.Post(func() {
			if gen != t.gen || t.closed {
				return
			}
			t.loading, t.loaded = false, true
			t.tab.SetBusy(false)
			if err != nil {
				x.api.Log().Warn("the server information cannot be read", "err", err)
				t.problem = errorText(err)
				t.tab.Refresh()
				return
			}
			t.at = time.Now()
			cols := make([]resultgrid.Column, len(table.Columns))
			for i, c := range table.Columns {
				cols[i] = resultgrid.Column{Name: c}
			}
			t.table = resultgrid.NewTable(cols)
			for _, row := range table.Rows {
				vals := make([]resultgrid.Value, len(row))
				for i, c := range row {
					vals[i] = resultgrid.Text(c)
				}
				t.table.Append(vals)
			}
			t.grid.SetSource(t.table)
			setColumnWidthsFor(t.grid, view)
			t.tab.Refresh()
		})
	}()
}

// fetch asks the server for the view. An error reply of the server (a command the server does not have) is the error of the view.
func (t *infoTab) fetch(ctx context.Context, view int) (InfoTable, error) {
	db, _, err := t.x.drv.database(t.conn, 0)
	if err != nil {
		return InfoTable{}, err
	}
	var args []string
	switch view {
	case infoViewSlow:
		args = []string{"SLOWLOG", "GET", strconv.Itoa(slowLogCount)}
	case infoViewClients:
		args = []string{"CLIENT", "LIST"}
	case infoViewMemory:
		args = []string{"MEMORY", "STATS"}
	default:
		args = []string{"INFO", "all"}
	}
	rep, err := db.Do(ctx, args)
	if err != nil {
		return InfoTable{}, err
	}
	if rep.IsError() {
		return InfoTable{}, errors.New("RedisInfoRefused", rep.Str, "command", args[0])
	}
	switch view {
	case infoViewSlow:
		return ParseSlowLog(rep, t.x.loc()), nil
	case infoViewClients:
		return ParseClientList(rep.Str), nil
	case infoViewMemory:
		return ParseMemoryStats(rep), nil
	}
	return ParseInfo(rep.Str), nil
}

// build makes the widget of the tab.
func (t *infoTab) build(bc widgets.BuildContext) widgets.Widget {
	colors := ui.Theme(bc)
	var body widgets.Widget
	switch {
	case !t.x.drv.Connected(t.conn):
		body = pageMessage(bc, colors, "The connection is closed. Connect it in the Databases view, and this tab loads the server information again.", false)
	case t.loading && !t.loaded:
		body = pageMessage(bc, colors, "Loading...", false)
	case t.problem != "":
		body = pageMessage(bc, colors, "The server information cannot be read: "+t.problem, true)
	default:
		body = t.grid.Widget()
	}
	var views []widgets.Widget
	for i, name := range infoViewNames {
		view := i
		views = append(views, ui.NewButton(name, ui.ButtonOpts{Small: true, Secondary: t.view != i, OnPressed: func() { t.choose(view) }}), pageui.HGap(4))
	}
	when := ""
	if !t.at.IsZero() {
		when = "read at " + t.at.Format("15:04:05")
	}
	head := widgets.NewPadding(painting.NewEdgeInsetsFromLTRB(16, 12, 16, 8), widgets.NewColumn([]widgets.Widget{
		widgets.NewRow([]widgets.Widget{
			widgets.NewFlexible(widgets.NewText(t.x.connectionName(t.conn), widgets.TextOpts{
				Style: pageui.Bold(ui.TextStyle(bc, 15, colors.Foreground)), MaxLines: foundation.Some(1), Overflow: painting.TextOverflowEllipsis})),
			pageui.HGap(8), ui.NewIconButton("refresh", ui.IconButtonOpts{Tooltip: "Refresh", OnPressed: t.reload}),
		}, widgets.RowOpts{CrossAxisAlignment: rendering.CrossAxisAlignmentCenter}),
		pageui.Gap(6),
		widgets.NewRow(append(views, pageui.HGap(8), widgets.NewText(when, widgets.TextOpts{Style: ui.TextStyle(bc, 12, colors.DescriptionForeground)})),
			widgets.RowOpts{CrossAxisAlignment: rendering.CrossAxisAlignmentCenter}),
	}, widgets.ColumnOpts{CrossAxisAlignment: rendering.CrossAxisAlignmentStretch}))
	col := widgets.NewColumn([]widgets.Widget{head, hline(colors), widgets.NewExpanded(body)},
		widgets.ColumnOpts{CrossAxisAlignment: rendering.CrossAxisAlignmentStretch})
	return widgets.NewColoredBox(colors.EditorBackground, col)
}

// pageMessage is a line in the middle of a tab.
func pageMessage(bc widgets.BuildContext, colors ui.Colors, text string, bad bool) widgets.Widget {
	color := colors.DescriptionForeground
	if bad {
		color = colors.ErrorForeground
	}
	return widgets.NewPadding(painting.NewEdgeInsetsAll(16), widgets.NewAlign(
		widgets.NewText(text, widgets.TextOpts{Style: ui.TextStyle(bc, 13, color)}), widgets.AlignOpts{Alignment: painting.AlignmentTopLeft}))
}
