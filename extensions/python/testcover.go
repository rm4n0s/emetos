package python

import (
	"context"
	"encoding/json"
	"path/filepath"
	"slices"
	"sort"
	"strconv"
	"strings"

	"github.com/rm4n0s/errors"

	"github.com/rm4n0s/emetos/extension"
)

// Coverage from coverage.py (SPEC §10.32 "The providers", Python): `coverage run` writes its data in a temporary folder and
// `coverage json` turns it into a report. The statements of a file are its executable lines; with branch measurement (the project's own
// coverage configuration asks for it) the report also has the branches, and, from coverage.py 7.6 on, the functions.

// covJSON is coverage.py's JSON report.
type covJSON struct {
	Files map[string]covFile `json:"files"`
}

// covFile is the report of one file.
type covFile struct {
	ExecutedLines    []int               `json:"executed_lines"`
	MissingLines     []int               `json:"missing_lines"`
	ExecutedBranches [][]int             `json:"executed_branches"`
	MissingBranches  [][]int             `json:"missing_branches"`
	Functions        map[string]covFunc  `json:"functions"`
	Summary          covSummary          `json:"summary"`
	Contexts         map[string][]string `json:"contexts"`
}

// covFunc is the report of one function.
type covFunc struct {
	ExecutedLines []int `json:"executed_lines"`
	MissingLines  []int `json:"missing_lines"`
}

// covSummary has the counts of a file.
type covSummary struct {
	CoveredLines  int  `json:"covered_lines"`
	NumStatements int  `json:"num_statements"`
	NumBranches   *int `json:"num_branches"`
	CoveredBranch *int `json:"covered_branches"`
}

// pyCoverFile is what the coverage of a file keeps for LoadDetailedCoverage (the FileCoverage's Data).
type pyCoverFile struct {
	file covFile
}

// parseCoverage reads coverage.py's JSON report into the coverage of its files, sorted by path; a relative path in the report is relative
// to dir. It fails with PythonCoverJSONInvalid for text that is not a report.
func parseCoverage(data []byte, dir string) ([]extension.FileCoverage, error) {
	var rep covJSON
	if err := json.Unmarshal(data, &rep); err != nil {
		return nil, errors.NewErr("PythonCoverJSONInvalid", err)
	}
	if rep.Files == nil {
		return nil, errors.New("PythonCoverJSONInvalid", "the coverage report has no files")
	}
	paths := make([]string, 0, len(rep.Files))
	for p := range rep.Files {
		paths = append(paths, p)
	}
	sort.Strings(paths)
	var out []extension.FileCoverage
	for _, p := range paths {
		f := rep.Files[p]
		abs := p
		if !filepath.IsAbs(abs) {
			abs = filepath.Join(dir, abs)
		}
		fc := extension.FileCoverage{URI: fileURI(filepath.Clean(abs)), Data: &pyCoverFile{file: f}}
		fc.Statement = extension.CoverageCount{Covered: f.Summary.CoveredLines, Total: f.Summary.NumStatements}
		if f.Summary.NumBranches != nil && f.Summary.CoveredBranch != nil {
			fc.Branch = &extension.CoverageCount{Covered: *f.Summary.CoveredBranch, Total: *f.Summary.NumBranches}
		}
		if decls := declarations(f); len(decls) > 0 {
			d := extension.CoverageCount{Total: len(decls)}
			for _, x := range decls {
				if x.Executed > 0 {
					d.Covered++
				}
			}
			fc.Declaration = &d
		}
		out = append(out, fc)
	}
	return out, nil
}

// wholeLines is the range of a line (counted from 1), the whole line.
func wholeLines(line int) extension.Range {
	return extension.Range{Start: extension.Position{Line: line - 1}, End: extension.Position{Line: line}}
}

// declarations are the functions of the report, each covered when one of its lines ran, in the order of their first line. The code of the
// module itself (the function "") is not a declaration.
func declarations(f covFile) []extension.DeclarationCoverage {
	var out []extension.DeclarationCoverage
	for name, fn := range f.Functions {
		lines := append(slices.Clone(fn.ExecutedLines), fn.MissingLines...)
		if name == "" || len(lines) == 0 {
			continue
		}
		first, last := slices.Min(lines), slices.Max(lines)
		d := extension.DeclarationCoverage{Name: name, Location: extension.Range{Start: extension.Position{Line: first - 1}, End: extension.Position{Line: last}}}
		if len(fn.ExecutedLines) > 0 {
			d.Executed = 1
		}
		out = append(out, d)
	}
	slices.SortFunc(out, func(a, b extension.DeclarationCoverage) int {
		if a.Location.Start.Line != b.Location.Start.Line {
			return a.Location.Start.Line - b.Location.Start.Line
		}
		return strings.Compare(a.Name, b.Name)
	})
	return out
}

// branchesAt are the branches that leave the line: where each goes (a negative target is the end of the function or module), and
// whether it ran.
func branchesAt(f covFile, line int) []extension.BranchCoverage {
	var out []extension.BranchCoverage
	add := func(pairs [][]int, executed int) {
		for _, b := range pairs {
			if len(b) != 2 || b[0] != line {
				continue
			}
			label := "to line " + strconv.Itoa(b[1])
			if b[1] < 0 {
				label = "to the end of the block"
			}
			out = append(out, extension.BranchCoverage{Executed: executed, Label: label})
		}
	}
	add(f.ExecutedBranches, 1)
	add(f.MissingBranches, 0)
	return out
}

// loadPythonCoverage is the LoadDetailedCoverage of the coverage report: the lines of the file as statements (with their branches) and
// its functions as declarations. It fails with PythonCoverageNoData for a file that this extension did not report.
func loadPythonCoverage(_ context.Context, file extension.FileCoverage) (extension.CoverageDetails, error) {
	data, ok := file.Data.(*pyCoverFile)
	if !ok || data == nil {
		return extension.CoverageDetails{}, errors.New("PythonCoverageNoData", "the file has no coverage data of the Python extension", "uri", file.URI)
	}
	var d extension.CoverageDetails
	add := func(lines []int, executed int) {
		for _, l := range lines {
			d.Statements = append(d.Statements, extension.StatementCoverage{Executed: executed, Location: wholeLines(l), Branches: branchesAt(data.file, l)})
		}
	}
	add(data.file.ExecutedLines, 1)
	add(data.file.MissingLines, 0)
	slices.SortFunc(d.Statements, func(a, b extension.StatementCoverage) int { return a.Location.Start.Line - b.Location.Start.Line })
	d.Declarations = declarations(data.file)
	return d, nil
}

// reportCoverage turns the data of the run into a report and reports the files in it. A failure is told to the user.
func (tc *testCtl) reportCoverage(ctx context.Context, tr extension.TestRun, python string, cfg testConfig, env []string, report string) {
	p := tc.p
	spec := cmdSpec{Program: python, Args: []string{"-m", "coverage", "json", "-o", report, "--ignore-errors"}, Dir: cfg.cwd, Env: env}
	var out []string
	if err := p.runner(ctx, spec, func(l string) { out = append(out, l) }); err != nil {
		p.api.Log().Warn("the coverage report cannot be made", "err", err, "output", out)
		p.api.Notifications().Warn("The coverage report cannot be made: " + lastOr(out, describe(err)))
		return
	}
	data, err := readFile(report)
	if err != nil {
		p.api.Log().Warn("the coverage report cannot be read", "err", err)
		p.api.Notifications().Warn("The coverage report cannot be read: " + describe(err))
		return
	}
	files, err := parseCoverage(data, cfg.cwd)
	if err != nil {
		p.api.Log().Warn("the coverage report is not valid", "err", err)
		p.api.Notifications().Warn("The coverage report cannot be read: " + describe(err))
		return
	}
	for _, f := range files {
		tr.AddCoverage(f)
	}
}

// lastOr is the last of the lines that is not blank, or the fallback.
func lastOr(lines []string, fallback string) string {
	for i := len(lines) - 1; i >= 0; i-- {
		if t := strings.TrimSpace(lines[i]); t != "" {
			return t
		}
	}
	return fallback
}
