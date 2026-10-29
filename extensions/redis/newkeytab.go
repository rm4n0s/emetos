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
)

// newKeyState is the state of a New Key form: where the key is made, and the folder the form was asked in.
type newKeyState struct {
	Conn   string `json:"conn"`
	DB     int    `json:"db"`
	Prefix string `json:"prefix,omitempty"`
}

// newKeyTab is the New Key form (SPEC §10.23 "The explorer": New Key is a form with the type and the first value).
type newKeyTab struct {
	x     *Extension
	tab   extension.EditorTab
	state newKeyState

	name   *ui.InputBoxController
	ttl    *ui.InputBoxController
	typ    int
	form   *rowForm
	note   pageui.Note
	busy   bool
	scroll ui.ScrollHandle
	// closed says that the tab was closed while the key was being made.
	closed bool
}

// createNewKeyTab makes the content of a New Key tab.
func (x *Extension) createNewKeyTab(tab extension.EditorTab, state json.RawMessage) (extension.EditorTabContent, error) {
	var st newKeyState
	if len(state) == 0 {
		return extension.EditorTabContent{}, errors.New("RedisNewKeyStateMissing", "the New Key form is opened with the connection it makes the key in")
	}
	if err := json.Unmarshal(state, &st); err != nil {
		return extension.EditorTabContent{}, errors.NewErr("RedisNewKeyStateInvalid", err)
	}
	t := &newKeyTab{x: x, tab: tab, state: st, name: ui.NewInputBoxController(st.Prefix), ttl: ui.NewInputBoxController("")}
	t.chooseType(0)
	x.newKeyTabs = append(x.newKeyTabs, t)
	tab.SetTitle("New Key")
	hint := "db" + strconv.Itoa(st.DB)
	if c, ok := x.hub.Store().Get(st.Conn); ok {
		hint = c.Name + " " + hint
	}
	tab.SetHint("New key in " + hint)
	return extension.EditorTabContent{
		Build:     t.build,
		SaveState: func() (any, error) { return t.state, nil },
		OnClose: func() {
			t.closed = true
			t.name.Dispose()
			t.ttl.Dispose()
			t.form.dispose()
			for i, o := range x.newKeyTabs {
				if o == t {
					x.newKeyTabs = append(x.newKeyTabs[:i], x.newKeyTabs[i+1:]...)
					break
				}
			}
		},
	}, nil
}

// chooseType shows the fields of the first value of a type.
func (t *newKeyTab) chooseType(i int) {
	t.typ = i
	if t.form != nil {
		t.form.dispose()
	}
	t.form = newRowForm("", NewKeyFields(KeyTypes[i].Type))
}

// create checks the form and makes the key.
func (t *newKeyTab) create() {
	if t.busy {
		return
	}
	key := t.name.Value()
	if key == "" {
		t.note = pageui.Bad("A key needs a name.")
		t.tab.Refresh()
		return
	}
	if m := t.form.missing(); m != "" {
		t.note = pageui.Bad(m + " is required.")
		t.tab.Refresh()
		return
	}
	var ttl time.Duration
	if txt := strings.TrimSpace(t.ttl.Value()); txt != "" {
		d, err := ParseTTLSeconds(txt)
		if err != nil {
			t.note = pageui.Bad(err.Error())
			t.tab.Refresh()
			return
		}
		ttl = d
	}
	typ, values, st := KeyTypes[t.typ].Type, t.form.values(), t.state
	t.busy, t.note = true, pageui.Fine("Creating...")
	t.tab.SetBusy(true)
	t.tab.Refresh()
	go func() {
		ctx, cancel := context.WithTimeout(t.x.ctx, opTimeout)
		defer cancel()
		db, _, err := t.x.drv.database(st.Conn, st.DB)
		if err == nil {
			err = CreateKey(ctx, db, key, typ, values, ttl)
		}
		if err == nil {
			if s := t.x.state(st.Conn, st.DB); s != nil {
				err = s.addKey(ctx, key)
			}
		}
		t.x.api.Post(func() {
			if t.closed {
				if err == nil {
					t.x.keyCreated(KeyRef{Conn: st.Conn, DB: st.DB, Key: key})
				}
				return
			}
			t.busy = false
			t.tab.SetBusy(false)
			if err != nil {
				t.x.api.Log().Warn("a key cannot be made", "err", err)
				t.note = pageui.Bad(err.Error())
				t.tab.Refresh()
				return
			}
			t.tab.Close()
			t.x.keyCreated(KeyRef{Conn: st.Conn, DB: st.DB, Key: key})
		})
	}()
}

// build makes the form.
func (t *newKeyTab) build(bc widgets.BuildContext) widgets.Widget {
	colors := ui.Theme(bc)
	readOnly := false
	if c, ok := t.x.hub.Store().Get(t.state.Conn); ok {
		readOnly = c.ReadOnly
	}
	types := make([]ui.DropdownOption, len(KeyTypes))
	for i, k := range KeyTypes {
		types[i] = ui.DropdownOption{Label: k.Label}
	}
	label := func(s string) widgets.Widget {
		return widgets.NewText(s, widgets.TextOpts{Style: ui.TextStyle(bc, 12, colors.DescriptionForeground)})
	}
	rows := []widgets.Widget{
		widgets.NewText("New key", widgets.TextOpts{Style: pageui.Bold(ui.TextStyle(bc, 18, colors.Foreground))}), pageui.Gap(14),
		label("Name"), pageui.Gap(4),
		ui.NewInputBox(nil, nil, ui.InputBoxOpts{Controller: t.name, Placeholder: "user:42:name", Autofocus: true, OnSubmit: func(string) { t.create() }}), pageui.Gap(10),
		label("Type"), pageui.Gap(4),
		ui.NewDropdown(types, ui.DropdownOpts{Selected: t.typ, Width: foundation.Some(220.0), OnChanged: func(i int) { t.chooseType(i); t.tab.Refresh() }}),
		pageui.Gap(10),
	}
	rows = append(rows, t.form.widgetsOf(bc, colors, t.tab.Refresh, t.create)...)
	rows = append(rows, label("Time to live in seconds (empty: the key does not expire)"), pageui.Gap(4),
		ui.NewInputBox(nil, nil, ui.InputBoxOpts{Controller: t.ttl, OnSubmit: func(string) { t.create() }}), pageui.Gap(16))
	if readOnly {
		rows = append(rows, pageui.Problem(bc, colors, "This connection is read-only: it cannot make keys."), pageui.Gap(8))
	}
	rows = append(rows, widgets.NewRow([]widgets.Widget{
		ui.NewButton("Create", ui.ButtonOpts{Disabled: t.busy || readOnly, OnPressed: t.create}), pageui.HGap(8),
		ui.NewButton("Cancel", ui.ButtonOpts{Secondary: true, OnPressed: t.tab.Close}),
	}, widgets.RowOpts{}), pageui.NoteLine(bc, colors, t.note))
	body := widgets.NewColumn(rows, widgets.ColumnOpts{CrossAxisAlignment: rendering.CrossAxisAlignmentStretch})
	page := widgets.NewPadding(painting.NewEdgeInsetsAll(24), widgets.NewAlign(
		widgets.NewConstrainedBox(rendering.NewBoxConstraints(rendering.BoxConstraintsOpts{MaxWidth: foundation.Some(formMaxWidth)}), body),
		widgets.AlignOpts{Alignment: painting.AlignmentTopLeft}))
	return widgets.NewColoredBox(colors.EditorBackground, ui.NewScrollView(page, &t.scroll))
}
