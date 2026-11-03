package golang

import (
	"fmt"
	"go/ast"
	"go/token"
	"strconv"
	"strings"
	"unicode"
)

// The subtests of a test function (SPEC §10.32 "The providers", Go): the calls `t.Run("name", func(t *testing.T) {...})` whose name is a
// string literal, found in the function and in the functions they run, with go test's names for them.

// Ported from VS Code Go: extension/src/goTest/ (the subtests found in the source by their literal names; the others are added when
// the run reports them) and testing.rewrite in the Go source (the name of a subtest: spaces become underscores, a name used twice gets
// #01).

// goSubtest is a call of Run with a literal name.
type goSubtest struct {
	// name is the name go test gives it below its parent ("a_b", "case#01"), the element after the last slash of the whole name.
	name string
	// the place of the call: lines from 0, byte columns.
	startLine, startCol, endLine, endCol int
	subs                                 []goSubtest
}

// subtestName is the name go test gives a subtest that was called by the literal name (testing's rewrite: a space becomes an
// underscore; a name with a character that cannot be printed is not found by a literal). ok is false for the empty name and the
// names that cannot be matched by a run pattern.
func subtestName(lit string) (string, bool) {
	if lit == "" {
		return "", false
	}
	var sb strings.Builder
	for _, r := range lit {
		switch {
		case r == '/':
			return "", false // a slash is a level of the name for go test, not a character
		case unicode.IsSpace(r):
			sb.WriteByte('_')
		case !unicode.IsPrint(r):
			return "", false
		default:
			sb.WriteRune(r)
		}
	}
	return sb.String(), true
}

// scanSubtests finds the calls of Run on the parameter of the function body with a literal name, and, in the function literal they are
// given, the calls on its parameter.
func scanSubtests(fset *token.FileSet, body *ast.BlockStmt, param, pkg string) []goSubtest {
	if body == nil || param == "" || param == "_" {
		return nil
	}
	var out []goSubtest
	used := map[string]int{}
	ast.Inspect(body, func(n ast.Node) bool {
		call, ok := n.(*ast.CallExpr)
		if !ok {
			return true
		}
		sel, ok := call.Fun.(*ast.SelectorExpr)
		if !ok || sel.Sel.Name != "Run" || len(call.Args) != 2 {
			return true
		}
		if id, ok := sel.X.(*ast.Ident); !ok || id.Name != param {
			return true
		}
		lit, ok := call.Args[0].(*ast.BasicLit)
		if !ok || lit.Kind != token.STRING {
			return true
		}
		text, err := strconv.Unquote(lit.Value)
		if err != nil {
			return true
		}
		name, ok := subtestName(text)
		if !ok {
			return true
		}
		if n := used[name]; n > 0 {
			used[name]++
			name += fmt.Sprintf("#%02d", n)
		} else {
			used[name] = 1
		}
		start, end := fset.Position(call.Pos()), fset.Position(call.End())
		st := goSubtest{name: name, startLine: start.Line - 1, startCol: start.Column - 1, endLine: end.Line - 1, endCol: end.Column - 1}
		if fl, ok := call.Args[1].(*ast.FuncLit); ok {
			st.subs = scanSubtests(fset, fl.Body, funcLitParam(fl, pkg), pkg)
		}
		out = append(out, st)
		return false // the calls inside are the subtests of this one
	})
	return out
}

// funcLitParam is the name of the *testing.T or *testing.B parameter of a function literal, or "" when it has none that is named.
func funcLitParam(fl *ast.FuncLit, pkg string) string {
	params := fl.Type.Params
	if params == nil || len(params.List) != 1 || len(params.List[0].Names) != 1 {
		return ""
	}
	if !isTestingType(params.List[0].Type, pkg, "T") && !isTestingType(params.List[0].Type, pkg, "B") {
		return ""
	}
	return params.List[0].Names[0].Name
}

// funcParam is the name of the parameter of a test or benchmark function.
func funcParam(fn *ast.FuncDecl) string {
	params := fn.Type.Params
	if params == nil || len(params.List) != 1 || len(params.List[0].Names) != 1 {
		return ""
	}
	return params.List[0].Names[0].Name
}
