package golang

import (
	"context"
	"go/ast"
	"go/format"
	"go/parser"
	"go/token"
	"go/types"
	"os"
	"path"
	"path/filepath"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"unicode"
	"unicode/utf8"

	"github.com/rm4n0s/errors"

	"github.com/rm4n0s/emetos/extension"
)

// "Go: Generate Unit Tests For Function" (SPEC §10.17 "As built (step 14.2)"): a table-driven test in the style of gotests for the
// function under the cursor, written by the extension into <file>_test.go. VS Code's Go extension runs gotests (or asks gopls for
// its addTest action); this is the template of gotests in Go, so that no tool has to be installed.

// GenerateTestsCommand is the command that generates the test of the function at the cursor.
const GenerateTestsCommand = ID + ".generateTestsForFunction"

// Limits of the search for the declaration of the type of a receiver in the other files of the package.
const (
	maxSiblingFiles = 500
	maxSiblingBytes = 4 << 20
)

// generated is a test that was made: the text of the function, the imports it needs and the name.
type generated struct {
	// name is the name of the test function (TestFoo), body its text (gofmt-ed, no trailing newline).
	name, body string
	// imports are the packages the function needs, "reflect" and "testing" among them; the standard library first.
	imports []importRef
}

// importRef is an import: the name it is written with (empty when the package name is the last element of the path) and the path.
type importRef struct{ Name, Path string }

// spec is the import as it is written in a block.
func (i importRef) spec() string {
	if i.Name != "" {
		return i.Name + " " + strconv.Quote(i.Path)
	}
	return strconv.Quote(i.Path)
}

// generateTest makes the test of the function that holds the byte offset of src, the text of the file in the folder dir (the files
// of the folder are searched for the declaration of the type of a receiver). It fails with GenerateTestNotAFunction when the offset
// is outside every function, GenerateTestUnsupported for a generic function, and GenerateTestSourceInvalid for a file with syntax
// errors.
func generateTest(src, dir string, offset int) (generated, *ast.File, error) {
	fset := token.NewFileSet()
	file, err := parser.ParseFile(fset, "document.go", src, parser.ParseComments)
	if err != nil {
		return generated{}, nil, errors.NewErr("GenerateTestSourceInvalid", err).SetMessage("the file has a syntax error: " + err.Error())
	}
	tf := fset.File(file.Pos())
	pos := tf.Pos(min(max(offset, 0), tf.Size()))
	var fn *ast.FuncDecl
	for _, d := range file.Decls {
		if f, ok := d.(*ast.FuncDecl); ok {
			start := f.Pos()
			if f.Doc != nil {
				start = f.Doc.Pos()
			}
			if start <= pos && pos <= f.End() {
				fn = f
			}
		}
	}
	if fn == nil {
		return generated{}, nil, errors.New("GenerateTestNotAFunction", "the cursor is not in a function")
	}
	if fn.Type.TypeParams != nil || (fn.Recv != nil && isGenericReceiver(fn.Recv)) {
		return generated{}, nil, errors.New("GenerateTestUnsupported", "generic functions are not supported", "function", fn.Name.Name)
	}
	tfn := newTestFunc(fn)
	if tfn.recv != nil {
		tfn.recv.fields, tfn.recv.known = structFields(file, dir, tfn.recv.typeName)
	}
	body := tfn.render()
	formatted, err := format.Source([]byte(body))
	if err != nil {
		return generated{}, nil, errors.NewErr("GenerateTestSourceInvalid", err).SetMetadata("function", fn.Name.Name)
	}
	g := generated{name: tfn.name, body: strings.TrimRight(string(formatted), "\n")}
	g.imports = tfn.imports(file)
	return g, file, nil
}

// isGenericReceiver tells whether the receiver is the instance of a generic type: T[P].
func isGenericReceiver(recv *ast.FieldList) bool {
	if len(recv.List) == 0 {
		return false
	}
	t := recv.List[0].Type
	if s, ok := t.(*ast.StarExpr); ok {
		t = s.X
	}
	switch t.(type) {
	case *ast.IndexExpr, *ast.IndexListExpr:
		return true
	}
	return false
}

// testFunc is the function a test is made for.
type testFunc struct {
	// name is the name of the test, callee the name of the function or method and label how the messages call it.
	name, callee, label string
	recv                *testRecv
	params              []testParam
	// results are the types of the results that are not the error; returnsErr says that the last result is an error.
	results    []string
	returnsErr bool
	// used are the type expressions printed in the test, for the imports they need.
	used []ast.Expr
}

// testRecv is the receiver of a method.
type testRecv struct {
	// name is the name of the variable, typ the type as written (*Foo) and typeName its name without the star (Foo).
	name, typ, typeName string
	star                bool
	// fields are the fields of the struct, when it is one and was found (known); else the test holds the receiver itself.
	fields []testField
	known  bool
}

// testField is a field of the struct of a receiver.
type testField struct{ name, typ string }

// testParam is a parameter of the function.
type testParam struct {
	name, typ string
	variadic  bool
}

// newTestFunc reads the declaration.
func newTestFunc(fn *ast.FuncDecl) *testFunc {
	t := &testFunc{callee: fn.Name.Name, label: fn.Name.Name}
	if fn.Recv != nil && len(fn.Recv.List) > 0 {
		f := fn.Recv.List[0]
		typ := f.Type
		r := &testRecv{typ: types.ExprString(typ)}
		if s, ok := typ.(*ast.StarExpr); ok {
			r.star = true
			typ = s.X
		}
		r.typeName = types.ExprString(typ)
		first, _ := utf8.DecodeRuneInString(r.typeName)
		r.name = string(unicode.ToLower(first))
		if len(f.Names) > 0 && f.Names[0].Name != "_" {
			r.name = f.Names[0].Name
		}
		t.recv = r
		t.label = r.typeName + "." + fn.Name.Name
		t.used = append(t.used, f.Type)
	}
	t.name = testName(fn.Name.Name, t.recv)
	n := 0
	for _, f := range fn.Type.Params.List {
		typ := types.ExprString(f.Type)
		_, variadic := f.Type.(*ast.Ellipsis)
		if variadic {
			typ = "[]" + types.ExprString(f.Type.(*ast.Ellipsis).Elt)
		}
		t.used = append(t.used, f.Type)
		names := f.Names
		if len(names) == 0 {
			names = []*ast.Ident{nil}
		}
		for _, id := range names {
			n++
			name := "in" + strconv.Itoa(n)
			if id != nil && id.Name != "_" {
				name = id.Name
			}
			t.params = append(t.params, testParam{name: name, typ: typ, variadic: variadic})
		}
	}
	if fn.Type.Results != nil {
		for _, f := range fn.Type.Results.List {
			count := max(len(f.Names), 1)
			for range count {
				t.results = append(t.results, types.ExprString(f.Type))
			}
			t.used = append(t.used, f.Type)
		}
		if k := len(t.results); k > 0 && t.results[k-1] == "error" {
			t.returnsErr = true
			t.results = t.results[:k-1]
		}
	}
	return t
}

// testName is the name of the test of a function or method (gotests): TestFoo, Test_foo, TestT_Method, Test_t_Method.
func testName(name string, r *testRecv) string {
	if r == nil {
		if isLower(name) {
			return "Test_" + name
		}
		return "Test" + name
	}
	if isLower(r.typeName) {
		return "Test_" + r.typeName + "_" + name
	}
	return "Test" + r.typeName + "_" + name
}

// isLower tells whether the first letter is lower case (an underscore or a letter that has no case is not).
func isLower(s string) bool {
	r, _ := utf8.DecodeRuneInString(s)
	return unicode.IsLower(r)
}

// structFields finds the declaration of the type in the file, else in the other files of the package in the folder, and returns
// its fields when it is a struct. known is false when the type is not found or is not a struct.
func structFields(file *ast.File, dir, name string) (fields []testField, known bool) {
	if st := findStruct(file, name); st != nil {
		return fieldsOf(st), true
	}
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil, false
	}
	read := 0
	for _, e := range entries {
		n := e.Name()
		if e.IsDir() || !strings.HasSuffix(n, ".go") || strings.HasSuffix(n, "_test.go") {
			continue
		}
		if info, err := e.Info(); err != nil || info.Size() > maxSiblingBytes {
			continue
		}
		if read++; read > maxSiblingFiles {
			break
		}
		other, err := parser.ParseFile(token.NewFileSet(), filepath.Join(dir, n), nil, parser.SkipObjectResolution)
		if err != nil || other.Name.Name != file.Name.Name {
			continue
		}
		if st := findStruct(other, name); st != nil {
			return fieldsOf(st), true
		}
	}
	return nil, false
}

// findStruct is the struct type declared with the name in the file.
func findStruct(file *ast.File, name string) *ast.StructType {
	for _, d := range file.Decls {
		gd, ok := d.(*ast.GenDecl)
		if !ok || gd.Tok != token.TYPE {
			continue
		}
		for _, s := range gd.Specs {
			if ts, ok := s.(*ast.TypeSpec); ok && ts.Name.Name == name && !ts.Assign.IsValid() {
				if st, ok := ts.Type.(*ast.StructType); ok {
					return st
				}
			}
		}
	}
	return nil
}

// fieldsOf lists the fields of a struct: each name of each field, the type name for an embedded one, and none for _.
func fieldsOf(st *ast.StructType) []testField {
	var out []testField
	for _, f := range st.Fields.List {
		typ := types.ExprString(f.Type)
		if len(f.Names) == 0 {
			e := f.Type
			if s, ok := e.(*ast.StarExpr); ok {
				e = s.X
			}
			if sel, ok := e.(*ast.SelectorExpr); ok {
				out = append(out, testField{name: sel.Sel.Name, typ: typ})
			} else {
				out = append(out, testField{name: types.ExprString(e), typ: typ})
			}
			continue
		}
		for _, id := range f.Names {
			if id.Name != "_" {
				out = append(out, testField{name: id.Name, typ: typ})
			}
		}
	}
	return out
}

// valueName is want, want1, want2, ... or got, got1, ...
func valueName(prefix string, i int) string {
	if i == 0 {
		return prefix
	}
	return prefix + strconv.Itoa(i)
}

// call is the expression that calls the function, with the arguments of the table.
func (t *testFunc) call() string {
	args := make([]string, len(t.params))
	for i, p := range t.params {
		args[i] = "tt.args." + p.name
		if p.variadic {
			args[i] += "..."
		}
	}
	callee := t.callee
	if r := t.recv; r != nil {
		if r.useFields() {
			callee = r.name + "." + callee
		} else {
			callee = "tt." + r.name + "." + callee
		}
	}
	return callee + "(" + strings.Join(args, ", ") + ")"
}

// useFields says that the test builds the receiver from the fields of its struct.
func (r *testRecv) useFields() bool { return r.known }

// render writes the function. gofmt aligns it afterwards.
func (t *testFunc) render() string {
	var b strings.Builder
	w := func(s string) { b.WriteString(s + "\n") }
	w("func " + t.name + "(t *testing.T) {")
	hasFields := t.recv != nil && t.recv.useFields() && len(t.recv.fields) > 0
	if hasFields {
		w("type fields struct {")
		for _, f := range t.recv.fields {
			w(f.name + " " + f.typ)
		}
		w("}")
	}
	if len(t.params) > 0 {
		w("type args struct {")
		for _, p := range t.params {
			w(p.name + " " + p.typ)
		}
		w("}")
	}
	w("tests := []struct {")
	w("name string")
	if t.recv != nil {
		if hasFields {
			w("fields fields")
		} else if !t.recv.useFields() {
			w(t.recv.name + " " + t.recv.typ)
		}
	}
	if len(t.params) > 0 {
		w("args args")
	}
	for i, r := range t.results {
		w(valueName("want", i) + " " + r)
	}
	if t.returnsErr {
		w("wantErr bool")
	}
	w("}{")
	w("// TODO: Add test cases.")
	w("}")
	w("for _, tt := range tests {")
	w("t.Run(tt.name, func(t *testing.T) {")
	if r := t.recv; r != nil && r.useFields() {
		amp := ""
		if r.star {
			amp = "&"
		}
		if hasFields {
			w(r.name + " := " + amp + r.typeName + "{")
			for _, f := range r.fields {
				w(f.name + ": tt.fields." + f.name + ",")
			}
			w("}")
		} else {
			w(r.name + " := " + amp + r.typeName + "{}")
		}
	}
	t.renderCall(w)
	w("})")
	w("}")
	w("}")
	return b.String()
}

// renderCall writes the call and the checks of its results.
func (t *testFunc) renderCall(w func(string)) {
	call, n := t.call(), len(t.results)
	errCheck := func(indent string) {
		w(indent + `t.Errorf("` + t.label + `() error = %v, wantErr %v", err, tt.wantErr)`)
	}
	switch {
	case n == 0 && !t.returnsErr:
		w(call)
	case n == 0:
		w("if err := " + call + "; (err != nil) != tt.wantErr {")
		errCheck("")
		w("}")
	case n == 1 && !t.returnsErr:
		w("if got := " + call + "; !reflect.DeepEqual(got, tt.want) {")
		w(`t.Errorf("` + t.label + `() = %v, want %v", got, tt.want)`)
		w("}")
	default:
		gots := make([]string, 0, n+1)
		for i := range t.results {
			gots = append(gots, valueName("got", i))
		}
		if t.returnsErr {
			gots = append(gots, "err")
		}
		w(strings.Join(gots, ", ") + " := " + call)
		if t.returnsErr {
			w("if (err != nil) != tt.wantErr {")
			errCheck("")
			w("return")
			w("}")
		}
		for i := range t.results {
			got, want := valueName("got", i), valueName("want", i)
			w("if !reflect.DeepEqual(" + got + ", tt." + want + ") {")
			label := t.label + "()"
			if n > 1 {
				label += " " + got
			}
			w(`t.Errorf("` + label + ` = %v, want %v", ` + got + ", tt." + want + ")")
			w("}")
		}
	}
}

// imports are the packages the test needs: reflect when it compares results, testing, and those the types in it use that the source
// file imports. The standard library comes first, each group in order.
func (t *testFunc) imports(file *ast.File) []importRef {
	need := map[string]bool{}
	for _, e := range t.used {
		ast.Inspect(e, func(n ast.Node) bool {
			if sel, ok := n.(*ast.SelectorExpr); ok {
				if id, ok := sel.X.(*ast.Ident); ok {
					need[id.Name] = true
				}
			}
			return true
		})
	}
	if t.recv != nil {
		for _, f := range t.recv.fields {
			for _, name := range typeQualifiers(f.typ) {
				need[name] = true
			}
		}
	}
	out := []importRef{{Path: "testing"}}
	if len(t.results) > 0 {
		out = append(out, importRef{Path: "reflect"})
	}
	for _, spec := range file.Imports {
		p, err := strconv.Unquote(spec.Path.Value)
		if err != nil {
			continue
		}
		name := ""
		if spec.Name != nil {
			name = spec.Name.Name
		}
		pkg := name
		if pkg == "" {
			pkg = guessPackageName(p)
		}
		if need[pkg] && name != "_" && name != "." {
			ref := importRef{Path: p}
			if name != "" {
				ref.Name = name
			}
			out = append(out, ref)
		}
	}
	sortImports(out)
	return out
}

// qualifierPattern finds pkg in pkg.Name inside the text of a type.
var qualifierPattern = regexp.MustCompile(`([A-Za-z_][A-Za-z0-9_]*)\.[A-Za-z_]`)

// typeQualifiers are the package names a type written as text mentions.
func typeQualifiers(typ string) []string {
	var out []string
	for _, m := range qualifierPattern.FindAllStringSubmatch(typ, -1) {
		out = append(out, m[1])
	}
	return out
}

// versionSuffix is the last element of a module path that is a major version: v2.
var versionSuffix = regexp.MustCompile(`^v[0-9]+$`)

// guessPackageName is the name a package is most likely used by, from its import path: the last element, without a major version
// element, a ".v3" suffix, a "go-" prefix or a "-go" suffix. A guess may be wrong (a package can be named anything); then the
// import is not added.
func guessPackageName(importPath string) string {
	base := path.Base(importPath)
	if versionSuffix.MatchString(base) {
		base = path.Base(path.Dir(importPath))
	}
	base, _, _ = strings.Cut(base, ".")
	base = strings.TrimPrefix(strings.TrimSuffix(base, "-go"), "go-")
	return strings.ReplaceAll(base, "-", "_")
}

// isStdlib tells whether an import path is of the standard library: its first element has no dot.
func isStdlib(importPath string) bool {
	first, _, _ := strings.Cut(importPath, "/")
	return !strings.Contains(first, ".")
}

// sortImports puts the standard library first, then the others, each in the order of the paths.
func sortImports(refs []importRef) {
	slices.SortFunc(refs, func(a, b importRef) int {
		if sa, sb := isStdlib(a.Path), isStdlib(b.Path); sa != sb {
			if sa {
				return -1
			}
			return 1
		}
		return strings.Compare(a.Path, b.Path)
	})
}

// importBlock is the import declaration for the imports: one block, the standard library apart from the rest; a line for one import.
func importBlock(refs []importRef) string {
	if len(refs) == 1 {
		return "import " + refs[0].spec()
	}
	var b strings.Builder
	b.WriteString("import (\n")
	for i, r := range refs {
		if i > 0 && isStdlib(refs[i-1].Path) && !isStdlib(r.Path) {
			b.WriteString("\n")
		}
		b.WriteString("\t" + r.spec() + "\n")
	}
	b.WriteString(")")
	return b.String()
}

// ---- writing the test ----

// testFilePath is the path of the test file of a Go file: foo.go has foo_test.go.
func testFilePath(path string) string { return strings.TrimSuffix(path, ".go") + "_test.go" }

// newTestFile is the text of a test file that holds the test: the package clause, the imports and the function.
func newTestFile(pkg string, g generated) string {
	return "package " + pkg + "\n\n" + importBlock(g.imports) + "\n\n" + g.body + "\n"
}

// appendTest returns the edits that add the test to the text of an existing test file, and the line (from 0) of the new function in
// the result. Imports it lacks are added to its first import declaration, at the end of the block, or after the package clause when
// it has none. It fails with GenerateTestSourceInvalid for a file that does not parse and GenerateTestExists, with the line of the
// function, when the file has the test.
func appendTest(text, pkg string, g generated) ([]extension.RangeEdit, int, error) {
	if strings.TrimSpace(text) == "" {
		edits := []extension.RangeEdit{{Range: extension.Range{Start: positionAt(text, 0), End: positionAt(text, len(text))}, Text: newTestFile(pkg, g)}}
		return edits, lineOf(newTestFile(pkg, g), "func "+g.name+"("), nil
	}
	fset := token.NewFileSet()
	file, err := parser.ParseFile(fset, "test.go", text, parser.ParseComments)
	if err != nil {
		return nil, 0, errors.NewErr("GenerateTestSourceInvalid", err).SetMessage("the file has a syntax error: " + err.Error())
	}
	for _, d := range file.Decls {
		if f, ok := d.(*ast.FuncDecl); ok && f.Recv == nil && f.Name.Name == g.name {
			return nil, fset.Position(f.Pos()).Line - 1, errors.New("GenerateTestExists", "the test exists already", "test", g.name)
		}
	}
	tf := fset.File(file.Pos())
	have := map[string]bool{}
	for _, s := range file.Imports {
		if p, err := strconv.Unquote(s.Path.Value); err == nil {
			have[p] = true
		}
	}
	var missing []importRef
	for _, r := range g.imports {
		if !have[r.Path] {
			missing = append(missing, r)
		}
	}
	var edits []extension.RangeEdit
	if len(missing) > 0 {
		edits = append(edits, importEdits(text, file, tf, missing)...)
	}
	sep := "\n"
	if !strings.HasSuffix(text, "\n") {
		sep = "\n\n"
	}
	end := positionAt(text, len(text))
	edits = append(edits, extension.RangeEdit{Range: extension.Range{Start: end, End: end}, Text: sep + g.body + "\n"})
	return edits, lineOf(applyRangeEdits(text, edits), "func "+g.name+"("), nil
}

// importEdits adds the imports to the text of a file: into the group of the standard library or of the others of its first import
// block, in the order of the paths, else as a block of their own; an import declaration without parentheses becomes a block; a file
// without imports gets a block after the package clause.
func importEdits(text string, file *ast.File, tf *token.File, missing []importRef) []extension.RangeEdit {
	at := func(p token.Pos) extension.Position { return positionAt(text, tf.Offset(p)) }
	lineStart := func(line int) extension.Position { return extension.Position{Line: line} }
	var decl *ast.GenDecl
	for _, d := range file.Decls {
		if gd, ok := d.(*ast.GenDecl); ok && gd.Tok == token.IMPORT {
			decl = gd
			break
		}
	}
	insert := func(p extension.Position, text string) extension.RangeEdit {
		return extension.RangeEdit{Range: extension.Range{Start: p, End: p}, Text: text}
	}
	switch {
	case decl == nil:
		return []extension.RangeEdit{insert(at(file.Name.End()), "\n\n"+importBlock(missing))}
	case !decl.Lparen.IsValid():
		var all []importRef
		for _, s := range decl.Specs {
			all = append(all, refOf(s.(*ast.ImportSpec)))
		}
		all = append(all, missing...)
		sortImports(all)
		return []extension.RangeEdit{{Range: extension.Range{Start: at(decl.Pos()), End: at(decl.End())}, Text: importBlock(all)}}
	}
	// the groups of the block: runs of specs on consecutive lines
	var groups [][]*ast.ImportSpec
	for _, s := range decl.Specs {
		spec := s.(*ast.ImportSpec)
		if n := len(groups); n > 0 {
			prev := groups[n-1][len(groups[n-1])-1]
			if at(specStart(spec)).Line-at(prev.End()).Line <= 1 {
				groups[n-1] = append(groups[n-1], spec)
				continue
			}
		}
		groups = append(groups, []*ast.ImportSpec{spec})
	}
	var edits []extension.RangeEdit
	var newStd, newOther []importRef
	for _, m := range missing {
		var group []*ast.ImportSpec
		for _, g := range groups {
			if isStdlib(refOf(g[0]).Path) == isStdlib(m.Path) {
				group = g
				break
			}
		}
		line := "\t" + m.spec() + "\n"
		switch {
		case group == nil && isStdlib(m.Path):
			newStd = append(newStd, m)
		case group == nil:
			newOther = append(newOther, m)
		default:
			// before the first spec of the group with a greater path, else after the last
			at0 := lineStart(at(group[len(group)-1].End()).Line + 1)
			for _, s := range group {
				if refOf(s).Path > m.Path {
					at0 = lineStart(at(specStart(s)).Line)
					break
				}
			}
			edits = append(edits, insert(at0, line))
		}
	}
	if len(newStd) > 0 { // the standard library goes first, apart from the rest
		first := lineStart(at(decl.Lparen).Line + 1)
		edits = append(edits, insert(first, importLines(newStd)+"\n"))
	}
	if len(newOther) > 0 {
		last := lineStart(at(decl.Rparen).Line)
		edits = append(edits, insert(last, "\n"+importLines(newOther)))
	}
	return edits
}

// importLines are the lines of the specs of the imports, in a block.
func importLines(refs []importRef) string {
	var b strings.Builder
	for _, r := range refs {
		b.WriteString("\t" + r.spec() + "\n")
	}
	return b.String()
}

// specStart is where an import spec starts, with its comment.
func specStart(s *ast.ImportSpec) token.Pos {
	if s.Doc != nil {
		return s.Doc.Pos()
	}
	return s.Pos()
}

// refOf is the import of a spec.
func refOf(s *ast.ImportSpec) importRef {
	p, _ := strconv.Unquote(s.Path.Value)
	ref := importRef{Path: p}
	if s.Name != nil {
		ref.Name = s.Name.Name
	}
	return ref
}

// lineOf is the line, from 0, of the first line of the text that starts with the prefix; 0 when there is none.
func lineOf(text, prefix string) int {
	for i, l := range strings.Split(text, "\n") {
		if strings.HasPrefix(l, prefix) {
			return i
		}
	}
	return 0
}

// applyRangeEdits applies edits, whose ranges are in the text, from the end to the start; edits at one place keep their order.
func applyRangeEdits(text string, edits []extension.RangeEdit) string {
	type span struct {
		from, to, index int
		text            string
	}
	spans := make([]span, len(edits))
	for i, e := range edits {
		spans[i] = span{offsetOf(text, e.Range.Start), offsetOf(text, e.Range.End), i, e.Text}
	}
	slices.SortFunc(spans, func(a, b span) int {
		if a.from != b.from {
			return b.from - a.from
		}
		return b.index - a.index
	})
	for _, s := range spans {
		text = text[:s.from] + s.text + text[s.to:]
	}
	return text
}

// ---- the command ----

// generateTestsCommand is the command "Go: Generate Unit Tests For Function".
func (g *golang) generateTestsCommand(ctx context.Context, _ any) error {
	const what = "Generate Unit Tests"
	file, ok := g.activeGoFile(ctx, what)
	if !ok {
		return nil
	}
	name := filepath.Base(file.path)
	if strings.HasSuffix(file.path, "_test.go") {
		g.api.Notifications().Info(what + " works on a function of a file that is not a test file; " + name + " is one.")
		return nil
	}
	g.generateTests(ctx, what, file)
	return nil
}

// generateTests makes the test of the function at the cursor of the file and writes it to the test file.
func (g *golang) generateTests(ctx context.Context, what string, file goFile) {
	gen, parsed, err := generateTest(file.text, filepath.Dir(file.path), offsetOf(file.text, file.sel.Active))
	if err != nil {
		g.api.Notifications().Warn(what + ": " + describe(err))
		return
	}
	pkg := parsed.Name.Name
	target := testFilePath(file.path)
	targetURI := fileURI(target)
	base := filepath.Base(target)
	if !g.exists(target, targetURI) {
		// a new file is written whole, so that no empty or half written Go file is left on disk
		if err := writeNewFile(target, newTestFile(pkg, gen)); err != nil {
			g.api.Log().Warn("the test file cannot be written", "path", target, "err", err)
			g.api.Notifications().Error(what + ": " + base + " cannot be written: " + describe(err))
			return
		}
		g.showTest(targetURI, lineOf(newTestFile(pkg, gen), "func "+gen.name+"("))
		g.api.Notifications().Info("Generated " + gen.name + " in " + base + ".")
		return
	}
	text, err := g.api.Documents().Read(ctx, targetURI)
	if err != nil {
		g.api.Log().Warn("the test file cannot be read", "uri", targetURI, "err", err)
		g.api.Notifications().Error(what + ": " + base + " cannot be read: " + describe(err))
		return
	}
	edits, line, err := appendTest(text, pkg, gen)
	if appErr, ok := errors.FromError(err); ok && appErr.Tag == "GenerateTestExists" {
		g.showTest(targetURI, line)
		g.api.Notifications().Info(gen.name + " exists already in " + base + ", so no test was added.")
		return
	}
	if err != nil {
		g.api.Notifications().Warn(what + ": " + base + ": " + describe(err))
		return
	}
	edit := extension.WorkspaceEdit{Changes: []extension.ResourceChange{{Kind: "edit", URI: targetURI, Edits: edits}}}
	if err := g.api.Documents().Apply(ctx, edit); err != nil {
		g.api.Log().Warn("the test cannot be added", "uri", targetURI, "err", err)
		g.api.Notifications().Error(what + ": the edit of " + base + " cannot be applied: " + describe(err))
		return
	}
	g.showTest(targetURI, line)
	g.api.Notifications().Info("Added " + gen.name + " to " + base + ".")
}

// exists tells whether the file is on disk or open in the editor (with changes that are not saved).
func (g *golang) exists(path, uri string) bool {
	if _, err := os.Stat(path); err == nil {
		return true
	}
	return slices.Contains(g.api.Documents().Open(), uri)
}

// showTest opens the file with the cursor on the line (from 0).
func (g *golang) showTest(uri string, line int) {
	if err := g.api.Documents().Show(uri, line+1, 1, false); err != nil {
		g.api.Log().Warn("the test file cannot be shown", "uri", uri, "err", err)
	}
}

// writeNewFile writes a file that must not exist yet. It fails with FileWriteFailed.
func writeNewFile(path, text string) error {
	f, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o644)
	if err != nil {
		return errors.NewErr("FileWriteFailed", err).SetMetadata("path", path)
	}
	if _, err := f.WriteString(text); err != nil {
		_ = f.Close()
		return errors.NewErr("FileWriteFailed", err).SetMetadata("path", path)
	}
	if err := f.Close(); err != nil {
		return errors.NewErr("FileWriteFailed", err).SetMetadata("path", path)
	}
	return nil
}
