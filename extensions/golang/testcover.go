package golang

import (
	"context"
	"go/ast"
	"go/parser"
	"go/token"
	"slices"
	"strconv"
	"strings"

	"github.com/rm4n0s/errors"

	"github.com/rm4n0s/emetos/extension"
)

// Coverage from a Go coverage profile (SPEC §10.32 "The providers", Go): the profile `go test -coverprofile` writes is a list of
// blocks, each a range of the source with its number of statements and how many times it ran. The statements of a file are the sums of
// its blocks' statements, the details are the blocks, and a function is covered when its first block ran.

// coverBlock is a line of a profile: file.go:startLine.startCol,endLine.endCol numStmt count (lines and columns from 1; the columns are
// bytes).
type coverBlock struct {
	startLine, startCol, endLine, endCol, stmts, count int
}

// parseCoverProfile reads a profile into its blocks by the import path of the file ("example.com/p/add.go"). It fails with
// GoCoverProfileInvalid for a line that is not a block, with the line in the metadata.
func parseCoverProfile(text string) (map[string][]coverBlock, error) {
	out := map[string][]coverBlock{}
	for _, l := range strings.Split(text, "\n") {
		l = strings.TrimSpace(l)
		if l == "" || strings.HasPrefix(l, "mode:") {
			continue
		}
		file, rest, ok := cutLast(l, ':')
		if !ok {
			return nil, errors.New("GoCoverProfileInvalid", "the line of the coverage profile has no block", "line", l)
		}
		var b coverBlock
		pos, nums, ok := strings.Cut(rest, " ")
		if !ok {
			return nil, errors.New("GoCoverProfileInvalid", "the line of the coverage profile has no counts", "line", l)
		}
		start, end, ok := strings.Cut(pos, ",")
		if !ok {
			return nil, errors.New("GoCoverProfileInvalid", "the block of the coverage profile has no end", "line", l)
		}
		counts := strings.Fields(nums)
		vals := make([]int, 0, 6)
		for _, s := range append(append(strings.Split(start, "."), strings.Split(end, ".")...), counts...) {
			n, err := strconv.Atoi(s)
			if err != nil || n < 0 {
				return nil, errors.New("GoCoverProfileInvalid", "the block of the coverage profile has a number that is not one", "line", l)
			}
			vals = append(vals, n)
		}
		if len(vals) != 6 {
			return nil, errors.New("GoCoverProfileInvalid", "the block of the coverage profile does not have six numbers", "line", l)
		}
		b = coverBlock{startLine: vals[0], startCol: vals[1], endLine: vals[2], endCol: vals[3], stmts: vals[4], count: vals[5]}
		out[file] = append(out[file], b)
	}
	return out, nil
}

// cutLast splits the string at the last sep.
func cutLast(s string, sep byte) (before, after string, ok bool) {
	i := strings.LastIndexByte(s, sep)
	if i < 0 {
		return "", "", false
	}
	return s[:i], s[i+1:], true
}

// mergeBlocks adds the blocks of src into dst: a block at the same place (the same package tested by two runs) adds its count.
func mergeBlocks(dst, src map[string][]coverBlock) {
	for file, blocks := range src {
		have := dst[file]
		for _, b := range blocks {
			i := slices.IndexFunc(have, func(x coverBlock) bool {
				return x.startLine == b.startLine && x.startCol == b.startCol && x.endLine == b.endLine && x.endCol == b.endCol
			})
			if i >= 0 {
				have[i].count += b.count
			} else {
				have = append(have, b)
			}
		}
		dst[file] = have
	}
}

// goCoverFile is what the coverage of a file keeps for LoadDetailedCoverage (the FileCoverage's Data).
type goCoverFile struct {
	blocks []coverBlock
	decls  []extension.DeclarationCoverage
}

// before tells whether the position (line, col) is before the other.
func before(l1, c1, l2, c2 int) bool { return l1 < l2 || (l1 == l2 && c1 < c2) }

// declsOf are the functions of the source with a statement, each covered when the block at its start ran. Source that does not parse
// as far as its declarations has none.
func declsOf(src string, blocks []coverBlock) []extension.DeclarationCoverage {
	fset := token.NewFileSet()
	f, _ := parser.ParseFile(fset, "cover.go", src, parser.SkipObjectResolution)
	if f == nil {
		return nil
	}
	var out []extension.DeclarationCoverage
	for _, d := range f.Decls {
		fn, ok := d.(*ast.FuncDecl)
		if !ok || fn.Body == nil {
			continue
		}
		open, closing := fset.Position(fn.Body.Lbrace), fset.Position(fn.Body.Rbrace)
		first := -1
		for i, b := range blocks {
			if before(b.startLine, b.startCol, open.Line, open.Column) || before(closing.Line, closing.Column+1, b.endLine, b.endCol) {
				continue
			}
			if first < 0 || before(b.startLine, b.startCol, blocks[first].startLine, blocks[first].startCol) {
				first = i
			}
		}
		if first < 0 {
			continue // no statement: nothing to cover
		}
		start, end := fset.Position(fn.Pos()), fset.Position(fn.End())
		out = append(out, extension.DeclarationCoverage{
			Name: fn.Name.Name, Executed: blocks[first].count,
			Location: extension.Range{Start: extension.Position{Line: start.Line - 1, Col: start.Column - 1}, End: extension.Position{Line: end.Line - 1, Col: end.Column - 1}},
		})
	}
	return out
}

// fileCoverage is the coverage of a file whose source is src (empty when it cannot be read: there are no declarations then): the
// statements counted by the profile's numbers, and the declarations when there are any.
func fileCoverage(uri, src string, blocks []coverBlock) extension.FileCoverage {
	slices.SortFunc(blocks, func(a, b coverBlock) int {
		if a.startLine != b.startLine {
			return a.startLine - b.startLine
		}
		return a.startCol - b.startCol
	})
	fc := extension.FileCoverage{URI: uri}
	for _, b := range blocks {
		fc.Statement.Total += b.stmts
		if b.count > 0 {
			fc.Statement.Covered += b.stmts
		}
	}
	data := &goCoverFile{blocks: blocks}
	if src != "" {
		data.decls = declsOf(src, blocks)
	}
	if len(data.decls) > 0 {
		fc.Declaration = &extension.CoverageCount{Total: len(data.decls)}
		for _, d := range data.decls {
			if d.Executed > 0 {
				fc.Declaration.Covered++
			}
		}
	}
	fc.Data = data
	return fc
}

// loadGoCoverage is the LoadDetailedCoverage of the coverage profile: the blocks of the file as statements and its functions as
// declarations. It fails with GoCoverageNoData for a file that this extension did not report.
func loadGoCoverage(_ context.Context, file extension.FileCoverage) (extension.CoverageDetails, error) {
	data, ok := file.Data.(*goCoverFile)
	if !ok || data == nil {
		return extension.CoverageDetails{}, errors.New("GoCoverageNoData", "the file has no coverage data of the Go extension", "uri", file.URI)
	}
	var d extension.CoverageDetails
	for _, b := range data.blocks {
		d.Statements = append(d.Statements, extension.StatementCoverage{
			Executed: b.count,
			Location: extension.Range{Start: extension.Position{Line: b.startLine - 1, Col: b.startCol - 1}, End: extension.Position{Line: b.endLine - 1, Col: b.endCol - 1}},
		})
	}
	d.Declarations = slices.Clone(data.decls)
	return d, nil
}
