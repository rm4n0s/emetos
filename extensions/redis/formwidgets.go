package redis

import (
	"slices"

	"github.com/rm4n0s/glutter/foundation"
	"github.com/rm4n0s/glutter/rendering"
	"github.com/rm4n0s/glutter/widgets"

	"github.com/rm4n0s/emetos/extension/ui"
	"github.com/rm4n0s/emetos/extensions/internal/pageui"
)

// rowForm is the small form that adds or changes a row, and the first value of a new key: its fields and the controllers that hold
// what the user typed. It is used on the UI thread.
type rowForm struct {
	// Title heads the form.
	Title string
	// Edit says that the form changes row Row (a row of the source) instead of adding one.
	Edit bool
	Row  int

	fields []FormField
	ctl    []*ui.InputBoxController
	choice []int
}

// newRowForm makes a form of fields with their values.
func newRowForm(title string, fields []FormField) *rowForm {
	f := &rowForm{Title: title, fields: fields, ctl: make([]*ui.InputBoxController, len(fields)), choice: make([]int, len(fields))}
	for i, fld := range fields {
		f.ctl[i] = ui.NewInputBoxController(fld.Value)
		if len(fld.Choices) > 0 {
			f.choice[i] = max(0, slices.Index(fld.Choices, fld.Value))
		}
	}
	return f
}

// values are what the user entered, in the order of the fields: a choice gives the text of the option.
func (f *rowForm) values() []string {
	out := make([]string, len(f.fields))
	for i, fld := range f.fields {
		if len(fld.Choices) > 0 {
			out[i] = fld.Choices[f.choice[i]]
		} else {
			out[i] = f.ctl[i].Value()
		}
	}
	return out
}

// missing returns the label of the first required field that is empty, or "".
func (f *rowForm) missing() string {
	for i, fld := range f.fields {
		if fld.Required && len(fld.Choices) == 0 && f.ctl[i].Value() == "" {
			return fld.Label
		}
	}
	return ""
}

// dispose lets go of the controllers.
func (f *rowForm) dispose() {
	for _, c := range f.ctl {
		c.Dispose()
	}
}

// formMaxWidth is how wide the fields of a form are.
const formMaxWidth = 640.0

// widgetsOf builds the fields of the form. refresh builds the page again (a choice changed); submit is called by Ctrl+Enter in a text
// area and Enter in a line.
func (f *rowForm) widgetsOf(bc widgets.BuildContext, colors ui.Colors, refresh, submit func()) []widgets.Widget {
	var out []widgets.Widget
	for i, fld := range f.fields {
		label := widgets.NewText(fld.Label, widgets.TextOpts{Style: ui.TextStyle(bc, 12, colors.DescriptionForeground)})
		var input widgets.Widget
		if len(fld.Choices) > 0 {
			opts := make([]ui.DropdownOption, len(fld.Choices))
			for j, c := range fld.Choices {
				opts[j] = ui.DropdownOption{Label: c}
			}
			idx := i
			input = ui.NewDropdown(opts, ui.DropdownOpts{
				Selected: f.choice[i], Width: foundation.Some(180.0),
				OnChanged: func(j int) { f.choice[idx] = j; refresh() },
			})
		} else {
			o := ui.InputBoxOpts{Controller: f.ctl[i], Placeholder: fld.Placeholder, Disabled: fld.Disabled, OnSubmit: func(string) { submit() }}
			if fld.Multiline {
				o.MultiLine, o.Monospace = true, true
				o.MaxHeight = foundation.Some(150.0)
			}
			input = ui.NewInputBox(nil, nil, o)
		}
		out = append(out, widgets.NewColumn([]widgets.Widget{label, pageui.Gap(4), input},
			widgets.ColumnOpts{CrossAxisAlignment: rendering.CrossAxisAlignmentStretch}), pageui.Gap(10))
	}
	return out
}
