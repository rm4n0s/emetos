package checks

import (
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/rm4n0s/errors"

	"github.com/rm4n0s/emetos/extensions/httpclient/engine"
	"github.com/rm4n0s/emetos/extensions/httpclient/format"
)

const apiBody = `{"id":17,"name":"Ada Example","token":"tok-abc123","active":true,"zip":null,"ratio":1.5,
"roles":["admin","billing"],"address":{"city":"London"},"orders":[{"id":9,"sku":"A-1"},{"id":10,"sku":"B-2"}],"big":12345678901234567890}`

// apiResponse is a response with a JSON body, headers, a repeated header and two cookies, in 250 ms.
func apiResponse(body string) *engine.Response {
	h := http.Header{}
	h.Set("Content-Type", "application/json; charset=utf-8")
	h.Set("X-Count", "42")
	h.Set("X-Request-Id", "req-7f3a9c")
	h.Add("Link", "<a>; rel=next")
	h.Add("Link", "<b>; rel=last")
	return &engine.Response{
		Status: "200 OK", StatusCode: 200, Header: h, Body: engine.NewMemoryBody([]byte(body)),
		Cookies: []*http.Cookie{{Name: "session", Value: "s3cret-1"}, {Name: "theme", Value: "dark"}},
		Timings: engine.Timings{Total: 250 * time.Millisecond},
	}
}

func wantTag(t *testing.T, err error, route string) {
	t.Helper()
	appErr, ok := errors.FromError(err)
	if !ok {
		t.Fatalf("expected *errors.Error, got %T (%v)", err, err)
	}
	if !appErr.HasRoute(route) {
		t.Errorf("unexpected failure path: %s, want %s", appErr.Route(), route)
	}
}

func TestExtract(t *testing.T) {
	for _, tc := range []struct {
		name string
		rule format.ExtractRule
		want string
		// route is the route of the failure; empty when the rule finds a value
		route string
	}{
		{"a number of the body", format.ExtractRule{From: "body", Path: "$.id", To: "v"}, "17", ""},
		{"a number keeps its digits", format.ExtractRule{From: "body", Path: "$.big", To: "v"}, "12345678901234567890", ""},
		{"a decimal", format.ExtractRule{From: "body", Path: "$.ratio", To: "v"}, "1.5", ""},
		{"a string of the body", format.ExtractRule{From: "body", Path: "$.token", To: "v"}, "tok-abc123", ""},
		{"a bool", format.ExtractRule{From: "body", Path: "$.active", To: "v"}, "true", ""},
		{"a null", format.ExtractRule{From: "body", Path: "$.zip", To: "v"}, "null", ""},
		{"an object as JSON", format.ExtractRule{From: "body", Path: "$.address", To: "v"}, `{"city":"London"}`, ""},
		{"an array as JSON", format.ExtractRule{From: "body", Path: "$.roles", To: "v"}, `["admin","billing"]`, ""},
		{"the first of a star", format.ExtractRule{From: "body", Path: "$.orders[*].sku", To: "v"}, "A-1", ""},
		{"an index", format.ExtractRule{From: "body", Path: "$.orders[1].id", To: "v"}, "10", ""},
		{"at any depth", format.ExtractRule{From: "body", Path: "$..city", To: "v"}, "London", ""},
		{"a regex group of a value of the body", format.ExtractRule{From: "body", Path: "$.token", Regex: `tok-(\w+)`, To: "v"}, "abc123", ""},
		{"the whole body", format.ExtractRule{From: "body", To: "v"}, apiBody, ""},
		{"a regex over the whole body", format.ExtractRule{From: "body", Regex: `"id":(\d+)`, To: "v"}, "17", ""},
		{"a header", format.ExtractRule{From: "header", Header: "x-request-id", To: "v"}, "req-7f3a9c", ""},
		{"a repeated header", format.ExtractRule{From: "header", Header: "Link", To: "v"}, "<a>; rel=next, <b>; rel=last", ""},
		{"a regex group of a header", format.ExtractRule{From: "header", Header: "X-Request-Id", Regex: `req-(.+)`, To: "v"}, "7f3a9c", ""},
		{"a regex without a group gives the match", format.ExtractRule{From: "header", Header: "X-Request-Id", Regex: `[0-9a-f]{6}$`, To: "v"}, "7f3a9c", ""},
		{"the status", format.ExtractRule{From: "status", To: "v"}, "200", ""},
		{"a regex over the status", format.ExtractRule{From: "status", Regex: `^(\d)`, To: "v"}, "2", ""},
		{"a cookie", format.ExtractRule{From: "cookie", Cookie: "session", To: "v"}, "s3cret-1", ""},
		{"a regex over a cookie", format.ExtractRule{From: "cookie", Cookie: "session", Regex: `-(\d)$`, To: "v"}, "1", ""},

		{"a path that matches nothing", format.ExtractRule{From: "body", Path: "$.nothing", To: "v"}, "", "subject.extract.ChecksNoMatch"},
		{"a bad path", format.ExtractRule{From: "body", Path: "$.a[?(@.b)]", To: "v"}, "", "subject.extract->subject.jsonValue->Compile.JSONPathUnsupported"},
		{"a header that is not there", format.ExtractRule{From: "header", Header: "X-Nothing", To: "v"}, "", "subject.extract.ChecksHeaderMissing"},
		{"a header without a name", format.ExtractRule{From: "header", To: "v"}, "", "subject.extract.ChecksSourceMissing"},
		{"a cookie that is not there", format.ExtractRule{From: "cookie", Cookie: "nope", To: "v"}, "", "subject.extract.ChecksCookieMissing"},
		{"a cookie without a name", format.ExtractRule{From: "cookie", To: "v"}, "", "subject.extract.ChecksSourceMissing"},
		{"a bad regex", format.ExtractRule{From: "status", Regex: `(`, To: "v"}, "", "subject.extract.ChecksRegexInvalid"},
		{"a regex that matches nothing", format.ExtractRule{From: "status", Regex: `^9`, To: "v"}, "", "subject.extract.ChecksRegexNoMatch"},
		{"a place that is unknown", format.ExtractRule{From: "trailer", To: "v"}, "", "subject.extract.ChecksUnknownSource"},
		{"no variable", format.ExtractRule{From: "status"}, "", "subject.extract.ChecksTargetMissing"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			s := newSubject(apiResponse(apiBody))
			got, err := s.extract(tc.rule)
			if tc.route != "" {
				wantTag(t, err, tc.route)
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			if got != tc.want {
				t.Errorf("got %q, want %q", got, tc.want)
			}
		})
	}
}

func TestExtractGivesAResultForEachRule(t *testing.T) {
	rules := []format.ExtractRule{
		{From: "body", Path: "$.id", To: "userId"},
		{From: "body", Path: "$.nothing", To: "gone"},
		{From: "status", To: "code"},
	}
	got := Extract(apiResponse(apiBody), rules)
	if len(got) != 3 {
		t.Fatalf("%d results", len(got))
	}
	if !got[0].OK() || got[0].Value != "17" || got[0].Index != 0 || got[0].Rule.To != "userId" {
		t.Errorf("first: %+v", got[0])
	}
	if got[1].OK() || got[1].Value != "" || !strings.Contains(got[1].Problem, "matches nothing") || !strings.Contains(got[1].Problem, "path=$.nothing") {
		t.Errorf("second: %+v", got[1])
	}
	if !got[2].OK() || got[2].Value != "200" {
		t.Errorf("third: %+v", got[2])
	}
}

func TestTheBodyIsNotJSONOrTooLarge(t *testing.T) {
	t.Run("not JSON", func(t *testing.T) {
		s := newSubject(apiResponse("<html></html>"))
		_, err := s.extract(format.ExtractRule{From: "body", Path: "$.id", To: "v"})
		wantTag(t, err, "subject.extract->subject.jsonValue->subject.json->Decode.JSONPathBadDocument")
	})
	t.Run("not JSON, twice: the body is decoded once", func(t *testing.T) {
		s := newSubject(apiResponse("nope"))
		_, e1 := s.json()
		_, e2 := s.json()
		if e1 == nil || e2 == nil {
			t.Fatal("expected errors")
		}
	})
	t.Run("too large", func(t *testing.T) {
		s := newSubject(apiResponse(strings.Repeat("x", MaxBody+1)))
		_, err := s.extract(format.ExtractRule{From: "body", To: "v"})
		wantTag(t, err, "subject.extract->subject.text.ChecksBodyTooLarge")
	})
	t.Run("no body", func(t *testing.T) {
		r := apiResponse("")
		r.Body = nil
		s := newSubject(r)
		got, err := s.extract(format.ExtractRule{From: "body", To: "v"})
		if err != nil || got != "" {
			t.Errorf("got %q, %v", got, err)
		}
	})
}

func TestChecks(t *testing.T) {
	for _, tc := range []struct {
		name  string
		check format.Check
		pass  bool
		// actual is what the result shows
		actual string
	}{
		{"status equals a number", format.Check{Subject: "status", Op: "equals", Value: 200.0}, true, "200"},
		{"status equals a number that is not", format.Check{Subject: "status", Op: "equals", Value: 404.0}, false, "200"},
		{"status equals the text of a number", format.Check{Subject: "status", Op: "equals", Value: "200"}, true, "200"},
		{"status not equals", format.Check{Subject: "status", Op: "notEquals", Value: 500.0}, true, "200"},
		{"status not equals, failing", format.Check{Subject: "status", Op: "notEquals", Value: 200.0}, false, "200"},
		{"status below", format.Check{Subject: "status", Op: "lessThan", Value: 300.0}, true, "200"},
		{"status below, failing", format.Check{Subject: "status", Op: "lessThan", Value: 200.0}, false, "200"},
		{"status above", format.Check{Subject: "status", Op: "greaterThan", Value: 199.0}, true, "200"},
		{"status above, failing", format.Check{Subject: "status", Op: "greaterThan", Value: 200.0}, false, "200"},
		{"status matches", format.Check{Subject: "status", Op: "matches", Value: "^2\\d\\d$"}, true, "200"},
		{"status contains", format.Check{Subject: "status", Op: "contains", Value: "0"}, true, "200"},
		{"status exists", format.Check{Subject: "status", Op: "exists"}, true, "200"},
		{"status is a number", format.Check{Subject: "status", Op: "isType", Value: "number"}, true, "200"},

		{"time below", format.Check{Subject: "time", Op: "lessThan", Value: 500.0}, true, "250"},
		{"time below, failing", format.Check{Subject: "time", Op: "lessThan", Value: 100.0}, false, "250"},
		{"time above", format.Check{Subject: "time", Op: "greaterThan", Value: 100.0}, true, "250"},

		{"header equals", format.Check{Subject: "header", Header: "x-request-id", Op: "equals", Value: "req-7f3a9c"}, true, "req-7f3a9c"},
		{"header equals, failing", format.Check{Subject: "header", Header: "X-Request-Id", Op: "equals", Value: "other"}, false, "req-7f3a9c"},
		{"header equals a number", format.Check{Subject: "header", Header: "X-Count", Op: "equals", Value: 42.0}, true, "42"},
		{"header not equals", format.Check{Subject: "header", Header: "X-Count", Op: "notEquals", Value: "41"}, true, "42"},
		{"header contains", format.Check{Subject: "header", Header: "Content-Type", Op: "contains", Value: "json"}, true, "application/json; charset=utf-8"},
		{"header contains, failing", format.Check{Subject: "header", Header: "Content-Type", Op: "contains", Value: "xml"}, false, "application/json; charset=utf-8"},
		{"header matches", format.Check{Subject: "header", Header: "Content-Type", Op: "matches", Value: `^application/(json|xml)`}, true, "application/json; charset=utf-8"},
		{"header matches, failing", format.Check{Subject: "header", Header: "Content-Type", Op: "matches", Value: `^text/`}, false, "application/json; charset=utf-8"},
		{"header below", format.Check{Subject: "header", Header: "X-Count", Op: "lessThan", Value: 50.0}, true, "42"},
		{"header above", format.Check{Subject: "header", Header: "X-Count", Op: "greaterThan", Value: 50.0}, false, "42"},
		{"header exists", format.Check{Subject: "header", Header: "X-Count", Op: "exists"}, true, "42"},
		{"header exists, missing", format.Check{Subject: "header", Header: "X-Nothing", Op: "exists"}, false, "(missing)"},
		{"header not exists", format.Check{Subject: "header", Header: "X-Nothing", Op: "notExists"}, true, "(missing)"},
		{"header not exists, there", format.Check{Subject: "header", Header: "X-Count", Op: "notExists"}, false, "42"},
		{"header is a string", format.Check{Subject: "header", Header: "X-Count", Op: "isType", Value: "string"}, true, "42"},
		{"a repeated header", format.Check{Subject: "header", Header: "Link", Op: "contains", Value: "rel=last"}, true, "<a>; rel=next, <b>; rel=last"},

		{"body contains", format.Check{Subject: "body", Op: "contains", Value: "Ada Example"}, true, ""},
		{"body contains, failing", format.Check{Subject: "body", Op: "contains", Value: "Grace"}, false, ""},
		{"body matches", format.Check{Subject: "body", Op: "matches", Value: `"id":\s*17`}, true, ""},
		{"body equals", format.Check{Subject: "body", Op: "equals", Value: "{}"}, false, ""},
		{"body exists", format.Check{Subject: "body", Op: "exists"}, true, ""},
		{"body is a string", format.Check{Subject: "body", Op: "isType", Value: "string"}, true, ""},

		{"json equals a number", format.Check{Subject: "json", Path: "$.id", Op: "equals", Value: 17.0}, true, "17"},
		{"json equals a number, failing", format.Check{Subject: "json", Path: "$.id", Op: "equals", Value: 18.0}, false, "17"},
		{"json equals a string", format.Check{Subject: "json", Path: "$.name", Op: "equals", Value: "Ada Example"}, true, "Ada Example"},
		{"json string is not the number", format.Check{Subject: "json", Path: "$.name", Op: "equals", Value: 17.0}, false, "Ada Example"},
		{"json equals a bool", format.Check{Subject: "json", Path: "$.active", Op: "equals", Value: true}, true, "true"},
		{"json equals a null", format.Check{Subject: "json", Path: "$.zip", Op: "equals", Value: nil}, true, "null"},
		{"json equals an object", format.Check{Subject: "json", Path: "$.address", Op: "equals", Value: map[string]any{"city": "London"}}, true, `{"city":"London"}`},
		{"json equals an array", format.Check{Subject: "json", Path: "$.roles", Op: "equals", Value: []any{"admin", "billing"}}, true, `["admin","billing"]`},
		{"json not equals", format.Check{Subject: "json", Path: "$.name", Op: "notEquals", Value: "Grace"}, true, "Ada Example"},
		{"json default path is the root", format.Check{Subject: "json", Op: "isType", Value: "object"}, true, ""},
		{"json contains in a string", format.Check{Subject: "json", Path: "$.name", Op: "contains", Value: "Example"}, true, "Ada Example"},
		{"json contains an element", format.Check{Subject: "json", Path: "$.roles", Op: "contains", Value: "billing"}, true, `["admin","billing"]`},
		{"json contains an element, failing", format.Check{Subject: "json", Path: "$.roles", Op: "contains", Value: "root"}, false, `["admin","billing"]`},
		{"json contains a key", format.Check{Subject: "json", Path: "$.address", Op: "contains", Value: "city"}, true, `{"city":"London"}`},
		{"json matches", format.Check{Subject: "json", Path: "$.token", Op: "matches", Value: `^tok-[a-z0-9]+$`}, true, "tok-abc123"},
		{"json below", format.Check{Subject: "json", Path: "$.ratio", Op: "lessThan", Value: 2.0}, true, "1.5"},
		{"json above", format.Check{Subject: "json", Path: "$.ratio", Op: "greaterThan", Value: 2.0}, false, "1.5"},
		{"json big number above", format.Check{Subject: "json", Path: "$.big", Op: "greaterThan", Value: 1.0}, true, "12345678901234567890"},
		{"json exists", format.Check{Subject: "json", Path: "$.orders[1].sku", Op: "exists"}, true, "B-2"},
		{"json null exists", format.Check{Subject: "json", Path: "$.zip", Op: "exists"}, true, "null"},
		{"json exists, no match", format.Check{Subject: "json", Path: "$.orders[2]", Op: "exists"}, false, "(missing)"},
		{"json not exists", format.Check{Subject: "json", Path: "$.nothing", Op: "notExists"}, true, "(missing)"},
		{"json not exists, there", format.Check{Subject: "json", Path: "$.id", Op: "notExists"}, false, "17"},
		{"json is a string", format.Check{Subject: "json", Path: "$.name", Op: "isType", Value: "string"}, true, "Ada Example"},
		{"json is a number", format.Check{Subject: "json", Path: "$.id", Op: "isType", Value: "number"}, true, "17"},
		{"json is a boolean", format.Check{Subject: "json", Path: "$.active", Op: "isType", Value: "boolean"}, true, "true"},
		{"json is null", format.Check{Subject: "json", Path: "$.zip", Op: "isType", Value: "null"}, true, "null"},
		{"json is an object", format.Check{Subject: "json", Path: "$.address", Op: "isType", Value: "object"}, true, `{"city":"London"}`},
		{"json is an array", format.Check{Subject: "json", Path: "$.roles", Op: "isType", Value: "array"}, true, `["admin","billing"]`},
		{"json is not the type", format.Check{Subject: "json", Path: "$.id", Op: "isType", Value: "string"}, false, "17"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got := Run(apiResponse(apiBody), []format.Check{tc.check})
			if len(got) != 1 {
				t.Fatalf("%d results", len(got))
			}
			r := got[0]
			if r.Passed != tc.pass || r.Problem != "" {
				t.Errorf("passed = %v, problem %q; want %v", r.Passed, r.Problem, tc.pass)
			}
			// the body's actual is the whole body: only its start is compared
			if tc.check.Subject != "body" && tc.check.Path != "" || tc.check.Subject == "status" || tc.check.Subject == "time" || tc.check.Subject == "header" {
				if r.Actual != tc.actual {
					t.Errorf("actual = %q, want %q", r.Actual, tc.actual)
				}
			}
			if r.What == "" || r.Expected == "" {
				t.Errorf("What %q Expected %q", r.What, r.Expected)
			}
		})
	}
}

func TestChecksThatCannotBeMade(t *testing.T) {
	for _, tc := range []struct {
		name  string
		check format.Check
		body  string
		route string
	}{
		{"an unknown subject", format.Check{Subject: "trailer", Op: "equals", Value: "x"}, apiBody, "subject.value.ChecksUnknownSubject"},
		{"a header without a name", format.Check{Subject: "header", Op: "equals", Value: "x"}, apiBody, "subject.value.ChecksSourceMissing"},
		{"a header that is not there", format.Check{Subject: "header", Header: "X-Nothing", Op: "equals", Value: "x"}, apiBody, "subject.value.ChecksHeaderMissing"},
		{"a path that matches nothing", format.Check{Subject: "json", Path: "$.nothing", Op: "equals", Value: "x"}, apiBody, "subject.value.ChecksNoMatch"},
		{"a bad path", format.Check{Subject: "json", Path: "id", Op: "equals", Value: "x"}, apiBody, "subject.value->subject.jsonValue->Compile.JSONPathNotRoot"},
		{"a body that is not JSON", format.Check{Subject: "json", Path: "$.id", Op: "equals", Value: "x"}, "<html/>", "subject.value->subject.jsonValue->subject.json->Decode.JSONPathBadDocument"},
		{"a body that is too large", format.Check{Subject: "body", Op: "contains", Value: "x"}, strings.Repeat("y", MaxBody+1), "subject.value->subject.text.ChecksBodyTooLarge"},
		{"an unknown operator", format.Check{Subject: "status", Op: "near", Value: 1.0}, apiBody, "compare.ChecksUnknownOp"},
		{"a bad regex", format.Check{Subject: "status", Op: "matches", Value: "("}, apiBody, "compare.ChecksRegexInvalid"},
		{"a value that is not a number", format.Check{Subject: "json", Path: "$.name", Op: "lessThan", Value: 3.0}, apiBody, "compare.ChecksNotANumber"},
		{"a check value that is not a number", format.Check{Subject: "status", Op: "greaterThan", Value: "many"}, apiBody, "compare.ChecksNotANumber"},
		{"a type that is unknown", format.Check{Subject: "status", Op: "isType", Value: "integer"}, apiBody, "compare.ChecksUnknownType"},
		{"a type that is not text", format.Check{Subject: "status", Op: "isType", Value: 3.0}, apiBody, "compare.ChecksUnknownType"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			resp := apiResponse(tc.body)
			s := newSubject(resp)
			v, found, err := s.value(tc.check)
			if err == nil {
				_, err = compare(tc.check, v, found)
			}
			wantTag(t, err, tc.route)
			// and the same through Run: the check fails with its reason, and the others go on
			got := Run(resp, []format.Check{tc.check, {Subject: "status", Op: "equals", Value: 200.0}})
			if got[0].Passed || got[0].Problem == "" {
				t.Errorf("the check passed or has no problem: %+v", got[0])
			}
			if !got[1].Passed {
				t.Errorf("the next check failed: %+v", got[1])
			}
		})
	}
}

func TestExistsOfWhatCannotBeRead(t *testing.T) {
	for _, tc := range []struct {
		name  string
		check format.Check
		body  string
		pass  bool
	}{
		{"exists in a body that is not JSON", format.Check{Subject: "json", Path: "$.id", Op: "exists"}, "<html/>", false},
		{"not exists in a body that is not JSON", format.Check{Subject: "json", Path: "$.id", Op: "notExists"}, "<html/>", true},
		{"an empty body does not exist", format.Check{Subject: "body", Op: "exists"}, "", false},
		{"an empty body does not exist, the other way", format.Check{Subject: "body", Op: "notExists"}, "", true},
		{"an empty body equals the empty text", format.Check{Subject: "body", Op: "equals", Value: ""}, "", true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			r := Run(apiResponse(tc.body), []format.Check{tc.check})[0]
			if r.Passed != tc.pass || r.Problem != "" {
				t.Errorf("%+v", r)
			}
		})
	}
}

func TestTheWordsOfAResult(t *testing.T) {
	for _, tc := range []struct {
		check          format.Check
		what, expected string
	}{
		{format.Check{Subject: "status", Op: "equals", Value: 200.0}, "status", "equals 200"},
		{format.Check{Subject: "header", Header: "Content-Type", Op: "contains", Value: "json"}, "header Content-Type", `contains "json"`},
		{format.Check{Subject: "json", Path: "$.id", Op: "greaterThan", Value: 0.0}, "json $.id", "is greater than 0"},
		{format.Check{Subject: "json", Op: "isType", Value: "object"}, "json $", `is of type "object"`},
		{format.Check{Subject: "json", Path: "$.x", Op: "notExists"}, "json $.x", "does not exist"},
		{format.Check{Subject: "time", Op: "lessThan", Value: 500.0}, "time", "is less than 500"},
		{format.Check{Subject: "body", Op: "notEquals", Value: "x"}, "body", `does not equal "x"`},
		{format.Check{Subject: "body", Op: "matches", Value: "^a"}, "body", `matches "^a"`},
		{format.Check{Subject: "body", Op: "exists"}, "body", "exists"},
	} {
		if got := What(tc.check); got != tc.what {
			t.Errorf("What(%+v) = %q, want %q", tc.check, got, tc.what)
		}
		if got := Expected(tc.check); got != tc.expected {
			t.Errorf("Expected(%+v) = %q, want %q", tc.check, got, tc.expected)
		}
	}
}

func TestClipDoesNotCutACharacterInTwo(t *testing.T) {
	long := strings.Repeat("é", 150)
	got := clip(long)
	if !strings.HasSuffix(got, "...") || len(got) > 204 {
		t.Errorf("len %d", len(got))
	}
	for _, r := range got {
		if r == '�' {
			t.Fatal("a character was cut in two")
		}
	}
}
