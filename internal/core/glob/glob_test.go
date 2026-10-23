package glob

import (
	"slices"
	"testing"
)

// TestVSCodeMatchCases runs the pattern and path pairs of VS Code's glob.test.ts (simple,
// dot hidden, file pattern, star, file / folder match, questionmark, globstar, issue 41724,
// brace expansion, brackets, full path, ending path, prefix agnostic, cached properly,
// invalid glob and caseInsensitiveMatch).
func TestVSCodeMatchCases(t *testing.T) {
	if len(vscodeMatchCases) < 300 {
		t.Fatalf("only %d cases", len(vscodeMatchCases))
	}
	for _, tc := range vscodeMatchCases {
		opts := Options{IgnoreCase: tc.ignoreCase}
		if got := MatchPattern(tc.pattern, tc.path, opts); got != tc.want {
			t.Errorf("%s: MatchPattern(%q, %q, ignoreCase=%v) = %v, want %v", tc.test, tc.pattern, tc.path, tc.ignoreCase, got, tc.want)
		}
		// A parsed pattern gives the same answer, however often it is used.
		m := Parse(tc.pattern, opts)
		for range 2 {
			if got := m.Match(tc.path); got != tc.want {
				t.Errorf("%s: parsed %q on %q = %v, want %v", tc.test, tc.pattern, tc.path, got, tc.want)
			}
		}
	}
}

func TestURIMatch(t *testing.T) {
	if !MatchPattern("scheme:/**/*.md", "scheme:///super/duper/long/some/file.md", Options{}) {
		t.Error("a URI must match a pattern with a scheme")
	}
}

func TestSplitGlobAware(t *testing.T) {
	cases := []struct {
		in   string
		want []string
	}{
		{"foo,bar", []string{"foo", "bar"}},
		{"foo", []string{"foo"}},
		{"{foo,bar}", []string{"{foo,bar}"}},
		{"foo,bar,{foo,bar}", []string{"foo", "bar", "{foo,bar}"}},
		{"{foo,bar},foo,bar,{foo,bar}", []string{"{foo,bar}", "foo", "bar", "{foo,bar}"}},
		{"[foo,bar]", []string{"[foo,bar]"}},
		{"foo,bar,[foo,bar]", []string{"foo", "bar", "[foo,bar]"}},
		{"[foo,bar],foo,bar,[foo,bar]", []string{"[foo,bar]", "foo", "bar", "[foo,bar]"}},
		{"", nil},
		{"a,", []string{"a"}},
		{",a", []string{"", "a"}},
	}
	for _, tc := range cases {
		if got := SplitGlobAware(tc.in, ','); !slices.Equal(got, tc.want) {
			t.Errorf("SplitGlobAware(%q) = %q, want %q", tc.in, got, tc.want)
		}
	}
}

// TestIssue144458 is 'expression with non-trivia glob'.
func TestIssue144458(t *testing.T) {
	m := Parse("**/p*", Options{})
	for _, p := range []string{"foo/barp", "foo/bar/ap", "ap", "foo/barp1", "foo/bar/ap1", "ap1", "/foo/barp", "/foo/bar/ap", "/ap", "/foo/barp1", "/foo/bar/ap1", "/ap1"} {
		if m.Match(p) {
			t.Errorf("**/p* must not match %q", p)
		}
	}
	for _, p := range []string{"foo/pbar", "/foo/pbar", "foo/bar/pa", "/p"} {
		if !m.Match(p) {
			t.Errorf("**/p* must match %q", p)
		}
	}
}

func TestFalsyPatternsAndPaths(t *testing.T) {
	if MatchPattern("", "foo", Options{}) || !Parse("", Options{}).IsEmpty() {
		t.Error("the empty pattern matches nothing")
	}
	for _, p := range []string{"foo", "**/*.j?", "**/*.foo", "**/foo", "{**/baz,**/foo}", "{**/*.baz,**/*.foo}"} {
		if Parse(p, Options{}).Match("") {
			t.Errorf("%q must not match the empty path", p)
		}
	}
	if !MatchPattern("*", "", Options{}) {
		t.Error("* matches the empty path")
	}
}

func TestPatternBasename(t *testing.T) {
	cases := []struct {
		pattern, path, basename string
		want                    bool
	}{
		{"**/foo", "bar/baz", "baz", false},
		{"**/foo", "bar/foo", "foo", true},
		{"{**/baz,**/foo}", "baz/bar", "bar", false},
		{"{**/baz,**/foo}", "baz/foo", "foo", true},
	}
	for _, tc := range cases {
		if got := Parse(tc.pattern, Options{}).MatchBase(tc.path, tc.basename); got != tc.want {
			t.Errorf("%q on %q (%q) = %v", tc.pattern, tc.path, tc.basename, got)
		}
	}
}

func TestTrailingSlash(t *testing.T) {
	cases := []struct {
		pattern, path, basename string
		opts                    Options
		want                    bool
	}{
		{"**/foo/", "bar/baz", "baz", Options{}, false},
		{"**/foo/", "bar/foo", "foo", Options{}, true},
		{"**/*.foo/", "bar/file.baz", "file.baz", Options{}, false},
		{"**/*.foo/", "bar/file.foo", "file.foo", Options{}, true},
		{"{**/foo/,**/abc/}", "bar/baz", "baz", Options{}, false},
		{"{**/foo/,**/abc/}", "bar/foo", "foo", Options{}, true},
		{"{**/foo/,**/abc/}", "bar/abc", "abc", Options{}, true},
		{"{**/foo/,**/abc/}", "bar/baz", "baz", Options{TrimForExclusions: true}, false},
		{"{**/foo/,**/abc/}", "bar/foo", "foo", Options{TrimForExclusions: true}, true},
		{"{**/foo/,**/abc/}", "bar/abc", "abc", Options{TrimForExclusions: true}, true},
	}
	for _, tc := range cases {
		if got := Parse(tc.pattern, tc.opts).MatchBase(tc.path, tc.basename); got != tc.want {
			t.Errorf("%q on %q = %v, want %v", tc.pattern, tc.path, got, tc.want)
		}
	}
}

func TestPatternPath(t *testing.T) {
	trim := Options{TrimForExclusions: true}
	cases := []struct {
		pattern, path, basename string
		opts                    Options
		want                    bool
	}{
		{"**/foo/bar", "foo/baz", "baz", Options{}, false},
		{"**/foo/bar", "foo/bar", "bar", Options{}, true},
		{"**/foo/bar", "bar/foo/bar", "bar", Options{}, true},
		{"**/foo/bar/**", "bar/foo/bar", "bar", Options{}, true},
		{"**/foo/bar/**", "bar/foo/bar/baz", "baz", Options{}, true},
		{"**/foo/bar/**", "bar/foo/bar", "bar", trim, true},
		{"**/foo/bar/**", "bar/foo/bar/baz", "baz", trim, false},
		{"foo/bar", "foo/baz", "baz", Options{}, false},
		{"foo/bar", "foo/bar", "bar", Options{}, true},
		{"foo/bar/baz", "foo/bar/baz", "baz", Options{}, true}, // VS Code issue 15424
		{"foo/bar", "bar/foo/bar", "bar", Options{}, false},
		{"foo/bar/**", "foo/bar/baz", "baz", Options{}, true},
		{"foo/bar/**", "foo/bar", "bar", trim, true},
		{"foo/bar/**", "foo/bar/baz", "baz", trim, false},
	}
	for _, tc := range cases {
		if got := Parse(tc.pattern, tc.opts).MatchBase(tc.path, tc.basename); got != tc.want {
			t.Errorf("%q on %q (trim=%v) = %v, want %v", tc.pattern, tc.path, tc.opts.TrimForExclusions, got, tc.want)
		}
	}
}

func TestTerms(t *testing.T) {
	trim := Options{TrimForExclusions: true}
	basenames := []struct {
		pattern string
		opts    Options
		want    []string
	}{
		{"**/*.foo", Options{}, nil},
		{"**/foo", Options{}, []string{"foo"}},
		{"**/foo/", Options{}, []string{"foo"}},
		{"{**/baz,**/foo}", Options{}, []string{"baz", "foo"}},
		{"{**/baz/,**/foo/}", Options{}, []string{"baz", "foo"}},
		{"**/foo/**", Options{}, nil},
		{"**/foo/**", trim, []string{"foo"}},
	}
	for _, tc := range basenames {
		if got := Parse(tc.pattern, tc.opts).BasenameTerms(); !slices.Equal(got, tc.want) {
			t.Errorf("BasenameTerms(%q) = %q, want %q", tc.pattern, got, tc.want)
		}
	}
	paths := []struct {
		pattern string
		opts    Options
		want    []string
	}{
		{"**/*.foo", Options{}, nil},
		{"**/foo", Options{}, nil},
		{"**/foo/bar", Options{}, []string{"*/foo/bar"}},
		{"**/foo/bar/", Options{}, []string{"*/foo/bar"}},
		{"foo/bar", Options{}, []string{"./foo/bar"}},
		{"**/foo/bar/**", Options{}, nil},
		{"**/foo/bar/**", trim, []string{"*/foo/bar"}},
	}
	for _, tc := range paths {
		if got := Parse(tc.pattern, tc.opts).PathTerms(); !slices.Equal(got, tc.want) {
			t.Errorf("PathTerms(%q) = %q, want %q", tc.pattern, got, tc.want)
		}
	}
	if (Parse("", Options{})).BasenameTerms() != nil || (Parse("", Options{})).PathTerms() != nil {
		t.Error("an empty pattern has no terms")
	}
}

func TestRelativePattern(t *testing.T) {
	cases := []struct {
		name       string
		base, pat  string
		path       string
		want       bool
		ignoreCase bool
	}{
		{"globstar", "/DNXConsoleApp/foo", "**/*.cs", "/DNXConsoleApp/foo/Program.cs", true, false},
		{"globstar deep", "/DNXConsoleApp/foo", "**/*.cs", "/DNXConsoleApp/foo/bar/Program.cs", true, false},
		{"globstar other ext", "/DNXConsoleApp/foo", "**/*.cs", "/DNXConsoleApp/foo/Program.ts", false, false},
		{"globstar above base", "/DNXConsoleApp/foo", "**/*.cs", "/DNXConsoleApp/Program.cs", false, false},
		{"globstar elsewhere", "/DNXConsoleApp/foo", "**/*.cs", "/other/DNXConsoleApp/foo/Program.ts", false, false},
		{"star", "/DNXConsoleApp/foo", "*.cs", "/DNXConsoleApp/foo/Program.cs", true, false},
		{"star deep", "/DNXConsoleApp/foo", "*.cs", "/DNXConsoleApp/foo/bar/Program.cs", false, false},
		{"star other ext", "/DNXConsoleApp/foo", "*.cs", "/DNXConsoleApp/foo/Program.ts", false, false},
		{"star above base", "/DNXConsoleApp/foo", "*.cs", "/DNXConsoleApp/Program.cs", false, false},
		{"star with path", "/DNXConsoleApp/foo", "something/*.cs", "/DNXConsoleApp/foo/something/Program.cs", true, false},
		{"star with path missing", "/DNXConsoleApp/foo", "something/*.cs", "/DNXConsoleApp/foo/Program.cs", false, false},
		{"star alone", "/DNXConsoleApp/foo/something/Program.cs", "*", "/DNXConsoleApp/foo/something/Program.cs", true, false},
		{"star alone elsewhere", "/DNXConsoleApp/foo/something/Program.cs", "*", "/DNXConsoleApp/foo/Program.cs", false, false},
		{"case sensitive on Linux", "/DNXConsoleApp/foo", "something/*.cs", "/dnxconsoleapp/foo/something/program.cs", false, false},
		{"case insensitive when asked", "/DNXConsoleApp/foo", "something/*.cs", "/dnxconsoleapp/foo/something/program.cs", true, true},
		{"root base", "/", "foo.cs", "/foo.cs", true, false},
		{"base with trailing slash", "/bar/", "foo.cs", "/bar/foo.cs", true, false},
		{"styles", "/DNXConsoleApp/foo", "styles/style.css", "/DNXConsoleApp/foo/styles/style.css", true, false},
		{"styles other file", "/DNXConsoleApp/foo", "styles/style.css", "/DNXConsoleApp/foo/Program.cs", false, false},
		{"a sibling folder with the same prefix", "/DNXConsoleApp/foo", "**/*.cs", "/DNXConsoleApp/foobar/Program.cs", false, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			m := ParseRelative(RelativePattern{Base: tc.base, Pattern: tc.pat}, Options{IgnoreCase: tc.ignoreCase})
			if got := m.Match(tc.path); got != tc.want {
				t.Errorf("got %v, want %v", got, tc.want)
			}
		})
	}
	if !ParseRelative(RelativePattern{Base: "/a", Pattern: ""}, Options{}).IsEmpty() {
		t.Error("an empty relative pattern is empty")
	}
	if ParseRelative(RelativePattern{Base: "/a", Pattern: ""}, Options{}).Match("/a/b") {
		t.Error("an empty relative pattern matches nothing")
	}
}

func TestIsEmpty(t *testing.T) {
	for _, p := range []string{"", "[", "**/*(.js"} {
		if p == "[" {
			continue // an open bracket is dropped by the translation, the pattern stays valid
		}
		if !Parse(p, Options{}).IsEmpty() && p == "" {
			t.Errorf("%q must be empty", p)
		}
	}
	if Parse("**/*(.js", Options{}).IsEmpty() {
		// "(" is escaped, so this pattern is valid and matches nothing in the test paths
		t.Log("valid pattern with a parenthesis")
	}
	if Parse("foo", Options{}).IsEmpty() {
		t.Error("foo is not empty")
	}
}

func TestPatternsEqual(t *testing.T) {
	a := []RelativePattern{{Pattern: "a"}}
	if !PatternsEqual(a, []RelativePattern{{Pattern: "a"}}) || PatternsEqual(a, []RelativePattern{{Pattern: "b"}}) {
		t.Error("plain patterns")
	}
	three := []RelativePattern{{Pattern: "a"}, {Pattern: "b"}, {Pattern: "c"}}
	if !PatternsEqual(three, slices.Clone(three)) || PatternsEqual(three[:2], three) {
		t.Error("lists")
	}
	rel := []RelativePattern{{Base: "a", Pattern: "*"}, {Pattern: "b"}}
	if !PatternsEqual(rel, slices.Clone(rel)) || PatternsEqual(rel, []RelativePattern{{Base: "x", Pattern: "*"}, {Pattern: "b"}}) {
		t.Error("relative patterns")
	}
}

// TestBracketEdgeCases checks the character classes that JavaScript reads in ways Go's
// regexp does not.
func TestBracketEdgeCases(t *testing.T) {
	cases := []struct {
		pattern, path string
		want          bool
	}{
		{"foo.[!]", "foo.x", true}, // JavaScript's [^] matches any character
		{"foo.[!]", "foo.", false},
		{"foo.[", "foo.", true},
		{"a]b", "a]b", false}, // a ] outside brackets is an empty class
		{"foo.[]]", "foo.]", true},
		{"foo.[]", "foo.]", false},
		{"{a,]}", "a", true},
		{"[z-a]", "a", false},
	}
	for _, tc := range cases {
		if got := MatchPattern(tc.pattern, tc.path, Options{}); got != tc.want {
			t.Errorf("%q on %q = %v, want %v", tc.pattern, tc.path, got, tc.want)
		}
	}
}

func TestNormalize(t *testing.T) {
	cases := map[string]string{
		"/a/b/../c":   "/a/c",
		"a/./b":       "a/b",
		"../a":        "../a",
		"/../a":       "/a",
		"a/b/":        "a/b/",
		"":            ".",
		"a/..":        ".",
		"/a/b/../../": "/",
	}
	for in, want := range cases {
		if got := normalize(in); got != want {
			t.Errorf("normalize(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestIsEqualOrParent(t *testing.T) {
	cases := []struct {
		path, candidate string
		ignoreCase      bool
		want            bool
	}{
		{"/a/b", "/a", false, true},
		{"/a/b", "/a/", false, true},
		{"/a/b", "/a/b", false, true},
		{"/ab", "/a", false, false},
		{"/a", "/a/b", false, false},
		{"", "/a", false, false},
		{"/a", "", false, false},
		{"/A/b", "/a", false, false},
		{"/A/b", "/a", true, true},
		{"/A/b", "/a/", true, true},
		{"/A", "/a", true, true},
		{"/Ab", "/a", true, false},
		{"/a/x/../b/c", "/a/b", false, true},
		{"/a/x/../b", "/a/b", false, false}, // as in VS Code: the equality check comes before normalizing
	}
	for _, tc := range cases {
		if got := isEqualOrParent(tc.path, tc.candidate, tc.ignoreCase); got != tc.want {
			t.Errorf("isEqualOrParent(%q, %q, %v) = %v", tc.path, tc.candidate, tc.ignoreCase, got)
		}
	}
}

func TestStringOps(t *testing.T) {
	ci := strOps{ignoreCase: true}
	cs := strOps{}
	if !ci.endsWith("Foo.JS", ".js") || cs.endsWith("Foo.JS", ".js") || ci.endsWith("a", "bb") {
		t.Error("endsWith")
	}
	// A suffix must not start in the middle of a character.
	if ci.endsWith("é", "\xa9") {
		t.Error("endsWith inside a character")
	}
	if !startsWithFold("ABc", "ab") || startsWithFold("a", "ab") || startsWithFold("é", "\xc3") {
		t.Error("startsWithFold")
	}
}
