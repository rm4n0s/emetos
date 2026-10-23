package jsonc

import (
	"testing"

	"github.com/rm4n0s/errors"
)

var editOpts = FormattingOptions{InsertSpaces: true, TabSize: 2, EOL: "\n"}

// assertEdit applies edits as VS Code's test does, checking they are ordered and in range.
func assertEdit(t *testing.T, content string, edits []Edit, expected string) {
	t.Helper()
	last := len(content)
	for i := len(edits) - 1; i >= 0; i-- {
		e := edits[i]
		if e.Offset < 0 || e.Length < 0 || e.Offset+e.Length > len(content) || last < e.Offset+e.Length {
			t.Fatalf("edit %+v out of order or range in %q", e, content)
		}
		last = e.Offset
		content = content[:e.Offset] + e.Content + content[e.Offset+e.Length:]
	}
	if content != expected {
		t.Errorf("got\n%q\nwant\n%q", content, expected)
	}
}

func set(t *testing.T, content string, path Path, value any) []Edit {
	t.Helper()
	edits, err := SetValue(content, path, value, editOpts)
	if err != nil {
		t.Fatal(err)
	}
	return edits
}

func setAt(t *testing.T, content string, path Path, value any, index int) []Edit {
	t.Helper()
	edits, err := SetValueAt(content, path, value, editOpts, func([]string) int { return index })
	if err != nil {
		t.Fatal(err)
	}
	return edits
}

func remove(t *testing.T, content string, path Path) []Edit {
	t.Helper()
	edits, err := Remove(content, path, editOpts)
	if err != nil {
		t.Fatal(err)
	}
	return edits
}

// Ported from VS Code: src/vs/base/test/common/jsonEdit.test.ts. Removing an array element is
// Remove here, where VS Code sets it to undefined.
func TestVSCodeJSONEdit(t *testing.T) {
	t.Run("set property", func(t *testing.T) {
		c := "{\n  \"x\": \"y\"\n}"
		assertEdit(t, c, set(t, c, Path{"x"}, "bar"), "{\n  \"x\": \"bar\"\n}")
		c = "true"
		assertEdit(t, c, set(t, c, Path{}, "bar"), `"bar"`)
		c = "{\n  \"x\": \"y\"\n}"
		assertEdit(t, c, set(t, c, Path{"x"}, map[string]any{"key": true}), "{\n  \"x\": {\n    \"key\": true\n  }\n}")
		c = "{\n  \"a\": \"b\",  \"x\": \"y\"\n}"
		assertEdit(t, c, set(t, c, Path{"a"}, nil), "{\n  \"a\": null,  \"x\": \"y\"\n}")
	})
	t.Run("insert property", func(t *testing.T) {
		c := "{}"
		assertEdit(t, c, set(t, c, Path{"foo"}, "bar"), "{\n  \"foo\": \"bar\"\n}")
		assertEdit(t, c, set(t, c, Path{"foo", "foo2"}, "bar"), "{\n  \"foo\": {\n    \"foo2\": \"bar\"\n  }\n}")
		c = "{\n}"
		assertEdit(t, c, set(t, c, Path{"foo"}, "bar"), "{\n  \"foo\": \"bar\"\n}")
		c = "  {\n  }"
		assertEdit(t, c, set(t, c, Path{"foo"}, "bar"), "  {\n    \"foo\": \"bar\"\n  }")
		c = "{\n  \"x\": \"y\"\n}"
		assertEdit(t, c, set(t, c, Path{"foo"}, "bar"), "{\n  \"x\": \"y\",\n  \"foo\": \"bar\"\n}")
		c = "{\n  \"x\": \"y\"\n}"
		assertEdit(t, c, set(t, c, Path{"e"}, "null"), "{\n  \"x\": \"y\",\n  \"e\": \"null\"\n}")
		assertEdit(t, c, set(t, c, Path{"x"}, "bar"), "{\n  \"x\": \"bar\"\n}")
		c = "{\n  \"x\": {\n    \"a\": 1,\n    \"b\": true\n  }\n}\n"
		assertEdit(t, c, set(t, c, Path{"x"}, "bar"), "{\n  \"x\": \"bar\"\n}\n")
		assertEdit(t, c, set(t, c, Path{"x", "b"}, "bar"), "{\n  \"x\": {\n    \"a\": 1,\n    \"b\": \"bar\"\n  }\n}\n")
		assertEdit(t, c, setAt(t, c, Path{"x", "c"}, "bar", 0), "{\n  \"x\": {\n    \"c\": \"bar\",\n    \"a\": 1,\n    \"b\": true\n  }\n}\n")
		assertEdit(t, c, setAt(t, c, Path{"x", "c"}, "bar", 1), "{\n  \"x\": {\n    \"a\": 1,\n    \"c\": \"bar\",\n    \"b\": true\n  }\n}\n")
		assertEdit(t, c, setAt(t, c, Path{"x", "c"}, "bar", 2), "{\n  \"x\": {\n    \"a\": 1,\n    \"b\": true,\n    \"c\": \"bar\"\n  }\n}\n")
		assertEdit(t, c, set(t, c, Path{"c"}, "bar"), "{\n  \"x\": {\n    \"a\": 1,\n    \"b\": true\n  },\n  \"c\": \"bar\"\n}\n")
		c = "{\n  \"a\": [\n    {\n    } \n  ]  \n}"
		assertEdit(t, c, set(t, c, Path{"foo"}, "bar"), "{\n  \"a\": [\n    {\n    } \n  ],\n  \"foo\": \"bar\"\n}")
		c = ""
		assertEdit(t, c, set(t, c, Path{"foo", 0}, "bar"), "{\n  \"foo\": [\n    \"bar\"\n  ]\n}")
		c = "//comment"
		assertEdit(t, c, set(t, c, Path{"foo", 0}, "bar"), "{\n  \"foo\": [\n    \"bar\"\n  ]\n} //comment")
	})
	t.Run("remove property", func(t *testing.T) {
		c := "{\n  \"x\": \"y\"\n}"
		assertEdit(t, c, remove(t, c, Path{"x"}), "{\n}")
		c = "{\n  \"x\": \"y\", \"a\": []\n}"
		assertEdit(t, c, remove(t, c, Path{"x"}), "{\n  \"a\": []\n}")
		c = "{\n  \"x\": \"y\", \"a\": []\n}"
		assertEdit(t, c, remove(t, c, Path{"a"}), "{\n  \"x\": \"y\"\n}")
	})
	t.Run("remove only property if ends with comma", func(t *testing.T) {
		c := "{\n  \"x\": \"y\",\n}"
		assertEdit(t, c, remove(t, c, Path{"x"}), "{\n}")
		c = "{\n  \"x\": \"y\" /* x */, // trailing\n}"
		assertEdit(t, c, remove(t, c, Path{"x"}), "{ // trailing\n}")
		c = "{\n  \"s\": {\n    \"x\": \"y\",\n  }\n}"
		assertEdit(t, c, remove(t, c, Path{"s", "x"}), "{\n  \"s\": {\n  }\n}")
	})
	t.Run("arrays", func(t *testing.T) {
		cases := []struct {
			name, content string
			path          Path
			value         any
			remove        bool
			want          string
		}{
			{"insert item at 0", "[\n  2,\n  3\n]", Path{0}, 1, false, "[\n  1,\n  2,\n  3\n]"},
			{"insert item at 0 in empty array", "[\n]", Path{0}, 1, false, "[\n  1\n]"},
			{"insert item at an index", "[\n  1,\n  3\n]", Path{1}, 2, false, "[\n  1,\n  2,\n  3\n]"},
			{"insert item at an index in empty array", "[\n]", Path{1}, 1, false, "[\n  1\n]"},
			{"insert item at end index", "[\n  1,\n  2\n]", Path{2}, 3, false, "[\n  1,\n  2,\n  3\n]"},
			{"insert item at end to empty array", "[\n]", Path{-1}, "bar", false, "[\n  \"bar\"\n]"},
			{"insert item at end", "[\n  1,\n  2\n]", Path{-1}, "bar", false, "[\n  1,\n  2,\n  \"bar\"\n]"},
			{"remove item in array with one item", "[\n  1\n]", Path{0}, nil, true, "[]"},
			{"remove item in the middle of the array", "[\n  1,\n  2,\n  3\n]", Path{1}, nil, true, "[\n  1,\n  3\n]"},
			{"remove last item in the array", "[\n  1,\n  2,\n  \"bar\"\n]", Path{2}, nil, true, "[\n  1,\n  2\n]"},
			{"remove last item in the array if ends with comma", "[\n  1,\n  \"foo\",\n  \"bar\",\n]", Path{2}, nil, true, "[\n  1,\n  \"foo\"\n]"},
			{"remove last item if there is a comment in the beginning", "// This is a comment\n[\n  1,\n  \"foo\",\n  \"bar\"\n]", Path{2}, nil, true, "// This is a comment\n[\n  1,\n  \"foo\"\n]"},
			{"remove last item in compact array without whitespace", "[1,2]", Path{1}, nil, true, "[\n  1\n]"},
		}
		for _, c := range cases {
			t.Run(c.name, func(t *testing.T) {
				if c.remove {
					assertEdit(t, c.content, remove(t, c.content, c.path), c.want)
				} else {
					assertEdit(t, c.content, set(t, c.content, c.path, c.value), c.want)
				}
			})
		}
	})
}

func TestSetValueMore(t *testing.T) {
	t.Run("tabs and the line ending of the text", func(t *testing.T) {
		c := "{\r\n\t\"a\": 1\r\n}"
		edits, err := SetValue(c, Path{"b"}, []any{1, "x"}, FormattingOptions{})
		if err != nil {
			t.Fatal(err)
		}
		assertEdit(t, c, edits, "{\r\n\t\"a\": 1,\r\n\t\"b\": [\r\n\t\t1,\r\n\t\t\"x\"\r\n\t]\r\n}")
	})
	t.Run("comments are kept", func(t *testing.T) {
		c := "{\n  // the size\n  \"size\": 12, // pixels\n  \"name\": \"a\"\n}"
		assertEdit(t, c, set(t, c, Path{"size"}, 14), "{\n  // the size\n  \"size\": 14, // pixels\n  \"name\": \"a\"\n}")
		// What stands between the brace and the next property goes with the first one.
		assertEdit(t, c, remove(t, c, Path{"size"}), "{\n  \"name\": \"a\"\n}")
	})
	t.Run("a value is written as encoding/json writes it, without HTML escapes", func(t *testing.T) {
		c := "{}"
		assertEdit(t, c, set(t, c, Path{"a"}, "<b>&"), "{\n  \"a\": \"<b>&\"\n}")
	})
	t.Run("missing parents are created, arrays too", func(t *testing.T) {
		c := "{\n  \"a\": {}\n}"
		assertEdit(t, c, set(t, c, Path{"a", "b", 0, "c"}, 1), "{\n  \"a\": {\n    \"b\": [\n      {\n        \"c\": 1\n      }\n    ]\n  }\n}")
	})
	t.Run("an index past the end appends", func(t *testing.T) {
		c := "[1]"
		assertEdit(t, c, set(t, c, Path{5}, 2), "[\n  1,\n  2\n]")
	})
	t.Run("the insertion index is kept inside the object", func(t *testing.T) {
		c := "{\n  \"a\": 1\n}"
		assertEdit(t, c, setAt(t, c, Path{"b"}, 2, 99), "{\n  \"a\": 1,\n  \"b\": 2\n}")
		assertEdit(t, c, setAt(t, c, Path{"b"}, 2, -4), "{\n  \"b\": 2,\n  \"a\": 1\n}")
		var names []string
		_, err := SetValueAt(c, Path{"b"}, 2, editOpts, func(n []string) int { names = n; return 0 })
		if err != nil || len(names) != 1 || names[0] != "a" {
			t.Errorf("names %v, %v", names, err)
		}
	})
	t.Run("removing what is not there changes nothing", func(t *testing.T) {
		c := "{\n  \"a\": [1]\n}"
		for _, p := range []Path{{"b"}, {"a", 3}, {"a", -1}, {"x", "y", 0}, {}} {
			if edits := remove(t, c, p); len(edits) != 0 {
				t.Errorf("%v: edits %+v", p, edits)
			}
		}
		if edits := remove(t, "", Path{"a"}); len(edits) != 0 {
			t.Errorf("empty text: %+v", edits)
		}
	})
	t.Run("the first property of several", func(t *testing.T) {
		c := "{\n  \"a\": 1,\n  \"b\": 2\n}"
		assertEdit(t, c, remove(t, c, Path{"a"}), "{\n  \"b\": 2\n}")
	})
	t.Run("a document that is a value but not an object", func(t *testing.T) {
		c := "[1, 2]"
		assertEdit(t, c, set(t, c, Path{}, map[string]any{"a": 1}), "{\n  \"a\": 1\n}")
	})
}

func TestSetValueErrors(t *testing.T) {
	cases := []struct {
		name, content string
		path          Path
		value         any
		route         string
	}{
		{"a name on an array", "[1]", Path{"a"}, 1, "setValue.JSONCCannotSet"},
		{"an index on an object", `{"a": 1}`, Path{3}, 1, "setValue.JSONCCannotSet"},
		{"a name on a string", `{"a": "s"}`, Path{"a", "b"}, 1, "setValue.JSONCCannotSet"},
		{"an index on a number", `{"a": 5}`, Path{"a", 0}, 1, "setValue.JSONCCannotSet"},
		{"a negative index other than -1 before an array start", `[1]`, Path{-7}, 1, "setInArray.JSONCCannotSet"},
		{"a value that cannot be written", `{}`, Path{"a"}, make(chan int), "stringify.JSONCEncodeFailed"},
		{"a value that cannot be written at the root", `1`, Path{}, make(chan int), "stringify.JSONCEncodeFailed"},
		{"a value that cannot be written over a value", `{"a": 1}`, Path{"a"}, make(chan int), "stringify.JSONCEncodeFailed"},
		{"a value that cannot be written in an array", `[]`, Path{0}, make(chan int), "stringify.JSONCEncodeFailed"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			_, err := SetValue(c.content, c.path, c.value, editOpts)
			appErr, ok := errors.FromError(err)
			if !ok {
				t.Fatalf("expected *errors.Error, got %T", err)
			}
			if !appErr.HasRoute(c.route) {
				t.Errorf("unexpected failure path: %s", appErr.Route())
			}
		})
	}
	t.Run("the property name cannot fail to encode", func(t *testing.T) {
		if _, err := SetValue("{}", Path{"\xff"}, 1, editOpts); err != nil {
			t.Error(err)
		}
	})
}

func TestApplyEdits(t *testing.T) {
	t.Run("edits in any order, offsets in the original text", func(t *testing.T) {
		got, err := ApplyEdits("0123456789", []Edit{{Offset: 8, Length: 1, Content: "X"}, {Offset: 0, Length: 2, Content: ""}, {Offset: 4, Length: 0, Content: "--"}})
		if err != nil || got != "23--4567X9" {
			t.Errorf("got %q, %v", got, err)
		}
	})
	t.Run("an insert and a replacement at the same place", func(t *testing.T) {
		got, err := ApplyEdits("abc", []Edit{{Offset: 1, Length: 1, Content: "X"}, {Offset: 1, Length: 0, Content: "-"}})
		if err != nil || got != "a-Xc" {
			t.Errorf("got %q, %v", got, err)
		}
	})
	t.Run("the edits are not changed", func(t *testing.T) {
		edits := []Edit{{Offset: 3, Length: 0, Content: "b"}, {Offset: 0, Length: 0, Content: "a"}}
		if _, err := ApplyEdits("xyz", edits); err != nil || edits[0].Offset != 3 {
			t.Errorf("edits %+v, %v", edits, err)
		}
	})
	failures := []struct {
		name  string
		text  string
		edits []Edit
		route string
	}{
		{"overlap", "0123456789", []Edit{{Offset: 1, Length: 4, Content: ""}, {Offset: 3, Length: 2, Content: ""}}, "ApplyEdits.JSONCEditsOverlap"},
		{"past the end", "abc", []Edit{{Offset: 2, Length: 5}}, "ApplyEdits.JSONCEditOutOfRange"},
		{"before the start", "abc", []Edit{{Offset: -1, Length: 1}}, "ApplyEdits.JSONCEditOutOfRange"},
		{"negative length", "abc", []Edit{{Offset: 1, Length: -1}}, "ApplyEdits.JSONCEditOutOfRange"},
	}
	for _, c := range failures {
		t.Run(c.name, func(t *testing.T) {
			_, err := ApplyEdits(c.text, c.edits)
			appErr, ok := errors.FromError(err)
			if !ok {
				t.Fatalf("expected *errors.Error, got %T", err)
			}
			if !appErr.HasRoute(c.route) {
				t.Errorf("unexpected failure path: %s", appErr.Route())
			}
		})
	}
}

// FuzzSetValue checks that editing never panics, that the edit it gives applies, and that
// the result is valid JSON-with-comments holding the value, when the text was valid.
func FuzzSetValue(f *testing.F) {
	for _, s := range []string{`{}`, `[]`, `{"a": [1, {"b": 2}], "c": 3}`, "// c\n{\"a\": 1, // x\n}", `[1,2,3]`, `{"a": `, ``, `[`} {
		f.Add(s, "a", 0, true)
		f.Add(s, "b", 1, false)
	}
	f.Fuzz(func(t *testing.T, text, name string, index int, byName bool) {
		var path Path
		if byName {
			path = Path{"a", name}
		} else {
			path = Path{"a", index % 5}
		}
		for _, remove := range []bool{false, true} {
			var edits []Edit
			var err error
			if remove {
				edits, err = Remove(text, path, editOpts)
			} else {
				edits, err = SetValue(text, path, 42, editOpts)
			}
			if err != nil {
				continue
			}
			out, err := ApplyEdits(text, edits)
			if err != nil {
				t.Fatalf("edits %+v of %q do not apply: %v", edits, text, err)
			}
			_, before := ParseTree(text, ParseOptions{})
			if len(before) != 0 || remove {
				continue
			}
			tree, after := ParseTree(out, ParseOptions{})
			if len(after) != 0 {
				t.Fatalf("%q with %v set is %q, which has errors %+v", text, path, out, after)
			}
			if !hasValue(tree, 42.0) {
				t.Fatalf("%q with %v set is %q, without the value", text, path, out)
			}
		}
	})
}

// hasValue tells whether a literal with the value is in the tree.
func hasValue(n *Node, value any) bool {
	if n == nil {
		return false
	}
	if len(n.Children) == 0 {
		return n.Value == value
	}
	for _, c := range n.Children {
		if hasValue(c, value) {
			return true
		}
	}
	return false
}
