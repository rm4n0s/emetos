package python

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/rm4n0s/errors"

	"github.com/rm4n0s/emetos/extension"
)

// Coverage from coverage.py's JSON report (step 26.6): the sums, the lines, the branches and the functions.

func recordedCoverage(t *testing.T) []byte {
	t.Helper()
	data, err := os.ReadFile(filepath.Join("testdata", "events", "cov_report.json"))
	if err != nil {
		t.Fatal(err)
	}
	return data
}

func TestACoverageReportBecomesFilesWithTheirSums(t *testing.T) {
	files, err := parseCoverage(recordedCoverage(t), "/work")
	if err != nil {
		t.Fatal(err)
	}
	if len(files) != 2 {
		t.Fatalf("%d files", len(files))
	}
	// sorted by the path of the report: the absolute one first
	abs, rel := files[0], files[1]
	if abs.URI != "file:///abs/tests/test_calc.py" || abs.Statement != (extension.CoverageCount{Covered: 2, Total: 2}) || abs.Branch != nil || abs.Declaration != nil {
		t.Errorf("absolute = %+v", abs)
	}
	if rel.URI != "file:///work/calc.py" || rel.Statement != (extension.CoverageCount{Covered: 5, Total: 9}) {
		t.Errorf("relative = %+v", rel)
	}
	if rel.Branch == nil || *rel.Branch != (extension.CoverageCount{Covered: 3, Total: 4}) {
		t.Errorf("branches = %+v", rel.Branch)
	}
	// three functions, the code of the module itself is none; one of them did not run
	if rel.Declaration == nil || *rel.Declaration != (extension.CoverageCount{Covered: 2, Total: 3}) {
		t.Errorf("declarations = %+v", rel.Declaration)
	}
}

func TestCoverageDetailsAreTheLinesTheirBranchesAndTheFunctions(t *testing.T) {
	files, err := parseCoverage(recordedCoverage(t), "/work")
	if err != nil {
		t.Fatal(err)
	}
	d, err := loadPythonCoverage(context.Background(), files[1])
	if err != nil {
		t.Fatal(err)
	}
	type st struct{ line, hits, branches int }
	var got []st
	for _, s := range d.Statements {
		got = append(got, st{s.Location.Start.Line + 1, s.Executed, len(s.Branches)})
		if s.Location.End.Line != s.Location.Start.Line+1 || s.Location.Start.Col != 0 || s.Location.End.Col != 0 {
			t.Errorf("not the whole line: %+v", s.Location)
		}
	}
	want := []st{{1, 1, 0}, {2, 1, 0}, {5, 1, 0}, {6, 1, 2}, {7, 0, 0}, {8, 1, 2}, {9, 0, 0}, {12, 0, 0}, {13, 0, 0}}
	if len(got) != len(want) {
		t.Fatalf("statements = %v", got)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("statement %d = %v, want %v", i, got[i], want[i])
		}
	}
	// line 8: one branch ran and one did not
	for _, s := range d.Statements {
		if s.Location.Start.Line == 7 {
			if s.Branches[0].Executed != 1 || s.Branches[0].Label != "to line 9" || s.Branches[1].Executed != 0 || s.Branches[1].Label != "to line 10" {
				t.Errorf("branches of line 8 = %+v", s.Branches)
			}
		}
	}
	var decls []string
	for _, x := range d.Declarations {
		decls = append(decls, x.Name+":"+string(rune('0'+x.Executed)))
	}
	if strings.Join(decls, " ") != "double:1 sign:1 unused:0" {
		t.Errorf("declarations = %v", decls)
	}
	if d.Declarations[1].Location.Start.Line != 5 || d.Declarations[1].Location.End.Line != 9 {
		t.Errorf("sign = %+v", d.Declarations[1].Location)
	}
}

func TestAnExitBranchIsNamedByTheEndOfItsBlock(t *testing.T) {
	f := covFile{ExecutedBranches: [][]int{{4, -1}}, MissingBranches: [][]int{{4, 5}, {6, 7}, {4}}}
	got := branchesAt(f, 4)
	if len(got) != 2 || got[0].Label != "to the end of the block" || got[0].Executed != 1 || got[1].Label != "to line 5" || got[1].Executed != 0 {
		t.Errorf("branches = %+v", got)
	}
}

func TestParseCoverageFailsForTextThatIsNotAReport(t *testing.T) {
	for name, text := range map[string]string{"not json": "No data to report.", "no files": `{"meta":{}}`} {
		t.Run(name, func(t *testing.T) {
			_, err := parseCoverage([]byte(text), "/work")
			appErr, ok := errors.FromError(err)
			if !ok || !appErr.HasRoute("parseCoverage.PythonCoverJSONInvalid") {
				t.Fatalf("error = %v", err)
			}
		})
	}
}

func TestLoadingTheCoverageOfAFileOfAnotherExtensionFails(t *testing.T) {
	_, err := loadPythonCoverage(context.Background(), extension.FileCoverage{URI: "file:///x.py", Data: "other"})
	appErr, ok := errors.FromError(err)
	if !ok || !appErr.HasRoute("loadPythonCoverage.PythonCoverageNoData") {
		t.Fatalf("error = %v", err)
	}
}

func TestReadingAFileThatIsNotThereFails(t *testing.T) {
	_, err := readFile(filepath.Join(t.TempDir(), "missing.json"))
	appErr, ok := errors.FromError(err)
	if !ok || !appErr.HasRoute("readFile.PythonTestFileReadFailed") {
		t.Fatalf("error = %v", err)
	}
}

func TestACoverageReportThatCannotBeReadIsToldToTheUser(t *testing.T) {
	fr := &fakeRunner{}
	base := answers(t, true, true)
	fr.answer = func(spec cmdSpec) ([]string, error) {
		if strings.Contains(strings.Join(spec.Args, " "), "coverage json") {
			for i, a := range spec.Args {
				if a == "-o" {
					if err := os.WriteFile(spec.Args[i+1], []byte("not a report"), 0o600); err != nil {
						t.Error(err)
					}
				}
			}
			return nil, nil
		}
		return base(spec)
	}
	r := testRig(t, map[string]any{PytestEnabledSetting: true}, testFolder, fr)
	r.discovered()
	runIt(t, r, extension.TestKindCoverage, nil, nil)
	r.api.Pump()
	if notes := r.notes(); len(notes) != 1 || !strings.HasPrefix(notes[0], "The coverage report cannot be read") {
		t.Errorf("notes = %q", notes)
	}
}
