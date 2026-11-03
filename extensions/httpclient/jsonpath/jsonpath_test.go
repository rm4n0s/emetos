package jsonpath

import (
	"encoding/json"
	"testing"

	"github.com/rm4n0s/errors"
)

const store = `{
  "id": 17, "name": "Ada", "a-b": "dash", "it's": "quote", "": "empty",
  "tags": ["x", "y", "z"],
  "address": {"city": "London", "zip": null, "tags": ["home"]},
  "orders": [{"id": 1, "items": [{"sku": "A"}, {"sku": "B"}]}, {"id": 2, "items": []}],
  "big": 12345678901234567890, "ratio": 1.50, "ok": true
}`

func TestEval(t *testing.T) {
	doc, err := Decode([]byte(store))
	if err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct {
		name, expr string
		want       string // the matches as JSON
	}{
		{"the root", `$`, ``},
		{"a member", `$.name`, `["Ada"]`},
		{"a number keeps its text", `$.big`, `[12345678901234567890]`},
		{"a number keeps its trailing zero", `$.ratio`, `[1.50]`},
		{"a nested member", `$.address.city`, `["London"]`},
		{"null is a value", `$.address.zip`, `[null]`},
		{"a bracket name", `$['name']`, `["Ada"]`},
		{"a double quoted bracket name", `$["name"]`, `["Ada"]`},
		{"a dash in a dot name", `$.a-b`, `["dash"]`},
		{"a quote in a quoted name", `$['it\'s']`, `["quote"]`},
		{"a quote in a double quoted name", `$["it's"]`, `["quote"]`},
		{"an empty quoted name", `$['']`, `["empty"]`},
		{"a unicode escape", `$['name']`, `["Ada"]`},
		{"an index", `$.tags[1]`, `["y"]`},
		{"the first index", `$.tags[0]`, `["x"]`},
		{"a negative index", `$.tags[-1]`, `["z"]`},
		{"an index past the end", `$.tags[3]`, `[]`},
		{"a negative index past the start", `$.tags[-4]`, `[]`},
		{"an index of an object", `$.address[0]`, `[]`},
		{"a name of an array", `$.tags.x`, `[]`},
		{"a name that is not there", `$.nothing`, `[]`},
		{"a name of a scalar", `$.id.x`, `[]`},
		{"every element", `$.tags[*]`, `["x","y","z"]`},
		{"every element with a dot", `$.tags.*`, `["x","y","z"]`},
		{"every member in key order", `$.address.*`, `["London",["home"],null]`},
		{"an index after a star", `$.orders[*].id`, `[1,2]`},
		{"two stars", `$.orders[*].items[*].sku`, `["A","B"]`},
		{"a name at any depth", `$..sku`, `["A","B"]`},
		{"a name at any depth, parents first", `$..tags`, `[["x","y","z"],["home"]]`},
		{"a name at any depth under a member", `$.orders..sku`, `["A","B"]`},
		{"a bracket name at any depth", `$..['city']`, `["London"]`},
		{"an index at any depth", `$..[0]`, `["home",{"id":1,"items":[{"sku":"A"},{"sku":"B"}]},{"sku":"A"},"x"]`},
		{"a star at any depth under a member", `$.address..*`, `["London",["home"],null,"home"]`},
		{"no match at any depth", `$..nothing`, `[]`},
		{"spaces around the expression", ` $.name `, `["Ada"]`},
	} {
		t.Run(tc.name, func(t *testing.T) {
			p, err := Compile(tc.expr)
			if err != nil {
				t.Fatal(err)
			}
			got := p.Eval(doc)
			if tc.expr == `$` {
				if len(got) != 1 {
					t.Fatalf("the root gave %d matches", len(got))
				}
				return
			}
			data := []byte("[]")
			if len(got) > 0 {
				var err error
				if data, err = json.Marshal(got); err != nil {
					t.Fatal(err)
				}
			}
			if string(data) != tc.want {
				t.Errorf("%s gave %s, want %s", tc.expr, data, tc.want)
			}
			if p.String() != tc.expr {
				t.Errorf("String() = %q", p.String())
			}
		})
	}
}

func TestFirst(t *testing.T) {
	doc, err := Decode([]byte(store))
	if err != nil {
		t.Fatal(err)
	}
	p, _ := Compile("$.tags[*]")
	if v, ok := p.First(doc); !ok || v != "x" {
		t.Errorf("First = %v, %v", v, ok)
	}
	p, _ = Compile("$.nothing")
	if _, ok := p.First(doc); ok {
		t.Error("First found something that is not there")
	}
}

func TestCompileErrors(t *testing.T) {
	for _, tc := range []struct{ name, expr, tag string }{
		{"empty", ``, "JSONPathNotRoot"},
		{"no root", `name`, "JSONPathNotRoot"},
		{"a dot name without a root", `.name`, "JSONPathNotRoot"},
		{"a bare character", `$name`, "JSONPathUnexpected"},
		{"a character after a bracket", `$.a]`, "JSONPathUnexpected"},
		{"a dot at the end", `$.`, "JSONPathEmptyName"},
		{"two dots at the end", `$..`, "JSONPathEmptyName"},
		{"a dot before a quote", `$.'a'`, "JSONPathEmptyName"},
		{"a bracket that is not closed", `$.a[0`, "JSONPathUnterminated"},
		{"a quote that is not closed", `$['a]`, "JSONPathUnterminated"},
		{"a quote closed too early", `$['a'b]`, "JSONPathBadQuote"},
		{"a bad escape", `$['a\q']`, "JSONPathBadQuote"},
		{"a bad unicode escape", `$['\u00zz']`, "JSONPathBadQuote"},
		{"a short unicode escape", `$['\u00']`, "JSONPathBadQuote"},
		{"an escape at the end", `$['a\']`, "JSONPathUnterminated"},
		{"a filter", `$.a[?(@.b == 1)]`, "JSONPathUnsupported"},
		{"a slice", `$.a[1:3]`, "JSONPathUnsupported"},
		{"a union", `$.a[0,1]`, "JSONPathUnsupported"},
		{"an empty bracket", `$.a[]`, "JSONPathUnsupported"},
		{"an index too large", `$.a[99999999999999999999]`, "JSONPathIndexRange"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			_, err := Compile(tc.expr)
			if err == nil {
				t.Fatalf("%q was accepted", tc.expr)
			}
			appErr, ok := errors.FromError(err)
			if !ok {
				t.Fatalf("expected *errors.Error, got %T", err)
			}
			if !appErr.HasRoute("Compile.") || appErr.Tag != tc.tag {
				t.Errorf("unexpected failure path: %s (tag %s, want %s)", appErr.Route(), appErr.Tag, tc.tag)
			}
		})
	}
}

func TestDecode(t *testing.T) {
	for _, tc := range []struct {
		name, data string
		bad        bool
	}{
		{"an object", `{"a":1}`, false},
		{"a scalar", `42`, false},
		{"white space around", " [1] \n", false},
		{"not JSON", `<html>`, true},
		{"two values", `{} {}`, true},
		{"empty", ``, true},
		{"cut", `{"a":`, true},
		{"not UTF-8", "\"\xff\"", true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			_, err := Decode([]byte(tc.data))
			if !tc.bad {
				if err != nil {
					t.Fatal(err)
				}
				return
			}
			appErr, ok := errors.FromError(err)
			if !ok {
				t.Fatalf("expected *errors.Error, got %T", err)
			}
			if !appErr.HasRoute("Decode.JSONPathBadDocument") {
				t.Errorf("unexpected failure path: %s", appErr.Route())
			}
		})
	}
}
