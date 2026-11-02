package debug_test

import (
	"reflect"
	"strings"
	"testing"

	"github.com/rm4n0s/emetos/internal/core/debug"
)

// source is a small program; words are its identifiers, found with a plain split (the editor uses its tokens).
var source = []string{
	"func main() {",
	"\tprices := []float64{1, 2, 3}",
	"\tsum := 0.0",
	"\tfor i, p := range prices {",
	"\t\tsum += p",
	"\t}",
	"\tfmt.Println(sum) // sum is printed",
	"}",
}

func wordsOfSource(line int) []string {
	if line < 1 || line > len(source) {
		return nil
	}
	text := source[line-1]
	if i := strings.Index(text, "//"); i >= 0 {
		text = text[:i]
	}
	return strings.FieldsFunc(text, func(r rune) bool {
		return r != '_' && (r < '0' || r > '9') && (r < 'a' || r > 'z') && (r < 'A' || r > 'Z')
	})
}

func contentOfSource(line int) string { return source[line-1] }

func TestInlineValues(t *testing.T) {
	locals := debug.InlineScope{Variables: []debug.InlineVariable{{"prices", "[]float64 len:3"}, {"sum", "6"}, {"i", "2"}, {"p", "3"}}}
	tests := []struct {
		name        string
		scopes      []debug.InlineScope
		stopped     int
		first, last int
		want        []debug.InlineLine
	}{
		{
			name: "the lines before the stopped line, in the order of the names", scopes: []debug.InlineScope{locals}, stopped: 5, first: 1, last: 8,
			want: []debug.InlineLine{
				{Line: 2, Values: []debug.InlineVariable{{"prices", "[]float64 len:3"}}},
				{Line: 3, Values: []debug.InlineVariable{{"sum", "6"}}},
				{Line: 4, Values: []debug.InlineVariable{{"i", "2"}, {"p", "3"}, {"prices", "[]float64 len:3"}}},
				{Line: 5, Values: []debug.InlineVariable{{"sum", "6"}, {"p", "3"}}},
			},
		},
		{
			name: "only the lines the editor shows", scopes: []debug.InlineScope{locals}, stopped: 5, first: 3, last: 4,
			want: []debug.InlineLine{
				{Line: 3, Values: []debug.InlineVariable{{"sum", "6"}}},
				{Line: 4, Values: []debug.InlineVariable{{"i", "2"}, {"p", "3"}, {"prices", "[]float64 len:3"}}},
			},
		},
		{
			name: "a scope starts at its first line", scopes: []debug.InlineScope{{Variables: []debug.InlineVariable{{"p", "3"}}, FirstLine: 5}, {Variables: []debug.InlineVariable{{"sum", "6"}}}},
			stopped: 5, first: 1, last: 8,
			want: []debug.InlineLine{
				{Line: 3, Values: []debug.InlineVariable{{"sum", "6"}}},
				{Line: 5, Values: []debug.InlineVariable{{"sum", "6"}, {"p", "3"}}},
			},
		},
		{
			name:    "the narrowest scope wins for a name in two",
			scopes:  []debug.InlineScope{{Variables: []debug.InlineVariable{{"sum", "inner"}}}, {Variables: []debug.InlineVariable{{"sum", "outer"}}}},
			stopped: 3, first: 1, last: 8,
			want: []debug.InlineLine{{Line: 3, Values: []debug.InlineVariable{{"sum", "inner"}}}},
		},
		{
			name: "a comment names nothing", scopes: []debug.InlineScope{{Variables: []debug.InlineVariable{{"printed", "x"}}}}, stopped: 7, first: 1, last: 8,
			want: []debug.InlineLine{},
		},
		{name: "a stop before the view", scopes: []debug.InlineScope{locals}, stopped: 1, first: 5, last: 8, want: []debug.InlineLine{}},
		{name: "no scopes", stopped: 5, first: 1, last: 8, want: []debug.InlineLine{}},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := debug.InlineValues(tc.scopes, tc.stopped, tc.first, tc.last, wordsOfSource, contentOfSource)
			if !reflect.DeepEqual(got, tc.want) {
				t.Errorf("got  %+v\nwant %+v", got, tc.want)
			}
		})
	}
}

func TestInlineValuesStopAtMaxInlineValues(t *testing.T) {
	var vars []debug.InlineVariable
	for i := range debug.MaxInlineValues + 20 {
		vars = append(vars, debug.InlineVariable{Name: "v" + string(rune('a'+i%26)) + string(rune('a'+i/26)), Value: "1"})
	}
	words := func(int) []string {
		var w []string
		for _, v := range vars {
			w = append(w, v.Name)
		}
		return w
	}
	got := debug.InlineValues([]debug.InlineScope{{Variables: vars}}, 1, 1, 1, words, func(int) string { return "" })
	if len(got) != 1 || len(got[0].Values) != debug.MaxInlineValues {
		t.Fatalf("got %d lines with %d values, want 1 line with %d", len(got), len(got[0].Values), debug.MaxInlineValues)
	}
}

func TestInlineText(t *testing.T) {
	tests := []struct {
		name      string
		values    []debug.InlineVariable
		max       int
		wantShown string
		wantFull  string
	}{
		{"one value", []debug.InlineVariable{{"a", "1"}}, 150, "a = 1", "a = 1"},
		{"two values", []debug.InlineVariable{{"a", "1"}, {"b", "x"}}, 150, "a = 1, b = x", "a = 1, b = x"},
		{"a value over lines", []debug.InlineVariable{{"s", "ab\ncd\tef"}}, 150, "s = ab cd ef", "s = ab\ncd\tef"},
		{"too long", []debug.InlineVariable{{"name", "0123456789"}}, 8, "name = 0...", "name = 0123456789"},
		{"letters count as one", []debug.InlineVariable{{"π", "文文文"}}, 5, "π = 文...", "π = 文文文"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			shown, full := debug.InlineText(tc.values, tc.max)
			if shown != tc.wantShown || full != tc.wantFull {
				t.Errorf("InlineText = %q, %q; want %q, %q", shown, full, tc.wantShown, tc.wantFull)
			}
		})
	}
}

func TestInlineHover(t *testing.T) {
	tests := []struct{ name, in, want string }{
		{"one value", "a = 1", "a = 1"},
		{"a comma with no pair", "a = 1,2", "a = 1,2"},
		{"two values", "a = 1, b = 2", "a = 1,\nb = 2"},
		{"a comma in a value", "a = [1, 2], b = 3", "a = [1, 2],\nb = 3"},
		{"a value over lines", "a = x\ny, b = 2", "a = x\n    y,\nb = 2"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if got := debug.InlineHover(tc.in); got != tc.want {
				t.Errorf("InlineHover(%q) = %q, want %q", tc.in, got, tc.want)
			}
		})
	}
}
