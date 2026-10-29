package terminal

import (
	"strconv"

	"github.com/rm4n0s/glutter/foundation"
	"github.com/rm4n0s/glutter/gestures"
	"github.com/rm4n0s/glutter/painting"
	"github.com/rm4n0s/glutter/rendering"
	gui "github.com/rm4n0s/glutter/ui"
	"github.com/rm4n0s/glutter/widgets"

	"github.com/rm4n0s/emetos/extension/ui"
)

// The measures of the panel: the width of the tabs list (VS Code's default is 150 px to 200 px) and the height of a row.
const (
	tabsWidth = 180.0
	tabRowH   = 22.0
	// markH is the height of the second line that names the Relay clients that typed in a terminal.
	markH    = 14.0
	minPaneW = 80.0
)

// build makes the widget of the view: the terminals of the active tab side by side, the tabs list beside them when there are
// several, and the context menu over all of it.
func (t *terminals) build(bc widgets.BuildContext) widgets.Widget {
	t.visible = true
	colors := ui.Theme(bc)
	bg := colors.TerminalBackground
	if bg.Alpha() == 0 {
		bg = colors.PanelBackground
	}
	var content widgets.Widget
	if g := t.activeGroup(); g != nil {
		content = t.groupWidget(g, colors)
	} else {
		content = t.emptyWidget(bc, colors)
		t.openIfNone()
	}
	cells := []widgets.Widget{widgets.NewExpanded(content)}
	if t.showTabs() {
		tabs := t.tabsWidget(bc, colors)
		if t.cfg.tabsLocation == "left" {
			cells = append([]widgets.Widget{tabs}, cells...)
		} else {
			cells = append(cells, tabs)
		}
	}
	layers := []widgets.Widget{widgets.NewRow(cells, widgets.RowOpts{CrossAxisAlignment: rendering.CrossAxisAlignmentStretch})}
	if t.ask != nil {
		layers = append(layers, widgets.NewPositioned(t.askBar(bc, colors), widgets.PositionedOpts{
			Left: foundation.Some(0.0), Right: foundation.Some(0.0), Top: foundation.Some(0.0),
		}))
	}
	body := widgets.NewColoredBox(bg, widgets.NewStack(layers, widgets.StackOpts{Fit: rendering.StackFitExpand}))
	return ui.NewContextMenu(body, ui.ContextMenuOpts{Controller: t.menu})
}

// openIfNone makes a terminal for a view that is shown with none (the first time the panel opens, or after the last terminal
// ended), unless the last try failed: then the view says so and has a button to try again.
func (t *terminals) openIfNone() {
	if t.opening || t.suppressOpen || t.failure != "" {
		return
	}
	t.opening = true
	t.api.Post(func() {
		t.opening = false
		if len(t.groups) == 0 && t.visible {
			if _, err := t.open(openOpts{}); err != nil {
				t.api.Log().Warn("the terminal cannot be started", "err", err)
			}
		}
	})
}

// emptyWidget is what the view shows with no terminal: that one is starting, or why it could not.
func (t *terminals) emptyWidget(bc widgets.BuildContext, colors ui.Colors) widgets.Widget {
	small := func(s string) widgets.Widget {
		return widgets.NewText(s, widgets.TextOpts{Style: ui.TextStyle(bc, 12, colors.DescriptionForeground)})
	}
	if t.failure == "" {
		return widgets.NewPadding(painting.NewEdgeInsetsAll(16), small("Starting the terminal..."))
	}
	return widgets.NewPadding(painting.NewEdgeInsetsAll(16), widgets.NewColumn([]widgets.Widget{
		small(t.failure),
		widgets.NewSizedBox(nil, widgets.SizedBoxOpts{Height: foundation.Some(8.0)}),
		ui.NewButton("Try Again", ui.ButtonOpts{Small: true, OnPressed: func() {
			t.failure = ""
			t.handle.Refresh()
		}}),
	}, widgets.ColumnOpts{CrossAxisAlignment: rendering.CrossAxisAlignmentStart}))
}

// groupWidget is the terminals of a tab side by side, with a sash between each two.
func (t *terminals) groupWidget(g *group, colors ui.Colors) widgets.Widget {
	key := widgets.WidgetOpts{Key: foundation.NewValueKey(g.id)}
	if len(g.entries) == 1 {
		return widgets.NewKeyedSubtree(paneWidget(g.entries[0].pv), key)
	}
	panes := make([]ui.SplitPane, len(g.entries))
	for i, e := range g.entries {
		size := 1000.0
		if i < len(g.sizes) {
			size = g.sizes[i]
		}
		panes[i] = ui.SplitPane{Child: paneWidget(e.pv), Size: size, MinSize: minPaneW}
	}
	return widgets.NewKeyedSubtree(ui.NewSplitView(panes, ui.SplitViewOpts{
		Orientation: ui.SplitHorizontal, ProportionalLayout: true, Controller: g.split, SeparatorBorder: colors.TerminalBorder,
		OnDidChange: func(sizes []float64) {
			if len(sizes) == len(g.entries) {
				copy(g.sizes, sizes)
			}
		},
	}), key)
}

// ---- the tabs list ----

// tabsWidget is the list of the terminals at the side of the view: one row each; the terminals of a split are joined by the
// box-drawing lines VS Code draws before their names.
func (t *terminals) tabsWidget(bc widgets.BuildContext, colors ui.Colors) widgets.Widget {
	var rows []widgets.Widget
	index := 0
	for gi, g := range t.groups {
		for ei, e := range g.entries {
			prefix := ""
			if len(g.entries) > 1 {
				switch {
				case ei == 0:
					prefix = "┌ "
				case ei == len(g.entries)-1:
					prefix = "└ "
				default:
					prefix = "├ "
				}
			}
			rows = append(rows, t.tabRow(colors, gi == t.cur && ei == g.active, prefix, e, index))
			index++
		}
	}
	list := widgets.NewColumn(rows, widgets.ColumnOpts{CrossAxisAlignment: rendering.CrossAxisAlignmentStretch})
	border := widgets.NewSizedBox(widgets.NewColoredBox(colors.TerminalBorder, nil), widgets.SizedBoxOpts{Width: foundation.Some(1.0)})
	body := widgets.NewSizedBox(ui.NewScrollView(list, nil), widgets.SizedBoxOpts{Width: foundation.Some(tabsWidth)})
	cells := []widgets.Widget{border, body}
	if t.cfg.tabsLocation == "left" {
		cells = []widgets.Widget{body, border}
	}
	return widgets.NewRow(cells, widgets.RowOpts{CrossAxisAlignment: rendering.CrossAxisAlignmentStretch})
}

// tabRow is the row of a terminal in the tabs list: its icon, its title, and, while the pointer is over the row, the buttons
// that split and kill it. A click selects the terminal, a click with the middle button kills it and the secondary button opens
// the context menu.
func (t *terminals) tabRow(colors ui.Colors, active bool, prefix string, e *entry, index int) widgets.Widget {
	hovered := false
	return widgets.NewStatefulBuilder(func(bc widgets.BuildContext, setState widgets.StateSetter) widgets.Widget {
		fg := colors.Foreground
		var bg gui.Color
		switch {
		case active:
			bg = colors.ListInactiveSelectionBackground
			if f := colors.ListInactiveSelectionForeground; f.Alpha() != 0 {
				fg = f
			}
		case hovered:
			bg = colors.ListHoverBackground
		}
		iconColor := fg
		if c, ok := ansiColor(colors, e.term.Meta().color); ok {
			iconColor = c
		}
		cells := []widgets.Widget{
			ui.Icon(t.tabIconOf(e), 14, iconColor),
			widgets.NewSizedBox(nil, widgets.SizedBoxOpts{Width: foundation.Some(6.0)}),
			widgets.NewExpanded(widgets.NewText(prefix+t.title(e), widgets.TextOpts{
				Style: ui.TextStyle(bc, 13, fg), MaxLines: foundation.Some(1), Overflow: painting.TextOverflowEllipsis,
			})),
		}
		if t.bellOn(e) {
			cells = append(cells, ui.Icon("bell", 14, colors.IconForeground))
		}
		if hovered || active {
			cells = append(cells,
				ui.NewIconButton("split-horizontal", ui.IconButtonOpts{Tooltip: "Split Terminal", OnPressed: func() { t.splitFrom(e) }}),
				ui.NewIconButton("trash", ui.IconButtonOpts{Tooltip: "Kill Terminal", OnPressed: func() { t.kill(e) }}))
		}
		line := widgets.Widget(widgets.NewRow(cells, widgets.RowOpts{CrossAxisAlignment: rendering.CrossAxisAlignmentCenter}))
		height := tabRowH
		if mark := clientMark(e); mark != "" {
			// a Relay client typed in this terminal: a second line says which (SPEC §10.25 "Relay")
			height += markH
			line = widgets.NewColumn([]widgets.Widget{
				widgets.NewSizedBox(line, widgets.SizedBoxOpts{Height: foundation.Some(tabRowH)}),
				widgets.NewPadding(painting.NewEdgeInsetsFromLTRB(20, 0, 0, 0), widgets.NewText(mark, widgets.TextOpts{
					Style: ui.TextStyle(bc, 11, colors.NotificationsWarningIconForeground), MaxLines: foundation.Some(1), Overflow: painting.TextOverflowEllipsis,
				})),
			}, widgets.ColumnOpts{CrossAxisAlignment: rendering.CrossAxisAlignmentStretch})
		}
		row := widgets.Widget(widgets.NewContainer(line, widgets.ContainerOpts{
			Height: foundation.Some(height), Padding: painting.NewEdgeInsetsSymmetric(0, 8), Color: foundation.Some(bg),
		}))
		row = t.dropMark(colors, row, e)
		set := func(v bool) {
			if hovered != v {
				setState(func() { hovered = v })
			}
		}
		tracked := widgets.NewListener(widgets.NewGestureDetector(row, widgets.GestureDetectorOpts{
			OnTap:              func() { t.focusEntry(e, false) },
			OnSecondaryTapDown: func(d gestures.TapDownDetails) { t.focusEntry(e, false); t.showMenuAt(e, d.GlobalPosition) },
			OnTertiaryTapDown:  func(gestures.TapDownDetails) { t.kill(e) },
		}), widgets.ListenerOpts{
			OnPointerDown:   func(ev *gestures.PointerDownEvent) { t.tabPointerDown(ev, e, index) },
			OnPointerMove:   t.tabPointerMove,
			OnPointerUp:     t.tabPointerUp,
			OnPointerCancel: t.tabPointerCancel,
		})
		return ui.NewHover(widgets.NewMouseRegion(tracked, widgets.MouseRegionOpts{
			OnEnter: func(*gestures.PointerEnterEvent) { set(true) }, OnHover: func(*gestures.PointerHoverEvent) { set(true) }, OnExit: func(*gestures.PointerExitEvent) { set(false) },
		}), ui.HoverOpts{Text: t.hoverText(e), Compact: true})
	})
}

// ---- the context menu ----

// showMenu is the context menu of a pane, opened by the secondary button (rightClickBehavior default).
func (t *terminals) showMenu(pv *paneView, at gui.Offset) {
	for _, e := range t.all() {
		if e.pv == pv {
			t.showMenuAt(e, at)
			return
		}
	}
}

// showMenuAt opens the context menu of a terminal at a place of the window: Copy, Paste, Select All, Clear, then what the tab
// of the terminal offers (Split, Unsplit, Move into the editor area or back into the panel, Rename, Change Icon, Change Color)
// and Kill (SPEC §10.25 "The UI", "The rest of the tabs list").
func (t *terminals) showMenuAt(e *entry, at gui.Offset) {
	pv := e.pv
	items := []ui.MenuItem{
		{ID: "copy", Label: "Copy", Keybinding: "Ctrl+Shift+C", Disabled: !pv.hasSelection(), OnRun: pv.copySelection},
		{ID: "paste", Label: "Paste", Keybinding: "Ctrl+Shift+V", OnRun: pv.pasteClipboard},
		{ID: "selectAll", Label: "Select All", OnRun: pv.selectAll},
		ui.SeparatorItem(),
		{ID: "clear", Label: "Clear", OnRun: pv.clear},
		ui.SeparatorItem(),
		{ID: "split", Label: "Split Terminal", Keybinding: "Ctrl+Shift+5", OnRun: func() { t.splitFrom(e) }},
	}
	if g := groupOf(t, e); g != nil && len(g.entries) > 1 {
		items = append(items, ui.MenuItem{ID: "unsplit", Label: "Unsplit Terminal", OnRun: func() { t.unsplit(e) }})
	}
	if e.inEditor() {
		items = append(items, ui.MenuItem{ID: "moveToPanel", Label: "Move Terminal into Panel", OnRun: func() { t.moveToPanel(e) }})
	} else {
		items = append(items, ui.MenuItem{ID: "moveToEditor", Label: "Move Terminal into Editor Area", OnRun: func() { t.moveToEditor(e) }})
	}
	items = append(items,
		ui.SeparatorItem(),
		ui.MenuItem{ID: "rename", Label: "Rename...", OnRun: func() { t.renameEntry(e) }},
		ui.MenuItem{ID: "changeIcon", Label: "Change Icon...", OnRun: func() { t.changeIcon(e) }},
		ui.MenuItem{ID: "changeColor", Label: "Change Color...", OnRun: func() { t.changeColor(e) }},
		ui.SeparatorItem(),
		ui.MenuItem{ID: "kill", Label: "Kill Terminal", OnRun: func() { t.kill(e) }})
	t.menu.Show(at, items)
}

// splitFrom puts a new terminal beside one, in its tab.
func (t *terminals) splitFrom(e *entry) {
	t.focusEntry(e, false)
	o := openOpts{split: true}
	if e.inEditor() {
		o.location = locEditor
	}
	if _, err := t.open(o); err != nil {
		t.api.Log().Warn("the terminal cannot be split", "err", err)
	}
}

// ---- the question about pasting several lines ----

// confirmPaste asks the user to confirm a paste of several lines; paste is called when the user agrees.
func (t *terminals) confirmPaste(lines int, paste func()) {
	t.ask = &pasteAsk{lines: lines, paste: paste}
	if e := t.paneFocus; e != nil {
		t.ask.pane = e.pv
	}
	t.handle.Refresh()
}

// answerPaste ends the question; with yes the text is pasted. The focus goes back to the terminal.
func (t *terminals) answerPaste(yes bool) {
	a := t.ask
	t.ask = nil
	t.handle.Refresh()
	if a == nil {
		return
	}
	if yes {
		a.paste()
	}
	if a.pane != nil {
		t.api.Post(a.pane.requestFocus)
	}
}

// askBar is the bar over the top of the view that asks about the paste (VS Code asks with a dialog).
func (t *terminals) askBar(bc widgets.BuildContext, colors ui.Colors) widgets.Widget {
	text := "Do you want to paste " + strconv.Itoa(t.ask.lines) + " lines of text into the terminal?"
	return widgets.NewContainer(widgets.NewRow([]widgets.Widget{
		widgets.NewExpanded(widgets.NewText(text, widgets.TextOpts{
			Style: ui.TextStyle(bc, 13, colors.EditorWidgetForeground), MaxLines: foundation.Some(2), Overflow: painting.TextOverflowEllipsis,
		})),
		widgets.NewSizedBox(nil, widgets.SizedBoxOpts{Width: foundation.Some(8.0)}),
		ui.NewButton("Paste", ui.ButtonOpts{Small: true, OnPressed: func() { t.answerPaste(true) }}),
		widgets.NewSizedBox(nil, widgets.SizedBoxOpts{Width: foundation.Some(6.0)}),
		ui.NewButton("Cancel", ui.ButtonOpts{Small: true, Secondary: true, OnPressed: func() { t.answerPaste(false) }}),
	}, widgets.RowOpts{CrossAxisAlignment: rendering.CrossAxisAlignmentCenter}), widgets.ContainerOpts{
		Padding: painting.NewEdgeInsetsSymmetric(6, 12), Color: foundation.Some(colors.EditorWidgetBackground),
	})
}
