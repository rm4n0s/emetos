package golang

import (
	"context"
	"go/ast"
	"go/parser"
	"go/token"
	"strconv"
	"strings"
	"unicode"
	"unicode/utf8"

	"github.com/rm4n0s/emetos/extension"
)

// The tests of a Go file and their code lenses (SPEC §10.17 "As built (step 14.3)").

// Ported from VS Code Go: extension/src/goRunTestCodelens.ts (the lenses "run test", "debug test", "run benchmark", "debug
// benchmark", "run package tests" and "run file tests") and extension/src/testUtils.ts (which functions are tests).

// The kinds of function that `go test` runs.
const (
	kindTest = iota
	kindBenchmark
	kindExample
	kindFuzz
)

// goTestFunc is a function that go test runs.
type goTestFunc struct {
	name string
	kind int
	// startLine is the line of the func keyword and endLine the last line of the function (from 0); col and nameEnd are the
	// byte columns of the keyword and of the end of the name in startLine.
	startLine, endLine, col, nameEnd int
}

// goTestFile is what is known of the source of a Go file for the tests.
type goTestFile struct {
	// pkgLine and pkgCol are the place of the package clause (from 0); hasPkg says there is one.
	pkgLine, pkgCol, pkgEnd int
	hasPkg                  bool
	funcs                   []goTestFunc
}

// testingName is the name the file gives the package testing ("testing", the name of a renamed import, "." for a dot import);
// empty when it does not import it.
func testingName(f *ast.File) string {
	for _, imp := range f.Imports {
		path, err := strconv.Unquote(imp.Path.Value)
		if err != nil || path != "testing" {
			continue
		}
		if imp.Name != nil {
			return imp.Name.Name
		}
		return "testing"
	}
	return ""
}

// isTestingType tells whether the expression is *testing.<name> (or *<name> with a dot import).
func isTestingType(e ast.Expr, pkg, name string) bool {
	star, ok := e.(*ast.StarExpr)
	if !ok {
		return false
	}
	switch x := star.X.(type) {
	case *ast.SelectorExpr:
		id, ok := x.X.(*ast.Ident)
		return ok && pkg != "." && id.Name == pkg && x.Sel.Name == name
	case *ast.Ident:
		return pkg == "." && x.Name == name
	}
	return false
}

// hasTestPrefix is go test's rule for the name of a test: the prefix, then nothing or a character that is not a lower case letter
// (Testfoo is not a test).
func hasTestPrefix(name, prefix string) bool {
	rest, ok := strings.CutPrefix(name, prefix)
	if !ok {
		return false
	}
	if rest == "" {
		return true
	}
	r, _ := utf8.DecodeRuneInString(rest)
	return !unicode.IsLower(r)
}

// scanTestFile reads the functions of a _test.go file that go test runs: TestXxx(t *testing.T), BenchmarkXxx(b *testing.B),
// FuzzXxx(f *testing.F) and ExampleXxx(). The source may have syntax errors: what the parser made of it is used. A source that
// cannot be read at all has no functions.
func scanTestFile(src string) goTestFile {
	fset := token.NewFileSet()
	f, _ := parser.ParseFile(fset, "document_test.go", src, parser.SkipObjectResolution|parser.ParseComments)
	var out goTestFile
	if f == nil {
		return out
	}
	if f.Package.IsValid() {
		p := fset.Position(f.Package)
		out.pkgLine, out.pkgCol, out.hasPkg = p.Line-1, p.Column-1, true
		out.pkgEnd = fset.Position(f.Name.End()).Column - 1
	}
	pkg := testingName(f)
	for _, d := range f.Decls {
		fn, ok := d.(*ast.FuncDecl)
		if !ok || fn.Recv != nil || fn.Name == nil || fn.Type == nil || fn.Type.TypeParams != nil {
			continue
		}
		name := fn.Name.Name
		kind, ok := testKind(fn, name, pkg)
		if !ok {
			continue
		}
		start, end, nameEnd := fset.Position(fn.Pos()), fset.Position(fn.End()), fset.Position(fn.Name.End())
		out.funcs = append(out.funcs, goTestFunc{name: name, kind: kind, startLine: start.Line - 1, endLine: end.Line - 1, col: start.Column - 1, nameEnd: nameEnd.Column - 1})
	}
	return out
}

// testKind says what kind of test the function is, by its name and its parameters.
func testKind(fn *ast.FuncDecl, name, pkg string) (int, bool) {
	params := fn.Type.Params
	one := func(typ string) bool {
		return pkg != "" && params != nil && len(params.List) == 1 && len(params.List[0].Names) <= 1 && isTestingType(params.List[0].Type, pkg, typ) && fn.Type.Results == nil
	}
	switch {
	case hasTestPrefix(name, "Test") && one("T"):
		return kindTest, true
	case hasTestPrefix(name, "Benchmark") && one("B"):
		return kindBenchmark, true
	case hasTestPrefix(name, "Fuzz") && one("F"):
		return kindFuzz, true
	case hasTestPrefix(name, "Example") && (params == nil || len(params.List) == 0) && fn.Type.Results == nil:
		return kindExample, true
	}
	return 0, false
}

// at is the function that holds the line, or false.
func (f goTestFile) at(line int) (goTestFunc, bool) {
	for _, fn := range f.funcs {
		if line >= fn.startLine && line <= fn.endLine {
			return fn, true
		}
	}
	return goTestFunc{}, false
}

// tests are the names of the tests, examples and fuzz targets of the file (not the benchmarks), in the order of the file.
func (f goTestFile) tests() []string {
	var names []string
	for _, fn := range f.funcs {
		if fn.kind != kindBenchmark {
			names = append(names, fn.name)
		}
	}
	return names
}

// runPattern is the value of -run (or -bench) for the names: ^Name$ for one, ^(A|B)$ for several. A name is an identifier, so
// it needs no quoting.
func runPattern(names []string) string {
	if len(names) == 1 {
		return "^" + names[0] + "$"
	}
	return "^(" + strings.Join(names, "|") + ")$"
}

// ---- the code lenses ----

// The ids of the commands the lenses run.
const (
	// TestFunctionCommand runs one test or benchmark by name; the lenses use it, and it is not in the palette.
	TestFunctionCommand = ID + ".testFunction"
)

// lensArgs are the arguments of the commands the lenses run.
type lensArgs struct {
	URI  string `json:"uri"`
	Name string `json:"name,omitempty"`
}

// goLenses is the provider of the code lenses of _test.go files.
type goLenses struct{ g *golang }

// lensCommand makes the command of a lens.
func lensCommand(title, id string, args lensArgs) (extension.Command, error) {
	return extension.NewCommand(title, id, args)
}

// ProvideCodeLenses implements extension.CodeLensProvider. It runs off the UI thread and uses the text it is given only.
func (l goLenses) ProvideCodeLenses(_ context.Context, doc extension.TextDocument) ([]extension.CodeLens, error) {
	if !l.g.lensesOn.Load() || !strings.HasSuffix(doc.URI, "_test.go") {
		return nil, nil
	}
	file := scanTestFile(doc.Text)
	if len(file.funcs) == 0 {
		return nil, nil
	}
	var out []extension.CodeLens
	add := func(line, col, end int, title, id string, args lensArgs, disabled bool) error {
		cmd, err := lensCommand(title, id, args)
		if err != nil {
			return err
		}
		if disabled {
			cmd.ID, cmd.Arguments = "", nil
		}
		out = append(out, extension.CodeLens{
			Range:   extension.Range{Start: extension.Position{Line: line, Col: col}, End: extension.Position{Line: line, Col: end}},
			Command: cmd, Disabled: disabled,
		})
		return nil
	}
	if file.hasPkg {
		a := lensArgs{URI: doc.URI}
		if err := add(file.pkgLine, file.pkgCol, file.pkgEnd, "run package tests", TestPackageCommand, a, false); err != nil {
			return nil, err
		}
		if err := add(file.pkgLine, file.pkgCol, file.pkgEnd, "run file tests", TestFileCommand, a, false); err != nil {
			return nil, err
		}
	}
	for _, fn := range file.funcs {
		a := lensArgs{URI: doc.URI, Name: fn.name}
		run, debug := "run test", "debug test"
		if fn.kind == kindBenchmark {
			run, debug = "run benchmark", "debug benchmark"
		}
		// "debug" is shown and cannot be run: debugging comes with area 6
		if err := add(fn.startLine, fn.col, fn.nameEnd, run, TestFunctionCommand, a, false); err != nil {
			return nil, err
		}
		if err := add(fn.startLine, fn.col, fn.nameEnd, debug, "", a, true); err != nil {
			return nil, err
		}
	}
	return out, nil
}
