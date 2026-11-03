package httpclient

import (
	"encoding/json"
	"strconv"
	"strings"

	"github.com/rm4n0s/emetos/extension/ui"
	"github.com/rm4n0s/emetos/extensions/httpclient/format"
	"github.com/rm4n0s/emetos/extensions/httpclient/store"
)

// The draft is what the request tab edits: the request of a file as boxes and tables. Nothing here touches a widget except the
// controllers of the boxes, so it is tested without a window.

// pairBox is a row of a table: a parameter, a header, a form field or a multipart field.
type pairBox struct {
	enabled           bool
	name, value, desc *ui.InputBoxController
	// isFile says that the field is a file of a multipart body: value holds its path.
	isFile bool
	// watched says the tab listens to the boxes of the row.
	watched bool
}

func (p *pairBox) dispose() {
	p.name.Dispose()
	p.value.Dispose()
	p.desc.Dispose()
}

// toPair is the pair the row stands for.
func (p *pairBox) toPair() format.Pair {
	pr := format.Pair{Name: p.name.Value(), Description: p.desc.Value(), Enabled: p.enabled}
	if p.isFile {
		pr.File = p.value.Value()
	} else {
		pr.Value = p.value.Value()
	}
	return pr
}

// The fields of the Auth tab that are boxes; which of them an auth type shows is in authFields.
const (
	fUsername     = "username"
	fPassword     = "password"
	fToken        = "token"
	fKey          = "key"
	fValue        = "value"
	fTokenURL     = "tokenUrl"
	fAuthURL      = "authUrl"
	fClientID     = "clientId"
	fClientSecret = "clientSecret"
	fScope        = "scope"
	fAudience     = "audience"
)

// authFields lists the boxes of each type of authorization, in the order the tab shows them.
var authFields = map[string][]string{
	format.AuthBasic:  {fUsername, fPassword},
	format.AuthDigest: {fUsername, fPassword},
	format.AuthBearer: {fToken},
	format.AuthAPIKey: {fKey, fValue},
	format.AuthOAuth2: {fTokenURL, fAuthURL, fClientID, fClientSecret, fScope, fAudience},
}

// authBox is the Auth tab.
type authBox struct {
	typ string
	box map[string]*ui.InputBoxController
	// in is "header" or "query" of an API key, grant the grant of OAuth 2.0, addTo where a token goes.
	in, grant, addTo string
	pkce             bool
}

// tri is a setting a request may leave to the editor's setting.
type tri int

const (
	triDefault tri = iota
	triYes
	triNo
)

// draft is the request tab's copy of a request.
type draft struct {
	// req is the request as loaded: what the form has no box for (the name, the description, the extraction rules and the checks) is
	// written back as it was.
	req format.Request

	method string
	url    *ui.InputBoxController

	params, headers []*pairBox

	bodyType string
	// texts hold the text of each raw kind, so that going to another kind and back loses nothing.
	texts   map[string]*ui.InputBoxController
	gqlVars *ui.InputBoxController
	form    []*pairBox
	multi   []*pairBox
	binary  *ui.InputBoxController

	auth authBox

	timeout, maxRedirects *ui.InputBoxController
	follow, verify        tri
}

// rawKinds are the body types whose text is edited in a text box (and is a file beside the request).
var rawKinds = []string{format.BodyJSON, format.BodyXML, format.BodyText, format.BodyHTML, format.BodyJavaScript, format.BodyGraphQL}

// bodyKinds are the choices of the Body tab, in the order they are shown.
var bodyKinds = []string{format.BodyNone, format.BodyJSON, format.BodyXML, format.BodyText, format.BodyHTML, format.BodyJavaScript, format.BodyGraphQL,
	format.BodyForm, format.BodyMultipart, format.BodyBinary}

// bodyKindLabels are the names the Body tab shows for the kinds.
var bodyKindLabels = map[string]string{
	format.BodyNone: "None", format.BodyJSON: "JSON", format.BodyXML: "XML", format.BodyText: "Text", format.BodyHTML: "HTML",
	format.BodyJavaScript: "JavaScript", format.BodyGraphQL: "GraphQL", format.BodyForm: "Form (urlencoded)", format.BodyMultipart: "Multipart",
	format.BodyBinary: "Binary file",
}

// newDraft makes the draft of a request and the text of its body files. The URL box shows the URL with the enabled parameters in its
// query; a query the file's URL has itself is folded into the Params table (the engine sends both the same way).
func newDraft(files store.RequestFiles) *draft {
	req := files.Request
	d := &draft{req: req, method: req.Method, texts: map[string]*ui.InputBoxController{}}
	if d.method == "" {
		d.method = "GET"
	}
	base, query, fragment, _ := splitURL(req.URL)
	for _, kv := range parseQuery(query) {
		d.params = append(d.params, newPairBox(format.Pair{Name: kv.name, Value: kv.value, Enabled: true}))
	}
	for _, p := range req.Params {
		d.params = append(d.params, newPairBox(p))
	}
	d.url = ui.NewInputBoxController(urlWithQuery(base+fragment, d.paramQuery()))
	for _, h := range req.Headers {
		d.headers = append(d.headers, newPairBox(h))
	}
	d.loadBody(files)
	d.loadAuth(req.Auth)
	d.loadSettings(req.Settings)
	return d
}

func newPairBox(p format.Pair) *pairBox {
	value := p.Value
	if p.File != "" {
		value = p.File
	}
	return &pairBox{enabled: p.Enabled, name: ui.NewInputBoxController(p.Name), value: ui.NewInputBoxController(value), desc: ui.NewInputBoxController(p.Description), isFile: p.File != ""}
}

func (d *draft) loadBody(files store.RequestFiles) {
	for _, k := range rawKinds {
		d.texts[k] = ui.NewInputBoxController("")
	}
	d.gqlVars, d.binary = ui.NewInputBoxController(files.Variables), ui.NewInputBoxController("")
	d.bodyType = format.BodyNone
	b := files.Request.Body
	if b == nil || b.Type == "" {
		return
	}
	d.bodyType = b.Type
	if c, ok := d.texts[b.Type]; ok {
		c.SetValue(files.Body)
	}
	for _, f := range b.Fields {
		switch b.Type {
		case format.BodyMultipart:
			d.multi = append(d.multi, newPairBox(f))
		default:
			d.form = append(d.form, newPairBox(f))
		}
	}
	d.binary.SetValue(b.Path)
}

func (d *draft) loadAuth(a *format.Auth) {
	d.auth = authBox{typ: format.AuthInherit, box: map[string]*ui.InputBoxController{}, in: "header", grant: "client_credentials", addTo: "header", pkce: true}
	for _, fields := range authFields {
		for _, f := range fields {
			if d.auth.box[f] == nil {
				d.auth.box[f] = ui.NewInputBoxController("")
			}
		}
	}
	if a == nil || a.Type == "" {
		return
	}
	d.auth.typ = a.Type
	for f, v := range map[string]string{fUsername: a.Username, fPassword: a.Password, fToken: a.Token, fKey: a.Key, fValue: a.Value, fTokenURL: a.TokenURL,
		fAuthURL: a.AuthURL, fClientID: a.ClientID, fClientSecret: a.ClientSecret, fScope: a.Scope, fAudience: a.Audience} {
		d.auth.box[f].SetValue(v)
	}
	if a.In != "" {
		d.auth.in = a.In
	}
	if a.Grant != "" {
		d.auth.grant = a.Grant
	}
	if a.AddTo != "" {
		d.auth.addTo = a.AddTo
	}
	d.auth.pkce = a.PKCE || a.Type != format.AuthOAuth2
}

func (d *draft) loadSettings(s *format.Settings) {
	d.timeout, d.maxRedirects = ui.NewInputBoxController(""), ui.NewInputBoxController("")
	if s == nil {
		return
	}
	if s.TimeoutMs != nil {
		d.timeout.SetValue(strconv.Itoa(*s.TimeoutMs))
	}
	if s.MaxRedirects != nil {
		d.maxRedirects.SetValue(strconv.Itoa(*s.MaxRedirects))
	}
	d.follow, d.verify = triOf(s.FollowRedirects), triOf(s.VerifyTLS)
}

func triOf(b *bool) tri {
	switch {
	case b == nil:
		return triDefault
	case *b:
		return triYes
	}
	return triNo
}

func (t tri) ptr() *bool {
	switch t {
	case triYes:
		v := true
		return &v
	case triNo:
		v := false
		return &v
	}
	return nil
}

// dispose lets go of every controller.
func (d *draft) dispose() {
	d.url.Dispose()
	for _, rows := range [][]*pairBox{d.params, d.headers, d.form, d.multi} {
		for _, r := range rows {
			r.dispose()
		}
	}
	for _, c := range d.texts {
		c.Dispose()
	}
	d.gqlVars.Dispose()
	d.binary.Dispose()
	for _, c := range d.auth.box {
		c.Dispose()
	}
	d.timeout.Dispose()
	d.maxRedirects.Dispose()
}

// ---- params and URL ----

// paramQuery is the query the enabled parameters make.
func (d *draft) paramQuery() string {
	rows := make([]paramRow, len(d.params))
	for i, p := range d.params {
		rows[i] = paramRow{p.name.Value(), p.value.Value(), p.enabled}
	}
	return queryOfRows(rows)
}

// urlFromParams writes the query of the enabled parameters into the URL box; it returns whether the box changed.
func (d *draft) urlFromParams() bool {
	text := urlWithQuery(d.url.Value(), d.paramQuery())
	if text == d.url.Value() {
		return false
	}
	d.url.SetValue(text)
	return true
}

// paramsFromURL changes the Params table to match the query typed in the URL box (see rowsFromQuery); it returns whether the table
// changed.
func (d *draft) paramsFromURL() bool {
	_, query, _, _ := splitURL(d.url.Value())
	rows := make([]paramRow, len(d.params))
	for i, p := range d.params {
		rows[i] = paramRow{p.name.Value(), p.value.Value(), p.enabled}
	}
	out, from := rowsFromQuery(rows, query)
	changed := len(out) != len(rows)
	next := make([]*pairBox, len(out))
	used := map[int]bool{}
	for j, row := range out {
		if i := from[j]; i >= 0 {
			used[i] = true
			b := d.params[i]
			if b.name.Value() != row.name || b.value.Value() != row.value || b.enabled != row.enabled {
				changed = true
				b.name.SetValue(row.name)
				b.value.SetValue(row.value)
				b.enabled = row.enabled
			}
			next[j] = b
			continue
		}
		changed = true
		next[j] = newPairBox(format.Pair{Name: row.name, Value: row.value, Enabled: true})
	}
	for i, b := range d.params {
		if !used[i] {
			b.dispose()
		}
	}
	d.params = next
	return changed
}

// ---- back to a request ----

// pairs are the pairs of the rows worth keeping: a row with nothing in it is a row someone added and did not fill.
func pairs(rows []*pairBox) []format.Pair {
	var out []format.Pair
	for _, r := range rows {
		p := r.toPair()
		if p.Name == "" && p.Value == "" && p.File == "" && p.Description == "" {
			continue
		}
		out = append(out, p)
	}
	return out
}

// files is the request the draft stands for, with the text of its body files.
func (d *draft) files() store.RequestFiles {
	req := d.req
	req.Method = d.method
	base, _, fragment, _ := splitURL(d.url.Value())
	req.URL = base + fragment
	req.Params, req.Headers = pairs(d.params), pairs(d.headers)
	req.Auth = d.authValue()
	req.Settings = d.settingsValue()
	out := store.RequestFiles{}
	out.Request.Body, out.Body, out.Variables = d.bodyValue(req.Body)
	req.Body = out.Request.Body
	out.Request = req
	return out
}

// bodyValue is the Body of the request and the text of its files. old is the body the request had: a raw body of the same kind keeps
// the file names it had.
func (d *draft) bodyValue(old *format.Body) (b *format.Body, text, variables string) {
	if d.bodyType == format.BodyNone || d.bodyType == "" {
		if old == nil {
			return nil, "", ""
		}
		return &format.Body{Type: format.BodyNone}, "", ""
	}
	b = &format.Body{Type: d.bodyType}
	if old != nil && old.Type == d.bodyType {
		b.File, b.Variables = old.File, old.Variables
	}
	switch d.bodyType {
	case format.BodyForm:
		b.Fields = pairs(d.form)
	case format.BodyMultipart:
		b.Fields = pairs(d.multi)
	case format.BodyBinary:
		b.Path = strings.TrimSpace(d.binary.Value())
	default:
		text = d.texts[d.bodyType].Value()
		if d.bodyType == format.BodyGraphQL {
			variables = d.gqlVars.Value()
		}
	}
	return b, text, variables
}

func (d *draft) authValue() *format.Auth {
	a := d.auth
	if a.typ == format.AuthInherit || a.typ == "" {
		if d.req.Auth != nil && d.req.Auth.Type == format.AuthInherit {
			return d.req.Auth
		}
		return nil
	}
	out := &format.Auth{Type: a.typ}
	get := func(f string) string { return a.box[f].Value() }
	for _, f := range authFields[a.typ] {
		switch f {
		case fUsername:
			out.Username = get(f)
		case fPassword:
			out.Password = get(f)
		case fToken:
			out.Token = get(f)
		case fKey:
			out.Key = get(f)
		case fValue:
			out.Value = get(f)
		case fTokenURL:
			out.TokenURL = get(f)
		case fAuthURL:
			out.AuthURL = get(f)
		case fClientID:
			out.ClientID = get(f)
		case fClientSecret:
			out.ClientSecret = get(f)
		case fScope:
			out.Scope = get(f)
		case fAudience:
			out.Audience = get(f)
		}
	}
	switch a.typ {
	case format.AuthAPIKey:
		out.In = a.in
	case format.AuthOAuth2:
		out.Grant, out.PKCE, out.AddTo = a.grant, a.pkce, a.addTo
	}
	return out
}

func (d *draft) settingsValue() *format.Settings {
	s := &format.Settings{FollowRedirects: d.follow.ptr(), VerifyTLS: d.verify.ptr()}
	if n, err := strconv.Atoi(strings.TrimSpace(d.timeout.Value())); err == nil && n >= 0 {
		s.TimeoutMs = &n
	}
	if n, err := strconv.Atoi(strings.TrimSpace(d.maxRedirects.Value())); err == nil && n >= 0 {
		s.MaxRedirects = &n
	}
	if *s == (format.Settings{}) {
		return nil
	}
	return s
}

// snapshot is everything the person can change, as one text: the tab is dirty while it differs from the baseline. The names of the body files are
// left out.
func (d *draft) snapshot() string {
	f := d.files()
	if b := f.Request.Body; b != nil { // the names of the body files are the store's, not something the form edits
		c := *b
		c.File, c.Variables = "", ""
		f.Request.Body = &c
	}
	data, err := json.Marshal(f)
	if err != nil {
		return ""
	}
	return string(data)
}

// ---- bulk edit ----

// bulkText writes rows as the lines of the bulk editor: "name: value", a "# " before a row that is off.
func bulkText(rows []*pairBox) string {
	var b strings.Builder
	for _, r := range pairs(rows) {
		if !r.Enabled {
			b.WriteString("# ")
		}
		b.WriteString(r.Name + ": " + r.Value + "\n")
	}
	return strings.TrimSuffix(b.String(), "\n")
}

// parseBulk reads the lines of the bulk editor: "name: value" (the first colon cuts), "# name: value" for a row that is off; blank lines
// are skipped. A line without a colon is a name with no value.
func parseBulk(text string) []format.Pair {
	var out []format.Pair
	for _, line := range strings.Split(text, "\n") {
		line = strings.TrimRight(line, "\r")
		enabled := true
		if rest, ok := strings.CutPrefix(strings.TrimLeft(line, " \t"), "#"); ok {
			enabled, line = false, strings.TrimPrefix(rest, " ")
		}
		if strings.TrimSpace(line) == "" {
			continue
		}
		name, value, _ := strings.Cut(line, ":")
		out = append(out, format.Pair{Name: strings.TrimSpace(name), Value: strings.TrimPrefix(value, " "), Enabled: enabled})
	}
	return out
}

// setRows replaces the rows with the pairs, keeping the boxes (and so the descriptions) of the rows that stay at the same place.
func setRows(rows []*pairBox, pairs []format.Pair) []*pairBox {
	out := make([]*pairBox, len(pairs))
	for i, p := range pairs {
		if i < len(rows) {
			b := rows[i]
			b.name.SetValue(p.Name)
			b.value.SetValue(p.Value)
			b.enabled = p.Enabled
			out[i] = b
			continue
		}
		out[i] = newPairBox(p)
	}
	for i := len(pairs); i < len(rows); i++ {
		rows[i].dispose()
	}
	return out
}
