package archtest

import (
	"go/ast"
	"go/token"
	"go/types"
	"strconv"
	"strings"
)

// errorConstructors are the functions of github.com/rm4n0s/errors that capture a
// stack and so make an error at the line where they are called.
var errorConstructors = map[string]bool{"New": true, "NewErr": true, "NewErrf": true}

// errorChain are the methods that return the same *errors.Error, so a chain of
// them after a constructor is still the constructor's error.
var errorChain = map[string]bool{"SetMetadata": true, "SetMessage": true, "RemoveMetadata": true}

// constructorCall returns the errors.New, errors.NewErr or errors.NewErrf call
// that expr is, or that expr is a chain of SetMetadata, SetMessage or
// RemoveMetadata calls on. errorsName is the local name of the errors package.
// It returns nil for any other expression.
func constructorCall(expr ast.Expr, errorsName string) *ast.CallExpr {
	call, ok := expr.(*ast.CallExpr)
	if !ok {
		return nil
	}
	sel, ok := call.Fun.(*ast.SelectorExpr)
	if !ok {
		return nil
	}
	if id, ok := sel.X.(*ast.Ident); ok && id.Name == errorsName && errorConstructors[sel.Sel.Name] {
		return call
	}
	if errorChain[sel.Sel.Name] {
		return constructorCall(sel.X, errorsName)
	}
	return nil
}

// checkErrorRules applies the two per-file rules about where errors are built
// (RulePackageLevelError and RuleNonLiteralTag) to one file that
// imports github.com/rm4n0s/errors as errorsName.
//
//   - RulePackageLevelError: a var declaration at package level calls a
//     constructor (a function literal in it is not run at package level and is
//     skipped). A sentinel is &errors.Error{Tag: ...}. The rule covers test files.
//   - RuleProjectErrorRewrapped (non-test files): the error argument of
//     errors.NewErr, or an error argument of errors.NewErrf with %w, is a variable
//     whose nearest definition in scope is a call of pkg.Func where pkg is an
//     import of a package of Emetos (a path under github.com/rm4n0s/emetos/). Such
//     an error already carries its Tag and route (AI_GUIDE R3), so it is returned
//     as it is. The check is syntactic and by import path, so it has no false
//     positives apart from a function of Emetos that returns an untagged error
//     of the standard library; it does not see the calls of methods and of
//     functions of the same package, which only review can catch. A wrap inside an
//     if whose init statement or condition calls FromError (the guard that keeps tagged errors) is
//     not reported.
//   - RuleErrorHelper is not checked here, see scanHelpers.
//   - RuleNonLiteralTag: the first argument of a constructor in a non-test file is
//     not a string literal (a variable or a parameter hides the Tag from
//     scripts/error-tags.sh and from the route tests).
func checkErrorRules(fset *token.FileSet, rel string, file *ast.File, errorsName string) []Violation {
	var found []Violation
	add := func(pos token.Pos, rule Rule, detail string) {
		found = append(found, Violation{File: rel, Line: fset.Position(pos).Line, Rule: rule, Detail: detail})
	}
	isTest := strings.HasSuffix(rel, "_test.go")

	for _, decl := range file.Decls {
		gen, ok := decl.(*ast.GenDecl)
		if !ok {
			continue
		}
		for _, spec := range gen.Specs {
			vs, ok := spec.(*ast.ValueSpec)
			if !ok {
				continue
			}
			for _, v := range vs.Values {
				ast.Inspect(v, func(n ast.Node) bool {
					if _, ok := n.(*ast.FuncLit); ok {
						return false
					}
					if e, ok := n.(ast.Expr); ok {
						if call := constructorCall(e, errorsName); call != nil {
							add(call.Pos(), RulePackageLevelError, constructorName(call))
							return false
						}
					}
					return true
				})
			}
		}
	}

	if isTest {
		return found
	}

	ast.Inspect(file, func(n ast.Node) bool {
		call, ok := n.(*ast.CallExpr)
		if !ok {
			return true
		}
		sel, ok := call.Fun.(*ast.SelectorExpr)
		if !ok {
			return true
		}
		id, ok := sel.X.(*ast.Ident)
		if !ok || id.Name != errorsName || !errorConstructors[sel.Sel.Name] || len(call.Args) == 0 {
			return true
		}
		if lit, ok := call.Args[0].(*ast.BasicLit); !ok || lit.Kind != token.STRING {
			add(call.Pos(), RuleNonLiteralTag, constructorName(call)+"("+types.ExprString(call.Args[0])+", ...)")
		}
		return true
	})

	found = append(found, rewrapped(fset, rel, file, errorsName)...)
	return found
}

// constructorName returns "errors.New" and the like for a constructor call.
func constructorName(call *ast.CallExpr) string {
	return types.ExprString(call.Fun)
}

// returnsError reports whether a function type has a result of type error or
// *errors.Error.
func returnsError(ft *ast.FuncType, errorsName string) bool {
	if ft.Results == nil {
		return false
	}
	for _, f := range ft.Results.List {
		switch t := f.Type.(type) {
		case *ast.Ident:
			if t.Name == "error" {
				return true
			}
		case *ast.StarExpr:
			if sel, ok := t.X.(*ast.SelectorExpr); ok {
				if id, ok := sel.X.(*ast.Ident); ok && id.Name == errorsName && sel.Sel.Name == "Error" {
					return true
				}
			}
		}
	}
	return false
}

// buildsOnly reports whether a function body is assignments and declarations
// followed by one return with a constructor call among its results.
func buildsOnly(body *ast.BlockStmt, errorsName string) bool {
	n := len(body.List)
	if n == 0 {
		return false
	}
	for _, st := range body.List[:n-1] {
		switch st.(type) {
		case *ast.AssignStmt, *ast.DeclStmt:
		default:
			return false
		}
	}
	ret, ok := body.List[n-1].(*ast.ReturnStmt)
	if !ok {
		return false
	}
	for _, r := range ret.Results {
		if constructorCall(r, errorsName) != nil {
			return true
		}
	}
	return false
}

// helperScan holds what the RuleErrorHelper check needs from the non-test files of
// one folder (one package): the functions that only build an error, and the names
// that some function returns the result of.
type helperScan struct {
	candidates []helperCandidate
	returned   map[string]bool
}

// helperCandidate is a function that only builds an error.
type helperCandidate struct {
	file string
	line int
	name string
}

// scanHelpers adds one non-test file to the scan of its folder.
//
// RuleErrorHelper: a function is a helper when it returns error or *errors.Error,
// its body is only assignments and declarations followed by one return that has a
// constructor (with its SetMetadata chain) among its results, and another
// function in the folder returns a call of it (`return nil, p.fail(...)`) or
// panics with one. A
// function declaration is checked, and a function literal assigned to a variable.
// Such a function exists only to build the error for its callers, so the route
// would name the helper instead of the function that detects the failure, which
// builds the error itself. A body with an if, for, switch or any other statement
// is not matched, so a function that tests something and then builds the error
// is fine, and so is a function that nobody returns the result of: an
// implementation that always fails, such as the stub of a platform, is the place
// of its failure. Names are compared without the type of a method. Test files are
// not checked: their fakes build the errors they stand for.
func (s *helperScan) scanFile(fset *token.FileSet, rel string, file *ast.File) {
	errorsName := ""
	for _, imp := range file.Imports {
		if path, err := strconv.Unquote(imp.Path.Value); err == nil && path == errorsModule {
			errorsName = "errors"
			if imp.Name != nil {
				errorsName = imp.Name.Name
			}
		}
	}
	if s.returned == nil {
		s.returned = map[string]bool{}
	}
	note := func(e ast.Expr) {
		if call, ok := e.(*ast.CallExpr); ok {
			switch f := call.Fun.(type) {
			case *ast.Ident:
				s.returned[f.Name] = true
			case *ast.SelectorExpr:
				s.returned[f.Sel.Name] = true
			}
		}
	}
	ast.Inspect(file, func(n ast.Node) bool {
		switch st := n.(type) {
		case *ast.ReturnStmt:
			for _, r := range st.Results {
				note(r)
			}
		case *ast.CallExpr:
			if id, ok := st.Fun.(*ast.Ident); ok && id.Name == "panic" && len(st.Args) == 1 {
				note(st.Args[0])
			}
		}
		return true
	})
	if errorsName == "" || errorsName == "_" || errorsName == "." {
		return
	}
	check := func(name string, ftype *ast.FuncType, body *ast.BlockStmt, pos token.Pos) {
		if body != nil && returnsError(ftype, errorsName) && buildsOnly(body, errorsName) {
			s.candidates = append(s.candidates, helperCandidate{rel, fset.Position(pos).Line, name})
		}
	}
	for _, decl := range file.Decls {
		if fn, ok := decl.(*ast.FuncDecl); ok {
			check(fn.Name.Name, fn.Type, fn.Body, fn.Pos())
		}
	}
	ast.Inspect(file, func(n ast.Node) bool {
		switch st := n.(type) {
		case *ast.AssignStmt:
			for i, r := range st.Rhs {
				if lit, ok := r.(*ast.FuncLit); ok && i < len(st.Lhs) {
					if id, ok := st.Lhs[i].(*ast.Ident); ok {
						check(id.Name, lit.Type, lit.Body, lit.Pos())
					}
				}
			}
		case *ast.ValueSpec:
			for i, r := range st.Values {
				if lit, ok := r.(*ast.FuncLit); ok && i < len(st.Names) {
					check(st.Names[i].Name, lit.Type, lit.Body, lit.Pos())
				}
			}
		}
		return true
	})
}

// violations returns the RuleErrorHelper violations of the scanned folder.
func (s *helperScan) violations() []Violation {
	var found []Violation
	for _, c := range s.candidates {
		if s.returned[c.name] {
			found = append(found, Violation{File: c.file, Line: c.line, Rule: RuleErrorHelper, Detail: c.name})
		}
	}
	return found
}

// rewrapped finds the RuleProjectErrorRewrapped violations of one non-test file.
func rewrapped(fset *token.FileSet, rel string, file *ast.File, errorsName string) []Violation {
	projectImports := map[string]bool{} // local names of the packages of Emetos
	for _, imp := range file.Imports {
		p, err := strconv.Unquote(imp.Path.Value)
		if err != nil || !strings.HasPrefix(p, emetosModule) {
			continue
		}
		name := p[strings.LastIndex(p, "/")+1:]
		if imp.Name != nil {
			name = imp.Name.Name
		}
		projectImports[name] = true
	}
	if len(projectImports) == 0 {
		return nil
	}
	var found []Violation
	var stack []ast.Node
	ast.Inspect(file, func(n ast.Node) bool {
		if n == nil {
			stack = stack[:len(stack)-1]
			return true
		}
		stack = append(stack, n)
		call, ok := n.(*ast.CallExpr)
		if !ok || !isConstructor(call, errorsName) || constructorName(call) == errorsName+".New" {
			return true
		}
		var errArgs []*ast.Ident
		switch {
		case constructorName(call) == errorsName+".NewErr" && len(call.Args) == 2:
			if id, ok := call.Args[1].(*ast.Ident); ok {
				errArgs = append(errArgs, id)
			}
		case len(call.Args) > 2:
			for _, a := range call.Args[2:] {
				if id, ok := a.(*ast.Ident); ok {
					errArgs = append(errArgs, id)
				}
			}
		}
		if guarded(stack) {
			return true
		}
		for _, id := range errArgs {
			if def := definition(stack, id.Name); def != nil {
				if sel, ok := def.Fun.(*ast.SelectorExpr); ok {
					if x, ok := sel.X.(*ast.Ident); ok && projectImports[x.Name] {
						found = append(found, Violation{File: rel, Line: fset.Position(call.Pos()).Line, Rule: RuleProjectErrorRewrapped, Detail: x.Name + "." + sel.Sel.Name})
					}
				}
			}
		}
		return true
	})
	return found
}

// guarded reports whether the node at the end of the stack is inside an if whose
// init statement or condition calls FromError: the guard that passes a tagged error on.
func guarded(stack []ast.Node) bool {
	for _, n := range stack {
		ifs, ok := n.(*ast.IfStmt)
		if !ok {
			continue
		}
		found := false
		for _, part := range []ast.Node{ifs.Init, ifs.Cond} {
			if part == nil {
				continue
			}
			ast.Inspect(part, func(c ast.Node) bool {
				if call, ok := c.(*ast.CallExpr); ok {
					if sel, ok := call.Fun.(*ast.SelectorExpr); ok && sel.Sel.Name == "FromError" {
						found = true
					}
				}
				return !found
			})
		}
		if found {
			return true
		}
	}
	return false
}

// definition finds the call that gave the variable name its value, looking from
// the node at the end of the stack outwards through the statements that come
// before it in each block and the init statements of the if, for and switch
// statements around it. It returns nil when the nearest definition is not
// an assignment from a call.
func definition(stack []ast.Node, name string) *ast.CallExpr {
	assigned := func(st ast.Stmt) (*ast.CallExpr, bool) {
		as, ok := st.(*ast.AssignStmt)
		if !ok {
			return nil, false
		}
		for i, l := range as.Lhs {
			if id, ok := l.(*ast.Ident); !ok || id.Name != name {
				continue
			}
			var r ast.Expr
			switch {
			case len(as.Rhs) == len(as.Lhs):
				r = as.Rhs[i]
			case len(as.Rhs) == 1:
				r = as.Rhs[0]
			}
			call, _ := r.(*ast.CallExpr)
			return call, true
		}
		return nil, false
	}
	inList := func(list []ast.Stmt, child ast.Node) (*ast.CallExpr, bool) {
		at := -1
		for i, st := range list {
			if st == child {
				at = i
			}
		}
		for i := at - 1; i >= 0; i-- {
			if call, ok := assigned(list[i]); ok {
				return call, true
			}
		}
		return nil, false
	}
	for i := len(stack) - 2; i >= 0; i-- {
		child := stack[i+1]
		var call *ast.CallExpr
		var ok bool
		switch a := stack[i].(type) {
		case *ast.BlockStmt:
			call, ok = inList(a.List, child)
		case *ast.CaseClause:
			call, ok = inList(a.Body, child)
		case *ast.CommClause:
			call, ok = inList(a.Body, child)
		case *ast.IfStmt:
			if a.Init != nil {
				call, ok = assigned(a.Init)
			}
		case *ast.SwitchStmt:
			if a.Init != nil {
				call, ok = assigned(a.Init)
			}
		case *ast.ForStmt:
			if a.Init != nil {
				call, ok = assigned(a.Init)
			}
		case *ast.FuncDecl, *ast.FuncLit:
			return nil
		}
		if ok {
			return call
		}
	}
	return nil
}
