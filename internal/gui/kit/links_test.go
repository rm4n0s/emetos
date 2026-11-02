package kit

import (
	"reflect"
	"strings"
	"testing"
)

// Ported from VS Code: src/vs/workbench/contrib/debug/test/browser/linkDetector.test.ts (the cases for the POSIX paths; the cases of
// the DOM that follow from the parts are the owner's, and the Windows ones are skipped: Emetos targets the Linux desktop).
func TestDetectLinks(t *testing.T) {
	path := func(index int, value, p string, line, col int) LinkPart {
		return LinkPart{Kind: LinkPath, Value: value, Path: p, Line: line, Column: col, Index: index}
	}
	text := func(index int, value string) LinkPart { return LinkPart{Kind: LinkText, Value: value, Index: index} }
	web := func(index int, value string) LinkPart { return LinkPart{Kind: LinkWeb, Value: value, Index: index} }
	cases := []struct {
		name, in string
		want     []LinkPart
	}{
		{"no links", "I am a string", []LinkPart{text(0, "I am a string")}},
		{"a trailing newline", "I am a string\n", []LinkPart{text(0, "I am a string\n")}},
		{"a path with line and column", "/Users/foo/bar.js:12:34", []LinkPart{path(0, "/Users/foo/bar.js:12:34", "/Users/foo/bar.js", 12, 34)}},
		{"a path with a line", "/Users/foo/bar.js:12", []LinkPart{path(0, "/Users/foo/bar.js:12", "/Users/foo/bar.js", 12, 0)}},
		{"a path alone", "/Users/foo/bar.js", []LinkPart{path(0, "/Users/foo/bar.js", "/Users/foo/bar.js", 0, 0)}},
		{"a link with @ (#282635)", "(/home/a/node_modules/.pnpm/playwright-core@1.57.0/lib/errors.js:56:16)",
			[]LinkPart{text(0, "("), path(1, "/home/a/node_modules/.pnpm/playwright-core@1.57.0/lib/errors.js:56:16", "/home/a/node_modules/.pnpm/playwright-core@1.57.0/lib/errors.js", 56, 16), text(70, ")")}},
		{"a relative path is found (the owner decides)", "./foo/bar.js", []LinkPart{path(0, "./foo/bar.js", "./foo/bar.js", 0, 0)}},
		{"a link and text", "The link: /Users/foo/bar.js:12:34", []LinkPart{text(0, "The link: "), path(10, "/Users/foo/bar.js:12:34", "/Users/foo/bar.js", 12, 34)}},
		{"a C# stack trace line", "/Users/foo/bar.cs:line 6", []LinkPart{path(0, "/Users/foo/bar.cs:line 6", "/Users/foo/bar.cs", 6, 0)}},
		{"a C# stack trace line and column", "/Users/foo/bar.cs:line 6:10", []LinkPart{path(0, "/Users/foo/bar.cs:line 6:10", "/Users/foo/bar.cs", 6, 10)}},
		{"a web address", "see https://example.com/a/b?c=d#e for more",
			[]LinkPart{text(0, "see "), web(4, "https://example.com/a/b?c=d#e"), text(33, " for more")}},
		{"an address does not end in punctuation", "(https://example.com/a).", []LinkPart{text(0, "("), web(1, "https://example.com/a"), text(22, ").")}},
		{"www", "go to www.example.org now", []LinkPart{text(0, "go to "), web(6, "www.example.org"), text(21, " now")}},
		{"a file address", "file:///Users/foo/bar.js:12:34", []LinkPart{web(0, "file:///Users/foo/bar.js:12:34")}},
		{"a file address without a position", "file:///Users/foo/bar.js", []LinkPart{web(0, "file:///Users/foo/bar.js")}},
		{"a home path", "~/work/x.go:3", []LinkPart{path(0, "~/work/x.go:3", "~/work/x.go", 3, 0)}},
		{"a long line has no links", strings.Repeat("a", MaxLinkLineLength) + " /Users/foo/bar.js", nil},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := DetectLinks(tc.in)
			if tc.want == nil {
				if len(got) != 1 || got[0].Kind != LinkText {
					t.Fatalf("parts = %+v, want one text", got)
				}
				return
			}
			// the text between two links may include the space that the path expression took: compare the pieces that matter
			if !reflect.DeepEqual(got, tc.want) {
				t.Errorf("parts = %+v\nwant   %+v", got, tc.want)
			}
		})
	}
}

func TestDetectLinksSeveral(t *testing.T) {
	got := DetectLinks("Here is a link /Users/foo/bar.js:12:34 and here is another /Users/boo/far.js:56:78")
	var paths []string
	var lines []int
	for _, p := range got {
		if p.Kind == LinkPath {
			paths = append(paths, strings.TrimSpace(p.Path))
			lines = append(lines, p.Line)
		}
	}
	if !reflect.DeepEqual(paths, []string{"/Users/foo/bar.js", "/Users/boo/far.js"}) || !reflect.DeepEqual(lines, []int{12, 56}) {
		t.Errorf("paths = %q lines = %v (parts %+v)", paths, lines, got)
	}
	// the parts put together are the text
	var all strings.Builder
	for _, p := range got {
		all.WriteString(p.Value)
	}
	if all.String() != "Here is a link /Users/foo/bar.js:12:34 and here is another /Users/boo/far.js:56:78" {
		t.Errorf("parts put together = %q", all.String())
	}
}

func TestSplitLinkLines(t *testing.T) {
	cases := []struct {
		in   string
		want []string
	}{
		{"", []string{}},
		{"a", []string{"a"}},
		{"a\n", []string{"a\n"}},
		{"a\nb", []string{"a\n", "b"}},
		{"a\n\nb\n", []string{"a\n", "\n", "b\n"}},
	}
	for _, tc := range cases {
		if got := SplitLinkLines(tc.in); !reflect.DeepEqual(got, tc.want) {
			t.Errorf("SplitLinkLines(%q) = %q, want %q", tc.in, got, tc.want)
		}
	}
}

func TestWebLinkPosition(t *testing.T) {
	cases := []struct {
		in, rest  string
		line, col int
	}{
		{"https://x.org/a.js:12:3", "https://x.org/a.js", 12, 3},
		{"https://x.org/a.js:12", "https://x.org/a.js", 12, 0},
		{"https://x.org/a.js", "https://x.org/a.js", 0, 0},
		{"file:///a/b.cs:line 6", "file:///a/b.cs", 6, 0},
	}
	for _, tc := range cases {
		rest, line, col := WebLinkPosition(tc.in)
		if rest != tc.rest || line != tc.line || col != tc.col {
			t.Errorf("WebLinkPosition(%q) = %q %d %d, want %q %d %d", tc.in, rest, line, col, tc.rest, tc.line, tc.col)
		}
	}
}
