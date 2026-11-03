package jsonc

import (
	"strings"
	"testing"

	"github.com/rm4n0s/errors"
)

func TestVSCodeFormat(t *testing.T) {
	for _, c := range formatCases {
		t.Run(c.name, func(t *testing.T) {
			content := c.content
			var rng *Range
			start, end := strings.Index(content, "|"), strings.LastIndex(content, "|")
			if start != -1 && end != -1 {
				content = content[:start] + content[start+1:end] + content[end+1:]
				rng = &Range{Offset: start, Length: end - start}
			}
			edits := Format(content, rng, FormattingOptions{TabSize: 2, InsertSpaces: c.spaces, EOL: "\n"})
			// The edits are in order and inside the text.
			last := len(content)
			for i := len(edits) - 1; i >= 0; i-- {
				e := edits[i]
				if e.Offset < 0 || e.Length < 0 || e.Offset+e.Length > len(content) || last < e.Offset+e.Length {
					t.Fatalf("edit %+v out of order or range", e)
				}
				last = e.Offset
				content = content[:e.Offset] + e.Content + content[e.Offset+e.Length:]
			}
			if content != c.expected {
				t.Errorf("got\n%q\nwant\n%q", content, c.expected)
			}
		})
	}
}

func TestVSCodeToFormattedString(t *testing.T) {
	value := map[string]any{"a": map[string]any{"b": 1, "d": []any{"hello"}}}
	want := func(tab, eol string) string {
		return strings.Join([]string{"{", tab + `"a": {`, tab + tab + `"b": 1,`, tab + tab + `"d": [`, tab + tab + tab + `"hello"`, tab + tab + `]`, tab + `}`, "}"}, eol)
	}
	cases := []struct {
		opts FormattingOptions
		want string
	}{
		{FormattingOptions{InsertSpaces: true, TabSize: 2, EOL: "\n"}, want("  ", "\n")},
		{FormattingOptions{InsertSpaces: true, TabSize: 2, EOL: "\r\n"}, want("  ", "\r\n")},
		{FormattingOptions{EOL: "\r\n"}, want("\t", "\r\n")},
		{FormattingOptions{InsertSpaces: true}, want("    ", "\n")},
	}
	for _, c := range cases {
		got, err := ToFormattedString(value, c.opts)
		if err != nil || got != c.want {
			t.Errorf("%+v: %q, %v; want %q", c.opts, got, err, c.want)
		}
	}
	if got, _ := ToFormattedString("<&>", FormattingOptions{}); got != `"<&>"` {
		t.Errorf("HTML characters were escaped: %s", got)
	}
	_, err := ToFormattedString(make(chan int), FormattingOptions{})
	if appErr, ok := errors.FromError(err); !ok || !appErr.HasRoute("ToFormattedString.JSONCEncodeFailed") {
		t.Errorf("unexpected error %v", err)
	}
}

func TestFormatOptionsAndEdges(t *testing.T) {
	apply := func(text string, rng *Range, opts FormattingOptions) string {
		out, err := ApplyEdits(text, Format(text, rng, opts))
		if err != nil {
			t.Fatal(err)
		}
		return out
	}
	t.Run("the line ending of the text wins over the option", func(t *testing.T) {
		if got := apply("{\r\n\"a\":1}", nil, FormattingOptions{InsertSpaces: true, TabSize: 2, EOL: "\n"}); got != "{\r\n  \"a\": 1\r\n}" {
			t.Errorf("got %q", got)
		}
		if got := apply("{\r\"a\":1}", nil, FormattingOptions{InsertSpaces: true, TabSize: 2}); got != "{\r  \"a\": 1\r}" {
			t.Errorf("got %q", got)
		}
		if got := apply(`{"a":1}`, nil, FormattingOptions{InsertSpaces: true, TabSize: 2, EOL: "\r\n"}); got != "{\r\n  \"a\": 1\r\n}" {
			t.Errorf("got %q", got)
		}
	})
	t.Run("the tab size defaults to 4", func(t *testing.T) {
		if got := apply(`{"a":1}`, nil, FormattingOptions{InsertSpaces: true}); got != "{\n    \"a\": 1\n}" {
			t.Errorf("got %q", got)
		}
	})
	t.Run("text with errors is formatted up to the error", func(t *testing.T) {
		o := FormattingOptions{InsertSpaces: true, TabSize: 2}
		cases := map[string]string{
			`{"a": 1 ?? }`: "{\n  \"a\": 1 ??\n}",
			`{"a": "x`:     "{\n  \"a\": \"x",
			`{"a" 1}`:      "{\n  \"a\" 1\n}",
			`{"a":1,"b":}`: "{\n  \"a\": 1,\n  \"b\":\n}",
		}
		for text, want := range cases {
			if got := apply(text, nil, o); got != want {
				t.Errorf("%q: got %q, want %q", text, got, want)
			}
		}
	})
	t.Run("an empty text and a range outside the text change nothing", func(t *testing.T) {
		if edits := Format("", nil, FormattingOptions{}); len(edits) != 0 {
			t.Errorf("edits %+v", edits)
		}
		for _, r := range []*Range{{Offset: 100, Length: 5}, {Offset: -3, Length: 2}} {
			if got := apply(`{"a":1}`, r, FormattingOptions{}); got != `{"a":1}` {
				t.Errorf("%+v: got %q", *r, got)
			}
		}
	})
}
