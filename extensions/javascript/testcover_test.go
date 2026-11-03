package javascript

import (
	"context"
	"strings"
	"testing"

	"github.com/rm4n0s/errors"

	"github.com/rm4n0s/emetos/extension"
)

func TestParseIstanbulReadsTheRecordedCoverage(t *testing.T) {
	for _, name := range []string{"jest_coverage.json", "vitest_coverage.json"} {
		files, err := parseIstanbul([]byte(recorded(t, name, "/p")), "/p", nil)
		if err != nil {
			t.Fatal(err)
		}
		if len(files) != 1 || files[0].URI != "file:///p/src/"+map[string]string{"jest_coverage.json": "math.js", "vitest_coverage.json": "math.ts"}[name] {
			t.Fatalf("%s: %+v", name, files)
		}
		f := files[0]
		if f.Statement.Total != map[string]int{"jest_coverage.json": 6, "vitest_coverage.json": 5}[name] || f.Statement.Covered != f.Statement.Total-1 {
			t.Errorf("%s statements %+v", name, f.Statement)
		}
		if f.Branch == nil || f.Branch.Total == 0 || f.Declaration == nil || *f.Declaration != (extension.CoverageCount{Covered: 2, Total: 3}) {
			t.Errorf("%s: branch %+v, declarations %+v", name, f.Branch, f.Declaration)
		}
		d, err := loadJSCoverage(context.Background(), f)
		if err != nil {
			t.Fatal(err)
		}
		if len(d.Statements) != f.Statement.Total || len(d.Declarations) != 3 {
			t.Fatalf("%s: %d statements, %d declarations", name, len(d.Statements), len(d.Declarations))
		}
		// the statement at line 6 (index 1) is the `if`, with its branches; the statement of line 13 never ran
		if s := d.Statements[1]; s.Location.Start.Line != 5 || s.Executed != 2 || len(s.Branches) == 0 || !strings.Contains(s.Branches[0].Label, "branch 1") {
			t.Errorf("%s: statement %+v", name, s)
		}
		last := d.Statements[len(d.Statements)-1]
		if name == "vitest_coverage.json" && (last.Executed != 0 || last.Location.Start.Line != 12) {
			t.Errorf("%s: last %+v", name, last)
		}
		if got := d.Declarations[2]; got.Name != "unused" || got.Executed != 0 || got.Location.Start.Line != 11 || got.Location.End.Line != 13 {
			t.Errorf("%s: declaration %+v", name, got)
		}
	}
}

func TestParseIstanbulSkipsAndResolves(t *testing.T) {
	data := `{"/p/node_modules/x/i.js":{"path":"/p/node_modules/x/i.js","statementMap":{},"s":{}},
	"a.js":{"statementMap":{"0":{"start":{"line":1,"column":0},"end":{"line":1,"column":5}}},"s":{"0":1}},
	"/p/skipme.js":{"path":"/p/skipme.js","statementMap":{},"s":{}}}`
	files, err := parseIstanbul([]byte(data), "/p", func(p string) bool { return p == "/p/skipme.js" })
	if err != nil {
		t.Fatal(err)
	}
	if len(files) != 1 || files[0].URI != "file:///p/a.js" || files[0].Statement != (extension.CoverageCount{Covered: 1, Total: 1}) || files[0].Branch != nil {
		t.Errorf("%+v", files)
	}
}

func TestParseIstanbulRefusesWhatIsNoCoverage(t *testing.T) {
	for _, text := range []string{"", "not json", `[1]`, `{"a": 1}`} {
		_, err := parseIstanbul([]byte(text), "/p", nil)
		appErr, ok := errors.FromError(err)
		if !ok {
			t.Errorf("%q: expected *errors.Error, got %T", text, err)
			continue
		}
		if !appErr.HasRoute("parseIstanbul.JsCoverageJSONInvalid") {
			t.Errorf("%q: unexpected failure path: %s", text, appErr.Route())
		}
	}
}

func TestParseLcovReadsNodesCoverage(t *testing.T) {
	skip := func(p string) bool { return strings.HasSuffix(p, "math.test.js") }
	files := parseLcov([]byte(recorded(t, "node_coverage.lcov", "/p")), "/p", skip)
	if len(files) != 1 || files[0].URI != "file:///p/src/math.js" {
		t.Fatalf("%+v", files)
	}
	f := files[0]
	if f.Statement != (extension.CoverageCount{Covered: 13, Total: 16}) || f.Branch == nil || *f.Branch != (extension.CoverageCount{Covered: 4, Total: 4}) ||
		f.Declaration == nil || *f.Declaration != (extension.CoverageCount{Covered: 2, Total: 3}) {
		t.Errorf("counts %+v %+v %+v", f.Statement, f.Branch, f.Declaration)
	}
	d, err := loadJSCoverage(context.Background(), f)
	if err != nil {
		t.Fatal(err)
	}
	// line 12 never ran; line 5 has a branch
	byLine := map[int]extension.StatementCoverage{}
	for _, s := range d.Statements {
		byLine[s.Location.Start.Line+1] = s
	}
	if byLine[12].Executed != 0 || byLine[5].Executed != 2 || len(byLine[5].Branches) != 1 || byLine[5].Branches[0].Executed != 2 {
		t.Errorf("line 12 %+v, line 5 %+v", byLine[12], byLine[5])
	}
	if len(d.Declarations) != 3 || d.Declarations[2].Name != "unused" || d.Declarations[2].Executed != 0 || d.Declarations[2].Location.Start.Line != 11 {
		t.Errorf("declarations %+v", d.Declarations)
	}
	both := parseLcov([]byte(recorded(t, "node_coverage.lcov", "/p")), "/p", nil)
	if len(both) != 2 {
		t.Errorf("without the skip: %d files", len(both))
	}
}

func TestParseLcovToleratesNoise(t *testing.T) {
	files := parseLcov([]byte("garbage\nTN:\nSF:a.js\nDA:x,1\nDA:3,2\nBRDA:3,0,0,-\nFN:bad\nFNDA:1,nofn\nend_of_record\nDA:9,9\n"), "/p", nil)
	if len(files) != 1 || files[0].Statement != (extension.CoverageCount{Covered: 1, Total: 1}) || files[0].Branch == nil || files[0].Branch.Covered != 0 {
		t.Errorf("%+v", files)
	}
}

func TestLoadJSCoverageNeedsTheDataOfThisExtension(t *testing.T) {
	_, err := loadJSCoverage(context.Background(), extension.FileCoverage{URI: "file:///x", Data: "other"})
	appErr, ok := errors.FromError(err)
	if !ok {
		t.Fatalf("expected *errors.Error, got %T", err)
	}
	if !appErr.HasRoute("loadJSCoverage.JsCoverageNoData") {
		t.Errorf("unexpected failure path: %s", appErr.Route())
	}
}
