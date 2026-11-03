package rundata

import (
	"context"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/rm4n0s/errors"
)

func TestParse(t *testing.T) {
	for _, tc := range []struct {
		name, file, text string
		cols             []string
		rows             []map[string]string
	}{
		{"plain csv", "d.csv", "id,name\n1,Ada\n2,Grace\n", []string{"id", "name"},
			[]map[string]string{{"id": "1", "name": "Ada"}, {"id": "2", "name": "Grace"}}},
		{"quotes, commas and line breaks", "d.CSV", "id,note\n1,\"Hello, \"\"world\"\"\"\n2,\"two\nlines\"\n", []string{"id", "note"},
			[]map[string]string{{"id": "1", "note": `Hello, "world"`}, {"id": "2", "note": "two\nlines"}}},
		{"byte order mark and CRLF", "d.csv", "\xef\xbb\xbfid,name\r\n7,Linus\r\n", []string{"id", "name"},
			[]map[string]string{{"id": "7", "name": "Linus"}}},
		{"header names are trimmed", "d.csv", " id , name \n1,x\n", []string{"id", "name"}, []map[string]string{{"id": "1", "name": "x"}}},
		{"empty values stay empty", "d.csv", "a,b\n,2\n", []string{"a", "b"}, []map[string]string{{"a": "", "b": "2"}}},
		{"json objects", "d.json", `[{"id": 1, "name": "Ada", "ok": true}, {"id": 2, "name": "Grace", "ok": false}]`, []string{"id", "name", "ok"},
			[]map[string]string{{"id": "1", "name": "Ada", "ok": "true"}, {"id": "2", "name": "Grace", "ok": "false"}}},
		{"json values are text", "d.json", `[{"n": 1.50, "big": 12345678901234567890, "nil": null, "list": [1, "a"], "obj": {"k": "v"}}]`,
			[]string{"big", "list", "n", "nil", "obj"},
			[]map[string]string{{"n": "1.50", "big": "12345678901234567890", "nil": "", "list": `[1,"a"]`, "obj": `{"k":"v"}`}}},
		{"json objects with other keys", "d.json", "\xef\xbb\xbf[{\"a\": \"1\"}, {\"b\": \"2\"}]", []string{"a", "b"},
			[]map[string]string{{"a": "1"}, {"b": "2"}}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got, err := Parse(tc.file, []byte(tc.text))
			if err != nil {
				t.Fatal(err)
			}
			if !reflect.DeepEqual(got.Columns, tc.cols) {
				t.Errorf("columns = %q, want %q", got.Columns, tc.cols)
			}
			if !reflect.DeepEqual(got.Rows, tc.rows) {
				t.Errorf("rows = %q, want %q", got.Rows, tc.rows)
			}
		})
	}
}

func TestParseFailures(t *testing.T) {
	for _, tc := range []struct {
		name, file, text, route string
	}{
		{"unknown format", "d.txt", "a", "Parse.HttpRunDataFormat"},
		{"empty csv", "d.csv", "", "ParseCSV.HttpRunDataNoHeader"},
		{"only a byte order mark", "d.csv", "\xef\xbb\xbf", "ParseCSV.HttpRunDataNoHeader"},
		{"header only", "d.csv", "id,name\n", "ParseCSV.HttpRunDataNoRows"},
		{"empty column name", "d.csv", "id,\n1,2\n", "ParseCSV.HttpRunDataColumnInvalid"},
		{"duplicate column name", "d.csv", "id,id\n1,2\n", "ParseCSV.HttpRunDataColumnInvalid"},
		{"row with a missing field", "d.csv", "id,name\n1\n", "ParseCSV.HttpRunDataCSVInvalid"},
		{"row with an extra field", "d.csv", "id,name\n1,2,3\n", "ParseCSV.HttpRunDataCSVInvalid"},
		{"quote that never ends", "d.csv", "id,name\n1,\"abc\n", "ParseCSV.HttpRunDataCSVInvalid"},
		{"stray quote", "d.csv", "id,name\n1,ab\"c\n", "ParseCSV.HttpRunDataCSVInvalid"},
		{"json syntax", "d.json", `[{"a": 1,]`, "ParseJSON.HttpRunDataJSONInvalid"},
		{"json text after the array", "d.json", `[{"a": 1}] x`, "ParseJSON.HttpRunDataJSONInvalid"},
		{"json object instead of an array", "d.json", `{"a": 1}`, "ParseJSON.HttpRunDataJSONShape"},
		{"json array of numbers", "d.json", `[1, 2]`, "ParseJSON.HttpRunDataJSONShape"},
		{"json empty array", "d.json", `[]`, "ParseJSON.HttpRunDataNoRows"},
		{"json empty file", "d.json", ``, "ParseJSON.HttpRunDataJSONInvalid"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			_, err := Parse(tc.file, []byte(tc.text))
			appErr, ok := errors.FromError(err)
			if !ok {
				t.Fatalf("expected *errors.Error, got %T (%v)", err, err)
			}
			if !appErr.HasRoute(tc.route) {
				t.Errorf("unexpected failure path: %s", appErr.Route())
			}
		})
	}
}

func TestTooManyRows(t *testing.T) {
	csvText := "a\n" + strings.Repeat("1\n", MaxRows+1)
	jsonText := "[" + strings.Repeat(`{"a":1},`, MaxRows) + `{"a":1}]`
	for name, f := range map[string]func() error{
		"csv":  func() error { _, err := ParseCSV([]byte(csvText)); return err },
		"json": func() error { _, err := ParseJSON([]byte(jsonText)); return err },
	} {
		appErr, ok := errors.FromError(f())
		if !ok || !appErr.HasRoute("HttpRunDataTooManyRows") {
			t.Errorf("%s: %v", name, appErr)
		}
	}
	if _, err := ParseCSV([]byte("a\n" + strings.Repeat("1\n", MaxRows))); err != nil {
		t.Errorf("exactly the most rows: %v", err)
	}
}

func TestLoad(t *testing.T) {
	dir := t.TempDir()
	write := func(name, text string) string {
		p := filepath.Join(dir, name)
		if err := os.WriteFile(p, []byte(text), 0o644); err != nil {
			t.Fatal(err)
		}
		return p
	}
	ctx := context.Background()
	tb, err := Load(ctx, write("ok.csv", "id\n1\n"))
	if err != nil || len(tb.Rows) != 1 {
		t.Fatalf("%v %v", tb, err)
	}
	canceled, cancel := context.WithCancel(ctx)
	cancel()
	for name, tc := range map[string]struct {
		ctx   context.Context
		path  string
		route string
	}{
		"missing":     {ctx, filepath.Join(dir, "none.csv"), "Load.HttpRunDataRead"},
		"bad format":  {ctx, write("x.yaml", "a"), "Load.HttpRunDataFormat"},
		"bad content": {ctx, write("bad.json", "{"), "Parse->ParseJSON.HttpRunDataJSONInvalid"},
		"canceled":    {canceled, filepath.Join(dir, "ok.csv"), "Load.HttpRunDataRead"},
		"a directory": {ctx, dir + ".csv", "Load.HttpRunDataRead"},
		"too large":   {ctx, write("big.csv", "a\n"+strings.Repeat("x", MaxBytes)), "Load.HttpRunDataTooLarge"},
	} {
		t.Run(name, func(t *testing.T) {
			_, err := Load(tc.ctx, tc.path)
			appErr, ok := errors.FromError(err)
			if !ok {
				t.Fatalf("expected *errors.Error, got %T (%v)", err, err)
			}
			if !appErr.HasRoute(tc.route) {
				t.Errorf("unexpected failure path: %s", appErr.Route())
			}
		})
	}
}
