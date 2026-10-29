package resultgrid

import (
	"strconv"
	"testing"
	"time"

	"github.com/rm4n0s/errors"
)

// wantRoute checks that err is a tagged error whose route contains want.
func wantRoute(t *testing.T, err error, want string) {
	t.Helper()
	if err == nil {
		t.Fatalf("no error, want the route %s", want)
	}
	appErr, ok := errors.FromError(err)
	if !ok {
		t.Fatalf("expected *errors.Error, got %T: %v", err, err)
	}
	if !appErr.HasRoute(want) {
		t.Errorf("unexpected failure path: %s (want %s)", appErr.Route(), want)
	}
}

// people is a small table of every kind of value.
func people() *Table {
	t := NewTable([]Column{{Name: "id", Type: "integer"}, {Name: "name", Type: "text"}, {Name: "score", Type: "double"}, {Name: "born", Type: "date"}, {Name: "note"}})
	day := func(y int, m time.Month, d int) Value { return Time(time.Date(y, m, d, 0, 0, 0, 0, time.UTC)) }
	t.Append(
		[]Value{Int(3), Text("carol"), Float(7.5), day(1990, 5, 17), Null()},
		[]Value{Int(1), Text("Alice"), Float(9.25), day(1985, 1, 2), Text("line one\nline two")},
		[]Value{Int(2), Text("bob"), Null(), day(2001, 12, 31), Text("tab\there")},
		[]Value{Int(10), Text("Bob"), Float(-1), Null(), Text(`say "hi"`)},
	)
	return t
}

// numbered is a lazy source of n rows of three columns.
func numbered(n int) *FuncSource {
	return NewFuncSource([]Column{{Name: "n", Type: "bigint"}, {Name: "word"}, {Name: "ratio"}}, n, func(i int, dst []Value) {
		dst[0] = Int(int64(i))
		dst[1] = Text("row " + strconv.Itoa(i))
		dst[2] = Float(float64(i) / 7)
	})
}

// column reads the display column c of the model as text.
func column(m *Model, c int) []string {
	out := make([]string, m.Len())
	for r := range out {
		out[r] = m.CopyCell(r, c)
	}
	return out
}
