package terminal

import (
	"reflect"
	"strings"
	"testing"
)

// Ported from VS Code: src/vs/workbench/contrib/terminalContrib/links/test/browser/terminalLinkParsing.test.ts
//
// The cases of the three operating systems that differ only in the path of the test run once, for Linux. The 99 links of the
// table, the other cases of removeLinkQueryString and detectLinks, and the suites that put three of the links on one line are
// ported; the Windows-only cases (drive letters, UNC paths) are skipped because Emetos runs on Linux only.

const (
	testRow    = 339
	testCol    = 12
	testRowEnd = 341
	testColEnd = 789
)

// testLink is a link and what is in it.
type testLink struct {
	link, prefix, suffix                 string
	hasRow, hasCol, hasRowEnd, hasColEnd bool
}

// want is the suffix the link of the table should give.
func (l testLink) want(offset int) *linkSuffix {
	if l.suffix == "" {
		return nil
	}
	n := func(has bool, v int) int {
		if has {
			return v
		}
		return none
	}
	return &linkSuffix{
		row: n(l.hasRow, testRow), col: n(l.hasCol, testCol), rowEnd: n(l.hasRowEnd, testRowEnd), colEnd: n(l.hasColEnd, testColEnd),
		suffix: linkPart{index: offset + len([]rune(l.link)) - len([]rune(l.suffix)), text: l.suffix},
	}
}

var testLinks = []testLink{
	{"foo", "", "", false, false, false, false},
	{"foo:339", "", ":339", true, false, false, false},
	{"foo:339:12", "", ":339:12", true, true, false, false},
	{"foo:339:12-789", "", ":339:12-789", true, true, false, true},
	{"foo:339.12", "", ":339.12", true, true, false, false},
	{"foo:339.12-789", "", ":339.12-789", true, true, false, true},
	{"foo:339.12-341.789", "", ":339.12-341.789", true, true, true, true},
	{"foo#339", "", "#339", true, false, false, false},
	{"foo#339:12", "", "#339:12", true, true, false, false},
	{"foo#339:12-789", "", "#339:12-789", true, true, false, true},
	{"foo#339.12", "", "#339.12", true, true, false, false},
	{"foo#339.12-789", "", "#339.12-789", true, true, false, true},
	{"foo#339.12-341.789", "", "#339.12-341.789", true, true, true, true},
	{"foo 339", "", " 339", true, false, false, false},
	{"foo 339:12", "", " 339:12", true, true, false, false},
	{"foo 339:12-789", "", " 339:12-789", true, true, false, true},
	{"foo 339.12", "", " 339.12", true, true, false, false},
	{"foo 339.12-789", "", " 339.12-789", true, true, false, true},
	{"foo 339.12-341.789", "", " 339.12-341.789", true, true, true, true},
	{"foo, 339", "", ", 339", true, false, false, false},
	{"\"foo\",339", "\"", "\",339", true, false, false, false},
	{"\"foo\",339:12", "\"", "\",339:12", true, true, false, false},
	{"\"foo\",339.12", "\"", "\",339.12", true, true, false, false},
	{"\"foo\", line 339", "\"", "\", line 339", true, false, false, false},
	{"\"foo\", line 339, col 12", "\"", "\", line 339, col 12", true, true, false, false},
	{"\"foo\", line 339, column 12", "\"", "\", line 339, column 12", true, true, false, false},
	{"\"foo\":line 339", "\"", "\":line 339", true, false, false, false},
	{"\"foo\":line 339, col 12", "\"", "\":line 339, col 12", true, true, false, false},
	{"\"foo\":line 339, column 12", "\"", "\":line 339, column 12", true, true, false, false},
	{"\"foo\": line 339", "\"", "\": line 339", true, false, false, false},
	{"\"foo\": line 339, col 12", "\"", "\": line 339, col 12", true, true, false, false},
	{"\"foo\": line 339, column 12", "\"", "\": line 339, column 12", true, true, false, false},
	{"\"foo\" on line 339", "\"", "\" on line 339", true, false, false, false},
	{"\"foo\" on line 339, col 12", "\"", "\" on line 339, col 12", true, true, false, false},
	{"\"foo\" on line 339, column 12", "\"", "\" on line 339, column 12", true, true, false, false},
	{"\"foo\" line 339", "\"", "\" line 339", true, false, false, false},
	{"\"foo\" line 339 column 12", "\"", "\" line 339 column 12", true, true, false, false},
	{"'foo',339", "'", "',339", true, false, false, false},
	{"'foo',339:12", "'", "',339:12", true, true, false, false},
	{"'foo',339.12", "'", "',339.12", true, true, false, false},
	{"'foo', line 339", "'", "', line 339", true, false, false, false},
	{"'foo', line 339, col 12", "'", "', line 339, col 12", true, true, false, false},
	{"'foo', line 339, column 12", "'", "', line 339, column 12", true, true, false, false},
	{"'foo':line 339", "'", "':line 339", true, false, false, false},
	{"'foo':line 339, col 12", "'", "':line 339, col 12", true, true, false, false},
	{"'foo':line 339, column 12", "'", "':line 339, column 12", true, true, false, false},
	{"'foo': line 339", "'", "': line 339", true, false, false, false},
	{"'foo': line 339, col 12", "'", "': line 339, col 12", true, true, false, false},
	{"'foo': line 339, column 12", "'", "': line 339, column 12", true, true, false, false},
	{"'foo' on line 339", "'", "' on line 339", true, false, false, false},
	{"'foo' on line 339, col 12", "'", "' on line 339, col 12", true, true, false, false},
	{"'foo' on line 339, column 12", "'", "' on line 339, column 12", true, true, false, false},
	{"'foo' line 339", "'", "' line 339", true, false, false, false},
	{"'foo' line 339 column 12", "'", "' line 339 column 12", true, true, false, false},
	{"foo, line 339", "", ", line 339", true, false, false, false},
	{"foo, line 339, col 12", "", ", line 339, col 12", true, true, false, false},
	{"foo, line 339, column 12", "", ", line 339, column 12", true, true, false, false},
	{"foo:line 339", "", ":line 339", true, false, false, false},
	{"foo:line 339, col 12", "", ":line 339, col 12", true, true, false, false},
	{"foo:line 339, column 12", "", ":line 339, column 12", true, true, false, false},
	{"foo: line 339", "", ": line 339", true, false, false, false},
	{"foo: line 339, col 12", "", ": line 339, col 12", true, true, false, false},
	{"foo: line 339, column 12", "", ": line 339, column 12", true, true, false, false},
	{"foo on line 339", "", " on line 339", true, false, false, false},
	{"foo on line 339, col 12", "", " on line 339, col 12", true, true, false, false},
	{"foo on line 339, column 12", "", " on line 339, column 12", true, true, false, false},
	{"foo line 339", "", " line 339", true, false, false, false},
	{"foo line 339 column 12", "", " line 339 column 12", true, true, false, false},
	{"foo(339)", "", "(339)", true, false, false, false},
	{"foo(339,12)", "", "(339,12)", true, true, false, false},
	{"foo(339, 12)", "", "(339, 12)", true, true, false, false},
	{"foo (339)", "", " (339)", true, false, false, false},
	{"foo (339,12)", "", " (339,12)", true, true, false, false},
	{"foo (339, 12)", "", " (339, 12)", true, true, false, false},
	{"foo: (339)", "", ": (339)", true, false, false, false},
	{"foo: (339,12)", "", ": (339,12)", true, true, false, false},
	{"foo: (339, 12)", "", ": (339, 12)", true, true, false, false},
	{"foo(339:12)", "", "(339:12)", true, true, false, false},
	{"foo (339:12)", "", " (339:12)", true, true, false, false},
	{"foo[339]", "", "[339]", true, false, false, false},
	{"foo[339,12]", "", "[339,12]", true, true, false, false},
	{"foo[339, 12]", "", "[339, 12]", true, true, false, false},
	{"foo [339]", "", " [339]", true, false, false, false},
	{"foo [339,12]", "", " [339,12]", true, true, false, false},
	{"foo [339, 12]", "", " [339, 12]", true, true, false, false},
	{"foo: [339]", "", ": [339]", true, false, false, false},
	{"foo: [339,12]", "", ": [339,12]", true, true, false, false},
	{"foo: [339, 12]", "", ": [339, 12]", true, true, false, false},
	{"foo[339:12]", "", "[339:12]", true, true, false, false},
	{"foo [339:12]", "", " [339:12]", true, true, false, false},
	{"\"foo\", line 339, character 12", "\"", "\", line 339, character 12", true, true, false, false},
	{"\"foo\", line 339, characters 12-789", "\"", "\", line 339, characters 12-789", true, true, false, true},
	{"\"foo\", lines 339-341", "\"", "\", lines 339-341", true, false, true, false},
	{"\"foo\", lines 339-341, characters 12-789", "\"", "\", lines 339-341, characters 12-789", true, true, true, true},
	{"foo\u00a0339:12", "", "\u00a0339:12", true, true, false, false},
	{"\"foo\" on line 339,\u00a0column 12", "\"", "\" on line 339,\u00a0column 12", true, true, false, false},
	{"'foo' on line\u00a0339, column 12", "'", "' on line\u00a0339, column 12", true, true, false, false},
	{"foo (339,\u00a012)", "", " (339,\u00a012)", true, true, false, false},
	{"foo\u00a0[339, 12]", "", "\u00a0[339, 12]", true, true, false, false},
}

func testLinksWithSuffix() []testLink {
	var out []testLink
	for _, l := range testLinks {
		if l.suffix != "" {
			out = append(out, l)
		}
	}
	return out
}

func TestRemoveLinkSuffix(t *testing.T) {
	for _, l := range testLinks {
		t.Run(l.link, func(t *testing.T) {
			want := l.link
			if l.suffix != "" {
				want = strings.Replace(l.link, l.suffix, "", 1)
			}
			if got := removeLinkSuffix(l.link); got != want {
				t.Errorf("removeLinkSuffix(%q) = %q, want %q", l.link, got, want)
			}
		})
	}
}

func TestLinkSuffixOf(t *testing.T) {
	for _, l := range testLinks {
		t.Run(l.link, func(t *testing.T) {
			if got := linkSuffixOf(l.link); !reflect.DeepEqual(got, l.want(0)) {
				t.Errorf("linkSuffixOf(%q) = %+v, want %+v", l.link, got, l.want(0))
			}
		})
	}
}

func TestDetectLinkSuffixes(t *testing.T) {
	for _, l := range testLinks {
		t.Run(l.link, func(t *testing.T) {
			var want []linkSuffix
			if w := l.want(0); w != nil {
				want = []linkSuffix{*w}
			}
			if got := detectLinkSuffixes(l.link); !reflect.DeepEqual(got, want) {
				t.Errorf("detectLinkSuffixes(%q) = %+v, want %+v", l.link, got, want)
			}
		})
	}
	t.Run("several in one line", func(t *testing.T) {
		got := detectLinkSuffixes("foo(1, 2) bar[3, 4] baz on line 5")
		want := []linkSuffix{
			{row: 1, col: 2, rowEnd: none, colEnd: none, suffix: linkPart{3, "(1, 2)"}},
			{row: 3, col: 4, rowEnd: none, colEnd: none, suffix: linkPart{13, "[3, 4]"}},
			{row: 5, col: none, rowEnd: none, colEnd: none, suffix: linkPart{23, " on line 5"}},
		}
		if !reflect.DeepEqual(got, want) {
			t.Errorf("got %+v\nwant %+v", got, want)
		}
	})
}

func TestRemoveLinkQueryString(t *testing.T) {
	tests := []struct{ in, want string }{
		{"?a=b", ""}, {"foo?a=b", "foo"}, {"./foo?a=b", "./foo"}, {"/foo/bar?a=b", "/foo/bar"}, {"foo?a=b?", "foo"},
		{"foo?a=b&c=d", "foo"}, {`\\?\foo?a=b`, `\\?\foo`},
	}
	for _, tt := range tests {
		if got := removeLinkQueryString(tt.in); got != tt.want {
			t.Errorf("removeLinkQueryString(%q) = %q, want %q", tt.in, got, tt.want)
		}
	}
}

// sfx makes the suffix of a parsed link in the tests; none is written -1.
func sfx(row, col, rowEnd, colEnd, index int, text string) *linkSuffix {
	return &linkSuffix{row: row, col: col, rowEnd: rowEnd, colEnd: colEnd, suffix: linkPart{index, text}}
}

func TestDetectLinks(t *testing.T) {
	tests := []struct {
		name, line string
		want       []parsedLink
	}{
		{"suffixes of three kinds", `foo(1, 2) bar[3, 4] "baz" on line 5`, []parsedLink{
			{path: linkPart{0, "foo"}, suffix: sfx(1, 2, none, none, 3, "(1, 2)")},
			{path: linkPart{10, "bar"}, suffix: sfx(3, 4, none, none, 13, "[3, 4]")},
			{path: linkPart{21, "baz"}, prefix: &linkPart{20, `"`}, suffix: sfx(5, none, none, none, 24, `" on line 5`)},
		}},
		{"several links when an opening bracket is in the text", `notlink[foo:45]`, []parsedLink{
			{path: linkPart{0, "notlink[foo"}, suffix: sfx(45, none, none, none, 11, ":45")},
			{path: linkPart{8, "foo"}, suffix: sfx(45, none, none, none, 11, ":45")},
		}},
		{"the prefix", `"foo", line 5, col 6`, []parsedLink{
			{path: linkPart{1, "foo"}, prefix: &linkPart{0, `"`}, suffix: sfx(5, 6, none, none, 4, `", line 5, col 6`)},
		}},
		{"several prefix characters: the outer quotes are not the prefix", `echo '"foo", line 5, col 6'`, []parsedLink{
			{path: linkPart{7, "foo"}, prefix: &linkPart{6, `"`}, suffix: sfx(5, 6, none, none, 10, `", line 5, col 6`)},
		}},
		// (VS Code excludes | from a path with no suffix on Windows only; the Linux rule lets it in)
		{"pipes are not in a path with a suffix", `|/test/path/linux:400|`, []parsedLink{
			{path: linkPart{1, "/test/path/linux"}, suffix: sfx(400, none, none, none, 17, ":400")},
		}},
		{"angle brackets are not in a path", `<` + "/test/path/linux" + `<`, []parsedLink{{path: linkPart{1, "/test/path/linux"}}}},
		{"closing angle brackets are not in a path", `>/test/path/linux>`, []parsedLink{{path: linkPart{1, "/test/path/linux"}}}},
		{"angle brackets are not in a path with a suffix", `<` + "/test/path/linux:400" + `<`, []parsedLink{
			{path: linkPart{1, "/test/path/linux"}, suffix: sfx(400, none, none, none, 17, ":400")},
		}},
		{"the query string is not in a path", `/test/path/linux?a=b`, []parsedLink{{path: linkPart{0, "/test/path/linux"}}}},
		{"the query string with an & is not in a path", `/test/path/linux?a=b&c=d`, []parsedLink{{path: linkPart{0, "/test/path/linux"}}}},
		{"numeric git diff prefix of the old file", `--- 1/foo/bar`, []parsedLink{{path: linkPart{6, "foo/bar"}}}},
		{"numeric git diff prefix of the new file", `+++ 2/foo/baz`, []parsedLink{{path: linkPart{6, "foo/baz"}}}},
		{"numeric prefixes of diff --git", `diff --git 1/foo/bar 2/foo/baz`, []parsedLink{{path: linkPart{13, "foo/bar"}}, {path: linkPart{23, "foo/baz"}}}},
		{"reversed numeric prefixes", `diff --git 2/foo/baz 1/foo/bar`, []parsedLink{{path: linkPart{13, "foo/baz"}}, {path: linkPart{23, "foo/bar"}}}},
		{"an ordinary numeric suffix", `foo 1`, []parsedLink{{path: linkPart{0, "foo"}, suffix: sfx(1, none, none, none, 3, " 1")}}},
		{"a numeric suffix followed by a path separator", `foo 1/bar`, []parsedLink{{path: linkPart{4, "1/bar"}}}},
		{"a numeric suffix after diff --git text", `diff --git foo.ts:123`, []parsedLink{{path: linkPart{11, "foo.ts"}, suffix: sfx(123, none, none, none, 17, ":123")}}},
		{"an empty path with a suffix is no link", `""",1`, nil},
	}
	for _, p := range []string{"a", "c", "w", "i", "o"} {
		tests = append(tests, struct {
			name, line string
			want       []parsedLink
		}{"git diff old file " + p, "--- " + p + "/foo/bar", []parsedLink{{path: linkPart{6, "foo/bar"}}}})
	}
	for _, p := range []string{"b", "c", "w", "i", "o"} {
		tests = append(tests, struct {
			name, line string
			want       []parsedLink
		}{"git diff new file " + p, "+++ " + p + "/foo/bar", []parsedLink{{path: linkPart{6, "foo/bar"}}}})
	}
	for _, pair := range [][2]string{{"a", "b"}, {"c", "w"}, {"i", "o"}} {
		tests = append(tests, struct {
			name, line string
			want       []parsedLink
		}{"diff --git " + pair[0] + " " + pair[1], "diff --git " + pair[0] + "/foo/bar " + pair[1] + "/foo/baz",
			[]parsedLink{{path: linkPart{13, "foo/bar"}}, {path: linkPart{23, "foo/baz"}}}})
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := detectLinks(tt.line)
			if len(got) == 0 && len(tt.want) == 0 {
				return
			}
			if !reflect.DeepEqual(got, tt.want) {
				t.Errorf("detectLinks(%q)\n got %s\nwant %s", tt.line, dump(got), dump(tt.want))
			}
		})
	}
}

// dump writes parsed links for a message.
func dump(links []parsedLink) string {
	var sb strings.Builder
	for _, l := range links {
		sb.WriteString("{path " + l.path.text + "@" + itoa(l.path.index))
		if l.prefix != nil {
			sb.WriteString(" prefix " + l.prefix.text + "@" + itoa(l.prefix.index))
		}
		if s := l.suffix; s != nil {
			sb.WriteString(" suffix " + s.suffix.text + "@" + itoa(s.suffix.index) + " r" + itoa(s.row) + " c" + itoa(s.col) + " re" + itoa(s.rowEnd) + " ce" + itoa(s.colEnd))
		}
		sb.WriteString("} ")
	}
	return sb.String()
}

func TestAQueryStringDoesNotMakeALinkStartWithAQuestionMark(t *testing.T) {
	for _, line := range []string{`http://foo.com/?bar=/a/b&baz=c`, `http://foo.com/?bar=a:\b&baz=c`} {
		for _, l := range detectLinks(line) {
			if strings.HasPrefix(l.path.text, "?") {
				t.Errorf("a link starts with ? in %q: %q", line, l.path.text)
			}
		}
	}
}

func TestThreeLinksWithSuffixesOnOneLine(t *testing.T) {
	links := testLinksWithSuffix()
	for i := 0; i+2 < len(links); i++ {
		l1, l2, l3 := links[i], links[i+1], links[i+2]
		line := " " + l1.link + " " + l2.link + " " + l3.link + " "
		t.Run(strings.ReplaceAll(line, "\u00a0", "<nbsp>"), func(t *testing.T) {
			got := detectLinks(line)
			if len(got) != 3 {
				t.Fatalf("%d links in %q: %s", len(got), line, dump(got))
			}
			// where each link starts in the line, in characters
			start := 1
			for k, l := range []testLink{l1, l2, l3} {
				prefixLen := len([]rune(l.prefix))
				want := parsedLink{
					path:   linkPart{start + prefixLen, strings.Replace(strings.Replace(l.link, l.suffix, "", 1), l.prefix, "", 1)},
					suffix: l.want(start),
				}
				if l.prefix != "" {
					want.prefix = &linkPart{start, l.prefix}
				}
				if !reflect.DeepEqual(got[k], want) {
					t.Errorf("link %d of %q\n got %s\nwant %s", k+1, line, dump(got[k:k+1]), dump([]parsedLink{want}))
				}
				start += len([]rune(l.link)) + 1
			}
		})
	}
}
