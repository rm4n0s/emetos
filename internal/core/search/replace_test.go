package search

import (
	"context"
	"os"
	"path/filepath"
	"testing"
)

func TestReplacer(t *testing.T) {
	tests := []struct {
		name         string
		q            Query
		replacement  string
		preserveCase bool
		match, want  string
	}{
		{"plain", Query{Pattern: "foo"}, "bar", false, "foo", "bar"},
		{"plain keeps backslashes and dollars", Query{Pattern: "foo"}, `a\nb$1`, false, "foo", `a\nb$1`},
		{"preserve case: capital first", Query{Pattern: "foo"}, "bar", true, "Foo", "Bar"},
		{"preserve case: all capitals", Query{Pattern: "foo"}, "bar", true, "FOO", "BAR"},
		{"preserve case: lower", Query{Pattern: "foo"}, "Bar", true, "foo", "bar"},
		{"preserve case: with underscores", Query{Pattern: "foo_bar"}, "xyz_abc", true, "Foo_Bar", "Xyz_Abc"},
		{"preserve case: with dashes", Query{Pattern: "foo-bar"}, "xyz-abc", true, "FOO-BAR", "XYZ-ABC"},
		{"preserve case off", Query{Pattern: "foo"}, "bar", false, "FOO", "bar"},
		{"groups", Query{Pattern: `(\w+)@(\w+)`, IsRegExp: true}, "$2 at $1", false, "me@host", "host at me"},
		{"whole match", Query{Pattern: `\d+`, IsRegExp: true}, "<$&>", false, "123", "<123>"},
		{"group zero", Query{Pattern: `\d+`, IsRegExp: true}, "[$0]", false, "123", "[123]"},
		{"line break in a regular expression", Query{Pattern: `a(b)`, IsRegExp: true}, `$1\n$1\t.`, false, "ab", "b\nb\t."},
		{"case operations", Query{Pattern: `(\w)(\w+)`, IsRegExp: true}, `\u$1\U$2`, false, "word", "WORD"},
		{"case operation on one letter", Query{Pattern: `(\w)(\w+)`, IsRegExp: true}, `\u$1$2`, false, "word", "Word"},
		{"unmatched group is empty", Query{Pattern: `a(x)?b`, IsRegExp: true}, "[$1]", false, "ab", "[]"},
		{"whole word wrapped expression", Query{Pattern: `(f)oo`, IsRegExp: true, IsWordMatch: true}, "$1", false, "foo", "f"},
		{"a lookbehind finds no groups in the match alone", Query{Pattern: `(?<=x)(a)`, IsRegExp: true}, "<$1>", false, "a", "<>"},
		{"preserve case with groups", Query{Pattern: `(f)oo`, IsRegExp: true}, "bar", true, "Foo", "Bar"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			r, err := NewReplacer(tt.q, tt.replacement, tt.preserveCase)
			if err != nil {
				t.Fatal(err)
			}
			if got := r.Replace(tt.match); got != tt.want {
				t.Errorf("got %q, want %q", got, tt.want)
			}
		})
	}
}

func TestApplyEdits(t *testing.T) {
	text := "one two\nthree four\nfive\n"
	tests := []struct {
		name        string
		edits       []Edit
		want        string
		wantSkipped []int
	}{
		{"one", []Edit{{Line: 0, Col: 4, EndLine: 0, EndCol: 7, Expected: "two", Text: "2"}}, "one 2\nthree four\nfive\n", nil},
		{"two on a line, out of order", []Edit{
			{Line: 1, Col: 6, EndLine: 1, EndCol: 10, Expected: "four", Text: "4"},
			{Line: 1, Col: 0, EndLine: 1, EndCol: 5, Expected: "three", Text: "3"},
		}, "one two\n3 4\nfive\n", nil},
		{"over lines", []Edit{{Line: 0, Col: 4, EndLine: 1, EndCol: 5, Expected: "two\nthree", Text: "x"}}, "one x four\nfive\n", nil},
		{"a CRLF in the expected text", []Edit{{Line: 0, Col: 4, EndLine: 1, EndCol: 5, Expected: "two\r\nthree", Text: "x"}}, "one x four\nfive\n", nil},
		{"stale text is skipped", []Edit{
			{Line: 0, Col: 4, EndLine: 0, EndCol: 7, Expected: "TWO", Text: "2"},
			{Line: 2, Col: 0, EndLine: 2, EndCol: 4, Expected: "five", Text: "5"},
		}, "one two\nthree four\n5\n", []int{0}},
		{"a range outside the text is skipped", []Edit{{Line: 9, Col: 0, EndLine: 9, EndCol: 1, Expected: "x", Text: "y"}, {Line: 0, Col: 40, EndLine: 0, EndCol: 41, Expected: "x", Text: "y"}}, text, []int{0, 1}},
		{"an overlapping edit is skipped", []Edit{
			{Line: 0, Col: 0, EndLine: 0, EndCol: 5, Expected: "one t", Text: "A"},
			{Line: 0, Col: 4, EndLine: 0, EndCol: 7, Expected: "two", Text: "B"},
		}, "Awo\nthree four\nfive\n", []int{1}},
		{"a deletion", []Edit{{Line: 0, Col: 3, EndLine: 0, EndCol: 7, Expected: " two", Text: ""}}, "one\nthree four\nfive\n", nil},
		{"none", nil, text, nil},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, skipped := ApplyEdits(text, tt.edits)
			if got != tt.want {
				t.Errorf("text %q, want %q", got, tt.want)
			}
			if !equalInts(skipped, tt.wantSkipped) {
				t.Errorf("skipped %v, want %v", skipped, tt.wantSkipped)
			}
		})
	}
}

func equalInts(a, b []int) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

func TestReplaceInFile(t *testing.T) {
	edit := Edit{Line: 1, Col: 0, EndLine: 1, EndCol: 3, Expected: "two", Text: "2"}
	tests := []struct {
		name        string
		content     string
		encoding    string
		edits       []Edit
		want        string
		applied     int
		skipped     int
		routeSuffix string
	}{
		{"lf", "one\ntwo\n", "", []Edit{edit}, "one\n2\n", 1, 0, ""},
		{"crlf is kept", "one\r\ntwo\r\n", "", []Edit{edit}, "one\r\n2\r\n", 1, 0, ""},
		{"a byte order mark is kept", "\xef\xbb\xbfone\ntwo\n", "", []Edit{edit}, "\xef\xbb\xbfone\n2\n", 1, 0, ""},
		{"utf16 with a mark is kept", "\xff\xfeo\x00n\x00e\x00\n\x00t\x00w\x00o\x00\n\x00", "", []Edit{edit}, "\xff\xfeo\x00n\x00e\x00\n\x002\x00\n\x00", 1, 0, ""},
		{"a configured encoding", "caf\xe9\ntwo\n", "windows1252", []Edit{edit}, "caf\xe9\n2\n", 1, 0, ""},
		{"stale edit leaves the file alone", "one\nTWO\n", "", []Edit{edit}, "one\nTWO\n", 0, 1, ""},
		{"one stale and one good", "one\ntwo\n", "", []Edit{edit, {Line: 0, Col: 0, EndLine: 0, EndCol: 3, Expected: "xxx", Text: "y"}}, "one\n2\n", 1, 1, ""},
		{"a binary file", "one\n\x00two\n", "", []Edit{edit}, "", 0, 0, "ReplaceInFile->fileio.Read.FileBinary"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "f.txt")
			if err := os.WriteFile(path, []byte(tt.content), 0o640); err != nil {
				t.Fatal(err)
			}
			res, err := ReplaceInFile(context.Background(), path, tt.encoding, tt.edits)
			if tt.routeSuffix != "" {
				if appErr := asError(t, err); !appErr.HasRoute("Read.FileBinary") {
					t.Errorf("route %s", appErr.Route())
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			if res.Applied != tt.applied || res.Skipped != tt.skipped {
				t.Errorf("result %+v, want applied %d skipped %d", res, tt.applied, tt.skipped)
			}
			got, err := os.ReadFile(path)
			if err != nil {
				t.Fatal(err)
			}
			if string(got) != tt.want {
				t.Errorf("file %q, want %q", got, tt.want)
			}
			if info, err := os.Stat(path); err != nil || info.Mode().Perm() != 0o640 {
				t.Errorf("mode %v %v", info, err)
			}
		})
	}
	t.Run("a file that is not there", func(t *testing.T) {
		_, err := ReplaceInFile(context.Background(), filepath.Join(t.TempDir(), "missing"), "", []Edit{edit})
		if appErr := asError(t, err); !appErr.HasRoute("readFile->checkRegular.FileNotFound") {
			t.Errorf("route %s", appErr.Route())
		}
	})
	t.Run("a cancelled context", func(t *testing.T) {
		path := filepath.Join(t.TempDir(), "f.txt")
		if err := os.WriteFile(path, []byte("one\ntwo\n"), 0o644); err != nil {
			t.Fatal(err)
		}
		ctx, cancel := context.WithCancel(context.Background())
		cancel()
		_, err := ReplaceInFile(ctx, path, "", []Edit{edit})
		if appErr := asError(t, err); !appErr.HasRoute("Read.ReadCancelled") {
			t.Errorf("route %s", appErr.Route())
		}
	})
}
