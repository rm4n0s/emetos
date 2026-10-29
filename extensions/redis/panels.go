package redis

import (
	"slices"
	"time"

	"github.com/rm4n0s/glutter/foundation"
	"github.com/rm4n0s/glutter/painting"
	"github.com/rm4n0s/glutter/rendering"
	"github.com/rm4n0s/glutter/widgets"

	"github.com/rm4n0s/emetos/extension"
	"github.com/rm4n0s/emetos/extension/ui"
	"github.com/rm4n0s/emetos/extensions/internal/dbconn"
	"github.com/rm4n0s/emetos/extensions/internal/resultgrid"
)

// The panel views and the editor kind the step adds (SPEC §10.23).
const (
	// PanelViewID is the one tab of the panel ("Redis"): the title strip of the panel gives the tabs the room before the actions of
	// a view, so three tabs of Redis left no room for the filter of Problems. The console, Pub/Sub and Monitor are inside it.
	PanelViewID = ID + ".panel"
	// ConsoleViewID, PubSubViewID and MonitorViewID name the three pages of the Redis tab: the console, Pub/Sub and Monitor.
	ConsoleViewID = ID + ".console"
	PubSubViewID  = ID + ".pubsub"
	MonitorViewID = ID + ".monitor"
	// InfoKindID is the editor kind of the server information tab.
	InfoKindID = ID + ".info"
	// RedisLanguageID is the language of .redis files.
	RedisLanguageID = "redis"
	// SettingConfirmDangerous asks before a dangerous command runs.
	SettingConfirmDangerous = "redis.confirmDangerous"
)

// The commands of the step.
const (
	CmdOpenConsole   = ID + ".openConsole"
	CmdClearConsole  = ID + ".clearConsole"
	CmdCancel        = ID + ".cancel"
	CmdRun           = ID + ".run"
	CmdRunAll        = ID + ".runAll"
	CmdSelectConn    = ID + ".selectConnection"
	CmdOpenPubSub    = ID + ".openPubSub"
	CmdSubscribe     = ID + ".subscribe"
	CmdPSubscribe    = ID + ".psubscribe"
	CmdPublish       = ID + ".publish"
	CmdOpenMonitor   = ID + ".openMonitor"
	CmdStartMonitor  = ID + ".startMonitor"
	CmdStopMonitor   = ID + ".stopMonitor"
	CmdServerInfo    = ID + ".serverInfo"
	CmdOpenKey       = ID + ".openKey"
	CmdCopyLastReply = ID + ".copyLastReply"
)

// liveConnections are the Redis connections of this workspace that are open, in the order of the connection file.
func (x *Extension) liveConnections() []dbconn.Connection {
	var out []dbconn.Connection
	for _, c := range x.hub.Store().List(x.api.Scope().Folder) {
		if c.Engine == EngineID && x.drv.Connected(c.ID) {
			out = append(out, c)
		}
	}
	return out
}

// connectionName is the name of a connection, or its id when it is gone.
func (x *Extension) connectionName(id string) string {
	if c, ok := x.hub.Store().Get(id); ok {
		return c.Name
	}
	return id
}

// chooseTarget makes the connection the tabs of the panel show when it is open, else the first one that is.
func (x *Extension) chooseTarget() string {
	live := x.liveConnections()
	if len(live) == 0 {
		x.setTarget("")
		return ""
	}
	if slices.IndexFunc(live, func(c dbconn.Connection) bool { return c.ID == x.target }) < 0 {
		x.setTarget(live[0].ID)
	}
	return x.target
}

// connPicker is the select box of the open connections at the head of each tab.
func (x *Extension) connPicker(current string, onChange func(id string)) widgets.Widget {
	live := x.liveConnections()
	opts := make([]ui.DropdownOption, len(live))
	selected := -1
	for i, c := range live {
		opts[i] = ui.DropdownOption{Label: c.Name}
		if c.ID == current {
			selected = i
		}
	}
	return ui.NewDropdown(opts, ui.DropdownOpts{
		Selected: selected, Width: foundation.Some(200.0), Placeholder: "No open connection", Disabled: len(live) == 0,
		OnChanged: func(i int) {
			if i >= 0 && i < len(live) {
				onChange(live[i].ID)
			}
		},
	})
}

// panelPages are the pages of the Redis tab of the panel, in the order of the switch at its head.
var panelPages = []struct{ id, title string }{{ConsoleViewID, "Console"}, {PubSubViewID, "Pub/Sub"}, {MonitorViewID, "Monitor"}}

// buildPanel makes the widget of the Redis tab of the panel: a switch between the console, Pub/Sub and Monitor over the page chosen.
func (x *Extension) buildPanel(bc widgets.BuildContext) widgets.Widget {
	colors := ui.Theme(bc)
	var page widgets.Widget
	switch x.panelTab {
	case PubSubViewID:
		page = x.buildPubSub(bc)
	case MonitorViewID:
		page = x.buildMonitor(bc)
	default:
		page = x.buildConsole(bc)
	}
	var switcher []widgets.Widget
	for _, p := range panelPages {
		id := p.id
		current := x.panelTab == id || (x.panelTab == "" && id == ConsoleViewID)
		switcher = append(switcher, ui.NewButton(p.title, ui.ButtonOpts{Small: true, Secondary: !current,
			OnPressed: func() { x.selectPage(id) }}), widgets.NewSizedBox(nil, widgets.SizedBoxOpts{Width: foundation.Some(4.0)}))
	}
	return widgets.NewColoredBox(colors.PanelBackground, widgets.NewColumn([]widgets.Widget{
		widgets.NewPadding(painting.NewEdgeInsetsFromLTRB(12, 6, 12, 0), widgets.NewRow(switcher, widgets.RowOpts{CrossAxisAlignment: rendering.CrossAxisAlignmentCenter})),
		widgets.NewExpanded(page),
	}, widgets.ColumnOpts{CrossAxisAlignment: rendering.CrossAxisAlignmentStretch}))
}

// selectPage shows a page of the Redis tab (the tab itself is not revealed).
func (x *Extension) selectPage(page string) {
	x.panelTab = page
	if x.views.panel != nil {
		x.views.panel.Refresh()
	}
}

// refreshPage builds the Redis tab again when it shows the page.
func (x *Extension) refreshPage(page string) {
	if x.views.panel != nil && (x.panelTab == page || (x.panelTab == "" && page == ConsoleViewID)) {
		x.views.panel.Refresh()
	}
}

// noConnection is the body of a tab when no connection is open.
func (x *Extension) noConnection(bc widgets.BuildContext, colors ui.Colors) widgets.Widget {
	return widgets.NewPadding(painting.NewEdgeInsetsAll(16), widgets.NewAlign(
		widgets.NewText("No Redis connection is open. Connect one in the Databases view.",
			widgets.TextOpts{Style: ui.TextStyle(bc, 13, colors.DescriptionForeground)}), widgets.AlignOpts{Alignment: painting.AlignmentTopLeft}))
}

// tabBar is the row at the head of a panel tab: the connection, then the widgets given.
func tabBar(children ...widgets.Widget) widgets.Widget {
	return widgets.NewPadding(painting.NewEdgeInsetsFromLTRB(12, 8, 12, 6), widgets.NewRow(children,
		widgets.RowOpts{CrossAxisAlignment: rendering.CrossAxisAlignmentCenter}))
}

// small is a small button.
func small(label string, secondary, disabled bool, fn func()) widgets.Widget {
	return ui.NewButton(label, ui.ButtonOpts{Small: true, Secondary: secondary, Disabled: disabled, OnPressed: fn})
}

// hline is a line across the width.
func hline(colors ui.Colors) widgets.Widget {
	return widgets.NewColoredBox(colors.PanelBorder, widgets.NewSizedBox(nil, widgets.SizedBoxOpts{Height: foundation.Some(1.0)}))
}

// muted is a small dimmed text.
func muted(bc widgets.BuildContext, colors ui.Colors, text string) widgets.Widget {
	return widgets.NewText(text, widgets.TextOpts{Style: ui.TextStyle(bc, 12, colors.DescriptionForeground), MaxLines: foundation.Some(1), Overflow: painting.TextOverflowEllipsis})
}

// confirmDangerous is the setting redis.confirmDangerous (on by default).
func (x *Extension) confirmDangerous() bool {
	on, err := extension.SettingValue[bool](x.api.Settings(), SettingConfirmDangerous)
	if err != nil {
		return true
	}
	return on
}

// loc is the time zone the times of the tabs are written in: the desktop's, unless a test sets another.
func (x *Extension) loc() *time.Location {
	if x.zone != nil {
		return x.zone
	}
	return time.Local
}

// setColumnWidths gives the columns of a grid their widths, so that they do not depend on the rows the grid held when it was made.
func setColumnWidths(g *resultgrid.Grid, widths ...float64) {
	for i, w := range widths {
		if i < len(g.Model().Columns()) {
			g.Model().SetWidth(i, w)
		}
	}
}

// setColumnWidthsFor sizes the columns of a view of the server information.
func setColumnWidthsFor(g *resultgrid.Grid, view int) {
	switch view {
	case infoViewSlow:
		setColumnWidths(g, 50, 190, 90, 280, 150, 80)
	case infoViewInfo:
		setColumnWidths(g, 120, 260, 400)
	case infoViewMemory:
		setColumnWidths(g, 320, 160)
	}
}
