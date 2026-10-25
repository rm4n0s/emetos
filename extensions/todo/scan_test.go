package todo

import (
	"reflect"
	"strings"
	"testing"
)

func TestScanText(t *testing.T) {
	tags := []string{"TODO", "FIXME"}
	tests := []struct {
		name string
		text string
		tags []string
		want []Item
	}{
		{"a line comment", "x := 1 // TODO: handle the error\n", tags, []Item{{Tag: "TODO", Message: "handle the error", Line: 1, Column: 11}}},
		{"a hash comment with an owner", "# FIXME(ann): crashes on empty input", tags, []Item{{Tag: "FIXME", Message: "crashes on empty input", Line: 1, Column: 3}}},
		{"a block comment on one line", "/* TODO fix this */ int x;", tags, []Item{{Tag: "TODO", Message: "fix this", Line: 1, Column: 4}}},
		{"the continuation of a block comment", "/*\n * FIXME - rename\n */", tags, []Item{{Tag: "FIXME", Message: "rename", Line: 2, Column: 4}}},
		{"an HTML comment", "<p><!-- TODO: add alt text --></p>", tags, []Item{{Tag: "TODO", Message: "add alt text", Line: 1, Column: 9}}},
		{"an SQL comment", "select 1; -- TODO later", tags, []Item{{Tag: "TODO", Message: "later", Line: 1, Column: 14}}},
		{"two lines", "// TODO one\ncode\n// FIXME two\n", tags, []Item{{Tag: "TODO", Message: "one", Line: 1, Column: 4}, {Tag: "FIXME", Message: "two", Line: 3, Column: 4}}},
		{"windows line endings", "// TODO one\r\n// TODO two\r\n", tags, []Item{{Tag: "TODO", Message: "one", Line: 1, Column: 4}, {Tag: "TODO", Message: "two", Line: 2, Column: 4}}},
		{"the column counts UTF-16 units", "é𝄞 // TODO x", tags, []Item{{Tag: "TODO", Message: "x", Line: 1, Column: 8}}},
		{"code that is not a comment", "const TODO = 1\nfmt.Println(\"TODO\")\n", tags, nil},
		{"a tag inside a word", "// TODOS are listed\n// MYTODO\n// TODO_ITEM", tags, nil},
		{"a lower case tag", "// todo: not a tag", tags, nil},
		{"no message", "// TODO", tags, []Item{{Tag: "TODO", Message: "", Line: 1, Column: 4}}},
		{"the first tag of a line wins", "// FIXME before TODO", tags, []Item{{Tag: "FIXME", Message: "before TODO", Line: 1, Column: 4}}},
		{"a tag the user chose", "// HACK: until v2\n// TODO: not listed", []string{"HACK"}, []Item{{Tag: "HACK", Message: "until v2", Line: 1, Column: 4}}},
		{"no tags", "// TODO x", nil, nil},
		{"a line that is too long", "// " + strings.Repeat("x", maxLine) + " TODO y", tags, nil},
		{"an empty text", "", tags, nil},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := scanText(tt.text, tt.tags); !reflect.DeepEqual(got, tt.want) {
				t.Errorf("scanText = %+v\nwant %+v", got, tt.want)
			}
		})
	}
}
