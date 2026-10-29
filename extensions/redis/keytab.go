package redis

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
	"github.com/rm4n0s/emetos/extensions/internal/pageui"
	"github.com/rm4n0s/emetos/extensions/internal/resultgrid"
)

// The views of a string key (SPEC §10.23 "The key editor": text, a JSON view when it parses, hex for binary).
const (
	modeText = iota
	modeJSON
	modeHex
)

// gridSource shows a Collection to the grid. Its length changes only on the UI thread (sync), so that a page that a goroutine adds
// to the collection cannot make the grid read a row it has not been told about.
type gridSource struct {
	c Collection
	n int
}

func (g *gridSource) Columns() []resultgrid.Column { return g.c.Columns() }
func (g *gridSource) Len() int                     { return g.n }
func (g *gridSource) sync()                        { g.n = g.c.Len() }

func (g *gridSource) Row(i int, dst []resultgrid.Value) {
	if i < g.c.Len() {
		g.c.Row(i, dst)
	}
}

// loadResult is what the goroutine of reload found.
type loadResult struct {
	info         KeyInfo
	missing      bool
	disconnected bool
	err          error
	readOnly     bool

	str    StringValue
	stored string // a ReJSON value as the server holds it
	pretty string // and indented
	coll   Collection
}

// keyTab is the key editor: one tab for one key of one database (SPEC §10.23 "The key editor"). Its fields belong to the UI thread.
type keyTab struct {
	x   *Extension
	tab extension.EditorTab
	ref KeyRef

	// gen counts the loads, so that the answer of one that was replaced is dropped.
	gen          int
	loaded       bool
	loading      bool
	disconnected bool
	missing      bool
	problem      string
	readOnly     bool
	info         KeyInfo

	// string and ReJSON values
	str     StringValue
	stored  string
	mode    int
	shown   string // the text the editor was given; a different text in the editor is an edit
	dirty   bool
	editor  *ui.InputBoxController
	scroll  ui.ScrollHandle
	pretty  string
	jsonOK  bool
	hexText string

	// rows
	coll Collection
	src  *gridSource
	grid *resultgrid.Grid
	form *rowForm

	busy bool
	note pageui.Note

	// closed says that the tab was closed: what a goroutine brings back after that is dropped, for the controllers are disposed.
	closed bool
	stop   func()
}

// createKeyTab makes the content of a key editor tab, for a new one and for one that comes back after a restart.
func (x *Extension) createKeyTab(tab extension.EditorTab, state json.RawMessage) (extension.EditorTabContent, error) {
	var st keyState
	if len(state) == 0 {
		return extension.EditorTabContent{}, errors.New("RedisKeyStateMissing", "a key editor is opened with the key it shows")
	}
	if err := json.Unmarshal(state, &st); err != nil {
		return extension.EditorTabContent{}, errors.NewErr("RedisKeyStateInvalid", err)
	}
	t := &keyTab{x: x, tab: tab, ref: st.ref(), editor: ui.NewInputBoxController("")}
	t.grid = resultgrid.New(resultgrid.Options{OnCopy: x.copyText, OnFetchMore: t.fetchMore})
	x.keyTabs = append(x.keyTabs, t)
	t.setTitle()
	t.stop = x.hub.Subscribe(func() { x.api.Post(t.connectionChanged) })
	t.reload()
	return extension.EditorTabContent{
		Build:     t.build,
		SaveState: func() (any, error) { return stateOf(t.ref), nil },
		OnClose: func() {
			t.closed = true
			t.stop()
			t.editor.Dispose()
			t.grid.Dispose()
			if t.form != nil {
				t.form.dispose()
			}
			for i, o := range x.keyTabs {
				if o == t {
					x.keyTabs = append(x.keyTabs[:i], x.keyTabs[i+1:]...)
					break
				}
			}
		},
	}, nil
}

// setTitle names the tab after the key.
func (t *keyTab) setTitle() {
	name := t.ref.Key
	if i := strings.LastIndexAny(name, ":/"); i >= 0 && i < len(name)-1 {
		name = name[i+1:]
	}
	t.tab.SetTitle(name)
	hint := "db" + strconv.Itoa(t.ref.DB) + " " + t.ref.Key
	if c, ok := t.x.hub.Store().Get(t.ref.Conn); ok {
		hint = c.Name + " " + hint
	}
	t.tab.SetHint(hint)
}

// renamed follows the key to its new name.
func (t *keyTab) renamed(newKey string) {
	t.ref.Key = newKey
	t.setTitle()
	t.reload()
}

// connectionChanged follows the connection: a tab of a connection that closed says so, and loads again when it opens.
func (t *keyTab) connectionChanged() {
	if t.closed {
		return
	}
	connected := t.x.drv.Connected(t.ref.Conn)
	switch {
	case !connected && !t.disconnected:
		t.disconnected = true
		t.gen++
		t.loading = false
		t.tab.SetBusy(false)
		t.tab.Refresh()
	case connected && t.disconnected:
		t.reload()
	}
}

// reload reads the key again.
func (t *keyTab) reload() {
	if t.closed {
		return
	}
	t.gen++
	gen := t.gen
	t.loading, t.problem = true, ""
	t.tab.SetBusy(true)
	t.tab.Refresh()
	ref := t.ref // the goroutine reads this copy: a rename changes t.ref on the UI thread
	go func() {
		ctx, cancel := context.WithTimeout(t.x.ctx, opTimeout)
		defer cancel()
		res := t.load(ctx, ref)
		t.x.api.Post(func() {
			if gen == t.gen && !t.closed {
				t.apply(res)
			}
		})
	}()
}

// load reads the header and the first page of the value. It runs off the UI thread and touches only the collection it makes.
func (t *keyTab) load(ctx context.Context, ref KeyRef) loadResult {
	var res loadResult
	db, sess, err := t.x.drv.database(ref.Conn, ref.DB)
	if err != nil {
		res.err = err
		if appErr, ok := errors.FromError(err); ok && appErr.Tag == "RedisNotConnected" {
			res.disconnected = true
		}
		return res
	}
	res.readOnly = sess.conn.ReadOnly
	res.info, err = db.Info(ctx, ref.Key)
	if err != nil {
		res.err = err
		return res
	}
	switch res.info.Type {
	case TypeNone:
		res.missing = true
	case TypeString:
		res.str, res.err = LoadString(ctx, db, ref.Key)
	case TypeJSON:
		res.stored, res.pretty, res.err = LoadJSON(ctx, db, ref.Key)
	case TypeHash, TypeList, TypeSet, TypeZSet, TypeStream:
		res.coll, res.err = NewCollection(db, ref.Key, res.info.Type)
		if res.err == nil {
			_, res.err = res.coll.Fetch(ctx)
		}
	}
	return res
}

// apply shows what load found.
func (t *keyTab) apply(res loadResult) {
	t.loading, t.loaded = false, true
	t.tab.SetBusy(t.busy)
	t.disconnected, t.missing, t.readOnly, t.info = res.disconnected, res.missing, res.readOnly, res.info
	t.coll, t.src, t.form = nil, nil, nil
	if res.err != nil {
		t.problem = res.err.Error()
		if !res.disconnected {
			t.x.api.Log().Warn("the key cannot be read", "err", res.err)
		}
		t.tab.Refresh()
		return
	}
	switch res.info.Type {
	case TypeString:
		t.str, t.stored = res.str, ""
		t.pretty, t.jsonOK = res.str.PrettyJSON()
		t.hexText = ""
		switch {
		case res.str.Binary():
			t.setMode(modeHex)
		case t.jsonOK:
			t.setMode(modeJSON)
		default:
			t.setMode(modeText)
		}
	case TypeJSON:
		t.stored, t.pretty, t.jsonOK = res.stored, res.pretty, true
		t.str = StringValue{Data: res.stored, Size: int64(len(res.stored))}
		t.setMode(modeJSON)
	default:
		if res.coll != nil {
			t.coll = res.coll
			t.src = &gridSource{c: res.coll}
			t.src.sync()
			t.grid.SetSource(t.src)
			t.grid.SetMore(res.coll.More())
		}
	}
	t.setDirty(false)
	t.tab.Refresh()
}

// setMode puts the text of a view of the string in the editor.
func (t *keyTab) setMode(mode int) {
	t.mode = mode
	switch mode {
	case modeHex:
		if t.hexText == "" {
			t.hexText = HexDump(t.str.Data)
		}
		t.shown = t.hexText
	case modeJSON:
		t.shown = t.pretty
	default:
		t.shown = t.str.Data
	}
	t.editor.SetValue(t.shown)
}

// locked tells whether the editor refuses edits: hex, a value that was cut, a read-only connection.
func (t *keyTab) locked() bool {
	return t.mode == modeHex || t.str.Truncated || t.readOnly || t.disconnected
}

// editorChanged follows the text of the editor: an edit marks the tab, and a locked editor takes its text back.
func (t *keyTab) editorChanged(text string) {
	if text == t.shown {
		t.setDirty(false)
		return
	}
	if t.locked() {
		t.editor.SetValue(t.shown)
		return
	}
	t.setDirty(true)
}

// setDirty marks the tab and builds it again when the state changed.
func (t *keyTab) setDirty(d bool) {
	if t.dirty == d {
		return
	}
	t.dirty = d
	t.tab.SetDirty(d)
	t.tab.Refresh()
}

// ---- actions ----

// run does an operation off the UI thread with the database of the key, then done on the UI thread.
func (t *keyTab) run(what string, op func(ctx context.Context, db DB) error, done func()) {
	if t.busy {
		return
	}
	t.busy = true
	t.note = pageui.Fine(what + "...")
	t.tab.SetBusy(true)
	t.tab.Refresh()
	ref := t.ref
	go func() {
		ctx, cancel := context.WithTimeout(t.x.ctx, opTimeout)
		defer cancel()
		db, _, err := t.x.drv.database(ref.Conn, ref.DB)
		if err == nil {
			err = op(ctx, db)
		}
		t.x.api.Post(func() {
			if t.closed {
				return
			}
			t.busy = false
			t.tab.SetBusy(t.loading)
			if err != nil {
				t.x.api.Log().Warn("an operation of the key editor failed", "operation", what, "err", err)
				t.note = pageui.Bad(what + " failed: " + err.Error())
				t.tab.Refresh()
				return
			}
			t.note = pageui.Note{}
			done()
		})
	}()
}

// saveString writes the text of the editor.
func (t *keyTab) saveString() {
	text := t.editor.Value()
	if t.locked() || text == t.shown {
		return
	}
	key, typ, stored, loaded, mode, original := t.ref.Key, t.info.Type, t.stored, t.str, t.mode, t.str.Data
	t.run("Saving", func(ctx context.Context, db DB) error {
		switch {
		case typ == TypeJSON:
			return SaveJSON(ctx, db, key, stored, text)
		case mode == modeJSON:
			doc, err := JSONForSave(original, text)
			if err != nil {
				return err
			}
			return SaveString(ctx, db, key, loaded, doc)
		}
		return SaveString(ctx, db, key, loaded, text)
	}, func() {
		t.dirty = false
		t.tab.SetDirty(false)
		t.reload()
	})
}

// revert puts the loaded text back in the editor.
func (t *keyTab) revert() {
	t.editor.SetValue(t.shown)
	t.setDirty(false)
}

// fetchMore loads the next page of rows.
func (t *keyTab) fetchMore() {
	if t.coll == nil || t.busy || !t.coll.More() {
		return
	}
	coll, gen := t.coll, t.gen
	t.run("Loading", func(ctx context.Context, _ DB) error {
		_, err := coll.Fetch(ctx)
		return err
	}, func() {
		if gen != t.gen || coll != t.coll {
			return
		}
		t.src.sync()
		t.grid.Appended()
		t.grid.SetMore(coll.More())
		t.tab.Refresh()
	})
}

// selectedRows are the rows of the source the selection of the grid covers.
func (t *keyTab) selectedRows() []int {
	m := t.grid.Model()
	r0, _, r1, _, ok := m.Selection().Bounds(m.Len(), len(m.Columns()))
	if !ok {
		return nil
	}
	rows := make([]int, 0, r1-r0+1)
	for r := r0; r <= r1; r++ {
		rows = append(rows, m.SourceRow(r))
	}
	return rows
}

// openAddForm shows the form that adds a row.
func (t *keyTab) openAddForm() {
	if t.coll == nil || t.readOnly {
		return
	}
	t.closeForm()
	t.form = newRowForm("Add "+strings.ToLower(t.coll.AddNoun()), t.coll.AddForm())
	t.note = pageui.Note{}
	t.tab.Refresh()
}

// openEditForm shows the form that changes the selected row.
func (t *keyTab) openEditForm() {
	if t.coll == nil || t.readOnly {
		return
	}
	rows := t.selectedRows()
	if len(rows) != 1 {
		t.note = pageui.Bad("Select the one row to change.")
		t.tab.Refresh()
		return
	}
	fields, err := t.coll.EditForm(rows[0])
	if err != nil {
		t.note = pageui.Bad(err.Error())
		t.tab.Refresh()
		return
	}
	t.closeForm()
	t.form = newRowForm("Change the row", fields)
	t.form.Edit, t.form.Row = true, rows[0]
	t.note = pageui.Note{}
	t.tab.Refresh()
}

// closeForm hides the form.
func (t *keyTab) closeForm() {
	if t.form != nil {
		t.form.dispose()
		t.form = nil
	}
}

// submitForm applies the form.
func (t *keyTab) submitForm() {
	f, coll := t.form, t.coll
	if f == nil || coll == nil || t.busy {
		return
	}
	if m := f.missing(); m != "" {
		t.note = pageui.Bad(m + " is required.")
		t.tab.Refresh()
		return
	}
	values, edit, row := f.values(), f.Edit, f.Row
	t.run("Saving", func(ctx context.Context, _ DB) error {
		if edit {
			return coll.Edit(ctx, row, values)
		}
		return coll.Add(ctx, values)
	}, func() {
		t.closeForm()
		t.afterRowChange(coll)
	})
}

// afterRowChange shows the rows after a change: the collection that changes in place is shown again from its rows, one whose indexes
// move is loaded again.
func (t *keyTab) afterRowChange(coll Collection) {
	if coll != t.coll {
		return
	}
	if coll.NeedsReload() {
		t.reload()
		return
	}
	t.src.sync()
	t.grid.SetSource(t.src)
	t.grid.SetMore(coll.More())
	t.tab.Refresh()
}

// removeRows removes the selected rows; more than one asks first.
func (t *keyTab) removeRows() {
	if t.coll == nil || t.readOnly {
		return
	}
	rows := t.selectedRows()
	if len(rows) == 0 {
		t.note = pageui.Bad("Select the rows to remove.")
		t.tab.Refresh()
		return
	}
	remove := func() {
		coll := t.coll
		t.run("Removing", func(ctx context.Context, _ DB) error { return coll.Remove(ctx, rows) }, func() { t.afterRowChange(coll) })
	}
	if len(rows) == 1 {
		remove()
		return
	}
	t.x.api.Notifications().Warn("Remove "+strconv.Itoa(len(rows))+" rows from \""+t.ref.Key+"\"?",
		extension.NotificationAction{Label: "Remove", Run: remove})
}

// ---- the widgets ----

const (
	headPad = 16.0
	infoPx  = 12.0
)

// InfoLine writes the line under the key's name: the type, how much is in it, the TTL, the memory and the encoding.
func InfoLine(info KeyInfo) string {
	parts := []string{LabelOf(info.Type)}
	if n := info.Size; n >= 0 {
		switch info.Type {
		case TypeString:
			parts = append(parts, plural(n, "byte", "bytes"))
		case TypeHash:
			parts = append(parts, plural(n, "field", "fields"))
		case TypeList:
			parts = append(parts, plural(n, "item", "items"))
		case TypeSet, TypeZSet:
			parts = append(parts, plural(n, "member", "members"))
		case TypeStream:
			parts = append(parts, plural(n, "entry", "entries"))
		}
	}
	parts = append(parts, "TTL "+FormatTTL(info.TTLMillis))
	if info.Memory >= 0 {
		parts = append(parts, FormatBytes(info.Memory))
	}
	if info.Encoding != "" {
		parts = append(parts, info.Encoding)
	}
	return strings.Join(parts, "  ·  ")
}

// build makes the widget of the tab.
func (t *keyTab) build(bc widgets.BuildContext) widgets.Widget {
	colors := ui.Theme(bc)
	var body widgets.Widget
	switch {
	case t.loading && !t.loaded:
		body = t.message(bc, colors, "Loading...", false)
	case t.disconnected:
		body = t.message(bc, colors, "The connection is closed. Connect it in the Databases view, and this editor loads the key again.", false)
	case t.problem != "":
		body = t.message(bc, colors, "The key cannot be read: "+t.problem, true)
	case t.missing:
		body = t.message(bc, colors, "This key does not exist (any more).", false)
	case t.coll != nil:
		body = t.collectionBody(bc, colors)
	case t.info.Type == TypeString || t.info.Type == TypeJSON:
		body = t.stringBody(bc, colors)
	default:
		body = t.message(bc, colors, "The editor does not show keys of the type \""+t.info.Type+"\".", false)
	}
	col := widgets.NewColumn([]widgets.Widget{t.header(bc, colors), widgets.NewExpanded(body)},
		widgets.ColumnOpts{CrossAxisAlignment: rendering.CrossAxisAlignmentStretch})
	return widgets.NewColoredBox(colors.EditorBackground, col)
}

// message is a line in the middle of the body.
func (t *keyTab) message(bc widgets.BuildContext, colors ui.Colors, text string, bad bool) widgets.Widget {
	color := colors.DescriptionForeground
	if bad {
		color = colors.ErrorForeground
	}
	return widgets.NewPadding(painting.NewEdgeInsetsAll(headPad), widgets.NewAlign(
		widgets.NewText(text, widgets.TextOpts{Style: ui.TextStyle(bc, 13, color)}), widgets.AlignOpts{Alignment: painting.AlignmentTopLeft}))
}

// header is the name of the key, its facts and the actions on the key.
func (t *keyTab) header(bc widgets.BuildContext, colors ui.Colors) widgets.Widget {
	title := widgets.NewRow([]widgets.Widget{
		ui.Icon(IconOf(t.info.Type), 18, colors.Foreground), pageui.HGap(8),
		widgets.NewFlexible(widgets.NewText(t.ref.Key, widgets.TextOpts{
			Style: pageui.Bold(ui.TextStyle(bc, 15, colors.Foreground)), MaxLines: foundation.Some(1), Overflow: painting.TextOverflowEllipsis})),
		pageui.HGap(8),
		ui.NewIconButton("refresh", ui.IconButtonOpts{Tooltip: "Refresh", OnPressed: t.reload}),
		ui.NewIconButton("copy", ui.IconButtonOpts{Tooltip: "Copy the key", OnPressed: func() { t.x.copyText(t.ref.Key) }}),
	}, widgets.RowOpts{CrossAxisAlignment: rendering.CrossAxisAlignmentCenter})
	rows := []widgets.Widget{title}
	if t.loaded && !t.missing && t.problem == "" && !t.disconnected {
		rows = append(rows, pageui.Gap(4), widgets.NewText(InfoLine(t.info), widgets.TextOpts{Style: ui.TextStyle(bc, infoPx, colors.DescriptionForeground)}))
		off := t.readOnly || t.busy
		rows = append(rows, pageui.Gap(8), widgets.NewRow([]widgets.Widget{
			t.small("Rename", off, func() { t.x.renameKey(t.ref) }), pageui.HGap(6),
			t.small("Set TTL", off, func() { t.x.setTTL(t.ref) }), pageui.HGap(6),
			t.small("Persist", off || t.info.TTLMillis < 0, func() { t.x.persistKey(t.ref) }), pageui.HGap(6),
			t.small("Delete", off, func() { t.x.deleteKey(t.ref) }),
			pageui.HGap(12), t.readOnlyBadge(bc, colors),
		}, widgets.RowOpts{CrossAxisAlignment: rendering.CrossAxisAlignmentCenter}))
	}
	rows = append(rows, pageui.NoteLine(bc, colors, t.note))
	line := widgets.NewColoredBox(colors.PanelBorder, widgets.NewSizedBox(nil, widgets.SizedBoxOpts{Height: foundation.Some(1.0)}))
	return widgets.NewColumn([]widgets.Widget{
		widgets.NewPadding(painting.NewEdgeInsetsFromLTRB(headPad, headPad, headPad, 10),
			widgets.NewColumn(rows, widgets.ColumnOpts{CrossAxisAlignment: rendering.CrossAxisAlignmentStretch})),
		line,
	}, widgets.ColumnOpts{CrossAxisAlignment: rendering.CrossAxisAlignmentStretch})
}

// readOnlyBadge says that the connection refuses changes.
func (t *keyTab) readOnlyBadge(bc widgets.BuildContext, colors ui.Colors) widgets.Widget {
	if !t.readOnly {
		return pageui.Nothing()
	}
	return widgets.NewRow([]widgets.Widget{ui.Icon("lock", 12, colors.DescriptionForeground), pageui.HGap(4),
		widgets.NewText("Read-only connection", widgets.TextOpts{Style: ui.TextStyle(bc, infoPx, colors.DescriptionForeground)})},
		widgets.RowOpts{CrossAxisAlignment: rendering.CrossAxisAlignmentCenter})
}

// small is a small secondary button.
func (t *keyTab) small(label string, disabled bool, fn func()) widgets.Widget {
	return ui.NewButton(label, ui.ButtonOpts{Secondary: true, Small: true, Disabled: disabled, OnPressed: fn})
}

// stringBody is the editor of a string or a ReJSON value.
func (t *keyTab) stringBody(bc widgets.BuildContext, colors ui.Colors) widgets.Widget {
	isJSONKey := t.info.Type == TypeJSON
	var bar []widgets.Widget
	if !isJSONKey {
		mode := func(label string, m int, enabled bool) widgets.Widget {
			return ui.NewButton(label, ui.ButtonOpts{Small: true, Secondary: t.mode != m, Disabled: !enabled || t.dirty, OnPressed: func() { t.setMode(m); t.tab.Refresh() }})
		}
		bar = append(bar, mode("Text", modeText, !t.str.Binary()), pageui.HGap(4), mode("JSON", modeJSON, t.jsonOK), pageui.HGap(4), mode("Hex", modeHex, true))
	} else {
		bar = append(bar, widgets.NewText("JSON", widgets.TextOpts{Style: ui.TextStyle(bc, infoPx, colors.DescriptionForeground)}))
	}
	bar = append(bar, widgets.NewSpacer(),
		ui.NewButton("Revert", ui.ButtonOpts{Secondary: true, Small: true, Disabled: !t.dirty || t.busy, OnPressed: t.revert}), pageui.HGap(6),
		ui.NewButton("Save", ui.ButtonOpts{Small: true, Disabled: !t.dirty || t.locked() || t.busy, OnPressed: t.saveString}))
	rows := []widgets.Widget{widgets.NewRow(bar, widgets.RowOpts{CrossAxisAlignment: rendering.CrossAxisAlignmentCenter}), pageui.Gap(8)}
	if t.str.Truncated {
		rows = append(rows, pageui.Problem(bc, colors, "The value is "+FormatBytes(t.str.Size)+" long. The first "+FormatBytes(MaxStringBytes)+" are shown, and it cannot be saved."), pageui.Gap(8))
	}
	editor := ui.NewInputBox(nil, nil, ui.InputBoxOpts{Controller: t.editor, MultiLine: true, Monospace: true, OnChanged: t.editorChanged})
	scroll := ui.NewScrollView(widgets.NewPadding(painting.NewEdgeInsetsFromLTRB(0, 0, 8, 0), editor), &t.scroll)
	rows = append(rows, widgets.NewExpanded(scroll))
	return widgets.NewPadding(painting.NewEdgeInsetsAll(headPad), widgets.NewColumn(rows, widgets.ColumnOpts{CrossAxisAlignment: rendering.CrossAxisAlignmentStretch}))
}

// collectionBody is the grid of rows with the buttons that change them.
func (t *keyTab) collectionBody(bc widgets.BuildContext, colors ui.Colors) widgets.Widget {
	noun := t.coll.AddNoun()
	bar := []widgets.Widget{ui.NewButton("Add "+noun, ui.ButtonOpts{Small: true, Disabled: t.readOnly || t.busy, OnPressed: t.openAddForm}), pageui.HGap(6)}
	if t.coll.CanEdit() {
		bar = append(bar, t.small("Edit", t.readOnly || t.busy, t.openEditForm), pageui.HGap(6))
	}
	bar = append(bar, t.small("Remove", t.readOnly || t.busy, t.removeRows))
	rows := []widgets.Widget{
		widgets.NewPadding(painting.NewEdgeInsetsFromLTRB(headPad, 10, headPad, 8),
			widgets.NewRow(bar, widgets.RowOpts{CrossAxisAlignment: rendering.CrossAxisAlignmentCenter})),
		widgets.NewExpanded(t.grid.Widget()),
	}
	if t.form != nil {
		rows = append(rows, t.formPanel(bc, colors))
	}
	return widgets.NewColumn(rows, widgets.ColumnOpts{CrossAxisAlignment: rendering.CrossAxisAlignmentStretch})
}

// formPanel is the form under the grid.
func (t *keyTab) formPanel(bc widgets.BuildContext, colors ui.Colors) widgets.Widget {
	f := t.form
	children := []widgets.Widget{
		widgets.NewText(f.Title, widgets.TextOpts{Style: pageui.Bold(ui.TextStyle(bc, 13, colors.Foreground))}), pageui.Gap(8),
	}
	children = append(children, f.widgetsOf(bc, colors, t.tab.Refresh, t.submitForm)...)
	children = append(children, widgets.NewRow([]widgets.Widget{
		ui.NewButton("Save", ui.ButtonOpts{Small: true, Disabled: t.busy, OnPressed: t.submitForm}), pageui.HGap(6),
		ui.NewButton("Cancel", ui.ButtonOpts{Small: true, Secondary: true, OnPressed: func() { t.closeForm(); t.tab.Refresh() }}),
	}, widgets.RowOpts{}))
	line := widgets.NewColoredBox(colors.PanelBorder, widgets.NewSizedBox(nil, widgets.SizedBoxOpts{Height: foundation.Some(1.0)}))
	inner := widgets.NewConstrainedBox(rendering.NewBoxConstraints(rendering.BoxConstraintsOpts{MaxWidth: foundation.Some(formMaxWidth)}),
		widgets.NewColumn(children, widgets.ColumnOpts{CrossAxisAlignment: rendering.CrossAxisAlignmentStretch}))
	return widgets.NewColumn([]widgets.Widget{line, widgets.NewPadding(painting.NewEdgeInsetsAll(headPad),
		widgets.NewAlign(inner, widgets.AlignOpts{Alignment: painting.AlignmentTopLeft}))},
		widgets.ColumnOpts{CrossAxisAlignment: rendering.CrossAxisAlignmentStretch})
}
