package archtest

import (
	"go/ast"
	"go/token"
	"go/types"
	"os"
	"path"
	"regexp"
	"sort"
	"strconv"
	"strings"

	"github.com/rm4n0s/errors"
)

// errorTagRoots are the folders, relative to the repository root, whose Go files
// make and test the errors of Emetos. extension and extensions are the public
// contract and the bundled extensions of M11; they may not exist yet.
var errorTagRoots = []string{"cmd/", "internal/", "extension/", "extensions/"}

// TagSite is one place that makes an error with a Tag.
type TagSite struct {
	// Tag is the first argument of errors.New, errors.NewErr or errors.NewErrf.
	Tag string
	// File is the slash-separated path of the file relative to the root.
	File string
	// Line is the line of the call.
	Line int
	// Func is the function that makes the error, as the route names it
	// (Type.Method or Func).
	Func string
}

// ErrorTags returns the Tag of every errors.New, errors.NewErr and errors.NewErrf
// call in the non-test Go files of cmd, internal, extension and extensions below
// root, sorted by Tag, file and line. A call whose first argument is not a string
// literal has no Tag to list: it is returned as a RuleNonLiteralTag violation, so
// a Tag cannot hide in a variable.
func ErrorTags(root string) ([]TagSite, []Violation, error) {
	var sites []TagSite
	var bad []Violation
	err := walkGo(root, func(rel string, fset *token.FileSet, file *ast.File) {
		if !inErrorTagRoots(rel) || strings.HasSuffix(rel, "_test.go") {
			return
		}
		errorsName := errorsImportName(file)
		if errorsName == "" {
			return
		}
		for _, decl := range file.Decls {
			fn, isFunc := decl.(*ast.FuncDecl)
			owner := ""
			if isFunc {
				owner = funcRouteName(fn)
			}
			ast.Inspect(decl, func(n ast.Node) bool {
				call, ok := n.(*ast.CallExpr)
				if !ok || len(call.Args) == 0 || !isConstructor(call, errorsName) {
					return true
				}
				line := fset.Position(call.Pos()).Line
				lit, ok := call.Args[0].(*ast.BasicLit)
				if !ok || lit.Kind != token.STRING {
					bad = append(bad, Violation{File: rel, Line: line, Rule: RuleNonLiteralTag, Detail: constructorName(call) + "(" + types.ExprString(call.Args[0]) + ", ...)"})
					return true
				}
				tag, err := strconv.Unquote(lit.Value)
				if err != nil {
					return true // the parser accepted the literal, so this cannot happen
				}
				sites = append(sites, TagSite{Tag: tag, File: rel, Line: line, Func: owner})
				return true
			})
		}
	})
	if err != nil {
		return nil, nil, err
	}
	sort.Slice(sites, func(i, j int) bool {
		a, b := sites[i], sites[j]
		if a.Tag != b.Tag {
			return a.Tag < b.Tag
		}
		if a.File != b.File {
			return a.File < b.File
		}
		return a.Line < b.Line
	})
	sort.Slice(bad, func(i, j int) bool {
		if bad[i].File != bad[j].File {
			return bad[i].File < bad[j].File
		}
		return bad[i].Line < bad[j].Line
	})
	return sites, bad, nil
}

// UntestedErrorTags returns the sites of the Tags that no test asserts, sorted
// like ErrorTags, and the violations ErrorTags found. A test asserts a Tag when,
// in a Go test file of cmd, internal, extension or extensions below root:
//
//   - a string that is an argument of HasRoute ends in the Tag (after the last
//     "." of the route, or is the Tag alone);
//   - a string of a test file of a folder where some test calls HasRoute or
//     compares a .Tag with == or != has the form of a route, Func.Tag or
//     Type.Method->Func.Tag, and ends in the Tag (the strings of a table of cases,
//     and of helpers like wantRoute(t, err, "Func.Tag"), are found this way);
//   - a string of such a folder is the Tag alone, when a test of the folder
//     compares a .Tag or gives HasRoute a route that is not a literal (tables of
//     Tags, and helpers like wantTag(t, err, "Func", "Tag"));
//   - errors.Is is given a &errors.Error{Tag: "..."} sentinel.
//
// A constant of the folder with a string value counts as that string. A string
// that is the first argument of errors.New, errors.NewErr or errors.NewErrf (a
// fake error of a test) never counts, and neither does a Tag named only in a
// comment.
func UntestedErrorTags(root string) ([]TagSite, []Violation, error) {
	sites, bad, err := ErrorTags(root)
	if err != nil {
		return nil, nil, err
	}
	known := map[string]bool{}
	for _, s := range sites {
		known[s.Tag] = true
	}
	tested, err := assertedErrorTags(root, known)
	if err != nil {
		return nil, nil, err
	}
	var untested []TagSite
	for _, s := range sites {
		if !tested[s.Tag] {
			untested = append(untested, s)
		}
	}
	return untested, bad, nil
}

// ReadUntestedList reads docs/ERROR-TAGS-UNTESTED.md, the only exception list of
// the route tests: the Tags it names, each with its reason. A line of the form
// "- `Tag`: reason" names one; every other line is prose. A file that does not
// exist is an empty list.
//
// The error has the tag ErrorTagsExceptionsReadFailed.
func ReadUntestedList(file string) (map[string]string, error) {
	data, err := os.ReadFile(file)
	if os.IsNotExist(err) {
		return map[string]string{}, nil
	}
	if err != nil {
		return nil, errors.NewErr("ErrorTagsExceptionsReadFailed", err).SetMetadata("path", file)
	}
	out := map[string]string{}
	for _, line := range strings.Split(string(data), "\n") {
		if m := exceptionLine.FindStringSubmatch(line); m != nil {
			out[m[1]] = strings.TrimSpace(m[2])
		}
	}
	return out, nil
}

var exceptionLine = regexp.MustCompile("^[-*] `([A-Za-z][A-Za-z0-9]*)`:?(.*)$")

// routeShape is a route as the errors package prints it: Func.Tag or
// Type.Method.Tag, joined by "->".
var routeShape = regexp.MustCompile(`^(\*?[A-Za-z_][A-Za-z0-9_]*(\.[A-Za-z_][A-Za-z0-9_]*)*->)*[A-Za-z_][A-Za-z0-9_]*(\.[A-Za-z_][A-Za-z0-9_]*)*\.([A-Za-z][A-Za-z0-9]*)$`)

// tagShape is a Tag alone.
var tagShape = regexp.MustCompile(`^[A-Z][A-Za-z0-9]*$`)

// assertedErrorTags returns the members of known that a test of root asserts
// (see UntestedErrorTags).
func assertedErrorTags(root string, known map[string]bool) (map[string]bool, error) {
	type testFile struct {
		rel  string
		file *ast.File
	}
	consts := map[string]map[string]string{} // folder -> constant -> string value
	var tests []testFile
	err := walkGo(root, func(rel string, _ *token.FileSet, file *ast.File) {
		if !inErrorTagRoots(rel) {
			return
		}
		dir := path.Dir(rel)
		if consts[dir] == nil {
			consts[dir] = map[string]string{}
		}
		for name, val := range stringConsts(file) {
			consts[dir][name] = val
		}
		if strings.HasSuffix(rel, "_test.go") {
			tests = append(tests, testFile{rel, file})
		}
	})
	if err != nil {
		return nil, err
	}
	tested := map[string]bool{}
	type marks struct {
		skip   map[ast.Expr]bool // fake errors: never an assertion
		direct map[ast.Expr]bool // the operands of assertions
		str    func(ast.Expr) (string, bool)
	}
	folderAsserts := map[string]bool{}     // a test of the folder calls HasRoute or compares a .Tag
	folderComparesTag := map[string]bool{} // a test of the folder compares a .Tag
	fileMarks := make([]marks, len(tests))
	for i, tf := range tests {
		dir := path.Dir(tf.rel)
		errorsName := errorsImportName(tf.file)
		values := consts[dir]
		// str gives the string an expression stands for, or false.
		str := func(e ast.Expr) (string, bool) {
			switch x := e.(type) {
			case *ast.BasicLit:
				if x.Kind == token.STRING {
					v, err := strconv.Unquote(x.Value)
					return v, err == nil
				}
			case *ast.Ident:
				v, ok := values[x.Name]
				return v, ok
			}
			return "", false
		}
		m := marks{skip: map[ast.Expr]bool{}, direct: map[ast.Expr]bool{}, str: str}
		ast.Inspect(tf.file, func(n ast.Node) bool {
			switch x := n.(type) {
			case *ast.CallExpr:
				if errorsName != "" && isConstructor(x, errorsName) && len(x.Args) > 0 {
					m.skip[x.Args[0]] = true
				}
				if sel, ok := x.Fun.(*ast.SelectorExpr); ok {
					switch {
					case sel.Sel.Name == "HasRoute":
						folderAsserts[dir] = true
						for _, a := range x.Args {
							m.direct[a] = true
							if _, ok := str(a); !ok {
								// A route built by a helper (route+"."+tag): the Tags are its arguments.
								folderComparesTag[dir] = true
							}
						}
					case sel.Sel.Name == "Is" && errorsName != "" && isIdent(sel.X, errorsName):
						for _, a := range x.Args {
							if tag, ok := sentinelTag(a, errorsName, str); ok && known[tag] {
								tested[tag] = true
							}
						}
					}
				}
			case *ast.BinaryExpr:
				if x.Op != token.EQL && x.Op != token.NEQ {
					return true
				}
				for _, pair := range [][2]ast.Expr{{x.X, x.Y}, {x.Y, x.X}} {
					if sel, ok := pair[0].(*ast.SelectorExpr); ok && sel.Sel.Name == "Tag" {
						folderAsserts[dir], folderComparesTag[dir] = true, true
						m.direct[pair[1]] = true
					}
				}
			}
			return true
		})
		fileMarks[i] = m
	}
	for i, tf := range tests {
		dir := path.Dir(tf.rel)
		if !folderAsserts[dir] {
			continue
		}
		m := fileMarks[i]
		ast.Inspect(tf.file, func(n ast.Node) bool {
			e, ok := n.(ast.Expr)
			if !ok || m.skip[e] {
				return true
			}
			v, ok := m.str(e)
			if !ok {
				return true
			}
			switch {
			case m.direct[e]:
				tag := v
				if j := strings.LastIndex(v, "."); j >= 0 {
					tag = v[j+1:]
				}
				if known[tag] {
					tested[tag] = true
				}
			case routeShape.MatchString(v):
				if r := routeShape.FindStringSubmatch(v); known[r[4]] {
					tested[r[4]] = true
				}
			case folderComparesTag[dir] && tagShape.MatchString(v) && known[v]:
				tested[v] = true
			}
			return true
		})
	}
	return tested, nil
}

// sentinelTag returns the Tag of a &errors.Error{Tag: "..."} expression.
func sentinelTag(e ast.Expr, errorsName string, str func(ast.Expr) (string, bool)) (string, bool) {
	if u, ok := e.(*ast.UnaryExpr); ok && u.Op == token.AND {
		e = u.X
	}
	lit, ok := e.(*ast.CompositeLit)
	if !ok {
		return "", false
	}
	sel, ok := lit.Type.(*ast.SelectorExpr)
	if !ok || !isIdent(sel.X, errorsName) || sel.Sel.Name != "Error" {
		return "", false
	}
	for _, el := range lit.Elts {
		if kv, ok := el.(*ast.KeyValueExpr); ok && isIdent(kv.Key, "Tag") {
			return str(kv.Value)
		}
	}
	return "", false
}

// stringConsts returns the constants of a file with a string literal value.
func stringConsts(file *ast.File) map[string]string {
	out := map[string]string{}
	for _, decl := range file.Decls {
		gen, ok := decl.(*ast.GenDecl)
		if !ok || gen.Tok != token.CONST {
			continue
		}
		for _, spec := range gen.Specs {
			vs, ok := spec.(*ast.ValueSpec)
			if !ok {
				continue
			}
			for i, name := range vs.Names {
				if i >= len(vs.Values) {
					break
				}
				if lit, ok := vs.Values[i].(*ast.BasicLit); ok && lit.Kind == token.STRING {
					if v, err := strconv.Unquote(lit.Value); err == nil {
						out[name.Name] = v
					}
				}
			}
		}
	}
	return out
}

// inErrorTagRoots reports whether a slash path is below one of errorTagRoots.
func inErrorTagRoots(rel string) bool {
	for _, r := range errorTagRoots {
		if strings.HasPrefix(rel, r) {
			return true
		}
	}
	return false
}

// errorsImportName returns the local name of github.com/rm4n0s/errors in a file,
// or "" when it is not imported (or imported as _ or .).
func errorsImportName(file *ast.File) string {
	for _, imp := range file.Imports {
		if p, err := strconv.Unquote(imp.Path.Value); err == nil && p == errorsModule {
			name := "errors"
			if imp.Name != nil {
				name = imp.Name.Name
			}
			if name == "_" || name == "." {
				return ""
			}
			return name
		}
	}
	return ""
}

// isConstructor reports whether call is errors.New, errors.NewErr or
// errors.NewErrf.
func isConstructor(call *ast.CallExpr, errorsName string) bool {
	sel, ok := call.Fun.(*ast.SelectorExpr)
	return ok && isIdent(sel.X, errorsName) && errorConstructors[sel.Sel.Name]
}

// isIdent reports whether e is the identifier name.
func isIdent(e ast.Expr, name string) bool {
	id, ok := e.(*ast.Ident)
	return ok && id.Name == name
}

// funcRouteName names a function declaration as a route does: Type.Method for a
// method, Func for a function.
func funcRouteName(fn *ast.FuncDecl) string {
	if fn.Recv == nil || len(fn.Recv.List) == 0 {
		return fn.Name.Name
	}
	t := fn.Recv.List[0].Type
	for {
		switch x := t.(type) {
		case *ast.StarExpr:
			t = x.X
			continue
		case *ast.IndexExpr:
			t = x.X
			continue
		case *ast.IndexListExpr:
			t = x.X
			continue
		case *ast.Ident:
			return x.Name + "." + fn.Name.Name
		}
		return fn.Name.Name
	}
}
