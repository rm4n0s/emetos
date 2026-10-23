package glob

import (
	"slices"
	"testing"
)

func siblingsOf(names ...string) func(string) bool {
	return func(name string) bool { return slices.Contains(names, name) }
}

// matchExpr runs an expression and returns the pattern that matched, "" for no match.
func matchExpr(e Expression, path, basename string, hasSibling func(string) bool, opts Options) string {
	pat, _ := ParseExpression(e, opts).Match(path, basename, hasSibling)
	return pat
}

func TestExpressionSupportSingle(t *testing.T) {
	hasSibling := siblingsOf("test.html", "test.txt", "test.ts", "test.js")
	expr := Expression{{Pattern: "**/*.js", HasWhen: true, When: "$(basename).ts"}}

	if got := matchExpr(expr, "test.js", "", hasSibling, Options{}); got != "**/*.js" {
		t.Errorf("sibling exists: %q", got)
	}
	if got := matchExpr(expr, "test.js", "", func(string) bool { return false }, Options{}); got != "" {
		t.Errorf("no sibling: %q", got)
	}
	if got := matchExpr(expr, "test.js", "", func(n string) bool { return n == "te.ts" }, Options{}); got != "" {
		t.Errorf("another sibling: %q", got)
	}
	if got := matchExpr(expr, "test.js", "", nil, Options{}); got != "" {
		t.Errorf("no sibling function: %q", got)
	}
	// An empty condition looks for a file with an empty name.
	empty := Expression{{Pattern: "**/*.js", HasWhen: true}}
	if got := matchExpr(empty, "test.js", "", hasSibling, Options{}); got != "" {
		t.Errorf("empty condition: %q", got)
	}
	// An object without a when is a plain pattern.
	plain := Expression{{Pattern: "**/*.js"}}
	if got := matchExpr(plain, "test.js", "", hasSibling, Options{}); got != "**/*.js" {
		t.Errorf("object without when: %q", got)
	}
	if got := matchExpr(Expression{}, "test.js", "", hasSibling, Options{}); got != "" {
		t.Errorf("empty expression: %q", got)
	}
}

func TestExpressionSupportMultiple(t *testing.T) {
	hasSibling := siblingsOf("test.html", "test.txt", "test.ts", "test.js")
	expr := Expression{
		{Pattern: "**/*.js", HasWhen: true, When: "$(basename).ts"},
		{Pattern: "**/*.as"},
		{Pattern: "**/*.foo", Disabled: true},
		{Pattern: "**/*.bananas"},
	}
	cases := []struct {
		path       string
		hasSibling func(string) bool
		want       string
	}{
		{"test.js", hasSibling, "**/*.js"},
		{"test.as", hasSibling, "**/*.as"},
		{"test.bananas", hasSibling, "**/*.bananas"},
		{"test.bananas", nil, "**/*.bananas"},
		{"test.foo", hasSibling, ""},
	}
	for _, tc := range cases {
		if got := matchExpr(expr, tc.path, "", tc.hasSibling, Options{}); got != tc.want {
			t.Errorf("%s: %q, want %q", tc.path, got, tc.want)
		}
	}
}

func TestExpressionBasics(t *testing.T) {
	cases := []struct {
		name string
		expr Expression
		path string
		opts Options
		want string
	}{
		{"disabled", Expression{{Pattern: "**/*.js", Disabled: true}}, "foo.js", Options{}, ""},
		{"two non-trivia globs", Expression{{Pattern: "**/*.j?"}, {Pattern: "**/*.t?"}}, "foo.js", Options{}, "**/*.j?"},
		{"two non-trivia globs, no match", Expression{{Pattern: "**/*.j?"}, {Pattern: "**/*.t?"}}, "foo.as", Options{}, ""},
		{"empty glob", Expression{{Pattern: ""}}, "foo.js", Options{}, ""},
		{"two basename globs", Expression{{Pattern: "**/bar"}, {Pattern: "**/baz"}}, "bar", Options{}, "**/bar"},
		{"two basename globs, none", Expression{{Pattern: "**/bar"}, {Pattern: "**/baz"}}, "foo", Options{}, ""},
		{"two basename globs, in folder", Expression{{Pattern: "**/bar"}, {Pattern: "**/baz"}}, "foo/bar", Options{}, "**/bar"},
		{"two basename globs, backslash", Expression{{Pattern: "**/bar"}, {Pattern: "**/baz"}}, `foo\bar`, Options{}, "**/bar"},
		{"two basename globs, wrong folder", Expression{{Pattern: "**/bar"}, {Pattern: "**/baz"}}, "foo/foo", Options{}, ""},
		{"ignore case", Expression{{Pattern: "**/BAR"}, {Pattern: "**/BAZ"}}, "bar", Options{IgnoreCase: true}, "**/BAR"},
		{"ignore case, second", Expression{{Pattern: "**/BAR"}, {Pattern: "**/BAZ"}}, "baz", Options{IgnoreCase: true}, "**/BAZ"},
		{"ignore case, in folder", Expression{{Pattern: "**/BAR"}, {Pattern: "**/BAZ"}}, "src/bar", Options{IgnoreCase: true}, "**/BAR"},
		{"case sensitive", Expression{{Pattern: "**/BAR"}, {Pattern: "**/BAZ"}}, "bar", Options{}, ""},
		{"mixed case", Expression{{Pattern: "**/BAR"}, {Pattern: "**/BAZ"}}, "BaR", Options{IgnoreCase: true}, "**/BAR"},
		{"basename and braces", Expression{{Pattern: "**/bar"}, {Pattern: "{**/baz,**/foo}"}}, "bar", Options{}, "**/bar"},
		{"braces, first", Expression{{Pattern: "**/bar"}, {Pattern: "{**/baz,**/foo}"}}, "foo", Options{}, "{**/baz,**/foo}"},
		{"braces, second", Expression{{Pattern: "**/bar"}, {Pattern: "{**/baz,**/foo}"}}, "baz", Options{}, "{**/baz,**/foo}"},
		{"braces, none", Expression{{Pattern: "**/bar"}, {Pattern: "{**/baz,**/foo}"}}, "abc", Options{}, ""},
		{"a pattern named base (VS Code issue 36081)", Expression{{Pattern: "base"}}, "base", Options{}, "base"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := matchExpr(tc.expr, tc.path, "", nil, tc.opts); got != tc.want {
				t.Errorf("got %q, want %q", got, tc.want)
			}
			got, ok := MatchExpression(tc.expr, tc.path, tc.opts)
			if got != tc.want || ok != (tc.want != "") {
				t.Errorf("MatchExpression: %q %v", got, ok)
			}
		})
	}
}

func TestExpressionWithBasenamesAndSiblings(t *testing.T) {
	expr := Expression{
		{Pattern: "**/bar"}, {Pattern: "**/baz"},
		{Pattern: "**/*.js", HasWhen: true, When: "$(basename).ts"},
	}
	hasSibling := siblingsOf("foo.ts", "foo.js", "foo", "bar")
	cases := []struct{ path, want string }{
		{"bar", "**/bar"}, {"foo", ""}, {"foo/bar", "**/bar"}, {"foo/foo", ""},
		{"foo.js", "**/*.js"}, {"bar.js", ""},
	}
	for _, tc := range cases {
		if got := matchExpr(expr, tc.path, "", hasSibling, Options{}); got != tc.want {
			t.Errorf("%s: %q, want %q", tc.path, got, tc.want)
		}
	}
	// With the basename given.
	one := Expression{{Pattern: "**/*.js", HasWhen: true, When: "$(basename).ts"}}
	siblings := siblingsOf("foo.ts", "foo.js")
	if matchExpr(one, "bar/baz.js", "baz.js", siblings, Options{}) != "" || matchExpr(one, "bar/foo.js", "foo.js", siblings, Options{}) != "**/*.js" {
		t.Error("expression/pattern basename")
	}
}

func TestExpressionTerms(t *testing.T) {
	trim := Options{TrimForExclusions: true}
	cases := []struct {
		name      string
		expr      Expression
		opts      Options
		basenames []string
		paths     []string
	}{
		{"basenames", Expression{{Pattern: "**/foo"}, {Pattern: "{**/bar,**/baz}"}, {Pattern: "{**/bar2/,**/baz2/}"}, {Pattern: "**/bulb", Disabled: true}}, Options{},
			[]string{"foo", "bar", "baz", "bar2", "baz2"}, nil},
		{"a sibling clause has no terms", Expression{{Pattern: "**/foo", HasWhen: true, When: "$(basename).zip"}, {Pattern: "**/bar"}}, Options{},
			[]string{"bar"}, nil},
		{"paths", Expression{{Pattern: "**/foo/bar"}, {Pattern: "**/foo2/bar2"}, {Pattern: "**/bulb"}, {Pattern: "**/bulb2"}, {Pattern: "**/bulb/foo", Disabled: true}}, Options{},
			[]string{"bulb", "bulb2"}, []string{"*/foo/bar", "*/foo2/bar2"}},
		{"paths and a sibling clause", Expression{{Pattern: "**/foo/bar", HasWhen: true, When: "$(basename).zip"}, {Pattern: "**/bar/foo"}, {Pattern: "**/bar2/foo2"}}, Options{},
			nil, []string{"*/bar/foo", "*/bar2/foo2"}},
		{"trimmed basenames", Expression{{Pattern: "**/foo/**"}, {Pattern: "{**/bar/**,**/baz/**}"}, {Pattern: "**/bulb/**", Disabled: true}}, trim,
			[]string{"foo", "bar", "baz"}, nil},
		{"trimmed sibling clause", Expression{{Pattern: "**/foo/**", HasWhen: true, When: "$(basename).zip"}, {Pattern: "**/bar/**"}}, trim,
			[]string{"bar"}, nil},
		{"trimmed paths", Expression{{Pattern: "**/foo/bar/**"}, {Pattern: "**/bulb/bar/**", Disabled: true}}, trim,
			nil, []string{"*/foo/bar"}},
		{"trimmed paths and a sibling clause", Expression{{Pattern: "**/foo/123/**", HasWhen: true, When: "$(basename).zip"}, {Pattern: "**/bar/123/**"}}, trim,
			nil, []string{"*/bar/123"}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			m := ParseExpression(tc.expr, tc.opts)
			if got := m.BasenameTerms(); !slices.Equal(got, tc.basenames) {
				t.Errorf("BasenameTerms = %q, want %q", got, tc.basenames)
			}
			if got := m.PathTerms(); !slices.Equal(got, tc.paths) {
				t.Errorf("PathTerms = %q, want %q", got, tc.paths)
			}
		})
	}
}

// TestExpressionOptimizations checks that matching with trimForExclusions still answers as
// the unoptimized patterns do (testOptimizationForBasenames and testOptimizationForPaths).
func TestExpressionOptimizations(t *testing.T) {
	trim := Options{TrimForExclusions: true}
	type check struct {
		path       string
		want       string
		hasSibling func(string) bool
	}
	siblings := siblingsOf("baz", "baz.zip", "nope")
	cases := []struct {
		name   string
		expr   Expression
		checks []check
	}{
		{"basename pattern", Expression{{Pattern: "**/*.foo/**"}}, []check{{"baz/bar.foo/bar/baz", "**/*.foo/**", nil}}},
		{"basename term", Expression{{Pattern: "**/foo/**"}}, []check{{"bar/foo", "**/foo/**", nil}, {"bar/foo/baz", "", nil}}},
		{"basename braces", Expression{{Pattern: "{**/baz/**,**/foo/**}"}}, []check{{"bar/baz", "{**/baz/**,**/foo/**}", nil}, {"bar/foo", "{**/baz/**,**/foo/**}", nil}}},
		{"several", Expression{{Pattern: "**/foo/**"}, {Pattern: "{**/bar/**,**/baz/**}"}, {Pattern: "**/bulb/**", Disabled: true}},
			[]check{{"bar/foo", "**/foo/**", nil}, {"foo/bar", "{**/bar/**,**/baz/**}", nil}, {"bar/nope", "", nil}}},
		{"with siblings", Expression{{Pattern: "**/foo/**", HasWhen: true, When: "$(basename).zip"}, {Pattern: "**/bar/**"}},
			[]check{{"bar/foo", "", nil}, {"bar/foo/baz", "", siblings}, {"bar/foo/nope", "", siblings}, {"foo/bar", "**/bar/**", nil}}},
		{"path pattern", Expression{{Pattern: "**/*.foo/bar/**"}}, []check{{"baz/bar.foo/bar/baz", "**/*.foo/bar/**", nil}}},
		{"path term", Expression{{Pattern: "**/foo/bar/**"}}, []check{{"bar/foo/bar", "**/foo/bar/**", nil}, {"bar/foo/bar/baz", "", nil}}},
		{"path terms", Expression{{Pattern: "**/foo/bar/**"}, {Pattern: "**/bulb/bar/**", Disabled: true}},
			[]check{{"bar/foo/bar", "**/foo/bar/**", nil}, {"/foo/bar/nope", "", nil}}},
		{"path with siblings", Expression{{Pattern: "**/foo/123/**", HasWhen: true, When: "$(basename).zip"}, {Pattern: "**/bar/123/**"}},
			[]check{{"bar/foo/123", "", nil}, {"bar/foo/123/baz", "", siblings}, {"bar/foo/123/nope", "", siblings}, {"foo/bar/123", "**/bar/123/**", nil}}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			for _, c := range tc.checks {
				if got := matchExpr(tc.expr, c.path, "", c.hasSibling, trim); got != c.want {
					t.Errorf("%s: %q, want %q", c.path, got, c.want)
				}
			}
		})
	}
}

func TestExpressionIsEmpty(t *testing.T) {
	for _, e := range []Expression{nil, {}, {{Pattern: ""}}, {{Pattern: "**/*.js", Disabled: true}}} {
		if !ParseExpression(e, Options{}).IsEmpty() {
			t.Errorf("%v must be empty", e)
		}
	}
	if ParseExpression(Expression{{Pattern: "a"}}, Options{}).IsEmpty() {
		t.Error("a is not empty")
	}
}

func TestExpressionFromSetting(t *testing.T) {
	got := ExpressionFromSetting(map[string]any{
		"**/.git":     true,
		"**/*.js":     map[string]any{"when": "$(basename).ts"},
		"**/*.off":    false,
		"**/*.banana": map[string]any{"banana": true},
		"**/*.zero":   0.0,
	})
	want := Expression{
		{Pattern: "**/*.banana"},
		{Pattern: "**/*.js", HasWhen: true, When: "$(basename).ts"},
		{Pattern: "**/*.off", Disabled: true},
		{Pattern: "**/*.zero"},
		{Pattern: "**/.git"},
	}
	if !slices.Equal(got, want) {
		t.Errorf("got %+v\nwant %+v", got, want)
	}
	if ExpressionFromSetting(nil) != nil || ExpressionFromSetting("x") != nil {
		t.Error("a value that is not an object gives no clauses")
	}
}

func TestPathHelpers(t *testing.T) {
	cases := []struct{ path, base, ext string }{
		{"a/b/c.txt", "c.txt", ".txt"},
		{"a/b/", "b", ""},
		{"a/b.tar.gz", "b.tar.gz", ".gz"},
		{".bashrc", ".bashrc", ""},
		{"dir/.bashrc", ".bashrc", ""},
		{"a.", "a.", "."},
		{"..", "..", ""},
		{"/", "", ""},
		{"", "", ""},
	}
	for _, tc := range cases {
		if got := baseName(tc.path); got != tc.base {
			t.Errorf("baseName(%q) = %q, want %q", tc.path, got, tc.base)
		}
		if got := extName(tc.path); got != tc.ext {
			t.Errorf("extName(%q) = %q, want %q", tc.path, got, tc.ext)
		}
	}
}
