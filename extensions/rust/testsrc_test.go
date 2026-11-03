package rust

import (
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"testing"
)

// The scan of the sources for the places of the tests (step 26.7).

// scanFiles writes the files under a folder and scans the crate whose root is lib.rs; it returns "kind name file:line" lines, sorted.
func scanFiles(t *testing.T, files map[string]string) []string {
	t.Helper()
	dir := t.TempDir()
	for name, text := range files {
		writeFile(t, dir, name, text, 0o644)
	}
	idx := scanTarget(filepath.Join(dir, "src/lib.rs"))
	var out []string
	for name, p := range idx.tests {
		rel, _ := filepath.Rel(dir, p.File)
		out = append(out, "test "+name+" "+rel+":"+strconv.Itoa(p.Line))
	}
	for name, p := range idx.mods {
		rel, _ := filepath.Rel(dir, p.File)
		out = append(out, "mod "+name+" "+rel+":"+strconv.Itoa(p.Line))
	}
	sort.Strings(out)
	return out
}

func TestTheScanFindsTestsModulesAndFiles(t *testing.T) {
	for _, tc := range []struct {
		name  string
		files map[string]string
		want  string
	}{
		{"a test and its module", map[string]string{"src/lib.rs": "pub fn f() {}\n\n#[cfg(test)]\nmod tests {\n    #[test]\n    fn a() {}\n\n    #[test]\n    #[ignore]\n    fn b() {}\n\n    fn helper() {}\n}\n"},
			"mod tests src/lib.rs:4\ntest tests::a src/lib.rs:6\ntest tests::b src/lib.rs:10"},
		{"a test at the top of the crate", map[string]string{"src/lib.rs": "#[test]\nfn top() {}\n"}, "test top src/lib.rs:2"},
		{"attributes of other crates and a path", map[string]string{"src/lib.rs": "#[tokio::test]\nasync fn a() {}\n#[test_case::test]\npub(crate) fn b() {}\n#[rstest]\nfn c() {}\n#[derive(Debug)]\nstruct S;\nfn d() {}\n"},
			"test a src/lib.rs:2\ntest b src/lib.rs:4\ntest c src/lib.rs:6"},
		{"nested modules", map[string]string{"src/lib.rs": "mod a {\n    mod b {\n        #[test]\n        fn t() {}\n    }\n    #[test]\n    fn u() {}\n}\n"},
			"mod a src/lib.rs:1\nmod a::b src/lib.rs:2\ntest a::b::t src/lib.rs:4\ntest a::u src/lib.rs:7"},
		{"modules in files", map[string]string{"src/lib.rs": "mod foo;\nmod bar;\n", "src/foo.rs": "#[test]\nfn t() {}\nmod deep;\n", "src/foo/deep.rs": "#[test]\nfn d() {}\n", "src/bar/mod.rs": "#[test]\nfn b() {}\n"},
			"mod bar src/bar/mod.rs:1\nmod foo src/foo.rs:1\nmod foo::deep src/foo/deep.rs:1\ntest bar::b src/bar/mod.rs:2\ntest foo::deep::d src/foo/deep.rs:2\ntest foo::t src/foo.rs:2"},
		{"a missing file", map[string]string{"src/lib.rs": "mod gone;\n"}, "mod gone src/gone.rs:1"},
		{"comments and strings do not count", map[string]string{"src/lib.rs": "// #[test]\n/* #[test] /* nested */ fn x() {} */\nconst S: &str = \"#[test] }\";\nconst R: &str = r#\"fn y() { \"#;\nconst C: char = '}';\nfn l<'a>(x: &'a str) {}\n#[test]\nfn real() {}\n"},
			"test real src/lib.rs:8"},
		{"a test inside a function body is not a test of the module", map[string]string{"src/lib.rs": "fn outer() {\n    #[test]\n    fn inner() {}\n}\n#[test]\nfn ok() {}\n"}, "test ok src/lib.rs:6"},
		{"a module declared in a module of a file", map[string]string{"src/lib.rs": "mod a {\n    mod b;\n}\n", "src/a/b.rs": "#[test]\nfn t() {}\n"},
			"mod a src/lib.rs:1\nmod a::b src/a/b.rs:1\ntest a::b::t src/a/b.rs:2"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if got := strings.Join(scanFiles(t, tc.files), "\n"); got != tc.want {
				t.Errorf("got\n%s\nwant\n%s", got, tc.want)
			}
		})
	}
}

func TestTheScanStopsOnAModuleThatIncludesItself(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, dir, "src/lib.rs", "mod a;\n", 0o644)
	writeFile(t, dir, "src/a.rs", "mod a;\n#[test]\nfn t() {}\n", 0o644)
	if _, err := os.Stat(filepath.Join(dir, "src/a/a.rs")); err == nil {
		t.Fatal("unexpected file")
	}
	idx := scanTarget(filepath.Join(dir, "src/lib.rs"))
	if _, ok := idx.tests["a::t"]; !ok {
		t.Errorf("tests %v", idx.tests)
	}
}
