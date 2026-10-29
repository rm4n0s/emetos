package dbconn

import (
	"encoding/json"
	"strings"

	"github.com/rm4n0s/errors"
	"github.com/rm4n0s/glutter/foundation"
	"github.com/rm4n0s/glutter/painting"
	"github.com/rm4n0s/glutter/rendering"
	"github.com/rm4n0s/glutter/widgets"

	"github.com/rm4n0s/emetos/extension"
	"github.com/rm4n0s/emetos/extension/ui"
	"github.com/rm4n0s/emetos/extensions/internal/pageui"
)

// formOpen is the state a form tab is opened with, and the state it saves: the draft of the form, which has no password.
type formOpen struct {
	Draft Draft `json:"draft"`
}

// The look of the form.
const (
	formWidth    = 520.0
	formLabelPx  = 12.0
	formSwatchPx = 18.0
)

// formTab is the connection form: an editor tab with the fields of a connection and the buttons Save, Test and Cancel.
type formTab struct {
	e     *Extension
	tab   extension.EditorTab
	model *FormModel

	inputs map[string]*ui.InputBoxController
	scroll ui.ScrollHandle

	// showProblems turns on the messages next to the fields, after the first try to save.
	showProblems bool
	// note is the answer of the last action under the buttons; busy is true while the hub works.
	note pageui.Note
	busy bool
}

// createForm makes the content of a form tab, for a new one and for one that comes back after a restart. The state is a
// formOpen; a tab restored before the extension of its engine is active gets a form without the fields of the engine, which
// appear when the extension comes.
func (e *Extension) createForm(tab extension.EditorTab, state json.RawMessage) (extension.EditorTabContent, error) {
	var open formOpen
	if len(state) == 0 {
		return extension.EditorTabContent{}, errors.New("ConnectionFormStateMissing", "a connection form is opened with the draft of its connection")
	}
	if err := json.Unmarshal(state, &open); err != nil {
		return extension.EditorTabContent{}, errors.NewErr("ConnectionFormStateInvalid", err)
	}
	eng, _, ok := e.hub.Engine(open.Draft.Engine)
	if !ok {
		eng = Engine{ID: open.Draft.Engine, Name: open.Draft.Engine}
	}
	f := &formTab{e: e, tab: tab, model: FormFromDraft(eng, open.Draft), inputs: map[string]*ui.InputBoxController{}}
	e.forms = append(e.forms, f)
	f.setTitle()
	stop := e.hub.Subscribe(func() { e.api.Post(tab.Refresh) })
	return extension.EditorTabContent{
		Build:     f.build,
		SaveState: func() (any, error) { return formOpen{Draft: f.model.Draft()}, nil },
		OnClose: func() {
			stop()
			for _, in := range f.inputs {
				in.Dispose()
			}
		},
	}, nil
}

// openForm opens a tab with a form.
func (e *Extension) openForm(open formOpen) {
	tab, err := e.api.Editors().Open(FormKindID, open, extension.EditorOpenOptions{})
	if err != nil {
		e.api.Log().Warn("the connection form cannot be opened", "err", err)
		e.api.Notifications().Error("Cannot open the connection form: " + err.Error())
		return
	}
	tab.Reveal(true)
}

// setTitle names the tab after what the form does.
func (f *formTab) setTitle() {
	switch {
	case f.model.ID != "":
		f.tab.SetTitle("Edit " + strings.TrimSpace(f.model.Name))
	default:
		f.tab.SetTitle("New " + f.model.Engine.Name + " Connection")
	}
	f.tab.SetHint(f.model.Engine.Name + " connection")
}

// input returns the controller of a text field, made with the value the model has.
func (f *formTab) input(key, initial string) *ui.InputBoxController {
	c, ok := f.inputs[key]
	if !ok {
		c = ui.NewInputBoxController(initial)
		f.inputs[key] = c
	}
	return c
}

// problemOf is the message for a field, when the form shows them.
func (f *formTab) problemOf(field string) string {
	if !f.showProblems {
		return ""
	}
	for _, p := range f.model.Problems() {
		if p.Field == field {
			return p.Message
		}
	}
	return ""
}

// edited marks the tab as having work in it.
func (f *formTab) edited() {
	f.tab.SetDirty(true)
	f.note = pageui.Note{}
}

// ---- actions ----

// submit checks the form and saves the connection through the hub.
func (f *formTab) submit() {
	if f.busy {
		return
	}
	f.syncEngine()
	f.showProblems = true
	if problems := f.model.Problems(); len(problems) > 0 {
		f.note = pageui.Bad(problems[0].Message)
		f.tab.Refresh()
		return
	}
	conn, pw := f.model.Connection(), f.model.PasswordChange()
	f.busy, f.note = true, pageui.Note{}
	f.tab.SetBusy(true)
	f.tab.Refresh()
	go func() {
		kept, res, err := f.e.hub.Save(f.e.ctx, conn, pw)
		f.e.api.Post(func() {
			f.busy = false
			f.tab.SetBusy(false)
			if err != nil {
				f.note = pageui.Bad(err.Error())
				f.tab.Refresh()
				return
			}
			f.model.ID = kept.ID
			f.tellPasswordFate(kept, res)
			f.tab.SetDirty(false)
			f.tab.Close()
		})
	}()
}

// tellPasswordFate tells the user when the password is held in memory only, or the keyring did not do what was asked.
func (f *formTab) tellPasswordFate(c Connection, res SaveResult) {
	n := f.e.api.Notifications()
	if res.Saved.KeyringErr != nil {
		f.e.api.Log().Warn("the keyring did not keep a password", "err", res.Saved.KeyringErr)
		n.Warn("No keyring answered (" + res.Saved.KeyringErr.Error() + "): the password of \"" + c.Name +
			"\" is held in memory for this session only and is not saved.")
	}
	if res.KeyringErr != nil {
		f.e.api.Log().Warn("the keyring did not remove a password", "err", res.KeyringErr)
		n.Warn("The keyring did not remove the old password of \"" + c.Name + "\": " + res.KeyringErr.Error())
	}
}

// test opens the connection and closes it again.
func (f *formTab) test() {
	if f.busy {
		return
	}
	f.syncEngine()
	f.showProblems = true
	if problems := f.model.Problems(); len(problems) > 0 {
		f.note = pageui.Bad(problems[0].Message)
		f.tab.Refresh()
		return
	}
	conn := f.model.Connection()
	password := f.model.Password
	id := f.model.ID
	f.busy, f.note = true, pageui.Fine("Testing...")
	f.tab.SetBusy(true)
	f.tab.Refresh()
	go func() {
		if !f.model.PasswordSet && id != "" {
			if res, err := f.e.hub.Passwords().Resolve(f.e.ctx, conn); err == nil {
				password = res.Password
			}
		}
		err := f.e.hub.Test(f.e.ctx, conn, password)
		f.e.api.Post(func() {
			f.busy = false
			f.tab.SetBusy(false)
			if err != nil {
				f.note = pageui.Bad("The connection failed: " + err.Error())
			} else {
				f.note = pageui.Fine("The connection works.")
			}
			f.tab.Refresh()
		})
	}()
}

// syncEngine takes the engine from the hub when the form was restored before its extension was active.
func (f *formTab) syncEngine() {
	if len(f.model.Engine.Fields) == 0 && f.model.Engine.DefaultPort == 0 && !f.model.Engine.File {
		if eng, _, ok := f.e.hub.Engine(f.model.Engine.ID); ok {
			f.model.Engine = eng
		}
	}
}

// browse asks for a file with the dialog of the desktop and puts it in the field.
func (f *formTab) browse(title string, set func(string), ctl *ui.InputBoxController) {
	go func() {
		path, err := f.e.api.FileChooser().Open(f.e.ctx, extension.FileChooserOptions{Title: title, AcceptLabel: "Choose"})
		f.e.api.Post(func() {
			if err != nil {
				f.note = pageui.Bad("The file dialog failed: " + err.Error())
				f.tab.Refresh()
				return
			}
			if path == "" {
				return
			}
			set(path)
			ctl.SetValue(path)
			f.edited()
			f.tab.Refresh()
		})
	}()
}

// ---- the widgets ----

// build makes the form.
func (f *formTab) build(bc widgets.BuildContext) widgets.Widget {
	f.syncEngine()
	colors := ui.Theme(bc)
	m := f.model
	e := m.Engine
	var rows []widgets.Widget
	title := "New " + e.Name + " connection"
	if m.ID != "" {
		title = "Edit the connection"
	}
	rows = append(rows, widgets.NewText(title, widgets.TextOpts{Style: pageui.Bold(ui.TextStyle(bc, 18, colors.Foreground))}), pageui.Gap(14))

	rows = append(rows, f.textField(bc, colors, fieldSpec{key: "name", label: "Name", placeholder: "Production", value: m.Name, set: func(s string) { m.Name = s }}))
	rows = append(rows, f.scopeField(bc, colors))
	switch {
	case e.File:
		rows = append(rows, f.fileField(bc, colors, "file", "Database file", "/path/to/database.db", m.File, func(s string) { m.File = s }))
	default:
		if e.usesURL() {
			rows = append(rows, f.urlField(bc, colors))
		}
		rows = append(rows, f.hostPortRow(bc, colors))
		if !e.NoUser {
			rows = append(rows, f.textField(bc, colors, fieldSpec{key: "user", label: "User", value: m.User, set: func(s string) { m.User = s }}))
		}
		if !e.NoDatabase {
			rows = append(rows, f.textField(bc, colors, fieldSpec{key: "database", label: "Database", value: m.Database, set: func(s string) { m.Database = s }}))
		}
	}
	for _, fld := range e.Fields {
		rows = append(rows, f.engineField(bc, colors, fld))
	}
	if !e.NoTLS {
		rows = append(rows, f.tlsField(bc, colors))
		rows = append(rows, f.fileField(bc, colors, "caFile", "CA certificate file (optional)", "/path/to/ca.pem", m.CAFile, func(s string) { m.CAFile = s }))
	}
	rows = append(rows, f.colorField(bc, colors))
	rows = append(rows, f.check(bc, colors, "Read-only: refuse statements that change data", m.ReadOnly, func(v bool) { m.ReadOnly = v }), pageui.Gap(16))
	if !e.NoPassword {
		rows = append(rows, f.passwordSection(bc, colors))
	}

	buttons := []widgets.Widget{
		ui.NewButton("Save", ui.ButtonOpts{OnPressed: f.submit, Disabled: f.busy}),
		pageui.HGap(8),
		ui.NewButton("Test Connection", ui.ButtonOpts{OnPressed: f.test, Secondary: true, Disabled: f.busy}),
		pageui.HGap(8),
		ui.NewButton("Cancel", ui.ButtonOpts{OnPressed: f.tab.Close, Secondary: true}),
	}
	rows = append(rows, pageui.Gap(14), widgets.NewRow(buttons, widgets.RowOpts{}), pageui.NoteLine(bc, colors, f.note))

	body := widgets.NewColumn(rows, widgets.ColumnOpts{CrossAxisAlignment: rendering.CrossAxisAlignmentStretch})
	page := widgets.NewPadding(painting.NewEdgeInsetsAll(24), widgets.NewAlign(
		widgets.NewConstrainedBox(rendering.NewBoxConstraints(rendering.BoxConstraintsOpts{MaxWidth: foundation.Some(formWidth)}), body),
		widgets.AlignOpts{Alignment: painting.AlignmentTopLeft}))
	return widgets.NewColoredBox(colors.EditorBackground, ui.NewScrollView(page, &f.scroll))
}

// fieldSpec describes a text field.
type fieldSpec struct {
	key, label, placeholder, value string
	set                            func(string)
	obscure                        bool
	help                           string
	// trailing are widgets after the box, in the same row.
	trailing []widgets.Widget
}

// label is the caption over a field.
func (f *formTab) label(bc widgets.BuildContext, colors ui.Colors, text string) widgets.Widget {
	return widgets.NewText(text, widgets.TextOpts{Style: ui.TextStyle(bc, formLabelPx, colors.DescriptionForeground)})
}

// below shows the problem and the help of a field under it.
func (f *formTab) below(bc widgets.BuildContext, colors ui.Colors, key, help string) []widgets.Widget {
	var out []widgets.Widget
	if p := f.problemOf(key); p != "" {
		out = append(out, widgets.NewPadding(painting.NewEdgeInsetsFromLTRB(0, 3, 0, 0),
			widgets.NewText(p, widgets.TextOpts{Style: ui.TextStyle(bc, formLabelPx, colors.ErrorForeground)})))
	}
	if help != "" {
		out = append(out, widgets.NewPadding(painting.NewEdgeInsetsFromLTRB(0, 3, 0, 0),
			widgets.NewText(help, widgets.TextOpts{Style: ui.TextStyle(bc, formLabelPx, colors.DescriptionForeground)})))
	}
	return out
}

// textField is a labelled input box.
func (f *formTab) textField(bc widgets.BuildContext, colors ui.Colors, s fieldSpec) widgets.Widget {
	ctl := f.input(s.key, s.value)
	box := ui.NewInputBox(nil, nil, ui.InputBoxOpts{
		Controller: ctl, Placeholder: s.placeholder, Obscure: s.obscure, Invalid: f.problemOf(s.key) != "",
		OnChanged: func(text string) {
			if text != s.value {
				s.set(text)
				f.edited()
			}
		},
		OnSubmit: func(string) { f.submit() },
	})
	var field widgets.Widget = box
	if len(s.trailing) > 0 {
		row := []widgets.Widget{widgets.NewExpanded(box)}
		for _, w := range s.trailing {
			row = append(row, pageui.HGap(6), w)
		}
		field = widgets.NewRow(row, widgets.RowOpts{CrossAxisAlignment: rendering.CrossAxisAlignmentCenter})
	}
	col := []widgets.Widget{f.label(bc, colors, s.label), pageui.Gap(4), field}
	col = append(col, f.below(bc, colors, s.key, s.help)...)
	col = append(col, pageui.Gap(10))
	return widgets.NewColumn(col, widgets.ColumnOpts{CrossAxisAlignment: rendering.CrossAxisAlignmentStretch})
}

// fileField is a text field with a button that opens the file dialog.
func (f *formTab) fileField(bc widgets.BuildContext, colors ui.Colors, key, label, placeholder, value string, set func(string)) widgets.Widget {
	ctl := f.input(key, value)
	return f.textField(bc, colors, fieldSpec{key: key, label: label, placeholder: placeholder, value: value, set: set,
		trailing: []widgets.Widget{ui.NewButton("Browse...", ui.ButtonOpts{Secondary: true, OnPressed: func() { f.browse(label, set, ctl) }})}})
}

// urlField is the connection URL; a password pasted in it moves to the password field.
func (f *formTab) urlField(bc widgets.BuildContext, colors ui.Colors) widgets.Widget {
	m := f.model
	ctl := f.input("url", m.URL)
	box := ui.NewInputBox(nil, nil, ui.InputBoxOpts{
		Controller: ctl, Placeholder: m.Engine.URLSchemes[0] + "://host:port/database", Invalid: f.problemOf("url") != "",
		OnChanged: func(text string) {
			if text == m.URL {
				return
			}
			moved := m.SetURL(text)
			f.edited()
			if moved {
				// the text kept has no password: show it, and show the password in its own field
				ctl.SetValue(m.URL)
				f.input("password", "").SetValue(m.Password)
				f.note = pageui.Fine("The password in the URL moved to the password field.")
				f.tab.Refresh()
			}
		},
		OnSubmit: func(string) { f.submit() },
	})
	col := []widgets.Widget{f.label(bc, colors, "Connection URL (optional, replaces host and port)"), pageui.Gap(4), box}
	col = append(col, f.below(bc, colors, "url", "A password in the URL moves to the password field and is never kept in the URL.")...)
	col = append(col, pageui.Gap(10))
	return widgets.NewColumn(col, widgets.ColumnOpts{CrossAxisAlignment: rendering.CrossAxisAlignmentStretch})
}

// hostPortRow is the host and the port side by side.
func (f *formTab) hostPortRow(bc widgets.BuildContext, colors ui.Colors) widgets.Widget {
	m := f.model
	host := f.textField(bc, colors, fieldSpec{key: "host", label: "Host", placeholder: "localhost", value: m.Host, set: func(s string) { m.Host = s }})
	port := f.textField(bc, colors, fieldSpec{key: "port", label: "Port", placeholder: "", value: m.Port, set: func(s string) { m.Port = s }})
	return widgets.NewRow([]widgets.Widget{
		widgets.NewExpanded(host), pageui.HGap(10), widgets.NewSizedBox(port, widgets.SizedBoxOpts{Width: foundation.Some(110.0)}),
	}, widgets.RowOpts{CrossAxisAlignment: rendering.CrossAxisAlignmentStart})
}

// dropdownField is a labelled select box.
func (f *formTab) dropdownField(bc widgets.BuildContext, colors ui.Colors, key, label string, options []ui.DropdownOption, selected int, onChange func(int), help string) widgets.Widget {
	col := []widgets.Widget{
		f.label(bc, colors, label), pageui.Gap(4),
		ui.NewDropdown(options, ui.DropdownOpts{Selected: selected, OnChanged: func(i int) { onChange(i); f.edited(); f.tab.Refresh() }}),
	}
	col = append(col, f.below(bc, colors, key, help)...)
	col = append(col, pageui.Gap(10))
	return widgets.NewColumn(col, widgets.ColumnOpts{CrossAxisAlignment: rendering.CrossAxisAlignmentStretch})
}

// scopeField chooses between the user's connections and the workspace's.
func (f *formTab) scopeField(bc widgets.BuildContext, colors ui.Colors) widgets.Widget {
	m := f.model
	opts := []ui.DropdownOption{{Label: "Every workspace"}, {Label: "This workspace only", Disabled: m.Folder == ""}}
	sel := 0
	if m.Scope == ScopeWorkspace {
		sel = 1
	}
	help := ""
	if m.Folder == "" {
		help = "This window has no folder, so the connection is kept for every workspace."
	}
	return f.dropdownField(bc, colors, "scope", "Available in", opts, sel, func(i int) {
		m.Scope = []Scope{ScopeUser, ScopeWorkspace}[i]
	}, help)
}

// tlsField chooses the TLS mode.
func (f *formTab) tlsField(bc widgets.BuildContext, colors ui.Colors) widgets.Widget {
	m := f.model
	modes := TLSModes()
	opts := []ui.DropdownOption{{Label: "Default of the engine"}}
	sel := 0
	for i, mode := range modes {
		opts = append(opts, ui.DropdownOption{Label: string(mode)})
		if m.TLS == mode {
			sel = i + 1
		}
	}
	return f.dropdownField(bc, colors, "tls", "TLS", opts, sel, func(i int) {
		if i == 0 {
			m.TLS = ""
		} else {
			m.TLS = modes[i-1]
		}
	}, "")
}

// engineField is a field only this engine has.
func (f *formTab) engineField(bc widgets.BuildContext, colors ui.Colors, fld Field) widgets.Widget {
	m := f.model
	get := m.Options[fld.Key]
	set := func(s string) { m.Options[fld.Key] = s }
	switch fld.Kind {
	case FieldChoice:
		opts := []ui.DropdownOption{{Label: "Default"}}
		sel := 0
		for i, c := range fld.Choices {
			label := c.Label
			if label == "" {
				label = c.Value
			}
			opts = append(opts, ui.DropdownOption{Label: label})
			if get == c.Value {
				sel = i + 1
			}
		}
		return f.dropdownField(bc, colors, fld.Key, fld.Label, opts, sel, func(i int) {
			if i == 0 {
				set("")
			} else {
				set(fld.Choices[i-1].Value)
			}
		}, "")
	case FieldBool:
		return widgets.NewColumn([]widgets.Widget{f.check(bc, colors, fld.Label, get == "true", func(v bool) {
			if v {
				set("true")
			} else {
				set("false")
			}
		}), pageui.Gap(6)}, widgets.ColumnOpts{CrossAxisAlignment: rendering.CrossAxisAlignmentStart})
	case FieldFile:
		return f.fileField(bc, colors, fld.Key, fld.Label, fld.Placeholder, get, set)
	}
	return f.textField(bc, colors, fieldSpec{key: fld.Key, label: fld.Label, placeholder: fld.Placeholder, value: get, set: set})
}

// check is a check box.
func (f *formTab) check(_ widgets.BuildContext, _ ui.Colors, label string, checked bool, set func(bool)) widgets.Widget {
	return ui.NewCheckbox(label, ui.CheckboxOpts{Checked: checked, OnChanged: func(v bool) { set(v); f.edited(); f.tab.Refresh() }})
}

// colorField is the color of the connection, with a swatch.
func (f *formTab) colorField(bc widgets.BuildContext, colors ui.Colors) widgets.Widget {
	m := f.model
	swatch := pageui.Nothing()
	if c, ok := parseColor(strings.TrimSpace(m.Color)); ok {
		swatch = widgets.NewColoredBox(c, widgets.NewSizedBox(nil, widgets.SizedBoxOpts{Width: foundation.Some(formSwatchPx), Height: foundation.Some(formSwatchPx)}))
	}
	return f.textField(bc, colors, fieldSpec{key: "color", label: "Color (optional): tints the status bar of an editor bound to it", placeholder: "#rrggbb",
		value: m.Color, set: func(s string) { m.Color = s }, trailing: []widgets.Widget{swatch}})
}

// passwordSection is the password, the keyring check box and the environment variable.
func (f *formTab) passwordSection(bc widgets.BuildContext, colors ui.Colors) widgets.Widget {
	m := f.model
	placeholder := ""
	if m.HadPassword {
		placeholder = "(saved: leave empty to keep it)"
	}
	pw := f.textField(bc, colors, fieldSpec{key: "password", label: "Password", placeholder: placeholder, value: "", obscure: true,
		set:  func(s string) { m.SetPassword(s) },
		help: "Never written to a file of Emetos. It is asked when connecting, unless you save it in the keyring or name a variable."})
	save := f.check(bc, colors, "Save the password in the keyring of the desktop", m.SavePassword, func(v bool) { m.SavePassword = v })
	env := f.textField(bc, colors, fieldSpec{key: "passwordEnv", label: "Or read the password from the environment variable", placeholder: "PGPASSWORD",
		value: m.PasswordEnv, set: func(s string) { m.PasswordEnv = s }})
	return widgets.NewColumn([]widgets.Widget{pw, save, pageui.Gap(14), env}, widgets.ColumnOpts{CrossAxisAlignment: rendering.CrossAxisAlignmentStretch})
}
