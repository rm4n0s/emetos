package agents

import (
	"context"
	"strconv"
	"strings"
	"time"

	"github.com/rm4n0s/errors"
	"github.com/rm4n0s/glutter/foundation"
	"github.com/rm4n0s/glutter/painting"
	"github.com/rm4n0s/glutter/rendering"
	"github.com/rm4n0s/glutter/services"
	"github.com/rm4n0s/glutter/widgets"

	"github.com/rm4n0s/emetos/extension"
	"github.com/rm4n0s/emetos/extension/ui"
)

// sync makes the list show the index and builds the view again.
func (a *agents) sync() {
	items := make([]ui.ListViewItem, 0, len(a.index))
	for _, e := range a.newest() {
		items = append(items, ui.ListViewItem{Key: e.ID, Data: e, Label: e.label()})
	}
	if err := a.model.SetItems(items); err != nil {
		a.api.Log().Warn("the list of sessions cannot be shown", "err", err)
	}
	if a.handle != nil {
		a.handle.Refresh()
	}
}

// ago says how long before now a time was, as a list shows it.
func ago(now time.Time, millis int64) string {
	then := time.UnixMilli(millis)
	d := now.Sub(then)
	switch {
	case d < time.Minute:
		return "now"
	case d < time.Hour:
		return strconv.Itoa(int(d/time.Minute)) + " min"
	case d < 24*time.Hour:
		return strconv.Itoa(int(d/time.Hour)) + " h"
	case d < 30*24*time.Hour:
		return strconv.Itoa(int(d/(24*time.Hour))) + " d"
	}
	return then.Format("Jan 2")
}

// build is the list of the side bar.
func (a *agents) build(bc widgets.BuildContext) widgets.Widget {
	colors := ui.Theme(bc)
	if len(a.index) == 0 {
		return widgets.NewPadding(painting.NewEdgeInsetsAll(12), widgets.NewColumn([]widgets.Widget{
			widgets.NewText("No sessions yet. A session is a conversation with the agent; it opens in an editor tab.",
				widgets.TextOpts{Style: ui.TextStyle(bc, 13, colors.DescriptionForeground)}),
			widgets.NewSizedBox(nil, widgets.SizedBoxOpts{Height: foundation.Some(10.0)}),
			ui.NewButton("New Session", ui.ButtonOpts{Expand: true, OnPressed: func() { a.newTab(0) }}),
		}, widgets.ColumnOpts{CrossAxisAlignment: rendering.CrossAxisAlignmentStretch}))
	}
	open := func(row ui.ListViewRow) {
		if e, ok := row.Data.(*sessionEntry); ok && a.renaming != e.ID {
			a.openSession(e.ID, 0)
		}
	}
	return ui.NewList(a.model, ui.ListOpts{
		OnSelect: open, OnOpen: open,
		Render: func(bc widgets.BuildContext, row ui.ListViewRow, st ui.RowState) widgets.Widget {
			e, _ := row.Data.(*sessionEntry)
			if e == nil {
				return widgets.NewSizedBox(nil)
			}
			icon := ui.Icon("comment-discussion", 16, colors.DescriptionForeground)
			gap := widgets.NewSizedBox(nil, widgets.SizedBoxOpts{Width: foundation.Some(6.0)})
			if a.renaming == e.ID && a.renameIn != nil {
				return widgets.NewRow([]widgets.Widget{icon, gap, widgets.NewExpanded(a.renameBox(e))},
					widgets.RowOpts{CrossAxisAlignment: rendering.CrossAxisAlignmentCenter})
			}
			var tail widgets.Widget = widgets.NewText(ago(a.now(), e.Updated), widgets.TextOpts{Style: ui.TextStyle(bc, 11, colors.DescriptionForeground)})
			if st.Hovered || st.Focused {
				tail = widgets.NewRow([]widgets.Widget{
					ui.NewIconButton("edit", ui.IconButtonOpts{Tooltip: "Rename", OnPressed: func() { a.startRename(e.ID) }}),
					ui.NewIconButton("trash", ui.IconButtonOpts{Tooltip: "Delete from the List", OnPressed: func() { a.remove(e.ID) }}),
				}, widgets.RowOpts{MainAxisSize: rendering.MainAxisSizeMin})
			}
			return widgets.NewRow([]widgets.Widget{
				icon, gap,
				widgets.NewExpanded(widgets.NewText(e.label(), widgets.TextOpts{Overflow: painting.TextOverflowEllipsis, MaxLines: foundation.Some(1)})),
				gap, tail,
			}, widgets.RowOpts{CrossAxisAlignment: rendering.CrossAxisAlignmentCenter})
		},
	})
}

// renameBox is the input box that takes the place of the label of the session being renamed.
func (a *agents) renameBox(e *sessionEntry) widgets.Widget {
	return ui.NewInputBox(nil, nil, ui.InputBoxOpts{
		Controller: a.renameIn, Autofocus: true, Placeholder: "Name",
		OnSubmit: func(text string) { a.rename(e.ID, text) },
		OnFocusChange: func(focused bool) {
			if !focused && a.renaming == e.ID {
				a.rename(e.ID, a.renameIn.Value())
			}
		},
	})
}

// startRename turns the row of the session into an input box with its name.
func (a *agents) startRename(id string) {
	e := a.entry(id)
	if e == nil {
		return
	}
	a.stopRename()
	a.renaming = id
	a.renameIn = ui.NewInputBoxController(e.label())
	a.renameIn.FocusNode().SetOnKeyEvent(func(_ *widgets.FocusNode, ev services.KeyEvent) widgets.KeyEventResult {
		if _, down := ev.(*services.KeyDownEvent); down && ev.LogicalKey() == services.LogicalKeyboardKeyEscape {
			a.stopRename()
			a.sync()
			return widgets.KeyEventResultHandled
		}
		return widgets.KeyEventResultIgnored
	})
	if a.handle != nil {
		a.handle.Reveal(false)
	}
	a.sync()
}

// stopRename ends a rename without changing the name.
func (a *agents) stopRename() {
	a.renaming = ""
	// the box may still be on screen until the view is built again: its controller is let go of later
	a.renameIn = nil
}

// rename gives the session a name; an empty one takes the name away.
func (a *agents) rename(id, name string) {
	a.stopRename()
	e := a.entry(id)
	if e == nil {
		a.sync()
		return
	}
	e.Name = strings.TrimSpace(name)
	if e.Name == e.Title {
		e.Name = ""
	}
	for _, t := range a.tabs {
		if t.id == id {
			t.retitle()
		}
	}
	a.sync()
}

// newTab opens a session tab in the group and starts the program.
func (a *agents) newTab(group int) {
	if _, err := a.openNew(group); err != nil {
		a.api.Notifications().Warn("Cannot open a session: " + err.Error())
	}
}

// openNew opens a tab for a new session.
func (a *agents) openNew(group int) (extension.EditorTab, error) {
	id, err := newUUID(a.random)
	if err != nil {
		return nil, err
	}
	return a.api.Editors().Open(KindID, tabState{Session: id, Start: true, Mode: string(defaultMode)}, extension.EditorOpenOptions{Group: group})
}

// openSession shows a session: the tab that has it, or a new tab with the conversation the index kept.
func (a *agents) openSession(id string, group int) {
	for _, t := range a.tabs {
		if t.id == id {
			t.tab.Reveal(true)
			return
		}
	}
	e := a.entry(id)
	if e == nil {
		return
	}
	st := tabState{Session: id, Used: true, Model: e.Model, Mode: e.Mode, Items: e.Items}
	if _, err := a.api.Editors().Open(KindID, st, extension.EditorOpenOptions{Group: group}); err != nil {
		a.api.Notifications().Warn("Cannot open the session: " + err.Error())
	}
}

// newSession is the command that opens a session tab in the group named by the argument and starts the program.
func (a *agents) newSession(_ context.Context, args any) error {
	_, err := a.openNew(extension.GroupOfArgs(args))
	return err
}

// sessionArg reads the {"session": id} argument of the commands of the list.
func sessionArg(args any) (id, name string, hasName bool, err error) {
	m, ok := args.(map[string]any)
	if ok {
		id, _ = m["session"].(string)
		name, hasName = m["name"].(string)
	}
	if id == "" {
		return "", "", false, errors.New("SessionArgumentMissing", `the command needs {"session": id}`)
	}
	return id, name, hasName, nil
}

// openSessionCommand is the command that shows the session of the argument {"session": id}.
func (a *agents) openSessionCommand(_ context.Context, args any) error {
	id, _, _, err := sessionArg(args)
	if err != nil {
		return err
	}
	if a.entry(id) == nil {
		return errors.New("SessionUnknown", "the index has no such session", "session", id)
	}
	a.openSession(id, extension.GroupOfArgs(args))
	return nil
}

// renameCommand is the command that renames the session of {"session": id, "name": text}, or starts the rename in the list
// when it has no name.
func (a *agents) renameCommand(_ context.Context, args any) error {
	id, name, hasName, err := sessionArg(args)
	if err != nil {
		return err
	}
	if a.entry(id) == nil {
		return errors.New("SessionUnknown", "the index has no such session", "session", id)
	}
	if hasName {
		a.rename(id, name)
		return nil
	}
	a.startRename(id)
	return nil
}

// deleteCommand is the command that deletes the session of {"session": id} from the list and closes its tabs.
func (a *agents) deleteCommand(_ context.Context, args any) error {
	id, _, _, err := sessionArg(args)
	if err != nil {
		return err
	}
	if a.entry(id) == nil {
		return errors.New("SessionUnknown", "the index has no such session", "session", id)
	}
	a.remove(id)
	return nil
}
