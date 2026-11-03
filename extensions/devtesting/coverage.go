package devtesting

import (
	"context"

	"github.com/rm4n0s/errors"

	"github.com/rm4n0s/emetos/extension"
)

// The coverage the controller reports for a run with coverage (step 26.4): the files of Script.Coverage, with counts worked out from their
// details and the details given back to LoadDetailedCoverage.

// detailsOf converts the script's statements and declarations of a file.
func detailsOf(f FileCoverage) extension.CoverageDetails {
	var d extension.CoverageDetails
	for _, st := range f.Statements {
		d.Statements = append(d.Statements, extension.StatementCoverage{Executed: st.Hits, Location: rangeOf(st.Line, st.EndLine, st.Col, st.EndCol), Branches: branchesOf(st)})
	}
	for _, de := range f.Declarations {
		d.Declarations = append(d.Declarations, extension.DeclarationCoverage{Name: de.Name, Executed: de.Hits, Location: rangeOf(de.Line, de.EndLine, 0, 0)})
	}
	return d
}

func branchesOf(st Statement) []extension.BranchCoverage {
	var out []extension.BranchCoverage
	for _, b := range st.Branches {
		br := extension.BranchCoverage{Executed: b.Hits, Label: b.Label}
		if b.Line > 0 {
			r := rangeOf(b.Line, b.Line, b.Col, b.EndCol)
			br.Location = &r
		}
		out = append(out, br)
	}
	return out
}

// rangeOf is the range of lines (counted from 1) and columns (counted from 1) of the script; without columns it is the whole lines.
func rangeOf(line, endLine, col, endCol int) extension.Range {
	if endLine < line {
		endLine = line
	}
	if col == 0 && endCol == 0 {
		return extension.Range{Start: extension.Position{Line: line - 1}, End: extension.Position{Line: endLine}}
	}
	return extension.Range{Start: extension.Position{Line: line - 1, Col: max(col-1, 0)}, End: extension.Position{Line: endLine - 1, Col: max(endCol-1, 0)}}
}

// fileCoverageOf is the coverage a file reports: the counts are those of its details.
func fileCoverageOf(folder string, f FileCoverage) extension.FileCoverage {
	d := detailsOf(f)
	fc := extension.FileCoverage{URI: fileURI(folder, f.File), Data: f.File}
	for _, st := range d.Statements {
		fc.Statement.Total++
		if st.Executed > 0 {
			fc.Statement.Covered++
		}
		for _, b := range st.Branches {
			if fc.Branch == nil {
				fc.Branch = &extension.CoverageCount{}
			}
			fc.Branch.Total++
			if b.Executed > 0 {
				fc.Branch.Covered++
			}
		}
	}
	for _, de := range d.Declarations {
		if fc.Declaration == nil {
			fc.Declaration = &extension.CoverageCount{}
		}
		fc.Declaration.Total++
		if de.Executed > 0 {
			fc.Declaration.Covered++
		}
	}
	return fc
}

// loadDetails is the LoadDetailedCoverage of the profiles.
func loadDetails(script Script) func(ctx context.Context, file extension.FileCoverage) (extension.CoverageDetails, error) {
	return func(_ context.Context, file extension.FileCoverage) (extension.CoverageDetails, error) {
		name, _ := file.Data.(string)
		for _, f := range script.Coverage {
			if f.File == name {
				return detailsOf(f), nil
			}
		}
		return extension.CoverageDetails{}, errors.New("DevTestingNoCoverage", "the script has no coverage for the file", "file", name)
	}
}
