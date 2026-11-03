package javascript

import (
	"bufio"
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

// Coverage (SPEC §10.32 "The providers", JavaScript and TypeScript; "As built (step 26.8)"): Jest and Vitest write Istanbul's
// `coverage-final.json` (statements, functions and branches with their places), Node's own runner writes lcov (the lines that ran, the
// functions and the branches of each line).

// jsCoverFile is what the coverage of a file keeps for LoadDetailedCoverage (the FileCoverage's Data).
type jsCoverFile struct {
	statements   []extension.StatementCoverage
	declarations []extension.DeclarationCoverage
}

// ---- Istanbul ----

type istPoint struct {
	Line   *int `json:"line"`
	Column *int `json:"column"`
}

type istSpan struct {
	Start istPoint `json:"start"`
	End   istPoint `json:"end"`
}

type istFn struct {
	Name string  `json:"name"`
	Decl istSpan `json:"decl"`
	Loc  istSpan `json:"loc"`
	Line int     `json:"line"`
}

type istBranch struct {
	Loc       istSpan   `json:"loc"`
	Type      string    `json:"type"`
	Locations []istSpan `json:"locations"`
	Line      int       `json:"line"`
}

// istFile is the entry of one file of coverage-final.json.
type istFile struct {
	Path         string               `json:"path"`
	StatementMap map[string]istSpan   `json:"statementMap"`
	S            map[string]int       `json:"s"`
	FnMap        map[string]istFn     `json:"fnMap"`
	F            map[string]int       `json:"f"`
	BranchMap    map[string]istBranch `json:"branchMap"`
	B            map[string][]int     `json:"b"`
}

// span turns an Istanbul span (lines from 1, columns from 0) into a range; ok is false when it has no start.
func (s istSpan) span() (extension.Range, bool) {
	if s.Start.Line == nil {
		return extension.Range{}, false
	}
	start := extension.Position{Line: max(*s.Start.Line-1, 0)}
	if s.Start.Column != nil {
		start.Col = max(*s.Start.Column, 0)
	}
	end := start
	if s.End.Line != nil {
		end = extension.Position{Line: max(*s.End.Line-1, 0)}
		if s.End.Column != nil {
			end.Col = max(*s.End.Column, 0)
		}
	}
	if end.Line < start.Line || (end.Line == start.Line && end.Col < start.Col) {
		end = start
	}
	return extension.Range{Start: start, End: end}, true
}

// numericKeys sorts the keys of a map whose keys are numbers.
func numericKeys[V any](m map[string]V) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Slice(keys, func(i, j int) bool {
		a, ea := strconv.Atoi(keys[i])
		b, eb := strconv.Atoi(keys[j])
		if ea != nil || eb != nil {
			return keys[i] < keys[j]
		}
		return a < b
	})
	return keys
}

// parseIstanbul reads a coverage-final.json into the coverage of its files, sorted by path; a relative path is relative to dir. Files in
// node_modules and those the skip function names are left out. It fails with JsCoverageJSONInvalid for text that is not one.
func parseIstanbul(data []byte, dir string, skip func(path string) bool) ([]extension.FileCoverage, error) {
	var files map[string]istFile
	if err := json.Unmarshal(data, &files); err != nil {
		return nil, errors.NewErr("JsCoverageJSONInvalid", err)
	}
	keys := make([]string, 0, len(files))
	for k := range files {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	var out []extension.FileCoverage
	for _, k := range keys {
		f := files[k]
		path := f.Path
		if path == "" {
			path = k
		}
		if !filepath.IsAbs(path) {
			path = filepath.Join(dir, path)
		}
		path = filepath.Clean(path)
		if strings.Contains(path, string(filepath.Separator)+"node_modules"+string(filepath.Separator)) || (skip != nil && skip(path)) {
			continue
		}
		out = append(out, istanbulCoverage(path, f))
	}
	return out, nil
}

// istanbulCoverage is the coverage of one file.
func istanbulCoverage(path string, f istFile) extension.FileCoverage {
	jc := &jsCoverFile{}
	fc := extension.FileCoverage{URI: fileURI(path), Data: jc}
	byLine := map[int]int{} // the first statement that starts on a line
	for _, id := range numericKeys(f.StatementMap) {
		rng, ok := f.StatementMap[id].span()
		if !ok {
			continue
		}
		n := f.S[id]
		if _, seen := byLine[rng.Start.Line]; !seen {
			byLine[rng.Start.Line] = len(jc.statements)
		}
		jc.statements = append(jc.statements, extension.StatementCoverage{Executed: n, Location: rng})
		fc.Statement.Total++
		if n > 0 {
			fc.Statement.Covered++
		}
	}
	var branch extension.CoverageCount
	for _, id := range numericKeys(f.BranchMap) {
		b := f.BranchMap[id]
		counts := f.B[id]
		line, ok := b.Loc.span()
		if !ok && len(b.Locations) > 0 {
			line, ok = b.Locations[0].span()
		}
		for i, n := range counts {
			branch.Total++
			if n > 0 {
				branch.Covered++
			}
			if !ok {
				continue
			}
			idx, found := byLine[line.Start.Line]
			if !found {
				continue
			}
			bc := extension.BranchCoverage{Executed: n, Label: b.Type + " branch " + strconv.Itoa(i+1)}
			if i < len(b.Locations) {
				if r, ok := b.Locations[i].span(); ok && r != line {
					bc.Location = &r
				}
			}
			jc.statements[idx].Branches = append(jc.statements[idx].Branches, bc)
		}
	}
	if branch.Total > 0 {
		fc.Branch = &branch
	}
	var decl extension.CoverageCount
	for _, id := range numericKeys(f.FnMap) {
		fn := f.FnMap[id]
		rng, ok := fn.Loc.span()
		if !ok {
			rng, ok = fn.Decl.span()
		}
		if !ok {
			continue
		}
		n := f.F[id]
		decl.Total++
		if n > 0 {
			decl.Covered++
		}
		jc.declarations = append(jc.declarations, extension.DeclarationCoverage{Name: fn.Name, Executed: n, Location: rng})
	}
	if decl.Total > 0 {
		fc.Declaration = &decl
	}
	return fc
}

// ---- lcov ----

// wholeLines is the range of a line (counted from 1), the whole line.
func wholeLines(line int) extension.Range {
	return extension.Range{Start: extension.Position{Line: line - 1}, End: extension.Position{Line: line}}
}

// parseLcov reads the lcov text of Node's runner; a relative path is relative to dir. Files the skip function names are left out. A line it
// does not understand is ignored.
func parseLcov(data []byte, dir string, skip func(path string) bool) []extension.FileCoverage {
	var out []extension.FileCoverage
	var cur *extension.FileCoverage
	var jc *jsCoverFile
	var fnLines map[string]int
	var fnCount map[string]int
	var branches map[int][]extension.BranchCoverage
	var lines map[int]int
	var fnOrder []string
	var curPath string
	end := func() {
		if cur == nil {
			return
		}
		nums := make([]int, 0, len(lines))
		for l := range lines {
			nums = append(nums, l)
		}
		sort.Ints(nums)
		for _, l := range nums {
			jc.statements = append(jc.statements, extension.StatementCoverage{Executed: lines[l], Location: wholeLines(l), Branches: branches[l]})
			cur.Statement.Total++
			if lines[l] > 0 {
				cur.Statement.Covered++
			}
		}
		var bc extension.CoverageCount
		for _, bs := range branches {
			for _, b := range bs {
				bc.Total++
				if b.Executed > 0 {
					bc.Covered++
				}
			}
		}
		if bc.Total > 0 {
			cur.Branch = &bc
		}
		var dc extension.CoverageCount
		for _, name := range fnOrder {
			dc.Total++
			if fnCount[name] > 0 {
				dc.Covered++
			}
			ln := fnLines[name]
			jc.declarations = append(jc.declarations, extension.DeclarationCoverage{Name: name, Executed: fnCount[name], Location: wholeLines(ln)})
		}
		if dc.Total > 0 {
			cur.Declaration = &dc
		}
		slices.SortFunc(jc.declarations, func(a, b extension.DeclarationCoverage) int { return a.Location.Start.Line - b.Location.Start.Line })
		if skip == nil || !skip(curPath) {
			out = append(out, *cur)
		}
		cur = nil
	}
	sc := bufio.NewScanner(strings.NewReader(string(data)))
	sc.Buffer(make([]byte, 0, 64*1024), 1<<20)
	for sc.Scan() {
		key, val, _ := strings.Cut(strings.TrimSpace(sc.Text()), ":")
		switch key {
		case "SF":
			end()
			path := val
			if !filepath.IsAbs(path) {
				path = filepath.Join(dir, path)
			}
			path = filepath.Clean(path)
			curPath = path
			jc = &jsCoverFile{}
			cur = &extension.FileCoverage{URI: fileURI(path), Data: jc}
			fnLines, fnCount, branches, lines, fnOrder = map[string]int{}, map[string]int{}, map[int][]extension.BranchCoverage{}, map[int]int{}, nil
		case "FN":
			ln, name, ok := strings.Cut(val, ",")
			if n, err := strconv.Atoi(ln); ok && err == nil && cur != nil {
				if _, dup := fnLines[name]; !dup {
					fnOrder = append(fnOrder, name)
				}
				fnLines[name] = n
			}
		case "FNDA":
			cnt, name, ok := strings.Cut(val, ",")
			if n, err := strconv.Atoi(cnt); ok && err == nil && cur != nil {
				fnCount[name] += n
			}
		case "DA":
			parts := strings.Split(val, ",")
			if len(parts) >= 2 && cur != nil {
				l, e1 := strconv.Atoi(parts[0])
				n, e2 := strconv.Atoi(parts[1])
				if e1 == nil && e2 == nil && l > 0 {
					lines[l] += n
				}
			}
		case "BRDA":
			parts := strings.Split(val, ",")
			if len(parts) == 4 && cur != nil {
				l, e1 := strconv.Atoi(parts[0])
				taken := 0
				if parts[3] != "-" {
					taken, _ = strconv.Atoi(parts[3])
				}
				if e1 == nil {
					branches[l] = append(branches[l], extension.BranchCoverage{Executed: taken, Label: "block " + parts[1] + " branch " + parts[2]})
				}
			}
		case "end_of_record":
			end()
		}
	}
	end()
	return out
}

// ---- LoadDetailedCoverage ----

// loadJSCoverage is the LoadDetailedCoverage of the coverage profiles: the statements of the file with their branches, and its functions.
// It fails with JsCoverageNoData for a file that this extension did not report.
func loadJSCoverage(_ context.Context, file extension.FileCoverage) (extension.CoverageDetails, error) {
	data, ok := file.Data.(*jsCoverFile)
	if !ok || data == nil {
		return extension.CoverageDetails{}, errors.New("JsCoverageNoData", "the file has no coverage data of the JavaScript extension", "uri", file.URI)
	}
	return extension.CoverageDetails{Statements: slices.Clone(data.statements), Declarations: slices.Clone(data.declarations)}, nil
}
