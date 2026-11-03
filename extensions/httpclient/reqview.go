package httpclient

import (
	"fmt"
	"sort"
	"strings"

	"github.com/rm4n0s/glutter/foundation"
	"github.com/rm4n0s/glutter/painting"
	"github.com/rm4n0s/glutter/rendering"
	"github.com/rm4n0s/glutter/widgets"

	gui "github.com/rm4n0s/glutter/ui"

	"github.com/rm4n0s/emetos/extension/ui"
	"github.com/rm4n0s/emetos/extensions/httpclient/format"
	"github.com/rm4n0s/emetos/extensions/httpclient/variables"
	"github.com/rm4n0s/emetos/extensions/internal/pageui"
)

// The widgets of the request tab.

var methods = []string{"GET", "POST", "PUT", "PATCH", "DELETE", "HEAD", "OPTIONS"}

// tabPad is the room around the form.
func tabPad(w widgets.Widget) widgets.Widget {
	return widgets.NewPadding(painting.NewEdgeInsetsFromLTRB(16, 12, 16, 8), w)
}

func stretch(children ...widgets.Widget) widgets.Widget {
	return widgets.NewColumn(children, widgets.ColumnOpts{CrossAxisAlignment: rendering.CrossAxisAlignmentStretch})
}

func center(children ...widgets.Widget) widgets.Widget {
	return widgets.NewRow(children, widgets.RowOpts{CrossAxisAlignment: rendering.CrossAxisAlignmentCenter})
}

func flex(w widgets.Widget, n int) widgets.Widget {
	return widgets.NewExpanded(w, widgets.ExpandedOpts{Flex: foundation.Some(n)})
}

func wide(w widgets.Widget, width float64) widgets.Widget {
	return widgets.NewSizedBox(w, widgets.SizedBoxOpts{Width: foundation.Some(width)})
}

// build makes the widget of the tab.
func (t *reqTab) build(bc widgets.BuildContext) widgets.Widget {
	colors := ui.Theme(bc)
	t.e.lastReq = t
	dim := ui.TextStyle(bc, 12, colors.DescriptionForeground)
	switch {
	case t.path != "" && t.e.tree == nil, t.d == nil && t.problem == "" && !t.gone:
		return tabPad(pageui.Paragraph("Reading the request...", dim))
	case t.gone:
		return tabPad(pageui.Paragraph("This request was deleted, renamed or moved. Close the tab.", ui.TextStyle(bc, 13, colors.ErrorForeground)))
	case t.problem != "" && t.d == nil:
		return tabPad(stretch(pageui.Problem(bc, colors, "This request cannot be read: "+t.problem),
			pageui.Gap(8), pageui.Paragraph("Open the file as text to fix it, or delete it.", dim)))
	}
	t.d.url.TextController().SetBuildTextSpan(func(bc widgets.BuildContext, style *painting.TextStyle, _ bool) *painting.TextSpan {
		return urlSpan(bc, t.d.url.Value(), style, t.look())
	})
	body := stretch(
		tabPad(t.topRow(bc)),
		t.sectionTabs(bc),
		flex(ui.NewScrollView(tabPad(t.panel(bc)), &t.scroll), 1),
		t.statusBar(bc),
	)
	return widgets.NewColoredBox(colors.EditorBackground, body)
}

// topRow is the method, the URL, Send and Save.
func (t *reqTab) topRow(bc widgets.BuildContext) widgets.Widget {
	d := t.d
	locked := t.readOnly
	options := make([]ui.DropdownOption, 0, len(methods)+2)
	selected := -1
	for i, m := range methods {
		options = append(options, ui.DropdownOption{Label: m})
		if m == d.method {
			selected = i
		}
	}
	if selected < 0 {
		options = append([]ui.DropdownOption{{Label: d.method}}, options...)
		selected = 0
	}
	options = append(options, ui.DropdownOption{Label: "Other..."})
	method := ui.NewDropdown(options, ui.DropdownOpts{Selected: selected, Disabled: locked, Width: foundation.Some(104.0), OnChanged: func(i int) {
		switch label := options[i].Label; label {
		case "Other...":
			t.askMethod()
		default:
			t.setMethod(label)
		}
	}})
	urlBox := ui.NewInputBox(nil, nil, ui.InputBoxOpts{
		Controller: d.url, Placeholder: "Enter a URL, for example {{baseUrl}}/users", Monospace: true, Disabled: locked,
		OnSubmit: func(string) { t.sendRequest() }, OnFocusChange: func(on bool) {
			if on {
				t.e.lastReq = t
			}
		},
	})
	var urlWidget widgets.Widget = urlBox
	if hover := t.urlHover(); hover != "" {
		urlWidget = ui.NewHover(urlBox, ui.HoverOpts{Text: hover})
	}
	var send widgets.Widget
	if t.send.sending {
		send = ui.NewButton("Cancel", ui.ButtonOpts{OnPressed: t.cancelSend, Secondary: true, Tooltip: "Stop the request"})
	} else {
		send = ui.NewButton("Send", ui.ButtonOpts{OnPressed: t.sendRequest, Disabled: locked, Tooltip: "Send the request (Ctrl+Enter)"})
	}
	save := ui.NewButton("Save", ui.ButtonOpts{OnPressed: func() { t.save(nil) }, Secondary: true, Disabled: locked || t.saving, Tooltip: "Save the request (Ctrl+S)"})
	row := center(wide(method, 104), pageui.HGap(8), flex(urlWidget, 1), pageui.HGap(8), send, pageui.HGap(6), save)
	extra := []widgets.Widget{row}
	if note := t.noteLine(bc); note != nil {
		extra = append(extra, note)
	}
	return stretch(extra...)
}

// noteLine is the line under the top row: what saving said, a warning about the file, or that the file is read-only.
func (t *reqTab) noteLine(bc widgets.BuildContext) widgets.Widget {
	colors := ui.Theme(bc)
	switch {
	case t.readOnly:
		return pageui.NoteLine(bc, colors, pageui.Note{Text: "This file is from a newer version of the format: it is shown read-only.", Bad: true})
	case t.diskChanged:
		return pageui.NoteLine(bc, colors, pageui.Note{Text: "The file changed on disk. Saving replaces its changes with yours.", Bad: true})
	case t.note.Text != "":
		return pageui.NoteLine(bc, colors, t.note)
	}
	return nil
}

// sectionTabs is the row of the tabs inside the tab.
func (t *reqTab) sectionTabs(bc widgets.BuildContext) widgets.Widget {
	colors := ui.Theme(bc)
	var items []widgets.Widget
	for _, s := range sections {
		s := s
		label := s.label
		if n := t.countOf(s.id); n > 0 {
			label = fmt.Sprintf("%s (%d)", label, n)
		}
		active := t.section == s.id
		items = append(items, ui.NewButton(label, ui.ButtonOpts{Secondary: !active, Small: true, OnPressed: func() {
			t.section = s.id
			t.scroll.ScrollTo(0, 0)
			t.tab.Refresh()
		}}), pageui.HGap(4))
	}
	return widgets.NewColoredBox(colors.EditorBackground, widgets.NewPadding(painting.NewEdgeInsetsFromLTRB(16, 0, 16, 0), center(items...)))
}

// countOf is the number shown after the name of a section: the rows that are on, or the body kind.
func (t *reqTab) countOf(section string) int {
	on := func(rows []*pairBox) int {
		n := 0
		for _, r := range rows {
			if r.enabled && r.name.Value() != "" {
				n++
			}
		}
		return n
	}
	switch section {
	case secParams:
		return on(t.d.params)
	case secHeaders:
		return on(t.d.headers)
	case secExtract:
		return len(t.d.req.Extract)
	case secChecks:
		return len(t.d.req.Checks)
	}
	return 0
}

// panel is the content of the chosen section.
func (t *reqTab) panel(bc widgets.BuildContext) widgets.Widget {
	switch t.section {
	case secHeaders:
		return t.headersPanel(bc)
	case secBody:
		return t.bodyPanel(bc)
	case secAuth:
		return t.authPanel(bc)
	case secSettings:
		return t.settingsPanel(bc)
	case secExtract, secChecks:
		return t.laterPanel(bc)
	}
	return t.paramsPanel(bc)
}

// laterPanel is the Extract and Checks tabs, which step 27.6 builds; what the file has is kept as it is.
func (t *reqTab) laterPanel(bc widgets.BuildContext) widgets.Widget {
	dim := ui.TextStyle(bc, 13, ui.Theme(bc).DescriptionForeground)
	what, n := "extraction rules", len(t.d.req.Extract)
	if t.section == secChecks {
		what, n = "checks", len(t.d.req.Checks)
	}
	text := fmt.Sprintf("The form for the %s comes in a later step. The request has %s; they are kept as they are when it is saved.", what, plural(n, strings.TrimSuffix(what, "s"), what))
	return pageui.Paragraph(text, dim)
}

// ---- tables ----

// tableOpts say how a table looks.
type tableOpts struct {
	section    string
	rows       *[]*pairBox
	nameHint   string
	valueHint  string
	describe   bool
	bulk       bool
	fileChoice bool
}

// table is a table of rows with a check box, a name, a value and a description, the buttons to add and to edit in bulk.
func (t *reqTab) table(bc widgets.BuildContext, o tableOpts) widgets.Widget {
	colors := ui.Theme(bc)
	dim := ui.TextStyle(bc, 12, colors.DescriptionForeground)
	locked := t.readOnly
	if t.open[o.section] && o.bulk {
		box := ui.NewInputBox(nil, nil, ui.InputBoxOpts{
			Controller: t.bulkBox(o.section), MultiLine: true, MinLines: 8, MaxLines: 14, Monospace: true, Disabled: locked,
			Placeholder: "name: value, one on each line. Start a line with # to turn it off.",
		})
		return stretch(box, pageui.Gap(8), center(t.smallButton("Key-Value Edit", "Go back to the table", func() { t.toggleBulk(o.section) }, false)))
	}
	label := func(s string, n int) widgets.Widget { return flex(widgets.NewText(s, widgets.TextOpts{Style: dim}), n) }
	heads := []widgets.Widget{wide(widgets.NewText("", widgets.TextOpts{}), 28), label("Name", 3), pageui.HGap(8), label("Value", 4)}
	if o.describe {
		heads = append(heads, pageui.HGap(8), label("Description", 3))
	}
	heads = append(heads, wide(widgets.NewText("", widgets.TextOpts{}), 40))
	rows := []widgets.Widget{widgets.NewRow(heads, widgets.RowOpts{}), pageui.Gap(4)}
	for _, r := range *o.rows {
		rows = append(rows, t.row(bc, r, o), pageui.Gap(4))
	}
	if len(*o.rows) == 0 {
		rows = append(rows, pageui.Paragraph("Nothing here yet.", dim), pageui.Gap(4))
	}
	buttons := []widgets.Widget{t.smallButton("Add", "Add a row", func() { t.addRow(o.rows, o.section) }, locked)}
	if o.bulk {
		buttons = append(buttons, pageui.HGap(6), t.smallButton("Bulk Edit", "Edit the rows as lines of text", func() { t.toggleBulk(o.section) }, locked))
	}
	rows = append(rows, pageui.Gap(4), center(buttons...))
	return stretch(rows...)
}

func (t *reqTab) smallButton(label, tip string, fn func(), disabled bool) widgets.Widget {
	return ui.NewButton(label, ui.ButtonOpts{OnPressed: fn, Small: true, Secondary: true, Disabled: disabled, Tooltip: tip})
}

// row is a row of a table.
func (t *reqTab) row(bc widgets.BuildContext, r *pairBox, o tableOpts) widgets.Widget {
	locked := t.readOnly
	box := func(c *ui.InputBoxController, hint string) widgets.Widget {
		return ui.NewInputBox(nil, nil, ui.InputBoxOpts{Key: foundation.NewObjectKey(c), Controller: c, Placeholder: hint, Disabled: locked, Monospace: false})
	}
	children := []widgets.Widget{
		wide(ui.NewCheckbox("", ui.CheckboxOpts{Key: foundation.NewObjectKey(r), Checked: r.enabled, Disabled: locked, Tooltip: "Use this row", OnChanged: func(on bool) { t.toggleRow(r, on, o.section) }}), 28),
		flex(box(r.name, o.nameHint), 3), pageui.HGap(8),
	}
	if o.fileChoice {
		kind := 0
		if r.isFile {
			kind = 1
		}
		type_ := ui.NewDropdown([]ui.DropdownOption{{Label: "Text"}, {Label: "File"}}, ui.DropdownOpts{Selected: kind, Disabled: locked, Width: foundation.Some(92.0), OnChanged: func(i int) {
			r.isFile = i == 1
			t.edited()
		}})
		value := box(r.value, o.valueHint)
		if r.isFile {
			value = center(flex(box(r.value, "the path of the file"), 1), pageui.HGap(4), ui.NewIconButton("folder-opened", ui.IconButtonOpts{
				Tooltip: "Choose the file", Disabled: locked, OnPressed: func() { t.chooseFile(r.value) },
			}))
		}
		children = append(children, flex(center(wide(type_, 92), pageui.HGap(6), flex(value, 1)), 4), pageui.HGap(8))
	} else {
		children = append(children, flex(box(r.value, o.valueHint), 4), pageui.HGap(8))
	}
	if o.describe {
		children = append(children, flex(box(r.desc, "description"), 3), pageui.HGap(8))
	}
	children = append(children, ui.NewIconButton("trash", ui.IconButtonOpts{Tooltip: "Remove the row", Disabled: locked, OnPressed: func() { t.removeRow(o.rows, r, o.section) }}))
	return center(children...)
}

func (t *reqTab) paramsPanel(bc widgets.BuildContext) widgets.Widget {
	return t.table(bc, tableOpts{section: secParams, rows: &t.d.params, nameHint: "name", valueHint: "value", describe: true, bulk: true})
}

func (t *reqTab) headersPanel(bc widgets.BuildContext) widgets.Widget {
	colors := ui.Theme(bc)
	dim := ui.TextStyle(bc, 12, colors.DescriptionForeground)
	mono := ui.TextStyle(bc, 12, colors.Foreground).CopyWith(painting.TextStyleOpts{FontFamily: pageui.MonoFamily(bc)})
	table := t.table(bc, tableOpts{section: secHeaders, rows: &t.d.headers, nameHint: "header", valueHint: "value", describe: true, bulk: true})
	added := []widgets.Widget{pageui.Gap(18), widgets.NewText("Added by Emetos", widgets.TextOpts{Style: pageui.Bold(ui.TextStyle(bc, 13, colors.Foreground))}), pageui.Gap(2),
		pageui.Paragraph("These are sent unless a header above has the same name.", dim), pageui.Gap(6)}
	for _, h := range t.addedHeaders() {
		added = append(added, widgets.NewRow([]widgets.Widget{wide(widgets.NewText(h[0], widgets.TextOpts{Style: mono}), 180), flex(widgets.NewText(h[1], widgets.TextOpts{Style: ui.TextStyle(bc, 12, colors.DescriptionForeground).CopyWith(painting.TextStyleOpts{FontFamily: pageui.MonoFamily(bc)})}), 1)}, widgets.RowOpts{}), pageui.Gap(2))
	}
	return stretch(table, stretch(added...))
}

// addedHeaders are the headers the engine adds, as name and value, minus the ones the request sets.
func (t *reqTab) addedHeaders() [][2]string {
	own := map[string]bool{}
	for _, h := range t.d.headers {
		if h.enabled {
			own[strings.ToLower(strings.TrimSpace(h.name.Value()))] = true
		}
	}
	all := [][2]string{{"Host", "<calculated when the request is sent>"}, {"User-Agent", "Emetos/dev"}, {"Accept-Encoding", "gzip, deflate"}}
	switch t.d.bodyType {
	case format.BodyJSON, format.BodyGraphQL:
		all = append(all, [2]string{"Content-Type", "application/json"})
	case format.BodyXML:
		all = append(all, [2]string{"Content-Type", "application/xml"})
	case format.BodyHTML:
		all = append(all, [2]string{"Content-Type", "text/html"})
	case format.BodyJavaScript:
		all = append(all, [2]string{"Content-Type", "application/javascript"})
	case format.BodyText:
		all = append(all, [2]string{"Content-Type", "text/plain"})
	case format.BodyForm:
		all = append(all, [2]string{"Content-Type", "application/x-www-form-urlencoded"})
	case format.BodyMultipart:
		all = append(all, [2]string{"Content-Type", "multipart/form-data; boundary=<calculated>"})
	case format.BodyBinary:
		all = append(all, [2]string{"Content-Type", "application/octet-stream"})
	}
	if t.d.bodyType != format.BodyNone && t.d.bodyType != "" && t.d.bodyType != format.BodyMultipart {
		all = append(all, [2]string{"Content-Length", "<calculated>"})
	}
	var out [][2]string
	for _, h := range all {
		if !own[strings.ToLower(h[0])] || h[0] == "Content-Type" && t.d.bodyType == format.BodyMultipart {
			out = append(out, h)
		}
	}
	return out
}

// ---- body ----

func (t *reqTab) bodyPanel(bc widgets.BuildContext) widgets.Widget {
	colors := ui.Theme(bc)
	d := t.d
	dim := ui.TextStyle(bc, 12, colors.DescriptionForeground)
	options := make([]ui.DropdownOption, len(bodyKinds))
	selected := 0
	for i, k := range bodyKinds {
		options[i] = ui.DropdownOption{Label: bodyKindLabels[k]}
		if k == d.bodyType {
			selected = i
		}
	}
	kind := ui.NewDropdown(options, ui.DropdownOpts{Selected: selected, Disabled: t.readOnly, Width: foundation.Some(190.0), OnChanged: func(i int) {
		d.bodyType = bodyKinds[i]
		t.edited()
	}})
	head := center(widgets.NewText("Body type", widgets.TextOpts{Style: dim}), pageui.HGap(10), wide(kind, 190))
	var content widgets.Widget
	switch d.bodyType {
	case format.BodyNone, "":
		content = pageui.Paragraph("This request has no body.", dim)
	case format.BodyForm:
		content = t.table(bc, tableOpts{section: "form", rows: &d.form, nameHint: "field", valueHint: "value"})
	case format.BodyMultipart:
		content = t.table(bc, tableOpts{section: "multipart", rows: &d.multi, nameHint: "field", valueHint: "value", fileChoice: true})
	case format.BodyBinary:
		content = stretch(pageui.Paragraph("The file is sent as the body. A path is relative to the folder of the workspace.", dim), pageui.Gap(8),
			center(flex(ui.NewInputBox(nil, nil, ui.InputBoxOpts{Controller: d.binary, Placeholder: "the path of the file", Disabled: t.readOnly}), 1), pageui.HGap(6),
				t.smallButton("Choose File...", "Choose the file to send", func() { t.chooseFile(d.binary) }, t.readOnly)))
	case format.BodyGraphQL:
		content = stretch(
			pageui.Paragraph("Query", dim), pageui.Gap(4), t.codeBox(d.texts[format.BodyGraphQL], format.BodyGraphQL, 10, "{ users { id name } }"), pageui.Gap(10),
			pageui.Paragraph("Variables (JSON)", dim), pageui.Gap(4), t.codeBox(d.gqlVars, format.BodyJSON, 5, "{ }"))
	default:
		content = t.codeBox(d.texts[d.bodyType], d.bodyType, 14, "")
	}
	return stretch(head, pageui.Gap(12), content)
}

// codeBox is the editor of a raw body: a multi-line box in the monospace font, colored with the language of the kind.
func (t *reqTab) codeBox(c *ui.InputBoxController, kind string, lines int, hint string) widgets.Widget {
	cache := t.tokens[kind+fmt.Sprintf("%p", c)]
	if cache == nil {
		cache = &tokenCache{}
		t.tokens[kind+fmt.Sprintf("%p", c)] = cache
	}
	lang := languageOf(kind)
	c.TextController().SetBuildTextSpan(func(bc widgets.BuildContext, style *painting.TextStyle, _ bool) *painting.TextSpan {
		return bodySpan(bc, c.Value(), lang, style, t.e.api.Languages(), cache)
	})
	return ui.NewInputBox(nil, nil, ui.InputBoxOpts{
		Key: foundation.NewObjectKey(c), Controller: c, MultiLine: true, MinLines: lines, MaxLines: lines, Monospace: true, Disabled: t.readOnly, Placeholder: hint,
		FontSize: foundation.Some(12.5), LineHeight: foundation.Some(18.0),
	})
}

// ---- auth ----

var authTypes = []struct{ id, label string }{
	{format.AuthInherit, "Inherit from parent"}, {format.AuthNone, "No Auth"}, {format.AuthBasic, "Basic Auth"}, {format.AuthBearer, "Bearer Token"},
	{format.AuthAPIKey, "API Key"}, {format.AuthDigest, "Digest Auth"}, {format.AuthOAuth2, "OAuth 2.0"},
}

var authLabels = map[string]string{
	fUsername: "Username", fPassword: "Password", fToken: "Token", fKey: "Key", fValue: "Value", fTokenURL: "Access token URL", fAuthURL: "Authorization URL",
	fClientID: "Client ID", fClientSecret: "Client secret", fScope: "Scope", fAudience: "Audience",
}

// obscured are the boxes of the Auth tab that hold a secret.
var obscured = map[string]bool{fPassword: true, fToken: true, fValue: true, fClientSecret: true}

func (t *reqTab) authPanel(bc widgets.BuildContext) widgets.Widget {
	colors := ui.Theme(bc)
	d := t.d
	dim := ui.TextStyle(bc, 12, colors.DescriptionForeground)
	locked := t.readOnly
	options := make([]ui.DropdownOption, len(authTypes))
	selected := 0
	for i, a := range authTypes {
		options[i] = ui.DropdownOption{Label: a.label}
		if a.id == d.auth.typ {
			selected = i
		}
	}
	typ := ui.NewDropdown(options, ui.DropdownOpts{Selected: selected, Disabled: locked, Width: foundation.Some(190.0), OnChanged: func(i int) {
		d.auth.typ = authTypes[i].id
		t.edited()
	}})
	rows := []widgets.Widget{center(widgets.NewText("Auth type", widgets.TextOpts{Style: dim}), pageui.HGap(10), wide(typ, 190)), pageui.Gap(14)}
	field := func(name string, w widgets.Widget) {
		rows = append(rows, center(wide(widgets.NewText(name, widgets.TextOpts{Style: dim}), 150), flex(w, 1)), pageui.Gap(8))
	}
	drop := func(name string, labels []string, sel int, fn func(int)) {
		opts := make([]ui.DropdownOption, len(labels))
		for i, l := range labels {
			opts[i] = ui.DropdownOption{Label: l}
		}
		field(name, center(wide(ui.NewDropdown(opts, ui.DropdownOpts{Selected: sel, Disabled: locked, Width: foundation.Some(190.0), OnChanged: fn}), 190)))
	}
	switch d.auth.typ {
	case format.AuthInherit, "":
		rows = append(rows, pageui.Paragraph("This request uses the authorization of its folder or collection: "+t.inheritedAuth()+".", dim))
	case format.AuthNone:
		rows = append(rows, pageui.Paragraph("This request is sent without authorization, whatever its folder or collection says.", dim))
	default:
		for _, f := range authFields[d.auth.typ] {
			f := f
			hint := ""
			if d.auth.typ == format.AuthAPIKey && f == fKey {
				hint = "X-API-Key"
			}
			field(authLabels[f], ui.NewInputBox(nil, nil, ui.InputBoxOpts{
				Key: foundation.NewObjectKey(d.auth.box[f]), Controller: d.auth.box[f], Placeholder: hint, Disabled: locked, Obscure: obscured[f] && !t.showSecrets,
			}))
		}
		if d.auth.typ == format.AuthAPIKey {
			sel := 0
			if d.auth.in == "query" {
				sel = 1
			}
			drop("Add to", []string{"Header", "Query params"}, sel, func(i int) {
				d.auth.in = []string{"header", "query"}[i]
				t.edited()
			})
		}
		if d.auth.typ == format.AuthOAuth2 {
			sel := 0
			if d.auth.grant == "authorization_code" {
				sel = 1
			}
			drop("Grant type", []string{"Client credentials", "Authorization code"}, sel, func(i int) {
				d.auth.grant = []string{"client_credentials", "authorization_code"}[i]
				t.edited()
			})
			field("", ui.NewCheckbox("Use PKCE", ui.CheckboxOpts{Checked: d.auth.pkce, Disabled: locked, OnChanged: func(on bool) { d.auth.pkce = on; t.edited() }}))
			rows = append(rows, pageui.Paragraph("Getting a token comes in a later step; the settings are saved with the request.", dim), pageui.Gap(8))
		}
		show := ui.NewCheckbox("Show passwords and tokens", ui.CheckboxOpts{Checked: t.showSecrets, OnChanged: func(on bool) { t.showSecrets = on; t.tab.Refresh() }})
		rows = append(rows, pageui.Gap(4), show, pageui.Gap(6), pageui.Paragraph("Use {{variables}} for secrets so that they stay out of the file; a secret variable's value is kept in the keyring.", dim))
	}
	return stretch(rows...)
}

// inheritedAuth says what the request would inherit.
func (t *reqTab) inheritedAuth() string {
	a := t.e.placeOf(t.path).auth
	if a == nil {
		return "none is set"
	}
	for _, x := range authTypes {
		if x.id == a.Type {
			return x.label
		}
	}
	return a.Type
}

// ---- settings ----

func (t *reqTab) settingsPanel(bc widgets.BuildContext) widgets.Widget {
	colors := ui.Theme(bc)
	d := t.d
	dim := ui.TextStyle(bc, 12, colors.DescriptionForeground)
	locked := t.readOnly
	var rows []widgets.Widget
	field := func(name string, w widgets.Widget) {
		rows = append(rows, center(wide(widgets.NewText(name, widgets.TextOpts{Style: dim}), 190), flex(w, 1)), pageui.Gap(8))
	}
	triDrop := func(name string, v tri, set func(tri)) {
		field(name, center(wide(ui.NewDropdown([]ui.DropdownOption{{Label: "Editor's setting"}, {Label: "Yes"}, {Label: "No"}},
			ui.DropdownOpts{Selected: int(v), Disabled: locked, Width: foundation.Some(210.0), OnChanged: func(i int) { set(tri(i)); t.edited() }}), 210)))
	}
	field("Timeout (milliseconds)", wide(ui.NewInputBox(nil, nil, ui.InputBoxOpts{Controller: d.timeout, Placeholder: "the editor's setting; 0 is no timeout", Disabled: locked}), 260))
	triDrop("Follow redirects", d.follow, func(v tri) { d.follow = v })
	field("Most redirects to follow", wide(ui.NewInputBox(nil, nil, ui.InputBoxOpts{Controller: d.maxRedirects, Placeholder: "10", Disabled: locked}), 260))
	triDrop("Check the TLS certificate", d.verify, func(v tri) { d.verify = v })
	if d.verify == triNo {
		rows = append(rows, pageui.NoteLine(bc, colors, pageui.Bad("The certificate of the server is not checked for this request.")))
	}
	rows = append(rows, pageui.Gap(4), pageui.Paragraph("An empty box uses the setting of the editor (httpClient.*). The settings are saved with the request.", dim))
	return stretch(rows...)
}

// ---- the status line ----

// statusBar is the line under the form: what the last send gave. (The response pane of step 27.5 shows the response itself.)
func (t *reqTab) statusBar(bc widgets.BuildContext) widgets.Widget {
	colors := ui.Theme(bc)
	s := t.send
	text, color := s.status, colors.DescriptionForeground
	switch {
	case text == "":
		text = "Press Send (Ctrl+Enter) to send the request."
	case s.bad:
		color = colors.ErrorForeground
	case s.code >= 200 && s.code < 300:
		color = colGood
	case s.code >= 300 && s.code < 400:
		color = colors.TextLinkForeground
	case s.code >= 400:
		color = colors.EditorWarningForeground
	}
	if s.code >= 500 {
		color = colors.ErrorForeground
	}
	line := widgets.NewText(text, widgets.TextOpts{Style: ui.TextStyle(bc, 12, color), MaxLines: foundation.Some(2), Overflow: painting.TextOverflowEllipsis})
	return widgets.NewColoredBox(colors.PanelBackground, widgets.NewPadding(painting.NewEdgeInsetsFromLTRB(16, 6, 16, 6), stretch(line)))
}

// colGood is the green of a successful status.
var colGood = gui.NewColor(0xFF3FB27F)

// ---- variables in the URL ----

// look gives the state of the variables of the URL box for its colors.
func (t *reqTab) look() varLook {
	pl := t.e.placeOf(t.path)
	secrets := secretNames(pl)
	r, _ := variables.Build(t.e.ctx, variables.Input{
		Environment: pl.env, Folders: pl.folders, Collection: pl.collection, Values: t.e.preview,
		Dynamic: variables.Dynamic{AllowedProcessEnv: t.e.allowedProcessEnv()},
	})
	return func(name string) (varState, string, string) {
		if strings.HasPrefix(name, "$") {
			if r != nil && len(r.Resolve("{{"+name+"}}").Unresolved) == 0 {
				return varResolved, "", "generated when the request is sent"
			}
			return varUnresolved, "", ""
		}
		if src, ok := secrets[name]; ok {
			return varSecret, "", src
		}
		if r != nil {
			if v, src, ok := r.Lookup(name); ok {
				return varResolved, v, string(src)
			}
		}
		return varUnresolved, "", ""
	}
}

// secretNames are the secret variables of the scopes (name to where they are), whose values the form never shows.
func secretNames(pl place) map[string]string {
	out := map[string]string{}
	add := func(s *variables.Scope, what string) {
		if s == nil {
			return
		}
		for _, v := range s.Variables {
			if v.Secret && v.Enabled {
				if _, has := out[v.Name]; !has {
					out[v.Name] = "secret, " + what + " " + s.Name
				}
			}
		}
	}
	add(pl.env, "environment")
	add(pl.collection, "collection")
	return out
}

// urlHover is the text of the hover of the URL box: each variable of the URL with its value and where it comes from, a secret masked
// and a name without a value marked.
func (t *reqTab) urlHover() string {
	refs := findVars(t.d.url.Value())
	if len(refs) == 0 {
		return ""
	}
	look := t.look()
	seen := map[string]bool{}
	var lines []string
	for _, v := range refs {
		if seen[v.name] {
			continue
		}
		seen[v.name] = true
		state, value, src := look(v.name)
		switch state {
		case varSecret:
			lines = append(lines, v.name+" = ********  ("+src+")")
		case varUnresolved:
			lines = append(lines, v.name+"  has no value: define it in the environment, the collection or the globals")
		default:
			if value == "" && strings.HasPrefix(v.name, "$") {
				lines = append(lines, v.name+"  ("+src+")")
			} else {
				lines = append(lines, v.name+" = "+value+"  ("+src+")")
			}
		}
	}
	sort.Strings(lines)
	return strings.Join(lines, "\n")
}
