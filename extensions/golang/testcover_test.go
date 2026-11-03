package golang

import (
	"context"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/rm4n0s/errors"

	"github.com/rm4n0s/emetos/extension"
	"github.com/rm4n0s/emetos/extensions/internal/fakeapi"
)

// Coverage from a Go coverage profile (step 26.5): the parser, the sums, the functions and the details. The profile of the real go is in
// testreal_test.go.

const coverSource = `package p

func Sign(n int) string {
	if n < 0 {
		return "neg"
	}
	if n == 0 {
		return "zero"
	}
	return "pos"
}

func Unused() int {
	return 1
}

func Empty() {}
`

// coverProfile is what go test -coverprofile writes for coverSource when only -1 and 1 are tried.
const coverProfile = `mode: count
example.com/p/p.go:3.25,4.10 1 2
example.com/p/p.go:4.10,6.3 1 1
example.com/p/p.go:7.12,9.3 1 0
example.com/p/p.go:10.2,10.14 1 1
example.com/p/p.go:13.20,15.2 1 0
`

func TestParseCoverProfileReadsTheBlocks(t *testing.T) {
	got, err := parseCoverProfile(coverProfile + "\nexample.com/p/q.go:1.1,2.2 3 4\n")
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 2 || len(got["example.com/p/p.go"]) != 5 {
		t.Fatalf("blocks = %+v", got)
	}
	if b := got["example.com/p/q.go"][0]; b != (coverBlock{startLine: 1, startCol: 1, endLine: 2, endCol: 2, stmts: 3, count: 4}) {
		t.Errorf("block = %+v", b)
	}
	// a colon in the folder is part of the file
	if got, err := parseCoverProfile("mode: set\n/w/a:b/p.go:1.1,2.2 1 1\n"); err != nil || len(got["/w/a:b/p.go"]) != 1 {
		t.Errorf("a colon in a folder: %+v, %v", got, err)
	}
}

func TestParseCoverProfileFailsWithItsTag(t *testing.T) {
	for name, text := range map[string]string{
		"no colon":     "just text\n",
		"no counts":    "p.go:1.1,2.2\n",
		"no end":       "p.go:1.1 1 1\n",
		"not a number": "p.go:1.x,2.2 1 1\n",
		"a negative":   "p.go:1.1,2.2 1 -1\n",
		"too few":      "p.go:1.1,2.2 1\n",
	} {
		t.Run(name, func(t *testing.T) {
			_, err := parseCoverProfile(text)
			appErr, ok := errors.FromError(err)
			if !ok {
				t.Fatalf("expected *errors.Error, got %T: %v", err, err)
			}
			if !appErr.HasRoute("parseCoverProfile.GoCoverProfileInvalid") {
				t.Errorf("unexpected failure path: %s", appErr.Route())
			}
		})
	}
}

func TestBlocksOfTwoProfilesAreMergedByAddingTheirCounts(t *testing.T) {
	a, _ := parseCoverProfile("mode: count\nf.go:1.1,2.2 1 1\nf.go:3.1,4.2 2 0\n")
	b, _ := parseCoverProfile("mode: count\nf.go:1.1,2.2 1 2\nf.go:5.1,6.2 1 7\ng.go:1.1,2.2 1 1\n")
	mergeBlocks(a, b)
	if len(a["f.go"]) != 3 || a["f.go"][0].count != 3 || a["f.go"][1].count != 0 || a["f.go"][2].count != 7 || len(a["g.go"]) != 1 {
		t.Errorf("merged = %+v", a)
	}
}

func TestTheCoverageOfAFileIsTheSumsOfItsBlocksAndItsFunctions(t *testing.T) {
	blocks, _ := parseCoverProfile(coverProfile)
	fc := fileCoverage("file:///w/p.go", coverSource, blocks["example.com/p/p.go"])
	// 5 blocks of one statement; 3 ran
	if fc.Statement != (extension.CoverageCount{Covered: 3, Total: 5}) {
		t.Errorf("statements = %+v", fc.Statement)
	}
	// Sign ran, Unused did not; Empty has no statement
	if fc.Declaration == nil || *fc.Declaration != (extension.CoverageCount{Covered: 1, Total: 2}) {
		t.Errorf("declarations = %+v", fc.Declaration)
	}
	if fc.Branch != nil {
		t.Errorf("branches = %+v", fc.Branch)
	}
	d, err := loadGoCoverage(context.Background(), fc)
	if err != nil {
		t.Fatal(err)
	}
	var ran []int
	for _, s := range d.Statements {
		ran = append(ran, s.Executed)
	}
	// the statements are in the order of the file, with the places of the blocks (from 0)
	if !slices.Equal(ran, []int{2, 1, 0, 1, 0}) || d.Statements[0].Location != (extension.Range{Start: extension.Position{Line: 2, Col: 24}, End: extension.Position{Line: 3, Col: 9}}) {
		t.Errorf("statements = %+v", d.Statements)
	}
	if len(d.Declarations) != 2 || d.Declarations[0].Name != "Sign" || d.Declarations[0].Executed != 2 || d.Declarations[1].Name != "Unused" || d.Declarations[1].Executed != 0 ||
		d.Declarations[0].Location.Start.Line != 2 || d.Declarations[0].Location.End.Line != 10 {
		t.Errorf("declarations = %+v", d.Declarations)
	}
	// without the source there are no functions
	if fc := fileCoverage("file:///w/p.go", "", blocks["example.com/p/p.go"]); fc.Declaration != nil || fc.Statement.Total != 5 {
		t.Errorf("no source: %+v", fc)
	}
}

func TestLoadingTheCoverageOfAFileThatIsNotOursFailsWithItsTag(t *testing.T) {
	for _, data := range []any{nil, "x", (*goCoverFile)(nil)} {
		_, err := loadGoCoverage(context.Background(), extension.FileCoverage{URI: "file:///w/x.go", Data: data})
		appErr, ok := errors.FromError(err)
		if !ok {
			t.Fatalf("%v: expected *errors.Error, got %T", data, err)
		}
		if !appErr.HasRoute("loadGoCoverage.GoCoverageNoData") {
			t.Errorf("unexpected failure path: %s", appErr.Route())
		}
	}
}

func TestACoverageProfileOfAModuleIsReportedFileByFile(t *testing.T) {
	r := newRig(t, rigOptions{withGo: true, files: map[string]string{
		"p.go":          coverSource,
		"p_test.go":     testSource("p", "func TestSign(t *testing.T) {}"),
		"sub/s.go":      "package sub\n\nfunc F() int {\n\treturn 1\n}\n",
		"sub/s_test.go": testSource("sub", "func TestF(t *testing.T) {}"),
	}})
	r.discovered()
	dir := t.TempDir()
	one, two := filepath.Join(dir, "one.out"), filepath.Join(dir, "two.out")
	for path, text := range map[string]string{
		one: "mode: count\nexample.com/p/p.go:3.25,4.10 1 1\nexample.com/p/sub/s.go:3.14,5.2 1 1\nelsewhere.example/x/y.go:1.1,2.2 1 1\n",
		two: "mode: count\nexample.com/p/p.go:3.25,4.10 1 1\nexample.com/p/p.go:13.20,15.2 1 0\n",
	} {
		if err := os.WriteFile(path, []byte(text), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	run, err := r.ctl().CreateTestRun(&extension.TestRunRequest{}, "x", false)
	if err != nil {
		t.Fatal(err)
	}
	r.ext.tests.reportCoverage(run, []string{one, filepath.Join(dir, "missing.out"), two})
	run.End()
	var files []fakeapi.TestRunEvent
	for _, e := range r.ctl().Runs()[0].Events() {
		if e.Kind == fakeapi.RunCoverage {
			files = append(files, e)
		}
	}
	// the file of the folder of a package, the file of another package, none for a path that is in no module of the folder
	if len(files) != 2 {
		t.Fatalf("%d files: %+v", len(files), files)
	}
	p, s := files[0].Coverage, files[1].Coverage
	if p.URI != fileURI(filepath.Join(r.folder, "p.go")) || p.Statement != (extension.CoverageCount{Covered: 1, Total: 2}) {
		t.Errorf("p.go = %+v", p)
	}
	if s.URI != fileURI(filepath.Join(r.folder, "sub", "s.go")) || s.Statement != (extension.CoverageCount{Covered: 1, Total: 1}) || s.Declaration == nil || s.Declaration.Total != 1 {
		t.Errorf("s.go = %+v", s)
	}
	d, err := profileOf(r, extension.TestKindCoverage).LoadCoverage(context.Background(), files[0].Coverage)
	if err != nil || len(d.Statements) != 2 {
		t.Errorf("details = %+v, %v", d, err)
	}
}

func TestAProfileThatCannotBeParsedIsToldToTheUser(t *testing.T) {
	r := newRig(t, rigOptions{withGo: true, files: map[string]string{"p_test.go": testSource("p", "func TestSign(t *testing.T) {}")}})
	r.discovered()
	bad := filepath.Join(t.TempDir(), "bad.out")
	if err := os.WriteFile(bad, []byte("mode: set\nnot a block\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	run, err := r.ctl().CreateTestRun(&extension.TestRunRequest{}, "x", false)
	if err != nil {
		t.Fatal(err)
	}
	r.ext.tests.reportCoverage(run, []string{bad})
	if sev, msg := r.lastNote(); sev != "warn" || !strings.Contains(msg, "The coverage profile cannot be read") {
		t.Errorf("note %s %q", sev, msg)
	}
}

func TestTheValuesOfAGotWantMessage(t *testing.T) {
	for _, tt := range []struct{ msg, expected, actual string }{
		{"Add(2, 2) = 4, want 5", "5", "4"},
		{"got 4, want 5", "5", "4"},
		{"Add: got 4 want 5", "5", "4"},
		{"got: 4, want: 5", "5", "4"},
		{"unexpected error: boom", "", ""},
		{"got 1\nwant 2", "", ""},
		{"", "", ""},
	} {
		expected, actual := splitWant(tt.msg)
		if expected != tt.expected || actual != tt.actual {
			t.Errorf("splitWant(%q) = %q, %q; want %q, %q", tt.msg, expected, actual, tt.expected, tt.actual)
		}
	}
}

func TestTheRunPatternOfASubtest(t *testing.T) {
	for in, want := range map[string]string{
		"TestB/x": "^TestB$/^x$", "TestB/a_b/deep": "^TestB$/^a_b$/^deep$", "TestB/n=10": "^TestB$/^n=10$", "TestB/a.b": `^TestB$/^a\.b$`, "TestB/x#01": "^TestB$/^x#01$",
	} {
		if got := subPattern(in); got != want {
			t.Errorf("subPattern(%q) = %q, want %q", in, got, want)
		}
	}
}
