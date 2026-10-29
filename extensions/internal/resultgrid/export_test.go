package resultgrid

import (
	"bytes"
	"context"
	"encoding/csv"
	"encoding/json"
	"io"
	"math"
	"strings"
	"testing"
	"time"

	"github.com/rm4n0s/errors"
)

func TestWriteCSV(t *testing.T) {
	m := NewModel(ModelOptions{})
	m.SetSource(people())
	tests := []struct {
		name string
		prep func()
		opts CSVOptions
		want string
	}{
		{"as the source has it", func() {}, CSVOptions{}, "id,name,score,born,note\n" +
			"3,carol,7.5,1990-05-17T00:00:00Z,\n" +
			"1,Alice,9.25,1985-01-02T00:00:00Z,\"line one\nline two\"\n" +
			"2,bob,,2001-12-31T00:00:00Z,tab\there\n" +
			"10,Bob,-1,,\"say \"\"hi\"\"\"\n"},
		{"sorted, as it is shown", func() { m.SortBy(0, SortDesc) }, CSVOptions{NoHeader: true},
			"10,Bob,-1,,\"say \"\"hi\"\"\"\n" +
				"3,carol,7.5,1990-05-17T00:00:00Z,\n" +
				"2,bob,,2001-12-31T00:00:00Z,tab\there\n" +
				"1,Alice,9.25,1985-01-02T00:00:00Z,\"line one\nline two\"\n"},
		{"with a text for NULL and a semicolon", func() { m.SortBy(0, SortAsc) }, CSVOptions{NullText: "NULL", Comma: ';'},
			"id;name;score;born;note\n" +
				"1;Alice;9.25;1985-01-02T00:00:00Z;\"line one\nline two\"\n" +
				"2;bob;NULL;2001-12-31T00:00:00Z;tab\there\n" +
				"3;carol;7.5;1990-05-17T00:00:00Z;NULL\n" +
				"10;Bob;-1;NULL;\"say \"\"hi\"\"\"\n"},
	}
	for _, tt := range tests {
		tt.prep()
		var buf bytes.Buffer
		if err := m.Export().WriteCSV(t.Context(), &buf, tt.opts); err != nil {
			t.Fatalf("%s: %v", tt.name, err)
		}
		if buf.String() != tt.want {
			t.Errorf("%s:\n got %q\nwant %q", tt.name, buf.String(), tt.want)
		}
	}
	// what it writes reads back with the standard reader
	var buf bytes.Buffer
	m.SortBy(0, SortNone)
	if err := m.Export().WriteCSV(t.Context(), &buf, CSVOptions{}); err != nil {
		t.Fatal(err)
	}
	recs, err := csv.NewReader(&buf).ReadAll()
	if err != nil || len(recs) != 5 || recs[2][4] != "line one\nline two" || recs[4][4] != `say "hi"` {
		t.Errorf("read back %q, %v", recs, err)
	}
}

func TestWriteJSON(t *testing.T) {
	tbl := NewTable([]Column{{Name: "id"}, {Name: "name"}, {Name: "name"}, {Name: "f"}, {Name: "ok"}, {Name: "blob"}, {Name: "doc"}, {Name: "bad"}, {Name: "when"}, {Name: "<tag>&"}})
	when := time.Date(2026, 10, 29, 1, 2, 3, 0, time.UTC)
	tbl.Append(
		[]Value{Int(1), Text("a \"quoted\" <b> & \n line"), Text("dup"), Float(1.5), Bool(true), Bytes([]byte{0xca, 0xfe}), JSON(`{"x": [1, 2]}`), JSON("{not json"), Time(when), Text("v")},
		[]Value{Int(2), Null(), Null(), Float(math.NaN()), Bool(false), Null(), Null(), Null(), Null(), Float(math.Inf(1))},
	)
	m := NewModel(ModelOptions{})
	m.SetSource(tbl)
	var buf bytes.Buffer
	if err := m.Export().WriteJSON(t.Context(), &buf); err != nil {
		t.Fatal(err)
	}
	var rows []map[string]any
	if err := json.Unmarshal(buf.Bytes(), &rows); err != nil {
		t.Fatalf("the output is not JSON: %v\n%s", err, buf.String())
	}
	if len(rows) != 2 {
		t.Fatalf("%d rows", len(rows))
	}
	r0 := rows[0]
	checks := map[string]any{
		"id": float64(1), "name": "a \"quoted\" <b> & \n line", "name_2": "dup", "f": 1.5, "ok": true, "blob": "0xcafe",
		"doc": map[string]any{"x": []any{1.0, 2.0}}, "bad": "{not json", "when": "2026-10-29T01:02:03Z", "<tag>&": "v",
	}
	for k, want := range checks {
		got, ok := r0[k]
		if !ok || !jsonEqual(got, want) {
			t.Errorf("row 0 %q = %#v, want %#v", k, got, want)
		}
	}
	r1 := rows[1]
	for _, k := range []string{"name", "name_2", "blob", "doc", "bad", "when"} {
		if v, ok := r1[k]; !ok || v != nil {
			t.Errorf("row 1 %q = %#v, want null", k, v)
		}
	}
	if r1["f"] != "NaN" || r1["<tag>&"] != "+Inf" || r1["ok"] != false {
		t.Errorf("row 1 = %#v", r1)
	}
	if strings.Contains(buf.String(), `\u003c`) {
		t.Error("the characters < > & are escaped for no reason")
	}
	// an empty result is an empty array
	empty := NewModel(ModelOptions{})
	empty.SetSource(NewTable([]Column{{Name: "a"}}))
	buf.Reset()
	if err := empty.Export().WriteJSON(t.Context(), &buf); err != nil || strings.TrimSpace(buf.String()) != "[]" {
		t.Errorf("empty = %q, %v", buf.String(), err)
	}
}

func jsonEqual(a, b any) bool {
	x, _ := json.Marshal(a)
	y, _ := json.Marshal(b)
	return bytes.Equal(x, y)
}

func TestAnExportOfAHundredThousandRowsIsStreamed(t *testing.T) {
	m := NewModel(ModelOptions{})
	m.SetSource(numbered(100_000))
	m.SortBy(0, SortDesc)
	var csvBuf bytes.Buffer
	if err := m.Export().WriteCSV(t.Context(), &csvBuf, CSVOptions{}); err != nil {
		t.Fatal(err)
	}
	lines := strings.Count(csvBuf.String(), "\n")
	if lines != 100_001 || !strings.HasPrefix(csvBuf.String(), "n,word,ratio\n99999,row 99999,") {
		t.Errorf("csv has %d lines, starts %.50q", lines, csvBuf.String())
	}
	var jsonBuf bytes.Buffer
	if err := m.Export().WriteJSON(t.Context(), &jsonBuf); err != nil {
		t.Fatal(err)
	}
	if !json.Valid(jsonBuf.Bytes()) || strings.Count(jsonBuf.String(), `"word"`) != 100_000 {
		t.Error("the JSON of 100,000 rows is not valid, or is not whole")
	}
}

func TestAnExportThatIsCancelledOrCannotWrite(t *testing.T) {
	m := NewModel(ModelOptions{})
	m.SetSource(numbered(5000))
	cancelled, cancel := context.WithCancel(t.Context())
	cancel()
	err := m.Export().WriteCSV(cancelled, io.Discard, CSVOptions{})
	wantRoute(t, err, "Export.WriteCSV.ResultExportCancelled")
	if !errors.Is(err, context.Canceled) {
		t.Error("the cause of the cancellation is lost")
	}
	wantRoute(t, m.Export().WriteJSON(cancelled, io.Discard), "Export.WriteJSON.ResultExportCancelled")

	for _, limit := range []int{0, 5, 100, 4000} {
		w := &failingWriter{limit: limit}
		wantRoute(t, m.Export().WriteCSV(t.Context(), w, CSVOptions{}), "Export.WriteCSV.ResultExportWriteFailed")
		w = &failingWriter{limit: limit}
		wantRoute(t, m.Export().WriteJSON(t.Context(), w), "Export.WriteJSON.func1.ResultExportWriteFailed")
	}
}

// failingWriter accepts limit bytes and then fails.
type failingWriter struct{ limit, n int }

func (w *failingWriter) Write(p []byte) (int, error) {
	if w.n+len(p) > w.limit {
		return 0, errors.New("DiskFull", "no space left")
	}
	w.n += len(p)
	return len(p), nil
}

func TestAnExportDoesNotFollowTheModelAfterwards(t *testing.T) {
	tbl := people()
	m := NewModel(ModelOptions{})
	m.SetSource(tbl)
	m.SortBy(0, SortAsc)
	ex := m.Export()
	m.SortBy(0, SortDesc)
	tbl.Append([]Value{Int(4242)})
	m.Appended()
	var buf bytes.Buffer
	if err := ex.WriteCSV(t.Context(), &buf, CSVOptions{NoHeader: true}); err != nil {
		t.Fatal(err)
	}
	if ex.Rows() != 4 || !strings.HasPrefix(buf.String(), "1,Alice") || strings.Contains(buf.String(), "4242") {
		t.Errorf("rows %d, output %q", ex.Rows(), buf.String())
	}
}
