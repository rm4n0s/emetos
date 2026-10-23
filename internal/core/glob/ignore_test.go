package glob

import "testing"

type ignoreFile struct{ dir, text string }

type ignoreQuery struct {
	path string
	dir  bool
	want bool
}

func f(path string, want bool) ignoreQuery { return ignoreQuery{path, false, want} }
func d(path string, want bool) ignoreQuery { return ignoreQuery{path, true, want} }

// ignoreCases are gitignore semantics, from the rules of https://git-scm.com/docs/gitignore.
var ignoreCases = []struct {
	name    string
	files   []ignoreFile
	opts    IgnoreOptions
	queries []ignoreQuery
}{
	{"empty", nil, IgnoreOptions{}, []ignoreQuery{f("a", false), d("b", false)}},
	{"comments and blank lines", []ignoreFile{{"", "# comment\n\n   \n*.log\n"}}, IgnoreOptions{},
		[]ignoreQuery{f("a.log", true), f("# comment", false), f("a.txt", false)}},
	{"escaped hash and bang", []ignoreFile{{"", "\\#file\n\\!important\n"}}, IgnoreOptions{},
		[]ignoreQuery{f("#file", true), f("!important", true), f("file", false)}},
	{"trailing spaces", []ignoreFile{{"", "foo   \nbar\\ \n"}}, IgnoreOptions{},
		[]ignoreQuery{f("foo", true), f("foo   ", false), f("bar ", true), f("bar", false)}},
	{"windows line endings", []ignoreFile{{"", "*.log\r\nbuild/\r\n"}}, IgnoreOptions{},
		[]ignoreQuery{f("a.log", true), d("build", true), f("build", false)}},
	{"byte order mark", []ignoreFile{{"", "\ufeff*.log\n"}}, IgnoreOptions{},
		[]ignoreQuery{f("a.log", true)}},
	{"name matches at every depth", []ignoreFile{{"", "node_modules\n"}}, IgnoreOptions{},
		[]ignoreQuery{f("node_modules", true), d("node_modules", true), f("a/node_modules", true), f("a/b/node_modules", true),
			f("node_modules/x.js", true), f("a/node_modules/x.js", true), f("my_node_modules", false)}},
	{"directory only", []ignoreFile{{"", "build/\n"}}, IgnoreOptions{},
		[]ignoreQuery{d("build", true), f("build", false), d("a/build", true), f("build/out.o", true), f("a/build/out.o", true), f("a/build", false)}},
	{"leading slash anchors", []ignoreFile{{"", "/todo\n"}}, IgnoreOptions{},
		[]ignoreQuery{f("todo", true), f("a/todo", false), f("todo/x", true)}},
	{"slash in the middle anchors", []ignoreFile{{"", "doc/frotz\n"}}, IgnoreOptions{},
		[]ignoreQuery{f("doc/frotz", true), f("a/doc/frotz", false), f("doc/frotz/x", true), f("frotz", false)}},
	{"anchored directory", []ignoreFile{{"", "/build/\n"}}, IgnoreOptions{},
		[]ignoreQuery{d("build", true), d("a/build", false), f("build/x", true)}},
	{"star does not cross slashes", []ignoreFile{{"", "doc/*.txt\n"}}, IgnoreOptions{},
		[]ignoreQuery{f("doc/a.txt", true), f("doc/a/b.txt", false), f("doc/.txt", true), f("doc/a.md", false)}},
	{"question mark", []ignoreFile{{"", "a?c\n"}}, IgnoreOptions{},
		[]ignoreQuery{f("abc", true), f("ac", false), f("abbc", false), f("a/c", false), f("x/abc", true)}},
	{"character classes", []ignoreFile{{"", "*.[oa]\nfile[0-9]\ntemp[!ab]\nx[^c]\n[[:upper:]]z\n"}}, IgnoreOptions{},
		[]ignoreQuery{f("x.o", true), f("x.a", true), f("x.c", false), f("file3", true), f("fileA", false),
			f("tempc", true), f("tempa", false), f("xd", true), f("xc", false), f("Az", true), f("az", false)}},
	{"unclosed bracket matches nothing", []ignoreFile{{"", "a[b\n*.o\n"}}, IgnoreOptions{},
		[]ignoreQuery{f("a[b", false), f("ab", false), f("x.o", true)}},
	{"bracket with a closing bracket first", []ignoreFile{{"", "a[]x]b\n"}}, IgnoreOptions{},
		[]ignoreQuery{f("a]b", true), f("axb", true), f("ab", false)}},
	{"leading globstar", []ignoreFile{{"", "**/foo\n"}}, IgnoreOptions{},
		[]ignoreQuery{f("foo", true), f("a/foo", true), f("a/b/foo", true), f("foo/x", true), f("a/foobar", false)}},
	{"trailing globstar", []ignoreFile{{"", "abc/**\n"}}, IgnoreOptions{},
		[]ignoreQuery{f("abc/x", true), f("abc/x/y", true), d("abc", false), f("a/abc/x", false)}},
	{"middle globstar", []ignoreFile{{"", "a/**/b\n"}}, IgnoreOptions{},
		[]ignoreQuery{f("a/b", true), f("a/x/b", true), f("a/x/y/b", true), f("b", false), f("a/x/c", false)}},
	{"globstar alone", []ignoreFile{{"", "**\n"}}, IgnoreOptions{},
		[]ignoreQuery{f("a", true), f("a/b", true)}},
	{"other double stars are plain stars", []ignoreFile{{"", "a**b\n"}}, IgnoreOptions{},
		[]ignoreQuery{f("ab", true), f("axxb", true), f("a/b", false)}},
	{"negation", []ignoreFile{{"", "*.log\n!keep.log\n"}}, IgnoreOptions{},
		[]ignoreQuery{f("a.log", true), f("keep.log", false), f("dir/keep.log", false), f("dir/b.log", true)}},
	{"the last pattern wins", []ignoreFile{{"", "!keep.log\n*.log\n"}}, IgnoreOptions{},
		[]ignoreQuery{f("keep.log", true)}},
	{"negation again", []ignoreFile{{"", "*.log\n!keep.log\nkeep.log\n"}}, IgnoreOptions{},
		[]ignoreQuery{f("keep.log", true)}},
	{"cannot re-include below an excluded folder", []ignoreFile{{"", "build/\n!build/keep.txt\n"}}, IgnoreOptions{},
		[]ignoreQuery{d("build", true), f("build/keep.txt", true), f("build/other", true)}},
	{"can re-include when the folder is not excluded", []ignoreFile{{"", "build/*\n!build/keep.txt\n"}}, IgnoreOptions{},
		[]ignoreQuery{d("build", false), f("build/other", true), f("build/keep.txt", false)}},
	{"re-include a folder and its content", []ignoreFile{{"", "/*\n!/src\n"}}, IgnoreOptions{},
		[]ignoreQuery{f("README", true), d("src", false), f("src/main.go", false), d("docs", true), f("docs/a.md", true)}},
	{"nested file is relative to its folder", []ignoreFile{{"", "*.log\n"}, {"src", "/gen\nlocal/*.txt\n"}}, IgnoreOptions{},
		[]ignoreQuery{f("src/gen", true), f("gen", false), f("src/a/gen", false), f("src/local/a.txt", true), f("local/a.txt", false), f("src/x.log", true)}},
	{"deeper file overrides", []ignoreFile{{"", "*.log\n"}, {"src", "!*.log\n"}, {"src/lib", "*.log\n"}}, IgnoreOptions{},
		[]ignoreQuery{f("a.log", true), f("src/a.log", false), f("src/lib/a.log", true), f("src/b/a.log", false)}},
	{"later file of the same folder overrides", []ignoreFile{{"", "*.log\n"}, {"", "!a.log\n"}}, IgnoreOptions{},
		[]ignoreQuery{f("a.log", false), f("b.log", true)}},
	{"folder with slashes around it", []ignoreFile{{"", "*.log\n"}, {"/src/", "!keep.log\n"}}, IgnoreOptions{},
		[]ignoreQuery{f("src/keep.log", false), f("src/other.log", true)}},
	{"case sensitive on Linux", []ignoreFile{{"", "README\n"}}, IgnoreOptions{},
		[]ignoreQuery{f("README", true), f("readme", false)}},
	{"ignore case when asked", []ignoreFile{{"", "README\n"}}, IgnoreOptions{IgnoreCase: true},
		[]ignoreQuery{f("README", true), f("readme", true)}},
	{"unicode names", []ignoreFile{{"", "café?\n"}}, IgnoreOptions{},
		[]ignoreQuery{f("café1", true), f("caf1", false)}},
	{"dot files", []ignoreFile{{"", ".*\n!.gitignore\n"}}, IgnoreOptions{},
		[]ignoreQuery{f(".env", true), f(".gitignore", false), f("a/.env", true), f("env", false)}},
	{"invalid trailing backslash is skipped", []ignoreFile{{"", "abc\\\n*.o\n"}}, IgnoreOptions{},
		[]ignoreQuery{f("abc", false), f("x.o", true)}},
}

func TestIgnore(t *testing.T) {
	for _, tc := range ignoreCases {
		t.Run(tc.name, func(t *testing.T) {
			ig := NewIgnore(tc.opts)
			for _, file := range tc.files {
				ig.AddFile(file.dir, file.text)
			}
			for _, q := range tc.queries {
				if got := ig.Ignored(q.path, q.dir); got != q.want {
					t.Errorf("Ignored(%q, dir=%v) = %v, want %v", q.path, q.dir, got, q.want)
				}
			}
		})
	}
}

func TestIgnoreGlobalHasLowestPriority(t *testing.T) {
	ig := NewIgnore(IgnoreOptions{})
	ig.AddGlobal("*.swp\n*.tmp\n")
	ig.AddFile("", "!keep.tmp\n")
	cases := []ignoreQuery{f("a.swp", true), f("a.tmp", true), f("keep.tmp", false), f("src/b.swp", true)}
	for _, q := range cases {
		if got := ig.Ignored(q.path, q.dir); got != q.want {
			t.Errorf("Ignored(%q) = %v, want %v", q.path, got, q.want)
		}
	}
}

func TestIgnoreCheck(t *testing.T) {
	ig := NewIgnore(IgnoreOptions{})
	ig.AddFile("", "build/\n!build/keep\n")
	// Check looks at one path only, without its parents.
	if ignored, matched := ig.Check("build/other", false); ignored || matched {
		t.Errorf("a file below an excluded folder has no pattern of its own: %v %v", ignored, matched)
	}
	if ignored, matched := ig.Check("build", true); !ignored || !matched {
		t.Errorf("build: %v %v", ignored, matched)
	}
	if ignored, matched := ig.Check("build/keep", false); ignored || !matched {
		t.Errorf("build/keep: %v %v", ignored, matched)
	}
	if ignored, matched := ig.Check("", true); ignored || matched {
		t.Errorf("the root: %v %v", ignored, matched)
	}
	if ig.Ignored("", true) || ig.Ignored("/", true) {
		t.Error("the root is never ignored")
	}
	if !ig.Ignored("/build/", true) {
		t.Error("slashes around a path are ignored")
	}
}
