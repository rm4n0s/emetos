package notes

import (
	"encoding/json"

	"github.com/rm4n0s/errors"
	"github.com/rm4n0s/glutter/foundation"
	"github.com/rm4n0s/glutter/painting"
	"github.com/rm4n0s/glutter/rendering"
	"github.com/rm4n0s/glutter/widgets"

	"github.com/rm4n0s/emetos/extension"
	"github.com/rm4n0s/emetos/extension/ui"
)

// noteTab is an editor tab that shows a note: a title and a body, both edited in place.
type noteTab struct {
	m     *notes
	tab   extension.EditorTab
	note  *note
	title *ui.InputBoxController
	body  *ui.InputBoxController
	// setting is true while the tab writes a change it got from another tab of the same note into its boxes.
	setting bool
}

// createTab makes the content of a tab, for a new tab and for one that came back after a restart.
func (m *notes) createTab(tab extension.EditorTab, state json.RawMessage) (extension.EditorTabContent, error) {
	var st tabState
	if len(state) > 0 {
		if err := json.Unmarshal(state, &st); err != nil {
			return extension.EditorTabContent{}, errors.NewErr("NotesTabStateInvalid", err)
		}
	}
	if st.Note == nil || st.Note.ID == "" {
		return extension.EditorTabContent{}, errors.New("NotesTabStateInvalid", "the state of a note tab has no note")
	}
	n := m.find(st.Note.ID)
	if n == nil {
		// the list did not come back (or the note is new): the tab's own copy is the note
		n = st.Note
		m.list = append(m.list, n)
		m.sync()
	}
	t := &noteTab{m: m, tab: tab, note: n, title: ui.NewInputBoxController(n.Title), body: ui.NewInputBoxController(n.Text)}
	m.tabs[n.ID] = append(m.tabs[n.ID], t)
	tab.SetTitle(n.name())
	return extension.EditorTabContent{
		Build:     t.build,
		SaveState: func() (any, error) { return tabState{Note: t.note}, nil },
		OnClose:   t.close,
	}, nil
}

// close forgets the tab.
func (t *noteTab) close() {
	list := t.m.tabs[t.note.ID]
	for i, x := range list {
		if x == t {
			t.m.tabs[t.note.ID] = append(list[:i:i], list[i+1:]...)
			break
		}
	}
	t.title.Dispose()
	t.body.Dispose()
}

// changed is called when the user edits the title or the body.
func (t *noteTab) changed() {
	if t.setting {
		return
	}
	t.note.Title, t.note.Text = t.title.Value(), t.body.Value()
	t.tab.SetTitle(t.note.name())
	for _, o := range t.m.tabs[t.note.ID] {
		if o != t {
			o.pull()
		}
	}
	t.m.sync()
}

// pull shows the text of the note, which another tab changed.
func (t *noteTab) pull() {
	t.setting = true
	defer func() { t.setting = false }()
	if t.title.Value() != t.note.Title {
		t.title.SetValue(t.note.Title)
	}
	if t.body.Value() != t.note.Text {
		t.body.SetValue(t.note.Text)
	}
	t.tab.SetTitle(t.note.name())
}

// build is the widget of the tab.
func (t *noteTab) build(bc widgets.BuildContext) widgets.Widget {
	colors := ui.Theme(bc)
	title := ui.NewInputBox(nil, nil, ui.InputBoxOpts{
		Controller: t.title, Placeholder: "Title", OnChanged: func(string) { t.changed() }, Autofocus: t.note.Title == "" && t.note.Text == "",
	})
	body := ui.NewInputBox(nil, nil, ui.InputBoxOpts{
		Controller: t.body, Placeholder: "Write a note...", MultiLine: true, OnChanged: func(string) { t.changed() },
	})
	return widgets.NewColoredBox(colors.EditorBackground, widgets.NewPadding(painting.NewEdgeInsetsFromLTRB(24, 16, 24, 16), widgets.NewColumn([]widgets.Widget{
		title,
		widgets.NewSizedBox(nil, widgets.SizedBoxOpts{Height: foundation.Some(10.0)}),
		widgets.NewExpanded(widgets.NewSingleChildScrollView(body)),
	}, widgets.ColumnOpts{CrossAxisAlignment: rendering.CrossAxisAlignmentStretch})))
}
