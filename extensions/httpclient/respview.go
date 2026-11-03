package httpclient

import (
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/rm4n0s/glutter/foundation"
	"github.com/rm4n0s/glutter/painting"
	"github.com/rm4n0s/glutter/widgets"

	gui "github.com/rm4n0s/glutter/ui"

	"github.com/rm4n0s/emetos/extension"
	"github.com/rm4n0s/emetos/extension/ui"
	"github.com/rm4n0s/emetos/extensions/internal/pageui"
)

// The widgets of the response pane (SPEC §10.33 "The response pane"): the status line, the tabs Body, Headers, Cookies and Timeline,
// and the way the layout puts the pane next to the request or below it.

// The tabs of the response pane.
const (
	respBody     = "body"
	respHeaders  = "headers"
	respCookies  = "cookies"
	respTimeline = "timeline"
)

// The ways the Body tab shows the body.
const (
	modePretty  = "pretty"
	modeRaw     = "raw"
	modePreview = "preview"
)

// maxTokenized is the longest text of the Pretty view that is colored with the grammar of its language (tokenizing costs a pass over
// the text on the UI thread); a longer one is drawn plain.
const maxTokenized = 128 << 10

// rowHeight is the height of a line of the body views.
const rowHeight = 18.0

// respPane is what the response pane remembers about the response in it. It is made when a response arrives.
type respPane struct {
	disp *respDisplay
	tab  string
	mode string
	// pretty and raw are the lines the Body tab shows; raw holds one page of the body, number rawPage.
	pretty  *lineView
	raw     *lineView
	rawPage int
	// err says why the raw page cannot be shown.
	err    string
	scroll ui.ScrollHandle
}

// newRespPane makes the pane of a response: the Body tab, in the way that shows it best.
func (t *reqTab) newRespPane(disp *respDisplay) *respPane {
	p := &respPane{disp: disp, tab: respBody, rawPage: -1}
	switch {
	case disp.pic != nil || disp.picNote != "":
		p.mode = modePreview
	case disp.hasPretty():
		p.mode = modePretty
	default:
		p.mode = modeRaw
	}
	if disp.hasPretty() && p.pretty == nil {
		p.pretty = newLineView(disp.pretty, disp.lang)
	}
	return p
}

// setMode changes the way the Body tab shows the body.
func (t *reqTab) setMode(mode string) {
	p := t.pane
	if p == nil || p.mode == mode {
		return
	}
	p.mode = mode
	if mode == modeRaw && p.rawPage < 0 {
		t.showPage(0)
	}
	t.tab.Refresh()
}

// showPage reads a page of the body for the Raw view.
func (t *reqTab) showPage(page int) {
	p, r := t.pane, t.send.resp
	if p == nil || r == nil || r.Body == nil {
		return
	}
	page = min(max(page, 0), p.disp.pages-1)
	if page == p.rawPage {
		return
	}
	lines, err := rawPage(r.Body, page, p.disp.binary)
	if err != nil {
		p.err = err.Error()
		p.raw, p.rawPage = nil, -1
		t.e.api.Log().Warn("a page of the response cannot be read", "err", err)
		return
	}
	p.err = ""
	p.raw, p.rawPage = newLineView(lines, ""), page
}

// ---- a view of lines ----

// lineView is a list of lines of text, colored with a language when it has one. It draws only the rows in view, so a body of a hundred
// thousand lines costs a frame no more than a body of ten.
type lineView struct {
	lines     []string
	lang      string
	toks      [][]extension.Token
	tokenized bool
	model     *ui.ListViewModel
	ctl       *ui.ListController
}

func newLineView(lines []string, lang string) *lineView {
	v := &lineView{lines: lines, lang: lang, model: ui.NewListViewModel(ui.ListViewModelOpts{RowHeight: foundation.Some(rowHeight)}), ctl: ui.NewListController()}
	items := make([]ui.ListViewItem, len(lines))
	for i := range lines {
		items[i] = ui.ListViewItem{Key: "l" + strconv.Itoa(i), Label: lines[i], Data: i}
	}
	if err := v.model.SetItems(items); err != nil {
		// the keys are unique, so the model takes them
		v.lines = nil
	}
	return v
}

// tokenize colors the lines with the grammar of the language, once.
func (v *lineView) tokenize(langs extension.Languages) {
	if v.tokenized || v.lang == "" || langs == nil {
		return
	}
	v.tokenized = true
	total := 0
	for _, l := range v.lines {
		total += len(l) + 1
	}
	if total > maxTokenized {
		return
	}
	id, ok := langs.Resolve(v.lang)
	if !ok {
		return
	}
	if toks, err := langs.Tokenize(id, strings.Join(v.lines, "\n")); err == nil {
		v.toks = toks
	}
}

// widget is the list of the lines.
func (v *lineView) widget() widgets.Widget {
	return ui.NewList(v.model, ui.ListOpts{
		Controller: v.ctl,
		Render: func(bc widgets.BuildContext, row ui.ListViewRow, st ui.RowState) widgets.Widget {
			i, _ := row.Data.(int)
			return v.line(bc, i, st)
		},
	})
}

// line is one line in the colors of its tokens.
func (v *lineView) line(bc widgets.BuildContext, i int, st ui.RowState) widgets.Widget {
	colors := ui.Theme(bc)
	mono := ui.TextStyle(bc, 12, st.Foreground).CopyWith(painting.TextStyleOpts{FontFamily: pageui.MonoFamily(bc)})
	opts := widgets.RichTextOpts{SoftWrap: foundation.Some(false), MaxLines: foundation.Some(1)}
	if i < 0 || i >= len(v.lines) {
		return widgets.NewText("", widgets.TextOpts{})
	}
	line := v.lines[i]
	if i >= len(v.toks) || len(v.toks[i]) == 0 {
		return widgets.NewRichText(painting.NewTextSpan(painting.TextSpanOpts{Text: foundation.Some(line), Style: mono}), opts)
	}
	look := ui.TokenLooker(bc)
	fallback := colors.EditorForeground
	var spans []painting.InlineSpan
	at := 0
	for _, t := range v.toks[i] {
		start, end := min(max(t.Start, at), len(line)), min(t.End, len(line))
		if start > at {
			spans = append(spans, painting.NewTextSpan(painting.TextSpanOpts{Text: foundation.Some(line[at:start])}))
		}
		if end > start {
			l := look(t.Scopes, fallback)
			o := painting.TextStyleOpts{Color: foundation.Some(l.Color)}
			if l.Bold {
				o.FontWeight = gui.FontWeightBold
			}
			if l.Italic {
				o.FontStyle = gui.FontStyleItalic
			}
			spans = append(spans, painting.NewTextSpan(painting.TextSpanOpts{Text: foundation.Some(line[start:end]), Style: mono.CopyWith(o)}))
		}
		at = max(at, end)
	}
	if at < len(line) {
		spans = append(spans, painting.NewTextSpan(painting.TextSpanOpts{Text: foundation.Some(line[at:])}))
	}
	return widgets.NewRichText(painting.NewTextSpan(painting.TextSpanOpts{Style: mono, Children: spans}), opts)
}

// ---- the layout ----

// responseLayout is the setting httpClient.responseLayout: "vertical" (the response below the request) or "horizontal" (beside it).
func (e *Extension) responseLayout() string {
	if v, err := extension.SettingValue[string](e.api.Settings(), SettingResponseLayout); err == nil && v == "horizontal" {
		return "horizontal"
	}
	return "vertical"
}

// withResponse puts the response pane with the request form: below it or beside it as the setting says, with a sash between them.
func (t *reqTab) withResponse(bc widgets.BuildContext, form, pane widgets.Widget) widgets.Widget {
	colors := ui.Theme(bc)
	layout := t.e.responseLayout()
	orientation := ui.SplitVertical
	formPane, panePane := ui.SplitPane{Child: form, Size: 330, MinSize: 150}, ui.SplitPane{Child: pane, Size: 330, MinSize: 120}
	if layout == "horizontal" {
		orientation = ui.SplitHorizontal
		formPane.Size, formPane.MinSize, panePane.Size, panePane.MinSize = 520, 300, 420, 260
	}
	return widgets.NewKeyedSubtree(ui.NewSplitView([]ui.SplitPane{formPane, panePane}, ui.SplitViewOpts{
		Orientation: orientation, ProportionalLayout: true, SeparatorBorder: colors.PanelBorder,
	}), widgets.WidgetOpts{Key: foundation.NewValueKey("response-" + layout)})
}

// ---- the pane ----

// responsePane is the pane of the response in the tab.
func (t *reqTab) responsePane(bc widgets.BuildContext) widgets.Widget {
	colors := ui.Theme(bc)
	p := t.pane
	var content widgets.Widget
	switch p.tab {
	case respHeaders:
		content = t.headersView(bc)
	case respCookies:
		content = t.cookiesView(bc)
	case respTimeline:
		content = t.timelineView(bc)
	default:
		content = t.bodyView(bc)
	}
	return widgets.NewColoredBox(colors.EditorBackground, stretch(t.statusRow(bc), t.respTabs(bc), flex(content, 1)))
}

// respStatusColor is the color of a status: green for a success, the link color for a redirect, the warning color for a client error and
// the error color for a server error or a failure.
func respStatusColor(colors ui.Colors, code int, bad bool) gui.Color {
	switch {
	case bad || code >= 500:
		return colors.ErrorForeground
	case code >= 400:
		return colors.EditorWarningForeground
	case code >= 300:
		return colors.TextLinkForeground
	case code >= 200:
		return colGood
	}
	return colors.DescriptionForeground
}

// statusRow is the status line of the pane: the status, the time and the size (the hover of the time has the breakdown), the notes of
// the request, and the buttons of the response.
func (t *reqTab) statusRow(bc widgets.BuildContext) widgets.Widget {
	colors := ui.Theme(bc)
	r := t.send.resp
	dim := ui.TextStyle(bc, 12, colors.DescriptionForeground)
	sep := widgets.NewText("  ·  ", widgets.TextOpts{Style: dim})
	status := widgets.NewText(r.Status, widgets.TextOpts{Style: pageui.Bold(ui.TextStyle(bc, 13, respStatusColor(colors, r.StatusCode, false))), MaxLines: foundation.Some(1)})
	size := byteSize(0)
	if r.Body != nil {
		size = byteSize(r.Body.Size())
	}
	took := widgets.NewText(durationText(t.send.duration), widgets.TextOpts{Style: ui.TextStyle(bc, 12, colors.EditorForeground)})
	timing := ui.NewHover(took, ui.HoverOpts{Text: timingHover(r.Timings)})
	d := t.pane.disp
	items := []widgets.Widget{status, sep, timing, sep, widgets.NewText(size, widgets.TextOpts{Style: ui.TextStyle(bc, 12, colors.EditorForeground)})}
	if len(r.Decoded) > 0 {
		items = append(items, widgets.NewText("  ("+byteSize(r.Transferred)+" on the wire)", widgets.TextOpts{Style: dim}))
	}
	items = append(items, pageui.HGap(8))
	grow := widgets.NewExpanded(widgets.NewSizedBox(nil, widgets.SizedBoxOpts{}), widgets.ExpandedOpts{})
	items = append(items, grow,
		ui.NewIconButton("save", ui.IconButtonOpts{Tooltip: "Save Response to File...", OnPressed: t.saveResponse, Disabled: r.Body == nil || r.Body.Size() == 0}), pageui.HGap(2),
		ui.NewIconButton("copy", ui.IconButtonOpts{Tooltip: "Copy Body", OnPressed: t.copyBody, Disabled: !d.textual()}), pageui.HGap(2),
		ui.NewIconButton("go-to-file", ui.IconButtonOpts{Tooltip: "Open in New Editor Tab", OnPressed: t.openBodyInTab, Disabled: !d.textual()}),
	)
	rows := []widgets.Widget{widgets.NewPadding(painting.NewEdgeInsetsFromLTRB(16, 4, 12, 2), center(items...))}
	if notes := t.respNotes(); notes != "" {
		rows = append(rows, widgets.NewPadding(painting.NewEdgeInsetsFromLTRB(16, 0, 16, 2),
			widgets.NewText(notes, widgets.TextOpts{Style: ui.TextStyle(bc, 12, colors.EditorWarningForeground), MaxLines: foundation.Some(2), Overflow: painting.TextOverflowEllipsis})))
	}
	return widgets.NewColoredBox(colors.PanelBackground, stretch(rows...))
}

// respNotes says what was wrong with the request that the status does not: variables without a value, a certificate that was not
// checked, the warnings of the engine and the error that came with the response.
func (t *reqTab) respNotes() string {
	r := t.send.resp
	var parts []string
	if len(r.Unresolved) > 0 {
		parts = append(parts, "no value for "+strings.Join(r.Unresolved, ", "))
	}
	if len(r.MissingSecrets) > 0 {
		parts = append(parts, "the secret "+strings.Join(r.MissingSecrets, ", ")+" has no value (set it in the environment editor)")
	}
	if r.InsecureTLS {
		parts = append(parts, "the certificate of the server was not checked")
	}
	parts = append(parts, r.Warnings...)
	if t.send.err != "" {
		parts = append(parts, t.send.err)
	}
	return strings.Join(parts, "  ·  ")
}

// respTabs is the row of the tabs of the pane.
func (t *reqTab) respTabs(bc widgets.BuildContext) widgets.Widget {
	colors := ui.Theme(bc)
	r := t.send.resp
	cookies := len(r.Cookies)
	headers := 0
	for _, v := range r.Header {
		headers += len(v)
	}
	tabs := []struct {
		id, label string
		n         int
	}{{respBody, "Body", 0}, {respHeaders, "Headers", headers}, {respCookies, "Cookies", cookies}, {respTimeline, "Timeline", len(r.Chain)}}
	var items []widgets.Widget
	for _, s := range tabs {
		label := s.label
		if s.n > 0 {
			label = fmt.Sprintf("%s (%d)", label, s.n)
		}
		items = append(items, ui.NewButton(label, ui.ButtonOpts{Secondary: t.pane.tab != s.id, Small: true, OnPressed: func() {
			t.pane.tab = s.id
			t.pane.scroll.ScrollTo(0, 0)
			t.tab.Refresh()
		}}), pageui.HGap(4))
	}
	return widgets.NewColoredBox(colors.EditorBackground, widgets.NewPadding(painting.NewEdgeInsetsFromLTRB(16, 2, 16, 4), center(items...)))
}

// ---- Body ----

// bodyView is the Body tab: the choice of Pretty, Raw and Preview, and the body.
func (t *reqTab) bodyView(bc widgets.BuildContext) widgets.Widget {
	colors := ui.Theme(bc)
	p := t.pane
	d := p.disp
	dim := ui.TextStyle(bc, 12, colors.DescriptionForeground)
	if d.kind == bodyKindEmpty {
		return tabPad(pageui.Paragraph("The response has no body.", dim))
	}
	var modes []widgets.Widget
	mode := func(id, label string) {
		modes = append(modes, ui.NewButton(label, ui.ButtonOpts{Secondary: p.mode != id, Small: true, OnPressed: func() { t.setMode(id) }}), pageui.HGap(4))
	}
	if d.hasPretty() {
		mode(modePretty, "Pretty")
	}
	if d.textual() || d.binary {
		mode(modeRaw, "Raw")
	}
	if d.kind == bodyKindImage {
		mode(modePreview, "Preview")
	}
	info := d.kind
	if d.mediaType != "" {
		info = d.mediaType
	}
	modes = append(modes, widgets.NewExpanded(widgets.NewText(info, widgets.TextOpts{Style: dim, MaxLines: foundation.Some(1), Overflow: painting.TextOverflowEllipsis, TextAlign: gui.TextAlignEnd}), widgets.ExpandedOpts{}))
	rows := []widgets.Widget{widgets.NewPadding(painting.NewEdgeInsetsFromLTRB(16, 2, 16, 4), center(modes...))}
	var body widgets.Widget
	switch p.mode {
	case modePreview:
		body = t.previewView(bc)
	case modeRaw:
		rows = append(rows, t.pager(bc))
		if note := d.why; note != "" && !d.hasPretty() && d.text {
			rows = append(rows, pageui.NoteLine(bc, colors, pageui.Fine(note)))
		}
		body = t.rawView(bc)
	default:
		if d.why != "" {
			rows = append(rows, pageui.NoteLine(bc, colors, pageui.Fine(d.why)))
		}
		p.pretty.tokenize(t.e.api.Languages())
		body = p.pretty.widget()
	}
	return stretch(append(rows, flex(widgets.NewColoredBox(colors.EditorBackground, body), 1))...)
}

// rawView is the page of the body in the Raw view.
func (t *reqTab) rawView(bc widgets.BuildContext) widgets.Widget {
	p := t.pane
	if p.raw == nil {
		t.showPage(max(p.rawPage, 0))
	}
	if p.raw == nil {
		return tabPad(pageui.Paragraph("The body cannot be read: "+p.err, ui.TextStyle(bc, 13, ui.Theme(bc).ErrorForeground)))
	}
	return p.raw.widget()
}

// pager is the row that moves between the pages of a body that has more than one.
func (t *reqTab) pager(bc widgets.BuildContext) widgets.Widget {
	p := t.pane
	d := p.disp
	if d.pages <= 1 {
		return pageui.Nothing()
	}
	colors := ui.Theme(bc)
	page := max(p.rawPage, 0)
	label := fmt.Sprintf("Page %s of %s  (%s each; %s in all)", thousands(page+1), thousands(d.pages), byteSize(pageBytes), byteSize(d.size))
	return widgets.NewPadding(painting.NewEdgeInsetsFromLTRB(16, 0, 16, 4), center(
		ui.NewIconButton("arrow-up", ui.IconButtonOpts{Tooltip: "First page", OnPressed: func() { t.goPage(0) }, Disabled: page == 0}), pageui.HGap(2),
		ui.NewIconButton("chevron-left", ui.IconButtonOpts{Tooltip: "Previous page", OnPressed: func() { t.goPage(page - 1) }, Disabled: page == 0}), pageui.HGap(2),
		ui.NewIconButton("chevron-right", ui.IconButtonOpts{Tooltip: "Next page", OnPressed: func() { t.goPage(page + 1) }, Disabled: page >= d.pages-1}), pageui.HGap(2),
		ui.NewIconButton("arrow-down", ui.IconButtonOpts{Tooltip: "Last page", OnPressed: func() { t.goPage(d.pages - 1) }, Disabled: page >= d.pages-1}), pageui.HGap(8),
		widgets.NewText(label, widgets.TextOpts{Style: ui.TextStyle(bc, 12, colors.DescriptionForeground), MaxLines: foundation.Some(1), Overflow: painting.TextOverflowEllipsis}),
	))
}

// goPage shows another page of the Raw view.
func (t *reqTab) goPage(page int) {
	t.showPage(page)
	t.tab.Refresh()
}

// thousands writes a number with a comma between the thousands.
func thousands(n int) string {
	s := strconv.Itoa(n)
	for i := len(s) - 3; i > 0; i -= 3 {
		s = s[:i] + "," + s[i:]
	}
	return s
}

// previewView is the image of the Preview.
func (t *reqTab) previewView(bc widgets.BuildContext) widgets.Widget {
	colors := ui.Theme(bc)
	d := t.pane.disp
	if d.pic == nil {
		return tabPad(pageui.Paragraph(d.picNote, ui.TextStyle(bc, 13, colors.EditorWarningForeground)))
	}
	caption := fmt.Sprintf("%d x %d pixels  ·  %s", d.picW, d.picH, byteSize(d.size))
	return stretch(
		flex(widgets.NewPadding(painting.NewEdgeInsetsAll(12), ui.NewImage(d.pic)), 1),
		widgets.NewPadding(painting.NewEdgeInsetsFromLTRB(16, 0, 16, 6), widgets.NewText(caption, widgets.TextOpts{Style: ui.TextStyle(bc, 12, colors.DescriptionForeground)})),
	)
}

// ---- Headers, Cookies and Timeline ----

// scrolled puts rows in the pane's scroll view.
func (t *reqTab) scrolled(children ...widgets.Widget) widgets.Widget {
	return ui.NewScrollView(tabPad(stretch(children...)), &t.pane.scroll)
}

// kvRow is a row of a table of names and values: the name in a column of its own, the value in monospace.
func kvRow(bc widgets.BuildContext, name, value string) widgets.Widget {
	colors := ui.Theme(bc)
	mono := ui.TextStyle(bc, 12, colors.EditorForeground).CopyWith(painting.TextStyleOpts{FontFamily: pageui.MonoFamily(bc)})
	return widgets.NewPadding(painting.NewEdgeInsetsFromLTRB(0, 2, 0, 2), widgets.NewRow([]widgets.Widget{
		wide(widgets.NewText(name, widgets.TextOpts{Style: pageui.Bold(ui.TextStyle(bc, 12, colors.TextLinkForeground))}), 190),
		flex(widgets.NewText(value, widgets.TextOpts{Style: mono}), 1),
	}, widgets.RowOpts{}))
}

// headersView is the Headers tab: the headers of the response, sorted by name.
func (t *reqTab) headersView(bc widgets.BuildContext) widgets.Widget {
	rows := headerRows(t.send.resp.Header)
	if len(rows) == 0 {
		return tabPad(pageui.Paragraph("The response has no headers.", ui.TextStyle(bc, 12, ui.Theme(bc).DescriptionForeground)))
	}
	var items []widgets.Widget
	for _, r := range rows {
		items = append(items, kvRow(bc, r[0], r[1]))
	}
	return t.scrolled(items...)
}

// cookiesView is the Cookies tab: the cookies the response set.
func (t *reqTab) cookiesView(bc widgets.BuildContext) widgets.Widget {
	colors := ui.Theme(bc)
	cookies := t.send.resp.Cookies
	if len(cookies) == 0 {
		return tabPad(pageui.Paragraph("The response set no cookies.", ui.TextStyle(bc, 12, colors.DescriptionForeground)))
	}
	cell := func(s string, style *painting.TextStyle, flexN int) widgets.Widget {
		return flex(widgets.NewPadding(painting.NewEdgeInsetsFromLTRB(0, 0, 8, 0), widgets.NewText(s, widgets.TextOpts{Style: style, MaxLines: foundation.Some(2), Overflow: painting.TextOverflowEllipsis})), flexN)
	}
	head := pageui.Bold(ui.TextStyle(bc, 12, colors.DescriptionForeground))
	body := ui.TextStyle(bc, 12, colors.EditorForeground)
	rows := []widgets.Widget{widgets.NewRow([]widgets.Widget{cell("Name", head, 2), cell("Value", head, 3), cell("Domain", head, 2), cell("Path", head, 1), cell("Expires", head, 2), cell("Flags", head, 2)}, widgets.RowOpts{})}
	for _, c := range cookies {
		expires := "Session"
		switch {
		case c.MaxAge > 0:
			expires = "in " + strconv.Itoa(c.MaxAge) + " s"
		case c.MaxAge < 0:
			expires = "deleted"
		case !c.Expires.IsZero():
			expires = c.Expires.UTC().Format(time.DateTime)
		}
		var flags []string
		if c.HttpOnly {
			flags = append(flags, "HttpOnly")
		}
		if c.Secure {
			flags = append(flags, "Secure")
		}
		if s := sameSiteName(int(c.SameSite)); s != "" {
			flags = append(flags, "SameSite="+s)
		}
		rows = append(rows, widgets.NewPadding(painting.NewEdgeInsetsFromLTRB(0, 3, 0, 3), widgets.NewRow([]widgets.Widget{
			cell(c.Name, pageui.Bold(body), 2), cell(c.Value, body, 3), cell(c.Domain, body, 2), cell(c.Path, body, 1), cell(expires, body, 2), cell(strings.Join(flags, ", "), body, 2),
		}, widgets.RowOpts{})))
	}
	return t.scrolled(rows...)
}

// sameSiteName is the name of http.SameSite's values.
func sameSiteName(v int) string {
	switch v {
	case 2:
		return "Lax"
	case 3:
		return "Strict"
	case 4:
		return "None"
	}
	return ""
}

// timelineView is the Timeline tab: the redirects, the request and the response with their headers, the TLS connection and the timings.
func (t *reqTab) timelineView(bc widgets.BuildContext) widgets.Widget {
	colors := ui.Theme(bc)
	r := t.send.resp
	var items []widgets.Widget
	title := func(s string, c gui.Color) {
		items = append(items, widgets.NewPadding(painting.NewEdgeInsetsFromLTRB(0, 8, 0, 2), widgets.NewText(s, widgets.TextOpts{Style: pageui.Bold(ui.TextStyle(bc, 13, c))})))
	}
	line := func(s string) {
		items = append(items, widgets.NewText(s, widgets.TextOpts{Style: ui.TextStyle(bc, 12, colors.EditorForeground).CopyWith(painting.TextStyleOpts{FontFamily: pageui.MonoFamily(bc)})}))
	}
	headers := func(rows [][2]string) {
		for _, h := range rows {
			items = append(items, kvRow(bc, h[0], h[1]))
		}
	}
	for i, h := range r.Chain {
		title(fmt.Sprintf("%d. %s %s  →  %s (%s)", i+1, h.Method, h.URL, h.Status, h.Reason), respStatusColor(colors, h.StatusCode, false))
		headers(headerRows(h.Header))
		line("Took " + fmtDuration(h.Timings.Total))
	}
	title(fmt.Sprintf("%d. %s %s", len(r.Chain)+1, r.Request.Method, r.URL), colors.TextLinkForeground)
	headers(maskedHeaderRows(r.Request.Header))
	if r.Request.BodySize > 0 {
		line("Body: " + byteSize(r.Request.BodySize))
	}
	title(strings.TrimSpace(r.Proto+" "+r.Status), respStatusColor(colors, r.StatusCode, false))
	headers(headerRows(r.Header))
	if tl := r.TLS; tl != nil {
		title("TLS", colors.EditorForeground)
		for _, kv := range [][2]string{{"Version", tl.Version}, {"Cipher suite", tl.CipherSuite}, {"Server name", tl.ServerName}, {"Subject", tl.Subject}, {"Issuer", tl.Issuer}, {"Expires", tl.NotAfter.UTC().Format(time.DateOnly)}} {
			if kv[1] != "" && !strings.HasPrefix(kv[1], "0001") {
				items = append(items, kvRow(bc, kv[0], kv[1]))
			}
		}
		if r.InsecureTLS {
			items = append(items, widgets.NewText("The certificate was not verified.", widgets.TextOpts{Style: ui.TextStyle(bc, 12, colors.EditorWarningForeground)}))
		}
	}
	title("Timings", colors.EditorForeground)
	for _, row := range timingRows(r.Timings) {
		items = append(items, kvRow(bc, row[0], row[1]))
	}
	if r.Timings.Reused {
		line("The connection was already open.")
	}
	return t.scrolled(items...)
}
