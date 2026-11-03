package httpclient

import (
	"encoding/json"
	"reflect"
	"strconv"
	"strings"

	"github.com/rm4n0s/glutter/foundation"
	"github.com/rm4n0s/glutter/painting"
	gui "github.com/rm4n0s/glutter/ui"
	"github.com/rm4n0s/glutter/widgets"

	"github.com/rm4n0s/emetos/extension/ui"
	"github.com/rm4n0s/emetos/extensions/httpclient/checks"
	"github.com/rm4n0s/emetos/extensions/httpclient/format"
	"github.com/rm4n0s/emetos/extensions/internal/pageui"
)

// The Extract and Checks tabs of the request (SPEC §10.33 "Extraction and checks"): forms over the rules and the checks of the
// request, saved with it by the edits of step 27.4.

// ---- the draft of the rules ----

// The places an extraction rule takes its value from, in the order the tab offers them.
var (
	extractFroms      = []string{"body", "header", "status", "cookie"}
	extractFromLabels = map[string]string{"body": "Body", "header": "Header", "status": "Status", "cookie": "Cookie"}
	extractScopes     = []string{scopeEnvironment, scopeCollection, scopeGlobal}
	extractScopeNames = map[string]string{scopeEnvironment: "Environment", scopeCollection: "Collection", scopeGlobal: "Global"}
)

// extractBox is a row of the Extract tab.
type extractBox struct {
	from string
	// source is the JSONPath of a body, the name of a header or of a cookie; a status has none.
	source, regex, to *ui.InputBoxController
	scope             string
	watched           bool
}

func newExtractBox(r format.ExtractRule) *extractBox {
	source := r.Path
	switch r.From {
	case "header":
		source = r.Header
	case "cookie":
		source = r.Cookie
	}
	from := r.From
	if from == "" {
		from = "body"
	}
	return &extractBox{from: from, source: ui.NewInputBoxController(source), regex: ui.NewInputBoxController(r.Regex), to: ui.NewInputBoxController(r.To), scope: r.Scope}
}

func (b *extractBox) dispose() {
	b.source.Dispose()
	b.regex.Dispose()
	b.to.Dispose()
}

// rule is the rule the row stands for.
func (b *extractBox) rule() format.ExtractRule {
	r := format.ExtractRule{From: b.from, Regex: b.regex.Value(), To: strings.TrimSpace(b.to.Value()), Scope: b.scope}
	switch b.from {
	case "body":
		r.Path = strings.TrimSpace(b.source.Value())
	case "header":
		r.Header = strings.TrimSpace(b.source.Value())
	case "cookie":
		r.Cookie = strings.TrimSpace(b.source.Value())
	}
	return r
}

// blank says that nobody filled the row in.
func (b *extractBox) blank() bool {
	return b.source.Value() == "" && b.regex.Value() == "" && strings.TrimSpace(b.to.Value()) == ""
}

// The subjects and operators of a check, in the order the tab offers them.
var (
	checkSubjects      = []string{"status", "time", "header", "body", "json"}
	checkSubjectLabels = map[string]string{"status": "Status", "time": "Time (ms)", "header": "Header", "body": "Body", "json": "JSON"}
	checkOps           = []string{"equals", "notEquals", "contains", "matches", "lessThan", "greaterThan", "exists", "notExists", "isType"}
	checkOpLabels      = map[string]string{
		"equals": "equals", "notEquals": "does not equal", "contains": "contains", "matches": "matches", "lessThan": "is less than",
		"greaterThan": "is greater than", "exists": "exists", "notExists": "does not exist", "isType": "is of type",
	}
)

// checkBox is a row of the Checks tab.
type checkBox struct {
	subject string
	// source is the JSONPath of a json check and the name of a header; the other subjects have none.
	source  *ui.InputBoxController
	op      string
	value   *ui.InputBoxController
	watched bool
}

func newCheckBox(c format.Check) *checkBox {
	subject, op := c.Subject, c.Op
	if subject == "" {
		subject = "status"
	}
	if op == "" {
		op = "equals"
	}
	source := c.Path
	if subject == "header" {
		source = c.Header
	}
	return &checkBox{subject: subject, source: ui.NewInputBoxController(source), op: op, value: ui.NewInputBoxController(checkValueText(subject, op, c.Value))}
}

func (b *checkBox) dispose() {
	b.source.Dispose()
	b.value.Dispose()
}

// check is the check the row stands for.
func (b *checkBox) check() format.Check {
	c := format.Check{Subject: b.subject, Op: b.op, Value: parseCheckValue(b.subject, b.op, b.value.Value())}
	switch b.subject {
	case "header":
		c.Header = strings.TrimSpace(b.source.Value())
	case "json":
		c.Path = strings.TrimSpace(b.source.Value())
	}
	return c
}

// hasValue says that the operator takes a value.
func hasValue(op string) bool { return op != "exists" && op != "notExists" }

// numeric says that the value of the check is a number: the status and the time are, and so are the two comparisons.
func numeric(subject, op string) bool {
	return subject == "status" || subject == "time" || op == "lessThan" || op == "greaterThan"
}

// parseCheckValue reads the text of the value box as the value of a check:
//   - no value for exists and notExists;
//   - a number where the value is a number (the status, the time, less than, greater than) and the text is one, else the text;
//   - for a JSON subject with equals, does not equal or contains, the JSON the text is (17, true, null, "text", {...}, [...]), else the
//     text as it is (so a quoted text is the way to compare with a string that looks like a number);
//   - the text as it is for everything else.
func parseCheckValue(subject, op, text string) any {
	if !hasValue(op) {
		return nil
	}
	if numeric(subject, op) {
		if f, err := strconv.ParseFloat(strings.TrimSpace(text), 64); err == nil {
			return f
		}
		return text
	}
	if subject == "json" && (op == "equals" || op == "notEquals" || op == "contains") {
		var v any
		if err := json.Unmarshal([]byte(text), &v); err == nil {
			return v
		}
	}
	return text
}

// checkValueText is the text parseCheckValue reads back as the same value: the inverse of it.
func checkValueText(subject, op string, v any) string {
	switch x := v.(type) {
	case nil:
		if subject == "json" && (op == "equals" || op == "notEquals") {
			return "null" // a check without a value compares with null
		}
		return ""
	case string:
		if subject == "json" && (op == "equals" || op == "notEquals" || op == "contains") && !numeric(subject, op) {
			if p := parseCheckValue(subject, op, x); !reflect.DeepEqual(p, any(x)) { // a text that reads as something else is quoted
				if data, err := json.Marshal(x); err == nil {
					return string(data)
				}
			}
		}
		return x
	case float64:
		return strconv.FormatFloat(x, 'f', -1, 64)
	}
	data, err := json.Marshal(v)
	if err != nil {
		return ""
	}
	return string(data)
}

// ---- the draft ----

func (d *draft) loadRules() {
	d.extract, d.checks = nil, nil
	for _, r := range d.req.Extract {
		d.extract = append(d.extract, newExtractBox(r))
	}
	for _, c := range d.req.Checks {
		d.checks = append(d.checks, newCheckBox(c))
	}
}

func (d *draft) extractRules() []format.ExtractRule {
	var out []format.ExtractRule
	for _, b := range d.extract {
		if !b.blank() {
			out = append(out, b.rule())
		}
	}
	return out
}

func (d *draft) checkList() []format.Check {
	var out []format.Check
	for _, b := range d.checks {
		out = append(out, b.check())
	}
	return out
}

func (d *draft) disposeRules() {
	for _, b := range d.extract {
		b.dispose()
	}
	for _, b := range d.checks {
		b.dispose()
	}
}

// ---- the tab ----

// listen calls fn when the text of the box changes.
func (t *reqTab) listen(c *ui.InputBoxController, fn func()) {
	last := c.Value()
	c.TextController().AddListener(func() {
		if v := c.Value(); v != last {
			last = v
			fn()
		}
	})
}

func (t *reqTab) watchExtract(b *extractBox) {
	b.watched = true
	for _, c := range []*ui.InputBoxController{b.source, b.regex, b.to} {
		t.listen(c, t.edited)
	}
}

func (t *reqTab) watchCheck(b *checkBox) {
	b.watched = true
	for _, c := range []*ui.InputBoxController{b.source, b.value} {
		t.listen(c, t.edited)
	}
}

// watchRules makes the boxes of the rules and the checks tell the tab.
func (t *reqTab) watchRules() {
	for _, b := range t.d.extract {
		t.watchExtract(b)
	}
	for _, b := range t.d.checks {
		t.watchCheck(b)
	}
}

// addExtract adds a rule to the Extract tab; it starts from the body, the common case.
func (t *reqTab) addExtract() {
	b := newExtractBox(format.ExtractRule{From: "body"})
	t.d.extract = append(t.d.extract, b)
	t.watchExtract(b)
	t.edited()
}

func (t *reqTab) removeExtract(b *extractBox) {
	for i, x := range t.d.extract {
		if x == b {
			t.d.extract = append(t.d.extract[:i], t.d.extract[i+1:]...)
			b.dispose()
			break
		}
	}
	t.edited()
}

// addCheck adds a check to the Checks tab; it starts with the commonest one, the status is 200.
func (t *reqTab) addCheck() {
	b := newCheckBox(format.Check{Subject: "status", Op: "equals", Value: 200.0})
	t.d.checks = append(t.d.checks, b)
	t.watchCheck(b)
	t.edited()
}

func (t *reqTab) removeCheck(b *checkBox) {
	for i, x := range t.d.checks {
		if x == b {
			t.d.checks = append(t.d.checks[:i], t.d.checks[i+1:]...)
			b.dispose()
			break
		}
	}
	t.edited()
}

// setRules replaces the rules and the checks of the draft (a test, or a request read again).
func (t *reqTab) setRules(rules []format.ExtractRule, list []format.Check) {
	t.d.disposeRules()
	t.d.req.Extract, t.d.req.Checks = rules, list
	t.d.loadRules()
	t.watchRules()
	t.edited()
}

// ---- the widgets ----

func dropdownOf(labels []string, selected int, width float64, disabled bool, onChanged func(int)) widgets.Widget {
	opts := make([]ui.DropdownOption, len(labels))
	for i, l := range labels {
		opts[i] = ui.DropdownOption{Label: l}
	}
	return wide(ui.NewDropdown(opts, ui.DropdownOpts{Selected: max(selected, 0), Disabled: disabled, Width: foundation.Some(width), OnChanged: onChanged}), width)
}

// textBox is an input box in a row of a rule table.
func (t *reqTab) textBox(c *ui.InputBoxController, hint string, mono bool) widgets.Widget {
	return ui.NewInputBox(nil, nil, ui.InputBoxOpts{Key: foundation.NewObjectKey(c), Controller: c, Placeholder: hint, Disabled: t.readOnly, Monospace: mono})
}

// head is a heading of a column of a rule table.
func head(bc widgets.BuildContext, s string, w float64, n int) widgets.Widget {
	st := ui.TextStyle(bc, 12, ui.Theme(bc).DescriptionForeground)
	if w > 0 {
		return wide(widgets.NewText(s, widgets.TextOpts{Style: st}), w)
	}
	return flex(widgets.NewText(s, widgets.TextOpts{Style: st}), n)
}

// extractPanel is the Extract tab.
func (t *reqTab) extractPanel(bc widgets.BuildContext) widgets.Widget {
	colors := ui.Theme(bc)
	dim := ui.TextStyle(bc, 12, colors.DescriptionForeground)
	locked := t.readOnly
	rows := []widgets.Widget{
		pageui.Paragraph("After a response arrives, each rule takes a value out of it and keeps it as the current value of a variable on this computer. "+
			"The value is never written into a file of the project, and a secret variable's value goes to the keyring. "+
			"The path is a JSONPath ($, .name, ['name'], [n], [*] for the first match, ..name); a regular expression takes its first group.", dim),
		pageui.Gap(10),
		widgets.NewRow([]widgets.Widget{head(bc, "From", 112, 0), pageui.HGap(8), head(bc, "Path, header or cookie", 0, 3), pageui.HGap(8), head(bc, "Regular expression", 0, 3), pageui.HGap(8),
			head(bc, "Variable", 0, 2), pageui.HGap(8), head(bc, "Scope", 146, 0), wide(widgets.NewText("", widgets.TextOpts{}), 40)}, widgets.RowOpts{}),
		pageui.Gap(4),
	}
	for _, b := range t.d.extract {
		rows = append(rows, t.extractRow(bc, b), pageui.Gap(4))
	}
	if len(t.d.extract) == 0 {
		rows = append(rows, pageui.Paragraph("No rules yet.", dim), pageui.Gap(4))
	}
	rows = append(rows, pageui.Gap(4), center(t.smallButton("Add Rule", "Add a rule that takes a value out of the response", t.addExtract, locked)))
	return stretch(rows...)
}

func (t *reqTab) extractRow(bc widgets.BuildContext, b *extractBox) widgets.Widget {
	colors := ui.Theme(bc)
	locked := t.readOnly
	labels := make([]string, len(extractFroms))
	for k, f := range extractFroms {
		labels[k] = extractFromLabels[f]
	}
	scopeLabels := make([]string, len(extractScopes))
	for k, s := range extractScopes {
		scopeLabels[k] = extractScopeNames[s]
	}
	from := dropdownOf(labels, indexOf(extractFroms, b.from), 112, locked, func(k int) { b.from = extractFroms[k]; t.edited() })
	var source widgets.Widget
	switch b.from {
	case "body":
		source = t.textBox(b.source, "$.data.id (empty: the whole body)", true)
	case "header":
		source = t.textBox(b.source, "header name", false)
	case "cookie":
		source = t.textBox(b.source, "cookie name", false)
	default:
		source = widgets.NewText("the status code", widgets.TextOpts{Style: ui.TextStyle(bc, 12, colors.DescriptionForeground)})
	}
	scope := b.scope
	if scope == "" {
		scope = scopeEnvironment
	}
	row := center(from, pageui.HGap(8), flex(source, 3), pageui.HGap(8), flex(t.textBox(b.regex, "optional, first group", true), 3), pageui.HGap(8),
		flex(t.textBox(b.to, "variable name", false), 2), pageui.HGap(8),
		dropdownOf(scopeLabels, indexOf(extractScopes, scope), 146, locked, func(k int) { b.scope = extractScopes[k]; t.edited() }),
		ui.NewIconButton("trash", ui.IconButtonOpts{Tooltip: "Remove the rule", Disabled: locked, OnPressed: func() { t.removeExtract(b) }}))
	if line := t.extractResult(bc, b); line != nil {
		return stretch(row, line)
	}
	return row
}

// extractResult is the line under a rule that says what it did the last time a response arrived.
func (t *reqTab) extractResult(bc widgets.BuildContext, b *extractBox) widgets.Widget {
	colors := ui.Theme(bc)
	rule := b.rule()
	for _, o := range t.send.extracted {
		if o.Rule != rule {
			continue
		}
		if !o.OK() {
			return resultLine(bc, "Not extracted: "+o.Problem, colors.ErrorForeground)
		}
		return resultLine(bc, rule.To+" = "+clipText(o.shown(), 80)+"   ("+o.Scope+" current value)", colGood)
	}
	return nil
}

// resultLine is a line of small text under a row.
func resultLine(bc widgets.BuildContext, text string, color gui.Color) widgets.Widget {
	return widgets.NewPadding(painting.NewEdgeInsetsFromLTRB(120, 1, 0, 3), widgets.NewText(text, widgets.TextOpts{
		Style: ui.TextStyle(bc, 12, color), MaxLines: foundation.Some(2), Overflow: painting.TextOverflowEllipsis,
	}))
}

// clipText cuts a text for a line of the form.
func clipText(s string, n int) string {
	if len(s) <= n {
		return s
	}
	cut := n
	for cut > 0 && s[cut]&0xC0 == 0x80 {
		cut--
	}
	return s[:cut] + "..."
}

// checksPanel is the Checks tab.
func (t *reqTab) checksPanel(bc widgets.BuildContext) widgets.Widget {
	dim := ui.TextStyle(bc, 12, ui.Theme(bc).DescriptionForeground)
	locked := t.readOnly
	rows := []widgets.Widget{
		pageui.Paragraph("After a response arrives, each check tests it, and the Checks tab of the response says which passed and which failed. "+
			"A JSON check looks at the first value its path selects. Numbers are compared by value, and a regular expression matches anywhere in the value.", dim),
		pageui.Gap(10),
		widgets.NewRow([]widgets.Widget{head(bc, "Check", 112, 0), pageui.HGap(8), head(bc, "Path or header", 0, 3), pageui.HGap(8), head(bc, "Operator", 164, 0), pageui.HGap(8),
			head(bc, "Value", 0, 4), wide(widgets.NewText("", widgets.TextOpts{}), 40)}, widgets.RowOpts{}),
		pageui.Gap(4),
	}
	for _, b := range t.d.checks {
		rows = append(rows, t.checkRow(bc, b), pageui.Gap(4))
	}
	if len(t.d.checks) == 0 {
		rows = append(rows, pageui.Paragraph("No checks yet.", dim), pageui.Gap(4))
	}
	rows = append(rows, pageui.Gap(4), center(t.smallButton("Add Check", "Add a check that tests the response", t.addCheck, locked)))
	return stretch(rows...)
}

func (t *reqTab) checkRow(bc widgets.BuildContext, b *checkBox) widgets.Widget {
	colors := ui.Theme(bc)
	locked := t.readOnly
	subjects := make([]string, len(checkSubjects))
	for k, s := range checkSubjects {
		subjects[k] = checkSubjectLabels[s]
	}
	ops := make([]string, len(checkOps))
	for k, o := range checkOps {
		ops[k] = checkOpLabels[o]
	}
	subject := dropdownOf(subjects, indexOf(checkSubjects, b.subject), 112, locked, func(k int) { b.subject = checkSubjects[k]; t.edited() })
	dimText := func(s string) widgets.Widget {
		return widgets.NewText(s, widgets.TextOpts{Style: ui.TextStyle(bc, 12, colors.DescriptionForeground)})
	}
	var source widgets.Widget
	switch b.subject {
	case "json":
		source = t.textBox(b.source, "$.data.id (empty: the whole body)", true)
	case "header":
		source = t.textBox(b.source, "header name", false)
	case "status":
		source = dimText("the status code")
	case "time":
		source = dimText("the time of the exchange")
	default:
		source = dimText("the body as text")
	}
	var value widgets.Widget
	switch {
	case !hasValue(b.op):
		value = dimText("no value")
	default:
		value = t.textBox(b.value, valueHint(b), b.op == "matches" || b.subject == "json")
	}
	row := center(subject, pageui.HGap(8), flex(source, 3), pageui.HGap(8),
		dropdownOf(ops, indexOf(checkOps, b.op), 164, locked, func(k int) { b.op = checkOps[k]; t.edited() }), pageui.HGap(8),
		flex(value, 4),
		ui.NewIconButton("trash", ui.IconButtonOpts{Tooltip: "Remove the check", Disabled: locked, OnPressed: func() { t.removeCheck(b) }}))
	if line := t.checkResult(bc, b); line != nil {
		return stretch(row, line)
	}
	return row
}

// valueHint says what the value of a check can be.
func valueHint(b *checkBox) string {
	switch {
	case b.op == "isType":
		return "string, number, boolean, null, object or array"
	case b.op == "matches":
		return "regular expression"
	case numeric(b.subject, b.op):
		return "a number"
	case b.subject == "json":
		return `17, "text", true, null, {...}, [...]`
	}
	return "text"
}

// checkResult is the line under a check that says how it came out the last time a response arrived.
func (t *reqTab) checkResult(bc widgets.BuildContext, b *checkBox) widgets.Widget {
	colors := ui.Theme(bc)
	c := b.check()
	for _, r := range t.send.checkResults {
		if !reflect.DeepEqual(r.Check, c) {
			continue
		}
		switch {
		case r.Passed:
			return resultLine(bc, "Passed: "+clipText(r.Actual, 80), colGood)
		case r.Problem != "":
			return resultLine(bc, "Failed: "+r.Problem, colors.ErrorForeground)
		}
		return resultLine(bc, "Failed: the response has "+clipText(r.Actual, 80), colors.ErrorForeground)
	}
	return nil
}

// checkCounts is the number of checks that passed and the number there are, for the label of the Checks tab of the response.
func checkCounts(results []checks.Result) (passed, total int) {
	for _, r := range results {
		if r.Passed {
			passed++
		}
	}
	return passed, len(results)
}
