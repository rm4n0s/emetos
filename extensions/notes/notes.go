// Package notes is the extension that keeps the notes of a workspace (SPEC §10.15 "As built (step 11.6)"). It is the pattern
// of an agent extension that needs no network: a side bar view that lists its items (kept in the state of the view, which
// the window saves per workspace), an editor tab kind that shows one item and of which several are open at once, and an
// editor title action that makes a new one in the group whose tab strip the icon is in.
package notes

import (
	"context"
	"encoding/json"
	"strconv"
	"strings"

	"github.com/rm4n0s/errors"
	"github.com/rm4n0s/glutter/foundation"
	"github.com/rm4n0s/glutter/painting"
	"github.com/rm4n0s/glutter/rendering"
	"github.com/rm4n0s/glutter/widgets"

	"github.com/rm4n0s/emetos/extension"
	"github.com/rm4n0s/emetos/extension/ui"
)

// The ids of the extension.
const (
	// ID is the id of the extension.
	ID = "emetos.notes"
	// ContainerID and ViewID are the container and the view of the side bar, KindID the kind of the editor tab.
	ContainerID = "emetos.notes.container"
	ViewID      = "emetos.notes.list"
	KindID      = "emetos.notes.note"
	// NewNoteCommand makes a note and opens it in a tab; DeleteNoteCommand deletes the note it is given ({"note": id}).
	NewNoteCommand    = "emetos.notes.newNote"
	DeleteNoteCommand = "emetos.notes.deleteNote"
)

// untitled is the name of a note that has no title.
const untitled = "Untitled Note"

// New is the Factory of the extension.
func New() extension.Extension { return &notes{} }

// Manifest implements extension.Extension.
func (*notes) Manifest() extension.Manifest {
	return extension.Manifest{
		ID: ID, Name: "Notes", Version: "0.1.0",
		Description: "Keeps notes for the workspace: a list in the side bar, a note in an editor tab, and a New Note icon in the tab strip.",
		Contributes: extension.Contributes{
			Commands: []extension.CommandSpec{
				{ID: NewNoteCommand, Title: "New Note", Category: "Notes", Description: "Makes a note and opens it in an editor tab."},
				{ID: DeleteNoteCommand, Title: "Delete Note", Category: "Notes", Description: "Deletes the note it is given and closes its tabs."},
			},
			ViewContainers: []extension.ViewContainerSpec{{ID: ContainerID, Title: "Notes", Icon: "note", Order: 60}},
			Views:          []extension.ViewSpec{{ID: ViewID, Name: "Notes", Container: ContainerID}},
			ViewActions:    []extension.ViewActionSpec{{View: ViewID, Command: NewNoteCommand, Icon: "add", Tooltip: "New Note", Order: 1}},
			EditorKinds:    []extension.EditorKindSpec{{ID: KindID, Name: "Note", Icon: "note"}},
			EditorActions:  []extension.EditorActionSpec{{Command: NewNoteCommand, Icon: "new-file", Tooltip: "New Note", Order: 10}},
		},
	}
}

// note is a note.
type note struct {
	ID    string `json:"id"`
	Title string `json:"title"`
	Text  string `json:"text"`
}

// name is what the list and the tab call the note.
func (n *note) name() string {
	if t := strings.TrimSpace(n.Title); t != "" {
		return t
	}
	return untitled
}

// listState is the state of the view: the notes of the workspace.
type listState struct {
	Notes []*note `json:"notes"`
}

// notes is the extension of one scope. Everything in it runs on the UI thread.
type notes struct {
	api    extension.API
	list   []*note
	tabs   map[string][]*noteTab
	handle extension.ViewHandle
	model  *ui.ListViewModel
}

// Activate implements extension.Extension.
func (m *notes) Activate(_ context.Context, api extension.API) error {
	m.api = api
	m.tabs = map[string][]*noteTab{}
	m.model = ui.NewListViewModel()
	var err error
	m.handle, err = api.Views().Provide(ViewID, extension.ViewContent{Build: m.build, SaveState: m.saveState, RestoreState: m.restoreState})
	if err != nil {
		return err
	}
	if err := api.Editors().Provide(KindID, extension.EditorKind{Create: m.createTab}); err != nil {
		return err
	}
	if err := api.Commands().Handle(NewNoteCommand, m.newNote); err != nil {
		return err
	}
	return api.Commands().Handle(DeleteNoteCommand, m.deleteNote)
}

// saveState implements the state of the view.
func (m *notes) saveState() (any, error) { return listState{Notes: m.list}, nil }

// restoreState gives the view the notes of the last session. The notes the tabs made already stay.
func (m *notes) restoreState(raw json.RawMessage) error {
	var st listState
	if err := json.Unmarshal(raw, &st); err != nil {
		return errors.NewErr("NotesStateInvalid", err)
	}
	for _, n := range st.Notes {
		if n != nil && n.ID != "" && m.find(n.ID) == nil {
			m.list = append(m.list, n)
		}
	}
	m.sync()
	return nil
}

// find is the note with the id.
func (m *notes) find(id string) *note {
	for _, n := range m.list {
		if n.ID == id {
			return n
		}
	}
	return nil
}

// nextID is an id no note has.
func (m *notes) nextID() string {
	for i := len(m.list) + 1; ; i++ {
		id := "n" + strconv.Itoa(i)
		if m.find(id) == nil {
			return id
		}
	}
}

// sync makes the list show the notes.
func (m *notes) sync() {
	items := make([]ui.ListViewItem, len(m.list))
	for i, n := range m.list {
		items[i] = ui.ListViewItem{Key: n.ID, Data: n, Label: n.name()}
	}
	if err := m.model.SetItems(items); err != nil {
		m.api.Log().Warn("the list of notes cannot be shown", "err", err)
	}
	if m.handle != nil {
		m.handle.Refresh()
	}
}

// build is the list of the side bar.
func (m *notes) build(bc widgets.BuildContext) widgets.Widget {
	colors := ui.Theme(bc)
	if len(m.list) == 0 {
		return widgets.NewPadding(painting.NewEdgeInsetsAll(12), widgets.NewText(
			"No notes yet. Use New Note to make one.", widgets.TextOpts{Style: ui.TextStyle(bc, 13, colors.DescriptionForeground)}))
	}
	open := func(row ui.ListViewRow) {
		if n, ok := row.Data.(*note); ok {
			m.open(n)
		}
	}
	return ui.NewList(m.model, ui.ListOpts{
		OnSelect: open, OnOpen: open,
		Render: func(bc widgets.BuildContext, row ui.ListViewRow, st ui.RowState) widgets.Widget {
			n, _ := row.Data.(*note)
			if n == nil {
				return widgets.NewSizedBox(nil)
			}
			return widgets.NewRow([]widgets.Widget{
				ui.Icon("note", 16, colors.DescriptionForeground),
				widgets.NewSizedBox(nil, widgets.SizedBoxOpts{Width: foundation.Some(6.0)}),
				widgets.NewExpanded(widgets.NewText(n.name(), widgets.TextOpts{Overflow: painting.TextOverflowEllipsis, MaxLines: foundation.Some(1)})),
				ui.NewIconButton("trash", ui.IconButtonOpts{Tooltip: "Delete Note", OnPressed: func() { m.remove(n.ID) }}),
			}, widgets.RowOpts{CrossAxisAlignment: rendering.CrossAxisAlignmentCenter})
		},
	})
}

// newNote is the command that makes a note and opens it in the group named by the argument.
func (m *notes) newNote(_ context.Context, args any) error {
	n := &note{ID: m.nextID()}
	m.list = append(m.list, n)
	m.sync()
	_, err := m.openTab(n, extension.GroupOfArgs(args))
	return err
}

// deleteNote is the command that deletes the note of the argument {"note": id}.
func (m *notes) deleteNote(_ context.Context, args any) error {
	if a, ok := args.(map[string]any); ok {
		if id, ok := a["note"].(string); ok {
			m.remove(id)
		}
	}
	return nil
}

// remove deletes a note and closes its tabs.
func (m *notes) remove(id string) {
	n := m.find(id)
	if n == nil {
		return
	}
	for i, x := range m.list {
		if x == n {
			m.list = append(m.list[:i:i], m.list[i+1:]...)
			break
		}
	}
	tabs := m.tabs[id]
	delete(m.tabs, id)
	for _, t := range tabs {
		t.tab.Close()
	}
	m.sync()
}

// open shows a note: the tab that has it, or a new tab in the active group.
func (m *notes) open(n *note) {
	if tabs := m.tabs[n.ID]; len(tabs) > 0 {
		tabs[0].tab.Reveal(true)
		return
	}
	if _, err := m.openTab(n, 0); err != nil {
		m.api.Notifications().Warn("Cannot open the note: " + err.Error())
	}
}

// tabState is the state of a note tab: the id, with the note's text as a fallback for a list that did not come back.
type tabState struct {
	Note *note `json:"note"`
}

// openTab opens a tab for a note in a group.
func (m *notes) openTab(n *note, group int) (extension.EditorTab, error) {
	return m.api.Editors().Open(KindID, tabState{Note: n}, extension.EditorOpenOptions{Group: group})
}
