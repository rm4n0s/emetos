package httpclient

import (
	"context"
	"encoding/json"
	"path/filepath"
	"strings"

	"github.com/rm4n0s/errors"
	"github.com/rm4n0s/glutter/foundation"
	"github.com/rm4n0s/glutter/painting"
	"github.com/rm4n0s/glutter/rendering"
	"github.com/rm4n0s/glutter/widgets"

	"github.com/rm4n0s/emetos/extension"
	"github.com/rm4n0s/emetos/extension/ui"
	"github.com/rm4n0s/emetos/extensions/httpclient/format"
	"github.com/rm4n0s/emetos/extensions/httpclient/store"
	"github.com/rm4n0s/emetos/extensions/httpclient/variables"
	"github.com/rm4n0s/emetos/extensions/internal/pageui"
)

// environmentOwner is the owner of the current values and the secrets of the environment with the key.
func environmentOwner(key string) string { return variables.EnvironmentOwner(key) }

// envTabState is the state of an environment editor tab: which file it edits.
type envTabState struct {
	Path string `json:"path"`
}

// envRow is a row of the table: a variable with the boxes of its name, its initial value and its current value.
type envRow struct {
	enabled, secret        bool
	name, initial, current *ui.InputBoxController
}

// envTab is the environment editor: an editor tab with a table of name, initial value, current value, secret and enabled. It is used on
// the UI thread.
type envTab struct {
	e   *Extension
	tab extension.EditorTab

	path string
	// title is the box with the name of the environment.
	title  *ui.InputBoxController
	rows   []*envRow
	scroll ui.ScrollHandle

	loaded   bool
	baseline string
	readOnly bool
	// problem is why the file cannot be edited (it cannot be read), gone says that it was deleted.
	problem string
	gone    bool
	note    pageui.Note
	busy    bool
}

// createEnvTab makes the content of an environment tab, for a new one and for one that comes back after a restart.
func (e *Extension) createEnvTab(tab extension.EditorTab, state json.RawMessage) (extension.EditorTabContent, error) {
	var st envTabState
	if len(state) == 0 {
		return extension.EditorTabContent{}, errors.New("HttpEnvironmentTabStateMissing", "an environment tab is opened with the file of its environment")
	}
	if err := json.Unmarshal(state, &st); err != nil {
		return extension.EditorTabContent{}, errors.NewErr("HttpEnvironmentTabStateInvalid", err)
	}
	t := &envTab{e: e, tab: tab, path: st.Path, title: ui.NewInputBoxController("")}
	e.envTabs = append(e.envTabs, t)
	tab.SetTitle(envKeyOf(st.Path))
	tab.SetHint(st.Path)
	t.storeChanged()
	return extension.EditorTabContent{
		Build:     t.build,
		SaveState: func() (any, error) { return envTabState{Path: t.path}, nil },
		OnClose: func() {
			for i, x := range e.envTabs {
				if x == t {
					e.envTabs = append(e.envTabs[:i], e.envTabs[i+1:]...)
					break
				}
			}
			t.dispose()
		},
	}, nil
}

// editEnvironment shows the editor of an environment, the one that is open already when there is one.
func (e *Extension) editEnvironment(n *store.Node) {
	for _, t := range e.envTabs {
		if t.path == n.Path {
			t.tab.Reveal(true)
			return
		}
	}
	tab, err := e.api.Editors().Open(EnvironmentKindID, envTabState{Path: n.Path}, extension.EditorOpenOptions{})
	if err != nil {
		e.fail("open the environment", err)
		return
	}
	tab.Reveal(true)
}

// node is the environment of the tab in the tree, or nil.
func (t *envTab) node() *store.Node { return t.e.tree.Find(t.path) }

// dispose lets go of the controllers.
func (t *envTab) dispose() {
	t.title.Dispose()
	for _, r := range t.rows {
		r.dispose()
	}
}

func (r *envRow) dispose() {
	r.name.Dispose()
	r.initial.Dispose()
	r.current.Dispose()
}

// storeChanged follows the files: a tab with no edits shows what is on disk now; one with edits keeps them.
func (t *envTab) storeChanged() {
	if t.e.tree == nil {
		return
	}
	n := t.node()
	switch {
	case n == nil:
		t.gone = true
		t.tab.SetBusy(false)
		t.tab.Refresh()
		return
	case t.loaded && t.snapshot() != t.baseline:
		t.gone = false
		t.tab.Refresh()
		return
	}
	t.gone = false
	t.load(n)
}

// load fills the table from the environment and the current values.
func (t *envTab) load(n *store.Node) {
	for _, r := range t.rows {
		r.dispose()
	}
	t.rows = nil
	t.readOnly, t.problem = n.ReadOnly, ""
	if n.Err != nil && !n.ReadOnly {
		t.problem = n.Err.Error()
	}
	doc := environmentFrom(n)
	t.title.SetValue(doc.Name)
	current := t.e.values.All(environmentOwner(n.Key))
	for _, v := range doc.Variables {
		r := t.newRow(v.Name, v.Value, current[v.Name], v.Enabled, v.Secret)
		t.rows = append(t.rows, r)
	}
	t.loaded = true
	t.baseline = t.snapshot()
	t.tab.SetTitle(doc.Name)
	t.tab.SetDirty(false)
	t.tab.Refresh()
}

// newRow makes a row with its boxes.
func (t *envTab) newRow(name, initial, current string, enabled, secret bool) *envRow {
	return &envRow{
		enabled: enabled, secret: secret,
		name: ui.NewInputBoxController(name), initial: ui.NewInputBoxController(initial), current: ui.NewInputBoxController(current),
	}
}

// snapshot is everything the user can change, as one text: the tab is dirty while it differs from the baseline.
func (t *envTab) snapshot() string {
	var b strings.Builder
	b.WriteString(t.title.Value())
	for _, r := range t.rows {
		b.WriteString("\x00")
		for _, s := range []string{r.name.Value(), r.initial.Value(), r.current.Value()} {
			b.WriteString(s)
			b.WriteString("\x01")
		}
		b.WriteString(map[bool]string{true: "e", false: "-"}[r.enabled] + map[bool]string{true: "s", false: "-"}[r.secret])
	}
	return b.String()
}

// edited marks the tab dirty when the table differs from what was loaded or saved.
func (t *envTab) edited() {
	t.note = pageui.Note{}
	t.tab.SetDirty(t.snapshot() != t.baseline)
	t.tab.Refresh()
}

// ---- actions ----

// addRow adds an empty variable.
func (t *envTab) addRow() {
	if t.readOnly || t.problem != "" {
		return
	}
	t.rows = append(t.rows, t.newRow("", "", "", true, false))
	t.edited()
}

// removeRow takes a variable out of the table.
func (t *envTab) removeRow(r *envRow) {
	for i, x := range t.rows {
		if x == r {
			t.rows = append(t.rows[:i], t.rows[i+1:]...)
			r.dispose()
			break
		}
	}
	t.edited()
}

// persist writes the current value into the initial value (not for a secret): it is saved with the file, where the team sees it.
func (t *envTab) persist(r *envRow) {
	if r.secret || r.current.Value() == "" {
		return
	}
	r.initial.SetValue(r.current.Value())
	r.current.SetValue("")
	t.edited()
}

// revert reads the file again, dropping the edits.
func (t *envTab) revert() {
	if n := t.node(); n != nil {
		t.load(n)
	}
}

// variablesToSave are the variables of the table that have a name; a secret has no value in the file.
func (t *envTab) variablesToSave() []format.Variable {
	var out []format.Variable
	for _, r := range t.rows {
		name := strings.TrimSpace(r.name.Value())
		if name == "" {
			continue
		}
		v := format.Variable{Name: name, Enabled: r.enabled, Secret: r.secret}
		if !r.secret {
			v.Value = r.initial.Value()
		}
		out = append(out, v)
	}
	return out
}

// save writes the environment file and the current values, off the UI thread.
func (t *envTab) save() {
	n := t.node()
	if t.busy || n == nil || t.readOnly || t.problem != "" {
		return
	}
	name := strings.TrimSpace(t.title.Value())
	if name == "" {
		t.note = pageui.Bad("The environment needs a name.")
		t.tab.Refresh()
		return
	}
	seen := map[string]bool{}
	for _, v := range t.variablesToSave() {
		if seen[v.Name] {
			t.note = pageui.Bad("The variable \"" + v.Name + "\" is in the table twice.")
			t.tab.Refresh()
			return
		}
		seen[v.Name] = true
	}
	doc := format.Environment{Name: name, Variables: t.variablesToSave()}
	type cur struct {
		name, value string
		secret      bool
	}
	var currents []cur
	for _, r := range t.rows {
		if nm := strings.TrimSpace(r.name.Value()); nm != "" {
			currents = append(currents, cur{nm, r.current.Value(), r.secret})
		}
	}
	oldKey, oldPath, rename := n.Key, n.Path, name != n.Name
	t.busy, t.note = true, pageui.Fine("Saving...")
	t.tab.SetBusy(true)
	t.tab.Refresh()
	newPath := oldPath
	t.e.async(func(ctx context.Context) error {
		if rename {
			p, err := t.e.store.Rename(ctx, oldPath, name)
			if err != nil {
				return err
			}
			newPath = p
		}
		if err := t.e.store.SaveEnvironment(ctx, newPath, doc); err != nil {
			return err
		}
		key := envKeyOf(newPath)
		owner := environmentOwner(key)
		keep := map[string]bool{}
		for _, c := range currents {
			keep[c.name] = true
			switch {
			case c.secret && c.value != "":
				if err := t.e.values.Set(ctx, owner, c.name, c.value, true); err != nil {
					return err
				}
			case !c.secret && c.value != "":
				if err := t.e.values.Set(ctx, owner, c.name, c.value, false); err != nil {
					return err
				}
			case !c.secret:
				if err := t.e.values.Reset(ctx, owner, c.name, false); err != nil {
					return err
				}
			}
		}
		for old := range t.e.values.All(owner) {
			if !keep[old] {
				if err := t.e.values.Reset(ctx, owner, old, false); err != nil {
					return err
				}
			}
		}
		return nil
	}, func(err error) {
		t.busy = false
		t.tab.SetBusy(false)
		if err != nil {
			t.note = pageui.Bad("Cannot save: " + err.Error())
			t.e.api.Log().Warn("the environment cannot be saved", "err", err)
			t.tab.Refresh()
			return
		}
		if rename {
			t.e.environmentRenamed(oldKey, envKeyOf(newPath))
		}
		t.path = newPath
		for _, r := range t.rows {
			if r.secret {
				r.current.SetValue("") // the keyring has it now
			}
		}
		t.baseline = t.snapshot()
		t.tab.SetTitle(name)
		t.tab.SetHint(newPath)
		t.tab.SetDirty(false)
		t.note = pageui.Fine("Saved.")
		t.tab.Refresh()
		t.e.refreshAll()
	})
}

// ---- the widget ----

// The sizes of the table.
const (
	envNameWidth = 190.0
	envBoxGap    = 8.0
)

// build makes the widget of the tab.
func (t *envTab) build(bc widgets.BuildContext) widgets.Widget {
	colors := ui.Theme(bc)
	dim := ui.TextStyle(bc, 12, colors.DescriptionForeground)
	pad := func(w widgets.Widget) widgets.Widget {
		return widgets.NewPadding(painting.NewEdgeInsetsFromLTRB(20, 16, 20, 12), w)
	}
	switch {
	case t.e.tree == nil:
		return pad(pageui.Paragraph("Reading the environment...", dim))
	case t.gone:
		return pad(pageui.Paragraph("This environment was deleted or moved. Close the tab.", ui.TextStyle(bc, 13, colors.ErrorForeground)))
	case t.problem != "":
		return pad(widgets.NewColumn([]widgets.Widget{pageui.Problem(bc, colors, "This environment cannot be read: "+t.problem),
			pageui.Gap(8), pageui.Paragraph("Open the file to fix it, or delete it.", dim)}, widgets.ColumnOpts{CrossAxisAlignment: rendering.CrossAxisAlignmentStretch}))
	}
	small := func(label, tip string, fn func(), disabled bool) widgets.Widget {
		return ui.NewButton(label, ui.ButtonOpts{OnPressed: fn, Small: true, Secondary: true, Disabled: disabled, Tooltip: tip})
	}
	head := widgets.NewRow([]widgets.Widget{
		widgets.NewText("Name", widgets.TextOpts{Style: dim}), pageui.HGap(8),
		widgets.NewSizedBox(ui.NewInputBox(nil, nil, ui.InputBoxOpts{Controller: t.title, Disabled: t.readOnly, OnChanged: func(string) { t.edited() }, OnSubmit: func(string) { t.save() }}),
			widgets.SizedBoxOpts{Width: foundation.Some(260.0)}),
		pageui.HGap(16),
		ui.NewButton("Save", ui.ButtonOpts{OnPressed: t.save, Disabled: t.readOnly || t.busy}), pageui.HGap(8),
		small("Revert", "Read the file again and drop the changes", t.revert, t.busy),
	}, widgets.RowOpts{CrossAxisAlignment: rendering.CrossAxisAlignmentCenter})
	explain := "The initial value is saved in the file and shared with the team. The current value stays on this computer, in Emetos's state: " +
		"a request uses it before the initial value. A secret's value is kept in the keyring and never written to a file."
	if t.readOnly {
		explain = "This file is from a newer version of the format: it is shown read-only."
	}
	label := func(s string, w float64) widgets.Widget {
		return widgets.NewSizedBox(widgets.NewText(s, widgets.TextOpts{Style: dim}), widgets.SizedBoxOpts{Width: foundation.Some(w)})
	}
	header := widgets.NewRow([]widgets.Widget{
		label("", 28), label("Variable", envNameWidth), pageui.HGap(envBoxGap),
		widgets.NewExpanded(widgets.NewText("Initial value", widgets.TextOpts{Style: dim})), pageui.HGap(envBoxGap),
		widgets.NewExpanded(widgets.NewText("Current value", widgets.TextOpts{Style: dim})), pageui.HGap(envBoxGap),
		label("Secret", 70), label("", 84),
	}, widgets.RowOpts{})
	body := []widgets.Widget{header, pageui.Gap(4)}
	for _, r := range t.rows {
		body = append(body, t.rowWidget(bc, r), pageui.Gap(4))
	}
	if len(t.rows) == 0 {
		body = append(body, pageui.Paragraph("There are no variables yet.", dim), pageui.Gap(4))
	}
	body = append(body, pageui.Gap(6), widgets.NewRow([]widgets.Widget{small("Add Variable", "Add a row to the table", t.addRow, t.readOnly)}, widgets.RowOpts{}))
	content := widgets.NewColumn([]widgets.Widget{
		head, pageui.NoteLine(bc, colors, t.note), pageui.Gap(8), pageui.Paragraph(explain, dim), pageui.Gap(14),
		widgets.NewColumn(body, widgets.ColumnOpts{CrossAxisAlignment: rendering.CrossAxisAlignmentStretch}),
	}, widgets.ColumnOpts{CrossAxisAlignment: rendering.CrossAxisAlignmentStretch})
	return ui.NewScrollView(pad(content), &t.scroll)
}

// rowWidget is a row of the table.
func (t *envTab) rowWidget(bc widgets.BuildContext, r *envRow) widgets.Widget {
	locked := t.readOnly
	box := func(c *ui.InputBoxController, placeholder string, disabled, obscure bool) widgets.Widget {
		return ui.NewInputBox(nil, nil, ui.InputBoxOpts{Controller: c, Placeholder: placeholder, Disabled: disabled || locked, Obscure: obscure, OnChanged: func(string) { t.edited() }})
	}
	initialHint, currentHint := "", "same as the initial value"
	if r.secret {
		initialHint, currentHint = "in the keyring", "type to set the secret"
	}
	return widgets.NewRow([]widgets.Widget{
		widgets.NewSizedBox(ui.NewCheckbox("", ui.CheckboxOpts{Checked: r.enabled, Disabled: locked, Tooltip: "Use this variable", OnChanged: func(on bool) { r.enabled = on; t.edited() }}),
			widgets.SizedBoxOpts{Width: foundation.Some(28.0)}),
		widgets.NewSizedBox(box(r.name, "name", false, false), widgets.SizedBoxOpts{Width: foundation.Some(envNameWidth)}), pageui.HGap(envBoxGap),
		widgets.NewExpanded(box(r.initial, initialHint, r.secret, false)), pageui.HGap(envBoxGap),
		widgets.NewExpanded(box(r.current, currentHint, false, r.secret)), pageui.HGap(envBoxGap),
		widgets.NewSizedBox(ui.NewCheckbox("", ui.CheckboxOpts{Checked: r.secret, Disabled: locked, Tooltip: "A secret's value is kept in the keyring, never in the file",
			OnChanged: func(on bool) { r.secret = on; t.edited() }}), widgets.SizedBoxOpts{Width: foundation.Some(70.0)}),
		ui.NewIconButton("discard", ui.IconButtonOpts{Tooltip: "Reset: use the initial value", Disabled: locked, OnPressed: func() { r.current.SetValue(""); t.edited() }}),
		ui.NewIconButton("arrow-up", ui.IconButtonOpts{Tooltip: "Persist: make the current value the initial value", Disabled: locked || r.secret, OnPressed: func() { t.persist(r) }}),
		ui.NewIconButton("trash", ui.IconButtonOpts{Tooltip: "Remove the variable", Disabled: locked, OnPressed: func() { t.removeRow(r) }}),
	}, widgets.RowOpts{CrossAxisAlignment: rendering.CrossAxisAlignmentCenter})
}

// envKeyOf is the key of the environment in the file at path: its name without the extension.
func envKeyOf(path string) string {
	return strings.TrimSuffix(filepath.Base(path), format.EnvironmentSuffix)
}
