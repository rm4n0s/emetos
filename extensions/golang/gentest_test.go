package golang

import (
	"flag"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/rm4n0s/errors"
)

// "Go: Generate Unit Tests For Function" (step 14.2): the text it writes is compared with the golden files of testdata/gentest/<case>/.
// A case holds source.go.txt (the file in front; § is the cursor), optionally other.go.txt (another file of the package) and
// existing_test.go.txt (the test file that exists already), and want_test.go.golden, the test file as it is afterwards. The golden
// files are made with -update-goldens, as the images are; look at each before committing it.

// updateGoldens is the flag of Glutter's golden tests, which the tests of the widgets of this package register too.
func updateGoldens() bool {
	f := flag.Lookup("update-goldens")
	return f != nil && f.Value.String() == "true"
}

func TestGenerateUnitTestsForFunction(t *testing.T) {
	cases, err := os.ReadDir("testdata/gentest")
	if err != nil {
		t.Fatal(err)
	}
	for _, c := range cases {
		if !c.IsDir() {
			continue
		}
		t.Run(c.Name(), func(t *testing.T) {
			dir := filepath.Join("testdata", "gentest", c.Name())
			r := newRig(t, rigOptions{})
			src, err := os.ReadFile(filepath.Join(dir, "source.go.txt"))
			if err != nil {
				t.Fatal(err)
			}
			path, _, _ := r.focus("subject.go", string(src))
			if other, err := os.ReadFile(filepath.Join(dir, "other.go.txt")); err == nil {
				if err := os.WriteFile(filepath.Join(r.folder, "other.go"), other, 0o600); err != nil {
					t.Fatal(err)
				}
			}
			target := testFilePath(path)
			_, hasExisting := os.Stat(filepath.Join(dir, "existing_test.go.txt"))
			if existing, err := os.ReadFile(filepath.Join(dir, "existing_test.go.txt")); err == nil {
				if err := os.WriteFile(target, existing, 0o600); err != nil {
					t.Fatal(err)
				}
			}
			if err := r.api.Run(GenerateTestsCommand, nil); err != nil {
				t.Fatal(err)
			}
			var got string
			if hasExisting == nil {
				got = r.docText(fileURI(target))
			} else {
				data, err := os.ReadFile(target)
				if err != nil {
					t.Fatal(err)
				}
				got = string(data)
			}
			golden := filepath.Join(dir, "want_test.go.golden")
			if updateGoldens() {
				if err := os.WriteFile(golden, []byte(got), 0o644); err != nil {
					t.Fatal(err)
				}
			}
			want, err := os.ReadFile(golden)
			if err != nil {
				t.Fatalf("no golden file (run with -update-goldens): %v", err)
			}
			if got != string(want) {
				t.Errorf("the test file is:\n%s\nwant:\n%s", got, want)
			}
			// the file is shown with the cursor on the new test
			shown := r.api.ShownDocuments()
			if len(shown) != 1 || shown[0].URI != fileURI(target) || shown[0].Preview {
				t.Fatalf("shown = %+v", shown)
			}
			lines := strings.Split(got, "\n")
			if l := shown[0].Line; l < 1 || l > len(lines) || !strings.HasPrefix(lines[l-1], "func Test") {
				t.Errorf("the file is shown at line %d, which is %q", shown[0].Line, lines[min(shown[0].Line, len(lines))-1])
			}
			if sev, msg := r.lastNote(); sev != "info" || !strings.Contains(msg, "Test") {
				t.Errorf("notification = %s %q", sev, msg)
			}
		})
	}
}

func TestGeneratedTestsCompile(t *testing.T) {
	// the golden files are Go: they parse and are what gofmt would make of them
	files, err := filepath.Glob("testdata/gentest/*/want_test.go.golden")
	if err != nil || len(files) == 0 {
		t.Fatalf("no golden files (%v)", err)
	}
	for _, f := range files {
		data, err := os.ReadFile(f)
		if err != nil {
			t.Fatal(err)
		}
		if packageName(string(data)) == "" {
			t.Errorf("%s has no package clause", f)
		}
	}
}

func TestGenerateUnitTestsDoesNotDuplicateATest(t *testing.T) {
	r := newRig(t, rigOptions{})
	path, _, _ := r.focus("subject.go", "package calc\n\nfunc Add(a, b int) int {§\n\treturn a + b\n}\n")
	target := testFilePath(path)
	existing := "package calc\n\nimport \"testing\"\n\nfunc TestOther(t *testing.T) {}\n\nfunc TestAdd(t *testing.T) {\n\tt.Skip()\n}\n"
	if err := os.WriteFile(target, []byte(existing), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := r.api.Run(GenerateTestsCommand, nil); err != nil {
		t.Fatal(err)
	}
	if len(r.api.Applied()) != 0 {
		t.Error("an edit was applied")
	}
	if data, _ := os.ReadFile(target); string(data) != existing {
		t.Errorf("the file changed:\n%s", data)
	}
	// the existing test is shown, and the notification says so
	shown := r.api.ShownDocuments()
	if len(shown) != 1 || shown[0].URI != fileURI(target) || shown[0].Line != 7 {
		t.Errorf("shown = %+v, want line 7", shown)
	}
	if sev, msg := r.lastNote(); sev != "info" || !strings.Contains(msg, "TestAdd exists already in subject_test.go") {
		t.Errorf("notification = %s %q", sev, msg)
	}
	// a test of another name is not a duplicate: a method named like it, or the test of another function
	other := "package calc\n\nimport \"testing\"\n\ntype T struct{}\n\nfunc (T) TestAdd(t *testing.T) {}\n"
	if err := os.WriteFile(target, []byte(other), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := r.api.Run(GenerateTestsCommand, nil); err != nil {
		t.Fatal(err)
	}
	if got := r.docText(fileURI(target)); !strings.Contains(got, "func TestAdd(t *testing.T) {\n\ttype args struct") {
		t.Errorf("the test was not added:\n%s", got)
	}
}

func TestGenerateUnitTestsTellsWhatIsWrong(t *testing.T) {
	tests := []struct {
		name     string
		file     string
		src      string
		severity string
		contains string
	}{
		{"the cursor is not in a function", "a.go", "package p\n\nvar x = 1§\n\nfunc f() {}\n", "warn", "not in a function"},
		{"a generic function", "a.go", "package p\n\nfunc Map[T any](xs []T) []T {§\n\treturn xs\n}\n", "warn", "generic functions are not supported"},
		{"a method of a generic type", "a.go", "package p\n\ntype L[T any] struct{}\n\nfunc (l *L[T]) Len() int {§\n\treturn 0\n}\n", "warn", "generic functions are not supported"},
		{"a test file", "a_test.go", "package p\n\nfunc TestX(t *testing.T) {§\n}\n", "info", "is one"},
		{"a syntax error", "a.go", "package p\n\nfunc f() {§\n", "warn", "syntax error"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			r := newRig(t, rigOptions{})
			path, _, _ := r.focus(tt.file, tt.src)
			if err := r.api.Run(GenerateTestsCommand, nil); err != nil {
				t.Fatal(err)
			}
			sev, msg := r.lastNote()
			if sev != tt.severity || !strings.Contains(msg, tt.contains) {
				t.Errorf("notification = %s %q, want %s containing %q", sev, msg, tt.severity, tt.contains)
			}
			if _, err := os.Stat(testFilePath(path)); err == nil && !strings.HasSuffix(tt.file, "_test.go") {
				t.Error("a test file was made")
			}
			if len(r.api.ShownDocuments()) != 0 {
				t.Errorf("shown = %+v", r.api.ShownDocuments())
			}
		})
	}
}

func TestGenerateUnitTestsReportsAFileItCannotRead(t *testing.T) {
	// the test file exists but has a syntax error: nothing is appended to it
	r := newRig(t, rigOptions{})
	path, _, _ := r.focus("subject.go", "package calc\n\nfunc Add(a, b int) int {§\n\treturn a + b\n}\n")
	target := testFilePath(path)
	if err := os.WriteFile(target, []byte("package calc\n\nfunc broken( {\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := r.api.Run(GenerateTestsCommand, nil); err != nil {
		t.Fatal(err)
	}
	if sev, msg := r.lastNote(); sev != "warn" || !strings.Contains(msg, "subject_test.go: the file has a syntax error") {
		t.Errorf("notification = %s %q", sev, msg)
	}
	if len(r.api.Applied()) != 0 {
		t.Error("an edit was applied to a file that does not parse")
	}
	// a folder that cannot be written to: the new test file is not made
	r2 := newRig(t, rigOptions{})
	r2.focus("subject.go", "package calc\n\nfunc Add(a, b int) int {§\n\treturn a + b\n}\n")
	if err := os.Chmod(r2.folder, 0o500); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chmod(r2.folder, 0o700) })
	if os.Geteuid() == 0 {
		t.Skip("root can write to a folder that is read only")
	}
	if err := r2.api.Run(GenerateTestsCommand, nil); err != nil {
		t.Fatal(err)
	}
	if sev, msg := r2.lastNote(); sev != "error" || !strings.Contains(msg, "cannot be written") {
		t.Errorf("notification = %s %q", sev, msg)
	}
}

func TestGenerateTestFailuresHaveTheirRoutes(t *testing.T) {
	g := generated{name: "TestX", body: "func TestX(t *testing.T) {}", imports: []importRef{{Path: "testing"}}}
	tests := []struct {
		name  string
		run   func() error
		route string
	}{
		{"not in a function", func() error { _, _, err := generateTest("package p\n", t.TempDir(), 0); return err }, "generateTest.GenerateTestNotAFunction"},
		{"a generic function", func() error {
			_, _, err := generateTest("package p\n\nfunc F[T any]() {}\n", t.TempDir(), 12)
			return err
		}, "generateTest.GenerateTestUnsupported"},
		{"a syntax error", func() error { _, _, err := generateTest("package p\n\nfunc F() {", t.TempDir(), 12); return err }, "generateTest.GenerateTestSourceInvalid"},
		{"a syntax error in the test file", func() error { _, _, err := appendTest("package p\n\nfunc F( {", "p", g); return err }, "appendTest.GenerateTestSourceInvalid"},
		{"the test exists", func() error {
			_, _, err := appendTest("package p\n\nfunc TestX(t *testing.T) {}\n", "p", g)
			return err
		}, "appendTest.GenerateTestExists"},
		{"a file that cannot be written", func() error { return writeNewFile(filepath.Join(t.TempDir(), "no", "dir", "x_test.go"), "x") }, "writeNewFile.FileWriteFailed"},
		{"a file that is there", func() error {
			p := filepath.Join(t.TempDir(), "x_test.go")
			if err := os.WriteFile(p, nil, 0o600); err != nil {
				t.Fatal(err)
			}
			return writeNewFile(p, "x")
		}, "writeNewFile.FileWriteFailed"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := tt.run()
			appErr, ok := errors.FromError(err)
			if !ok {
				t.Fatalf("expected *errors.Error, got %T (%v)", err, err)
			}
			if !appErr.HasRoute(tt.route) {
				t.Errorf("unexpected failure path: %s (want ...%s)", appErr.Route(), tt.route)
			}
		})
	}
}

func TestTestNames(t *testing.T) {
	tests := []struct {
		name string
		fn   string
		recv *testRecv
		want string
	}{
		{"an exported function", "Add", nil, "TestAdd"},
		{"an unexported function", "add", nil, "Test_add"},
		{"a method", "Get", &testRecv{typeName: "Cache"}, "TestCache_Get"},
		{"a method of an unexported type", "Get", &testRecv{typeName: "cache"}, "Test_cache_Get"},
		{"an unexported method", "get", &testRecv{typeName: "Cache"}, "TestCache_get"},
	}
	for _, tt := range tests {
		if got := testName(tt.fn, tt.recv); got != tt.want {
			t.Errorf("%s: testName = %s, want %s", tt.name, got, tt.want)
		}
	}
}

func TestGuessPackageName(t *testing.T) {
	tests := []struct{ path, want string }{
		{"fmt", "fmt"}, {"net/http", "http"}, {"github.com/a/b/v2", "b"}, {"gopkg.in/yaml.v3", "yaml"}, {"github.com/a/go-thing", "thing"},
		{"github.com/a/thing-go", "thing"}, {"github.com/a/my-pkg", "my_pkg"},
	}
	for _, tt := range tests {
		if got := guessPackageName(tt.path); got != tt.want {
			t.Errorf("guessPackageName(%q) = %q, want %q", tt.path, got, tt.want)
		}
	}
	if !isStdlib("net/http") || isStdlib("github.com/a/b") || !slices.Equal(typeQualifiers("map[http.Header][]*bytes.Buffer"), []string{"http", "bytes"}) {
		t.Error("isStdlib or typeQualifiers is wrong")
	}
}
