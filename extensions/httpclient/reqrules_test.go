package httpclient

import (
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/rm4n0s/glutter/gluttertest"

	"github.com/rm4n0s/emetos/extension/ui/uitest"
	"github.com/rm4n0s/emetos/extensions/httpclient/format"
)

func TestTheTextOfAValueAndTheValueItIsReadAs(t *testing.T) {
	for _, tc := range []struct {
		name        string
		subject, op string
		value       any
		text        string
	}{
		{"a status number", "status", "equals", 200.0, "200"},
		{"a status number that has a decimal", "time", "lessThan", 12.5, "12.5"},
		{"a big number is not written in exponent form", "json", "equals", 12000000.0, "12000000"},
		{"a string of digits under status", "status", "equals", "200", "200"},
		{"a header text", "header", "equals", "application/json", "application/json"},
		{"a header text that looks like a number", "header", "equals", "42", "42"},
		{"a body text", "body", "contains", "Ada", "Ada"},
		{"a regular expression", "json", "matches", `^\d+$`, `^\d+$`},
		{"a type", "json", "isType", "object", "object"},
		{"a json string", "json", "equals", "Ada", "Ada"},
		{"a json string that looks like a number is quoted", "json", "equals", "17", `"17"`},
		{"a json string that looks like true is quoted", "json", "notEquals", "true", `"true"`},
		{"a json string that is quoted is quoted again", "json", "equals", `"x"`, `"\"x\""`},
		{"a json string with a space and a letter", "json", "contains", "a b", "a b"},
		{"a json number", "json", "equals", 17.0, "17"},
		{"a json bool", "json", "equals", true, "true"},
		{"a json object", "json", "equals", map[string]any{"city": "London"}, `{"city":"London"}`},
		{"a json array", "json", "equals", []any{"a", 1.0}, `["a",1]`},
		{"no value is null for equals", "json", "equals", nil, "null"},
		{"no value is nothing for contains", "json", "contains", nil, ""},
		{"no value for exists", "json", "exists", nil, ""},
		{"no value for a header", "header", "equals", nil, ""},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if got := checkValueText(tc.subject, tc.op, tc.value); got != tc.text {
				t.Errorf("text = %q, want %q", got, tc.text)
			}
			// and the text reads back as the same value (a number that was a string under a numeric check is a number)
			back := parseCheckValue(tc.subject, tc.op, tc.text)
			want := tc.value
			if s, ok := tc.value.(string); ok && numeric(tc.subject, tc.op) {
				if f, ok := back.(float64); ok {
					want = f
				} else {
					want = s
				}
			}
			switch {
			case !hasValue(tc.op):
				want = nil
			case tc.value == nil && tc.text == "": // nothing typed is the empty text
				want = ""
			}
			if !reflect.DeepEqual(back, want) {
				t.Errorf("read back %#v, want %#v", back, want)
			}
		})
	}
}

func TestParsingTheValueOfACheck(t *testing.T) {
	for _, tc := range []struct {
		name        string
		subject, op string
		text        string
		want        any
	}{
		{"a status is a number", "status", "equals", " 404 ", 404.0},
		{"a status that is not a number stays text", "status", "equals", "4xx", "4xx"},
		{"less than is a number", "header", "lessThan", "10", 10.0},
		{"greater than that is not a number stays text", "body", "greaterThan", "many", "many"},
		{"a header is text even when it is a number", "header", "equals", "42", "42"},
		{"a body is text", "body", "contains", "true", "true"},
		{"json true is a bool", "json", "equals", "true", true},
		{"json null is nil", "json", "equals", "null", nil},
		{"json text that is not JSON is text", "json", "equals", "hello world", "hello world"},
		{"json quoted text is the string", "json", "equals", `"42"`, "42"},
		{"json an object", "json", "equals", `{"a":1}`, map[string]any{"a": 1.0}},
		{"json contains a number", "json", "contains", "5", 5.0},
		{"json matches is text", "json", "matches", "5", "5"},
		{"json isType is text", "json", "isType", "null", "null"},
		{"exists has no value", "json", "exists", "ignored", nil},
		{"does not exist has no value", "header", "notExists", "ignored", nil},
		{"empty text", "json", "equals", "", ""},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if got := parseCheckValue(tc.subject, tc.op, tc.text); !reflect.DeepEqual(got, tc.want) {
				t.Errorf("got %#v, want %#v", got, tc.want)
			}
		})
	}
}

const requestWithRules = `// Creates a user.
{
  "emetosHttp": 1,
  "name": "Create user", // shown in the tab
  "method": "POST",
  "url": "{{baseUrl}}/v1/users",
  "x-note": "kept",
  "extract": [
    { "from": "body", "path": "$.id", "to": "userId" },
    { "from": "header", "header": "Location", "regex": "/users/(\\d+)", "to": "location", "scope": "collection" },
    { "from": "status", "to": "code", "scope": "global" },
    { "from": "cookie", "cookie": "session", "to": "session", "scope": "environment" }
  ],
  "checks": [
    { "subject": "status", "op": "equals", "value": 201 },
    { "subject": "header", "header": "Content-Type", "op": "contains", "value": "json" },
    { "subject": "json", "path": "$.name", "op": "equals", "value": "17" },
    { "subject": "json", "path": "$.roles", "op": "isType", "value": "array" },
    { "subject": "time", "op": "lessThan", "value": 800 },
    { "subject": "json", "path": "$.email", "op": "exists" }
  ]
}
`

func TestTheRulesAndChecksOfAFileAreInTheForm(t *testing.T) {
	r := newRig(t, func(folder string) {
		writeFile(t, folder+"/.emetos/http/collections/shop-api/users/create-user.request.jsonc", requestWithRules)
	})
	tab := r.openReq(pCreate)
	d := tab.d
	if len(d.extract) != 4 || len(d.checks) != 6 {
		t.Fatalf("%d rules and %d checks", len(d.extract), len(d.checks))
	}
	if tab.dirty() {
		t.Error("a request with rules is dirty as it opens")
	}
	if tab.countOf(secExtract) != 4 || tab.countOf(secChecks) != 6 {
		t.Errorf("counts %d, %d", tab.countOf(secExtract), tab.countOf(secChecks))
	}
	// the draft writes back what the file had
	node := r.node(r.col(pCreate))
	if got := d.files().Request; !reflect.DeepEqual(got.Extract, node.Request.Extract) || !reflect.DeepEqual(got.Checks, node.Request.Checks) {
		t.Errorf("the draft has\n%+v\n%+v\nthe file has\n%+v\n%+v", got.Extract, got.Checks, node.Request.Extract, node.Request.Checks)
	}
	// the boxes
	if b := d.extract[1]; b.from != "header" || b.source.Value() != "Location" || b.regex.Value() != `/users/(\d+)` || b.to.Value() != "location" || b.scope != "collection" {
		t.Errorf("rule 2 = %+v", b)
	}
	if b := d.checks[2]; b.subject != "json" || b.source.Value() != "$.name" || b.op != "equals" || b.value.Value() != `"17"` {
		t.Errorf("check 3 = %+v / %q", b, b.value.Value())
	}
}

func TestEditingTheRulesAndChecksSavesThemWithTheRequest(t *testing.T) {
	r := newRig(t, func(folder string) {
		writeFile(t, folder+"/.emetos/http/collections/shop-api/users/create-user.request.jsonc", requestWithRules)
	})
	tab := r.openReq(pCreate)
	d := tab.d
	// change a rule and a check, remove one of each, add one of each
	d.extract[0].to.SetValue("newUserId")
	if !tab.dirty() {
		t.Fatal("changing a rule does not make the tab dirty")
	}
	tab.removeExtract(d.extract[3])
	tab.removeCheck(d.checks[5])
	d.checks[0].value.SetValue("200")
	tab.addExtract()
	added := d.extract[len(d.extract)-1]
	added.from = "status"
	added.to.SetValue("lastStatus")
	tab.addCheck() // status equals 200
	d.checks[len(d.checks)-1].op = "greaterThan"
	d.checks[len(d.checks)-1].value.SetValue("199")
	tab.addExtract() // a row nobody filled in is not saved
	tab.save(nil)
	r.settle()
	text := r.read(r.col(pCreate))
	for _, want := range []string{"// Creates a user.", "// shown in the tab", `"x-note": "kept"`, `"to": "newUserId"`, `"to": "lastStatus"`, `"value": 200`, `"op": "greaterThan"`, `"value": 199`} {
		if !strings.Contains(text, want) {
			t.Errorf("the file lacks %q:\n%s", want, text)
		}
	}
	for _, gone := range []string{`"to": "session"`, `"$.email"`} {
		if strings.Contains(text, gone) {
			t.Errorf("the file still has %q", gone)
		}
	}
	n := r.node(r.col(pCreate)).Request
	if len(n.Extract) != 4 || len(n.Checks) != 6 {
		t.Errorf("the saved request has %d rules and %d checks", len(n.Extract), len(n.Checks))
	}
	if tab.dirty() {
		t.Error("dirty after a save")
	}
	// a request read again has the same boxes
	r.api.Pump()
	again := newDraft(r.ext.reqTabs[0].d.files())
	defer again.dispose()
	if len(again.extract) != 4 || again.extract[3].from != "status" || again.extract[3].to.Value() != "lastStatus" {
		t.Errorf("rules after a save: %d", len(again.extract))
	}
}

func TestTheAddButtonsOfTheTabsAddRowsAndMakeTheTabDirty(t *testing.T) {
	r, tab := reqRig(t, pCreate)
	for _, tc := range []struct {
		section, button string
		count           func() int
	}{
		{secExtract, "Add Rule", func() int { return len(tab.d.extract) }},
		{secChecks, "Add Check", func() int { return len(tab.d.checks) }},
	} {
		tab.section = tc.section
		ts := r.showReq(uitest.Themes()[0], tab, reqW, 400)
		ts.Pump(time.Millisecond)
		before := tc.count()
		ts.Tap(gluttertest.First(gluttertest.ByText(tc.button, rich)))
		ts.Pump(time.Millisecond)
		if tc.count() != before+1 {
			t.Errorf("%s: %d rows after, %d before", tc.button, tc.count(), before)
		}
	}
	if !tab.dirty() {
		t.Error("the tab is not dirty after adding a check")
	}
	// an added extraction row has nothing in it: it is not part of the request
	if got := tab.d.files().Request.Extract; len(got) != 0 {
		t.Errorf("a blank rule was kept: %+v", got)
	}
	if got := tab.d.files().Request.Checks; len(got) != 1 || got[0].Subject != "status" || got[0].Value != 200.0 {
		t.Errorf("checks = %+v", got)
	}
}

func TestARequestTabRestoredFromTheStateKeepsItsRules(t *testing.T) {
	r, tab := reqRig(t, pCreate)
	tab.setRules([]format.ExtractRule{{From: "body", Path: "$.id", To: "userId"}}, []format.Check{{Subject: "status", Op: "equals", Value: 201.0}})
	state, err := tab.saveState()
	if err != nil {
		t.Fatal(err)
	}
	st := state.(reqTabState)
	if st.Draft == nil || len(st.Draft.Request.Extract) != 1 || len(st.Draft.Request.Checks) != 1 {
		t.Fatalf("the saved state = %+v", st.Draft)
	}
	again := newDraft(*st.Draft)
	defer again.dispose()
	if len(again.extract) != 1 || len(again.checks) != 1 || again.checks[0].value.Value() != "201" {
		t.Errorf("restored %d rules, %d checks", len(again.extract), len(again.checks))
	}
	_ = r
}
